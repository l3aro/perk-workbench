import assert from 'node:assert/strict';
import { createHash } from 'node:crypto';
import { cp, chmod, mkdir, mkdtemp, readFile, readdir, rm, writeFile } from 'node:fs/promises';
import test from 'node:test';
import { execFile } from 'node:child_process';
import { promisify } from 'node:util';
import { dirname, join } from 'node:path';
import { fileURLToPath, pathToFileURL } from 'node:url';
import { tmpdir } from 'node:os';
import { stageOfficialPlugins } from './npm-package.mjs';
import { inspectPackages } from './npm-inspect.mjs';

const execFileAsync = promisify(execFile);
const root = dirname(fileURLToPath(import.meta.url));
const targets = [
  ['darwin', 'amd64'],
  ['darwin', 'arm64'],
  ['linux', 'amd64'],
  ['linux', 'arm64'],
  ['windows', 'amd64'],
];
const pluginNames = ['sqlite', 'mysql', 'postgres', 'mongodb'];
const testedHostCommit = 'f64c76dfa6e901fa6c1edb8e662554f378d33028';

function digest(contents) {
  return createHash('sha256').update(contents).digest('hex');
}

async function archiveFixture(directory, name, goos, goarch) {
  const executable = `perk-${name}${goos === 'windows' ? '.exe' : ''}`;
  const payload = join(directory, `${name}-${goos}-${goarch}-payload`);
  const archive = join(directory, `${name}-${goos}-${goarch}.tar.gz`);
  const contents = Buffer.from(`${name}:${goos}:fixture\n`);
  await mkdir(payload);
  await writeFile(join(payload, executable), contents, { mode: goos === 'windows' ? 0o644 : 0o755 });
  if (goos === 'windows') {
    const zip = archive.replace(/\.tar\.gz$/, '.zip');
    await execFileAsync('zip', ['-q', zip, executable], { cwd: payload });
    return { target: { goos, goarch }, asset_url: pathToFileURL(zip).href, asset_sha256: digest(await readFile(zip)), executable, executable_sha256: digest(contents) };
  }
  await execFileAsync('tar', ['-czf', archive, '-C', payload, executable]);
  return { target: { goos, goarch }, asset_url: pathToFileURL(archive).href, asset_sha256: digest(await readFile(archive)), executable, executable_sha256: digest(contents) };
}

async function fixtureManifest(directory) {
  const manifest = { schema_version: 1, protocol: 1, tested_host_commit: testedHostCommit, plugins: [] };
  for (const name of pluginNames) {
    const plugin = {
      name,
      repository: `https://github.com/l3aro/${name}-driver-for-perk-workbench`,
      release_tag: 'v0.1.0',
      protocol: 1,
      tested_ref: testedHostCommit,
      targets: {},
    };
    for (const [goos, goarch] of targets) plugin.targets[`${goos}/${goarch}`] = { status: 'verified', ...(await archiveFixture(directory, name, goos, goarch)) };
    manifest.plugins.push(plugin);
  }
  return manifest;
}

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

test('production manifest rejects pending release metadata', async () => {
  await assert.rejects(execFileAsync('node', [join(root, 'npm-package.mjs'), '--version', '1.2.3'], { cwd: dirname(root) }), /has no verified release asset/);
});

test('stages only manifest-selected, verified plugin assets', async () => {
  const directory = await mkdtemp(join(tmpdir(), 'npm-package-test-'));
  try {
    const manifest = await fixtureManifest(directory);
    const destination = join(directory, 'package');
    const downloads = [];
    await stageOfficialPlugins(destination, 'linux', 'amd64', manifest, async (url, output) => {
      downloads.push(url);
      await cp(fileURLToPath(new URL(url)), output);
    });
    assert.equal(downloads.length, 4);
    for (const name of pluginNames) assert.equal(await readFile(join(destination, 'bin', 'plugins', `perk-${name}`), 'utf8'), `${name}:linux:fixture\n`);
  } finally {
    await rm(directory, { recursive: true, force: true });
  }
});

