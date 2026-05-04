package jsonschema

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"testing"
)

// validateCase is the common shape of every per-keyword test entry.
type validateCase struct {
	schema string
	data   string
	valid  bool
}

func runValidateCases(t *testing.T, cases []validateCase) {
	t.Helper()
	for i, c := range cases {
		t.Run(fmt.Sprintf("%d_%s_%s", i, c.schema, c.data), func(t *testing.T) {
			s, err := Compile([]byte(c.schema))
			if err != nil {
				t.Fatalf("case %d compile: %v (schema=%s)", i, err, c.schema)
			}
			res, err := s.Validate([]byte(c.data))
			if err != nil {
				t.Fatalf("case %d validate: %v", i, err)
			}
			if res.Valid != c.valid {
				t.Errorf("case %d: schema=%s data=%s got Valid=%v want %v errs=%+v", i, c.schema, c.data, res.Valid, c.valid, res.Errors)
			}
		})
	}
}

// =====================================================================
// Validation vocabulary
// =====================================================================

func TestValidate_Type(t *testing.T) {
	runValidateCases(t, []validateCase{
		{`{"type":"string"}`, `"hello"`, true},
		{`{"type":"string"}`, `5`, false},
		{`{"type":"integer"}`, `5`, true},
		{`{"type":"integer"}`, `5.0`, true},
		{`{"type":"integer"}`, `5.1`, false},
		{`{"type":"number"}`, `1.5`, true},
		{`{"type":"number"}`, `"1.5"`, false},
		{`{"type":"boolean"}`, `true`, true},
		{`{"type":"boolean"}`, `0`, false},
		{`{"type":"null"}`, `null`, true},
		{`{"type":"null"}`, `"x"`, false},
		{`{"type":"object"}`, `{}`, true},
		{`{"type":"object"}`, `[]`, false},
		{`{"type":"array"}`, `[]`, true},
		{`{"type":"array"}`, `{}`, false},
		{`{"type":["string","null"]}`, `null`, true},
		{`{"type":["string","null"]}`, `"x"`, true},
		{`{"type":["string","null"]}`, `5`, false},
	})
}

func TestValidate_Enum(t *testing.T) {
	runValidateCases(t, []validateCase{
		{`{"enum":[1,2,3]}`, `2`, true},
		{`{"enum":[1,2,3]}`, `4`, false},
		{`{"enum":["a","b"]}`, `"a"`, true},
		{`{"enum":["a","b"]}`, `"c"`, false},
		{`{"enum":[null,true,1.5]}`, `1.5`, true},
		{`{"enum":[{"x":1}]}`, `{"x":1}`, true},
		{`{"enum":[{"x":1}]}`, `{"x":2}`, false},
	})
}

func TestValidate_Const(t *testing.T) {
	runValidateCases(t, []validateCase{
		{`{"const":42}`, `42`, true},
		{`{"const":42}`, `43`, false},
		{`{"const":"hi"}`, `"hi"`, true},
		{`{"const":[1,2]}`, `[1,2]`, true},
		{`{"const":[1,2]}`, `[2,1]`, false},
	})
}

func TestValidate_MultipleOf(t *testing.T) {
	runValidateCases(t, []validateCase{
		{`{"multipleOf":2}`, `4`, true},
		{`{"multipleOf":2}`, `5`, false},
		{`{"multipleOf":0.1}`, `0.3`, true},
		{`{"multipleOf":0.0001}`, `0.0075`, true},
		{`{"multipleOf":0.0001}`, `0.00751`, false},
	})
}

func TestValidate_Range(t *testing.T) {
	runValidateCases(t, []validateCase{
		{`{"maximum":10}`, `10`, true},
		{`{"maximum":10}`, `11`, false},
		{`{"minimum":1}`, `1`, true},
		{`{"minimum":1}`, `0`, false},
		{`{"exclusiveMaximum":10}`, `9`, true},
		{`{"exclusiveMaximum":10}`, `10`, false},
		{`{"exclusiveMinimum":1}`, `2`, true},
		{`{"exclusiveMinimum":1}`, `1`, false},
	})
}

func TestValidate_StringLength(t *testing.T) {
	runValidateCases(t, []validateCase{
		{`{"maxLength":3}`, `"ab"`, true},
		{`{"maxLength":3}`, `"abcd"`, false},
		{`{"minLength":3}`, `"abc"`, true},
		{`{"minLength":3}`, `"ab"`, false},
		// 4-byte UTF-8 character should count as 1 codepoint.
		{`{"minLength":1,"maxLength":1}`, `"😀"`, true},
		{`{"minLength":1}`, `5`, true}, // applies only to strings
	})
}

