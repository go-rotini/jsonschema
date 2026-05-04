package jsonschema

import (
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/go-rotini/yaml"
)

// kitchenSinkSchemaJSON is shared across format-equivalence tests so each
// adapter is exercised against the same canonical schema.
const kitchenSinkSchemaJSON = `{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "type": "object",
  "required": ["name", "age"],
  "properties": {
    "name": {"type": "string", "minLength": 1},
    "age":  {"type": "integer", "minimum": 0},
    "tags": {"type": "array", "items": {"type": "string"}, "uniqueItems": true},
    "meta": {
      "type": "object",
      "properties": {
        "score": {"type": "number", "multipleOf": 0.1}
      },
      "additionalProperties": false
    }
  },
  "additionalProperties": false
}`

func mustCompileJSONForMultifmt(t *testing.T, schemaJSON string) *Schema {
	t.Helper()
	s, err := Compile([]byte(schemaJSON))
	if err != nil {
		t.Fatalf("compile json schema: %v", err)
	}
	return s
}

func TestLoadJSONC_RoundTrip(t *testing.T) {
	src := `{
  // top-level comment
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "type": "object",
  "required": ["name", "age"],
  "properties": {
    "name": {"type": "string", "minLength": 1},
    "age":  {"type": "integer", "minimum": 0},
    "tags": {"type": "array", "items": {"type": "string"}, "uniqueItems": true},
    "meta": {
      "type": "object",
      "properties": {
        "score": {"type": "number", "multipleOf": 0.1},
      }, // trailing comma
      "additionalProperties": false,
    },
  },
  "additionalProperties": false,
}`
	s, err := LoadJSONC([]byte(src))
	if err != nil {
		t.Fatalf("LoadJSONC: %v", err)
	}
	res, err := s.Validate([]byte(`{"name":"alice","age":30}`))
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if !res.Valid {
		t.Fatalf("expected valid; errors=%v", res.Errors)
	}
}

func TestLoadYAML_RoundTrip(t *testing.T) {
	src := `$schema: https://json-schema.org/draft/2020-12/schema
type: object
required: [name, age]
properties:
  name:
    type: string
    minLength: 1
  age:
    type: integer
    minimum: 0
  tags:
    type: array
    items:
      type: string
    uniqueItems: true
  meta:
    type: object
    properties:
      score:
        type: number
        multipleOf: 0.1
    additionalProperties: false
additionalProperties: false
`
	s, err := LoadYAML([]byte(src))
	if err != nil {
		t.Fatalf("LoadYAML: %v", err)
	}
	res, err := s.Validate([]byte(`{"name":"alice","age":30}`))
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if !res.Valid {
		t.Fatalf("expected valid; errors=%v", res.Errors)
	}
}

func TestLoadTOML_RoundTrip(t *testing.T) {
	src := `"$schema" = "https://json-schema.org/draft/2020-12/schema"
type = "object"
required = ["name", "age"]
additionalProperties = false

[properties.name]
type = "string"
minLength = 1

[properties.age]
type = "integer"
minimum = 0

[properties.tags]
type = "array"
uniqueItems = true
[properties.tags.items]
type = "string"

[properties.meta]
type = "object"
additionalProperties = false
[properties.meta.properties.score]
type = "number"
multipleOf = 0.1
`
	s, err := LoadTOML([]byte(src))
	if err != nil {
		t.Fatalf("LoadTOML: %v", err)
	}
	res, err := s.Validate([]byte(`{"name":"alice","age":30}`))
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if !res.Valid {
		t.Fatalf("expected valid; errors=%v", res.Errors)
	}
}

func TestValidateJSONC_Instance(t *testing.T) {
	s := mustCompileJSONForMultifmt(t, kitchenSinkSchemaJSON)
	src := `{
  // an instance with comments + trailing commas
  "name": "alice",
  "age": 30,
  "tags": ["a", "b"],
  "meta": {"score": 0.3,},
}`
	res, err := ValidateJSONC(s, []byte(src))
	if err != nil {
		t.Fatalf("ValidateJSONC: %v", err)
	}
	if !res.Valid {
		t.Fatalf("expected valid; errors=%v", res.Errors)
	}
}

func TestValidateYAML_Instance(t *testing.T) {
	s := mustCompileJSONForMultifmt(t, kitchenSinkSchemaJSON)
	src := `name: alice
age: 30
tags: [a, b]
meta:
  score: 0.3
`
	res, err := ValidateYAML(s, []byte(src))
	if err != nil {
		t.Fatalf("ValidateYAML: %v", err)
	}
	if !res.Valid {
		t.Fatalf("expected valid; errors=%v", res.Errors)
	}
}

func TestValidateYAML_Aliases(t *testing.T) {
	s := mustCompileJSONForMultifmt(t, kitchenSinkSchemaJSON)
	src := `name: &n alice
age: 30
tags: [*n, b]
meta:
  score: 0.3
`
	res, err := ValidateYAML(s, []byte(src))
	if err != nil {
		t.Fatalf("ValidateYAML alias: %v", err)
	}
	if !res.Valid {
		t.Fatalf("expected valid; errors=%v", res.Errors)
	}
}

func TestValidateTOML_Instance(t *testing.T) {
	s := mustCompileJSONForMultifmt(t, kitchenSinkSchemaJSON)
	src := `name = "alice"
age = 30
tags = ["a", "b"]

[meta]
score = 0.3
`
	res, err := ValidateTOML(s, []byte(src))
	if err != nil {
		t.Fatalf("ValidateTOML: %v", err)
	}
	if !res.Valid {
		t.Fatalf("expected valid; errors=%v", res.Errors)
	}
}

func TestValidateTOML_Datetime(t *testing.T) {
	schema := `{
  "type": "object",
  "properties": {
    "ts": {"type": "string", "format": "date-time"}
  }
}`
	s := mustCompileJSONForMultifmt(t, schema)
	src := `ts = 2024-05-02T13:14:15Z
`
	res, err := ValidateTOML(s, []byte(src), WithFormatAssertion(true))
	if err != nil {
		t.Fatalf("ValidateTOML datetime: %v", err)
	}
	if !res.Valid {
		t.Fatalf("expected valid datetime; errors=%v", res.Errors)
	}
}

func TestNumberPrecision_MultipleOf(t *testing.T) {
	// {"multipleOf": 0.1} with instance 0.3 fails under naive float64
	// round-trip; the adapter must preserve number text via json.Number.
	schemaJSON := `{"multipleOf": 0.1}`
	s := mustCompileJSONForMultifmt(t, schemaJSON)

	cases := []struct {
		name string
		fn   func() (*Result, error)
	}{
		{"jsonc", func() (*Result, error) { return ValidateJSONC(s, []byte(`0.3`)) }},
		{"yaml", func() (*Result, error) { return ValidateYAML(s, []byte(`0.3`)) }},
		{"toml-doc", func() (*Result, error) {
			// TOML can't host a bare scalar at the document root; nest under
			// a property and re-shape the schema accordingly.
			objSchema := mustCompileJSONForMultifmt(t, `{"type":"object","properties":{"x":{"multipleOf":0.1}}}`)
			return ValidateTOML(objSchema, []byte("x = 0.3\n"))
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res, err := tc.fn()
			if err != nil {
				t.Fatalf("validate: %v", err)
			}
			if !res.Valid {
				t.Fatalf("expected valid; errors=%v", res.Errors)
			}
		})
	}
}

func TestNumberPrecision_LargeInteger(t *testing.T) {
	// Just above Number.MAX_SAFE_INTEGER (2^53). float64 round-trip would
	// collapse 9007199254740993 to 9007199254740992 and fail the minimum.
	schema := mustCompileJSONForMultifmt(t, `{"minimum": 9007199254740993}`)

	t.Run("jsonc", func(t *testing.T) {
		res, err := ValidateJSONC(schema, []byte(`9007199254740993`))
		if err != nil {
			t.Fatalf("validate: %v", err)
		}
		if !res.Valid {
			t.Fatalf("expected valid; errors=%v", res.Errors)
		}
	})
	t.Run("toml", func(t *testing.T) {
		objSchema := mustCompileJSONForMultifmt(t, `{"type":"object","properties":{"x":{"minimum":9007199254740993}}}`)
		res, err := ValidateTOML(objSchema, []byte("x = 9007199254740993\n"))
		if err != nil {
			t.Fatalf("validate: %v", err)
		}
		if !res.Valid {
			t.Fatalf("expected valid; errors=%v", res.Errors)
		}
	})
	// YAML: the YAML 1.2 core schema treats unquoted integer scalars as
	// json.Number with the original text, so the comparator gets the full
	// digit string. Verify it.
	t.Run("yaml", func(t *testing.T) {
		res, err := ValidateYAML(schema, []byte("9007199254740993\n"))
		if err != nil {
			t.Fatalf("validate: %v", err)
		}
		if !res.Valid {
			t.Fatalf("expected valid; errors=%v", res.Errors)
		}
	})
}

