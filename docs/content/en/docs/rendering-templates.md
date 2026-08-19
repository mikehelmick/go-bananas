---
title: Rendering & templates
weight: 3
description: The renderer, its FuncMap, SRI asset tags, and hot reload.
---

The [`render`](https://pkg.go.dev/github.com/mikehelmick/go-bananas/render)
package loads templates from an `fs.FS` and renders HTML, JSON, and CSV. It
buffers output before writing, so a template error never produces a partial
response.

## Constructing a renderer

```go
r, err := render.New(fsys,
	render.WithDevMode(devMode),          // reload templates each render; show real errors
	render.WithBuildID(buildID),          // cache-busting query string on assets
	render.WithLogger(logger),            // *slog.Logger for render-time errors
	render.WithFuncs(template.FuncMap{    // add or override template functions
		"greeting": func() string { return "hi" },
	}),
)
```

Templates are discovered by walking the FS: files ending in `.html` become HTML
templates and `.txt` files become text templates. Each template is referenced by
its `{{ define "name" }}`.

## Rendering responses

```go
r.RenderHTML(w, "home", data)                 // 200 OK
r.RenderHTMLStatus(w, http.StatusNotFound, "404", data)
r.RenderJSON(w, http.StatusOK, payload)       // payload, error, or []error
r.RenderCSV(w, http.StatusOK, "report.csv", marshaler)
```

Status codes passed to `RenderHTMLStatus` must be in the renderer's allow-list
(200, 400, 401, 403, 404, 405, 409, 412, 413, 422, 429, 500) — anything else is
refused with a 500, so a typo cannot ship an unexpected status. Check with
`r.AllowedResponseCode(code)`.

The [`response`](https://pkg.go.dev/github.com/mikehelmick/go-bananas/response)
package wraps the common error statuses and negotiates HTML vs JSON vs plain
text from the request's `Accept`/`Content-Type`: `response.BadRequest`,
`Unauthorized` (no or invalid credentials), `Forbidden` (authenticated but not
permitted), `NotFound`, and `InternalError`. Each renders the like-named
template (`"400"`, `"401"`, `"403"`, `"404"`, `"500"`) for HTML clients, so
provide all five.

`RenderJSON` special-cases errors: a single `error` renders as `{"error":"…"}`,
and a joined error (from `errors.Join`) or `[]error` renders as
`{"errors":[…]}`.

## Subresource Integrity asset tags

Place JavaScript under `static/js` and CSS under `static/css` in your FS. The
`cssIncludeTag` and `jsIncludeTag` template functions emit `<link>`/`<script>`
tags with a SHA-512 [Subresource Integrity](https://developer.mozilla.org/docs/Web/Security/Subresource_Integrity)
hash and the build id for cache-busting:

```html
<head>
  {{ cssIncludeTag }}
</head>
<body>
  {{ jsIncludeTag }}
</body>
```

In production the tags are computed once and cached per renderer; in dev mode
they are recomputed each render so edits show up immediately.

## The default FuncMap

Beyond the asset helpers, the default FuncMap includes string helpers
(`joinStrings`, `toSentence`, `trimSpace`, `toUpper`/`toLower`, `stringContains`),
formatting (`humanizeTime`, `toPercent`, `toBase64`, `toJSON`), HTML attribute
helpers (`checkedIf`, `disabledIf`, `selectedIf`, …), URL escaping, and
internationalization (`t`, `tDefault`). Add your own — or override these — with
`WithFuncs` and `WithTextFuncs`.

See the runnable
[`render` examples](https://pkg.go.dev/github.com/mikehelmick/go-bananas/render#pkg-examples)
on pkg.go.dev.
