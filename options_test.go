package jsonschema

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

// TestWithMaxInstanceSize covers the size-cap pre-decode reject path:
// instances larger than the configured cap surface ErrInstanceTooLarge
// before any JSON parsing happens.
func TestWithMaxInstanceSize(t *testing.T) {
	s := MustCompile([]byte(`{"type":"string"}`))

	// Small instance fits under the cap.
	if _, err := s.Validate([]byte(`"hi"`), WithMaxInstanceSize(10)); err != nil {
		t.Errorf("small instance under cap: %v", err)
	}

	// Large instance exceeds the cap.
	big := []byte(`"` + strings.Repeat("x", 100) + `"`)
	_, err := s.Validate(big, WithMaxInstanceSize(10))
	if err == nil {
		t.Fatal("expected error for oversize instance")
	}
	if !errors.Is(err, ErrInstanceTooLarge) {
		t.Errorf("err = %v, want errors.Is(_, ErrInstanceTooLarge)", err)
	}
}

// TestWithMaxDocumentSizeAlias confirms the sister-package alias writes to
// the same underlying field as [WithMaxInstanceSize].
func TestWithMaxDocumentSizeAlias(t *testing.T) {
	s := MustCompile([]byte(`{"type":"string"}`))
	big := []byte(`"` + strings.Repeat("x", 100) + `"`)
	_, err := s.Validate(big, WithMaxDocumentSize(10))
	if err == nil {
		t.Fatal("expected error for oversize instance")
	}
	if !errors.Is(err, ErrInstanceTooLarge) {
		t.Errorf("err = %v, want errors.Is(_, ErrInstanceTooLarge)", err)
	}
}

// TestWithMaxDepthAlias confirms the [WithMaxDepth] alias is wired to the
// same field as [WithMaxValidationDepth].
func TestWithMaxDepthAlias(t *testing.T) {
	s, err := Compile([]byte(`{"$ref":"#"}`))
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	res, err := s.Validate([]byte(`null`), WithMaxDepth(5))
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	if res.Valid {
		t.Fatal("expected validation failure for deep recursion via alias")
	}
}

