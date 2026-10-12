package jsonschema

import (
	"bytes"
	"strings"
	"testing"
)

// findError returns the first error in errs (searching Causes depth-first)
// whose Keyword is keyword.
func findError(errs []ValidationError, keyword string) *ValidationError {
	for i := range errs {
		if errs[i].Keyword == keyword {
			return &errs[i]
		}
		if e := findError(errs[i].Causes, keyword); e != nil {
			return e
		}
	}
	return nil
}

// TestKeywordLocationThroughRefs checks that KeywordLocation is the
// evaluation path, naming each reference keyword crossed, while
// AbsoluteKeywordLocation stays the dereferenced location.
func TestKeywordLocationThroughRefs(t *testing.T) {
	cases := []struct {
		name, schema, instance, keyword, want, wantAbs string
	}{
		{
			name:     "$ref with $id",
			schema:   `{"$id":"https://x/root.json","properties":{"a":{"$ref":"#/$defs/s"}},"$defs":{"s":{"type":"string"}}}`,
			instance: `{"a":1}`, keyword: "type",
			want:    "#/properties/a/$ref/type",
			wantAbs: "https://x/root.json#/$defs/s/type",
		},
		{
			name:     "$ref without $id",
			schema:   `{"properties":{"a":{"$ref":"#/$defs/s"}},"$defs":{"s":{"type":"string"}}}`,
			instance: `{"a":1}`, keyword: "type",
			want: "#/properties/a/$ref/type",
		},
		{
			name:     "$ref to another resource",
			schema:   `{"$id":"https://x/root.json","properties":{"a":{"$ref":"other.json"}},"$defs":{"o":{"$id":"other.json","type":"integer"}}}`,
			instance: `{"a":"x"}`, keyword: "type",
			want:    "#/properties/a/$ref/type",
			wantAbs: "https://x/other.json#/type",
		},
		{
			name:     "nested $ref",
			schema:   `{"$id":"https://x/root.json","properties":{"a":{"$ref":"#/$defs/b"}},"$defs":{"b":{"$ref":"#/$defs/c"},"c":{"items":{"$ref":"#/$defs/s"}},"s":{"type":"string"}}}`,
			instance: `{"a":[1]}`, keyword: "type",
			want:    "#/properties/a/$ref/$ref/items/$ref/type",
			wantAbs: "https://x/root.json#/$defs/s/type",
		},
		{
			name:     "recursive $ref",
			schema:   refBenchSchema,
			instance: `{"name":"a","children":[{"name":"b","children":[{"name":""}]}]}`, keyword: "minLength",
			want:    "#/$ref/properties/children/items/$ref/properties/children/items/$ref/properties/name/$ref/minLength",
			wantAbs: "https://example.com/tree.json#/$defs/name/minLength",
		},
		{
			name:     "$dynamicRef",
			schema:   `{"$id":"https://x/t","$dynamicAnchor":"n","type":"object","properties":{"a":{"$dynamicRef":"#n"}}}`,
			instance: `{"a":{"a":1}}`, keyword: "type",
			want:    "#/properties/a/$dynamicRef/properties/a/$dynamicRef/type",
			wantAbs: "https://x/t#/type",
		},
		{
			name: "$dynamicRef to an extending schema",
			schema: `{"$id":"https://x/ext","$ref":"base","$defs":{"node":{"$dynamicAnchor":"node","type":"string"},` +
				`"base":{"$id":"base","$defs":{"node":{"$dynamicAnchor":"node"}},"items":{"$dynamicRef":"#node"}}}}`,
			instance: `[1]`, keyword: "type",
			want:    "#/$ref/items/$dynamicRef/type",
			wantAbs: "https://x/ext#/$defs/node/type",
		},
		{
			name:     "$recursiveRef",
			schema:   `{"$schema":"https://json-schema.org/draft/2019-09/schema","$id":"https://x/r","$recursiveAnchor":true,"type":"object","properties":{"a":{"$recursiveRef":"#"}}}`,
			instance: `{"a":{"a":1}}`, keyword: "type",
			want:    "#/properties/a/$recursiveRef/properties/a/$recursiveRef/type",
			wantAbs: "https://x/r#/type",
		},
		{
			name:     "false schema target",
			schema:   `{"properties":{"a":{"$ref":"#/$defs/f"}},"$defs":{"f":false}}`,
			instance: `{"a":1}`, keyword: "false",
			want: "#/properties/a/$ref",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res, err := MustCompile([]byte(tc.schema)).Validate([]byte(tc.instance))
			if err != nil {
				t.Fatal(err)
			}
			e := findError(res.Errors, tc.keyword)
			if e == nil {
				t.Fatalf("no %s error in %+v", tc.keyword, res.Errors)
			}
			if e.KeywordLocation != tc.want {
				t.Errorf("KeywordLocation = %q, want %q", e.KeywordLocation, tc.want)
			}
			if e.AbsoluteKeywordLocation != tc.wantAbs {
				t.Errorf("AbsoluteKeywordLocation = %q, want %q", e.AbsoluteKeywordLocation, tc.wantAbs)
			}
		})
	}
}

