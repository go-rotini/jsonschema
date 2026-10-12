package jsonschema

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// CompileError is returned when a schema document is malformed or violates a
// keyword's value constraints (e.g. minLength is a string instead of a
// non-negative integer). An unresolvable $ref is reported as a [*RefError]
// instead.
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
// ErrRef, ErrLoader, ErrFormat) match any error of their concrete type via
// [errors.Is]; the others mark specific failure conditions.
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
	// It is the Cause of the [*FormatError] held in [ValidationError.Cause].
	ErrUnknownFormat = errors.New("jsonschema: unknown format")
	// ErrRefCycle is reserved for a $ref cycle the compiler cannot resolve.
	// No function returns it today: every cycle becomes a lazy edge that
	// validation follows, bounded by [WithMaxRefDepth].
	ErrRefCycle = errors.New("jsonschema: ref cycle detected")
	// ErrMaxRefDepth indicates validation nested more than [WithMaxRefDepth]
	// $ref, $dynamicRef or $recursiveRef hops. Its text is used as the
	// Message of the resulting [ValidationError]; it is not set as the
	// Cause, so [errors.Is] does not match it.
	ErrMaxRefDepth = errors.New("jsonschema: max ref depth exceeded")
	// ErrMaxValidationDepth indicates subschema evaluation nested deeper
	// than [WithMaxValidationDepth]. It is the Cause of the resulting
	// [ValidationError].
	ErrMaxValidationDepth = errors.New("jsonschema: max validation depth exceeded")
	// ErrMaxKeyCount indicates an object instance had more keys than the
	// [WithMaxKeyCount] cap allows. It is the Cause of the resulting
	// [ValidationError].
	ErrMaxKeyCount = errors.New("jsonschema: max key count exceeded")
	// ErrInstanceTooLarge indicates an instance document larger than
	// [WithMaxInstanceSize] was rejected before parsing.
	ErrInstanceTooLarge = errors.New("jsonschema: instance exceeds size limit")
	// ErrLoaderRejected indicates a [Loader] declined a URI: it does not
	// handle the scheme or does not serve that resource (e.g. the
	// [DefaultLoader] declining an https:// $ref, since it serves only the
	// embedded meta-schemas).
	ErrLoaderRejected = errors.New("jsonschema: loader rejected URI scheme")
	// ErrSchemaNotCompiled indicates a nil [*Schema] or [*Compiler] (or a
	// nil generator in a subpackage) was used, including a nil schema passed
	// to [ValidateTo] or the multi-format Validate functions.
	ErrSchemaNotCompiled = errors.New("jsonschema: schema not compiled")
	// ErrValidationFailed is returned by [ValidateTo] and
	// [Schema.ValidateAndUnmarshal] when the instance was rejected without
	// a structured [*ValidationError] to return.
	ErrValidationFailed = errors.New("jsonschema: validation failed")
	// ErrNilReader indicates a nil [io.Reader] was passed to
	// [*Schema.ValidateReader].
	ErrNilReader = errors.New("jsonschema: nil reader")
	// ErrUnsupportedSchemaShape is reserved for a schema slot holding
	// neither a JSON object nor a boolean. No function returns it today.
	ErrUnsupportedSchemaShape = errors.New("jsonschema: unsupported schema shape")
)

// RenderError produces a human-readable, multi-line error string.
//
// When err wraps a [*ValidationError], the output has one block per leaf
// failure in its cause tree, giving the message, keyword, schema location
// and instance location, plus — when schemaSrc / instanceSrc are non-empty
// JSON and the location resolves in them — the offending source line with
// a caret. When err wraps a [*CompileError], only the schema side is
// rendered, followed by its cause. Any other error renders as err.Error().
//
// Pass true for color to add ANSI escapes (red error labels and caret,
// bold snippet headers). Color is off by default.
//
// Do not parse the returned string; use [errors.Is] / [errors.As] with the
// typed errors and switch on [ValidationError.Keyword].
func RenderError(schemaSrc, instanceSrc []byte, err error, color ...bool) string {
	if err == nil {
		return ""
	}
	useColor := len(color) > 0 && color[0]

	if ve, ok := errors.AsType[*ValidationError](err); ok {
		return renderValidationError(schemaSrc, instanceSrc, ve, useColor)
	}
	if ce, ok := errors.AsType[*CompileError](err); ok {
		return renderCompileError(schemaSrc, ce, useColor)
	}
	return err.Error()
}

// renderValidationError emits the structured form for a *ValidationError.
// Leaf causes (no nested Causes) produce a block; non-leaf nodes recurse so
// every leaf in the cause tree appears exactly once.
func renderValidationError(schemaSrc, instanceSrc []byte, ve *ValidationError, color bool) string {
	var buf bytes.Buffer
	leaves := collectLeafCauses(ve)
	for i, leaf := range leaves {
		if i > 0 {
			buf.WriteByte('\n')
		}
		writeValidationBlock(&buf, schemaSrc, instanceSrc, leaf, color)
	}
	return buf.String()
}

