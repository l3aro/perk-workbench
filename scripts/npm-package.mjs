import { cp, chmod, mkdir, mkdtemp, readFile, readdir, rm, stat, writeFile } from 'node:fs/promises';
import { createHash } from 'node:crypto';
import { fileURLToPath } from 'node:url';
import { dirname, join } from 'node:path';
import { tmpdir } from 'node:os';
import { spawnSync } from 'node:child_process';
import { inspectPackages } from './npm-inspect.mjs';

const root = dirname(dirname(fileURLToPath(import.meta.url)));
const output = join(root, 'dist', 'npm');
const officialManifestPath = join(root, 'plugins', 'official-manifest.json');
const targets = [
  ['darwin', 'amd64', 'x64'],
  ['darwin', 'arm64', 'arm64'],
  ['linux', 'amd64', 'x64'],
  ['linux', 'arm64', 'arm64'],
  ['windows', 'amd64', 'x64'],
];
const officialNames = ['sqlite', 'mysql', 'postgres', 'mongodb'];
const targetKeys = new Set(targets.map(([goos, goarch]) => `${goos}/${goarch}`));
const sha256Pattern = /^[0-9a-f]{64}$/i;
const versionPattern = /^(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)(?:-((?:0|[1-9]\d*|[0-9A-Za-z-]*[A-Za-z-][0-9A-Za-z-]*)(?:\.(?:0|[1-9]\d*|[0-9A-Za-z-]*[A-Za-z-][0-9A-Za-z-]*))*))?$/;

function parseVersion(args) {
  if (args.length !== 2 || args[0] !== '--version' || !versionPattern.test(args[1])) {
    throw new Error(`Invalid npm version: ${args[1] ?? '(missing)'}`);
  }
  return args[1];
}

function run(command, args, options = {}) {
  const result = spawnSync(command, args, { cwd: root, encoding: 'utf8', ...options });
  if (result.error) throw result.error;
  if (result.status !== 0) throw new Error(`${command} ${args.join(' ')} failed:\n${result.stderr}`);
  return result.stdout;
}

function targetKey(goos, goarch) {
  return `${goos}/${goarch}`;
}

function targetExecutable(name, goos) {
  return `perk-${name}${goos === 'windows' ? '.exe' : ''}`;
}

function validateDigest(value, label) {
  if (typeof value !== 'string' || !sha256Pattern.test(value)) {
    throw new Error(`${label} must be a 64-character SHA-256 digest`);
  }
}

