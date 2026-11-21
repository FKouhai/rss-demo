# rss_frontend (Go)

Newspaper-style web UI for the RSS fleet. Go + `templ` + HTMX + Alpine.js,
no Node.js at runtime. Replaces the former Astro frontend.

## Routes

| Route | What |
|---|---|
| `GET /` | Front page: search, feed filter, pagination (HTMX partials) |
| `GET /partials/items` | Feed list fragment (search paging + 120s refresh) |
| `GET /items/{id}` | Reader view: article fetched + sanitized server-side |
| `GET /status`, `GET /partials/status` | Backend health fan-out (30s refresh) |
| `GET /config` | Feeds observed by the local store (read-only until `rss_config` lands) |
| `GET /img?url=` | Image proxy (allowlist `http(s)`, 5MB cap, cached 24h by browsers) |
| `GET /auth/login|callback|logout` | OIDC login via Pocket-ID/Tiny Auth (only when configured) |
| `GET /healthz`, `GET /ready` | Liveness/readiness (public) |

Static assets are embedded (`static/`, vendored `htmx` + `alpine`).

## Data

The frontend is stateless: items come from the `rss_aggregator` HTTP API
(`GET /api/items`, `/api/items/:id`, `/api/feeds` — see
`handlers/aggclient.go` for the exact contract), sessions from OIDC
cookies. Without an aggregator it degrades to an "unavailable" notice
instead of failing pages. Article bodies are fetched on open
(`internal/extract`): server-side fetch, readability selection,
`bluemonday` sanitize, images rewritten through `/img` (SSRF-guarded,
no hotlinking).

## Env

| Var | Default | Note |
|---|---|---|
| `FRONTEND_HTTP_ADDR` | `:4321` | container listens here (compose maps 4321) |
| `AGGREGATOR_URL` | `http://rss_aggregator:3000` | read APIs live here (pending service) |
| `OUTBOX_URL` / `POLLER_URL` / `NOTIFY_URL` | stable DNS `:3000` | status fan-out |
| `OIDC_ISSUER_URL` | empty = **open mode** | set to require login |
| `OIDC_CLIENT_ID` / `OIDC_CLIENT_SECRET` / `OIDC_REDIRECT_URL` | | Pocket-ID client (confidential, PKCE S256) |
| `AUTH_SESSION_KEY` | | base64 32B AES-GCM key for cookies |
| `SERVICE_FQDN`, `*_SKIP_TRACER`, `OTEL_EP` | | shared bootstrap seam |

Pocket-ID setup: confidential client, redirect `https://<host>/auth/callback`,
scopes `openid email profile groups`. Passkeys happen at Pocket-ID;
the frontend only does the code flow (`libs/auth/oidc`).

## Dev

```bash
nix develop .#rss_frontend
templ generate ./...            # after editing views/*.templ (commit output)
tailwindcss -i static/input.css -o static/app.css --minify
go test ./... && golangci-lint run
arion up -d                      # full stack on :4321
```

`static/app.css` is committed (dev `go run` needs it); the nix build
regenerates it. `static/js` is vendored htmx 2.0.4 + Alpine 3.14.8.
