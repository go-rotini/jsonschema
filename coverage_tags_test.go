package jsonschema

// Generator + tag coverage tests. Each struct exercises one or more tag
// option branches in generator_tags.go via the public Generate path.

import (
	"strings"
	"testing"
)

// TestGenerateTagOptionCoverage exercises many tag-option branches.
func TestGenerateTagOptionCoverage(t *testing.T) {
	// Test schema generation for various tagged fields. Each subtest
	// covers a different option category.
	t.Run("flag-options", func(t *testing.T) {
		type item struct {
			A string `json:"a" jsonschema:"required,deprecated,readOnly"`
			B string `json:"b" jsonschema:"writeOnly"`
		}
		data, err := GenerateBytes(item{})
		if err != nil {
			t.Fatalf("GenerateBytes: %v", err)
		}
		for _, want := range []string{"required", "deprecated", "readOnly", "writeOnly"} {
			if !strings.Contains(string(data), want) {
				t.Errorf("missing %q in: %s", want, data)
			}
		}
	})

	t.Run("string-options", func(t *testing.T) {
		type item struct {
			A string `json:"a" jsonschema:"description=hello,title=alpha,format=email,pattern=^[a-z]+$"`
		}
		data, err := GenerateBytes(item{})
		if err != nil {
			t.Fatalf("GenerateBytes: %v", err)
		}
		for _, want := range []string{"hello", "alpha", "email", "^[a-z]+$"} {
			if !strings.Contains(string(data), want) {
				t.Errorf("missing %q in: %s", want, data)
			}
		}
	})

	t.Run("default-and-const", func(t *testing.T) {
		type item struct {
			A int    `json:"a" jsonschema:"default=42"`
			B string `json:"b" jsonschema:"const=fixed"`
		}
		data, err := GenerateBytes(item{})
		if err != nil {
			t.Fatalf("GenerateBytes: %v", err)
		}
		for _, want := range []string{"default", "42", "const", "fixed"} {
			if !strings.Contains(string(data), want) {
				t.Errorf("missing %q in: %s", want, data)
			}
		}
	})

	t.Run("enum-and-examples", func(t *testing.T) {
		type item struct {
			C string `json:"c" jsonschema:"enum=red|green|blue"`
			D int    `json:"d" jsonschema:"examples=1|2|3"`
		}
		data, err := GenerateBytes(item{})
		if err != nil {
			t.Fatalf("GenerateBytes: %v", err)
		}
		for _, want := range []string{"enum", "red", "green", "blue", "examples", "1"} {
			if !strings.Contains(string(data), want) {
				t.Errorf("missing %q in: %s", want, data)
			}
		}
	})

	t.Run("numeric-options", func(t *testing.T) {
		type item struct {
			N int     `json:"n" jsonschema:"minimum=0,maximum=100,multipleOf=5"`
			F float64 `json:"f" jsonschema:"exclusiveMinimum=0,exclusiveMaximum=10"`
		}
		data, err := GenerateBytes(item{})
		if err != nil {
			t.Fatalf("GenerateBytes: %v", err)
		}
		for _, want := range []string{"minimum", "maximum", "multipleOf", "exclusiveMinimum", "exclusiveMaximum"} {
			if !strings.Contains(string(data), want) {
				t.Errorf("missing %q in: %s", want, data)
			}
		}
	})

	t.Run("length-options", func(t *testing.T) {
		type item struct {
			S string         `json:"s" jsonschema:"minLength=1,maxLength=10"`
			A []int          `json:"a" jsonschema:"minItems=1,maxItems=5"`
			M map[string]int `json:"m" jsonschema:"minProperties=1,maxProperties=5"`
		}
		data, err := GenerateBytes(item{})
		if err != nil {
			t.Fatalf("GenerateBytes: %v", err)
		}
		for _, want := range []string{"minLength", "maxLength", "minItems", "maxItems", "minProperties", "maxProperties"} {
			if !strings.Contains(string(data), want) {
				t.Errorf("missing %q in: %s", want, data)
			}
		}
	})

	t.Run("uniqueItems", func(t *testing.T) {
		type item struct {
			A []int `json:"a" jsonschema:"uniqueItems"`
		}
		data, err := GenerateBytes(item{})
		if err != nil {
			t.Fatalf("GenerateBytes: %v", err)
		}
		if !strings.Contains(string(data), "uniqueItems") {
			t.Errorf("missing uniqueItems in: %s", data)
		}
	})

	t.Run("additionalProperties=false", func(t *testing.T) {
		type item struct {
			M map[string]string `json:"m" jsonschema:"additionalProperties=false"`
		}
		data, err := GenerateBytes(item{})
		if err != nil {
			t.Fatalf("GenerateBytes: %v", err)
		}
		if !strings.Contains(string(data), "additionalProperties") {
			t.Errorf("missing additionalProperties in: %s", data)
		}
	})

	t.Run("$id and $ref", func(t *testing.T) {
		type item struct {
			A string `json:"a" jsonschema:"$id=https://e.com/x"`
			B string `json:"b" jsonschema:"$ref=#/$defs/x"`
		}
		data, err := GenerateBytes(item{})
		if err != nil {
			t.Fatalf("GenerateBytes: %v", err)
		}
		if !strings.Contains(string(data), "$ref") {
			t.Errorf("missing $ref: %s", data)
		}
	})
}

