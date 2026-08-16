// Package conformance runs the perk/v1 protocol conformance suite
// against one external plugin executable, outside Go's unit-test
// harness: fixture-driven protocol cases and generated transport cases,
// each in a fresh child spoken to as raw NDJSON-RPC on stdio. The
// runner never routes frames through the production Client — it must be
// able to send deliberately invalid frames — and never invokes
// build_target, open, or any session RPC, so a transport-only plugin
// (perk-redis without a Redis server) can be tested. Every child is
// terminated and reaped before the next case starts, and independent
// failures never stop later cases.
package conformance

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/l3aro/perk-workbench/internal/database"
	"github.com/l3aro/perk-workbench/internal/database/plugin"
	"github.com/l3aro/perk-workbench/protocol/perk-v1"
)

// Stable failure categories for one conformance case.
const (
	CategorySpawn    = "spawn"
	CategoryProtocol = "protocol"
	CategoryBehavior = "behavior"
	CategoryTimeout  = "timeout"
	CategoryShutdown = "shutdown"
)

// EvidenceSchema is the canonical identity of the evidence document
// schema (protocol/perk-v1/plugin-test-evidence.schema.json). The
// emitted evidence document names it so any consumer can validate the
// document against the exact schema that produced it.
const EvidenceSchema = "perk/v1/plugin-test-evidence.schema.json"

// EvidenceVersion is the version of the evidence document shape this
// binary emits. A future change to the document shape bumps this
// version together with the schema asset.
const EvidenceVersion = 1

// CapabilitiesIdentity is the declarative identity of a plugin's
// capability advertisement, captured from the initialize handshake —
// no backend required. The v1 protocol advertises no implementation
// version, so none is reported.
type CapabilitiesIdentity struct {
	Name    string `json:"name"`
	Display string `json:"display,omitempty"`
}

// CaseError is one case failure with its stable category and message.
// Messages are structural — raw protocol frames and request data are
// never reported.
type CaseError struct {
	Category string
	Message  string
}

func (e *CaseError) Error() string { return e.Message }

// Result is the outcome of one conformance case: a stable name, pass or
// fail, duration, the failure category and message, and the bounded
// sanitized stderr tail. It is also the JSON document shape of one case.
type Result struct {
	Name     string        `json:"name"`
	OK       bool          `json:"ok"`
	Duration time.Duration `json:"duration"`
	Category string        `json:"category,omitempty"`
	Error    string        `json:"error,omitempty"`
	Stderr   []string      `json:"stderr,omitempty"`
}

// Document is the complete machine-readable outcome of one run: a
// self-contained release evidence document. It carries the stable
// evidence fields (schema identity and version, perk protocol version,
// host build version, canonical contract digest, executable digest and
// resolved path, capabilities identity) plus the per-case results and
// final counts. It never carries connection targets, form values,
// credentials, statements, or raw protocol frames, and it is
// deterministic except per-case durations and the path.
type Document struct {
	EvidenceSchema   string                `json:"evidence_schema"`
	EvidenceVersion  int                   `json:"evidence_version"`
	ProtocolVersion  int                   `json:"protocol_version"`
	HostVersion      string                `json:"host_version,omitempty"`
	ContractSHA256   string                `json:"contract_sha256,omitempty"`
	Entry            string                `json:"entry"`
	Path             string                `json:"path,omitempty"`
	ExecutableSHA256 string                `json:"executable_sha256,omitempty"`
	Capabilities     *CapabilitiesIdentity `json:"capabilities,omitempty"`
	Error            string                `json:"error,omitempty"` // suite-level failure (interrupt, setup, resolve, hash)
	Cases            []Result              `json:"cases,omitempty"`
	Passed           int                   `json:"passed"`
	Failed           int                   `json:"failed"`
	OK               bool                  `json:"ok"`
}