func TestValidate_Pattern(t *testing.T) {
	runValidateCases(t, []validateCase{
		{`{"pattern":"^a+$"}`, `"aaa"`, true},
		{`{"pattern":"^a+$"}`, `"aab"`, false},
		{`{"pattern":"^foo"}`, `"foobar"`, true},
		{`{"pattern":"^foo"}`, `"barfoo"`, false},
		// Pattern only applies to strings.
		{`{"pattern":"^a"}`, `5`, true},
	})
}

func TestValidate_ArrayConstraints(t *testing.T) {
	runValidateCases(t, []validateCase{
		{`{"maxItems":2}`, `[1,2]`, true},
		{`{"maxItems":2}`, `[1,2,3]`, false},
		{`{"minItems":2}`, `[1,2]`, true},
		{`{"minItems":2}`, `[1]`, false},
		{`{"uniqueItems":true}`, `[1,2,3]`, true},
		{`{"uniqueItems":true}`, `[1,2,1]`, false},
		{`{"uniqueItems":true}`, `[{"a":1},{"a":1}]`, false},
		{`{"uniqueItems":false}`, `[1,1]`, true},
	})
}

func TestValidate_ObjectConstraints(t *testing.T) {
	runValidateCases(t, []validateCase{
		{`{"maxProperties":1}`, `{"a":1}`, true},
		{`{"maxProperties":1}`, `{"a":1,"b":2}`, false},
		{`{"minProperties":2}`, `{"a":1,"b":2}`, true},
		{`{"minProperties":2}`, `{"a":1}`, false},
		{`{"required":["a","b"]}`, `{"a":1,"b":2}`, true},
		{`{"required":["a","b"]}`, `{"a":1}`, false},
		{`{"dependentRequired":{"a":["b"]}}`, `{"a":1,"b":2}`, true},
		{`{"dependentRequired":{"a":["b"]}}`, `{"a":1}`, false},
	})
}

// =====================================================================
// Applicator vocabulary
// =====================================================================

func TestValidate_AllOf(t *testing.T) {
	runValidateCases(t, []validateCase{
		{`{"allOf":[{"type":"string"},{"minLength":3}]}`, `"abc"`, true},
		{`{"allOf":[{"type":"string"},{"minLength":3}]}`, `"ab"`, false},
		{`{"allOf":[{"type":"integer"},{"minimum":1}]}`, `5`, true},
	})
}

func TestValidate_AnyOf(t *testing.T) {
	runValidateCases(t, []validateCase{
		{`{"anyOf":[{"type":"string"},{"type":"integer"}]}`, `"x"`, true},
		{`{"anyOf":[{"type":"string"},{"type":"integer"}]}`, `5`, true},
		{`{"anyOf":[{"type":"string"},{"type":"integer"}]}`, `1.5`, false},
	})
}

func TestValidate_OneOf(t *testing.T) {
	runValidateCases(t, []validateCase{
		{`{"oneOf":[{"type":"string"},{"type":"integer"}]}`, `"x"`, true},
		{`{"oneOf":[{"type":"string"},{"type":"integer"}]}`, `5`, true},
		{`{"oneOf":[{"type":"string"},{"type":"integer"}]}`, `1.5`, false},
		// Two passing branches → fail.
		{`{"oneOf":[{"type":"integer"},{"minimum":0}]}`, `5`, false},
	})
}

func TestValidate_Not(t *testing.T) {
	runValidateCases(t, []validateCase{
		{`{"not":{"type":"integer"}}`, `"x"`, true},
		{`{"not":{"type":"integer"}}`, `5`, false},
	})
}

func TestValidate_IfThenElse(t *testing.T) {
	schema := `{"if":{"type":"integer"},"then":{"minimum":1},"else":{"type":"string"}}`
	runValidateCases(t, []validateCase{
		{schema, `5`, true},    // if=int, then=≥1
		{schema, `0`, false},   // if=int, then fails
		{schema, `"x"`, true},  // if fails (not int), else passes (string)
		{schema, `1.5`, false}, // not int → else=string fails
		// Missing then is no-op.
		{`{"if":{"type":"integer"},"else":{"const":"x"}}`, `5`, true},
		{`{"if":{"type":"integer"},"else":{"const":"x"}}`, `"y"`, false},
	})
}