// collectLeafCauses returns every leaf ValidationError in the cause tree
// rooted at ve. A "leaf" is a node with no nested Causes.
func collectLeafCauses(ve *ValidationError) []*ValidationError {
	if ve == nil {
		return nil
	}
	if len(ve.Causes) == 0 {
		return []*ValidationError{ve}
	}
	var out []*ValidationError
	for i := range ve.Causes {
		out = append(out, collectLeafCauses(&ve.Causes[i])...)
	}
	return out
}

func writeValidationBlock(buf *bytes.Buffer, schemaSrc, instanceSrc []byte, ve *ValidationError, color bool) {
	red := func(s string) string {
		if color {
			return "\x1b[31m" + s + "\x1b[0m"
		}
		return s
	}
	bold := func(s string) string {
		if color {
			return "\x1b[1m" + s + "\x1b[0m"
		}
		return s
	}

	fmt.Fprintf(buf, "%s %s\n", red("error:"), ve.Message)
	if ve.Keyword != "" {
		fmt.Fprintf(buf, "  keyword: %s\n", ve.Keyword)
	}
	if ve.KeywordLocation != "" {
		fmt.Fprintf(buf, "  schema:   %s\n", ve.KeywordLocation)
	}
	if ve.InstanceLocation != "" {
		fmt.Fprintf(buf, "  instance: %s\n", ve.InstanceLocation)
	}

	if len(schemaSrc) > 0 && ve.KeywordLocation != "" {
		writeSourceSnippet(buf, "schema", schemaSrc, ve.KeywordLocation, color, bold)
	}
	if len(instanceSrc) > 0 && ve.InstanceLocation != "" {
		writeSourceSnippet(buf, "instance", instanceSrc, ve.InstanceLocation, color, bold)
	}
}

// renderCompileError emits the structured form for a *CompileError. Only
// the schema-side snippet is meaningful here.
func renderCompileError(schemaSrc []byte, ce *CompileError, color bool) string {
	red := func(s string) string {
		if color {
			return "\x1b[31m" + s + "\x1b[0m"
		}
		return s
	}
	bold := func(s string) string {
		if color {
			return "\x1b[1m" + s + "\x1b[0m"
		}
		return s
	}
	var buf bytes.Buffer
	fmt.Fprintf(&buf, "%s %s\n", red("compile error:"), ce.Message)
	if ce.KeywordLocation != "" {
		fmt.Fprintf(&buf, "  schema: %s\n", ce.KeywordLocation)
	}
	if len(schemaSrc) > 0 && ce.KeywordLocation != "" {
		writeSourceSnippet(&buf, "schema", schemaSrc, ce.KeywordLocation, color, bold)
	}
	if ce.Cause != nil {
		fmt.Fprintf(&buf, "  cause: %s\n", ce.Cause.Error())
	}
	return buf.String()
}

// writeSourceSnippet attempts to resolve pointer against src and emit a
// "label (line N): <line text>\n  <caret>\n" snippet. Silently returns when
// resolution fails (best-effort; the structured block above carries the
// pointer text either way).
func writeSourceSnippet(buf *bytes.Buffer, label string, src []byte, pointer string, color bool, bold func(string) string) {
	off, ok := jsonPointerByteOffset(src, pointer)
	if !ok {
		return
	}
	line, col := byteOffsetToLineCol(src, off)
	if line == 0 {
		return
	}
	lineText := extractLine(src, line)
	header := fmt.Sprintf("  %s (line %d):", label, line)
	fmt.Fprintln(buf, bold(header))
	fmt.Fprintf(buf, "    %s\n", lineText)
	caret := "^"
	if color {
		caret = "\x1b[31m^\x1b[0m"
	}
	if col >= 1 {
		fmt.Fprintf(buf, "    %s%s\n", strings.Repeat(" ", col-1), caret)
	} else {
		fmt.Fprintf(buf, "    %s\n", caret)
	}
}

// extractLine returns the 1-based line at lineNo from src, with trailing
// CR / LF trimmed. Empty string when out of range.
func extractLine(src []byte, lineNo int) string {
	if lineNo <= 0 {
		return ""
	}
	cur := 1
	start := 0
	for i, b := range src {
		if b == '\n' {
			if cur == lineNo {
				return strings.TrimRight(string(src[start:i]), "\r")
			}
			cur++
			start = i + 1
		}
	}
	if cur == lineNo && start <= len(src) {
		return strings.TrimRight(string(src[start:]), "\r\n")
	}
	return ""
}

