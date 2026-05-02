package jsonschema

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
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
}

func TestCompileTypeMustBeStringOrArray(t *testing.T) {
	_, err := Compile([]byte(`{"type":123}`))
	if err == nil {
		t.Fatal("expected error for numeric type")
	}
}

func TestCompileRequiredMustBeStrings(t *testing.T) {
	_, err := Compile([]byte(`{"required":[1,2,3]}`))
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestCompileEmptyAllOf(t *testing.T) {
	_, err := Compile([]byte(`{"allOf":[]}`))
	if err == nil {
		t.Fatal("expected error for empty allOf")
	}
}

func TestCompileTrailingContent(t *testing.T) {
	_, err := Compile([]byte(`{} {}`))
	if err == nil {
		t.Fatal("expected error for trailing content")
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

func TestValidateTopLevelStubReturnsValidatorNotImplemented(t *testing.T) {
	_, err := Validate([]byte(`{}`), []byte(`null`))
	if !errors.Is(err, ErrValidatorNotImplemented) {
		t.Errorf("Validate err = %v, want ErrValidatorNotImplemented", err)
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
