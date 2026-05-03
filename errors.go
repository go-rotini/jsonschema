package jsonschema

import (
	"errors"
	"fmt"
)

// CompileError is returned when a schema document is malformed, references a
// non-existent vocabulary, or violates a keyword's value constraints (e.g.
// minLength is a string instead of a non-negative integer).
type CompileError struct {
	// KeywordLocation is the JSON Pointer (or URL with fragment) to the
	// position in the schema where the problem was found.
	KeywordLocation string
	// Message is a human-readable description of the problem.
	Message string
	// Cause carries an optional underlying error (e.g. a Loader I/O
	// failure wrapped by the compiler when resolving an external $ref).
	Cause error
}

// Error returns a human-readable representation.
func (e *CompileError) Error() string {
	switch {
	case e.KeywordLocation != "" && e.Message != "":
		return fmt.Sprintf("jsonschema: compile %s: %s", e.KeywordLocation, e.Message)
	case e.Message != "":
		return "jsonschema: compile: " + e.Message
	case e.Cause != nil:
		return "jsonschema: compile: " + e.Cause.Error()
	default:
		return "jsonschema: compile error"
	}
}

// Is reports whether target is a [*CompileError] sentinel.
func (e *CompileError) Is(target error) bool {
	_, ok := target.(*CompileError)
	return ok
}

// Unwrap returns the underlying cause, if any.
func (e *CompileError) Unwrap() error { return e.Cause }

// RefError is returned when a $ref or $dynamicRef cannot be resolved against
// any in-scope schema resource.
type RefError struct {
	// Ref is the unresolved reference value as written in the schema.
	Ref string
	// BaseURI is the base URI in effect when resolution was attempted.
	BaseURI string
	// Cause carries an optional underlying error (e.g. a Loader fetch
	// failure or a JSON-Pointer syntax error).
	Cause error
}

// Error returns a human-readable representation.
func (e *RefError) Error() string {
	var msg string
	switch {
	case e.Ref != "" && e.BaseURI != "":
		msg = fmt.Sprintf("ref %q against base %q", e.Ref, e.BaseURI)
	case e.Ref != "":
		msg = fmt.Sprintf("ref %q", e.Ref)
	case e.BaseURI != "":
		msg = fmt.Sprintf("base %q", e.BaseURI)
	default:
		msg = "ref"
	}
	if e.Cause != nil {
		return fmt.Sprintf("jsonschema: %s: %s", msg, e.Cause.Error())
	}
	return "jsonschema: " + msg + ": cannot resolve"
}

// Is reports whether target is a [*RefError] sentinel.
func (e *RefError) Is(target error) bool {
	_, ok := target.(*RefError)
	return ok
}

// Unwrap returns the underlying cause, if any.
func (e *RefError) Unwrap() error { return e.Cause }

// LoaderError wraps an underlying I/O / network error from a [Loader].
type LoaderError struct {
	// URI identifies the resource that failed to load.
	URI string
	// Cause is the underlying I/O / network / parse error.
	Cause error
}

// Error returns a human-readable representation.
func (e *LoaderError) Error() string {
	switch {
	case e.URI != "" && e.Cause != nil:
		return fmt.Sprintf("jsonschema: loader %q: %s", e.URI, e.Cause.Error())
	case e.URI != "":
		return fmt.Sprintf("jsonschema: loader %q: failed", e.URI)
	case e.Cause != nil:
		return "jsonschema: loader: " + e.Cause.Error()
	default:
		return "jsonschema: loader: failed"
	}
}

// Is reports whether target is a [*LoaderError] sentinel.
func (e *LoaderError) Is(target error) bool {
	_, ok := target.(*LoaderError)
	return ok
}

// Unwrap returns the underlying cause, if any.
func (e *LoaderError) Unwrap() error { return e.Cause }

// FormatError is surfaced when a value with a "format" keyword fails its
// associated format validator (and format assertion is enabled).
type FormatError struct {
	// Format is the format name (e.g. "date-time", "uuid").
	Format string
	// Value is the offending source value.
	Value string
	// Cause carries an optional underlying parser/validator error.
	Cause error
}

// Error returns a human-readable representation.
func (e *FormatError) Error() string {
	var head string
	switch {
	case e.Format != "" && e.Value != "":
		head = fmt.Sprintf("format %q: value %q", e.Format, e.Value)
	case e.Format != "":
		head = fmt.Sprintf("format %q", e.Format)
	case e.Value != "":
		head = fmt.Sprintf("value %q", e.Value)
	default:
		head = "format"
	}
	if e.Cause != nil {
		return fmt.Sprintf("jsonschema: %s: %s", head, e.Cause.Error())
	}
	return "jsonschema: " + head + ": invalid"
}

