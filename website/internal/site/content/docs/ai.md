---
title: AI assistance
eyebrow: Documentation / optional assistance
lede: Ask for help understanding a schema or shaping a query while keeping the database workflow in the terminal.
keywords: [AI, queries, schema, assistance, terminal]
---

## Use the provider you choose

AI assistance is optional. Configure OpenAI, Anthropic, Gemini, or an OpenAI-compatible endpoint when you want natural-language help; the workbench remains usable without a provider.

Press <kbd>Ctrl</kbd>+<kbd>A</kbd> to open the AI assistance flow.

## Read first, write with confirmation

AI tools can inspect the available schema and use read-only tools to gather context. They do not silently mutate your database.

Any write must be shown for review and explicitly confirmed before execution. Inspect generated SQL or mongosh statements before confirming.
