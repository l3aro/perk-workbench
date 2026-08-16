// Type declarations for perk-workbench-plugin-sdk — the author SDK for
// external perk/v1 database plugins. The public v1 DTOs and enums below
// freeze the JSON field names of the perk-workbench plugin protocol
// (Go host: internal/database/plugin, internal/database, internal/sql).

/// <reference types="node" />

// --- Driver capabilities ---------------------------------------------------

export interface TargetPattern {
  /** Target prefix routed to this driver ("mysql:", "postgres://"). */
  prefix: string;
  /** Keep the whole target for open when true (URL scheme forms). */
  keep_target?: boolean;
}

/** Connection-form widget kinds: 0 input, 1 password, 2 select. */
export const FormFieldKind: {
  readonly Input: 0;
  readonly Password: 1;
  readonly Select: 2;
};
export type FormFieldKind = typeof FormFieldKind[keyof typeof FormFieldKind];

/** Connection-form validation rules: 0 none, 1 required, 2 port. */
export const FormValidation: {
  readonly None: 0;
  readonly Required: 1;
  readonly Port: 2;
};
export type FormValidation = typeof FormValidation[keyof typeof FormValidation];

export interface FormOption {
  label: string;
  value: string;
}

export interface FormField {
  key: string;
  title: string;
  kind: FormFieldKind;
  placeholder?: string;
  default?: string;
  options?: FormOption[];
  validate: FormValidation;
  error?: string;
}

export interface FormSpec {
  fields: FormField[];
  prefix?: string;
}

/** Tagged payload format for document stores. */
export const DocumentFormat: {
  readonly MongoExtendedJSON: 'application/vnd.perk.mongodb.extjson+json;version=2;mode=relaxed';
};
export type DocumentFormat = typeof DocumentFormat[keyof typeof DocumentFormat];

export interface DocumentWriteCapability {
  /** The only payload format the driver accepts. */
  format: string;
  /** Whether whole-document text editing is safe. */
  text: boolean;
}

export interface WriteCapabilities {
  row_writer: boolean;
  document?: DocumentWriteCapability | null;
}

/**
 * One static command entry of a query language advertisement: canonical
 * command name, a backend-native usage line, and a concise summary. All
 * three are required, nonblank, bounded, and control-free; `name` must
 * be ASCII letters/digits/underscores (the editor's token charset),
 * names are unique case-insensitively, and the list is capped so a
 * plugin can never force an unbounded completion list or handshake
 * frame.
 */
export interface QueryCommand {
  name: string;
  usage: string;
  summary: string;
}

/**
 * Query editor language for one driver: name, editor tab label, input
 * placeholder, optional lexer hint (the UI falls back when blank),
 * optional example statements the driver's parser already accepts, and
 * an optional static command catalog for completion.
 * The host normalizes an omitted/null/all-empty advertisement to the
 * legacy SQL default; a present advertisement must carry nonblank
 * name/editor_label/placeholder, nonblank examples, and valid command
 * entries (nonblank bounded control-free name/usage/summary, unique
 * case-insensitively, capped at 512).
 */
export interface QueryLanguage {
  name: string;
  editor_label: string;
  placeholder: string;
  lexer?: string;
  examples?: string[];
  commands?: QueryCommand[];
}

/**
 * One standard workspace tab a driver may explicitly advertise support
 * for: columns, indexes, foreign_keys, or diagram. Query and Browse are
 * never part of the advertisement — they keep their per-scope policy at
 * every driver. The list must be duplicate-free and bounded to the four
 * fixed values.
 */
export const StandardWorkspaceTab: {
  readonly Columns: 'columns';
  readonly Indexes: 'indexes';
  readonly ForeignKeys: 'foreign_keys';
  readonly Diagram: 'diagram';
};
export type StandardWorkspaceTab = typeof StandardWorkspaceTab[keyof typeof StandardWorkspaceTab];

/** Structured-target scope kinds a custom workspace view may serve. */
export const WorkspaceViewScope: {
  readonly Database: 'database';
  readonly Schema: 'schema';
  readonly Table: 'table';
};
export type WorkspaceViewScope = typeof WorkspaceViewScope[keyof typeof WorkspaceViewScope];

