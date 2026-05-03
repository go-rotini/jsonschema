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

func TestRenderErrorStub(t *testing.T) {
	if got := RenderError(nil, nil, nil); got != "" {
		t.Errorf("RenderError(nil err) = %q, want empty", got)
	}
	target := errors.New("test")
	if got := RenderError(nil, nil, target); got != "test" {
		t.Errorf("RenderError stub = %q, want %q", got, "test")
	}
	// Color flag is currently a no-op.
	if got := RenderError(nil, nil, target, true); got != "test" {
		t.Errorf("RenderError stub with color = %q, want %q", got, "test")
	}
}
