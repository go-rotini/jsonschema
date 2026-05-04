package jsonschema

import (
	"encoding/json"
	"errors"
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
