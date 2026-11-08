---
title: "Close Record: Sign in to Geronimo"
epic: "#2"
feature: "Inbox Triage"
date: 2026-11-08
nexus_version: 0.128.0
analyze: ran 2026-11-08 @ 215f933b55cbee056dc639ba06aca516737b9170
record: "#56"
record_hash: e5e1ebf9fd7bdabf0cae10b4386d902bcfbf5879db2a52471ce8e5e1e60ba5eb
range:
  - repo: sameera/geronimo
    pr: 57
    base: a4fed273cff77b9b746f46818b911f5872e7ece1
    head: dd1033fa66947cb6798c1cde3a2a1e5dbfd29cdf
story_ranges:
  - story: "#31"
    ranges:
      - { repo: sameera/geronimo, pr: 57, base: a4fed273cff77b9b746f46818b911f5872e7ece1, head: dd1033fa66947cb6798c1cde3a2a1e5dbfd29cdf }
  - story: "#32"
    ranges:
      - { repo: sameera/geronimo, pr: 57, base: a4fed273cff77b9b746f46818b911f5872e7ece1, head: dd1033fa66947cb6798c1cde3a2a1e5dbfd29cdf }
landed_check:
  - story: "#31"
    result: unchanged
    prs:
      - { repo: sameera/geronimo, pr: 57, result: unchanged }
  - story: "#32"
    result: unchanged
    prs:
      - { repo: sameera/geronimo, pr: 57, result: unchanged }
---

# Close Record: Sign in to Geronimo

## Key Decisions

