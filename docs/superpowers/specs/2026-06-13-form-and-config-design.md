# go-bananas v0.3.0 — form handling + config adoption

**Date:** 2026-06-13
**Status:** approved (design)

## Context

After v0.2.0, go-bananas is a curated SSR toolkit: middleware + render + session +
secrets/keys/KMS on top of `gorilla/mux`. The deliberate philosophy (see
`memory/framework-philosophy.md`) is to stay a toolkit, **not** a batteries-included
framework — add only primitives that compose into the existing request cycle, and
avoid anything that forces a storage opinion (rate limiting, DB-backed sessions are
explicitly out).

Comparing against other SSR stacks (Rails/Django/Phoenix; Buffalo; Go API
microframeworks), the one Tier-1 gap that fits the toolkit philosophy is **form
handling** — the parse → validate → re-render-with-errors loop is the core of an SSR
request and is currently fully manual. A secondary, smaller win is standardizing
**configuration** on `go-envconfig` (already a direct dependency) instead of the
example's hand-rolled `env`/`envBool` helpers.

Target: both land via PR(s), then tag **v0.3.0**. They are independent and may ship
as separate PRs.

## Non-goals

- No ORM, generators, DB layer, background jobs, mailer, or rate limiting.
- No DB-backed session store (storage opinion — out per philosophy).
- No new `config` package (decided: per-package `Config` structs instead).
- Routing form errors through i18n `t` — deferred; the message-override hook leaves
  the door open but it is not built in v0.3.0.

---

## Part 1 — `form` package (the primary feature)

New top-level package beside `render`/`response`/`session`. Two new dependencies,
**isolated to this package only** (core stays lean; they appear in root `go.mod` but
nothing else imports them):

- `github.com/gorilla/schema` — struct binding (same family as `gorilla/mux`).
- `github.com/go-playground/validator/v10` — declarative `validate:"..."` tags.

### Public surface

```go
package form // form/form.go

// Errors maps an HTML form field name to its human-readable messages.
type Errors map[string][]string

func (e Errors) Add(field, msg string)
func (e Errors) Get(field string) []string   // nil if none; range-safe in templates
func (e Errors) Has(field string) bool
func (e Errors) Any() bool
func (e Errors) Merge(o Errors)

// Validator is implemented by destination structs that need cross-field or
// otherwise bespoke rules the `validate` tags cannot express. Optional.
type Validator interface {
    Validate() Errors
}

// Bind parses the request body, decodes it into dst, validates it, and returns
// per-field Errors for invalid user input. A non-nil error means the request was
// unprocessable (wrong content type, ParseForm failure, body too large) — the
// handler should respond 400, NOT re-render the form.
func Bind(r *http.Request, dst any, opts ...Option) (Errors, error)

type Option func(*options)
func WithMaxBodyBytes(n int64) Option   // default 10 MiB; bounds multipart parsing
func WithMessages(m map[string]string) Option  // override the per-tag message map
```

### `Bind` pipeline

1. **Parse.** Enforce a max body size (`http.MaxBytesReader`, default 10 MiB), then
   `r.ParseForm()` / `r.ParseMultipartForm()`. Any failure here → `return nil, err`.
   This is the honest split: a malformed *request* is not a validation error.
2. **Decode.** A package-cached `*schema.Decoder` decodes `r.PostForm` into `dst`
   (fields tagged `form:"email"`). Type-conversion failures (e.g. `"abc"` into an
   `int`) are folded into `Errors` keyed by the field ("must be a number") — they are
   *user* errors, not the fatal `error`. `schema.ConversionError` entries are
   translated; an unknown-key is ignored (`IgnoreUnknownKeys(true)`).
