# perk-workbench-plugin-sdk

Author SDK for building external database plugins for [Perk Workbench](https://github.com/l3aro/perk-workbench).

The SDK implements the plugin side of the `perk/v1` JSON-RPC 2.0 stdio protocol:
newline-delimited JSON (NDJSON), 16 MiB frame bound, UTF-8 encoded, with numeric request IDs and structured operation errors.

- **Zero dependencies**: Uses Node.js standard library only (`node:stream`, `node:events`).
- **Engine**: Requires Node >= 18.
- **Full Type Definitions**: Comprehensive TypeScript declarations in `index.d.ts`.

## Installation

```sh
npm install perk-workbench-plugin-sdk
```

## Quick Start

Create an executable file (e.g. `perk-my-plugin.js`):

```javascript
#!/usr/bin/env node
'use strict';

const {
  createPluginServer,
  PluginOperationError,
  ErrorKind,
  IndexKind,
  RequestCancelledError
} = require('perk-workbench-plugin-sdk');

const store = new Map([
  ['greeting', 'Welcome to Node Perk plugin!']
]);

function makeResult(columns, columnTypes, rows, durationNs = 0, affected = 0) {
  return {
    columns,
    column_types: columnTypes,
    rows,
    untruncated_rows: rows,
    rows_affected: affected,
    has_more: false,
    duration_ns: durationNs,
    truncated: false
  };
}

const definition = {
  capabilities: {
    name: 'node-kv',
    display: 'Node KV Store',
    targets: [{ prefix: 'nodekv:' }],
    query_language: {
      name: 'KV',
      editor_label: 'KV',
      placeholder: 'GET key, SET key val, KEYS...',
      lexer: 'bash',
      commands: [
        { name: 'GET', usage: 'GET <key>', summary: 'Fetch value for key' },
        { name: 'SET', usage: 'SET <key> <val>', summary: 'Set value for key' },
        { name: 'KEYS', usage: 'KEYS', summary: 'List all stored keys' }
      ]
    },
    write_capabilities: {
      row_writer: false
    }
  },

  buildTarget: async (values) => ({
    target: `nodekv:${values.host || 'local'}`,
    ok: true
  }),

  open: async (target, { signal }) => ({
    info: { product: 'NodeKV', version: '1.0.0' },
    service: {
      execute: async ({ statement }, { signal }) => {
        const start = process.hrtime.bigint();
        const parts = statement.trim().split(/\s+/);
        const cmd = parts[0]?.toUpperCase();

        if (cmd === 'GET') {
          const val = store.get(parts[1]);
          const duration = Number(process.hrtime.bigint() - start);
          return makeResult(
            ['value'],
            ['TEXT'],
            [[val !== undefined ? val : '(nil)']],
            duration
          );
        }
        if (cmd === 'SET') {
          store.set(parts[1], parts.slice(2).join(' '));
          const duration = Number(process.hrtime.bigint() - start);
          return makeResult(['status'], ['TEXT'], [['OK']], duration, 1);
        }
        if (cmd === 'KEYS') {
          const keys = [...store.keys()].sort();
          const duration = Number(process.hrtime.bigint() - start);
          return makeResult(
            ['key'],
            ['TEXT'],
            keys.map((k) => [k]),
            duration
          );
        }

        throw new PluginOperationError(`Unknown command: ${cmd}`, {
          kind: ErrorKind.Validation
        });
      },

      executeReadOnly: async (req, ctx) => {
        if (req.statement.trim().toUpperCase().startsWith('SET')) {
          throw new PluginOperationError('SET is rejected in read-only mode', {
            kind: ErrorKind.Validation
          });
        }
        const opened = await definition.open(target, ctx);
        return opened.service.execute(req, ctx);
      },

      validate: async ({ statement }) => {
        const cmd = statement.trim().split(/\s+/)[0]?.toUpperCase();
        if (!['GET', 'SET', 'KEYS', ''].includes(cmd)) {
          throw new PluginOperationError(`Invalid command: ${cmd}`, {
            kind: ErrorKind.Validation
          });
        }
      },

      listSchema: async () => [
        { database: '', type: 'table', name: 'kv_data', row_count: store.size }
      ],

      tableInfo: async () => [
        {
          name: 'key',
          type: 'TEXT',
          attributes: '',
          nullable: false,
          default_value: null,
          primary_key: 1,
          indexes: [IndexKind.PrimaryKey]
        },
        {
          name: 'value',
          type: 'TEXT',
          attributes: '',
          nullable: false,
          default_value: null,
          primary_key: 0,
          indexes: []
        }
      ],

      browseTable: async ({ options }) => {
        const start = process.hrtime.bigint();
        const keys = [...store.keys()].sort();
        const slice = keys.slice(options.offset, options.offset + options.limit);
        const duration = Number(process.hrtime.bigint() - start);
        return makeResult(
          ['key', 'value'],
          ['TEXT', 'TEXT'],
          slice.map((k) => [k, store.get(k)]),
          duration
        );
      },

      listIndexes: async () => [],
      createIndex: async () => {},
      replaceIndex: async () => {},
      dropIndex: async () => {},
      listForeignKeys: async () => [],
      listReferencingForeignKeys: async () => [],
      listForeignKeysAll: async () => ({}),
      listIndexesAll: async () => ({}),
      createForeignKey: async () => {},
      replaceForeignKey: async () => {},
      dropForeignKey: async () => {},
      alterColumn: async () => {},
      dropColumn: async () => {},
      addColumn: async () => {},
      close: async () => {}
    }
  })
};

createPluginServer(definition, {
  input: process.stdin,
  output: process.stdout
});
```

Make the script executable:

```sh
chmod +x perk-my-plugin.js
```

## API Reference

### `createPluginServer(definition, options)`

Starts the perk/v1 protocol server over standard input and output.

- `definition` (`PluginDefinition`):
  - `capabilities` (`Capabilities`): Plugin metadata, target prefixes, query language, write capabilities, and custom workspace views.
  - `buildTarget(values, ctx)`: Builds a connection target string from connection form values (`host`, `port`, `user`, `pass`, `database`, `tls`, `extras`).
  - `open(target, ctx)`: Connects to target, returning `{ info: DatabaseInfo, service: SessionService }`.
- `options` (`PluginServerOptions`):
  - `input` (`ReadableStream`): Host input stream (default: `process.stdin`).
  - `output` (`WritableStream`): Host output stream (default: `process.stdout`).

### Result Structure and Display Bounds

The host and Node SDK do not automatically truncate or mutate output returned by `execute` or `browseTable`. To conform with the display contract:
- `rows` should contain display-safe formatted cell strings (capped at 500 rows and 300 runes per cell).
- `untruncated_rows` must be parallel to `rows`, holding the full raw strings so the TUI cell viewer modal and exports have access to complete values.
- `duration_ns` is the execution duration in nanoseconds.

### Error Handling

Throw `PluginOperationError` to return a structured error to the host:

```javascript
throw new PluginOperationError('Table not found', {
  code: -32000,
  kind: ErrorKind.Operation,
  hint: 'Check table name in schema sidebar',
  suggested_statement: 'SHOW TABLES'
});
```

Available `ErrorKind` values:
- `ErrorKind.Validation`: Statement syntax or validation failure.
- `ErrorKind.Authentication`: Bad credentials or denied access.
- `ErrorKind.Connection`: Host unreachable or network drop.
- `ErrorKind.Operation`: General execution failure.
- `ErrorKind.Unsupported`: Unsupported backend feature.
- `ErrorKind.Cancelled`: Query canceled by user.

### Handling Cancellation

When the user presses `Escape` during query execution, the host sends a `perk/v1/cancel` notification.
The SDK triggers the `signal` (`AbortSignal`) on the handler's `context` object:

```javascript
execute: async ({ statement }, { signal }) => {
  if (signal.aborted) {
    throw new RequestCancelledError();
  }

  // Pass signal to database client or check abort status:
  const result = await db.query(statement, { signal });
  return formatResult(result);
}
```

### Logging Rule: Standard Error Only

`stdout` is strictly reserved for JSON-RPC 2.0 frames. Writing plain text to `stdout` (`console.log`) will corrupt the NDJSON stream and cause the workbench to terminate the plugin immediately. Always use `console.error` for debug logging or telemetry:

```javascript
console.error('[debug] connection opened to', target);
```

## Testing Your Plugin

Perk Workbench ships with a built-in conformance runner that verifies all 16 protocol framing, handshake, and transport edge cases:

```sh
perk-workbench plugin test ./perk-my-plugin.js
```

You can inspect capabilities and lifecycle:

```sh
perk-workbench plugin inspect ./perk-my-plugin.js
```

When all tests pass, preview and approve it in your configuration:

```sh
# Preview SHA-256 fingerprint:
perk-workbench plugin add ./perk-my-plugin.js

# Pin and enable in ~/.config/perk-workbench/config.json:
perk-workbench plugin add --approve <SHA256> ./perk-my-plugin.js
```
