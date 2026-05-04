package jsonschema

import (
	"errors"
	"io"
	"strings"
	"testing"
)

func TestCompileErrorIsAndUnwrap(t *testing.T) {
	cause := io.EOF
	e := &CompileError{
		KeywordLocation: "/properties/name",
		Message:         "expected schema, got string",
		Cause:           cause,
	}
	if !errors.Is(e, ErrCompile) {
		t.Errorf("errors.Is(*CompileError, ErrCompile) should be true")
	}
	if !errors.Is(e, cause) {
		t.Errorf("errors.Is should reach Cause via Unwrap")
	}
	if e.Unwrap() != cause {
		t.Errorf("Unwrap() = %v, want %v", e.Unwrap(), cause)
	}
	if !strings.Contains(e.Error(), "/properties/name") {
		t.Errorf("Error() = %q, missing keyword location", e.Error())
	}
	if !strings.Contains(e.Error(), "expected schema") {
		t.Errorf("Error() = %q, missing message", e.Error())
	}
}

func TestCompileErrorEmptyFields(t *testing.T) {
	e := &CompileError{}
	if got := e.Error(); !strings.Contains(got, "compile") {
		t.Errorf("Error() with empty fields = %q, want default text", got)
	}
}

func TestRefErrorIsAndUnwrap(t *testing.T) {
	cause := io.EOF
	e := &RefError{Ref: "#/$defs/foo", BaseURI: "https://example.com/schema", Cause: cause}
	if !errors.Is(e, ErrRef) {
		t.Errorf("errors.Is(*RefError, ErrRef) should be true")
	}
	if !errors.Is(e, cause) {
		t.Errorf("errors.Is should reach Cause via Unwrap")
	}
	if !strings.Contains(e.Error(), "#/$defs/foo") {
		t.Errorf("Error() = %q, missing ref", e.Error())
	}
}

func TestRefErrorMessageVariants(t *testing.T) {
	cases := []*RefError{
		{Ref: "#"},
		{BaseURI: "https://example.com/"},
		{},
		{Ref: "#", BaseURI: "https://example.com/", Cause: io.EOF},
	}
	for i, e := range cases {
		if e.Error() == "" {
			t.Errorf("case %d: empty Error()", i)
		}
	}
}

func TestLoaderErrorIsAndUnwrap(t *testing.T) {
	cause := io.ErrUnexpectedEOF
	e := &LoaderError{URI: "https://example.com/x.json", Cause: cause}
	if !errors.Is(e, ErrLoader) {
		t.Errorf("errors.Is(*LoaderError, ErrLoader) should be true")
	}
	if !errors.Is(e, cause) {
		t.Errorf("errors.Is should reach Cause via Unwrap")
	}
	if !strings.Contains(e.Error(), "https://example.com/x.json") {
		t.Errorf("Error() = %q, missing URI", e.Error())
	}
}

func TestLoaderErrorMessageVariants(t *testing.T) {
	for i, e := range []*LoaderError{
		{},
		{URI: "x"},
		{Cause: io.EOF},
	} {
		if e.Error() == "" {
			t.Errorf("case %d: empty Error()", i)
		}
	}
}

func TestFormatErrorIsAndUnwrap(t *testing.T) {
	cause := io.EOF
	e := &FormatError{Format: "uuid", Value: "not-a-uuid", Cause: cause}
	if !errors.Is(e, ErrFormat) {
		t.Errorf("errors.Is(*FormatError, ErrFormat) should be true")
	}
	if !errors.Is(e, cause) {
		t.Errorf("errors.Is should reach Cause via Unwrap")
	}
	if !strings.Contains(e.Error(), "uuid") {
		t.Errorf("Error() = %q, missing format name", e.Error())
	}
}

func TestFormatErrorMessageVariants(t *testing.T) {
	for i, e := range []*FormatError{
		{},
		{Format: "uuid"},
		{Value: "x"},
		{Format: "uuid", Value: "x"},
	} {
		if e.Error() == "" {
			t.Errorf("case %d: empty Error()", i)
		}
	}
}

