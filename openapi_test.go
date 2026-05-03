package jsonschema

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestOASDialectConstants pins the public surface for the OpenAPI 3.1
// dialect so callers can rely on the canonical URLs without re-deriving
// them.
func TestOASDialectConstants(t *testing.T) {
	if VocabOAS != "https://spec.openapis.org/oas/3.1/vocab/base" {
		t.Errorf("VocabOAS = %q", VocabOAS)
	}
	if OASDialectURL != "https://spec.openapis.org/oas/3.1/dialect/base" {
		t.Errorf("OASDialectURL = %q", OASDialectURL)
	}
	if OASBaseSchemaURL != "https://spec.openapis.org/oas/3.1/schema/2022-10-07" {
		t.Errorf("OASBaseSchemaURL = %q", OASBaseSchemaURL)
	}
}

// TestOASDialectDraftMapping confirms that schemas declaring the OAS
// dialect URL as $schema resolve to Draft 2020-12 (the dialect is a
// strict superset of 2020-12).
func TestOASDialectDraftMapping(t *testing.T) {
	if got := DraftFromMetaSchemaURL(OASDialectURL); got != Draft202012 {
		t.Errorf("DraftFromMetaSchemaURL(OASDialectURL) = %s, want Draft202012", got)
	}
	// Trailing fragments and HTTP variants must round-trip too.
	for _, variant := range []string{
		OASDialectURL,
		OASDialectURL + "#",
		"http://spec.openapis.org/oas/3.1/dialect/base",
	} {
		if got := DraftFromMetaSchemaURL(variant); got != Draft202012 {
			t.Errorf("DraftFromMetaSchemaURL(%q) = %s, want Draft202012", variant, got)
		}
	}
}

// TestOASVocabularyKeywords verifies that the OAS vocabulary contributes
// exactly the four annotation-only keywords required by the dialect.
func TestOASVocabularyKeywords(t *testing.T) {
	want := map[string]bool{
		"discriminator": true,
		"xml":           true,
		"externalDocs":  true,
		"example":       true,
	}
	var found []string
	for _, v := range Vocabularies() {
		if v.URI != VocabOAS {
			continue
		}
		for _, k := range v.Keywords {
			found = append(found, k.Name())
			if !want[k.Name()] {
				t.Errorf("unexpected OAS keyword %q", k.Name())
			}
		}
	}
	if len(found) != len(want) {
		t.Errorf("OAS vocabulary keywords = %v, want %d entries", found, len(want))
	}
	// LookupKeyword finds them in the active 2020-12 set.
	for name := range want {
		if _, ok := LookupKeyword(name, Draft202012); !ok {
			t.Errorf("LookupKeyword(%q, Draft202012) not found", name)
		}
	}
}

// TestOASDialectCompiles is the end-to-end smoke for the dialect. A schema
// declaring `$schema: "https://spec.openapis.org/oas/3.1/dialect/base"`
// with all four OAS keywords must compile cleanly, and the keywords must
// surface as annotations rather than errors at validation time.
func TestOASDialectCompiles(t *testing.T) {
	schemaJSON := []byte(`{
		"$schema": "https://spec.openapis.org/oas/3.1/dialect/base",
		"$id": "https://example.com/oas-dialect-smoke",
		"type": "object",
		"discriminator": {
			"propertyName": "kind",
			"mapping": {
				"cat": "#/$defs/Cat",
				"dog": "#/$defs/Dog"
			}
		},
		"externalDocs": {
			"url": "https://example.com/docs",
			"description": "Reference docs"
		},
		"xml": {
			"name": "Pet",
			"namespace": "https://example.com/xml"
		},
		"example": {"kind": "cat", "name": "Whiskers"},
		"properties": {
			"kind": {"type": "string"},
			"name": {"type": "string"}
		},
		"required": ["kind"]
	}`)

	schema, err := Compile(schemaJSON)
	if err != nil {
		t.Fatalf("compile OAS dialect schema: %v", err)
	}
	if schema.Draft() != Draft202012 {
		t.Errorf("schema.Draft() = %s, want Draft202012", schema.Draft())
	}

	// A passing instance must validate cleanly.
	pass := []byte(`{"kind": "cat", "name": "Whiskers"}`)
	res, err := schema.Validate(pass)
	if err != nil {
		t.Fatalf("validate pass: %v", err)
	}
	if !res.Valid {
		for i, e := range res.Errors {
			t.Logf("  unexpected error[%d] at %s: %s", i, e.InstanceLocation, e.Message)
		}
		t.Fatalf("expected pass instance valid, got %d errors", len(res.Errors))
	}
	// The OAS keywords must surface as annotations (not errors).
	wantAnnots := map[string]bool{
		"discriminator": false,
		"xml":           false,
		"externalDocs":  false,
		"example":       false,
	}
	for _, a := range res.Annotations {
		if _, ok := wantAnnots[a.Keyword]; ok {
			wantAnnots[a.Keyword] = true
		}
	}
	for name, seen := range wantAnnots {
		if !seen {
			t.Errorf("expected annotation for %q not found", name)
		}
	}

	// A failing instance still rejects via the standard validation
	// keywords (the OAS vocabulary contributes only annotations).
	fail := []byte(`{"name": "Whiskers"}`) // missing "kind"
	failRes, err := schema.Validate(fail)
	if err != nil {
		t.Fatalf("validate fail: %v", err)
	}
	if failRes.Valid {
		t.Fatalf("expected fail instance invalid, got valid result")
	}
}

