package postgres

import (
	"bufio"
	"encoding/json"
	"io"
	"strings"
	"testing"

	"github.com/l3aro/perk-workbench-plugin-sdk-go/server"
)

func TestFactoryBuildTargetTransportUsesWireCredentials(t *testing.T) {
	inputReader, inputWriter := io.Pipe()
	outputReader, outputWriter := io.Pipe()
	done := make(chan error, 1)
	go func() {
		done <- server.Run(inputReader, outputWriter, Factory{})
	}()
	output := bufio.NewReader(outputReader)
	writeFrame := func(value string) {
		t.Helper()
		if _, err := io.WriteString(inputWriter, value+"\n"); err != nil {
			t.Fatal(err)
		}
	}
	readFrame := func() []byte {
		t.Helper()
		frame, err := output.ReadBytes('\n')
		if err != nil {
			t.Fatal(err)
		}
		return frame[:len(frame)-1]
	}

	writeFrame(`{"jsonrpc":"2.0","id":1,"method":"perk/v1/initialize","params":{"protocol_version":1,"workbench_version":"test"}}`)
	_ = readFrame()
	writeFrame(`{"jsonrpc":"2.0","id":2,"method":"perk/v1/build_target","params":{"host":"db.example","port":"5432","user":"alice","pass":"secret","database":"app","tls":"require"}}`)
	var response struct {
		Result struct {
			Target string `json:"target"`
			OK     bool   `json:"ok"`
		} `json:"result"`
	}
	if err := json.Unmarshal(readFrame(), &response); err != nil {
		t.Fatal(err)
	}
	if !response.Result.OK {
		t.Fatal("build_target returned ok=false")
	}
	for _, want := range []string{"alice", "secret", "db.example:5432", "/app", "sslmode=require"} {
		if !strings.Contains(response.Result.Target, want) {
			t.Fatalf("target %q does not contain %q", response.Result.Target, want)
		}
	}

	if err := inputWriter.Close(); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}
