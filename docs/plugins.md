# Perk Workbench external plugins (perk/v1)

External database plugins let third parties add backends to perk-workbench
without touching the Go codebase. A plugin is a separate executable that
speaks the **perk/v1** protocol to the host: JSON-RPC 2.0 over
newline-delimited UTF-8 JSON on stdin/stdout. The host spawns it, verifies
it at handshake, and registers it in the driver group, so a plugin-backed
driver is indistinguishable from a compiled-in one.

This document is the normative perk/v1 contract. The authoritative
implementations are:

- Go host: `internal/database/plugin` (transport, loader, client, shim),
  `internal/database/drivers.go` (capabilities DTOs, registration
  validation), `internal/sql` (shared service DTOs and row/document write
  DTOs), `internal/workbench/app/config.go` (config allowlist).
- Node SDK: `npm/plugin-sdk/index.cjs` and `npm/plugin-sdk/index.d.ts`
  (package `perk-workbench-plugin-sdk`).

## Machine-readable contract

The contract has a canonical machine-readable companion:

- `protocol/perk-v1/schema.json` — JSON Schema draft 2020-12 describing
  the v1 wire shapes: the JSON-RPC request/notification/success/error
  envelopes (numeric integer ids, `jsonrpc: "2.0"`, exact method
  names), error data and the stable kinds, initialize capabilities
  (including `query_language`), forms and targets, every shared SQL DTO,
  the row/document write DTOs, and a `$defs/methods` registry mapping
  each `perk/v1/<method>` constant to its params and result schema.
- `protocol/perk-v1/fixtures/` — canonical frames: representative valid
  request/notification/success/error frames plus parseable invalid
  semantic frames, each described by `manifest.json` (file, validity,
  schema `$ref` target, expected method/error). The Go host and Node
  SDK test suites load these exact files; there are no copies.
  Boundary-sized (16 MiB) frames are deliberately not stored; conformance
  tests generate them.

### Compatibility policy

The full policy is the versioned asset `protocol/perk-v1/policy.md`
(policy version 1); the rules, in brief:

- **Unknown members MUST be ignored.** Every v1 JSON object tolerates
  unknown members — the schema marks `additionalProperties: true`
  throughout — so either side may add optional fields without a
  protocol version bump. An implementation MUST ignore unknown
  members: it must not reject a frame, change behavior, or fail a
  session because of them.
- **Additive changes are compatible.** Adding optional members,
  method-independent metadata (for example the structured error `data`
  provenance), and new *optional* capabilities (an optional write
  interface, an optional query-language command) is additive: a
  receiver that ignores unknown members stays interoperable, so no
  protocol version bump is required.
- **Stable enums and method names.** Enum values and the
  `perk/v1/<name>` method constants are frozen for v1. Adding,
  renaming, or renumbering them is a protocol change that requires a
  new version.
- **Breaking changes require perk/v2.** Changing a required field,
  changing the meaning of an existing field, changing error codes or
  error semantics, changing framing (frame size bound, encoding,
  newline/id rules), or changing the behavior of an existing method is
  breaking. A breaking change MUST move to a new `perk/v2` method
  namespace and a new protocol version; v1 frames and semantics are
  never altered in place.
- **The host speaks exactly protocol 1 today.** The initialize result
  must echo the requested `protocol_version` (currently `1`); a
  different value rejects the plugin before registration.
  `workbench_version` is informational — plugins must not gate on its
  exact value. The host does not implement perk/v2.
- **Prerelease expectations.** perk-workbench and perk/v1 plugins are
  pre-1.0 software. v1 is a contract between a specific host build and
  a specific plugin build, not a claim of global API stability beyond
  this policy. Host releases SHOULD inject their build version
  (`-ldflags "-X main.version=<version>"`; `perk-workbench --version`
  reports it, and an uninjected build reports `devel` honestly).
  Plugins SHOULD version their releases. The authoritative
  compatibility evidence is the `plugin test` evidence document, whose
  contract digest pins the exact schema, manifest, and fixture set a
  plugin was verified against.
- **Descriptive, not enforced.** The host never loads or evaluates the
  schema at runtime; it is a contract reference for tools and tests. The
  authoritative implementations remain the Go host and the Node SDK,
  and both test suites exercise the shared fixtures.

## Trust model

A configured plugin is **trusted code**. It is an executable the workbench
spawns as a child process with the workbench's own OS privileges: it can
read and write whatever the workbench user can, including the database
files and credentials the workbench opens. `config.json` is the explicit
**allowlist** — nothing is auto-discovered, nothing runs unless the user
listed it there. Adding an entry is the user's explicit authorization.

Entries added through `perk-workbench plugin add --approve` (or the TUI
plugins flow) are **pinned**: the config records the lowercase SHA-256 of
the exact executable bytes that were approved, and startup verifies that
fingerprint immediately before spawning the child. A pinned executable
whose bytes have changed since approval (a reinstall, a rebuild, a
tampered file) is **refused** with a clear expected/actual drift error and
never executes; the other configured plugins still load. Hand-written
`plugins` entries without a pin load **unpinned** exactly as before, for
backwards compatibility — `plugin list` and `plugin doctor` report them as
such, and pinning them is one `plugin add --approve` away.

## Configuring plugins

Plugins are listed in the workbench config file
(`$XDG_CONFIG_HOME/perk-workbench/config.json`) under `plugins`, e.g.:

```json
{"plugins":["perk-redis","/home/alice/projects/clickhouse-driver/node_modules/.bin/perk-clickhouse"]}
```

`plugin add --approve` additionally writes a `plugin_trust` object that
maps each pinned executable's **canonical absolute path** to its approved
SHA-256 digest:

```json
{"plugins":["/home/alice/.local/bin/perk-redis"],
 "plugin_trust":{"/home/alice/.local/bin/perk-redis":"0123…cdef"}}
```

The trust key is the same canonical path `plugins` entries resolve to
(`filepath.Abs` + `EvalSymlinks`), so the pin survives any entry spelling
that resolves to the same file. A `plugin_trust` digest must be 64
hexadecimal characters and its key an absolute path; a malformed trust
record fails config parsing, and a valid pin that does not match the
current bytes is refused per entry at startup. `plugin add` persists the
entry as its canonical absolute path, so the pin and the entry can never
resolve apart.

Resolution rules for one entry:

- **Bare names** (no path separator, e.g. `perk-redis`) resolve through
  `PATH` (`exec.LookPath`), so a globally installed plugin
  (`npm install -g` of a plugin package) works by name.
- **Paths containing a separator** resolve relative to the config file's
  directory when relative, and are used as-is when absolute. This is how
  a project-local install is addressed: list the absolute path into the
  project's `node_modules/.bin`, or a relative path when the config file
  lives in that project.
- Every entry is canonicalized (`filepath.Abs` + `EvalSymlinks`) and must
  be a regular file with at least one executable permission bit. Listing
  the same canonical executable twice loads it once.
- Each item is an **executable**, not an npm package spec. `npm install`
  of a plugin package only places its `bin` executable on disk (see
  below); the `plugins` entry names that executable.

Config-level validation happens at startup, before anything loads: an
entry that is blank after trimming, or contains a NUL byte, fails config
parsing (`config "<path>": plugins[<index>] must not be blank / must not
contain a NUL byte`) and startup stops. A freshly materialized config file
contains `"plugins": []`; a nil or empty list disables plugins.

Per-entry failures at load time — resolution, spawning, the handshake, or
registration — are logged and **skipped**; later entries still load and
the built-in drivers (SQLite, MySQL, PostgreSQL, MongoDB) always start. A
broken plugin never blocks startup. There is no auto-discovery of any
kind: only allowlisted entries are ever spawned.

Plugins load in config order: resolve → dedupe → verify pin → spawn →
handshake (`perk/v1/initialize`) → register. A pinned executable whose
digest no longer matches is refused at the verify step — its child is
never spawned — and contributes one logged error naming the entry with
the expected and actual digests. A plugin rejected at handshake (wrong
protocol version) or registration (invalid capabilities, duplicate name,
target-prefix overlap) is terminated immediately and contributes one
logged error.

## Inspecting plugins from the CLI

`perk-workbench plugin` manages the configured plugin fleet without
starting the TUI. Every command is scriptable: `--json` emits one
machine-readable document on stdout, diagnostics for the invocation
itself go to stderr only when no JSON document can be produced, and
item-level failures are encoded in the document. Exit status: 0
success, 1 plugin or operational failure, 2 usage error.

