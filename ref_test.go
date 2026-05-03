package jsonschema

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestResolveURIAbsoluteRef(t *testing.T) {
	out, err := resolveURI("https://example.com/a", "https://other.com/x")
	if err != nil {
		t.Fatalf("resolveURI: %v", err)
	}
	if out != "https://other.com/x" {
		t.Errorf("resolveURI = %q", out)
	}
}

func TestResolveURIRelativeRef(t *testing.T) {
	out, err := resolveURI("https://example.com/a/b", "c")
	if err != nil {
		t.Fatalf("resolveURI: %v", err)
	}
	if out != "https://example.com/a/c" {
		t.Errorf("resolveURI = %q", out)
	}
}

func TestResolveURIFragmentOnly(t *testing.T) {
	out, err := resolveURI("https://example.com/a", "#/$defs/x")
	if err != nil {
		t.Fatalf("resolveURI: %v", err)
	}
	if out != "https://example.com/a#/$defs/x" {
		t.Errorf("resolveURI = %q", out)
	}
}

func TestResolveURIEmptyBase(t *testing.T) {
	out, err := resolveURI("", "https://example.com/a")
	if err != nil {
		t.Fatalf("resolveURI: %v", err)
	}
	if out != "https://example.com/a" {
		t.Errorf("resolveURI = %q", out)
	}
}

func TestResolveURIBothEmpty(t *testing.T) {
	out, err := resolveURI("", "")
	if err != nil {
		t.Fatalf("resolveURI: %v", err)
	}
	if out != "" {
		t.Errorf("resolveURI = %q", out)
	}
}

func TestSplitFragment(t *testing.T) {
	cases := []struct {
		in       string
		wantBase string
		wantFrag string
	}{
		{"https://example.com/a", "https://example.com/a", ""},
		{"https://example.com/a#", "https://example.com/a", "#"},
		{"https://example.com/a#/x", "https://example.com/a", "#/x"},
		{"#name", "", "#name"},
		{"", "", ""},
	}
	for _, c := range cases {
		base, frag := splitFragment(c.in)
		if base != c.wantBase || frag != c.wantFrag {
			t.Errorf("splitFragment(%q) = (%q, %q), want (%q, %q)", c.in, base, frag, c.wantBase, c.wantFrag)
		}
	}
}

func TestWalkResourceSimple(t *testing.T) {
	value, err := decodeSchemaBytes([]byte(`{"type":"string"}`))
	if err != nil {
		t.Fatal(err)
	}
	rm := newResourceMap()
	if err := walkResource(rm, value, "", Draft202012); err != nil {
		t.Fatalf("walkResource: %v", err)
	}
	if len(rm.byURI) != 1 {
		t.Errorf("expected 1 resource, got %d", len(rm.byURI))
	}
}

