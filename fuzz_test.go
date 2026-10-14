package jsonschema

import (
	"math/big"
	"regexp"
	"strings"
	"testing"
)

// External seed corpus from the JSON Schema Test Suite. The seed list
// below covers the diverse "schema shape" surface from the upstream Draft
// 2020-12 suite — root pointer
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

// FuzzDecimal checks the exact decimal arithmetic against math/big.Rat:
// ordering, equality keys, integer tests and multipleOf divisibility must
// agree for every pair of numbers both parse.
func FuzzDecimal(f *testing.F) {
	for _, s := range [][2]string{{"0.1", "1e-1"}, {"-0", "0"}, {"12.5e3", "1.25e4"}, {"3", "0.3e1"},
		{"7.0", "0.5"}, {"100", "2.5"}, {"1e-20", "1e-21"}, {"390625", "0.0016"}, {"18446744073709551615", "5"}} {
		f.Add(s[0], s[1])
	}
	f.Fuzz(func(t *testing.T, a, b string) {
		if len(a) > 60 || len(b) > 60 {
			return
		}
		ra, okA := new(big.Rat).SetString(a)
		rb, okB := new(big.Rat).SetString(b)
		da, okDA := parseDecimal(a)
		db, okDB := parseDecimal(b)
		if !okA || !okB || !okDA || !okDB || da.nan || db.nan || da.inf || db.inf {
			return
		}
		if c, ok := da.cmp(db); !ok || c != ra.Cmp(rb) {
			t.Fatalf("cmp(%q, %q) = %d, %v; want %d", a, b, c, ok, ra.Cmp(rb))
		}
		var ka, kb strings.Builder
		da.key(&ka)
		db.key(&kb)
		if (ka.String() == kb.String()) != (ra.Cmp(rb) == 0) {
			t.Fatalf("keys of %q and %q: %q, %q", a, b, ka.String(), kb.String())
		}
		if da.isInteger() != ra.IsInt() {
			t.Fatalf("isInteger(%q) = %v", a, da.isInteger())
		}
		if rb.Sign() > 0 {
			want := new(big.Rat).Quo(ra, rb).IsInt()
			if got, ok := newDivisor(db).divides(da); !ok || got != want {
				t.Fatalf("divides(%q, %q) = %v, %v; want %v", a, b, got, ok, want)
			}
		}
	})
}

// FuzzTranslateECMA checks that translating any pattern never panics, and
// that a translation the regex format accepts is one RE2 compiles once its
// placeholders stand in for the constructs RE2 cannot run.
func FuzzTranslateECMA(f *testing.F) {
	for _, s := range []string{`^\d+$`, `[\S\s]`, `(?<n>a)\k<n>`, `(?=a)b`, `\u{1F600}`, `[^\S]`, `\cA\x41A`, `a{2,1}`, `[\b]`} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, p string) {
		if re, err := compileECMA(p); err == nil && re == nil {
			t.Fatalf("compileECMA(%q) returned neither a regexp nor an error", p)
		}
		if validateRegex(p) != nil {
			return
		}
		tr, err := translateECMAAll(p, true)
		if err != nil {
			t.Fatalf("validateRegex accepted %q, which does not translate: %v", p, err)
		}
		if _, err := regexp.Compile(tr.out.String()); err != nil {
			t.Fatalf("validateRegex accepted %q, whose translation %q RE2 rejects: %v", p, tr.out.String(), err)
		}
	})
}

// FuzzValidateYAML and FuzzValidateTOML check that decoding any YAML or
// TOML instance for validation returns a result or an error, never a panic.
func FuzzValidateYAML(f *testing.F) {
	for _, s := range []string{"a: 1\n", "a: &x [1, *x]\n", "a: &x 1\nb: *x\nc: &x 2\nd: *x\n", "- .nan\n- 1e400\n", "<<: {a: 1}\n"} {
		f.Add(s)
	}
	s := MustCompile([]byte(`{"uniqueItems":true,"items":{"multipleOf":0.5},"additionalProperties":{"maximum":10}}`))
	f.Fuzz(func(t *testing.T, doc string) {
		_, _ = ValidateYAML(s, []byte(doc))
	})
}

func FuzzValidateTOML(f *testing.F) {
	for _, s := range []string{"a = 1\n", "a = [nan, 1, inf]\n", "[t]\nx = 1979-05-27T07:32:00Z\n", "a = 0x10\nb = 1e400\n"} {
		f.Add(s)
	}
	s := MustCompile([]byte(`{"additionalProperties":{"uniqueItems":true,"maximum":10,"multipleOf":0.5}}`))
	f.Fuzz(func(t *testing.T, doc string) {
		_, _ = ValidateTOML(s, []byte(doc))
	})
}
