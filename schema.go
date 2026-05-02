package jsonschema

import (
	"fmt"
	"io"
	"strings"
)

// Schema is a compiled JSON Schema. Once produced by [Compile] (or its
// siblings), a Schema is immutable and safe for concurrent validation.
//
// Phase 2 populates the source / draft / id / metaSchemaURI metadata. The
// validator graph (keyword evaluators, ref edges, anchor index) lands in
// Phase 3 and is intentionally absent from the public API surface so that
// later phases can extend the type without breaking callers.
type Schema struct {
	// source is the canonical JSON byte sequence the schema was compiled
	// from. [Schema.MarshalJSON] returns this verbatim so a *Schema embeds
	// transparently in stdlib json.Marshal output.
	source []byte
	// draft is the effective draft for this schema (set from $schema, the
	// caller's WithDefaultDraft, or DraftDefault).
	draft Draft
	// id is the schema's resolved $id (absolute URI). Empty when no $id
	// was declared at the root.
	id string
	// metaSchemaURI is the URI from the $schema keyword (or
	// draft.MetaSchemaURL() when $schema is absent).
	metaSchemaURI string
}

// Draft returns the draft this schema was compiled against.
func (s *Schema) Draft() Draft {
	if s == nil {
		return DraftUnknown
	}
	return s.draft
}

// ID returns the schema's $id (the absolute URI); empty if no $id was
// declared at the schema root.
func (s *Schema) ID() string {
	if s == nil {
		return ""
	}
	return s.id
}

// MetaSchemaURI returns the meta-schema URI declared via the $schema
// keyword, or the canonical URL of the schema's draft when $schema was
// absent.
func (s *Schema) MetaSchemaURI() string {
	if s == nil {
		return ""
	}
	if s.metaSchemaURI != "" {
		return s.metaSchemaURI
	}
	return s.draft.MetaSchemaURL()
}

// MarshalJSON returns the schema's source bytes. A *Schema therefore embeds
// transparently in [encoding/json.Marshal] output.
func (s *Schema) MarshalJSON() ([]byte, error) {
	if s == nil || len(s.source) == 0 {
		return []byte("null"), nil
	}
	out := make([]byte, len(s.source))
	copy(out, s.source)
	return out, nil
}

// String returns a brief, human-readable summary of the schema for log lines
// and debug output. Format: `Schema(<draft> [id=<id>] [bytes=<n>])`.
func (s *Schema) String() string {
	if s == nil {
		return "Schema(<nil>)"
	}
	var b strings.Builder
	b.WriteString("Schema(")
	b.WriteString(s.draft.String())
	if s.id != "" {
		fmt.Fprintf(&b, " id=%s", s.id)
	}
	if len(s.source) > 0 {
		fmt.Fprintf(&b, " bytes=%d", len(s.source))
	}
	b.WriteString(")")
	return b.String()
}

// Validate validates instanceJSON against the schema and returns a [*Result].
//
// PHASE 3 STUB: the compiler / validator engine lands in Phase 3 / Phase 4.
// Until then this method returns [ErrSchemaNotCompiled] so callers receive
// a typed error rather than a misleading pass/fail Result.
func (s *Schema) Validate(_ []byte, _ ...Option) (*Result, error) {
	return nil, ErrSchemaNotCompiled
}

// ValidateValue validates an already-decoded Go value against the schema.
//
// PHASE 3 STUB: see [Schema.Validate].
func (s *Schema) ValidateValue(_ any, _ ...Option) (*Result, error) {
	return nil, ErrSchemaNotCompiled
}

// ValidateReader streams instance bytes from r and validates them against
// the schema.
//
// PHASE 3 STUB: see [Schema.Validate].
func (s *Schema) ValidateReader(_ io.Reader, _ ...Option) (*Result, error) {
	return nil, ErrSchemaNotCompiled
}

// ValidateAndUnmarshal validates instanceJSON, then (on success) decodes it
// into v.
//
// PHASE 3 STUB: see [Schema.Validate].
func (s *Schema) ValidateAndUnmarshal(_ []byte, _ any, _ ...Option) error {
	return ErrSchemaNotCompiled
}

// Option is the validation-time option type. Phase 3+ defines the concrete
// constructors (WithFormatAssertion, WithStopOnFirstError, ...). Phase 2
// declares the type only so the [Schema] method signatures are stable.
type Option func(*runOptions)

// runOptions carries the validation-time configuration. Phase 2 leaves the
// struct empty; later phases append fields without changing the public
// signature of [Option].
type runOptions struct{}

// newSchemaForTest is an unexported constructor used by Phase 2 tests to
// exercise Schema accessor methods without the full compiler. It is not
// part of the public API.
func newSchemaForTest(source []byte, draft Draft, id, metaSchemaURI string) *Schema {
	cp := append([]byte(nil), source...)
	return &Schema{
		source:        cp,
		draft:         draft,
		id:            id,
		metaSchemaURI: metaSchemaURI,
	}
}
