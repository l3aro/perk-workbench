'use strict';

// perk-workbench-plugin-sdk — CommonJS author SDK for external perk/v1
// database plugins. This is the plugin side of the perk/v1 JSON-RPC 2.0
// stdio protocol spoken by the perk-workbench host
// (internal/database/plugin): NDJSON frames, one UTF-8 JSON object per
// line, numeric ids, 16 MiB frame bound. Dependency-free; Node >= 18.

const PROTOCOL_VERSION = 1;

// MaxFrameBytes bounds one protocol frame — a UTF-8 JSON object plus a
// trailing newline — on the wire. A frame that does not fit is
// oversized and terminates the server, mirroring the host.
const MAX_FRAME_BYTES = 16 << 20;

// RPCErrorCanceled is the perk/v1 error code for a canceled operation.
const RPC_ERROR_CANCELED = -32800;

// ErrorKind — stable operation-error kinds, the frozen mirror of the Go
// host's plugin.Kind enum (internal/database/plugin/protocol.go). The
// host treats its own method and plugin identity as authoritative and
// normalizes unknown or blank kinds to operation.
const ErrorKind = Object.freeze({
  Validation: 'validation',
  Authentication: 'authentication',
  Connection: 'connection',
  Operation: 'operation',
  Unsupported: 'unsupported',
  Cancelled: 'cancelled',
  Protocol: 'protocol',
  PluginCrash: 'plugin_crash',
});

const ERROR_KINDS = new Set(Object.values(ErrorKind));

// PluginOperationError is a structured plugin operation error a handler
// can throw: the server replies with its integer code (default -32000)
// and message plus an optional data object carrying kind/plugin/method
// provenance and optional advisory guidance. A blank or unknown kind
// normalizes to operation, matching the host. Advisory hint and
// suggested_statement are non-control: the host renders them separately
// from the error and never executes a suggested statement; blank values
// are omitted from the wire. Generic thrown errors keep the legacy
// -32603 mapping and carry no data.
class PluginOperationError extends Error {
  constructor(message, options = {}) {
    super(message);
    this.name = 'PluginOperationError';
    this.code = Number.isInteger(options.code) ? options.code : -32000;
    this.kind = ERROR_KINDS.has(options.kind) ? options.kind : ErrorKind.Operation;
    if (typeof options.plugin === 'string' && options.plugin !== '') this.plugin = options.plugin;
    if (typeof options.method === 'string' && options.method !== '') this.method = options.method;
    if (typeof options.hint === 'string' && options.hint !== '') this.hint = options.hint;
    if (typeof options.suggested_statement === 'string' && options.suggested_statement !== '') {
      this.suggested_statement = options.suggested_statement;
    }
  }
}

// perk/v1 method names — mirror of internal/database/plugin/protocol.go.
const METHOD = {
  initialize: 'perk/v1/initialize',
  buildTarget: 'perk/v1/build_target',
  open: 'perk/v1/open',
  close: 'perk/v1/close',
  cancel: 'perk/v1/cancel',
  execute: 'perk/v1/execute',
  executeReadOnly: 'perk/v1/execute_read_only',
  validate: 'perk/v1/validate',
  listSchema: 'perk/v1/list_schema',
  tableInfo: 'perk/v1/table_info',
  listIndexes: 'perk/v1/list_indexes',
  createIndex: 'perk/v1/create_index',
  replaceIndex: 'perk/v1/replace_index',
  dropIndex: 'perk/v1/drop_index',
  listForeignKeys: 'perk/v1/list_foreign_keys',
  listReferencingForeignKeys: 'perk/v1/list_referencing_foreign_keys',
  listForeignKeysAll: 'perk/v1/list_foreign_keys_all',
  listIndexesAll: 'perk/v1/list_indexes_all',
  createForeignKey: 'perk/v1/create_foreign_key',
  replaceForeignKey: 'perk/v1/replace_foreign_key',
  dropForeignKey: 'perk/v1/drop_foreign_key',
  alterColumn: 'perk/v1/alter_column',
  dropColumn: 'perk/v1/drop_column',
  addColumn: 'perk/v1/add_column',
  browseTable: 'perk/v1/browse_table',
  rowWrite: 'perk/v1/row_write',
  documentWrite: 'perk/v1/document_write',
  workspaceView: 'perk/v1/workspace_view',
};

// Session-service handlers the SDK requires on every opened session,
// mirroring sharedsql.Service in internal/sql/service.go.
const MANDATORY_SERVICE_HANDLERS = [
  'execute', 'executeReadOnly', 'validate', 'listSchema', 'tableInfo',
  'listIndexes', 'createIndex', 'replaceIndex', 'dropIndex',
  'listForeignKeys', 'listReferencingForeignKeys', 'listForeignKeysAll',
  'listIndexesAll', 'createForeignKey', 'replaceForeignKey',
  'dropForeignKey', 'alterColumn', 'dropColumn', 'addColumn', 'browseTable',
];

