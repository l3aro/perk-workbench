import assert from 'node:assert/strict';
import { readFile, readdir } from 'node:fs/promises';
import { gunzipSync } from 'node:zlib';
import { join } from 'node:path';

const targets = [
  ['darwin', 'x64'],
  ['darwin', 'arm64'],
  ['linux', 'x64'],
  ['linux', 'arm64'],
  ['win32', 'x64'],
];
function field(block, start, end) {
  return block.subarray(start, end).toString('utf8').replace(/\0.*$/, '');
}

function tarEntries(buffer) {
  const entries = new Map();
  for (let offset = 0; offset < buffer.length;) {
    const header = buffer.subarray(offset, offset + 512);
    if (header.every((byte) => byte === 0)) break;
    const size = Number.parseInt(field(header, 124, 136).trim() || '0', 8);
    const name = field(header, 0, 100);
    entries.set(name, {
      content: buffer.subarray(offset + 512, offset + 512 + size),
      mode: Number.parseInt(field(header, 100, 108).trim() || '0', 8),
    });
    offset += 512 + Math.ceil(size / 512) * 512;
  }
  return entries;
}

function packageName(platform, arch) {
  return `perk-workbench-${platform}-${arch}`;
}

async function inspectArchive(path, version) {
  const archive = await readFile(path);
  const entries = tarEntries(gunzipSync(archive));
  const manifestEntry = entries.get('package/package.json');
  assert.ok(manifestEntry, `${path}: missing package.json`);
  const manifest = JSON.parse(manifestEntry.content);
  assert.equal(manifest.version, version, `${path}: package version`);
  assert.equal(manifest.license, 'MIT', `${path}: license`);
  assert.ok(entries.has('package/LICENSE'), `${path}: missing LICENSE`);

  if (manifest.name === 'perk-workbench') {
    assert.ok(entries.has('package/perk-workbench.cjs'), `${path}: missing launcher`);
    assert.ok(entries.has('package/README.md'), `${path}: missing README`);
    assert.deepEqual(manifest.bin, { 'perk-workbench': './perk-workbench.cjs' });
    for (const [platform, arch] of targets) {
      assert.equal(manifest.optionalDependencies[packageName(platform, arch)], version);
    }
    return;
  }

  if (manifest.name === 'perk-workbench-plugin-sdk') {
    assert.ok(entries.has('package/index.cjs'), `${path}: missing CommonJS implementation`);
    assert.ok(entries.has('package/index.d.ts'), `${path}: missing type declarations`);
    assert.equal(manifest.main, 'index.cjs', `${path}: main entry`);
    assert.equal(manifest.types, 'index.d.ts', `${path}: types entry`);
    assert.deepEqual(manifest.engines, { node: '>=18' }, `${path}: node floor`);
    assert.equal(manifest.dependencies, undefined, `${path}: sdk must be dependency-free`);
    assert.equal(manifest.bin, undefined, `${path}: sdk must not expose a bin`);
    const entryNames = [...entries.keys()];
    assert.ok(!entryNames.some((name) => name.startsWith('package/test/')), `${path}: sdk tests must not ship`);
    return;
  }

  const target = targets.find(([platform, arch]) => manifest.name === packageName(platform, arch));
  assert.ok(target, `${path}: unexpected package ${manifest.name}`);
  const [platform, arch] = target;
  assert.deepEqual(manifest.os, [platform], `${path}: os constraint`);
  assert.deepEqual(manifest.cpu, [arch], `${path}: cpu constraint`);
  assert.equal(manifest.bin, undefined, `${path}: platform package must not expose bin`);
  const binary = entries.get(`package/bin/perk-workbench${platform === 'win32' ? '.exe' : ''}`);
  assert.ok(binary, `${path}: missing native binary`);
  if (platform !== 'win32') assert.equal(binary.mode & 0o111, 0o111, `${path}: binary is executable`);

}

export async function inspectPackages(directory, version) {
  const files = (await readdir(directory)).filter((file) => file.endsWith('.tgz')).sort();
  assert.equal(files.length, 7, `${directory}: expected seven tarballs`);
  await Promise.all(files.map((file) => inspectArchive(join(directory, file), version)));
  return files;
}

if (process.argv[1] === new URL(import.meta.url).pathname) {
  const [directory, version] = process.argv.slice(2);
  if (!directory || !version) throw new Error('Usage: node scripts/npm-inspect.mjs <directory> <version>');
  const files = await inspectPackages(directory, version);
  console.log(`Inspected ${files.length} npm tarballs in ${directory}`);
}