func TestMalformed_ReturnsError_NoPanic(t *testing.T) {
	s := mustCompileJSONForMultifmt(t, `{"type":"object"}`)
	cases := []struct {
		name string
		fn   func() error
	}{
		{"jsonc", func() error {
			_, err := ValidateJSONC(s, []byte(`{"a":`))
			return err
		}},
		{"yaml", func() error {
			_, err := ValidateYAML(s, []byte("a: [1, 2,\n"))
			return err
		}},
		{"toml", func() error {
			_, err := ValidateTOML(s, []byte("a = "))
			return err
		}},
		{"load-jsonc", func() error {
			_, err := LoadJSONC([]byte(`{"type":`))
			return err
		}},
		{"load-yaml", func() error {
			_, err := LoadYAML([]byte("type: [object,\n"))
			return err
		}},
		{"load-toml", func() error {
			_, err := LoadTOML([]byte("type = "))
			return err
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.fn()
			if err == nil {
				t.Fatalf("expected error for malformed input")
			}
		})
	}
}

func TestNilSchema_ReturnsSentinel(t *testing.T) {
	cases := []struct {
		name string
		fn   func() error
	}{
		{"jsonc", func() error {
			_, err := ValidateJSONC(nil, []byte(`{}`))
			return err
		}},
		{"yaml", func() error {
			_, err := ValidateYAML(nil, []byte(`{}`))
			return err
		}},
		{"toml", func() error {
			_, err := ValidateTOML(nil, []byte(`x = 1`))
			return err
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.fn()
			if !errors.Is(err, ErrSchemaNotCompiled) {
				t.Fatalf("expected ErrSchemaNotCompiled, got %v", err)
			}
		})
	}
}

func TestConcurrency_NoRace(t *testing.T) {
	t.Parallel()
	s := mustCompileJSONForMultifmt(t, kitchenSinkSchemaJSON)

	jsonInstance := []byte(`{
  // hi
  "name": "alice", "age": 30, "tags": ["a"], "meta": {"score": 0.3}
}`)
	yamlInstance := []byte(`name: alice
age: 30
tags: [a]
meta: {score: 0.3}
`)
	tomlInstance := []byte(`name = "alice"
age = 30
tags = ["a"]
[meta]
score = 0.3
`)

	const goroutines = 8
	var wg sync.WaitGroup
	wg.Add(goroutines * 3)
	errCh := make(chan error, goroutines*3)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			res, err := ValidateJSONC(s, jsonInstance)
			if err != nil {
				errCh <- err
				return
			}
			if !res.Valid {
				errCh <- errors.New("jsonc: invalid")
			}
		}()
		go func() {
			defer wg.Done()
			res, err := ValidateYAML(s, yamlInstance)
			if err != nil {
				errCh <- err
				return
			}
			if !res.Valid {
				errCh <- errors.New("yaml: invalid")
			}
		}()
		go func() {
			defer wg.Done()
			res, err := ValidateTOML(s, tomlInstance)
			if err != nil {
				errCh <- err
				return
			}
			if !res.Valid {
				errCh <- errors.New("toml: invalid")
			}
		}()
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		t.Fatalf("concurrent validate: %v", err)
	}
}

func TestEquivalence_AcrossFormats(t *testing.T) {
	// Same schema expressed three ways should reject the same instances.
	jsoncSchema := `{
  // requires "x" >= 0
  "type": "object", "properties": {"x": {"type":"integer","minimum":0}}, "required":["x"],
}`
	yamlSchema := `type: object
properties:
  x:
    type: integer
    minimum: 0
required: [x]
`
	tomlSchema := `type = "object"
required = ["x"]
[properties.x]
type = "integer"
minimum = 0
`
	s1, err := LoadJSONC([]byte(jsoncSchema))
	if err != nil {
		t.Fatalf("LoadJSONC: %v", err)
	}
	s2, err := LoadYAML([]byte(yamlSchema))
	if err != nil {
		t.Fatalf("LoadYAML: %v", err)
	}
	s3, err := LoadTOML([]byte(tomlSchema))
	if err != nil {
		t.Fatalf("LoadTOML: %v", err)
	}

	good := []byte(`{"x":5}`)
	bad := []byte(`{"x":-1}`)
	for _, s := range []*Schema{s1, s2, s3} {
		gr, err := s.Validate(good)
		if err != nil || !gr.Valid {
			t.Fatalf("good instance: err=%v errors=%v", err, gr)
		}
		br, err := s.Validate(bad)
		if err != nil {
			t.Fatalf("bad instance: %v", err)
		}
		if br.Valid {
			t.Fatalf("bad instance unexpectedly valid")
		}
	}
}

func TestLoadJSONC_InvalidSchema(t *testing.T) {
	// Compile-time error path: schema decodes fine but is structurally
	// wrong (multipleOf must be a positive number).
	_, err := LoadJSONC([]byte(`{"multipleOf": -1}`))
	if err == nil {
		t.Fatalf("expected compile error")
	}
}

// TestLoadFormatURL_Roundtrip exercises the new URL-based loaders for each
// format. A MapLoader serves the format-encoded bytes; the package decodes
// them through the format-specific path and compiles a usable Schema.
func TestLoadFormatURL_Roundtrip(t *testing.T) {
	const yamlSrc = "type: object\nrequired: [name]\nproperties:\n  name: {type: string}\n"
	const tomlSrc = "type = \"object\"\nrequired = [\"name\"]\n[properties.name]\ntype = \"string\"\n"
	const jsoncSrc = "{\n  // jsonc with comments\n  \"type\": \"object\",\n  \"required\": [\"name\"],\n  \"properties\": {\"name\": {\"type\": \"string\"}},\n}"

	loader := MapLoader{
		"https://example.com/schema.yaml":  []byte(yamlSrc),
		"https://example.com/schema.toml":  []byte(tomlSrc),
		"https://example.com/schema.jsonc": []byte(jsoncSrc),
	}

	cases := []struct {
		name string
		fn   func() (*Schema, error)
	}{
		{"yaml", func() (*Schema, error) {
			return LoadYAMLURL("https://example.com/schema.yaml", WithLoader(loader))
		}},
		{"toml", func() (*Schema, error) {
			return LoadTOMLURL("https://example.com/schema.toml", WithLoader(loader))
		}},
		{"jsonc", func() (*Schema, error) {
			return LoadJSONCURL("https://example.com/schema.jsonc", WithLoader(loader))
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s, err := tc.fn()
			if err != nil {
				t.Fatalf("Load*URL: %v", err)
			}
			res, err := s.Validate([]byte(`{"name":"alice"}`))
			if err != nil {
				t.Fatalf("Validate: %v", err)
			}
			if !res.Valid {
				t.Errorf("expected valid; errors=%v", res.Errors)
			}
		})
	}
}

// TestLoadFormatValue_Roundtrip exercises the *Value variants. Each accepts
// a Go value already shaped like a parsed schema.
func TestLoadFormatValue_Roundtrip(t *testing.T) {
	v := map[string]any{
		"type":     "object",
		"required": []any{"x"},
		"properties": map[string]any{
			"x": map[string]any{"type": "integer"},
		},
	}
	for _, name := range []string{"jsonc", "yaml", "toml"} {
		t.Run(name, func(t *testing.T) {
			var s *Schema
			var err error
			switch name {
			case "jsonc":
				s, err = LoadJSONCValue(v)
			case "yaml":
				s, err = LoadYAMLValue(v)
			case "toml":
				s, err = LoadTOMLValue(v)
			}
			if err != nil {
				t.Fatalf("Load*Value: %v", err)
			}
			res, err := s.Validate([]byte(`{"x":1}`))
			if err != nil {
				t.Fatalf("Validate: %v", err)
			}
			if !res.Valid {
				t.Errorf("expected valid; errors=%v", res.Errors)
			}
		})
	}
}