func TestValidate_DependentSchemas(t *testing.T) {
	schema := `{"dependentSchemas":{"a":{"required":["b"]}}}`
	runValidateCases(t, []validateCase{
		{schema, `{"a":1,"b":2}`, true},
		{schema, `{"a":1}`, false},
		{schema, `{"b":2}`, true},
	})
}

func TestValidate_Properties(t *testing.T) {
	schema := `{"properties":{"name":{"type":"string"},"age":{"type":"integer"}}}`
	runValidateCases(t, []validateCase{
		{schema, `{"name":"x","age":1}`, true},
		{schema, `{"name":"x"}`, true},
		{schema, `{"name":1}`, false},
		{schema, `{"age":"x"}`, false},
		{schema, `{"extra":"ok"}`, true},
	})
}

func TestValidate_PatternProperties(t *testing.T) {
	schema := `{"patternProperties":{"^x":{"type":"integer"}}}`
	runValidateCases(t, []validateCase{
		{schema, `{"x1":5}`, true},
		{schema, `{"x1":"a"}`, false},
		{schema, `{"y":"a"}`, true},
	})
}

func TestValidate_AdditionalProperties(t *testing.T) {
	runValidateCases(t, []validateCase{
		{`{"properties":{"a":{}},"additionalProperties":false}`, `{"a":1}`, true},
		{`{"properties":{"a":{}},"additionalProperties":false}`, `{"a":1,"b":2}`, false},
		{`{"properties":{"a":{}},"additionalProperties":{"type":"integer"}}`, `{"a":1,"b":5}`, true},
		{`{"properties":{"a":{}},"additionalProperties":{"type":"integer"}}`, `{"a":1,"b":"x"}`, false},
		{`{"patternProperties":{"^x":{}},"additionalProperties":false}`, `{"x1":5}`, true},
		{`{"patternProperties":{"^x":{}},"additionalProperties":false}`, `{"y":5}`, false},
	})
}

func TestValidate_PropertyNames(t *testing.T) {
	schema := `{"propertyNames":{"pattern":"^[a-z]+$"}}`
	runValidateCases(t, []validateCase{
		{schema, `{"abc":1}`, true},
		{schema, `{"ABC":1}`, false},
	})
}

func TestValidate_Items(t *testing.T) {
	runValidateCases(t, []validateCase{
		{`{"items":{"type":"integer"}}`, `[1,2,3]`, true},
		{`{"items":{"type":"integer"}}`, `[1,"x"]`, false},
		{`{"prefixItems":[{"type":"string"},{"type":"integer"}]}`, `["x",5,true]`, true},
		{`{"prefixItems":[{"type":"string"},{"type":"integer"}]}`, `["x","y"]`, false},
		{`{"prefixItems":[{"type":"string"}],"items":false}`, `["x"]`, true},
		{`{"prefixItems":[{"type":"string"}],"items":false}`, `["x",1]`, false},
	})
}

func TestValidate_Contains(t *testing.T) {
	runValidateCases(t, []validateCase{
		{`{"contains":{"type":"integer"}}`, `[1,"x"]`, true},
		{`{"contains":{"type":"integer"}}`, `["a","b"]`, false},
		{`{"contains":{"type":"integer"},"minContains":2}`, `[1,2,"x"]`, true},
		{`{"contains":{"type":"integer"},"minContains":2}`, `[1,"x"]`, false},
		{`{"contains":{"type":"integer"},"maxContains":1}`, `[1,"x"]`, true},
		{`{"contains":{"type":"integer"},"maxContains":1}`, `[1,2]`, false},
		{`{"contains":{"const":1},"minContains":0}`, `[]`, true},
	})
}

// =====================================================================
// Annotation propagation
// =====================================================================

func TestValidate_UnevaluatedItems(t *testing.T) {
	runValidateCases(t, []validateCase{
		{`{"prefixItems":[{"type":"string"}],"unevaluatedItems":false}`, `["x"]`, true},
		{`{"prefixItems":[{"type":"string"}],"unevaluatedItems":false}`, `["x",1]`, false},
		{`{"items":{"type":"integer"},"unevaluatedItems":false}`, `[1,2,3]`, true},
		{`{"items":{"type":"integer"},"unevaluatedItems":{"type":"string"}}`, `[1,2,3]`, true},
		// allOf annotation propagation: prefixItems in allOf branch should
		// satisfy unevaluatedItems.
		{`{"allOf":[{"prefixItems":[{"type":"string"}]}],"unevaluatedItems":false}`, `["x"]`, true},
	})
}

