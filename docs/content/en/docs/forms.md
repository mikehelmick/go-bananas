---
title: Forms
weight: 10
description: Bind, validate, and re-render HTML forms with preserved input and inline errors.
---

The [`form`](https://pkg.go.dev/github.com/mikehelmick/go-bananas/form) package
implements the server-side-rendering form loop: decode a POST into a struct,
validate it, and — when the input is invalid — re-render the same page with the
user's values preserved and per-field error messages shown inline.

## The struct with dual tags

A form is an ordinary struct carrying two sets of tags. The `form` tags drive
**binding** (gorilla/schema) and the `validate` tags drive **validation**
(go-playground/validator). Errors come back keyed by the `form`-tag name, so the
same name identifies a field in your HTML, your binding, and your errors.

```go
type messageForm struct {
	Name    string `form:"name"    validate:"required"`
	Email   string `form:"email"   validate:"required,email"`
	Message string `form:"message" validate:"required,min=3"`
}
```

> **Bind to inputs, never to models.** Every `form`-tagged field is settable by
> the client, so a posted key can fill any field that has one. Use a struct that
> holds only the user-supplied inputs (like `messageForm` above); never bind
> straight to a domain or persistence model, or a request could set an ID, an
> owner, or a role flag by posting an extra form key.

## The Bind loop

`Bind` parses the request body, decodes it into your struct, runs validation,
and returns `(Errors, error)`. The two return values are **two distinct failure
modes**, and keeping them apart is the headline design point:

- The **`error`** means the request was *unprocessable* — the wrong
  content-type, or a body that exceeded the size limit. There is no user input
  worth showing back. Respond `400` and do **not** re-render.
- The **`Errors`** (`errs.Any()`) means the request was processed fine but the
  *user's input was invalid*. Re-render the form with the errors and the
  preserved input.

```go
func (a *app) handleSubmit(w http.ResponseWriter, r *http.Request) {
	var in messageForm
	errs, err := form.Bind(r, &in)
	if err != nil {
		// Unprocessable request (oversized or malformed body): 400.
		response.BadRequest(w, r, a.renderer)
		return
	}

	if errs.Any() {
		// Invalid input: re-render with the user's input preserved and the
		// per-field errors shown inline, at 422 Unprocessable Entity — the
		// conventional status for a form that failed validation.
		a.renderer.RenderHTMLStatus(w, http.StatusUnprocessableEntity, "home", homeData(r, in, errs))
		return
	}

	// Valid: surface a one-shot success flash and post/redirect/get home.
	gbsession.Flash(webctx.SessionFromContext(r.Context())).
		Alert("Thanks for your message, %s!", in.Name)
	response.SeeOther(w, r, "/")
}
```

`RenderHTMLStatus` now permits **422 Unprocessable Entity**, the conventional
status for a form that failed validation. See
[Rendering & templates](rendering-templates) for the renderer API.

## Re-rendering with preserved input

Hand the bound struct back to the template as data so the inputs are
re-populated, and the errors alongside it. A small helper keeps the GET and POST
handlers rendering a uniform shape — both pass a `Form` and a non-nil `Errors`:

```go
func homeData(r *http.Request, in messageForm, errs form.Errors) webctx.TemplateMap {
	m := webctx.TemplateMapFromContext(r.Context())
	m.Title("Home")
	if errs == nil {
		errs = form.Errors{}
	}
	m["Form"] = in
	m["Errors"] = errs
	return m
}
```

In the template, echo each field's value from `.Form`, mark invalid fields with
the renderer's `invalidIf` function (it emits a CSS class when its argument is
true), and range over `.Errors.Get` for the messages. `Get` returns `nil` when a
field has no errors, so it is always safe to range over:

```html
<input type="text" name="email" value="{{.Form.Email}}"
       class="{{invalidIf (.Errors.Has "email")}}">
{{range .Errors.Get "email"}}<span class="error">{{.}}</span>{{end}}
```

