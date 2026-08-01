import { spawnSync } from 'node:child_process';
import { readdir } from 'node:fs/promises';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';

const root = dirname(dirname(fileURLToPath(import.meta.url)));
const output = join(root, 'dist', 'npm');
const platformPackages = [
  'perk-workbench-darwin-arm64',
  'perk-workbench-darwin-x64',
  'perk-workbench-linux-arm64',
  'perk-workbench-linux-x64',
  'perk-workbench-win32-x64',
];
const tagPattern = /^v(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)(-((?:0|[1-9]\d*|[0-9A-Za-z-]*[A-Za-z-][0-9A-Za-z-]*)(?:\.(?:0|[1-9]\d*|[0-9A-Za-z-]*[A-Za-z-][0-9A-Za-z-]*))*))?$/;

export function parseReleaseTag(tag) {
  const match = tagPattern.exec(tag);
  if (!match) throw new Error(`Invalid release tag: ${tag}`);
  return { version: tag.slice(1), distTag: match[4] ? 'next' : 'latest' };
}

function run(command, args, options = {}) {
  const result = spawnSync(command, args, { cwd: root, encoding: 'utf8', ...options });
  if (result.error) throw result.error;
  if (result.status !== 0) throw new Error(`${command} ${args.join(' ')} failed:\n${result.stderr}`);
  return result.stdout;
}

function assertCleanWorktree() {
  const status = run('git', ['status', '--porcelain']);
  if (status.trim()) throw new Error('Refusing to publish from a dirty worktree');
}

export function publishPackages(archives, distTag, publish = run) {
  const publishArchives = [...platformPackages, 'perk-workbench'].map((name) => {
    const versionPrefix = new RegExp(`^${name}-(?:0|[1-9]\\d*)\\.`);
    const matches = archives.filter((archive) => versionPrefix.test(archive) && archive.endsWith('.tgz'));
    if (matches.length !== 1) throw new Error(`Expected one archive for ${name}, found ${matches.length}`);
    return matches[0];
  });
  for (const archive of publishArchives) {
    publish('npm', ['publish', join(output, archive), '--provenance', '--access', 'public', '--tag', distTag]);
  }
}

async function packageArchives(version) {
  run('node', ['scripts/npm-package.mjs', '--version', version]);
  run('node', ['scripts/npm-inspect.mjs', 'dist/npm', version]);
  return (await readdir(output)).filter((file) => file.endsWith('.tgz'));
}

async function main() {
  const args = process.argv.slice(2);
  if (args.length !== 2 || args[0] !== '--tag') throw new Error('Usage: node scripts/npm-publish.mjs --tag vMAJOR.MINOR.PATCH[-prerelease]');
  const release = parseReleaseTag(args[1]);
  assertCleanWorktree();
  run('go', ['test', '-race', './cmd/...', './internal/...']);
  run('go', ['vet', './cmd/...', './internal/...']);
  run('go', ['build', './cmd/perk-workbench']);
  if (run('gofmt', ['-l', 'cmd', 'internal']).trim()) throw new Error('gofmt reported unformatted Go files');
  run('node', ['--test', 'npm/launcher/test/child-exit.test.cjs', 'npm/launcher/test/mapping.test.cjs']);
  const archives = await packageArchives(release.version);
  publishPackages(archives, release.distTag);
}

if (process.argv[1] === fileURLToPath(import.meta.url)) {
  try {
    await main();
  } catch (error) {
    console.error(error.message);
    process.exitCode = 1;
  }
}