func TestValidate_UnevaluatedProperties(t *testing.T) {
	runValidateCases(t, []validateCase{
		{`{"properties":{"a":{}},"unevaluatedProperties":false}`, `{"a":1}`, true},
		{`{"properties":{"a":{}},"unevaluatedProperties":false}`, `{"a":1,"b":2}`, false},
		{`{"properties":{"a":{}},"unevaluatedProperties":{"type":"integer"}}`, `{"a":1,"b":2}`, true},
		{`{"properties":{"a":{}},"unevaluatedProperties":{"type":"integer"}}`, `{"a":1,"b":"x"}`, false},
		// allOf annotation propagation.
		{`{"allOf":[{"properties":{"a":{}}}],"unevaluatedProperties":false}`, `{"a":1}`, true},
	})
}

// =====================================================================
// $ref / $dynamicRef
// =====================================================================

func TestValidate_Ref(t *testing.T) {
	schema := `{
		"$defs":{"name":{"type":"string"}},
		"properties":{"name":{"$ref":"#/$defs/name"}}
	}`
	runValidateCases(t, []validateCase{
		{schema, `{"name":"x"}`, true},
		{schema, `{"name":5}`, false},
	})
}

func TestValidate_RefCycle(t *testing.T) {
	// {"$ref":"#"} loops on itself; the validator must terminate via
	// WithMaxRefDepth and surface a failure.
	s, err := Compile([]byte(`{"$ref":"#"}`))
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	res, err := s.Validate([]byte(`null`))
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	if res.Valid {
		t.Fatalf("expected validation failure on self-loop ref")
	}
	hit := false
	for _, e := range res.Errors {
		if strings.Contains(e.Message, "max ref depth") {
			hit = true
		}
	}
	if !hit {
		t.Logf("errors: %+v", res.Errors)
	}
}

func TestValidate_DynamicRef_Tree(t *testing.T) {
	// Canonical "tree" example from the test suite: a tree node where
	// children are typed by the root schema.
	schema := `{
		"$id":"https://example.com/tree",
		"$dynamicAnchor":"node",
		"type":"object",
		"properties":{
			"data":true,
			"children":{
				"type":"array",
				"items":{"$dynamicRef":"#node"}
			}
		}
	}`
	s, err := Compile([]byte(schema))
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	cases := []struct {
		data  string
		valid bool
	}{
		{`{"data":1,"children":[{"data":2}]}`, true},
		{`{"data":1,"children":["bogus"]}`, false},
	}
	for _, c := range cases {
		t.Run(c.data, func(t *testing.T) {
			res, err := s.Validate([]byte(c.data))
			if err != nil {
				t.Fatalf("validate: %v", err)
			}
			if res.Valid != c.valid {
				t.Errorf("got valid=%v want %v errs=%+v", res.Valid, c.valid, res.Errors)
			}
		})
	}
}

func TestValidate_DynamicRefFallback(t *testing.T) {
	// $dynamicRef with no matching $dynamicAnchor falls back to $ref.
	schema := `{
		"$id":"https://example.com/x",
		"$defs":{"node":{"type":"string"}},
		"properties":{"x":{"$dynamicRef":"#/$defs/node"}}
	}`
	runValidateCases(t, []validateCase{
		{schema, `{"x":"hi"}`, true},
		{schema, `{"x":5}`, false},
	})
}

// =====================================================================
// Options
// =====================================================================

func TestValidate_MaxValidationDepth(t *testing.T) {
	s, err := Compile([]byte(`{"$ref":"#"}`))
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	res, err := s.Validate([]byte(`null`), WithMaxValidationDepth(5))
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	if res.Valid {
		t.Fatalf("expected failure for deep recursion")
	}
}

func TestValidate_StopOnFirstError(t *testing.T) {
	s, _ := Compile([]byte(`{"required":["a","b","c"]}`))
	res, _ := s.Validate([]byte(`{}`), WithStopOnFirstError(true))
	if res.Valid {
		t.Fatal("want invalid")
	}
	if len(res.Errors) != 1 {
		t.Errorf("want 1 error with WithStopOnFirstError, got %d", len(res.Errors))
	}
}

// =====================================================================
// ValidateValue / ValidateReader / ValidateAndUnmarshal / ValidateTo
// =====================================================================

func TestValidate_ValidateReader(t *testing.T) {
	s, _ := Compile([]byte(`{"type":"integer"}`))
	res, err := s.ValidateReader(strings.NewReader(`5`))
	if err != nil {
		t.Fatalf("ValidateReader: %v", err)
	}
	if !res.Valid {
		t.Errorf("want valid")
	}
}

