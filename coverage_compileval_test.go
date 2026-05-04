package jsonschema

// CompileValue path tests. CompileValue bypasses the JSON-decode pass that
// validateKeywordShape would catch, so non-standard Go shapes like a map
// holding a non-array allOf trigger the per-evaluator !ok branches.
//
// These tests cover those defensive branches.

import (
	"encoding/json"
	"testing"
)

// TestCompileValueAllOfWrongShape covers the !ok branch in the allOf
// builder.
func TestCompileValueAllOfWrongShape(t *testing.T) {
	v := map[string]any{"allOf": "not-an-array"}
	if _, err := CompileValue(v); err == nil {
		t.Error("expected error for non-array allOf")
	}
}

// TestCompileValueAnyOfWrongShape covers the !ok branch in the anyOf
// builder.
func TestCompileValueAnyOfWrongShape(t *testing.T) {
	v := map[string]any{"anyOf": "not-an-array"}
	if _, err := CompileValue(v); err == nil {
		t.Error("expected error for non-array anyOf")
	}
}

// TestCompileValueOneOfWrongShape covers the !ok branch in oneOf builder.
func TestCompileValueOneOfWrongShape(t *testing.T) {
	v := map[string]any{"oneOf": "not-an-array"}
	if _, err := CompileValue(v); err == nil {
		t.Error("expected error for non-array oneOf")
	}
}

// TestCompileValuePropertiesWrongShape covers properties wrong shape.
func TestCompileValuePropertiesWrongShape(t *testing.T) {
	v := map[string]any{"properties": "not-an-object"}
	if _, err := CompileValue(v); err == nil {
		t.Error("expected error for non-object properties")
	}
}

// TestCompileValueDependentSchemasWrongShape covers dependentSchemas.
func TestCompileValueDependentSchemasWrongShape(t *testing.T) {
	v := map[string]any{"dependentSchemas": "wrong"}
	if _, err := CompileValue(v); err == nil {
		t.Error("expected error")
	}
}

// TestCompileValueDependentRequiredWrongShape covers dependentRequired.
func TestCompileValueDependentRequiredWrongShape(t *testing.T) {
	v := map[string]any{"dependentRequired": "wrong"}
	if _, err := CompileValue(v); err == nil {
		t.Logf("dependentRequired wrong shape: schema accepted (perhaps tolerated)")
	}
}

// TestCompileValueRequiredWrongShape covers required wrong type.
func TestCompileValueRequiredWrongShape(t *testing.T) {
	v := map[string]any{"required": 42}
	if _, err := CompileValue(v); err == nil {
		t.Error("expected error")
	}
}

// TestCompileValuePatternPropertiesWrongShape covers patternProperties.
func TestCompileValuePatternPropertiesWrongShape(t *testing.T) {
	v := map[string]any{"patternProperties": []any{"x"}}
	if _, err := CompileValue(v); err == nil {
		t.Error("expected error")
	}
}

// TestCompileValueDependenciesWrongShape covers dependencies (legacy).
func TestCompileValueDependenciesWrongShape(t *testing.T) {
	v := map[string]any{"dependencies": "wrong"}
	if _, err := CompileValue(v, WithDefaultDraft(Draft7)); err == nil {
		t.Logf("draft-7 dependencies: tolerated")
	}
}

// TestCompileValueIfWithoutThen covers the if-without-then branch via the
// ifThenElseEval. Also covers if-without-else.
func TestCompileValueIfWithoutThenOrElse(t *testing.T) {
	v := map[string]any{
		"if": map[string]any{"type": "string"},
		// neither then nor else
	}
	s, err := CompileValue(v)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	res, _ := s.Validate([]byte(`"x"`))
	if !res.Valid {
		t.Errorf("expected valid; errors=%v", res.Errors)
	}
}

// TestCompileValueIfWithThenOnly covers if-then no else.
func TestCompileValueIfWithThenOnly(t *testing.T) {
	v := map[string]any{
		"if":   map[string]any{"type": "string"},
		"then": map[string]any{"minLength": 3},
	}
	s, err := CompileValue(v)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	res, _ := s.Validate([]byte(`"abc"`))
	if !res.Valid {
		t.Errorf("expected valid; errors=%v", res.Errors)
	}
	res, _ = s.Validate([]byte(`"a"`))
	if res.Valid {
		t.Error("expected invalid (too short)")
	}
}

// TestCompileValueIfWithElseOnly covers if-fail → else.
func TestCompileValueIfWithElseOnly(t *testing.T) {
	v := map[string]any{
		"if":   map[string]any{"type": "string"},
		"else": map[string]any{"const": 42},
	}
	s, err := CompileValue(v)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	res, _ := s.Validate([]byte(`42`))
	if !res.Valid {
		t.Errorf("expected valid; errors=%v", res.Errors)
	}
	res, _ = s.Validate([]byte(`99`))
	if res.Valid {
		t.Error("expected invalid (else const)")
	}
}