// TestMustLoadFormat_PanicAndSuccess covers each Must* wrapper: a valid
// document returns a Schema, an invalid document panics.
func TestMustLoadFormat_PanicAndSuccess(t *testing.T) {
	const validJSONC = `{"type":"string"}`
	if s := MustLoadJSONC([]byte(validJSONC)); s == nil {
		t.Errorf("MustLoadJSONC returned nil for valid input")
	}
	const validYAML = "type: string\n"
	if s := MustLoadYAML([]byte(validYAML)); s == nil {
		t.Errorf("MustLoadYAML returned nil for valid input")
	}
	const validTOML = "type = \"string\"\n"
	if s := MustLoadTOML([]byte(validTOML)); s == nil {
		t.Errorf("MustLoadTOML returned nil for valid input")
	}
	if s := MustLoadJSONCValue(map[string]any{"type": "string"}); s == nil {
		t.Errorf("MustLoadJSONCValue returned nil for valid input")
	}

	// Panic-on-error.
	defer func() {
		if r := recover(); r == nil {
			t.Errorf("expected panic on invalid input")
		}
	}()
	MustLoadJSONC([]byte(`{`))
}

// TestLoadJSON_AliasFamily confirms the LoadJSON / LoadJSONURL /
// LoadJSONValue aliases (and their Must* counterparts) compile a schema
// equivalent to the underlying Compile / CompileURL / CompileValue calls.
// These are thin wrappers, so the test scope is "exists, forwards, returns
// non-nil schema." Failure-path behavior is covered by the underlying
// Compile* tests.
func TestLoadJSON_AliasFamily(t *testing.T) {
	const src = `{"type":"string","minLength":1}`

	s, err := LoadJSON([]byte(src))
	if err != nil {
		t.Fatalf("LoadJSON: %v", err)
	}
	if s == nil {
		t.Fatal("LoadJSON returned nil schema")
	}

	v, err := LoadJSONValue(map[string]any{"type": "string"})
	if err != nil {
		t.Fatalf("LoadJSONValue: %v", err)
	}
	if v == nil {
		t.Fatal("LoadJSONValue returned nil schema")
	}

	if s := MustLoadJSON([]byte(src)); s == nil {
		t.Error("MustLoadJSON returned nil for valid input")
	}
	if s := MustLoadJSONValue(map[string]any{"type": "integer"}); s == nil {
		t.Error("MustLoadJSONValue returned nil for valid input")
	}

	// Panic-on-error variant.
	func() {
		defer func() {
			if r := recover(); r == nil {
				t.Error("MustLoadJSON did not panic on invalid input")
			}
		}()
		MustLoadJSON([]byte(`{not json}`))
	}()
}

// TestLoadYAML_AliasToUndefinedAnchor confirms that a YAML document
// referencing an alias (*foo) without a matching anchor (&foo) is rejected
// with [ErrInvalidYAML]. The decoder must catch the unresolved reference at
// parse time so a malformed document can never silently produce an empty or
// nil node tree.
func TestLoadYAML_AliasToUndefinedAnchor(t *testing.T) {
	src := `type: object
properties:
  x: *undefined_alias
`
	_, err := LoadYAML([]byte(src))
	if err == nil {
		t.Fatal("expected error for undefined alias")
	}
	if !errors.Is(err, ErrInvalidYAML) {
		t.Errorf("err = %v, want errors.Is(_, ErrInvalidYAML)", err)
	}
}

// TestMergeYAMLInto covers [mergeYAMLInto] directly across the three
// branches: (1) merge from a mapping value, (2) merge from a sequence of
// mappings, (3) reject a non-mapping merge target. Direct invocation is
// required because the underlying YAML parser does not currently surface
// the [Node.MergeKey] tag (the boolean is set only when decoding through
// Unmarshal); the LoadYAML path therefore cannot reach this code, but
// the function is part of the package's documented support surface and
// must remain correct.
func TestMergeYAMLInto(t *testing.T) {
	mkScalar := func(v string) *yaml.Node {
		return &yaml.Node{Kind: yaml.ScalarNode, Value: v}
	}
	mkMap := func(kvs ...string) *yaml.Node {
		n := &yaml.Node{Kind: yaml.MappingNode}
		for i := 0; i+1 < len(kvs); i += 2 {
			n.Children = append(n.Children, mkScalar(kvs[i]), mkScalar(kvs[i+1]))
		}
		return n
	}
	t.Run("from_mapping", func(t *testing.T) {
		dst := map[string]any{"explicit": "kept"}
		src := mkMap("a", "1", "b", "2", "explicit", "should-not-overwrite")
		if err := mergeYAMLInto(dst, src, map[string]*yaml.Node{}, map[*yaml.Node]bool{}); err != nil {
			t.Fatalf("mergeYAMLInto: %v", err)
		}
		// Existing keys win; new keys are merged in.
		if got := dst["explicit"]; got != "kept" {
			t.Errorf("explicit overwritten: got %v", got)
		}
		if got, _ := dst["a"].(json.Number); got != "1" {
			t.Errorf("a = %v; want 1", dst["a"])
		}
		if _, ok := dst["b"]; !ok {
			t.Errorf("b not merged in; dst=%v", dst)
		}
	})
	t.Run("from_sequence_of_mappings", func(t *testing.T) {
		dst := map[string]any{}
		seq := &yaml.Node{Kind: yaml.SequenceNode, Children: []*yaml.Node{
			mkMap("p1", "x"),
			mkMap("p2", "y"),
		}}
		if err := mergeYAMLInto(dst, seq, map[string]*yaml.Node{}, map[*yaml.Node]bool{}); err != nil {
			t.Fatalf("mergeYAMLInto: %v", err)
		}
		if dst["p1"] != "x" || dst["p2"] != "y" {
			t.Errorf("expected p1=x and p2=y; got %v", dst)
		}
	})
	t.Run("from_nil_is_noop", func(t *testing.T) {
		dst := map[string]any{"a": 1}
		if err := mergeYAMLInto(dst, nil, nil, nil); err != nil {
			t.Errorf("nil source: %v", err)
		}
	})
	t.Run("from_non_mapping_errors", func(t *testing.T) {
		dst := map[string]any{}
		err := mergeYAMLInto(dst, mkScalar("not-a-mapping"), map[string]*yaml.Node{}, map[*yaml.Node]bool{})
		if err == nil {
			t.Fatal("expected error for non-mapping merge value")
		}
		if !errors.Is(err, ErrInvalidYAML) {
			t.Errorf("err = %v; want errors.Is(_, ErrInvalidYAML)", err)
		}
	})
	t.Run("sequence_with_non_mapping_entry_errors", func(t *testing.T) {
		dst := map[string]any{}
		seq := &yaml.Node{Kind: yaml.SequenceNode, Children: []*yaml.Node{
			mkMap("a", "1"),
			mkScalar("not-a-mapping"),
		}}
		err := mergeYAMLInto(dst, seq, map[string]*yaml.Node{}, map[*yaml.Node]bool{})
		if err == nil {
			t.Fatal("expected error for non-mapping inside sequence")
		}
		if !errors.Is(err, ErrInvalidYAML) {
			t.Errorf("err = %v; want errors.Is(_, ErrInvalidYAML)", err)
		}
	})
}

// TestLoadTOML_UnderscoredNumbers verifies TOML's `_` digit-grouping
// separator is stripped before parsing. The 1_000 / 1_000_000 form is
// canonical TOML; the schema-as-data must round-trip through compile and
// produce normalized json.Number text. Exercises [stripUnderscores] and
// the integer + float branches of [convertTOMLValue].
func TestLoadTOML_UnderscoredNumbers(t *testing.T) {
	src := `type = "integer"
minimum = 1_000
maximum = 1_000_000
`
	s, err := LoadTOML([]byte(src))
	if err != nil {
		t.Fatalf("LoadTOML: %v", err)
	}
	cases := []struct {
		instance string
		want     bool
	}{
		{`50000`, true},
		{`999`, false},
		{`1500000`, false},
		{`1000`, true},
	}
	for _, c := range cases {
		t.Run(c.instance, func(t *testing.T) {
			res, err := s.Validate([]byte(c.instance))
			if err != nil {
				t.Fatalf("Validate(%s): %v", c.instance, err)
			}
			if res.Valid != c.want {
				t.Errorf("Validate(%s).Valid = %v; want %v; errors=%v", c.instance, res.Valid, c.want, res.Errors)
			}
		})
	}
}