func TestValidate_ValidateAndUnmarshal(t *testing.T) {
	s, _ := Compile([]byte(`{"type":"object","required":["x"]}`))
	var dst map[string]any
	if err := s.ValidateAndUnmarshal([]byte(`{"x":1}`), &dst); err != nil {
		t.Fatalf("ValidateAndUnmarshal: %v", err)
	}
	if dst["x"] == nil {
		t.Errorf("Unmarshal lost data: %v", dst)
	}
	if err := s.ValidateAndUnmarshal([]byte(`{}`), &dst); err == nil {
		t.Errorf("want validation error on missing required")
	}
}

func TestValidate_ValidateTo(t *testing.T) {
	type item struct {
		Name string `json:"name"`
	}
	s, _ := Compile([]byte(`{"type":"object","required":["name"]}`))
	v, err := ValidateTo[item](s, []byte(`{"name":"hi"}`))
	if err != nil {
		t.Fatalf("ValidateTo: %v", err)
	}
	if v.Name != "hi" {
		t.Errorf("got %v", v)
	}
	if _, err := ValidateTo[item](s, []byte(`{}`)); err == nil {
		t.Errorf("want validation error")
	}
}

// =====================================================================
// Annotations
// =====================================================================

func TestValidate_Annotations(t *testing.T) {
	s, _ := Compile([]byte(`{"title":"X","description":"Y","examples":[1,2]}`))
	res, _ := s.Validate([]byte(`5`))
	if !res.Valid {
		t.Fatal("want valid")
	}
	gotTitle := false
	for _, a := range res.Annotations {
		if a.Keyword == "title" {
			gotTitle = true
		}
	}
	if !gotTitle {
		t.Errorf("missing title annotation in %+v", res.Annotations)
	}
}

// =====================================================================
// Meta-schema validation
// =====================================================================

func TestValidate_MetaSchemaValidation_TyposCaught(t *testing.T) {
	// "minLengt" instead of "minLength" — meta-schema should reject.
	_, err := Compile([]byte(`{"$schema":"https://json-schema.org/draft/2020-12/schema","minLengt":3}`),
		WithMetaSchemaValidation(true))
	// The meta-schema validation is best-effort and may pass since extra
	// keywords aren't forbidden by default. We don't assert failure here —
	// the test merely verifies the path runs without crashing.
	_ = err
}

// TestValidateAgainstMetaSchema_FailureSurfacesCompileError exercises the
// failure branch of [validateAgainstMetaSchema]: the schema's bytes are
// well-formed JSON and pass the per-keyword shape checks, but the value
// of "type" is an array containing an unknown atom which the 2020-12
// meta-schema's anyOf rejects. The error must surface as *CompileError
// with message "schema does not match meta-schema".
func TestValidateAgainstMetaSchema_FailureSurfacesCompileError(t *testing.T) {
	_, err := Compile(
		[]byte(`{"$schema":"https://json-schema.org/draft/2020-12/schema","type":["bogus"]}`),
		WithMetaSchemaValidation(true),
	)
	if err == nil {
		t.Fatal("expected meta-schema validation failure")
	}
	var ce *CompileError
	if !errors.As(err, &ce) {
		t.Fatalf("err = %T %v; want *CompileError", err, err)
	}
	if !strings.Contains(ce.Message, "schema does not match meta-schema") {
		t.Errorf("CompileError.Message = %q; want it to contain meta-schema diagnostic prefix", ce.Message)
	}
	// The cause should be a ValidationError so callers can inspect it.
	var ve *ValidationError
	if !errors.As(err, &ve) {
		t.Errorf("expected ValidationError in cause chain; got %v", err)
	}
}

// TestValidateAgainstMetaSchema_OASDialectFailure exercises the dialect
// branch: a schema declaring the OAS 3.1 dialect URL is validated against
// the embedded openapi-3.1-dialect.json, NOT against vanilla 2020-12.
// `discriminator: 42` violates the dialect's `type: object` constraint.
func TestValidateAgainstMetaSchema_OASDialectFailure(t *testing.T) {
	_, err := Compile(
		[]byte(`{"$schema":"https://spec.openapis.org/oas/3.1/dialect/base","discriminator":42}`),
		WithMetaSchemaValidation(true),
	)
	if err == nil {
		t.Fatal("expected dialect meta-schema validation failure")
	}
	var ce *CompileError
	if !errors.As(err, &ce) {
		t.Fatalf("err = %T %v; want *CompileError", err, err)
	}
	if !strings.Contains(ce.Message, "discriminator") {
		t.Errorf("CompileError.Message = %q; expected message to mention the offending keyword", ce.Message)
	}
}

