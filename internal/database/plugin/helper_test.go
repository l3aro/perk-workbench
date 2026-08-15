package plugin

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/l3aro/perk-workbench/internal/database"
	sharedsql "github.com/l3aro/perk-workbench/internal/sql"
)

// spawnArgs (declared in loader.go) is the argv appended to every plugin
// spawn. TestMain sets it once so Load re-executes the current test
// binary as the plugin child; production code leaves it nil.
func TestMain(m *testing.M) {
	spawnArgs = []string{"-test.run=TestPluginHelperChild"}
	os.Exit(m.Run())
}

// TestPluginHelperChild is the re-executed plugin child. It serves the
// perk/v1 protocol on stdio, driven by PERK_PLUGIN_* env vars, and always
// ends with os.Exit — never returning to the testing framework, so no
// PASS/ok output corrupts the protocol stream.
func TestPluginHelperChild(t *testing.T) {
	if os.Getenv("PERK_PLUGIN_HELPER") != "1" {
		return
	}
	helper := &pluginHelper{
		name:            os.Getenv("PERK_PLUGIN_NAME"),
		display:         os.Getenv("PERK_PLUGIN_DISPLAY"),
		targets:         strings.Split(os.Getenv("PERK_PLUGIN_TARGETS"), ","),
		protocolVersion: envInt("PERK_PLUGIN_PROTOCOL_VERSION", 1),
		behavior:        os.Getenv("PERK_PLUGIN_BEHAVIOR"),
		marker:          os.Getenv("PERK_PLUGIN_MARKER"),
		rowWriter:       os.Getenv("PERK_PLUGIN_ROW_WRITER") == "1",
		document:        os.Getenv("PERK_PLUGIN_DOCUMENT") == "1",
	}
	if raw := os.Getenv("PERK_PLUGIN_SCHEMA"); raw != "" {
		if err := json.Unmarshal([]byte(raw), &helper.schemaObjects); err != nil {
			os.Exit(2)
		}
		helper.schemaSet = true
	}
	if helper.name == "" {
		helper.name = "pluginkv"
	}
	if helper.display == "" {
		helper.display = "PluginKV"
	}
	if len(helper.targets) == 1 && helper.targets[0] == "" {
		helper.targets = []string{"pluginkv:"}
	}
	helper.serve()
}

type pluginHelper struct {
	reader          *bufio.Reader
	name            string
	display         string
	targets         []string
	protocolVersion int
	behavior        string
	marker          string
	rowWriter       bool
	document        bool
	schemaObjects   []sharedsql.SchemaObject
	schemaSet       bool
}

