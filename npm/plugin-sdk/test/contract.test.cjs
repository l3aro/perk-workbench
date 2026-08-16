'use strict';

// Contract conformance for the canonical machine-readable perk/v1
// contract (protocol/perk-v1 at the repository root). Loads the exact
// schema, manifest, and fixture files — no copies — and proves the SDK's
// runtime constants and method surface align with the schema while
// driving canonical frames through createPluginServer over in-memory
// streams.

const test = require('node:test');
const assert = require('node:assert/strict');
const { readFile, readdir } = require('node:fs/promises');
const { join } = require('node:path');

const sdk = require('../index.cjs');
const { createServer, serviceStub, waitForFrames, delay } = require('./helpers.cjs');

const perkV1 = join(__dirname, '..', '..', '..', 'protocol', 'perk-v1');
const fixturesDir = join(perkV1, 'fixtures');

async function loadContract() {
  const schema = JSON.parse(await readFile(join(perkV1, 'schema.json'), 'utf8'));
  const manifest = JSON.parse(await readFile(join(fixturesDir, 'manifest.json'), 'utf8'));
  const entries = new Map();
  for (const entry of manifest.fixtures) entries.set(entry.file, entry);
  const fixtures = new Map();
  for (const file of entries.keys()) {
    fixtures.set(file, JSON.parse(await readFile(join(fixturesDir, file), 'utf8')));
  }
  return { schema, manifest, entries, fixtures };
}

function refsIn(node, refs) {
  if (Array.isArray(node)) {
    for (const item of node) refsIn(item, refs);
    return;
  }
  if (node === null || typeof node !== 'object') return;
  if (typeof node.$ref === 'string') refs.push(node.$ref);
  for (const value of Object.values(node)) refsIn(value, refs);
}

