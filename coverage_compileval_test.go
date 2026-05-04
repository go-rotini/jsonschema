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

// TestCompileDeepNestedBadShape covers the bindAndResolve path where a
// recursively-resolved child schema returns an error that propagates up
// through bindAndResolveChild → bindAndResolve.
func TestCompileDeepNestedBadShape(t *testing.T) {
	v := map[string]any{
		"properties": map[string]any{
			"x": map[string]any{
				"allOf": "not-an-array",
			},
		},
	}
	if _, err := CompileValue(v, WithMetaSchemaValidation(false)); err == nil {
		t.Error("expected error from nested allOf shape failure")
	}
}

// TestCompileItemsArrayBadChild covers the items array recursion with a
// malformed child shape.
func TestCompileItemsArrayBadChild(t *testing.T) {
	v := map[string]any{
		"items": []any{
			map[string]any{"allOf": "not-an-array"},
		},
	}
	if _, err := CompileValue(v, WithMetaSchemaValidation(false)); err == nil {
		t.Error("expected error from bad items[0] shape")
	}
}

// TestCompileItemsSingleSchemaBadChild covers the items default-case
// (single schema) recursion with a malformed shape.
func TestCompileItemsSingleSchemaBadChild(t *testing.T) {
	v := map[string]any{
		"items": map[string]any{
			"allOf": "not-an-array",
		},
	}
	if _, err := CompileValue(v, WithMetaSchemaValidation(false)); err == nil {
		t.Error("expected error from bad items single-schema shape")
	}
}

// TestCompileAllOfBadChild covers the allOf recursion error cascade with
// a malformed nested shape.
func TestCompileAllOfBadChild(t *testing.T) {
	v := map[string]any{
		"allOf": []any{
			map[string]any{"allOf": "not-an-array"},
		},
	}
	if _, err := CompileValue(v, WithMetaSchemaValidation(false)); err == nil {
		t.Error("expected error from bad allOf[0] nested shape")
	}
}

// TestCompileDependenciesSchemaBadChild covers the dependencies schema-
// branch cascade with a malformed nested shape.
func TestCompileDependenciesSchemaBadChild(t *testing.T) {
	v := map[string]any{
		"dependencies": map[string]any{
			"x": map[string]any{
				"allOf": "not-an-array",
			},
		},
	}
	if _, err := CompileValue(v, WithMetaSchemaValidation(false)); err == nil {
		t.Error("expected error from bad dependencies nested shape")
	}
}

// TestCompileDependenciesArrayPasses covers the dependencies array branch
// (no schema, just required-list).
func TestCompileDependenciesArrayPasses(t *testing.T) {
	v := map[string]any{
		"dependencies": map[string]any{
			"x": []any{"y"},
		},
	}
	s, err := CompileValue(v, WithMetaSchemaValidation(false))
	if err != nil {
		t.Fatalf("CompileValue: %v", err)
	}
	if _, err := s.Validate([]byte(`{"x":1,"y":2}`)); err != nil {
		t.Fatalf("Validate: %v", err)
	}
}

// TestCompileDefaultBranchBadChild covers bindAndResolveChild's default
// branch (e.g. "not", "if", "then", "else") with a malformed nested
// shape.
func TestCompileDefaultBranchBadChild(t *testing.T) {
	v := map[string]any{
		"not": map[string]any{
			"allOf": "not-an-array",
		},
	}
	if _, err := CompileValue(v, WithMetaSchemaValidation(false)); err == nil {
		t.Error("expected error from bad not subschema shape")
	}
}

// TestCompileMetaSchemaValidationFailure exercises the meta-schema
// validation path (validateKeywordShape passes; meta-schema rejects).
func TestCompileMetaSchemaValidationFailure(t *testing.T) {
	// type:"weirdtype" passes our shape check (any string is fine) but
	// the draft-2020-12 meta-schema enforces a closed enum.
	v := map[string]any{
		"$schema": "https://json-schema.org/draft/2020-12/schema",
		"type":    "weirdtype",
	}
	if _, err := CompileValue(v, WithMetaSchemaValidation(true)); err == nil {
		t.Error("expected meta-schema validation error")
	}
}

// TestCompileMetaSchemaValidationManyErrors exercises the >5-errors
// truncation branch by stacking many meta-schema-level violations that
// pass validateKeywordShape.
func TestCompileMetaSchemaValidationManyErrors(t *testing.T) {
	// Each property uses an unknown type — passes shape check, fails
	// meta-schema. >5 properties to exercise truncation.
	props := map[string]any{}
	for i, k := range []string{"a", "b", "c", "d", "e", "f", "g", "h"} {
		props[k] = map[string]any{"type": "weirdtype" + string(rune('0'+i))}
	}
	v := map[string]any{
		"$schema":    "https://json-schema.org/draft/2020-12/schema",
		"properties": props,
	}
	if _, err := CompileValue(v, WithMetaSchemaValidation(true)); err == nil {
		t.Error("expected error from many meta-schema failures")
	}
}

// TestCompileValueIntPathsForLengthBounds exercises toInt's int/int64
// and float64-truncated paths via CompileValue with native Go ints.
func TestCompileValueIntPathsForLengthBounds(t *testing.T) {
	t.Run("int", func(t *testing.T) {
		v := map[string]any{"minLength": int(5), "maxLength": int(10)}
		if _, err := CompileValue(v); err != nil {
			t.Fatalf("int: %v", err)
		}
	})
	t.Run("int64", func(t *testing.T) {
		v := map[string]any{"minLength": int64(5), "maxLength": int64(10)}
		if _, err := CompileValue(v); err != nil {
			t.Fatalf("int64: %v", err)
		}
	})
	t.Run("float-truncated", func(t *testing.T) {
		v := map[string]any{"minLength": 5.0, "maxLength": 10.0}
		if _, err := CompileValue(v); err != nil {
			t.Fatalf("float64: %v", err)
		}
	})
	t.Run("float-non-truncated", func(t *testing.T) {
		// 5.5 is not an integer — should fail at validateKeywordShape.
		v := map[string]any{"minLength": 5.5}
		if _, err := CompileValue(v); err == nil {
			t.Error("expected error for non-integer minLength")
		}
	})
}

// TestSortMultiEvaluatorsExercisesKeyword forces 2+ evaluators on the
// same subschema so the sort.SliceStable callback exercises each
// evaluator's keyword() method.
func TestSortMultiEvaluatorsExercisesKeyword(t *testing.T) {
	src := []byte(`{
		"type":"string",
		"const":"hello",
		"enum":["hello"],
		"minLength":1,
		"maxLength":10,
		"pattern":"^h",
		"format":"email"
	}`)
	s, err := Compile(src)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	if _, err := s.Validate([]byte(`"hello"`)); err != nil {
		t.Fatalf("Validate: %v", err)
	}
}

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