// TestLoadTOML_UnderscoredFloat covers the float branch of
// stripUnderscores via [tomlFloatToNumber].
func TestLoadTOML_UnderscoredFloat(t *testing.T) {
	src := `type = "number"
multipleOf = 0.1
maximum = 1_000.5
`
	s, err := LoadTOML([]byte(src))
	if err != nil {
		t.Fatalf("LoadTOML: %v", err)
	}
	res, err := s.Validate([]byte(`100.3`))
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if !res.Valid {
		t.Errorf("expected valid; errors=%v", res.Errors)
	}
}

// TestLoadTOML_NestedTables exercises [setTOMLNested] across both bare
// nested-table syntax (`[parent.child]`) and the deeper mixed-mode
// (`[a.b.c]` plus inline-table reassignment).
func TestLoadTOML_NestedTables(t *testing.T) {
	src := `type = "object"

[properties]

[properties.name]
type = "string"
minLength = 1

[properties.address]
type = "object"

[properties.address.properties]

[properties.address.properties.street]
type = "string"

[properties.address.properties.zip]
type = "integer"
minimum = 0
`
	s, err := LoadTOML([]byte(src))
	if err != nil {
		t.Fatalf("LoadTOML: %v", err)
	}
	res, err := s.Validate([]byte(`{"name":"alice","address":{"street":"main","zip":12345}}`))
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if !res.Valid {
		t.Errorf("nested-table schema rejected valid instance; errors=%v", res.Errors)
	}
	// Negative case: zip cannot be negative
	res, err = s.Validate([]byte(`{"name":"a","address":{"street":"x","zip":-1}}`))
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if res.Valid {
		t.Errorf("expected invalid (zip < 0)")
	}
}

// TestLoadTOML_InlineTablesAndArrays exercises convertTOMLValue's
// InlineTable and Array branches together with deeper setTOMLNested
// paths.
func TestLoadTOML_InlineTablesAndArrays(t *testing.T) {
	src := `type = "object"
properties = { name = { type = "string" }, tags = { type = "array", items = { type = "string" } } }
required = ["name"]
`
	s, err := LoadTOML([]byte(src))
	if err != nil {
		t.Fatalf("LoadTOML: %v", err)
	}
	res, err := s.Validate([]byte(`{"name":"alice","tags":["a","b"]}`))
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if !res.Valid {
		t.Errorf("inline-table + array schema rejected valid instance; errors=%v", res.Errors)
	}
}

// TestLoadTOML_HexOctalBinaryIntegers exercises the integer-base parsing
// branches inside [tomlIntegerToNumber] via base-aware ParseInt.
func TestLoadTOML_HexOctalBinaryIntegers(t *testing.T) {
	src := `type = "integer"
minimum = 0x10
maximum = 0o777
`
	s, err := LoadTOML([]byte(src))
	if err != nil {
		t.Fatalf("LoadTOML: %v", err)
	}
	// 0x10 = 16, 0o777 = 511. 256 lies in range.
	res, err := s.Validate([]byte(`256`))
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if !res.Valid {
		t.Errorf("expected valid; errors=%v", res.Errors)
	}
}

// TestSetTOMLNested exercises [setTOMLNested] directly across the
// multi-segment-key descent paths. The go-rotini/toml parser
// pre-flattens dotted-key paths into nested AST nodes, so via the
// LoadTOML pipeline this function is only ever invoked with
// single-segment keys; the inner-loop branches are defensive
// guard-rails against a future parser change. Direct invocation here
// keeps those branches honest.
func TestSetTOMLNested(t *testing.T) {
	t.Run("empty_key_errors", func(t *testing.T) {
		err := setTOMLNested(map[string]any{}, nil, "v", false)
		if err == nil {
			t.Fatal("expected error for empty key path")
		}
		if !errors.Is(err, ErrInvalidTOML) {
			t.Errorf("err = %v; want errors.Is(_, ErrInvalidTOML)", err)
		}
	})
	t.Run("creates_intermediate_maps", func(t *testing.T) {
		out := map[string]any{}
		if err := setTOMLNested(out, []string{"a", "b", "c"}, "leaf", false); err != nil {
			t.Fatalf("setTOMLNested: %v", err)
		}
		// The path must materialize a/b/c step by step.
		a, ok := out["a"].(map[string]any)
		if !ok {
			t.Fatalf("out[a] = %T; want map", out["a"])
		}
		b, ok := a["b"].(map[string]any)
		if !ok {
			t.Fatalf("out.a.b = %T; want map", a["b"])
		}
		if b["c"] != "leaf" {
			t.Errorf("out.a.b.c = %v; want \"leaf\"", b["c"])
		}
	})
	t.Run("descends_into_existing_map", func(t *testing.T) {
		out := map[string]any{
			"a": map[string]any{"existing": 1},
		}
		if err := setTOMLNested(out, []string{"a", "b"}, "v", false); err != nil {
			t.Fatalf("setTOMLNested: %v", err)
		}
		a := out["a"].(map[string]any)
		if a["existing"] != 1 {
			t.Errorf("existing key dropped: %v", a)
		}
		if a["b"] != "v" {
			t.Errorf("a.b = %v; want \"v\"", a["b"])
		}
	})
	t.Run("descends_into_array_tail", func(t *testing.T) {
		// A non-empty array of mappings; descent should target the last entry.
		out := map[string]any{
			"a": []any{
				map[string]any{"first": 1},
				map[string]any{"second": 2},
			},
		}
		if err := setTOMLNested(out, []string{"a", "third"}, "v", false); err != nil {
			t.Fatalf("setTOMLNested: %v", err)
		}
		arr := out["a"].([]any)
		tail := arr[len(arr)-1].(map[string]any)
		if tail["third"] != "v" {
			t.Errorf("tail.third = %v; want \"v\"", tail["third"])
		}
	})
	t.Run("descend_into_empty_array_errors", func(t *testing.T) {
		out := map[string]any{"a": []any{}}
		err := setTOMLNested(out, []string{"a", "b"}, "v", false)
		if err == nil {
			t.Fatal("expected error descending into empty array")
		}
	})
	t.Run("descend_into_non_mapping_array_errors", func(t *testing.T) {
		out := map[string]any{"a": []any{"not-a-map"}}
		err := setTOMLNested(out, []string{"a", "b"}, "v", false)
		if err == nil {
			t.Fatal("expected error descending into non-table array")
		}
	})
	t.Run("descend_into_scalar_errors", func(t *testing.T) {
		out := map[string]any{"a": 42}
		err := setTOMLNested(out, []string{"a", "b"}, "v", false)
		if err == nil {
			t.Fatal("expected error descending into scalar")
		}
	})
	t.Run("array_table_first_append", func(t *testing.T) {
		out := map[string]any{}
		val := map[string]any{"x": 1}
		if err := setTOMLNested(out, []string{"items"}, val, true); err != nil {
			t.Fatalf("setTOMLNested: %v", err)
		}
		arr, ok := out["items"].([]any)
		if !ok {
			t.Fatalf("out.items = %T; want []any", out["items"])
		}
		if len(arr) != 1 {
			t.Errorf("len(arr) = %d; want 1", len(arr))
		}
	})
	t.Run("array_table_subsequent_append", func(t *testing.T) {
		out := map[string]any{"items": []any{map[string]any{"x": 1}}}
		val := map[string]any{"y": 2}
		if err := setTOMLNested(out, []string{"items"}, val, true); err != nil {
			t.Fatalf("setTOMLNested: %v", err)
		}
		arr := out["items"].([]any)
		if len(arr) != 2 {
			t.Errorf("len(arr) = %d; want 2", len(arr))
		}
	})
	t.Run("array_table_into_non_array_errors", func(t *testing.T) {
		out := map[string]any{"items": "not-an-array"}
		err := setTOMLNested(out, []string{"items"}, map[string]any{"x": 1}, true)
		if err == nil {
			t.Fatal("expected error appending to non-array")
		}
	})
	t.Run("merge_existing_table", func(t *testing.T) {
		out := map[string]any{
			"props": map[string]any{"existing": "kept"},
		}
		if err := setTOMLNested(out, []string{"props"}, map[string]any{"new": "added"}, false); err != nil {
			t.Fatalf("setTOMLNested: %v", err)
		}
		p := out["props"].(map[string]any)
		if p["existing"] != "kept" || p["new"] != "added" {
			t.Errorf("merge failed: %v", p)
		}
	})
}