// RequestCancelledError signals that a request was canceled by the host.
class RequestCancelledError extends Error {
  constructor(message = 'request cancelled') {
    super(message);
    this.name = 'RequestCancelledError';
    this.code = RPC_ERROR_CANCELED;
  }
}

// InvalidParamsError maps to the JSON-RPC -32602 invalid params error.
class InvalidParamsError extends Error {
  constructor(message) {
    super(message);
    this.name = 'InvalidParamsError';
    this.code = -32602;
  }
}

function requireObject(value, label) {
  if (typeof value !== 'object' || value === null || Array.isArray(value)) {
    throw new InvalidParamsError(`${label} must be an object`);
  }
  return value;
}

function requireString(value, label) {
  if (typeof value !== 'string') {
    throw new InvalidParamsError(`${label} must be a string`);
  }
  return value;
}

// Bounds every advertised query_language command entry must respect,
// mirroring internal/sql/query_language.go. A plugin can never force an
// unbounded completion list or handshake frame.
const MAX_QUERY_COMMANDS = 512;
const MAX_QUERY_COMMAND_NAME_RUNES = 64;
const MAX_QUERY_COMMAND_USAGE_RUNES = 256;
const MAX_QUERY_COMMAND_SUMMARY_RUNES = 256;

const CONTROL_RE = /[\u0000-\u001F\u007F-\u009F]/;

function requireControlFree(value, label) {
  if (CONTROL_RE.test(value)) {
    throw new TypeError(`${label} must not contain control characters`);
  }
}

function runeLength(value) {
  return Array.from(value).length;
}

// normalizeQueryLanguage makes the optional query_language shape
// explicit: absent, null, or all-empty advertisements stay absent (the
// host applies its legacy SQL default), while a present advertisement
// must carry nonblank name/editor_label/placeholder and only nonblank
// examples and valid command entries — mirroring the host's validation
// in internal/sql/query_language.go.
function normalizeQueryLanguage(queryLanguage) {
  if (queryLanguage === undefined || queryLanguage === null) {
    return undefined;
  }
  requireObject(queryLanguage, 'capabilities.query_language');
  const name = queryLanguage.name;
  const editorLabel = queryLanguage.editor_label;
  const placeholder = queryLanguage.placeholder;
  const lexer = queryLanguage.lexer;
  const examples = queryLanguage.examples;
  const commands = queryLanguage.commands;
  if (
    (name === undefined || name === '') &&
    (editorLabel === undefined || editorLabel === '') &&
    (placeholder === undefined || placeholder === '') &&
    (lexer === undefined || lexer === '') &&
    (examples === undefined || (Array.isArray(examples) && examples.length === 0)) &&
    (commands === undefined || (Array.isArray(commands) && commands.length === 0))
  ) {
    return undefined;
  }
  if (typeof name !== 'string' || name.trim() === '') {
    throw new TypeError('capabilities.query_language.name must be a nonblank string');
  }
  if (typeof editorLabel !== 'string' || editorLabel.trim() === '') {
    throw new TypeError('capabilities.query_language.editor_label must be a nonblank string');
  }
  if (typeof placeholder !== 'string' || placeholder.trim() === '') {
    throw new TypeError('capabilities.query_language.placeholder must be a nonblank string');
  }
  if (lexer !== undefined && typeof lexer !== 'string') {
    throw new TypeError('capabilities.query_language.lexer must be a string');
  }
  if (examples !== undefined) {
    if (!Array.isArray(examples)) {
      throw new TypeError('capabilities.query_language.examples must be an array');
    }
    for (const example of examples) {
      if (typeof example !== 'string' || example.trim() === '') {
        throw new TypeError('capabilities.query_language.examples must contain only nonblank strings');
      }
    }
  }
  let normalizedCommands;
  if (commands !== undefined) {
    normalizedCommands = normalizeQueryCommands(commands);
  }
  const normalized = { name, editor_label: editorLabel, placeholder };
  if (lexer !== undefined) normalized.lexer = lexer;
  if (examples !== undefined) normalized.examples = [...examples];
  // An empty command list is omitted like the Go host's omitempty DTO.
  if (normalizedCommands !== undefined && normalizedCommands.length > 0) {
    normalized.commands = normalizedCommands;
  }
  return normalized;
}

// normalizeQueryCommands makes the optional query_language.commands
// shape explicit: every entry carries nonblank bounded control-free
// name/usage/summary, names are unique case-insensitively, and the list
// is capped. An undefined list stays undefined (omitted from the wire);
// an empty array normalizes to an empty array.
// Command names are ASCII letters/digits/underscores — the exact
// charset the host editor tokenizes, so every advertised name is
// completable — which also makes lowercase folding a total
// case-insensitive equality.
const COMMAND_NAME_RE = /^[A-Za-z0-9_]+$/;

// Bounds every workspace tab advertisement must respect, mirroring
// internal/sql/workspace_view.go. A plugin can never force an unbounded
// tab row or handshake frame.
const MAX_CUSTOM_WORKSPACE_VIEWS = 8;
const MAX_WORKSPACE_VIEW_ID_RUNES = 64;
const MAX_WORKSPACE_VIEW_LABEL_RUNES = 32;

