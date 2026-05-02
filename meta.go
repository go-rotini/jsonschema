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

// metaSchemaPaths maps every recognized draft to its embedded file path
// inside [metaSchemaFS]. New drafts must be added here AND in [Draft] /
// [Draft.MetaSchemaURL].
var metaSchemaPaths = map[Draft]string{
	Draft4:      "meta/draft-04.json",
	Draft6:      "meta/draft-06.json",
	Draft7:      "meta/draft-07.json",
	Draft201909: "meta/draft-2019-09.json",
	Draft202012: "meta/draft-2020-12.json",
}

// MetaSchemaBytes returns the JSON bytes of the canonical meta-schema for
// d. The bytes are a slice into the package's embedded copy and must be
// treated as read-only; callers that need a mutable buffer should copy.
//
// Returns [ErrUnknownDraft] for [DraftUnknown] or any unrecognized value.
func MetaSchemaBytes(d Draft) ([]byte, error) {
	path, ok := metaSchemaPaths[d]
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrUnknownDraft, d.String())
	}
	data, err := fs.ReadFile(metaSchemaFS, path)
	if err != nil {
		return nil, fmt.Errorf("jsonschema: read embedded meta-schema %q: %w", path, err)
	}
	return data, nil
}

// MetaSchemaURL returns the canonical URL of the meta-schema for d. It is
// equivalent to d.MetaSchemaURL() and re-exported as a package-level
// function for symmetry with [MetaSchemaBytes].
func MetaSchemaURL(d Draft) string {
	return d.MetaSchemaURL()
}

// metaSchemaCache memoizes [MetaSchema] results per draft so repeated calls
// don't re-parse the embedded bytes. The compile path uses an embedded-only
// MapLoader so it never touches the network.
var (
	metaSchemaCacheMu sync.Mutex
	metaSchemaCache   = make(map[Draft]*Schema)
)

// MetaSchema returns the compiled [*Schema] for the canonical meta-schema
// of d. The result is memoized; repeated calls return the same pointer.
//
// The compile path uses a [Compiler] backed by the embedded meta-schema
// [MapLoader] so refs into the per-vocabulary meta-schemas resolve without
// network access.
func MetaSchema(d Draft) (*Schema, error) {
	if _, ok := metaSchemaPaths[d]; !ok {
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
	metaSchemaCache[d] = s
	return s, nil
}
