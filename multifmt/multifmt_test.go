package multifmt_test

import (
	"errors"
	"sync"
	"testing"

	"github.com/go-rotini/jsonschema"
	"github.com/go-rotini/jsonschema/multifmt"
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

func mustCompileJSON(t *testing.T, schemaJSON string) *jsonschema.Schema {
	t.Helper()
	s, err := jsonschema.Compile([]byte(schemaJSON))
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
	s, err := multifmt.LoadJSONC([]byte(src))
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
	s, err := multifmt.LoadYAML([]byte(src))
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
	s, err := multifmt.LoadTOML([]byte(src))
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
	s := mustCompileJSON(t, kitchenSinkSchemaJSON)
	src := `{
  // an instance with comments + trailing commas
  "name": "alice",
  "age": 30,
  "tags": ["a", "b"],
  "meta": {"score": 0.3,},
}`
	res, err := multifmt.ValidateJSONC(s, []byte(src))
	if err != nil {
		t.Fatalf("ValidateJSONC: %v", err)
	}
	if !res.Valid {
		t.Fatalf("expected valid; errors=%v", res.Errors)
	}
}

func TestValidateYAML_Instance(t *testing.T) {
	s := mustCompileJSON(t, kitchenSinkSchemaJSON)
	src := `name: alice
age: 30
tags: [a, b]
meta:
  score: 0.3
`
	res, err := multifmt.ValidateYAML(s, []byte(src))
	if err != nil {
		t.Fatalf("ValidateYAML: %v", err)
	}
	if !res.Valid {
		t.Fatalf("expected valid; errors=%v", res.Errors)
	}
}

func TestValidateYAML_Aliases(t *testing.T) {
	s := mustCompileJSON(t, kitchenSinkSchemaJSON)
	src := `name: &n alice
age: 30
tags: [*n, b]
meta:
  score: 0.3
`
	res, err := multifmt.ValidateYAML(s, []byte(src))
	if err != nil {
		t.Fatalf("ValidateYAML alias: %v", err)
	}
	if !res.Valid {
		t.Fatalf("expected valid; errors=%v", res.Errors)
	}
}

func TestValidateTOML_Instance(t *testing.T) {
	s := mustCompileJSON(t, kitchenSinkSchemaJSON)
	src := `name = "alice"
age = 30
tags = ["a", "b"]

[meta]
score = 0.3
`
	res, err := multifmt.ValidateTOML(s, []byte(src))
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
	s := mustCompileJSON(t, schema)
	src := `ts = 2024-05-02T13:14:15Z
`
	res, err := multifmt.ValidateTOML(s, []byte(src), jsonschema.WithFormatAssertion(true))
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
	s := mustCompileJSON(t, schemaJSON)

	cases := []struct {
		name string
		fn   func() (*jsonschema.Result, error)
	}{
		{"jsonc", func() (*jsonschema.Result, error) { return multifmt.ValidateJSONC(s, []byte(`0.3`)) }},
		{"yaml", func() (*jsonschema.Result, error) { return multifmt.ValidateYAML(s, []byte(`0.3`)) }},
		{"toml-doc", func() (*jsonschema.Result, error) {
			// TOML can't host a bare scalar at the document root; nest under
			// a property and re-shape the schema accordingly.
			objSchema := mustCompileJSON(t, `{"type":"object","properties":{"x":{"multipleOf":0.1}}}`)
			return multifmt.ValidateTOML(objSchema, []byte("x = 0.3\n"))
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
	schema := mustCompileJSON(t, `{"minimum": 9007199254740993}`)

	t.Run("jsonc", func(t *testing.T) {
		res, err := multifmt.ValidateJSONC(schema, []byte(`9007199254740993`))
		if err != nil {
			t.Fatalf("validate: %v", err)
		}
		if !res.Valid {
			t.Fatalf("expected valid; errors=%v", res.Errors)
		}
	})
	t.Run("toml", func(t *testing.T) {
		objSchema := mustCompileJSON(t, `{"type":"object","properties":{"x":{"minimum":9007199254740993}}}`)
		res, err := multifmt.ValidateTOML(objSchema, []byte("x = 9007199254740993\n"))
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
		res, err := multifmt.ValidateYAML(schema, []byte("9007199254740993\n"))
		if err != nil {
			t.Fatalf("validate: %v", err)
		}
		if !res.Valid {
			t.Fatalf("expected valid; errors=%v", res.Errors)
		}
	})
}

func TestMalformed_ReturnsError_NoPanic(t *testing.T) {
	s := mustCompileJSON(t, `{"type":"object"}`)
	cases := []struct {
		name string
		fn   func() error
	}{
		{"jsonc", func() error {
			_, err := multifmt.ValidateJSONC(s, []byte(`{"a":`))
			return err
		}},
		{"yaml", func() error {
			_, err := multifmt.ValidateYAML(s, []byte("a: [1, 2,\n"))
			return err
		}},
		{"toml", func() error {
			_, err := multifmt.ValidateTOML(s, []byte("a = "))
			return err
		}},
		{"load-jsonc", func() error {
			_, err := multifmt.LoadJSONC([]byte(`{"type":`))
			return err
		}},
		{"load-yaml", func() error {
			_, err := multifmt.LoadYAML([]byte("type: [object,\n"))
			return err
		}},
		{"load-toml", func() error {
			_, err := multifmt.LoadTOML([]byte("type = "))
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
			_, err := multifmt.ValidateJSONC(nil, []byte(`{}`))
			return err
		}},
		{"yaml", func() error {
			_, err := multifmt.ValidateYAML(nil, []byte(`{}`))
			return err
		}},
		{"toml", func() error {
			_, err := multifmt.ValidateTOML(nil, []byte(`x = 1`))
			return err
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.fn()
			if !errors.Is(err, jsonschema.ErrSchemaNotCompiled) {
				t.Fatalf("expected ErrSchemaNotCompiled, got %v", err)
			}
		})
	}
}

func TestConcurrency_NoRace(t *testing.T) {
	t.Parallel()
	s := mustCompileJSON(t, kitchenSinkSchemaJSON)

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
			res, err := multifmt.ValidateJSONC(s, jsonInstance)
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
			res, err := multifmt.ValidateYAML(s, yamlInstance)
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
			res, err := multifmt.ValidateTOML(s, tomlInstance)
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
	s1, err := multifmt.LoadJSONC([]byte(jsoncSchema))
	if err != nil {
		t.Fatalf("LoadJSONC: %v", err)
	}
	s2, err := multifmt.LoadYAML([]byte(yamlSchema))
	if err != nil {
		t.Fatalf("LoadYAML: %v", err)
	}
	s3, err := multifmt.LoadTOML([]byte(tomlSchema))
	if err != nil {
		t.Fatalf("LoadTOML: %v", err)
	}

	good := []byte(`{"x":5}`)
	bad := []byte(`{"x":-1}`)
	for _, s := range []*jsonschema.Schema{s1, s2, s3} {
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
	_, err := multifmt.LoadJSONC([]byte(`{"multipleOf": -1}`))
	if err == nil {
		t.Fatalf("expected compile error")
	}
}
