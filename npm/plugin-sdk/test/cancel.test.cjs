'use strict';

const test = require('node:test');
const assert = require('node:assert/strict');

const { RequestCancelledError } = require('../index.cjs');
const {
  createServer,
  minimalDefinition,
  serviceStub,
  delay,
  withTimeout,
} = require('./helpers.cjs');

test('cancel aborts the request signal and replies -32800', async () => {
  let sawAbort = false;
  const { server, client } = createServer(
    minimalDefinition({
      open: async () => ({
        info: { product: 'KV', version: '1' },
        service: serviceStub({
          execute: async (request, { signal }) =>
            new Promise((resolve, reject) => {
              signal.addEventListener(
                'abort',
                () => {
                  sawAbort = true;
                  reject(new RequestCancelledError());
                },
                { once: true },
              );
            }),
        }),
      }),
    }),
  );
  await client.initialize();
  await client.request('perk/v1/open', { target: 'kv:x' });
  const pending = client.request('perk/v1/execute', { session_id: 1, statement: 'select 1' });
  await delay(10); // let the handler register its listener
  client.cancel(pending.requestID);
  await assert.rejects(pending, (error) => error.code === -32800);
  assert.equal(sawAbort, true);
  await server.close();
});

test('an observed aborted signal replies -32800 even when the handler completes', async () => {
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
  const pending = client.request('perk/v1/execute', { session_id: 1, statement: 'select 1' });
  client.cancel(pending.requestID);
  await assert.rejects(pending, (error) => error.code === -32800);
  await server.close();
});

test('cancel for an unknown id is ignored', async () => {
  const { server, client } = createServer(minimalDefinition());
  await client.initialize();
  client.cancel(999);
  const target = await client.request('perk/v1/build_target', { host: 'svc' });
  assert.deepEqual(target, { target: 'kv:svc', ok: true });
  assert.ok(client.frames.every((frame) => frame.id !== 999));
  await server.close();
});

test('closing the server aborts pending requests', async () => {
  let rejected = false;
  const { server, client } = createServer(
    minimalDefinition({
      open: async () => ({
        info: { product: 'KV', version: '1' },
        service: serviceStub({
          execute: async (request, { signal }) =>
            new Promise((resolve, reject) => {
              signal.addEventListener(
                'abort',
                () => {
                  rejected = true;
                  reject(new RequestCancelledError());
                },
                { once: true },
              );
            }),
        }),
      }),
    }),
  );
  await client.initialize();
  await client.request('perk/v1/open', { target: 'kv:x' });
  client.request('perk/v1/execute', { session_id: 1, statement: 'x' });
  await delay(10);
  await server.close();
  await withTimeout(server.closed);
  assert.equal(rejected, true);
  // No response is written after close: only initialize and open frames.
  await delay(20);
  assert.equal(client.frames.length, 2);
});
