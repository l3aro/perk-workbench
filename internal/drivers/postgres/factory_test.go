package postgres

import (
	"context"
	"encoding/json"
	"io"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/l3aro/perk-workbench-plugin-sdk-go/driver"
	"github.com/l3aro/perk-workbench-plugin-sdk-go/server"
)

func TestFactoryCapabilities_preservePostgreSQLContract(t *testing.T) {
	capabilities := (Factory{}).Capabilities()
	if err := driver.ValidateCapabilities(capabilities); err != nil {
		t.Fatalf("ValidateCapabilities() = %v", err)
	}
	wantTargets := []driver.TargetPattern{
		{Prefix: "postgres://", KeepTarget: true},
		{Prefix: "postgresql://", KeepTarget: true},
		{Prefix: "postgres:"},
	}
	if !reflect.DeepEqual(capabilities.Targets, wantTargets) {
		t.Fatalf("targets = %#v, want %#v", capabilities.Targets, wantTargets)
	}
	if capabilities.Form == nil {
		t.Fatal("form is nil")
	}
	if got := capabilities.Form.Prefix; got != "postgres:" {
		t.Fatalf("form prefix = %q, want postgres:", got)
	}
	keys := make([]string, 0, len(capabilities.Form.Fields))
	for _, field := range capabilities.Form.Fields {
		keys = append(keys, field.Key)
	}
	if want := []string{"host", "port", "username", "password", "database", "tls"}; !reflect.DeepEqual(keys, want) {
		t.Fatalf("form keys = %#v, want %#v", keys, want)
	}
	if got := capabilities.Form.Fields[5].Options; !reflect.DeepEqual(got, []driver.FormOption{
		{Label: "Verify certificate", Value: "verify-full"},
		{Label: "Encrypt, don't verify certificate", Value: "require"},
		{Label: "Don't encrypt", Value: "disable"},
	}) {
		t.Fatalf("TLS options = %#v", got)
	}
	if !capabilities.WriteCapabilities.RowWriter {
		t.Fatal("row writer capability is false")
	}
}

func TestFactoryBuildTarget_serializesFormValues(t *testing.T) {
	result, err := (Factory{}).BuildTarget(context.Background(), driver.FormValues{
		Host: "db.example", Port: "5432", User: "alice", Pass: "secret", Database: "app", TLS: "require",
	})
	if err != nil {
		t.Fatalf("BuildTarget() = %v", err)
	}
	if !result.OK {
		t.Fatal("BuildTarget() returned !OK")
	}
	parsed, err := url.Parse(result.Target)
	if err != nil {
		t.Fatalf("url.Parse() = %v", err)
	}
	if parsed.Scheme != "postgres" || parsed.Host != "db.example:5432" || parsed.Path != "/app" {
		t.Fatalf("target = %q", result.Target)
	}
	if parsed.Query().Get("sslmode") != "require" {
		t.Fatalf("sslmode = %q, want require", parsed.Query().Get("sslmode"))
	}
}

type factoryOpenFrameWriter struct {
	frames chan []byte
}

func (w *factoryOpenFrameWriter) Write(data []byte) (int, error) {
	w.frames <- append([]byte(nil), data...)
	return len(data), nil
}

func TestFactoryOpenReturnsSafeConnectionError(t *testing.T) {
	const target = "postgres://alice:supersecret@%41:5432/app"

	inputReader, inputWriter := io.Pipe()
	output := &factoryOpenFrameWriter{frames: make(chan []byte, 2)}
	done := make(chan error, 1)
	go func() { done <- server.Run(inputReader, output, Factory{}) }()

	writeFrame := func(frame string) {
		t.Helper()
		if _, err := io.WriteString(inputWriter, frame+"\n"); err != nil {
			t.Fatal(err)
		}
	}
	readFrame := func() []byte {
		t.Helper()
		select {
		case frame := <-output.frames:
			return frame
		case <-time.After(time.Second):
			t.Fatal("timed out waiting for response")
			return nil
		}
	}

	writeFrame(`{"jsonrpc":"2.0","id":1,"method":"perk/v1/initialize","params":{"protocol_version":1,"workbench_version":"test"}}`)
	_ = readFrame()
	writeFrame(`{"jsonrpc":"2.0","id":2,"method":"perk/v1/open","params":{"target":"postgres://alice:supersecret@%41:5432/app"}}`)
	frame := readFrame()

	var response struct {
		Error struct {
			Message string            `json:"message"`
			Data    map[string]string `json:"data"`
		} `json:"error"`
	}
	if err := json.Unmarshal(frame, &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if got, want := response.Error.Data["kind"], string(driver.KindConnection); got != want {
		t.Fatalf("error.data.kind = %q, want %q", got, want)
	}
	if got, want := response.Error.Message, "opening postgres database failed"; got != want {
		t.Fatalf("error.message = %q, want %q", got, want)
	}
	wire := string(frame)
	if strings.Contains(wire, target) || strings.Contains(wire, "supersecret") {
		t.Fatalf("error frame leaks target or credentials: %s", wire)
	}

	if err := inputWriter.Close(); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatalf("Run() = %v, want nil", err)
	}
}