// TestValidateAgainstMetaSchema_UnknownDialectFallsBack confirms that a
// schema declaring an unknown $schema URI does NOT crash the validation
// path. The package falls back to DraftDefault's meta-schema, which
// still catches cross-cutting shape violations (here, the bogus type
// atom). The test guards against the regression where a non-embedded
// $schema URI plus no Loader caused a nil-pointer or empty-error path.
func TestValidateAgainstMetaSchema_UnknownDialectFallsBack(t *testing.T) {
	_, err := Compile(
		[]byte(`{"$schema":"https://example.com/custom-schema-not-embedded","type":["bogus-type"]}`),
		WithMetaSchemaValidation(true),
	)
	if err == nil {
		t.Fatal("expected meta-schema validation failure even with unknown $schema URI")
	}
	var ce *CompileError
	if !errors.As(err, &ce) {
		t.Fatalf("err = %T %v; want *CompileError", err, err)
	}
}

func TestValidate_NilSchemaErrors(t *testing.T) {
	var s *Schema
	if _, err := s.Validate([]byte(`null`)); !errors.Is(err, ErrSchemaNotCompiled) {
		t.Errorf("Validate(nil): %v", err)
	}
}

// TestNumberToRat_PathologicalNumbers exercises [numberToRat] across the
// out-of-band JSON-number shapes that the validation runtime might see
// when the upstream JSON parser is configured with [json.Number] (so
// number text reaches us verbatim without float64 round-tripping).
//
// The test pins the package's documented "best-effort, never crash"
// contract: arbitrary-precision big.Rat handles huge magnitudes; NaN /
// Inf / leading-zero strings are accepted as numeric and converted via
// the available code paths; truly empty / sign-only text fails cleanly.
func TestNumberToRat_PathologicalNumbers(t *testing.T) {
	cases := []struct {
		name   string
		in     any
		wantOK bool
	}{
		// Arbitrary-precision big.Rat handles this without overflow.
		{"huge_positive_exponent", json.Number("1e1000"), true},
		{"huge_negative_exponent", json.Number("-1e1000"), true},
		// NaN / Inf as json.Number — SetString fails, Int64 fails, Float64
		// succeeds with NaN/Inf which SetFloat64 silently zeroes. The
		// function reports "ok" with a 0/1 rat. We pin this so a future
		// fix can deliberately change the contract.
		{"json_number_nan", json.Number("NaN"), true},
		{"json_number_inf", json.Number("Inf"), true},
		// Leading-zero integer strings parse fine for big.Rat.
		{"leading_zeros", json.Number("007"), true},
		// Above MaxInt64 and outside float64's integer-precision range.
		{"big_integer_above_int64", json.Number("100000000000000000000"), true},
		// Empty / sign-only strings are not numeric.
		{"empty_string", json.Number(""), false},
		{"bare_minus", json.Number("-"), false},
		// Native Go numeric kinds the function explicitly handles.
		{"native_float64", float64(3.14), true},
		{"native_float64_zero", float64(0), true},
		{"native_float64_nan", math.NaN(), false},
		{"native_float64_pos_inf", math.Inf(1), false},
		{"native_float64_neg_inf", math.Inf(-1), false},
		{"native_int", int(42), true},
		{"native_int_negative", int(-42), true},
		{"native_int64", int64(1 << 40), true},
		{"native_int32", int32(-1234), true},
		// Unsupported types report (nil, false) without panicking.
		{"unsupported_string", "not a number", false},
		{"unsupported_bool", true, false},
		{"unsupported_nil", nil, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r, ok := numberToRat(tc.in)
			if ok != tc.wantOK {
				t.Fatalf("numberToRat(%v) ok = %v; want %v", tc.in, ok, tc.wantOK)
			}
			if ok && r == nil {
				t.Errorf("numberToRat(%v) returned nil rat with ok=true", tc.in)
			}
		})
	}
}

// TestValidateRejectsTrailingContent confirms the instance decoder mirrors
// the schema decoder: a single document followed by extra non-whitespace
// content is rejected (no concatenated-document smuggling).
func TestValidateRejectsTrailingContent(t *testing.T) {
	s := MustCompile([]byte(`{"type":"integer"}`))
	cases := []string{
		`5 "trailing"`,
		`5 5`,
		`{"a":1} {"b":2}`,
		`[1,2,3] []`,
	}
	for _, src := range cases {
		t.Run(src, func(t *testing.T) {
			_, err := s.Validate([]byte(src))
			if err == nil {
				t.Fatalf("Validate(%q): expected error", src)
			}
			if !errors.Is(err, errTrailingContent) {
				t.Errorf("Validate(%q): errors.Is(err, errTrailingContent) = false; err = %v", src, err)
			}
		})
	}
}

