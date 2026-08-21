---
title: Connections
eyebrow: Documentation / database targets
lede: SQLite, MySQL, PostgreSQL, and MongoDB share one terminal-native workflow. Use a file path, a real remote target, or Laravel-compatible environment variables.
keywords: [SQLite, MySQL, PostgreSQL, MongoDB, drivers, connection]
---

## Four backends

SQLite opens a local database file. MySQL, PostgreSQL, and MongoDB accept real targets:

```sh
perk-workbench path/to/database.db
perk-workbench 'mysql:user:pass@tcp(host:3306)/db'
perk-workbench 'postgres://user:pass@host:5432/db'
perk-workbench 'mongodb://user:pass@host:27017/db'
```

MongoDB accepts mongosh-style `find`, `countDocuments`, `aggregate`, `distinct`, writes, and index DDL. Collections appear in the schema pane, and the structure tab shows sampled fields.

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

`DB_CONNECTION` accepts `sqlite`, `mysql`, or `pgsql`. SQLite requires `DB_DATABASE`; remote ports default to 3306 for MySQL and 5432 for PostgreSQL. MySQL and PostgreSQL require `DB_USERNAME`; `DB_PASSWORD` defaults to empty.

Precedence: a command-line database argument overrides real environment variables, which override a `.env` file in the working directory. The `.env` file is a fallback.

## Credential storage

Saved profiles live in `~/.config/perk-workbench/connections.json`. Passwords and credential-bearing targets are encrypted at rest with AES-256-GCM using `secret.key`; the directory and files are locked to 0700/0600. Non-credential targets remain readable. An undecryptable value is never shown as plaintext or silently rewritten.