// byteOffsetToLineCol returns the 1-based line and column for offset off in
// src. (0, 0) when off is out of range.
func byteOffsetToLineCol(src []byte, off int) (int, int) {
	if off < 0 || off > len(src) {
		return 0, 0
	}
	line := 1
	col := 1
	for i := range off {
		if i >= len(src) {
			break
		}
		if src[i] == '\n' {
			line++
			col = 1
			continue
		}
		col++
	}
	return line, col
}

// jsonPointerByteOffset resolves an RFC 6901 JSON pointer against src and
// returns the byte offset where the pointed-at value begins. Returns false
// if src is not valid JSON, or the pointer does not address a value in src.
//
// Pointers may use either bare "/properties/name" form or fragment form
// "#/properties/name"; an empty pointer or "#" returns offset 0 for the
// document root (best-effort — the caret will land at the document head).
func jsonPointerByteOffset(src []byte, pointer string) (int, bool) {
	if len(src) == 0 {
		return 0, false
	}
	p := pointer
	p = strings.TrimPrefix(p, "#")
	if p == "" || p == "/" {
		// Root: skip leading whitespace, point at first non-ws byte.
		for i, b := range src {
			if !isJSONWS(b) {
				return i, true
			}
		}
		return 0, true
	}
	if !strings.HasPrefix(p, "/") {
		return 0, false
	}
	tokens := strings.Split(p[1:], "/")
	for i, t := range tokens {
		tokens[i] = unescapeJSONPointerToken(t)
	}

	dec := json.NewDecoder(bytes.NewReader(src))
	dec.UseNumber()
	off, ok := walkJSONPointer(dec, tokens)
	if !ok {
		return 0, false
	}
	return off, true
}

// walkJSONPointer consumes JSON tokens from dec, following the path tokens
// in order, and returns the byte offset of the targeted value. Each
// recursion step descends into one structural token. Callers always pass
// at least one token (jsonPointerByteOffset short-circuits the empty
// pointer case before invoking this).
func walkJSONPointer(dec *json.Decoder, tokens []string) (int, bool) {
	tok, err := dec.Token()
	if err != nil {
		return 0, false
	}
	delim, isDelim := tok.(json.Delim)
	if !isDelim {
		return 0, false
	}
	switch delim {
	case json.Delim('{'):
		return walkObject(dec, tokens)
	case json.Delim('['):
		return walkArray(dec, tokens)
	default:
		return 0, false
	}
}

func walkObject(dec *json.Decoder, tokens []string) (int, bool) {
	want := tokens[0]
	rest := tokens[1:]
	for dec.More() {
		keyTok, err := dec.Token()
		if err != nil {
			return 0, false
		}
		key, ok := keyTok.(string)
		if !ok {
			return 0, false
		}
		if key == want {
			if len(rest) == 0 {
				// The next token is the value we want; capture the offset
				// just before it.
				off := int(dec.InputOffset())
				// Advance the decoder past whitespace to find the value
				// start. The decoder's InputOffset is just past the ":" or
				// whitespace; the next non-whitespace byte is the value.
				return off, true
			}
			return walkJSONPointer(dec, rest)
		}
		// Skip the value.
		if err := skipValue(dec); err != nil {
			return 0, false
		}
	}
	return 0, false
}

func walkArray(dec *json.Decoder, tokens []string) (int, bool) {
	idx, err := strconv.Atoi(tokens[0])
	if err != nil || idx < 0 {
		return 0, false
	}
	rest := tokens[1:]
	cur := 0
	for dec.More() {
		if cur == idx {
			if len(rest) == 0 {
				off := int(dec.InputOffset())
				return off, true
			}
			return walkJSONPointer(dec, rest)
		}
		if err := skipValue(dec); err != nil {
			return 0, false
		}
		cur++
	}
	return 0, false
}

// skipValue consumes one complete JSON value (atomic or composite) from
// dec. Used to skip over object values or array elements that don't match
// the JSON-pointer descent.
func skipValue(dec *json.Decoder) error {
	tok, err := dec.Token()
	if err != nil {
		return err
	}
	delim, ok := tok.(json.Delim)
	if !ok {
		return nil
	}
	depth := 1
	for depth > 0 {
		t, err := dec.Token()
		if err != nil {
			return err
		}
		if d, ok := t.(json.Delim); ok {
			switch d {
			case json.Delim('{'), json.Delim('['):
				depth++
			case json.Delim('}'), json.Delim(']'):
				depth--
			}
		}
	}
	_ = delim
	return nil
}

// unescapeJSONPointerToken replaces ~1 with / and ~0 with ~ per RFC 6901.
func unescapeJSONPointerToken(t string) string {
	if !strings.Contains(t, "~") {
		return t
	}
	t = strings.ReplaceAll(t, "~1", "/")
	t = strings.ReplaceAll(t, "~0", "~")
	return t
}

// isJSONWS reports whether b is a JSON whitespace byte (RFC 8259 §2).
func isJSONWS(b byte) bool {
	return b == ' ' || b == '\t' || b == '\n' || b == '\r'
}
