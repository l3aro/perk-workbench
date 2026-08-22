import assert from 'node:assert/strict';
import { execFile } from 'node:child_process';
import { chmod, cp, mkdir, mkdtemp, readFile, rm, writeFile } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import test from 'node:test';
import { promisify } from 'node:util';

import { parseReleaseTag, publishPackages } from './npm-publish.mjs';

const execFileAsync = promisify(execFile);
const root = new URL('..', import.meta.url).pathname;
const platformArchives = [
  'perk-workbench-darwin-arm64-1.2.3.tgz',
  'perk-workbench-linux-arm64-1.2.3.tgz',
  'perk-workbench-linux-x64-1.2.3.tgz',
  'perk-workbench-win32-x64-1.2.3.tgz',
];
const sdkArchive = 'perk-workbench-plugin-sdk-1.2.3.tgz';

async function fakeReleaseFixture(archives) {
  const directory = await mkdtemp(join(tmpdir(), 'perk-npm-publish-'));
  const bin = join(directory, 'bin');
  const output = join(directory, 'dist', 'npm');
  await mkdir(bin, { recursive: true });
  await mkdir(join(directory, 'scripts'));
  await mkdir(output, { recursive: true });
  await cp(join(root, 'scripts', 'npm-publish.mjs'), join(directory, 'scripts', 'npm-publish.mjs'));
  await Promise.all(archives.map((archive) => writeFile(join(output, archive), 'fixture')));
  for (const command of ['git', 'go', 'gofmt', 'node']) {
    const path = join(bin, command);
    await writeFile(path, '#!/bin/sh\nexit 0\n');
    await chmod(path, 0o755);
  }
  const npm = join(bin, 'npm');
  await writeFile(
    npm,
    '#!/bin/sh\n'
      + 'if [ "$1" = "view" ]; then\n'
      + '  case " ${NPM_PUBLISHED:-} " in\n'
      + '    *" $2 "*) printf "%s\\n" "${2#*@}"; exit 0 ;;\n'
      + '  esac\n'
      + '  exit 1\n'
      + 'fi\n'
      + 'printf "%s\\n" "$*" >> "$NPM_LOG"\n'
      + 'exit 0\n',
  );
  await chmod(npm, 0o755);
  return { directory, bin, log: join(directory, 'npm.log'), output };
}

async function runFakeRelease(fixture, published = []) {
  return execFileAsync(process.execPath, ['scripts/npm-publish.mjs', '--tag', 'v1.2.3'], {
    cwd: fixture.directory,
    env: {
      ...process.env,
      NPM_LOG: fixture.log,
      NPM_PUBLISHED: published.join(' '),
      PATH: `${fixture.bin}:${process.env.PATH}`,
    },
  });
}


test('accepts stable and prerelease release tags', () => {
  // Given
  const stable = 'v1.2.3';
  const prerelease = 'v1.2.3-rc.1';

  // When
  const stableRelease = parseReleaseTag(stable);
  const prereleaseRelease = parseReleaseTag(prerelease);

  // Then
  assert.deepEqual(stableRelease, { version: '1.2.3', distTag: 'latest' });
  assert.deepEqual(prereleaseRelease, { version: '1.2.3-rc.1', distTag: 'next' });
});

test('rejects incomplete, unprefixed, and metadata tags', () => {
  // Given
  const invalidTags = ['1.2.3', 'v1.2', 'v1.2.3+build.1'];

  // When / Then
  for (const tag of invalidTags) {
    assert.throws(() => parseReleaseTag(tag), /Invalid release tag/);
  }
});

test('publishes all platform archives before the launcher', () => {
  // Given
  const invocations = [];
  const archives = [
    'perk-workbench-1.2.3.tgz',
    'perk-workbench-plugin-sdk-1.2.3.tgz',
    'perk-workbench-darwin-arm64-1.2.3.tgz',
    'perk-workbench-linux-arm64-1.2.3.tgz',
    'perk-workbench-linux-x64-1.2.3.tgz',
    'perk-workbench-win32-x64-1.2.3.tgz',
    'stale.tgz',
  ];

  // When
  publishPackages(
    archives,
    'latest',
    (command, args) => invocations.push([command, ...args]),
    () => '',
  );

  // Then
  assert.equal(invocations.length, 6);
  assert.deepEqual(
    invocations.map((invocation) => invocation[2]),
    [
      ...platformArchives.map((archive) => join(root, 'dist', 'npm', archive)),
      join(root, 'dist', 'npm', sdkArchive),
      join(root, 'dist', 'npm', 'perk-workbench-1.2.3.tgz'),
    ],
  );
  for (const invocation of invocations) {
    assert.deepEqual(invocation.slice(0, 2), ['npm', 'publish']);
    assert.ok(invocation.includes('--provenance'));
    assert.ok(invocation.includes('--access'));
    assert.ok(invocation.includes('public'));
    assert.ok(invocation.includes('--tag'));
    assert.ok(invocation.includes('latest'));
  }

  const prereleaseInvocations = [];
  publishPackages(
    archives,
    'next',
    (command, args) => prereleaseInvocations.push([command, ...args]),
    () => '',
  );
  assert.ok(prereleaseInvocations.every((invocation) => invocation.includes('next')));
});