- **D1 — Sign in with Google OpenID Connect, asking for identity scopes only (#56).** Use the authorization-code flow with scopes openid, email and profile. No offline access and no mail scopes. Google's tokens from sign-in are discarded after the ID token is validated. **Why:** The story note says sign-in asks for identity only. The product's trust-first principle asks for the smallest scopes. Identity scopes are non-sensitive, so sign-in needs no restricted-scope verification. Storing no Google tokens at sign-in means no secret at rest until #3. **Refuted alternative:** Ask for the Gmail scope at sign-in, so sign-in and connect are one consent. It would save the user a step. It lost because it breaks the epic boundary with #3 and the pre-consent screen in flow 1 of the user flows. It also puts the trust-critical mail consent before the user has seen anything.
- **D2 — Key the account on Google's stable subject identifier, through a separate login identity (#56).** The account is found by provider and subject identifier. Email and name are display data, refreshed on each sign-in. The login identity is its own record, separate from the account and from any future mailbox connection. **Why:** Email addresses can change or be reused. The subject identifier cannot. A separate login identity lets Microsoft sign-in be added later without changing the account. It keeps "who signed in" apart from "which mailboxes are connected". **Refuted alternative:** Key the account on email address. It is simpler and readable. It lost because an email change at Google would create a duplicate account, breaking #31's returning-user criterion, and a reused address could take over an account.
- **D3 — Server-side sessions with an opaque random token in the cookie (#56).** Sessions live in the database. The cookie carries only a high-entropy random token, and the database stores only its hash. Idle expiry is 7 days, sliding, with last-seen refreshed at most hourly. Absolute expiry is 30 days. Every sign-in issues a new token, which prevents session fixation. **Why:** #32 needs sign-out to truly end the session, so the server must be able to revoke it. The database already exists for accounts, so a session table costs little. Hashing the token means a database leak does not leak usable cookies. The owner chose 7 and 30 days to limit exposure on shared or lost devices. Lifetimes are configuration, so they can change without a redesign. **Refuted alternative:** A stateless signed or encrypted cookie session. It needs no session table and no database read per request. It lost because it cannot be revoked at sign-out without a server-side denylist, which brings back the state it avoided. It also adds a long-lived signing key to manage.
- **D4 — Session cookie flags (#56).** The session cookie uses the host-only secure-prefix form and is HttpOnly, Secure, SameSite=Lax and Path=/. It is persistent, with Max-Age equal to the session's absolute expiry, because #31 requires it to survive a browser restart. The sign-in attempt cookie has the same flags with a 10-minute life. **Why:** Lax, not Strict, because the return from Google is a cross-site top-level navigation. With Strict the attempt cookie would not be sent and every sign-in would fail. A Strict session cookie would also look signed out on links arriving from Gmail. The host prefix blocks subdomain cookie injection. **Refuted alternative:** none
- **D5 — State, nonce and PKCE on every sign-in, held in a single-use server-side sign-in attempt (#56).** Each sign-in start creates an attempt record (10 minutes, single use) holding the state, the nonce and the PKCE verifier. A cookie binds it to the browser. The callback accepts only a matching, unexpired, unconsumed attempt from the same browser, consumes it before doing anything else, and validates the ID token signature, issuer, audience, expiry and nonce through a maintained OIDC library with a GPL-3.0-compatible license, for example go-oidc (Apache-2.0) with x/oauth2 (BSD). **Why:** State blocks login CSRF, where an attacker signs the victim into the attacker's account. PKCE blocks code injection. The nonce blocks ID-token replay. The database exists anyway, so a server-side attempt avoids a second cookie-signing secret. **Refuted alternative:** Carry state, nonce and verifier in an encrypted short-lived cookie with no server record. It is also secure. It lost on needing an extra key to rotate, and it cannot enforce single use.
- **D6 — Default-deny routing behind one session gate, with an HTMX-aware refusal (#56).** Every route is a signed-in route unless it is on the public list. When the gate refuses a request, a full-page request gets a redirect to the sign-in page, and an HTMX request gets an instruction to do a full-page redirect to the sign-in page. **Why:** The epic metric "a signed-out browser can open no signed-in page" must hold for routes added by #3 to #6 without each author remembering to protect them. Opt-out protection fails safe. A plain redirect on an HTMX request would be followed inside the request and swap the sign-in page into a fragment. **Refuted alternative:** Per-route opt-in protection. It is simpler at first. It lost because forgetting to protect one route exposes it, which is the failure the metric forbids.
- **D7 — An account is created only after a fully validated sign-in; every failure lands on the sign-in page with a plain message (#56).** Google's cancel or refusal, any other Google error, a state, attempt or nonce failure, and a failed code exchange all redirect to the sign-in page. The user sees a short message, never an error page or a stack trace. There are two messages: "You cancelled sign-in" for access denied, and "Sign-in didn't complete. Please try again." for everything else. The failure cause is logged on the server with no codes or tokens. A user who is already signed in and opens the sign-in page goes to home. After a successful sign-in the user always lands on home. **Why:** #31's cancel-or-refusal criterion. Creating the account last makes "no account is created" true by construction. Always landing on home keeps the epic small and leaves no redirect target to abuse. **Refuted alternative:** Return the user to the page they first asked for. It is better UX after an expired session. It lost on scope and on adding an open-redirect surface that needs strict local-path checks. The pages users would return to do not exist until #5, so it fits there, or in the re-authentication flow.
- **D8 — Sign-out is a same-origin POST that deletes the server-side session for this browser only (#56).** Sign-out is a form POST, not a link GET. It is accepted only when the request's origin (Origin or Fetch-Metadata header) is Geronimo itself. It deletes the session record, expires the cookie, and redirects to the sign-in page with a "You're signed out" note that says the user may still be signed in to Google. Other browsers' sessions stay. Geronimo does not sign the user out of Google. Sign-in always shows Google's account chooser (D1), so a later visitor on a shared browser is never silently signed in as the previous user. **Why:** #32's sign-out criterion. A GET sign-out can be triggered cross-site or by link prefetchers. The origin check plus SameSite=Lax protects without a token framework. Ending other devices' sessions or the Google session goes beyond "sign out of this browser" and would surprise users. The owner confirmed no "sign out everywhere" for now. **Refuted alternative:** Also end the Google session, or sign out everywhere. Both give stronger protection on shared machines. They lost because ending the Google session is outside what Geronimo controls and would sign the user out of Gmail too. Sign out everywhere is account management, which is deferred.
- **D9 — Back-button safety through no-store, server-side invalidation and no HTMX history snapshots (#56).** Every signed-in response carries no-store caching. HTMX history snapshotting to local storage is off for signed-in pages, so HTMX refetches from the server on back and forward. The sign-out response also asks the browser to clear this site's cache and storage, as defence in depth only. **Why:** #32's back-button criterion. no-store stops the HTTP cache from serving the page. HTMX's own history cache is the non-obvious leak: it stores page snapshots in local storage and restores them on back with no server request, and no-store does not cover it. **Refuted alternative:** A client-side script on page show that reloads when a page comes from the back-forward cache. It works on more browsers. It lost because it still paints the old content for a moment before reloading, which fails "no signed-in page content is shown".
- **D10 — One Google OAuth client shared with #3; mail access is added there by incremental consent (#56).** Sign-in and the Gmail connection use the same Google OAuth client and consent-screen brand. #3 starts its own authorization with the Gmail scope, offline access, include-granted-scopes and a login hint of the signed-in email. #3 stores its refresh token in a mailbox-connection record owned by the account. The client id, client secret and redirect base URL come from environment variables. **Why:** One brand and one verification process. Incremental consent merges grants when the same Google account is used. Sign-in stays unchanged when #3 lands. The owner accepted the Testing-status limit that follows (R2). **Refuted alternative:** Separate OAuth clients for sign-in and for mail. The sign-in client could go public without restricted-scope review. It lost because the user sees two app brands, the incremental-consent merge is lost, and there are two clients to verify and rotate.
- **D11 — Store accounts, login identities, sessions and sign-in attempts in SQLite (#56).** One SQLite database, accessed through a pure-Go driver (no C compiler needed), holds all sign-in data. The owner chose it over PostgreSQL. **Why:** It is the simplest and cheapest choice. It needs no separate database server. A pure-Go driver keeps builds and deployment simple. **Refuted alternative:** PostgreSQL. It handles the concurrent writes of the background sync in #4 alongside web requests better. It lost because the owner prefers the simpler setup and accepts a single instance with modest sync volume.
- **One gate around the whole mux; public list inside the gate** (a decision stub the verdict on sameera/geronimo#57 confirmed). **Why:** Default-deny (D6) must hold for routes later epics add **Refuted alternative:** Per-route middleware
- **Provider interface over go-oidc; fake OIDC server in tests** (a decision stub the verdict on sameera/geronimo#57 confirmed). **Why:** Fast flow tests while G12 still tests real signature checks **Refuted alternative:** Calling Google in tests
- **Failure messages chosen by fixed key in /signin?msg=** (a decision stub the verdict on sameera/geronimo#57 confirmed). **Why:** No server state or flash cookie; query text never echoed **Refuted alternative:** A short-lived flash cookie
- **HTMX refusal returns 200 with HX-Redirect** (a decision stub the verdict on sameera/geronimo#57 confirmed). **Why:** HTMX acts on HX-Redirect for 2xx so the sign-in page is never swapped into a fragment **Refuted alternative:** 401 with HX-Redirect
- **Origin check prefers Sec-Fetch-Site, falls back to Origin, refuses when neither** (a decision stub the verdict on sameera/geronimo#57 confirmed). **Why:** Matches D8 with no token framework and fails closed **Refuted alternative:** A CSRF token in the sign-out form

## Deviation Rationale

none

## Waived Stories

none

## Deferred Scope

none
