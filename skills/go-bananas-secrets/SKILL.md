---
name: go-bananas-secrets
description: Use when configuring secret or key management in a go-bananas (github.com/mikehelmick/go-bananas) app — selecting/registering a secrets or keys provider, enabling an opt-in cloud provider (GCP), resolving secret:// config references, or sourcing session-cookie keys from a secret manager. Triggers on "go-bananas secrets", "add a KMS/secret provider", "cookie keys from a secret", "register secrets/gcp".
---

# Secrets & keys in go-bananas

The `secrets` and `keys` packages abstract secret storage and key management
behind small interfaces with self-registering providers. The core ships
dependency-free providers; cloud providers are opt-in sub-packages.

## Select a provider

```go
sm, err := secrets.SecretManagerFor(ctx, &secrets.Config{
	Type: "FILESYSTEM", FilesystemRoot: "/var/run/secrets",
})
km, err := keys.KeyManagerFor(ctx, &keys.Config{
	Type: "FILESYSTEM", FilesystemRoot: "/var/run/keys",
})
```

Core registers `FILESYSTEM` + `IN_MEMORY` (secrets) and `FILESYSTEM` (keys), all
with **zero external dependencies**.

## Opt-in cloud providers

Blank-import the sub-package to register its provider; only then is the cloud SDK
compiled in. Available: `secrets/{gcp,aws,azure,vault}` and
`keys/{gcp,aws,azure,vault}`.

```go
import _ "github.com/mikehelmick/go-bananas/secrets/gcp" // "GOOGLE_SECRET_MANAGER"
import _ "github.com/mikehelmick/go-bananas/secrets/aws" // "AWS_SECRETS_MANAGER"
import _ "github.com/mikehelmick/go-bananas/keys/vault"  // "HASHICORP_VAULT"
```

A binary that imports only `secrets` (not `secrets/gcp`) links no cloud SDK —
verify with `go list -deps ./secrets | grep cloud.google.com`. New providers
follow the identical pattern: a sub-package whose `init` calls
`secrets.RegisterManager` / `keys.RegisterManager`.

## Resolve `secret://` config references

`secrets.Resolver` plugs into `sethvargo/go-envconfig`:

```go
err := envconfig.ProcessWith(ctx, &cfg, envconfig.OsLookuper(), secrets.Resolver(sm, smCfg))
// An env var "secret://projects/p/secrets/db/versions/1" becomes the secret value.
```

Wrap any manager with `secrets.WrapCacher(ctx, sm, ttl)` (TTL caching) or
`secrets.WrapJSONExpander(ctx, sm)` (extract a field from a JSON secret, e.g.
`creds.username`).

## Source session-cookie keys from a secret manager

This composes the infra and web layers — `cookiestore.EntropyFunc` reads its
64-byte keys (32-byte encryption + 32-byte HMAC) from a secret:

```go
sm, _ := secrets.NewFilesystem(ctx, &secrets.Config{FilesystemRoot: dir})
entropy := func() ([][]byte, error) {
	v, err := sm.GetSecretValue(ctx, "cookie-key")
	if err != nil { return nil, err }
	key, err := base64.StdEncoding.DecodeString(v) // a 64-byte key, base64-encoded
	if err != nil { return nil, err }
	return [][]byte{key}, nil
}
store := cookiestore.New(entropy, &sessions.Options{Path: "/"})
```

Returning multiple keys enables rotation: cookies encode with the first and
decode against all. See `examples/ssr-oidc/main.go` for an end-to-end example
that generates and persists the key on first run.

Full API: https://pkg.go.dev/github.com/mikehelmick/go-bananas/secrets
