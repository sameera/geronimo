---
title: Unread Baseline
aliases: [baseline unread count, baseline]
touches: [mailbox-sync]
last_updated_by: "#4"
status: active
verification: verified
---

# Unread Baseline

Geronimo records each Gmail account's inbox unread count once, when its first sync finishes. Later epics measure unread reduction against this number.

## How It Works

When the first sync has listed the whole inbox and no pending mail remains, the worker reads the inbox counts from Gmail again. In one transaction it marks the sync done and stores the unread count with the time, only if the account has no baseline yet. The count is the number of unread messages Gmail reports for the inbox at that moment. Local message records are not counted. Gmail counts messages, while its web page shows unread conversations, so the number can differ from what users see in Gmail.

## Key Invariants

1. An account has no baseline before its first sync finishes, and exactly one after.
2. A stop and a retry never create a baseline early or a second one.
3. Later syncs, reconnects and visits never change a stored baseline.
4. The baseline is Gmail's inbox unread message count, not a count of stored records.
5. The baseline belongs to one Gmail account and is removed with it.

## Integration Points

- [mailbox-sync](mailbox-sync.md) — finishing the first sync stores the baseline in the same transaction.

## Decision Log

### 2026-11-10 — #4 — Gmail's inbox unread count, written once with the done state
- **Why:** Inbox unread is the count that triage reduces, since archiving an unread email lowers it and the all-mail count would not. The story fixes the count Gmail reports at that moment. One transaction means no baseline exists before the finish and one exists after it, even across stops and retries.
- **Refuted alternatives:** Counting unread among stored records is cheaper and matches Geronimo's data, but mail arriving during the sync makes it drift from Gmail's count, which the story fixes.