// Is reports whether target is a [*FormatError] sentinel.
func (e *FormatError) Is(target error) bool {
	_, ok := target.(*FormatError)
	return ok
}

// Unwrap returns the underlying cause, if any.
func (e *FormatError) Unwrap() error { return e.Cause }

// Sentinel errors. The pointer-typed sentinels (ErrCompile, ErrValidation,
// ErrRef, ErrLoader, ErrFormat) match instances of their concrete error
// type via [errors.Is]; the package-level errors.New sentinels surface
// specific failure conditions that do not warrant a structured type.
var (
	// ErrCompile matches any [*CompileError].
	ErrCompile = &CompileError{}
	// ErrValidation matches any [*ValidationError].
	ErrValidation = &ValidationError{}
	// ErrRef matches any [*RefError].
	ErrRef = &RefError{}
	// ErrLoader matches any [*LoaderError].
	ErrLoader = &LoaderError{}
	// ErrFormat matches any [*FormatError].
	ErrFormat = &FormatError{}

	// ErrUnknownDraft indicates a draft selector that the package does not
	// recognize (typically [DraftUnknown] passed to a Draft-only API).
	ErrUnknownDraft = errors.New("jsonschema: unknown draft")
	// ErrUnknownKeyword indicates a keyword that is not registered for the
	// active draft and [WithStrictKeywords] is enabled.
	ErrUnknownKeyword = errors.New("jsonschema: unknown keyword")
	// ErrUnknownFormat indicates a "format" value with no registered
	// validator while the unknown-format policy is [UnknownFormatError].
	// Surfaced as the [FormatError.Cause] inside the wrapping
	// [*ValidationError.Cause].
	ErrUnknownFormat = errors.New("jsonschema: unknown format")
	// ErrRefCycle indicates a cyclic $ref chain that the compiler could
	// not turn into a lazy edge. Reserved for future use; v0.1's compile
	// path resolves every cycle into a lazy edge bounded at run time by
	// [WithMaxRefDepth] (which surfaces as [ErrMaxRefDepth]).
	ErrRefCycle = errors.New("jsonschema: ref cycle detected")
	// ErrMaxRefDepth indicates a single keyword evaluation followed more
	// than [WithMaxRefDepth] hops.
	ErrMaxRefDepth = errors.New("jsonschema: max ref depth exceeded")
	// ErrMaxValidationDepth indicates the validator recursed past
	// [WithMaxValidationDepth] levels into nested instances.
	ErrMaxValidationDepth = errors.New("jsonschema: max validation depth exceeded")
	// ErrInstanceTooLarge indicates an instance document larger than
	// [WithMaxInstanceSize] was rejected before parsing.
	ErrInstanceTooLarge = errors.New("jsonschema: instance exceeds size limit")
	// ErrLoaderRejected indicates a [Loader] declined a URI scheme (e.g.
	// the default chain rejecting http:// without explicit opt-in).
	ErrLoaderRejected = errors.New("jsonschema: loader rejected URI scheme")
	// ErrSchemaNotCompiled indicates a [*Schema] method was called on a
	// nil receiver, or on a value that was not produced by the compiler
	// (e.g. a zero-value [Schema] literal).
	ErrSchemaNotCompiled = errors.New("jsonschema: schema not compiled")
	// ErrValidationFailed is returned when validation produced no
	// structured [*ValidationError] but the instance was nevertheless
	// rejected.
	ErrValidationFailed = errors.New("jsonschema: validation failed")
	// ErrNilReader indicates a nil [io.Reader] was passed to
	// [*Schema.ValidateReader].
	ErrNilReader = errors.New("jsonschema: nil reader")
	// ErrUnsupportedSchemaShape indicates a schema slot held a value the
	// compiler/runtime cannot evaluate (neither a JSON object nor a
	// boolean schema). Surfaced as the [CompileError.Cause].
	ErrUnsupportedSchemaShape = errors.New("jsonschema: unsupported schema shape")
)

// RenderError produces a human-readable error string.
//
// In v0.1 this is a passthrough: the function returns err.Error() and the
// schema/instance bytes plus the color flag are accepted for forward
// compatibility but are not used. The full source-pointer formatter
// (showing the offending JSON line with a caret marker) is planned for
// v0.2; the signature here is stable across that change.
//
// Programmatic callers should not parse the returned string — use
// [errors.Is] / [errors.As] against the typed errors ([*CompileError],
// [*ValidationError], [*RefError], [*LoaderError], [*FormatError]) and
// switch on [ValidationError.Keyword] for stable classification.
func RenderError(_, _ []byte, err error, _ ...bool) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
