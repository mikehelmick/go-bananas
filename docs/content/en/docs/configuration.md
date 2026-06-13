---
title: Configuration
weight: 11
description: Compose per-package env-tagged Config structs with go-envconfig — no central config package.
---

go-bananas has **no central `config` package**, and that is a deliberate choice.
Each package that needs environment configuration exposes its own small `Config`
struct with [`env`](https://pkg.go.dev/github.com/sethvargo/go-envconfig) tags,
and your application composes the ones it uses into a single struct that
[`sethvargo/go-envconfig`](https://pkg.go.dev/github.com/sethvargo/go-envconfig)
processes in one pass. A package owns the env vars it understands; your app
decides which packages to assemble.

## Per-package Config structs

Several packages already follow this pattern — for example
[`secrets.Config`](secrets-and-keys) and `keys.Config` — and this release adds
`logging.Config`:

```go
type Config struct {
	// Level is the minimum log level ("debug", "info", "warn"/"warning",
	// or "error"). Unrecognized values fall back to info.
	Level string `env:"LOG_LEVEL, default=info"`

	// Mode selects the output format. "development" produces human-readable
	// text; any other value produces JSON.
	Mode string `env:"LOG_MODE, default=production"`
}
```

Once the struct is populated, build the logger from it:

```go
logger := logging.NewLoggerFromConfig(cfg.Logging)
```

See the
[`logging`](https://pkg.go.dev/github.com/mikehelmick/go-bananas/logging) GoDoc —
and each package's GoDoc — for the env vars it owns.

## Composing an application config

An application declares one struct that embeds the framework `Config` structs it
needs and adds its own fields, then processes the whole thing with a single
`envconfig.Process` call:

```go
type appConfig struct {
	Logging logging.Config
	Secrets secrets.Config
	OIDC    oidcConfig

	DevMode bool   `env:"DEV_MODE, default=false"`
	Port    string `env:"PORT, default=8080"`
	BuildID string `env:"BUILD_ID, default=dev"`
}
```

`go-envconfig` walks nested structs, so the `env`-tagged fields inside
`logging.Config` and `secrets.Config` are populated right alongside the
application's own fields. Process them all at once near the top of `main`:

```go
func realMain(ctx context.Context) error {
	var cfg appConfig
	if err := envconfig.Process(ctx, &cfg); err != nil {
		return fmt.Errorf("failed to process configuration: %w", err)
	}

	logger := logging.NewLoggerFromConfig(cfg.Logging)
	ctx = logging.WithLogger(ctx, logger)
	// … hand cfg to the rest of the app.
}
```

The application's own grouped config — here `oidcConfig` — is just another
struct with `env` tags, defined wherever it makes sense in your codebase:

```go
type oidcConfig struct {
	Issuer       string `env:"OIDC_ISSUER"`
	ClientID     string `env:"OIDC_CLIENT_ID"`
	ClientSecret string `env:"OIDC_CLIENT_SECRET"`
	RedirectURL  string `env:"OIDC_REDIRECT_URL"`
}
```

## The `env` tag

The tag names the environment variable and, optionally, supplies a default after
a comma:

```go
Port    string `env:"PORT, default=8080"`           // 8080 when PORT is unset
DevMode bool   `env:"DEV_MODE, default=false"`       // parsed as a bool
Issuer  string `env:"OIDC_ISSUER"`                   // empty when unset (no default)
```

Defaults apply only when the variable is unset, and `go-envconfig` parses the
target type for you (`bool`, numeric types, `time.Duration`, and more). An empty
`OIDC_ISSUER`, for instance, lets the example disable its OIDC flow entirely.

For secret-valued configuration, `go-envconfig` can resolve `secret://`
references through a secret manager while it processes — see
[Secrets & keys](secrets-and-keys#resolving-secrets-in-configuration).

The runnable [`examples/ssr-oidc`](https://github.com/mikehelmick/go-bananas/tree/main/examples/ssr-oidc)
application composes exactly this `appConfig` and processes it once in
`realMain`.