/**
 * One plain-data workspace tab a driver advertises: a stable nonblank
 * id, a human label rendered in the workspace tab row, and the scopes
 * it serves (one or more of database/schema/table). It carries no code
 * and no UI: the host owns lifecycle, rendering, input, and
 * cancellation; the driver only answers bounded table data for the
 * view. IDs and labels must be nonblank, bounded, control-free, and
 * unique case-insensitively within the list; scopes must be nonempty
 * and duplicate-free. The list is capped at 8.
 */
export interface CustomWorkspaceView {
  id: string;
  label: string;
  scopes: WorkspaceViewScope[];
}

/**
 * Optional workspace tab advertisement of one driver: the subset of
 * standard tabs it supports (columns, indexes, foreign_keys, diagram)
 * and its ordered custom plain-data views. Absent (or null) keeps the
 * host's legacy per-product tab policy exactly, so old plugins load
 * unchanged. When present it is authoritative: standard tabs are
 * filtered by the explicit advertisement and custom views are appended
 * after them in advertised order, filtered by scope.
 */
export interface WorkspaceCapability {
  standard_tabs?: StandardWorkspaceTab[];
  custom_views?: CustomWorkspaceView[];
}

export interface Capabilities {
  name: string;
  display: string;
  targets?: TargetPattern[];
  form?: FormSpec | null;
  query_language?: QueryLanguage | null;
  write_capabilities: WriteCapabilities;
  workspace?: WorkspaceCapability | null;
}

/** Connection-form values sent to buildTarget (database.FormValues). */
export interface FormValues {
  host?: string;
  port?: string;
  user?: string;
  pass?: string;
  database?: string;
  tls?: string;
  extras?: Record<string, string>;
}

// --- Shared v1 DTOs (internal/sql) ----------------------------------------

export interface DatabaseInfo {
  product: string;
  version: string;
}

export interface SchemaObject {
  database: string;
  type: string;
  name: string;
  /** Estimated row count; null when unknown. */
  row_count?: number | null;
}

export interface DocumentPayload {
  format: string;
  /** Bytes as a JSON base64 string. */
  data: string;
}

/**
 * Structured metadata for a backend-native statement. Meaningful only
 * when the accompanying statement is nonblank; a handler result carrying
 * `statement_metadata` without a nonblank statement is rejected before it
 * reaches the wire. The object is authoritative: every field is required
 * when the object is present. Omitted (or null) metadata keeps the legacy
 * defaults — replayable, not sensitive, no language.
 */
export interface StatementMetadata {
  /** Backend statement language (e.g. "redis"); empty when unknown. */
  language: string;
  /** Whether the statement may be copied/re-run/explained. */
  replayable: boolean;
  /** Whether the statement must never be stored verbatim. */
  sensitive: boolean;
}

export interface Result {
  columns: string[];
  column_types: string[];
  /** Nullable cells; the host caps display at 500 rows / 300 runes per cell. */
  rows: Array<Array<string | null>>;
  untruncated_rows: Array<Array<string | null>>;
  rows_affected: number;
  has_more: boolean;
  /** Execution time in nanoseconds. */
  duration_ns: number;
  truncated: boolean;
  /** One stable document identity per row; empty when not document-capable. */
  document_ids?: DocumentPayload[];
  /**
   * Optional backend-native statement for the operation that produced
   * this result; empty/omitted for compiled-in drivers. The host logs it
   * in place of the generic write preview when non-blank. Replayability
   * and sensitivity are described by `statement_metadata`.
   */
  statement?: string;
  /** Optional structured metadata for `statement`; rejected when `statement` is blank. */
  statement_metadata?: StatementMetadata | null;
}

/** Index kinds: 1 primary key, 2 unique, 3 regular. */
export const IndexKind: {
  readonly PrimaryKey: 1;
  readonly Unique: 2;
  readonly Regular: 3;
};
export type IndexKind = typeof IndexKind[keyof typeof IndexKind];

export interface ColumnInfo {
  name: string;
  type: string;
  attributes: string;
  nullable: boolean;
  default_value: string | null;
  primary_key: number;
  indexes: IndexKind[];
}

