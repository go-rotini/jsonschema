package jsonschema

import (
	"fmt"
	"strings"
)

// Schema is a compiled JSON Schema. Once produced by [Compile] (or its
// siblings), a Schema is immutable and safe for concurrent validation.
//
// Phase 3 wires the resource tree, keyword bindings, and resolved ref
// edges. Phase 4 adds the validator engine that walks them.
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
	// resources is the resource tree built at compile time. Phase 4 walks
	// this for validation.
	resources *resourceMap
	// bindings carries one entry per recognized keyword instance — the
	// compile-time stub Phase 4 turns into a real evaluator chain.
	bindings []keywordBinding
	// root is the runtime evaluator tree — a parallel structure that the
	// validator walks against an instance. Populated by the compile path.
	root *subschema
	// compileOpts carries the options the schema was compiled with.
	// Validation re-uses the loader and ref-depth limits stored here.
	compileOpts *compileOptions
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

// Resources returns the absolute URIs of every $id-bounded resource the
// [*Schema] directly contains: the root URI first, then each nested
// resource in declaration order. Returns nil when the schema is nil or was
// constructed without a resource map (e.g. a Phase 2 test fixture).
func (s *Schema) Resources() []string {
	if s == nil || s.resources == nil {
		return nil
	}
	out := make([]string, 0, len(s.resources.order))
	seen := make(map[string]struct{}, len(s.resources.order))
	for _, uri := range s.resources.order {
		if _, dup := seen[uri]; dup {
			continue
		}
		seen[uri] = struct{}{}
		out = append(out, uri)
	}
	return out
}

// Anchors returns the plain-name anchors declared in the root resource.
// Returns nil when the schema is nil or was constructed without a resource
// map. The returned slice is freshly allocated; callers may mutate it.
func (s *Schema) Anchors() []string {
	if s == nil || s.resources == nil {
		return nil
	}
	root, ok := s.resources.byURI[s.resources.rootURI]
	if !ok {
		return nil
	}
	out := make([]string, 0, len(root.anchors))
	for name := range root.anchors {
		out = append(out, name)
	}
	return out
}

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
