package jsonschema

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestSchemaAccessorsOnNil(t *testing.T) {
	var s *Schema
	if s.Draft() != DraftUnknown {
		t.Errorf("nil Schema.Draft() = %s, want DraftUnknown", s.Draft())
	}
	if s.ID() != "" {
		t.Errorf("nil Schema.ID() = %q, want empty", s.ID())
	}
	if s.MetaSchemaURI() != "" {
		t.Errorf("nil Schema.MetaSchemaURI() = %q, want empty", s.MetaSchemaURI())
	}
	if s.String() != "Schema(<nil>)" {
		t.Errorf("nil Schema.String() = %q", s.String())
	}
	data, err := s.MarshalJSON()
	if err != nil {
		t.Fatalf("nil Schema.MarshalJSON: %v", err)
	}
	if string(data) != "null" {
		t.Errorf("nil Schema.MarshalJSON = %s, want null", data)
	}
}

func TestSchemaAccessors(t *testing.T) {
	src := []byte(`{"$id":"https://example.com/x","type":"object"}`)
	s := newSchemaForTest(src, Draft202012, "https://example.com/x", "https://json-schema.org/draft/2020-12/schema")
	if s.Draft() != Draft202012 {
		t.Errorf("Draft = %s, want %s", s.Draft(), Draft202012)
	}
	if s.ID() != "https://example.com/x" {
		t.Errorf("ID = %q", s.ID())
	}
	if s.MetaSchemaURI() != "https://json-schema.org/draft/2020-12/schema" {
		t.Errorf("MetaSchemaURI = %q", s.MetaSchemaURI())
	}
}

func TestSchemaMetaSchemaURIFallsBackToDraft(t *testing.T) {
	s := newSchemaForTest([]byte(`{}`), Draft7, "", "")
	if got := s.MetaSchemaURI(); got != Draft7.MetaSchemaURL() {
		t.Errorf("MetaSchemaURI() fallback = %q, want %q", got, Draft7.MetaSchemaURL())
	}
}

func TestSchemaMarshalJSONReturnsSource(t *testing.T) {
	src := []byte(`{"type":"string","minLength":3}`)
	s := newSchemaForTest(src, Draft202012, "", "")
	data, err := s.MarshalJSON()
	if err != nil {
		t.Fatalf("MarshalJSON: %v", err)
	}
	if !bytes.Equal(data, src) {
		t.Errorf("MarshalJSON = %s, want %s", data, src)
	}
	// MarshalJSON output must round-trip through json.Unmarshal.
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Errorf("Unmarshal MarshalJSON output: %v", err)
	}
	// Mutating the returned slice must not affect the schema's internal
	// source bytes (defensive copy).
	data[0] = 'X'
	again, _ := s.MarshalJSON()
	if again[0] == 'X' {
		t.Errorf("MarshalJSON did not return a defensive copy")
	}
}

func TestSchemaString(t *testing.T) {
	s := newSchemaForTest([]byte(`{"type":"object"}`), Draft202012, "https://example.com/x", "")
	str := s.String()
	for _, want := range []string{"Schema(", "Draft 2020-12", "id=https://example.com/x", "bytes="} {
		if !strings.Contains(str, want) {
			t.Errorf("String() = %q, missing %q", str, want)
		}
	}
}

func TestSchemaStringMinimal(t *testing.T) {
	s := newSchemaForTest(nil, Draft7, "", "")
	if got := s.String(); got != "Schema(Draft 7)" {
		t.Errorf("String() = %q, want %q", got, "Schema(Draft 7)")
	}
}

// TestSchemaVocabularies covers the new public Vocabularies() accessor:
// nil schema returns nil; a draft 2020-12 schema returns the standard set;
// declaring the OAS dialect adds VocabOAS.
func TestSchemaVocabularies(t *testing.T) {
	var nilS *Schema
	if got := nilS.Vocabularies(); got != nil {
		t.Errorf("nil Schema.Vocabularies() = %v, want nil", got)
	}
	s := MustCompile([]byte(`{"$schema":"https://json-schema.org/draft/2020-12/schema","type":"object"}`))
	got := s.Vocabularies()
	want := []string{VocabCore, VocabApplicator, VocabValidation, VocabFormatAnnot}
	for _, w := range want {
		if !containsString(got, w) {
			t.Errorf("Vocabularies missing %q; got=%v", w, got)
		}
	}
	// VocabOAS only appears for OAS-dialect schemas.
	if containsString(got, VocabOAS) {
		t.Errorf("Vocabularies should not include VocabOAS for plain 2020-12; got=%v", got)
	}

	oasS := MustCompile([]byte(`{"$schema":"https://spec.openapis.org/oas/3.1/dialect/base","type":"object"}`))
	if !containsString(oasS.Vocabularies(), VocabOAS) {
		t.Errorf("OAS-dialect Vocabularies missing VocabOAS; got=%v", oasS.Vocabularies())
	}
}

// TestSchemaBindings confirms the new public Bindings() accessor exposes
// keyword bindings extracted at compile time, with copies that callers may
// mutate freely.
func TestSchemaBindings(t *testing.T) {
	var nilS *Schema
	if got := nilS.Bindings(); got != nil {
		t.Errorf("nil Schema.Bindings() = %v, want nil", got)
	}
	s := MustCompile([]byte(`{"type":"object","properties":{"name":{"type":"string"}}}`))
	bindings := s.Bindings()
	if len(bindings) == 0 {
		t.Fatal("Bindings() empty; expected at least 'type' and 'properties'")
	}
	// Second call returns a fresh slice — mutating it shouldn't affect the
	// schema's internal state.
	first := s.Bindings()
	first[0].Name = "MUTATED"
	second := s.Bindings()
	if second[0].Name == "MUTATED" {
		t.Error("Bindings() returned shared storage; expected fresh slice")
	}
}

func containsString(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}

func TestSchemaValidateNilSchema(t *testing.T) {
	var s *Schema
	if _, err := s.Validate([]byte(`null`)); !errors.Is(err, ErrSchemaNotCompiled) {
		t.Errorf("Validate(nil schema) err = %v, want ErrSchemaNotCompiled", err)
	}
	if _, err := s.ValidateValue(nil); !errors.Is(err, ErrSchemaNotCompiled) {
		t.Errorf("ValidateValue(nil schema) err = %v, want ErrSchemaNotCompiled", err)
	}
	if _, err := s.ValidateReader(strings.NewReader(`{}`)); !errors.Is(err, ErrSchemaNotCompiled) {
		t.Errorf("ValidateReader(nil schema) err = %v, want ErrSchemaNotCompiled", err)
	}
	var v any
	if err := s.ValidateAndUnmarshal([]byte(`{}`), &v); !errors.Is(err, ErrSchemaNotCompiled) {
		t.Errorf("ValidateAndUnmarshal(nil schema) err = %v, want ErrSchemaNotCompiled", err)
	}
}
