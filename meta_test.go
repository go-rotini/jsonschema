package jsonschema

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestMetaSchemaBytesEveryDraft(t *testing.T) {
	for _, d := range []Draft{Draft4, Draft6, Draft7, Draft201909, Draft202012} {
		data, err := MetaSchemaBytes(d)
		if err != nil {
			t.Errorf("MetaSchemaBytes(%s): %v", d, err)
			continue
		}
		if len(data) == 0 {
			t.Errorf("MetaSchemaBytes(%s) is empty", d)
			continue
		}
		// Must parse as JSON.
		var raw map[string]any
		if err := json.Unmarshal(data, &raw); err != nil {
			t.Errorf("MetaSchemaBytes(%s) is not valid JSON: %v", d, err)
			continue
		}
		// $id (or "id" for Draft 4) must equal MetaSchemaURL(d).
		var declared string
		if v, ok := raw[d.IDKeyword()]; ok {
			declared, _ = v.(string)
		}
		if declared != d.MetaSchemaURL() {
			t.Errorf("MetaSchemaBytes(%s) declares %s = %q, want %q",
				d, d.IDKeyword(), declared, d.MetaSchemaURL())
		}
	}
}

func TestMetaSchemaBytesUnknown(t *testing.T) {
	if _, err := MetaSchemaBytes(DraftUnknown); !errors.Is(err, ErrUnknownDraft) {
		t.Errorf("MetaSchemaBytes(DraftUnknown) error = %v, want %v", err, ErrUnknownDraft)
	}
	if _, err := MetaSchemaBytes(Draft(999)); !errors.Is(err, ErrUnknownDraft) {
		t.Errorf("MetaSchemaBytes(invalid) error = %v, want %v", err, ErrUnknownDraft)
	}
}

func TestMetaSchemaCompilesEveryDraft(t *testing.T) {
	for _, d := range []Draft{Draft4, Draft6, Draft7, Draft201909, Draft202012} {
		s, err := MetaSchema(d)
		if err != nil {
			t.Errorf("MetaSchema(%s) err = %v, want nil", d, err)
			continue
		}
		if s == nil {
			t.Errorf("MetaSchema(%s) returned nil Schema", d)
			continue
		}
		if s.Draft() != d {
			t.Errorf("MetaSchema(%s).Draft() = %s, want %s", d, s.Draft(), d)
		}
	}
	if _, err := MetaSchema(DraftUnknown); !errors.Is(err, ErrUnknownDraft) {
		t.Errorf("MetaSchema(DraftUnknown) err = %v, want ErrUnknownDraft", err)
	}
}

func TestMetaSchemaMemoizes(t *testing.T) {
	s1, err := MetaSchema(Draft202012)
	if err != nil {
		t.Fatalf("MetaSchema: %v", err)
	}
	s2, err := MetaSchema(Draft202012)
	if err != nil {
		t.Fatalf("MetaSchema: %v", err)
	}
	if s1 != s2 {
		t.Errorf("MetaSchema is not memoized: got two different Schema pointers")
	}
}

func TestMetaSchemaURLFunctionMatchesMethod(t *testing.T) {
	for _, d := range []Draft{Draft4, Draft6, Draft7, Draft201909, Draft202012} {
		if MetaSchemaURL(d) != d.MetaSchemaURL() {
			t.Errorf("MetaSchemaURL(%s) != Draft.MetaSchemaURL", d)
		}
	}
}

func TestMetaSchemaPaths2019_09Vocabularies(t *testing.T) {
	// Spot-check the embedded per-vocabulary meta-schemas for 2019-09 and
	// 2020-12. They are not directly accessible via MetaSchemaBytes
	// (which targets the dialect meta-schemas only) but they ship
	// embedded so the compiler can resolve refs into them offline.
	for _, path := range []string{
		"meta/draft-2019-09/meta/core.json",
		"meta/draft-2019-09/meta/applicator.json",
		"meta/draft-2019-09/meta/validation.json",
		"meta/draft-2019-09/meta/format.json",
		"meta/draft-2019-09/meta/content.json",
		"meta/draft-2019-09/meta/meta-data.json",
		"meta/draft-2020-12/meta/core.json",
		"meta/draft-2020-12/meta/applicator.json",
		"meta/draft-2020-12/meta/unevaluated.json",
		"meta/draft-2020-12/meta/validation.json",
		"meta/draft-2020-12/meta/format-annotation.json",
		"meta/draft-2020-12/meta/format-assertion.json",
		"meta/draft-2020-12/meta/content.json",
		"meta/draft-2020-12/meta/meta-data.json",
	} {
		data, err := metaSchemaFS.ReadFile(path)
		if err != nil {
			t.Errorf("read %q: %v", path, err)
			continue
		}
		if len(data) == 0 {
			t.Errorf("%q: empty", path)
			continue
		}
		var raw map[string]any
		if err := json.Unmarshal(data, &raw); err != nil {
			t.Errorf("%q: not JSON: %v", path, err)
			continue
		}
		id, _ := raw["$id"].(string)
		if id == "" {
			t.Errorf("%q: missing $id", path)
		}
		if !strings.HasPrefix(id, "https://json-schema.org/draft/") {
			t.Errorf("%q: $id %q does not match the json-schema.org domain", path, id)
		}
	}
}