const STANDARD_WORKSPACE_TABS = new Set(['columns', 'indexes', 'foreign_keys', 'diagram']);
const WORKSPACE_VIEW_KINDS = new Set(['database', 'schema', 'table']);

// normalizeWorkspaceCapability makes the optional workspace shape
// explicit: a present (non-null, non-undefined) object is validated and
// returned with only non-empty lists (matching the Go host's omitempty
// DTOs), so an explicitly advertised empty workspace — which means no
// standard tabs beyond Query/Browse and no custom views — stays
// distinguishable from an absent advertisement (whose legacy per-product
// tab policy applies). Standard tabs come from the fixed four-value set
// without duplicates; custom views are capped with nonblank bounded
// control-free ids and labels, unique case-insensitively, plus nonempty
// duplicate-free scopes from database/schema/table.
function normalizeWorkspaceCapability(workspace) {
  if (workspace === undefined || workspace === null) {
    return undefined;
  }
  requireObject(workspace, 'capabilities.workspace');
  const normalized = {};
  const standardTabs = workspace.standard_tabs;
  if (standardTabs !== undefined) {
    if (!Array.isArray(standardTabs)) {
      throw new TypeError('capabilities.workspace.standard_tabs must be an array');
    }
    const seen = new Set();
    const tabs = [];
    for (const tab of standardTabs) {
      if (typeof tab !== 'string' || !STANDARD_WORKSPACE_TABS.has(tab)) {
        throw new TypeError('capabilities.workspace.standard_tabs must contain only columns, indexes, foreign_keys, diagram');
      }
      if (seen.has(tab)) {
        throw new TypeError('capabilities.workspace.standard_tabs must not contain duplicates');
      }
      seen.add(tab);
      tabs.push(tab);
    }
    if (tabs.length > 0) normalized.standard_tabs = tabs;
  }
  const customViews = workspace.custom_views;
  if (customViews !== undefined) {
    if (!Array.isArray(customViews)) {
      throw new TypeError('capabilities.workspace.custom_views must be an array');
    }
    if (customViews.length > MAX_CUSTOM_WORKSPACE_VIEWS) {
      throw new TypeError(`capabilities.workspace.custom_views must not exceed ${MAX_CUSTOM_WORKSPACE_VIEWS} entries`);
    }
    const seenIDs = new Set();
    const seenLabels = new Set();
    const views = [];
    for (let i = 0; i < customViews.length; i++) {
      const view = customViews[i];
      requireObject(view, `capabilities.workspace.custom_views[${i}]`);
      const id = view.id;
      const label = view.label;
      const scopes = view.scopes;
      if (typeof id !== 'string' || id.trim() === '') {
        throw new TypeError(`capabilities.workspace.custom_views[${i}].id must be a nonblank string`);
      }
      if (runeLength(id) > MAX_WORKSPACE_VIEW_ID_RUNES) {
        throw new TypeError(`capabilities.workspace.custom_views[${i}].id must not exceed ${MAX_WORKSPACE_VIEW_ID_RUNES} runes`);
      }
      requireControlFree(id, `capabilities.workspace.custom_views[${i}].id`);
      if (typeof label !== 'string' || label.trim() === '') {
        throw new TypeError(`capabilities.workspace.custom_views[${i}].label must be a nonblank string`);
      }
      if (runeLength(label) > MAX_WORKSPACE_VIEW_LABEL_RUNES) {
        throw new TypeError(`capabilities.workspace.custom_views[${i}].label must not exceed ${MAX_WORKSPACE_VIEW_LABEL_RUNES} runes`);
      }
      requireControlFree(label, `capabilities.workspace.custom_views[${i}].label`);
      if (!Array.isArray(scopes) || scopes.length === 0) {
        throw new TypeError(`capabilities.workspace.custom_views[${i}].scopes must be a nonempty array`);
      }
      const scopeSeen = new Set();
      const normalizedScopes = [];
      for (const scope of scopes) {
        if (typeof scope !== 'string' || !WORKSPACE_VIEW_KINDS.has(scope)) {
          throw new TypeError(`capabilities.workspace.custom_views[${i}].scopes must contain only database, schema, table`);
        }
        if (scopeSeen.has(scope)) {
          throw new TypeError(`capabilities.workspace.custom_views[${i}].scopes must not contain duplicates`);
        }
        scopeSeen.add(scope);
        normalizedScopes.push(scope);
      }
      const idKey = id.toLowerCase();
      if (seenIDs.has(idKey)) {
        throw new TypeError('capabilities.workspace.custom_views ids must be unique case-insensitively');
      }
      seenIDs.add(idKey);
      const labelKey = label.toLowerCase();
      if (seenLabels.has(labelKey)) {
        throw new TypeError('capabilities.workspace.custom_views labels must be unique case-insensitively');
      }
      seenLabels.add(labelKey);
      views.push({ id, label, scopes: normalizedScopes });
    }
    if (views.length > 0) normalized.custom_views = views;
  }
  return normalized;
}

