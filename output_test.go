package jsonschema

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// outputTestSchema is the schema used by the per-format golden tests. It
// has multiple keywords (allOf, properties, minLength, title) so the
// rendered output exercises both errors and annotations.
var outputTestSchema = []byte(`{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "title": "Person",
  "type": "object",
  "properties": {
    "name": {
      "type": "string",
      "minLength": 3,
      "title": "Name field"
    },
    "age": {
      "type": "integer",
      "minimum": 0
    }
  },
  "required": ["name", "age"]
}`)

// outputTestInstance is a value that fails the schema (name too short).
var outputTestInstance = []byte(`{"name": "x", "age": 30}`)

// outputTestValidInstance is a value that passes the schema.
var outputTestValidInstance = []byte(`{"name": "Alice", "age": 30}`)

func TestOutputFlag(t *testing.T) {
	s := mustCompile(t, outputTestSchema)
	res, err := s.Validate(outputTestInstance)
	if err != nil {
		t.Fatal(err)
	}
	got := string(res.Output(OutputFlag))
	if got != `{"valid":false}` {
		t.Errorf("flag(invalid) = %s", got)
	}

	res2, err := s.Validate(outputTestValidInstance)
	if err != nil {
		t.Fatal(err)
	}
	got2 := string(res2.Output(OutputFlag))
	if got2 != `{"valid":true}` {
		t.Errorf("flag(valid) = %s", got2)
	}
}

func TestOutputBasicShape(t *testing.T) {
	s := mustCompile(t, outputTestSchema)
	res, err := s.Validate(outputTestInstance)
	if err != nil {
		t.Fatal(err)
	}
	out := res.Output(OutputBasic)
	var doc map[string]any
	if err := json.Unmarshal(out, &doc); err != nil {
		t.Fatalf("basic output is not valid JSON: %v\noutput: %s", err, out)
	}
	if doc["valid"] != false {
		t.Errorf("basic.valid = %v, want false", doc["valid"])
	}
	if _, ok := doc["errors"]; !ok {
		t.Errorf("basic missing errors slot: %s", out)
	}
	if doc["keywordLocation"] != "" {
		t.Errorf("basic.keywordLocation = %v, want empty", doc["keywordLocation"])
	}
	if doc["instanceLocation"] != "" {
		t.Errorf("basic.instanceLocation = %v, want empty", doc["instanceLocation"])
	}
}

func TestOutputBasicValid(t *testing.T) {
	s := mustCompile(t, outputTestSchema)
	res, err := s.Validate(outputTestValidInstance)
	if err != nil {
		t.Fatal(err)
	}
	out := res.Output(OutputBasic)
	var doc map[string]any
	if err := json.Unmarshal(out, &doc); err != nil {
		t.Fatalf("basic(valid) parse: %v", err)
	}
	if doc["valid"] != true {
		t.Errorf("basic(valid).valid = %v, want true", doc["valid"])
	}
	// annotations should include "title"
	annos, ok := doc["annotations"].([]any)
	if !ok || len(annos) == 0 {
		t.Errorf("basic(valid) missing annotations: %s", out)
	}
}

func TestOutputDetailedShape(t *testing.T) {
	s := mustCompile(t, outputTestSchema)
	res, err := s.Validate(outputTestInstance)
	if err != nil {
		t.Fatal(err)
	}
	out := res.Output(OutputDetailed)
	var doc map[string]any
	if err := json.Unmarshal(out, &doc); err != nil {
		t.Fatalf("detailed parse: %v\n%s", err, out)
	}
	if doc["valid"] != false {
		t.Errorf("detailed.valid = %v, want false", doc["valid"])
	}
	// Walk the tree and confirm a leaf with `error` exists.
	if !hasErrorLeaf(doc) {
		t.Errorf("detailed missing error leaf: %s", out)
	}
}