// TestOASDialectMetaSchemaValidation exercises the dialect path through
// [WithMetaSchemaValidation]: the schema must validate against the
// embedded OAS dialect meta-schema, not against vanilla Draft 2020-12.
func TestOASDialectMetaSchemaValidation(t *testing.T) {
	schemaJSON := []byte(`{
		"$schema": "https://spec.openapis.org/oas/3.1/dialect/base",
		"$id": "https://example.com/oas-meta-schema-smoke",
		"type": "object",
		"discriminator": {"propertyName": "kind"},
		"externalDocs": {"url": "https://example.com/docs"},
		"xml": {"name": "Pet"},
		"example": {"kind": "cat"}
	}`)

	if _, err := Compile(schemaJSON, WithMetaSchemaValidation(true)); err != nil {
		t.Fatalf("compile OAS dialect schema with meta validation: %v", err)
	}

	// Confirm the dialect meta-schema is reachable offline.
	if _, ok := metaSchemaForDialect(OASDialectURL); !ok {
		t.Errorf("metaSchemaForDialect(OASDialectURL) returned false")
	}

	// And that an unknown dialect URI returns the (nil, false) signal.
	if _, ok := metaSchemaForDialect("https://example.com/not/a/dialect"); ok {
		t.Errorf("metaSchemaForDialect(unknown) returned true")
	}
}

// TestOASDialectEmbeddedFile confirms the dialect meta-schema ships
// alongside the canonical draft meta-schemas inside [metaSchemaFS].
func TestOASDialectEmbeddedFile(t *testing.T) {
	data, err := metaSchemaFS.ReadFile("meta/openapi-3.1-dialect.json")
	if err != nil {
		t.Fatalf("read embedded dialect: %v", err)
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("dialect meta-schema is not JSON: %v", err)
	}
	if id, _ := raw["$id"].(string); id != OASDialectURL {
		t.Errorf("dialect $id = %q, want %q", id, OASDialectURL)
	}
	vocab, _ := raw["$vocabulary"].(map[string]any)
	if _, ok := vocab[VocabOAS]; !ok {
		t.Errorf("dialect $vocabulary missing %q", VocabOAS)
	}
}

// TestOASUpstreamFixtureLoadsViaDialect compiles the upstream OpenAPI 3.1
// fixture under explicit dialect mode (we rewrite the in-memory $schema
// pointer to the OAS dialect URL) and verifies it still validates a
// passing OpenAPI document.
func TestOASUpstreamFixtureLoadsViaDialect(t *testing.T) {
	schemaPath := filepath.Join(acceptanceFixtureDir, "openapi-3.1.json")
	schemaBytes, err := os.ReadFile(schemaPath)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	rewritten := strings.Replace(
		string(schemaBytes),
		`"$schema": "https://json-schema.org/draft/2020-12/schema"`,
		`"$schema": "https://spec.openapis.org/oas/3.1/dialect/base"`,
		1,
	)
	if rewritten == string(schemaBytes) {
		t.Fatal("fixture rewrite did not change any bytes")
	}
	schema, err := Compile([]byte(rewritten))
	if err != nil {
		t.Fatalf("compile fixture under OAS dialect: %v", err)
	}
	if schema.Draft() != Draft202012 {
		t.Errorf("schema.Draft() = %s, want Draft202012", schema.Draft())
	}

	instPath := filepath.Join(acceptanceFixtureDir, "instances", "openapi-3.1.pass.json")
	instBytes, err := os.ReadFile(instPath)
	if err != nil {
		t.Fatalf("read pass instance: %v", err)
	}
	res, err := schema.Validate(instBytes)
	if err != nil {
		t.Fatalf("validate pass instance: %v", err)
	}
	if !res.Valid {
		for i, e := range res.Errors {
			t.Logf("  err[%d] at %s: %s", i, e.InstanceLocation, e.Message)
			if i >= 4 {
				break
			}
		}
		t.Fatalf("expected pass instance valid, got %d errors", len(res.Errors))
	}
}

// TestOASStrictKeywordsAccepted ensures the OAS keywords are recognized
// (i.e. don't trip [WithStrictKeywords]) even when the schema does not
// declare the dialect URL — many real OpenAPI 3.1 documents declare
// only the standard 2020-12 $schema and rely on the keywords being
// tolerated.
func TestOASStrictKeywordsAccepted(t *testing.T) {
	schemaJSON := []byte(`{
		"$schema": "https://json-schema.org/draft/2020-12/schema",
		"type": "object",
		"discriminator": {"propertyName": "kind"},
		"externalDocs": {"url": "https://example.com"},
		"xml": {"name": "X"},
		"example": 42
	}`)
	if _, err := Compile(schemaJSON, WithStrictKeywords(true)); err != nil {
		// strictKeywords mode treats unknown keywords as errors; the
		// OAS quartet must NOT be flagged.
		var ce *CompileError
		if errors.As(err, &ce) && errors.Is(ce.Cause, ErrUnknownKeyword) {
			t.Fatalf("strict mode rejected an OAS keyword: %v", err)
		}
		t.Fatalf("compile: %v", err)
	}
}