test('skips packages whose exact version is already published', () => {
  // Given
  const invocations = [];
  const viewed = [];
  const archives = [...platformArchives, sdkArchive, 'perk-workbench-1.2.3.tgz'];
  const alreadyPublished = new Set([
    'perk-workbench-darwin-arm64@1.2.3',
    'perk-workbench-linux-x64@1.2.3',
    'perk-workbench-plugin-sdk@1.2.3',
  ]);

  // When
  publishPackages(
    archives,
    'latest',
    (command, args) => invocations.push([command, ...args]),
    (command, args) => {
      viewed.push([command, ...args]);
      const packageVersion = args[1];
      return alreadyPublished.has(packageVersion) ? `${packageVersion.slice(packageVersion.lastIndexOf('@') + 1)}\n` : '';
    },
  );

  // Then
  assert.deepEqual(
    viewed.map((invocation) => invocation[2]),
    [
      'perk-workbench-darwin-arm64@1.2.3',
      'perk-workbench-linux-arm64@1.2.3',
      'perk-workbench-linux-x64@1.2.3',
      'perk-workbench-win32-x64@1.2.3',
      'perk-workbench-plugin-sdk@1.2.3',
      'perk-workbench@1.2.3',
    ],
  );
  assert.deepEqual(
    invocations.map((invocation) => invocation[2]),
    [
      ...platformArchives
        .filter((archive) => !['perk-workbench-darwin-arm64-1.2.3.tgz', 'perk-workbench-linux-x64-1.2.3.tgz'].includes(archive))
        .map((archive) => join(root, 'dist', 'npm', archive)),
      join(root, 'dist', 'npm', 'perk-workbench-1.2.3.tgz'),
    ],
  );
});

test('publishes platform archives before the launcher when the CLI runs against fake npm', async () => {
  // Given
  const fixture = await fakeReleaseFixture([...platformArchives, sdkArchive, 'perk-workbench-1.2.3.tgz']);
  try {
    // When
    await runFakeRelease(fixture);

    // Then
    const log = await readFile(fixture.log, 'utf8');
    assert.deepEqual(
      log.trim().split('\n').map((line) => line.split(' ')[1]),
      [...platformArchives, sdkArchive, 'perk-workbench-1.2.3.tgz'].map((archive) => join(fixture.output, archive)),
    );
  } finally {
    await rm(fixture.directory, { recursive: true, force: true });
  }
});

test('skips already-published packages when the CLI runs against fake npm', async () => {
  // Given
  const fixture = await fakeReleaseFixture([...platformArchives, sdkArchive, 'perk-workbench-1.2.3.tgz']);
  const published = [
    'perk-workbench-darwin-arm64@1.2.3',
    'perk-workbench-linux-x64@1.2.3',
    'perk-workbench-plugin-sdk@1.2.3',
  ];
  try {
    // When
    await runFakeRelease(fixture, published);

    // Then
    const log = await readFile(fixture.log, 'utf8');
    assert.deepEqual(
      log.trim().split('\n').map((line) => line.split(' ')[1]),
      [
        ...platformArchives
          .filter((archive) => !['perk-workbench-darwin-arm64-1.2.3.tgz', 'perk-workbench-linux-x64-1.2.3.tgz'].includes(archive))
          .map((archive) => join(fixture.output, archive)),
        join(fixture.output, 'perk-workbench-1.2.3.tgz'),
      ],
    );
  } finally {
    await rm(fixture.directory, { recursive: true, force: true });
  }
});

test('does not invoke fake npm when the sdk archive is missing', async () => {
  // Given
  const fixture = await fakeReleaseFixture([...platformArchives, 'perk-workbench-1.2.3.tgz']);
  try {
    // When / Then
    await assert.rejects(runFakeRelease(fixture), /Expected one archive for perk-workbench-plugin-sdk, found 0/);
    await assert.rejects(readFile(fixture.log), { code: 'ENOENT' });
  } finally {
    await rm(fixture.directory, { recursive: true, force: true });
  }
});

test('does not invoke fake npm when an archive is missing', async () => {
  // Given
  const fixture = await fakeReleaseFixture([...platformArchives.slice(0, -1), 'perk-workbench-1.2.3.tgz']);
  try {
    // When / Then
    await assert.rejects(runFakeRelease(fixture), /Expected one archive for perk-workbench-win32-x64, found 0/);
    await assert.rejects(readFile(fixture.log), { code: 'ENOENT' });
  } finally {
    await rm(fixture.directory, { recursive: true, force: true });
  }
});
