---
title: Connections
eyebrow: Documentation / database targets
order: 20
lede: SQLite, MySQL, PostgreSQL, and MongoDB share one terminal-native workflow. Use a file path, a real remote target, or Laravel-compatible environment variables.
keywords: [SQLite, MySQL, PostgreSQL, MongoDB, drivers, connection]
---

## Four backends

SQLite opens a local database file. MySQL, PostgreSQL, and MongoDB run as self-hosted plugin child processes and accept real targets:

| Backend | Target format | Example |
| --- | --- | --- |
| SQLite | Local file path | `perk-workbench path/to/database.db` |
| MySQL | `mysql:user:pass@tcp(host:port)/db` | `perk-workbench 'mysql:user:pass@tcp(host:3306)/db'` |
| PostgreSQL | `postgres://` URL | `perk-workbench 'postgres://user:pass@host:5432/db'` |
| MongoDB | `mongodb://` URL | `perk-workbench 'mongodb://user:pass@host:27017/db'` |

MongoDB accepts mongosh-style statements: `find`, `countDocuments`, `aggregate`, `distinct`, writes, and index DDL. Collections appear in the schema pane, and the structure tab shows sampled fields.

## Laravel-compatible configuration

Set `DB_*` variables, then launch without an argument:

```sh
export DB_CONNECTION=mysql
export DB_HOST=127.0.0.1
export DB_PORT=3306
export DB_DATABASE=office
export DB_USERNAME=root
export DB_PASSWORD=secret

perk-workbench
```

| Variable | Required | Default |
| --- | --- | --- |
| `DB_CONNECTION` | yes | — (`sqlite`, `mysql`, or `pgsql`) |
| `DB_HOST` | `sqlite`: no · `mysql`/`pgsql`: yes | — |
| `DB_PORT` | no | `3306` (`mysql`) · `5432` (`pgsql`) |
| `DB_DATABASE` | `sqlite`: yes · `mysql`/`pgsql`: no | — |
| `DB_USERNAME` | `mysql`/`pgsql`: yes | — |
| `DB_PASSWORD` | no | empty |

Precedence is strict: a command-line database argument overrides real environment variables, which override a `.env` file in the working directory. The `.env` file is a fallback, never an override.

## Saved profiles

Profiles saved from the connection screen live in `~/.config/perk-workbench/connections.json`. Launch with `--select` to choose one interactively from the CLI; `--select` cannot be combined with a database target.

## Credential storage

Passwords and credential-bearing targets are encrypted at rest with AES-256-GCM using a key in `secret.key`, and the directory and files are locked to 0700/0600. Non-credential targets remain readable. An undecryptable value is never shown as plaintext or silently rewritten.

The key and the ciphertext live under the same user-owned directory, so encryption protects against accidental disclosure and copies of your config — backups, shared screenshots, misplaced files — **not** against an attacker who controls your user account, because that attacker can read both. For secrets you want kept out of stored config entirely, reference environment variables or files instead of literal values.