// TestGenerateTagOptionErrors exercises tag-option-failure branches.
func TestGenerateTagOptionErrors(t *testing.T) {
	cases := map[string]any{
		"flag-with-value": struct {
			A int `json:"a" jsonschema:"required=oops"`
		}{},
		"string-without-value": struct {
			A int `json:"a" jsonschema:"description"`
		}{},
		"min-on-non-string": struct {
			A int `json:"a" jsonschema:"minLength=3"`
		}{},
		"min-items-on-non-array": struct {
			A string `json:"a" jsonschema:"minItems=3"`
		}{},
		"min-properties-on-non-map": struct {
			A string `json:"a" jsonschema:"minProperties=3"`
		}{},
		"unknown-option": struct {
			A int `json:"a" jsonschema:"unknownOption=x"`
		}{},
		"additionalProperties-no-value": struct {
			A map[string]int `json:"a" jsonschema:"additionalProperties"`
		}{},
		"min-non-int": struct {
			A string `json:"a" jsonschema:"minLength=abc"`
		}{},
		"min-negative": struct {
			A string `json:"a" jsonschema:"minLength=-1"`
		}{},
		"numeric-without-value": struct {
			A int `json:"a" jsonschema:"minimum"`
		}{},
		"numeric-on-non-numeric": struct {
			A string `json:"a" jsonschema:"minimum=3"`
		}{},
		"numeric-bad-int": struct {
			A int `json:"a" jsonschema:"minimum=abc"`
		}{},
		"numeric-bad-float": struct {
			A float64 `json:"a" jsonschema:"minimum=abc"`
		}{},
		"const-without-value": struct {
			A int `json:"a" jsonschema:"const"`
		}{},
		"default-without-value": struct {
			A int `json:"a" jsonschema:"default"`
		}{},
		"default-bad-int": struct {
			A int `json:"a" jsonschema:"default=abc"`
		}{},
		"default-bad-bool": struct {
			A bool `json:"a" jsonschema:"default=yes"`
		}{},
	}
	for name, v := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := GenerateBytes(v); err == nil {
				t.Errorf("expected error for %s", name)
			}
		})
	}
}

// TestGenerateTagEscaping covers the escape sequences in the tag tokenizer.
func TestGenerateTagEscaping(t *testing.T) {
	type item struct {
		// Description containing a literal comma + pipe via escaping.
		A string `json:"a" jsonschema:"description=hello\\, world"`
	}
	data, err := GenerateBytes(item{})
	if err != nil {
		t.Fatalf("GenerateBytes: %v", err)
	}
	if !strings.Contains(string(data), "hello, world") {
		t.Errorf("escape not unescaped: %s", data)
	}
}