// NewDocument returns the base evidence document for one entry with
// every stable field the engine can fill: the evidence schema identity
// and version, the perk protocol version the host speaks, and the
// canonical contract digest. HostVersion is filled by the CLI from the
// host build version; Test fills the resolved path, executable digest,
// capabilities identity, and the run results. It fails only when the
// embedded contract assets cannot be read — no evidence document can
// be produced then.
func NewDocument(entry string) (Document, error) {
	digest, err := ContractDigest(perkv1.Source{})
	if err != nil {
		return Document{}, err
	}
	return Document{
		EvidenceSchema:  EvidenceSchema,
		EvidenceVersion: EvidenceVersion,
		ProtocolVersion: plugin.ProtocolVersion,
		ContractSHA256:  digest,
		Entry:           entry,
		Cases:           []Result{},
	}, nil
}

// Case is one named conformance case. Run receives the fresh child and
// the case deadline; a nil error is a pass. The child is always
// terminated and reaped by the engine afterwards.
type Case struct {
	Name string
	Run  func(child *Child, until time.Time) error
}

// Engine runs the conformance suite against one executable. New builds
// it from the canonical embedded protocol assets; tests inject a fake
// source through NewFrom to exercise manifest drift.
type Engine struct {
	src          source
	manifest     manifest
	fixtures     map[string][]byte
	codes        map[string]int
	methods      map[string]string
	capabilities *CapabilitiesIdentity // captured from the first validated initialize reply

	// Timeout bounds one case end to end — its exchanges and its
	// shutdown; a case that exceeds it fails. Default 30 seconds.
	Timeout time.Duration
	// Quiet is the silence window required after a case's expected
	// exchanges (catching duplicate or fabricated responses and
	// premature exit) and the window a deliberately invalid input frame
	// must stay quiet for. Default 200ms.
	Quiet time.Duration
}

// source is the canonical protocol-asset provider; perkv1.Source is the
// production implementation, tests inject fakes to exercise drift.
type source interface {
	Schema() []byte
	Manifest() ([]byte, error)
	Fixture(name string) ([]byte, error)
	FixtureNames() ([]string, error)
}

// manifest is the fixture manifest's document shape.
type manifest struct {
	Fixtures []manifestEntry `json:"fixtures"`
}

// manifestEntry is one fixture description: the file name, whether the
// frame is protocol-valid, the schema $def it targets, and — where
// applicable — the expected method, JSON-RPC error code, normalized
// error kind, and rejection.
type manifestEntry struct {
	File   string `json:"file"`
	Valid  bool   `json:"valid"`
	Ref    string `json:"ref"`
	Method string `json:"method,omitempty"`
	Code   *int   `json:"code,omitempty"`
	Kind   string `json:"kind,omitempty"`
	Reject string `json:"reject,omitempty"`
}

// requiredFixtures are the manifest entries the cases send, with the
// metadata each case relies on: a frame with an unsigned numeric id, a
// manifest-declared expected error code, and a manifest-declared
// method. Drift in any of these fails the whole run coherently instead
// of misbehaving per case.
var requiredFixtures = []struct {
	file   string
	id     bool // frame must carry an unsigned numeric request id
	code   bool // manifest must declare the expected JSON-RPC error code
	method bool // manifest must declare the expected method
}{
	{file: "request-initialize.json", id: true, method: true},
	{file: "invalid-request-jsonrpc.json", id: true, code: true, method: true},
	{file: "invalid-request-string-id.json", code: true, method: true},
	{file: "invalid-request-float-id.json", code: true, method: true},
	{file: "invalid-request-unknown-method.json", id: true, code: true, method: true},
	{file: "notification-cancel.json", method: true},
}

// New builds the engine from the canonical embedded protocol assets.
func New() (*Engine, error) {
	return NewFrom(perkv1.Source{})
}

