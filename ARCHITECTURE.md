# Perk Workbench — Architecture, Design & Capabilities

A terminal UI for exploring databases, built with
[Bubble Tea](https://github.com/charmbracelet/bubbletea). It opens one
SQLite, MySQL, PostgreSQL, or MongoDB target; exposes its schema; executes
SQL (or mongosh-style statements for MongoDB); and supports table browsing,
schema changes, and row/document editing through a shared driver contract.

This document synthesizes the app's design, capabilities, and feature set.
Companion docs: [`README.md`](README.md) (quick start) and
[`DESIGN.md`](DESIGN.md) (design rationale).

---

## 1. What the app is (and is not)

| Is | Is not |
|---|---|
| Interactive terminal database client | Database server management / migrations / ORM |
| One active connection with schema, query, browse, chat | Multiple simultaneous connections |
| Plugin-host for built-in and third-party drivers | A web or GUI client |
| Optional AI assistant for natural-language SQL | AI configuration required for normal use |

The executable is `cmd/perk-workbench` (module `github.com/l3aro/perk-workbench`).
Four database modes ship in every binary: SQLite, MySQL, PostgreSQL, MongoDB.

---

## 2. Feature overview

| Area | Capabilities |
|---|---|
| **Schema** | Sidebar tree of tables/views/collections; filter (`/`); expand/collapse; add/rename/delete table; create database; per-table structure (columns), indexes, and foreign keys |
| **Query** | SQL editor with validation, tab-completion (keywords, tables, columns, statements), execution & cancellation, editor history, explain, sensitive-statement redaction, query-log with paging/detail |
| **Browse** | Paginated table browsing (25/page default), filters (LIKE / = / < / > / IS NULL / …), sorting, cell viewer, copy cell, row insert/edit/delete, cell editor with null/default, document editor for MongoDB |
| **Schema mutation** | Add/edit/delete columns; create/edit/drop indexes; create/edit/drop foreign keys — all via declarative forms; live relationship & index diagrams |
| **AI assistant** | Optional chat pane with streaming, markdown rendering, slash commands, persisted per-connection conversation history, and read/write database tools |
| **Connections** | Profile form per driver, recent list + filter, encrypted credential storage, `--select` CLI picker, Laravel-compatible `DB_*` env / `.env` bootstrap |
| **Customization** | `config.json` (defaults, keybindings, themes, log level, page sizes), 6 built-in themes + light/dark appearance, nerd-font icons, vim mode |
| **Plugins** | External drivers via the perk/v1 JSON-RPC stdio protocol; `plugin list/inspect/add/remove/doctor/test` CLI; sha256 pinning; conformance testing |
| **Platform** | Mouse support, clipboard, OSC 11 auto light/dark detection, external `$EDITOR` editing, read-only mode, session pinning (`--pin`) |

---

## 3. High-level architecture

```mermaid
flowchart LR
    subgraph Host[perk-workbench host]
        CLI[cmd/perk-workbench<br/>CLI + self-plugin dispatch]
        TUI[internal/workbench/app<br/>Bubble Tea shell]
        CORE[internal/core<br/>workflow state machine]
        DB[internal/database<br/>plugin-aware routing]
        SQLI[internal/sql<br/>shared service + DTO contracts]
        AI[internal/ai<br/>OpenAI/Anthropic/Gemini]
        CHROME[internal/chrome<br/>stateless rendering]
        CLIP[internal/clipboard]
        LOG[internal/log<br/>event log]
    end

    subgraph Features[Workbench feature packages]
        F1[querylog]
        F2[notification]
        F3[connection]
        F4[chat]
        F5[browse]
        F6[schema]
        F7[uikit]
        F8[profile]
    end

    subgraph Children[Driver child processes]
        S[perk-workbench --plugin sqlite]
        M[perk-workbench --plugin mysql]
        P[perk-workbench --plugin postgres]
        MG[perk-workbench --plugin mongodb]
        X[external executable]
    end

    CLI --> TUI
    TUI --> CORE
    TUI --> DB
    TUI --> Features
    TUI --> AI
    CORE --> SQLI
    DB --> SQLI
    TUI --> CHROME
    TUI --> CLIP
    TUI --> LOG
    DB -. spawn + perk/v1 NDJSON .-> S
    DB -. spawn + perk/v1 NDJSON .-> M
    DB -. spawn + perk/v1 NDJSON .-> P
    DB -. spawn + perk/v1 NDJSON .-> MG
    DB -. spawn + perk/v1 NDJSON .-> X
```

**Key design points**

- The four bundled backends are **child processes**, not in-process drivers.
  The host and its own built-ins communicate over the same `perk/v1` boundary
  used by external plugins (`perk-workbench --plugin sqlite|mysql|postgres|mongodb`).
- `internal/workbench/app` is a **shell**, not the home of feature state.
  Each UI feature lives in its own package; the shell coordinates layout,
  focus, keybindings, overlays, and the async query lifecycle.
- `internal/chrome` is stateless terminal rendering and must never import
  `workbench` packages or hold Bubble Tea state.
- `internal/sql.Service` is the single boundary between the UI and every
  database implementation. Capability-gated optional interfaces extend it
  without forcing every backend.

---

## 4. Process model & plugin boundary

```mermaid
flowchart TD
    CFG["config.json<br/>plugins[]"] --> LOADER[internal/database/plugin<br/>Loader]
    LOADER -->|descriptor builtin| BIN[this executable<br/>--plugin NAME]
    LOADER -->|descriptor path + sha256| EXT[external executable]
    BIN -->|spawn| P1[perk/v1 child]
    EXT -->|spawn| P2[perk/v1 child]
    P1 -->|JSON-RPC 2.0 over NDJSON stdin/stdout| SHIM[Shim implements sql.Service]
    P2 -->|JSON-RPC 2.0 over NDJSON stdin/stdout| SHIM
    SHIM --> TUI[workbench app]
    TUI -->|restart / status| LOADER
```

- Built-in descriptors are `{"builtin":"sqlite"}` (and the other families).
  External descriptors are `{"path":"…","sha256":"…"}` — the digest is
  optional, checked immediately before spawn, and applies only to external
  executables.
- Plugin identity (`name`) is **separate** from database family (`driver`):
  two plugins can both advertise `driver: "mysql"` and remain independently
  selectable. Persisted connections retain the selected plugin ID.
- `plugin` CLI commands manage descriptors: `list`, `inspect`, `add`,
  `remove`, `doctor`, `test` (perk/v1 conformance runner).
- The host ships all four built-ins in one executable and publishes no
  `plugins/` directory or independent driver assets.

---

## 5. Package map

```mermaid
flowchart TB
    subgraph app[internal/workbench/app — shell]
        M[Model: schema/browse/query/chat/notification/overlay/layout state]
        V[View: panes, workspace tabs, overlays]
        K[Keybindings + command palette]
        C[Config loading]
    end

    subgraph feat[internal/workbench — feature packages]
        SC[schema — tree, structure/index/FK forms, diagrams]
        BR[browse — result table, pager, editors, filters, cell viewer]
        CH[chat — AI assistant pane, runs, tools]
        QL[querylog — pane, paging, detail, persistence]
        NT[notification — popup, detail, history, persistence]
        CN[connection — profile form, recent list/filter]
        UI[uikit — shared editable input, filter rows, pager, form mode]
        PF[profile — connection profile load/save, encryption]
    end

    subgraph core[internal/core]
        WF[Workflow — state machine, query lifecycle, focus/tabs]
    end

    subgraph db[internal/database]
        OP[Open — plugin routing, target resolution]
        PL[plugin — loader, client, shim, conformance, digest, restart]
    end

    subgraph ssql[internal/sql]
        SVC[Service interface + Result/Opened DTOs]
        RW[RowValue / RowWriter / DocumentWriter capability contracts]
        WL[QueryLanguage / WorkspaceCapability advertisements]
    end

    subgraph drv[internal/drivers]
        D1[sqlite]
        D2[mysql]
        D3[postgres]
        D4[mongodb]
    end

    subgraph aux[internal]
        AUX1[ai — provider adapters + history]
        AUX2[chrome — rendering]
        AUX3[clipboard]
        AUX4[log — event log]
    end

    app --> feat
    app --> core
    app --> db
    app --> aux
    db --> PL
    PL --> ssql
    drv --> ssql
```

---

## 6. Connections & the database layer

### 6.1 Target forms

Targets are plain `TargetPattern` data; the registry keeps `PluginID`
unique and `Driver` non-unique.

| Driver | Prefixes | Notes |
|---|---|---|
| SQLite | *(unprefixed)*, `sqlite:` | Only existing files open (`:memory:` excepted); never creates files |
| MySQL | `mysql:` | `mysql:user:pass@tcp(host:3306)/db` |
| PostgreSQL | `postgres://`, `postgresql://`, `postgres:` | `postgres://user:pass@host:5432/db` |
| MongoDB | `mongo:`, `mongodb://`, `mongodb+srv://` | SRV targets keep their full URL |

Resolution rules (`internal/database.Open`):

- An **unprefixed** target selects plugin ID `sqlite`.
- A **prefixed** target selects its plugin only when exactly one registered
  plugin matches; an ambiguous family reports every candidate and requires
  the connection form or `--select`.

### 6.2 Connection screen states

The core workflow owns the top-level lifecycle:

```mermaid
stateDiagram-v2
    [*] --> Connection: launch, no target
    Connection --> Opening: pick / connect
    [*] --> Opening: target on CLI / env
    Opening --> Ready: schema loaded
    Opening --> Failure: open error
    Failure --> Picking: return to picker
    Picking --> Opening: pick saved profile
    Ready --> Opening: reconnect / switch profile
    Ready --> [*]: quit (after active query done)
```

### 6.3 Credential storage & encryption

- Profiles are saved to `connections.json` under `~/.config/perk-workbench/`.
- Literal passwords and credential-bearing targets are encrypted at rest with
  **AES-256-GCM** using a key in `secret.key` (same directory); each
  ciphertext is bound to its profile and field; files are locked to
  0700/0600.
- Non-credential targets (file paths, database names) stay plaintext.
- **Threat model:** the key and ciphertext live in the same user-owned
  directory, so encryption protects against accidental disclosure and copies
  of config (backups, screenshots) — not against an attacker with account
  access. Env/file references are the stronger separation.
- An undecryptable stored password is never shown as plaintext and never
  rewritten; the app reports it and refuses to save until the value is
  re-entered.

### 6.4 Connection form

Each driver advertises a declarative `FormSpec`. Fields are ordered,
validated, and serialized as `FormValues`/targets across the same DTO
boundary used by external plugins.

- **SQLite:** single `target` field (`path/to/database.db` or `:memory:`).
- **MySQL / PostgreSQL:** host, port (validated 1–65535, default 3306/5432),
  username, password, optional database, and a TLS select
  (`Verify certificate` / `Encrypt, don't verify` / `Don't encrypt`).
- **MongoDB:** no form; connection targets are built from the prefixed URL.

---

## 7. Database backends

All four backends implement the same `internal/sql.Service` contract and are
swappable through the plugin boundary. Optional advertisements extend each
driver without forcing others.

| | SQLite | MySQL | PostgreSQL | MongoDB |
|---|---|---|---|---|
| Query language | SQL | SQL | SQL | mongosh-style DSL (JS lexer) |
| Row writer (INSERT/UPDATE/DELETE) | ✅ | ✅ | ✅ | — (document writer) |
| Document writer (extended JSON) | — | — | — | ✅ |
| Workspace tabs advertised | legacy policy | Columns/Indexes/FK/Diagram | Columns/Indexes/FK/Diagram | Columns/Indexes + custom **Stats** view |
| Row counts in schema | exact | estimate (`information_schema`) | estimate (`pg_class.reltuples`) | — |
| TLS options | — | ✅ | ✅ | via connection string |

**MongoDB DSL.** The query editor accepts `db.<collection>.find(...)`,
`countDocuments`, `aggregate`, `distinct`, writes, `drop`, `createIndex`,
and `show collections`/`show dbs`, validated by its own parser. Collections
map to tables; `TableInfo` samples up to 100 documents with `_id` as the
implicit primary key; indexes are real MongoDB indexes. Cell viewer/copy
render object and array cells as **relaxed extended JSON** so values paste
straight into mongosh / mongoimport.

**Workspace capabilities.** `WorkspaceCapability` advertises which standard
tabs (Columns/Indexes/Foreign Keys/Diagram) a driver supports plus ordered
custom plain-data views (e.g. MongoDB's "Stats"). A nil capability keeps the
legacy per-product policy. Custom views carry no code or UI: the driver only
answers bounded table data for a `workspace_view` request.

---

## 8. Query lifecycle

```mermaid
sequenceDiagram
    participant UI as Workbench app
    participant WF as core.Workflow
    participant SVC as Service (driver child)
    participant R as Result table

    UI->>WF: StartQuery(ctx, statement)
    Note over WF: one query at a time — new request ID + child context
    WF->>SVC: Execute / ExecuteReadOnly (async)
    UI->>WF: Escape pressed → CancelQuery()
    WF-->>SVC: cancel context (perk/v1/cancel)
    SVC-->>WF: Result or error (completion msg w/ request ID)
    alt request ID matches active query
        WF->>R: replace displayed results
    else stale request ID
        Note over WF: completion ignored
    end
    Note over R: failure/cancel only appends query log —<br/>prior result table is preserved
```

Rules enforced by the shell:

1. Only **one** query runs at a time (async, cancelable).
2. Completion messages with a stale request ID are dropped.
3. `Escape` cancels the active context.
4. Failure and cancellation **preserve the prior result table** and append to
   the query log.
5. A pending quit completes only after the active query finishes or cancels.
6. Ad-hoc statements pass `sql.ValidateStatement`: one non-empty statement,
   at most one trailing semicolon, no trigger creation.

Display limits: results cap at **500 rows** and **300 runes per cell**;
`CollectRows` stores both display-safe and full cell values so the cell
viewer shows original content.

---

## 9. UI model

```mermaid
flowchart TB
    subgraph root[Root shell — internal/workbench/app]
        H[Header / status line]
        SIDEBAR[Schema sidebar]
        WS[Workspace tab row]
        QL[Query log pane]
        CHAT[AI chat pane]
        OL[Overlays: palette, pickers, menus, dialogs,<br/>context menus, confirmation, cell viewer]
    end

    SIDEBAR -->|select table/scope| WS
    WS --> TABS[Query / Browse / Structure / Indexes / Foreign Keys / Diagram / custom]
    TABS --> P1[SQL editor + results]
    TABS --> P2[Browse table + pager + editors]
    TABS --> P3[Structure / index / FK forms]
    TABS --> P4[Relationship / index diagrams]
    QL -->|detail / explain / copy| OL
    CHAT -->|sql_read / sql_write tools| OL
```

- Focus cycles through **Schema → Workspace → Query Log → AI Chat**
  (`1`/`2`/`3`/`4`), with `tab`/`shift+tab` or `]`/`[` for forward/backward,
  and `f` for fullscreen.
- Workspace **scope** is driven by the sidebar: no selection, a database, a
  PostgreSQL schema, or a qualified table/collection. The visible tab set is
  derived from that scope plus the driver's workspace advertisement.
- `table_open_target` config decides which workspace tab opens when a table
  is selected in the tree (structure / browse / sql / indexes / foreign_keys).
- The **feature-event rule:** feature logic lives in feature packages; the
  root only routes messages and applies typed events. Overlay precedence is
  pinned: palette/theme/table-target → notification history → notification
  detail → query-log detail → confirm overlays → popup (drawn last).
- Frames use rounded corners throughout; schema-tree connectors (`└`) and the
  confirmation accent bar (`┃`) keep square glyphs.

### 9.1 Focus diagrams

The Foreign Keys tab (`g`) toggles a **relationship diagram** and the
Indexes tab an **index diagram**: the selected table is the hub card, tables
referencing it render above, tables it references below. Edges read
`(1)──▶(N)` from parent to child (cardinalities derived from FK columns and a
cached whole-schema index map). Ring data comes from connection-level caches
(`ListForeignKeysAll`/`ListIndexesAll`) refreshed on connect and after every
DDL mutation, stamped with connection generation + revision so stale results
are dropped. Diagrams too large fall back to a flat list.

---

## 10. Feature packages

### schema — sidebar + structure tabs
Schema tree with filter, expand/collapse (accordion animation), context menu
(`,`) for table actions, database/schema scope navigation, and the
Structure/Indexes/Foreign-Keys tabs with declarative create/edit/delete forms
and type pickers.

### browse — data exploration & editing
Paginated result table (filters, sorting, row limit), page navigation
(`n`/`p`), row insert/edit/delete, cell editor with tri-state values
(Default/Null/String), cell viewer (`v`), copy cell (`y`), and — for MongoDB
— a JSON document editor for insert and whole-document replace. All write
actions are gated by the driver's serializable `WriteCapabilities` descriptor.

### chat — AI assistant
Streaming chat pane with markdown rendering (glamour), multi-turn runs that
stay independent per conversation, slash-command completions, prompt recall,
conversation rename/delete/clear, and tool rounds (see §12). Write calls
(`sql_write`) raise a root-owned confirmation dialog unless `/yolo-on`.

### querylog — query history
Query-log pane with paging, detail overlay, copy, explain, and scoped
persistence. Entries carry language, replayable/sensitive flags; sensitive
statements are stored redacted (verbatim text lives only in a transient
in-session cache and is resolved on explicit copy).

### notification — status pipeline
Popup toasts, detail, and a persisted history; retention and timeout are
configurable. The opening notice is transient — it toasts and reaches
`event.log` but never persists to history.

### connection — profiles & recent
Profile form, recent list with filter (`/`), add/edit/delete, context menu,
and the file picker; each profile carries a stable ID used as the scope for
query-log, notification, and chat history.

### uikit — shared widgets
Reusable `editable_input`, `filter`/`filter_row`, `pager`, `form_buttons`,
`form_mode`, `form_theme`, `confirmation`, and `cell_viewer` primitives that
feature packages compose so UI behavior stays consistent.

### profile — persistence + encryption
Loads/saves `connections.json`, manages the AES-256-GCM key in
`secret.key`, and resolves legacy profiles whose plugin ID is omitted
against the live registry (only when exactly one plugin serves the saved
driver; ambiguous records are never offered).

---

## 11. AI integration

AI is **optional** and never blocks opening or using a database.

### 11.1 Configuration layering

1. User config: `~/.config/perk-workbench/ai.json`
2. Project config: `.perk-workbench/ai.json` (overrides by name)

Strict JSON decoding rejects unknown fields and multiple JSON values. AI
activates only when an `assistant` agent is configured.

### 11.2 Providers & agents

```mermaid
flowchart LR
    subgraph Providers
        P1[openai]
        P2[anthropic]
        P3[gemini]
        P4[openai-compatible]
    end
    subgraph Agents[Agents in ai.json]
        A1[assistant — default]
        A2[spark — cheap / titles]
        A3[oracle — /premium, @oracle, complex prompts]
    end
    subgraph Client[ai.Client]
        C1[Chat / Complete non-streaming]
        C2[ChatStream streaming]
        C3[SupportsTools]
    end
    Providers --> Client
    A1 --> Client
    A2 --> Client
    A3 --> Client
```

- Provider/agent fields may reference environment variables via `env:NAME`.
- Tool calls are supported on OpenAI and OpenAI-compatible APIs (Anthropic
  and Gemini currently reject tool definitions).

### 11.3 Tools exposed to the assistant

| Tool | Gated by | Behavior |
|---|---|---|
| `sql_read` | always | Read-only query (SELECT/EXPLAIN/SHOW/PRAGMA/DESCRIBE); returns ≤500 rows, ≤40 chars/cell |
| `get_connection_info` | always | Product/version/user/database/session/host per backend |
| `get_visible_results` | `/share-results` | Current SQL results table |
| `sql_write` | not read-only | Exactly one write/DDL statement; requires user confirmation (unless `/yolo-on`) |

### 11.4 Slash commands

`/new` (fresh conversation), `/history` (pick a saved conversation),
`/yolo-on` / `/yolo-off` (skip write confirmations), `/share-results` /
`/unshare-results` (expose visible results to the assistant).

### 11.5 Conversation history

Persisted per connection scope in `~/.config/perk-workbench/data.db`
(SQLite): conversations and messages, with auto-generated titles (from the
cheap agent), rename, delete, and clear. Runs are independent — switching the
visible conversation never interrupts a background run.

---

## 12. Row & document writes (browse CRUD)

Writes are split across the boundary: `internal/workbench/browse` owns the
forms/editors and emits typed requests; the shell owns statement
construction/execution and capability wiring; drivers own dialect.

A **serializable capability descriptor** plus narrow optional interfaces in
`internal/sql` gate every action:

```go
type WriteCapabilities struct {
    RowWriter bool                      `json:"row_writer"`
    Document  *DocumentWriteCapability  `json:"document,omitempty"`
}
```

- SQL family drivers implement `RowWriter` (`InsertRow`/`UpdateRow`/
  `DeleteRow`), binding values as parameters.
- MongoDB implements `DocumentReader`/`DocumentWriter` exchanging
  `DocumentPayload` — a format tag (`application/vnd.perk.mongodb.extjson+json;version=2;mode=relaxed`) plus bytes — so documents travel exactly.
- `RowValue` is an explicit tagged tree (String/Bool/Integer/Float/Bytes/
  Decimal/Timestamp/Array/Object), JSON-encodable, so it survives a future
  out-of-process plugin boundary.
- The workbench derives its capability descriptor from
  `WriteCapabilitiesProvider` (or falls back to service interface
  assertions); product names are display-only.
- Read-only is a workbench policy, not a driver convention.

```mermaid
flowchart LR
    BR[Browse tab / form] -->|typed request| SH[app shell]
    SH -->|capability check| CAP{WriteCapabilities}
    CAP -->|RowWriter| RW[driver.RowWriter<br/>parameterized SQL]
    CAP -->|Document| DW[DocumentReader/Writer<br/>extended-JSON payloads]
    RW --> LOG[query log entry<br/>structured RowValue preview]
    DW --> LOG2[query log entry<br/>format-tagged payload]
```

---

## 13. The perk/v1 protocol

Every built-in and external plugin serves the same **JSON-RPC 2.0,
newline-delimited** protocol on stdin/stdout. The canonical contract lives as
an embedded JSON Schema with fixtures under `protocol/perk-v1/` (single
compiled-in source for the conformance runner).

**Methods** (from `internal/database/plugin/protocol.go`):

| Group | Methods |
|---|---|
| Lifecycle | `initialize`, `build_target`, `open`, `close`, `cancel` (notification) |
| Query | `execute`, `execute_read_only`, `validate` |
| Schema | `list_schema`, `table_info` |
| Indexes | `list_indexes`, `list_indexes_all`, `create_index`, `replace_index`, `drop_index` |
| Foreign keys | `list_foreign_keys`, `list_referencing_foreign_keys`, `list_foreign_keys_all`, `create_foreign_key`, `replace_foreign_key`, `drop_foreign_key` |
| Columns | `alter_column`, `drop_column`, `add_column` |
| Browse | `browse_table` |
| Writes | `row_write`, `document_write` |
| Views | `workspace_view` |

Conformance (`plugin test`) drives a child through handshake, capability
advertisement, framing, cancellation, and error-shape cases, producing an
evidence document (JSON Schema in `protocol/perk-v1/`).

```mermaid
sequenceDiagram
    participant Host as Loader (host)
    participant Child as plugin child
    Host->>Child: perk/v1/initialize
    Child-->>Host: capabilities, name, driver, form, language
    Host->>Child: perk/v1/open {target}
    Child-->>Host: databaseInfo + opened service
    Host->>Child: perk/v1/execute {statement}
    Child-->>Host: result (rows, columns, metadata)
    Host->>Child: perk/v1/cancel
    Child-->>Host: error kind=cancelled
    Host->>Child: perk/v1/close
```

---

## 14. Configuration & persistence

All persisted state lives under `~/.config/perk-workbench/`
(`os.UserConfigDir()`).

| Path | Contents |
|---|---|
| `config.json` | App defaults (written on first run) |
| `connections.json` | Encrypted connection profiles |
| `secret.key` | AES-256-GCM key for profile encryption |
| `data.db` | Shared SQLite: query-log history, notification history, AI conversation history (scoped by connection ID) |
| `event.log` | Structured event log (debug/info/warn/error) |
| `ai.json` | User AI provider/agent config |

### 14.1 `config.json` fields (all optional; 0/omitted = built-in default)

| Field | Meaning |
|---|---|
| `browse_page_size` | Default row limit for table browsing (1–500) |
| `query_log_page_size` | Query-log pane page size (1–100) |
| `query_log_retention_days` | Days of query-log history kept (default 30; `PERK_WORKBENCH_QUERY_LOG_RETENTION_DAYS=0` keeps none) |
| `notification_retention_days` | Notification history retention |
| `notification_timeout_seconds` | Popup toast timeout |
| `read_only` | Open every connection read-only by default |
| `appearance` | `light` / `dark` / `auto` |
| `auto_theme` | Automatically pick theme by terminal appearance |
| `dark_theme` / `light_theme` | Theme names for each appearance |
| `vim_mode` | Vim-style editing in editors |
| `nerd_font` | Nerd-font icons in the UI |
| `log_level` | Minimum severity written to `event.log` (`debug`/`info`/`warn`/`error`) |
| `table_open_target` | Tab focused after selecting a table (`structure`/`browse`/`sql`/`indexes`/`foreign_keys`) |
| `plugins` | Built-in / external plugin descriptors |
| `keybinds` | Manual keybinding overrides (never materialized) |

Environment variables still override their config values
(`PERK_WORKBENCH_QUERY_LOG_*`).

### 14.2 Keybinding overrides

There is **no keybindings file**; overrides live in the optional `keybinds`
object inside `config.json`. Both flat (`"app.quit": ["q"]`) and nested
(`"app": {"quit": ["q"]}`) maps are accepted; an empty array disables a
command. Unknown command IDs or invalid keystrokes fail startup with the
config path.

### 14.3 CLI

```
Usage: perk-workbench [--read-only] [--select] [--pin] [database]

Self-plugin mode:
  perk-workbench --plugin NAME        (sqlite, mysql, postgres, mongodb)

Commands:
  plugin list|inspect|add|remove|doctor|test   (perk/v1 tooling)

Options:
  --read-only / -r   open every connection read-only
  --select           choose a saved connection interactively (CLI picker)
  --pin              lock the session: disable all in-app quit affordances
  --version / -v     print "perk-workbench <version>"
  -h / --help        help
```

`--select` resolves legacy profiles against the live plugin registry and
persists uniquely-resolved plugin IDs.

---

## 15. Keybindings reference (built-in defaults)

| Group | Keys |
|---|---|
| **Global** | `ctrl+c` quit · `ctrl+q` quit w/ confirm · `ctrl+e` external editor · `f5`/`ctrl+enter`/`ctrl+s` run · `esc` cancel · `ctrl+r` query history · `1`–`4` focus schema/workspace/log/chat · `ctrl+g` toggle AI · `f` fullscreen · `tab`/`]` next, `shift+tab`/`[` prev |
| **Schema tree** | `/` filter · `enter` open · `→`/`l` expand · `←`/`h` collapse · `a` add table · `A` create database · `m`/`r` rename · `d` delete · `,` context menu |
| **Structure tab** | `/` filter · `r` reset · `enter`/`i` edit column · `a` add · `d` delete |
| **Browse tab** | `enter`/`e` edit row · `a` insert row · `d` delete row · `i` edit cell · `v` view cell · `/` filter+limit · `r` reset · `s` sort · `n`/`p` next/prev page · `y` copy cell · `,` context menu |
| **Indexes / FK tabs** | `/` filter · `r` reset · `g` diagram toggle · `n` new · `enter`/`i` edit · `d` delete · `}`/`{` diagram depth ± |
| **Query log** | `y` copy · `e` explain · `enter` detail · `,` context menu · `g`/`G` top/bottom · `n`/`p` page · `ctrl+d` delete chat · `ctrl+l` clear chats · `ctrl+a` apply SQL |
| **Detail overlay** | `y` copy · `e` explain · `enter`/`esc` close |
| **Connection** | `2` form · `1` profiles · `/` filter · `a` add · `e`/`enter` edit · `d` delete · `,` context menu · `f5`/`ctrl+enter` connect |
| **Forms** | `enter` edit · `ctrl+enter`/`ctrl+s`/`f5` save · `esc` discard · `j`/`k` field nav · `n`/`N` null/default · `g`/`G` top/bottom · `d` delete |
| **Chat** | `ctrl+space` completion · slash commands (`/new`, `/history`, …) |

---

## 16. Design invariants

- `internal/chrome` must not import `workbench` or hold Bubble Tea state.
- Driver SQL and connection details stay out of `workbench` and
  `internal/sql`.
- Execution stays asynchronous and cancelable; stale completions are never
  applied; failed queries retain prior results.
- The SQLite no-create-file rule holds (existing files only; `:memory:`
  excepted).
- New driver capabilities live in `internal/sql.Service` only when every
  backend can implement them coherently; otherwise behavior stays local and
  is exposed via capability-gated optional interfaces.
- AI remains optional and must not block opening or using a database.

---

## 17. Development & verification

```bash
go test -race ./cmd/... ./internal/...
go vet ./cmd/... ./internal/...
go build ./cmd/perk-workbench
gofmt -l cmd internal
```

Demo databases:

```bash
make sqlite      # Chinook (direct SQLite self-plugin)
make mysql       # Office demo (MySQL via Docker)
make postgres    # Employees demo (PostgreSQL via Docker)
make mongo       # Restaurants demo (MongoDB via Docker)
```

Manual query QA: `F5` executes, `Escape` cancels a running query.
