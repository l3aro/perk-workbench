---
title: AI assistance
eyebrow: Documentation / optional assistance
order: 40
lede: Use configured agents to inspect context, shape queries, and verify generated SQL without giving up control of execution.
keywords: [AI, queries, schema, assistance, terminal, openai-compatible]
---

## AI is optional

AI assistance appears only when the configuration defines an agent with the id `assistant`. There is no built-in provider, so normal database work remains available without AI.

## Configure AI in the TUI

The command palette's **configure AI** action opens a setup menu inside the
TUI. Choose **Providers** to view, add, or edit any number of providers, or
choose **Agents** to view, add, or edit the routed agents. Each section follows
the same flow: list entries, open an entry form, then return to the list.

The wizard exposes three routing agent IDs:

- `assistant` is required before AI can be activated.
- `oracle` is optional and handles premium, long, and reasoning-oriented
  requests when configured.
- `spark` is optional and handles lite requests and chat titles when
  configured.

Save writes only the user-level configuration and activates the resulting
effective configuration immediately; no restart is required. The normal loader
re-reads the effective user/project configuration while retaining chat history.
Canceling or leaving an unsuccessful save does not replace the current
configuration. The wizard preserves unrelated entries in the user file,
including providers and agents it does not manage.

Workbench reads two JSON files. Entries are merged by id, with project entries
overriding user entries with the same id:

- **User:** `$XDG_CONFIG_HOME/perk-workbench/ai.json` (on Linux, commonly `~/.config/perk-workbench/ai.json`)
- **Project:** `.perk-workbench/ai.json`

The wizard loads and saves only the user file. It never writes a resolved
configuration to the project file, so project-specific overrides remain in
place and continue to win for matching IDs. API-key fields are masked while
editing. Prefer an `env:NAME` reference (for example, `env:OPENAI_API_KEY`) so
the key stays out of the JSON; references and literal keys are persisted in
the user file.

This minimal manual configuration keeps one provider and the required
`assistant` agent:

```json
{
  "providers": {
    "openai": {
      "name": "OpenAI",
      "api": "openai",
      "base_url": "https://api.openai.com/v1",
      "api_key": "sk-…",
      "models": ["gpt-4o"]
    }
  },
  "agents": {
    "assistant": {
      "name": "Assistant",
      "provider": "openai",
      "model": "gpt-4o"
    }
  }
}
```

### Configuration checklist

- `api` must be `openai`, `anthropic`, `gemini`, or `openai-compatible`.
- Every provider needs a nonblank `name`, `base_url`, `api_key`, and `models` list.
- Every agent must reference an existing provider and one of that provider's configured models.
- Any config value, including `api_key`, may use an `env:NAME` reference resolved from the environment.

The JSON schema is map-based: add as many provider entries as needed, keyed by
provider ID. Agent entries are also keyed by ID; the wizard manages the
canonical `assistant`, `oracle`, and `spark` entries while retaining other
entries already present in the user file.

| Entry | Fields |
| --- | --- |
| Provider (any number) | `name`, `api`, `base_url`, `api_key`, `models` |
| Agent (`assistant`, `oracle`, or `spark`) | `name`, `provider`, `model`, `system_prompt` |

**Practical guidance:** keep keys out of JSON with `env:NAME`, and use a
project file only when its overrides are intentional.

## Use the chat pane

Focus chat with <kbd>4</kbd> and toggle its visibility with <kbd>Ctrl</kbd>+<kbd>G</kbd>. Chat input is single-line and supports slash completion.

<figure class="mt-8">
  <div class="overflow-hidden rounded-xl border border-line bg-[#0b0e14] shadow-deep [[data-theme=light]_&]:border-[var(--color-line-strong)] [[data-theme=light]_&]:bg-panel">
    <img class="block h-auto w-full [[data-theme=light]_&]:hidden" data-tui-theme-shot="dark" src="/static/tui.png" width="1444" height="868" alt="Perk Workbench dark TUI workspace preview showing the general application shell">
    <img class="hidden h-auto w-full [[data-theme=light]_&]:block" data-tui-theme-shot="light" src="/static/tui-light.png" width="1444" height="868" alt="Perk Workbench light TUI workspace preview showing the general application shell">
  </div>
  <figcaption class="mt-3 text-sm text-muted">This is a general workspace view, not an AI-specific capture.</figcaption>
