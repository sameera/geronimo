---
title: Session Gate
aliases: [auth middleware, default-deny routing, signed-in routes]
touches: [signed-in-sessions]
last_updated_by: "#2"
status: active
verification: verified
---

# Session Gate

One gate in front of every route decides whether a browser may see a signed-in page. Every route is signed-in unless it is on a short public list. A signed-out browser can open no signed-in page.

## How It Works

The gate wraps the whole router. A request to a public route passes straight through. The public routes are the sign-in page, the two Google redirect routes, the health check and static files. For any other route the gate looks up the session from the cookie. With a session, the request continues and the handler can read the account. Without one, the gate refuses. A full-page request is redirected to the sign-in page. An HTMX request gets a 200 response with an instruction to do a full-page redirect, so the sign-in page is not swapped into a fragment. Refusals and signed-in responses carry no-store caching.

## Key Invariants

1. A new route is protected by default. It becomes public only by being added to the public list on purpose.
2. A signed-out request to a signed-in route never receives page content.
3. An HTMX request that is refused gets a 200 response with a redirect instruction, never a plain redirect or a 401.
4. Signed-in and refused responses are never stored by caches.
5. A signed-in user who opens the sign-in page goes to home.

## Integration Points

- [signed-in-sessions](signed-in-sessions.md) — the gate asks for the session behind the cookie and refuses when none is valid.

## Decision Log

### 2026-11-08 — #2 — Default-deny routing behind one gate
- **Why:** The epic metric says a signed-out browser can open no signed-in page. That must hold for routes added by later epics without each author remembering to protect them, so protection is opt-out and fails safe. One gate wraps the whole router with the public list inside it. A plain redirect on an HTMX request would be followed inside the request and put the sign-in page into a fragment. HTMX acts on a redirect instruction only for a 2xx response, so the refusal is 200 with that instruction.
- **Refuted alternatives:** Per-route opt-in protection is simpler at first, but forgetting one route exposes it. Per-route middleware has the same flaw. A 401 with the redirect instruction is not acted on by HTMX.