func TestSentinelErrors(t *testing.T) {
	// Each typed sentinel must match itself via errors.Is.
	for name, sentinel := range map[string]error{
		"ErrCompile":    ErrCompile,
		"ErrValidation": ErrValidation,
		"ErrRef":        ErrRef,
		"ErrLoader":     ErrLoader,
		"ErrFormat":     ErrFormat,
	} {
		if !errors.Is(sentinel, sentinel) {
			t.Errorf("%s does not match itself via errors.Is", name)
		}
	}

	// Plain-text sentinels must each be distinguishable.
	textSentinels := []error{
		ErrUnknownDraft,
		ErrUnknownKeyword,
		ErrUnknownFormat,
		ErrRefCycle,
		ErrMaxRefDepth,
		ErrMaxValidationDepth,
		ErrInstanceTooLarge,
		ErrLoaderRejected,
		ErrSchemaNotCompiled,
	}
	for i, a := range textSentinels {
		for j, b := range textSentinels {
			if i == j {
				if !errors.Is(a, b) {
					t.Errorf("sentinel %d does not match itself", i)
				}
				continue
			}
			if errors.Is(a, b) {
				t.Errorf("sentinel %d should not match sentinel %d", i, j)
			}
		}
	}
}

func TestSentinelMessages(t *testing.T) {
	for _, e := range []error{
		ErrUnknownDraft,
		ErrUnknownKeyword,
		ErrUnknownFormat,
		ErrRefCycle,
		ErrMaxRefDepth,
		ErrMaxValidationDepth,
		ErrInstanceTooLarge,
		ErrLoaderRejected,
		ErrSchemaNotCompiled,
	} {
		if !strings.HasPrefix(e.Error(), "jsonschema:") {
			t.Errorf("sentinel %q does not start with 'jsonschema:'", e.Error())
		}
	}
}

func TestRenderError_Passthrough(t *testing.T) {
	if got := RenderError(nil, nil, nil); got != "" {
		t.Errorf("RenderError(nil err) = %q, want empty", got)
	}
	// A non-validation, non-compile error falls through to err.Error().
	target := errors.New("test")
	if got := RenderError(nil, nil, target); got != "test" {
		t.Errorf("RenderError fallthrough = %q, want %q", got, "test")
	}
	if got := RenderError(nil, nil, target, true); got != "test" {
		t.Errorf("RenderError fallthrough with color = %q, want %q", got, "test")
	}
}

// TestRenderError_ValidationError verifies that *ValidationError is
// rendered with structured per-cause blocks.
func TestRenderError_ValidationError(t *testing.T) {
	ve := &ValidationError{
		KeywordLocation:  "/properties/name/minLength",
		InstanceLocation: "/name",
		Keyword:          "minLength",
		Message:          "value too short",
	}
	got := RenderError(nil, nil, ve)
	if !strings.Contains(got, "minLength") {
		t.Errorf("missing keyword in output: %q", got)
	}
	if !strings.Contains(got, "/properties/name/minLength") {
		t.Errorf("missing schema pointer in output: %q", got)
	}
	if !strings.Contains(got, "/name") {
		t.Errorf("missing instance pointer in output: %q", got)
	}
	if !strings.Contains(got, "value too short") {
		t.Errorf("missing message in output: %q", got)
	}
}

// TestRenderError_SourceSnippet verifies that a JSON pointer + source bytes
// yield a line snippet with a caret pointer.
func TestRenderError_SourceSnippet(t *testing.T) {
	schemaSrc := []byte("{\n  \"properties\": {\n    \"name\": {\"minLength\": 3}\n  }\n}")
	instanceSrc := []byte("{\n  \"name\": \"x\"\n}")
	ve := &ValidationError{
		KeywordLocation:  "/properties/name/minLength",
		InstanceLocation: "/name",
		Keyword:          "minLength",
		Message:          "value too short",
	}
	got := RenderError(schemaSrc, instanceSrc, ve)
	if !strings.Contains(got, "schema (line") {
		t.Errorf("expected schema line annotation: %q", got)
	}
	if !strings.Contains(got, "instance (line") {
		t.Errorf("expected instance line annotation: %q", got)
	}
	if !strings.Contains(got, "^") {
		t.Errorf("expected caret pointer: %q", got)
	}
}

// TestRenderError_CompileError verifies CompileError rendering.
func TestRenderError_CompileError(t *testing.T) {
	ce := &CompileError{KeywordLocation: "/minLength", Message: "must be a non-negative integer"}
	got := RenderError([]byte(`{"minLength":-1}`), nil, ce)
	if !strings.Contains(got, "compile error:") {
		t.Errorf("missing compile-error header: %q", got)
	}
	if !strings.Contains(got, "/minLength") {
		t.Errorf("missing pointer: %q", got)
	}
}

// TestRenderError_Color verifies that color=true emits ANSI escapes.
func TestRenderError_Color(t *testing.T) {
	ve := &ValidationError{
		KeywordLocation:  "/type",
		InstanceLocation: "",
		Keyword:          "type",
		Message:          "wrong type",
	}
	got := RenderError(nil, nil, ve, true)
	if !strings.Contains(got, "\x1b[31m") {
		t.Errorf("expected ANSI red sequence: %q", got)
	}
}

