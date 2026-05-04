package jsonschema

import (
	"reflect"
	"testing"
)

// External seed corpus from the JSON Schema Test Suite (§8.4 of the
// requirements doc). The seed list below covers the diverse "schema
// shape" surface from the upstream Draft 2020-12 suite — root pointer
// refs, recursive defs, dynamicRef anchors, allOf / anyOf / oneOf
// branches, format keywords, vocabulary disagreements, infinite-loop
// detection — alongside the legacy in-package adversarial set. Embed
// kept as in-process `[]byte` literals so the fuzz target ships with a
// non-empty corpus by default (rather than relying on testdata
// presence on disk).

// FuzzCompile exercises [Compile] on arbitrary byte input. The invariant is
// "no panics" — any compile failure surfaces as a typed *CompileError, never
// a runtime panic.
func FuzzCompile(f *testing.F) {
	seedFuzzCompile(f)
	f.Fuzz(func(t *testing.T, data []byte) {
		// Recover from any unexpected panic so the fuzz target reports the
		// crash with both the input and the panic value.
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("panic during Compile: %v\ninput: %q", r, data)
			}
		}()
		// Embedded-meta-only loader so fuzz inputs containing $ref to
		// external URLs cannot trigger DNS/TCP timeouts that hang the fuzz
		// worker (and its minimizer). Unknown URIs return a compile error,
		// which is the desired shape for a fuzz target.
		_, _ = Compile(data, WithLoader(embeddedMetaMapLoader()))
	})
}

// FuzzValidate compiles a fuzz-supplied schema and (on success) validates a
// fuzz-supplied instance against it. The invariant is "compile-then-validate
// returns a (*Result, error) pair, never panics".
func FuzzValidate(f *testing.F) {
	seedFuzzValidate(f)
	f.Fuzz(func(t *testing.T, schemaBytes, instanceBytes []byte) {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("panic during validate: %v\nschema: %q\ninstance: %q", r, schemaBytes, instanceBytes)
			}
		}()
		// Embedded-meta-only loader: see FuzzCompile.
		schema, err := Compile(schemaBytes, WithLoader(embeddedMetaMapLoader()))
		if err != nil {
			return
		}
		_, _ = schema.Validate(instanceBytes)
	})
}

// FuzzGenerate exercises [Generate] across a small enumerated set of Go
// shapes selected by an integer drawn from the fuzz seed. The invariant is
// "no panics on supported shapes; output round-trips through Compile".
func FuzzGenerate(f *testing.F) {
	seedFuzzGenerate(f)
	f.Fuzz(func(t *testing.T, choice uint8) {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("panic during Generate(choice=%d): %v", choice, r)
			}
		}()
		v := generateShape(choice)
		schema, err := Generate(v)
		if err != nil {
			// Some shapes (e.g. interface{}-only fields) may legitimately
			// surface a generator error; we treat those as "no panic" and
			// accept the failure.
			return
		}
		// Round-trip: marshaled bytes must compile back to a valid schema.
		bytesOut, err := schema.MarshalJSON()
		if err != nil {
			t.Fatalf("MarshalJSON: %v", err)
		}
		if _, err := Compile(bytesOut); err != nil {
			t.Fatalf("recompile after Generate (choice=%d, type=%s): %v\nschema: %s",
				choice, reflect.TypeOf(v), err, bytesOut)
		}
	})
}

// generateShape returns one of a small enumerated set of Go values keyed by
// the fuzz-supplied choice. Adding new shapes broadens the FuzzGenerate
// invariant surface.
func generateShape(choice uint8) any {
	type leafA struct {
		Name string `json:"name"`
		Age  int    `json:"age,omitempty"`
	}
	type leafB struct {
		Tags  []string `json:"tags"`
		Score float64  `json:"score"`
	}
	type nested struct {
		Inner *leafA            `json:"inner,omitempty"`
		Map   map[string]leafB  `json:"map"`
		List  []leafA           `json:"list"`
		Bools map[string]bool   `json:"bools"`
		Bytes []byte            `json:"bytes"`
		Pairs map[string]string `json:"pairs"`
	}
	type recursive struct {
		Value    string       `json:"value"`
		Children []*recursive `json:"children"`
	}
	switch choice % 6 {
	case 0:
		return (*leafA)(nil)
	case 1:
		return (*leafB)(nil)
	case 2:
		return (*nested)(nil)
	case 3:
		return (*recursive)(nil)
	case 4:
		return ([]string)(nil)
	default:
		return (map[string]int)(nil)
	}
}

