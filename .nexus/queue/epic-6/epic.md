---
feature: "Inbox Triage"
feature_path: docs/features/inbox-triage
epic: "Open email in Gmail"
slug: open-email-in-gmail
created: 2026-11-08
type: enhancement
complexity: S
complexity_drivers: [one write-back to Gmail, refresh on return]
concepts: []
link: "#6"
---

# Epic: Open email in Gmail

## Description

Tapping an email marks it read in Gmail and opens it in the Gmail web UI. The list refreshes when the user returns to Geronimo.

Geronimo is the triage layer, not the reader. Reading, replying and searching happen in Gmail. This epic ends the first sprint: a user can sign in, connect Gmail and triage their inbox by sender. The email list is the previous epic (#5).

## Success Metrics

- Every tap on an email opens that email in Gmail.
- After returning to Geronimo, the tapped email shows as read.

## Personas

Per `docs/product/context.md`.

## Smallest Usable Version

Mark read and open in Gmail; Refresh on return

## User Stories

### Story #43: Mark read and open in Gmail

- **story_type:** user
- **size:** S

**As a** user browsing an email list, **I want** to tap an email and read it in Gmail, **so that** I can read or reply there and keep triaging in Geronimo.

#### Acceptance Criteria

- [ ] **Given** an email list, **when** the user taps an email, **then** the email opens in the Gmail web UI in a new tab
- [ ] **Given** an unread email, **when** the user taps it, **then** the email is marked read in Gmail
- [ ] **Given** a user with several connected Gmail accounts, **when** they tap an email, **then** it opens in the Gmail account that holds it
- [ ] **Given** Gmail does not accept the read mark, **when** the user taps an email, **then** the email still opens and the user is told the read mark was not saved

#### Notes

On a phone, the link may open the Gmail app or its mobile web view. Geronimo cannot control which.

### Story #44: Refresh on return

- **story_type:** user
- **size:** S

**As a** user who read an email in Gmail, **I want** the list to be up to date when I come back, **so that** I see what I have already dealt with.

#### Acceptance Criteria

- [ ] **Given** a user who opened an email in Gmail, **when** they return to Geronimo, **then** the list refreshes
- [ ] **Given** a refreshed list, **when** it shows the email the user just opened, **then** that email is shown as read and its sender's unread count is one lower
- [ ] **Given** a refresh on return, **when** the list reloads, **then** the scroll position and sort order are kept

#### Notes

Marking an email unread happens in Gmail. Geronimo shows it after the next sync.

## Assumptions

- A tap marks the email read even if the user returns without reading it

## Out of Scope

- Reading, replying and searching inside Geronimo
- Archive, delete, undo and follow-up
- Opening emails from Outlook

## Open Questions

## Implementation Sequence

| Issue | blocked_by |
|---|---|
| #43 | none |
| #44 | #43 |
