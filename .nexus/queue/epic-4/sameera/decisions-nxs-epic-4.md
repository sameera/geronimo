
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

## 2026-11-09 — Done notice acknowledged when rendered
- **Choice:** a done sync's strip line is marked shown in the same response that renders it. The next poll then shows nothing.
- **Why:** the line appears once even if the poll never arrives, and no extra state or poll is needed.
- **Refuted alternative:** acknowledge on the next poll. That leaves the line showing after a missed poll and needs a second request.

## 2026-11-09 — Sync-start failure retries through the connect page
- **Choice:** when the sync record cannot be created after connect, home shows a notice whose retry link goes to /connect.
- **Why:** the connect step already creates the record, so a second connect reruns it with no new route or store call.
- **Refuted alternative:** a new retry route that creates missing sync records. It needs a store listing of mailbox IDs and a new handler.

## 2026-11-09 — Undecryptable token stops with retry, not reconnect
- **Choice:** a token_unreadable stop offers Retry sync, not Reconnect.
- **Why:** Reconnect would re-encrypt the token with the local key, which fixes the fault. Offering it again would repeat the "access removed" action the finding objects to.
- **Refuted alternative:** Reconnect as the action. It would also fix the stored token, but the record reserves Reconnect for removed access (G6, D5).
