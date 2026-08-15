package plugin

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"
)

// Client is a bounded concurrent JSON-RPC client for one plugin child.
// Requests and cancel notifications share one write mutex; responses are
// routed to pending calls by id; any protocol violation or child death
// terminates the client, failing every pending call.
type Client struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout io.Reader

	writeMu sync.Mutex // serializes all writes to stdin

	mu       sync.Mutex
	pending  map[uint64]*pendingCall
	nextID   atomic.Uint64
	unusable bool
	err      error // terminal error delivered to callers
	waitErr  error // cmd.Wait error, joined into err after the reap
	done     chan struct{}
	plugin   string // host-known identity, set after initialize

	closeOnce sync.Once
}

// pendingCall is one in-flight request.
type pendingCall struct {
	ch        chan callResult // buffered 1
	method    string
	stop      func() bool // context.AfterFunc cancel
	done      bool        // response received, result stashed
	cancelled bool        // caller canceled; the late response is discarded
	result    callResult  // stashed response, delivered at the read boundary
}

// callResult is the delivered outcome of one request.
type callResult struct {
	result json.RawMessage
	err    error
}

// spawn launches a plugin child. stderr is drained so the child can never
// block on a full stderr pipe; one reader goroutine serves responses.
func spawn(path string, args ...string) (*Client, error) {
	cmd := exec.Command(path, args...)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	client := &Client{
		cmd:     cmd,
		stdin:   stdin,
		stdout:  stdout,
		pending: map[uint64]*pendingCall{},
		done:    make(chan struct{}),
	}
	go func() { _, _ = io.Copy(io.Discard, stderr) }()
	go client.readLoop()
	return client, nil
}

// readFrame reads one newline-terminated frame, bounding the line at
// MaxFrameBytes. A line that does not fit is an oversized frame; io.EOF
// means the child closed stdout.
func readFrame(reader *bufio.Reader) ([]byte, error) {
	line, err := reader.ReadSlice('\n')
	if err == bufio.ErrBufferFull {
		return nil, fmt.Errorf("oversized frame: line exceeds %d bytes without a newline", MaxFrameBytes)
	}
	if err != nil {
		return nil, err
	}
	return line, nil
}

// readLoop owns the response stream until EOF or a protocol violation.
// Responses are stashed by dispatch and flushed only when no further
// frames are buffered, so a caller can never observe a result before the
// reader has processed every frame that arrived with it (a duplicate
// response in the same write batch is therefore always terminal before
// the first response is delivered). Pending calls are failed before any
// reap work, so a stuck child can never delay them; the reap's wait error
// is joined into the terminal error afterwards.
func (c *Client) readLoop() {
	defer close(c.done)
	reader := bufio.NewReaderSize(c.stdout, MaxFrameBytes)
	var readErr error
	for {
		frame, err := readFrame(reader)
		if err != nil {
			readErr = err
			break
		}
		if err := c.dispatch(frame); err != nil {
			readErr = err
			break
		}
		if reader.Buffered() == 0 {
			c.flushDeliveries()
		}
	}
	// Mark unusable and fail every pending call before stopping the
	// child: callers must never wait on a reap.
	c.terminal(readErr)
	if !errors.Is(readErr, io.EOF) {
		// Protocol violation: stop the child before waiting on it.
		_ = c.stdin.Close()
		_ = c.cmd.Process.Kill()
	}
	waitErr := c.cmd.Wait()
	c.mu.Lock()
	c.waitErr = waitErr
	if waitErr != nil || !errors.Is(readErr, io.EOF) {
		c.err = errors.Join(readErr, waitErr)
	}
	c.mu.Unlock()
}

// dispatch routes one response frame to its pending call, stashing the
// result until the read boundary. Any protocol violation is terminal and
// returned as an error; the terminal state is set atomically inside the
// critical section so no caller can observe a stale result.
func (c *Client) dispatch(frame []byte) error {
	// Frames are UTF-8 NDJSON; encoding/json would silently substitute
	// invalid bytes, so the check happens up front and is terminal.
	if !utf8.Valid(frame) {
		return fmt.Errorf("malformed response frame: invalid UTF-8")
	}
	var resp response
	if err := json.Unmarshal(frame, &resp); err != nil {
		return fmt.Errorf("malformed response frame: %w", err)
	}
	if resp.JSONRPC != "2.0" {
		return fmt.Errorf("response with jsonrpc %q, want \"2.0\"", resp.JSONRPC)
	}
	c.mu.Lock()
	entry, ok := c.pending[resp.ID]
	if !ok {
		// Unknown id: fabricated by the plugin, or a duplicate after the
		// entry completed — both terminal.
		c.terminalLocked(fmt.Errorf("response for unknown request id %d", resp.ID))
		c.mu.Unlock()
		return errors.New("terminal: unknown response id")
	}
	if entry.done {
		c.terminalLocked(fmt.Errorf("duplicate response for request id %d", resp.ID))
		c.mu.Unlock()
		return errors.New("terminal: duplicate response")
	}
	entry.done = true
	if entry.stop != nil {
		entry.stop()
	}
	if resp.Error != nil {
		entry.result = callResult{err: rpcErrorToGoError(entry.method, c.plugin, resp.Error)}
	} else {
		entry.result = callResult{result: resp.Result}
	}
	c.mu.Unlock()
	return nil
}

// flushDeliveries delivers every stashed response to its pending call.
func (c *Client) flushDeliveries() {
	c.mu.Lock()
	defer c.mu.Unlock()
	for id, entry := range c.pending {
		if entry.done {
			delete(c.pending, id)
			entry.ch <- entry.result // nonblocking: buffered 1
		}
	}
}

