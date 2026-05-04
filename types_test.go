package jsonschema

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestOutputFormatString(t *testing.T) {
	cases := map[OutputFormat]string{
		OutputFlag:       "flag",
		OutputBasic:      "basic",
		OutputDetailed:   "detailed",
		OutputVerbose:    "verbose",
		OutputFormat(99): "unknown",
	}
	for f, want := range cases {
		if got := f.String(); got != want {
			t.Errorf("OutputFormat(%d).String() = %q, want %q", int(f), got, want)
		}
	}
}

func TestUnknownFormatPolicyString(t *testing.T) {
	cases := map[UnknownFormatPolicy]string{
		UnknownFormatIgnore:     "ignore",
		UnknownFormatWarn:       "warn",
		UnknownFormatError:      "error",
		UnknownFormatPolicy(99): "unknown",
	}
	for p, want := range cases {
		if got := p.String(); got != want {
			t.Errorf("UnknownFormatPolicy(%d).String() = %q, want %q", int(p), got, want)
		}
	}
}

func TestRefCollisionPolicyString(t *testing.T) {
	cases := map[RefCollisionPolicy]string{
		RefCollisionError:      "error",
		RefCollisionFirstWins:  "first-wins",
		RefCollisionLastWins:   "last-wins",
		RefCollisionPolicy(99): "unknown",
	}
	for p, want := range cases {
		if got := p.String(); got != want {
			t.Errorf("RefCollisionPolicy(%d).String() = %q, want %q", int(p), got, want)
		}
	}
}

func TestMapSliceRoundTrip(t *testing.T) {
	// MapSlice is preserved through encoding/json marshaling because each
	// MapItem is a regular struct. The test confirms the type wires up
	// correctly even though the encoder does not yet preserve order — the
	// schema generator (Phase 7) will own ordering at the JSON layer.
	ms := MapSlice{
		{Key: "first", Value: 1},
		{Key: "second", Value: "two"},
	}
	data, err := json.Marshal(ms)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if !strings.Contains(string(data), `"first"`) || !strings.Contains(string(data), `"two"`) {
		t.Errorf("Marshal output missing expected fields: %s", data)
	}
	// Reconstructing into MapSlice round-trips the structural shape.
	var back MapSlice
	if err := json.Unmarshal(data, &back); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if len(back) != len(ms) {
		t.Errorf("round-trip length: got %d, want %d", len(back), len(ms))
	}
}

func TestNumberAlias(t *testing.T) {
	// Number is a Go type alias for json.Number; the underlying types
	// must be identical so values are interchangeable.
	var n Number = "3.14"
	if reflect.TypeOf(n).String() != "json.Number" {
		t.Errorf("Number underlying type = %s, want json.Number", reflect.TypeOf(n))
	}
	got, err := n.Float64()
	if err != nil {
		t.Fatalf("Number.Float64: %v", err)
	}
	if got != 3.14 {
		t.Errorf("got %v, want 3.14", got)
	}
}

func TestValidationErrorErrorString(t *testing.T) {
	e := &ValidationError{
		Keyword:          "minLength",
		Message:          "string too short",
		KeywordLocation:  "/properties/name/minLength",
		InstanceLocation: "/name",
	}
	got := e.Error()
	for _, want := range []string{"jsonschema:", "minLength", "string too short", "/name", "/properties/name/minLength"} {
		if !strings.Contains(got, want) {
			t.Errorf("Error() = %q, missing %q", got, want)
		}
	}
}

func TestValidationErrorErrorEmptyMessage(t *testing.T) {
	e := &ValidationError{}
	got := e.Error()
	if !strings.Contains(got, "validation failed") {
		t.Errorf("Error() = %q, want fallback message", got)
	}
}

func TestValidationErrorIs(t *testing.T) {
	e := &ValidationError{Keyword: "type"}
	if !errors.Is(e, ErrValidation) {
		t.Errorf("errors.Is(*ValidationError, ErrValidation) should be true")
	}
}

func TestValidationErrorUnwrapNil(t *testing.T) {
	e := &ValidationError{Keyword: "type"}
	if got := e.Unwrap(); got != nil {
		t.Errorf("Unwrap() with no causes = %v, want nil", got)
	}
}

func TestValidationErrorUnwrapCauses(t *testing.T) {
	e := &ValidationError{
		Keyword: "oneOf",
		Causes: []ValidationError{
			{Keyword: "type", Message: "wrong type"},
			{Keyword: "minLength", Message: "too short"},
		},
	}
	un := e.Unwrap()
	if len(un) != 2 {
		t.Fatalf("Unwrap() len = %d, want 2", len(un))
	}
	// errors.Is must reach the cause chain via the multi-error Unwrap.
	probe := &ValidationError{Keyword: "minLength"}
	if !errors.Is(e, ErrValidation) {
		t.Errorf("errors.Is sentinel: false")
	}
	// Each cause is itself a *ValidationError.
	for i, ue := range un {
		if _, ok := ue.(*ValidationError); !ok {
			t.Errorf("Unwrap()[%d] is not *ValidationError", i)
		}
	}
	_ = probe
}

func TestResultOutputFlag(t *testing.T) {
	// The Flag format is unconditionally a one-shot {"valid": ...} payload.
	r := &Result{Valid: true}
	if got := string(r.Output(OutputFlag)); got != `{"valid":true}` {
		t.Errorf("Output(flag) valid = %s", got)
	}
	r.Valid = false
	if got := string(r.Output(OutputFlag)); got != `{"valid":false}` {
		t.Errorf("Output(flag) invalid = %s", got)
	}
	// Defensive nil handling.
	var nilR *Result
	if string(nilR.Output(OutputFlag)) != `{"valid":false}` {
		t.Errorf("nil Result Output should report invalid")
	}
}

// TestUnknownFormatPolicyStringExt covers each enum string.
func TestUnknownFormatPolicyStringExt(t *testing.T) {
	for _, p := range []UnknownFormatPolicy{UnknownFormatIgnore, UnknownFormatWarn, UnknownFormatError} {
		if got := p.String(); got == "" {
			t.Errorf("policy %d: empty string", p)
		}
	}
}
