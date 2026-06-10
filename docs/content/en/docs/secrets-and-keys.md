---
title: Secrets & keys
weight: 6
description: Pluggable secret and key managers with opt-in cloud providers.
---

The [`secrets`](https://pkg.go.dev/github.com/mikehelmick/go-bananas/secrets) and
[`keys`](https://pkg.go.dev/github.com/mikehelmick/go-bananas/keys) packages
abstract secret storage and key management behind small interfaces. The core
ships dependency-free providers; cloud providers are **opt-in** sub-packages that
self-register, so their SDKs are only compiled into binaries that import them.

## Selecting a provider

Providers register themselves by name. Choose one with `Config.Type` and
construct it with `SecretManagerFor` / `KeyManagerFor`:

```go
sm, err := secrets.SecretManagerFor(ctx, &secrets.Config{Type: "FILESYSTEM",
	FilesystemRoot: "/var/run/secrets"})

km, err := keys.KeyManagerFor(ctx, &keys.Config{Type: "FILESYSTEM",
	FilesystemRoot: "/var/run/keys"})
```

The core registers `FILESYSTEM` and `IN_MEMORY` for secrets, and `FILESYSTEM`
for keys — all with zero external dependencies.

## Opt-in cloud providers

A cloud provider lives in a sub-package whose `init` calls `RegisterManager`.
Blank-import the ones you need to make their names available:

```go
// secrets
import _ "github.com/mikehelmick/go-bananas/secrets/gcp"   // GOOGLE_SECRET_MANAGER
import _ "github.com/mikehelmick/go-bananas/secrets/aws"   // AWS_SECRETS_MANAGER
import _ "github.com/mikehelmick/go-bananas/secrets/azure" // AZURE_KEY_VAULT
import _ "github.com/mikehelmick/go-bananas/secrets/vault" // HASHICORP_VAULT

// keys
import _ "github.com/mikehelmick/go-bananas/keys/gcp"   // GOOGLE_CLOUD_KMS
import _ "github.com/mikehelmick/go-bananas/keys/aws"   // AWS_KMS
import _ "github.com/mikehelmick/go-bananas/keys/azure" // AZURE_KEY_VAULT
import _ "github.com/mikehelmick/go-bananas/keys/vault" // HASHICORP_VAULT
```

Because the cloud SDK is imported only inside that sub-package, a binary that
imports just `secrets` (not `secrets/gcp`) never compiles or links the SDK. You
can verify this:

```sh
go list -deps ./secrets | grep cloud.google.com   # empty unless you import secrets/gcp
```

New providers follow the identical pattern: a sub-package whose `init` calls
`RegisterManager`.

## Resolving secrets in configuration

`secrets.Resolver` plugs into
[`sethvargo/go-envconfig`](https://pkg.go.dev/github.com/sethvargo/go-envconfig)
to resolve `secret://` references while loading configuration:

```go
sm, _ := secrets.SecretManagerFor(ctx, cfg)
err := envconfig.ProcessWith(ctx, &myConfig, envconfig.OsLookuper(),
	secrets.Resolver(sm, cfg))
// An env var set to "secret://projects/p/secrets/db/versions/1" is replaced
// with the secret's value.
```

Wrap any manager with `WrapCacher` (TTL caching, backed by the
[`cache`](cache) package) or `WrapJSONExpander` (extract a field from a
JSON-valued secret).

## Composing with sessions

A neat demonstration of the layers composing: feed
[`cookiestore.EntropyFunc`](sessions-and-csrf) from a secret manager so your
session keys are managed like any other secret. The
[example application](https://github.com/mikehelmick/go-bananas/tree/main/examples/ssr-oidc)
does exactly this.