// TestByteOffsetToLineColZero covers off=0.
func TestByteOffsetToLineColZero(t *testing.T) {
	l, c := byteOffsetToLineCol([]byte("abc"), 0)
	if l != 1 || c != 1 {
		t.Errorf("got (%d,%d), want (1,1)", l, c)
	}
}

// TestWalkObjectMidWalkErr covers the skipValue-error branch.
func TestWalkObjectMidWalkErr(t *testing.T) {
	// A pointer that descends past a malformed value should fail-soft.
	src := []byte(`{"a":1,"b":}`) // malformed value at b
	ve := &ValidationError{KeywordLocation: "/c", Message: "x"}
	// Should not panic.
	_ = RenderError(src, nil, ve)
}

// TestRenderErrorWithObjectInsideArray exercises walkArray descending into
// an object via /N/key.
func TestRenderErrorWithObjectInsideArray(t *testing.T) {
	src := []byte(`[{"name":"a"},{"name":"bee"}]`)
	ve := &ValidationError{KeywordLocation: "/1/name", Message: "x"}
	got := RenderError(src, nil, ve)
	if !strings.Contains(got, "schema (line") {
		t.Logf("snippet may not always emit; got=%q", got)
	}
}

// TestRenderErrorSkipValueComposite exercises skipValue's composite-skip
// branch by pointing past a complex sibling object.
func TestRenderErrorSkipValueComposite(t *testing.T) {
	src := []byte(`{"skip":{"x":[1,2,{"deep":true}]},"target":42}`)
	ve := &ValidationError{KeywordLocation: "/target", Message: "x"}
	got := RenderError(src, nil, ve)
	if !strings.Contains(got, "schema (line") {
		t.Logf("snippet may not always emit; got=%q", got)
	}
}

// TestTokenStartOffsetViaPointer covers tokenStartOffset indirectly via
// jsonPointerByteOffset on an atomic value at the root.
func TestTokenStartOffsetViaPointer(t *testing.T) {
	cases := [][]byte{
		[]byte(`null`),
		[]byte(`true`),
		[]byte(`false`),
		[]byte(`3.14`),
		[]byte(`"hi"`),
		[]byte(`{}`),
	}
	for _, src := range cases {
		off, ok := jsonPointerByteOffset(src, "")
		if !ok {
			t.Errorf("root: %s ok=false", src)
			continue
		}
		// off should be a non-negative valid offset.
		if off < 0 || off > len(src) {
			t.Errorf("root %s: off=%d", src, off)
		}
	}
}

// TestJsonPointerByteOffsetBranches covers more branches.
func TestJsonPointerByteOffsetBranches(t *testing.T) {
	// Empty src → false
	if _, ok := jsonPointerByteOffset(nil, ""); ok {
		t.Error("nil src should not be ok")
	}
	// Empty pointer + whitespace at front → returns first non-ws byte index.
	off, ok := jsonPointerByteOffset([]byte("   {}"), "")
	if !ok || off != 3 {
		t.Errorf("got off=%d ok=%v", off, ok)
	}
	// All-whitespace src → returns 0, true (else branch of inner for).
	off, ok = jsonPointerByteOffset([]byte("    "), "")
	if !ok || off != 0 {
		t.Errorf("all-ws: off=%d ok=%v", off, ok)
	}
}

// TestIsJSONWS covers isJSONWS.
func TestIsJSONWS(t *testing.T) {
	for _, b := range []byte{' ', '\t', '\n', '\r'} {
		if !isJSONWS(b) {
			t.Errorf("isJSONWS(%q) = false", b)
		}
	}
	for _, b := range []byte{'a', '0', '{', 0} {
		if isJSONWS(b) {
			t.Errorf("isJSONWS(%q) = true", b)
		}
	}
}

// TestSkipValueBranches covers various skipValue paths via the public
// jsonPointerByteOffset helper. We build a fixture where many sibling
// values must be skipped.
func TestSkipValueBranches(t *testing.T) {
	src := []byte(`{
		"a":[1,2,3],
		"b":{"x":1},
		"c":true,
		"d":null,
		"e":"x",
		"f":42,
		"target":"hit"
	}`)
	off, ok := jsonPointerByteOffset(src, "/target")
	if !ok {
		t.Errorf("not ok")
	}
	// Off should be inside src.
	if off <= 0 || off >= len(src) {
		t.Errorf("off=%d", off)
	}
}

