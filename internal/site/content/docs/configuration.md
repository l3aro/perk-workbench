---
title: Configuration
eyebrow: Documentation / user preferences
order: 25
lede: Configure Perk Workbench defaults, plugins, keybindings, database discovery, and terminal appearance from one user-owned JSON file.
keywords: [configuration, config.json, preferences, themes, keybindings, plugins, environment]
---

Perk Workbench keeps its general application settings in `config.json`. You can
omit any setting that should use its built-in default; the file does not need to
contain every key.

## Config file location and loading

The primary application file is:

```text
os.UserConfigDir()/perk-workbench/config.json
```

On a typical Linux installation this is
`~/.config/perk-workbench/config.json`. The exact base directory is supplied by
your operating system. On first launch, a missing file is created with the
built-in defaults, including the four bundled database plugins. The generated
file has mode `0600`.

The file is ordinary JSON. Omitted values, and the zero values documented below,
resolve to the corresponding built-in defaults. Unknown JSON keys are accepted
and preserved by configuration save operations, so adding a key from a newer
release does not cause an older release to discard it. An existing empty file
is treated like an empty object and populated with defaults. Malformed JSON or
an invalid known value reports the config path and the problem instead of
silently guessing.

Older plugin and theme fields are migrated to the current descriptor and theme
fields when the file is loaded. Do not add those legacy fields to a new file.

## JSON option reference

All of these keys belong to the application `config.json` file:

| JSON key | Default | Accepted values and effect |
| --- | --- | --- |
| `browse_page_size` | `25` | Integer from `1` through `500`. Default number of rows shown per browse page. |
| `query_log_page_size` | `25` | Integer from `1` through `100`. Number of entries shown per query-log page. |
| `query_log_retention_days` | `30` | Non-negative integer. How long query-log entries are retained. Omitted or `0` means the default (`30`); use the environment variable described below with value `0` to disable query history. |
| `notification_retention_days` | `30` | Non-negative integer. How long notification entries are retained. Omitted or `0` means `30`. |
| `notification_timeout_seconds` | `10` | Non-negative integer up to `86400` (one day). How long a notification popup remains visible. Omitted or `0` means `10`. |
| `read_only` | `false` | Boolean. Opens connections read-only by default. The per-connection form can still opt a connection back to read-write. |
| `appearance` | `"dark"` | `"dark"` or `"light"`. The effective light/dark appearance. It is also the fallback when system appearance detection is unavailable and the value used when automatic theme following is turned off. |
| `auto_theme` | `true` | Boolean. When true, follow the terminal's system light/dark appearance at startup. Omitted means true. |
| `dark_theme` | `"ocean"` | One of `"ocean"`, `"nord"`, `"monokai"`, `"dracula"`, `"catppuccin"`, or `"solarized"`, used for dark appearance. |
| `light_theme` | `"light-ocean"` | One of `"light-ocean"`, `"light-nord"`, `"light-monokai"`, `"light-dracula"`, `"light-catppuccin"`, or `"light-solarized"`, used for light appearance. |
| `vim_mode` | `true` | Boolean. Enables modal Vim-style editing: normal mode navigates with `j`/`k`-style keys and insert mode (`i` or `Enter`) types. Set false for always-editable focused inputs. Omitted means true. |
| `nerd_font` | `true` | Boolean. Uses Nerd Font icons for schema tree markers. Set false for geometric symbols in terminals without a Nerd Font. Omitted means true. |
| `log_level` | `"info"` | One of `"debug"`, `"info"`, `"warn"`, or `"error"`. Minimum severity written to the event log and surfaced as notifications. |
| `table_open_target` | `"structure"` | One of `"structure"`, `"browse"`, `"sql"`, `"indexes"`, or `"foreign_keys"`. Workspace tab selected after opening a table or collection. |
| `plugins` | The four bundled built-ins | Array of plugin descriptors. A descriptor has exactly one `builtin` name or an external `path`; an external descriptor may also have a lowercase 64-character hexadecimal `sha256` pin. An explicit `[]` disables all plugin instances. |
| `keybinds` | Built-in keybindings | A flat or nested map of command IDs to string arrays. An omitted command keeps its default; an empty array disables that command. `null` is ignored. |

