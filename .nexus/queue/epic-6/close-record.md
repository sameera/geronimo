---
title: "Close Record: Open email in Gmail"
epic: "#6"
feature: "Inbox Triage"
date: 2026-11-12
nexus_version: 0.133.0
analyze: ran 2026-11-11 @ 84a1cc71d85e6645051a23ae5563c35887c77ed3
range:
  - repo: sameera/geronimo
    pr: 69
    base: 5c7991438abce6fca393696ff8fcd46f7b9651fd
    head: d47046556003f2b7a2aa3a1f0bbc374f966c4077
story_ranges:
  - story: "#43"
    ranges:
      - { repo: sameera/geronimo, pr: 69, base: 5c7991438abce6fca393696ff8fcd46f7b9651fd, head: d47046556003f2b7a2aa3a1f0bbc374f966c4077 }
  - story: "#44"
    ranges:
      - { repo: sameera/geronimo, pr: 69, base: 5c7991438abce6fca393696ff8fcd46f7b9651fd, head: d47046556003f2b7a2aa3a1f0bbc374f966c4077 }
landed_check:
  - story: "#43"
    result: unchanged
    prs:
      - { repo: sameera/geronimo, pr: 69, result: unchanged }
  - story: "#44"
    result: unchanged
    prs:
      - { repo: sameera/geronimo, pr: 69, result: unchanged }
---

# Close Record: Open email in Gmail

## Key Decisions

- **Each row is a plain link to Gmail in a new tab; an unread row's wrapper carries hx-post=/emails/read on click, so open and mark-read run side by side** (a decision stub the verdict on sameera/geronimo#69 confirmed). **Why:** The email opens even when the read mark fails, and the answer swaps the row or an alert into the page the user returns to **Refuted alternative:** A Geronimo redirect route that marks read, then 303s to Gmail
- **Gmail web address picks the account by mailbox address: https://mail.google.com/mail/?authuser=<mailbox email>#all/<message id>** (a decision stub the verdict on sameera/geronimo#69 confirmed). **Why:** authuser by address selects the right signed-in Google account; a /u/<n> index depends on browser sign-in order **Refuted alternative:** /mail/u/<index>/ addresses
- **Store marks the email read only after Gmail accepts** (a decision stub the verdict on sameera/geronimo#69 confirmed). **Why:** The list never shows read what Gmail still holds unread **Refuted alternative:** Optimistic local mark with rollback
- **Refresh on return reloads #email-view in place via htmx.ajax on visibilitychange/focus, with through=<last row's cursor>, after any pending read mark** (a decision stub the verdict on sameera/geronimo#69 confirmed). **Why:** The same rows come back, so page height and scroll offset hold, and the sort travels in the refresh address **Refuted alternative:** A full page reload, which shows only the first 50 rows and loses the scroll offset

## Deviation Rationale

none

## Waived Stories

none

## Deferred Scope

none