// TestWithLoaderTrace confirms each successful loader fetch emits one line
// to the writer. The test schema uses a $ref to drive a loader call, then
// inspects the trace buffer.
func TestWithLoaderTrace(t *testing.T) {
	var buf bytes.Buffer
	loader := MapLoader{
		"https://example.com/types": []byte(`{"$id":"https://example.com/types","$defs":{"name":{"type":"string"}}}`),
	}
	src := `{"$ref":"https://example.com/types#/$defs/name"}`
	c := NewCompiler(WithLoader(loader), WithLoaderTrace(&buf))
	if _, err := c.Compile([]byte(src)); err != nil {
		t.Fatalf("Compile: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "https://example.com/types") {
		t.Errorf("expected trace to contain target URI; got: %q", out)
	}
	// Trace lines end in \n; at least one full line is present.
	if !strings.Contains(out, "\n") {
		t.Errorf("expected newline-terminated trace line; got: %q", out)
	}
}

// TestWithRefCollisionPolicyError exercises the duplicate $id detection.
// A single document where two distinct subschemas declare the same $id
// must surface a *CompileError.
func TestWithRefCollisionPolicyError(t *testing.T) {
	src := []byte(`{
		"$id":"https://example.com/root",
		"$defs":{
			"a":{"$id":"https://example.com/dup","type":"string"},
			"b":{"$id":"https://example.com/dup","type":"integer"}
		}
	}`)
	_, err := Compile(src, WithRefCollisionPolicy(RefCollisionError))
	if err == nil {
		t.Fatal("expected duplicate $id to error")
	}
	var ce *CompileError
	if !errors.As(err, &ce) {
		t.Errorf("err type = %T, want *CompileError", err)
	}
	if ce != nil && !strings.Contains(ce.Message, "duplicate") {
		t.Errorf("CompileError.Message = %q, want a duplicate-$id message", ce.Message)
	}
}

// TestWithReadOnlyAndWriteOnlyDirection covers the direction-aware required
// gating. A property marked `readOnly: true` should not be enforced under
// WithWriteOnly; one marked `writeOnly: true` should not be enforced under
// WithReadOnly. Without either option, both are still required.
func TestWithReadOnlyAndWriteOnlyDirection(t *testing.T) {
	src := []byte(`{
		"type":"object",
		"properties":{
			"id":{"type":"string","readOnly":true},
			"password":{"type":"string","writeOnly":true},
			"name":{"type":"string"}
		},
		"required":["id","password","name"]
	}`)
	s := MustCompile(src)

	// Default mode: both id and password are required → instance missing
	// either fails.
	res, _ := s.Validate([]byte(`{"name":"alice","password":"x"}`))
	if res.Valid {
		t.Errorf("default mode: missing required id should fail; got valid")
	}
	res, _ = s.Validate([]byte(`{"name":"alice","id":"x"}`))
	if res.Valid {
		t.Errorf("default mode: missing required password should fail; got valid")
	}

	// WithWriteOnly: caller is submitting input. The readOnly id field is
	// not required (since output-only fields shouldn't appear in input),
	// but writeOnly password remains required.
	res, _ = s.Validate([]byte(`{"name":"alice","password":"x"}`), WithWriteOnly(true))
	if !res.Valid {
		t.Errorf("WithWriteOnly: id (readOnly) should not be required; errors=%v", res.Errors)
	}
	res, _ = s.Validate([]byte(`{"name":"alice","id":"x"}`), WithWriteOnly(true))
	if res.Valid {
		t.Errorf("WithWriteOnly: password (writeOnly) should still be required; got valid")
	}

	// WithReadOnly: caller is producing output. The writeOnly password is
	// not required, but id remains required.
	res, _ = s.Validate([]byte(`{"name":"alice","id":"x"}`), WithReadOnly(true))
	if !res.Valid {
		t.Errorf("WithReadOnly: password (writeOnly) should not be required; errors=%v", res.Errors)
	}
	res, _ = s.Validate([]byte(`{"name":"alice","password":"x"}`), WithReadOnly(true))
	if res.Valid {
		t.Errorf("WithReadOnly: id (readOnly) should still be required; got valid")
	}
}

// TestWithGenerateOrderedProperties_True confirms that with ordering enabled
// (the default), the generated schema's properties slot is rendered in Go
// declaration order. We assert by comparing the byte order of the property
// names in the marshaled output.
func TestWithGenerateOrderedProperties_True(t *testing.T) {
	type item struct {
		Charlie string `json:"charlie"`
		Alpha   string `json:"alpha"`
		Bravo   string `json:"bravo"`
	}
	data, err := GenerateBytes(item{}, WithGenerateOrderedProperties(true))
	if err != nil {
		t.Fatalf("GenerateBytes: %v", err)
	}
	idxC := bytes.Index(data, []byte(`"charlie"`))
	idxA := bytes.Index(data, []byte(`"alpha"`))
	idxB := bytes.Index(data, []byte(`"bravo"`))
	if idxC < 0 || idxA < 0 || idxB < 0 {
		t.Fatalf("missing property in output: %s", data)
	}
	if !(idxC < idxA && idxA < idxB) {
		t.Errorf("ordered properties not preserved: charlie@%d alpha@%d bravo@%d\n%s",
			idxC, idxA, idxB, data)
	}
}

// TestWithGenerateOrderedProperties_False confirms ordering disabled emits
// a plain map[string]any so callers downstream do not depend on a stable
// key order. We do not assert the actual order (it is map-iteration
// dependent); we assert only that all three properties round-trip through
// json.Unmarshal cleanly.
func TestWithGenerateOrderedProperties_False(t *testing.T) {
	type item struct {
		Charlie string `json:"charlie"`
		Alpha   string `json:"alpha"`
		Bravo   string `json:"bravo"`
	}
	data, err := GenerateBytes(item{}, WithGenerateOrderedProperties(false))
	if err != nil {
		t.Fatalf("GenerateBytes: %v", err)
	}
	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("Unmarshal: %v\n%s", err, data)
	}
	props, ok := doc["properties"].(map[string]any)
	if !ok {
		t.Fatalf("properties slot missing or wrong shape: %s", data)
	}
	for _, k := range []string{"charlie", "alpha", "bravo"} {
		if _, ok := props[k]; !ok {
			t.Errorf("property %q missing from generated output", k)
		}
	}
}

// TestWithStrict confirms the zero-arg [WithStrict] alias rejects schemas
// containing unknown keywords at compile time.
func TestWithStrict(t *testing.T) {
	src := []byte(`{"madeUpKeyword":42}`)
	if _, err := Compile(src); err != nil {
		t.Errorf("baseline (non-strict) compile: %v", err)
	}
	if _, err := Compile(src, WithStrict()); err == nil {
		t.Error("expected error in WithStrict mode")
	}
}

// TestMustValidateTo covers both branches of the MustValidateTo helper:
// success returns the typed value; failure panics.
func TestMustValidateTo(t *testing.T) {
	type item struct {
		Name string `json:"name"`
	}
	s := MustCompile([]byte(`{"type":"object","required":["name"],"properties":{"name":{"type":"string"}}}`))

	got := MustValidateTo[item](s, []byte(`{"name":"alice"}`))
	if got.Name != "alice" {
		t.Errorf("got %+v, want Name=alice", got)
	}

	// Bad instance must panic.
	defer func() {
		if r := recover(); r == nil {
			t.Error("MustValidateTo did not panic on validation failure")
		}
	}()
	_ = MustValidateTo[item](s, []byte(`{}`))
}

