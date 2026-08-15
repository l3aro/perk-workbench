'use strict';

const { PassThrough } = require('node:stream');
const { createPluginServer } = require('../index.cjs');

const MANDATORY_HANDLERS = [
  'execute', 'executeReadOnly', 'validate', 'listSchema', 'tableInfo',
  'listIndexes', 'createIndex', 'replaceIndex', 'dropIndex',
  'listForeignKeys', 'listReferencingForeignKeys', 'listForeignKeysAll',
  'listIndexesAll', 'createForeignKey', 'replaceForeignKey',
  'dropForeignKey', 'alterColumn', 'dropColumn', 'addColumn', 'browseTable',
];

function serviceStub(overrides = {}) {
  const service = {};
  for (const name of MANDATORY_HANDLERS) {
    service[name] = async () => null;
  }
  return Object.assign(service, overrides);
}

function minimalDefinition(overrides = {}) {
  return Object.assign(
    {
      capabilities: { name: 'kv', display: 'KV', targets: [{ prefix: 'kv:' }] },
      buildTarget: async (values) => ({ target: `kv:${values.host ?? 'local'}`, ok: true }),
      open: async () => ({ info: { product: 'KV', version: '1.0' }, service: serviceStub() }),
    },
    overrides,
  );
}

function createServer(definition, options = {}) {
  const input = new PassThrough();
  const output = new PassThrough();
  const server = createPluginServer(definition, { input, output, ...options });
  const client = new RpcClient(input, output);
  return { server, input, output, client };
}

// RpcClient is the test-side host: it writes JSON-RPC frames into the
// server input and resolves promises when the matching response id
// arrives on the output.
class RpcClient {
  constructor(input, output) {
    this.input = input;
    this.output = output;
    this.nextID = 1;
    this.pending = new Map();
    this.frames = [];
    this.raw = [];
    this._leftover = '';
    output.on('data', (chunk) => this._feed(chunk));
  }

  _feed(chunk) {
    this.raw.push(chunk);
    const text = this._leftover + chunk.toString('utf8');
    const lines = text.split('\n');
    this._leftover = lines.pop();
    for (const line of lines) {
      if (line === '') continue;
      const frame = JSON.parse(line);
      this.frames.push(frame);
      if (Number.isInteger(frame.id) && this.pending.has(frame.id)) {
        const { resolve, reject } = this.pending.get(frame.id);
        this.pending.delete(frame.id);
        if (frame.error) {
          const error = new Error(frame.error.message);
          error.code = frame.error.code;
          if (frame.error.data !== undefined) error.data = frame.error.data;
          reject(error);
        } else {
          resolve(frame.result);
        }
      }
    }
  }

  request(method, params) {
    const id = this.nextID++;
    const promise = new Promise((resolve, reject) => {
      this.pending.set(id, { resolve, reject });
    });
    promise.requestID = id;
    const message = { jsonrpc: '2.0', id, method };
    if (params !== undefined) message.params = params;
    this.input.write(`${JSON.stringify(message)}\n`);
    return promise;
  }

  notify(method, params) {
    const message = { jsonrpc: '2.0', method };
    if (params !== undefined) message.params = params;
    this.input.write(`${JSON.stringify(message)}\n`);
  }

  cancel(id) {
    this.notify('perk/v1/cancel', { id });
  }

  async initialize(protocolVersion = 1) {
    return this.request('perk/v1/initialize', {
      protocol_version: protocolVersion,
      workbench_version: 'perk-workbench test 0.0.0',
    });
  }

  rawText() {
    return Buffer.concat(this.raw).toString('utf8');
  }
}

function delay(ms) {
  return new Promise((resolve) => setTimeout(resolve, ms));
}

async function waitForFrames(client, count, timeoutMs = 2000) {
  const start = Date.now();
  while (client.frames.length < count) {
    if (Date.now() - start > timeoutMs) {
      throw new Error(`timed out waiting for ${count} frames, saw ${client.frames.length}`);
    }
    await delay(5);
  }
  return client.frames;
}

async function withTimeout(promise, timeoutMs = 2000, label = 'timed out') {
  let timer;
  const timeout = new Promise((_, reject) => {
    timer = setTimeout(() => reject(new Error(label)), timeoutMs);
  });
  try {
    return await Promise.race([promise, timeout]);
  } finally {
    clearTimeout(timer);
  }
}

module.exports = {
  MANDATORY_HANDLERS,
  serviceStub,
  minimalDefinition,
  createServer,
  delay,
  waitForFrames,
  withTimeout,
};