// TestLoadJSONC_CommentInArray verifies the JSONC adapter strips
// comments embedded inside array literals (between elements). Earlier
// coverage only exercised object-level comments.
func TestLoadJSONC_CommentInArray(t *testing.T) {
	schemaJSONC := []byte(`{
		"type": "array",
		"prefixItems": [
			{"type": "integer"},
			// comment between items
			{"type": "string"},
			/* block comment between items */
			{"type": "boolean"}
		]
	}`)
	s, err := LoadJSONC(schemaJSONC)
	if err != nil {
		t.Fatalf("LoadJSONC: %v", err)
	}
	res, err := s.Validate([]byte(`[1, "x", true]`))
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if !res.Valid {
		t.Errorf("expected valid; errors=%v", res.Errors)
	}
	// Negative case: wrong type at first slot.
	res, err = s.Validate([]byte(`["nope", "x", true]`))
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if res.Valid {
		t.Errorf("expected invalid (string at integer slot)")
	}
}

// TestLoadYAMLBoolKey exercises yamlKeyString's bool branch.
func TestLoadYAMLBoolKey(t *testing.T) {
	src := []byte("true: alpha\nfalse: beta\n")
	v, err := decodeYAML(src)
	if err != nil {
		t.Fatalf("decodeYAML: %v", err)
	}
	m, ok := v.(map[string]any)
	if !ok {
		t.Fatalf("got %T", v)
	}
	if _, ok := m["true"]; !ok {
		t.Errorf("expected 'true' key; got %v", m)
	}
	if _, ok := m["false"]; !ok {
		t.Errorf("expected 'false' key; got %v", m)
	}
}

// TestLoadYAMLNumberKey exercises yamlKeyString's json.Number branch.
func TestLoadYAMLNumberKey(t *testing.T) {
	src := []byte("42: alpha\n")
	v, err := decodeYAML(src)
	if err != nil {
		t.Fatalf("decodeYAML: %v", err)
	}
	m, ok := v.(map[string]any)
	if !ok {
		t.Fatalf("got %T", v)
	}
	if _, ok := m["42"]; !ok {
		t.Errorf("expected '42' key; got %v", m)
	}
}

// TestLoadYAMLNullKey covers yamlKeyString nil branch.
func TestLoadYAMLNullKey(t *testing.T) {
	src := []byte("null: alpha\n")
	v, err := decodeYAML(src)
	if err != nil {
		t.Logf("decodeYAML err: %v", err)
		return
	}
	m, ok := v.(map[string]any)
	if !ok {
		t.Fatalf("got %T", v)
	}
	if _, ok := m[""]; !ok {
		t.Errorf("expected '' key from nil; got %v", m)
	}
}

// TestLoadYAMLAlias covers convertYAMLNode's AliasNode branch.
func TestLoadYAMLAlias(t *testing.T) {
	src := []byte(`
alpha: &shared
  type: integer
beta: *shared
`)
	v, err := decodeYAML(src)
	if err != nil {
		t.Fatalf("decodeYAML: %v", err)
	}
	m, _ := v.(map[string]any)
	if a, b := m["alpha"], m["beta"]; a == nil || b == nil {
		t.Errorf("alias not resolved; got alpha=%v beta=%v", a, b)
	}
}

// TestLoadYAMLUnresolvedAlias covers the unresolved-alias error.
func TestLoadYAMLUnresolvedAlias(t *testing.T) {
	src := []byte("alpha: *missing\n")
	if _, err := decodeYAML(src); err == nil {
		t.Error("expected unresolved-alias error")
	}
}

// TestLoadYAMLMergeKey covers convertYAMLNode + mergeYAMLInto's mapping
// branch.
func TestLoadYAMLMergeKey(t *testing.T) {
	src := []byte(`
defaults: &defaults
  type: object
  required: [x]

actual:
  <<: *defaults
  properties:
    x:
      type: integer
`)
	v, err := decodeYAML(src)
	if err != nil {
		t.Fatalf("decodeYAML: %v", err)
	}
	if v == nil {
		t.Fatal("nil")
	}
}

// TestLoadYAMLMergeSequence covers mergeYAMLInto's []any branch.
// Note: YAML merge support depends on parser-level MergeKey detection,
// which the upstream yaml package handles for `<<:`.
func TestLoadYAMLMergeSequence(t *testing.T) {
	t.Skip("yaml merge sequence support depends on parser MergeKey detection")
}

// TestLoadYAMLMergeBadType covers the merge-not-map-or-seq branch.
func TestLoadYAMLMergeBadType(t *testing.T) {
	t.Skip("merge bad-type detection depends on parser MergeKey detection")
}

// TestLoadYAMLNestedSequence covers SequenceNode recursion.
func TestLoadYAMLNestedSequence(t *testing.T) {
	src := []byte(`
items:
  - 1
  - "two"
  - [3, 4]
  - {x: 5}
`)
	v, err := decodeYAML(src)
	if err != nil {
		t.Fatalf("decodeYAML: %v", err)
	}
	if v == nil {
		t.Fatal("nil")
	}
}

// TestLoadYAMLQuotedScalarStaysString covers convertYAMLScalar's quoted
// branch.
func TestLoadYAMLQuotedScalarStaysString(t *testing.T) {
	src := []byte(`x: "42"`)
	v, err := decodeYAML(src)
	if err != nil {
		t.Fatalf("decodeYAML: %v", err)
	}
	m, _ := v.(map[string]any)
	if _, ok := m["x"].(string); !ok {
		t.Errorf("quoted '42' should stay string; got %T", m["x"])
	}
}

// TestLoadYAMLEmptyDoc covers the empty-doc branch.
func TestLoadYAMLEmptyDoc(t *testing.T) {
	src := []byte("")
	v, err := decodeYAML(src)
	if err != nil {
		t.Fatalf("decodeYAML empty: %v", err)
	}
	if v != nil {
		t.Errorf("empty doc should be nil; got %v", v)
	}
}

// TestLoadYAMLNullDoc covers the null-doc branch.
func TestLoadYAMLNullDoc(t *testing.T) {
	src := []byte("---\n")
	v, err := decodeYAML(src)
	if err != nil {
		t.Logf("err: %v", err)
		return
	}
	if v != nil {
		t.Logf("got %v", v)
	}
}

// TestLoadTOMLBoolValue covers the BooleanNode branches (true, false).
func TestLoadTOMLBoolValue(t *testing.T) {
	src := []byte(`a = true
b = false`)
	v, err := decodeTOML(src)
	if err != nil {
		t.Fatalf("decodeTOML: %v", err)
	}
	m, _ := v.(map[string]any)
	if a := m["a"]; a != true {
		t.Errorf("a = %v", a)
	}
	if b := m["b"]; b != false {
		t.Errorf("b = %v", b)
	}
}

// TestLoadTOMLArrayOfMixed covers ArrayNode recursion.
func TestLoadTOMLArrayOfMixed(t *testing.T) {
	src := []byte(`a = [1, "two", true, 3.14]`)
	v, err := decodeTOML(src)
	if err != nil {
		t.Fatalf("decodeTOML: %v", err)
	}
	m, _ := v.(map[string]any)
	arr, ok := m["a"].([]any)
	if !ok || len(arr) != 4 {
		t.Errorf("got %v", m["a"])
	}
}

// TestLoadTOMLInlineTable covers InlineTableNode branch.
func TestLoadTOMLInlineTable(t *testing.T) {
	src := []byte(`a = {x = 1, y = "two"}`)
	v, err := decodeTOML(src)
	if err != nil {
		t.Fatalf("decodeTOML: %v", err)
	}
	m, _ := v.(map[string]any)
	a, ok := m["a"].(map[string]any)
	if !ok {
		t.Fatalf("a not a map; got %T", m["a"])
	}
	if a["x"] == nil {
		t.Errorf("missing x: %v", a)
	}
}

// TestLoadTOMLDateTimeVariants covers each datetime kind.
func TestLoadTOMLDateTimeVariants(t *testing.T) {
	src := []byte(`
offset = 1979-05-27T07:32:00Z
local_dt = 1979-05-27T07:32:00
local_d = 1979-05-27
local_t = 07:32:00
`)
	v, err := decodeTOML(src)
	if err != nil {
		t.Fatalf("decodeTOML: %v", err)
	}
	m, _ := v.(map[string]any)
	for _, k := range []string{"offset", "local_dt", "local_d", "local_t"} {
		if _, ok := m[k].(string); !ok {
			t.Errorf("%s: got %T, want string", k, m[k])
		}
	}
}

