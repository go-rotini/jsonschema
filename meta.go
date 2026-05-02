package jsonschema

import (
	"embed"
	"fmt"
	"io/fs"
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

// MetaSchema returns the compiled [*Schema] for the canonical meta-schema
// of d.
//
// PHASE 3 STUB: requires the compiler. Returns nil + [ErrSchemaNotCompiled]
// until Phase 3 lands. Callers that just need the meta-schema bytes (e.g.
// for offline validation tooling) should use [MetaSchemaBytes] instead.
func MetaSchema(d Draft) (*Schema, error) {
	if _, ok := metaSchemaPaths[d]; !ok {
		return nil, fmt.Errorf("%w: %s", ErrUnknownDraft, d.String())
	}
	return nil, ErrSchemaNotCompiled
}