func TestWalkResourceNestedID(t *testing.T) {
	src := `{"$id":"https://example.com/root","$defs":{"x":{"$id":"https://example.com/x","type":"string"}}}`
	value, err := decodeSchemaBytes([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	rm := newResourceMap()
	if err := walkResource(rm, value, "https://example.com/root", Draft202012); err != nil {
		t.Fatalf("walkResource: %v", err)
	}
	if _, ok := rm.byURI["https://example.com/root"]; !ok {
		t.Error("missing root resource")
	}
	if _, ok := rm.byURI["https://example.com/x"]; !ok {
		t.Error("missing nested resource")
	}
}

func TestWalkResourceAnchors(t *testing.T) {
	src := `{"$defs":{"x":{"$anchor":"foo","type":"string"},"y":{"$dynamicAnchor":"bar","type":"number"}}}`
	value, err := decodeSchemaBytes([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	rm := newResourceMap()
	if err := walkResource(rm, value, "", Draft202012); err != nil {
		t.Fatalf("walkResource: %v", err)
	}
	root := rm.byURI[""]
	if root == nil {
		t.Fatal("missing root resource")
	}
	if _, ok := root.anchors["foo"]; !ok {
		t.Error("missing anchor 'foo'")
	}
	if _, ok := root.dynamicAnchors["bar"]; !ok {
		t.Error("missing dynamic anchor 'bar'")
	}
}

func TestResolveRefFragmentOnly(t *testing.T) {
	src := `{"$defs":{"foo":{"type":"string"}}}`
	value, err := decodeSchemaBytes([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	rm := newResourceMap()
	if err := walkResource(rm, value, "", Draft202012); err != nil {
		t.Fatal(err)
	}
	r, err := resolveRef(rm, nil, "", "#/$defs/foo", nil, Draft202012)
	if err != nil {
		t.Fatalf("resolveRef: %v", err)
	}
	if r.Lazy {
		t.Error("expected eager resolution, got lazy")
	}
	if r.Target == nil {
		t.Error("expected non-nil target")
	}
}

func TestResolveRefPlainNameAnchor(t *testing.T) {
	src := `{"$defs":{"x":{"$anchor":"bar","type":"number"}}}`
	value, err := decodeSchemaBytes([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	rm := newResourceMap()
	if err := walkResource(rm, value, "https://example.com/", Draft202012); err != nil {
		t.Fatal(err)
	}
	r, err := resolveRef(rm, nil, "https://example.com/", "#bar", nil, Draft202012)
	if err != nil {
		t.Fatalf("resolveRef: %v", err)
	}
	if r.Target == nil {
		t.Error("expected non-nil target for plain-name anchor")
	}
}

func TestResolveRefAbsoluteFragment(t *testing.T) {
	src := `{"$id":"https://example.com/root","$defs":{"foo":{"type":"string"}}}`
	value, err := decodeSchemaBytes([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	rm := newResourceMap()
	if err := walkResource(rm, value, "https://example.com/root", Draft202012); err != nil {
		t.Fatal(err)
	}
	r, err := resolveRef(rm, nil, "", "https://example.com/root#/$defs/foo", nil, Draft202012)
	if err != nil {
		t.Fatalf("resolveRef: %v", err)
	}
	if r.Target == nil {
		t.Error("expected non-nil target for absolute ref")
	}
}

func TestResolveRefSelfLoop(t *testing.T) {
	value, err := decodeSchemaBytes([]byte(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	rm := newResourceMap()
	if err := walkResource(rm, value, "https://example.com/", Draft202012); err != nil {
		t.Fatal(err)
	}
	stack := []string{"https://example.com/"}
	r, err := resolveRef(rm, nil, "https://example.com/", "#", stack, Draft202012)
	if err != nil {
		t.Fatalf("resolveRef: %v", err)
	}
	if !r.Lazy {
		t.Error("expected lazy edge for self-loop")
	}
}

func TestResolveRefMalformed(t *testing.T) {
	rm := newResourceMap()
	rm.byURI[""] = &resource{baseURI: "", root: map[string]any{}, anchors: map[string]any{}, dynamicAnchors: map[string]any{}, draft: Draft202012}
	rm.rootURI = ""
	_, err := resolveRef(rm, nil, "", "#missing-anchor", nil, Draft202012)
	if err == nil {
		t.Error("expected error for missing anchor")
	}
	var refErr *RefError
	if !errors.As(err, &refErr) {
		t.Errorf("err type = %T, want *RefError", err)
	}
}

func TestJSONPointer(t *testing.T) {
	root := map[string]any{
		"a": map[string]any{
			"b": []any{"x", "y", "z"},
		},
	}
	v, err := jsonPointer(root, "/a/b/1")
	if err != nil {
		t.Fatalf("jsonPointer: %v", err)
	}
	if v != "y" {
		t.Errorf("got %v, want y", v)
	}
}

func TestJSONPointerEscapes(t *testing.T) {
	root := map[string]any{"a/b": map[string]any{"c~d": "ok"}}
	v, err := jsonPointer(root, "/a~1b/c~0d")
	if err != nil {
		t.Fatalf("jsonPointer: %v", err)
	}
	if v != "ok" {
		t.Errorf("got %v", v)
	}
}

func TestJSONPointerOutOfRange(t *testing.T) {
	root := []any{"x"}
	if _, err := jsonPointer(root, "/3"); err == nil {
		t.Error("expected out-of-range error")
	}
}

func TestJSONPointerInvalidIndex(t *testing.T) {
	root := []any{"x"}
	if _, err := jsonPointer(root, "/foo"); err == nil {
		t.Error("expected invalid-index error")
	}
}

// TestRefSelfLoop verifies that a top-level {"$ref": "#"} terminates
// (via WithMaxRefDepth) instead of recursing forever.
func TestRefSelfLoop(t *testing.T) {
	schema, err := Compile([]byte(`{"$ref":"#"}`))
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	res, err := schema.Validate([]byte(`null`))
	if err != nil {
		// Some configurations surface the depth limit as an error rather
		// than a result error; both are acceptable.
		if !strings.Contains(err.Error(), "max ref depth") {
			t.Fatalf("validate: %v", err)
		}
		return
	}
	if res == nil {
		t.Fatal("nil result")
	}
	if res.Valid {
		return
	}
	found := false
	for _, e := range res.Errors {
		if strings.Contains(e.Message, "max ref depth") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected max-ref-depth error in result, got: %+v", res.Errors)
	}
}

// TestRefBillionLaughs builds a schema where each $defs entry references
// the next several times. The expansion would be exponential under naive
// inlining; with WithMaxRefDepth in place the validator must terminate
// cleanly.
func TestRefBillionLaughs(t *testing.T) {
	const levels = 12
	var b strings.Builder
	b.WriteString(`{"$defs":{`)
	for i := 0; i <= levels; i++ {
		if i > 0 {
			b.WriteString(",")
		}
		if i == levels {
			fmt.Fprintf(&b, `"l%d":{"type":"string"}`, i)
			continue
		}
		fmt.Fprintf(&b, `"l%d":{"allOf":[{"$ref":"#/$defs/l%d"},{"$ref":"#/$defs/l%d"},{"$ref":"#/$defs/l%d"}]}`, i, i+1, i+1, i+1)
	}
	b.WriteString(`},"$ref":"#/$defs/l0"}`)
	schema, err := Compile([]byte(b.String()), WithMaxRefDepth(50))
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	res, err := schema.Validate([]byte(`"hi"`))
	if err != nil {
		if strings.Contains(err.Error(), "max ref depth") {
			return
		}
		t.Fatalf("validate: %v", err)
	}
	if res == nil {
		t.Fatal("nil result")
	}
	// Either Valid:true (validation bounded) or a max-ref-depth error are
	// acceptable; the only failure mode is stack overflow / OOM.
	_ = res
}

// TestRefMutuallyRecursiveAcrossIDs exercises two $id-bounded resources
// that reference one another via absolute refs, then validates an instance
// that alternates between them.
func TestRefMutuallyRecursiveAcrossIDs(t *testing.T) {
	schema, err := Compile([]byte(`{
		"$id": "https://example.com/root",
		"$schema": "https://json-schema.org/draft/2020-12/schema",
		"$defs": {
			"a": {
				"$id": "https://example.com/a",
				"type": "object",
				"properties": {"next": {"$ref": "https://example.com/b"}}
			},
			"b": {
				"$id": "https://example.com/b",
				"type": "object",
				"properties": {"next": {"$ref": "https://example.com/a"}}
			}
		},
		"$ref": "https://example.com/a"
	}`))
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	res, err := schema.Validate([]byte(`{"next":{"next":{"next":{}}}}`))
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	if !res.Valid {
		t.Fatalf("expected valid; got %+v", res.Errors)
	}
}

// TestRefPlainNameAnchorEndToEnd exercises plain-name fragment refs (#name)
// that land on a $anchor declaration in a nested $defs entry, end-to-end
// through Compile + Validate.
func TestRefPlainNameAnchorEndToEnd(t *testing.T) {
	schema, err := Compile([]byte(`{
		"$schema": "https://json-schema.org/draft/2020-12/schema",
		"$id": "https://example.com/anchored",
		"$defs": {
			"named": {
				"$anchor": "myThing",
				"type": "string",
				"minLength": 2
			}
		},
		"$ref": "#myThing"
	}`))
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	res, err := schema.Validate([]byte(`"ok"`))
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	if !res.Valid {
		t.Fatalf("expected valid; got errors: %+v", res.Errors)
	}
	res, err = schema.Validate([]byte(`"x"`))
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	if res.Valid {
		t.Fatal("expected anchor-targeted minLength=2 to reject single-char string")
	}
}

// TestRefRecursiveDynamic exercises a $dynamicRef that recurses through
// itself across nested instance values. The canonical use case is a tree
// schema where each node carries a list of children referencing the same
// schema. With WithMaxRefDepth and instance-bounded recursion, the walk
// must terminate.
func TestRefRecursiveDynamic(t *testing.T) {
	schema, err := Compile([]byte(`{
		"$schema": "https://json-schema.org/draft/2020-12/schema",
		"$id": "https://example.com/tree",
		"$dynamicAnchor": "node",
		"type": "object",
		"properties": {
			"value": {"type": "string"},
			"children": {
				"type": "array",
				"items": {"$dynamicRef": "#node"}
			}
		}
	}`))
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	tree := []byte(`{"value":"root","children":[{"value":"a","children":[]},{"value":"b","children":[{"value":"b1","children":[]}]}]}`)
	res, err := schema.Validate(tree)
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	if !res.Valid {
		t.Fatalf("expected valid; got errors: %+v", res.Errors)
	}
}
