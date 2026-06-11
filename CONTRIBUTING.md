# Contributing to go-bananas 🍌

Thanks for your interest in contributing! Issues and pull requests are welcome.

## Development setup

go-bananas requires **Go 1.26** or newer. The repository contains two Go
modules:

- the root module (the framework itself), and
- [`examples/ssr-oidc`](./examples/ssr-oidc), a separate module so its
  OIDC/demo dependencies never enter the core module graph.

```sh
git clone https://github.com/mikehelmick/go-bananas.git
cd go-bananas

# Framework
make test          # go test (short mode, shuffled)
make lint          # golangci-lint (pinned version, installed automatically)

# Example app
cd examples/ssr-oidc
go test ./...
go run .           # then open http://localhost:8080
```

## Before you open a pull request

1. **Run the checks CI runs:** `make test` and `make lint` from the repo root,
   and `go test ./...` in `examples/ssr-oidc` if you touched the example.
2. **Add a copyright header** to any new `.go` file. Line 1 must contain
   `Copyright <year> the go-bananas authors` (CI enforces this) followed by the
   Apache 2.0 license block — copy it from any existing file.
3. **Write godoc.** Every exported identifier needs a doc comment; headline
   APIs should have a runnable `Example` test (these are verified by `go test`
   and rendered on pkg.go.dev, so they can't rot).
4. **Keep the core lean.** New dependencies need a strong justification. Cloud
   SDKs must live in opt-in, self-registering sub-packages (see
   `secrets/gcp` for the pattern) so consumers of the core link zero cloud
   dependencies — verify with `go list -deps ./secrets`.
5. **Fill in the PR template**, including the release note block.

## Working on the documentation site

The docs live in [`docs/`](./docs) (Hugo + Docsy via Hugo Modules) and deploy
to [mikehelmick.github.io/go-bananas](https://mikehelmick.github.io/go-bananas)
on merge to `main`:

```sh
cd docs
hugo mod npm pack && npm install   # one-time: fetch Docsy's npm deps
hugo server                        # live-reload preview
```

## Reporting bugs and requesting features

Use the [issue templates](https://github.com/mikehelmick/go-bananas/issues/new/choose).
For security vulnerabilities, **do not open a public issue** — see
[SECURITY.md](./SECURITY.md).

## License

By contributing, you agree that your contributions will be licensed under the
[Apache License 2.0](./LICENSE).