export interface ColumnDef {
  name: string;
  type: string;
  nullable: boolean;
  default_value: string | null;
  attributes: string | null;
}

export interface ColumnChange {
  previous_name: string;
  name: string;
  type: string;
  nullable: boolean;
  default_value: string | null;
  attributes: string | null;
}

export interface IndexInfo {
  name: string;
  unique: boolean;
  primary_key: boolean;
  columns: string[];
}

export interface IndexChange {
  name: string;
  unique: boolean;
  primary_key: boolean;
  columns: string[];
}

export interface ForeignKeyInfo {
  id: string;
  columns: string[];
  reference_table: string;
  reference_columns: string[];
  on_delete: string;
  on_update: string;
}

export interface ReferencingForeignKeyInfo extends ForeignKeyInfo {
  /** The table declaring the foreign key. */
  table: string;
}

export interface ForeignKeyChange {
  columns: string[];
  reference_table: string;
  reference_columns: string[];
  on_delete: string;
  on_update: string;
}

export interface BrowseFilter {
  column: string;
  operator: string;
  value: string;
}

export interface BrowseSort {
  column: string;
  descending: boolean;
}

export interface BrowseOptions {
  columns?: string[];
  filters?: BrowseFilter[];
  sorts?: BrowseSort[];
  offset?: number;
  limit?: number;
}

// --- Row and document writes (internal/sql/row_write.go) ------------------

/** Tagged cell payload kinds. */
export const ValueKind: {
  readonly Default: 'default';
  readonly Null: 'null';
  readonly String: 'string';
  readonly Bool: 'bool';
  readonly Integer: 'integer';
  readonly Float: 'float';
  readonly Bytes: 'bytes';
  readonly Decimal: 'decimal';
  readonly Timestamp: 'timestamp';
  readonly Array: 'array';
  readonly Object: 'object';
};
export type ValueKind = typeof ValueKind[keyof typeof ValueKind];

export interface Value {
  kind: ValueKind;
  string?: string;
  bool?: boolean;
  integer?: number;
  float?: number;
  /** Bytes as a JSON base64 string. */
  bytes?: string;
  decimal?: string;
  /** RFC 3339 timestamp. */
  timestamp?: string;
  array?: Value[];
  object?: NamedValue[];
}

export interface NamedValue {
  name: string;
  value: Value;
}

export interface RowValue {
  name: string;
  value: Value;
}

export const RowWriteOperation: {
  readonly Insert: 'insert';
  readonly Update: 'update';
  readonly Delete: 'delete';
};
export type RowWriteOperation = typeof RowWriteOperation[keyof typeof RowWriteOperation];

export interface RowWriteRequest {
  operation: RowWriteOperation;
  table: string;
  key?: RowValue[];
  values?: RowValue[];
}

export interface RowWriteResponse {
  result: {
    rows_affected: number;
    /** Optional backend-native statement for the write. */
    statement?: string;
    /** Optional structured metadata for `statement`; rejected when `statement` is blank. */
    statement_metadata?: StatementMetadata | null;
  };
}

export const DocumentWriteOperation: {
  readonly Read: 'read';
  readonly Insert: 'insert';
  readonly Replace: 'replace';
  readonly Delete: 'delete';
};
export type DocumentWriteOperation = typeof DocumentWriteOperation[keyof typeof DocumentWriteOperation];

export interface DocumentWriteRequest {
  operation: DocumentWriteOperation;
  collection: string;
  id?: DocumentPayload | null;
  document?: DocumentPayload | null;
}

export interface DocumentWriteResponse {
  result: {
    rows_affected: number;
    /** Optional backend-native statement for the write. */
    statement?: string;
    /** Optional structured metadata for `statement`; rejected when `statement` is blank. */
    statement_metadata?: StatementMetadata | null;
  };
  /** Set for read operations. */
  document?: DocumentPayload | null;
}

// --- Handler contracts -----------------------------------------------------

/** Aborts when the host cancels the request (perk/v1/cancel). */
export interface HandlerContext {
  signal: AbortSignal;
}

export interface BuildTargetResult {
  target: string;
  ok: boolean;
}

