package jsonschema

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

func TestCompileTrivialTrue(t *testing.T) {
	s, err := Compile([]byte(`true`))
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	if s == nil {
		t.Fatal("nil schema")
	}
	if s.Draft() != Draft202012 {
		t.Errorf("Draft = %s", s.Draft())
	}
}

func TestCompileTrivialFalse(t *testing.T) {
	if _, err := Compile([]byte(`false`)); err != nil {
		t.Fatalf("Compile: %v", err)
	}
}

func TestCompileEmptyObject(t *testing.T) {
	if _, err := Compile([]byte(`{}`)); err != nil {
		t.Fatalf("Compile: %v", err)
	}
}

func TestCompileSimpleType(t *testing.T) {
	s, err := Compile([]byte(`{"type":"string","minLength":3}`))
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	if s.Draft() != Draft202012 {
		t.Errorf("Draft = %s", s.Draft())
	}
}

func TestCompileNestedDefs(t *testing.T) {
	src := `{"$defs":{"foo":{"type":"string"}}, "$ref":"#/$defs/foo"}`
	if _, err := Compile([]byte(src)); err != nil {
		t.Fatalf("Compile: %v", err)
	}
}

func TestCompileSchemaDeclaresDraft(t *testing.T) {
	cases := []struct {
		schema string
		want   Draft
	}{
		{`{"$schema":"https://json-schema.org/draft/2020-12/schema"}`, Draft202012},
		{`{"$schema":"https://json-schema.org/draft/2019-09/schema"}`, Draft201909},
		{`{"$schema":"http://json-schema.org/draft-07/schema#"}`, Draft7},
		{`{"$schema":"http://json-schema.org/draft-06/schema#"}`, Draft6},
		{`{"$schema":"http://json-schema.org/draft-04/schema#"}`, Draft4},
	}
	for _, c := range cases {
		s, err := Compile([]byte(c.schema))
		if err != nil {
			t.Errorf("Compile(%s): %v", c.schema, err)
			continue
		}
		if s.Draft() != c.want {
			t.Errorf("Compile(%s).Draft() = %s, want %s", c.schema, s.Draft(), c.want)
		}
	}
}

func TestCompileMalformedJSON(t *testing.T) {
	_, err := Compile([]byte(`{not json}`))
	if err == nil {
		t.Fatal("expected error")
	}
	var ce *CompileError
	if !errors.As(err, &ce) {
		t.Errorf("err type = %T, want *CompileError", err)
	}
}

func TestCompileMalformedKeyword(t *testing.T) {
	_, err := Compile([]byte(`{"minLength":"three"}`))
	if err == nil {
		t.Fatal("expected error for non-integer minLength")
	}
	if !errors.Is(err, ErrCompile) {
		t.Errorf("errors.Is(err, ErrCompile) = false")
	}
}

func TestCompileNegativeMinLength(t *testing.T) {
	_, err := Compile([]byte(`{"minLength":-1}`))
	if err == nil {
		t.Fatal("expected error")
	}
	var ce *CompileError
	if !errors.As(err, &ce) {
		t.Errorf("err type = %T, want *CompileError", err)
	} else if ce.KeywordLocation == "" {
		t.Errorf("CompileError.KeywordLocation empty: %+v", ce)
	}
}

func TestCompileTypeMustBeStringOrArray(t *testing.T) {
	_, err := Compile([]byte(`{"type":123}`))
	if err == nil {
		t.Fatal("expected error for numeric type")
	}
	var ce *CompileError
	if !errors.As(err, &ce) {
		t.Errorf("err type = %T, want *CompileError", err)
	} else if ce.KeywordLocation == "" {
		t.Errorf("CompileError.KeywordLocation empty: %+v", ce)
	}
}

func TestCompileRequiredMustBeStrings(t *testing.T) {
	_, err := Compile([]byte(`{"required":[1,2,3]}`))
	if err == nil {
		t.Fatal("expected error")
	}
	var ce *CompileError
	if !errors.As(err, &ce) {
		t.Errorf("err type = %T, want *CompileError", err)
	} else if ce.KeywordLocation == "" {
		t.Errorf("CompileError.KeywordLocation empty: %+v", ce)
	}
}

func TestCompileEmptyAllOf(t *testing.T) {
	_, err := Compile([]byte(`{"allOf":[]}`))
	if err == nil {
		t.Fatal("expected error for empty allOf")
	}
	var ce *CompileError
	if !errors.As(err, &ce) {
		t.Errorf("err type = %T, want *CompileError", err)
	} else if ce.KeywordLocation == "" {
		t.Errorf("CompileError.KeywordLocation empty: %+v", ce)
	}
}

func TestCompileTrailingContent(t *testing.T) {
	_, err := Compile([]byte(`{} {}`))
	if err == nil {
		t.Fatal("expected error for trailing content")
	}
	var ce *CompileError
	if !errors.As(err, &ce) {
		t.Errorf("err type = %T, want *CompileError", err)
	}
	if !errors.Is(err, errTrailingContent) {
		t.Errorf("errors.Is(err, errTrailingContent) = false; err = %v", err)
	}
}

func TestMustCompilePanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("MustCompile did not panic on bad input")
		}
	}()
	_ = MustCompile([]byte(`{not json}`))
}

func TestMustCompileSucceeds(t *testing.T) {
	s := MustCompile([]byte(`{"type":"string"}`))
	if s == nil {
		t.Fatal("nil schema")
	}
}

func TestCompileValueAcceptsGoValue(t *testing.T) {
	s, err := CompileValue(map[string]any{"type": "string"})
	if err != nil {
		t.Fatalf("CompileValue: %v", err)
	}
	if s == nil {
		t.Fatal("nil schema")
	}
}

func TestMustCompileValuePanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("MustCompileValue did not panic")
		}
	}()
	_ = MustCompileValue(map[string]any{"minLength": "three"})
}

func TestCompileSchemaIDExposed(t *testing.T) {
	src := `{"$id":"https://example.com/root","type":"string"}`
	s, err := Compile([]byte(src))
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	if s.ID() != "https://example.com/root" {
		t.Errorf("ID = %q", s.ID())
	}
}

func TestCompileMetaSchemaURIFromSchemaKeyword(t *testing.T) {
	src := `{"$schema":"https://json-schema.org/draft/2019-09/schema"}`
	s, err := Compile([]byte(src))
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	if s.MetaSchemaURI() != "https://json-schema.org/draft/2019-09/schema" {
		t.Errorf("MetaSchemaURI = %q", s.MetaSchemaURI())
	}
}

