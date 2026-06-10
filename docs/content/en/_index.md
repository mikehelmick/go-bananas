---
title: go-bananas
---

{{< blocks/cover title="go-bananas" height="auto" color="primary" >}}

The application framework for Go that is so simple, it's Bananas. 🍌

A lean, server-side-rendered web core plus a small application-infrastructure
layer — composed from focused packages, so you import only what you need.

<a class="btn btn-lg btn-primary me-3 mb-4" href="docs/">Get started</a>
<a class="btn btn-lg btn-secondary me-3 mb-4" href="https://pkg.go.dev/github.com/mikehelmick/go-bananas">API reference</a>

{{< /blocks/cover >}}

{{% blocks/lead color="dark" %}}
go-bananas extracts the genuinely reusable pieces of a production SSR stack — a
template renderer with SRI asset tags, composable middleware, CSRF, secure-cookie
sessions, flash messages — and pairs them with pluggable secrets, keys, and a
graceful HTTP server. One small `Authenticator` seam makes OIDC a wiring
exercise, not a framework dependency.
{{% /blocks/lead %}}

{{% blocks/section color="light" type="row" %}}

{{% blocks/feature icon="fa-solid fa-feather" title="Lean and composable" %}}
Flat, single-purpose packages. The core has no cloud or database dependencies —
add what you need.
{{% /blocks/feature %}}

{{% blocks/feature icon="fa-solid fa-shield-halved" title="Secure by default" %}}
CSRF, secure headers, hot-reloadable secure-cookie sessions, and Subresource
Integrity asset tags out of the box.
{{% /blocks/feature %}}

{{% blocks/feature icon="fa-solid fa-plug" title="Pluggable seams" %}}
One `Authenticator` interface for OIDC. Self-registering secret/key providers.
slog for logging. Bring your own.
{{% /blocks/feature %}}

{{% /blocks/section %}}