</figure>

### What chat can see

Each context snapshot can include the following:

| Context | Included |
| --- | --- |
| Connection | Connection id, database product and version |
| Schema | Schema objects |
| Editor | Current editor SQL |
| Failure | Last failed query and its error |
| Results | Tool-capable agents receive visible results only after `/share-results`; providers without tools receive visible results in request context |
| Snapshot size | Capped at 12,000 runes (an implementation limit) |

For tool-capable agents, use `/share-results` when the visible result set is relevant, then `/unshare-results` to remove the optional results tool. Providers without tool support receive visible results in their request context when rows are present.

### Tools and safety

| Connection or setting | Available tools | Write behavior |
| --- | --- | --- |
| Any connection | `sql_read` and connection information | Read context only |
| Results shared | Visible results are available in addition to the always-available tools | Sharing is controlled by `/share-results` and `/unshare-results` |
| Read-only connection | Read tools only | `sql_write` is not exposed |
| Writable connection | Read tools, plus `sql_write` for one write or DDL operation | A write normally requires explicit confirmation |
| `/yolo-on` | The same eligible tools | Skips write confirmation and visibly marks YOLO |

Tool-capable agents are OpenAI and OpenAI-compatible agents. Anthropic and Gemini agents use the context snapshot instead. Provider choice does not make every agent tool-capable.

**Practical guidance:** inspect generated SQL and its assumptions before confirming a write. Turning on YOLO changes the confirmation step; it does not make review unnecessary.

## Use an efficient loop

Keep requests bounded and make verification explicit:

> **Inspect → ask → verify → apply/run**

Reusable prompt formula:

> `Goal + relevant scope + constraints + requested verification`

Examples:

1. **Inspect:** “Inspect the available schema and identify the tables and columns relevant to customer retention. Do not write anything.”
2. **Ask:** “Using the current editor SQL and the last failed query, explain the failure and propose one corrected read-only query.”
3. **Verify:** “Review this generated query for joins, filters, and write operations. State assumptions and what result would confirm it is correct.”
4. **Apply/run:** “Apply the reviewed SQL to the editor; I will inspect it before deciding whether to run it.”

`Ctrl`+`A` applies the latest generated SQL to the editor. It does not open or toggle AI, and does not execute SQL. Execution remains a separate decision, and normal writes require explicit confirmation.

## Model routing

When a routed agent is configured, matching requests use it; otherwise the
request falls back to `assistant`. Routing follows these rules:

| Request pattern | Routed agent |
| --- | --- |
| `/premium`, `@oracle`, the configured oracle name, a prompt longer than 350 runes, or a prompt containing `migration`, `multi-step`, `reason`, or `analyze` | `oracle` when configured; otherwise `assistant` |
| `/lite`, `@spark`, or the configured spark name | `spark` when configured; otherwise `assistant` |
| Anything else | `assistant` |
| Chat titles | `spark` when configured; otherwise `assistant` |

Routing selects an agent; it does not bypass connection permissions or write confirmation. Keep potentially destructive work in the inspect-and-verify loop.

## Keyboard and commands

| Action | Shortcut or command | Effect |
| --- | --- | --- |
| Configure AI | command palette | Opens the provider and agent setup menu |
| Focus chat | <kbd>4</kbd> | Moves focus to the AI chat pane |
| Toggle chat visibility | <kbd>Ctrl</kbd>+<kbd>G</kbd> | Shows or hides the pane |
| Apply generated SQL | <kbd>Ctrl</kbd>+<kbd>A</kbd> | Places the latest generated SQL in the editor; does not execute it |
| Start a new chat | `/new` | Starts a new chat |
| Open chat history | `/history` | Opens chat history |
| Share visible results | `/share-results` | Enables visible result sharing |
| Stop sharing results | `/unshare-results` | Disables visible result sharing |
| Toggle confirmation bypass | `/yolo-on`, `/yolo-off` | Enables or disables YOLO mode |