For example, this is a complete, valid configuration file that changes several
defaults while keeping all four bundled plugins enabled:

```json
{
  "browse_page_size": 50,
  "query_log_page_size": 40,
  "query_log_retention_days": 90,
  "notification_retention_days": 14,
  "notification_timeout_seconds": 15,
  "read_only": true,
  "appearance": "dark",
  "auto_theme": false,
  "dark_theme": "nord",
  "light_theme": "light-ocean",
  "vim_mode": false,
  "nerd_font": false,
  "log_level": "debug",
  "table_open_target": "browse",
  "plugins": [
    {"builtin": "sqlite"},
    {"builtin": "mysql"},
    {"builtin": "postgres"},
    {"builtin": "mongodb"}
  ],
  "keybinds": {
    "app": {"palette": ["ctrl+shift+p"]},
    "browse": {"next_page": ["ctrl+n"]}
  }
}
```

The `keybinds` example uses the nested form. The equivalent flat entries are
`"app.palette": ["ctrl+shift+p"]` and
`"browse.next_page": ["ctrl+n"]`.

## Plugins

The default plugin list is:

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

Remove a built-in descriptor to disable that backend, or set `plugins` to `[]`
to disable every plugin. External plugins use an executable path and may be
pinned with a lowercase SHA-256 digest:

```json
{
  "plugins": [
    {"builtin": "sqlite"},
    {
      "path": "/home/alice/.local/bin/perk-redis",
      "sha256": "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
    }
  ]
}
```

A descriptor must contain exactly one of `builtin` or `path`. Built-in names are
`sqlite`, `mysql`, `postgres`, and `mongodb`. A `sha256` value is only valid for
an external path and must be exactly 64 lowercase hexadecimal characters. The
workbench validates descriptors before starting child processes; malformed,
blank, duplicate, or otherwise invalid entries fail startup. See [Plugins](/docs/plugins)
for the plugin lifecycle and management commands.

An enabled external plugin is executable code running with your OS privileges.
Only add plugins you trust. A SHA-256 pin makes a changed external executable
refuse to start, but it does not make an untrusted executable safe to run in the
first place.

## Keybindings

Keybinding overrides replace a command's complete default key list. Commands not
mentioned in `keybinds` keep their defaults, while `[]` deliberately disables a
command. Both these forms are accepted:

```json
{
  "keybinds": {
    "app.palette": ["ctrl+shift+p"],
    "browse.next_page": ["ctrl+n"],
    "form.save": []
  }
}
```

```json
{
  "keybinds": {
    "app": {"palette": ["ctrl+shift+p"]},
    "browse": {"next_page": ["ctrl+n"]},
    "form": {"save": []}
  }
}
```

Values must be arrays of strings. Keystrokes can be rune keys or named keys
such as `enter`, `esc`, `tab`, `up`, `down`, and `f5`, with modifiers such as
`ctrl+`, `alt+`, or `shift+` (for example, `ctrl+shift+p`). Unknown command IDs,
invalid keys, and invalid map or array values fail startup with a keybinding
error. A `null` nested entry or whole `keybinds` value is ignored, leaving the
relevant defaults in place.

The command palette (<kbd>Ctrl</kbd>+<kbd>P</kbd>) shows actions available in
the current context; use it to discover the action you want before adding a
custom override. The [Workspace](/docs/workspace) page covers the most visible
built-in shortcuts. The examples above are representative overrides rather than
a complete list of command IDs, because many commands are context-sensitive.

## Environment variables and CLI options

### Query-log overrides

These variables override the matching JSON values when they are present in the
process environment:

| Environment variable | Effect |
| --- | --- |
| `PERK_WORKBENCH_QUERY_LOG_RETENTION_DAYS` | A non-negative integer overrides `query_log_retention_days`. Set it to `0` to keep no query history. |
| `PERK_WORKBENCH_QUERY_LOG_PAGE_SIZE` | An integer of at least `1` overrides `query_log_page_size`; values above `100` are capped at `100`. |