3. **Validate (tags).** A package-cached `*validator.Validate` runs `validate:"..."`
   tags via `validate.Struct(dst)`. `validator.ValidationErrors` translate into
   `Errors`, **keyed by the `form` tag name** — achieved with
   `validate.RegisterTagNameFunc` reading the struct's `form` tag — so error keys
   match the HTML field names, not Go struct names. Each violation maps to a message
   via a small built-in English map (`required` → "is required", `email` → "must be a
   valid email address", `min`/`max`/`len` → sensible defaults, fallback → "is
   invalid"), overridable with `WithMessages`.
4. **Validate (bespoke).** If `dst` implements `Validator`, run `Validate()` and
   `Merge` its result over the tag errors.
5. Return `(errs, nil)`.

The `schema.Decoder` and `validator.Validate` are constructed once at package init
(both are explicitly designed to be reused and are safe for concurrent use).

### Handler usage (real APIs)

```go
type Signup struct {
    Email string `form:"email" validate:"required,email"`
    Age   int    `form:"age"   validate:"gte=18"`
    Terms bool   `form:"terms"` // must-accept: a bespoke rule, see Validate below
}

// Validate covers what tags can't express cleanly (a bool that must be true is a
// validator gotcha — `required` treats false as the zero/missing value). This is
// why the Validator hook exists; its errors merge over the tag errors.
func (s Signup) Validate() form.Errors {
    e := form.Errors{}
    if !s.Terms {
        e.Add("terms", "you must accept the terms")
    }
    return e
}

func (a *app) handleSignup(w http.ResponseWriter, r *http.Request) {
    var in Signup
    errs, err := form.Bind(r, &in)
    if err != nil {                         // unprocessable request
        response.BadRequest(w, r)
        return
    }
    if errs.Any() {                          // invalid input → re-render, input preserved
        a.renderer.RenderHTMLStatus(w, http.StatusUnprocessableEntity, "signup",
            webctx.TemplateMap{"Form": in, "Errors": errs})
        return
    }
    // valid → persist / redirect
    response.SeeOther(w, r, "/thanks")
}
```

### Template integration

Relies on what already exists — no new renderer funcs are strictly required. `Errors`
methods are directly callable in templates, and the renderer already registers
`invalidIf`. CSRF is already handled by `HandleCSRF` (`{{.csrfField}}`); the `form`
package does not touch it.

```html
<form method="POST" action="/signup">
  {{.csrfField}}
  <input name="email" value="{{.Form.Email}}" class="{{invalidIf (.Errors.Has "email")}}">
  {{range .Errors.Get "email"}}<span class="error">{{.}}</span>{{end}}

  <input name="age" value="{{.Form.Age}}" class="{{invalidIf (.Errors.Has "age")}}">
  {{range .Errors.Get "age"}}<span class="error">{{.}}</span>{{end}}
</form>
```

**Preserved input** comes free from handing the bound struct back as `.Form`.
**Documented limitation:** a value that *fails type conversion* (e.g. `age="abc"`)
lands as the struct's zero value, so the raw bad text is not echoed. The escape hatch
is reading `r.PostFormValue("age")` into the template data when that matters; raw-value
preservation can be added later without breaking the API.

### Tests

- `form/form_test.go`, table-driven `Bind`:
  - valid input → empty `Errors`, populated struct;
  - type mismatch (`age="abc"`) → field error, no fatal error;
  - tag violation → `Errors` keyed by the **`form`** name (assert key is `email`, not
    `Email`);
  - `Validator` implementation → its errors merged in;
  - unprocessable request (wrong content type / oversized body) → non-nil `error`;
  - `WithMessages` override applied.
- `form/example_test.go`: runnable `ExampleBind`.
- Example e2e (`examples/ssr-oidc/e2e_test.go`): a validated POST that asserts (a)
  invalid input re-renders 422 with the error text and the preserved value, and (b)
  valid input redirects.

---

## Part 2 — config: adopt `go-envconfig`, per-package `Config` structs

**Decision:** do **not** add a `config` package. Instead, each package that needs
environment configuration exposes its own `Config` struct with `env:` tags — the
pattern `secrets.Config` (`secrets/config.go`) and `keys.Config` (`keys/config.go`)
already use. Applications compose the structs they need into one `AppConfig` and call
`envconfig.Process` once. The example demonstrates the composition.

### Changes

1. **`logging.Config` (new).** Extract the env reading currently inlined in
   `NewLoggerFromEnv` (`logging/logging.go:69-70`) into a struct:
   ```go
   type Config struct {
       Level string `env:"LOG_LEVEL, default=info"`
       Mode  string `env:"LOG_MODE, default=production"`  // "development" → text output
   }
   func NewLoggerFromConfig(c Config) *slog.Logger
   ```
   `NewLoggerFromEnv` is refactored to `envconfig.Process` a `Config` and delegate to
   `NewLoggerFromConfig`, so env parsing lives in exactly one place and existing
   callers/behavior are unchanged.

2. **Example composition (`examples/ssr-oidc/main.go`).** Replace the hand-rolled
   `env` / `envBool` helpers (`main.go:330-343`) with a composed config:
   ```go
   type AppConfig struct {
       Logging logging.Config
       Secrets secrets.Config
       DevMode bool   `env:"DEV_MODE, default=false"`
       Port    string `env:"PORT, default=8080"`
       BuildID string `env:"BUILD_ID, default=dev"`
       OIDC    OIDCConfig   // existing OIDC_* vars, given env tags
   }
   // ...
   var cfg AppConfig
   if err := envconfig.Process(ctx, &cfg); err != nil { /* fatal */ }
   ```
   `DEV_MODE`/`BUILD_ID` feed `render.WithDevMode`/`WithBuildID`; `Port` feeds
   `server.New`. The `env`/`envBool` funcs are deleted.

   Render and server keep their functional-option / `New(port)` APIs unchanged — they
   are configured from the composed struct, not given their own env-reading structs
   (no env duplication, no churn to stable APIs).

### Tests

- `logging`: `NewLoggerFromConfig` honors `Level`/`Mode`; `NewLoggerFromEnv` still
  reads the env (regression).
- Example: existing e2e suite continues to pass with the composed config (the tests
  already set `DEV_MODE` etc. via `t.Setenv`, which `envconfig.Process` reads).

---

## Docs, skills, release

- **Docs site** (`docs/content/en/docs/`): new `forms.md` guide (struct + tags, the
  `Bind` loop, re-render with errors, the type-conversion limitation, CSRF coexistence);
  a `configuration.md` guide (per-package `Config` structs, composing `AppConfig`,
  `envconfig.Process`). Cross-link from getting-started.
- **Skills**: `skills/go-bananas-scaffold/SKILL.md` gains the form POST loop and the
  config composition as reference wiring; `skills/go-bananas-middleware/SKILL.md`
  notes the form package coexists with `HandleCSRF`.
- **README**: feature list gains form handling + config.
- **Release**: after merge + green CI, tag `v0.3.0`, `gh release create` with notes
  summarizing form handling and config adoption (same flow as v0.2.0).

## Build order

1. `form` package + tests (`go get gorilla/schema`, `go get go-playground/validator/v10`).
2. `logging.Config` + `NewLoggerFromConfig` refactor + tests.
3. Example wiring: form POST route + template; composed `AppConfig`; delete
   `env`/`envBool`; extend `e2e_test.go`.
4. Docs + skills + README.
5. PR(s), CI green, merge, tag `v0.3.0`, release.

## Verification

- Core: `make lint` exit 0; `go test ./...` green; new tests pass;
  `go list -deps ./secrets | grep cloud` still 0 (no isolation regression);
  `go list -deps ./form` shows only `gorilla/schema` + `go-playground/validator` as
  notable additions and **no** cloud deps; `govulncheck ./...` clean after the new deps.
- Example: `go test ./...` green incl. new form e2e; manual `go run .` shows an invalid
  submit re-rendering with field errors + preserved input, and a valid submit
  redirecting.
- Docs: `cd docs && hugo --minify` builds clean with the new pages.
- Release: PR checks green → merge → `git tag v0.3.0` → `gh release create` →
  `GOPROXY=https://proxy.golang.org go list -m github.com/mikehelmick/go-bananas@v0.3.0`.