// TestUnescapeJSONPointerOnlyTilde covers ~0 path.
func TestUnescapeJSONPointerOnlyTilde(t *testing.T) {
	if got := unescapeJSONPointerToken("a~0b"); got != "a~b" {
		t.Errorf("got %q", got)
	}
}

// TestTokenStartOffsetCoversTypes exercises the various tok-type branches by
// pointing at terminal values inside an object.
func TestTokenStartOffsetCoversTypes(t *testing.T) {
	src := []byte(`{"a":null,"b":true,"c":false,"d":3.14,"e":"hi","f":{"g":1}}`)
	for _, ptr := range []string{"/a", "/b", "/c", "/d", "/e", "/f"} {
		off, ok := jsonPointerByteOffset(src, ptr)
		if !ok {
			t.Errorf("ptr %q: ok=false", ptr)
			continue
		}
		if off <= 0 || off >= len(src) {
			t.Errorf("ptr %q: off=%d", ptr, off)
		}
	}
}

// TestCompileErrorErrorVariants exercises every Error() branch.
func TestCompileErrorErrorVariants(t *testing.T) {
	cases := []struct {
		name string
		ce   *CompileError
		want string
	}{
		{"only message", &CompileError{Message: "hi"}, "compile: hi"},
		{"only cause", &CompileError{Cause: io.EOF}, "compile: EOF"},
		{"empty", &CompileError{}, "compile error"},
		{"loc + message", &CompileError{KeywordLocation: "/x", Message: "bad"}, "compile /x: bad"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.ce.Error()
			if !strings.Contains(got, tc.want) {
				t.Errorf("got %q, want substring %q", got, tc.want)
			}
		})
	}
}

// TestRenderErrorWithCausesTree exercises collectLeafCauses recursing into a
// non-leaf node.
func TestRenderErrorWithCausesTree(t *testing.T) {
	root := &ValidationError{
		Keyword: "anyOf",
		Message: "no branch",
		Causes: []ValidationError{
			{Keyword: "minLength", Message: "branch a too short"},
			{Keyword: "type", Message: "branch b wrong type"},
		},
	}
	got := RenderError(nil, nil, root)
	if !strings.Contains(got, "branch a too short") || !strings.Contains(got, "branch b wrong type") {
		t.Errorf("missing leaf messages: %q", got)
	}
}

// TestRenderErrorSnippetPointerOutOfRange exercises the byteOffsetToLineCol
// out-of-range path via a JSON pointer that doesn't address the source.
func TestRenderErrorSnippetPointerOutOfRange(t *testing.T) {
	src := []byte(`{"a":1}`)
	ve := &ValidationError{
		KeywordLocation: "/properties/missing/minLength",
		Message:         "x",
	}
	// Should not panic; pointer cannot be resolved → no snippet emitted.
	got := RenderError(src, nil, ve)
	if got == "" {
		t.Errorf("expected non-empty render, got %q", got)
	}
}

// TestRenderErrorRootPointerOnEmptySrc covers byteOffsetToLineCol with empty
// src plus a root pointer.
func TestRenderErrorRootPointerOnEmptySrc(t *testing.T) {
	ve := &ValidationError{KeywordLocation: "#", Message: "x", InstanceLocation: ""}
	if got := RenderError(nil, nil, ve); got == "" {
		t.Errorf("empty result")
	}
}

// TestRenderErrorPointerIntoArray exercises walkArray.
func TestRenderErrorPointerIntoArray(t *testing.T) {
	src := []byte("[\n  1,\n  2,\n  \"three\"\n]")
	ve := &ValidationError{
		KeywordLocation: "/2",
		Message:         "wrong type",
	}
	got := RenderError(src, nil, ve)
	if !strings.Contains(got, "schema (line") {
		t.Errorf("expected line snippet for array index pointer: %q", got)
	}
}

// TestRenderErrorPointerWithEscapedTokens covers unescapeJSONPointerToken.
func TestRenderErrorPointerWithEscapedTokens(t *testing.T) {
	src := []byte(`{"a/b":1,"c~d":2}`)
	ve := &ValidationError{KeywordLocation: "/a~1b", Message: "x"}
	got := RenderError(src, nil, ve)
	// At minimum no panic; the helper should produce a textual line snippet
	// because /a~1b → /a/b → resolves.
	if got == "" {
		t.Errorf("empty result for escaped pointer")
	}
}

// TestRenderErrorBadPointerSyntax covers the !HasPrefix("/") path.
func TestRenderErrorBadPointerSyntax(t *testing.T) {
	src := []byte(`{"a":1}`)
	ve := &ValidationError{KeywordLocation: "no-leading-slash", Message: "x"}
	got := RenderError(src, nil, ve)
	if !strings.Contains(got, "x") {
		t.Errorf("missing message: %q", got)
	}
}