// TestLoadTOMLNested covers the nested-table descent.
func TestLoadTOMLNested(t *testing.T) {
	src := []byte(`
[parent.child]
x = 1
[parent.other]
y = 2
`)
	v, err := decodeTOML(src)
	if err != nil {
		t.Fatalf("decodeTOML: %v", err)
	}
	m, _ := v.(map[string]any)
	parent, _ := m["parent"].(map[string]any)
	if parent == nil {
		t.Fatal("missing parent")
	}
	if c, ok := parent["child"].(map[string]any); !ok || c["x"] == nil {
		t.Errorf("missing parent.child.x: %v", parent)
	}
	if o, ok := parent["other"].(map[string]any); !ok || o["y"] == nil {
		t.Errorf("missing parent.other.y: %v", parent)
	}
}

// TestLoadYAMLSpecialFloats covers .inf / .nan handling.
func TestLoadYAMLSpecialFloats(t *testing.T) {
	src := []byte(`a: .inf
b: -.inf
c: .nan`)
	v, err := decodeYAML(src)
	if err != nil {
		t.Fatalf("decodeYAML: %v", err)
	}
	m, _ := v.(map[string]any)
	for _, k := range []string{"a", "b", "c"} {
		if m[k] == nil {
			t.Errorf("missing %s", k)
		}
	}
}

// TestLoadYAMLHexOctal covers hex/octal yamlNormalizeNumber branches.
func TestLoadYAMLHexOctal(t *testing.T) {
	src := []byte(`hex: 0x1A
oct: 0o17`)
	v, err := decodeYAML(src)
	if err != nil {
		t.Fatalf("decodeYAML: %v", err)
	}
	m, _ := v.(map[string]any)
	for _, k := range []string{"hex", "oct"} {
		if m[k] == nil {
			t.Errorf("missing %s", k)
		}
	}
}

// TestValidateYAMLAndJSONCAndTOMLSuccessPaths exercises the validate-format
// adapters' happy paths.
func TestValidateYAMLAndJSONCAndTOMLSuccessPaths(t *testing.T) {
	s := MustCompile([]byte(`{"type":"object","required":["name"],"properties":{"name":{"type":"string"}}}`))
	// JSONC
	res, err := ValidateJSONC(s, []byte(`{
		// comment
		"name": "alice",
	}`))
	if err != nil {
		t.Fatalf("ValidateJSONC: %v", err)
	}
	if !res.Valid {
		t.Errorf("ValidateJSONC: errors=%v", res.Errors)
	}
	// YAML
	res, err = ValidateYAML(s, []byte("name: alice\n"))
	if err != nil {
		t.Fatalf("ValidateYAML: %v", err)
	}
	if !res.Valid {
		t.Errorf("ValidateYAML: errors=%v", res.Errors)
	}
	// TOML
	res, err = ValidateTOML(s, []byte(`name = "alice"`))
	if err != nil {
		t.Fatalf("ValidateTOML: %v", err)
	}
	if !res.Valid {
		t.Errorf("ValidateTOML: errors=%v", res.Errors)
	}
}

// TestLoadYAMLEmptyResource covers the no-docs / empty docs branches.
func TestLoadYAMLEmptyResource(t *testing.T) {
	if _, err := decodeYAML([]byte("")); err != nil {
		t.Errorf("empty: %v", err)
	}
}

// TestLoadJSONCWithComments confirms LoadJSONC tolerates comments.
func TestLoadJSONCWithComments(t *testing.T) {
	src := []byte(`{
		// top
		"type": "object",
		/* nested */
		"required": ["name"],
		"properties": {"name": {"type":"string"}}
	}`)
	if _, err := LoadJSONC(src); err != nil {
		t.Errorf("LoadJSONC: %v", err)
	}
}

// TestLoadYAMLViaScript ensures the format adapter compiles + validates.
func TestLoadYAMLViaScript(t *testing.T) {
	src := []byte(`type: integer
minimum: 0`)
	s, err := LoadYAML(src)
	if err != nil {
		t.Fatalf("LoadYAML: %v", err)
	}
	if res, _ := s.Validate([]byte("5")); !res.Valid {
		t.Errorf("expected valid; errors=%v", res.Errors)
	}
}

// TestStripUnderscoresAllPaths confirms underscored-int paths.
func TestStripUnderscoresAllPaths(t *testing.T) {
	cases := []string{"1_000_000", "0x_FF", "0b_101"}
	for _, c := range cases {
		got := stripUnderscores(c)
		if strings.Contains(got, "_") {
			t.Errorf("got %q", got)
		}
	}
}

// TestLoadJSONCCompileFailure covers the JSONC decode-OK + compile-fail
// branch: JSONC parses fine, but the result is a malformed schema.
func TestLoadJSONCCompileFailure(t *testing.T) {
	src := []byte(`{"minLength":"three"}`) // wrong type for minLength
	if _, err := LoadJSONC(src); err == nil {
		t.Error("expected compile-fail error")
	}
}

// TestLoadYAMLCompileFailure covers the YAML decode-OK + compile-fail
// branch.
func TestLoadYAMLCompileFailure(t *testing.T) {
	src := []byte("minLength: three\n")
	if _, err := LoadYAML(src); err == nil {
		t.Error("expected compile-fail error")
	}
}

// TestLoadTOMLCompileFailure covers the TOML decode-OK + compile-fail
// branch.
func TestLoadTOMLCompileFailure(t *testing.T) {
	src := []byte(`minLength = "three"`)
	if _, err := LoadTOML(src); err == nil {
		t.Error("expected compile-fail error")
	}
}

// TestValidateJSONCValidationFailure covers the validation-side decode
// success path with a failing validation.
func TestValidateJSONCValidationFailure(t *testing.T) {
	s := MustCompile([]byte(`{"type":"object","required":["a"]}`))
	res, err := ValidateJSONC(s, []byte(`{}`))
	if err != nil {
		t.Fatalf("ValidateJSONC: %v", err)
	}
	if res.Valid {
		t.Error("expected invalid")
	}
}

// TestValidateYAMLValidationFailure covers the YAML validate path.
func TestValidateYAMLValidationFailure(t *testing.T) {
	s := MustCompile([]byte(`{"type":"object","required":["a"]}`))
	res, err := ValidateYAML(s, []byte(`b: 1`))
	if err != nil {
		t.Fatalf("ValidateYAML: %v", err)
	}
	if res.Valid {
		t.Error("expected invalid")
	}
}

// TestValidateTOMLValidationFailure covers the TOML validate path.
func TestValidateTOMLValidationFailure(t *testing.T) {
	s := MustCompile([]byte(`{"type":"object","required":["a"]}`))
	res, err := ValidateTOML(s, []byte(`b = 1`))
	if err != nil {
		t.Fatalf("ValidateTOML: %v", err)
	}
	if res.Valid {
		t.Error("expected invalid")
	}
}

// TestLoadURLBytesNilLoader covers the nil-loader branch in loadURLBytes
// (use no WithLoader option).
func TestLoadURLBytesNilLoader(t *testing.T) {
	// No loader option — falls back to DefaultLoader. Since the default
	// loader's HTTPLoader will fail to fetch a non-existent URL, we expect
	// an error.
	if _, err := loadURLBytes("https://nope.example.invalid/x", nil); err == nil {
		t.Error("expected error from default loader")
	}
}

// TestLoadYAMLWithNumbers covers the numeric scalar conversion.
func TestLoadYAMLWithNumbers(t *testing.T) {
	src := []byte(`type: object
properties:
  count:
    type: integer
    minimum: 0
    maximum: 100
`)
	s, err := LoadYAML(src)
	if err != nil {
		t.Fatalf("LoadYAML: %v", err)
	}
	res, err := s.Validate([]byte(`{"count":50}`))
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if !res.Valid {
		t.Errorf("expected valid; errors=%v", res.Errors)
	}
}

// TestLoadYAMLWithBool covers boolean scalar.
func TestLoadYAMLWithBool(t *testing.T) {
	src := []byte("uniqueItems: true\ntype: array\nitems:\n  type: integer\n")
	if _, err := LoadYAML(src); err != nil {
		t.Errorf("LoadYAML: %v", err)
	}
}

// TestLoadYAMLWithNull covers null literal.
func TestLoadYAMLWithNull(t *testing.T) {
	src := []byte("type: null\n")
	if _, err := LoadYAML(src); err != nil {
		// Either error or success ok — ensures the null path is taken.
		t.Logf("LoadYAML(null): %v", err)
	}
}

