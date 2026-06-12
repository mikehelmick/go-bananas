---
title: Getting started
weight: 1
description: Install go-bananas and stand up a minimal SSR server.
---

## Install

go-bananas requires Go 1.26 or newer.

```sh
go get github.com/mikehelmick/go-bananas@latest
```

## A minimal server

The snippet below renders an HTML page through the [`render`](rendering-templates)
package and serves it with the graceful [`server`](server). Templates are loaded
from any `fs.FS` — an `embed.FS` in production.

```go
package main

import (
	"context"
	"embed"
	"net/http"

	"github.com/gorilla/mux"
	"github.com/mikehelmick/go-bananas/middleware"
	"github.com/mikehelmick/go-bananas/render"
	"github.com/mikehelmick/go-bananas/server"
	"github.com/mikehelmick/go-bananas/webctx"
)

//go:embed templates static
var assets embed.FS

func main() {
	ctx := context.Background()

	h, err := render.New(assets, render.WithDevMode(true))
	if err != nil {
		panic(err)
	}

	r := mux.NewRouter()
	r.Use(middleware.Recovery(h))
	r.HandleFunc("/", func(w http.ResponseWriter, req *http.Request) {
		m := webctx.TemplateMapFromContext(req.Context())
		m.Title("Home")
		h.RenderHTML(w, "home", m)
	})

	// Serve the embedded static assets the renderer's SRI tags point at.
	static := middleware.ConfigureStaticAssets(true)
	r.PathPrefix("/static/").Handler(static(http.FileServerFS(assets)))

	srv, err := server.New("8080")
	if err != nil {
		panic(err)
	}
	_ = srv.ServeHTTPHandler(ctx, r)
}
```

With a `templates/home.html` containing:

```html
{{ define "home" }}<!doctype html>
<title>{{ .title }}</title>
<h1>Hello, Bananas 🍌</h1>{{ end }}
```

## Next steps

- [The middleware chain](middleware-chain) — assemble recovery, logging, secure
  headers, sessions, and CSRF in the right order.
- [Rendering & templates](rendering-templates) — the FuncMap, SRI asset tags, and
  hot reload.
- The runnable [`examples/ssr-oidc`](https://github.com/mikehelmick/go-bananas/tree/main/examples/ssr-oidc)
  application ties everything together, including OIDC.
