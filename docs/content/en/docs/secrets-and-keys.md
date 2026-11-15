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

### Caching and cancellation

`WrapCacher` coalesces concurrent lookups of the same secret into one fetch. That
shared fetch is **detached from the first caller's cancellation and deadline**:
it keeps the caller's context values but always runs with a fixed 30-second
timeout. No caller's deadline shortens or extends it. Cancelling one request, or
one that is close to its deadline, therefore doesn't fail the others waiting on
the same secret, and a hung backend is bounded.

The trade-offs:

- A waiting caller's own context is not honored. A caller whose context is
  cancelled while a fetch is in flight still waits for that fetch to finish, for
  up to 30 seconds.
- The first caller's context values are used for a result shared with every
  waiter, so a provider **must not authorize per caller through context values**
  (for example, a caller identity carried on the context).

## Provider notes

### HashiCorp Vault

- **Secrets read the latest version by default.** A reference is the KV v2 path
  with an optional `?version=N`; without a version, Vault returns the latest.
  (Before v1.1 the default was version 1.) Pin `?version=1` to keep the old
  behavior. References with a URL scheme, host, or fragment are rejected.
- **Signatures are pinned to the signer's key version.** `Sign` sends
  `key_version`, so a signer for `k/2` keeps signing with version 2 after the key
  is rotated.
- **Key IDs** are `name/N`, as returned by `KeyID()` and `CreateKeyVersion`;
  the older `name@N` form is still accepted. The version must be numeric.
  `SigningKeyVersions` lists versions newest first.

### Azure Key Vault

- **No additional authenticated data.** Azure Key Vault's RSA-OAEP-256 wrap has
  no AAD. `Encrypt` with non-empty `aad` **returns an error** rather than
  silently dropping it. `Decrypt` ignores `aad` so data encrypted by earlier
  versions stays readable, and logs a warning once per `KeyVault`.
- **Key IDs** are `VAULT/KEY/VERSION`. That is the form `KeyID()` returns and
  `NewSigner` accepts. `SigningKeyVersions` lists enabled versions only, newest
  first by creation time.
- **Vault names** must match `^[A-Za-z0-9-]{3,24}$`; anything else is rejected
  before a request is sent.

### AWS Secrets Manager

A secret name may carry `@VERSION` and `#STAGE` suffixes
(`SECRET@VERSION#STAGE`), so `@` and `#` cannot appear in secret names.

### Filesystem keys

Key version files are named with decimal digits (as `CreateKeyVersion` writes
them). Files with other names are ignored, and ciphertext whose version prefix
isn't numeric is rejected with `invalid ciphertext: bad version`.

## Composing with sessions

A neat demonstration of the layers composing: feed
[`cookiestore.EntropyFunc`](sessions-and-csrf) from a secret manager so your
session keys are managed like any other secret. The
[example application](https://github.com/mikehelmick/go-bananas/tree/main/examples/ssr-oidc)
does exactly this.
