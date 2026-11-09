---
feature: "Inbox Triage"
feature_path: docs/features/inbox-triage
epic: "Connect Gmail with consent"
slug: connect-gmail-with-consent
created: 2026-11-08
type: enhancement
complexity: M
complexity_drivers: [Google OAuth consent flow, three failure paths]
concepts: []
link: "#3"
record: "#58"
record_state: closed
---

# Epic: Connect Gmail with consent

## Description

A signed-in user reads what Geronimo will and will not do, then connects a Gmail account through Google OAuth. Consent is the main drop-off point in the first session, so the user sees plain copy before Google asks for anything.

A user may connect more than one Google account. This epic ends when Gmail is connected. Syncing mail is the next epic (#4). Signing in is the previous one (#2).

## Success Metrics

- A signed-in user reaches Google's consent step from the pre-consent screen in one tap.
- A user who denies consent, or whose access was revoked, sees a plain message and a way to retry, and never sees an error page.

## Personas

Per `docs/product/context.md`.

## Smallest Usable Version

Pre-consent screen; Google OAuth connection; Denied and revoked consent handling; Connect additional Gmail accounts

## User Stories

### Story #33: Pre-consent screen

- **story_type:** user
- **size:** S

**As a** signed-in user, **I want** to read what Geronimo will and will not do with my mail, **so that** I can decide whether to connect Gmail.

#### Acceptance Criteria

- [ ] **Given** a signed-in user with no connected Gmail account, **when** they open Geronimo, **then** they see the pre-consent screen with a connect action
- [ ] **Given** the pre-consent screen, **when** the user reads it, **then** it states in plain words what Geronimo does with mail and what it never does
- [ ] **Given** the pre-consent screen on a phone-width screen, **when** the user opens it, **then** the copy and the connect action are readable without horizontal scrolling

#### Notes

The copy follows the consent copy section of `docs/design/user-flows.md`. A "never reads message bodies" claim is made only if the build enforces it.

### Story #34: Google OAuth connection

- **story_type:** user
- **size:** M

**As a** signed-in user, **I want** to grant Geronimo access to my Gmail account, **so that** Geronimo can sync my mail.

#### Acceptance Criteria

- [ ] **Given** the pre-consent screen, **when** the user chooses connect, **then** they are taken to Google's consent step
- [ ] **Given** the user grants consent at Google, **when** they return to Geronimo, **then** Gmail shows as connected to their Geronimo account
- [ ] **Given** a user with Gmail connected, **when** they open Geronimo, **then** they no longer see the pre-consent screen
- [ ] **Given** the user grants only part of the access Geronimo asks for, **when** they return to Geronimo, **then** Gmail is not shown as connected and the user is told what is missing
- [ ] **Given** a user who is not signed in, **when** they finish Google's consent step, **then** no Gmail account is connected and they are sent to sign in

#### Notes

Geronimo requests Gmail modify access, the smallest scope that allows the read mark and the later clear actions. In this initiative Geronimo uses it only to read metadata and to write the read mark on open. The scope choice is recorded in the decision record.

### Story #35: Denied and revoked consent handling

- **story_type:** user
- **size:** S

**As a** signed-in user, **I want** a clear message and a way to retry when consent is denied or later revoked, **so that** I am never stuck on an error page.

#### Acceptance Criteria

- [ ] **Given** the user denies consent at Google, **when** they return to Geronimo, **then** they see a plain message that Gmail was not connected and a retry action
- [ ] **Given** a user whose Gmail access was revoked at Google, **when** they open Geronimo, **then** they see a plain message that access was removed and a reconnect action
- [ ] **Given** a user who chooses retry or reconnect, **when** they continue, **then** they start again from the pre-consent screen

#### Notes

Account management and banners for several accounts are a later epic.

### Story #36: Connect additional Gmail accounts

- **story_type:** user
- **size:** S

**As a** signed-in user, **I want** to connect more than one Gmail account, **so that** all my mail can be triaged in Geronimo.

#### Acceptance Criteria

- [ ] **Given** a user with a Gmail account connected, **when** they choose to connect another account, **then** they go through Google's consent step again and return with both accounts shown as connected
- [ ] **Given** a user with one or more Gmail accounts connected, **when** they look for a way to add another, **then** a connect-another action is available
- [ ] **Given** a user who connects an account that is already connected, **when** they return to Geronimo, **then** the account is not duplicated and they are told it is already connected

#### Notes

Each connected account keeps its own consent. Account chips on lists and the unified view come with later epics.

## Assumptions

- Connected accounts are Gmail accounts only
- Outlook is not offered on the pre-consent screen

## Out of Scope

- Syncing mail after connection, covered by "First metadata sync with progress and baseline"
- Clear actions such as archive and delete
- Disconnecting an account or deleting stored data

## Open Questions

## Implementation Sequence

| Issue | blocked_by |
|---|---|
| #33 | none |
| #34 | #33 |
| #35 | #34 |
| #36 | #34 |