// NewFrom builds the engine from an injected fixture source. The
// manifest and every named fixture are loaded up front, and the
// metadata the cases rely on is required: missing files, missing
// entries, missing codes or methods, or a fixture whose method drifts
// from its manifest entry fail the run coherently here.
func NewFrom(src source) (*Engine, error) {
	engine := &Engine{
		src:      src,
		Timeout:  30 * time.Second,
		Quiet:    200 * time.Millisecond,
		fixtures: map[string][]byte{},
		codes:    map[string]int{},
		methods:  map[string]string{},
	}
	raw, err := src.Manifest()
	if err != nil {
		return nil, fmt.Errorf("conformance: loading the fixture manifest: %w", err)
	}
	if err := json.Unmarshal(raw, &engine.manifest); err != nil {
		return nil, fmt.Errorf("conformance: fixture manifest does not parse: %w", err)
	}
	if len(engine.manifest.Fixtures) == 0 {
		return nil, errors.New("conformance: fixture manifest lists no fixtures")
	}
	// The schema is the manifest's ref target; drift between the two
	// (a renamed $def, a ref outside $defs) fails the run coherently.
	schema := src.Schema()
	if len(schema) == 0 {
		return nil, errors.New("conformance: embedded schema is empty")
	}
	var schemaDoc struct {
		Defs map[string]json.RawMessage `json:"$defs"`
	}
	if err := json.Unmarshal(schema, &schemaDoc); err != nil {
		return nil, fmt.Errorf("conformance: schema does not parse: %w", err)
	}
	for _, entry := range engine.manifest.Fixtures {
		if entry.Ref == "" || !strings.HasPrefix(entry.Ref, "#/$defs/") {
			return nil, fmt.Errorf("conformance: fixture manifest entry %q ref %q is not an envelope $def", entry.File, entry.Ref)
		}
		if _, ok := schemaDoc.Defs[strings.TrimPrefix(entry.Ref, "#/$defs/")]; !ok {
			return nil, fmt.Errorf("conformance: fixture manifest entry %q ref %q does not resolve in the schema", entry.File, entry.Ref)
		}
	}
	for _, entry := range engine.manifest.Fixtures {
		if entry.File == "" {
			return nil, errors.New("conformance: fixture manifest entry with a blank file name")
		}
		if _, dup := engine.fixtures[entry.File]; dup {
			return nil, fmt.Errorf("conformance: fixture manifest lists %q twice", entry.File)
		}
		frame, err := src.Fixture(entry.File)
		if err != nil {
			return nil, fmt.Errorf("conformance: fixture manifest drift: %w", err)
		}
		if !json.Valid(frame) {
			return nil, fmt.Errorf("conformance: fixture %q is not parseable JSON", entry.File)
		}
		frame, err = compactFrame(frame)
		if err != nil {
			return nil, fmt.Errorf("conformance: fixture %q does not re-marshal as a wire frame: %w", entry.File, err)
		}
		engine.fixtures[entry.File] = frame
	}
	for _, need := range requiredFixtures {
		entry, ok := engine.entry(need.file)
		if !ok {
			return nil, fmt.Errorf("conformance: fixture manifest does not list required fixture %q", need.file)
		}
		if need.code && entry.Code == nil {
			return nil, fmt.Errorf("conformance: fixture manifest entry %q is missing the expected error code", need.file)
		}
		if need.method && entry.Method == "" {
			return nil, fmt.Errorf("conformance: fixture manifest entry %q is missing the expected method", need.file)
		}
		if need.id {
			if _, ok := frameID(engine.fixtures[need.file]); !ok {
				return nil, fmt.Errorf("conformance: fixture %q carries no unsigned numeric request id", need.file)
			}
		}
		if entry.Code != nil {
			engine.codes[need.file] = *entry.Code
		}
		engine.methods[need.file] = entry.Method
	}
	// A request fixture whose method drifted from its manifest entry is
	// the classic silent-breakage mode; catch it before any case runs.
	for _, entry := range engine.manifest.Fixtures {
		var obj struct {
			Method string `json:"method"`
		}
		if err := json.Unmarshal(engine.fixtures[entry.File], &obj); err != nil {
			return nil, fmt.Errorf("conformance: fixture %q does not parse as an object: %w", entry.File, err)
		}
		if obj.Method != "" && entry.Method != "" && obj.Method != entry.Method {
			return nil, fmt.Errorf("conformance: fixture %q method %q, manifest expects %q", entry.File, obj.Method, entry.Method)
		}
	}
	return engine, nil
}

