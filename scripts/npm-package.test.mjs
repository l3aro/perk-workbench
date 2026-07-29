import assert from 'node:assert/strict';
import { readdir, rm } from 'node:fs/promises';
import { join } from 'node:path';
import test from 'node:test';
import { execFile } from 'node:child_process';
import { promisify } from 'node:util';

const execFileAsync = promisify(execFile);
const root = new URL('..', import.meta.url).pathname;

test('rejects an invalid version without creating tarballs', async () => {
  const dist = join(root, 'dist', 'npm');

  try {
    await rm(dist, { recursive: true, force: true });

    await assert.rejects(
      execFileAsync('node', [join(root, 'scripts', 'npm-package.mjs'), '--version', '1.2'], {
        cwd: root,
      }),
      /Invalid npm version: 1\.2/,
    );
    await assert.rejects(readdir(dist), { code: 'ENOENT' });
  } finally {
    await rm(dist, { recursive: true, force: true });
  }
});