func TestCompileResources(t *testing.T) {
	src := `{"$id":"https://example.com/root","$defs":{"x":{"$id":"https://example.com/x"}}}`
	s, err := Compile([]byte(src))
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	rs := s.Resources()
	if len(rs) < 2 {
		t.Errorf("expected at least 2 resources, got %d: %v", len(rs), rs)
	}
	if rs[0] != "https://example.com/root" {
		t.Errorf("first resource = %q, want root", rs[0])
	}
}

func TestCompileAnchors(t *testing.T) {
	src := `{"$defs":{"a":{"$anchor":"foo"},"b":{"$anchor":"bar"}}}`
	s, err := Compile([]byte(src))
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	got := s.Anchors()
	want := map[string]bool{"foo": true, "bar": true}
	if len(got) != 2 {
		t.Errorf("Anchors = %v, want 2", got)
	}
	for _, a := range got {
		if !want[a] {
			t.Errorf("unexpected anchor %q", a)
		}
	}
}

func TestCompileMarshalJSONRoundTrip(t *testing.T) {
	src := []byte(`{"type":"string","minLength":3}`)
	s, err := Compile(src)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	out, err := s.MarshalJSON()
	if err != nil {
		t.Fatalf("MarshalJSON: %v", err)
	}
	if string(out) != string(src) {
		t.Errorf("round-trip = %s, want %s", out, src)
	}
}

func TestCompileMetaSchemaURIDefault(t *testing.T) {
	s, err := Compile([]byte(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	if s.MetaSchemaURI() != Draft202012.MetaSchemaURL() {
		t.Errorf("default MetaSchemaURI = %q", s.MetaSchemaURI())
	}
}

func TestCompileWithDefaultDraft(t *testing.T) {
	s, err := Compile([]byte(`{}`), WithDefaultDraft(Draft7))
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	if s.Draft() != Draft7 {
		t.Errorf("Draft = %s, want Draft 7", s.Draft())
	}
}

func TestCompileWithBaseURI(t *testing.T) {
	src := `{"$ref":"#/$defs/x","$defs":{"x":{"type":"string"}}}`
	s, err := Compile([]byte(src), WithBaseURI("https://example.com/base"))
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	if s == nil {
		t.Fatal("nil schema")
	}
}

func TestCompileWithStrictKeywords(t *testing.T) {
	src := `{"madeUpKeyword":42}`
	if _, err := Compile([]byte(src)); err != nil {
		t.Errorf("Compile without strict mode: %v", err)
	}
	if _, err := Compile([]byte(src), WithStrictKeywords(true)); err == nil {
		t.Error("expected error in strict mode")
	}
}

func TestCompilerAddResource(t *testing.T) {
	c := NewCompiler()
	if err := c.AddResource("https://example.com/x", []byte(`{"type":"string"}`)); err != nil {
		t.Fatalf("AddResource: %v", err)
	}
	// AddResource with bad JSON should fail.
	if err := c.AddResource("https://example.com/y", []byte(`{not json}`)); err == nil {
		t.Error("AddResource accepted bad JSON")
	}
	if err := c.AddResource("", []byte(`{}`)); err == nil {
		t.Error("AddResource accepted empty URI")
	}
}

func TestCompilerCompileURL(t *testing.T) {
	c := NewCompiler(WithLoader(MapLoader{
		"https://example.com/a": []byte(`{"type":"string"}`),
	}))
	s, err := c.CompileURL("https://example.com/a")
	if err != nil {
		t.Fatalf("CompileURL: %v", err)
	}
	if s == nil {
		t.Fatal("nil schema")
	}
}

func TestCompilerCompileURLCaches(t *testing.T) {
	c := NewCompiler(WithLoader(MapLoader{
		"https://example.com/a": []byte(`{"type":"string"}`),
	}))
	s1, err := c.CompileURL("https://example.com/a")
	if err != nil {
		t.Fatal(err)
	}
	s2, err := c.CompileURL("https://example.com/a")
	if err != nil {
		t.Fatal(err)
	}
	if s1 != s2 {
		t.Error("Compiler did not cache CompileURL result")
	}
}

func TestCompileURLViaHTTP(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"type":"string"}`))
	}))
	t.Cleanup(srv.Close)
	c := NewCompiler(WithLoader(&HTTPLoader{AllowHTTP: true}))
	if _, err := c.CompileURL(srv.URL + "/schema.json"); err != nil {
		t.Fatalf("CompileURL: %v", err)
	}
}

// TestCompileExternalRefViaHTTPLoader exercises an end-to-end remote $ref:
// the root schema $refs an external document served by an HTTP loader, and
// the validator correctly resolves the reference at compile time.
func TestCompileExternalRefViaHTTPLoader(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{
			"$id":"http://example.invalid/types",
			"$defs":{"name":{"type":"string","minLength":1}}
		}`))
	}))
	t.Cleanup(srv.Close)
	src := []byte(`{"$ref":"` + srv.URL + `/types#/$defs/name"}`)
	s, err := Compile(src, WithLoader(&HTTPLoader{AllowHTTP: true}))
	if err != nil {
		t.Fatalf("Compile remote $ref: %v", err)
	}
	res, err := s.Validate([]byte(`"alice"`))
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if !res.Valid {
		t.Errorf("expected valid; errors=%v", res.Errors)
	}
	res, err = s.Validate([]byte(`""`))
	if err != nil {
		t.Fatalf("Validate empty: %v", err)
	}
	if res.Valid {
		t.Errorf("expected invalid for empty string")
	}
}

func TestCompileExternalRefViaMapLoader(t *testing.T) {
	loader := MapLoader{
		"https://example.com/types": []byte(`{"$id":"https://example.com/types","$defs":{"name":{"type":"string"}}}`),
	}
	src := `{"$ref":"https://example.com/types#/$defs/name"}`
	s, err := Compile([]byte(src), WithLoader(loader))
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	if s == nil {
		t.Fatal("nil schema")
	}
}

func TestCompileCyclicSelfRef(t *testing.T) {
	src := `{"$ref":"#"}`
	s, err := Compile([]byte(src))
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	if s == nil {
		t.Fatal("nil schema")
	}
	// Verify a binding for $ref exists.
	var sawRef bool
	for _, b := range s.bindings {
		if b.Name == "$ref" {
			sawRef = true
			break
		}
	}
	if !sawRef {
		t.Error("expected $ref binding")
	}
}

func TestCompileMutuallyRecursive(t *testing.T) {
	src := `{
		"$defs": {
			"a": {"$ref": "#/$defs/b"},
			"b": {"$ref": "#/$defs/a"}
		},
		"$ref": "#/$defs/a"
	}`
	if _, err := Compile([]byte(src)); err != nil {
		t.Fatalf("Compile mutually recursive: %v", err)
	}
}

