---
title: Mailbox Sync
aliases: [first sync, metadata sync, inbox sync, sync worker]
touches: [sync-progress-strip, unread-baseline]
last_updated_by: "#4"
status: active
verification: verified
---

# Mailbox Sync

After a Gmail account connects, Geronimo copies the metadata of its inbox mail into its own database with no further action from the user. The sync never reads message bodies and never changes the user's mailbox. Each connected Gmail account syncs on its own, so one failing account does not stop the others.

## How It Works

Connecting creates one durable sync record for the Gmail account, before the user returns to home. A background worker in the same service process runs the sync. It reads the inbox totals, then lists the inbox newest first, one page at a time. It saves each page's mail IDs and the listing position in one transaction. It then reads the metadata of each pending email, up to 5 reads at a time, behind one pacing gate near 50 reads per second. Each message record holds headers, labels, dates and IDs, and no body or snippet. Home stays open throughout.

A Gmail or token call is retried with backoff. When one call uses up its attempts, the sync stops with a reason: rate limited, access removed, token unreadable or other. A stop keeps all stored mail. Retry resumes from the saved position and skips mail already stored. When Gmail rejects the saved position, the worker lists again from the top. After a restart, syncs that were starting or running resume. Stopped syncs wait for the user.

## Key Invariants

1. A sync record, its pending list and its message set belong to one connected Gmail account, never to the Geronimo login.
2. A failure on one Gmail account leaves every other account's sync running.
3. A stopped sync keeps every stored message record, and a resume never stores an email twice.
4. Every Gmail read asks for metadata only, through the Gmail access point, and the sync sends no write request.
5. A sync never runs twice at once for the same Gmail account.
6. Both HTTP 429 and 403 map to the rate-limited stop reason, because Gmail's quota errors arrive as 403 and the access point reads no body to tell them apart.
7. Removing the account removes its message records.

## Integration Points

- [sync-progress-strip](sync-progress-strip.md) — home renders the strip from the sync record's state and counts.
- [unread-baseline](unread-baseline.md) — finishing the first sync stores the baseline in the same transaction.

## Decision Log

### 2026-11-10 — #4 — Resumable in-process sync, metadata only, one record per Gmail account
- **Why:** There is one server instance and one operator, so a durable sync record in the existing database drives a background worker in the service, and makes the sync resumable after a stop, a retry or a restart. Listing newest first and interleaving it with metadata reads makes the first stored mail the most useful and leaves home open from the first second. Gmail charges the same quota for batched and single reads, so single reads are used. They stay inside the access point's allowlist, which keeps bodies unreadable. Snippets are body excerpts, so none are stored. The allowlist grows by one request, a read of the inbox label for its counts, because the message-list size estimate is approximate. Rate limits back off before any stop, because most clear in seconds. Keying by message ID makes a resume safe. Metadata is plain text so later sender views can query it. Records are keyed per connected Gmail account, because one login may connect several and cursor and progress belong to one. Record #63 should define "account" this way. A rejected listing position relists from the top, and an undecryptable token has its own stop reason with Retry sync, since Reconnect is reserved for removed access. Reads run 5 at a time because sequential reads were bound by round-trip latency, not quota.
- **Refuted alternatives:** A job-queue library or separate worker adds deployment cost with no scale-out need. Gmail's batch endpoint saves no quota and its multipart requests cannot be checked by the allowlist. Taking the total from the list estimate shows a wrong total. Restarting from scratch on retry discards quota-bound work. Encrypting sender and subject fields would force decryption in the app for every query. Stopping the sync on a rejected position would lose a recoverable sync.
