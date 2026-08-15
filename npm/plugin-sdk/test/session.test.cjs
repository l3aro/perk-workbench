'use strict';

const test = require('node:test');
const assert = require('node:assert/strict');

const { createServer, minimalDefinition, serviceStub } = require('./helpers.cjs');

const sampleResult = {
  columns: ['id', 'name'],
  column_types: ['INTEGER', 'TEXT'],
  rows: [['1', null]],
  untruncated_rows: [['1', null]],
  rows_affected: 0,
  has_more: false,
  duration_ns: 123,
  truncated: false,
};

test('open, list_schema and execute round trip through one session', async () => {
  let closeCalls = 0;
  const schema = [{ database: '', type: 'table', name: 'kv', row_count: 3 }];
  const service = serviceStub({
    execute: async (request) => {
      assert.deepEqual(request, { statement: 'select 1' });
      return sampleResult;
    },
    listSchema: async (request) => {
      assert.deepEqual(request, {});
      return schema;
    },
    close: async () => {
      closeCalls += 1;
    },
  });
  const { server, client } = createServer(
    minimalDefinition({
      open: async (target) => {
        assert.equal(target, 'kv:svc');
        return { info: { product: 'KV', version: '1.2' }, service };
      },
    }),
  );
  await client.initialize();
  const opened = await client.request('perk/v1/open', { target: 'kv:svc' });
  assert.deepEqual(opened, { session_id: 1, info: { product: 'KV', version: '1.2' } });

  const executed = await client.request('perk/v1/execute', { session_id: 1, statement: 'select 1' });
  assert.deepEqual(executed, sampleResult);

  const listed = await client.request('perk/v1/list_schema', { session_id: 1 });
  assert.deepEqual(listed, schema);

  const closed = await client.request('perk/v1/close', { session_id: 1 });
  assert.equal(closed, null);
  assert.equal(closeCalls, 1);

  // The closed session is gone.
  await assert.rejects(
    client.request('perk/v1/execute', { session_id: 1, statement: 'select 1' }),
    (error) => error.code === -32602 && /unknown session_id/.test(error.message),
  );
  await assert.rejects(client.request('perk/v1/close', { session_id: 1 }), (error) => error.code === -32602);
  assert.equal(closeCalls, 1);
  await server.close();
});

test('session ids are unique and increment', async () => {
  const { server, client } = createServer(minimalDefinition());
  await client.initialize();
  const first = await client.request('perk/v1/open', { target: 'kv:a' });
  const second = await client.request('perk/v1/open', { target: 'kv:b' });
  assert.equal(first.session_id, 1);
  assert.equal(second.session_id, 2);
  await client.request('perk/v1/close', { session_id: 1 });
  const third = await client.request('perk/v1/open', { target: 'kv:c' });
  assert.equal(third.session_id, 3);
  await server.close();
});

test('invalid or unknown session ids are invalid params', async () => {
  const { server, client } = createServer(minimalDefinition());
  await client.initialize();
  await client.request('perk/v1/open', { target: 'kv:x' });
  await assert.rejects(
    client.request('perk/v1/execute', { statement: 'select 1' }),
    (error) => error.code === -32602 && /session_id/.test(error.message),
  );
  await assert.rejects(
    client.request('perk/v1/execute', { session_id: '1', statement: 'x' }),
    (error) => error.code === -32602,
  );
  await assert.rejects(
    client.request('perk/v1/execute', { session_id: 1.5, statement: 'x' }),
    (error) => error.code === -32602,
  );
  await assert.rejects(
    client.request('perk/v1/execute', { session_id: 99, statement: 'x' }),
    (error) => error.code === -32602 && /unknown session_id/.test(error.message),
  );
  await server.close();
});

test('per-method params are validated', async () => {
  const { server, client } = createServer(minimalDefinition());
  await client.initialize();
  await client.request('perk/v1/open', { target: 'kv:x' });
  await assert.rejects(client.request('perk/v1/open', {}), (error) => error.code === -32602);
  await assert.rejects(client.request('perk/v1/open', { target: 7 }), (error) => error.code === -32602);
  await assert.rejects(
    client.request('perk/v1/execute', { session_id: 1 }),
    (error) => error.code === -32602 && /statement/.test(error.message),
  );
  await assert.rejects(
    client.request('perk/v1/create_index', { session_id: 1, table: 't' }),
    (error) => error.code === -32602 && /change/.test(error.message),
  );
  await assert.rejects(
    client.request('perk/v1/browse_table', { session_id: 1, table: 't' }),
    (error) => error.code === -32602 && /options/.test(error.message),
  );
  await assert.rejects(
    client.request('perk/v1/validate', { session_id: 1, statement: 3 }),
    (error) => error.code === -32602,
  );
  await assert.rejects(
    client.request('perk/v1/build_target', 'not-an-object'),
    (error) => error.code === -32602,
  );
  await server.close();
});

test('open rejects a session service missing mandatory handlers', async () => {
  const { server, client } = createServer(
    minimalDefinition({
      open: async () => {
        const service = serviceStub();
        delete service.browseTable;
        return { info: { product: 'KV', version: '1' }, service };
      },
    }),
  );
  await client.initialize();
  await assert.rejects(
    client.request('perk/v1/open', { target: 'kv:x' }),
    (error) => error.code === -32603 && /browseTable/.test(error.message),
  );
  await server.close();
});

test('handler errors become internal error responses', async () => {
  const { server, client } = createServer(
    minimalDefinition({
      open: async () => ({
        info: { product: 'KV', version: '1' },
        service: serviceStub({
          execute: async () => {
            throw new Error('boom');
          },
        }),
      }),
    }),
  );
  await client.initialize();
  await client.request('perk/v1/open', { target: 'kv:x' });
  await assert.rejects(
    client.request('perk/v1/execute', { session_id: 1, statement: 'select 1' }),
    (error) => error.code === -32603 && error.message === 'boom',
  );
  await server.close();
});
