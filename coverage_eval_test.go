package jsonschema

// Targeted eval-helper coverage tests.

import (
	"errors"
	"testing"
)

// TestAddErrorWithCauseMaxErrorsGate covers the maxErrors-gate branch.
func TestAddErrorWithCauseMaxErrorsGate(t *testing.T) {
	src := []byte(`{
		"type":"object",
		"properties":{
			"a":{"format":"uuid"},
			"b":{"format":"uuid"},
			"c":{"format":"uuid"}
		}
	}`)
	s := MustCompile(src)
	res, err := s.Validate(
		[]byte(`{"a":"x","b":"x","c":"x"}`),
		WithFormatAssertion(true),
		WithMaxErrors(1),
	)
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if res.Valid {
		t.Fatal("expected invalid")
	}
	if len(res.Errors) > 1 {
		t.Errorf("WithMaxErrors(1): got %d errors, want 1", len(res.Errors))
	}
}

// TestAddErrorWithCauseStopOnFirstError covers the stopOnFirstError branch
// of addErrorWithCause.
func TestAddErrorWithCauseStopOnFirstError(t *testing.T) {
	src := []byte(`{
		"type":"object",
		"properties":{
			"a":{"format":"uuid"},
			"b":{"format":"uuid"}
		}
	}`)
	s := MustCompile(src)
	res, err := s.Validate(
		[]byte(`{"a":"x","b":"y"}`),
		WithFormatAssertion(true),
		WithStopOnFirstError(true),
	)
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if res.Valid {
		t.Fatal("expected invalid")
	}
	if len(res.Errors) != 1 {
		t.Errorf("WithStopOnFirstError: got %d errors, want 1", len(res.Errors))
	}
}

// TestAddCausesErrorMaxErrorsGate covers the maxErrors-gate branch in
// addCausesError. Use anyOf which goes through addCausesError when no
// branch matches.
func TestAddCausesErrorMaxErrorsGate(t *testing.T) {
	src := []byte(`{
		"type":"object",
		"properties":{
			"x":{"anyOf":[{"type":"integer"},{"type":"boolean"}]},
			"y":{"anyOf":[{"type":"integer"},{"type":"boolean"}]}
		}
	}`)
	s := MustCompile(src)
	res, err := s.Validate(
		[]byte(`{"x":"text","y":"text"}`),
		WithMaxErrors(1),
	)
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if res.Valid {
		t.Fatal("expected invalid")
	}
	if len(res.Errors) > 1 {
		t.Errorf("WithMaxErrors(1): got %d errors, want 1", len(res.Errors))
	}
}

// TestAddCausesErrorStopOnFirstError covers stopOnFirstError on
// addCausesError.
func TestAddCausesErrorStopOnFirstError(t *testing.T) {
	src := []byte(`{
		"type":"object",
		"properties":{
			"x":{"anyOf":[{"type":"integer"},{"type":"boolean"}]},
			"y":{"anyOf":[{"type":"integer"},{"type":"boolean"}]}
		}
	}`)
	s := MustCompile(src)
	res, err := s.Validate(
		[]byte(`{"x":"text","y":"text"}`),
		WithStopOnFirstError(true),
	)
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if res.Valid {
		t.Fatal("expected invalid")
	}
	if len(res.Errors) != 1 {
		t.Errorf("WithStopOnFirstError: got %d errors, want 1", len(res.Errors))
	}
}

// TestUnknownFormatErrorPolicy covers UnknownFormatError handling.
func TestUnknownFormatErrorPolicy(t *testing.T) {
	s := MustCompile([]byte(`{"format":"x-unknown"}`))
	res, err := s.Validate(
		[]byte(`"x"`),
		WithFormatAssertion(true),
		WithUnknownFormat(UnknownFormatError),
	)
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if res.Valid {
		t.Fatal("expected invalid for unknown format")
	}
	have := false
	for _, e := range res.Errors {
		if errors.Is(&e, ErrUnknownFormat) {
			have = true
		}
	}
	if !have {
		t.Errorf("expected ErrUnknownFormat in errors")
	}
}

// TestEvalCheckMaxKeyCountAlreadyFired covers the already-fired path of
// checkMaxKeyCount: when a sibling applicator triggers $maxKeyCount, the
// second applicator's call should short-circuit silently (returning false
// without appending another error).
func TestEvalCheckMaxKeyCountAlreadyFired(t *testing.T) {
	// Schema with multiple object applicators that all consult the cap.
	src := []byte(`{
		"type":"object",
		"properties":{"a":{"type":"integer"}},
		"additionalProperties":true
	}`)
	s := MustCompile(src)
	// Instance with many keys → exceeds cap.
	res, err := s.Validate(
		[]byte(`{"a":1,"b":2,"c":3,"d":4}`),
		WithMaxKeyCount(2),
	)
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if res.Valid {
		t.Fatal("expected invalid")
	}
	// Count $maxKeyCount errors — should be exactly 1 even though multiple
	// applicators may visit.
	count := 0
	for _, e := range res.Errors {
		if e.Keyword == "$maxKeyCount" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("got %d $maxKeyCount errors, want 1", count)
	}
}
