# Geronimo

## Project Overview

Geronimo is an inbox app for Gmail and Outlook. It gives users views over their inbox by topic, sender, and urgency, each with drill-down, so they can skim and clear email faster. No LLM or AI features for now. Product context: [docs/product/context.md](docs/product/context.md).

## Architecture

- Go backend (our own service) syncs mail through the Gmail API and Microsoft Graph and builds the views.
- Server-rendered HTML with HTMX for the UI.
- No application code yet.

## Technical Patterns and Standards

- [docs/system/stack.md](docs/system/stack.md) — technology stack
- [docs/system/standards/standards.md](docs/system/standards/standards.md) — project decisions and prohibitions

## Code Style

- Go: `gofmt` (tabs). Other files: 4 spaces, YAML 2 spaces. See `.editorconfig`.
- Use double quotes for strings where the language allows a choice.

## Scratch Files

Put scratch work in `.tmp/` at the repo root (gitignored).

## License

GPL-3.0. New dependencies must be GPL-3.0 compatible.

## In-Flight Decision Stubs

When you make a non-obvious implementation choice — you picked between viable approaches —
append a stub to your per-user scratch inside the epic's queue entry, at the moment of
choosing, not later:

    .nexus/queue/epic-<epic-issue-number>/<your-username>/decisions-<branch>.md

- `<epic-issue-number>` — the GitHub issue number of the epic your story belongs to.
- `<your-username>` — your GitHub login (`gh api user --jq .login`; fall back to a slug of
  `git config user.name`).
- `<branch>` — current branch with `/` → `-`. Append-only; one file per branch.

    ## <date> — <short decision title>
    - **Choice:** <what was chosen>
    - **Why:** <one sentence>
    - **Refuted alternative:** <the viable option not taken, or "none">

**Resolving `<epic-issue-number>`** (do this silently — a stub in the wrong folder is worse
than none): find your story issue (the number in the branch name, or the issue the open PR
closes), `gh` its **parent epic issue**, and take that number; else **write nothing**.
Resolution is issue-only — never look for a queue entry, an `epic.md`, or a matching directory
name: the entry is born at close, at this same `epic-<epic-issue-number>` directory.

This scratch is committed — ordinary commits carry it through the PR. It is a pre-checkpoint
hint the lead-run stages (hld, analyze, close) mine and verify against the diff, never
load-bearing; the distiller deletes the whole entry post-merge, so never link these paths.
Working notes go beside it as `notes-<branch>.md`. Obvious choices get no stub.