export function validateManifest(manifest, { requireAssets = true } = {}) {
  if (!manifest || manifest.schema_version !== 1 || manifest.protocol !== 1) {
    throw new Error('official plugin manifest must declare schema_version 1 and protocol 1');
  }
  if (typeof manifest.tested_host_commit !== 'string' || !/^[0-9a-f]{40}$/.test(manifest.tested_host_commit)) {
    throw new Error('official plugin manifest tested_host_commit must be a 40-character lowercase commit');
  }
  if (!Array.isArray(manifest.plugins) || manifest.plugins.length !== officialNames.length) {
    throw new Error('official plugin manifest must contain exactly four official plugins');
  }
  const names = new Set();
  for (const plugin of manifest.plugins) {
    if (!plugin || !officialNames.includes(plugin.name) || names.has(plugin.name)) {
      throw new Error(`official plugin manifest has invalid or duplicate plugin ${plugin?.name ?? '(missing)'}`);
    }
    names.add(plugin.name);
    if (plugin.protocol !== 1 || plugin.tested_ref !== manifest.tested_host_commit) {
      throw new Error(`official plugin ${plugin.name} is not compatible with protocol 1 and the tested host commit`);
    }
    if (typeof plugin.repository !== 'string' || typeof plugin.release_tag !== 'string' || !plugin.repository || !plugin.release_tag) {
      throw new Error(`official plugin ${plugin.name} is missing repository or release_tag`);
    }
    if (!plugin.targets || Object.keys(plugin.targets).length !== targets.length) {
      throw new Error(`official plugin ${plugin.name} must declare all five host targets`);
    }
    for (const [goos, goarch] of targets) {
      const key = targetKey(goos, goarch);
      const asset = plugin.targets[key];
      if (!asset || typeof asset !== 'object') throw new Error(`official plugin ${plugin.name} is missing target ${key}`);
      if (asset.target?.goos !== goos || asset.target?.goarch !== goarch) {
        throw new Error(`official plugin ${plugin.name} target ${key} declares ${asset.target?.goos ?? '(missing)'}/${asset.target?.goarch ?? '(missing)'}`);
      }
      if (asset.executable !== targetExecutable(plugin.name, goos)) {
        throw new Error(`official plugin ${plugin.name} target ${key} has the wrong executable name`);
      }
      if (!requireAssets && asset.status === 'pending') continue;
      if (asset.status !== 'verified' || typeof asset.asset_url !== 'string' || !asset.asset_url) {
        throw new Error(`official plugin ${plugin.name} target ${key} has no verified release asset`);
      }
      validateDigest(asset.asset_sha256, `official plugin ${plugin.name} target ${key} asset_sha256`);
      validateDigest(asset.executable_sha256, `official plugin ${plugin.name} target ${key} executable_sha256`);
    }
  }
  for (const name of officialNames) if (!names.has(name)) throw new Error(`official plugin manifest is missing ${name}`);
  return manifest;
}

export async function readManifest(path = process.env.PERK_OFFICIAL_MANIFEST ?? officialManifestPath) {
  const manifest = JSON.parse(await readFile(path, 'utf8'));
  return validateManifest(manifest);
}

async function hashFile(path) {
  const hash = createHash('sha256');
  hash.update(await readFile(path));
  return hash.digest('hex');
}

async function downloadAsset(url, destination) {
  if (url.startsWith('file:')) {
    await cp(fileURLToPath(new URL(url)), destination);
    return;
  }
  const response = await fetch(url);
  if (!response.ok) throw new Error(`download ${url} failed with HTTP ${response.status}`);
  await writeFile(destination, Buffer.from(await response.arrayBuffer()));
}

async function extractArchive(archive, destination) {
  const pathname = archive.toLowerCase();
  if (pathname.endsWith('.zip')) {
    run('unzip', ['-q', archive, '-d', destination]);
  } else {
    run('tar', ['-xzf', archive, '-C', destination]);
  }
}

async function findExecutable(directory, expected) {
  const matches = [];
  async function visit(current) {
    for (const entry of await readdir(current, { withFileTypes: true })) {
      const path = join(current, entry.name);
      if (entry.isDirectory()) await visit(path);
      else if (entry.isFile() && entry.name === expected) matches.push(path);
    }
  }
  await visit(directory);
  if (matches.length !== 1) {
    throw new Error(`archive must contain exactly one ${expected} executable`);
  }
  return matches[0];
}

