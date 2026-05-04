package jsonschema

import (
	"fmt"
	"strings"
)

// Schema is a compiled JSON Schema. Once produced by [Compile] (or its
// siblings), a Schema is immutable and safe for concurrent validation —
// callers may freely share a single [*Schema] across goroutines and call
// any of the Validate-family methods concurrently.
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
	// resources is the resource tree built at compile time. The validator
	// walks this for ref / dynamicRef resolution.
	resources *resourceMap
	// bindings carries one entry per recognized keyword instance, captured
	// for introspection helpers and metadata-only consumers.
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
// constructed without a resource map.
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

// Vocabularies returns the set of vocabulary URIs declared by the schema's
// effective $vocabulary keyword (or, when absent, the standard set for the
// schema's draft). Returns a fresh slice each call; safe for callers to
// mutate.
//
// In v0.1, $vocabulary at the schema root is recognized as a structural
// keyword but not honored as an opt-in selector — the returned set is
// always the standard vocabularies for [Schema.Draft]. When the root
// schema declares a [VocabOAS]-aware $schema (the [OASDialectURL]),
// [VocabOAS] is included alongside the draft's standard set. Custom
// vocabulary registration is reserved for v0.2.
func (s *Schema) Vocabularies() []string {
	if s == nil {
		return nil
	}
	stdSet := stdVocabularySet(s.draft)
	// If the root schema declared $vocabulary, prefer that set verbatim
	// (even though we don't honor opt-in/opt-out semantics yet — this lets
	// introspecting tools see what the schema asked for).
	if uris := rootVocabularyURIs(s); uris != nil {
		out := make([]string, len(uris))
		copy(out, uris)
		return out
	}
	out := make([]string, 0, len(stdSet)+1)
	out = append(out, stdSet...)
	if s.metaSchemaURI == OASDialectURL {
		out = append(out, VocabOAS)
	}
	return out
}

// Bindings returns the keyword bindings extracted at compile time. Each
// binding records the keyword name, its source location, and the keyword's
// raw value. Returns a fresh slice each call; the slice is safe for callers
// to mutate, but the embedded RawValue may share storage with the schema's
// parsed source.
//
// In v0.1, ref-resolution targets are not exposed in the public binding —
// only Name, Location, and RawValue are populated. v0.2 may extend
// [KeywordBinding] with a typed Resolved field.
func (s *Schema) Bindings() []KeywordBinding {
	if s == nil || len(s.bindings) == 0 {
		return nil
	}
	out := make([]KeywordBinding, len(s.bindings))
	for i, b := range s.bindings {
		out[i] = KeywordBinding{
			Name:     b.Name,
			Location: b.Location,
			RawValue: b.RawValue,
		}
	}
	return out
}

// KeywordBinding is the public projection of one keyword binding extracted
// at compile time. Returned by [*Schema.Bindings] for introspection and
// metadata-only consumers.
type KeywordBinding struct {
	// Name is the keyword identifier (e.g. "minLength", "$ref").
	Name string
	// Location is the JSON Pointer of the keyword in the source schema.
	Location string
	// RawValue is the parsed value of the keyword (json.Unmarshal'd).
	// May be a map[string]any, []any, json.Number, string, bool, or nil.
	RawValue any
}

// stdVocabularySet returns the standard vocabulary URIs registered for
// draft d. The slice is built fresh on each call.
func stdVocabularySet(d Draft) []string {
	// Vocabulary URIs vary across drafts, but the package's stdVocabularies
	// table is keyed at the 2020-12 URIs. For pre-2019-09 drafts the
	// $vocabulary mechanism does not exist — we still surface the set the
	// validator implements so introspection callers see something useful.
	if d == DraftUnknown {
		return nil
	}
	out := make([]string, 0, len(stdVocabularies))
	for _, v := range stdVocabularies {
		// VocabOAS is included only when the schema's meta-schema opts
		// into it; it is not a standard 2020-12 vocabulary.
		if v.URI == VocabOAS {
			continue
		}
		out = append(out, v.URI)
	}
	return out
}

// rootVocabularyURIs extracts the URIs declared by a $vocabulary keyword
// at the schema root, in declaration order. Returns nil if no $vocabulary
// is present (or the binding is malformed).
func rootVocabularyURIs(s *Schema) []string {
	for _, b := range s.bindings {
		if b.Name != "$vocabulary" || b.Location != "#/$vocabulary" {
			continue
		}
		m, ok := b.RawValue.(map[string]any)
		if !ok {
			return nil
		}
		out := make([]string, 0, len(m))
		for uri, enabled := range m {
			// Per Draft 2019-09, the value is a bool. Honor only true
			// entries; unknown/false vocabularies are dropped.
			b, ok := enabled.(bool)
			if !ok || !b {
				continue
			}
			out = append(out, uri)
		}
		if len(out) == 0 {
			return nil
		}
		return out
	}
	return nil
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

// newSchemaForTest is an unexported constructor used by tests to exercise
// Schema accessor methods without the full compiler. It is not part of the
// public API.
func newSchemaForTest(source []byte, draft Draft, id, metaSchemaURI string) *Schema {
	cp := append([]byte(nil), source...)
	return &Schema{
		source:        cp,
		draft:         draft,
		id:            id,
		metaSchemaURI: metaSchemaURI,
	}
}
