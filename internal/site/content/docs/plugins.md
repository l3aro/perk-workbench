---
title: Plugins
eyebrow: Documentation / extension protocol
order: 50
lede: Manage the plugins that drive database backends and workspace views — the four built-ins plus external executables you pin and approve yourself.
keywords: [plugins, Perk protocol, drivers, workspace views, extensions, sha256]
---

## What a plugin is

A plugin is a process that speaks the Perk protocol to the workbench. The four database backends — SQLite, MySQL, PostgreSQL, MongoDB — are built-in plugins: child processes of the same `perk-workbench` binary, launched with `--plugin sqlite`, `--plugin mysql`, `--plugin postgres`, or `--plugin mongodb`. External plugins are separate executables you choose to run.

Nothing is auto-discovered. `$XDG_CONFIG_HOME/perk-workbench/config.json` is the explicit allowlist: a plugin runs only when its descriptor is listed there, and a missing config file is materialized with the four built-ins in stable order:

```json
{
  "plugins": [
    {"builtin": "sqlite"},
    {"builtin": "mysql"},
    {"builtin": "postgres"},
    {"builtin": "mongodb"}
  ]
}
```

An external descriptor pins an executable by path with an optional SHA-256 digest, verified immediately before the process spawns:

```json
{
  "plugins": [
    {"builtin": "sqlite"},
    {"builtin": "postgres"},
    {"builtin": "mongodb"},
    {"path": "/home/alice/.local/bin/perk-redis",
     "sha256": "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"}
  ]
}
```

Remove a built-in line to disable that plugin; add a `path` entry to enable an external one. Config validation runs before any child starts: invalid descriptors, malformed lowercase digests, blank paths, and duplicates stop startup with a diagnostic.

## Manage the plugin set

```sh
perk-workbench plugin list
perk-workbench plugin inspect EXECUTABLE
perk-workbench plugin add EXECUTABLE
perk-workbench plugin add --approve SHA256 EXECUTABLE
perk-workbench plugin remove NAME_OR_PATH
perk-workbench plugin doctor
perk-workbench plugin test EXECUTABLE
```

Every command is scriptable: `--json` emits one machine-readable document on stdout, item-level failures are encoded in it, and diagnostics go to stderr only when no JSON document can be produced. Exit status is 0 on success, 1 on a plugin or operational failure, 2 on a usage error.

- **`plugin list [--json]`** reads the same config file and parser as startup and lists the configured entries in order — resolving each without spawning anything — and reports trust state (`unpinned`, or `pinned` with the configured fingerprint). An unresolvable entry is reported per entry and makes the exit status 1.
- **`plugin inspect [--json] EXECUTABLE`** runs one executable through the real loader lifecycle — spawn, `perk/v1/initialize` handshake, registration-invariant validation — then closes it cleanly. It works for executables not yet in config. A pinned executable whose bytes drifted is refused before the child spawns.
- **`plugin add [--json] EXECUTABLE`** is the preview stage: it resolves the executable, runs the full inspect lifecycle, and fingerprints the canonical bytes. The report shows the capabilities and `sha256` fingerprint with the config **never touched** — not even materialized when it does not exist. The output ends with `NOT ENABLED: rerun with --approve <fingerprint> to pin and enable this plugin`.
- **`plugin add --approve SHA256 EXECUTABLE`** is the mutating stage: resolve, inspect, and hash run again from scratch, and the command fails closed when the supplied digest does not exactly match the current bytes. Only then is the plugin persisted atomically — appended as its canonical absolute path (or replacing an existing entry resolving to the same file) with its trust record set, leaving every unrelated config key untouched. The `--json` flag works here too.
- **`plugin remove [--json] NAME_OR_PATH`** atomically removes one configured plugin and its trust record. The operand matches a configured entry exactly, or an executable that resolves to exactly one configured entry's canonical path; ambiguous matches and unknown operands fail instead of guessing.
- **`plugin doctor [--json] [EXECUTABLE...]`** checks every configured entry — or exactly the executables given — running the full resolve, pin-verify, initialize, register, shutdown lifecycle per item. The report marks the failing phase per item (`resolve`, `initialize`, `protocol`, `register`, `trust`, or `shutdown`) and the overall exit status is 1 when any item fails.
- **`plugin test [--json] EXECUTABLE`** runs the perk/v1 conformance suite against the executable, one case per fresh child. Exit status is 1 when any case fails.

## Trust and safety

A configured plugin is trusted executable code: the workbench spawns it with your OS privileges, so never add a plugin you do not trust. `config.json` is the allowlist, the SHA-256 pin makes a drifted external binary refuse to start, and every add/remove path is previewable or reversible — the two-stage `add` flow means nothing is enabled until you approve the exact fingerprint you saw.

## Perk v1

The Perk protocol boundary is what `inspect`, `doctor`, and `test` exercise. Plugins communicate over `perk-v1`: JSON-RPC 2.0 over newline-delimited JSON (NDJSON), one message per line, and the handshake echoes the requested protocol version (currently `1`). Unknown protocol members are ignored for forward compatibility, so additive changes stay interoperable without a version bump; breaking changes require `perk/v2` and are never folded into `perk-v1`. The host speaks protocol 1 only.