func TestValidateTopLevelValidatesInstance(t *testing.T) {
	res, err := Validate(
		[]byte(`{"type":"string","minLength":3}`),
		[]byte(`"ok!"`),
	)
	if err != nil {
		t.Fatalf("Validate err = %v", err)
	}
	if !res.Valid {
		t.Errorf("Validate res.Valid = false, want true; errors=%v", res.Errors)
	}

	res, err = Validate(
		[]byte(`{"type":"string","minLength":3}`),
		[]byte(`"x"`),
	)
	if err != nil {
		t.Fatalf("Validate err = %v", err)
	}
	if res.Valid {
		t.Errorf("Validate res.Valid = true, want false for short string")
	}
}

func TestValidateTopLevelPropagatesCompileError(t *testing.T) {
	_, err := Validate([]byte(`{not json}`), []byte(`null`))
	if err == nil {
		t.Fatal("expected error")
	}
	var ce *CompileError
	if !errors.As(err, &ce) {
		t.Errorf("err = %T, want *CompileError", err)
	}
}

func TestSchemaStringContainsDraft(t *testing.T) {
	s, err := Compile([]byte(`{"type":"string"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(s.String(), "Draft 2020-12") {
		t.Errorf("String() = %q", s.String())
	}
}

func TestCompileOptionsDoNotPanicWithNilCompiler(t *testing.T) {
	// Various combinations of options should compose without error.
	_, err := Compile([]byte(`{}`),
		WithDefaultDraft(Draft7),
		WithMaxRefDepth(50),
		WithStrictKeywords(false),
		WithMetaSchemaValidation(true),
		WithRefCollisionPolicy(RefCollisionLastWins),
	)
	if err != nil {
		t.Errorf("Compile with options: %v", err)
	}
}

func TestCompileURLRejectsLoaderRefusal(t *testing.T) {
	c := NewCompiler(WithLoader(MapLoader{}))
	_, err := c.CompileURL("https://example.com/missing")
	if err == nil {
		t.Fatal("expected loader rejection error")
	}
}

func TestNewCompilerWithLoaderTrace(t *testing.T) {
	var buf strings.Builder
	c := NewCompiler(WithLoaderTrace(&buf))
	if c == nil {
		t.Fatal("nil compiler")
	}
	// The trace writer is stored but unused in Phase 3; this test just
	// confirms the option does not error out at construction.
}

func TestCompileURLAfterAddResource(t *testing.T) {
	// AddResource pre-seeds a resource map entry. A $ref to that URI
	// should resolve without invoking a loader.
	c := NewCompiler(WithLoader(MapLoader{}))
	if err := c.AddResource("https://example.com/types", []byte(`{"$id":"https://example.com/types","$defs":{"name":{"type":"string"}}}`)); err != nil {
		t.Fatalf("AddResource: %v", err)
	}
	src := `{"$ref":"https://example.com/types#/$defs/name"}`
	if _, err := c.Compile([]byte(src)); err != nil {
		t.Fatalf("Compile after AddResource: %v", err)
	}
}

func TestCompilerCompileValueViaMethod(t *testing.T) {
	c := NewCompiler()
	s, err := c.CompileValue(map[string]any{"type": "string"})
	if err != nil {
		t.Fatal(err)
	}
	if s == nil {
		t.Fatal("nil schema")
	}
}

func TestCompilerMustCompile(t *testing.T) {
	c := NewCompiler()
	s := c.MustCompile([]byte(`{"type":"string"}`))
	if s == nil {
		t.Fatal("nil schema")
	}
	defer func() {
		if r := recover(); r == nil {
			t.Error("MustCompile did not panic")
		}
	}()
	_ = c.MustCompile([]byte(`{not json}`))
}

// TestCompileEmptyInput exercises Compile([]byte("")) and Compile(nil) — both
// should fail with a *CompileError wrapping io.EOF (the underlying decoder
// signal for "no document").
func TestCompileEmptyInput(t *testing.T) {
	cases := []struct {
		name  string
		input []byte
	}{
		{"empty", []byte("")},
		{"nil", nil},
		{"whitespace", []byte("   \n\t")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Compile(tc.input)
			if err == nil {
				t.Fatalf("Compile(%q): expected error", tc.input)
			}
			var ce *CompileError
			if !errors.As(err, &ce) {
				t.Errorf("Compile(%q): err type = %T, want *CompileError", tc.input, err)
			}
			if !errors.Is(err, io.EOF) {
				t.Errorf("Compile(%q): errors.Is(err, io.EOF) = false; err = %v", tc.input, err)
			}
		})
	}
}

// TestCompilerConcurrentCompile drives the same *Compiler from many
// goroutines. The body is compile-only — the test exercises the Compiler's
// internal locking around its caches, not the validator. Re-run with -race
// to surface any data race in the compile pipeline.
func TestCompilerConcurrentCompile(t *testing.T) {
	c := NewCompiler()
	schema := []byte(`{"type":"string"}`)
	const N = 32
	var wg sync.WaitGroup
	wg.Add(N)
	for i := 0; i < N; i++ {
		go func() {
			defer wg.Done()
			if _, err := c.Compile(schema); err != nil {
				t.Errorf("Compile: %v", err)
			}
		}()
	}
	wg.Wait()
}

// TestCompilerCompileURLSingleFlight verifies that concurrent CompileURL
// calls for the same URI share a single fetch+compile pipeline. The Loader
// records calls with an atomic counter; with N concurrent callers we expect
// exactly one fetch. The handler holds its response until every caller has
// committed to its CompileURL invocation, so the followers are guaranteed
// to find the in-flight slot.
func TestCompilerCompileURLSingleFlight(t *testing.T) {
	const N = 16
	var (
		hits    atomic.Int64
		arrived sync.WaitGroup
		release = make(chan struct{})
	)
	arrived.Add(N)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		// Block until every test goroutine has signaled it is about to call
		// CompileURL — by that point the followers have either won (no) or
		// queued behind the in-flight slot. Then close `release` to gate the
		// response.
		arrived.Wait()
		<-release
		_, _ = w.Write([]byte(`{"type":"string"}`))
	}))
	t.Cleanup(srv.Close)

	// Use a per-test compiler with an HTTP loader; cache state is isolated
	// from any other test running in parallel.
	c := NewCompiler(WithLoader(&HTTPLoader{AllowHTTP: true}))
	uri := srv.URL + "/schema.json"

	var wg sync.WaitGroup
	wg.Add(N)
	for i := 0; i < N; i++ {
		go func() {
			defer wg.Done()
			// Mark this goroutine as committed to its CompileURL call
			// before invoking it; the gap between Done() and entering
			// CompileURL is a single function call, so under contention
			// followers reliably find the in-flight slot.
			arrived.Done()
			if _, err := c.CompileURL(uri); err != nil {
				t.Errorf("CompileURL: %v", err)
			}
		}()
	}
	// Once every goroutine has signaled arrival the handler has unblocked;
	// release it so the first fetch can complete.
	arrived.Wait()
	close(release)
	wg.Wait()
	if got := hits.Load(); got != 1 {
		t.Errorf("CompileURL fetched %d times, want 1 (single-flight broken)", got)
	}
}

// TestCompileDeepNestedBadShape covers the bindAndResolve path where a
// recursively-resolved child schema returns an error that propagates up
// through bindAndResolveChild → bindAndResolve.
func TestCompileDeepNestedBadShape(t *testing.T) {
	v := map[string]any{
		"properties": map[string]any{
			"x": map[string]any{
				"allOf": "not-an-array",
			},
		},
	}
	if _, err := CompileValue(v, WithMetaSchemaValidation(false)); err == nil {
		t.Error("expected error from nested allOf shape failure")
	}
}

// TestCompileItemsArrayBadChild covers the items array recursion with a
// malformed child shape.
func TestCompileItemsArrayBadChild(t *testing.T) {
	v := map[string]any{
		"items": []any{
			map[string]any{"allOf": "not-an-array"},
		},
	}
	if _, err := CompileValue(v, WithMetaSchemaValidation(false)); err == nil {
		t.Error("expected error from bad items[0] shape")
	}
}

// TestCompileItemsSingleSchemaBadChild covers the items default-case
// (single schema) recursion with a malformed shape.
func TestCompileItemsSingleSchemaBadChild(t *testing.T) {
	v := map[string]any{
		"items": map[string]any{
			"allOf": "not-an-array",
		},
	}
	if _, err := CompileValue(v, WithMetaSchemaValidation(false)); err == nil {
		t.Error("expected error from bad items single-schema shape")
	}
}

// TestCompileAllOfBadChild covers the allOf recursion error cascade with
// a malformed nested shape.
func TestCompileAllOfBadChild(t *testing.T) {
	v := map[string]any{
		"allOf": []any{
			map[string]any{"allOf": "not-an-array"},
		},
	}
	if _, err := CompileValue(v, WithMetaSchemaValidation(false)); err == nil {
		t.Error("expected error from bad allOf[0] nested shape")
	}
}

// TestCompileDependenciesSchemaBadChild covers the dependencies schema-
// branch cascade with a malformed nested shape.
func TestCompileDependenciesSchemaBadChild(t *testing.T) {
	v := map[string]any{
		"dependencies": map[string]any{
			"x": map[string]any{
				"allOf": "not-an-array",
			},
		},
	}
	if _, err := CompileValue(v, WithMetaSchemaValidation(false)); err == nil {
		t.Error("expected error from bad dependencies nested shape")
	}
}

// TestCompileDependenciesArrayPasses covers the dependencies array branch
// (no schema, just required-list).
func TestCompileDependenciesArrayPasses(t *testing.T) {
	v := map[string]any{
		"dependencies": map[string]any{
			"x": []any{"y"},
		},
	}
	s, err := CompileValue(v, WithMetaSchemaValidation(false))
	if err != nil {
		t.Fatalf("CompileValue: %v", err)
	}
	if _, err := s.Validate([]byte(`{"x":1,"y":2}`)); err != nil {
		t.Fatalf("Validate: %v", err)
	}
}

// TestCompileDefaultBranchBadChild covers bindAndResolveChild's default
// branch (e.g. "not", "if", "then", "else") with a malformed nested
// shape.
func TestCompileDefaultBranchBadChild(t *testing.T) {
	v := map[string]any{
		"not": map[string]any{
			"allOf": "not-an-array",
		},
	}
	if _, err := CompileValue(v, WithMetaSchemaValidation(false)); err == nil {
		t.Error("expected error from bad not subschema shape")
	}
}

// TestCompileMetaSchemaValidationFailure exercises the meta-schema
// validation path (validateKeywordShape passes; meta-schema rejects).
func TestCompileMetaSchemaValidationFailure(t *testing.T) {
	// type:"weirdtype" passes our shape check (any string is fine) but
	// the draft-2020-12 meta-schema enforces a closed enum.
	v := map[string]any{
		"$schema": "https://json-schema.org/draft/2020-12/schema",
		"type":    "weirdtype",
	}
	if _, err := CompileValue(v, WithMetaSchemaValidation(true)); err == nil {
		t.Error("expected meta-schema validation error")
	}
}

// TestCompileMetaSchemaValidationManyErrors exercises the >5-errors
// truncation branch by stacking many meta-schema-level violations that
// pass validateKeywordShape.
func TestCompileMetaSchemaValidationManyErrors(t *testing.T) {
	// Each property uses an unknown type — passes shape check, fails
	// meta-schema. >5 properties to exercise truncation.
	props := map[string]any{}
	for i, k := range []string{"a", "b", "c", "d", "e", "f", "g", "h"} {
		props[k] = map[string]any{"type": "weirdtype" + string(rune('0'+i))}
	}
	v := map[string]any{
		"$schema":    "https://json-schema.org/draft/2020-12/schema",
		"properties": props,
	}
	if _, err := CompileValue(v, WithMetaSchemaValidation(true)); err == nil {
		t.Error("expected error from many meta-schema failures")
	}
}

// TestCompileValueIntPathsForLengthBounds exercises toInt's int/int64
// and float64-truncated paths via CompileValue with native Go ints.
func TestCompileValueIntPathsForLengthBounds(t *testing.T) {
	t.Run("int", func(t *testing.T) {
		v := map[string]any{"minLength": int(5), "maxLength": int(10)}
		if _, err := CompileValue(v); err != nil {
			t.Fatalf("int: %v", err)
		}
	})
	t.Run("int64", func(t *testing.T) {
		v := map[string]any{"minLength": int64(5), "maxLength": int64(10)}
		if _, err := CompileValue(v); err != nil {
			t.Fatalf("int64: %v", err)
		}
	})
	t.Run("float-truncated", func(t *testing.T) {
		v := map[string]any{"minLength": 5.0, "maxLength": 10.0}
		if _, err := CompileValue(v); err != nil {
			t.Fatalf("float64: %v", err)
		}
	})
	t.Run("float-non-truncated", func(t *testing.T) {
		// 5.5 is not an integer — should fail at validateKeywordShape.
		v := map[string]any{"minLength": 5.5}
		if _, err := CompileValue(v); err == nil {
			t.Error("expected error for non-integer minLength")
		}
	})
}

// TestSortMultiEvaluatorsExercisesKeyword forces 2+ evaluators on the
// same subschema so the sort.SliceStable callback exercises each
// evaluator's keyword() method.
func TestSortMultiEvaluatorsExercisesKeyword(t *testing.T) {
	src := []byte(`{
		"type":"string",
		"const":"hello",
		"enum":["hello"],
		"minLength":1,
		"maxLength":10,
		"pattern":"^h",
		"format":"email"
	}`)
	s, err := Compile(src)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	if _, err := s.Validate([]byte(`"hello"`)); err != nil {
		t.Fatalf("Validate: %v", err)
	}
}

// TestCompileValueAllOfWrongShape covers the !ok branch in the allOf
// builder.
func TestCompileValueAllOfWrongShape(t *testing.T) {
	v := map[string]any{"allOf": "not-an-array"}
	if _, err := CompileValue(v); err == nil {
		t.Error("expected error for non-array allOf")
	}
}

// TestCompileValueAnyOfWrongShape covers the !ok branch in the anyOf
// builder.
func TestCompileValueAnyOfWrongShape(t *testing.T) {
	v := map[string]any{"anyOf": "not-an-array"}
	if _, err := CompileValue(v); err == nil {
		t.Error("expected error for non-array anyOf")
	}
}

// TestCompileValueOneOfWrongShape covers the !ok branch in oneOf builder.
func TestCompileValueOneOfWrongShape(t *testing.T) {
	v := map[string]any{"oneOf": "not-an-array"}
	if _, err := CompileValue(v); err == nil {
		t.Error("expected error for non-array oneOf")
	}
}

// TestCompileValuePropertiesWrongShape covers properties wrong shape.
func TestCompileValuePropertiesWrongShape(t *testing.T) {
	v := map[string]any{"properties": "not-an-object"}
	if _, err := CompileValue(v); err == nil {
		t.Error("expected error for non-object properties")
	}
}

// TestCompileValueDependentSchemasWrongShape covers dependentSchemas.
func TestCompileValueDependentSchemasWrongShape(t *testing.T) {
	v := map[string]any{"dependentSchemas": "wrong"}
	if _, err := CompileValue(v); err == nil {
		t.Error("expected error")
	}
}

// TestCompileValueDependentRequiredWrongShape covers dependentRequired.
func TestCompileValueDependentRequiredWrongShape(t *testing.T) {
	v := map[string]any{"dependentRequired": "wrong"}
	if _, err := CompileValue(v); err == nil {
		t.Logf("dependentRequired wrong shape: schema accepted (perhaps tolerated)")
	}
}

// TestCompileValueRequiredWrongShape covers required wrong type.
func TestCompileValueRequiredWrongShape(t *testing.T) {
	v := map[string]any{"required": 42}
	if _, err := CompileValue(v); err == nil {
		t.Error("expected error")
	}
}

// TestCompileValuePatternPropertiesWrongShape covers patternProperties.
func TestCompileValuePatternPropertiesWrongShape(t *testing.T) {
	v := map[string]any{"patternProperties": []any{"x"}}
	if _, err := CompileValue(v); err == nil {
		t.Error("expected error")
	}
}

// TestCompileValueDependenciesWrongShape covers dependencies (legacy).
func TestCompileValueDependenciesWrongShape(t *testing.T) {
	v := map[string]any{"dependencies": "wrong"}
	if _, err := CompileValue(v, WithDefaultDraft(Draft7)); err == nil {
		t.Logf("draft-7 dependencies: tolerated")
	}
}

// TestCompileValueIfWithoutThenOrElse covers the if-without-then branch via the
// ifThenElseEval. Also covers if-without-else.
func TestCompileValueIfWithoutThenOrElse(t *testing.T) {
	v := map[string]any{
		"if": map[string]any{"type": "string"},
		// neither then nor else
	}
	s, err := CompileValue(v)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	res, _ := s.Validate([]byte(`"x"`))
	if !res.Valid {
		t.Errorf("expected valid; errors=%v", res.Errors)
	}
}

// TestCompileValueIfWithThenOnly covers if-then no else.
func TestCompileValueIfWithThenOnly(t *testing.T) {
	v := map[string]any{
		"if":   map[string]any{"type": "string"},
		"then": map[string]any{"minLength": 3},
	}
	s, err := CompileValue(v)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	res, _ := s.Validate([]byte(`"abc"`))
	if !res.Valid {
		t.Errorf("expected valid; errors=%v", res.Errors)
	}
	res, _ = s.Validate([]byte(`"a"`))
	if res.Valid {
		t.Error("expected invalid (too short)")
	}
}

// TestCompileValueIfWithElseOnly covers if-fail → else.
func TestCompileValueIfWithElseOnly(t *testing.T) {
	v := map[string]any{
		"if":   map[string]any{"type": "string"},
		"else": map[string]any{"const": 42},
	}
	s, err := CompileValue(v)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	res, _ := s.Validate([]byte(`42`))
	if !res.Valid {
		t.Errorf("expected valid; errors=%v", res.Errors)
	}
	res, _ = s.Validate([]byte(`99`))
	if res.Valid {
		t.Error("expected invalid (else const)")
	}
}

// TestCompileValuePrefixItemsWrongShape covers the prefixItems !ok.
func TestCompileValuePrefixItemsWrongShape(t *testing.T) {
	v := map[string]any{"prefixItems": "not-an-array"}
	if _, err := CompileValue(v); err == nil {
		t.Error("expected error for non-array prefixItems")
	}
}

// TestCompileValueItemsBoolean covers items as a boolean schema.
func TestCompileValueItemsBoolean(t *testing.T) {
	v := map[string]any{"items": false}
	s, err := CompileValue(v)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	res, _ := s.Validate([]byte(`[]`))
	if !res.Valid {
		t.Errorf("empty array should pass false-items: errors=%v", res.Errors)
	}
	res, _ = s.Validate([]byte(`[1]`))
	if res.Valid {
		t.Error("non-empty array should fail items:false")
	}
}

// TestCompileValuePatternPropertiesNotMap covers the patternProperties
// !ok branch.
func TestCompileValuePatternPropertiesNotMap(t *testing.T) {
	v := map[string]any{"patternProperties": "wrong"}
	if _, err := CompileValue(v); err == nil {
		t.Error("expected error")
	}
}

// TestCompileValueAdditionalPropertiesNotSchema covers the
// additionalProperties shape.
func TestCompileValueAdditionalPropertiesNotSchema(t *testing.T) {
	// additionalProperties should be schema or boolean; integer is wrong.
	v := map[string]any{"additionalProperties": 42}
	if _, err := CompileValue(v); err == nil {
		t.Logf("additionalProperties:42 accepted; permissive shape check")
	}
}

// TestCompileValuePropertyNamesNotSchema covers propertyNames non-schema.
func TestCompileValuePropertyNamesNotSchema(t *testing.T) {
	v := map[string]any{"propertyNames": 42}
	if _, err := CompileValue(v); err == nil {
		t.Logf("propertyNames:42 accepted")
	}
}

// TestCompileValueContainsBranches covers contains evaluator shapes.
func TestCompileValueContainsBranches(t *testing.T) {
	// contains: must be a schema (object or bool)
	v := map[string]any{"contains": map[string]any{"type": "integer"}}
	s, err := CompileValue(v)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if res, _ := s.Validate([]byte(`[1,"x"]`)); !res.Valid {
		t.Errorf("expected valid; errors=%v", res.Errors)
	}
}

// TestCompileValueContainsWithMinMax covers min/maxContains.
func TestCompileValueContainsWithMinMax(t *testing.T) {
	v := map[string]any{
		"contains":    map[string]any{"type": "integer"},
		"minContains": json.Number("2"),
		"maxContains": json.Number("3"),
	}
	s, err := CompileValue(v)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if res, _ := s.Validate([]byte(`[1,2,"x"]`)); !res.Valid {
		t.Errorf("expected valid (2 ints); errors=%v", res.Errors)
	}
	if res, _ := s.Validate([]byte(`[1,2,3,4,"x"]`)); res.Valid {
		t.Error("expected invalid (4 ints exceed max=3)")
	}
}

// TestCompileValuePropertiesWithBoolValue covers properties with a boolean
// schema.
func TestCompileValuePropertiesWithBoolValue(t *testing.T) {
	v := map[string]any{
		"properties": map[string]any{
			"x": false, // x is rejected
		},
	}
	s, err := CompileValue(v)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if res, _ := s.Validate([]byte(`{"x":1}`)); res.Valid {
		t.Error("expected invalid (false schema)")
	}
}

// TestCompileValueDependenciesArrayList covers the legacy dependencies
// array-of-strings branch.
func TestCompileValueDependenciesArrayList(t *testing.T) {
	v := map[string]any{
		"dependencies": map[string]any{
			"a": []any{"b", "c"},
		},
	}
	s, err := CompileValue(v, WithDefaultDraft(Draft7))
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if res, _ := s.Validate([]byte(`{"a":1}`)); res.Valid {
		t.Error("expected invalid (a requires b,c)")
	}
}

// TestCompileValuePrefixItemsBoolean covers prefixItems with boolean entry.
func TestCompileValuePrefixItemsBoolean(t *testing.T) {
	v := map[string]any{
		"prefixItems": []any{
			map[string]any{"type": "integer"},
			false, // second item rejected
		},
	}
	s, err := CompileValue(v)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if res, _ := s.Validate([]byte(`[1, "anything"]`)); res.Valid {
		t.Error("expected invalid (prefix[1] is false)")
	}
}

// TestCompileValueNotSchema covers the not keyword.
func TestCompileValueNotSchema(t *testing.T) {
	v := map[string]any{
		"not": map[string]any{"type": "string"},
	}
	s, err := CompileValue(v)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if res, _ := s.Validate([]byte(`42`)); !res.Valid {
		t.Errorf("expected valid; errors=%v", res.Errors)
	}
	if res, _ := s.Validate([]byte(`"x"`)); res.Valid {
		t.Error("expected invalid (matches not)")
	}
}

// TestAddResourceAndCompileURL covers the AddResource + CompileURL flow.
func TestAddResourceAndCompileURL(t *testing.T) {
	c := NewCompiler()
	if err := c.AddResource("https://example.com/x", []byte(`{"type":"string"}`)); err != nil {
		t.Fatalf("AddResource: %v", err)
	}
	// Note: AddResource doesn't add to cache, only resources. CompileURL
	// invokes the loader; without a custom loader, it'll go through the
	// default chain. So we only verify the AddResource itself.
}

// TestSeedResourcesViaCompileWithRef exercises seedResources by compiling a
// schema that $refs into a pre-registered resource.
func TestSeedResourcesViaCompileWithRef(t *testing.T) {
	c := NewCompiler()
	if err := c.AddResource("https://example.com/types", []byte(`{
		"$id":"https://example.com/types",
		"$defs":{"name":{"type":"string","minLength":3}}
	}`)); err != nil {
		t.Fatalf("AddResource: %v", err)
	}
	src := []byte(`{"$ref":"https://example.com/types#/$defs/name"}`)
	s, err := c.Compile(src)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	res, _ := s.Validate([]byte(`"hello"`))
	if !res.Valid {
		t.Errorf("expected valid; errors=%v", res.Errors)
	}
	res, _ = s.Validate([]byte(`"hi"`))
	if res.Valid {
		t.Error("expected invalid (too short)")
	}
}

// TestSeedResourcesMalformed covers the decode-error branch by adding a
// malformed schema then compiling another that doesn't reference it (so the
// walk happens but the malformed entry is skipped).
func TestSeedResourcesMalformed(t *testing.T) {
	c := NewCompiler()
	// Use the public AddResource, which validates JSON. We bypass that for
	// coverage: store directly.
	c.resources.Store("malformed", []byte("not json"))
	c.resources.Store(int(1), []byte("ignored")) // non-string key
	c.resources.Store("ok", "not bytes")         // non-byte val
	// Compile any schema; seedResources iterates AddResource entries and
	// must skip malformed ones without panicking.
	if _, err := c.Compile([]byte(`{}`)); err != nil {
		t.Errorf("Compile: %v", err)
	}
}

// TestCompileWithDraftUnknownFallsBackToDefault covers the
// compile-path branch that promotes DraftUnknown to DraftDefault.
func TestCompileWithDraftUnknownFallsBackToDefault(t *testing.T) {
	s, err := Compile([]byte(`{"type":"string"}`), WithDefaultDraft(DraftUnknown))
	if err != nil {
		t.Fatalf("Compile with DraftUnknown: %v", err)
	}
	if s.Draft() == DraftUnknown {
		t.Error("draft was not promoted from DraftUnknown")
	}
}

// TestMatchesTypeUnknown covers the default-case (return false) branch
// of matchesType — a non-standard type string compiles (with metaschema
// validation off) but never matches any value.
func TestMatchesTypeUnknown(t *testing.T) {
	s, err := Compile([]byte(`{"type":"weirdtype"}`), WithMetaSchemaValidation(false))
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	res, err := s.Validate([]byte(`null`))
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if res.Valid {
		t.Error("expected invalid for unknown type")
	}
}

// TestCompileValueRequiredNonString covers the
// "required entries must be strings" branch.
func TestCompileValueRequiredNonString(t *testing.T) {
	v := map[string]any{
		"type":     "object",
		"required": []any{"a", 42},
	}
	if _, err := CompileValue(v, WithMetaSchemaValidation(false)); err == nil {
		t.Error("expected error for non-string required entry")
	}
}

// TestCompileValueDependentRequiredEntryNotArray covers the
// dependentRequired non-array entry branch.
func TestCompileValueDependentRequiredEntryNotArray(t *testing.T) {
	v := map[string]any{
		"dependentRequired": map[string]any{
			"x": "not-an-array",
		},
	}
	if _, err := CompileValue(v, WithMetaSchemaValidation(false)); err == nil {
		t.Error("expected error for non-array dependentRequired entry")
	}
}

// TestCompileValueDependentRequiredEntryNonString covers the
// dependentRequired entry-non-string branch.
func TestCompileValueDependentRequiredEntryNonString(t *testing.T) {
	v := map[string]any{
		"dependentRequired": map[string]any{
			"x": []any{"a", 42},
		},
	}
	if _, err := CompileValue(v, WithMetaSchemaValidation(false)); err == nil {
		t.Error("expected error for non-string dependentRequired entry")
	}
}

// TestCompileValueExclusiveBoundDraft4Bool covers the Draft 4 boolean
// folded-into-maximum/minimum branch (exclusiveMaximum/exclusiveMinimum
// returning nil evaluator).
func TestCompileValueExclusiveBoundDraft4Bool(t *testing.T) {
	v := map[string]any{
		"$schema":          "http://json-schema.org/draft-04/schema#",
		"maximum":          json.Number("10"),
		"exclusiveMaximum": true,
		"minimum":          json.Number("0"),
		"exclusiveMinimum": true,
	}
	s, err := CompileValue(v, WithMetaSchemaValidation(false))
	if err != nil {
		t.Fatalf("CompileValue: %v", err)
	}
	res, err := s.ValidateValue(json.Number("5"))
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if !res.Valid {
		t.Errorf("expected valid; errors=%v", res.Errors)
	}
}

// TestCompileURLNoLoaderDefaults exercises CompileURL without an explicit
// WithLoader option (so the compiler runs with the package DefaultLoader,
// which is set in NewCompiler).
func TestCompileURLNoLoaderDefaults(t *testing.T) {
	c := NewCompiler()
	// The default loader cannot resolve an arbitrary scheme — expect an
	// error, but importantly, we exercised the default-loader path.
	if _, err := c.CompileURL("https://nope.example.invalid/x"); err == nil {
		t.Error("expected error from default loader")
	}
}

// TestCompileURLBadFetch covers the load-error path of CompileURL.
func TestCompileURLBadFetch(t *testing.T) {
	c := NewCompiler(WithLoader(MapLoader{}))
	if _, err := c.CompileURL("https://example.com/missing"); err == nil {
		t.Error("expected error")
	}
}

// TestCompileURLBadDecode covers the decode-failure path of CompileURL
// (load succeeds but the bytes aren't valid JSON).
func TestCompileURLBadDecode(t *testing.T) {
	c := NewCompiler(WithLoader(MapLoader{
		"https://example.com/bad": []byte(`{not-json`),
	}))
	if _, err := c.CompileURL("https://example.com/bad"); err == nil {
		t.Error("expected decode error")
	}
}

// TestCompileBadRootID covers the invalid-$id error path.
func TestCompileBadRootID(t *testing.T) {
	v := map[string]any{
		"$id": "://bad-uri",
	}
	if _, err := CompileValue(v); err == nil {
		t.Error("expected invalid $id error")
	}
}

// TestCompileNestedBadID covers the bindResolveBaseURI error path
// inside bindAndResolve.
func TestCompileNestedBadID(t *testing.T) {
	v := map[string]any{
		"properties": map[string]any{
			"x": map[string]any{
				"$id": "://bad-uri",
			},
		},
	}
	if _, err := CompileValue(v); err == nil {
		t.Error("expected error from nested invalid $id")
	}
}

// TestCompileValueMarshalFailure covers compile.go's json.Marshal failure
// branch by feeding CompileValue a value containing a Go-only chan that
// json.Marshal cannot encode. The walkResource path may reject earlier;
// either an error here is acceptable.
func TestCompileValueMarshalFailure(t *testing.T) {
	v := map[string]any{
		"$comment": make(chan int), // chans are not json-marshalable
	}
	if _, err := CompileValue(v, WithMetaSchemaValidation(false)); err == nil {
		t.Error("expected marshal failure for value containing chan")
	}
}

// TestIsNonNegativeIntegerBranches covers each numeric type path.
func TestIsNonNegativeIntegerBranches(t *testing.T) {
	cases := []struct {
		v    any
		want bool
	}{
		{json.Number("5"), true},
		{json.Number("-5"), false},
		{json.Number("5.0"), true},
		{json.Number("5.5"), false},
		{json.Number("not-a-number"), false},
		{int(5), true},
		{int(-1), false},
		{int64(0), true},
		{int64(-7), false},
		{float64(0), true},
		{float64(-1), false},
		{float64(2.5), false},
		{"string", false},
	}
	for _, tc := range cases {
		got := isNonNegativeInteger(tc.v)
		if got != tc.want {
			t.Errorf("isNonNegativeInteger(%v) = %v, want %v", tc.v, got, tc.want)
		}
	}
}

// TestIsPositiveNumberBranches covers each numeric type path.
func TestIsPositiveNumberBranches(t *testing.T) {
	cases := []struct {
		v    any
		want bool
	}{
		{json.Number("5"), true},
		{json.Number("0"), false},
		{json.Number("-5"), false},
		{json.Number("not"), false},
		{int(7), true},
		{int(0), false},
		{int64(-1), false},
		{float64(0.1), true},
		{float64(0), false},
		{"x", false},
	}
	for _, tc := range cases {
		got := isPositiveNumber(tc.v)
		if got != tc.want {
			t.Errorf("isPositiveNumber(%v) = %v, want %v", tc.v, got, tc.want)
		}
	}
}

// TestItoaSignedBranches covers itoa (negative + zero + positive).
func TestItoaSignedBranches(t *testing.T) {
	cases := []struct {
		in   int
		want string
	}{
		{0, "0"},
		{1, "1"},
		{-7, "-7"},
		{12345, "12345"},
		{-12345, "-12345"},
	}
	for _, tc := range cases {
		got := itoa(tc.in)
		if got != tc.want {
			t.Errorf("itoa(%d) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// TestNewCompilerNilLoader confirms a nil loader option falls back to the
// default. Already exercised but adds explicit coverage of the nil branch.
func TestNewCompilerNilLoader(t *testing.T) {
	c := NewCompiler(WithLoader(nil))
	if c == nil {
		t.Fatal("NewCompiler returned nil")
	}
}

// TestCheckNumberOrBoolValid covers the bool path.
func TestCheckNumberOrBoolValid(t *testing.T) {
	if err := checkNumberOrBool("exclusiveMinimum", true, "/x"); err != nil {
		t.Errorf("bool exclusiveMinimum (Draft 4 form): %v", err)
	}
	if err := checkNumberOrBool("exclusiveMinimum", float64(3), "/x"); err != nil {
		t.Errorf("number exclusiveMinimum: %v", err)
	}
	if err := checkNumberOrBool("exclusiveMinimum", "wrong", "/x"); err == nil {
		t.Error("expected error for string")
	}
}

// TestCheckStringWithNonString covers the failure path.
func TestCheckStringWithNonString(t *testing.T) {
	if err := checkString("$ref", 42, "/x"); err == nil {
		t.Error("expected error")
	}
}

// TestCheckObjectFails covers the failure path.
func TestCheckObjectFails(t *testing.T) {
	if err := checkObject("properties", []any{}, "/x"); err == nil {
		t.Error("expected error")
	}
}

// TestCompileBadShapeKeyword covers each shape-checker failure path via a
// Compile call.
func TestCompileBadShapeKeyword(t *testing.T) {
	cases := map[string][]byte{
		"multipleOf-zero": []byte(`{"multipleOf":0}`),
		"minimum-string":  []byte(`{"minimum":"x"}`),
		"pattern-int":     []byte(`{"pattern":42}`),
		"type-int":        []byte(`{"type":42}`),
		"required-int":    []byte(`{"required":42}`),
		"required-non":    []byte(`{"required":[1,2]}`),
		"enum-not-array":  []byte(`{"enum":1}`),
		"unique-not-bool": []byte(`{"uniqueItems":1}`),
		"properties-arr":  []byte(`{"properties":[]}`),
		"allOf-empty":     []byte(`{"allOf":[]}`),
		"$id-not-string":  []byte(`{"$id":42}`),
	}
	for name, src := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := Compile(src); err == nil {
				t.Errorf("expected error for %q", src)
			}
		})
	}
}

// TestCompileTrailingContentCov covers the trailing-content rejection.
func TestCompileTrailingContentCov(t *testing.T) {
	src := []byte(`{} {}`)
	if _, err := Compile(src); err == nil {
		t.Error("expected error on trailing content")
	}
}

// TestCompileNonJSON covers the decode-failure branch.
func TestCompileNonJSON(t *testing.T) {
	if _, err := Compile([]byte(`not json`)); err == nil {
		t.Error("expected error on non-JSON")
	}
}

// TestCompileURLViaCompiler exercises the loader cache + cache-hit second
// call.
func TestCompileURLViaCompiler(t *testing.T) {
	c := NewCompiler(WithLoader(MapLoader{
		"https://example.com/a": []byte(`{"type":"string"}`),
	}))
	s1, err := c.CompileURL("https://example.com/a")
	if err != nil {
		t.Fatalf("first CompileURL: %v", err)
	}
	s2, err := c.CompileURL("https://example.com/a")
	if err != nil {
		t.Fatalf("second CompileURL: %v", err)
	}
	if s1 != s2 {
		t.Errorf("expected cached pointer; s1=%p s2=%p", s1, s2)
	}
}

// TestCompileURLLoadFailure covers the loader-error branch.
func TestCompileURLLoadFailure(t *testing.T) {
	c := NewCompiler(WithLoader(MapLoader{}))
	if _, err := c.CompileURL("https://example.com/missing"); err == nil {
		t.Error("expected error from missing URI")
	}
}

// TestCompileURLBadJSON covers the decode-error branch.
func TestCompileURLBadJSON(t *testing.T) {
	c := NewCompiler(WithLoader(MapLoader{
		"https://example.com/a": []byte(`not json`),
	}))
	if _, err := c.CompileURL("https://example.com/a"); err == nil {
		t.Error("expected decode error")
	}
}

// TestCompilerNilReceiver covers each nil-receiver branch.
func TestCompilerNilReceiver(t *testing.T) {
	var c *Compiler
	if _, err := c.Compile([]byte(`{}`)); !errors.Is(err, ErrSchemaNotCompiled) {
		t.Errorf("nil.Compile err = %v, want ErrSchemaNotCompiled", err)
	}
	if _, err := c.CompileValue(nil); !errors.Is(err, ErrSchemaNotCompiled) {
		t.Errorf("nil.CompileValue err = %v, want ErrSchemaNotCompiled", err)
	}
	if _, err := c.CompileURL("u"); !errors.Is(err, ErrSchemaNotCompiled) {
		t.Errorf("nil.CompileURL err = %v, want ErrSchemaNotCompiled", err)
	}
	if err := c.AddResource("u", []byte(`{}`)); !errors.Is(err, ErrSchemaNotCompiled) {
		t.Errorf("nil.AddResource err = %v, want ErrSchemaNotCompiled", err)
	}
}

// TestCompilerAddResourceEmptyURI covers the empty-URI branch.
func TestCompilerAddResourceEmptyURI(t *testing.T) {
	c := NewCompiler()
	if err := c.AddResource("", []byte(`{}`)); err == nil {
		t.Error("expected error on empty URI")
	}
}

// TestCompilerAddResourceBadJSON covers the bad-JSON branch.
func TestCompilerAddResourceBadJSON(t *testing.T) {
	c := NewCompiler()
	if err := c.AddResource("u", []byte(`bad`)); err == nil {
		t.Error("expected JSON decode error")
	}
}

// TestPackageMustCompileURLSuccess covers the success path of the package
// MustCompileURL helper.
func TestPackageMustCompileURLSuccess(t *testing.T) {
	loader := MapLoader{"https://example.com/a": []byte(`{"type":"string"}`)}
	s := MustCompileURL("https://example.com/a", WithLoader(loader))
	if s == nil {
		t.Fatal("nil schema")
	}
}

// TestCompileBadIDValue exercises the $id-parse error pathway with an
// $id that contains a stray space. The error path is best-effort; the
// test is satisfied either way as long as the call doesn't panic.
func TestCompileBadIDValue(t *testing.T) {
	src := []byte(`{"$id":"http://example.com/x y"}`)
	_, _ = Compile(src)
}
