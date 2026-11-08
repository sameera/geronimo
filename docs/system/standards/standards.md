---
standard: Project Standards
category: architecture
applies_to: ["go", "htmx"]
description: Project-wide decisions and prohibitions for Geronimo
---

# Project Standards

> A ledger of decisions an agent cannot recover from the code. There is no application code yet, so there are no exemplar paths yet. Add a path once the first implementation lands.

## Decisions

### Server-rendered UI

**Decision**: The Go backend renders HTML. HTMX swaps fragments in place.

**Rationale**: This is the chosen stack (`docs/system/stack.md`). It keeps UI logic on the server.

### Formatting

**Decision**: Go code is formatted with `gofmt` (tabs). All other files use 4-space indentation, except YAML (2 spaces). See `.editorconfig`.

**Rationale**: `gofmt` is non-negotiable in Go. The 4-space rule covers everything else.

## Prohibitions

- **Never** add a client-side SPA framework (React, Vue, Svelte, etc.). Use server-rendered templates with HTMX attributes instead.
- **Never** add a dependency whose license is incompatible with GPL-3.0 (for example SSPL, BSL, proprietary, or "non-commercial" licenses). Check the license before adding a module.
- **Never** add an LLM or AI service for categorizing or summarizing (product anti-goal, `docs/product/context.md`). Use rules and email metadata instead.
- **Never** commit secrets such as OAuth client secrets or refresh tokens. Use environment variables, and keep `.env` files out of git (`.gitignore`).

## Checklist

- [ ] No SPA framework added
- [ ] New dependencies are GPL-3.0 compatible
- [ ] No secrets in the diff
