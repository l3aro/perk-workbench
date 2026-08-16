'use strict';

const test = require('node:test');
const assert = require('node:assert/strict');

const { RequestCancelledError } = require('../index.cjs');
const { createServer, minimalDefinition, serviceStub, delay } = require('./helpers.cjs');

const WORKSPACE = {
  standard_tabs: ['columns', 'indexes'],
  custom_views: [{ id: 'key-info', label: 'Key Info', scopes: ['table'] }],
};

function workspaceDefinition(overrides = {}) {
  return minimalDefinition({
    capabilities: {
      name: 'kv',
      display: 'KV',
      targets: [{ prefix: 'kv:' }],
      write_capabilities: { row_writer: false },
      workspace: WORKSPACE,
    },
    open: async () => ({
      info: { product: 'KV', version: '1' },
      service: serviceStub({ workspaceView: async () => ({ rows_affected: 1 }) }),
    }),
    ...overrides,
  });
}

test('initialize includes a normalized workspace advertisement when present', async () => {
  const { server, client } = createServer(workspaceDefinition());
  const result = await client.initialize();
  assert.deepEqual(result.capabilities.workspace, WORKSPACE);
  await server.close();
});

test('initialize omits workspace when absent or null', async () => {
  const { server, client } = createServer(minimalDefinition());
  const result = await client.initialize();
  assert.ok(!('workspace' in result.capabilities));
  await server.close();

  const { server: nullServer, client: nullClient } = createServer(
    minimalDefinition({
      capabilities: {
        name: 'kv',
        display: 'KV',
        targets: [{ prefix: 'kv:' }],
        write_capabilities: { row_writer: false },
        workspace: null,
      },
    }),
  );
  const nullResult = await nullClient.initialize();
  assert.ok(!('workspace' in nullResult.capabilities));
  await nullServer.close();
});

test('an explicitly empty workspace stays present, distinct from absent', async () => {
  // Present-but-empty advertises the explicit policy: no standard tabs
  // beyond Query/Browse and no custom views. Absent keeps the legacy
  // per-product tab policy. The two must never normalize to the same
  // wire shape.
  const { server, client } = createServer(
    minimalDefinition({
      capabilities: {
        name: 'kv',
        display: 'KV',
        targets: [{ prefix: 'kv:' }],
        write_capabilities: { row_writer: false },
        workspace: {},
      },
    }),
  );
  const result = await client.initialize();
  assert.deepEqual(result.capabilities.workspace, {});
  await server.close();
});

test('create rejects invalid workspace metadata', () => {
  const base = {
    name: 'kv',
    display: 'KV',
    targets: [{ prefix: 'kv:' }],
    write_capabilities: { row_writer: false },
  };
  const cases = [
    { name: 'workspace not an object', workspace: [] },
    { name: 'standard_tabs not an array', workspace: { standard_tabs: 'columns' } },
    { name: 'unknown standard tab', workspace: { standard_tabs: ['relations'] } },
    { name: 'duplicate standard tab', workspace: { standard_tabs: ['columns', 'columns'] } },
    { name: 'custom_views not an array', workspace: { custom_views: {} } },
    { name: 'blank view id', workspace: { custom_views: [{ id: ' ', label: 'Keys', scopes: ['table'] }] } },
    { name: 'blank view label', workspace: { custom_views: [{ id: 'keys', label: '', scopes: ['table'] }] } },
    {
      name: 'id overlong',
      workspace: { custom_views: [{ id: 'i'.repeat(65), label: 'Keys', scopes: ['table'] }] },
    },
    {
      name: 'label overlong',
      workspace: { custom_views: [{ id: 'keys', label: 'l'.repeat(33), scopes: ['table'] }] },
    },
    { name: 'control in id', workspace: { custom_views: [{ id: 'a\nb', label: 'Keys', scopes: ['table'] }] } },
    { name: 'control in label', workspace: { custom_views: [{ id: 'keys', label: 'a\x00b', scopes: ['table'] }] } },
    {
      name: 'duplicate ids case-insensitive',
      workspace: {
        custom_views: [
          { id: 'Keys', label: 'One', scopes: ['table'] },
          { id: 'keys', label: 'Two', scopes: ['table'] },
        ],
      },
    },
    {
      name: 'duplicate labels case-insensitive',
      workspace: {
        custom_views: [
          { id: 'one', label: 'Keys', scopes: ['table'] },
          { id: 'two', label: 'keys', scopes: ['table'] },
        ],
      },
    },
    { name: 'empty scopes', workspace: { custom_views: [{ id: 'keys', label: 'Keys', scopes: [] }] } },
    { name: 'invalid scope', workspace: { custom_views: [{ id: 'keys', label: 'Keys', scopes: ['collection'] }] } },
    {
      name: 'duplicate scope',
      workspace: { custom_views: [{ id: 'keys', label: 'Keys', scopes: ['table', 'table'] }] },
    },
    {
      name: 'over the custom view cap',
      workspace: {
        custom_views: Array.from({ length: 9 }, (_, i) => ({
          id: `view${'x'.repeat(i)}`,
          label: 'View',
          scopes: ['table'],
        })),
      },
    },
  ];
  for (const testCase of cases) {
    assert.throws(
      () =>
        createServer(
          minimalDefinition({
            capabilities: { ...base, workspace: testCase.workspace },
          }),
        ),
      (error) => /workspace/.test(error && error.message),
      `workspace case "${testCase.name}" must be rejected`,
    );
  }
});

