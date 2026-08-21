import assert from 'node:assert/strict';
import { readFile, readdir, rm } from 'node:fs/promises';
import test from 'node:test';
import { execFile } from 'node:child_process';
import { promisify } from 'node:util';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { inspectPackages } from './npm-inspect.mjs';


const execFileAsync = promisify(execFile);
const root = dirname(fileURLToPath(import.meta.url));

test('rejects an invalid version without creating tarballs', async () => {
  const dist = join(dirname(root), 'dist', 'npm');
  try {
    await rm(dist, { recursive: true, force: true });
    await assert.rejects(execFileAsync('node', [join(root, 'npm-package.mjs'), '--version', '1.2'], { cwd: dirname(root) }), /Invalid npm version: 1\.2/);
    await assert.rejects(readdir(dist), { code: 'ENOENT' });
  } finally {
    await rm(dist, { recursive: true, force: true });
  }
});


test('injects repository metadata and builds every platform package', async () => {
  const dist = join(dirname(root), 'dist', 'npm');
  try {
    await execFileAsync('node', [join(root, 'npm-package.mjs'), '--version', '1.2.3'], { cwd: dirname(root) });
    await inspectPackages(dist, '1.2.3');
    for (const name of ['perk-workbench', 'perk-workbench-linux-x64', 'perk-workbench-plugin-sdk']) {
      const packageManifest = JSON.parse(await readFile(join(dist, name, 'package.json'), 'utf8'));
      assert.deepEqual(packageManifest.repository, { type: 'git', url: 'https://github.com/l3aro/perk-workbench' });
    }
  } finally {
    await rm(dist, { recursive: true, force: true });
  }
});
