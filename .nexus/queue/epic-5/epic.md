---
feature: "Inbox Triage"
feature_path: docs/features/inbox-triage
epic: "Sender view and drill-down"
slug: sender-view-and-drill-down
created: 2026-11-08
type: enhancement
complexity: M
complexity_drivers: [grouping a large mailbox by sender, list navigation with preserved position]
concepts: []
link: "#5"
record: "#67"
record_state: closed
---

# Epic: Sender view and drill-down

## Description

The user sees their inbox grouped by sender and drills into a sender to an email list. This is the first view of Geronimo, and the one the Subscription Hoarder starts from: it shows which senders fill the inbox.

The view only reads. Opening an email in Gmail is the next epic (#6). Syncing mail is the previous one (#4).

## Success Metrics

- A user with a synced inbox reaches the email list of any sender in two taps from the sender view.
- For every sender, the total on its group row equals the number of emails in its list.

## Personas

Per `docs/product/context.md`.

## Smallest Usable Version

Sender groups; Email list; Sort and back navigation

## User Stories

### Story #40: Sender groups

- **story_type:** user
- **size:** M

**As a** user with a synced inbox, **I want** to see my mail grouped by sender, **so that** I can see which senders fill my inbox.

#### Acceptance Criteria

- [ ] **Given** a user with synced mail, **when** they open the sender view, **then** their mail is shown grouped by sender
- [ ] **Given** the sender view, **when** the user reads a group row, **then** it shows the sender's name, the total number of emails, the number unread and the date of the newest email
- [ ] **Given** the sender view is opened without a chosen sort, **when** it loads, **then** groups are ordered by total emails, largest first
- [ ] **Given** a sync still running, **when** the user opens the sender view, **then** the groups from the mail synced so far are shown
- [ ] **Given** a phone-width screen, **when** the user opens the sender view, **then** group rows are readable and tappable without horizontal scrolling

#### Notes

A sender is one email address. The Subscription Hoarder starts here, per `docs/product/context.md`.

### Story #41: Email list

- **story_type:** user
- **size:** S

**As a** user, **I want** to open a sender and see its emails, **so that** I can see what that sender has sent me.

#### Acceptance Criteria

- [ ] **Given** the sender view, **when** the user taps a sender group, **then** they see a list of that sender's emails
- [ ] **Given** an email list, **when** the user reads a row, **then** it shows the subject, the date, and whether the email is read or unread
- [ ] **Given** a sender with many emails, **when** the user scrolls the list, **then** every email of that sender can be reached
- [ ] **Given** a sender group and no sync storing mail since the sender view loaded, **when** the user opens its list, **then** the number of emails in the list equals the total on the group row

#### Notes

Email rows are not tappable in this epic. Opening an email is the next epic.

### Story #42: Sort and back navigation

- **story_type:** user
- **size:** S

**As a** user, **I want** to change the sort order and go back to where I was, **so that** I can work through many senders without losing my place.

#### Acceptance Criteria

- [ ] **Given** the sender view, **when** the user changes the sort order, **then** the groups are re-ordered by the chosen field
- [ ] **Given** an open email list, **when** the user uses back, **then** they return to the sender view
- [ ] **Given** a user who scrolled and sorted the sender view, **when** they return from an email list, **then** the scroll position and sort order are as they left them

#### Notes

Sort fields are the ones on the group row: total, unread and newest date.

## Assumptions

- Mail from all connected Gmail accounts is grouped together, with no account label on rows

## Out of Scope

- Opening an email in Gmail, covered by "Open email in Gmail"
- Archive, delete, undo and selecting emails
- Topic and urgency views
- Account labels on rows

## Open Questions

## Implementation Sequence

| Issue | blocked_by |
|---|---|
| #40 | none |
| #41 | #40 |
| #42 | #40, #41 |
