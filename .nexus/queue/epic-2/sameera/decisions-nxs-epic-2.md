## 2026-11-08 — Gate wraps the whole mux, public list inside the gate
- **Choice:** One gate around the entire router; a path is public only if it is in the Service's public list. Unregistered paths are gated too.
- **Why:** Default-deny (D6) must hold for routes later epics add and for paths that do not exist.
- **Refuted alternative:** Per-route middleware wrapping.

## 2026-11-08 — Provider interface over go-oidc, tests use a fake OIDC server
- **Choice:** `auth.Provider` interface; `auth.Google` implements it with go-oidc and x/oauth2. Handler tests use a fake provider; a signed-token fake IdP tests the ID-token checks.
- **Why:** Flow tests stay fast and deterministic while G12 is still tested against real signature, issuer, audience, expiry and nonce checks.
- **Refuted alternative:** Calling Google in tests.

## 2026-11-08 — Failure messages chosen by fixed key in the query string
- **Choice:** `/signin?msg=cancelled|failed|signed_out`, mapped to fixed text; unknown keys show nothing.
- **Why:** The redirect needs no server state or flash cookie, and query text is never echoed.
- **Refuted alternative:** A short-lived flash cookie.

## 2026-11-08 — HTMX refusal returns 200 with HX-Redirect
- **Choice:** Gate answers an HTMX request with status 200 and `HX-Redirect: /signin`.
- **Why:** HTMX acts on HX-Redirect for 2xx responses, so the sign-in page is never swapped into a fragment.
- **Refuted alternative:** 401 with HX-Redirect.