- `plugin list [--json]` — reads the same config file and parser as
  startup and lists the configured entries in config order, resolving
  each with the exact startup resolution rules above **without spawning
  anything**. Each entry also reports its trust state: `unpinned`, or
  `pinned` with the configured fingerprint (`trust` /
  `expected_sha256` in JSON; `[unpinned]` / `[pinned sha256:…]` in human
  output — list does not hash, it reports the configured pin). An entry
  that cannot be resolved is reported per entry (its `error` field / an
  `invalid:` line) and makes the exit status 1; an empty config is
  successful and explicit (`[]` / `no plugins configured`).

  ```console
  $ perk-workbench plugin list
  perk-redis -> /home/alice/.local/bin/perk-redis [pinned sha256:0123…cdef]
  ./clickhouse-driver -> invalid: exec: "clickhouse-driver": executable file not found in $PATH
  ```

- `plugin inspect [--json] EXECUTABLE` — resolves the executable, runs
  it through the real loader lifecycle (spawn → `perk/v1/initialize`
  handshake → registration-invariant validation, without installing a
  global driver), closes it cleanly, and reports the advertised
  capabilities plus the final diagnostic snapshot: canonical path, init
  duration, exit/running state, and the bounded stderr tail. It works
  for executables not listed in config; a bare name resolves through
  PATH, a relative path against the working directory. The report also
  carries the fingerprint/trust state of the resolved canonical path:
  `trust: match (sha256 …)` when the configured pin matches the current
  bytes, `trust: unpinned` when no pin exists — and a **mismatch is
  refused before the child spawns** (`phase: "trust"` in JSON, `trust:
  FAILED` in human output, with the expected and actual digests), so a
  drifted pinned executable never executes.

- `plugin doctor [--json] [EXECUTABLE...]` — with no arguments checks
  every configured entry in order; with arguments checks exactly those
  executables. Each item runs the full
  resolve/verify-pin/initialize/register/shutdown lifecycle
  independently — with its own loader and validation-only registration,
  so duplicate identities or overlapping target prefixes across items
  never mutate or contaminate the global driver registry — and a
  failing item never stops later ones. An interrupt (Ctrl-C) stops the
  check between items; items already checked are still reported. The
  report marks the failing phase per item (`resolve`, `initialize`,
  `protocol`, `register`, `trust`, or `shutdown`) and carries the
  fingerprint/trust state (`unpinned`, `match`, `mismatch`); a pinned
  executable whose bytes drifted fails its item with the `trust` phase
  before the child spawns. The overall exit status is 1 when any item
  fails.

  ```console
  $ perk-workbench plugin doctor
  plugin perk-redis:
    path: /home/alice/.local/bin/perk-redis
    trust: match (sha256 0123…cdef)
    initialize: ok (12ms)
    capabilities: name=perk-redis display="Redis (plugin)" targets=[redis:] query_language=sql writes=none
    shutdown: ok
  ```

- `plugin add [--json] [--approve SHA256] EXECUTABLE` — the two-stage
  pin flow. Without `--approve` it resolves the executable (bare names
  through PATH, relative paths against the working directory), runs the
  full inspect lifecycle, and fingerprints the canonical bytes: the
  report shows the capabilities, the `sha256` fingerprint, `changed:
  false`, and `pending: true`, and the **config is never touched** — not
  even materialized when it does not exist. The output ends with
  `NOT ENABLED: rerun with --approve <fingerprint>`; copying that exact
  digest is the explicit approval. With `--approve <digest>` the
  resolve/inspect/hash is repeated from scratch and fails closed when
  the supplied digest does not exactly match the current bytes
  (`phase: "trust"`, `changed: false`, nothing written); only then is
  the plugin persisted atomically (same-directory temp + rename, mode
  0600): the entry is appended as its canonical absolute path (or
  replaces an existing entry resolving to the same file), the trust
  record is set, and every unrelated config key survives. An approve
  whose resulting state already matches reports `changed: false` and
  rewrites nothing. Exit status 1 on any resolve/inspect/hash/persist
  failure, 2 on usage errors.

  ```console
  $ perk-workbench plugin add perk-redis
  plugin perk-redis:
    path: /home/alice/.local/bin/perk-redis
    trust: unpinned
    initialize: ok (12ms)
    capabilities: name=perk-redis display="Redis (plugin)" targets=[redis:] query_language=sql writes=none
    shutdown: ok
    sha256: 0123…cdef
    NOT ENABLED: rerun with --approve 0123…cdef to pin and enable this plugin
  ```

- `plugin remove [--json] NAME_OR_EXECUTABLE` — atomically removes one
  configured plugin and its trust record. The operand matches a
  configured entry exactly (so an unresolvable entry can still be
  removed by name), or an executable that resolves to **exactly one**
  configured entry's canonical path; ambiguous matches — several
  configured entries resolving to the same executable — fail instead of
  removing multiple entries, as does an operand that matches nothing
  (`not configured`). The rewrite is atomic, preserves every unrelated
  key, and drops the trust record only when no remaining entry resolves
  to the removed canonical path. The JSON report carries `entry`, the
  canonical `path`, and `changed`; exit status 1 on failure.

  ```console
  $ perk-workbench plugin remove perk-redis
  removed plugin "perk-redis" (/home/alice/.local/bin/perk-redis)
  ```

