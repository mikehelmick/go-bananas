// Copyright 2026 the go-bananas authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// Package form binds and validates HTML form submissions into Go structs.
//
// Bind is the headline API: it parses an *http.Request body, decodes the form
// values into a destination struct (using "form" struct tags via
// gorilla/schema), runs declarative validation (using "validate" struct tags
// via go-playground/validator), and optionally runs bespoke cross-field rules
// via the Validator interface. User-facing problems (a non-numeric value in a
// numeric field, a missing required field, a malformed email, and so on) are
// reported as an Errors value keyed by form field name. Only genuinely
// unprocessable requests (a body that exceeds the size limit, an unparseable
// content type) are reported as a returned error, which a handler should
// translate into an HTTP 400 response.
package form

import (
	"errors"
	"maps"
	"net/http"
	"reflect"
	"strings"

	"github.com/go-playground/validator/v10"
	"github.com/gorilla/schema"
)

// defaultMaxBodyBytes is the default maximum number of bytes Bind will read
// from a request body before treating the request as unprocessable. It is 10
// MiB, which is generous for ordinary form submissions while still bounding
// memory use.
const defaultMaxBodyBytes int64 = 10 << 20

// Validator is implemented by destination structs that need cross-field or
// otherwise bespoke validation rules that the "validate" struct tags cannot
// express cleanly. It is optional: Bind only invokes Validate when the
// destination (or the value it points to) implements the interface.
//
// Validate returns an Errors value keyed by form field name. The returned
// errors are merged over (appended to) any errors already produced by tag
// validation.
type Validator interface {
	Validate() Errors
}

// decoder is a package-level, concurrency-safe gorilla/schema decoder reused
// across all Bind calls. It is configured to read "form" tags and to ignore
// unknown keys so that extra form fields (CSRF tokens, submit buttons, and so
// on) do not cause errors.
var decoder = func() *schema.Decoder {
	d := schema.NewDecoder()
	d.IgnoreUnknownKeys(true)
	d.SetAliasTag("form")
	return d
}()

// validate is a package-level, concurrency-safe go-playground validator reused
// across all Bind calls. Its tag-name func makes FieldError.Field() report the
// "form" tag name rather than the Go struct field name, so error keys line up
// with the names used in HTML and by the schema decoder.
var validate = func() *validator.Validate {
	v := validator.New()
	v.RegisterTagNameFunc(func(fld reflect.StructField) string {
		name := strings.SplitN(fld.Tag.Get("form"), ",", 2)[0]
		if name == "-" || name == "" {
			return fld.Name
		}
		return name
	})
	return v
}()

// defaultMessages maps a validator tag to the human-readable message used when
// that tag's rule is violated. Callers can override individual messages with
// WithMessages. The "default" key supplies the fallback message for any tag
// not otherwise listed.
var defaultMessages = map[string]string{
	"required": "is required",
	"email":    "must be a valid email address",
	"min":      "is too short",
	"max":      "is too long",
	"gte":      "is too small",
	"lte":      "is too large",
	"len":      "has the wrong length",
	"default":  "is invalid",
}

// options holds the resolved configuration for a single Bind call.
type options struct {
	maxBodyBytes int64
	messages     map[string]string
}

// Option configures the behavior of Bind.
type Option func(*options)

// WithMaxBodyBytes sets the maximum number of bytes Bind will read from the
// request body. A request whose body exceeds this limit is treated as
// unprocessable and causes Bind to return a non-nil error. The default is 10
// MiB. A non-positive value is ignored and the default is retained.
//
// The limit is only enforced if nothing has already read the body: net/http's
// ParseForm is idempotent, so if an earlier middleware parsed the form (for
// example a CSRF middleware that reads a posted token), that read used the
// standard library's own limit and Bind's cap no longer applies. To bound body
// size reliably regardless of middleware ordering, wrap the request earlier in
// the chain with http.MaxBytesHandler.
func WithMaxBodyBytes(n int64) Option {
	return func(o *options) {
		if n > 0 {
			o.maxBodyBytes = n
		}
	}
}

// WithMessages overrides the human-readable messages used for one or more
// validation tags. The keys are validator tag names (for example "required" or
// "email"); the special key "default" overrides the fallback message. Tags not
// present in m fall back to the built-in defaults.
func WithMessages(m map[string]string) Option {
	return func(o *options) {
		maps.Copy(o.messages, m)
	}
}

