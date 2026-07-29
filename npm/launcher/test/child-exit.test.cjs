// test/child-exit.test.cjs — exit code/signal passthrough for the run function.
// Uses injected spawn function to avoid forking real processes.

const { describe, it, mock } = require('node:test');
const assert = require('node:assert');
const { EventEmitter } = require('node:events');
const launcher = require('../perk-workbench.cjs');

/**
 * Create a fake spawn that returns an EventEmitter-shaped child.
 * The returned child fires 'exit' after a microtask.
 */
function fakeSpawn(exitCode, exitSignal) {
  return (_binPath, _args, _opts) => {
    const child = new EventEmitter();
    child.pid = 9999;
    // Simulate async exit on next tick.
    process.nextTick(() => child.emit('exit', exitCode, exitSignal));
    return child;
  };
}

describe('run exit code passthrough', () => {
  it('exits with code 0 when child exits 0', async () => {
    // We need to intercept process.exit to avoid actually exiting.
    const origExit = process.exit;
    let capturedCode = null;
    process.exit = (code) => { capturedCode = code; };

    try {
      launcher.run('/fake/bin', [], fakeSpawn(0, null));
      // Wait for next tick.
      await new Promise(r => setImmediate(r));
      assert.strictEqual(capturedCode, 0);
    } finally {
      process.exit = origExit;
    }
  });

  it('exits with child exit code when non-zero', async () => {
    const origExit = process.exit;
    let capturedCode = null;
    process.exit = (code) => { capturedCode = code; };

    try {
      launcher.run('/fake/bin', [], fakeSpawn(42, null));
      await new Promise(r => setImmediate(r));
      assert.strictEqual(capturedCode, 42);
    } finally {
      process.exit = origExit;
    }
  });

  it('exits with 1 when child exit code is null without signal', async () => {
    const origExit = process.exit;
    let capturedCode = null;
    process.exit = (code) => { capturedCode = code; };

    try {
      launcher.run('/fake/bin', [], fakeSpawn(null, null));
      await new Promise(r => setImmediate(r));
      assert.strictEqual(capturedCode, 1);
    } finally {
      process.exit = origExit;
    }
  });
});

describe('run error handling', () => {
  it('emits error and exits 1 on spawn error', async () => {
    const origExit = process.exit;
    const origError = console.error;
    let capturedCode = null;
    let capturedErrorMsg = '';
    process.exit = (code) => { capturedCode = code; };
    console.error = (...args) => { capturedErrorMsg = args.join(' '); };

    try {
      const errorSpawn = () => {
        const child = new EventEmitter();
        process.nextTick(() => child.emit('error', new Error('ENOENT')));
        return child;
      };
      launcher.run('/nonexistent', [], errorSpawn);
      await new Promise(r => setImmediate(r));
      assert.strictEqual(capturedCode, 1);
      assert.ok(capturedErrorMsg.includes('ENOENT'));
    } finally {
      process.exit = origExit;
      console.error = origError;
    }
  });
});
