package jsonschema

import (
	"bytes"
	"encoding/json"
	"reflect"
	"sync"
	"testing"
)

// TestStdlibCompatMarshalRoundTrip confirms that json.Marshal on a *Schema
// returns the schema's source bytes verbatim (no HTML escaping, no field
// reordering). The package's compatibility surface promises this so that a
// *Schema embeds transparently in encoding/json output.
func TestStdlibCompatMarshalRoundTrip(t *testing.T) {
	source := []byte(`{"type":"object","properties":{"a":{"type":"string"}},"required":["a"]}`)
	schema, err := Compile(source)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	got, err := json.Marshal(schema)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	if !bytes.Equal(got, source) {
		t.Errorf("json.Marshal(*Schema)\n got:  %s\n want: %s", got, source)
	}
}

// TestStdlibCompatCompileValueParity confirms that decoding schema bytes via
// json.Unmarshal and feeding the result into CompileValue produces a
// compiled schema equivalent to Compile applied to the same bytes.
func TestStdlibCompatCompileValueParity(t *testing.T) {
	source := []byte(`{
		"$schema": "https://json-schema.org/draft/2020-12/schema",
		"$id": "https://example.com/parity",
		"type": "object",
		"properties": {"x": {"type": "integer"}},
		"required": ["x"]
	}`)
	fromBytes, err := Compile(source)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	var asValue any
	if err := json.Unmarshal(source, &asValue); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	fromValue, err := CompileValue(asValue)
	if err != nil {
		t.Fatalf("CompileValue: %v", err)
	}
	if got, want := fromValue.Draft(), fromBytes.Draft(); got != want {
		t.Errorf("draft mismatch: %v vs %v", got, want)
	}
	if got, want := fromValue.ID(), fromBytes.ID(); got != want {
		t.Errorf("$id mismatch: %q vs %q", got, want)
	}
	// Validate the same instance against both; results must match.
	instance := []byte(`{"x":42}`)
	r1, err := fromBytes.Validate(instance)
	if err != nil {
		t.Fatalf("validate fromBytes: %v", err)
	}
	r2, err := fromValue.Validate(instance)
	if err != nil {
		t.Fatalf("validate fromValue: %v", err)
	}
	if r1.Valid != r2.Valid {
		t.Errorf("validity mismatch: %v vs %v", r1.Valid, r2.Valid)
	}
}

// TestStdlibCompatValidateAndUnmarshal confirms that ValidateAndUnmarshal
// matches json.Unmarshal byte-for-byte on schema-accepted inputs.
func TestStdlibCompatValidateAndUnmarshal(t *testing.T) {
	type person struct {
		Name string `json:"name"`
		Age  int    `json:"age"`
	}
	schema, err := Compile([]byte(`{
		"type":"object",
		"properties":{"name":{"type":"string"},"age":{"type":"integer"}},
		"required":["name","age"]
	}`))
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	instance := []byte(`{"name":"Alice","age":30}`)

	var viaSchema person
	if err := schema.ValidateAndUnmarshal(instance, &viaSchema); err != nil {
		t.Fatalf("ValidateAndUnmarshal: %v", err)
	}
	var viaJSON person
	if err := json.Unmarshal(instance, &viaJSON); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if !reflect.DeepEqual(viaSchema, viaJSON) {
		t.Errorf("decoded values differ:\n  ValidateAndUnmarshal: %+v\n  json.Unmarshal:        %+v",
			viaSchema, viaJSON)
	}
}

// TestStdlibCompatValidateAndUnmarshalRejects confirms that
// ValidateAndUnmarshal does NOT decode into v when validation fails.
func TestStdlibCompatValidateAndUnmarshalRejects(t *testing.T) {
	schema, err := Compile([]byte(`{"type":"object","required":["name"]}`))
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	var v map[string]any
	err = schema.ValidateAndUnmarshal([]byte(`{}`), &v)
	if err == nil {
		t.Fatal("expected validation error, got nil")
	}
	if v != nil {
		t.Errorf("v decoded despite validation failure: %+v", v)
	}
}

// TestStdlibCompatValidateTo exercises the generic ValidateTo[T] entry point
// against three representative T types.
func TestStdlibCompatValidateTo(t *testing.T) {
	t.Run("struct", func(t *testing.T) {
		type pt struct {
			X int `json:"x"`
			Y int `json:"y"`
		}
		schema, err := Compile([]byte(`{"type":"object","properties":{"x":{"type":"integer"},"y":{"type":"integer"}},"required":["x","y"]}`))
		if err != nil {
			t.Fatalf("compile: %v", err)
		}
		got, err := ValidateTo[pt](schema, []byte(`{"x":1,"y":2}`))
		if err != nil {
			t.Fatalf("ValidateTo: %v", err)
		}
		if got.X != 1 || got.Y != 2 {
			t.Errorf("got %+v, want {1 2}", got)
		}
	})
	t.Run("map", func(t *testing.T) {
		schema, err := Compile([]byte(`{"type":"object"}`))
		if err != nil {
			t.Fatalf("compile: %v", err)
		}
		got, err := ValidateTo[map[string]any](schema, []byte(`{"a":1}`))
		if err != nil {
			t.Fatalf("ValidateTo: %v", err)
		}
		if len(got) != 1 {
			t.Errorf("got %v, want one-key map", got)
		}
	})
	t.Run("slice", func(t *testing.T) {
		schema, err := Compile([]byte(`{"type":"array","items":{"type":"string"}}`))
		if err != nil {
			t.Fatalf("compile: %v", err)
		}
		got, err := ValidateTo[[]string](schema, []byte(`["a","b"]`))
		if err != nil {
			t.Fatalf("ValidateTo: %v", err)
		}
		if len(got) != 2 || got[0] != "a" || got[1] != "b" {
			t.Errorf("got %v", got)
		}
	})
}

// TestStdlibCompatConcurrentValidate confirms that a single *Schema is safe
// for concurrent use across many goroutines (race-clean under -race).
func TestStdlibCompatConcurrentValidate(t *testing.T) {
	schema, err := Compile([]byte(`{
		"type":"object",
		"properties":{
			"name":{"type":"string","minLength":1},
			"items":{"type":"array","items":{"type":"integer"}}
		},
		"required":["name"]
	}`))
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	const workers = 32
	const iterations = 200
	var wg sync.WaitGroup
	wg.Add(workers)
	errCh := make(chan error, workers)
	instances := [][]byte{
		[]byte(`{"name":"alice","items":[1,2,3]}`),
		[]byte(`{"name":"bob","items":[]}`),
		[]byte(`{"name":"carol"}`),
		[]byte(`{"items":[1]}`), // expected invalid (missing name)
		[]byte(`{"name":"","items":[1]}`),
	}
	for w := 0; w < workers; w++ {
		go func(id int) {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				inst := instances[(id+i)%len(instances)]
				res, err := schema.Validate(inst)
				if err != nil {
					errCh <- err
					return
				}
				if res == nil {
					errCh <- errNilResult
					return
				}
			}
		}(w)
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		t.Errorf("concurrent validate: %v", err)
	}
}

// errNilResult is the static sentinel for the concurrent-validate test.
var errNilResult = stdlibError("nil result")

// stdlibError is a tiny string-error type used by the stdlib compat tests.
type stdlibError string

func (e stdlibError) Error() string { return string(e) }
