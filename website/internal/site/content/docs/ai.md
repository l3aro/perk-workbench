---
title: AI assistance
eyebrow: Documentation / optional assistance
order: 40
lede: Ask for help understanding a schema or shaping a query while keeping the database workflow in the terminal.
keywords: [AI, queries, schema, assistance, terminal, openai-compatible]
---

## When AI is available

AI assistance is optional and activates only when your configuration defines an agent named `assistant`. Without it the AI chat pane never appears and normal database work is completely unaffected. There is no built-in provider: you bring your own.

Configuration is read from two JSON files, merged by id with the project file winning:

- User: `$XDG_CONFIG_HOME/perk-workbench/ai.json`
- Project: `.perk-workbench/ai.json` (repository or working directory)

A provider or agent in the project file overrides the same-named entry in the user file. A minimal configuration with one provider and one `assistant` agent:

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

`api` accepts `openai`, `anthropic`, `gemini`, and `openai-compatible` (an OpenAI-shaped endpoint of your own). Every provider needs a non-empty `name`, `base_url`, `api_key`, and `models` list; each agent must reference an existing provider and one of its configured models. Any value — including `api_key` — may instead be an `env:VARIABLE` reference resolved from the environment, which keeps secrets out of the config file.

## Use the chat pane

Focus the AI chat pane with <kbd>4</kbd> and toggle its visibility with <kbd>Ctrl</kbd>+<kbd>G</kbd>. The assistant answers with the current schema and your query in context: database product and version, schema objects, the current editor statement, and recent failures. You can also share the visible results with the assistant so it can reason about actual data.

<kbd>Ctrl</kbd>+<kbd>A</kbd> applies the assistant's latest generated SQL to the editor — it does not open or toggle AI.

## Read first, write with confirmation

The assistant uses read-only tools to gather context and never silently mutates your database. Any write must be shown for review and explicitly confirmed before execution; on a read-only connection only read tools are exposed. Inspect generated SQL or mongosh statements before confirming them.