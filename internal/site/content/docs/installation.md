---
title: Installation
eyebrow: Documentation / installation
order: 10
lede: Install the Perk Workbench CLI with npm, Homebrew, a release binary, or from source.
keywords: [install, installation, npm, homebrew, binary, release, source, terminal, database]
---

## Choose an installation method

| Method | Platforms | Best for |
| --- | --- | --- |
| [npm](#npm) | macOS, Linux, Windows | The quickest path with Node.js |
| [Homebrew](#homebrew) | Apple Silicon macOS, Linux | A package-manager installation |
| [Install script](#install-script) | Linux AMD64/ARM64, Apple Silicon macOS, Windows Git Bash | A local binary in `~/.local/bin` |
| [GitHub Releases](#github-releases) | Linux AMD64/ARM64, Apple Silicon macOS, Windows AMD64 | Manual binary installation |
| [Build from source](#build-from-source) | Platforms supported by Go | Development or local builds |

After installation, verify the executable:

```sh
perk-workbench --version
```

## npm

The launcher downloads the matching native package automatically. No global
install is required:

```sh
npx perk-workbench path/to/database.db
```

To make the command available everywhere, install it globally:

```sh
npm install -g perk-workbench
perk-workbench path/to/database.db
```

The npm packages support macOS ARM64, Linux AMD64, Linux ARM64, and Windows
AMD64. Node.js and npm are required.

## Homebrew

Install the prebuilt CLI from the `l3aro/tap` third-party tap:

```sh
brew install l3aro/tap/perk-workbench
perk-workbench path/to/database.db
```

The formula supports Apple Silicon macOS, Linux AMD64, and Linux ARM64.
`perk-workbench` is not currently in the official `homebrew/core` catalog.

## Install script

The installer downloads the matching GitHub Release archive, verifies its
SHA-256 checksum, and installs `perk-workbench` into `~/.local/bin`:

```sh
curl -LSsf https://raw.githubusercontent.com/l3aro/perk-workbench/main/install.sh | bash
```

Install a specific release:

```sh
curl -LSsf https://raw.githubusercontent.com/l3aro/perk-workbench/main/install.sh | bash -s -- 1.0.0
```

The script supports Linux AMD64/ARM64, Apple Silicon macOS, and Windows AMD64
through Git Bash. It requires `curl`, `tar`, and either `sha256sum` or
`shasum`; Windows Git Bash additionally requires `unzip`. Add `~/.local/bin`
to `PATH` if it is not already there.

## GitHub Releases

Download a versioned archive and checksum from the
[GitHub Releases page](https://github.com/l3aro/perk-workbench/releases).
Available release targets are:

| Target | Archive |
| --- | --- |
| macOS ARM64 | `perk-workbench-darwin-arm64.tar.gz` |
| Linux AMD64 | `perk-workbench-linux-amd64.tar.gz` |
| Linux ARM64 | `perk-workbench-linux-arm64.tar.gz` |
| Windows AMD64 | `perk-workbench-windows-amd64.zip` |

For example, install the Linux AMD64 archive into `~/.local/bin`:

```sh
version=1.0.0
archive=perk-workbench-linux-amd64.tar.gz
mkdir -p ~/.local/bin
curl -fLO "https://github.com/l3aro/perk-workbench/releases/download/v${version}/${archive}"
curl -fLO "https://github.com/l3aro/perk-workbench/releases/download/v${version}/${archive}.sha256"
sha256sum -c "${archive}.sha256"
tar -xzf "$archive" -C ~/.local/bin
chmod 755 ~/.local/bin/perk-workbench
```

Use the matching archive for your operating system and architecture. The
Windows archive contains `perk-workbench.exe`.

## Build from source

Install directly with the Go toolchain:

```sh
go install github.com/l3aro/perk-workbench/cmd/perk-workbench@v1.0.0
```

The Go binary is installed into Go's configured binary directory, usually
`~/go/bin`; add that directory to `PATH` if needed. A `go install` build
reports `perk-workbench devel` because release linker metadata is not injected.

If you have the repository checked out, run the current source without
installing it:

```sh
go run ./cmd/perk-workbench path/to/database.db
```

Go 1.27 is required.

## Try the bundled demo databases

The repository ships four demo databases. The SQLite one runs anywhere; the
others start a self-hosted container service:

```sh
make sqlite      # Chinook (SQLite)
make mysql       # Office (MySQL via Docker)
make postgres    # Employees (PostgreSQL via Docker)
make mongo       # Restaurants (MongoDB via Docker)
```

Launch the SQLite demo directly with:

```sh
npx perk-workbench demo/chinook-sqlite.db
```

## Your first query

1. **Launch** — start the workbench with a database target. See [Connections](/docs/connections) for file paths, remote targets, and environment-variable configuration.
2. **Select a schema object** — find a table or collection in the schema pane (`1`).
3. **Write a statement** — type SQL or a mongosh-style statement in the workspace editor (`2`).
4. **Execute** — run it with <kbd>F5</kbd>, <kbd>Ctrl</kbd>+<kbd>Enter</kbd>, or <kbd>Ctrl</kbd>+<kbd>S</kbd> and review the results.

Execution is asynchronous and cancelable with <kbd>Escape</kbd>. If a query
fails, the previous result table stays visible.

## CLI options

| Option | Effect |
| --- | --- |
| `--select` | Choose a saved connection interactively; cannot be combined with a database target. |
| `--pin` | Lock the session: every quit affordance (keys, header button, palette entry) is disabled. |
| `--version`, `-v` | Print the build version (`perk-workbench <version>`, or `perk-workbench devel` for an uninjected build). |
| `-h`, `--help` | Show usage and the full option list. |

## The live demo website

The `/demo` page on this site streams the real TUI in a **read-only, pinned**
session against the Chinook SQLite demo: queries run, but writes are rejected
and the session cannot be quit. It is safe for exploration. For write-capable
actions — editing rows, inserting documents, running mutations — install the
CLI and open a local database instead.