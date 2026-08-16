package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/l3aro/perk-workbench/internal/database/plugin"
	"github.com/l3aro/perk-workbench/internal/database/plugin/conformance"
)

// pluginCaseTimeout bounds one conformance case: a child that does not
// answer within the bound fails the case and is terminated. The whole
// run stays signal-aware and every child is reaped before the next case
// starts. Tests shorten the bound.
var pluginCaseTimeout = 30 * time.Second

// pluginQuietBound is the silence window each case requires after its
// expected exchanges — catching duplicate or fabricated responses and
// premature exit — and the window a deliberately invalid input frame
// must stay quiet for.
var pluginQuietBound = 200 * time.Millisecond

// runPluginTest resolves one executable and runs the perk/v1
// conformance suite against it: fixture-driven protocol cases and
// generated transport cases, each in a fresh child that is terminated
// and reaped when the case ends. The suite never invokes build_target,
// open, or any session RPC, so a transport-only plugin needs no
// backend. --json emits exactly one self-contained release evidence
// document on stdout, pass or fail; human output prints a
// protocol/host/contract/executable summary, one PASS/FAIL line per
// case, and final counts. Raw protocol frames and request data are
// never reported.
func runPluginTest(jsonOut bool, entry string, stdout, stderr io.Writer) int {
	doc, err := conformance.NewDocument(entry)
	if err != nil {
		// No evidence document can be produced: the embedded contract
		// assets are unreadable. Diagnostics go to stderr.
		fmt.Fprintf(stderr, "plugin test %s: %v\n", entry, err)
		return 1
	}
	doc.HostVersion = hostVersion()
	path, err := plugin.ResolveExecutable(entry, "")
	if err != nil {
		doc.Error = "resolve: " + err.Error()
		return emitTestDocument(jsonOut, stdout, doc)
	}
	doc.Path = path

	engine, err := conformance.New()
	if err != nil {
		doc.Error = err.Error()
		return emitTestDocument(jsonOut, stdout, doc)
	}
	engine.Timeout = pluginCaseTimeout
	engine.Quiet = pluginQuietBound

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	result := engine.Test(ctx, entry, path)
	result.HostVersion = doc.HostVersion
	return emitTestDocument(jsonOut, stdout, result)
}

// emitTestDocument emits one evidence document as JSON or human
// output and returns the exit status (0 pass, 1 failure).
func emitTestDocument(jsonOut bool, stdout io.Writer, doc conformance.Document) int {
	if jsonOut {
		return emitJSON(stdout, doc, exitCode(doc.OK))
	}
	printTestReport(stdout, doc)
	return exitCode(doc.OK)
}

// printTestReport renders one conformance run as concise deterministic
// human output: the protocol/host/contract/executable evidence summary,
// one PASS/FAIL line per case with its duration, the bounded stderr
// tail for failed cases, any suite-level error, and the final counts.
// Raw protocol frames are never shown.
func printTestReport(w io.Writer, doc conformance.Document) {
	fmt.Fprintf(w, "plugin test %s:\n", doc.Entry)
	if doc.ProtocolVersion != 0 {
		fmt.Fprintf(w, "  protocol: perk/v%d\n", doc.ProtocolVersion)
	}
	if doc.HostVersion != "" {
		fmt.Fprintf(w, "  host: %s\n", doc.HostVersion)
	}
	if doc.ContractSHA256 != "" {
		fmt.Fprintf(w, "  contract: sha256:%s\n", doc.ContractSHA256)
	}
	if doc.Path != "" && doc.Path != doc.Entry {
		fmt.Fprintf(w, "  path: %s\n", doc.Path)
	}
	if doc.ExecutableSHA256 != "" {
		fmt.Fprintf(w, "  executable: sha256:%s\n", doc.ExecutableSHA256)
	}
	if doc.Capabilities != nil {
		fmt.Fprintf(w, "  capabilities: %s", doc.Capabilities.Name)
		if doc.Capabilities.Display != "" {
			fmt.Fprintf(w, " (%s)", doc.Capabilities.Display)
		}
		fmt.Fprintln(w)
	}
	for _, result := range doc.Cases {
		if result.OK {
			fmt.Fprintf(w, "  %-28s PASS  %s\n", result.Name, result.Duration)
			continue
		}
		fmt.Fprintf(w, "  %-28s FAIL  %s: %s\n", result.Name, result.Category, result.Error)
		if len(result.Stderr) > 0 {
			fmt.Fprintln(w, "    stderr (tail):")
			for _, line := range result.Stderr {
				fmt.Fprintf(w, "      %s\n", line)
			}
		}
	}
	if doc.Error != "" {
		fmt.Fprintf(w, "  error: %s\n", doc.Error)
	}
	fmt.Fprintf(w, "  %d passed, %d failed\n", doc.Passed, doc.Failed)
}