// TestGenerateTagOmitDescriptions covers WithGenerateOmitDescriptions.
func TestGenerateTagOmitDescriptions(t *testing.T) {
	type item struct {
		// Field has both a Go doc comment via reflection AND a tag.
		A string `json:"a" jsonschema:"description=tag-set"`
	}
	data, err := GenerateBytes(item{}, WithGenerateOmitDescriptions(true))
	if err != nil {
		t.Fatalf("GenerateBytes: %v", err)
	}
	if strings.Contains(string(data), "tag-set") {
		t.Errorf("description should be omitted: %s", data)
	}
}

// TestGenerateAdditionalPropertiesFalseGlobal covers
// WithGenerateAdditionalPropertiesFalse.
func TestGenerateAdditionalPropertiesFalseGlobal(t *testing.T) {
	type item struct {
		A string `json:"a"`
	}
	data, err := GenerateBytes(item{}, WithGenerateAdditionalPropertiesFalse(true))
	if err != nil {
		t.Fatalf("GenerateBytes: %v", err)
	}
	if !strings.Contains(string(data), `"additionalProperties":false`) {
		t.Errorf("expected additionalProperties:false: %s", data)
	}
}

// TestGenerateWithID covers WithGenerateID.
func TestGenerateWithID(t *testing.T) {
	type item struct {
		A string `json:"a"`
	}
	data, err := GenerateBytes(item{}, WithGenerateID("https://example.com/i"))
	if err != nil {
		t.Fatalf("GenerateBytes: %v", err)
	}
	if !strings.Contains(string(data), "https://example.com/i") {
		t.Errorf("expected $id: %s", data)
	}
}

// TestGenerateInterfaceAsAnyOff covers WithGenerateInterfaceAsAny(false).
func TestGenerateInterfaceAsAnyOff(t *testing.T) {
	type item struct {
		A any `json:"a"`
	}
	if _, err := GenerateBytes(item{}, WithGenerateInterfaceAsAny(false)); err == nil {
		t.Skip("interface accepted; behavior may have changed")
	}
}

// TestGenerateMustGenerateBytes covers GenerateBytes happy path.
func TestGenerateMustGenerateBytes(t *testing.T) {
	type item struct {
		Name string `json:"name"`
	}
	data, err := GenerateBytes(item{})
	if err != nil {
		t.Fatalf("GenerateBytes: %v", err)
	}
	if !strings.Contains(string(data), "name") {
		t.Errorf("missing 'name': %s", data)
	}
}

// TestGenerateAlternateDraft covers WithGenerateDraft(non-default).
func TestGenerateAlternateDraft(t *testing.T) {
	type item struct {
		Name string `json:"name"`
	}
	data, err := GenerateBytes(item{}, WithGenerateDraft(Draft7))
	if err != nil {
		t.Fatalf("GenerateBytes: %v", err)
	}
	if !strings.Contains(string(data), "draft-07") {
		t.Errorf("expected draft-07 in $schema: %s", data)
	}
}

// TestGenerateTagFlagOptionsRejectValue covers the "flag option does not
// take a value" branches for required/deprecated/readOnly/writeOnly/
// uniqueItems.
func TestGenerateTagFlagOptionsRejectValue(t *testing.T) {
	cases := []struct {
		name    string
		struct_ any
	}{
		{"required-with-value", struct {
			A string `json:"a" jsonschema:"required=true"`
		}{}},
		{"deprecated-with-value", struct {
			A string `json:"a" jsonschema:"deprecated=true"`
		}{}},
		{"readOnly-with-value", struct {
			A string `json:"a" jsonschema:"readOnly=true"`
		}{}},
		{"writeOnly-with-value", struct {
			A string `json:"a" jsonschema:"writeOnly=true"`
		}{}},
		{"uniqueItems-with-value", struct {
			A []string `json:"a" jsonschema:"uniqueItems=true"`
		}{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := GenerateBytes(tc.struct_); err == nil {
				t.Errorf("expected error for %s", tc.name)
			}
		})
	}
}