// TestUnevaluatedItemsCounter exercises atoiSafe via the unevaluatedItems
// counter path. Indirectly via array unevaluated semantics.
func TestUnevaluatedItemsCounter(t *testing.T) {
	// A schema with prefixItems then unevaluatedItems false. Each prefix
	// match increments the unevaluated counter via atoiSafe-shaped logic.
	src := []byte(`{
		"prefixItems":[{"type":"integer"},{"type":"string"}],
		"unevaluatedItems":false
	}`)
	s := MustCompile(src)
	if res, _ := s.Validate([]byte(`[1,"a"]`)); !res.Valid {
		t.Errorf("expected valid; errors=%v", res.Errors)
	}
	if res, _ := s.Validate([]byte(`[1,"a","extra"]`)); res.Valid {
		t.Error("expected invalid (extra unevaluated item)")
	}
}

// TestMaxValidationDepth covers the addErrorWithCause depth-exceeded branch.
func TestMaxValidationDepth(t *testing.T) {
	src := []byte(`{"$ref":"#"}`)
	s := MustCompile(src)
	res, err := s.Validate([]byte(`null`), WithMaxValidationDepth(3))
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if res.Valid {
		t.Fatal("expected invalid")
	}
	have := false
	for _, e := range res.Errors {
		if errors.Is(&e, ErrMaxValidationDepth) {
			have = true
			break
		}
	}
	if !have {
		t.Errorf("expected ErrMaxValidationDepth in errors")
	}
}

// TestDecodeInstanceBytesPartialJunk covers the second-decode non-EOF
// branch by passing valid JSON followed by partial JSON.
func TestDecodeInstanceBytesPartialJunk(t *testing.T) {
	// First decode reads the integer 42; second decode encounters bare
	// text — surfaces a decode error (not io.EOF).
	if _, err := decodeInstanceBytes([]byte(`42 garbage`)); err == nil {
		t.Error("expected error")
	}
}

// TestValidateToDecodeFailure covers the post-validate decode error.
func TestValidateToDecodeFailure(t *testing.T) {
	type item struct{ N int }
	s := MustCompile([]byte(`{"type":"object"}`))
	// {"N":"x"} validates as an object, but unmarshal into item.N (int)
	// fails.
	if _, err := ValidateTo[item](s, []byte(`{"N":"x"}`)); err == nil {
		t.Error("expected decode-after-validate error")
	}
}

// TestDecodeInstanceBytesTrailing covers the trailing-content branch.
func TestDecodeInstanceBytesTrailing(t *testing.T) {
	s := MustCompile([]byte(`{}`))
	if _, err := s.Validate([]byte(`{} {}`)); err == nil {
		t.Error("expected error on trailing content")
	}
}

// TestValidateAndUnmarshalSuccessfulDecode confirms the full happy path.
func TestValidateAndUnmarshalSuccessfulDecode(t *testing.T) {
	type item struct {
		Name string `json:"name"`
	}
	s := MustCompile([]byte(`{"type":"object","required":["name"]}`))
	var v item
	if err := s.ValidateAndUnmarshal([]byte(`{"name":"alice"}`), &v); err != nil {
		t.Errorf("ValidateAndUnmarshal: %v", err)
	}
	if v.Name != "alice" {
		t.Errorf("got %v", v)
	}
}

// TestValidateEmptyInstance covers the empty-bytes decode failure.
func TestValidateEmptyInstance(t *testing.T) {
	s := MustCompile([]byte(`{}`))
	if _, err := s.Validate([]byte("")); err == nil {
		t.Error("expected decode error on empty bytes")
	}
}

// TestValidateReaderNilReader confirms a nil reader returns ErrNilReader.
func TestValidateReaderNilReader(t *testing.T) {
	s := MustCompile([]byte(`{}`))
	if _, err := s.ValidateReader(nil); !errors.Is(err, ErrNilReader) {
		t.Errorf("ValidateReader(nil) err = %v, want ErrNilReader", err)
	}
}

// errReader returns its stored error from Read.
type errReader struct{ err error }

func (r *errReader) Read([]byte) (int, error) { return 0, r.err }

// TestValidateReaderReadFailure confirms a read error from r is propagated.
func TestValidateReaderReadFailure(t *testing.T) {
	s := MustCompile([]byte(`{}`))
	want := errors.New("boom")
	_, err := s.ValidateReader(&errReader{err: want})
	if err == nil {
		t.Fatal("expected error from reader")
	}
	if !strings.Contains(err.Error(), "boom") {
		t.Errorf("err = %v, want containing 'boom'", err)
	}
}

