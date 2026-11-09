---
title: "Close Record: Connect Gmail with consent"
epic: "#3"
feature: "Inbox Triage"
date: 2026-11-09
nexus_version: 0.129.0
analyze: ran 2026-11-08 @ a388abdd206f95501a2b1a97e4c3c14d5695debd
record: "#58"
record_hash: c437409f24ceca36e2590b8b4d82ac21bb628682374ca08b0e669aab0d4e7a97
range:
  - repo: sameera/geronimo
    pr: 61
    base: e01f46030c7b0e24af75282195371fbb5fee20b4
    head: 53133aaacde434e2ff7c25183d668ff92e1febae
story_ranges:
  - story: "#33"
    ranges:
      - { repo: sameera/geronimo, pr: 61, base: e01f46030c7b0e24af75282195371fbb5fee20b4, head: 53133aaacde434e2ff7c25183d668ff92e1febae }
  - story: "#34"
    ranges:
      - { repo: sameera/geronimo, pr: 61, base: e01f46030c7b0e24af75282195371fbb5fee20b4, head: 53133aaacde434e2ff7c25183d668ff92e1febae }
  - story: "#35"
    ranges:
      - { repo: sameera/geronimo, pr: 61, base: e01f46030c7b0e24af75282195371fbb5fee20b4, head: 53133aaacde434e2ff7c25183d668ff92e1febae }
  - story: "#36"
    ranges:
      - { repo: sameera/geronimo, pr: 61, base: e01f46030c7b0e24af75282195371fbb5fee20b4, head: 53133aaacde434e2ff7c25183d668ff92e1febae }
landed_check:
  - story: "#33"
    result: unchanged
    prs:
      - { repo: sameera/geronimo, pr: 61, result: unchanged }
  - story: "#34"
    result: unchanged
    prs:
      - { repo: sameera/geronimo, pr: 61, result: unchanged }
  - story: "#35"
    result: unchanged
    prs:
      - { repo: sameera/geronimo, pr: 61, result: unchanged }
  - story: "#36"
    result: unchanged
    prs:
      - { repo: sameera/geronimo, pr: 61, result: unchanged }
---

# Close Record: Connect Gmail with consent

## Key Decisions

