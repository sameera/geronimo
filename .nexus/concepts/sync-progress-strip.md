---
title: Sync Progress Strip
aliases: [progress strip, sync notice, sync retry]
touches: [mailbox-sync]
last_updated_by: "#4"
status: active
verification: verified
---

# Sync Progress Strip

Home shows one line per connected Gmail account whose first sync is not done, such as "12,400 of 31,000 emails". The line updates on its own, shows when the sync is done, and then goes away. A stopped sync shows a plain message with one action instead.

## How It Works

The strip is a part of the home page that asks the server for itself about every 2 seconds while any sync is active. It stops asking when no sync is active. The first home page after connecting already shows the strip, because connecting creates the sync record first. Progress is the synced count over the inbox total read when the sync started. The count shown never goes above the total. A finished sync shows as done, taken from the record's done state and not from the two counts being equal. That line is marked shown in the same response that renders it, so it appears once even when no later poll arrives. A stopped sync shows its reason in plain words. The action is Retry sync, or Reconnect when access was removed.

## Key Invariants

1. The strip shows the first time home loads after a connect, unless that account's first sync was already done.
2. The synced count shown is never above the total shown.
3. Done comes from the sync's done state, never from equal counts.
4. A done line appears once, then no later page load shows a strip for that account.
5. Reconnect is offered only for removed access. An unreadable stored token offers Retry sync.
6. When the sync record cannot be created after connecting, home shows a notice whose retry link goes to the connect step.

## Integration Points

- [mailbox-sync](mailbox-sync.md) — the strip reads each Gmail account's sync record, and its retry action restarts the sync.

## Decision Log

### 2026-11-10 — #4 — Poll a home fragment every 2 seconds
- **Why:** Creating the sync record in the connect step puts the strip on the first page after connecting, whatever the worker's timing. Polling every 2 seconds fits the existing HTMX setup with no extension. Mail arrives in and leaves the inbox during a sync, so the counts rarely match at the end, and only the done state is reliable. Marking the done line shown in the response that renders it needs no extra state or poll. A failed record creation reuses the connect step as the retry, since that step creates the record.
- **Refuted alternatives:** Server-sent events push sooner but need an extension and long-lived connections through the public tunnel, for no visible gain at a 2-second refresh. A first-run screen that holds the user adds a page and delays entry. Acknowledging the done line on the next poll needs state and depends on a poll that may never arrive. A new retry route for missing records duplicates the connect step.