// seedFuzzCompile populates the FuzzCompile target with a small corpus of
// edge-case schemas — the same adversarial shapes the conformance + edge
// tests cover, plus a few short literal seeds so go test -fuzz doesn't
// start from an empty pool.
func seedFuzzCompile(f *testing.F) {
	f.Helper()
	for _, seed := range fuzzSeedCorpus {
		f.Add(seed)
	}
}

// seedFuzzValidate seeds the FuzzValidate target with (schema, instance)
// pairs covering valid + invalid combinations.
func seedFuzzValidate(f *testing.F) {
	f.Helper()
	for _, schema := range fuzzSeedCorpus {
		// Pair every seed schema with two instance candidates — valid JSON
		// values selected so at least one branch of common keywords gets
		// exercised, and one non-JSON byte string to stress the decoder.
		f.Add(schema, []byte(`null`))
		f.Add(schema, []byte(`{}`))
		f.Add(schema, []byte(`"hello"`))
		f.Add(schema, []byte(`42`))
		f.Add(schema, []byte(`[]`))
	}
}

// seedFuzzGenerate seeds FuzzGenerate with one byte per shape so each shape
// is exercised at least once on a fresh corpus.
func seedFuzzGenerate(f *testing.F) {
	f.Helper()
	for i := uint8(0); i < 6; i++ {
		f.Add(i)
	}
}