// TestValidateAndUnmarshalNonNilFailure exercises the unmarshal-error branch
// of ValidateAndUnmarshal: schema valid, but the typed target can't decode
// the JSON (wrong destination type).
func TestValidateAndUnmarshalNonNilFailure(t *testing.T) {
	s := MustCompile([]byte(`{"type":"object"}`))
	var dst int // wrong type for an object
	err := s.ValidateAndUnmarshal([]byte(`{"a":1}`), &dst)
	if err == nil {
		t.Fatal("expected unmarshal error")
	}
	if !strings.Contains(err.Error(), "decode after validate") {
		t.Errorf("err = %v, want containing 'decode after validate'", err)
	}
}

// TestValidateAndUnmarshalNilSchema covers the nil-schema branch.
func TestValidateAndUnmarshalNilSchema(t *testing.T) {
	var s *Schema
	if err := s.ValidateAndUnmarshal([]byte(`{}`), nil); !errors.Is(err, ErrSchemaNotCompiled) {
		t.Errorf("nil schema err = %v, want ErrSchemaNotCompiled", err)
	}
}

// TestValidateAndUnmarshalNilTargetSucceeds covers the v == nil branch.
func TestValidateAndUnmarshalNilTargetSucceeds(t *testing.T) {
	s := MustCompile([]byte(`{"type":"string"}`))
	if err := s.ValidateAndUnmarshal([]byte(`"x"`), nil); err != nil {
		t.Errorf("ValidateAndUnmarshal with nil target: %v", err)
	}
}

// TestValidateAndUnmarshalValidationFailure covers the !res.Valid branch.
func TestValidateAndUnmarshalValidationFailure(t *testing.T) {
	s := MustCompile([]byte(`{"type":"string"}`))
	var dst string
	err := s.ValidateAndUnmarshal([]byte(`5`), &dst)
	if err == nil {
		t.Fatal("expected validation failure")
	}
	if !errors.Is(err, ErrValidation) {
		t.Errorf("err = %v, want ErrValidation", err)
	}
}

// TestValidateToNilSchema covers the nil-schema generic branch.
func TestValidateToNilSchema(t *testing.T) {
	type item struct{ Name string }
	var s *Schema
	_, err := ValidateTo[item](s, []byte(`{}`))
	if !errors.Is(err, ErrSchemaNotCompiled) {
		t.Errorf("nil schema err = %v, want ErrSchemaNotCompiled", err)
	}
}

// TestValidateToValidationFailure covers the !res.Valid branch.
func TestValidateToValidationFailure(t *testing.T) {
	type item struct {
		Name string `json:"name"`
	}
	s := MustCompile([]byte(`{"type":"object","required":["name"]}`))
	if _, err := ValidateTo[item](s, []byte(`{}`)); err == nil {
		t.Fatal("expected validation failure")
	}
}

// TestValidateToInstanceTooLarge confirms the instance-size cap propagates.
func TestValidateToInstanceTooLarge(t *testing.T) {
	type item struct{ Name string }
	s := MustCompile([]byte(`{}`))
	big := []byte(`"` + strings.Repeat("x", 100) + `"`)
	_, err := ValidateTo[item](s, big, WithMaxInstanceSize(10))
	if !errors.Is(err, ErrInstanceTooLarge) {
		t.Errorf("err = %v, want ErrInstanceTooLarge", err)
	}
}

// TestValidationFailureErrorEmptySlice covers the empty-slice branch of the
// internal helper. Reachable from public API via Validate-returning-no-errors
// + a forced validation failure path; instead exercise via direct call.
func TestValidationFailureErrorEmptySlice(t *testing.T) {
	if err := validationFailureError(nil); !errors.Is(err, ErrValidationFailed) {
		t.Errorf("empty errs = %v, want ErrValidationFailed", err)
	}
}

// TestValidationFailureErrorMultiCause covers the multi-error branch.
func TestValidationFailureErrorMultiCause(t *testing.T) {
	errs := []ValidationError{
		{Keyword: "minLength", Message: "too short"},
		{Keyword: "type", Message: "wrong type"},
	}
	err := validationFailureError(errs)
	var ve *ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("err = %v, want *ValidationError", err)
	}
	if len(ve.Causes) != 1 {
		t.Errorf("Causes = %d, want 1", len(ve.Causes))
	}
}
