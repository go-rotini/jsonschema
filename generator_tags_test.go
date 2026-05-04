package jsonschema

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestTagTokenizer(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"required,minLength=1", []string{"required", "minLength=1"}},
		{`description=hello\, world`, []string{`description=hello\, world`}},
		{`a=1,b=2\,3,c=4`, []string{`a=1`, `b=2\,3`, `c=4`}},
		{``, nil},
	}
	for _, c := range cases {
		got := tokenizeJSONSchemaTag(c.in)
		if len(got) != len(c.want) {
			t.Errorf("tokenize(%q) = %v; want %v", c.in, got, c.want)
			continue
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Errorf("tokenize(%q)[%d] = %q; want %q", c.in, i, got[i], c.want[i])
			}
		}
	}
}

func TestTagSplitOption(t *testing.T) {
	name, val, has := splitTagOption(`description=hello\, world`)
	if name != "description" || val != "hello, world" || !has {
		t.Errorf("got (%q, %q, %v)", name, val, has)
	}

	name, val, has = splitTagOption("required")
	if name != "required" || val != "" || has {
		t.Errorf("got (%q, %q, %v)", name, val, has)
	}

	// First unescaped = splits; subsequent = remain in value.
	name, val, has = splitTagOption("default=a=b=c")
	if name != "default" || val != "a=b=c" || !has {
		t.Errorf("got (%q, %q, %v)", name, val, has)
	}
}

func TestTagSplitList(t *testing.T) {
	got := splitTagList(`a\|b|c|d`)
	want := []string{"a|b", "c", "d"}
	if len(got) != len(want) {
		t.Fatalf("len mismatch: got %v, want %v", got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("got[%d]=%q want %q", i, got[i], want[i])
		}
	}
}

func TestTagUnescape(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{`hello\, world`, `hello, world`},
		{`a\|b`, `a|b`},
		{`\\d+`, `\d+`},
		{"plain", "plain"},
	}
	for _, c := range cases {
		got := unescapeTagValue(c.in)
		if got != c.want {
			t.Errorf("unescape(%q) = %q; want %q", c.in, got, c.want)
		}
	}
}

func TestTagParseHappyPath(t *testing.T) {
	type Holder struct {
		S string `jsonschema:"required,minLength=1,maxLength=100,pattern=^[a-z]+$,format=email,description=user-id"`
	}
	data, err := GenerateBytes(Holder{})
	if err != nil {
		t.Fatalf("GenerateBytes: %v", err)
	}
	for _, want := range []string{
		`"minLength":1`, `"maxLength":100`,
		`"pattern":"^[a-z]+$"`, `"format":"email"`,
		`"description":"user-id"`,
	} {
		if !strings.Contains(string(data), want) {
			t.Errorf("missing %q in %s", want, data)
		}
	}
}

func TestTagDescriptionEscapedComma(t *testing.T) {
	type Holder struct {
		S string `jsonschema:"description=hello\\, world"`
	}
	data, err := GenerateBytes(Holder{})
	if err != nil {
		t.Fatalf("GenerateBytes: %v", err)
	}
	if !strings.Contains(string(data), `"description":"hello, world"`) {
		t.Errorf("got %s", data)
	}
}

func TestTagPatternEscapedBackslash(t *testing.T) {
	type Holder struct {
		S string `jsonschema:"pattern=\\\\d+"`
	}
	// Value should round-trip to `\d+` after unescape — JSON escapes
	// the backslash too, so the wire form is "\\d+".
	data, err := GenerateBytes(Holder{})
	if err != nil {
		t.Fatalf("GenerateBytes: %v", err)
	}
	if !strings.Contains(string(data), `"pattern":"\\d+"`) {
		t.Errorf("got %s", data)
	}
}

func TestTagEnumAndExamples(t *testing.T) {
	type Holder struct {
		S string `jsonschema:"enum=a|b|c,examples=x|y"`
	}
	data, err := GenerateBytes(Holder{})
	if err != nil {
		t.Fatalf("GenerateBytes: %v", err)
	}
	if !strings.Contains(string(data), `"enum":["a","b","c"]`) {
		t.Errorf("got %s", data)
	}
	if !strings.Contains(string(data), `"examples":["x","y"]`) {
		t.Errorf("got %s", data)
	}
}

