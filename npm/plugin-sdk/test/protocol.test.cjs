'use strict';

const test = require('node:test');
const assert = require('node:assert/strict');

const {
  createServer,
  minimalDefinition,
  serviceStub,
  waitForFrames,
  withTimeout,
  delay,
} = require('./helpers.cjs');

test('non-initialize requests are rejected until initialize succeeds', async () => {
  const { server, client } = createServer(minimalDefinition());
  await assert.rejects(
    client.request('perk/v1/build_target', { host: 'svc' }),
    (error) => error.code === -32600 && /initialize/.test(error.message),
  );
  await assert.rejects(
    client.request('perk/v1/execute', { session_id: 1, statement: 'select 1' }),
    (error) => error.code === -32600 && /initialize/.test(error.message),
  );
  const result = await client.initialize();
  assert.equal(result.protocol_version, 1);
  const target = await client.request('perk/v1/build_target', { host: 'svc' });
  assert.deepEqual(target, { target: 'kv:svc', ok: true });
  await server.close();
});

test('initialize is pinned to protocol version 1', async () => {
  const { server, client } = createServer(minimalDefinition());
  await assert.rejects(
    client.initialize(2),
    (error) => error.code === -32600 && /protocol_version/.test(error.message),
  );
  await assert.rejects(
    client.initialize(0),
    (error) => error.code === -32600 && /protocol_version/.test(error.message),
  );
  await assert.rejects(
    client.request('perk/v1/initialize', {}),
    (error) => error.code === -32600 && /protocol_version/.test(error.message),
  );
  // Still uninitialized after rejected attempts.
  await assert.rejects(
    client.request('perk/v1/build_target', { host: 'svc' }),
    (error) => error.code === -32600 && /initialize/.test(error.message),
  );
  const result = await client.initialize();
  assert.equal(result.protocol_version, 1);
  await server.close();
});

test('initialize with non-object params is invalid', async () => {
  const { server, client } = createServer(minimalDefinition());
  client.input.write('{"jsonrpc":"2.0","id":1,"method":"perk/v1/initialize","params":"x"}\n');
  await waitForFrames(client, 1);
  assert.equal(client.frames[0].error.code, -32602);
  await server.close();
});

test('initialize can only run once', async () => {
  const { server, client } = createServer(minimalDefinition());
  await client.initialize();
  await assert.rejects(
    client.initialize(),
    (error) => error.code === -32600 && /already initialized/.test(error.message),
  );
  await server.close();
});

test('initialize result advertises protocol version and supplied capabilities', async () => {
  const writeCapabilities = {
    row_writer: true,
    document: {
      format: 'application/vnd.perk.mongodb.extjson+json;version=2;mode=relaxed',
      text: true,
    },
  };
  const definition = minimalDefinition({
    capabilities: {
      name: 'mongo',
      display: 'MongoDB',
      targets: [{ prefix: 'mongo:', keep_target: true }],
      write_capabilities: writeCapabilities,
    },
    open: async () => ({
      info: { product: 'MongoDB', version: '7.0' },
      service: serviceStub({
        rowWrite: async () => ({ result: { rows_affected: 0 } }),
        documentWrite: async () => ({ result: { rows_affected: 0 } }),
      }),
    }),
  });
  const { server, client } = createServer(definition);
  const result = await client.initialize();
  assert.equal(result.protocol_version, 1);
  assert.deepEqual(result.capabilities, {
    name: 'mongo',
    display: 'MongoDB',
    targets: [{ prefix: 'mongo:', keep_target: true }],
    write_capabilities: writeCapabilities,
  });
  await server.close();
});

test('unknown methods return -32601', async () => {
  const { server, client } = createServer(minimalDefinition());
  await client.initialize();
  await assert.rejects(client.request('perk/v1/nope'), (error) => error.code === -32601);
  await assert.rejects(
    client.request('perk/v1/row_write', { session_id: 1, request: {} }),
    (error) => error.code === -32601,
  );
  await server.close();
});

test('notifications never produce responses', async () => {
  const { server, client } = createServer(minimalDefinition());
  await client.initialize();
  client.notify('perk/v1/nope', {});
  client.notify('perk/v1/cancel', { id: 42 });
  await delay(20);
  assert.equal(client.frames.length, 1);
  await server.close();
});

test('invalid request shapes return -32600', async () => {
  const { server, client } = createServer(minimalDefinition());
  client.input.write('{"jsonrpc":"2.0","id":1}\n');
  client.input.write('{"jsonrpc":"1.0","id":2,"method":"perk/v1/initialize","params":{"protocol_version":1}}\n');
  client.input.write('{"jsonrpc":"2.0","id":"x","method":"perk/v1/initialize","params":{"protocol_version":1}}\n');
  client.input.write('{"jsonrpc":"2.0","id":3,"method":7}\n');
  await waitForFrames(client, 4);
  const [missingMethod, wrongVersion, stringId, badMethod] = client.frames;
  assert.equal(missingMethod.error.code, -32600);
  assert.equal(missingMethod.id, 1);
  assert.equal(wrongVersion.error.code, -32600);
  assert.equal(stringId.error.code, -32600);
  assert.equal(stringId.id, null);
  assert.equal(badMethod.error.code, -32600);
  // Invalid requests do not terminate the server.
  await client.initialize();
  const target = await client.request('perk/v1/build_target', {});
  assert.deepEqual(target, { target: 'kv:local', ok: true });
  await server.close();
});

test('empty and malformed frames close the server', async () => {
  const empty = createServer(minimalDefinition());
  empty.client.input.write('\n');
  await withTimeout(empty.server.closed);
  assert.equal(empty.client.frames.length, 0);

  const malformed = createServer(minimalDefinition());
  malformed.client.input.write('{not json}\n');
  await withTimeout(malformed.server.closed);
  assert.equal(malformed.client.frames.length, 0);
});

test('invalid UTF-8 input closes the server', async () => {
  const { server, client } = createServer(minimalDefinition());
  client.input.write(Buffer.from([0xff, 0xfe, 0x0a]));
  await withTimeout(server.closed);
  assert.equal(client.frames.length, 0);
});

test('oversized input closes the server', async () => {
  const { server, client } = createServer(minimalDefinition());
  client.input.write(Buffer.alloc(16 * 1024 * 1024, 0x61));
  await withTimeout(server.closed);
  assert.equal(client.frames.length, 0);
});

test('stdout carries only valid protocol frames', async () => {
  const { server, client } = createServer(
    minimalDefinition({
      open: async () => ({
        info: { product: 'KV', version: '1' },
        service: serviceStub({
          execute: async () => ({
            columns: ['c'],
            column_types: ['T'],
            rows: [['1', null]],
            untruncated_rows: [['1', null]],
            rows_affected: 0,
            has_more: false,
            duration_ns: 7,
            truncated: false,
          }),
        }),
      }),
    }),
  );
  await client.initialize();
  await client.request('perk/v1/open', { target: 'kv:x' });
  await client.request('perk/v1/execute', { session_id: 1, statement: 'select 1' });
  await client.request('perk/v1/nope').catch(() => {});
  await client.request('perk/v1/build_target', { host: 'h' });
  const lines = client.rawText().split('\n').filter((line) => line !== '');
  for (const line of lines) {
    const frame = JSON.parse(line);
    assert.equal(frame.jsonrpc, '2.0');
    assert.ok(Number.isInteger(frame.id));
    assert.ok(frame.result !== undefined || frame.error !== undefined);
  }
  assert.equal(lines.length, client.frames.length);
  await server.close();
});
