#!/usr/bin/env node
// perk-workbench.cjs — CommonJS launcher using only Node built-ins.
// Resolves the host-architecture binary, inherits stdio, proxies exit/signal.

const { spawn } = require('node:child_process');
const path = require('node:path');

// ---------------------------------------------------------------------------
// Deterministic platform → package-name mapping for the five approved targets.
// ---------------------------------------------------------------------------
const PLATFORM_PACKAGES = {
  darwin: { x64: 'perk-workbench-darwin-x64', arm64: 'perk-workbench-darwin-arm64' },
  linux:  { x64: 'perk-workbench-linux-x64',  arm64: 'perk-workbench-linux-arm64' },
  win32:  { x64: 'perk-workbench-win32-x64' },
};

/**
 * @param {string} platform — Node `process.platform` value
 * @param {string} arch     — Node `process.arch` value
 * @returns {string|null}   — npm package name or null for unsupported combos
 */
function resolvePackageName(platform, arch) {
  return PLATFORM_PACKAGES[platform]?.[arch] ?? null;
}

/**
 * @param {string} platform
 * @returns {string} — binary filename (with `.exe` on win32)
 */
function binaryName(platform) {
  return 'perk-workbench' + (platform === 'win32' ? '.exe' : '');
}

/**
 * Resolve the absolute path of the native binary for the current platform.
 *
 * @param {string}   platform
 * @param {string}   arch
 * @param {Function} [resolveModule] — test seam, defaults to `require.resolve`
 * @returns {string} — absolute binary path
 */
function resolveBinaryPath(platform, arch, resolveModule) {
  const pkgName = resolvePackageName(platform, arch);
  if (!pkgName) {
    throw Object.assign(
      new Error(
        `Unsupported platform: ${platform} ${arch}\n` +
        `Supported targets: darwin (x64, arm64), linux (x64, arm64), win32 (x64)`
      ),
      { code: 'ERR_UNSUPPORTED_PLATFORM' },
    );
  }

  const relPath = path.posix.join(pkgName, 'bin', binaryName(platform));
  try {
    return (resolveModule || require.resolve)(relPath);
  } catch (_) {
    throw Object.assign(
      new Error(
        `Missing platform package: ${pkgName}\n` +
        `The platform binary was not found. This usually happens when\n` +
        `optional dependencies were skipped during installation.\n` +
        `\n` +
        `Fix: re-install and allow optional dependencies:\n` +
        `  npm install  (or your package manager's equivalent)\n` +
        `\n` +
        `Skip this check with:\n` +
        `  npm install --no-optional\n` +
        `(the launcher will fail again at runtime — install the matching\n` +
        ` platform package manually to proceed)`
      ),
      { code: 'ERR_MISSING_PACKAGE' },
    );
  }
}

/**
 * Spawn the native binary, inherit stdio, and proxy its exit.
 */
function run(binPath, args, spawnFn) {
  const child = (spawnFn || spawn)(binPath, args, {
    stdio: 'inherit',
    windowsHide: false,
  });

  child.on('exit', (code, signal) => {
    if (signal !== null && signal !== undefined) {
      // Pass through kill signal to this process.
      process.kill(process.pid, signal);
    } else {
      process.exit(code === null ? 1 : code);
    }
  });

  child.on('error', (err) => {
    console.error('Failed to launch perk-workbench:', err.message);
    process.exit(1);
  });
}

// ---------------------------------------------------------------------------
// Entry point — only when invoked directly via CLI
// ---------------------------------------------------------------------------
if (require.main === module) {
  try {
    const binPath = resolveBinaryPath(process.platform, process.arch);
    run(binPath, process.argv.slice(2));
  } catch (err) {
    console.error(err.message);
    process.exit(1);
  }
}

module.exports = { resolvePackageName, resolveBinaryPath, binaryName, run };
