## 2026-11-11 — Sender view route and unknown-value sentinels
- **Choice:** Sender view at GET /senders (home links to it); unknown date stored as math.MinInt64, unknown sender key "\x00unknown"; backfill marks old rows by empty sender_key.
- **Why:** Keeps home and its tests unchanged, and lets one index and plain DESC order serve grouping and paging.
- **Refuted alternative:** Nullable date column with NULLS LAST ordering.

## 2026-11-11 — Email list continuation cursor in the page address
- **Choice:** Load more carries `after=<date>:<mailbox>:<message>` in the address; the button swaps itself with the next rows.
- **Why:** Reads strictly after the last row shown (D8), with no server state.
- **Refuted alternative:** Offset paging.

## 2026-11-11 — Sort change resets shown count; in-app back uses history only when referrer matches
- **Choice:** Sort links reset the shown count to 100; the back link calls history.back() only when the referrer equals its address, else it follows the link.
- **Why:** A new sort starts at the top; matching referrer avoids stepping back to an unrelated page.
- **Refuted alternative:** Keep the shown count across sorts.