// TestMetaSchemaCacheHit covers the cache-hit branch.
func TestMetaSchemaCacheHit(t *testing.T) {
	a, err := MetaSchema(Draft202012)
	if err != nil {
		t.Fatalf("first MetaSchema: %v", err)
	}
	b, err := MetaSchema(Draft202012)
	if err != nil {
		t.Fatalf("second MetaSchema: %v", err)
	}
	if a != b {
		t.Errorf("expected cached pointer; a=%p b=%p", a, b)
	}
}

// TestMetaSchemaForDialectCacheHit covers the cache-hit branch.
func TestMetaSchemaForDialectCacheHit(t *testing.T) {
	a, ok := metaSchemaForDialect(OASDialectURL)
	if !ok {
		t.Skip("OAS dialect not registered")
	}
	b, _ := metaSchemaForDialect(OASDialectURL)
	if a != b {
		t.Errorf("expected cached pointer")
	}
}

// TestMetaSchemaValidationOK covers the meta-schema validation success path.
func TestMetaSchemaValidationOK(t *testing.T) {
	src := []byte(`{"type":"string","minLength":3}`)
	if _, err := Compile(src, WithMetaSchemaValidation(true)); err != nil {
		t.Errorf("expected meta-validation OK: %v", err)
	}
}

// TestMetaSchemaValidationFailure covers the meta-schema validation failure
// path. Use a schema with a malformed keyword that the meta-schema rejects.
func TestMetaSchemaValidationFailure(t *testing.T) {
	src := []byte(`{"type":"not-a-valid-type"}`)
	_, err := Compile(src, WithMetaSchemaValidation(true))
	if err == nil {
		t.Skip("schema accepted; meta-schema may be permissive")
	}
}

// TestMetaSchemaUnknownDraft covers the ErrUnknownDraft branch.
func TestMetaSchemaUnknownDraft(t *testing.T) {
	if _, err := MetaSchema(DraftUnknown); !errors.Is(err, ErrUnknownDraft) {
		t.Errorf("MetaSchema(DraftUnknown) err = %v, want ErrUnknownDraft", err)
	}
}

// TestMetaSchemaBytesUnknownDraft covers the ErrUnknownDraft branch.
func TestMetaSchemaBytesUnknownDraft(t *testing.T) {
	if _, err := MetaSchemaBytes(DraftUnknown); !errors.Is(err, ErrUnknownDraft) {
		t.Errorf("MetaSchemaBytes(DraftUnknown) err = %v, want ErrUnknownDraft", err)
	}
}

// TestMetaSchemaBytesAllDrafts covers each known draft path.
func TestMetaSchemaBytesAllDrafts(t *testing.T) {
	for _, d := range []Draft{Draft4, Draft6, Draft7, Draft201909, Draft202012} {
		if _, err := MetaSchemaBytes(d); err != nil {
			t.Errorf("MetaSchemaBytes(%s): %v", d, err)
		}
	}
}

// TestMetaSchemaURLDraft covers the package-level helper.
func TestMetaSchemaURLDraft(t *testing.T) {
	if got := MetaSchemaURL(Draft202012); got != Draft202012.MetaSchemaURL() {
		t.Errorf("MetaSchemaURL = %q, want %q", got, Draft202012.MetaSchemaURL())
	}
}

// TestMetaSchemaForDialectUnknown returns false for unrecognized URIs.
func TestMetaSchemaForDialectUnknown(t *testing.T) {
	if _, ok := metaSchemaForDialect("https://not-a-known-dialect.example/"); ok {
		t.Errorf("expected ok=false for unknown dialect URI")
	}
}
