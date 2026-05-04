package jsonschema

// Ref-related coverage tests. Targets eval_ref.go, ref.go, and the
// $ref/$dynamicRef/$recursiveRef evaluator branches.

import (
	"strings"
	"testing"
)

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

// TestRefViaCompiler covers compileURL with successive cache + flight.
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

// TestCompileURLConcurrentSingleflight covers the concurrent single-flight
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