// normalizeQueryCommands makes the optional query_language.commands

function normalizeQueryCommands(commands) {
  if (!Array.isArray(commands)) {
    throw new TypeError('capabilities.query_language.commands must be an array');
  }
  if (commands.length > MAX_QUERY_COMMANDS) {
    throw new TypeError(`capabilities.query_language.commands must not exceed ${MAX_QUERY_COMMANDS} entries`);
  }
  const seen = new Set();
  const normalized = [];
  for (let i = 0; i < commands.length; i++) {
    const entry = commands[i];
    requireObject(entry, `capabilities.query_language.commands[${i}]`);
    const commandName = entry.name;
    const usage = entry.usage;
    const summary = entry.summary;
    if (typeof commandName !== 'string' || commandName.trim() === '') {
      throw new TypeError(`capabilities.query_language.commands[${i}].name must be a nonblank string`);
    }
    if (!COMMAND_NAME_RE.test(commandName)) {
      throw new TypeError(`capabilities.query_language.commands[${i}].name must be ASCII letters, digits, or underscores`);
    }
    if (runeLength(commandName) > MAX_QUERY_COMMAND_NAME_RUNES) {
      throw new TypeError(`capabilities.query_language.commands[${i}].name must not exceed ${MAX_QUERY_COMMAND_NAME_RUNES} runes`);
    }
    for (const [field, value, maxRunes] of [
      ['usage', usage, MAX_QUERY_COMMAND_USAGE_RUNES],
      ['summary', summary, MAX_QUERY_COMMAND_SUMMARY_RUNES],
    ]) {
      if (typeof value !== 'string' || value.trim() === '') {
        throw new TypeError(`capabilities.query_language.commands[${i}].${field} must be a nonblank string`);
      }
      if (runeLength(value) > maxRunes) {
        throw new TypeError(`capabilities.query_language.commands[${i}].${field} must not exceed ${maxRunes} runes`);
      }
      requireControlFree(value, `capabilities.query_language.commands[${i}].${field}`);
    }
    const key = commandName.toLowerCase();
    if (seen.has(key)) {
      throw new TypeError(`capabilities.query_language.commands names must be unique case-insensitively (duplicate ${commandName})`);
    }
    seen.add(key);
    normalized.push({ name: commandName, usage, summary });
  }
  return normalized;
}

// normalizeCapabilities makes the wire shape explicit: driver is trimmed
// and falls back to name, write_capabilities is always present, document
// is omitted when null, query_language is omitted when absent or null,
// and workspace is omitted when absent or null — matching the Go host
// DTOs (database.Capabilities, sharedsql.WriteCapabilities,
// sharedsql.WorkspaceCapability).
function normalizeCapabilities(capabilities) {
  requireObject(capabilities, 'capabilities');
  const driver = capabilities.driver;
  if (driver !== undefined && typeof driver !== 'string') {
    throw new TypeError('capabilities.driver must be a string when provided');
  }
  const write = capabilities.write_capabilities;
  if (write !== undefined && write !== null && (typeof write !== 'object' || Array.isArray(write))) {
    throw new TypeError('capabilities.write_capabilities must be an object');
  }
  const writeCapabilities = { row_writer: !!(write && write.row_writer) };
  if (write && write.document != null) {
    writeCapabilities.document = write.document;
  }
  const queryLanguage = normalizeQueryLanguage(capabilities.query_language);
  const workspace = normalizeWorkspaceCapability(capabilities.workspace);
  const normalized = { ...capabilities, write_capabilities: writeCapabilities };
  if (driver !== undefined) {
    const fallbackDriver = typeof capabilities.name === 'string' ? capabilities.name.trim() : '';
    const normalizedDriver = driver.trim() || fallbackDriver;
    if (normalizedDriver === '') {
      throw new TypeError('capabilities.driver must resolve to a nonblank string');
    }
    normalized.driver = normalizedDriver;
  }
  if (queryLanguage === undefined) {
    delete normalized.query_language;
  } else {
    normalized.query_language = queryLanguage;
  }
  if (workspace === undefined) {
    delete normalized.workspace;
  } else {
    normalized.workspace = workspace;
  }
  return normalized;
}