export interface OpenResult {
  info: DatabaseInfo;
  service: SessionService;
}

/** perk/v1 session request DTOs — the wire params minus session_id. */
export interface StatementRequest {
  statement: string;
}
export interface TableRequest {
  table: string;
}
export interface IndexChangeRequest {
  table: string;
  change: IndexChange;
}
export interface ReplaceIndexRequest {
  table: string;
  old_name: string;
  change: IndexChange;
}
export interface DropRequest {
  table: string;
  name: string;
}
export interface ForeignKeyChangeRequest {
  table: string;
  change: ForeignKeyChange;
}
export interface ReplaceForeignKeyRequest {
  table: string;
  old_name: string;
  change: ForeignKeyChange;
}
export interface ColumnChangeRequest {
  table: string;
  change: ColumnChange;
}
export interface AddColumnRequest {
  table: string;
  def: ColumnDef;
}
export interface BrowseTableRequest {
  table: string;
  options: BrowseOptions;
}

/**
 * The active structured target of one workspace view request: the scope
 * kind plus the identifiers the kind needs. `kind` must be database,
 * schema, or table.
 */
export interface WorkspaceViewTarget {
  kind: WorkspaceViewScope;
  database?: string;
  schema?: string;
  table?: string;
}

/** One plain-data custom workspace view request (params minus session_id). */
export interface WorkspaceViewRequest {
  view_id: string;
  target: WorkspaceViewTarget;
}

export type EmptyRequest = Record<string, never>;

/**
 * One open database session. The SDK routes every session-bound perk/v1
 * method to the matching handler with the wire params minus session_id.
 * All mandatory handlers must be implemented; the optional write
 * handlers must match the advertised write_capabilities (enforced when
 * the session is opened).
 */
export interface SessionService {
  execute(request: StatementRequest, context: HandlerContext): Result | Promise<Result>;
  executeReadOnly(request: StatementRequest, context: HandlerContext): Result | Promise<Result>;
  validate(request: StatementRequest, context: HandlerContext): void | Promise<void>;
  listSchema(request: EmptyRequest, context: HandlerContext): SchemaObject[] | Promise<SchemaObject[]>;
  tableInfo(request: TableRequest, context: HandlerContext): ColumnInfo[] | Promise<ColumnInfo[]>;
  listIndexes(request: TableRequest, context: HandlerContext): IndexInfo[] | Promise<IndexInfo[]>;
  createIndex(request: IndexChangeRequest, context: HandlerContext): void | Promise<void>;
  replaceIndex(request: ReplaceIndexRequest, context: HandlerContext): void | Promise<void>;
  dropIndex(request: DropRequest, context: HandlerContext): void | Promise<void>;
  listForeignKeys(request: TableRequest, context: HandlerContext): ForeignKeyInfo[] | Promise<ForeignKeyInfo[]>;
  listReferencingForeignKeys(request: TableRequest, context: HandlerContext): ReferencingForeignKeyInfo[] | Promise<ReferencingForeignKeyInfo[]>;
  listForeignKeysAll(request: EmptyRequest, context: HandlerContext): Record<string, ForeignKeyInfo[]> | Promise<Record<string, ForeignKeyInfo[]>>;
  listIndexesAll(request: EmptyRequest, context: HandlerContext): Record<string, IndexInfo[]> | Promise<Record<string, IndexInfo[]>>;
  createForeignKey(request: ForeignKeyChangeRequest, context: HandlerContext): void | Promise<void>;
  replaceForeignKey(request: ReplaceForeignKeyRequest, context: HandlerContext): void | Promise<void>;
  dropForeignKey(request: DropRequest, context: HandlerContext): void | Promise<void>;
  alterColumn(request: ColumnChangeRequest, context: HandlerContext): void | Promise<void>;
  dropColumn(request: DropRequest, context: HandlerContext): void | Promise<void>;
  addColumn(request: AddColumnRequest, context: HandlerContext): void | Promise<void>;
  browseTable(request: BrowseTableRequest, context: HandlerContext): Result | Promise<Result>;
  /**
   * Optional custom workspace view, required when capabilities.workspace
   * .custom_views is a non-empty array and rejected when it is not.
   * Returns bounded table data for one advertised view and the active
   * structured target.
   */
  workspaceView?(request: WorkspaceViewRequest, context: HandlerContext): Result | Promise<Result>;
  /**
   * Optional row writer, required when capabilities.write_capabilities
   * .row_writer is true and rejected when it is not.
   */
  rowWrite?(request: RowWriteRequest, context: HandlerContext): RowWriteResponse | Promise<RowWriteResponse>;
  /**
   * Optional document writer, required when capabilities.write_capabilities
   * .document is non-null and rejected when it is null.
   */
  documentWrite?(request: DocumentWriteRequest, context: HandlerContext): DocumentWriteResponse | Promise<DocumentWriteResponse>;
  /** Called once by the SDK when the host closes this session. */
  close?(): void | Promise<void>;
}

