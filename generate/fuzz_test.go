package generate

import (
	"reflect"
	"testing"

	"github.com/go-rotini/jsonschema"
)

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
		if _, err := jsonschema.Compile(bytesOut); err != nil {
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

// seedFuzzGenerate seeds FuzzGenerate with one byte per shape so each shape
// is exercised at least once on a fresh corpus.
func seedFuzzGenerate(f *testing.F) {
	f.Helper()
	for i := uint8(0); i < 6; i++ {
		f.Add(i)
	}
}