// TestKeywordLocationApplicatorsThroughRefs crosses a $ref under each
// applicator and checks the evaluation path names both.
func TestKeywordLocationApplicatorsThroughRefs(t *testing.T) {
	cases := []struct {
		name, body, instance, keyword, want string
	}{
		{"allOf", `"allOf":[{"$ref":"#/$defs/s"}]`, `1`, "type", "#/allOf/0/$ref/type"},
		{"anyOf", `"anyOf":[{"$ref":"#/$defs/s"}]`, `1`, "type", "#/anyOf/0/$ref/type"},
		{"oneOf", `"oneOf":[{"$ref":"#/$defs/s"},false]`, `1`, "type", "#/oneOf/0/$ref/type"},
		{"not", `"$ref":"#/$defs/w","$defs":{"s":{"type":"string"},"w":{"not":{"type":"integer"}}}`, `1`, "not", "#/$ref/not"},
		{"then", `"if":true,"then":{"$ref":"#/$defs/s"}`, `1`, "type", "#/then/$ref/type"},
		{"else", `"if":false,"else":{"$ref":"#/$defs/s"}`, `1`, "type", "#/else/$ref/type"},
		{"items", `"items":{"$ref":"#/$defs/s"}`, `[1]`, "type", "#/items/$ref/type"},
		{"prefixItems", `"prefixItems":[{"$ref":"#/$defs/s"}]`, `[1]`, "type", "#/prefixItems/0/$ref/type"},
		{"contains", `"$ref":"#/$defs/w","$defs":{"s":{"type":"string"},"w":{"contains":{"$ref":"#/$defs/s"}}}`, `[1]`, "contains", "#/$ref/contains"},
		{"properties", `"properties":{"p":{"$ref":"#/$defs/s"}}`, `{"p":1}`, "type", "#/properties/p/$ref/type"},
		{"patternProperties", `"patternProperties":{"^p":{"$ref":"#/$defs/s"}}`, `{"p":1}`, "type", "#/patternProperties/^p/$ref/type"},
		{"additionalProperties", `"additionalProperties":{"$ref":"#/$defs/s"}`, `{"p":1}`, "type", "#/additionalProperties/$ref/type"},
		{"propertyNames", `"propertyNames":{"$ref":"#/$defs/n"},"$defs":{"s":{},"n":{"maxLength":1}}`, `{"pp":1}`, "maxLength", "#/propertyNames/$ref/maxLength"},
		{"dependentSchemas", `"dependentSchemas":{"p":{"properties":{"p":{"$ref":"#/$defs/s"}}}}`, `{"p":1}`, "type", "#/dependentSchemas/p/properties/p/$ref/type"},
		{"unevaluatedProperties", `"unevaluatedProperties":{"$ref":"#/$defs/s"}`, `{"p":1}`, "type", "#/unevaluatedProperties/$ref/type"},
		{"unevaluatedItems", `"unevaluatedItems":{"$ref":"#/$defs/s"}`, `[1]`, "type", "#/unevaluatedItems/$ref/type"},
		{"applicator inside target", `"$ref":"#/$defs/w","$defs":{"s":{"type":"string"},"w":{"allOf":[{"items":{"$ref":"#/$defs/s"}}]}}`, `[1]`, "type", "#/$ref/allOf/0/items/$ref/type"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			schema := `{` + tc.body + `}`
			if !strings.Contains(tc.body, `"$defs"`) {
				schema = `{"$defs":{"s":{"type":"string"}},` + tc.body + `}`
			}
			res, err := MustCompile([]byte(schema)).Validate([]byte(tc.instance))
			if err != nil {
				t.Fatal(err)
			}
			e := findError(res.Errors, tc.keyword)
			if e == nil {
				t.Fatalf("no %s error in %+v", tc.keyword, res.Errors)
			}
			if e.KeywordLocation != tc.want {
				t.Errorf("KeywordLocation = %q, want %q", e.KeywordLocation, tc.want)
			}
		})
	}
}