An invalid or negative value is ignored and the JSON value (or built-in default)
is used. The JSON file cannot express “no query history,” because JSON `0` means
“use the default”; use the retention environment variable with value `0` for
that case. These variables are read from the process environment; the `.env`
fallback described below is only for database discovery.

### Database discovery

If you launch without a positional target, the workbench can build a target
from Laravel-compatible variables:

| Variable | Required | Default or supported values |
| --- | --- | --- |
| `DB_CONNECTION` | yes | `sqlite`, `mysql`, or `pgsql` |
| `DB_DATABASE` | SQLite: yes; MySQL/PostgreSQL: no | SQLite file path or optional remote database name |
| `DB_HOST` | MySQL/PostgreSQL: yes; SQLite: no | Remote host |
| `DB_USERNAME` | MySQL/PostgreSQL: yes | Remote username |
| `DB_PASSWORD` | no | Optional password; empty when omitted |
| `DB_PORT` | no | `3306` for MySQL, `5432` for PostgreSQL |

For SQLite, `DB_DATABASE` is the local database path. MySQL and PostgreSQL
require both `DB_HOST` and `DB_USERNAME`; an explicitly supplied port must be
between `1` and `65535`.

Target precedence is strict:

1. One positional database target on the command line.
2. A complete target from the real process environment.
3. A complete target from `.env` in the current working directory.

A `.env` file is a fallback, not an override for exported variables. It may use
`KEY=VALUE`, an optional `export` prefix, comments, blank lines, and quoted
values. `--select` skips environment discovery and opens the saved-connection
picker; it cannot be combined with a database target. See [Connections](/docs/connections)
for target formats and saved profiles.

### Command-line flags

| Flag | Effect |
| --- | --- |
| `--read-only`, `-r` | Start the session read-only. |
| `--select` | Choose a saved connection interactively; mutually exclusive with a positional target. |
| `--pin` | Lock the session by disabling in-app quit affordances. The process still exits when its context is cancelled. |
| `--version`, `-v` | Print `perk-workbench <version>` (or `perk-workbench devel` for an uninjected build). |
| `-h`, `--help` | Show usage and the complete option list. |

There is at most one optional positional target. For example:

```sh
perk-workbench path/to/database.db
perk-workbench --read-only path/to/database.db
perk-workbench --select
```

The `read_only` JSON setting is the default for connections and can be opted out
of in the connection form; `--read-only` is a command-line request for the
session and is independent of database target discovery.

## AI configuration is separate

AI providers and agents do not belong in the application `config.json`. They are
loaded from the user file `$XDG_CONFIG_HOME/perk-workbench/ai.json` and the
project file `.perk-workbench/ai.json`; entries with the same ID in the project
file override user entries. Provider keys can use `env:VARIABLE` references so
secrets stay out of JSON. See [AI assistance](/docs/ai) for the provider and
agent schema.

## Validation and troubleshooting

Configuration is validated before the workbench starts:

- JSON syntax errors identify the config file that could not be parsed.
- Page sizes must be within their documented ranges.
- Retention and timeout values cannot be negative; notification timeout cannot
  exceed `86400` seconds.
- Appearance, theme, log-level, and table-open values must be one of the listed
  names.
- Plugin descriptors and keybinding maps must have the documented shapes.
- Keybinding command IDs and keystrokes must be known and valid.

When startup reports a validation error, correct or remove the named key and
start again. For a setting that appears ineffective, check whether a query-log
environment variable is overriding JSON, and check that database discovery is
not selecting a higher-precedence target. Keep a backup before hand-editing a
working file; a missing file can be regenerated with defaults on the next launch.

## Filesystem and security notes

The application config is user configuration, not a secrets vault. Keep
`config.json` readable only by its owner (`0600`) and do not commit it when it
contains local paths or sensitive values. Prefer environment references over
literal database passwords and AI API keys.

Saved connection profiles are stored separately in `connections.json` under the
same user configuration directory; their credential-bearing values are
encrypted using `secret.key`. `data.db` and `event.log` are internal application
state, not additional configuration files.
Protect the entire user configuration directory and its backups, because an
attacker who controls your user account can read both encrypted data and its key.
