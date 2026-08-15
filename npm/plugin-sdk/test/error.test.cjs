'use strict';

const test = require('node:test');
const assert = require('node:assert/strict');

const { ErrorKind, PluginOperationError, RequestCancelledError } = require('../index.cjs');
const {
  createServer,
  minimalDefinition,
  serviceStub,
  delay,
} = require('./helpers.cjs');

test('ErrorKind constants are frozen with the v1 values', () => {
  assert.deepEqual(ErrorKind, {
    Validation: 'validation',
    Authentication: 'authentication',
    Connection: 'connection',
    Operation: 'operation',
    Unsupported: 'unsupported',
    Cancelled: 'cancelled',
    Protocol: 'protocol',
    PluginCrash: 'plugin_crash',
  });
  assert.ok(Object.isFrozen(ErrorKind));
});

test('PluginOperationError carries code, normalized kind, and provenance', () => {
  const error = new PluginOperationError('auth denied', {
    code: -32001,
    kind: ErrorKind.Authentication,
    plugin: 'demo-kv',
    method: 'perk/v1/execute',
  });
  assert.equal(error.name, 'PluginOperationError');
  assert.ok(error instanceof Error);
  assert.equal(error.code, -32001);
  assert.equal(error.kind, 'authentication');
  assert.equal(error.plugin, 'demo-kv');
  assert.equal(error.method, 'perk/v1/execute');

  const defaults = new PluginOperationError('boom');
  assert.equal(defaults.code, -32000);
  assert.equal(defaults.kind, 'operation');
  assert.equal(defaults.plugin, undefined);
  assert.equal(defaults.method, undefined);

  const unknownKind = new PluginOperationError('x', { kind: 'bogus' });
  assert.equal(unknownKind.kind, 'operation');
  const blankKind = new PluginOperationError('x', { kind: '' });
  assert.equal(blankKind.kind, 'operation');
  const nonIntegerCode = new PluginOperationError('x', { code: 'nope' });
  assert.equal(nonIntegerCode.code, -32000);
  const nonStringProvenance = new PluginOperationError('x', { plugin: 7, method: 9 });
  assert.equal(nonStringProvenance.plugin, undefined);
  assert.equal(nonStringProvenance.method, undefined);
});

test('structured operation errors roundtrip onto code/message and data', async () => {
  const { server, client } = createServer(
    minimalDefinition({
      open: async () => ({
        info: { product: 'KV', version: '1' },
        service: serviceStub({
          execute: async () => {
            throw new PluginOperationError('read-only: SET is not allowed', {
              code: -32001,
              kind: ErrorKind.Validation,
              plugin: 'demo-kv',
              method: 'perk/v1/execute',
            });
          },
        }),
      }),
    }),
  );
  await client.initialize();
  await client.request('perk/v1/open', { target: 'kv:x' });
  const error = await client.request('perk/v1/execute', { session_id: 1, statement: 'SET k v' }).catch((e) => e);
  assert.equal(error.code, -32001);
  assert.equal(error.message, 'read-only: SET is not allowed');
  assert.deepEqual(error.data, {
    kind: 'validation',
    plugin: 'demo-kv',
    method: 'perk/v1/execute',
  });
  await server.close();
});

test('an omitted error method is filled with the wire method', async () => {
  const { server, client } = createServer(
    minimalDefinition({
      open: async () => ({
        info: { product: 'KV', version: '1' },
        service: serviceStub({
          execute: async () => {
            throw new PluginOperationError('nope', { kind: ErrorKind.Unsupported });
          },
        }),
      }),
    }),
  );
  await client.initialize();
  await client.request('perk/v1/open', { target: 'kv:x' });
  const error = await client.request('perk/v1/execute', { session_id: 1, statement: 'x' }).catch((e) => e);
  assert.equal(error.code, -32000);
  assert.equal(error.message, 'nope');
  assert.deepEqual(error.data, { kind: 'unsupported', method: 'perk/v1/execute' });
  await server.close();
});

test('unknown kinds normalize to operation on the wire', async () => {
  const { server, client } = createServer(
    minimalDefinition({
      open: async () => ({
        info: { product: 'KV', version: '1' },
        service: serviceStub({
          execute: async () => {
            throw new PluginOperationError('mystery', { kind: 'bogus' });
          },
        }),
      }),
    }),
  );
  await client.initialize();
  await client.request('perk/v1/open', { target: 'kv:x' });
  const error = await client.request('perk/v1/execute', { session_id: 1, statement: 'x' }).catch((e) => e);
  assert.equal(error.data.kind, 'operation');
  await server.close();
});

test('generic thrown errors keep -32603 and carry no data', async () => {
  const { server, client } = createServer(
    minimalDefinition({
      open: async () => ({
        info: { product: 'KV', version: '1' },
        service: serviceStub({
          execute: async () => {
            throw new Error('plain boom');
          },
        }),
      }),
    }),
  );
  await client.initialize();
  await client.request('perk/v1/open', { target: 'kv:x' });
  const error = await client.request('perk/v1/execute', { session_id: 1, statement: 'x' }).catch((e) => e);
  assert.equal(error.code, -32603);
  assert.equal(error.message, 'plain boom');
  assert.equal(error.data, undefined);
  await server.close();
});

test('generic errors with an integer code keep their code and no data', async () => {
  const { server, client } = createServer(
    minimalDefinition({
      open: async () => ({
        info: { product: 'KV', version: '1' },
        service: serviceStub({
          execute: async () => {
            const error = new Error('custom');
            error.code = -32010;
            throw error;
          },
        }),
      }),
    }),
  );
  await client.initialize();
  await client.request('perk/v1/open', { target: 'kv:x' });
  const error = await client.request('perk/v1/execute', { session_id: 1, statement: 'x' }).catch((e) => e);
  assert.equal(error.code, -32010);
  assert.equal(error.data, undefined);
  await server.close();
});

test('RequestCancelledError replies -32800 with cancelled data', async () => {
  const { server, client } = createServer(
    minimalDefinition({
      open: async () => ({
        info: { product: 'KV', version: '1' },
        service: serviceStub({
          execute: async (request, { signal }) =>
            new Promise((resolve, reject) => {
              signal.addEventListener(
                'abort',
                () => reject(new RequestCancelledError('request canceled')),
                { once: true },
              );
            }),
        }),
      }),
    }),
  );
  await client.initialize();
  await client.request('perk/v1/open', { target: 'kv:x' });
  const pending = client.request('perk/v1/execute', { session_id: 1, statement: 'x' });
  await delay(10);
  client.cancel(pending.requestID);
  const error = await pending.catch((e) => e);
  assert.equal(error.code, -32800);
  // The aborted signal takes precedence over the thrown message.
  assert.equal(error.message, 'canceled');
  assert.deepEqual(error.data, { kind: 'cancelled' });
  await server.close();
});

test('a handler finishing after the signal fired replies -32800 with cancelled data', async () => {
  const { server, client } = createServer(
    minimalDefinition({
      open: async () => ({
        info: { product: 'KV', version: '1' },
        service: serviceStub({
          execute: async () => {
            await delay(50);
            return { rows_affected: 1 };
          },
        }),
      }),
    }),
  );
  await client.initialize();
  await client.request('perk/v1/open', { target: 'kv:x' });
  const pending = client.request('perk/v1/execute', { session_id: 1, statement: 'x' });
  client.cancel(pending.requestID);
  const error = await pending.catch((e) => e);
  assert.equal(error.code, -32800);
  assert.deepEqual(error.data, { kind: 'cancelled' });
  await server.close();
});
