package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/l3aro/perk-workbench-plugin-sdk-go/driver"
	"github.com/l3aro/perk-workbench/internal/drivers/mongodb"
	"github.com/l3aro/perk-workbench/internal/drivers/mysql"
	"github.com/l3aro/perk-workbench/internal/drivers/postgres"
	"github.com/l3aro/perk-workbench/internal/drivers/sqlite"
)

func TestSelfPluginFactoriesAdvertiseFamily(t *testing.T) {
	tests := []struct {
		name    string
		factory driver.Factory
	}{
		{name: "sqlite", factory: sqlite.Factory{}},
		{name: "mysql", factory: mysql.Factory{}},
		{name: "postgres", factory: postgres.Factory{}},
		{name: "mongodb", factory: mongodb.Factory{}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			caps := test.factory.Capabilities()
			if caps.Name != test.name || caps.Driver != test.name {
				t.Fatalf("capabilities identity = %q/%q, want %q/%q", caps.Name, caps.Driver, test.name, test.name)
			}
		})
	}
}

func TestDispatchSelfPluginRejectsInvalidOperands(t *testing.T) {
	for _, args := range [][]string{{}, {"sqlite", "extra"}, {"redis"}} {
		var stdout, stderr bytes.Buffer
		if status := dispatchSelfPlugin(args, bytes.NewReader(nil), &stdout, &stderr); status != 2 {
			t.Fatalf("dispatchSelfPlugin(%v) status = %d, want 2", args, status)
		}
		if stdout.Len() != 0 || stderr.Len() == 0 {
			t.Fatalf("dispatchSelfPlugin(%v) stdout/stderr = %q/%q, want stderr-only diagnostic", args, stdout.String(), stderr.String())
		}
	}
}

func TestBuiltHostSelfPluginSQLiteLifecycleIsNDJSON(t *testing.T) {
	repoRoot := filepath.Join("..", "..")
	binary := filepath.Join(t.TempDir(), "perk-workbench")
	build := exec.Command("go", "build", "-o", binary, ".")
	build.Dir = filepath.Join(repoRoot, "cmd", "perk-workbench")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, output)
	}

	command := exec.Command(binary, "--plugin", "sqlite")
	var stderr bytes.Buffer
	command.Stderr = &stderr
	stdin, err := command.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := command.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = stdin.Close()
		_ = command.Process.Kill()
		_ = command.Wait()
	}()

	requests := []string{
		`{"jsonrpc":"2.0","id":1,"method":"perk/v1/initialize","params":{"protocol_version":1,"workbench_version":"test"}}` + "\n",
		`{"jsonrpc":"2.0","id":2,"method":"perk/v1/open","params":{"target":":memory:"}}` + "\n",
	}
	for _, request := range requests {
		if _, err := io.WriteString(stdin, request); err != nil {
			t.Fatal(err)
		}
	}

	scanner := bufio.NewScanner(stdout)
	responses := make([]map[string]any, 0, 3)
	for len(responses) < 2 && scanner.Scan() {
		var response map[string]any
		if err := json.Unmarshal(scanner.Bytes(), &response); err != nil {
			t.Fatalf("stdout frame %q is not JSON: %v", scanner.Bytes(), err)
		}
		responses = append(responses, response)
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	if len(responses) != 2 {
		t.Fatalf("received %d initialize/open responses, want 2", len(responses))
	}
	openResult, ok := responses[1]["result"].(map[string]any)
	if !ok {
		t.Fatalf("open response = %#v, want result", responses[1])
	}
	sessionID, ok := openResult["session_id"].(float64)
	if !ok || sessionID < 1 {
		t.Fatalf("open session_id = %#v, want positive number", openResult["session_id"])
	}

	closeRequest := fmt.Sprintf(`{"jsonrpc":"2.0","id":3,"method":"perk/v1/close","params":{"session_id":%d}}`+"\n", int(sessionID))
	if _, err := io.WriteString(stdin, closeRequest); err != nil {
		t.Fatal(err)
	}
	if scanner.Scan() {
		var response map[string]any
		if err := json.Unmarshal(scanner.Bytes(), &response); err != nil {
			t.Fatalf("close stdout frame %q is not JSON: %v", scanner.Bytes(), err)
		}
		responses = append(responses, response)
	}
	if len(responses) != 3 || responses[2]["id"] != float64(3) {
		t.Fatalf("responses = %#v, want initialize/open/close ids 1,2,3", responses)
	}
	if err := stdin.Close(); err != nil {
		t.Fatal(err)
	}
	if err := command.Wait(); err != nil {
		t.Fatalf("self-plugin process: %v; stderr=%q", err, stderr.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty on successful lifecycle", stderr.String())
	}
}