export async function stageOfficialPlugins(destination, goos, goarch, manifest, download = downloadAsset) {
  validateManifest(manifest);
  const key = targetKey(goos, goarch);
  if (!targetKeys.has(key)) throw new Error(`unsupported official plugin target ${key}`);
  const temporary = await mkdtemp(join(tmpdir(), 'perk-workbench-plugin-'));
  try {
    const pluginDirectory = join(destination, 'bin', 'plugins');
    await mkdir(pluginDirectory, { recursive: true });
    for (const plugin of manifest.plugins) {
      const asset = plugin.targets[key];
      const archive = join(temporary, `${plugin.name}-${goos}-${goarch}${asset.asset_url.toLowerCase().endsWith('.zip') ? '.zip' : '.tar.gz'}`);
      await download(asset.asset_url, archive);
      const archiveDigest = await hashFile(archive);
      if (archiveDigest !== asset.asset_sha256.toLowerCase()) {
        throw new Error(`official plugin ${plugin.name} target ${key} archive SHA-256 mismatch: expected ${asset.asset_sha256}, got ${archiveDigest}`);
      }
      const extracted = join(temporary, `${plugin.name}-${goos}-${goarch}`);
      await mkdir(extracted);
      await extractArchive(archive, extracted);
      const executable = await findExecutable(extracted, asset.executable);
      const executableDigest = await hashFile(executable);
      if (executableDigest !== asset.executable_sha256.toLowerCase()) {
        throw new Error(`official plugin ${plugin.name} target ${key} executable SHA-256 mismatch: expected ${asset.executable_sha256}, got ${executableDigest}`);
      }
      const mode = (await stat(executable)).mode;
      if (goos !== 'windows' && (mode & 0o111) === 0) {
        throw new Error(`official plugin ${plugin.name} target ${key} executable is not executable`);
      }
      const staged = join(pluginDirectory, asset.executable);
      await cp(executable, staged);
      if (goos !== 'windows') await chmod(staged, 0o755);
    }
  } finally {
    await rm(temporary, { recursive: true, force: true });
  }
}

async function manifest(template, destination, version) {
  const contents = JSON.parse(await readFile(template, 'utf8'));
  contents.version = version;
  contents.repository = { type: 'git', url: 'https://github.com/l3aro/perk-workbench' };
  if (contents.optionalDependencies) {
    for (const name of Object.keys(contents.optionalDependencies)) contents.optionalDependencies[name] = version;
  }
  await writeFile(destination, `${JSON.stringify(contents, null, 2)}\n`);
}

async function stagePackage(name, version) {
  const isLauncher = name === 'perk-workbench';
  const isSdk = name === 'perk-workbench-plugin-sdk';
  const source = join(root, 'npm', isLauncher ? 'launcher' : isSdk ? 'plugin-sdk' : 'platforms', isLauncher || isSdk ? '' : name);
  const destination = join(output, name);
  await cp(source, destination, { recursive: true });
  await cp(join(root, 'LICENSE'), join(destination, 'LICENSE'));
  await cp(join(root, 'README.md'), join(destination, 'README.md'));
  await manifest(join(source, 'package.json'), join(destination, 'package.json'), version);
  return destination;
}

async function buildTarget(goos, goarch, npmArch, version, officialManifest) {
  const name = `perk-workbench-${goos === 'windows' ? 'win32' : goos}-${npmArch}`;
  const destination = await stagePackage(name, version);
  const binary = join(destination, 'bin', `perk-workbench${goos === 'windows' ? '.exe' : ''}`);
  await mkdir(dirname(binary), { recursive: true });
  run('go', ['build', '-ldflags', `-X main.version=${version}`, '-o', binary, './cmd/perk-workbench'], {
    env: { ...process.env, CGO_ENABLED: '0', GOOS: goos, GOARCH: goarch },
  });
  if (goos !== 'windows') await chmod(binary, 0o755);
  await stageOfficialPlugins(destination, goos, goarch, officialManifest);
  return destination;
}

async function pack(directory) {
  run('npm', ['pack', '--json', '--pack-destination', output], { cwd: directory });
}

async function main() {
  const version = parseVersion(process.argv.slice(2));
  const officialManifest = await readManifest();
  await rm(output, { recursive: true, force: true });
  await mkdir(output, { recursive: true });
  const packages = [
    await stagePackage('perk-workbench', version),
    await stagePackage('perk-workbench-plugin-sdk', version),
  ];
  for (const target of targets) packages.push(await buildTarget(...target, version, officialManifest));
  for (const directory of packages) await pack(directory);
  const files = await inspectPackages(output, version);
  console.log(`Created ${files.length} npm tarballs in dist/npm`);
}

if (process.argv[1] === fileURLToPath(import.meta.url)) {
  try {
    await main();
  } catch (error) {
    console.error(error.message);
    process.exitCode = 1;
  }
}
