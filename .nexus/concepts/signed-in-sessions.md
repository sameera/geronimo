---
title: Signed-In Sessions
aliases: [session, sign out, logout, session cookie]
touches: [google-sign-in, session-gate]
last_updated_by: "#2"
status: active
verification: verified
---

# Signed-In Sessions

A signed-in browser holds a server-side session that the user can end by signing out. The browser keeps only a random token. Signing out ends this browser's session and no other.

## How It Works

Sessions live in a SQLite database with the accounts and sign-in attempts. The cookie holds a long random token, and the database stores only its hash. The cookie is host-only, HttpOnly, Secure, SameSite=Lax, and persistent for the session's absolute life. A session expires after 7 days idle (sliding, last-seen refreshed at most hourly) or 30 days absolute. Both lifetimes are configuration and must be positive. Sign-out is a POST accepted only from Geronimo's own origin. It deletes this browser's session, expires the cookie and shows the sign-in page with a note that the user may still be signed in to Google. Signed-in pages are sent with no-store caching, and HTMX keeps no page snapshots for them, so back does not show old content.

## Key Invariants

1. The database holds only a hash of the session token, so a leaked database gives no usable cookie.
2. Every sign-in issues a new token. A token set earlier is never promoted.
3. Sign-out deletes the session on the server before it answers, so the old cookie stops working at once.
4. Sign-out accepts only a same-origin POST. It uses Fetch-Metadata when sent, falls back to Origin, and refuses when neither shows Geronimo.
5. Sign-out ends this browser's session only. It does not end other browsers' sessions or the Google session.
6. Signed-in responses carry no-store caching, and HTMX history snapshots are off for signed-in pages.
7. Session cookie flags use Lax, not Strict, because the return from Google is a cross-site navigation.

## Integration Points

- [google-sign-in](google-sign-in.md) — a validated sign-in creates the session and sets its cookie.
- [session-gate](session-gate.md) — the gate resolves the cookie to a session on every signed-in request.

## Decision Log

### 2026-11-08 — #2 — Server-side sessions and same-origin sign-out
- **Why:** Sign-out must truly end a session, so the server must be able to revoke it. The database already exists for accounts, so a session table costs little. Hashing the token keeps a database leak from leaking cookies. The owner chose 7 idle and 30 absolute days to limit exposure on shared or lost devices. SameSite=Lax is needed because Strict would drop the attempt cookie on the return from Google and make links from Gmail look signed out. A GET sign-out can be triggered cross-site or by prefetchers, so it is a same-origin POST, with no token framework. HTMX's history cache stores page snapshots and restores them on back with no server request, which no-store does not cover, so it is off. Clear-Site-Data on sign-out is defence in depth only. SQLite with a pure-Go driver was chosen as the simplest store, accepting one instance and modest sync volume.
- **Refuted alternatives:** A stateless signed cookie needs no table but cannot be revoked without a denylist. A page-show script that reloads on back-forward cache still paints old content first. Ending the Google session or signing out everywhere goes beyond this browser and is account management. PostgreSQL handles concurrent sync writes better, but the owner preferred the simpler setup. A CSRF token in the sign-out form was rejected for the Fetch-Metadata and Origin check.