func TestOutputVerboseShape(t *testing.T) {
	s := mustCompile(t, outputTestSchema)
	res, err := s.Validate(outputTestInstance)
	if err != nil {
		t.Fatal(err)
	}
	out := res.Output(OutputVerbose)
	var doc map[string]any
	if err := json.Unmarshal(out, &doc); err != nil {
		t.Fatalf("verbose parse: %v\n%s", err, out)
	}
	if doc["valid"] != false {
		t.Errorf("verbose.valid = %v", doc["valid"])
	}
}

func TestOutputDeeplyNestedAllOf(t *testing.T) {
	schema := []byte(`{
  "allOf": [
    {"allOf": [{"minimum": 5}]},
    {"type": "integer"}
  ]
}`)
	s := mustCompile(t, schema)
	res, err := s.ValidateValue(json.Number("3"))
	if err != nil {
		t.Fatal(err)
	}
	if res.Valid {
		t.Fatalf("expected invalid result")
	}
	det := res.Output(OutputDetailed)
	var doc map[string]any
	if err := json.Unmarshal(det, &doc); err != nil {
		t.Fatalf("detailed parse: %v", err)
	}
	if !hasErrorLeaf(doc) {
		t.Errorf("detailed deeply-nested allOf missing error leaf: %s", det)
	}
}

func TestOutputSingleErrorBasicVsDetailed(t *testing.T) {
	schema := []byte(`{"minimum": 5}`)
	s := mustCompile(t, schema)
	res, err := s.ValidateValue(json.Number("3"))
	if err != nil {
		t.Fatal(err)
	}
	basic := res.Output(OutputBasic)
	detailed := res.Output(OutputDetailed)
	// Basic and Detailed must differ — Basic is flat, Detailed is nested.
	if bytes.Equal(basic, detailed) {
		t.Errorf("basic and detailed should differ for single error")
	}
}

func TestOutputEmptyResult(t *testing.T) {
	r := &Result{Valid: true}
	for _, f := range []OutputFormat{OutputFlag, OutputBasic, OutputDetailed, OutputVerbose} {
		out := r.Output(f)
		var v any
		if err := json.Unmarshal(out, &v); err != nil {
			t.Errorf("Output(%s) on empty result: invalid JSON: %v\n%s", f, err, out)
		}
	}
}

func TestOutputMetaSchemaCompiles(t *testing.T) {
	s := OutputMetaSchema()
	if s == nil {
		t.Fatal("OutputMetaSchema() returned nil; embedded fixture failed to compile")
	}
}

func TestOutputValidatesAgainstMetaSchema(t *testing.T) {
	meta := OutputMetaSchema()
	if meta == nil {
		t.Fatal("OutputMetaSchema() nil")
	}

	cases := []struct {
		name    string
		schema  []byte
		invalid []byte
		valid   []byte
	}{
		{
			name:    "person_invalid",
			schema:  outputTestSchema,
			invalid: outputTestInstance,
		},
		{
			name:   "person_valid",
			schema: outputTestSchema,
			valid:  outputTestValidInstance,
		},
		{
			name:    "minimum_only",
			schema:  []byte(`{"minimum": 5}`),
			invalid: []byte(`3`),
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := mustCompile(t, tc.schema)
			var instance []byte
			if tc.invalid != nil {
				instance = tc.invalid
			} else {
				instance = tc.valid
			}
			res, err := s.Validate(instance)
			if err != nil {
				t.Fatal(err)
			}
			for _, f := range []OutputFormat{OutputFlag, OutputBasic, OutputDetailed, OutputVerbose} {
				out := res.Output(f)
				vr, err := meta.Validate(out)
				if err != nil {
					t.Errorf("meta.Validate(%s): %v\noutput: %s", f, err, out)
					continue
				}
				if !vr.Valid {
					t.Errorf("output %s does not validate against meta-schema; errors=%d\noutput: %s\nfirst: %v",
						f, len(vr.Errors), out, vr.Errors)
				}
			}
		})
	}
}

