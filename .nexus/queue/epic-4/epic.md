---
feature: "Inbox Triage"
feature_path: docs/features/inbox-triage
epic: "First metadata sync with progress and baseline"
slug: first-metadata-sync-with-progress-and-baseline
created: 2026-11-08
type: enhancement
complexity: M
complexity_drivers: [Gmail API sync of a large mailbox, progress reporting, per-account failure handling]
concepts: []
link: "#4"
record: "#63"
record_state: closed
---

# Epic: First metadata sync with progress and baseline

## Description

After connecting Gmail, the user sees their mail sync with progress, and Geronimo records the unread count at the end of the first sync as the baseline. Later sessions measure unread reduction against this baseline.

The sync reads message metadata only. It changes nothing in the user's mailbox. Sender view is the next epic (#5). Connecting Gmail is the previous one (#3).

## Success Metrics

- The user sees sync progress within 5 seconds of connecting Gmail.
- The baseline unread count of each connected account is recorded once, when its first sync finishes.

## Personas

Per `docs/product/context.md`.

## Smallest Usable Version

Initial metadata sync; Progress strip; Baseline unread count

## User Stories

### Story #37: Initial metadata sync

- **story_type:** user
- **size:** M

**As a** user who has connected Gmail, **I want** my mail to sync into Geronimo without further action, **so that** I can triage it.

#### Acceptance Criteria

- [ ] **Given** a user who has just connected a Gmail account, **when** the connection completes, **then** the sync of that account's mail starts without further action
- [ ] **Given** a sync in progress, **when** the first batch of mail is ready, **then** the user can enter Geronimo without waiting for the rest
- [ ] **Given** several connected accounts, **when** one account's sync fails, **then** the other accounts keep syncing
- [ ] **Given** a sync that stops partway because of a rate limit or revoked access, **when** the user looks at Geronimo, **then** they see a plain message and a retry action (reconnect, when access was removed), and the mail already synced is kept
- [ ] **Given** a sync, **when** it runs, **then** it reads message metadata only and changes nothing in the user's mailbox

#### Notes

The product never reads message bodies. See `docs/design/user-flows.md`, first-run flow.

### Story #38: Progress strip

- **story_type:** user
- **size:** S

**As a** user who has just connected Gmail, **I want** to see how far the sync has got, **so that** I know Geronimo is working and how long to wait.

#### Acceptance Criteria

- [ ] **Given** a sync in progress, **when** the user views Geronimo, **then** a progress strip shows how many emails are synced out of the total
- [ ] **Given** a sync that finishes, **when** the last email is synced, **then** the progress strip shows the sync is done and then goes away

#### Notes

The strip reads like "12,400 of 31,000 emails".

### Story #39: Baseline unread count

- **story_type:** system
- **size:** S

**As a** user, **I want** Geronimo to record my unread count when the first sync finishes, **so that** my progress can be measured against it later.

#### Acceptance Criteria

- [ ] **Given** the first sync of an account finishes, **when** it finishes, **then** that account's baseline equals the number of unread emails in the account's inbox, as Gmail reports it at that moment, and exactly one baseline is stored
- [ ] **Given** a recorded baseline, **when** later syncs run or the user returns, **then** the stored baseline is unchanged
- [ ] **Given** a first sync that fails partway, **when** the user retries and it finishes, **then** no baseline exists before that finish and one baseline exists after it

#### Notes

Showing unread against the baseline on the home page is a later epic.

## Assumptions

- The first sync covers the mail in the user's inbox

## Out of Scope

- Keeping mail up to date after the first sync
- Grouping mail by sender, covered by "Sender view and drill-down"
- Outlook and other providers

## Open Questions

## Implementation Sequence

| Issue | blocked_by |
|---|---|
| #37 | none |
| #38 | #37 |
| #39 | #37 |