// TestAnnotationKeywordLocationThroughRefs checks annotation locations
// below one and two references, and inside a branch merged after the
// reference returns.
func TestAnnotationKeywordLocationThroughRefs(t *testing.T) {
	s := MustCompile([]byte(`{
		"$id":"https://x/a.json",
		"properties":{
			"a":{"$ref":"#/$defs/t"},
			"b":{"$ref":"#/$defs/u"},
			"c":{"anyOf":[{"$ref":"#/$defs/t"}]}
		},
		"$defs":{"t":{"title":"T"},"u":{"$ref":"#/$defs/t"}}
	}`))
	res, err := s.Validate([]byte(`{"a":1,"b":1,"c":1}`))
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, a := range res.Annotations {
		if a.Keyword == "title" {
			got[a.InstanceLocation] = a.KeywordLocation + " " + a.AbsoluteKeywordLocation
		}
	}
	want := map[string]string{
		"/a": "#/properties/a/$ref/title https://x/a.json#/$defs/t/title",
		"/b": "#/properties/b/$ref/$ref/title https://x/a.json#/$defs/t/title",
		"/c": "#/properties/c/anyOf/0/$ref/title https://x/a.json#/$defs/t/title",
	}
	for inst, w := range want {
		if got[inst] != w {
			t.Errorf("instance %s: title annotation = %q, want %q", inst, got[inst], w)
		}
	}
}

// TestOutputTreeThroughRefs checks that every output format reports the
// evaluation path and that the detailed and verbose trees nest by it.
func TestOutputTreeThroughRefs(t *testing.T) {
	s := MustCompile([]byte(`{"$id":"https://x/root.json","properties":{"a":{"$ref":"#/$defs/s"}},"$defs":{"s":{"type":"string"}}}`))
	res, err := s.Validate([]byte(`{"a":1}`))
	if err != nil {
		t.Fatal(err)
	}
	leaf := `{"valid":false,"keywordLocation":"/properties/a/$ref/type","absoluteKeywordLocation":"https://x/root.json#/$defs/s/type","instanceLocation":"/a","error":"value is not of type string"}`
	want := map[OutputFormat]string{
		OutputFlag:     `{"valid":false}`,
		OutputBasic:    `{"valid":false,"keywordLocation":"","instanceLocation":"","errors":[{"valid":false,"keywordLocation":"","instanceLocation":"","error":"validation failed"},` + leaf + `]}`,
		OutputDetailed: `{"valid":false,"keywordLocation":"","instanceLocation":"","errors":[` + leaf + `]}`,
		OutputVerbose: `{"valid":false,"keywordLocation":"","instanceLocation":"","errors":[` +
			`{"valid":false,"keywordLocation":"/properties","instanceLocation":"/a","errors":[` +
			`{"valid":false,"keywordLocation":"/properties/a","instanceLocation":"/a","errors":[` +
			`{"valid":false,"keywordLocation":"/properties/a/$ref","instanceLocation":"/a","errors":[` + leaf + `]}]}]}],` +
			`"annotations":[{"valid":true,"keywordLocation":"/properties","absoluteKeywordLocation":"https://x/root.json#/properties","instanceLocation":"","annotation":["a"]}]}`,
	}
	for f, w := range want {
		if got := string(res.Output(f)); got != w {
			t.Errorf("%s output:\n got %s\nwant %s", f, got, w)
		}
	}
}

