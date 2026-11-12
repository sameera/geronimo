## 2026-11-11 — Tap opens Gmail directly; mark-read rides the same click
- **Choice:** Each row is a plain link to Gmail (new tab); an unread row's wrapper carries hx-post="/emails/read" on click, so the open and the mark-read run side by side.
- **Why:** The email opens even when the read mark fails, and the answer swaps the row or an alert notice into the page the user returns to.
- **Refuted alternative:** A Geronimo redirect route that marks read, then 303s to Gmail (a failure could only be shown in the Gmail tab).

## 2026-11-11 — Gmail web address picks the account by mailbox address
- **Choice:** `https://mail.google.com/mail/?authuser=<mailbox email>#all/<message id>`, built in internal/gmail.
- **Why:** authuser by address selects the right signed-in Google account; a /u/<n> index depends on browser sign-in order.
- **Refuted alternative:** `/mail/u/<index>/` addresses.

## 2026-11-11 — Store marks read only after Gmail accepts
- **Choice:** MarkRead removes only UNREAD through the access point's single send; the stored record drops UNREAD and its unread flag only on success.
- **Why:** The list never shows read what Gmail still holds unread.
- **Refuted alternative:** Optimistic local mark with rollback.

## 2026-11-11 — Refresh on return re-reads through the last shown row
- **Choice:** After a tap on an email link, the page script reloads #email-view in place (htmx.ajax, default swap) on visibilitychange/focus, with `through=<last row's cursor>`; it waits for any pending read mark first. The header now shows the sender's unread count.
- **Why:** The same rows come back, so the page height and scroll offset hold, and the sort travels in the refresh address.
- **Refuted alternative:** A full page reload, which shows only the first 50 rows and loses the scroll offset past them; an hx-get on the view, which would become the page's first hx-get.