- `plugin test [--json] EXECUTABLE` — resolves the executable and runs
  the perk/v1 conformance suite against it (see
  [Conformance testing](#conformance-testing-plugin-test)): each case
  runs in a fresh child and the child is terminated and reaped before
  the next case starts. Exit status 1 when any case fails; 2 on usage
  errors.

**Secret-safe diagnostics.** The plugin commands never exchange target
or form values, credentials, or statements with the plugin: the
lifecycle sends only the `perk/v1/initialize` handshake and then closes
the child — `build_target`, `open`, and every session RPC are never
invoked, so user-supplied values cannot appear in reports by
construction. Reports carry declarative data only (the advertised
capabilities, including the form *description* — never values) plus
process diagnostics (canonical path, pid/exit state, bounded stderr
tail). stderr is the plugin's own diagnostic stream, retained with the
same bounds as the TUI (newest 64 KiB / 100 lines) and shown only when
non-empty; stdout protocol frames are never reported. Resolution bases
differ by origin: config entries resolve relative to the config file's
directory (bare names through PATH), while explicit
`inspect`/`doctor`/`test`/`add` operands resolve against the working
directory (bare names through PATH).

## Managing plugins in the TUI

The workbench's command palette (`Ctrl+P`, or the `>_ Command` header
button) offers **plugins**, the same registration and trust flow without
leaving the terminal. The overlay is keyboard-driven like the other
pickers (`j`/`k` or arrows to move, `enter` to select, `esc` to cancel).

**Adding a plugin** walks two explicit stages, mirroring `plugin add`:

1. *Add plugin* — type the executable path and press `enter`. The
   manager asynchronously runs the same resolve, trust-check, inspect,
   and hash lifecycle as the CLI preview.
2. *Plugin preview* — before anything is enabled, the overlay shows the
   canonical path, the advertised identity/display, target prefixes,
   query language, write interfaces, and the lowercase SHA-256 of the
   exact bytes — and nothing else: no form values, credentials,
   statements, or plugin stderr ever appear. `enter` is the explicit
   confirmation: the manager re-resolves, re-inspects, and re-hashes
   with the previewed digest (bytes changed since the preview fail the
   enable closed), then saves the pin atomically, states **`plugin
   enabled; restart required`**, and closes. `esc` cancels without
   touching config.

**Removing a plugin** lists the configured entries; selecting one asks
for explicit confirmation (`Yes`/`No`) before the entry and its trust
record are removed atomically, with a **`plugin removed; restart
required`** status. Neither flow mutates the live driver registry — a
restart applies the change, so a pinned mismatch can never be enabled
or loaded around.

**Live status** — the menu's *Status* entry shows one line per
configured entry — the host-known plugin name (or the configured entry
before any successful handshake) and its state (`running`, `crashed`,
`rejected`, `stopped`, or `unresolved`) — with the selected entry's
compact details: the configured entry and canonical path, plugin
identity, perk protocol version, trust state and fingerprint,
pid/running/exit state, initialize duration, in-flight count, the last
terminal/structured failure, and the newest bounded stderr tail (the
loader retains at most 64 KiB / 100 lines per child, and the view caps
the rendered tail). The snapshot is taken once when the view opens;
`r` refreshes it explicitly — never automatically. Status reads never
spawn, mutate, or exchange protocol traffic, and never expose targets
or credentials: the failure text and every stderr line pass through the
credential redactor before they are shown, logged, or persisted.

**Restarting a plugin** — selecting *Restart* (on the selected entry)
asks for explicit confirmation (`Yes`/`No`). The loader re-verifies the
entry's configured pin immediately before the replacement spawns — a
drifted pin fails closed and nothing executes — then terminates and
reaps the old child, initializes and validates the replacement (same
perk protocol version and driver identity), and atomically swaps the
transport client used by future session opens, without touching the
global driver registration. Sessions opened before the restart keep
their old generation and fail deterministically; they never silently
jump to the replacement. If the restarted entry backs the current
connection, the dead service is closed first and the same target
reconnects through the normal open path, so schema, browse, and query
work again; otherwise only the child is restarted. A failed restart
leaves the previous state intact and records the failure in the status
view, where it stays inspectable.

**Stopped-plugin detection** — when an operation fails because a plugin
child exited or the perk/v1 protocol died (never for operation errors),
the status line shows *the plugin stopped; open Plugins → Status →
Restart to recover it*. The original structured error stays in the
query-log detail and diagnostics; the workbench never restarts children
or replays queries automatically.

## Conformance testing (`plugin test`)

`plugin test [--json] EXECUTABLE` runs the perk/v1 conformance suite
against one plugin executable, outside Go's unit-test harness, to
validate its transport and lifecycle behavior against the canonical
protocol assets (`protocol/perk-v1/schema.json`, `fixtures/`, and
`manifest.json`, embedded into the binary — never copied).

Guarantees:

- **Fixture-driven cases** read the manifest and send its named
  request/notification frames (derived frames keep their fixture
  origin), asserting the manifest-declared methods, error codes, and
  rejections: a valid initialize (exact protocol version `1` and
  capabilities passing the side-effect-free registration invariants),
  a parseable wrong `jsonrpc`, string and float request ids, an unknown
  method (`-32601`), and the id-less `perk/v1/cancel` notification
  (no response, child stays usable).
- **Generated transport cases**: a request before initialize gets a
  JSON-RPC error and the child remains usable; two requests in one
  write get correct ids and responses in processing order; an unknown
  cancel id never kills the child; malformed JSON, non-object JSON,
  invalid UTF-8, EOF input, and frames over 16 MiB are terminal with no
  protocol response; an exact 16 MiB frame (newline included) is
  accepted and answered; a clean EOF shutdown exits 0 within the case
  bound.
- **Raw NDJSON-RPC, strictly parsed.** The runner speaks directly to
  the child's stdio — frames never pass through the production client,
  so deliberately invalid frames can be sent. Every response must be
  valid UTF-8, a single JSON object in one LF-terminated frame of at
  most 16 MiB including the newline, `jsonrpc: "2.0"`, with exactly one
  `result` or `error`, an unsigned numeric id answering an outstanding
  request exactly once, and no stdout noise; any violation terminates
  the child and fails the case.
- **Isolation and cleanup.** Every case spawns a fresh child; stderr is
  drained concurrently with the same bounded intent as the host (newest
  64 KiB / 100 lines), so a chatty child can never deadlock or grow
  diagnostics without limit. Each case and its shutdown are time-bounded
  (30 seconds per case; a child that ignores EOF is killed after a 5
  second grace), every child is reaped before the next case starts, and
  independent failures never stop later cases.
- **No backend required.** The suite never invokes `build_target`,
  `open`, or any session RPC, so a transport-only plugin such as
  perk-redis can be tested without a Redis server.
- **Drift fails coherently.** The manifest, every named fixture, and
  the schema are loaded and cross-checked when the command starts; a
  missing or renamed fixture, a manifest entry whose method or expected
  error code is missing or disagrees with its frame, or a `$ref` that
  no longer resolves in the schema fails the whole run with a coherent
  error instead of misbehaving per case.

Limits:

- The suite verifies transport and handshake behavior, not backend or
  session semantics. Because no session handler is ever in flight, the
  canceled-code (`-32800`) response is not verified; cancellation is
  exercised at the transport level only (id-less cancel notifications
  produce no response, and unknown cancel ids never kill the child).
- The schema is a contract reference, not a validator: the runner
  checks the manifest's `$ref`s against it, but frames are judged by
  the strict parsing rules above, exactly as the production host does.
- Deliberately invalid input frames (malformed JSON, non-object JSON,
  invalid UTF-8, oversized frames) are constructed by the runner —
  they cannot exist as canonical fixtures.
- `--json` emits exactly one self-contained release evidence document
  on stdout, pass or fail, and nothing else; diagnostics for the
  invocation itself go to stderr only when no document can be
  produced. Raw protocol frames and request data are never reported.
  The document is validated by
  `protocol/perk-v1/plugin-test-evidence.schema.json` (version 1) and
  carries the stable evidence fields:
  - `evidence_schema` / `evidence_version` — the evidence document
    shape (`perk/v1/plugin-test-evidence.schema.json`, version 1).
  - `protocol_version` — the perk protocol version the host speaks
    (exactly `1` today).
  - `host_version` — the host build identity (`perk-workbench
    <version>`; `<version>` is injected at build time via
    `-ldflags "-X main.version=<version>"` and honestly reports
    `devel` when nothing was injected).
  - `contract_sha256` — SHA-256 of the canonical contract asset set
    (schema + manifest + every fixture, in sorted name order with
    explicit length framing); any asset drift changes the digest.
  - `entry`, `path`, `executable_sha256` — the input entry, the
    resolved canonical executable path, and the SHA-256 of its bytes
    (the fingerprint a release archive's binary must match).
  - `capabilities` — the plugin's advertised identity (name/display)
    captured from the initialize handshake, no backend required; the
    v1 protocol advertises no implementation version, so none is
    reported.
  - `error`, `cases`, `passed`, `failed`, `ok` — any suite-level
    failure and the per-case results with the final counts.
  The document is deterministic except per-case durations and the
  path; it never carries a timestamp, credentials, connection targets,
  form values, statements, or protocol frames. Each case result
  carries a stable name, pass/fail, duration, failure category
  (`spawn`, `protocol`, `behavior`, `timeout`, `shutdown`) with
  message, and the bounded sanitized stderr tail. Human output prints
  the same protocol/host/contract/executable summary before the case
  lines.

Examples:

```console
$ perk-workbench plugin test --json ./perk-redis
{
  "evidence_schema": "perk/v1/plugin-test-evidence.schema.json",
  "evidence_version": 1,
  "protocol_version": 1,
  "host_version": "perk-workbench 0.1.0",
  "contract_sha256": "9f2c…64 hex chars…",
  "entry": "./perk-redis",
  "path": "/home/alice/.local/bin/perk-redis",
  "executable_sha256": "7ab1…64 hex chars…",
  "capabilities": {"name": "redis", "display": "Redis (Rust plugin)"},
  "cases": [
    {"name": "initialize", "ok": true, "duration": 12345678, "stderr": []},
    {"name": "wrong_jsonrpc", "ok": true, "duration": 2234567, "stderr": []},
    ...
  ],
  "passed": 16,
  "failed": 0,
  "ok": true
}
$ perk-workbench plugin test ./perk-redis
plugin test ./perk-redis:
  protocol: perk/v1
  host: perk-workbench 0.1.0
  contract: sha256:9f2c…64 hex chars…
  path: /home/alice/.local/bin/perk-redis
  executable: sha256:7ab1…64 hex chars…
  capabilities: redis (Redis (Rust plugin))
  initialize               PASS  12ms
  wrong_jsonrpc            PASS   2ms
  ...
  16 passed, 0 failed
```

## Writing a plugin with the Node SDK

The SDK (`perk-workbench-plugin-sdk`, CommonJS, dependency-free,
Node >= 18) implements the plugin side of the protocol: framing, the
handshake, session routing, parameter validation, cancellation, and the
JSON-RPC error mapping.

```bash
npm install perk-workbench-plugin-sdk
```

A plugin package declares its executable with a `bin` entry:

```json
{
  "name": "perk-demo-kv",
  "version": "1.0.0",
  "bin": { "perk-demo": "./perk-demo.js" },
  "dependencies": { "perk-workbench-plugin-sdk": "^0.0.0" }
}
```

Usage options:

- **Global install** — `npm install -g perk-demo-kv` links
  `perk-demo` into the npm global bin directory, which is normally on
  `PATH`; configure `"plugins": ["perk-demo"]`.
- **Project-local** — `npm install` in a project places the executable
  at `node_modules/.bin/perk-demo`; configure the explicit path
  `"plugins": ["/home/alice/projects/perk-demo-kv/node_modules/.bin/perk-demo"]`
  (or a path relative to the config file's directory).

The rest of this document defines what the SDK implements; a complete,
executable plugin is shown in [Example plugin](#example-plugin).

Public SDK type names, by area (all declared in `index.d.ts`; wire names
below are the JSON field names):

| Area | SDK types |
|---|---|
| Capabilities & forms | `Capabilities`, `TargetPattern`, `FormSpec`, `FormField`, `FormOption`, `FormFieldKind`, `FormValidation`, `FormValues`, `QueryLanguage`, `WriteCapabilities`, `DocumentWriteCapability`, `DocumentFormat` |
| Shared DTOs | `DatabaseInfo`, `SchemaObject`, `Result`, `ColumnInfo`, `ColumnDef`, `ColumnChange`, `IndexInfo`, `IndexChange`, `ForeignKeyInfo`, `ReferencingForeignKeyInfo`, `ForeignKeyChange`, `BrowseFilter`, `BrowseSort`, `BrowseOptions`, `DocumentPayload` |
| Row & document writes | `Value`, `ValueKind`, `NamedValue`, `RowValue`, `RowWriteOperation`, `RowWriteRequest`, `RowWriteResponse`, `DocumentWriteOperation`, `DocumentWriteRequest`, `DocumentWriteResponse` |
| Handler contracts | `HandlerContext`, `StatementRequest`, `TableRequest`, `IndexChangeRequest`, `ReplaceIndexRequest`, `DropRequest`, `ForeignKeyChangeRequest`, `ReplaceForeignKeyRequest`, `ColumnChangeRequest`, `AddColumnRequest`, `BrowseTableRequest`, `EmptyRequest`, `BuildTargetResult`, `OpenResult`, `SessionService` |
| Entry points | `PluginDefinition`, `PluginServer`, `PluginServerOptions`, `createPluginServer` |
| Errors | `ErrorKind`, `PluginOperationError`, `RequestCancelledError` |

Handlers receive `(request, context)` where `request` is the wire params
minus `session_id` (the `*Request` types above) and `context.signal` is
the `AbortSignal` for cancellation. `PluginServer` exposes `closed` (a
promise resolving at termination) and `close()` (idempotent).

## Wire protocol

**JSON-RPC 2.0 over newline-delimited UTF-8 JSON** on stdin/stdout:

- Every frame is exactly one JSON object followed by a single newline
  (`0x0A`). No pretty printing, no extra whitespace.
- The host writes requests and notifications to the plugin's **stdin**;
  the plugin writes responses to its **stdout**. stdout carries protocol
  frames **only** — the host parses every stdout line as a frame.
- **stderr is for diagnostics.** The host drains the plugin's stderr so a
  verbose plugin can never block on a full pipe, and retains only the
  newest bounded tail — at most 64 KiB and 100 logical lines per plugin —
  for later inspection. Treat stderr as retained, inspectable output:
  never write protocol frames, connection targets, form values,
  credentials, or statements to it.
- Request IDs are **unsigned numeric** JSON integers assigned by the host,
  starting at 1 and increasing per child. Responses must echo the request
  id. `perk/v1/cancel` is a JSON-RPC notification: it has no `id` member.
- One frame must fit within **16 MiB** (16 × 2²⁰ bytes) including its
  newline. A frame that does not fit is oversized.

### Terminal behavior

Both sides treat protocol violations as terminal: the connection is
broken, in-flight operations fail, and the child is shut down.

Host side — a plugin response frame that is invalid UTF-8, is not
parseable JSON, carries `"jsonrpc"` other than `"2.0"`, is a response for
an **unknown request id**, is a **duplicate response** for a request id
that already answered, or is oversized terminates the child. Clean EOF on
the child's stdout (the plugin exited) also fails all pending calls, but
does not kill anything further. A stuck child is reaped with a kill after
a bounded wait (see [Shutdown](#shutdown)).

SDK side — input that is invalid UTF-8, unparseable, not a JSON object, or
oversized terminates the server: every in-flight request is aborted, no
further frames are processed or written, and the `closed` promise
resolves. Input EOF terminates the server the same way. Malformed
*requests* that still parse — wrong `jsonrpc`, non-integer id, requests
before initialization, unknown methods, bad params — are answered with a
JSON-RPC error, not treated as terminal.

## Handshake

The host calls `perk/v1/initialize` as the very first request. A plugin
whose initialize result carries a different `protocol_version` is
**rejected before registration**: the child is terminated, the entry is
logged and skipped. The version number in `protocol_version` is the
perk/v1 wire version (currently `1`); `workbench_version` is
informational — plugins may use it to adapt to host feature level, but
must not gate on its exact value.

Request (host → plugin):

```json
{"jsonrpc":"2.0","id":1,"method":"perk/v1/initialize","params":{"protocol_version":1,"workbench_version":"perk-workbench 0.1.0"}}
```

Response (plugin → host):

```json
{"jsonrpc":"2.0","id":1,"result":{"protocol_version":1,"capabilities":{"name":"perk-redis","display":"Redis (plugin)","targets":[{"prefix":"redis:"}],"form":{"prefix":"redis:","fields":[{"key":"host","title":"Host","kind":0,"validate":0,"placeholder":"localhost"},{"key":"port","title":"Port","kind":0,"default":"6379","validate":2,"error":"port must be between 1 and 65535"}]},"write_capabilities":{"row_writer":false}}}}
```

`capabilities` is the plugin's driver advertisement:

| Field | Type | Notes |
|---|---|---|
| `name` | string | Unique driver name; stored in profiles. Collides with a built-in or another plugin → registration rejected. |
| `display` | string | Human-readable driver label shown in the UI. |
| `targets` | `TargetPattern[]` | Optional; omitted for target-only drivers. A plugin must declare at least one target form (only the built-in SQLite fallback may have none). |
| `form` | `FormSpec` or null | Optional; omitted for target-only drivers (MongoDB-style, opened by target URL only). |
| `query_language` | `QueryLanguage` or null | Optional; how the query editor presents this driver's statements. Omitted, null, or all-empty advertisements are normalized by the host to the legacy SQL default, so plugins written before this field existed keep working unchanged; a present-but-invalid advertisement rejects registration. |
| `write_capabilities` | `WriteCapabilities` | Always present. Gates the optional `row_write`/`document_write` RPCs. |

`protocol_version` in the result must equal the requested version (1) or
the plugin is rejected before registration.

## Methods

Method names are `perk/v1/<name>`. The host never calls a session method
before `perk/v1/open` has answered, and it sends at most one `perk/v1/close`
per session. Every session method takes `session_id` in its params; the
SDK strips it before dispatching to the session service (the handler
request shapes below are the wire params **minus** `session_id`).

### Lifecycle

| Method | Params | Result | Notes |
|---|---|---|---|
| `perk/v1/initialize` | `{protocol_version: 1, workbench_version: string}` | `{protocol_version: 1, capabilities: Capabilities}` | Handshake. Version mismatch → rejected before registration. |
| `perk/v1/build_target` | `FormValues` | `{target: string, ok: boolean}` | Serializes one connection form into the opener target body. Host-bound by a 5-second timeout (this interface has no context). Only invoked for plugins that advertise a `form`. |
| `perk/v1/open` | `{target: string}` | `{session_id: number, info: DatabaseInfo}` | Opens one session. The target is the connection target with a stripped label prefix (or a whole URL-scheme target). `info` is cached by the host and never re-fetched. |
| `perk/v1/close` | `{session_id: number}` | `null` | Closes one session. Host-bound by a 5-second timeout; sent at most once per session (idempotent on the host side). |
| `perk/v1/cancel` | `{id: number}` (notification) | — | Asks the plugin to cancel the in-flight request with the **original request id**. No `id` member on the envelope; no response. |

`FormValues` (the `build_target` params) is the driver-facing view of the
connection form:

| Field | Type | Notes |
|---|---|---|
| `host` | string | Optional. |
| `port` | string | Optional. |
| `user` | string | Optional. |
| `pass` | string | Optional; secrets already resolved. |
| `database` | string | Optional. |
| `tls` | string | Optional; selected TLS mode. |
| `extras` | map[string]string | Optional; driver-specific fields not in the fixed key set. |

Connection-form field keys from the fixed set (`host`, `port`,
`username`, `password`, `database`, `target`, `tls`) bind to typed form
state; any other key lands in `extras`.

`build_target` must produce a target the host routes back to this driver:
a string starting with the form's declared `prefix` (a label prefix is
then stripped again before `open`). `ok: false` (or an error response)
rejects the connection attempt; the UI then falls back to the raw target
field like a target-only driver.

### Mandatory session RPCs (20)

The SDK requires every one of these handlers on every opened session.

| Method | Params | Result | Handler request (params minus `session_id`) |
|---|---|---|---|
| `perk/v1/execute` | `{session_id, statement}` | `Result` | `{statement}` |
| `perk/v1/execute_read_only` | `{session_id, statement}` | `Result` | `{statement}` |
| `perk/v1/validate` | `{session_id, statement}` | `null` | `{statement}` |
| `perk/v1/list_schema` | `{session_id}` | `SchemaObject[]` | `{}` |
| `perk/v1/table_info` | `{session_id, table}` | `ColumnInfo[]` | `{table}` |
| `perk/v1/list_indexes` | `{session_id, table}` | `IndexInfo[]` | `{table}` |
| `perk/v1/create_index` | `{session_id, table, change}` | `null` | `{table, change: IndexChange}` |
| `perk/v1/replace_index` | `{session_id, table, old_name, change}` | `null` | `{table, old_name, change: IndexChange}` |
| `perk/v1/drop_index` | `{session_id, table, name}` | `null` | `{table, name}` |
| `perk/v1/list_foreign_keys` | `{session_id, table}` | `ForeignKeyInfo[]` | `{table}` |
| `perk/v1/list_referencing_foreign_keys` | `{session_id, table}` | `ReferencingForeignKeyInfo[]` | `{table}` |
| `perk/v1/list_foreign_keys_all` | `{session_id}` | `map[string]ForeignKeyInfo[]` | `{}` |
| `perk/v1/list_indexes_all` | `{session_id}` | `map[string]IndexInfo[]` | `{}` |
| `perk/v1/create_foreign_key` | `{session_id, table, change}` | `null` | `{table, change: ForeignKeyChange}` |
| `perk/v1/replace_foreign_key` | `{session_id, table, old_name, change}` | `null` | `{table, old_name, change: ForeignKeyChange}` |
| `perk/v1/drop_foreign_key` | `{session_id, table, name}` | `null` | `{table, name}` |
| `perk/v1/alter_column` | `{session_id, table, change}` | `null` | `{table, change: ColumnChange}` |
| `perk/v1/drop_column` | `{session_id, table, name}` | `null` | `{table, name}` |
| `perk/v1/add_column` | `{session_id, table, def}` | `null` | `{table, def: ColumnDef}` |
| `perk/v1/browse_table` | `{session_id, table, options}` | `Result` | `{table, options: BrowseOptions}` |

Notes:

- `list_foreign_keys_all` and `list_indexes_all` return the whole schema
  keyed by the table (collection) that declares the entry. Stores without
  foreign keys return an empty object.
- `null` results: the host decodes them as empty; handlers that return
  nothing (void) are answered with `"result": null`.
- All session operations forward the workbench's context unchanged: the
  caller's cancellation reaches the plugin as a `perk/v1/cancel`
  notification (see [Cancellation](#cancellation)).

### Optional write RPCs

Advertised by `write_capabilities`; the SDK enforces the match in both
directions when a session opens (handler required when advertised,
**rejected** when not).

| Method | Params | Result | Handler request (params minus `session_id`) |
|---|---|---|---|
| `perk/v1/row_write` | `{session_id, request}` | `RowWriteResponse` | `request: RowWriteRequest` |
| `perk/v1/document_write` | `{session_id, request}` | `DocumentWriteResponse` | `request: DocumentWriteRequest` |

## Data types

### Capabilities and forms

**`TargetPattern`** — `{prefix: string, keep_target?: boolean}`. A
pattern addresses targets beginning with `prefix`. Label prefixes end in
`:` (`"mysql:"`) and are **stripped** from the target before `open`;
scheme prefixes are full URL schemes (`"postgres://"`) passed to `open`
**whole** (`keep_target: true`). A scheme pattern must be declared before
a label pattern that would otherwise shadow it.

**`FormSpec`** — `{fields: FormField[], prefix?: string}`. `prefix` is
prepended to the serialized target so the host routes it back to this
driver; when set it must be one of the driver's own stripped target
prefixes.

**`FormField`** — `{key, title, kind, placeholder?, default?, options?,
validate, error?}`:

| Field | Type | Notes |
|---|---|---|
| `key` | string | Binds to typed form state for the fixed set; otherwise an `extras` key. |
| `title` | string | Field label. |
| `kind` | number | 0 input, 1 password, 2 select. |
| `placeholder` | string | Optional. |
| `default` | string | Optional; well-known value shown when blank (e.g. the default port). |
| `options` | `FormOption[]` | Optional; required for select fields. `{label: string, value: string}`. |
| `validate` | number | 0 none, 1 required, 2 port. |
| `error` | string | Optional; message shown when validation fails. |

**`WriteCapabilities`** — `{row_writer: boolean, document?: DocumentWriteCapability | null}`.
`document` is omitted (or null) when the driver has no document support.
**`DocumentWriteCapability`** — `{format: string, text: boolean}`:
`format` is the only payload format the driver accepts, `text` reports
whether whole-document text editing is safe. The single defined format:
`"application/vnd.perk.mongodb.extjson+json;version=2;mode=relaxed"`.

**`QueryLanguage`** — `{name: string, editor_label: string, placeholder:
string, lexer?: string, examples?: string[], commands?: QueryCommand[]}`.
Advertises how the query editor presents this driver's statements:
`name` is the language name, `editor_label` the editor tab label,
`placeholder` the input placeholder, `lexer` an optional lexer hint the
UI falls back from when blank or unknown (`"sql"`, `"javascript"`, …),
`examples` optional statements the driver's parser already accepts, and
`commands` an optional static catalog the query editor completes from
(`Ctrl+Space` in the Command tab; the editor inserts the chosen command
name — it never executes it, and the host performs no dynamic schema
calls). `name`, `editor_label`, and `placeholder` must be nonblank after
trimming; every `examples` entry must be nonblank. Each `commands`
entry is `{name: string, usage: string, summary: string}`: `name` is
the canonical command name, `usage` a backend-native usage line, and
`summary` a concise description — all three required, nonblank after
trimming, and free of control characters; `name` must be ASCII letters,
digits, or underscores (the exact charset the editor tokenizes, so every
advertised name is completable — and lowercase folding is then a total
case-insensitive equality); `name` ≤ 64 runes, `usage` ≤ 256 runes,
`summary` ≤ 256 runes. Command names must be unique case-insensitively
and the list is capped at 512 entries, so a plugin can never force an
unbounded completion list or handshake frame.
Empty/absent `commands` preserves the pre-catalog behavior exactly. The
host normalizes an omitted, null, or all-empty `query_language` to the
legacy SQL default — `name: "SQL"`, `editor_label: "SQL"`,
`placeholder: "Enter a query…"`, `lexer: "sql"` — before registration;
a present-but-invalid advertisement is **rejected** at registration,
never silently defaulted. The protocol version is unchanged, so
existing plugins (which omit the field) load exactly as before.

### Shared service DTOs

**`DatabaseInfo`** — `{product: string, version: string}`. Product names
are display-only; the workbench does not branch driver behavior on them.

**`SchemaObject`** — `{database: string, type: string, name: string,
row_count?: number | null}`. `row_count` is an estimate where the engine
exposes one; `null` (or absent) when unknown. Plugins may return flat
objects — every database's tables and views without a database root. The
host synthesizes one missing `type: 'database'` root per distinct
non-empty `database` for its internal rendering, prepended in first-seen
database order; plugins need not emit UI-only roots.

**`Result`** — the execution and browse result:

| Field | Type | Notes |
|---|---|---|
| `columns` | `string[]` | Column names. |
| `column_types` | `string[]` | Driver-provided type display strings, parallel to `columns`. |
| `rows` | `(string \| null)[][]` | Display cells; `null` is SQL NULL. Host caps at 500 rows. |
| `untruncated_rows` | `(string \| null)[][]` | Full cell values, parallel to `rows`, for the cell viewer. |
| `rows_affected` | number | Integer; rows written/affected. |
| `has_more` | boolean | More rows exist beyond the returned page. |
| `duration_ns` | number | Execution time, **integer nanoseconds**. |
| `truncated` | boolean | True when `rows` was cut at the row cap. |
| `document_ids` | `DocumentPayload[]` | Optional; one stable document identity per row, parallel to `rows`. `null` (or absent) when the backend is not document-capable or a row has no identity. |
| `statement` | string | Optional; the backend-native statement for the operation that produced this result. Row/document writes return it so the host logs the exact command (e.g. `RENAME key user:2 user:3`) instead of the generic UI preview; empty (or absent) keeps the preview. The host never executes this text itself. Replayability and sensitivity are described by `statement_metadata`, which defaults to replayable/not sensitive when absent. |
| `statement_metadata` | `StatementMetadata` | Optional; structured metadata for `statement`. Meaningful only when `statement` is nonblank — a result carrying it without a nonblank statement is rejected as a result-shape violation (an operation error, never terminal). Omitted (or `null`) keeps the legacy semantics: replayable, not sensitive, no language. |

Display conventions (follow the built-in drivers): at most 500 rows with
`truncated`/`has_more` signaling, display cells sanitized and capped at
300 runes with an ellipsis suffix, full values preserved in
`untruncated_rows`.

**`StatementMetadata`** — `{language: string, replayable: boolean,
sensitive: boolean}`. Optional structured metadata for a backend-native
`statement`. It is meaningful only when the statement is nonblank, and
the object is authoritative: all three scalar fields are present when
the object is present. Plugins omit the field when there is no metadata;
the host also accepts `null` as equivalent. Omitted metadata keeps the
legacy defaults — `replayable: true`, `sensitive: false`,
`language: ""` — so plugins written before this field existed keep
working unchanged.

- `language` is the backend statement language (e.g. `"redis"`); the host
  carries it on query-log entries (later UI work may derive the active
  driver language from it).
- `replayable` describes whether the user may copy, re-run, or explain
  the statement. `false` makes those actions no-ops behind a
  "not replayable" status; the entry still renders.
- `sensitive` marks a statement that must never be stored verbatim. The
  host never persists the text: the query log stores a stable redacted
  marker and forces the entry non-replayable, in memory and at rest.

**`ColumnInfo`** — `{name, type, attributes, nullable: boolean,
default_value: string | null, primary_key: number, indexes: IndexKind[]}`.
`primary_key` is the column's position in the primary key (0 = not part
of it). `attributes` is a display string (e.g. auto-increment, collation
notes).

**`ColumnDef`** — `{name, type, nullable: boolean, default_value: string | null, attributes: string | null}`.

**`ColumnChange`** — `{previous_name, name, type, nullable: boolean, default_value: string | null, attributes: string | null}`. Renames when `previous_name != name`.

**`IndexInfo` / `IndexChange`** — `{name: string, unique: boolean,
primary_key: boolean, columns: string[]}` (identical shape; `change`
additionally carries the new name).

**`ForeignKeyInfo`** — `{id: string, columns: string[],
reference_table: string, reference_columns: string[], on_delete: string,
on_update: string}`. Actions are the standard SQL strings (`NO ACTION`,
`RESTRICT`, `SET NULL`, `SET DEFAULT`, `CASCADE`), driver-normalized.

**`ReferencingForeignKeyInfo`** — `ForeignKeyInfo` plus `table: string`,
the table that declares the foreign key.

**`ForeignKeyChange`** — `{columns: string[], reference_table: string,
reference_columns: string[], on_delete: string, on_update: string}`.
Column counts must match between `columns` and `reference_columns`.

**`BrowseOptions`** — `{columns?: string[], filters?: BrowseFilter[],
sorts?: BrowseSort[], offset?: number, limit?: number}`. `offset`/`limit`
default to 0 / unbounded.

**`BrowseFilter`** — `{column: string, operator: string, value: string}`.
Operator values: `""` (none), `LIKE`, `NOT LIKE`, `PATTERN`,
`NOT PATTERN`, `=`, `!=`, `<`, `<=`, `>`, `>=`, `IS NULL`, `IS NOT NULL`.

**`BrowseSort`** — `{column: string, descending: boolean}`.

### Row and document writes

**`RowValue`** — `{name: string, value: Value}`. One column of a row
write; ordering is the caller's and must be preserved when building
parameter lists. `key` carries the row identity for update/delete;
`values` carries the insert/update payload.

**`Value`** — a tagged cell payload: `{kind, string?, bool?, integer?,
float?, bytes?, decimal?, timestamp?, array?, object?}`. Exactly the
payload matching `kind` is meaningful; the others are omitted.

| `kind` | Payload field | Notes |
|---|---|---|
| `"default"` | — | Use the column default; omit the column. |
| `"null"` | — | Explicit SQL NULL. |
| `"string"` | `string` | |
| `"bool"` | `bool` | |
| `"integer"` | `integer` | JSON number. |
| `"float"` | `float` | JSON number. |
| `"bytes"` | `bytes` | **base64** JSON string. |
| `"decimal"` | `decimal` | Exact decimal text. |
| `"timestamp"` | `timestamp` | RFC 3339 string. |
| `"array"` | `array` | `Value[]`. |
| `"object"` | `object` | `NamedValue[]` (`{name, value}`). |

**`RowWriteRequest`** — `{operation: "insert" | "update" | "delete",
table: string, key?: RowValue[], values?: RowValue[]}`.

**`RowWriteResponse`** — `{result: {rows_affected: number, statement?:
string, statement_metadata?: StatementMetadata | null}}`.
`statement` is the optional backend-native command the driver executed
for the write; the host logs it in place of the generic UI preview when
non-blank and omits it from the wire when empty. `statement_metadata`
follows the `Result` convention: meaningful only with a nonblank
`statement`, omitted (or `null`) keeps the legacy defaults.

**`DocumentPayload`** — `{format: string, data: string}`. `data` is the
document bytes as a **base64** JSON string; `format` is the driver's
declared format.

**`DocumentWriteRequest`** — `{operation: "read" | "insert" | "replace" |
"delete", collection: string, id?: DocumentPayload | null,
document?: DocumentPayload | null}`. `id` carries the document identity
for read/replace/delete; `document` the body for insert/replace.

**`DocumentWriteResponse`** — `{result: {rows_affected: number,
statement?: string, statement_metadata?: StatementMetadata | null},
document?: DocumentPayload | null}`. `document` is set for read
operations; a read that returns no document is an error.
`statement` is the optional backend-native command the driver executed
for the write (same convention as `RowWriteResponse`), with
`statement_metadata` following the `Result` convention too.

### Nullability and encoding conventions

- **Nil cells and nil pointers serialize as `null`**: `Result.rows` /
  `untruncated_rows` cells (SQL NULL), `default_value`, `attributes`,
  `SchemaObject.row_count`, `DocumentWriteRequest.id/document`,
  `DocumentWriteResponse.document`, `WriteCapabilities.document`.
- **Optional fields are omitted, not nulled**, when empty: `targets`,
  `form`, `keep_target` (false), `extras`, `document` (null), payload
  fields on `Value`, `key`/`values` on `RowWriteRequest`, `document_ids`
  (null when not document-capable), `statement` (empty on `Result` and
  write results), `statement_metadata` (omitted when there is no
  metadata).
- **Bytes are base64 JSON strings**: `DocumentPayload.data`,
  `Value.bytes`.
- **`duration_ns` is an integer** — nanoseconds, not a float or string.

### Enums

| Enum | Values |
|---|---|
| Form field kind | `0` input, `1` password, `2` select |
| Form validation | `0` none, `1` required, `2` port (blank or 1–65535) |
| Index kind | `1` primary key, `2` unique, `3` regular |
| Value kind | `"default"`, `"null"`, `"string"`, `"bool"`, `"integer"`, `"float"`, `"bytes"`, `"decimal"`, `"timestamp"`, `"array"`, `"object"` |
| Row write operation | `"insert"`, `"update"`, `"delete"` |
| Document write operation | `"read"`, `"insert"`, `"replace"`, `"delete"` |
| Document format | `"application/vnd.perk.mongodb.extjson+json;version=2;mode=relaxed"` |

## Errors

JSON-RPC 2.0 error codes used by the protocol, plus the perk/v1
cancellation code:

| Code | Meaning |
|---|---|
| `-32600` | Invalid request (wrong `jsonrpc`, non-integer id, request before initialize, unsupported `protocol_version`, double initialize). |
| `-32601` | Method not found. |
| `-32602` | Invalid params (non-object params, non-string `statement`/`table`/`name`, unknown `session_id`, missing `session_id`). |
| `-32603` | Internal error: a handler threw (unless the error carries an integer `code`, which is used instead). |
| `-32800` | **Canceled** (perk/v1). The host maps it exactly to `context.Canceled`. |

Error responses have the JSON-RPC shape
`{"jsonrpc":"2.0","id":<id>,"error":{"code":<number>,"message":<string>,"data":<object>}}`,
where `data` is optional and carries provenance plus advisory guidance
only, never control information. Exactly one of `result` or `error` is
set per response.

**Structured provenance.** An error's `data` object may carry five
string fields:

| Field | Meaning |
|---|---|
| `kind` | Stable failure kind (see below). Unknown or blank kinds are treated as `operation`; malformed non-object `data` is ignored entirely. |
| `plugin` | Advisory plugin identity. The host never trusts it: the identity it retains from a successful `perk/v1/initialize` handshake is authoritative and overrides this field; before that handshake it is empty. |
| `method` | Advisory method name. The host always uses the actual request method instead — the method renders exactly once, never duplicated. |
| `hint` | Optional advisory guidance explaining the failure. Non-control: the host renders it separately from the error (labeled `Hint:`) and never acts on it. Empty strings are omitted from the wire. |
| `suggested_statement` | Optional advisory statement the user may try instead of the failed one (labeled `Try:`). Non-control: the host never executes it, never merges it into the error identity or message, and never uses it for matching or diagnostics. Empty strings are omitted from the wire. |

The `hint` and `suggested_statement` fields are the protocol's only
*actionable* guidance: they surface in the workbench query-log detail
view next to the raw backend error, so a failed statement like `GET
user:1` against a hash can report `Hint: GET accepts strings, but
user:1 is a hash` and `Try: HGETALL user:1`. A plugin should derive them
from the actual failure (e.g. inspecting the key type after a Redis
`WRONGTYPE` reply) and must never fabricate a suggestion that changes
the operation's meaning; the workbench never executes suggestions, and
sensitive statements drop both fields with the error text.

Stable `kind` values (mirrored as the `ErrorKind` constants in the Node
SDK and the `plugin.Kind` enum in the Go host):

| Kind | Meaning |
|---|---|
| `validation` | The operation was rejected as invalid. |
| `authentication` | Credentials were missing or rejected. |
| `connection` | The backend connection failed or dropped. |
| `operation` | Generic operation failure (the default). |
| `unsupported` | The backend does not support the operation. |
| `cancelled` | The operation was canceled. |
| `protocol` | Protocol-level failure inside the plugin. |
| `plugin_crash` | The plugin's own runtime crashed or was killed. |

**Operation-error behavior:** any error other than `-32800` is an
*operation error* naming the method — the workbench surfaces
`perk/v1/<method>: <message> (code <code>)` (the method rendered exactly
once) as a failed operation (query log, status line), and the plugin
child keeps running. On the Go host the error is a `plugin.Error`
(inspect with `errors.As`): `Code`, `Message`, the normalized `Kind`,
the host-known `Plugin` (empty before initialize), the host `Method`,
and the optional advisory `Hint` and `SuggestedStatement` carried
verbatim from the error data — the workbench renders the advisory fields
separately, labeled, in the query-log detail view and never appends them
to the error text. Protocol violations are the only terminal failures. A
result that does not decode into the expected DTO shape is also an
operation error, never terminal.

**SDK-side mapping.** The Node SDK's `PluginServer` maps handler errors
onto this envelope:

- A thrown `PluginOperationError(message, {code, kind, plugin, method,
  hint, suggested_statement})` replies with its integer `code` (default
  `-32000`) and `message` plus `data` carrying `kind`/`plugin`/`method`
  (an omitted `method` is filled with the wire method; an unknown or
  blank `kind` normalizes to `operation`). Non-blank `hint` and
  `suggested_statement` strings serialize into `data` as advisory
  guidance; blank values are omitted.
- A thrown `RequestCancelledError` replies `-32800` with
  `data: {"kind": "cancelled"}` — the cancellation code is unchanged.
- Any other thrown error keeps the legacy behavior: `-32603` (or the
  error's own integer `code` when present) with no `data`.

## Sessions

- **One session per `perk/v1/open`.** Each open returns a fresh
  `session_id` (the SDK assigns 1, 2, 3, …); the host tracks every open
  session with the loader and routes all subsequent session RPCs by id.
  The workbench opens at most one session per plugin at a time; the
  protocol itself allows more.
- **`info` is cached from the open result** and never re-fetched: the
  host answers `Service.Info()` from memory after `open`.
- **Write capabilities are cached from the initialize handshake** and
  gate the optional RPCs: a session is wrapped so the workbench sees
  `RowWriter`/`DocumentWriter` behavior exactly when
  `write_capabilities.row_writer`/`document` advertised it.
- **Close idempotence:** the host sends `perk/v1/close` at most once per
  session (later closes are no-ops); the SDK removes the session before
  invoking its `close()` hook, so a throwing `close()` can never
  resurrect the session. A second close RPC answers `-32602` (unknown
  `session_id`).

## Cancellation

- The host cancels an operation by aborting the caller's context, which
  sends a `perk/v1/cancel` **notification carrying the original request
  id**: `{"jsonrpc":"2.0","method":"perk/v1/cancel","params":{"id":<id>}}`.
  No cancel is sent for a request that never reached the wire.
- The SDK maps a cancel notification to the matching in-flight request's
  **`AbortSignal`** (the `context.signal` passed to every handler);
  unknown cancel ids are ignored.
- A handler observes cancellation by `context.signal.aborted` or by
  `await`-ing something that rejects on abort. It should then throw
  `RequestCancelledError` (code `-32800`); the SDK also answers `-32800`
  if the handler finishes (successfully or not) after the signal fired.
  Both paths reply `-32800` with `data: {"kind": "cancelled"}`.
- The host discards a response that arrives after cancellation, and maps
  `-32800` to `context.Canceled`, so the workbench treats the operation
  as canceled (the prior result table is preserved).

## Shutdown

- At shutdown the host closes every open session first (each gets its
  `perk/v1/close`), then closes the children: stdin is closed (the
  plugin sees **EOF**), the reader is given up to 5 seconds to observe
  the child exiting, and a still-running child is killed and reaped.
- The SDK terminates on stdin EOF: in-flight requests are aborted and the
  `closed` promise resolves; the plugin process should then exit.
- `Loader.Close` and the client close are idempotent; pending calls fail
  with the terminal error. A plugin that exits on its own fails every
  pending call with the read error.

## Example plugin

A complete, minimal, executable Node driver: an in-memory key-value store
served over perk/v1. It advertises one non-conflicting target prefix
(`demo-kv:`), builds a target from form values, opens a session, lists
schema objects, executes statements, and supports a blocking statement
(`SLEEP`) that is canceled through `{signal}` by throwing
`RequestCancelledError`. All 20 mandatory handlers are provided; the
optional write handlers are absent because the capabilities do not
advertise them (the SDK rejects mismatches in both directions). The file
is a `bin` executable with a shebang; diagnostics go to stderr.

```js
#!/usr/bin/env node
'use strict';

// perk-demo.js — a complete perk/v1 plugin: an in-memory key-value store.
// Speaks JSON-RPC 2.0 over NDJSON stdio: requests on stdin, protocol
// frames on stdout, diagnostics on stderr.
//
// package.json:
//   {
//     "name": "perk-demo-kv",
//     "version": "1.0.0",
//     "bin": { "perk-demo": "./perk-demo.js" },
//     "dependencies": { "perk-workbench-plugin-sdk": "^0.0.0" }
//   }
//
// config.json:
//   { "plugins": ["perk-demo"] }                              # npm install -g perk-demo-kv
//   # project-local: "plugins": ["/home/alice/projects/perk-demo-kv/node_modules/.bin/perk-demo"]

const {
  createPluginServer,
  RequestCancelledError,
  PluginOperationError,
  ErrorKind,
  IndexKind,
} = require('perk-workbench-plugin-sdk');

const databases = new Map(); // database name -> Map(key, value)

// Resolves after ms milliseconds, or rejects with RequestCancelledError
// when the host cancels the request (perk/v1/cancel).
function sleep(ms, signal) {
  return new Promise((resolve, reject) => {
    if (signal.aborted) {
      reject(new RequestCancelledError('request canceled'));
      return;
    }
    const timer = setTimeout(() => {
      signal.removeEventListener('abort', onAbort);
      resolve();
    }, ms);
    const onAbort = () => {
      clearTimeout(timer);
      reject(new RequestCancelledError('request canceled'));
    };
    signal.addEventListener('abort', onAbort, { once: true });
  });
}

// result builds a wire Result DTO.
function result(columns, rows, rowsAffected = 0) {
  return {
    columns,
    column_types: columns.map(() => 'string'),
    rows,
    untruncated_rows: rows,
    rows_affected: rowsAffected,
    has_more: false,
    duration_ns: 0,
    truncated: false,
  };
}

// makeService returns the 20 mandatory session handlers. Handler request
// shapes are the wire params minus session_id; context.signal aborts on
// cancellation.
function makeService(store, name) {
  const execute = async (statement, signal, writable) => {
    const [verb, key, ...rest] = statement.trim().split(/\s+/);
    switch ((verb || '').toUpperCase()) {
      case 'SET':
        if (!writable) {
          throw new PluginOperationError('read-only: SET is not allowed', { kind: ErrorKind.Validation });
        }
        if (key === undefined) throw new Error('SET requires a key');
        store.set(key, rest.join(' '));
        return result([], [], 1);
      case 'GET': {
        const value = store.get(key);
        return result(['key', 'value'], value === undefined ? [] : [[key, value]]);
      }
      case 'DEL':
        if (!writable) {
          throw new PluginOperationError('read-only: DEL is not allowed', { kind: ErrorKind.Validation });
        }
        if (key === undefined) throw new Error('DEL requires a key');
        return result([], [], store.delete(key) ? 1 : 0);
      case 'SLEEP': {
        // Blocking statement: cancellation aborts it through {signal}.
        const ms = Number(key || 0);
        await sleep(ms, signal);
        return result(['slept'], [[String(ms)]]);
      }
      default:
        throw new PluginOperationError(`unsupported statement: ${statement}`, { kind: ErrorKind.Unsupported });
    }
  };

  return {
    execute(request, context) {
      return execute(request.statement, context.signal, true);
    },
    executeReadOnly(request, context) {
      return execute(request.statement, context.signal, false);
    },
    validate() {
      // Statements are parsed during execute; nothing to pre-check.
    },
    listSchema() {
      return [{ database: name, type: 'table', name: 'kv', row_count: store.size }];
    },
    tableInfo() {
      return [
        { name: 'key', type: 'string', attributes: '', nullable: false, default_value: null, primary_key: 1, indexes: [IndexKind.PrimaryKey] },
        { name: 'value', type: 'string', attributes: '', nullable: true, default_value: null, primary_key: 0, indexes: [] },
      ];
    },
    listIndexes() {
      return [{ name: 'PRIMARY', unique: true, primary_key: true, columns: ['key'] }];
    },
    createIndex() {
      throw new PluginOperationError('the demo store has a fixed primary index', { kind: ErrorKind.Unsupported });
    },
    replaceIndex() {
      throw new PluginOperationError('the demo store has a fixed primary index', { kind: ErrorKind.Unsupported });
    },
    dropIndex() {
      throw new PluginOperationError('the demo store has a fixed primary index', { kind: ErrorKind.Unsupported });
    },
    listForeignKeys() { return []; },
    listReferencingForeignKeys() { return []; },
    listForeignKeysAll() { return {}; },
    listIndexesAll() {
      return { kv: [{ name: 'PRIMARY', unique: true, primary_key: true, columns: ['key'] }] };
    },
    createForeignKey() {
      throw new PluginOperationError('the demo store has no foreign keys', { kind: ErrorKind.Unsupported });
    },
    replaceForeignKey() {
      throw new PluginOperationError('the demo store has no foreign keys', { kind: ErrorKind.Unsupported });
    },
    dropForeignKey() {
      throw new PluginOperationError('the demo store has no foreign keys', { kind: ErrorKind.Unsupported });
    },
    alterColumn() {
      throw new PluginOperationError('the demo store has a fixed schema', { kind: ErrorKind.Unsupported });
    },
    dropColumn() {
      throw new PluginOperationError('the demo store has a fixed schema', { kind: ErrorKind.Unsupported });
    },
    addColumn() {
      throw new PluginOperationError('the demo store has a fixed schema', { kind: ErrorKind.Unsupported });
    },
    browseTable(request) {
      const entries = [...store.entries()];
      const offset = request.options.offset || 0;
      const limit = request.options.limit ?? entries.length;
      const page = entries.slice(offset, offset + limit);
      return {
        ...result(['key', 'value'], page.map(([k, v]) => [k, v])),
        has_more: offset + page.length < entries.length,
      };
    },
    close() {
      // The store is in-process; nothing to release.
    },
    // rowWrite/documentWrite are deliberately absent: the advertised
    // write_capabilities do not include them.
  };
}

const definition = {
  capabilities: {
    name: 'demo-kv',
    display: 'Demo KV',
    targets: [{ prefix: 'demo-kv:' }],
    form: {
      fields: [
        { key: 'database', title: 'Database', kind: 0, placeholder: 'default', default: 'default', validate: 0 },
      ],
    },
    query_language: {
      name: 'KV',
      editor_label: 'Command',
      placeholder: 'Enter a statement…',
      examples: ['GET key', 'SET key value', 'DEL key'],
    },
    write_capabilities: { row_writer: false },
  },
  buildTarget(values) {
    // Required by the SDK; the workbench calls it when a driver
    // advertises a form. Serialized targets carry the driver's prefix
    // so the host routes them back; the host strips "demo-kv:" before
    // open.
    return { target: `demo-kv:${(values.database || '').trim() || 'default'}`, ok: true };
  },
  open(target) {
    // Label prefixes are stripped by the host, so target is the
    // database name ("default" when the form left it blank).
    const name = target.trim() || 'default';
    if (!databases.has(name)) databases.set(name, new Map());
    const store = databases.get(name);
    console.error(`[perk-demo] session for database ${name} (${store.size} keys)`);
    return { info: { product: 'Demo KV', version: '1.0' }, service: makeService(store, name) };
  },
};

createPluginServer(definition, { input: process.stdin, output: process.stdout })
  .closed.then(() => process.exit(0));
```

What the example demonstrates:

- `capabilities.targets` declares the single `demo-kv:` label prefix —
  no overlap with the built-in drivers (`mysql:`, `postgres://`,
  `postgresql://`, `postgres:`, `mongo:`, `mongodb://`,
  `mongodb+srv://`).
- `capabilities.form` declares the Database input; without it the
  driver is target-only and never appears in the connection form's
  driver select.
- `capabilities.query_language` advertises the KV statement language
  for the query editor; a plugin that omits it (or sends an empty
  object) gets the host's legacy SQL default instead.
- `buildTarget` serializes the form values into `demo-kv:<database>`,
  which the host routes back and strips before `open`.
- `open` returns `{info, service}`; the SDK assigns the `session_id` and
  owns the session lifecycle. `listSchema` serves schema objects; the
  flat list here is fine — the host synthesizes the missing `database`
  root for internal rendering.
- `execute` runs statements and supports the blocking `SLEEP` statement,
  canceled through `context.signal` by throwing `RequestCancelledError`
  (or by observing `signal.aborted` directly).
- Failures are thrown as `PluginOperationError` with a stable
  `ErrorKind` (validation for read-only writes, unsupported for the
  fixed-schema handlers), so the host surfaces the operation with its
  kind while the generic `Error`s keep the plain internal-error
  mapping.
- stderr carries diagnostics; stdout carries protocol frames only.
- `server.closed` resolves at input EOF or termination, after which the
  process exits.

Run it with the workbench after installing the SDK and linking the bin;
an invalid entry is logged and skipped while built-in drivers start.