test('open requires workspaceView when custom views are advertised', async () => {
  const { server, client } = createServer(
    minimalDefinition({
      capabilities: {
        name: 'kv',
        display: 'KV',
        targets: [{ prefix: 'kv:' }],
        write_capabilities: { row_writer: false },
        workspace: WORKSPACE,
      },
      open: async () => ({
        info: { product: 'KV', version: '1' },
        service: serviceStub(), // no workspaceView handler
      }),
    }),
  );
  await client.initialize();
  await assert.rejects(
    client.request('perk/v1/open', { target: 'kv:x' }),
    (error) =>
      error.code === -32603 &&
      /workspaceView is required/.test(error.message),
  );
  await server.close();
});

test('open rejects a workspaceView handler without advertised custom views', async () => {
  const { server, client } = createServer(
    minimalDefinition({
      open: async () => ({
        info: { product: 'KV', version: '1' },
        service: serviceStub({ workspaceView: async () => ({ rows_affected: 1 }) }),
      }),
    }),
  );
  await client.initialize();
  await assert.rejects(
    client.request('perk/v1/open', { target: 'kv:x' }),
    (error) =>
      error.code === -32603 &&
      /workspaceView is not supported/.test(error.message),
  );
  await server.close();
});

test('unadvertised workspace_view method is not implemented', async () => {
  const { server, client } = createServer(minimalDefinition());
  await client.initialize();
  await assert.rejects(
    client.request('perk/v1/workspace_view', {
      session_id: 1,
      view_id: 'keys',
      target: { kind: 'table', table: 'widgets' },
    }),
    (error) => error.code === -32601,
  );
  await server.close();
});

test('workspaceView round trips through the session service', async () => {
  const target = { kind: 'table', database: 'kv', table: 'widgets' };
  let sawRequest = null;
  let sawContext = null;
  const { server, client } = createServer(
    workspaceDefinition({
      open: async () => ({
        info: { product: 'KV', version: '1' },
        service: serviceStub({
          workspaceView: async (request, context) => {
            sawRequest = request;
            sawContext = context;
            return {
              columns: ['key', 'ttl'],
              column_types: ['string', 'integer'],
              rows: [['user:2', '3600'], ['user:3', null]],
              untruncated_rows: [['user:2', '3600'], ['user:3', null]],
              rows_affected: 0,
              has_more: false,
              duration_ns: 900,
              truncated: false,
            };
          },
        }),
      }),
    }),
  );
  await client.initialize();
  await client.request('perk/v1/open', { target: 'kv:x' });
  const response = await client.request('perk/v1/workspace_view', {
    session_id: 1,
    view_id: 'key-info',
    target,
  });
  assert.equal(response.rows[0][0], 'user:2');
  assert.equal(response.rows[1][1], null);
  assert.deepEqual(sawRequest, { view_id: 'key-info', target });
  assert.ok(sawContext.signal instanceof AbortSignal);
  await server.close();
});

test('workspace_view validates its params', async () => {
  const { server, client } = createServer(workspaceDefinition());
  await client.initialize();
  await client.request('perk/v1/open', { target: 'kv:x' });

  await assert.rejects(
    client.request('perk/v1/workspace_view', {
      session_id: 1,
      view_id: '  ',
      target: { kind: 'table', table: 'widgets' },
    }),
    (error) => error.code === -32602,
  );
  await assert.rejects(
    client.request('perk/v1/workspace_view', {
      session_id: 1,
      view_id: 'key-info',
      target: { kind: 'collection', table: 'widgets' },
    }),
    (error) => error.code === -32602,
  );
  await assert.rejects(
    client.request('perk/v1/workspace_view', { session_id: 1, view_id: 'key-info' }),
    (error) => error.code === -32602,
  );
  await assert.rejects(
    client.request('perk/v1/workspace_view', {
      session_id: 99,
      view_id: 'key-info',
      target: { kind: 'table' },
    }),
    (error) => error.code === -32602 && /unknown session_id/.test(error.message),
  );
  await server.close();
});

test('workspace_view cancellation aborts the signal and replies -32800', async () => {
  let sawAbort = false;
  const { server, client } = createServer(
    workspaceDefinition({
      open: async () => ({
        info: { product: 'KV', version: '1' },
        service: serviceStub({
          workspaceView: async (request, { signal }) =>
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
  const pending = client.request('perk/v1/workspace_view', {
    session_id: 1,
    view_id: 'key-info',
    target: { kind: 'table', table: 'widgets' },
  });
  await delay(10); // let the handler register its listener
  client.cancel(pending.requestID);
  await assert.rejects(pending, (error) => error.code === -32800);
  assert.equal(sawAbort, true);
  await server.close();
});