// fuzzSeedCorpus is a curated list of schema bytes that together cover the
// most adversarial Compile inputs the package has historically tripped on,
// plus a curated slice of upstream JSON-Schema-Test-Suite (Draft 2020-12)
// schemas covering the diverse keyword surface.
var fuzzSeedCorpus = [][]byte{
	// In-package adversarial seeds — historically caught panics.
	[]byte(`{}`),
	[]byte(`true`),
	[]byte(`false`),
	[]byte(`{"$ref":"#"}`),
	[]byte(`{"$ref":"#/$defs/missing"}`),
	[]byte(`{"$defs":{"a":{"$ref":"#/$defs/a"}},"$ref":"#/$defs/a"}`),
	[]byte(`{"type":"object","properties":{"x":{"$ref":"#"}}}`),
	[]byte(`{"type":"string","minLength":-1}`),
	[]byte(`{"type":"string","pattern":"["}`),
	[]byte(`{"allOf":[{"type":"string"},{"type":"integer"}]}`),
	[]byte(`{"oneOf":[]}`),
	[]byte(`{"anyOf":[true,false]}`),
	[]byte(`{"$schema":"http://json-schema.org/draft-04/schema#","type":"string"}`),
	[]byte(`{"$schema":"https://json-schema.org/draft/2020-12/schema","$dynamicAnchor":"node","type":"object","properties":{"children":{"type":"array","items":{"$dynamicRef":"#node"}}}}`),
	[]byte(`{"properties":{"a":{"$ref":"#/properties/b"},"b":{"$ref":"#/properties/a"}}}`),
	[]byte(`null`),
	[]byte(`[]`),
	[]byte(`""`),
	// JSON-Schema-Test-Suite (Draft 2020-12) — diverse keyword surface.
	[]byte(`{"$schema":"https://json-schema.org/draft/2020-12/schema","properties":{"foo":{"$ref":"#"}},"additionalProperties":false}`),                                                                                                                             // ref :: root pointer ref
	[]byte(`{"$schema":"https://json-schema.org/draft/2020-12/schema","properties":{"foo":{"type":"integer"},"bar":{"$ref":"#/properties/foo"}}}`),                                                                                                                  // ref :: relative pointer to object
	[]byte(`{"$schema":"https://json-schema.org/draft/2020-12/schema","properties":{"foo":{},"bar":{}},"required":["foo"]}`),                                                                                                                                        // required :: validation
	[]byte(`{"$schema":"https://json-schema.org/draft/2020-12/schema","properties":{"foo":{}}}`),                                                                                                                                                                    // required :: default
	[]byte(`{"$schema":"https://json-schema.org/draft/2020-12/schema","unevaluatedProperties":true}`),                                                                                                                                                               // unevaluatedProperties :: true
	[]byte(`{"$schema":"https://json-schema.org/draft/2020-12/schema","unevaluatedProperties":{"type":"string","minLength":3}}`),                                                                                                                                    // unevaluatedProperties :: schema
	[]byte(`{"$schema":"https://json-schema.org/draft/2020-12/schema","$ref":"https://json-schema.org/draft/2020-12/schema"}`),                                                                                                                                      // defs :: validate against metaschema
	[]byte(`{"$schema":"https://json-schema.org/draft/2020-12/schema","if":{"const":0}}`),                                                                                                                                                                           // if-then-else :: if alone
	[]byte(`{"$schema":"https://json-schema.org/draft/2020-12/schema","then":{"const":0}}`),                                                                                                                                                                         // if-then-else :: then alone
	[]byte(`{"$schema":"https://json-schema.org/draft/2020-12/schema","items":{"type":"integer"}}`),                                                                                                                                                                 // items :: schema
	[]byte(`{"$schema":"https://json-schema.org/draft/2020-12/schema","items":true}`),                                                                                                                                                                               // items :: boolean true
	[]byte(`{"$schema":"https://json-schema.org/draft/2020-12/schema","prefixItems":[{"type":"integer"},{"type":"string"}]}`),                                                                                                                                       // prefixItems :: schemas
	[]byte(`{"$schema":"https://json-schema.org/draft/2020-12/schema","prefixItems":[true,false]}`),                                                                                                                                                                 // prefixItems :: booleans
	[]byte(`{"$schema":"https://json-schema.org/draft/2020-12/schema","patternProperties":{"f.*o":{"type":"integer"}}}`),                                                                                                                                            // patternProperties
	[]byte(`{"$schema":"https://json-schema.org/draft/2020-12/schema","patternProperties":{"a*":{"type":"integer"},"aaa*":{"maximum":20}}}`),                                                                                                                        // patternProperties :: multiple
	[]byte(`{"$schema":"https://json-schema.org/draft/2020-12/schema","propertyNames":{"maxLength":3}}`),                                                                                                                                                            // propertyNames
	[]byte(`{"$schema":"https://json-schema.org/draft/2020-12/schema","propertyNames":{"pattern":"^a+$"}}`),                                                                                                                                                         // propertyNames :: pattern
	[]byte(`{"$schema":"https://json-schema.org/draft/2020-12/schema","contains":{"minimum":5}}`),                                                                                                                                                                   // contains
	[]byte(`{"$schema":"https://json-schema.org/draft/2020-12/schema","contains":{"const":5}}`),                                                                                                                                                                     // contains :: const
	[]byte(`{"$schema":"https://json-schema.org/draft/2020-12/schema","format":"email"}`),                                                                                                                                                                           // format :: email
	[]byte(`{"$schema":"https://json-schema.org/draft/2020-12/schema","format":"idn-email"}`),                                                                                                                                                                       // format :: idn-email
	[]byte(`{"$schema":"https://json-schema.org/draft/2020-12/schema","$defs":{"int":{"type":"integer"}},"allOf":[{"properties":{"foo":{"$ref":"#/$defs/int"}}},{"additionalProperties":{"$ref":"#/$defs/int"}}]}`),                                                 // infinite-loop-detection
	[]byte(`{"$schema":"https://json-schema.org/draft/2020-12/schema","oneOf":[{"type":"integer"},{"minimum":2}]}`),                                                                                                                                                 // oneOf
	[]byte(`{"$schema":"https://json-schema.org/draft/2020-12/schema","type":"string","oneOf":[{"minLength":2},{"maxLength":4}]}`),                                                                                                                                  // oneOf :: with base schema
	[]byte(`{"$schema":"https://json-schema.org/draft/2020-12/schema","allOf":[{"properties":{"bar":{"type":"integer"}},"required":["bar"]},{"properties":{"foo":{"type":"string"}},"required":["foo"]}]}`),                                                         // allOf
	[]byte(`{"$schema":"https://json-schema.org/draft/2020-12/schema","properties":{"bar":{"type":"integer"}},"required":["bar"],"allOf":[{"properties":{"foo":{"type":"string"}},"required":["foo"]},{"properties":{"baz":{"type":"null"}},"required":["baz"]}]}`), // allOf :: with base
	[]byte(`{"$schema":"https://json-schema.org/draft/2020-12/schema","anyOf":[{"type":"integer"},{"minimum":2}]}`),                                                                                                                                                 // anyOf
	[]byte(`{"$schema":"https://json-schema.org/draft/2020-12/schema","type":"string","anyOf":[{"maxLength":2},{"minLength":4}]}`),                                                                                                                                  // anyOf :: with base
	[]byte(`{"$schema":"https://json-schema.org/draft/2020-12/schema","$ref":"http://localhost:1234/draft2020-12/integer.json"}`),                                                                                                                                   // refRemote :: remote ref
	[]byte(`{"$schema":"https://json-schema.org/draft/2020-12/schema","$ref":"http://localhost:1234/draft2020-12/subSchemas.json#/$defs/integer"}`),                                                                                                                 // refRemote :: fragment within remote
}
