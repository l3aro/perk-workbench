'use strict';

const test = require('node:test');
const assert = require('node:assert/strict');

const { createServer, minimalDefinition, serviceStub } = require('./helpers.cjs');

const EXTJSON = 'application/vnd.perk.mongodb.extjson+json;version=2;mode=relaxed';

test('creation requires buildTarget, open, and capabilities', () => {
  assert.throws(
    () => createServer(minimalDefinition({ buildTarget: undefined })),
    /buildTarget must be a function/,
  );
  assert.throws(
    () => createServer(minimalDefinition({ open: undefined })),
    /open must be a function/,
  );
  assert.throws(
    () => createServer(minimalDefinition({ capabilities: undefined })),
    /capabilities must be an object/,
  );
});

test('open rejects row_writer advertised without a rowWrite handler', async () => {
  const { server, client } = createServer(
    minimalDefinition({
      capabilities: {
        name: 'kv',
        display: 'KV',
        targets: [{ prefix: 'kv:' }],
        write_capabilities: { row_writer: true },
      },
    }),
  );
  await client.initialize();
  await assert.rejects(
    client.request('perk/v1/open', { target: 'kv:x' }),
    (error) => error.code === -32603 && /rowWrite/.test(error.message),
  );
  await server.close();
});

test('open rejects a rowWrite handler without the row_writer capability', async () => {
  const { server, client } = createServer(
    minimalDefinition({
      open: async () => ({
        info: { product: 'KV', version: '1' },
        service: serviceStub({ rowWrite: async () => ({ result: { rows_affected: 0 } }) }),
      }),
    }),
  );
  await client.initialize();
  await assert.rejects(
    client.request('perk/v1/open', { target: 'kv:x' }),
    (error) => error.code === -32603 && /rowWrite/.test(error.message),
  );
  await server.close();
});

test('open rejects document advertised without a documentWrite handler', async () => {
  const { server, client } = createServer(
    minimalDefinition({
      capabilities: {
        name: 'kv',
        display: 'KV',
        targets: [{ prefix: 'kv:' }],
        write_capabilities: { row_writer: false, document: { format: EXTJSON, text: true } },
      },
    }),
  );
  await client.initialize();
  await assert.rejects(
    client.request('perk/v1/open', { target: 'kv:x' }),
    (error) => error.code === -32603 && /documentWrite/.test(error.message),
  );
  await server.close();
});

test('open rejects a documentWrite handler without the document capability', async () => {
  const { server, client } = createServer(
    minimalDefinition({
      open: async () => ({
        info: { product: 'KV', version: '1' },
        service: serviceStub({ documentWrite: async () => ({ result: { rows_affected: 0 } }) }),
      }),
    }),
  );
  await client.initialize();
  await assert.rejects(
    client.request('perk/v1/open', { target: 'kv:x' }),
    (error) => error.code === -32603 && /documentWrite/.test(error.message),
  );
  await server.close();
});

test('initialize result includes write_capabilities even when empty', async () => {
  const { server, client } = createServer(minimalDefinition());
  const result = await client.initialize();
  assert.deepEqual(result.capabilities.write_capabilities, { row_writer: false });
  assert.ok(!('document' in result.capabilities.write_capabilities));
  await server.close();
});

test('unadvertised write methods are not implemented', async () => {
  const { server, client } = createServer(minimalDefinition());
  await client.initialize();
  await assert.rejects(
    client.request('perk/v1/row_write', { session_id: 1, request: {} }),
    (error) => error.code === -32601,
  );
  await assert.rejects(
    client.request('perk/v1/document_write', { session_id: 1, request: {} }),
    (error) => error.code === -32601,
  );
  await server.close();
});

test('rowWrite round trips through the session service', async () => {
  const writeRequest = {
    operation: 'insert',
    table: 'kv',
    values: [{ name: 'k', value: { kind: 'string', string: 'v' } }],
  };
  const native = 'SET kv:v 1';
  let sawContext = null;
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
        service: serviceStub({
          rowWrite: async (request, context) => {
            assert.deepEqual(request, writeRequest);
            sawContext = context;
            return { result: { rows_affected: 1, statement: native } };
          },
        }),
      }),
    }),
  );
  await client.initialize();
  await client.request('perk/v1/open', { target: 'kv:x' });
  const response = await client.request('perk/v1/row_write', {
    session_id: 1,
    request: writeRequest,
  });
  assert.deepEqual(response, { result: { rows_affected: 1, statement: native } });
  assert.ok(sawContext.signal instanceof AbortSignal);
  await server.close();
});

test('rowWrite result without a statement stays without one on the wire', async () => {
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
        service: serviceStub({ rowWrite: async () => ({ result: { rows_affected: 1 } }) }),
      }),
    }),
  );
  await client.initialize();
  await client.request('perk/v1/open', { target: 'kv:x' });
  const response = await client.request('perk/v1/row_write', {
    session_id: 1,
    request: { operation: 'delete', table: 'kv', key: [{ name: 'k', value: { kind: 'string', string: 'v' } }] },
  });
  assert.deepEqual(response, { result: { rows_affected: 1 } });
  assert.ok(!('statement' in response.result));
  await server.close();
});

test('rowWrite requires an existing session', async () => {
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
        service: serviceStub({ rowWrite: async () => ({ result: { rows_affected: 0 } }) }),
      }),
    }),
  );
  await client.initialize();
  await assert.rejects(
    client.request('perk/v1/row_write', { session_id: 99, request: {} }),
    (error) => error.code === -32602 && /unknown session_id/.test(error.message),
  );
  await server.close();
});

test('documentWrite round trips with base64 byte payloads', async () => {
  const documentRequest = {
    operation: 'replace',
    collection: 'restaurants',
    id: { format: EXTJSON, data: 'eyJfaWQiOiIxIn0=' },
    document: { format: EXTJSON, data: 'e30=' },
  };
  const { server, client } = createServer(
    minimalDefinition({
      capabilities: {
        name: 'mongo',
        display: 'MongoDB',
        targets: [{ prefix: 'mongo:', keep_target: true }],
        write_capabilities: { row_writer: false, document: { format: EXTJSON, text: true } },
      },
      open: async () => ({
        info: { product: 'MongoDB', version: '7.0' },
        service: serviceStub({
          documentWrite: async (request) => {
            assert.deepEqual(request, documentRequest);
            return { result: { rows_affected: 1, statement: 'db.restaurants.replaceOne({_id: ObjectId("1")}, {})' }, document: documentRequest.id };
          },
        }),
      }),
    }),
  );
  await client.initialize();
  await client.request('perk/v1/open', { target: 'mongo://db' });
  const response = await client.request('perk/v1/document_write', {
    session_id: 1,
    request: documentRequest,
  });
  assert.deepEqual(response, {
    result: { rows_affected: 1, statement: 'db.restaurants.replaceOne({_id: ObjectId("1")}, {})' },
    document: documentRequest.id,
  });
  await server.close();
});