test('schema, manifest, and every fixture parse as JSON with a coherent structure', async () => {
  const { schema, manifest, entries, fixtures } = await loadContract();

  assert.equal(schema.$schema, 'https://json-schema.org/draft/2020-12/schema');
  assert.equal(typeof schema.$id, 'string');
  assert.ok(schema.$id.length > 0);
  assert.equal(typeof schema.title, 'string');
  assert.ok(schema.title.length > 0);
  assert.equal(schema.version, 1);
  assert.equal(typeof schema.$defs, 'object');
  assert.notEqual(schema.$defs, null);

  // Every $ref in the schema resolves against $defs.
  const refs = [];
  refsIn(schema, refs);
  for (const ref of refs) {
    assert.match(ref, /^#\/\$defs\//);
    const name = ref.slice('#/$defs/'.length);
    assert.ok(name in schema.$defs, `unresolved $ref ${ref}`);
  }

  // The methods registry is exhaustive and unambiguous.
  const methods = schema.$defs.methods;
  const names = Object.keys(methods.properties);
  assert.equal(new Set(names).size, names.length, 'duplicate method names');
  assert.deepEqual([...names].sort(), [...methods.propertyNames.enum].sort());
  for (const name of names) {
    assert.equal(typeof name, 'string');
    assert.match(name, /^perk\/v1\//);
  }

  // Envelope invariants: jsonrpc const, integer ids, exact method.
  const request = schema.$defs.request;
  assert.deepEqual(request.required, ['jsonrpc', 'id', 'method', 'params']);
  assert.equal(request.properties.jsonrpc.const, '2.0');
  assert.equal(request.properties.id.$ref, '#/$defs/uint64');
  assert.deepEqual(request.properties.method.enum, names);
  const notification = schema.$defs.notification;
  assert.equal(notification.properties.method.const, 'perk/v1/cancel');
  assert.equal(notification.properties.params.$ref, '#/$defs/cancelParams');
  assert.deepEqual(schema.$defs.success.required, ['jsonrpc', 'id', 'result']);
  assert.equal(schema.$defs.success.properties.error, false);
  assert.deepEqual(schema.$defs.error.required, ['jsonrpc', 'id', 'error']);
  assert.equal(schema.$defs.error.properties.result, false);

  // The manifest names exactly the fixture files present, and each entry
  // references a resolvable envelope $def.
  const files = (await readdir(fixturesDir)).filter((file) => file !== 'manifest.json');
  assert.deepEqual([...entries.keys()].sort(), [...files].sort());
  assert.equal(fixtures.size, entries.size);
  const envelopeRefs = new Set(['#/$defs/request', '#/$defs/notification', '#/$defs/success', '#/$defs/error']);
  for (const entry of entries.values()) {
    assert.ok(envelopeRefs.has(entry.ref), `${entry.file}: ref ${entry.ref} is not an envelope $def`);
    assert.equal(typeof entry.valid, 'boolean');
    if (entry.method !== undefined && entry.valid) {
      // An invalid fixture may deliberately carry a non-protocol
      // method (unknown-method frames); a valid one must not.
      assert.ok(names.includes(entry.method), `${entry.file}: method ${entry.method} is not in the schema`);
    }
    if (entry.ref === '#/$defs/error' && entry.valid) {
      assert.equal(typeof entry.code, 'number', `${entry.file}: valid error frame needs an expected code`);
    }
  }
});

test('schema enums match the SDK runtime constants', async () => {
  const { schema } = await loadContract();
  const defs = schema.$defs;
  assert.deepEqual(defs.formFieldKind.enum, Object.values(sdk.FormFieldKind));
  assert.deepEqual(defs.formValidation.enum, Object.values(sdk.FormValidation));
  assert.deepEqual(defs.indexKind.enum, Object.values(sdk.IndexKind));
  assert.deepEqual(defs.valueKind.enum, Object.values(sdk.ValueKind));
  assert.deepEqual(defs.rowWriteOperation.enum, Object.values(sdk.RowWriteOperation));
  assert.deepEqual(defs.documentWriteOperation.enum, Object.values(sdk.DocumentWriteOperation));
  assert.equal(defs.documentFormat.const, sdk.DocumentFormat.MongoExtendedJSON);
  assert.deepEqual(defs.kind.enum, Object.values(sdk.ErrorKind));
});

test('every schema method is served at runtime, and cancel is a notification', async () => {
  const { schema } = await loadContract();
  const names = Object.keys(schema.$defs.methods.properties);
  const definition = {
    capabilities: {
      name: 'kv',
      display: 'KV',
      targets: [{ prefix: 'kv:' }],
      write_capabilities: {
        row_writer: true,
        document: { format: sdk.DocumentFormat.MongoExtendedJSON, text: true },
      },
      workspace: {
        standard_tabs: ['columns', 'indexes', 'foreign_keys', 'diagram'],
        custom_views: [{ id: 'keys', label: 'Keys', scopes: ['database', 'schema', 'table'] }],
      },
    },
    buildTarget: async () => ({ target: 'kv:x', ok: true }),
    open: async () => ({
      info: { product: 'KV', version: '1' },
      service: serviceStub({
        rowWrite: async () => ({ result: { rows_affected: 0 } }),
        documentWrite: async () => ({ result: { rows_affected: 0 } }),
        workspaceView: async () => ({ rows_affected: 0 }),
      }),
    }),
  };
  const { server, client } = createServer(definition);
  await client.initialize();
  const ordered = [...names].filter((name) => name !== 'perk/v1/initialize' && name !== 'perk/v1/cancel');
  // close removes the session; every other session call needs it alive.
  ordered.splice(ordered.indexOf('perk/v1/close'), 1);
  ordered.push('perk/v1/close');
  for (const method of ordered) {
    try {
      const result = await client.request(method, paramsFor(method));
      assert.notEqual(result, undefined, `${method} must answer with a result`);
    } catch (error) {
      assert.fail(`${method} rejected with code ${error.code}: ${error.message}`);
    }
  }
  const before = client.frames.length;
  client.notify('perk/v1/cancel', { id: 999 });
  await delay(20);
  assert.equal(client.frames.length, before, 'cancel notification must not produce a response');
  await server.close();
});

function paramsFor(method) {
  const session = { session_id: 1 };
  switch (method) {
    case 'perk/v1/build_target':
      return {};
    case 'perk/v1/open':
      return { target: 'kv:x' };
    case 'perk/v1/close':
      return session;
    case 'perk/v1/execute':
    case 'perk/v1/execute_read_only':
    case 'perk/v1/validate':
      return { ...session, statement: 'GET x' };
    case 'perk/v1/list_schema':
    case 'perk/v1/list_foreign_keys_all':
    case 'perk/v1/list_indexes_all':
      return session;
    case 'perk/v1/table_info':
    case 'perk/v1/list_indexes':
    case 'perk/v1/list_foreign_keys':
    case 'perk/v1/list_referencing_foreign_keys':
      return { ...session, table: 'kv' };
    case 'perk/v1/create_index':
    case 'perk/v1/create_foreign_key':
    case 'perk/v1/alter_column':
      return { ...session, table: 'kv', change: {} };
    case 'perk/v1/replace_index':
    case 'perk/v1/replace_foreign_key':
      return { ...session, table: 'kv', old_name: 'a', change: {} };
    case 'perk/v1/drop_index':
    case 'perk/v1/drop_foreign_key':
    case 'perk/v1/drop_column':
      return { ...session, table: 'kv', name: 'a' };
    case 'perk/v1/add_column':
      return { ...session, table: 'kv', def: {} };
    case 'perk/v1/browse_table':
      return { ...session, table: 'kv', options: {} };
    case 'perk/v1/row_write':
    case 'perk/v1/document_write':
      return { ...session, request: {} };
    case 'perk/v1/workspace_view':
      return { ...session, view_id: 'keys', target: { kind: 'table', table: 'kv' } };
    default:
      throw new Error(`no params for ${method}`);
  }
}

test('canonical fixture frames drive through createPluginServer', async () => {
  const { fixtures, entries } = await loadContract();
  const capabilityFixture = fixtures.get('success-initialize.json');
  const definition = {
    capabilities: capabilityFixture.result.capabilities,
    buildTarget: async () => ({ target: 'redis:x', ok: true }),
    open: async () => ({
      info: { product: 'Redis (plugin)', version: '7.2.0' },
      service: serviceStub({
        rowWrite: async () => ({ result: { rows_affected: 0 } }),
        documentWrite: async () => ({ result: { rows_affected: 0 } }),
        workspaceView: async () => ({ rows_affected: 0 }),
      }),
    }),
  };
  const { server, client } = createServer(definition);

  // Initialize with the canonical request frame; the answer must carry
  // the canonical capabilities verbatim (the SDK normalizes, it never
  // invents fields).
  client.input.write(`${JSON.stringify(fixtures.get('request-initialize.json'))}\n`);
  await waitForFrames(client, 1);
  const handshake = client.frames[0];
  assert.equal(handshake.error, undefined);
  assert.equal(handshake.result.protocol_version, 1);
  assert.deepEqual(handshake.result.capabilities, capabilityFixture.result.capabilities);

  // The canonical execute frame targets session_id 7; open sessions
  // until that id exists. Frames accumulate: 1 (initialize) + 7 (opens).
  for (let id = 1; id <= 7; id++) {
    await client.request('perk/v1/open', { target: 'redis:x' });
  }
  client.input.write(`${JSON.stringify(fixtures.get('request-execute.json'))}\n`);
  await waitForFrames(client, 9);
  assert.equal(client.frames[8].error, undefined);
  assert.equal(client.frames[8].result, null);

  // Canonical write frames reach the advertised write handlers.
  client.input.write(`${JSON.stringify(fixtures.get('request-row-write.json'))}\n`);
  await waitForFrames(client, 10);
  assert.equal(client.frames[9].error, undefined);
  client.input.write(`${JSON.stringify(fixtures.get('request-document-write.json'))}\n`);
  await waitForFrames(client, 11);
  assert.equal(client.frames[10].error, undefined);

  // The canonical workspace-view frame reaches the advertised custom
  // view handler with session_id 7 stripped.
  client.input.write(`${JSON.stringify(fixtures.get('request-workspace-view.json'))}\n`);
  await waitForFrames(client, 12);
  assert.equal(client.frames[11].error, undefined);

  // The canonical cancel notification never gets a response.
  client.input.write(`${JSON.stringify(fixtures.get('notification-cancel.json'))}\n`);
  await delay(20);
  assert.equal(client.frames.length, 12);

  // Parseable invalid frames are answered with the manifest's expected
  // JSON-RPC codes, and the server keeps serving.
  const invalidFiles = ['invalid-request-jsonrpc.json', 'invalid-request-string-id.json', 'invalid-request-float-id.json', 'invalid-request-unknown-method.json'];
  for (const file of invalidFiles) {
    client.input.write(`${JSON.stringify(fixtures.get(file))}\n`);
  }
  await waitForFrames(client, 12 + invalidFiles.length);
  for (let i = 0; i < invalidFiles.length; i++) {
    const frame = client.frames[12 + i];
    const entry = entries.get(invalidFiles[i]);
    assert.equal(frame.error.code, entry.code, `${invalidFiles[i]} must answer ${entry.code}`);
    assert.equal(frame.error.data, undefined);
  }
  // Still healthy after the rejections.
  const result = await client.request('perk/v1/build_target', { host: 'h' });
  assert.deepEqual(result, { target: 'redis:x', ok: true });
  await server.close();
});
