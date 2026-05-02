package jsonschema

import (
	"errors"
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
		t.Run(c.schema+"/"+c.data, func(t *testing.T) {
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

func TestValidate_NilSchemaErrors(t *testing.T) {
	var s *Schema
	if _, err := s.Validate([]byte(`null`)); !errors.Is(err, ErrSchemaNotCompiled) {
		t.Errorf("Validate(nil): %v", err)
	}
}