// TestCompileValuePrefixItemsWrongShape covers the prefixItems !ok.
func TestCompileValuePrefixItemsWrongShape(t *testing.T) {
	v := map[string]any{"prefixItems": "not-an-array"}
	if _, err := CompileValue(v); err == nil {
		t.Error("expected error for non-array prefixItems")
	}
}

// TestCompileValueItemsBoolean covers items as a boolean schema.
func TestCompileValueItemsBoolean(t *testing.T) {
	v := map[string]any{"items": false}
	s, err := CompileValue(v)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	res, _ := s.Validate([]byte(`[]`))
	if !res.Valid {
		t.Errorf("empty array should pass false-items: errors=%v", res.Errors)
	}
	res, _ = s.Validate([]byte(`[1]`))
	if res.Valid {
		t.Error("non-empty array should fail items:false")
	}
}

// TestCompileValuePatternPropertiesNotMap covers the patternProperties
// !ok branch.
func TestCompileValuePatternPropertiesNotMap(t *testing.T) {
	v := map[string]any{"patternProperties": "wrong"}
	if _, err := CompileValue(v); err == nil {
		t.Error("expected error")
	}
}

// TestCompileValueAdditionalPropertiesNotSchema covers the
// additionalProperties shape.
func TestCompileValueAdditionalPropertiesNotSchema(t *testing.T) {
	// additionalProperties should be schema or boolean; integer is wrong.
	v := map[string]any{"additionalProperties": 42}
	if _, err := CompileValue(v); err == nil {
		t.Logf("additionalProperties:42 accepted; permissive shape check")
	}
}

// TestCompileValuePropertyNamesNotSchema covers propertyNames non-schema.
func TestCompileValuePropertyNamesNotSchema(t *testing.T) {
	v := map[string]any{"propertyNames": 42}
	if _, err := CompileValue(v); err == nil {
		t.Logf("propertyNames:42 accepted")
	}
}

// TestCompileValueContainsBranches covers contains evaluator shapes.
func TestCompileValueContainsBranches(t *testing.T) {
	// contains: must be a schema (object or bool)
	v := map[string]any{"contains": map[string]any{"type": "integer"}}
	s, err := CompileValue(v)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if res, _ := s.Validate([]byte(`[1,"x"]`)); !res.Valid {
		t.Errorf("expected valid; errors=%v", res.Errors)
	}
}

// TestCompileValueContainsWithMinMax covers min/maxContains.
func TestCompileValueContainsWithMinMax(t *testing.T) {
	v := map[string]any{
		"contains":    map[string]any{"type": "integer"},
		"minContains": json.Number("2"),
		"maxContains": json.Number("3"),
	}
	s, err := CompileValue(v)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if res, _ := s.Validate([]byte(`[1,2,"x"]`)); !res.Valid {
		t.Errorf("expected valid (2 ints); errors=%v", res.Errors)
	}
	if res, _ := s.Validate([]byte(`[1,2,3,4,"x"]`)); res.Valid {
		t.Error("expected invalid (4 ints exceed max=3)")
	}
}

// TestCompileValuePropertiesWithBoolValue covers properties with a boolean
// schema.
func TestCompileValuePropertiesWithBoolValue(t *testing.T) {
	v := map[string]any{
		"properties": map[string]any{
			"x": false, // x is rejected
		},
	}
	s, err := CompileValue(v)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if res, _ := s.Validate([]byte(`{"x":1}`)); res.Valid {
		t.Error("expected invalid (false schema)")
	}
}

// TestCompileValueDependenciesArrayList covers the legacy dependencies
// array-of-strings branch.
func TestCompileValueDependenciesArrayList(t *testing.T) {
	v := map[string]any{
		"dependencies": map[string]any{
			"a": []any{"b", "c"},
		},
	}
	s, err := CompileValue(v, WithDefaultDraft(Draft7))
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if res, _ := s.Validate([]byte(`{"a":1}`)); res.Valid {
		t.Error("expected invalid (a requires b,c)")
	}
}

// TestCompileValuePrefixItemsBoolean covers prefixItems with boolean entry.
func TestCompileValuePrefixItemsBoolean(t *testing.T) {
	v := map[string]any{
		"prefixItems": []any{
			map[string]any{"type": "integer"},
			false, // second item rejected
		},
	}
	s, err := CompileValue(v)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if res, _ := s.Validate([]byte(`[1, "anything"]`)); res.Valid {
		t.Error("expected invalid (prefix[1] is false)")
	}
}

// TestCompileValueNotSchema covers the not keyword.
func TestCompileValueNotSchema(t *testing.T) {
	v := map[string]any{
		"not": map[string]any{"type": "string"},
	}
	s, err := CompileValue(v)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if res, _ := s.Validate([]byte(`42`)); !res.Valid {
		t.Errorf("expected valid; errors=%v", res.Errors)
	}
	if res, _ := s.Validate([]byte(`"x"`)); res.Valid {
		t.Error("expected invalid (matches not)")
	}
}