// Public enum constants — frozen mirrors of the Go host's DTO enums,
// matching index.d.ts. Values are the v1 wire numbers/strings.
const FormFieldKind = Object.freeze({ Input: 0, Password: 1, Select: 2 });
const FormValidation = Object.freeze({ None: 0, Required: 1, Port: 2 });
const DocumentFormat = Object.freeze({
  MongoExtendedJSON: 'application/vnd.perk.mongodb.extjson+json;version=2;mode=relaxed',
});
const IndexKind = Object.freeze({ PrimaryKey: 1, Unique: 2, Regular: 3 });
const ValueKind = Object.freeze({
  Default: 'default',
  Null: 'null',
  String: 'string',
  Bool: 'bool',
  Integer: 'integer',
  Float: 'float',
  Bytes: 'bytes',
  Decimal: 'decimal',
  Timestamp: 'timestamp',
  Array: 'array',
  Object: 'object',
});
const RowWriteOperation = Object.freeze({ Insert: 'insert', Update: 'update', Delete: 'delete' });
const DocumentWriteOperation = Object.freeze({ Read: 'read', Insert: 'insert', Replace: 'replace', Delete: 'delete' });
const StandardWorkspaceTab = Object.freeze({
  Columns: 'columns',
  Indexes: 'indexes',
  ForeignKeys: 'foreign_keys',
  Diagram: 'diagram',
});
const WorkspaceViewScope = Object.freeze({ Database: 'database', Schema: 'schema', Table: 'table' });

class PluginServer {
  constructor(definition, options) {
    requireObject(definition, 'definition');
    if (typeof definition.buildTarget !== 'function') {
      throw new TypeError('definition.buildTarget must be a function');
    }
    if (typeof definition.open !== 'function') {
      throw new TypeError('definition.open must be a function');
    }
    requireObject(options, 'options');
    const input = options.input;
    const output = options.output;
    if (!input || typeof input.on !== 'function' || typeof input[Symbol.asyncIterator] !== 'function') {
      throw new TypeError('options.input must be a readable stream');
    }
    if (!output || typeof output.write !== 'function') {
      throw new TypeError('options.output must be a writable stream');
    }
    this.definition = definition;
    this.capabilities = normalizeCapabilities(definition.capabilities);
    this.input = input;
    this.output = output;
    this.initialized = false;
    this.sessions = new Map(); // session_id -> { service, info }
    this.requests = new Map(); // request id -> { controller }
    this.nextSessionID = 1;
    this._terminated = false;
    this._writeTail = Promise.resolve();
    this._buffer = Buffer.alloc(0);
    this._decoder = new TextDecoder('utf-8', { fatal: true });
    this.closed = new Promise((resolve) => {
      this._resolveClosed = resolve;
    });
    this._supported = new Set([
      METHOD.buildTarget, METHOD.open, METHOD.close,
      METHOD.execute, METHOD.executeReadOnly, METHOD.validate, METHOD.listSchema,
      METHOD.tableInfo, METHOD.listIndexes, METHOD.createIndex, METHOD.replaceIndex,
      METHOD.dropIndex, METHOD.listForeignKeys, METHOD.listReferencingForeignKeys,
      METHOD.listForeignKeysAll, METHOD.listIndexesAll, METHOD.createForeignKey,
      METHOD.replaceForeignKey, METHOD.dropForeignKey, METHOD.alterColumn,
      METHOD.dropColumn, METHOD.addColumn, METHOD.browseTable,
    ]);
    if (this.capabilities.write_capabilities.row_writer === true) {
      this._supported.add(METHOD.rowWrite);
    }
    if (this.capabilities.write_capabilities.document != null) {
      this._supported.add(METHOD.documentWrite);
    }
    const customViews = this.capabilities.workspace && this.capabilities.workspace.custom_views;
    if (customViews !== undefined && customViews.length > 0) {
      this._supported.add(METHOD.workspaceView);
    }
    output.on('error', () => this._terminate());
    this._readLoop();
  }

  // close terminates the server: in-flight requests are aborted and no
  // further frames are processed or written. Idempotent.
  close() {
    this._terminate();
    if (typeof this.input.destroy === 'function') {
      this.input.destroy();
    }
    return this.closed;
  }

  // _readLoop consumes NDJSON frames until EOF, a protocol violation, or
  // close(). Any malformed, invalid-UTF-8, or oversized frame terminates
  // the server, mirroring the host's terminal handling.
  async _readLoop() {
    try {
      for await (const chunk of this.input) {
        if (this._terminated) return;
        const data = Buffer.isBuffer(chunk) ? chunk : Buffer.from(chunk);
        this._buffer = this._buffer.length === 0 ? data : Buffer.concat([this._buffer, data]);
        let newline;
        while ((newline = this._buffer.indexOf(0x0a)) !== -1) {
          const frame = this._buffer.subarray(0, newline);
          this._buffer = this._buffer.subarray(newline + 1);
          if (frame.length > MAX_FRAME_BYTES - 1) {
            this._terminate();
            return;
          }
          this._handleFrame(frame);
          if (this._terminated) return;
        }
        if (this._buffer.length >= MAX_FRAME_BYTES) {
          this._terminate();
          return;
        }
      }
    } catch {
      // A destroyed or failed input ends the server either way.
    }
    this._terminate();
  }

  _handleFrame(frame) {
    let text;
    try {
      text = this._decoder.decode(frame);
    } catch {
      this._terminate();
      return;
    }
    let message;
    try {
      message = JSON.parse(text);
    } catch {
      this._terminate();
      return;
    }
    if (typeof message !== 'object' || message === null || Array.isArray(message)) {
      this._terminate();
      return;
    }
    this._dispatch(message);
  }

