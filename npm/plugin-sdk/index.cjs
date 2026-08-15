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

// normalizeCapabilities makes the wire shape explicit: write_capabilities
// is always present, and document is omitted when null — matching the Go
// host DTOs (database.Capabilities, sharedsql.WriteCapabilities).
function normalizeCapabilities(capabilities) {
  requireObject(capabilities, 'capabilities');
  const write = capabilities.write_capabilities;
  if (write !== undefined && write !== null && (typeof write !== 'object' || Array.isArray(write))) {
    throw new TypeError('capabilities.write_capabilities must be an object');
  }
  const writeCapabilities = { row_writer: !!(write && write.row_writer) };
  if (write && write.document != null) {
    writeCapabilities.document = write.document;
  }
  return { ...capabilities, write_capabilities: writeCapabilities };
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
        this._respondError(id, RPC_ERROR_CANCELED, 'canceled');
        return;
      }
      if (error) {
        let code;
        let message;
        if (error instanceof RequestCancelledError) {
          code = RPC_ERROR_CANCELED;
          message = error.message || 'canceled';
        } else if (Number.isInteger(error && error.code)) {
          code = error.code;
          message = (error && error.message) || 'internal error';
        } else {
          code = -32603;
          message = (error && error.message) || 'internal error';
        }
        this._respondError(id, code, message);
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
      (result) => finish(undefined, result),
      (error) => finish(error),
    );
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

  _respondError(id, code, message) {
    this._respond(id, { error: { code, message } });
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
  FormFieldKind,
  FormValidation,
  DocumentFormat,
  IndexKind,
  ValueKind,
  RowWriteOperation,
  DocumentWriteOperation,
};
