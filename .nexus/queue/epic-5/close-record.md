---
title: "Close Record: Sender view and drill-down"
epic: "#5"
feature: "Inbox Triage"
date: 2026-11-11
nexus_version: 0.133.0
analyze: ran 2026-11-11 @ a68378f6fa71bbfd30866c56a1997ad19f20d4ba
record: "#67"
record_hash: 11a75de03512ade29f2c431d20f087954053c6a614737838d7d46cec8d18fcda
range:
  - repo: sameera/geronimo
    pr: 68
    base: 01bd8334540d0bc8bdea8db6c51ec9af4373610b
    head: 5c7991438abce6fca393696ff8fcd46f7b9651fd
story_ranges:
  - story: "#40"
    ranges:
      - { repo: sameera/geronimo, pr: 68, base: 01bd8334540d0bc8bdea8db6c51ec9af4373610b, head: 5c7991438abce6fca393696ff8fcd46f7b9651fd }
  - story: "#41"
    ranges:
      - { repo: sameera/geronimo, pr: 68, base: 01bd8334540d0bc8bdea8db6c51ec9af4373610b, head: 5c7991438abce6fca393696ff8fcd46f7b9651fd }
  - story: "#42"
    ranges:
      - { repo: sameera/geronimo, pr: 68, base: 01bd8334540d0bc8bdea8db6c51ec9af4373610b, head: 5c7991438abce6fca393696ff8fcd46f7b9651fd }
landed_check:
  - story: "#40"
    result: unchanged
    prs:
      - { repo: sameera/geronimo, pr: 68, result: unchanged }
  - story: "#41"
    result: unchanged
    prs:
      - { repo: sameera/geronimo, pr: 68, result: unchanged }
  - story: "#42"
    result: unchanged
    prs:
      - { repo: sameera/geronimo, pr: 68, result: unchanged }
---

# Close Record: Sender view and drill-down

## Key Decisions

