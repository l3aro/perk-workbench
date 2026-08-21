package mysql

import (
	"encoding/json"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/l3aro/perk-workbench-plugin-sdk-go/driver"
	"github.com/l3aro/perk-workbench-plugin-sdk-go/server"
)

type factoryOpenFrameWriter struct {
	frames chan []byte
}

func (w *factoryOpenFrameWriter) Write(data []byte) (int, error) {
	w.frames <- append([]byte(nil), data...)
	return len(data), nil
}

func TestFactoryOpenReturnsSafeConnectionError(t *testing.T) {
	const target = "alice:supersecret@tcp([::1)/app"

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
	writeFrame(`{"jsonrpc":"2.0","id":2,"method":"perk/v1/open","params":{"target":"alice:supersecret@tcp([::1)/app"}}`)
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
	if got, want := response.Error.Message, "opening mysql database failed"; got != want {
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