// Bind parses, decodes, and validates the form submission in r into dst.
//
// dst must be a non-nil pointer to a struct. Its fields are populated from the
// request's POST form values using "form" struct tags, then validated using
// "validate" struct tags. If dst (or the struct it points to) implements
// Validator, its Validate method is invoked and the resulting errors are
// merged in.
//
// Bind distinguishes two failure modes:
//
//   - User errors (a wrong type for a field, a failed validation rule) are
//     returned in the Errors result keyed by form field name. In this case the
//     returned error is nil. Callers test Errors.Any to decide whether to
//     re-render the form.
//   - Unprocessable requests (a body that exceeds the configured size limit, a
//     malformed or unparseable body) are reported via the returned error. In
//     this case the Errors result is nil. Handlers should respond with HTTP
//     400 (Bad Request).
//
// Callers should check the returned error first and respond 400 if it is
// non-nil; only then consult Errors.Any to decide whether to re-render. The
// Errors result is safe to use even when nil or empty (its read methods are
// nil-safe), so the two checks must not be collapsed: a non-nil error with a
// nil Errors would make a bare Errors.Any check silently skip the 400.
//
// Only "form"-tagged fields are populated, but every such field is settable by
// the client. Bind to a struct that contains only user-supplied inputs; never
// bind directly to a domain or persistence model, or a client could set fields
// (an ID, an ownership or role flag) by posting extra form keys.
func Bind(r *http.Request, dst any, opts ...Option) (Errors, error) {
	o := &options{
		maxBodyBytes: defaultMaxBodyBytes,
		messages:     make(map[string]string),
	}
	for _, opt := range opts {
		opt(o)
	}

	// Step 1: parse. Cap the body before parsing. http.MaxBytesReader accepts
	// a nil ResponseWriter; it only uses the writer to signal the connection
	// should be closed, which is irrelevant here because we surface the
	// too-large condition through the parse error instead.
	if r.Body != nil {
		r.Body = http.MaxBytesReader(nil, r.Body, o.maxBodyBytes)
	}

	if isMultipart(r) {
		if err := r.ParseMultipartForm(o.maxBodyBytes); err != nil {
			return nil, err
		}
	} else {
		if err := r.ParseForm(); err != nil {
			return nil, err
		}
	}

	errs := make(Errors)

	// Step 2: decode form values into dst. Type-conversion failures are user
	// errors and go into errs; anything else is fatal.
	if err := decoder.Decode(dst, r.PostForm); err != nil {
		if fatal := collectDecodeErrors(err, errs); fatal != nil {
			return nil, fatal
		}
	}

	// Step 3: declarative ("validate" tag) validation.
	if err := validate.Struct(dst); err != nil {
		var verrs validator.ValidationErrors
		if !errors.As(err, &verrs) {
			// e.g. *validator.InvalidValidationError — a programming error,
			// not a user error.
			return nil, err
		}
		for _, fe := range verrs {
			errs.Add(fe.Field(), o.message(fe.Tag()))
		}
	}

	// Step 4: bespoke validation via the Validator interface. Handle dst being
	// a pointer: the method set may be satisfied by either the pointer or the
	// pointed-to value.
	if bespoke := asValidator(dst); bespoke != nil {
		errs.Merge(bespoke.Validate())
	}

	// Step 5: return the populated (possibly empty) Errors; callers use Any.
	return errs, nil
}

// message resolves the human-readable message for a validator tag, preferring
// a caller-supplied override, then a built-in message, then the fallback.
func (o *options) message(tag string) string {
	if msg, ok := o.messages[tag]; ok {
		return msg
	}
	if msg, ok := defaultMessages[tag]; ok {
		return msg
	}
	if msg, ok := o.messages["default"]; ok {
		return msg
	}
	return defaultMessages["default"]
}

// isMultipart reports whether r carries a multipart/form-data body, which must
// be parsed with ParseMultipartForm rather than ParseForm.
func isMultipart(r *http.Request) bool {
	ct := r.Header.Get("Content-Type")
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(ct)), "multipart/form-data")
}

// collectDecodeErrors translates a gorilla/schema decode error into user-facing
// field errors recorded in errs. It returns a non-nil error only when the
// failure is not a per-field conversion problem (which would indicate a
// programming error rather than bad user input).
func collectDecodeErrors(err error, errs Errors) error {
	var multi schema.MultiError
	if !errors.As(err, &multi) {
		return err
	}
	for key, fieldErr := range multi {
		var convErr schema.ConversionError
		if errors.As(fieldErr, &convErr) {
			field := convErr.Key
			if field == "" {
				field = key
			}
			errs.Add(field, conversionMessage(convErr))
			continue
		}
		// A non-conversion entry (which should not occur given
		// IgnoreUnknownKeys) is treated as fatal.
		return fieldErr
	}
	return nil
}

// conversionMessage produces a user-facing message for a schema conversion
// failure, specializing the message for numeric targets.
func conversionMessage(convErr schema.ConversionError) string {
	if convErr.Type != nil && isNumericKind(convErr.Type.Kind()) {
		return "must be a number"
	}
	return "must be a valid value"
}

// numericKinds is the set of integer and floating-point reflect kinds for
// which a friendlier "must be a number" message is appropriate.
var numericKinds = map[reflect.Kind]struct{}{
	reflect.Int:     {},
	reflect.Int8:    {},
	reflect.Int16:   {},
	reflect.Int32:   {},
	reflect.Int64:   {},
	reflect.Uint:    {},
	reflect.Uint8:   {},
	reflect.Uint16:  {},
	reflect.Uint32:  {},
	reflect.Uint64:  {},
	reflect.Float32: {},
	reflect.Float64: {},
}

// isNumericKind reports whether k is one of the integer or floating-point
// kinds.
func isNumericKind(k reflect.Kind) bool {
	_, ok := numericKinds[k]
	return ok
}

// asValidator returns dst as a Validator if either dst itself or the value it
// points to implements Validator, or nil otherwise.
func asValidator(dst any) Validator {
	if v, ok := dst.(Validator); ok {
		return v
	}
	rv := reflect.ValueOf(dst)
	if rv.Kind() == reflect.Pointer && !rv.IsNil() {
		if v, ok := rv.Elem().Interface().(Validator); ok {
			return v
		}
	}
	return nil
}
