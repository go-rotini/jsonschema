package jsonschema

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Number is an alias for [encoding/json.Number]. It preserves the original
// source text of a JSON number so that precision-sensitive keywords such as
// multipleOf can compare against the wire form rather than a lossy float64.
//
// The alias lets values flow between this package, the standard library, and
// the rotini sister packages without a wrapper type at every boundary.
type Number = json.Number

// MapSlice is an ordered slice of key-value pairs. The schema generator emits
// MapSlice for properties / definitions / $defs to preserve Go declaration
// order on round-trip; the validator accepts MapSlice (alongside
// map[string]any) as the in-memory representation of a JSON object.
type MapSlice []MapItem

// MapItem is a single key-value pair within a [MapSlice]. JSON object keys
// are always strings, so [MapItem.Key] is typed as string.
type MapItem struct {
	// Key is the JSON object member name.
	Key string
	// Value is the decoded value for this member.
	Value any
}

// OutputFormat selects the JSON shape produced by [Result.Output] per
// Draft 2020-12 §12.
type OutputFormat int

// Recognized output formats.
const (
	// OutputFlag emits {"valid": true|false} only.
	OutputFlag OutputFormat = iota
	// OutputBasic emits a flat list of assertion outcomes.
	OutputBasic
	// OutputDetailed emits a nested tree mirroring the schema's applicator
	// structure, collapsing groups with no errors.
	OutputDetailed
	// OutputVerbose emits the full nested tree, including passing groups.
	OutputVerbose
)

// String returns a human-readable label for f (e.g. "flag", "basic").
func (f OutputFormat) String() string {
	switch f {
	case OutputFlag:
		return "flag"
	case OutputBasic:
		return "basic"
	case OutputDetailed:
		return "detailed"
	case OutputVerbose:
		return "verbose"
	default:
		return "unknown"
	}
}

// UnknownFormatPolicy controls how the validator reacts to an unrecognized
// "format" keyword value when format assertion is enabled.
type UnknownFormatPolicy int

// Recognized unknown-format policies.
const (
	// UnknownFormatIgnore silently accepts unknown formats. This is the
	// spec default behavior and matches most existing implementations.
	UnknownFormatIgnore UnknownFormatPolicy = iota
	// UnknownFormatWarn records an annotation noting the unknown format
	// but does not fail validation.
	UnknownFormatWarn
	// UnknownFormatError fails validation when an unknown format is
	// encountered.
	UnknownFormatError
)

// String returns a human-readable label for p.
func (p UnknownFormatPolicy) String() string {
	switch p {
	case UnknownFormatIgnore:
		return "ignore"
	case UnknownFormatWarn:
		return "warn"
	case UnknownFormatError:
		return "error"
	default:
		return "unknown"
	}
}

// RefCollisionPolicy controls behavior when two schema documents share the
// same $id within a single compiler.
type RefCollisionPolicy int

// Recognized ref-collision policies.
const (
	// RefCollisionError aborts compilation on collision (default).
	RefCollisionError RefCollisionPolicy = iota
	// RefCollisionFirstWins keeps the first-registered document and
	// silently ignores subsequent collisions.
	RefCollisionFirstWins
	// RefCollisionLastWins replaces an earlier document with the later one.
	RefCollisionLastWins
)

// String returns a human-readable label for p.
func (p RefCollisionPolicy) String() string {
	switch p {
	case RefCollisionError:
		return "error"
	case RefCollisionFirstWins:
		return "first-wins"
	case RefCollisionLastWins:
		return "last-wins"
	default:
		return "unknown"
	}
}

// Result is the structured outcome of a validation run. It is returned by
// every [*Schema] Validate-family method; the [Result.Output] helper renders
// it into one of the four spec-defined output formats.
//
// Callers should not construct a [Result] via a struct literal: future
// versions of this package may add fields and a literal-construction site
// would silently miss them. Always obtain a [*Result] from a [*Schema]
// Validate-family method.
type Result struct {
	// Valid reports whether the instance validated successfully.
	Valid bool
	// Errors is the flat list of assertion failures (Basic-format equivalent).
	Errors []ValidationError
	// Annotations is the flat list of annotations produced by passing keywords.
	Annotations []Annotation
}