  _dispatch(message) {
    const isNotification = !Object.prototype.hasOwnProperty.call(message, 'id');
    if (isNotification) {
      if (message.method === METHOD.cancel) {
        const id = message.params && message.params.id;
        if (Number.isInteger(id)) {
          const request = this.requests.get(id);
          if (request) request.controller.abort();
        }
      }
      return;
    }

    const { id } = message;
    if (
      message.jsonrpc !== '2.0' ||
      typeof message.method !== 'string' ||
      message.method === '' ||
      !Number.isInteger(id)
    ) {
      this._respondError(Number.isInteger(id) ? id : null, -32600, 'invalid request');
      return;
    }

    if (message.method === METHOD.initialize) {
      this._handleInitialize(id, message.params);
      return;
    }
    if (!this.initialized) {
      this._respondError(id, -32600, `initialize required before ${message.method}`);
      return;
    }
    if (!this._supported.has(message.method)) {
      this._respondError(id, -32601, `method not found: ${message.method}`);
      return;
    }
    this._dispatchRequest(id, message.method, message.params);
  }

  _handleInitialize(id, params) {
    if (this.initialized) {
      this._respondError(id, -32600, 'already initialized');
      return;
    }
    if (typeof params !== 'object' || params === null || Array.isArray(params)) {
      this._respondError(id, -32602, 'params must be an object');
      return;
    }
    if (!Number.isInteger(params.protocol_version) || params.protocol_version !== PROTOCOL_VERSION) {
      this._respondError(id, -32600, `unsupported protocol_version: ${params.protocol_version}`);
      return;
    }
    this.initialized = true;
    this._respondResult(id, { protocol_version: PROTOCOL_VERSION, capabilities: this.capabilities });
  }

  _dispatchRequest(id, method, params) {
    const controller = new AbortController();
    this.requests.set(id, { controller });
    const context = { signal: controller.signal };
    const finish = (error, result) => {
      this.requests.delete(id);
      if (controller.signal.aborted) {
        this._respondError(id, RPC_ERROR_CANCELED, 'canceled', { kind: ErrorKind.Cancelled });
        return;
      }
      if (error) {
        let code;
        let message;
        let data;
        if (error instanceof RequestCancelledError) {
          code = RPC_ERROR_CANCELED;
          message = error.message || 'canceled';
          data = { kind: ErrorKind.Cancelled };
        } else if (error instanceof PluginOperationError) {
          code = error.code;
          message = error.message || 'internal error';
          data = { kind: error.kind };
          if (error.plugin !== undefined) data.plugin = error.plugin;
          // The wire method is authoritative here; the host overrides
          // data.method with its own request method anyway.
          data.method = error.method !== undefined ? error.method : method;
          // Advisory guidance, never control: the host renders it
          // separately and never executes a suggested statement. Empty
          // strings are omitted entirely.
          if (error.hint !== undefined && error.hint !== '') data.hint = error.hint;
          if (error.suggested_statement !== undefined && error.suggested_statement !== '') {
            data.suggested_statement = error.suggested_statement;
          }
        } else if (Number.isInteger(error && error.code)) {
          code = error.code;
          message = (error && error.message) || 'internal error';
        } else {
          code = -32603;
          message = (error && error.message) || 'internal error';
        }
        this._respondError(id, code, message, data);
        return;
      }
      this._respondResult(id, result);
    };
    let promise;
    try {
      promise = this._invoke(method, params, context);
    } catch (error) {
      finish(error);
      return;
    }
    Promise.resolve(promise).then(
      (result) => {
        try {
          this._validateResultMetadata(method, result);
        } catch (error) {
          finish(error);
          return;
        }
        finish(undefined, result);
      },
      (error) => finish(error),
    );
  }

  // _validateResultMetadata mirrors the host's plugin-boundary rule for
  // statement_metadata: the object is meaningful only with a nonblank
  // statement, and when present it is authoritative — every field must be
  // present with the right type. Violations reject the handler result
  // (an internal error) before it reaches the wire; omission and explicit
  // false pass through verbatim.
  _validateResultMetadata(method, result) {
    let target = result;
    if (method === METHOD.rowWrite || method === METHOD.documentWrite) {
      target = result && typeof result === 'object' && !Array.isArray(result) ? result.result : result;
    }
    if (target === null || typeof target !== 'object' || Array.isArray(target)) {
      return;
    }
    if (!Object.prototype.hasOwnProperty.call(target, 'statement_metadata')) {
      return;
    }
    const metadata = target.statement_metadata;
    if (metadata === null) {
      return; // null is treated as omitted, like the host DTO
    }
    if (typeof metadata !== 'object' || Array.isArray(metadata)) {
      throw new TypeError('statement_metadata must be an object');
    }
    if (typeof metadata.language !== 'string') {
      throw new TypeError('statement_metadata.language must be a string');
    }
    if (typeof metadata.replayable !== 'boolean') {
      throw new TypeError('statement_metadata.replayable must be a boolean');
    }
    if (typeof metadata.sensitive !== 'boolean') {
      throw new TypeError('statement_metadata.sensitive must be a boolean');
    }
    const statement = target.statement;
    if (typeof statement !== 'string' || statement.trim() === '') {
      throw new TypeError('statement_metadata requires a nonblank statement');
    }
  }

