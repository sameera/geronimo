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
- **Configuration**: environment variables only (`GERONIMO_BASE_URL`, `GERONIMO_GOOGLE_CLIENT_ID`, `GERONIMO_GOOGLE_CLIENT_SECRET`, `GERONIMO_TOKEN_KEY`; optional `GERONIMO_ADDR`, `GERONIMO_DB_PATH`). See `.env.example`.
- **Local development**: `http://localhost`, which Google allows as a redirect address. Session cookies are `Secure`, which Chromium and Firefox accept on localhost. Safari may refuse them on plain HTTP.

## Gmail connection

- **Connect flow**: separate from sign-in. Redirect address: `<GERONIMO_BASE_URL>/connect/google/callback`. Register it with the same Google client, next to the sign-in address.
- **Scopes**: `openid`, `email` and `https://www.googleapis.com/auth/gmail.modify`. No other Gmail scope.
- **Gmail access point**: `internal/gmail` is the only code that calls the Gmail API. It refuses full, raw and attachment requests. Every Gmail read, including epic #4's sync, goes through it. Its one write is `MarkRead`, which removes only the `UNREAD` label from one message (epic #6). The sync's Gmail interface has no write.
- **Token storage**: only the refresh token is stored, encrypted with AES-256-GCM under `GERONIMO_TOKEN_KEY`. Access tokens are never stored.

### Token key (`GERONIMO_TOKEN_KEY`)

- **Required.** The service refuses to start without it. It must be base64 of exactly 32 random bytes. Generate one with `openssl rand -base64 32`.
- **Keep it persistent.** Set it once and keep the same value across restarts and deploys.
- **Back it up apart from the database.** A database copy without the key exposes no usable token. A backup that holds both defeats the encryption.
- **Losing or replacing the key** makes every stored token unreadable. Every Gmail account then shows "access was removed", and every user must reconnect Gmail. There is no key rotation.
- **Recovery**: restore the key from its backup and restart. If no backup exists, set a new key, restart, and tell users to reconnect.

## Hosting

- **Plan**: the owner's always-on mini PC, published on the public internet through Tailscale Funnel at a stable `ts.net` HTTPS name. Users need no Tailscale.
- **Open before the first deploy**: confirm that Google accepts the `ts.net` name as the OAuth redirect address. If it does not, the fallback is a custom domain behind a Cloudflare Tunnel. Google's restricted-scope review also needs a domain the owner can verify, which a `ts.net` name is not.

## Licensing

- **License**: GPL-3.0 (`LICENSE`)