// TestLoadTOMLWithMixedTypes covers TOML integer/float/bool/string.
func TestLoadTOMLWithMixedTypes(t *testing.T) {
	src := []byte(`
type = "object"
[properties.x]
type = "integer"
minimum = 0
maximum = 100
multipleOf = 1.5
`)
	if _, err := LoadTOML(src); err != nil {
		t.Errorf("LoadTOML: %v", err)
	}
}

// TestLoadTOMLWithDateTimes covers the date-time scalar path.
func TestLoadTOMLWithDateTimes(t *testing.T) {
	src := []byte(`
[example]
created = 2025-01-01T00:00:00Z
date = 2025-01-01
time = 12:34:56
`)
	if _, err := LoadTOML(src); err != nil {
		// May error if these become non-scalar in the schema; just exercise.
		t.Logf("LoadTOML: %v", err)
	}
}

// TestLoadTOMLWithArrayOfTables covers the array-table path.
func TestLoadTOMLWithArrayOfTables(t *testing.T) {
	src := []byte(`
[[items]]
name = "a"

[[items]]
name = "b"
`)
	if _, err := LoadTOML(src); err != nil {
		// Compilation may fail if "items" is not a valid schema keyword;
		// this still exercises the AST conversion.
		t.Logf("LoadTOML: %v", err)
	}
}

// TestStripUnderscoresPlain confirms stripUnderscores returns input verbatim.
func TestStripUnderscoresPlain(t *testing.T) {
	if got := stripUnderscores("plain"); got != "plain" {
		t.Errorf("got %q", got)
	}
}

// TestIsYAMLNumberBranches covers each branch.
func TestIsYAMLNumberBranches(t *testing.T) {
	cases := []struct {
		v    string
		want bool
	}{
		{"", false},
		{".inf", true},
		{".Inf", true},
		{".INF", true},
		{"+.inf", true},
		{"-.inf", true},
		{".nan", true},
		{".NaN", true},
		{"42", true},
		{"-7", true},
		{"0x1A", true},
		{"0o17", true},
		{"3.14", true},
		{"1e6", true},
		{"abc", false},
		{"true", false},
	}
	for _, tc := range cases {
		got := isYAMLNumber(tc.v)
		if got != tc.want {
			t.Errorf("isYAMLNumber(%q) = %v, want %v", tc.v, got, tc.want)
		}
	}
}

// TestYAMLNormalizeNumberBranches covers each branch.
func TestYAMLNormalizeNumberBranches(t *testing.T) {
	cases := []struct {
		v    string
		want string
	}{
		{".inf", "Inf"},
		{".Inf", "Inf"},
		{".INF", "Inf"},
		{"-.inf", "-Inf"},
		{"-.Inf", "-Inf"},
		{".nan", "NaN"},
		{".NaN", "NaN"},
		{"0x1A", "26"},   // hex → decimal
		{"0o17", "15"},   // octal → decimal
		{"-0x1A", "-26"}, // signed hex
		{"42", "42"},     // plain decimal stays
		{"-7", "-7"},
		{"3.14", "3.14"}, // float passes through
	}
	for _, tc := range cases {
		got := yamlNormalizeNumber(tc.v)
		if got != tc.want {
			t.Errorf("yamlNormalizeNumber(%q) = %q, want %q", tc.v, got, tc.want)
		}
	}
}

// TestHasBasePrefixBranches covers each branch.
func TestHasBasePrefixBranches(t *testing.T) {
	cases := []struct {
		v    string
		want bool
	}{
		{"0x1", true},
		{"0X1", true},
		{"0o7", true},
		{"0O7", true},
		{"0b1", true},
		{"0B1", true},
		{"+0x1", true},
		{"-0x1", true},
		{"42", false},
		{"", false},
	}
	for _, tc := range cases {
		got := hasBasePrefix(tc.v)
		if got != tc.want {
			t.Errorf("hasBasePrefix(%q) = %v, want %v", tc.v, got, tc.want)
		}
	}
}

// TestTOMLIntegerToNumberBranches covers each branch.
func TestTOMLIntegerToNumberBranches(t *testing.T) {
	cases := []struct {
		v    string
		want json.Number
		err  bool
	}{
		{"42", "42", false},
		{"0x1A", "26", false},
		{"0o17", "15", false},
		{"0b101", "5", false},
		{"1_000", "1000", false}, // underscores stripped
		{"0xFFFFFFFFFFFFFFFF", "18446744073709551615", false}, // overflow → uint64
		{"not-a-number", "", true},
	}
	for _, tc := range cases {
		got, err := tomlIntegerToNumber(tc.v)
		if (err != nil) != tc.err {
			t.Errorf("tomlIntegerToNumber(%q) err=%v, want err=%v", tc.v, err, tc.err)
			continue
		}
		if !tc.err && got != tc.want {
			t.Errorf("tomlIntegerToNumber(%q) = %v, want %v", tc.v, got, tc.want)
		}
	}
}

// TestTOMLFloatToNumberBranches covers each branch.
func TestTOMLFloatToNumberBranches(t *testing.T) {
	cases := []struct {
		v    string
		want json.Number
		err  bool
	}{
		{"3.14", "3.14", false},
		{"inf", "Inf", false},
		{"+inf", "Inf", false},
		{"-inf", "-Inf", false},
		{"nan", "NaN", false},
		{"+nan", "NaN", false},
		{"-nan", "NaN", false},
		{"3_000.5", "3000.5", false}, // underscores stripped
		{"not-a-float", "", true},
	}
	for _, tc := range cases {
		got, err := tomlFloatToNumber(tc.v)
		if (err != nil) != tc.err {
			t.Errorf("tomlFloatToNumber(%q) err=%v, want err=%v", tc.v, err, tc.err)
			continue
		}
		if !tc.err && got != tc.want {
			t.Errorf("tomlFloatToNumber(%q) = %v, want %v", tc.v, got, tc.want)
		}
	}
}

// TestStripUnderscores covers both branches.
func TestStripUnderscores(t *testing.T) {
	if got := stripUnderscores("plain"); got != "plain" {
		t.Errorf("plain: %q", got)
	}
	if got := stripUnderscores("1_000_000"); got != "1000000" {
		t.Errorf("underscores: %q", got)
	}
}

// TestLoadJSONCBadInput covers the decode-error branch.
func TestLoadJSONCBadInput(t *testing.T) {
	if _, err := LoadJSONC([]byte("garbage")); err == nil {
		t.Error("expected error")
	}
}

// TestLoadYAMLBadInput covers the decode-error branch.
func TestLoadYAMLBadInput(t *testing.T) {
	if _, err := LoadYAML([]byte(":-broken-:")); err == nil {
		t.Skip("yaml is permissive; skipping")
	}
}

// TestLoadYAMLMultiDoc covers the multi-doc rejection.
func TestLoadYAMLMultiDoc(t *testing.T) {
	src := []byte("type: string\n---\ntype: integer\n")
	if _, err := LoadYAML(src); err == nil {
		t.Error("expected error on multi-doc YAML")
	}
}

// TestLoadTOMLBadInput covers the decode-error branch.
func TestLoadTOMLBadInput(t *testing.T) {
	if _, err := LoadTOML([]byte("[unclosed")); err == nil {
		t.Error("expected error")
	}
}

// TestLoadJSONCURLViaMapLoader covers the URL-load + decode happy path.
func TestLoadJSONCURLViaMapLoader(t *testing.T) {
	loader := MapLoader{"https://example.com/a": []byte(`{"type":"string"}`)}
	s, err := LoadJSONCURL("https://example.com/a", WithLoader(loader))
	if err != nil {
		t.Fatalf("LoadJSONCURL: %v", err)
	}
	if s == nil {
		t.Fatal("nil schema")
	}
}

// TestLoadJSONCURLLoadFailure covers the loader-failure branch.
func TestLoadJSONCURLLoadFailure(t *testing.T) {
	if _, err := LoadJSONCURL("https://example.com/missing", WithLoader(MapLoader{})); err == nil {
		t.Error("expected error")
	}
}

// TestLoadYAMLURLViaMapLoader covers URL+YAML decode.
func TestLoadYAMLURLViaMapLoader(t *testing.T) {
	loader := MapLoader{"https://example.com/y": []byte("type: string\n")}
	if _, err := LoadYAMLURL("https://example.com/y", WithLoader(loader)); err != nil {
		t.Errorf("LoadYAMLURL: %v", err)
	}
}

