package jsonschema

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

// TestAnnotationScoping checks that additionalProperties, items and
// unevaluated* see only the annotations of their own subschema and its
// passing in-place subschemas, never those of a parent or a sibling.
func TestAnnotationScoping(t *testing.T) {
	cases := []struct {
		schema, instance string
		want             bool
	}{
		{`{"properties":{"a":{}},"allOf":[{"additionalProperties":false}]}`, `{"a":1}`, false},
		{`{"prefixItems":[{}],"allOf":[{"items":false}]}`, `[1]`, false},
		{`{"properties":{"a":{}},"allOf":[{"unevaluatedProperties":false}]}`, `{"a":1}`, false},
		{`{"allOf":[{"properties":{"a":{}}},{"unevaluatedProperties":false}]}`, `{"a":1}`, false},
		{`{"dependentSchemas":{"a":{"properties":{"b":{}}}},"unevaluatedProperties":false}`, `{"a":1,"b":1}`, false},
		{`{"properties":{"a":{}},"dependentSchemas":{"a":{"properties":{"b":{}}}},"unevaluatedProperties":false}`, `{"a":1,"b":1}`, true},
		{`{"allOf":[{"properties":{"a":{}}}],"unevaluatedProperties":false}`, `{"a":1}`, true},
		{`{"anyOf":[{"properties":{"a":{}},"required":["a"]},{"properties":{"b":{}},"required":["b"]}],"unevaluatedProperties":false}`, `{"a":1,"b":1}`, true},
		{`{"anyOf":[{"properties":{"a":{}},"required":["a"]},{"properties":{"b":{}},"required":["x"]}],"unevaluatedProperties":false}`, `{"a":1,"b":1}`, false},
		{`{"not":{"not":{"properties":{"a":{}}}},"unevaluatedProperties":false}`, `{"a":1}`, false},
		{`{"prefixItems":[{}],"items":false}`, `[1]`, true},
		{`{"properties":{"a":{}},"patternProperties":{"^b":{}},"additionalProperties":false}`, `{"a":1,"bc":2}`, true},
		{`{"properties":{"a":{}},"patternProperties":{"^b":{}},"additionalProperties":false}`, `{"a":1,"c":2}`, false},
	}
	for _, tc := range cases {
		if got := validOf(t, tc.schema, tc.instance); got != tc.want {
			t.Errorf("%s with %s: valid=%v, want %v", tc.schema, tc.instance, got, tc.want)
		}
	}
}

// TestNestedAnyOfIsNotExponential checks that anyOf stops at the first
// passing branch when nothing can read the other branches' annotations.
func TestNestedAnyOfIsNotExponential(t *testing.T) {
	// Build a chain of shared subschemas through $defs so the schema text
	// stays small while the evaluation tree is 2^depth.
	var defs strings.Builder
	const depth = 30
	defs.WriteString(`{"$defs":{"d0":{"type":"integer"}`)
	for i := 1; i <= depth; i++ {
		prev := `{"$ref":"#/$defs/d` + itoa(i-1) + `"}`
		fmt.Fprintf(&defs, `,"d%d":{"anyOf":[%s,%s]}`, i, prev, prev)
	}
	fmt.Fprintf(&defs, `},"$ref":"#/$defs/d%d"}`, depth)
	s := MustCompile([]byte(defs.String()), WithMaxRefDepth(1000))
	start := time.Now()
	res, err := s.Validate([]byte(`1`), WithCollectAnnotations(false))
	if err != nil || !res.Valid {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	if d := time.Since(start); d > 5*time.Second {
		t.Fatalf("validation took %v", d)
	}
}

// TestOneOfStopsAtSecondMatch checks the oneOf message once two branches
// pass.
func TestOneOfStopsAtSecondMatch(t *testing.T) {
	s := MustCompile([]byte(`{"oneOf":[{"type":"integer"},{"minimum":0},{"maximum":5}]}`))
	res, err := s.Validate([]byte(`1`))
	if err != nil || res.Valid {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	if got := res.Errors[0].Message; !strings.Contains(got, "indices 0 and 1") {
		t.Errorf("message = %q", got)
	}
}

// TestAnnotationValuesArePublicTypes checks that Result.Annotations carries
// the spec's annotation values, not internal types.
func TestAnnotationValuesArePublicTypes(t *testing.T) {
	s := MustCompile([]byte(`{"properties":{"b":{},"a":{}},"prefixItems":[{}],"items":{},"contains":{}}`))
	for _, inst := range []string{`{"a":1,"b":2}`, `[1,2]`} {
		res, err := s.Validate([]byte(inst))
		if err != nil {
			t.Fatal(err)
		}
		for _, a := range res.Annotations {
			switch v := a.Value.(type) {
			case []string:
				if a.Keyword == "properties" && strings.Join(v, ",") != "a,b" {
					t.Errorf("properties value = %v", v)
				}
			case int, bool:
			default:
				t.Errorf("%s annotation has type %T", a.Keyword, a.Value)
			}
		}
	}
}