- **D1 — Request Gmail modify access (#58).** Request Gmail modify access, plus openid and email to identify the mailbox. Request offline access. Force the consent prompt and the account chooser on every connect. Request no other Gmail scope. **Why:** Opening an email writes the read mark (epic #6, same initiative). The metadata and read-only scopes cannot write it. Modify is the smallest scope that allows the read mark and the later archive and trash epics (#8 to #10). Product context already commits to requesting `gmail.modify` from day one. Modify cannot permanently delete, so Google enforces the "never permanently deletes" copy. Forcing the consent prompt makes Google return a refresh token on every connect, which reconnect and duplicate handling need. The openid subject gives a stable mailbox identity. **Refuted alternative:** Request the Gmail metadata scope now and add modify later through incremental consent when the user first opens an email (#6). It lost because #6 lands in the same first session, so users would face two Google consent screens in their first session. Consent is the named drop-off point. Both scopes are restricted, so starting narrow saves no verification work.
- **D2 — Connecting Gmail is a separate flow from sign-in (#58).** Connecting Gmail is its own OAuth flow, separate from sign-in. It has its own callback address and attempt kind and uses the same Google client. Sign-in stays identity-only and stores no token. **Why:** This keeps epic #2's guarantees (identity scopes only, no stored token) intact. A user may connect a mailbox other than the account they signed in with. That is the normal case for #36. **Refuted alternative:** Ask for Gmail scopes during sign-in, so one Google trip does both. It lost because it puts the consent before the user has read the pre-consent copy, which defeats the epic's purpose. It also cannot cover extra mailboxes.
- **D3 — Bind a connect to the signed-in user and browser (#58).** A connect starts only from a same-origin form post by a signed-in user. Its attempt is single-use, expires after a few minutes, uses PKCE, and is bound to the starting browser and the starting Geronimo account. The callback exchanges the code only when a signed-in session for that same account is present. With no session, the code is never exchanged and the user goes to the sign-in page. A different signed-in account, or a missing, used, expired or mismatched attempt, gets the generic failure message and connects nothing. **Why:** This blocks connect injection, where an attacker gets a victim's browser to finish a flow the attacker started and so attaches the attacker's mailbox, or attaches a mailbox to the wrong user. It meets #34's "not signed in, nothing connected, sent to sign in". It reuses the proven attempt pattern from sign-in. **Refuted alternative:** Leave the callback ungated and connect against the account stored on the attempt alone. It lost because a session that expired mid-flow would still attach a mailbox. That breaks #34's acceptance criterion and the rule that every signed-in action needs a live session.
- **D4 — Reject partial grants (#58).** After the exchange, read the scopes Google reports as granted. If Gmail modify is missing, throw the tokens away, store nothing, and show a fixed message naming the missing Gmail permission, with a retry. Geronimo never calls Google's revoke endpoint in this epic. **Why:** Google's granular consent lets users untick the Gmail box. The scopes reported with the token are the only reliable signal. Revoking at Google works on the whole grant for this client, so a revoke could also cancel tokens Geronimo holds for that mailbox from an earlier connect. **Refuted alternative:** Accept the partial grant and let the first Gmail call fail later. It lost because #34 requires that the account "is not shown as connected and the user is told what is missing".
- **D5 — One Gmail account per Geronimo user, keyed by Google subject (#58).** A connected mailbox is unique per (Geronimo account, Google subject of the mailbox). Connecting a mailbox that is already connected adds no row. It replaces the stored refresh token and granted scopes and sets the status to *connected*. The user is told "already connected" if it was connected, or "reconnected" if its access had been removed. The old token is never revoked (see D4). The same mailbox may be connected under two different Geronimo accounts, each with its own grant. **Why:** The subject is stable. The email address can change. One replace-in-place path covers both #36's duplicate case and #35's reconnect. Each mailbox keeps its own consent, as #36's notes say. **Refuted alternative:** Spot duplicates by email address. It lost because addresses can be renamed, and Workspace aliases make email a weak identity.
- **D6 — Store only an encrypted refresh token (#58).** Store only the refresh token. Encrypt it at rest with authenticated encryption from the Go standard library, under a token key taken from a new required environment variable. Bind the ciphertext to its mailbox record, so a token cannot be moved to another row. Mint access tokens in memory and never persist them. The service refuses to start without a valid token key. A token that cannot be decrypted makes its mailbox *access removed*, never a crash or an error page. **Why:** A refresh token gives standing access to someone's mailbox. Copies and backups of the SQLite file must not leak it. Google's restricted-scope security assessment expects encryption at rest. The standards forbid committed secrets, so the key lives in the environment like the client secret. **Refuted alternative:** Store tokens in plain text and rely on file permissions on the single host. It is simpler and the host is the owner's own machine. It lost because the database file is the thing most likely to be copied (backups, debugging), and that copy would carry live mailbox access.
- **D7 — Prove Gmail answers before showing connected (#58).** Before marking a mailbox connected, make one Gmail profile request through the Gmail access point. If it fails, the mailbox is not connected and the generic failure message is shown. **Why:** A Google account can grant the scope without having a working Gmail mailbox (for example, a Workspace account with Gmail turned off). "Gmail shows as connected" should mean Gmail answered. The profile request returns only account-level data, no message content. **Refuted alternative:** Trust a successful token exchange alone. It is one fewer call. It lost because a mailbox that cannot sync would show as connected and only fail in epic #4.
- **D8 — Detect revoked access with a check on open (#58).** Detect revoked access with a status check when Geronimo is opened. At most once per 15 minutes per mailbox, refresh the access token, within a 3-second limit. Only Google's explicit "invalid grant" answer, a missing Gmail scope, or a token that cannot be decrypted sets *access removed*. Timeouts, network errors and server errors leave the status unchanged, and the page still renders. Epic #4's sync should set the same status when it hits the same answer. **Why:** #35 requires the message "when they open Geronimo". This epic has no sync, so the check on open is the only detector that exists here. A refresh is a token-endpoint call, not a Gmail call, so it carries no body risk. **Refuted alternative:** Wait for the sync in #4 to find revocations. It lost because #35's acceptance criterion could not be met or tested in this epic. Google's push-based revocation events were also weighed. They detect revocation without polling, but need endpoint registration and a verified app, which is disproportionate weight for a solo MVP.
- **D9 — Every failure is a plain message with a retry (#58).** Every failure shows a plain message and a retry or reconnect action, and never an error page. Google's "access denied" answer shows "Gmail was not connected" with retry. A partial grant is handled as in D4. Every other failure shows one generic "Gmail wasn't connected, please try again" message with retry. A mailbox whose access was removed shows "access was removed" with reconnect. Retry and reconnect both open the pre-consent screen. Messages are chosen by fixed keys. Query text is never echoed. **Why:** This directly delivers the epic's second success metric. Fixed keys reuse the sign-in pattern, which blocks reflected-content injection. **Refuted alternative:** none
- **D10 — Home is driven by connected-account state (#58).** With no mailboxes, the user sees the pre-consent screen with one connect action. With one or more, the user sees the list of mailboxes, each with its status, and a connect-another action. A mailbox with removed access shows its own message and reconnect action, and the other mailboxes stay listed as connected. The connect action is a full-page navigation, never an HTMX fragment swap, because Google cannot be reached through a fragment request. The pre-consent screen is mobile-first and has no horizontal scroll at phone width. **Why:** This covers #33's entry rule, #34's "no longer see the pre-consent screen", #35's per-account message, and #36's connect-another. Account banners and chips stay with later epics, as #35 and #36 note. **Refuted alternative:** none
- **D11 — Enforce "never reads message bodies" in the build (#58).** The pre-consent screen states "never reads message bodies". The build enforces it as follows. All Gmail API traffic goes through the Gmail access point. The access point allows only account-profile and metadata-level message requests: headers, labels, IDs and the snippet. It rejects full, raw and attachment requests. A test fails if any Gmail request goes around the access point or asks for a body-bearing format. This epic builds the access point and its test, because its only Gmail call (D7) uses it. Epic #4 must use it. Because the snippet is a short excerpt of the body, the copy also says Geronimo shows short message previews. The "never permanently deletes" line is enforced by Google through D1's scope. **Why:** The modify scope permits body access, so the promise needs a structural guard. Story #33 allows the claim only if the build enforces it. Product principle 2 calls it a commitment, not copy. The lead chose to allow the snippet, so the guard stays at the format level and the copy discloses previews to stay literally true. **Refuted alternative:** Leave the body claim off the copy until epic #4 ships its sync with enforcement. It is honest by construction. It lost because the copy is the trust lever at the main drop-off point, and putting the access point in now costs little.
- **Gmail access point is an allowlist on path and format** (a decision stub the verdict on sameera/geronimo#61 confirmed). **Why:** Gmail's default message format is full, so a denylist would miss a missing format **Refuted alternative:** Denylist of full, raw and attachment paths
- **Status check claims the interval before calling Google** (a decision stub the verdict on sameera/geronimo#61 confirmed). **Why:** Two page loads never check one mailbox twice, and an outage costs at most one 3-second wait per interval **Refuted alternative:** Stamp only on a definitive answer

## Deviation Rationale

none

## Waived Stories

none

## Deferred Scope

none