// TestCompilerMustCompileURL covers the panic-on-error variant of
// CompileURL on a [*Compiler] receiver.
func TestCompilerMustCompileURL(t *testing.T) {
	c := NewCompiler(WithLoader(MapLoader{
		"https://example.com/a": []byte(`{"type":"string"}`),
	}))
	s := c.MustCompileURL("https://example.com/a")
	if s == nil {
		t.Fatal("nil schema")
	}
	defer func() {
		if r := recover(); r == nil {
			t.Error("MustCompileURL did not panic on missing URI")
		}
	}()
	_ = c.MustCompileURL("https://example.com/missing")
}

// TestCompilerMustCompileValue covers the panic-on-error variant of
// CompileValue on a [*Compiler] receiver.
func TestCompilerMustCompileValue(t *testing.T) {
	c := NewCompiler()
	s := c.MustCompileValue(map[string]any{"type": "string"})
	if s == nil {
		t.Fatal("nil schema")
	}
	defer func() {
		if r := recover(); r == nil {
			t.Error("MustCompileValue did not panic on bad input")
		}
	}()
	_ = c.MustCompileValue(map[string]any{"minLength": "three"})
}

// TestPackageMustCompileURL covers the panic-on-error package-level
// MustCompileURL helper.
func TestPackageMustCompileURL(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("MustCompileURL did not panic on missing URI")
		}
	}()
	_ = MustCompileURL("https://example.com/missing", WithLoader(MapLoader{}))
}

// TestPackageMustCompileValue covers the panic-on-error package-level
// MustCompileValue helper.
func TestPackageMustCompileValue(t *testing.T) {
	s := MustCompileValue(map[string]any{"type": "string"})
	if s == nil {
		t.Fatal("nil schema")
	}
	defer func() {
		if r := recover(); r == nil {
			t.Error("MustCompileValue did not panic")
		}
	}()
	_ = MustCompileValue(map[string]any{"minLength": "three"})
}

// TestFormatErrorReachableViaValidationCause verifies that a format
// assertion failure surfaces the underlying *FormatError on the
// ValidationError.Cause field, reachable via errors.As.
func TestFormatErrorReachableViaValidationCause(t *testing.T) {
	s := MustCompile([]byte(`{"format":"uuid"}`))
	res, err := s.Validate([]byte(`"not-a-uuid"`), WithFormatAssertion(true))
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if res.Valid {
		t.Fatal("expected validation failure under WithFormatAssertion")
	}
	// Walk the first error's cause chain via errors.As.
	if len(res.Errors) == 0 {
		t.Fatal("no errors collected")
	}
	ve := &res.Errors[0]
	var fe *FormatError
	if !errors.As(ve, &fe) {
		t.Fatalf("errors.As did not reach *FormatError; ve.Cause=%#v", ve.Cause)
	}
	if fe.Format != "uuid" {
		t.Errorf("FormatError.Format = %q, want %q", fe.Format, "uuid")
	}
}

// TestValidationErrorIsNotOverbroad confirms the post-fix behavior of
// ValidationError.Is: only the zero-value sentinel ErrValidation matches;
// a non-zero exemplar must not match an unrelated keyword failure.
// TestWithMaxKeyCount confirms the per-object key cap surfaces an error
// keyed "$maxKeyCount" once the cap is exceeded, and that an instance
// at the cap boundary still validates cleanly.
func TestWithMaxKeyCount(t *testing.T) {
	s := MustCompile([]byte(`{"type":"object","additionalProperties":true}`))

	// At cap (3 keys) → valid.
	res, err := s.Validate([]byte(`{"a":1,"b":2,"c":3}`), WithMaxKeyCount(3))
	if err != nil {
		t.Fatalf("Validate at cap: %v", err)
	}
	if !res.Valid {
		t.Fatalf("expected valid at cap; got errors=%v", res.Errors)
	}

	// Over cap (4 keys) → fails with $maxKeyCount.
	res, err = s.Validate([]byte(`{"a":1,"b":2,"c":3,"d":4}`), WithMaxKeyCount(3))
	if err != nil {
		t.Fatalf("Validate over cap: %v", err)
	}
	if res.Valid {
		t.Fatal("expected invalid when over cap")
	}
	found := false
	for _, ve := range res.Errors {
		if ve.Keyword == "$maxKeyCount" {
			found = true
			if !errors.Is(&ve, ErrMaxKeyCount) {
				t.Errorf("error does not match ErrMaxKeyCount: %v", ve)
			}
			break
		}
	}
	if !found {
		t.Errorf("expected a $maxKeyCount error; got: %v", res.Errors)
	}
}

