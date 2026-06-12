# go-bananas example: SSR + OIDC

A small but complete server-side-rendered application built with
[go-bananas](https://github.com/mikehelmick/go-bananas). It demonstrates how the
web and infrastructure layers compose:

- the recommended **middleware chain** (recovery, request/trace IDs, request
  logging, secure headers, a nonce'd **Content-Security-Policy**, gzip, sessions
  with an idle timeout, CSRF, template variables, current path, locale);
- **HTML rendering** from an `embed.FS` with SRI-tagged, cache-controlled
  `static/` assets and hot-reload in dev mode;
- **internationalization** via the `i18n` package (gettext `.po` files under
  `locales/`; try `/?lang=es`);
- a **CSRF-protected form** using the post/redirect/get pattern with one-shot
  **flash messages**;
- a **JSON** endpoint;
- the pluggable **`Authenticator` seam** guarding a protected `/me` route, with a
  real **OIDC** authorization-code flow (`coreos/go-oidc` + `x/oauth2`);
- **secure-cookie sessions** whose keys are sourced from a `secrets` manager,
  showing the infra and web layers composing.

This is a **separate Go module** (see [`go.mod`](./go.mod)) with a `replace`
directive pointing at the parent. That keeps the OIDC and example-only
dependencies out of the core go-bananas module graph.

## Run it

```sh
cd examples/ssr-oidc
DEV_MODE=true go run .
# then open http://localhost:8080  (try /?lang=es for the Spanish translation)
```

Configuration is via environment variables (all optional):

| Variable | Default | Purpose |
|---|---|---|
| `PORT` | `8080` | Listen port |
| `DEV_MODE` | `false` | Template hot-reload, relaxed HTTPS, dev login, debug errors — opt in explicitly for local development |
| `BUILD_ID` | `dev` | Asset cache-busting query string |
| `SECRETS_DIR` | `./local-secrets` | Where the generated cookie key is stored |
| `LOG_LEVEL` | `info` | `debug`, `info`, `warn`, `error` |
| `OIDC_ISSUER` | _(unset)_ | OIDC discovery URL; when set, enables `/login` |
| `OIDC_CLIENT_ID` / `OIDC_CLIENT_SECRET` / `OIDC_REDIRECT_URL` | | OIDC client config |

On first run a random 64-byte cookie key is generated and written under
`SECRETS_DIR`, then read back through `secrets.NewFilesystem` on every request via
`cookiestore.EntropyFunc`, so sessions survive restarts.

## Routes

| Method | Path | Description |
|---|---|---|
| GET | `/` | Home page: CSRF form + flash messages (localized; `?lang=es`) |
| POST | `/submit` | CSRF-protected; sets a flash, redirects to `/` |
| GET | `/static/...` | Embedded assets with cache headers and SRI tags |
| GET | `/healthz` | Liveness probe (`server.HealthzHandler`) |
| GET | `/readyz` | Readiness probe with pluggable checks (`server.ReadyzHandler`) |
| GET | `/api/health` | JSON health check (app-level demo) |
| GET | `/login` | Start OIDC sign-in (or a notice if unconfigured) |
| GET | `/auth/callback` | OIDC callback |
| GET | `/me` | Protected by `RequireAuthenticated` |
| GET | `/logout` | Clear the session user |
| POST | `/dev/login` | Dev-only: sign in as a fake user (CSRF-protected) |

## Sign in without an identity provider

When `OIDC_ISSUER` is unset, sign-in falls back to a dev-only login: the app
exposes `POST /dev/login` (in dev mode), which stores a fake user on the session
so you can exercise the protected `/me` route without standing up an identity
provider. To try the real flow, point `OIDC_ISSUER` at any compliant provider and
set the client variables.

## Tests

[`e2e_test.go`](./e2e_test.go) drives the assembled router through an
`httptest.Server`: it asserts the home page renders the CSRF meta tag and SRI
asset tags, that a POST without a token is rejected with 401 while a POST with a
token succeeds and surfaces a one-shot flash, that the JSON endpoint works, and
that the protected route returns 401 until the user signs in.

```sh
go test ./...
```