// serve reads request frames and answers until stdin closes.
func (h *pluginHelper) serve() {
	h.reader = bufio.NewReaderSize(os.Stdin, MaxFrameBytes)
	for {
		frame, err := readFrame(h.reader)
		if err != nil {
			os.Exit(0) // stdin closed: normal end of service
		}
		var incoming struct {
			ID     *uint64         `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		if err := json.Unmarshal(frame, &incoming); err != nil {
			os.Exit(1)
		}
		if incoming.ID == nil {
			h.handleNotification(incoming.Method, incoming.Params)
			continue
		}
		if !h.handleRequest(*incoming.ID, incoming.Method, incoming.Params) {
			return
		}
	}
}

// handleNotification processes a host notification (perk/v1/cancel);
// notifications never get a response.
func (h *pluginHelper) handleNotification(method string, params json.RawMessage) {
	if method != methodCancel {
		return
	}
	var canceled cancelParams
	_ = json.Unmarshal(params, &canceled)
	h.markerLine(fmt.Sprintf("cancel %d", canceled.ID))
}

// handleRequest answers one request. false stops the serve loop.
func (h *pluginHelper) handleRequest(id uint64, method string, params json.RawMessage) bool {
	switch h.behavior {
	case "wrong_version":
		if method == methodInitialize {
			h.respond(id, initializeResult{ProtocolVersion: 2, Capabilities: h.capabilities()}, nil)
			return true
		}
	case "wrong_jsonrpc":
		fmt.Fprintf(os.Stdout, `{"jsonrpc":"1.0","id":%d,"result":{}}`+"\n", id)
		return true
	case "malformed":
		fmt.Fprint(os.Stdout, "not json\n")
		return true
	case "nonutf8":
		// A frame whose string content is not valid UTF-8: json.Unmarshal
		// would substitute replacement characters, so the host must gate
		// on UTF-8 validity explicitly.
		_, _ = os.Stdout.Write([]byte("{\"jsonrpc\":\"2.0\",\"id\":1,\"result\":\"\xff\xfe\"}\n"))
		return true
	case "nonnumeric_id":
		fmt.Fprint(os.Stdout, "{\"jsonrpc\":\"2.0\",\"id\":\"nope\",\"result\":{}}\n")
		return true
	case "oversized":
		fmt.Fprint(os.Stdout, strings.Repeat(" ", 17<<20)+"\n")
		return true
	case "wrong_id":
		h.respond(999999, struct{}{}, nil)
		return true
	case "duplicate":
		// Respond twice to the first request, in one write: the first
		// response completes it, the second is the duplicate that must
		// terminate the host. A single write keeps both frames
		// observable back-to-back, so the host's reader processes the
		// duplicate before any caller can observe the first response.
		result := h.resultFor(method, params)
		frame1 := h.responseFrame(id, result, nil)
		frame2 := h.responseFrame(id, result, nil)
		_, _ = os.Stdout.Write(append(frame1, frame2...))
		return true
	case "exit_on_execute":
		if method == methodExecute || method == methodExecuteReadOnly {
			os.Exit(3)
		}
	case "block_execute":
		if method == methodExecute || method == methodExecuteReadOnly {
			h.markerLine(fmt.Sprintf("execute %d", id))
			for {
				frame, err := readFrame(h.reader)
				if err != nil {
					os.Exit(0) // stdin closed before the cancel arrived
				}
				var incoming struct {
					ID     *uint64         `json:"id"`
					Method string          `json:"method"`
					Params json.RawMessage `json:"params"`
				}
				if err := json.Unmarshal(frame, &incoming); err != nil {
					os.Exit(1)
				}
				if incoming.ID == nil && incoming.Method == methodCancel {
					var canceled cancelParams
					_ = json.Unmarshal(incoming.Params, &canceled)
					if canceled.ID == id {
						h.markerLine(fmt.Sprintf("cancel %d", id))
						h.respond(id, nil, &rpcError{Code: RPCErrorCanceled, Message: "canceled"})
						return true
					}
				}
			}
		}
	case "schema_error":
		if method == methodListSchema {
			h.respond(id, nil, &rpcError{Code: -32000, Message: "schema exploded"})
			return true
		}
	case "out_of_order":
		if method == methodExecute || method == methodExecuteReadOnly {
			// Respond late, from another goroutine, so the immediate
			// responses for other requests overtake this one.
			go func() {
				time.Sleep(300 * time.Millisecond)
				h.respond(id, sharedsql.Result{
					Columns:      []string{"EXECUTED"},
					Rows:         [][]*string{{strPtr("late")}},
					RowsAffected: 1,
					Duration:     time.Millisecond,
				}, nil)
			}()
			return true
		}
		if method == methodListSchema {
			h.respond(id, []sharedsql.SchemaObject{{Database: "pluginkv", Type: "collection", Name: "LISTS"}}, nil)
			return true
		}
	}
	h.respond(id, h.resultFor(method, params), nil)
	return true
}

// resultFor builds the canned response for one method.
func (h *pluginHelper) resultFor(method string, params json.RawMessage) any {
	switch method {
	case methodInitialize:
		return initializeResult{ProtocolVersion: h.protocolVersion, Capabilities: h.capabilities()}
	case methodBuildTarget:
		var values database.FormValues
		_ = json.Unmarshal(params, &values)
		return buildTargetResult{Target: "pluginkv:" + values.Host, OK: true}
	case methodOpen:
		return openResult{SessionID: 7, Info: sharedsql.DatabaseInfo{Product: "PluginKV", Version: "helper"}}
	case methodClose, methodValidate, methodCreateIndex, methodReplaceIndex, methodDropIndex,
		methodCreateForeignKey, methodReplaceForeignKey, methodDropForeignKey,
		methodAlterColumn, methodDropColumn, methodAddColumn:
		return struct{}{}
	case methodExecute, methodExecuteReadOnly, methodBrowseTable:
		return sharedsql.Result{
			Columns:      []string{"name"},
			Rows:         [][]*string{{strPtr("widgets")}},
			RowsAffected: 1,
			Duration:     time.Millisecond,
		}
	case methodListSchema:
		if h.schemaSet {
			return h.schemaObjects
		}
		return []sharedsql.SchemaObject{{Database: "pluginkv", Type: "collection", Name: "widgets"}}
	case methodTableInfo:
		return []sharedsql.ColumnInfo{{Name: "name", Type: "string", Nullable: true}}
	case methodListIndexes:
		return []sharedsql.IndexInfo{}
	case methodListForeignKeys:
		return []sharedsql.ForeignKeyInfo{}
	case methodListReferencingForeignKeys:
		return []sharedsql.ReferencingForeignKeyInfo{}
	case methodListForeignKeysAll:
		return map[string][]sharedsql.ForeignKeyInfo{}
	case methodListIndexesAll:
		return map[string][]sharedsql.IndexInfo{}
	case methodRowWrite:
		return sharedsql.RowWriteResponse{Result: sharedsql.WriteResult{RowsAffected: 1}}
	case methodDocumentWrite:
		var request documentWriteParams
		_ = json.Unmarshal(params, &request)
		response := sharedsql.DocumentWriteResponse{Result: sharedsql.WriteResult{RowsAffected: 1}}
		if request.Request.Operation == sharedsql.DocumentWriteRead {
			response.Document = request.Request.ID
		}
		return response
	}
	return struct{}{}
}

// capabilities builds the advertised driver capabilities from env.
func (h *pluginHelper) capabilities() database.Capabilities {
	caps := database.Capabilities{Name: h.name, Display: h.display}
	for _, prefix := range h.targets {
		if strings.TrimSpace(prefix) == "" {
			continue
		}
		caps.Targets = append(caps.Targets, database.TargetPattern{Prefix: prefix})
	}
	if h.rowWriter {
		caps.WriteCapabilities.RowWriter = true
	}
	if h.document {
		caps.WriteCapabilities.Document = &sharedsql.DocumentWriteCapability{
			Format: sharedsql.DocumentFormatMongoExtendedJSON,
			Text:   true,
		}
	}
	return caps
}

// responseFrame builds one complete response frame.
func (h *pluginHelper) responseFrame(id uint64, result any, rpcErr *rpcError) []byte {
	frame := response{JSONRPC: "2.0", ID: id, Error: rpcErr}
	if rpcErr == nil {
		payload, err := json.Marshal(result)
		if err != nil {
			os.Exit(1)
		}
		frame.Result = payload
	}
	payload, err := json.Marshal(frame)
	if err != nil {
		os.Exit(1)
	}
	return append(payload, '\n')
}

// respond writes one response frame.
func (h *pluginHelper) respond(id uint64, result any, rpcErr *rpcError) {
	_, _ = os.Stdout.Write(h.responseFrame(id, result, rpcErr))
}

// markerLine appends one event line to the marker file, flushing each
// write.
func (h *pluginHelper) markerLine(line string) {
	if h.marker == "" {
		return
	}
	file, err := os.OpenFile(h.marker, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	_, _ = file.WriteString(line + "\n")
	_ = file.Close()
}

func envInt(name string, fallback int) int {
	value, err := strconv.Atoi(os.Getenv(name))
	if err != nil {
		return fallback
	}
	return value
}

func strPtr(value string) *string { return &value }