- **D1 — Group by a normalised sender key derived at write time, with a backfill (#67).** Compute the derived fields when each email record is written, and fill them once for existing records at start-up. Group by sender key. The key is the lowercased whole address. An unparseable From header groups by its raw value. An empty From header goes into one "Unknown sender" group. Every email falls in exactly one group. **Why:** The epic says a sender is one email address, but the stored From header holds a display name and mixed case. Grouping needs one stable key, used the same way by the group row and the email list (success metric 2). Deriving it once keeps per-request cost flat and puts all parsing in one routine. The fallback groups mean nothing is hidden. **Refuted alternative:** Parse From headers and group in memory on each request. This needs no schema change. It lost on cost and drift: every page view would re-parse the whole mailbox (tens of thousands of headers), and the list and the row would each apply the same parsing in their own code, which is how counts drift apart.
- **D2 — Row content and layout (#67).** The group row shows the display name from the sender's newest email, or the address if there is none. The address appears as a secondary line. The total, the unread count and the newest date sit in a fixed right-hand area. Long text is truncated with an ellipsis. The whole row is one link at least 44px tall. Email rows show the subject, with "(no subject)" when empty, and the date. Read state is shown by weight and the left-edge mark from the design system, plus a text label for screen readers. Email rows are not links. **Why:** These meet the row content in #40 and #41 and the phone-width criterion, using the design-system tokens. **Refuted alternative:** Use the most frequent display name. Newest is cheaper to compute and reflects how the sender names itself now.
- **D3 — Use the parsed Date header as the email date (#67).** Use the parsed Date header for the newest-date column, the "newest" sort and the list order. An unparseable or missing date sorts last and shows as blank. **Why:** The header is already stored for every email, so no Gmail re-read is needed. **Refuted alternative:** Gmail's own received time. It cannot be forged and matches Gmail's order. It lost because the sync does not fetch it today: adopting it means changing the Gmail access point and the sync, then re-reading every stored email.
- **D4 — One account-scoped mail set and one sender filter for both levels; the views read only the store (#67).** The group rows and the email list both read the account mail set, filtered by the same sender key. Mail from all of the user's Gmail mailboxes is merged, including a mailbox whose access was removed. Rows carry no account label. Each page's counts come from one read. These pages make no Gmail call, no token check, and no write to stored mail. **Why:** Success metric 2 holds only if the row and the list count exactly the same set. Read-only is the epic's scope. Scoping by the signed-in account at the query is the security boundary. **Refuted alternative:** none
- **D5 — Sender view loads in chunks of 100 groups, with the shown count in the page address (#67).** Show more replaces the list with all rows up to the new count. It does not append. It updates the page address in place. **Why:** A hoarder's inbox can have thousands of senders, and rendering all of them makes the first page load on a phone large. Re-rendering up to N, instead of appending by offset, avoids duplicated or skipped groups when totals change during a sync. Keeping N in the address means a reload rebuilds the same height of page, so a saved scroll offset can be restored (D7). **Refuted alternative:** Render every group in one response. It is simplest and makes scroll restoration trivial. It lost on first-load size: roughly 1 MB or more of HTML for about 3,000 senders on mobile, with no compression in place.
- **D6 — Sort by total, unread or newest; each descending; kept in the page address (#67).** The three sort fields each run descending. Ties go by sender address. A sort change replaces the page address and adds no history entry. A sender view opened with no sort in its address uses total. The sort is not remembered across visits. **Why:** The fields come from #42's notes. A fixed tie-break makes chunks and restores deterministic. Replacing the address means back from the sender view leaves the view and does not step through sort changes. Not persisting the sort avoids storing per-user state that no story asks for. **Refuted alternative:** Remember the last sort per user on the server. That is one plausible reading of "first time". It lost because it adds stored state and a write on a read-only view for no stated need.
- **D7 — Email list as its own page; back plus a small page script restores sort and scroll (#67).** Drill-down is a full-page navigation to the email list. Back, from the browser or the in-app control, reloads the sender view from its address, so sort and shown count are kept. The page script saves the scroll offset per address and restores it after load. **Why:** Signed-in pages are no-store, and the HTMX history cache is deliberately off (session-gate invariant 4, sign-out safety), so back always reloads. Browser scroll restoration after a reload is unreliable across mobile browsers. An explicit save and restore of a number keeps mail data out of the browser. The script is plain inline JavaScript, not an SPA framework, so it conforms to the standards. **Refuted alternative:** Open the list in a panel over the sender view, with a pushed history state that a popstate handler closes. The sender view's DOM and scroll then stay untouched. It lost on fragility: the HTMX history handler is configured to refresh on a history miss, so the custom history handling and HTMX's would compete. It also makes the list address unusable on its own.
- **D8 — Email list paged by continuation point, 50 at a time, newest first (#67).** Load more reads the 50 emails after the last one shown, ordered by date, mailbox and message. It is appended to the list. **Why:** During a sync, new older mail is inserted, because the sync runs newest to oldest. Offset paging would skip or repeat rows when that happens. Reading after the last row shown does not. **Refuted alternative:** Render the whole list at once. It is simple, and rows are not tappable yet. It lost on large senders with thousands of emails, the same size cost as in D5.
- **D9 — Dates shown in the user's local time (#67).** The server emits a machine-readable time with a UTC date as fallback text. The page script rewrites it to the browser's local date. **Why:** The server does not know the user's time zone. UTC dates are off by a day near midnight for most users. **Refuted alternative:** Server-side formatting with a stored time-zone setting. This needs a settings flow no story covers.
- **D10 — Partial sync shows stored mail plus the existing progress strip (#67).** The sender view reads whatever is stored. It shows the existing sync progress strip while any of the account's mailboxes is syncing. With nothing stored yet, it shows a "still syncing" state. With no mailbox connected, it links to connect. **Why:** This is #40's acceptance criterion on a running sync. The strip tells the user why counts are still rising, and reuses epic #4's component. **Refuted alternative:** none
- **Sender view at GET /senders; unknown date stored as math.MinInt64, unknown sender key \x00unknown; backfill marks old rows by empty sender_key** (a decision stub the verdict on sameera/geronimo#68 confirmed). **Why:** Keeps home and its tests unchanged, and lets one index and plain DESC order serve grouping and paging **Refuted alternative:** Nullable date column with NULLS LAST ordering
- **Email list continuation cursor in the page address (after=date:mailbox:message)** (a decision stub the verdict on sameera/geronimo#68 confirmed). **Why:** Reads strictly after the last row shown (D8), with no server state **Refuted alternative:** Offset paging
- **Sort change resets shown count; in-app back uses history only when referrer matches** (a decision stub the verdict on sameera/geronimo#68 confirmed). **Why:** A new sort starts at the top; matching referrer avoids stepping back to an unrelated page **Refuted alternative:** Keep the shown count across sorts

## Deviation Rationale

none

## Waived Stories

none

## Deferred Scope

none