// TestOutputNoInternalLocationKeys checks that no output format exposes the
// evaluator's internal location keys for $dynamicRef / $recursiveRef or an
// absolute URI as a keywordLocation.
func TestOutputNoInternalLocationKeys(t *testing.T) {
	schemas := []string{
		`{"$id":"https://x/t","$dynamicAnchor":"n","type":"object","properties":{"a":{"$dynamicRef":"#n"},"t":{"title":"T"}}}`,
		`{"$schema":"https://json-schema.org/draft/2019-09/schema","$id":"https://x/r","$recursiveAnchor":true,"type":"object","properties":{"a":{"$recursiveRef":"#"},"t":{"title":"T"}}}`,
	}
	for _, src := range schemas {
		s := MustCompile([]byte(src))
		for _, inst := range []string{`{"a":{"a":1}}`, `{"a":{"a":{"t":1}}}`} {
			res, err := s.Validate([]byte(inst))
			if err != nil {
				t.Fatal(err)
			}
			for _, f := range []OutputFormat{OutputBasic, OutputDetailed, OutputVerbose} {
				out := res.Output(f)
				for _, bad := range []string{`"keywordLocation":"dyn:`, `"keywordLocation":"rec:`, `"keywordLocation":"https:`, `"keywordLocation":"/https:`} {
					if bytes.Contains(out, []byte(bad)) {
						t.Errorf("%s output for %s contains %s: %s", f, inst, bad, out)
					}
				}
				if vr, err := OutputMetaSchema().Validate(out); err != nil || !vr.Valid {
					t.Errorf("%s output does not validate against the output meta-schema: %s", f, out)
				}
			}
		}
	}
}

// TestRenderErrorSnippetThroughRef checks that the schema snippet follows a
// local $ref to the failing keyword.
func TestRenderErrorSnippetThroughRef(t *testing.T) {
	schema := []byte("{\n  \"properties\": {\"a\": {\"$ref\": \"#/$defs/s\"}},\n  \"$defs\": {\n    \"s\": {\"type\": \"string\"}\n  }\n}")
	instance := []byte(`{"a":1}`)
	err := MustCompile(schema).ValidateAndUnmarshal(instance, nil)
	if err == nil {
		t.Fatal("want a validation error")
	}
	out := RenderError(schema, instance, err)
	if !strings.Contains(out, "schema:   #/properties/a/$ref/type") {
		t.Errorf("render lacks the evaluation path:\n%s", out)
	}
	if !strings.Contains(out, "schema (line 4):") {
		t.Errorf("render lacks the snippet at the $ref target:\n%s", out)
	}
}

func TestFollowLocalRefs(t *testing.T) {
	src := []byte(`{"properties":{"$ref":{"$ref":"#"},"r":{"$ref":"other.json"},"d":{"$ref":"#/$defs/a~1b"}},"$defs":{"a/b":{"type":"string"}}}`)
	cases := []struct {
		in, want string
		ok       bool
	}{
		{"#/type", "#/type", true},
		{"#/properties/d/$ref/type", "/$defs/a~1b/type", true},
		{"#/properties/$ref/$ref", "", true},
		{"#/properties/r/$ref/type", "", false},
		{"#/properties/missing/$ref", "#/properties/missing/$ref", true},
	}
	for _, tc := range cases {
		got, ok := followLocalRefs(src, tc.in)
		if got != tc.want || ok != tc.ok {
			t.Errorf("followLocalRefs(%q) = %q, %v; want %q, %v", tc.in, got, ok, tc.want, tc.ok)
		}
	}
}
