import assert from 'node:assert/strict';
import { readFile, readdir, rm } from 'node:fs/promises';
import test from 'node:test';
import { execFile } from 'node:child_process';
import { promisify } from 'node:util';
import { join } from 'node:path';

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

test('injects repository metadata into generated manifests', async () => {
  const dist = join(root, 'dist', 'npm');

  try {
    await execFileAsync('node', [join(root, 'scripts', 'npm-package.mjs'), '--version', '1.2.3'], {
      cwd: root,
    });
    for (const name of ['perk-workbench', 'perk-workbench-linux-x64']) {
      const manifest = JSON.parse(await readFile(join(dist, name, 'package.json'), 'utf8'));
      assert.deepEqual(manifest.repository, {
        type: 'git',
        url: 'https://github.com/l3aro/perk-workbench',
      });
    }
  } finally {
    await rm(dist, { recursive: true, force: true });
  }
});