func TestOutputGoldenFixtures(t *testing.T) {
	s := mustCompile(t, outputTestSchema)
	res, err := s.Validate(outputTestInstance)
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join("testdata", "output")
	cases := map[OutputFormat]string{
		OutputFlag:     "flag.json",
		OutputBasic:    "basic.json",
		OutputDetailed: "detailed.json",
		OutputVerbose:  "verbose.json",
	}
	for f, name := range cases {
		path := filepath.Join(dir, name)
		got := res.Output(f)
		expected, err := os.ReadFile(path)
		if err != nil {
			t.Errorf("read fixture %s: %v", path, err)
			continue
		}
		// Compare after deserialize → re-serialize so whitespace differences
		// in committed fixtures don't cause failures.
		if !equalJSON(t, got, expected) {
			t.Errorf("output %s mismatch\nfixture: %s\ngot:     %s",
				f, strings.TrimSpace(string(expected)), string(got))
		}
	}
}

func TestStopOnFirstErrorOneError(t *testing.T) {
	// Schema with multiple violations: missing required + min violation.
	schema := []byte(`{
  "type": "object",
  "required": ["a", "b", "c"],
  "properties": {
    "x": {"minimum": 5}
  }
}`)
	s := mustCompile(t, schema)
	instance := []byte(`{"x": 3}`)
	res, err := s.Validate(instance, WithStopOnFirstError(true))
	if err != nil {
		t.Fatal(err)
	}
	if res.Valid {
		t.Fatalf("expected invalid result")
	}
	if len(res.Errors) != 1 {
		t.Errorf("WithStopOnFirstError: got %d errors, want exactly 1\nerrors: %+v",
			len(res.Errors), res.Errors)
	}
	// Pin the keyword: the first error in deterministic schema-walk order
	// for this fixture is the "minimum" violation on /x. If a future
	// applicator refactor changes the walk order, update this expectation
	// in lockstep with the documented order.
	if len(res.Errors) > 0 && res.Errors[0].Keyword != "minimum" {
		t.Errorf("WithStopOnFirstError: first-error keyword = %q, want %q",
			res.Errors[0].Keyword, "minimum")
	}
	// Annotations should be skipped in stop-on-first-error mode.
	if len(res.Annotations) != 0 {
		t.Errorf("WithStopOnFirstError: annotations present (%d), want 0",
			len(res.Annotations))
	}
}

// TestOutputVerbosePassingShape exercises the verbose tree on a successful
// validation: the top-level "valid" must be true and the tree must have
// at least one body node so callers can introspect annotations from passing
// keywords.
func TestOutputVerbosePassingShape(t *testing.T) {
	s := mustCompile(t, outputTestSchema)
	res, err := s.Validate(outputTestValidInstance)
	if err != nil {
		t.Fatal(err)
	}
	out := res.Output(OutputVerbose)
	var doc map[string]any
	if err := json.Unmarshal(out, &doc); err != nil {
		t.Fatalf("verbose(passing) parse: %v\n%s", err, out)
	}
	if doc["valid"] != true {
		t.Errorf("verbose(passing).valid = %v, want true", doc["valid"])
	}
	// Verbose must contain a non-trivial tree even on success — at
	// minimum the keywordLocation/instanceLocation slots are present, and
	// the body has either annotations or nested groups. Detect by
	// confirming at least one of those sentinel keys exists.
	bodyKeys := []string{"annotations", "errors", "keywordLocation", "instanceLocation"}
	hit := false
	for _, k := range bodyKeys {
		if _, ok := doc[k]; ok {
			hit = true
			break
		}
	}
	if !hit {
		t.Errorf("verbose(passing) tree missing body keys; doc=%v", doc)
	}
}

