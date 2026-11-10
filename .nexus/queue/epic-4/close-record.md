---
title: "Close Record: First metadata sync with progress and baseline"
epic: "#4"
feature: "Inbox Triage"
date: 2026-11-10
nexus_version: 0.130.0
analyze: ran 2026-11-10 @ 735ac214656c2d1c0dfb3aa1d88dee3c39432e59
record: "#63"
record_hash: 439555d246d30e89f81870252bbe496b107f88ee7ea7ad922a4a74b4274bea48
range:
  - repo: sameera/geronimo
    pr: 65
    base: b2653ec6defb6db612407c058bff47d91a80cf60
    head: fbf861e979a16a71bda4874661246842e3b0b45e
story_ranges:
  - story: "#37"
    ranges:
      - { repo: sameera/geronimo, pr: 65, base: b2653ec6defb6db612407c058bff47d91a80cf60, head: fbf861e979a16a71bda4874661246842e3b0b45e }
  - story: "#38"
    ranges:
      - { repo: sameera/geronimo, pr: 65, base: b2653ec6defb6db612407c058bff47d91a80cf60, head: fbf861e979a16a71bda4874661246842e3b0b45e }
  - story: "#39"
    ranges:
      - { repo: sameera/geronimo, pr: 65, base: b2653ec6defb6db612407c058bff47d91a80cf60, head: fbf861e979a16a71bda4874661246842e3b0b45e }
landed_check:
  - story: "#37"
    result: unchanged
    prs:
      - { repo: sameera/geronimo, pr: 65, result: unchanged }
  - story: "#38"
    result: unchanged
    prs:
      - { repo: sameera/geronimo, pr: 65, result: unchanged }
  - story: "#39"
    result: unchanged
    prs:
      - { repo: sameera/geronimo, pr: 65, result: unchanged }
---

# Close Record: First metadata sync with progress and baseline

## Key Decisions