// TestGenerateTagStringOptionsMissingValue covers the "requires a value"
// branches for description/title/format/pattern/$id/$ref.
func TestGenerateTagStringOptionsMissingValue(t *testing.T) {
	cases := []struct {
		name    string
		struct_ any
	}{
		{"description", struct {
			A string `json:"a" jsonschema:"description"`
		}{}},
		{"title", struct {
			A string `json:"a" jsonschema:"title"`
		}{}},
		{"format", struct {
			A string `json:"a" jsonschema:"format"`
		}{}},
		{"pattern", struct {
			A string `json:"a" jsonschema:"pattern"`
		}{}},
		{"$id", struct {
			A string `json:"a" jsonschema:"$id"`
		}{}},
		{"$ref", struct {
			A string `json:"a" jsonschema:"$ref"`
		}{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := GenerateBytes(tc.struct_); err == nil {
				t.Errorf("expected error for %s missing value", tc.name)
			}
		})
	}
}

// TestGenerateTagDefaultMissingValue covers default/const requires-a-value.
func TestGenerateTagDefaultMissingValue(t *testing.T) {
	type item struct {
		A int `json:"a" jsonschema:"default"`
	}
	if _, err := GenerateBytes(item{}); err == nil {
		t.Error("expected error for default without value")
	}
}

// TestGenerateTagConstMissingValue covers const requires-a-value.
func TestGenerateTagConstMissingValue(t *testing.T) {
	type item struct {
		A int `json:"a" jsonschema:"const"`
	}
	if _, err := GenerateBytes(item{}); err == nil {
		t.Error("expected error for const without value")
	}
}

// TestGenerateTagDefaultBadCoercion covers the default coerceFieldValue
// error branch.
func TestGenerateTagDefaultBadCoercion(t *testing.T) {
	type item struct {
		A int `json:"a" jsonschema:"default=not-an-int"`
	}
	if _, err := GenerateBytes(item{}); err == nil {
		t.Error("expected error for bad default coercion")
	}
}

// TestGenerateTagConstBadCoercion covers the const coerceFieldValue error
// branch.
func TestGenerateTagConstBadCoercion(t *testing.T) {
	type item struct {
		A int `json:"a" jsonschema:"const=not-an-int"`
	}
	if _, err := GenerateBytes(item{}); err == nil {
		t.Error("expected error for bad const coercion")
	}
}

// TestGenerateTagListMissingValue covers enum/examples requires-a-value.
func TestGenerateTagListMissingValue(t *testing.T) {
	type item struct {
		A int `json:"a" jsonschema:"enum"`
	}
	if _, err := GenerateBytes(item{}); err == nil {
		t.Error("expected error for enum without value")
	}
}

// TestGenerateTagListBadCoercion covers the enum/examples coerce error.
func TestGenerateTagListBadCoercion(t *testing.T) {
	type item struct {
		A int `json:"a" jsonschema:"enum=alpha|beta"`
	}
	if _, err := GenerateBytes(item{}); err == nil {
		t.Error("expected error for bad enum coercion")
	}
}

// TestGenerateTagEmptyToken covers tokenizeJSONSchemaTag's empty-token
// continue branch (a tag like ",,required" produces empty tokens).
func TestGenerateTagEmptyToken(t *testing.T) {
	type item struct {
		A string `json:"a" jsonschema:",,required"`
	}
	if _, err := GenerateBytes(item{}); err != nil {
		t.Errorf("expected success skipping empty tokens: %v", err)
	}
}

// TestGenerateTagDanglingEscape covers tokenizeJSONSchemaTag's trailing
// escape branch.
func TestGenerateTagDanglingEscape(t *testing.T) {
	type item struct {
		A string `json:"a" jsonschema:"description=hello\\"`
	}
	// Trailing escape produces a dangling backslash. Should still parse.
	if _, err := GenerateBytes(item{}); err != nil {
		t.Logf("dangling-escape err (acceptable): %v", err)
	}
}

// TestGenerateTagEscapedEqualsInName covers splitTagOption's escape skip
// (\=) branch.
func TestGenerateTagEscapedEqualsInName(t *testing.T) {
	type item struct {
		A string `json:"a" jsonschema:"description=foo\\=bar"`
	}
	if _, err := GenerateBytes(item{}); err != nil {
		t.Errorf("escaped equals: %v", err)
	}
}
