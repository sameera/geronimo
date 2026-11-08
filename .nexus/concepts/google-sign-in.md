---
title: Google Sign-In
aliases: [sign in with Google, login, account creation]
touches: [signed-in-sessions]
last_updated_by: "#2"
status: active
verification: verified
---

# Google Sign-In

A visitor signs in with their Google account, and Geronimo finds or creates their account. Sign-in asks Google for identity only (openid, email, profile) and never for mail access. Mail access is a separate consent in a later epic.

## How It Works

Starting sign-in creates a single-use attempt that lasts 10 minutes. It holds a state value, a nonce and a PKCE verifier. A cookie ties the attempt to the browser. Google sends the visitor back with a code. Geronimo consumes the attempt first, then exchanges the code and validates the ID token (signature, issuer, audience, expiry, nonce). Only then does it find the account by Google's provider and subject identifier, or create one. Email and name are refreshed on every sign-in. The Google tokens are discarded. Then a session starts and the visitor lands on home. Any failure sends the visitor to the sign-in page with a plain message.

## Key Invariants

1. Sign-in requests identity scopes only. No offline access and no mail scopes.
2. An account is created only after the ID token is fully validated, so a failed sign-in never leaves an account.
3. An account is keyed on the provider and subject identifier, never on email. A changed email does not create a second account.
4. Each sign-in attempt works once. The callback consumes it before any other check.
5. A refusal, cancel or error at Google, or any failed check, shows the sign-in page. "You cancelled sign-in" is for a refusal. "Sign-in didn't complete. Please try again." is for everything else. Causes are logged without codes or tokens.
6. A successful sign-in always lands on home, never on a page the visitor first asked for.
7. Message text comes from a fixed key. Query text is never echoed.

## Integration Points

- [signed-in-sessions](signed-in-sessions.md) — a validated sign-in starts a session, with a new token every time.

## Decision Log

### 2026-11-08 — #2 — Sign in with Google, identity only
- **Why:** Sign-in asks for identity scopes only, which keeps to the product's smallest-scopes principle and needs no restricted-scope review. Accounts are keyed on the stable subject identifier through a separate login identity record, so a changed or reused email cannot duplicate or take over an account, and a second provider can be added later. State, nonce and PKCE are held in a single-use server-side attempt, which needs no extra signing key. Creating the account last makes "no account on failure" true by construction. Always landing on home leaves no redirect target to abuse. One Google OAuth client is shared with the Gmail connection epic (#3), which adds mail access by incremental consent. Failure messages use a fixed key in the query string instead of a flash cookie. Tests run against a fake OIDC server behind a provider interface, so flow tests are fast while signature checks stay real.
- **Refuted alternatives:** Asking for the Gmail scope at sign-in would save one consent step. It breaks the epic boundary with #3 and puts the mail consent before the user has seen anything. Keying on email is simpler, but an email change would create a duplicate account. Carrying state in an encrypted cookie needs an extra key and cannot enforce single use. Returning the visitor to the first requested page is better UX, but adds an open-redirect surface.