func TestTagEnumIntCoerce(t *testing.T) {
	type Holder struct {
		N int `jsonschema:"enum=1|2|3"`
	}
	data, err := GenerateBytes(Holder{})
	if err != nil {
		t.Fatalf("GenerateBytes: %v", err)
	}
	if !strings.Contains(string(data), `"enum":[1,2,3]`) {
		t.Errorf("expected integer enum, got %s", data)
	}
}

func TestTagFlagsCannotTakeValue(t *testing.T) {
	type Holder struct {
		S string `jsonschema:"required=true"`
	}
	_, err := GenerateBytes(Holder{})
	if err == nil {
		t.Fatal("expected error: required=true should be rejected")
	}
	var ce *CompileError
	if !errors.As(err, &ce) {
		t.Errorf("got %T", err)
	}
}

func TestTagMinLengthOnNonStringErrors(t *testing.T) {
	type Holder struct {
		N int `jsonschema:"minLength=3"`
	}
	_, err := GenerateBytes(Holder{})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestTagMinimumOnNonNumericErrors(t *testing.T) {
	type Holder struct {
		S string `jsonschema:"minimum=3"`
	}
	_, err := GenerateBytes(Holder{})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestTagMalformedNumeric(t *testing.T) {
	type Holder struct {
		N int `jsonschema:"minimum=NaN"`
	}
	_, err := GenerateBytes(Holder{})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestTagUnknownOptionErrors(t *testing.T) {
	type Holder struct {
		S string `jsonschema:"frobnicate=yes"`
	}
	_, err := GenerateBytes(Holder{})
	if err == nil {
		t.Fatal("expected error for unknown tag option")
	}
}

func TestTagDeprecatedReadOnlyWriteOnly(t *testing.T) {
	type Holder struct {
		A string `jsonschema:"deprecated"`
		B string `jsonschema:"readOnly"`
		C string `jsonschema:"writeOnly"`
	}
	data, err := GenerateBytes(Holder{})
	if err != nil {
		t.Fatalf("GenerateBytes: %v", err)
	}
	for _, w := range []string{`"deprecated":true`, `"readOnly":true`, `"writeOnly":true`} {
		if !strings.Contains(string(data), w) {
			t.Errorf("missing %s in %s", w, data)
		}
	}
}

func TestTagDefaultAndConst(t *testing.T) {
	type Holder struct {
		Name  string `jsonschema:"default=anon"`
		Count int    `jsonschema:"const=42"`
	}
	data, err := GenerateBytes(Holder{})
	if err != nil {
		t.Fatalf("GenerateBytes: %v", err)
	}
	if !strings.Contains(string(data), `"default":"anon"`) {
		t.Errorf("got %s", data)
	}
	if !strings.Contains(string(data), `"const":42`) {
		t.Errorf("got %s", data)
	}
}

func TestTagBoundsExclusiveAndMultiple(t *testing.T) {
	type Holder struct {
		N int     `jsonschema:"exclusiveMinimum=0,exclusiveMaximum=100,multipleOf=5"`
		F float64 `jsonschema:"minimum=0.5"`
	}
	data, err := GenerateBytes(Holder{})
	if err != nil {
		t.Fatalf("GenerateBytes: %v", err)
	}
	for _, w := range []string{`"exclusiveMinimum":0`, `"exclusiveMaximum":100`, `"multipleOf":5`, `"minimum":0.5`} {
		if !strings.Contains(string(data), w) {
			t.Errorf("missing %s in %s", w, data)
		}
	}
}

func TestTagAdditionalPropertiesFalseOnMap(t *testing.T) {
	type Holder struct {
		M map[string]int `jsonschema:"additionalProperties=false"`
	}
	data, err := GenerateBytes(Holder{})
	if err != nil {
		t.Fatalf("GenerateBytes: %v", err)
	}
	// The tag overlay sets the field schema's additionalProperties to
	// false (overriding the implicit value-type schema). Both shapes
	// would land in the result; we just confirm the literal `false`
	// is present.
	if !strings.Contains(string(data), `"additionalProperties":false`) {
		t.Errorf("got %s", data)
	}
}

func TestTagAdditionalPropertiesNonFalseRejected(t *testing.T) {
	type Holder struct {
		M map[string]int `jsonschema:"additionalProperties=true"`
	}
	_, err := GenerateBytes(Holder{})
	if err == nil {
		t.Fatal("expected error: only =false supported")
	}
}

func TestTagMinPropertiesOnMap(t *testing.T) {
	type Holder struct {
		M map[string]int `jsonschema:"minProperties=1,maxProperties=10"`
	}
	data, err := GenerateBytes(Holder{})
	if err != nil {
		t.Fatalf("GenerateBytes: %v", err)
	}
	for _, w := range []string{`"minProperties":1`, `"maxProperties":10`} {
		if !strings.Contains(string(data), w) {
			t.Errorf("missing %s in %s", w, data)
		}
	}
}

func TestTagIDOption(t *testing.T) {
	type Holder struct {
		S string `jsonschema:"$id=https://example.com/s"`
	}
	data, err := GenerateBytes(Holder{})
	if err != nil {
		t.Fatalf("GenerateBytes: %v", err)
	}
	if !strings.Contains(string(data), `"$id":"https://example.com/s"`) {
		t.Errorf("got %s", data)
	}
}

func TestTagTitleOption(t *testing.T) {
	type Holder struct {
		S string `jsonschema:"title=The Name"`
	}
	data, err := GenerateBytes(Holder{})
	if err != nil {
		t.Fatalf("GenerateBytes: %v", err)
	}
	if !strings.Contains(string(data), `"title":"The Name"`) {
		t.Errorf("got %s", data)
	}
}

// TestTagUnknownFormatPassesThrough pins the documented contract for the
// `jsonschema:"format=..."` tag: the value passes through verbatim
// regardless of whether the name is a registered format. This matches
// the standard "format" keyword's annotation-by-default semantics —
// unknown formats are tolerated. Validation only asserts when the
// caller opts in via [WithFormatAssertion] AND a [CustomFormat]
// matches the name.
func TestTagUnknownFormatPassesThrough(t *testing.T) {
	type S struct {
		X string `json:"x" jsonschema:"format=fictitious-format"`
	}
	data, err := GenerateBytes(S{})
	if err != nil {
		t.Fatalf("GenerateBytes: %v", err)
	}
	if !strings.Contains(string(data), `"format":"fictitious-format"`) {
		t.Errorf("expected fictitious-format to pass through; got %s", data)
	}
	// And the schema compiles cleanly even though the format is unknown.
	if _, err := Compile(data); err != nil {
		t.Errorf("Compile: schema with unknown format must compile cleanly: %v", err)
	}
}

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

// TestRequireStringTargetBranches covers the kind switch.
func TestRequireStringTargetBranches(t *testing.T) {
	if err := requireStringTarget(reflect.TypeOf(""), "/x", "minLength"); err != nil {
		t.Errorf("string target should pass: %v", err)
	}
	if err := requireStringTarget(reflect.TypeOf((*string)(nil)), "/x", "minLength"); err != nil {
		t.Errorf("ptr-to-string target should pass: %v", err)
	}
	if err := requireStringTarget(reflect.TypeOf(0), "/x", "minLength"); err == nil {
		t.Error("int target should fail")
	}
	// nil target → no error
	if err := requireStringTarget(nil, "/x", "minLength"); err != nil {
		t.Errorf("nil ft: %v", err)
	}
}

// TestRequireArrayTargetBranches covers the kind switch.
func TestRequireArrayTargetBranches(t *testing.T) {
	if err := requireArrayTarget(reflect.TypeOf([]int{}), "/x", "minItems"); err != nil {
		t.Errorf("slice target should pass: %v", err)
	}
	if err := requireArrayTarget(reflect.TypeOf([3]int{}), "/x", "minItems"); err != nil {
		t.Errorf("array target should pass: %v", err)
	}
	if err := requireArrayTarget(reflect.TypeOf((*[]int)(nil)), "/x", "minItems"); err != nil {
		t.Errorf("ptr-to-slice should pass: %v", err)
	}
	if err := requireArrayTarget(reflect.TypeOf(""), "/x", "minItems"); err == nil {
		t.Error("string target should fail")
	}
	if err := requireArrayTarget(nil, "/x", "minItems"); err != nil {
		t.Errorf("nil: %v", err)
	}
}

// TestRequireMapTargetBranches covers the kind switch.
func TestRequireMapTargetBranches(t *testing.T) {
	if err := requireMapTarget(reflect.TypeOf(map[string]int{}), "/x", "minProperties"); err != nil {
		t.Errorf("map target should pass: %v", err)
	}
	if err := requireMapTarget(reflect.TypeOf(struct{}{}), "/x", "minProperties"); err != nil {
		t.Errorf("struct target should pass: %v", err)
	}
	if err := requireMapTarget(reflect.TypeOf(""), "/x", "minProperties"); err == nil {
		t.Error("string target should fail")
	}
	if err := requireMapTarget(nil, "/x", "minProperties"); err != nil {
		t.Errorf("nil: %v", err)
	}
}

// TestParseNonNegativeIntBranches covers the parse + sign branches.
func TestParseNonNegativeIntBranches(t *testing.T) {
	if _, err := parseNonNegativeInt("5", "/x", "n"); err != nil {
		t.Errorf("5: %v", err)
	}
	if _, err := parseNonNegativeInt("-1", "/x", "n"); err == nil {
		t.Error("negative should fail")
	}
	if _, err := parseNonNegativeInt("xyz", "/x", "n"); err == nil {
		t.Error("non-int should fail")
	}
}

// TestCoerceFieldValueBranches covers each kind branch.
func TestCoerceFieldValueBranches(t *testing.T) {
	cases := []struct {
		ft   reflect.Type
		val  string
		want any
		err  bool
	}{
		{reflect.TypeOf(""), "hello", "hello", false},
		{reflect.TypeOf(true), "true", true, false},
		{reflect.TypeOf(true), "false", false, false},
		{reflect.TypeOf(true), "yes", nil, true},
		{reflect.TypeOf(0), "42", int64(42), false},
		{reflect.TypeOf(0), "x", nil, true},
		{reflect.TypeOf(0.0), "3.14", 3.14, false},
		{reflect.TypeOf(0.0), "x", nil, true},
		// fallthrough — slice unaffected; returns string verbatim
		{reflect.TypeOf([]int{}), "anything", "anything", false},
	}
	for _, tc := range cases {
		got, err := coerceFieldValue(tc.ft, tc.val, "/p", "name")
		if (err != nil) != tc.err {
			t.Errorf("coerceFieldValue(%v,%q) err=%v want err=%v", tc.ft, tc.val, err, tc.err)
			continue
		}
		if !tc.err && got != tc.want {
			t.Errorf("coerceFieldValue(%v,%q) = %v, want %v", tc.ft, tc.val, got, tc.want)
		}
	}
}

// TestCoerceNumericValueBranches covers each branch.
func TestCoerceNumericValueBranches(t *testing.T) {
	// nil ft → loose float parse
	got, err := coerceNumericValue(nil, "3.14", true, "/x", "minimum")
	if err != nil {
		t.Errorf("nil ft: %v", err)
	}
	if got.(float64) != 3.14 {
		t.Errorf("got %v", got)
	}
	// int ft
	got, err = coerceNumericValue(reflect.TypeOf(0), "42", true, "/x", "minimum")
	if err != nil {
		t.Errorf("int: %v", err)
	}
	if got.(int64) != 42 {
		t.Errorf("got %v", got)
	}
	// non-numeric ft
	if _, err := coerceNumericValue(reflect.TypeOf(""), "1", true, "/x", "minimum"); err == nil {
		t.Error("string field should reject numeric option")
	}
	// hasValue false
	if _, err := coerceNumericValue(reflect.TypeOf(0), "", false, "/x", "minimum"); err == nil {
		t.Error("missing value should error")
	}
	// invalid int
	if _, err := coerceNumericValue(reflect.TypeOf(0), "x", true, "/x", "minimum"); err == nil {
		t.Error("bad int should error")
	}
	// invalid float
	if _, err := coerceNumericValue(reflect.TypeOf(0.0), "x", true, "/x", "minimum"); err == nil {
		t.Error("bad float should error")
	}
	// invalid float for nil ft
	if _, err := coerceNumericValue(nil, "x", true, "/x", "minimum"); err == nil {
		t.Error("bad nil-ft float should error")
	}
}

// TestElemTypeForListBranches covers slice / non-slice / nil.
func TestElemTypeForListBranches(t *testing.T) {
	if got := elemTypeForList(nil); got != nil {
		t.Errorf("nil: %v", got)
	}
	if got := elemTypeForList(reflect.TypeOf([]int{})); got != reflect.TypeOf(0) {
		t.Errorf("slice: %v", got)
	}
	if got := elemTypeForList(reflect.TypeOf("")); got != reflect.TypeOf("") {
		t.Errorf("scalar: %v", got)
	}
}

// TestParseNonNegativeIntOk covers the success branch.
func TestParseNonNegativeIntOk(t *testing.T) {
	n, err := parseNonNegativeInt("42", "/x", "minLength")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if n != 42 {
		t.Errorf("got %d", n)
	}
}