// Annotation is a successful keyword annotation produced during validation.
type Annotation struct {
	// KeywordLocation is the JSON Pointer to the keyword in the schema that
	// produced the annotation (e.g. "/properties/name/title").
	KeywordLocation string
	// AbsoluteKeywordLocation is the resolved-URL form of KeywordLocation;
	// empty unless the schema was loaded from a remote URI.
	AbsoluteKeywordLocation string
	// InstanceLocation is the JSON Pointer to the instance value that the
	// annotation describes.
	InstanceLocation string
	// Keyword is the bare keyword name (e.g. "title").
	Keyword string
	// Value is the annotation payload (the keyword's value, by default).
	Value any
}

// ValidationError represents a single assertion failure surfaced by the
// validator. Failures from compound applicators (anyOf, oneOf, $ref, ...)
// expose their nested causes via [ValidationError.Causes]; the Go 1.20 multi-
// error [errors.Unwrap] convention is honored so that errors.Is / errors.As
// can walk the cause chain.
type ValidationError struct {
	// KeywordLocation is the JSON Pointer to the failing keyword in the
	// schema (e.g. "/properties/name/minLength").
	KeywordLocation string
	// AbsoluteKeywordLocation is the resolved-URL form of KeywordLocation;
	// empty unless the schema was loaded from a remote URI.
	AbsoluteKeywordLocation string
	// InstanceLocation is the JSON Pointer to the failing value in the
	// instance (e.g. "/name").
	InstanceLocation string
	// Keyword is the bare keyword name that triggered the failure
	// (e.g. "minLength"). This is the stable, machine-readable
	// classification of the error.
	Keyword string
	// Message is a human-readable description of the failure.
	Message string
	// Causes carries nested failures from compound applicators; it is empty
	// for leaf assertion failures.
	Causes []ValidationError
	// Cause carries an optional underlying typed error (e.g. a
	// [*FormatError] surfaced by the format assertion). Walked by
	// [errors.As] via [ValidationError.Unwrap].
	Cause error
}

// Error returns a human-readable, single-line summary of the failure suitable
// for log lines. Programmatic callers should switch on
// [ValidationError.Keyword] rather than parse the message text.
func (e *ValidationError) Error() string {
	var b strings.Builder
	b.WriteString("jsonschema: ")
	if e.Keyword != "" {
		fmt.Fprintf(&b, "%s: ", e.Keyword)
	}
	if e.Message != "" {
		b.WriteString(e.Message)
	} else {
		b.WriteString("validation failed")
	}
	if e.InstanceLocation != "" {
		fmt.Fprintf(&b, " (instance: %s)", e.InstanceLocation)
	}
	if e.KeywordLocation != "" {
		fmt.Fprintf(&b, " (schema: %s)", e.KeywordLocation)
	}
	return b.String()
}

// Is reports whether target is the [ErrValidation] sentinel. Only the
// zero-value [*ValidationError] (i.e. [ErrValidation]) matches; another
// concrete [*ValidationError] does not match unrelated keyword failures via
// [errors.Is]. Use [errors.As] to extract the typed error and inspect
// [ValidationError.Keyword] for stable classification.
func (e *ValidationError) Is(target error) bool {
	tgt, ok := target.(*ValidationError)
	if !ok || tgt == nil {
		return false
	}
	// Only the zero-value sentinel matches; non-zero targets are unrelated
	// instances. Use errors.As to inspect a concrete error.
	return tgt.KeywordLocation == "" && tgt.AbsoluteKeywordLocation == "" &&
		tgt.InstanceLocation == "" && tgt.Keyword == "" && tgt.Message == "" &&
		len(tgt.Causes) == 0 && tgt.Cause == nil
}

// Unwrap returns the nested causes for use with Go 1.20+ multi-error
// errors.Is / errors.As. Returns nil when there are no causes so that the
// stdlib treats the error as a leaf. When [ValidationError.Cause] is set it
// is included alongside any nested [Causes].
func (e *ValidationError) Unwrap() []error {
	if len(e.Causes) == 0 && e.Cause == nil {
		return nil
	}
	out := make([]error, 0, len(e.Causes)+1)
	for i := range e.Causes {
		out = append(out, &e.Causes[i])
	}
	if e.Cause != nil {
		out = append(out, e.Cause)
	}
	return out
}
