---
product: Geronimo
phase: 1
last_updated: 2026-11-08
---

# User Flows (Phase 1)

## Model

- Geronimo is the daily entry point and triage layer over Gmail and Outlook. The user opens Geronimo instead of the provider inbox.
- The native Gmail or Outlook UI is the reader. It also handles replies, search, threads, and mark-as-unread.
- Geronimo shows email lists only. It does not render message bodies, attachments, or threads.
- Platform is mobile-first responsive web. No native app.
- Accounts are unified: one list, grouped, each row carries an account chip.

## Phase 1 scope

| In | Out (delegated to the native UI, or later) |
| --- | --- |
| Auth, account connection, first-run sync | Email detail, body rendering, attachments |
| Views by topic, sender, urgency | Threads / conversation view |
| Drill-down to an email list | Search |
| Mark read, archive, delete (to trash), follow-up | Mark as unread |
| Open email in native UI | Compose, reply, send |
| Account management, re-auth, settings, privacy | Unsubscribe |
| | Permanent delete, empty trash |

## Flow index

1. [First-run: connect and sync](#1-first-run-connect-and-sync)
2. [Daily open and home](#2-daily-open-and-home)
3. [View to group to email list (drill-down)](#3-drill-down)
4. [Open an email in the native UI](#4-open-an-email-in-the-native-ui)
5. [Clear actions](#5-clear-actions)
6. [Follow-up](#6-follow-up)
7. [Sync and freshness](#7-sync-and-freshness)
8. [Account management and re-auth](#8-account-management-and-re-auth)
9. [Settings, privacy, and exit](#9-settings-privacy-and-exit)
10. [Empty and edge states](#10-empty-and-edge-states)

Authentication and connection creation are specified separately. This document covers what happens between them and what follows.

## 1. First-run: connect and sync

Goal: reach a useful first view fast, and build trust at the consent step.

1. User signs in.
2. User connects Gmail or Outlook. Pre-consent screen states what Geronimo can do and what it never does (see [Consent copy](#consent-copy)).
3. Provider OAuth consent. Return to Geronimo.
4. Initial sync starts. Show progress ("12,400 of 31,000 emails").
5. Record the baseline unread count when the sync finishes.
6. User enters home once the first batch is ready. Sync continues in the background with a progress strip.
7. Optional: connect a second account from home.

Failure states: user denies consent, partial sync, provider rate limit, token revoked mid-sync. Each shows a plain message and a retry action.

### Consent copy

Say plainly: Geronimo can read, archive, and move email to trash. It never permanently deletes. It never reads message bodies. Only claim the last point if code enforces it.

## 2. Daily open and home

Goal: show what needs attention, then offer the views.

- Home shows the unified view across all connected accounts.
- Entry points: urgency summary, the three views (topic, sender, urgency), follow-up list, unread count against baseline.
- Account chips show which mailbox each count comes from.
- A banner appears for any account that needs re-auth. It does not block the other account.

## 3. Drill-down

View → group → email list. Every group is a doorway to emails, never a dead end.

- Group rows: name, total, unread, newest date, account chips.
- Sort and filter controls on each level.
- Email list row: sender, subject, date, account chip, follow-up mark, selection checkbox. Show snippet only if the scope allows it.
- Breadcrumb or back control. Preserve scroll position and filters on return.
- Counts stay consistent across views after any action.

## 4. Open an email in the native UI

1. User taps a row.
2. Geronimo marks the email read (write-back to the provider).
3. Geronimo opens the email in the native UI in a new tab.
    - Gmail: message URL with `authuser` set to the account email.
    - Outlook: the `webLink` from Microsoft Graph.
4. User replies, searches, or reads the thread in the provider.
5. User returns to Geronimo (back button or app switch). The list refreshes on `visibilitychange`.

Notes:

- On mobile the link may open the provider's app or its mobile web view. Geronimo cannot control which. Test both.
- Mark-as-unread happens in the native UI. The next sync picks it up.
- Tapping a row marks it read even if the user then returns without reading. The follow-up mark is how they keep it on their list.

## 5. Clear actions

Actions: mark read, archive, delete, follow-up. Per email, per selection, per group.

| Action | Gmail | Outlook |
| --- | --- | --- |
| Mark read | Remove `UNREAD` label | Set `isRead` |
| Archive | Remove `INBOX` label | Move to Archive folder |
| Delete | Move to Trash | Move to Deleted Items |
| Follow-up | Add `STARRED` label | Set `flag.flagStatus` |

Rules:

- Delete always means move to trash. Geronimo never deletes permanently and has no "empty trash".
- Every action shows an undo toast. Undo restores from trash or reverses the label or flag.
- Bulk actions on a group show the count and confirm: "Archive 214 emails from Acme?"
- A bulk action on a mixed-account group runs against both providers. Report partial failure: "Archived 198 of 214. 16 failed in Outlook. Retry?"
- Counts update in place after each action (HTMX partial update).
- Mobile: checkbox column or long-press to select. Fixed bottom action bar. Touch targets at least 44px.

## 6. Follow-up

- The user marks an email for follow-up from the list.
- The mark is stored in the provider (Gmail star, Outlook flag), so it also shows in the native UI.
- A follow-up view lists all marked emails across accounts, including ones already read.
- Unmarking removes the star or flag.

## 7. Sync and freshness

- New mail must appear soon after it arrives, or users go back to the provider.
- Show a "new emails" pill. Do not reshuffle a list the user is reading.
- Show last-synced time and a manual refresh.
- Changes made in the native UI (read state, star, archive) appear after sync.

## 8. Account management and re-auth

- List connected accounts with status: healthy, syncing, needs re-auth.
- Add, remove, and reconnect accounts.
- Token expired or revoked: persistent banner on that account, one-click reconnect, no loss of the user's place.
- Removing an account deletes its stored data.

## 9. Settings, privacy, and exit

- What Geronimo stores and which scopes it holds.
- Disconnect an account. Delete all data. Delete the Geronimo account.
- Deletion removes personal data (GDPR / CCPA).

## 10. Empty and edge states

- Zero emails in a view or group: a clear "all done" state. This is the reward moment.
- Unclassified mail: an "Other" group so nothing is hidden.
- Very large groups: pagination or incremental load.
- Account in error: show stale data with a banner, not a blank screen.

## Journeys these flows serve

| Journey | Flows | Main risk |
| --- | --- | --- |
| Activation (first session) | 1, 2, 3, 5 | Consent drop-off, long sync |
| Habit (sessions 2 to 4) | 2, 3, 4, 5, 7 | User returns to the provider inbox out of habit |
| Steady state | 2, 3, 4, 6, 7 | No reason to open Geronimo first |
| Recovery | 8, 5 (undo) | Fear of lost mail |
| Exit | 9 | Trust |

Persona entry points: the Overloaded Professional starts from urgency. The Subscription Hoarder starts from the sender view and clears in bulk.

## Open questions

- Does one email appear in several views (a topic group and a sender group)? Counts must stay consistent after a clear.
- Is urgency a view, a home banner, or both?
- What is the classification quality without message bodies? Depends on which fields each scope returns (headers, labels, snippet).
- Do reply deep links open the reply box, or only the thread? Test Gmail and Outlook on mobile.
- Sync strategy for freshness: push (Gmail watch, Graph webhooks) or polling.
