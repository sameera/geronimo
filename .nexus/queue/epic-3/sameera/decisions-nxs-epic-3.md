## 2026-11-08 — Home shows pre-consent unconditionally in #33
- **Choice:** Home renders the pre-consent section for every signed-in user until #34 adds connected-mailbox state.
- **Why:** No mailbox storage exists before #34, so routing on connected state would need a table this story does not own.
- **Refuted alternative:** Add a stub mailbox count to the store in #33.

## 2026-11-08 — #34 stores a mailbox insert-only; replace-in-place waits for #36
- **Choice:** `SaveMailbox` inserts and returns `ErrMailboxExists` on a repeat connect; the callback shows the generic failure for it.
- **Why:** D5 (duplicates, reconnect replace) is delivered by #36, so #34 only builds the unique key.
- **Refuted alternative:** Build the upsert in #34.

## 2026-11-08 — Gmail access point is an allowlist on path and format
- **Choice:** `gmail.Client.Get` allows only profile, labels, history, message list and message get with `format=metadata|minimal`; a bypass test scans source for Gmail API names outside `internal/gmail`.
- **Why:** Gmail's default message format is full, so a denylist of "full/raw" would miss a missing format.
- **Refuted alternative:** Denylist of `full`, `raw` and attachment paths.

## 2026-11-08 — Connect callback is a public path that checks the session itself
- **Choice:** `/connect/google/callback` is public; the handler consumes the attempt, then redirects to `/signin` when no session exists.
- **Why:** The gate would redirect before the attempt is consumed, and G7 needs the attempt used up and a deliberate sign-in redirect.
- **Refuted alternative:** Gate the callback like other signed-in routes.

## 2026-11-08 — Order of #36 and #35
- **Choice:** Build #36 before #35.
- **Why:** Both depend only on #34, and #35's reconnect uses the replace-in-place path that #36 delivers (D5).
- **Refuted alternative:** Build #35 first and add its own replace path.

## 2026-11-08 — Retry and reconnect link to a new GET /connect page
- **Choice:** Retry and reconnect link to `GET /connect`, which always renders the pre-consent screen; home keeps showing the account list for connected users.
- **Why:** G24 needs the pre-consent screen reachable by a user who already has accounts, and home no longer shows it to them (G26).
- **Refuted alternative:** Link retry to `/` and show pre-consent there.

## 2026-11-08 — Status check claims the interval before calling Google
- **Choice:** `ClaimDueChecks` stamps `last_checked_at` in one transaction before the Google call, including when the call fails.
- **Why:** Two page loads never check one mailbox twice, and an outage costs at most one 3-second wait per interval.
- **Refuted alternative:** Stamp only on a definitive answer, which would repeat the wait on every open during an outage.
