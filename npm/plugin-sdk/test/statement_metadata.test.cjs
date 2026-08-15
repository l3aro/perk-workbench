'use strict';

const test = require('node:test');
const assert = require('node:assert/strict');

const { createServer, minimalDefinition, serviceStub } = require('./helpers.cjs');

// statement_metadata mirrors the host's plugin-boundary rule: the object
// is meaningful only with a nonblank statement, and when present it is
// authoritative — every field must be present with the right type.
// Omission and explicit false pass through verbatim.

const METADATA = { language: 'redis', replayable: false, sensitive: false };

function executeServer(execute) {
  return createServer(
    minimalDefinition({
      open: async () => ({
        info: { product: 'KV', version: '1' },
        service: serviceStub({ execute }),
      }),
    }),
  );
}

test('execute passes statement_metadata through with explicit false preserved', async () => {
  const { server, client } = executeServer(async () => ({
    rows_affected: 1,
    statement: 'SET key 1',
    statement_metadata: METADATA,
  }));
  await client.initialize();
  await client.request('perk/v1/open', { target: 'kv:x' });
  const executed = await client.request('perk/v1/execute', { session_id: 1, statement: 'SET key 1' });
  assert.deepEqual(executed.statement_metadata, METADATA);
  assert.ok('replayable' in executed.statement_metadata && executed.statement_metadata.replayable === false);
  await server.close();
});

test('execute keeps an omitted statement_metadata off the wire', async () => {
  const { server, client } = executeServer(async () => ({ rows_affected: 1, statement: 'SET key 1' }));
  await client.initialize();
  await client.request('perk/v1/open', { target: 'kv:x' });
  const executed = await client.request('perk/v1/execute', { session_id: 1, statement: 'SET key 1' });
  assert.deepEqual(executed, { rows_affected: 1, statement: 'SET key 1' });
  assert.ok(!('statement_metadata' in executed));
  await server.close();
});

test('execute treats a null statement_metadata as omitted', async () => {
  const { server, client } = executeServer(async () => ({ rows_affected: 1, statement: 'SET key 1', statement_metadata: null }));
  await client.initialize();
  await client.request('perk/v1/open', { target: 'kv:x' });
  const executed = await client.request('perk/v1/execute', { session_id: 1, statement: 'SET key 1' });
  assert.deepEqual(executed.statement_metadata, null);
  await server.close();
});

test('execute rejects orphan statement_metadata without a nonblank statement', async () => {
  const { server, client } = executeServer(async () => ({ rows_affected: 1, statement_metadata: METADATA }));
  await client.initialize();
  await client.request('perk/v1/open', { target: 'kv:x' });
  await assert.rejects(
    client.request('perk/v1/execute', { session_id: 1, statement: 'SET key 1' }),
    (error) => error.code === -32603 && /statement_metadata requires a nonblank statement/.test(error.message),
  );
  await server.close();
});

test('execute rejects an orphan statement_metadata with a blank statement', async () => {
  const { server, client } = executeServer(async () => ({ rows_affected: 1, statement: '   ', statement_metadata: METADATA }));
  await client.initialize();
  await client.request('perk/v1/open', { target: 'kv:x' });
  await assert.rejects(
    client.request('perk/v1/execute', { session_id: 1, statement: 'SET key 1' }),
    (error) => error.code === -32603 && /statement_metadata requires a nonblank statement/.test(error.message),
  );
  await server.close();
});

test('execute rejects a partial statement_metadata object', async () => {
  const { server, client } = executeServer(async () => ({
    rows_affected: 1,
    statement: 'SET key 1',
    statement_metadata: { language: 'redis' },
  }));
  await client.initialize();
  await client.request('perk/v1/open', { target: 'kv:x' });
  await assert.rejects(
    client.request('perk/v1/execute', { session_id: 1, statement: 'SET key 1' }),
    (error) => error.code === -32603 && /statement_metadata\.replayable must be a boolean/.test(error.message),
  );
  await server.close();
});

test('execute rejects a non-object statement_metadata', async () => {
  const { server, client } = executeServer(async () => ({ rows_affected: 1, statement: 'SET key 1', statement_metadata: 'redis' }));
  await client.initialize();
  await client.request('perk/v1/open', { target: 'kv:x' });
  await assert.rejects(
    client.request('perk/v1/execute', { session_id: 1, statement: 'SET key 1' }),
    (error) => error.code === -32603 && /statement_metadata must be an object/.test(error.message),
  );
  await server.close();
});

test('execute rejects a mistyped statement_metadata field', async () => {
  const { server, client } = executeServer(async () => ({
    rows_affected: 1,
    statement: 'SET key 1',
    statement_metadata: { language: 'redis', replayable: 'yes', sensitive: false },
  }));
  await client.initialize();
  await client.request('perk/v1/open', { target: 'kv:x' });
  await assert.rejects(
    client.request('perk/v1/execute', { session_id: 1, statement: 'SET key 1' }),
    (error) => error.code === -32603 && /statement_metadata\.replayable must be a boolean/.test(error.message),
  );
  await server.close();
});

test('rowWrite passes statement_metadata through in the result envelope', async () => {
  const { server, client } = createServer(
    minimalDefinition({
      capabilities: {
        name: 'kv',
        display: 'KV',
        targets: [{ prefix: 'kv:' }],
        write_capabilities: { row_writer: true },
      },
      open: async () => ({
        info: { product: 'KV', version: '1' },
        service: serviceStub({ rowWrite: async () => ({ result: { rows_affected: 1, statement: 'SET key 1', statement_metadata: METADATA } }) }),
      }),
    }),
  );
  await client.initialize();
  await client.request('perk/v1/open', { target: 'kv:x' });
  const response = await client.request('perk/v1/row_write', {
    session_id: 1,
    request: { operation: 'insert', table: 'kv', values: [] },
  });
  assert.deepEqual(response.result.statement_metadata, METADATA);
  await server.close();
});

test('rowWrite rejects orphan statement_metadata in the result envelope', async () => {
  const { server, client } = createServer(
    minimalDefinition({
      capabilities: {
        name: 'kv',
        display: 'KV',
        targets: [{ prefix: 'kv:' }],
        write_capabilities: { row_writer: true },
      },
      open: async () => ({
        info: { product: 'KV', version: '1' },
        service: serviceStub({ rowWrite: async () => ({ result: { rows_affected: 1, statement_metadata: METADATA } }) }),
      }),
    }),
  );
  await client.initialize();
  await client.request('perk/v1/open', { target: 'kv:x' });
  await assert.rejects(
    client.request('perk/v1/row_write', { session_id: 1, request: { operation: 'insert', table: 'kv', values: [] } }),
    (error) => error.code === -32603 && /statement_metadata requires a nonblank statement/.test(error.message),
  );
  await server.close();
});