  // _invoke resolves one supported method onto the definition and the
  // target session. Handler results become the wire result verbatim;
  // void handlers (undefined) become null.
  _invoke(method, params, context) {
    const definition = this.definition;
    switch (method) {
      case METHOD.buildTarget:
        return definition.buildTarget(requireObject(params, 'params'), context);
      case METHOD.open:
        return this._openSession(params, context);
      case METHOD.close:
        return this._closeSession(params);
      case METHOD.execute:
        return this._sessionCall(params, 'execute', { statement: this._statement(params) }, context);
      case METHOD.executeReadOnly:
        return this._sessionCall(params, 'executeReadOnly', { statement: this._statement(params) }, context);
      case METHOD.validate:
        return this._sessionCall(params, 'validate', { statement: this._statement(params) }, context);
      case METHOD.listSchema:
        return this._sessionCall(params, 'listSchema', {}, context);
      case METHOD.tableInfo:
        return this._sessionCall(params, 'tableInfo', { table: this._table(params) }, context);
      case METHOD.listIndexes:
        return this._sessionCall(params, 'listIndexes', { table: this._table(params) }, context);
      case METHOD.createIndex:
        return this._sessionCall(params, 'createIndex', { table: this._table(params), change: this._object(params, 'change') }, context);
      case METHOD.replaceIndex:
        return this._sessionCall(params, 'replaceIndex', { table: this._table(params), old_name: this._name(params, 'old_name'), change: this._object(params, 'change') }, context);
      case METHOD.dropIndex:
        return this._sessionCall(params, 'dropIndex', { table: this._table(params), name: this._name(params, 'name') }, context);
      case METHOD.listForeignKeys:
        return this._sessionCall(params, 'listForeignKeys', { table: this._table(params) }, context);
      case METHOD.listReferencingForeignKeys:
        return this._sessionCall(params, 'listReferencingForeignKeys', { table: this._table(params) }, context);
      case METHOD.listForeignKeysAll:
        return this._sessionCall(params, 'listForeignKeysAll', {}, context);
      case METHOD.listIndexesAll:
        return this._sessionCall(params, 'listIndexesAll', {}, context);
      case METHOD.createForeignKey:
        return this._sessionCall(params, 'createForeignKey', { table: this._table(params), change: this._object(params, 'change') }, context);
      case METHOD.replaceForeignKey:
        return this._sessionCall(params, 'replaceForeignKey', { table: this._table(params), old_name: this._name(params, 'old_name'), change: this._object(params, 'change') }, context);
      case METHOD.dropForeignKey:
        return this._sessionCall(params, 'dropForeignKey', { table: this._table(params), name: this._name(params, 'name') }, context);
      case METHOD.alterColumn:
        return this._sessionCall(params, 'alterColumn', { table: this._table(params), change: this._object(params, 'change') }, context);
      case METHOD.dropColumn:
        return this._sessionCall(params, 'dropColumn', { table: this._table(params), name: this._name(params, 'name') }, context);
      case METHOD.addColumn:
        return this._sessionCall(params, 'addColumn', { table: this._table(params), def: this._object(params, 'def') }, context);
      case METHOD.browseTable:
        return this._sessionCall(params, 'browseTable', { table: this._table(params), options: this._object(params, 'options') }, context);
      case METHOD.rowWrite:
        return this._sessionCall(params, 'rowWrite', this._object(params, 'request'), context);
      case METHOD.documentWrite:
        return this._sessionCall(params, 'documentWrite', this._object(params, 'request'), context);
      case METHOD.workspaceView:
        return this._sessionCall(params, 'workspaceView', this._workspaceViewRequest(params), context);
      default:
        throw new Error(`perk-workbench-plugin-sdk: unsupported method ${method}`);
    }
  }

