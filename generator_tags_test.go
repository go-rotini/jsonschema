package jsonschema

import (
	"errors"
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
