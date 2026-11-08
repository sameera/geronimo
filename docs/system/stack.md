---
stack: Primary Technology Stack
version: 1.0.0
last_updated: 2026-11-08
---

# Technology Stack

Project type: full-stack web app. This file records the choices made so far.

## Frontend

- **Approach**: Server-rendered HTML with HTMX for partial page updates.

## Backend

- **Language**: Go
- **Role**: Our own service. It syncs mail from Gmail and Outlook and builds the inbox views (topic, sender, urgency).
- **Mail APIs**: Gmail API, Microsoft Graph

## Sign-in and storage

- **Sign-in**: Google OpenID Connect, identity scopes only (`openid`, `email`, `profile`). Libraries: `github.com/coreos/go-oidc/v3` (Apache-2.0) and `golang.org/x/oauth2` (BSD-3-Clause).
- **Database**: SQLite through the pure-Go driver `modernc.org/sqlite` (BSD-3-Clause). One writer at a time and a single server instance. The background sync may force a move to PostgreSQL.
- **Configuration**: environment variables only (`GERONIMO_BASE_URL`, `GERONIMO_GOOGLE_CLIENT_ID`, `GERONIMO_GOOGLE_CLIENT_SECRET`; optional `GERONIMO_ADDR`, `GERONIMO_DB_PATH`). See `.env.example`.
- **Local development**: `http://localhost`, which Google allows as a redirect address. Session cookies are `Secure`, which Chromium and Firefox accept on localhost. Safari may refuse them on plain HTTP.

## Hosting

- **Plan**: the owner's always-on mini PC, published on the public internet through Tailscale Funnel at a stable `ts.net` HTTPS name. Users need no Tailscale.
- **Open before the first deploy**: confirm that Google accepts the `ts.net` name as the OAuth redirect address. If it does not, the fallback is a custom domain behind a Cloudflare Tunnel. Google's restricted-scope review also needs a domain the owner can verify, which a `ts.net` name is not.

## Licensing

- **License**: GPL-3.0 (`LICENSE`)
