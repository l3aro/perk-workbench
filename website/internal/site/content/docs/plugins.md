---
title: Plugins
eyebrow: Documentation / extension protocol
lede: Extend Perk Workbench with external processes while keeping the host boundary explicit, inspectable, and versioned.
keywords: [plugins, Perk protocol, drivers, workspace views, extensions]
---

## Perk v1

Plugins communicate through the `perk-v1` JSON-RPC protocol over newline-delimited JSON (NDJSON): one JSON-RPC message per line.

Configuration is constrained by an allowlist. The host controls which external process and configuration are permitted. `config.json` lists permitted plugin executables under its `plugins` allowlist; discovery is explicit and never automatic. Unknown protocol members are ignored for forward compatibility.

Breaking protocol changes require `v2`; they do not get folded into `perk-v1`.

## Manage the plugin set

Inspect and validate installations with:

```sh
perk-workbench plugin list
perk-workbench plugin inspect
perk-workbench plugin add
perk-workbench plugin remove
perk-workbench plugin doctor
perk-workbench plugin test
```

`list` shows plugins, `inspect` reports manifest and protocol details, `add` and `remove` change the configured set, and `doctor` and `test` check an installation.