// --- Public entry points ---------------------------------------------------

export interface PluginDefinition {
  capabilities: Capabilities;
  /** Serialize one connection form into the opener target body. */
  buildTarget(values: FormValues, context: HandlerContext): BuildTargetResult | Promise<BuildTargetResult>;
  /** Open a session; the SDK assigns its session_id and owns its lifetime. */
  open(target: string, context: HandlerContext): OpenResult | Promise<OpenResult>;
}

export interface PluginServer {
  /** Resolves when the server terminates: input EOF, protocol violation, or close(). */
  readonly closed: Promise<void>;
  /** Terminate the server, aborting every in-flight request. Idempotent. */
  close(): Promise<void>;
}

export interface PluginServerOptions {
  input: NodeJS.ReadableStream;
  output: NodeJS.WritableStream;
}

/** Stable plugin operation-error kinds, mirroring the Go host's plugin.Kind enum. */
export const ErrorKind: {
  readonly Validation: 'validation';
  readonly Authentication: 'authentication';
  readonly Connection: 'connection';
  readonly Operation: 'operation';
  readonly Unsupported: 'unsupported';
  readonly Cancelled: 'cancelled';
  readonly Protocol: 'protocol';
  readonly PluginCrash: 'plugin_crash';
};
export type ErrorKind = typeof ErrorKind[keyof typeof ErrorKind];

export interface PluginOperationErrorOptions {
  /** JSON-RPC error code; defaults to -32000 (server error). */
  code?: number;
  /** Stable failure kind; unknown or blank kinds normalize to operation. */
  kind?: ErrorKind;
  /** Advisory plugin identity; the host uses its own handshake identity. */
  plugin?: string;
  /** Advisory method name; the server fills in the wire method when absent. */
  method?: string;
  /**
   * Optional advisory hint explaining the failure. Non-control: the host
   * renders it separately from the error and never acts on it. Blank
   * values are omitted from the wire.
   */
  hint?: string;
  /**
   * Optional advisory statement the user may try instead of the failed
   * one. Non-control: the host never executes it and never merges it
   * into the error identity or message. Blank values are omitted from
   * the wire.
   */
  suggested_statement?: string;
}

/**
 * Structured plugin operation error a handler can throw. The server
 * replies with its code and message plus an optional `data` object
 * carrying kind/plugin/method provenance and optional advisory
 * hint/suggested_statement guidance. Generic thrown errors keep the
 * legacy -32603 mapping and carry no data.
 */
export class PluginOperationError extends Error {
  readonly name: 'PluginOperationError';
  readonly code: number;
  readonly kind: ErrorKind;
  readonly plugin?: string;
  readonly method?: string;
  readonly hint?: string;
  readonly suggested_statement?: string;
  constructor(message: string, options?: PluginOperationErrorOptions);
}

/** Error thrown for a request canceled by the host; replies with -32800 and `data.kind: "cancelled"`. */
export class RequestCancelledError extends Error {
  readonly name: 'RequestCancelledError';
  readonly code: -32800;
  constructor(message?: string);
}

/**
 * Start the perk/v1 JSON-RPC server over the given streams. Requests are
 * NDJSON frames; malformed, invalid-UTF-8, or oversized input terminates
 * the server. stdout carries protocol frames only.
 */
export function createPluginServer(definition: PluginDefinition, options: PluginServerOptions): PluginServer;
