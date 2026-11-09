
## 2026-11-09 — Sync keyed per connected Gmail mailbox
- **Choice:** one sync record, pending list and message set per `mailboxes` row, not per Geronimo account.
- **Why:** cursor, baseline and progress are per Gmail account, and one Geronimo account may connect several.
- **Refuted alternative:** one record per Geronimo account. It would mix two mailboxes' listing cursors.

## 2026-11-09 — Failure bound applies per call
- **Choice:** each Gmail or token call gets MaxFailures attempts with backoff. A stop follows when one call exhausts them.
- **Why:** the saved cursor and stored IDs let a retry resume, so no run-wide counter is needed.
- **Refuted alternative:** a counter of consecutive failures across the whole run.

## 2026-11-09 — Gmail 403 counts as rate limit
- **Choice:** HTTP 429 and 403 both map to the rate-limited stop reason.
- **Why:** Gmail's quota errors arrive as 403, and the access point reads no body to tell them apart.
- **Refuted alternative:** 429 only. Quota 403s would then stop as "other".