  // _openSession runs the definition's open and validates the yielded
  // session service against the advertised capabilities before
  // registering it: the mandatory handlers must exist, and the optional
  // write handlers must match write_capabilities in both directions.
  async _openSession(params, context) {
    const target = requireString(params && params.target, 'target');
    const opened = await this.definition.open(target, context);
    if (typeof opened !== 'object' || opened === null || Array.isArray(opened)) {
      throw new TypeError('open must return { info, service }');
    }
    const service = opened.service;
    if (typeof service !== 'object' || service === null || Array.isArray(service)) {
      throw new TypeError('open must return a service object');
    }
    for (const handler of MANDATORY_SERVICE_HANDLERS) {
      if (typeof service[handler] !== 'function') {
        throw new TypeError(`service must implement ${handler}`);
      }
    }
    const write = this.capabilities.write_capabilities;
    if (write.row_writer === true && typeof service.rowWrite !== 'function') {
      throw new TypeError('service.rowWrite is required when capabilities.write_capabilities.row_writer is true');
    }
    if (write.row_writer !== true && typeof service.rowWrite === 'function') {
      throw new TypeError('service.rowWrite is not supported without capabilities.write_capabilities.row_writer');
    }
    if (write.document != null && typeof service.documentWrite !== 'function') {
      throw new TypeError('service.documentWrite is required when capabilities.write_capabilities.document is set');
    }
    if (write.document == null && typeof service.documentWrite === 'function') {
      throw new TypeError('service.documentWrite is not supported without capabilities.write_capabilities.document');
    }
    const customViews = this.capabilities.workspace && this.capabilities.workspace.custom_views;
    if (customViews !== undefined && customViews.length > 0 && typeof service.workspaceView !== 'function') {
      throw new TypeError('service.workspaceView is required when capabilities.workspace.custom_views is non-empty');
    }
    if ((customViews === undefined || customViews.length === 0) && typeof service.workspaceView === 'function') {
      throw new TypeError('service.workspaceView is not supported without capabilities.workspace.custom_views');
    }
    if (typeof opened.info !== 'object' || opened.info === null || Array.isArray(opened.info)) {
      throw new TypeError('open must return an info object');
    }
    const sessionID = this.nextSessionID++;
    this.sessions.set(sessionID, { service, info: opened.info });
    return { session_id: sessionID, info: opened.info };
  }

  // _closeSession removes the session and closes it: service.close is
  // called when provided, after removal, so a throwing close never
  // resurrects the session.
  async _closeSession(params) {
    const sessionID = this._requireSessionID(params);
    const session = this.sessions.get(sessionID);
    this.sessions.delete(sessionID);
    if (typeof session.service.close === 'function') {
      await session.service.close();
    }
    return null;
  }

  _sessionCall(params, handler, request, context) {
    const sessionID = this._requireSessionID(params);
    return this.sessions.get(sessionID).service[handler](request, context);
  }

  _requireSessionID(params) {
    requireObject(params, 'params');
    const sessionID = params.session_id;
    if (!Number.isInteger(sessionID)) {
      throw new InvalidParamsError('session_id must be an integer');
    }
    if (!this.sessions.has(sessionID)) {
      throw new InvalidParamsError(`unknown session_id ${sessionID}`);
    }
    return sessionID;
  }

  _statement(params) {
    return requireString(params && params.statement, 'statement');
  }

  _table(params) {
    return requireString(params && params.table, 'table');
  }

  // _workspaceViewRequest validates one workspace_view request: a
  // nonblank view id and a target object whose kind is one of
  // database/schema/table, mirroring the host's WorkspaceViewTarget.
  _workspaceViewRequest(params) {
    const viewID = requireString(params && params.view_id, 'view_id');
    if (viewID.trim() === '') {
      throw new InvalidParamsError('view_id must be a nonblank string');
    }
    const target = requireObject(params && params.target, 'target');
    const kind = target.kind;
    if (typeof kind !== 'string' || !WORKSPACE_VIEW_KINDS.has(kind)) {
      throw new InvalidParamsError('target.kind must be one of database, schema, table');
    }
    const normalized = { kind };
    if (typeof target.database === 'string' && target.database !== '') normalized.database = target.database;
    if (typeof target.schema === 'string' && target.schema !== '') normalized.schema = target.schema;
    if (typeof target.table === 'string' && target.table !== '') normalized.table = target.table;
    return { view_id: viewID, target: normalized };
  }

  _name(params, key) {
    return requireString(params && params[key], key);
  }

  _object(params, key) {
    return requireObject(params && params[key], key);
  }

  _respond(id, payload) {
    if (this._terminated) return;
    const frame = `${JSON.stringify({ jsonrpc: '2.0', id, ...payload })}\n`;
    this._writeTail = this._writeTail
      .then(
        () =>
          new Promise((resolve, reject) => {
            this.output.write(frame, (error) => (error ? reject(error) : resolve()));
          }),
      )
      .catch(() => {
        this._terminate();
      });
  }

  _respondResult(id, result) {
    this._respond(id, { result: result === undefined ? null : result });
  }

  _respondError(id, code, message, data) {
    const error = { code, message };
    if (data !== undefined) error.data = data;
    this._respond(id, { error });
  }

  _terminate() {
    if (this._terminated) return;
    this._terminated = true;
    for (const request of this.requests.values()) {
      request.controller.abort();
    }
    this.requests.clear();
    this._resolveClosed();
  }
}

function createPluginServer(definition, options) {
  return new PluginServer(definition, options);
}

module.exports = {
  createPluginServer,
  RequestCancelledError,
  PluginOperationError,
  ErrorKind,
  FormFieldKind,
  FormValidation,
  DocumentFormat,
  IndexKind,
  ValueKind,
  RowWriteOperation,
  DocumentWriteOperation,
  StandardWorkspaceTab,
  WorkspaceViewScope,
};
