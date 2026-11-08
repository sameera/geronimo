---
feature: "Inbox Triage"
feature_path: docs/features/inbox-triage
epic: "Sign in to Geronimo"
slug: sign-in-to-geronimo
created: 2026-11-08
type: enhancement
complexity: M
complexity_drivers: [one external identity provider, session handling]
concepts: []
link: "#2"
record: "#56"
record_state: closed
---

# Epic: Sign in to Geronimo

## Description

A user can create a session and sign in and out of Geronimo. The user signs in with their Google identity, and Geronimo keeps a session so the user does not sign in on every page.

This is the first step of the first session. Connecting Gmail with mail access is a separate epic (#3). Signing in only identifies the user.

## Success Metrics

- A new user reaches a signed-in page from the sign-in page in under 30 seconds.
- A signed-out browser can open no signed-in page.

## Personas

Per `docs/product/context.md`.

## Smallest Usable Version

Create account session; Sign out

## User Stories

### Story #31: Create account session

- **story_type:** user
- **size:** M

**As a** visitor, **I want** to sign in with my Google account, **so that** Geronimo knows who I am and keeps me signed in.

#### Acceptance Criteria

- [ ] **Given** a visitor with no account, **when** they sign in with Google for the first time, **then** a Geronimo account is created and they land on a signed-in page
- [ ] **Given** a returning user, **when** they sign in with Google, **then** they reach the signed-in page with the same account and no duplicate is created
- [ ] **Given** a signed-in user, **when** they reload or return to Geronimo in the same browser, **then** they are still signed in
- [ ] **Given** a signed-out browser, **when** it requests a signed-in page, **then** it is sent to the sign-in page
- [ ] **Given** a visitor who cancels or is refused at Google, **when** they return to Geronimo, **then** they see the sign-in page with a plain message and no account is created

#### Notes

Signing in asks for identity only. Mail access is requested in a later epic.

### Story #32: Sign out

- **story_type:** user
- **size:** S

**As a** signed-in user, **I want** to sign out, **so that** nobody else using this browser can see my account.

#### Acceptance Criteria

- [ ] **Given** a signed-in user, **when** they choose sign out, **then** the session in this browser ends and they see the sign-in page
- [ ] **Given** a user who has signed out, **when** they use the browser's back button, **then** no signed-in page content is shown

## Assumptions

- Google is the only sign-in method in this epic

## Out of Scope

- Sign-in with email and password or with Microsoft
- Deleting the Geronimo account
- Gmail connection and mail scopes, covered by the next epic in the initiative

## Open Questions

## Implementation Sequence

| Issue | blocked_by |
|---|---|
| #31 | none |
| #32 | #31 |