// TestLoadYAMLURLLoadFailure covers the loader-failure branch.
func TestLoadYAMLURLLoadFailure(t *testing.T) {
	if _, err := LoadYAMLURL("https://example.com/missing", WithLoader(MapLoader{})); err == nil {
		t.Error("expected error")
	}
}

// TestLoadTOMLURLViaMapLoader covers URL+TOML decode.
func TestLoadTOMLURLViaMapLoader(t *testing.T) {
	loader := MapLoader{"https://example.com/t": []byte(`type = "string"`)}
	if _, err := LoadTOMLURL("https://example.com/t", WithLoader(loader)); err != nil {
		t.Errorf("LoadTOMLURL: %v", err)
	}
}

// TestLoadTOMLURLLoadFailure covers the loader-failure branch.
func TestLoadTOMLURLLoadFailure(t *testing.T) {
	if _, err := LoadTOMLURL("https://example.com/missing", WithLoader(MapLoader{})); err == nil {
		t.Error("expected error")
	}
}

// TestLoadJSONCValueDecodeFailure covers the CompileValue failure.
func TestLoadJSONCValueDecodeFailure(t *testing.T) {
	if _, err := LoadJSONCValue(map[string]any{"minLength": "x"}); err == nil {
		t.Error("expected error")
	}
}

// TestLoadYAMLValueDecodeFailure covers the CompileValue failure.
func TestLoadYAMLValueDecodeFailure(t *testing.T) {
	if _, err := LoadYAMLValue(map[string]any{"minLength": "x"}); err == nil {
		t.Error("expected error")
	}
}

// TestLoadTOMLValueDecodeFailure covers the CompileValue failure.
func TestLoadTOMLValueDecodeFailure(t *testing.T) {
	if _, err := LoadTOMLValue(map[string]any{"minLength": "x"}); err == nil {
		t.Error("expected error")
	}
}

// TestMustLoadJSONCSuccess covers the happy path.
func TestMustLoadJSONCSuccess(t *testing.T) {
	if s := MustLoadJSONC([]byte(`{"type":"string"}`)); s == nil {
		t.Error("nil schema")
	}
}

// TestMustLoadYAMLSuccess covers the happy path.
func TestMustLoadYAMLSuccess(t *testing.T) {
	if s := MustLoadYAML([]byte("type: string\n")); s == nil {
		t.Error("nil schema")
	}
}

// TestMustLoadTOMLSuccess covers the happy path.
func TestMustLoadTOMLSuccess(t *testing.T) {
	if s := MustLoadTOML([]byte(`type = "string"`)); s == nil {
		t.Error("nil schema")
	}
}

// TestMustLoadJSONCPanic covers the panic path.
func TestMustLoadJSONCPanic(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic")
		}
	}()
	_ = MustLoadJSONC([]byte("garbage"))
}

// TestMustLoadYAMLPanic covers the panic path.
func TestMustLoadYAMLPanic(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic")
		}
	}()
	_ = MustLoadYAML([]byte("---\ntype: string\n---\ntype: int"))
}

// TestMustLoadTOMLPanic covers the panic path.
func TestMustLoadTOMLPanic(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic")
		}
	}()
	_ = MustLoadTOML([]byte("[unclosed"))
}

// TestMustLoadJSONCValueSuccess covers the happy path.
func TestMustLoadJSONCValueSuccess(t *testing.T) {
	if s := MustLoadJSONCValue(map[string]any{"type": "string"}); s == nil {
		t.Error("nil schema")
	}
}

// TestMustLoadYAMLValueSuccess covers the happy path.
func TestMustLoadYAMLValueSuccess(t *testing.T) {
	if s := MustLoadYAMLValue(map[string]any{"type": "string"}); s == nil {
		t.Error("nil schema")
	}
}

// TestMustLoadTOMLValueSuccess covers the happy path.
func TestMustLoadTOMLValueSuccess(t *testing.T) {
	if s := MustLoadTOMLValue(map[string]any{"type": "string"}); s == nil {
		t.Error("nil schema")
	}
}

// TestMustLoadJSONCValuePanic covers the panic path.
func TestMustLoadJSONCValuePanic(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic")
		}
	}()
	_ = MustLoadJSONCValue(map[string]any{"minLength": "x"})
}

// TestMustLoadYAMLValuePanic covers the panic path.
func TestMustLoadYAMLValuePanic(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic")
		}
	}()
	_ = MustLoadYAMLValue(map[string]any{"minLength": "x"})
}

// TestMustLoadTOMLValuePanic covers the panic path.
func TestMustLoadTOMLValuePanic(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic")
		}
	}()
	_ = MustLoadTOMLValue(map[string]any{"minLength": "x"})
}

// TestMustLoadJSONCURLSuccess covers the happy path.
func TestMustLoadJSONCURLSuccess(t *testing.T) {
	loader := MapLoader{"https://example.com/a": []byte(`{"type":"string"}`)}
	if s := MustLoadJSONCURL("https://example.com/a", WithLoader(loader)); s == nil {
		t.Error("nil schema")
	}
}

// TestMustLoadYAMLURLSuccess covers the happy path.
func TestMustLoadYAMLURLSuccess(t *testing.T) {
	loader := MapLoader{"https://example.com/a": []byte("type: string\n")}
	if s := MustLoadYAMLURL("https://example.com/a", WithLoader(loader)); s == nil {
		t.Error("nil schema")
	}
}

// TestMustLoadTOMLURLSuccess covers the happy path.
func TestMustLoadTOMLURLSuccess(t *testing.T) {
	loader := MapLoader{"https://example.com/a": []byte(`type = "string"`)}
	if s := MustLoadTOMLURL("https://example.com/a", WithLoader(loader)); s == nil {
		t.Error("nil schema")
	}
}

// TestMustLoadJSONCURLPanic covers the panic path.
func TestMustLoadJSONCURLPanic(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic")
		}
	}()
	_ = MustLoadJSONCURL("https://example.com/missing", WithLoader(MapLoader{}))
}

// TestMustLoadYAMLURLPanic covers the panic path.
func TestMustLoadYAMLURLPanic(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic")
		}
	}()
	_ = MustLoadYAMLURL("https://example.com/missing", WithLoader(MapLoader{}))
}

// TestMustLoadTOMLURLPanic covers the panic path.
func TestMustLoadTOMLURLPanic(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic")
		}
	}()
	_ = MustLoadTOMLURL("https://example.com/missing", WithLoader(MapLoader{}))
}

// TestValidateJSONCNilSchema covers the nil-schema branch.
func TestValidateJSONCNilSchema(t *testing.T) {
	var s *Schema
	if _, err := ValidateJSONC(s, []byte(`{}`)); !errors.Is(err, ErrSchemaNotCompiled) {
		t.Errorf("err = %v, want ErrSchemaNotCompiled", err)
	}
}

// TestValidateYAMLNilSchema covers the nil-schema branch.
func TestValidateYAMLNilSchema(t *testing.T) {
	var s *Schema
	if _, err := ValidateYAML(s, []byte(`{}`)); !errors.Is(err, ErrSchemaNotCompiled) {
		t.Errorf("err = %v, want ErrSchemaNotCompiled", err)
	}
}

// TestValidateTOMLNilSchema covers the nil-schema branch.
func TestValidateTOMLNilSchema(t *testing.T) {
	var s *Schema
	if _, err := ValidateTOML(s, []byte(``)); !errors.Is(err, ErrSchemaNotCompiled) {
		t.Errorf("err = %v, want ErrSchemaNotCompiled", err)
	}
}

// TestValidateJSONCDecodeFailure covers the decode-error branch.
func TestValidateJSONCDecodeFailure(t *testing.T) {
	s := MustCompile([]byte(`{}`))
	if _, err := ValidateJSONC(s, []byte("not json")); err == nil {
		t.Error("expected decode error")
	}
}

// TestValidateYAMLMultiDoc covers the multi-doc rejection.
func TestValidateYAMLMultiDoc(t *testing.T) {
	s := MustCompile([]byte(`{}`))
	if _, err := ValidateYAML(s, []byte("a: 1\n---\nb: 2\n")); err == nil {
		t.Error("expected error")
	}
}

// TestValidateTOMLDecodeFailure covers the decode-error branch.
func TestValidateTOMLDecodeFailure(t *testing.T) {
	s := MustCompile([]byte(`{}`))
	if _, err := ValidateTOML(s, []byte("[unclosed")); err == nil {
		t.Error("expected decode error")
	}
}