func TestStopOnFirstErrorWithoutOption(t *testing.T) {
	schema := []byte(`{
  "type": "object",
  "required": ["a", "b", "c"]
}`)
	s := mustCompile(t, schema)
	instance := []byte(`{}`)
	res, err := s.Validate(instance)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Errors) < 2 {
		t.Errorf("default mode: got %d errors, expected >=2", len(res.Errors))
	}
}

// hasErrorLeaf walks doc looking for a node with a non-empty `error`
// string. Returns true when found.
func hasErrorLeaf(doc any) bool {
	switch v := doc.(type) {
	case map[string]any:
		if e, ok := v["error"].(string); ok && e != "" {
			return true
		}
		for _, child := range v {
			if hasErrorLeaf(child) {
				return true
			}
		}
	case []any:
		if slices.ContainsFunc(v, hasErrorLeaf) {
			return true
		}
	}
	return false
}

// equalJSON unmarshals a and b and reports whether the resulting Go values
// are deeply equal. Avoids whitespace / key-ordering false negatives.
func equalJSON(t *testing.T, a, b []byte) bool {
	t.Helper()
	var av, bv any
	if err := json.Unmarshal(a, &av); err != nil {
		t.Logf("equalJSON: unmarshal a: %v", err)
		return false
	}
	if err := json.Unmarshal(b, &bv); err != nil {
		t.Logf("equalJSON: unmarshal b: %v", err)
		return false
	}
	ab, _ := json.Marshal(av)
	bb, _ := json.Marshal(bv)
	return bytes.Equal(ab, bb)
}

func mustCompile(t *testing.T, src []byte) *Schema {
	t.Helper()
	s, err := Compile(src)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	return s
}

// BenchmarkValidateInvalid measures the cost of a failing validation
// without WithStopOnFirstError. The schema has multiple violations to
// magnify the difference vs the stop-early benchmark.
func BenchmarkValidateInvalid(b *testing.B) {
	schema := []byte(`{
  "type": "object",
  "required": ["a", "b", "c", "d", "e", "f"],
  "properties": {
    "x": {"minimum": 5},
    "y": {"minimum": 5},
    "z": {"minimum": 5}
  }
}`)
	s, err := Compile(schema)
	if err != nil {
		b.Fatal(err)
	}
	instance := []byte(`{"x": 1, "y": 2, "z": 3}`)
	for b.Loop() {
		_, _ = s.Validate(instance)
	}
}

// BenchmarkValidateInvalidWithStopOnFirstError measures the same workload
// with WithStopOnFirstError(true) so callers can see whether the option
// pays off in practice.
func BenchmarkValidateInvalidWithStopOnFirstError(b *testing.B) {
	schema := []byte(`{
  "type": "object",
  "required": ["a", "b", "c", "d", "e", "f"],
  "properties": {
    "x": {"minimum": 5},
    "y": {"minimum": 5},
    "z": {"minimum": 5}
  }
}`)
	s, err := Compile(schema)
	if err != nil {
		b.Fatal(err)
	}
	instance := []byte(`{"x": 1, "y": 2, "z": 3}`)
	for b.Loop() {
		_, _ = s.Validate(instance, WithStopOnFirstError(true))
	}
}

// refBenchSchema is a recursive schema that crosses $ref at every level, so
// the ref benchmarks exercise evaluation-path tracking.
const refBenchSchema = `{
  "$id": "https://example.com/tree.json",
  "$ref": "#/$defs/node",
  "$defs": {
    "node": {
      "type": "object",
      "properties": {
        "name": {"$ref": "#/$defs/name"},
        "children": {"type": "array", "items": {"$ref": "#/$defs/node"}}
      },
      "required": ["name"]
    },
    "name": {"type": "string", "minLength": 1}
  }
}`

// BenchmarkValidateRefs measures a passing validation through nested and
// recursive $ref.
func BenchmarkValidateRefs(b *testing.B) {
	s, err := Compile([]byte(refBenchSchema))
	if err != nil {
		b.Fatal(err)
	}
	instance := []byte(`{"name":"a","children":[{"name":"b","children":[{"name":"c"}]},{"name":"d"}]}`)
	for b.Loop() {
		_, _ = s.Validate(instance)
	}
}

