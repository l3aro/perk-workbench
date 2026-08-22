// test/mapping.test.cjs — platform mapping, binary naming, resolution, diagnostics

const { describe, it } = require('node:test');
const assert = require('node:assert');

// Attempt to load the launcher — these will fail red before implementation.
let launcher;
try {
  launcher = require('../perk-workbench.cjs');
} catch (_) {
  // RED: module not found yet — tests will fail as expected.
}

// ---------------------------------------------------------------------------
// Platform mapping — four approved targets
// ---------------------------------------------------------------------------
describe('platform mapping', { concurrency: false }, () => {
  it('maps darwin arm64 to perk-workbench-darwin-arm64', () => {
    assert.strictEqual(
      launcher.resolvePackageName('darwin', 'arm64'),
      'perk-workbench-darwin-arm64',
    );
  });

  it('maps linux x64 to perk-workbench-linux-x64', () => {
    assert.strictEqual(
      launcher.resolvePackageName('linux', 'x64'),
      'perk-workbench-linux-x64',
    );
  });

  it('maps linux arm64 to perk-workbench-linux-arm64', () => {
    assert.strictEqual(
      launcher.resolvePackageName('linux', 'arm64'),
      'perk-workbench-linux-arm64',
    );
  });

  it('maps win32 x64 to perk-workbench-win32-x64', () => {
    assert.strictEqual(
      launcher.resolvePackageName('win32', 'x64'),
      'perk-workbench-win32-x64',
    );
  });

  it('returns null for removed and unsupported platform architectures', () => {
    assert.strictEqual(launcher.resolvePackageName('darwin', 'x64'), null);
    assert.strictEqual(launcher.resolvePackageName('linux', 'ia32'), null);
    assert.strictEqual(launcher.resolvePackageName('win32', 'arm64'), null);
    assert.strictEqual(launcher.resolvePackageName('darwin', 'ia32'), null);
  });

  it('returns null for unrecognised platform name', () => {
    assert.strictEqual(launcher.resolvePackageName('freebsd', 'x64'), null);
    assert.strictEqual(launcher.resolvePackageName('android', 'arm64'), null);
  });
});

// ---------------------------------------------------------------------------
// Binary name
// ---------------------------------------------------------------------------
describe('binaryName', () => {
  it('appends .exe for win32', () => {
    assert.strictEqual(launcher.binaryName('win32'), 'perk-workbench.exe');
  });

  it('omits .exe for darwin', () => {
    assert.strictEqual(launcher.binaryName('darwin'), 'perk-workbench');
  });

  it('omits .exe for linux', () => {
    assert.strictEqual(launcher.binaryName('linux'), 'perk-workbench');
  });
});

// ---------------------------------------------------------------------------
// Binary path resolution
// ---------------------------------------------------------------------------
describe('resolveBinaryPath', () => {
  it('resolves darwin arm64 via injected resolve function', () => {
    const fakeResolve = (_req) => '/node_modules/perk-workbench-darwin-arm64/bin/perk-workbench';
    const p = launcher.resolveBinaryPath('darwin', 'arm64', fakeResolve);
    assert.strictEqual(p, '/node_modules/perk-workbench-darwin-arm64/bin/perk-workbench');
  });

  it('resolves win32 x64 with .exe via injected resolve', () => {
    const fakeResolve = (_req) => '/node_modules/perk-workbench-win32-x64/bin/perk-workbench.exe';
    const p = launcher.resolveBinaryPath('win32', 'x64', fakeResolve);
    assert.match(p, /\.exe$/);
  });

  it('throws ERR_UNSUPPORTED_PLATFORM for unrecognised combo', () => {
    assert.throws(
      () => launcher.resolveBinaryPath('freebsd', 'x64', () => '/ignored'),
      { code: 'ERR_UNSUPPORTED_PLATFORM' },
    );
  });

  it('throws ERR_UNSUPPORTED_PLATFORM for the removed darwin x64 target', () => {
    assert.throws(
      () => launcher.resolveBinaryPath('darwin', 'x64', () => '/ignored'),
      { code: 'ERR_UNSUPPORTED_PLATFORM' },
    );
  });

  it('throws ERR_MISSING_PACKAGE when require.resolve throws', () => {
    const failingResolve = () => { throw new Error('MODULE_NOT_FOUND'); };
    assert.throws(
      () => launcher.resolveBinaryPath('darwin', 'arm64', failingResolve),
      { code: 'ERR_MISSING_PACKAGE' },
    );
  });

  it('missing package diagnostic includes --no-optional remediation', () => {
    const failingResolve = () => { throw new Error('not found'); };
    assert.throws(
      () => launcher.resolveBinaryPath('darwin', 'arm64', failingResolve),
      (err) => {
        const msg = err.message;
        return (
          msg.includes('Missing platform package') &&
          msg.includes('perk-workbench-darwin-arm64') &&
          msg.includes('--no-optional')
        );
      },
    );
  });

  it('unsupported platform error lists all supported targets', () => {
    assert.throws(
      () => launcher.resolveBinaryPath('win32', 'arm64', () => '/x'),
      (err) => {
        const msg = err.message;
        return (
          msg.includes('darwin (arm64)') &&
          msg.includes('linux (x64, arm64)') &&
          msg.includes('win32 (x64)')
        );
      },
    );
  });
});