// TestWithWarningSink confirms unknown-format warnings under
// UnknownFormatWarn are written to the configured sink, deduplicated
// within a single Validate call.
func TestWithWarningSink(t *testing.T) {
	src := []byte(`{
		"type":"object",
		"properties":{
			"a":{"type":"string","format":"made-up"},
			"b":{"type":"string","format":"made-up"},
			"c":{"type":"string","format":"also-bogus"}
		}
	}`)
	s := MustCompile(src)
	var buf bytes.Buffer
	res, err := s.Validate(
		[]byte(`{"a":"x","b":"y","c":"z"}`),
		WithFormatAssertion(true),
		WithUnknownFormat(UnknownFormatWarn),
		WithWarningSink(&buf),
	)
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if !res.Valid {
		t.Errorf("expected valid (unknown formats are warn-only); got errors=%v", res.Errors)
	}
	out := buf.String()
	if !strings.Contains(out, "made-up") {
		t.Errorf("expected warning for 'made-up' format; got: %q", out)
	}
	if !strings.Contains(out, "also-bogus") {
		t.Errorf("expected warning for 'also-bogus' format; got: %q", out)
	}
	// Dedup: 'made-up' should appear only once even though two properties
	// reference it.
	if got := strings.Count(out, "made-up"); got != 1 {
		t.Errorf("'made-up' warning emitted %d times, want 1; got: %q", got, out)
	}
}

func TestValidationErrorIsNotOverbroad(t *testing.T) {
	maxLengthErr := &ValidationError{Keyword: "maxLength", Message: "too long"}
	// errors.Is against ErrValidation: must succeed.
	if !errors.Is(maxLengthErr, ErrValidation) {
		t.Error("errors.Is(_, ErrValidation) should be true for any *ValidationError")
	}
	// errors.Is against an unrelated keyword exemplar: must NOT match.
	probe := &ValidationError{Keyword: "minLength"}
	if errors.Is(maxLengthErr, probe) {
		t.Error("errors.Is over-broad: matched unrelated keyword exemplar")
	}
	// Empty exemplar is the sentinel and must match (alias for
	// ErrValidation).
	zero := &ValidationError{}
	if !errors.Is(maxLengthErr, zero) {
		t.Error("errors.Is should match the zero-value sentinel")
	}
}

// TestWithCustomFormatExercised confirms a custom format validator is
// invoked under WithFormatAssertion(true).
func TestWithCustomFormatExercised(t *testing.T) {
	calls := 0
	custom := func(_ string) error {
		calls++
		return errors.New("rejected")
	}
	s := MustCompile([]byte(`{"format":"x-custom"}`))
	res, err := s.Validate(
		[]byte(`"x"`),
		WithFormatAssertion(true),
		WithCustomFormat("x-custom", custom),
	)
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if res.Valid {
		t.Errorf("expected invalid; calls=%d", calls)
	}
	if calls != 1 {
		t.Errorf("custom called %d times, want 1", calls)
	}
}

// TestWithMaxErrorsCapApplied confirms WithMaxErrors caps the number of
// errors collected.
func TestWithMaxErrorsCapApplied(t *testing.T) {
	src := []byte(`{"type":"object","required":["a","b","c","d","e"]}`)
	s := MustCompile(src)
	res, err := s.Validate([]byte(`{}`), WithMaxErrors(2))
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if res.Valid {
		t.Fatal("expected invalid")
	}
	if len(res.Errors) > 2 {
		t.Errorf("WithMaxErrors(2): got %d errors, want ≤ 2", len(res.Errors))
	}
}

// TestWithCollectAnnotationsFalse confirms disabling annotation collection
// drops them from Result.Annotations.
func TestWithCollectAnnotationsFalse(t *testing.T) {
	s := MustCompile([]byte(`{"title":"Person","type":"object","properties":{"name":{"type":"string","title":"Name"}}}`))
	resOn, err := s.Validate([]byte(`{"name":"alice"}`), WithCollectAnnotations(true))
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if len(resOn.Annotations) == 0 {
		t.Errorf("WithCollectAnnotations(true): got 0 annotations, expected some")
	}
	resOff, err := s.Validate([]byte(`{"name":"alice"}`), WithCollectAnnotations(false))
	if err != nil {
		t.Fatalf("Validate (off): %v", err)
	}
	if len(resOff.Annotations) != 0 {
		t.Errorf("WithCollectAnnotations(false): got %d annotations, want 0", len(resOff.Annotations))
	}
}
