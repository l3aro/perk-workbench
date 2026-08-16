# Perk protocol compatibility policy

Policy version: 1
Applies to: perk/v1 (protocol version 1)
Parties: the perk-workbench host and every external perk plugin
(perk-redis, Node SDK plugins, any other implementation)

This asset is the concise, versioned compatibility policy of the
perk/v1 protocol. The normative prose contract is `docs/plugins.md`;
the machine-readable contract is `schema.json` plus
`fixtures/manifest.json` and the fixture frames in `fixtures/`.

## Rules

1. **Unknown object members MUST be ignored.** Every v1 JSON object
   tolerates unknown members (`additionalProperties: true` throughout
   `schema.json`). An implementation MUST ignore unknown members — it
   MUST NOT reject a frame, change behavior, or fail a session because
   of them.

2. **Additive changes are compatible.** Adding optional members,
   method-independent metadata (for example the structured error
   `data` provenance), and new *optional* capabilities (an optional
   write interface, an optional query-language command) is additive: a
   receiver that ignores unknown members stays interoperable, so no
   protocol version bump is required.

3. **Breaking changes require perk/v2.** Changing a required field,
   changing the meaning of an existing field, changing error codes or
   error semantics, changing framing (frame size bound, encoding,
   newline/id rules), or changing the behavior of an existing method
   is breaking. A breaking change MUST move to a new `perk/v2` method
   namespace and a new protocol version; v1 frames and semantics are
   never altered in place.

4. **The host supports exactly protocol version 1 today.** A plugin
   whose initialize result advertises any other protocol version is
   rejected before registration. The host does not speak perk/v2.

5. **Prerelease and semantic-versioning expectations.**
   perk-workbench and perk/v1 plugins are pre-1.0 software. v1 is a
   contract between a specific host build and a specific plugin build
   — it is not a claim of global API stability beyond this policy.
   Host releases SHOULD inject their build version
   (`-ldflags "-X main.version=<version>"`; `perk-workbench --version`
   reports it, and an uninjected build reports `devel` honestly).
   Plugins SHOULD version their releases. The authoritative
   compatibility evidence is the `perk-workbench plugin test --json`
   evidence document, whose `contract_sha256` pins the exact schema,
   manifest, and fixture set a plugin was verified against.

## Evidence

`perk-workbench plugin test --json EXECUTABLE` emits a self-contained
release evidence document validated by `plugin-test-evidence.schema.json`
in this directory. Its stable fields — protocol version, host build
version, contract digest, executable digest and canonical path,
capabilities identity, case results — make a release reproducible:
the same executable and the same contract digest imply the same
verified behavior.
