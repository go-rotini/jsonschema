package jsonschema

import (
	"embed"
	"fmt"
	"io/fs"
	"sync"
)

// metaSchemaFS embeds the canonical meta-schemas for every supported draft
// plus the Draft 2019-09 / 2020-12 per-vocabulary meta-schemas. The package
// reads them via [MetaSchemaBytes] so callers can validate input schemas
// fully offline (no Loader required for refs into the standard meta-schemas).
//
//go:embed meta
var metaSchemaFS embed.FS

// metaSchemaPaths lists every recognized draft with its embedded file path
// inside [metaSchemaFS]. New drafts must be added here AND in [Draft] /
// [Draft.MetaSchemaURL]. It is an array rather than a map so that it costs
// nothing at package init.
var metaSchemaPaths = [...]struct {
	draft Draft
	path  string
}{
	{Draft4, "meta/draft-04.json"},
	{Draft6, "meta/draft-06.json"},
	{Draft7, "meta/draft-07.json"},
	{Draft201909, "meta/draft-2019-09.json"},
	{Draft202012, "meta/draft-2020-12.json"},
}

// metaSchemaPath returns the embedded file path of d's meta-schema.
func metaSchemaPath(d Draft) (string, bool) {
	for _, e := range metaSchemaPaths {
		if e.draft == d {
			return e.path, true
		}
	}
	return "", false
}

// dialectMetaSchemaPath maps known dialect meta-schema URIs (those that
// are not one of the canonical draft URLs) to embedded files in
// [metaSchemaFS]. The package consults it before falling back to the
// draft-keyed meta-schema in [validateAgainstMetaSchema] so that schemas
// declaring `$schema: "https://spec.openapis.org/oas/3.1/dialect/base"`
// validate against the correct dialect rather than plain Draft 2020-12.
func dialectMetaSchemaPath(uri string) (string, bool) {
	if uri == OASDialectURL {
		return "meta/openapi-3.1-dialect.json", true
	}
	return "", false
}

// MetaSchemaBytes returns the JSON bytes of the embedded meta-schema for d,
// as a fresh copy on each call. It returns an error wrapping
// [ErrUnknownDraft] for [DraftUnknown] or any unrecognized value.
func MetaSchemaBytes(d Draft) ([]byte, error) {
	path, ok := metaSchemaPath(d)
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrUnknownDraft, d.String())
	}
	data, err := fs.ReadFile(metaSchemaFS, path)
	if err != nil {
		return nil, fmt.Errorf("jsonschema: read embedded meta-schema %q: %w", path, err)
	}
	return data, nil
}

// MetaSchemaURL returns the canonical URL of the meta-schema for d; it is
// the same as [Draft.MetaSchemaURL].
func MetaSchemaURL(d Draft) string {
	return d.MetaSchemaURL()
}

// metaSchemaCache memoizes [MetaSchema] results per draft so repeated calls
// don't re-parse the embedded bytes. The compile path uses an embedded-only
// MapLoader so it never touches the network.
var (
	metaSchemaCacheMu sync.Mutex
	metaSchemaCache   map[Draft]*Schema

	dialectMetaSchemaCacheMu sync.Mutex
	dialectMetaSchemaCache   map[string]*Schema
)

// MetaSchema returns the compiled [*Schema] for the meta-schema of d,
// compiled from the embedded copies without network access. The result is
// memoized; repeated calls return the same pointer. It returns an error
// wrapping [ErrUnknownDraft] for an unrecognized d.
func MetaSchema(d Draft) (*Schema, error) {
	if _, ok := metaSchemaPath(d); !ok {
		return nil, fmt.Errorf("%w: %s", ErrUnknownDraft, d.String())
	}
	metaSchemaCacheMu.Lock()
	defer metaSchemaCacheMu.Unlock()
	if s, ok := metaSchemaCache[d]; ok {
		return s, nil
	}
	data, err := MetaSchemaBytes(d)
	if err != nil {
		return nil, err
	}
	c := NewCompiler(WithLoader(embeddedMetaMapLoader()), WithDefaultDraft(d))
	s, err := c.Compile(data)
	if err != nil {
		return nil, err
	}
	if metaSchemaCache == nil {
		metaSchemaCache = make(map[Draft]*Schema)
	}
	metaSchemaCache[d] = s
	return s, nil
}

// metaSchemaForDialect returns the compiled meta-schema for the dialect URI
// uri, or (nil, false) when uri does not name an embedded dialect. The
// result is memoized; repeated calls for the same URI return the same
// pointer. Used by [validateAgainstMetaSchema] when a schema's `$schema`
// names a dialect rather than a canonical draft URL.
func metaSchemaForDialect(uri string) (*Schema, bool) {
	path, ok := dialectMetaSchemaPath(uri)
	if !ok {
		return nil, false
	}
	dialectMetaSchemaCacheMu.Lock()
	defer dialectMetaSchemaCacheMu.Unlock()
	if s, ok := dialectMetaSchemaCache[uri]; ok {
		return s, true
	}
	data, err := fs.ReadFile(metaSchemaFS, path)
	if err != nil {
		return nil, false
	}
	c := NewCompiler(WithLoader(embeddedMetaMapLoader()), WithDefaultDraft(Draft202012))
	s, err := c.Compile(data)
	if err != nil {
		return nil, false
	}
	if dialectMetaSchemaCache == nil {
		dialectMetaSchemaCache = make(map[string]*Schema)
	}
	dialectMetaSchemaCache[uri] = s
	return s, true
}