// BenchmarkValidateRefsNoAnnotations is BenchmarkValidateRefs without
// public annotations.
func BenchmarkValidateRefsNoAnnotations(b *testing.B) {
	s, err := Compile([]byte(refBenchSchema))
	if err != nil {
		b.Fatal(err)
	}
	instance := []byte(`{"name":"a","children":[{"name":"b","children":[{"name":"c"}]},{"name":"d"}]}`)
	for b.Loop() {
		_, _ = s.Validate(instance, WithCollectAnnotations(false))
	}
}

// BenchmarkValidateRefsInvalid measures a failing validation whose errors
// sit below nested and recursive $ref.
func BenchmarkValidateRefsInvalid(b *testing.B) {
	s, err := Compile([]byte(refBenchSchema))
	if err != nil {
		b.Fatal(err)
	}
	instance := []byte(`{"name":"a","children":[{"name":1,"children":[{"name":""}]},{"name":"d"}]}`)
	for b.Loop() {
		_, _ = s.Validate(instance)
	}
}

// TestOutputUnrecognizedFormat covers the default-case fallthrough.
func TestOutputUnrecognizedFormat(t *testing.T) {
	r := &Result{Valid: true}
	// Use a numeric value outside the known OutputFormat enum.
	got := r.Output(OutputFormat(999))
	if string(got) != `{"valid":true}` {
		t.Errorf("got %s, want flag fallback", got)
	}
}

// TestOutputNilResult covers the nil-receiver branch.
func TestOutputNilResult(t *testing.T) {
	var r *Result
	got := r.Output(OutputFlag)
	if string(got) != `{"valid":false}` {
		t.Errorf("got %s, want %q", got, `{"valid":false}`)
	}
}

// TestOutputBasicErrorWithoutMessage exercises errorMessage's
// keyword-fallback branch.
func TestOutputBasicErrorWithoutMessage(t *testing.T) {
	r := &Result{
		Valid: false,
		Errors: []ValidationError{{
			KeywordLocation: "/x",
			Keyword:         "type",
		}},
	}
	got := r.Output(OutputBasic)
	var doc map[string]any
	if err := json.Unmarshal(got, &doc); err != nil {
		t.Fatalf("parse: %v", err)
	}
	errs, ok := doc["errors"].([]any)
	if !ok || len(errs) < 2 {
		t.Fatalf("missing errors: %v", doc)
	}
	// 1st is header; 2nd is our entry.
	entry := errs[1].(map[string]any)
	msg, _ := entry["error"].(string)
	if !strings.Contains(msg, "type") {
		t.Errorf("error msg = %q, want fallback containing 'type'", msg)
	}
}

// TestOutputBasicErrorTotallyEmpty exercises the keyword-empty + msg-empty
// fallback in errorMessage.
func TestOutputBasicErrorTotallyEmpty(t *testing.T) {
	r := &Result{
		Valid:  false,
		Errors: []ValidationError{{KeywordLocation: "/x"}},
	}
	out := r.Output(OutputBasic)
	if !strings.Contains(string(out), "validation failed") {
		t.Errorf("missing fallback msg: %s", out)
	}
}

// TestOutputDetailedNormalizesKeywordLocation covers the "#"-prefix
// stripping branch via a populated keywordLocation.
func TestOutputDetailedNormalizesKeywordLocation(t *testing.T) {
	r := &Result{
		Valid: false,
		Errors: []ValidationError{{
			KeywordLocation: "#/properties/x/minLength",
			Keyword:         "minLength",
			Message:         "x",
		}},
	}
	out := r.Output(OutputDetailed)
	// "#" should be stripped.
	if strings.Contains(string(out), `"keywordLocation":"#/`) {
		t.Errorf("keywordLocation not normalized: %s", out)
	}
}