test('rejects missing targets, archive hash mismatches, and non-executable Unix assets', async () => {
  const directory = await mkdtemp(join(tmpdir(), 'npm-package-test-'));
  try {
    const manifest = await fixtureManifest(directory);
    const missingTarget = structuredClone(manifest);
    delete missingTarget.plugins[0].targets['linux/amd64'];
    await assert.rejects(stageOfficialPlugins(join(directory, 'missing'), 'linux', 'amd64', missingTarget), /must declare all five host targets/);

    const badArchive = structuredClone(manifest);
    badArchive.plugins[0].targets['linux/amd64'].asset_sha256 = '0'.repeat(64);
    await assert.rejects(stageOfficialPlugins(join(directory, 'bad-archive'), 'linux', 'amd64', badArchive), /archive SHA-256 mismatch/);
    const wrongArchitecture = structuredClone(manifest);
    wrongArchitecture.plugins[0].targets['linux/amd64'].target.goarch = 'arm64';
    await assert.rejects(stageOfficialPlugins(join(directory, 'wrong-architecture'), 'linux', 'amd64', wrongArchitecture), /declares linux\/arm64/);
    const badExecutable = structuredClone(manifest);
    badExecutable.plugins[0].targets['linux/amd64'].executable_sha256 = '0'.repeat(64);
    await assert.rejects(stageOfficialPlugins(join(directory, 'bad-executable'), 'linux', 'amd64', badExecutable), /executable SHA-256 mismatch/);

    const wrongTarget = structuredClone(manifest);
    wrongTarget.plugins[0].targets['linux/amd64'].executable = 'perk-sqlite.exe';
    await assert.rejects(stageOfficialPlugins(join(directory, 'wrong-target'), 'linux', 'amd64', wrongTarget), /wrong executable name/);

    const nonExecutable = structuredClone(manifest);
    const executable = join(directory, 'sqlite-linux-amd64-payload', 'perk-sqlite');
    await chmod(executable, 0o644);
    const archive = join(directory, 'sqlite-linux-nonexec.tar.gz');
    await execFileAsync('tar', ['-czf', archive, '-C', dirname(executable), 'perk-sqlite']);
    nonExecutable.plugins[0].targets['linux/amd64'] = { ...nonExecutable.plugins[0].targets['linux/amd64'], asset_url: pathToFileURL(archive).href, asset_sha256: digest(await readFile(archive)) };
    await assert.rejects(stageOfficialPlugins(join(directory, 'nonexec'), 'linux', 'amd64', nonExecutable), /not executable/);
  } finally {
    await rm(directory, { recursive: true, force: true });
  }
});

test('injects repository metadata and stages all four plugins in every platform package', async () => {
  const fixtureDirectory = await mkdtemp(join(tmpdir(), 'npm-package-test-'));
  const manifestPath = join(fixtureDirectory, 'official-manifest.json');
  const dist = join(dirname(root), 'dist', 'npm');
  try {
    await writeFile(manifestPath, `${JSON.stringify(await fixtureManifest(fixtureDirectory), null, 2)}\n`);
    await execFileAsync('node', [join(root, 'npm-package.mjs'), '--version', '1.2.3'], { cwd: dirname(root), env: { ...process.env, PERK_OFFICIAL_MANIFEST: manifestPath } });
    await inspectPackages(dist, '1.2.3');
    for (const name of ['perk-workbench', 'perk-workbench-linux-x64', 'perk-workbench-plugin-sdk']) {
      const packageManifest = JSON.parse(await readFile(join(dist, name, 'package.json'), 'utf8'));
      assert.deepEqual(packageManifest.repository, { type: 'git', url: 'https://github.com/l3aro/perk-workbench' });
    }
    for (const [platform, arch] of [['darwin', 'arm64'], ['darwin', 'x64'], ['linux', 'arm64'], ['linux', 'x64'], ['win32', 'x64']]) {
      const goos = platform === 'win32' ? 'windows' : platform;
      const suffix = platform === 'win32' ? '.exe' : '';
      for (const name of pluginNames) assert.equal(await readFile(join(dist, `perk-workbench-${platform}-${arch}`, 'bin', 'plugins', `perk-${name}${suffix}`), 'utf8'), `${name}:${goos}:fixture\n`);
    }
  } finally {
    await rm(dist, { recursive: true, force: true });
    await rm(fixtureDirectory, { recursive: true, force: true });
  }
});
