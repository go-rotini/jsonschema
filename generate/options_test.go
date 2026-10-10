package generate

import (
	"bytes"
	"encoding/json"
	"testing"
)

// TestWithGenerateOrderedProperties_True confirms that with ordering enabled
// (the default), the generated schema's properties slot is rendered in Go
// declaration order. We assert by comparing the byte order of the property
// names in the marshaled output.
func TestWithGenerateOrderedProperties_True(t *testing.T) {
	type item struct {
		Charlie string `json:"charlie"`
		Alpha   string `json:"alpha"`
		Bravo   string `json:"bravo"`
	}
	data, err := Bytes(item{}, WithOrderedProperties(true))
	if err != nil {
		t.Fatalf("Bytes: %v", err)
	}
	idxC := bytes.Index(data, []byte(`"charlie"`))
	idxA := bytes.Index(data, []byte(`"alpha"`))
	idxB := bytes.Index(data, []byte(`"bravo"`))
	if idxC < 0 || idxA < 0 || idxB < 0 {
		t.Fatalf("missing property in output: %s", data)
	}
	if !(idxC < idxA && idxA < idxB) {
		t.Errorf("ordered properties not preserved: charlie@%d alpha@%d bravo@%d\n%s",
			idxC, idxA, idxB, data)
	}
}

// TestWithGenerateOrderedProperties_False confirms ordering disabled emits
// a plain map[string]any so callers downstream do not depend on a stable
// key order. We do not assert the actual order (it is map-iteration
// dependent); we assert only that all three properties round-trip through
// json.Unmarshal cleanly.
func TestWithGenerateOrderedProperties_False(t *testing.T) {
	type item struct {
		Charlie string `json:"charlie"`
		Alpha   string `json:"alpha"`
		Bravo   string `json:"bravo"`
	}
	data, err := Bytes(item{}, WithOrderedProperties(false))
	if err != nil {
		t.Fatalf("Bytes: %v", err)
	}
	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("Unmarshal: %v\n%s", err, data)
	}
	props, ok := doc["properties"].(map[string]any)
	if !ok {
		t.Fatalf("properties slot missing or wrong shape: %s", data)
	}
	for _, k := range []string{"charlie", "alpha", "bravo"} {
		if _, ok := props[k]; !ok {
			t.Errorf("property %q missing from generated output", k)
		}
	}
}