// TestOutputDetailedRootKeywordLocation covers normalizeKeywordLocation
// for "#" exactly.
func TestOutputDetailedRootKeywordLocation(t *testing.T) {
	if got := normalizeKeywordLocation("#"); got != "" {
		t.Errorf("got %q, want empty", got)
	}
	if got := normalizeKeywordLocation(""); got != "" {
		t.Errorf("got %q, want empty", got)
	}
	if got := normalizeKeywordLocation("/foo"); got != "/foo" {
		t.Errorf("got %q, want /foo", got)
	}
}

// TestAbsoluteKeywordLocation checks the canonical-URI form of keyword
// locations on errors, annotations and rendered output.
func TestAbsoluteKeywordLocation(t *testing.T) {
	s := MustCompile([]byte(`{
		"$id":"https://example.com/root.json",
		"properties":{
			"ptr":{"$ref":"#/$defs/s"},
			"anchor":{"$ref":"#named"},
			"other":{"$ref":"other.json"},
			"inline":{"minimum":3},
			"nested":{"$id":"nested.json","maxLength":1},
			"a b":{"type":"null"}
		},
		"$defs":{
			"s":{"type":"string","title":"S"},
			"n":{"$anchor":"named","type":"boolean"},
			"o":{"$id":"other.json","type":"integer"}
		}
	}`))
	res, err := s.Validate([]byte(`{"ptr":1,"anchor":1,"other":"x","inline":1,"nested":"xx","a b":1}`))
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"/ptr":    "https://example.com/root.json#/$defs/s/type",
		"/anchor": "https://example.com/root.json#/$defs/n/type",
		"/other":  "https://example.com/other.json#/type",
		"/inline": "https://example.com/root.json#/properties/inline/minimum",
		"/nested": "https://example.com/nested.json#/maxLength",
		"/a b":    "https://example.com/root.json#/properties/a%20b/type",
	}
	got := map[string]string{}
	for _, e := range res.Errors {
		if e.Keyword != "properties" {
			got[e.InstanceLocation] = e.AbsoluteKeywordLocation
		}
	}
	for inst, w := range want {
		if got[inst] != w {
			t.Errorf("instance %s: AbsoluteKeywordLocation = %q, want %q", inst, got[inst], w)
		}
	}
	if !bytes.Contains(res.Output(OutputBasic), []byte(`"absoluteKeywordLocation":"https://example.com/other.json#/type"`)) {
		t.Errorf("basic output lacks absoluteKeywordLocation: %s", res.Output(OutputBasic))
	}

	res, _ = s.Validate([]byte(`{"ptr":"x"}`), WithCollectAnnotations(true))
	found := false
	for _, a := range res.Annotations {
		if a.Keyword == "title" {
			found = a.AbsoluteKeywordLocation == "https://example.com/root.json#/$defs/s/title"
		}
	}
	if !found {
		t.Errorf("title annotation lacks the absolute keyword location: %+v", res.Annotations)
	}
}

// TestAbsoluteKeywordLocationUnknownBase leaves the field empty when the
// schema has no absolute base URI.
func TestAbsoluteKeywordLocationUnknownBase(t *testing.T) {
	res, _ := MustCompile([]byte(`{"type":"string"}`)).Validate([]byte(`1`))
	if len(res.Errors) == 0 || res.Errors[0].AbsoluteKeywordLocation != "" {
		t.Errorf("errors = %+v, want one with no AbsoluteKeywordLocation", res.Errors)
	}
	res, _ = MustCompile([]byte(`{"type":"string"}`), WithBaseURI("https://example.com/s.json")).Validate([]byte(`1`))
	if len(res.Errors) == 0 || res.Errors[0].AbsoluteKeywordLocation != "https://example.com/s.json#/type" {
		t.Errorf("errors = %+v, want AbsoluteKeywordLocation from the base URI", res.Errors)
	}
}
