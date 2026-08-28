package site

import (
	_ "embed"

	"bufio"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"os/exec"
	"sync"
	"unicode/utf8"

	"github.com/coder/websocket"
	"github.com/creack/pty"
)

const (
	terminalDefaultCols = 120
	terminalDefaultRows = 36
)

// demoDatabase is the Chinook SQLite demo, embedded into the site binary and
// extracted to a temp file on first use so the real TUI can open it read-only
// as a plain file path.
//
//go:embed demo/chinook-sqlite.db
var demoDatabase []byte

var (
	extractOnce sync.Once
	extractPath string
	extractErr  error
)

// extractDemoDB materializes the embedded demo database once and returns its
// path. The file is created 0600 and is never opened for writing by the TUI.
func extractDemoDB() (string, error) {
	extractOnce.Do(func() {
		file, err := os.CreateTemp("", "perk-chinook-*.db")
		if err != nil {
			extractErr = err
			return
		}
		path := file.Name()
		defer file.Close()
		if err := file.Chmod(0o600); err != nil {
			extractErr = err
			return
		}
		if _, err := file.Write(demoDatabase); err != nil {
			extractErr = err
			return
		}
		extractPath = path
	})
	return extractPath, extractErr
}

// terminalServer bridges a browser xterm.js session to the real Perk Workbench
// Bubble Tea application running in a read-only, pinned session against the
// demo database. Each WebSocket connection gets its own isolated session.
type terminalServer struct {
	// bin is the perk-workbench binary; empty resolves "perk-workbench" on PATH.
	bin string
	// db is the demo database path; empty extracts the embedded copy.
	db string
}

// newTerminalServer reads configuration from the environment. PERK_WORKBENCH_BIN
// points at the TUI binary when it is not on PATH.
func newTerminalServer() *terminalServer {
	return &terminalServer{bin: os.Getenv("PERK_WORKBENCH_BIN")}
}

func demoCommandArgs(db string) []string {
	return []string{"--read-only", "--pin", db}
}

type terminalMessage struct {
	Type string `json:"type"`
	Data string `json:"data,omitempty"`
	Cols int    `json:"cols,omitempty"`
	Rows int    `json:"rows,omitempty"`
}

func (s *terminalServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	bin := s.bin
	if bin == "" {
		bin = "perk-workbench"
	}
	if _, err := exec.LookPath(bin); err != nil {
		http.Error(w, "live demo unavailable: perk-workbench binary not found", http.StatusServiceUnavailable)
		return
	}
	db := s.db
	if db == "" {
		var err error
		db, err = extractDemoDB()
		if err != nil {
			http.Error(w, "live demo unavailable: "+err.Error(), http.StatusServiceUnavailable)
			return
		}
	}

	conn, err := websocket.Accept(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close(websocket.StatusNormalClosure, "")

	// Isolate config, query history, and notifications per session so visitors
	// never read or write each other's state.
	home, err := os.MkdirTemp("", "perk-tui-")
	if err != nil {
		return
	}
	defer os.RemoveAll(home)

	cmd := exec.Command(bin, demoCommandArgs(db)...)
	cmd.Env = append(os.Environ(),
		"HOME="+home,
		"XDG_CONFIG_HOME="+home,
		"TERM=xterm-256color",
	)
	ptmx, err := pty.StartWithSize(cmd, &pty.Winsize{Cols: terminalDefaultCols, Rows: terminalDefaultRows})
	if err != nil {
		_ = conn.Write(r.Context(), websocket.MessageText, []byte("live demo unavailable: "+err.Error()))
		return
	}
	defer func() {
		_ = cmd.Process.Kill()
		_ = ptmx.Close()
		_ = cmd.Wait()
	}()

	ctx := r.Context()
	done := make(chan struct{})
	go func() {
		defer close(done)
		writePTYOutput(ctx, conn, ptmx)
	}()

	for {
		_, data, err := conn.Read(ctx)
		if err != nil {
			break
		}
		var msg terminalMessage
		if json.Unmarshal(data, &msg) != nil {
			continue
		}
		switch msg.Type {
		case "input":
			_, _ = io.WriteString(ptmx, msg.Data)
		case "resize":
			if msg.Cols > 0 && msg.Rows > 0 {
				_ = pty.Setsize(ptmx, &pty.Winsize{Cols: uint16(msg.Cols), Rows: uint16(msg.Rows)})
			}
		}
	}

	// The TUI keeps running after the client leaves; stop it and close the PTY
	// before waiting, so the writer goroutine can exit and nothing leaks.
	_ = cmd.Process.Kill()
	_ = ptmx.Close()
	<-done
}

// writePTYOutput forwards PTY bytes to the WebSocket as text frames, keeping
// multi-byte UTF-8 sequences intact across reads so xterm.js never receives a
// truncated rune.
func writePTYOutput(ctx context.Context, conn *websocket.Conn, ptmx *os.File) {
	reader := bufio.NewReader(ptmx)
	buf := make([]byte, 4096)
	var pending []byte
	for {
		n, err := reader.Read(buf)
		if n > 0 {
			data := append(pending, buf[:n]...)
			valid, rest := splitUTF8(data)
			pending = rest
			if len(valid) > 0 {
				if werr := conn.Write(ctx, websocket.MessageText, valid); werr != nil {
					return
				}
			}
		}
		if err != nil {
			if len(pending) > 0 {
				_ = conn.Write(ctx, websocket.MessageText, pending)
			}
			return
		}
	}
}

// splitUTF8 splits data at a rune boundary: the valid prefix is safe to send,
// the trailing remainder is held for the next read.
func splitUTF8(data []byte) (valid, rest []byte) {
	if utf8.Valid(data) {
		return data, nil
	}
	start := len(data)
	for i := len(data) - 1; i >= 0 && i >= len(data)-utf8.UTFMax; i-- {
		if utf8.RuneStart(data[i]) {
			start = i
			break
		}
	}
	if utf8.Valid(data[:start]) {
		return data[:start], data[start:]
	}
	return nil, data
}
