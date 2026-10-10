package httploader_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-rotini/jsonschema"
	"github.com/go-rotini/jsonschema/httploader"
)

func TestCompileURLViaHTTP(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"type":"string"}`))
	}))
	t.Cleanup(srv.Close)
	c := jsonschema.NewCompiler(jsonschema.WithLoader(&httploader.Loader{AllowHTTP: true}))
	if _, err := c.CompileURL(srv.URL + "/schema.json"); err != nil {
		t.Fatalf("CompileURL: %v", err)
	}
}

// TestCompileExternalRef exercises an end-to-end remote $ref:
// the root schema $refs an external document served by an HTTP loader, and
// the validator correctly resolves the reference at compile time.
func TestCompileExternalRef(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{
			"$id":"http://example.invalid/types",
			"$defs":{"name":{"type":"string","minLength":1}}
		}`))
	}))
	t.Cleanup(srv.Close)
	src := []byte(`{"$ref":"` + srv.URL + `/types#/$defs/name"}`)
	s, err := jsonschema.Compile(src, jsonschema.WithLoader(&httploader.Loader{AllowHTTP: true}))
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