// SetPlugin records the plugin's self-claimed identity once the
// initialize handshake succeeds. The Loader calls it so operation errors
// carry host-known provenance; protocol behavior never depends on it.
// Safe to call any time; the last write wins.
func (c *Client) SetPlugin(name string) {
	c.mu.Lock()
	c.plugin = name
	c.mu.Unlock()
}

// Call performs one request and unmarshals the result into result. The
// caller's context cancels the operation: a perk/v1/cancel notification
// is sent to the plugin and a late response is discarded. Result-shape
// mismatches are operation errors, never terminal.
func (c *Client) Call(ctx context.Context, method string, params any, result any) error {
	c.mu.Lock()
	if c.unusable {
		err := c.err
		c.mu.Unlock()
		return err
	}
	id := c.nextID.Add(1)
	entry := &pendingCall{ch: make(chan callResult, 1), method: method}
	c.mu.Unlock()

	stop := context.AfterFunc(ctx, func() { c.cancel(id, entry) })
	entry.stop = stop

	c.mu.Lock()
	if c.unusable {
		// Terminated between the usability check and the insert: the
		// terminal path already failed or will fail the entry; just
		// report the terminal error.
		err := c.err
		c.mu.Unlock()
		stop()
		return err
	}
	c.pending[id] = entry
	c.mu.Unlock()

	frame, err := c.marshalRequest(id, method, params)
	if err != nil {
		c.dropPending(id, stop)
		return fmt.Errorf("%s: %w", method, err)
	}
	c.writeMu.Lock()
	_, err = c.stdin.Write(frame)
	c.writeMu.Unlock()
	if err != nil {
		c.dropPending(id, stop)
		return fmt.Errorf("%s: writing request: %w", method, err)
	}

	select {
	case delivered := <-entry.ch:
		if delivered.err != nil {
			return delivered.err
		}
		if err := json.Unmarshal(delivered.result, result); err != nil {
			return fmt.Errorf("%s: decoding result: %w", method, err)
		}
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// marshalRequest builds one complete request frame.
func (c *Client) marshalRequest(id uint64, method string, params any) ([]byte, error) {
	raw, err := json.Marshal(params)
	if err != nil {
		return nil, fmt.Errorf("marshaling params: %w", err)
	}
	frame, err := json.Marshal(request{JSONRPC: "2.0", ID: id, Method: method, Params: raw})
	if err != nil {
		return nil, fmt.Errorf("marshaling request: %w", err)
	}
	return append(frame, '\n'), nil
}

// dropPending removes a request whose frame never reached the plugin.
func (c *Client) dropPending(id uint64, stop func() bool) {
	c.mu.Lock()
	delete(c.pending, id)
	c.mu.Unlock()
	stop()
}

// cancel marks the entry canceled and notifies the plugin. Called from
// the request's context.AfterFunc; never blocks.
func (c *Client) cancel(id uint64, entry *pendingCall) {
	c.mu.Lock()
	if entry.done || entry.cancelled {
		c.mu.Unlock()
		return // completed, or already canceled
	}
	entry.cancelled = true
	_, inFlight := c.pending[id]
	c.mu.Unlock()
	if !inFlight {
		// The request never reached the wire; there is nothing to cancel
		// (and a stray cancel would confuse the plugin).
		return
	}
	c.writeCancel(id)
}

// writeCancel sends a perk/v1/cancel notification under the write mutex.
// Write errors are ignored — the terminal path handles them.
func (c *Client) writeCancel(id uint64) {
	raw, err := json.Marshal(cancelParams{ID: id})
	if err != nil {
		return
	}
	frame, err := json.Marshal(notification{JSONRPC: "2.0", Method: methodCancel, Params: raw})
	if err != nil {
		return
	}
	frame = append(frame, '\n')
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	_, _ = c.stdin.Write(frame)
}

// terminal marks the client unusable, fails every pending call, stops all
// AfterFuncs, and closes the child's stdin. Callers wake with the
// terminal error; the reap and its wait error happen afterwards in
// readLoop.
func (c *Client) terminal(err error) {
	c.mu.Lock()
	c.terminalLocked(err)
	c.mu.Unlock()
	_ = c.stdin.Close()
}

// terminalLocked is terminal with c.mu held. It runs inside the dispatch
// critical section for protocol violations, so the terminal state is
// visible to every caller before any of them can run.
func (c *Client) terminalLocked(err error) {
	if c.unusable {
		return
	}
	c.unusable = true
	c.err = err
	for _, entry := range c.pending {
		if entry.stop != nil {
			entry.stop()
		}
		entry.ch <- callResult{err: err} // nonblocking: buffered 1
	}
	c.pending = make(map[uint64]*pendingCall)
}

// Close shuts the child down: stdin closes (EOF), the reader is given up
// to 5 seconds to reap the process, then the process is killed and reaped
// forcibly. Idempotent — the second call returns nil. Pending calls fail
// with the terminal error. Safe after the child already exited.
func (c *Client) Close() error {
	c.closeOnce.Do(func() {
		_ = c.stdin.Close()
		select {
		case <-c.done:
		case <-time.After(5 * time.Second):
			_ = c.cmd.Process.Kill()
			<-c.done
		}
		c.mu.Lock()
		c.unusable = true
		if c.err == nil {
			c.err = errors.New("perk/v1: client closed")
		}
		c.mu.Unlock()
	})
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.waitErr
}