`invalidIf` is registered on the renderer's FuncMap automatically — no extra
wiring is required.

## CSRF coexistence

The form package does not touch CSRF; the two are orthogonal and compose. As
long as `HandleCSRF` is in your [middleware chain](middleware-chain), drop the
`{{.csrfField}}` token into the form as usual. `Bind` ignores unknown form keys
(the CSRF token, submit buttons, and so on), so the token does not interfere
with binding:

```html
<form method="POST" action="/submit" novalidate>
  {{.csrfField}}
  <!-- inputs … -->
</form>
```

See [Sessions & CSRF](sessions-and-csrf) for how the token is issued and
verified.

## Bespoke rules with the Validator hook

When a rule can't be expressed cleanly with a `validate` tag — typically a
cross-field rule, or a checkbox that must be checked — implement the optional
`Validator` interface. `Bind` invokes `Validate()` after tag validation and
merges its `Errors` into the result.

A "must accept the terms" checkbox is the classic case. A bound `bool` is `false`
when the box is unchecked, and the `required` tag treats `false` as missing in a
confusing way — so validate it yourself:

```go
type signupForm struct {
	Email  string `form:"email"  validate:"required,email"`
	Accept bool   `form:"accept"` // no validate tag — handled below
}

func (f signupForm) Validate() form.Errors {
	errs := form.Errors{}
	if !f.Accept {
		errs.Add("accept", "you must accept the terms")
	}
	return errs
}
```

`Errors` is a `map[string][]string`; `Add` appends a message for a field and
`Merge` folds another `Errors` in.

## Type-conversion limitation

When the user types a value that can't be converted to the field's type — for
example `abc` into an `int` field — `Bind` records a field error **and** leaves
the struct field at its zero value. That means the offending raw value is *not*
echoed back by `{{.Form.Quantity}}` (it renders `0`, not `abc`). For most forms
this is acceptable. If you need to redisplay the exact text the user typed,
read it straight from the request with the standard library's escape hatch:

```go
rawQuantity := r.PostFormValue("quantity") // the literal "abc" the user typed
```

## Options

`Bind` takes functional options:

| Option | Effect |
|---|---|
| `WithMaxBodyBytes(n int64)` | Cap the request body. A larger body makes `Bind` return the unprocessable `error`. Default: 10 MiB. |
| `WithMessages(m map[string]string)` | Override the message used for one or more validation tags (e.g. `"required"`, `"email"`, `"min"`), keyed by tag name. |

> **Body limit and middleware ordering.** `net/http`'s `ParseForm` is
> idempotent. If an earlier middleware already read the form — a CSRF middleware
> that reads the posted token does this — `Bind`'s `WithMaxBodyBytes` cap is not
> consulted, because the body was already parsed under the standard library's
> own limit. To bound body size regardless of ordering, wrap the request early
> in the chain with [`http.MaxBytesHandler`](https://pkg.go.dev/net/http#MaxBytesHandler).

```go
errs, err := form.Bind(r, &in,
	form.WithMaxBodyBytes(1<<20),
	form.WithMessages(map[string]string{"required": "can't be blank"}),
)
```

The runnable [`examples/ssr-oidc`](https://github.com/mikehelmick/go-bananas/tree/main/examples/ssr-oidc)
application wires this loop end to end — see its `/submit` handler and
`templates/home.html`.

## Out of scope

`form` covers the parse → validate → re-render loop for ordinary form values.
Two adjacent concerns are intentionally left to you:

- **File uploads.** `Bind` decodes form *values*, not uploaded files. For
  `multipart/form-data` file parts, reach for the standard library directly
  after (or instead of) `Bind` — `r.FormFile("avatar")` or `r.MultipartForm.File`.
- **Translated messages.** The built-in validation messages are English; override
  them per tag with `WithMessages`, or set them from your
  [i18n]({{< relref "i18n" >}}) translator in the handler before re-rendering.
  `form` does not call into the i18n package itself.
