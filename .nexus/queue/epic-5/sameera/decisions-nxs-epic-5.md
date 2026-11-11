## 2026-11-11 — Sender view route and unknown-value sentinels
- **Choice:** Sender view at GET /senders (home links to it); unknown date stored as math.MinInt64, unknown sender key "\x00unknown"; backfill marks old rows by empty sender_key.
- **Why:** Keeps home and its tests unchanged, and lets one index and plain DESC order serve grouping and paging.
- **Refuted alternative:** Nullable date column with NULLS LAST ordering.