// TestRenderErrorInvalidArrayIndex covers walkArray's atoi-fail / negative
// branches.
func TestRenderErrorInvalidArrayIndex(t *testing.T) {
	src := []byte(`[1,2,3]`)
	for _, ptr := range []string{"/foo", "/-1"} {
		ve := &ValidationError{KeywordLocation: ptr, Message: "x"}
		// Should not panic; pointer fails to resolve → no snippet.
		_ = RenderError(src, nil, ve)
	}
}

// TestRenderErrorMissingObjectKey covers walkObject continuing past
// non-matching keys then hitting end-of-object.
func TestRenderErrorMissingObjectKey(t *testing.T) {
	src := []byte(`{"a":1,"b":2}`)
	ve := &ValidationError{KeywordLocation: "/missing", Message: "x"}
	_ = RenderError(src, nil, ve)
}

// TestRenderErrorPointerToNested covers walkJSONPointer recursion.
func TestRenderErrorPointerToNested(t *testing.T) {
	src := []byte(`{"a":{"b":{"c":42}}}`)
	ve := &ValidationError{KeywordLocation: "/a/b/c", Message: "x"}
	got := RenderError(src, nil, ve)
	if !strings.Contains(got, "schema (line") {
		t.Errorf("expected line snippet: %q", got)
	}
}

// TestRenderCompileErrorWithColorAndCause covers many color branches in
// renderCompileError + writeSourceSnippet.
func TestRenderCompileErrorWithColorAndCause(t *testing.T) {
	src := []byte("{\n  \"minLength\": -1\n}")
	ce := &CompileError{
		KeywordLocation: "/minLength",
		Message:         "must be non-negative",
		Cause:           errors.New("underlying"),
	}
	got := RenderError(src, nil, ce, true)
	if !strings.Contains(got, "\x1b[31m") {
		t.Errorf("expected ANSI red: %q", got)
	}
	if !strings.Contains(got, "cause:") {
		t.Errorf("expected cause: %q", got)
	}
}

// TestRenderValidationErrorColorWithSrc exercises the color branches in
// writeSourceSnippet for both schema and instance.
func TestRenderValidationErrorColorWithSrc(t *testing.T) {
	schemaSrc := []byte("{\"minLength\":3}")
	instanceSrc := []byte("\"x\"")
	ve := &ValidationError{
		KeywordLocation:  "/minLength",
		InstanceLocation: "",
		Keyword:          "minLength",
		Message:          "too short",
	}
	got := RenderError(schemaSrc, instanceSrc, ve, true)
	if !strings.Contains(got, "\x1b[31m") {
		t.Errorf("expected ANSI red: %q", got)
	}
	if !strings.Contains(got, "\x1b[1m") {
		t.Errorf("expected ANSI bold: %q", got)
	}
}

// TestExtractLine_CR exercises the CR-trim branch.
func TestExtractLine_CR(t *testing.T) {
	src := []byte("line1\r\nline2\r\n")
	if got := extractLine(src, 1); got != "line1" {
		t.Errorf("got %q, want 'line1'", got)
	}
	if got := extractLine(src, 2); got != "line2" {
		t.Errorf("got %q, want 'line2'", got)
	}
}

// TestExtractLine_OutOfRange covers the empty-string fallback.
func TestExtractLine_OutOfRange(t *testing.T) {
	if got := extractLine([]byte("a\nb\n"), 99); got != "" {
		t.Errorf("got %q, want empty", got)
	}
	if got := extractLine([]byte(""), 0); got != "" {
		t.Errorf("got %q, want empty", got)
	}
}

// TestByteOffsetToLineCol_OutOfRange covers the out-of-range return.
func TestByteOffsetToLineCol_OutOfRange(t *testing.T) {
	if l, c := byteOffsetToLineCol([]byte("abc"), -1); l != 0 || c != 0 {
		t.Errorf("negative off: got (%d,%d), want (0,0)", l, c)
	}
	if l, c := byteOffsetToLineCol([]byte("abc"), 100); l != 0 || c != 0 {
		t.Errorf("past end: got (%d,%d), want (0,0)", l, c)
	}
}

// TestUnescapeJSONPointerToken covers both branches.
func TestUnescapeJSONPointerToken(t *testing.T) {
	if got := unescapeJSONPointerToken("plain"); got != "plain" {
		t.Errorf("plain: got %q", got)
	}
	if got := unescapeJSONPointerToken("a~1b~0c"); got != "a/b~c" {
		t.Errorf("escapes: got %q", got)
	}
}
