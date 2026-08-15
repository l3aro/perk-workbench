import { cp, chmod, mkdir, readFile, rm, writeFile } from 'node:fs/promises';
import { fileURLToPath } from 'node:url';
import { dirname, join } from 'node:path';
import { spawnSync } from 'node:child_process';
import { inspectPackages } from './npm-inspect.mjs';

const root = dirname(dirname(fileURLToPath(import.meta.url)));
const output = join(root, 'dist', 'npm');
const targets = [
  ['darwin', 'amd64', 'x64'],
  ['darwin', 'arm64', 'arm64'],
  ['linux', 'amd64', 'x64'],
  ['linux', 'arm64', 'arm64'],
  ['windows', 'amd64', 'x64'],
];
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

async function buildTarget(goos, goarch, npmArch, version) {
  const name = `perk-workbench-${goos === 'windows' ? 'win32' : goos}-${npmArch}`;
  const destination = await stagePackage(name, version);
  const binary = join(destination, 'bin', `perk-workbench${goos === 'windows' ? '.exe' : ''}`);
  await mkdir(dirname(binary), { recursive: true });
  run('go', ['build', '-ldflags', `-X main.version=${version}`, '-o', binary, './cmd/perk-workbench'], {
    env: { ...process.env, CGO_ENABLED: '0', GOOS: goos, GOARCH: goarch },
  });
  if (goos !== 'windows') await chmod(binary, 0o755);
  return destination;
}

async function pack(directory) {
  run('npm', ['pack', '--json', '--pack-destination', output], { cwd: directory });
}

async function main() {
  const version = parseVersion(process.argv.slice(2));
  await rm(output, { recursive: true, force: true });
  await mkdir(output, { recursive: true });
  const packages = [
    await stagePackage('perk-workbench', version),
    await stagePackage('perk-workbench-plugin-sdk', version),
  ];
  for (const target of targets) packages.push(await buildTarget(...target, version));
  for (const directory of packages) await pack(directory);
  const files = await inspectPackages(output, version);
  console.log(`Created ${files.length} npm tarballs in dist/npm`);
}

try {
  await main();
} catch (error) {
  console.error(error.message);
  process.exitCode = 1;
}
