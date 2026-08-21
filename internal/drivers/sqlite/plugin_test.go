package sqlite_test

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"testing"
	"time"

	"github.com/l3aro/perk-workbench-plugin-sdk-go/driver"
	"github.com/l3aro/perk-workbench-plugin-sdk-go/server"
	"github.com/l3aro/perk-workbench/internal/drivers/sqlite"
)

func TestTransportLifecycleFixture(t *testing.T) {
	fixture, err := os.ReadFile("testdata/conformance/lifecycle.ndjson")
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := server.Run(&delayedEOFReader{Reader: bytes.NewReader(fixture)}, &output, sqlite.Factory{}); err != nil {
		t.Fatalf("server.Run() error = %v", err)
	}
	var responses []map[string]any
	for _, line := range bytes.Split(bytes.TrimSpace(output.Bytes()), []byte{'\n'}) {
		if len(line) == 0 {
			continue
		}
		var response map[string]any
		if err := json.Unmarshal(line, &response); err != nil {
			t.Fatalf("response %q is invalid JSON: %v", line, err)
		}
		responses = append(responses, response)
	}
	if len(responses) != 2 {
		t.Fatalf("response count = %d, want 2: %s", len(responses), output.Bytes())
	}
	var initialized bool
	var builtTarget bool
	for _, response := range responses {
		switch response["id"] {
		case float64(1):
			result, ok := response["result"].(map[string]any)
			if !ok {
				t.Fatalf("initialize response = %#v", response)
			}
			caps, ok := result["capabilities"].(map[string]any)
			if !ok || caps["name"] != "sqlite" || caps["display"] != "SQLite" {
				t.Fatalf("initialize capabilities = %#v", result["capabilities"])
			}
			initialized = true
		case float64(2):
			result, ok := response["result"].(map[string]any)
			if !ok || result["target"] != ":memory:" || result["ok"] != true {
				t.Fatalf("build_target response = %#v", response)
			}
			builtTarget = true
		}
	}
	if !initialized || !builtTarget {
		t.Fatalf("lifecycle responses missing initialize/build_target: %#v", responses)
	}
}

func TestFactoryCapabilities_advertiseSQLiteLabelTarget(t *testing.T) {
	capabilities := (sqlite.Factory{}).Capabilities()
	if err := driver.ValidateCapabilities(capabilities); err != nil {
		t.Fatalf("ValidateCapabilities() = %v", err)
	}
	if len(capabilities.Targets) != 1 || capabilities.Targets[0] != (driver.TargetPattern{Prefix: "sqlite:"}) {
		t.Fatalf("targets = %#v, want sqlite: label target", capabilities.Targets)
	}
}

type delayedEOFReader struct {
	*bytes.Reader
	delayed bool
}

func (r *delayedEOFReader) Read(data []byte) (int, error) {
	n, err := r.Reader.Read(data)
	if err == io.EOF && !r.delayed {
		r.delayed = true
		time.Sleep(50 * time.Millisecond)
	}
	return n, err
}