- **D1 — Run the sync in the existing service process, driven by a durable sync record per account (#63).** A background worker in the existing service runs each sync. A durable sync record per account, in the existing database, drives it. Writes are batched, one transaction per page. **Why:** There is one server instance and one operator, so a separate worker or queue adds deployment cost for no gain. The durable record makes the sync resumable after a stop, a retry or a restart. Per-page writes hold the database writer for milliseconds, well inside the 5-second busy timeout that sign-in and home requests rely on. **Refuted alternative:** a job-queue library or a separate worker process. It lost because nothing here needs scale-out, and the extra moving part and dependency cost more than they return at this scale.
- **D2 — List the inbox newest first, one page at a time, and do not gate entry (#63).** The sync lists the inbox newest first, one page at a time. It interleaves listing with metadata fetches and saves the listing position and pending IDs after each page. Home stays open throughout, and views show what is synced so far. **Why:** Newest-first makes the first mail stored the most useful, ready within seconds. Interleaving avoids waiting for the full ID listing, which is about 60 pages for 31,000 emails. Without a gate, entering the app is possible from the first second. **Refuted alternative:** a dedicated first-run screen that holds the user until the first batch is stored. It lost because it adds a page and a state, and it delays entry for no gain. Home already shows progress.
- **D3 — Read each message's metadata with one request per message, and store no snippet (#63).** Fetch each email's metadata with one read per message through the existing Gmail access point, in metadata format with a fixed header list. Throttle each account under the per-user quota. Store no snippet. **Why:** Gmail charges the same quota per message whether reads are batched or single, so quota sets the speed either way. Single reads stay inside the access point's allowlist, which epic #3 built to enforce that no bodies are read. A snippet is an excerpt of the body, so leaving it out keeps the consent promise literal. **Refuted alternative:** Gmail's batch endpoint, which takes up to 100 reads per HTTP call. It lost because it saves no quota, so it saves no time. It is also a multipart request that the allowlist cannot inspect the way it inspects single reads.
- **D4 — Add one read request to the Gmail access point, for the inbox counts only (#63).** Widen the Gmail access point's allowlist by exactly one request: read a single label, used only for the inbox label's counts. **Why:** Progress needs the total at once. Gmail's message-list size estimate is inaccurate, and counting by listing takes the whole listing. The baseline needs Gmail's own unread count at that moment. Reading one label returns both counts in one fast call and carries no message content. **Refuted alternative:** take the total from the message list's size estimate. It lost because Gmail documents that estimate as approximate. The strip would show a wrong total, and the baseline would have no exact source.
- **D5 — Back off on rate limits, stop after a bounded run of failures, and keep the stored data (#63).** Isolate failures per account. Back off automatically on rate limits, and stop only after a bounded run of failures. A stop carries a typed reason and keeps all stored data. Retry resumes the same sync. For removed access, the retry action is the existing reconnect flow, and reconnect restarts the sync. Records are keyed by Gmail message ID, so a resume never duplicates. **Why:** Most rate limits clear within seconds, so stopping at once would show users errors that fix themselves. Reusing the existing access-removed status and reconnect keeps one recovery path. Keying by message ID makes a resume safe even when Gmail rejects the saved listing position. **Refuted alternative:** restart the sync from scratch on retry. It is simpler to reason about. It lost because it throws away minutes of quota-bound work and goes against keeping the mail already synced.
- **D6 — Create the sync record in the connect step, and poll the progress strip (#63).** The connect step creates the sync record before it redirects. The progress strip is a home fragment that polls about every 2 seconds and shows one line per account not yet done. Polling stops when no sync is active. **Why:** Creating the record in the connect step puts the strip on the first page after connect, whatever the worker's timing. Polling every 2 seconds fits the existing HTMX setup with no extension. **Refuted alternative:** server-sent events through an HTMX extension. They push updates sooner. They lost because they need an extension and long-lived connections through the public tunnel, for no visible gain at a 2-second refresh.
- **D7 — Show progress as synced count over the inbox total, and show done from the sync's done state (#63).** Show progress as the synced count over the inbox total read when the sync started. Never show a total below the synced count. Show done from the record's done state, never from the two counts being equal. **Why:** Mail arrives in and leaves the inbox during a sync, so the two counts rarely match exactly at the end. The done state is the only reliable signal. **Refuted alternative:** none
- **D8 — Set the baseline to Gmail's inbox unread message count at sync finish, write once (#63).** Set the baseline to Gmail's inbox unread message count, read when the first sync finishes. Store it once per account, in the same transaction that marks the first sync done, and only if none exists. Reconnecting never changes it. **Why:** Inbox unread is the count that triage reduces. Archiving an unread email lowers it, while the all-mail unread count would not drop. Reading Gmail's count follows the story's wording, "the unread count Gmail reports for the account at that moment". One transaction gives no baseline before the finish and one after, even across stops and retries. **Refuted alternative:** count unread among the stored message records. It is cheaper and always matches Geronimo's own data. It lost because the story fixes Gmail's reported count, and mail arriving during the sync makes the local count drift from Gmail's.
- **D9 — Store message metadata in plain text, tied to its account (#63).** Store message metadata in plain text in the database. Every message record and the baseline belong to the account, so removing the account removes them all. **Why:** Sender grouping, sorting and counts in later epics need fields the database can query. The encryption key sits on the same host, so field encryption would guard only against a copy of the database alone, at a large query cost. Tying every record to the account makes account removal a single operation. **Refuted alternative:** encrypt sender and subject fields with the token key. It lost because grouping and filtering by sender would then need decryption inside the application for every query.
- **one sync record, pending list and message set per mailboxes row, not per Geronimo account** (a decision stub the verdict on sameera/geronimo#65 confirmed). **Why:** cursor, baseline and progress are per Gmail account, and one Geronimo account may connect several **Refuted alternative:** one record per Geronimo account
- **each Gmail or token call gets MaxFailures attempts with backoff; a stop follows when one call exhausts them** (a decision stub the verdict on sameera/geronimo#65 confirmed). **Why:** the saved cursor and stored IDs let a retry resume, so no run-wide counter is needed **Refuted alternative:** a counter of consecutive failures across the whole run
- **HTTP 429 and 403 both map to the rate-limited stop reason** (a decision stub the verdict on sameera/geronimo#65 confirmed). **Why:** Gmail's quota errors arrive as 403, and the access point reads no body to tell them apart **Refuted alternative:** 429 only
- **a done sync's strip line is marked shown in the same response that renders it** (a decision stub the verdict on sameera/geronimo#65 confirmed). **Why:** the line appears once even if the poll never arrives, and no extra state or poll is needed **Refuted alternative:** acknowledge on the next poll
- **when the sync record cannot be created after connect, home shows a notice whose retry link goes to /connect** (a decision stub the verdict on sameera/geronimo#65 confirmed). **Why:** the connect step already creates the record, so a second connect reruns it **Refuted alternative:** a new retry route that creates missing sync records
- **a token_unreadable stop offers Retry sync, not Reconnect** (a decision stub the verdict on sameera/geronimo#65 confirmed). **Why:** the record reserves Reconnect for removed access (G6, D5) **Refuted alternative:** Reconnect as the action
- **fetch() runs up to 5 reads at once per mailbox behind one shared pacing gate, so the rate stays near 50/s** (a decision stub the verdict on sameera/geronimo#65 confirmed). **Why:** sequential reads were bounded by round-trip latency, not quota **Refuted alternative:** amend the record to say one read at a time
- **on a 400 with a non-empty cursor, relist from the top in the same attempt; stored IDs are skipped** (a decision stub the verdict on sameera/geronimo#65 confirmed). **Why:** record step 6 says the worker lists again from the top and stored IDs are skipped **Refuted alternative:** stop the sync with reason other on a rejected cursor
- **fetch runs up to 5 reads at once per mailbox behind one shared pacing gate** (a decision stub the verdict on sameera/geronimo#65 confirmed). **Why:** sequential reads were bounded by round-trip latency, not quota (DV4) **Refuted alternative:** amend the record to say one read at a time
- **a rejected listing cursor (HTTP 400) clears the cursor and relists from the top** (a decision stub the verdict on sameera/geronimo#65 confirmed). **Why:** record step 6 says the worker lists again from the top and stored IDs are skipped (DV3) **Refuted alternative:** stop the sync with reason other on a rejected cursor

## Deviation Rationale

- **Departs from record #63 D1 — sameera/geronimo#65 DV1.** The sync record, pending list and message set are keyed per Gmail mailbox (one row per mailboxes row), not per account as D1 states. **Why:** The sync record and baseline are per connected Gmail account (mailboxes row), not per Geronimo login (accounts row). One login can connect several Gmail accounts (mailboxes UNIQUE(account_id, google_subject)), and cursor, pending IDs, baseline, progress and throttle each belong to one Gmail account. Keying on the login would mix two mailboxes' cursors and let one account's failure stop the others (G4). Record #63 should be amended to define "account" as a connected Gmail account in D1, D8 and G16–G18. **Accepted by:** @sameera (https://github.com/sameera/geronimo/pull/65#issuecomment-6739621958)

## Waived Stories

none

## Deferred Scope

none