// entry returns the manifest entry for one fixture file.
func (e *Engine) entry(file string) (manifestEntry, bool) {
	for _, entry := range e.manifest.Fixtures {
		if entry.File == file {
			return entry, true
		}
	}
	return manifestEntry{}, false
}

// contractAssetNames enumerates the canonical contract asset set:
// schema.json, fixtures/manifest.json, and every fixture frame, sorted
// by name. Sorted enumeration keeps the digest independent of map or
// directory iteration order.
func contractAssetNames(src source) ([]string, error) {
	names := []string{"schema.json", "fixtures/manifest.json"}
	fixtures, err := src.FixtureNames()
	if err != nil {
		return nil, err
	}
	for _, name := range fixtures {
		names = append(names, "fixtures/"+name)
	}
	sort.Strings(names)
	return names, nil
}

// ContractDigest returns the canonical SHA-256 of the whole perk/v1
// contract asset set: schema.json, fixtures/manifest.json, and every
// fixture frame, hashed in sorted name order with explicit length
// framing — each asset contributes "<name-length>:<name>" followed by
// "<content-length>:<content>". Any drift in any asset, including a
// rename, reorder, or content edit, changes the digest; the framing
// makes concatenation boundaries unambiguous.
func ContractDigest(src source) (string, error) {
	names, err := contractAssetNames(src)
	if err != nil {
		return "", err
	}
	hash := sha256.New()
	for _, name := range names {
		var content []byte
		switch {
		case name == "schema.json":
			content = src.Schema()
		case name == "fixtures/manifest.json":
			content, err = src.Manifest()
		default:
			content, err = src.Fixture(strings.TrimPrefix(name, "fixtures/"))
		}
		if err != nil {
			return "", fmt.Errorf("contract digest: %s: %w", name, err)
		}
		io.WriteString(hash, strconv.Itoa(len(name)))
		hash.Write([]byte{':'})
		io.WriteString(hash, name)
		io.WriteString(hash, strconv.Itoa(len(content)))
		hash.Write([]byte{':'})
		hash.Write(content)
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

// Test runs every case against one resolved executable path. Each case
// spawns a fresh child and terminates and reaps it before the next
// starts; independent failures never stop later cases. A canceled
// context stops the run between cases and fails it overall. The
// returned document carries the stable evidence fields: the evidence
// schema identity and version, the perk protocol version, the
// canonical contract digest (from the engine's asset source), the
// executable digest of the resolved path (when the file exists and is
// readable), and the capabilities identity captured from the first
// validated initialize reply. HostVersion is filled by the CLI.
func (e *Engine) Test(ctx context.Context, entry, path string) Document {
	e.capabilities = nil
	doc := Document{
		EvidenceSchema:  EvidenceSchema,
		EvidenceVersion: EvidenceVersion,
		ProtocolVersion: plugin.ProtocolVersion,
		Entry:           entry,
		Path:            path,
		Cases:           []Result{},
	}
	digest, err := ContractDigest(e.src)
	if err != nil {
		doc.Error = "contract digest: " + err.Error()
		return doc
	}
	doc.ContractSHA256 = digest
	if path != "" {
		digest, err := plugin.SHA256File(path)
		if err != nil {
			if !errors.Is(err, os.ErrNotExist) {
				doc.Error = "executable sha256: " + err.Error()
				return doc
			}
			// A path that does not exist is left unhashed: the spawn
			// failures of the cases below report it far more usefully.
		} else {
			doc.ExecutableSHA256 = digest
		}
	}
	for _, c := range e.cases() {
		if err := ctx.Err(); err != nil {
			doc.Error = "interrupted"
			break
		}
		result := e.runCase(c, path)
		doc.Cases = append(doc.Cases, result)
		if result.OK {
			doc.Passed++
		} else {
			doc.Failed++
		}
	}
	doc.OK = doc.Error == "" && doc.Failed == 0
	doc.Capabilities = e.capabilities
	return doc
}

// runCase runs one case in a fresh child and always terminates the
// child afterwards, so a stuck or misbehaving child can never leak.
func (e *Engine) runCase(c Case, path string) Result {
	start := time.Now()
	child, err := spawnChild(path)
	if err != nil {
		return Result{Name: c.Name, Duration: time.Since(start), Category: CategorySpawn, Error: err.Error()}
	}
	runErr := c.Run(child, start.Add(e.Timeout))
	closeErr := child.Close()
	result := Result{Name: c.Name, Duration: time.Since(start), Stderr: child.stderrTail()}
	switch {
	case runErr == nil && closeErr == nil:
		result.OK = true
	case runErr != nil:
		var caseErr *CaseError
		if errors.As(runErr, &caseErr) {
			result.Category = caseErr.Category
			result.Error = caseErr.Message
		} else {
			result.Category = CategoryProtocol
			result.Error = runErr.Error()
		}
	default:
		result.Category = CategoryShutdown
		result.Error = closeErr.Error()
	}
	return result
}

// compactFrame re-marshals one fixture as a single-line wire frame: the
// canonical fixture files are pretty-printed documents for readability,
// while a protocol frame is exactly one JSON object followed by a single
// LF. Content is preserved verbatim — numbers stay exact via UseNumber,
// and deliberately invalid fields (wrong jsonrpc, string or float ids)
// survive untouched.
func compactFrame(frame []byte) ([]byte, error) {
	decoder := json.NewDecoder(bytes.NewReader(frame))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}
	out, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	return append(out, '\n'), nil
}

// frameID extracts the unsigned numeric request id of one frame, when
// it carries one. Quoted numbers, floats, negatives, and null are never
// request ids.
func frameID(frame []byte) (uint64, bool) {
	var obj struct {
		ID json.RawMessage `json:"id"`
	}
	if err := json.Unmarshal(frame, &obj); err != nil || len(obj.ID) == 0 {
		return 0, false
	}
	trimmed := bytes.TrimSpace(obj.ID)
	if len(trimmed) == 0 || trimmed[0] < '0' || trimmed[0] > '9' {
		return 0, false
	}
	var id uint64
	if err := json.Unmarshal(trimmed, &id); err != nil {
		return 0, false
	}
	return id, true
}

// initializeResult is the wire shape of a successful initialize reply.
type initializeResult struct {
	ProtocolVersion int                   `json:"protocol_version"`
	Capabilities    database.Capabilities `json:"capabilities"`
}

// sendAndExpectInitialize sends the canonical initialize request and
// verifies the success reply.
func (e *Engine) sendAndExpectInitialize(child *Child, until time.Time, frame []byte) error {
	if err := child.SendFixture(frame); err != nil {
		return err
	}
	response, err := child.Expect(until)
	if err != nil {
		return err
	}
	return e.expectInitializeResult(response)
}

// expectInitializeResult verifies one initialize reply: a success
// carrying the exact protocol version and capabilities that pass the
// registration invariants (the side-effect-free ValidateShim checks,
// minus the registry-conflict half, which is global state a conformance
// run must not depend on). A validated reply also captures the
// plugin's capabilities identity for the evidence document.
func (e *Engine) expectInitializeResult(frame Frame) error {
	if frame.Error != nil {
		return &CaseError{CategoryBehavior, fmt.Sprintf(
			"initialize answered with error code %d: %s", frame.Error.Code, frame.Error.Message)}
	}
	var result initializeResult
	if err := json.Unmarshal(frame.Result, &result); err != nil {
		return &CaseError{CategoryBehavior, fmt.Sprintf("initialize result does not decode: %v", err)}
	}
	if result.ProtocolVersion != plugin.ProtocolVersion {
		return &CaseError{CategoryBehavior, fmt.Sprintf(
			"initialize protocol version %d, want %d", result.ProtocolVersion, plugin.ProtocolVersion)}
	}
	if err := validateCapabilities(result.Capabilities); err != nil {
		return &CaseError{CategoryBehavior, fmt.Sprintf("initialize capabilities: %v", err)}
	}
	if e.capabilities == nil {
		e.capabilities = &CapabilitiesIdentity{
			Name:    result.Capabilities.Name,
			Display: result.Capabilities.Display,
		}
	}
	return nil
}
