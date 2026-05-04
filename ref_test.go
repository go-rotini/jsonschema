package jsonschema

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
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

// TestRefRecursiveDraft201909 covers the Draft 2019-09 $recursiveRef /
// $recursiveAnchor pair. With $recursiveAnchor:true at the root, a
// nested $recursiveRef:"#" must resolve back to the outermost dynamic
// scope so a recursive tree validates end-to-end. The negative case
// pins the type-mismatch surface at the deepest descent.
func TestRefRecursiveDraft201909_OutermostResolution(t *testing.T) {
	schema, err := Compile([]byte(`{
		"$schema": "https://json-schema.org/draft/2019-09/schema",
		"$id": "https://example.com/tree-2019",
		"$recursiveAnchor": true,
		"type": "object",
		"properties": {
			"value": {"type": "string"},
			"children": {
				"type": "array",
				"items": {"$recursiveRef": "#"}
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
	bad := []byte(`{"value":"root","children":[{"value":42,"children":[]}]}`)
	res, err = schema.Validate(bad)
	if err != nil {
		t.Fatalf("validate bad: %v", err)
	}
	if res.Valid {
		t.Fatalf("expected invalid (children[0].value should be string)")
	}
	// The error must surface at the descended location with the right
	// keyword, confirming the recursive resolution actually reached the
	// inner node rather than short-circuiting.
	var hit bool
	for _, e := range res.Errors {
		if e.Keyword == "type" && e.InstanceLocation == "/children/0/value" {
			hit = true
			break
		}
	}
	if !hit {
		t.Errorf("expected type error at /children/0/value; got %+v", res.Errors)
	}
}

// TestRefRecursiveDraft201909_FallbackWithoutAnchor verifies the
// fallback semantics: when no $recursiveAnchor:true is in scope,
// $recursiveRef behaves like a static $ref. The same tree schema
// without the anchor still validates because "#" still resolves
// statically to the root resource.
func TestRefRecursiveDraft201909_FallbackWithoutAnchor(t *testing.T) {
	schema, err := Compile([]byte(`{
		"$schema": "https://json-schema.org/draft/2019-09/schema",
		"$id": "https://example.com/tree-no-anchor",
		"type": "object",
		"properties": {
			"value": {"type": "string"},
			"children": {
				"type": "array",
				"items": {"$recursiveRef": "#"}
			}
		}
	}`))
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	res, err := schema.Validate([]byte(`{"value":"root","children":[{"value":"x","children":[]}]}`))
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	if !res.Valid {
		t.Fatalf("expected valid; got errors: %+v", res.Errors)
	}
	// A type mismatch at a descended node still surfaces.
	res, err = schema.Validate([]byte(`{"value":"root","children":[{"value":42,"children":[]}]}`))
	if err != nil {
		t.Fatalf("validate bad: %v", err)
	}
	if res.Valid {
		t.Fatalf("expected invalid for non-string nested value")
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

// TestRefMaxDepthExceeded covers the depth-limit branch.
func TestRefMaxDepthExceeded(t *testing.T) {
	src := []byte(`{"$ref":"#"}`)
	s, err := Compile(src, WithMaxRefDepth(2))
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	res, err := s.Validate([]byte(`null`))
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if res.Valid {
		t.Fatal("expected invalid (max ref depth)")
	}
	have := false
	for _, e := range res.Errors {
		if e.Keyword == "$ref" && strings.Contains(e.Message, "max ref depth") {
			have = true
			break
		}
	}
	if !have {
		t.Errorf("expected max ref depth error in %v", res.Errors)
	}
}

// TestDynamicRefMaxDepth covers the depth-limit branch on $dynamicRef.
func TestDynamicRefMaxDepth(t *testing.T) {
	src := []byte(`{
		"$id":"https://example.com/x",
		"$dynamicAnchor":"loop",
		"properties":{"x":{"$dynamicRef":"#loop"}}
	}`)
	s, err := Compile(src, WithMaxRefDepth(50))
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	// Build a deeply nested instance.
	deep := strings.Repeat(`{"x":`, 200) + `null` + strings.Repeat(`}`, 200)
	res, err := s.Validate([]byte(deep))
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if res.Valid {
		// Either invalid via depth limit or via deeper validation.
		t.Logf("deep validation invalid (errors=%d)", len(res.Errors))
	}
}

// TestRecursiveRefBasic exercises Draft 2019-09 $recursiveRef.
func TestRecursiveRefBasic(t *testing.T) {
	src := []byte(`{
		"$schema":"https://json-schema.org/draft/2019-09/schema",
		"$id":"https://example.com/tree",
		"$recursiveAnchor":true,
		"type":"object",
		"properties":{"children":{"type":"array","items":{"$recursiveRef":"#"}}}
	}`)
	s, err := Compile(src)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	if res, _ := s.Validate([]byte(`{"children":[{"children":[]},{}]}`)); !res.Valid {
		t.Errorf("expected valid; errors=%v", res.Errors)
	}
	if res, _ := s.Validate([]byte(`{"children":[{"children":"not-array"}]}`)); res.Valid {
		t.Error("expected invalid (children: not-array)")
	}
}

// TestRefStaticTargetMissing covers the cannot-resolve branch.
func TestRefStaticTargetMissing(t *testing.T) {
	src := []byte(`{"$ref":"#/$defs/missing"}`)
	if _, err := Compile(src); err == nil {
		t.Error("expected error for missing $ref target")
	}
}

// TestDynamicRefFallback covers the staticTarget fallback when no dynamic
// scope match is found.
func TestDynamicRefFallback(t *testing.T) {
	src := []byte(`{
		"$id":"https://example.com/x",
		"$defs":{"alpha":{"$dynamicAnchor":"alpha","type":"integer"}},
		"$dynamicRef":"#alpha"
	}`)
	s, err := Compile(src)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	res, _ := s.Validate([]byte(`5`))
	if !res.Valid {
		t.Errorf("expected valid; errors=%v", res.Errors)
	}
	res, _ = s.Validate([]byte(`"x"`))
	if res.Valid {
		t.Error("expected invalid")
	}
}

// TestDynamicRefNoFragmentName exercises the empty-fragment-name branch.
func TestDynamicRefNoFragmentName(t *testing.T) {
	// $dynamicRef without a #fragment falls back like $ref.
	src := []byte(`{
		"$id":"https://example.com/x",
		"$defs":{"a":{"type":"integer"}},
		"$dynamicRef":"#/$defs/a"
	}`)
	s, err := Compile(src)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	res, _ := s.Validate([]byte(`5`))
	if !res.Valid {
		t.Errorf("expected valid; errors=%v", res.Errors)
	}
}

// TestRefViaCompilerCache covers compileURL with successive cache + flight.
func TestRefViaCompilerCache(t *testing.T) {
	c := NewCompiler(WithLoader(MapLoader{
		"https://example.com/t": []byte(`{"$id":"https://example.com/t","type":"integer"}`),
	}))
	// Compile the URL twice; second should hit cache.
	a, err := c.CompileURL("https://example.com/t")
	if err != nil {
		t.Fatalf("first: %v", err)
	}
	b, err := c.CompileURL("https://example.com/t")
	if err != nil {
		t.Fatalf("second: %v", err)
	}
	if a != b {
		t.Errorf("expected cached pointer; a=%p b=%p", a, b)
	}
}

// TestCompileURLConcurrentSingleflightCov covers the concurrent single-flight
// path for the same URI.
func TestCompileURLConcurrentSingleflightCov(t *testing.T) {
	loader := MapLoader{
		"https://example.com/u": []byte(`{"type":"string"}`),
	}
	c := NewCompiler(WithLoader(loader))
	const N = 10
	done := make(chan error, N)
	for range N {
		go func() {
			_, err := c.CompileURL("https://example.com/u")
			done <- err
		}()
	}
	for range N {
		if err := <-done; err != nil {
			t.Errorf("CompileURL: %v", err)
		}
	}
}

// TestRefCycleViaDeepRecursion covers the cycle-bounded-by-depth path.
func TestRefCycleViaDeepRecursion(t *testing.T) {
	src := []byte(`{
		"$id":"https://example.com/x",
		"$ref":"#/$defs/a",
		"$defs":{
			"a":{"$ref":"#/$defs/b"},
			"b":{"$ref":"#/$defs/a"}
		}
	}`)
	s, err := Compile(src, WithMaxRefDepth(5))
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	res, _ := s.Validate([]byte(`null`))
	// May or may not error depending on cycle handling; check no panic.
	_ = res
}

// TestPropertyRefAcrossResources exercises walkResource branches.
func TestPropertyRefAcrossResources(t *testing.T) {
	src := []byte(`{
		"$id":"https://example.com/root",
		"$defs":{
			"sub":{"$id":"https://example.com/sub","type":"string","minLength":3}
		},
		"properties":{
			"x":{"$ref":"https://example.com/sub"}
		}
	}`)
	s := MustCompile(src)
	if res, _ := s.Validate([]byte(`{"x":"hello"}`)); !res.Valid {
		t.Errorf("expected valid; errors=%v", res.Errors)
	}
	if res, _ := s.Validate([]byte(`{"x":"hi"}`)); res.Valid {
		t.Error("expected invalid (too short)")
	}
}

// TestPatternPropertiesViaCompile covers patternProperties walker.
func TestPatternPropertiesViaCompile(t *testing.T) {
	src := []byte(`{
		"type":"object",
		"patternProperties":{
			"^[a-z]+$":{"type":"integer"}
		}
	}`)
	s := MustCompile(src)
	if res, _ := s.Validate([]byte(`{"abc":42}`)); !res.Valid {
		t.Errorf("expected valid; errors=%v", res.Errors)
	}
	if res, _ := s.Validate([]byte(`{"abc":"x"}`)); res.Valid {
		t.Error("expected invalid")
	}
}

// TestUnknownDraftCompileURL covers the URL-load + compile-failure path.
func TestUnknownDraftCompileURL(t *testing.T) {
	c := NewCompiler(WithLoader(MapLoader{
		"https://example.com/bad": []byte(`{"minLength":"three"}`),
	}))
	if _, err := c.CompileURL("https://example.com/bad"); err == nil {
		t.Error("expected compile error")
	}
}

// TestResolveURIErrorPaths covers various error branches.
func TestResolveURIErrorPaths(t *testing.T) {
	if _, err := resolveURI("https://example.com/", "://broken"); err == nil {
		// May or may not error; net/url is lenient.
		t.Logf("broken ref accepted")
	}
	// Joining a relative ref against a relative base
	if _, err := resolveURI("relative-base/", "x"); err == nil {
		t.Logf("relative base+ref accepted")
	}
}

// TestParentPointerEmptyAndShallow covers parentPointer's branches.
func TestParentPointerEmptyAndShallow(t *testing.T) {
	if got := parentPointer(""); got != "" {
		t.Errorf("parentPointer(\"\") = %q", got)
	}
	if got := parentPointer("/foo"); got != "" {
		t.Errorf("parentPointer(\"/foo\") = %q", got)
	}
	if got := parentPointer("/foo/bar"); got != "/foo" {
		t.Errorf("parentPointer(\"/foo/bar\") = %q", got)
	}
}

// TestSplitPointerEmpty covers the empty-pointer branch.
func TestSplitPointerEmpty(t *testing.T) {
	if got := splitPointer(""); got != nil {
		t.Errorf("splitPointer(\"\") = %v", got)
	}
}

// TestResolveURIBranches covers various inputs.
func TestResolveURIBranches(t *testing.T) {
	if _, err := resolveURI("", "https://example.com/x"); err != nil {
		t.Errorf("absolute: %v", err)
	}
	if _, err := resolveURI("https://e/", "/x"); err != nil {
		t.Errorf("rooted relative: %v", err)
	}
	if _, err := resolveURI("https://e/", "rel"); err != nil {
		t.Errorf("simple relative: %v", err)
	}
	// Empty inputs → returns ""
	if got, err := resolveURI("", ""); err != nil || got != "" {
		t.Errorf("empty/empty: got=%q err=%v", got, err)
	}
}

// TestConcurrentLazyRefValidate exercises the runtime ref-resolution path
// from many goroutines simultaneously, against a single shared *Schema.
// The schema mixes `$dynamicRef`, `$ref` cycles, and sibling-dependent
// keywords (if/then/else, contains/maxContains/minContains, required's
// readOnly/writeOnly scan) so any per-call build state read off the wrong
// frame surfaces as an incorrect validity verdict — not just a race-flag.
//
// The bug Phase 2.5 fixed was that evalBuilder kept compile-time scratch
// state (currentParent / currentLoc / currentBase / currentResource /
// draft) on the shared struct. Two goroutines validating concurrently
// would trample each other's scratch state mid-build at runtime, producing
// observably wrong validation outputs in 5-15% of runs. Per-call frames
// (buildFrame) eliminate the shared mutable state entirely.
//
// The test asserts that every goroutine's verdict matches the expected
// verdict computed up front against a single-threaded validation.
func TestConcurrentLazyRefValidate(t *testing.T) {
	const schemaSrc = `{
		"$schema": "https://json-schema.org/draft/2020-12/schema",
		"$id": "https://example.com/concurrent-lazy-ref",
		"$dynamicAnchor": "node",
		"type": "object",
		"properties": {
			"kind": {"type": "string", "enum": ["leaf", "branch"]},
			"value": {"type": "integer"},
			"children": {
				"type": "array",
				"items": {"$dynamicRef": "#node"},
				"contains": {
					"type": "object",
					"properties": {"kind": {"const": "leaf"}}
				},
				"minContains": 0
			},
			"meta": {"$ref": "#/$defs/meta"}
		},
		"required": ["kind"],
		"if": {"properties": {"kind": {"const": "leaf"}}},
		"then": {"required": ["value"]},
		"else": {"required": ["children"]},
		"$defs": {
			"meta": {
				"type": "object",
				"properties": {
					"tag": {"type": "string", "minLength": 1},
					"flag": {"type": "boolean"}
				},
				"required": ["tag"]
			}
		}
	}`

	schema, err := Compile([]byte(schemaSrc))
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	// Each case is a (instance, expectedValid) pair. The mix of valid and
	// invalid instances gives the test bite: a corrupted scratch frame
	// could flip a verdict in either direction.
	cases := []struct {
		name  string
		raw   []byte
		valid bool
	}{
		{
			name:  "leaf-valid",
			raw:   []byte(`{"kind":"leaf","value":42,"meta":{"tag":"a"}}`),
			valid: true,
		},
		{
			name:  "leaf-missing-value",
			raw:   []byte(`{"kind":"leaf","meta":{"tag":"a"}}`),
			valid: false,
		},
		{
			name:  "branch-valid",
			raw:   []byte(`{"kind":"branch","children":[{"kind":"leaf","value":1},{"kind":"leaf","value":2}]}`),
			valid: true,
		},
		{
			name:  "branch-missing-children",
			raw:   []byte(`{"kind":"branch"}`),
			valid: false,
		},
		{
			name:  "branch-deep-valid",
			raw:   []byte(`{"kind":"branch","children":[{"kind":"branch","children":[{"kind":"leaf","value":7}]}],"meta":{"tag":"x"}}`),
			valid: true,
		},
		{
			name:  "branch-deep-invalid-leaf",
			raw:   []byte(`{"kind":"branch","children":[{"kind":"branch","children":[{"kind":"leaf"}]}]}`),
			valid: false,
		},
		{
			name:  "leaf-bad-meta",
			raw:   []byte(`{"kind":"leaf","value":1,"meta":{"flag":true}}`),
			valid: false,
		},
	}

	// Sanity-check the expected verdicts up front so a botched test schema
	// fails loudly here rather than masquerading as a concurrency hit.
	for _, c := range cases {
		res, err := schema.Validate(c.raw)
		if err != nil {
			t.Fatalf("baseline validate %s: %v", c.name, err)
		}
		if res.Valid != c.valid {
			t.Fatalf("baseline %s: expected valid=%v, got valid=%v errors=%v",
				c.name, c.valid, res.Valid, res.Errors)
		}
	}

	const workers = 32
	const iterations = 1000

	type mismatch struct {
		worker, iter int
		caseName     string
		expected     bool
		got          bool
		errs         []ValidationError
	}

	var (
		wg sync.WaitGroup
		mu sync.Mutex
	)
	var mismatches []mismatch

	wg.Add(workers)
	for w := 0; w < workers; w++ {
		go func(id int) {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				c := cases[(id+i)%len(cases)]
				// Decode each iteration so the runtime path actually
				// works against a fresh instance — defends against any
				// future caching of validated input.
				var inst any
				if err := json.Unmarshal(c.raw, &inst); err != nil {
					mu.Lock()
					mismatches = append(mismatches, mismatch{
						worker: id, iter: i, caseName: c.name,
						expected: c.valid, got: false,
						errs: []ValidationError{{Message: fmt.Sprintf("unmarshal: %v", err)}},
					})
					mu.Unlock()
					return
				}
				res, err := schema.ValidateValue(inst)
				if err != nil {
					mu.Lock()
					mismatches = append(mismatches, mismatch{
						worker: id, iter: i, caseName: c.name,
						expected: c.valid, got: false,
						errs: []ValidationError{{Message: fmt.Sprintf("validate: %v", err)}},
					})
					mu.Unlock()
					return
				}
				if res.Valid != c.valid {
					mu.Lock()
					mismatches = append(mismatches, mismatch{
						worker: id, iter: i, caseName: c.name,
						expected: c.valid, got: res.Valid,
						errs: res.Errors,
					})
					mu.Unlock()
				}
			}
		}(w)
	}
	wg.Wait()

	if len(mismatches) > 0 {
		// Cap the diagnostic dump so the test output stays readable when
		// the bug is rampant.
		const dumpCap = 10
		t.Errorf("%d concurrent verdict mismatches across %d workers x %d iterations",
			len(mismatches), workers, iterations)
		for i, m := range mismatches {
			if i >= dumpCap {
				t.Logf("(... %d more mismatches suppressed ...)", len(mismatches)-dumpCap)
				break
			}
			t.Logf("worker=%d iter=%d case=%s expected=%v got=%v errs=%v",
				m.worker, m.iter, m.caseName, m.expected, m.got, m.errs)
		}
	}
}
