package jsonschema

// Keyword is the metadata-only interface every JSON Schema keyword satisfies.
// It carries the keyword's name and the [Draft] range in which it is
// recognized. The compiler uses these values to route a schema member to its
// evaluator; tooling uses them to enumerate the keyword set active for a
// given draft via [KeywordsForDraft] / [LookupKeyword].
type Keyword interface {
	// Name returns the keyword as it appears in a schema (e.g. "minLength").
	Name() string
	// SinceDraft returns the first draft in which this keyword exists.
	SinceDraft() Draft
	// RetiredInDraft returns the draft in which this keyword was removed,
	// or [DraftUnknown] if the keyword is still current in 2020-12.
	RetiredInDraft() Draft
}

// Vocabulary groups a set of keywords under a single URI per Draft 2019-09's
// vocabulary mechanism. The standard vocabularies are registered at package
// init time; custom-vocabulary registration is reserved for v0.2.
type Vocabulary struct {
	// URI is the canonical identifier for the vocabulary (e.g.
	// VocabApplicator).
	URI string
	// Keywords lists every keyword that belongs to the vocabulary. The
	// order is informational; the package does not depend on it.
	Keywords []Keyword
}

// Standard vocabulary URIs (Draft 2020-12).
const (
	// VocabCore identifies the meta-keywords vocabulary
	// ($schema, $id, $ref, $dynamicRef, $defs, $anchor, $dynamicAnchor,
	// $comment, $vocabulary).
	VocabCore = "https://json-schema.org/draft/2020-12/vocab/core"
	// VocabApplicator identifies the keywords that apply subschemas
	// (allOf, anyOf, properties, items, ...).
	VocabApplicator = "https://json-schema.org/draft/2020-12/vocab/applicator"
	// VocabUnevaluated identifies unevaluatedItems / unevaluatedProperties.
	VocabUnevaluated = "https://json-schema.org/draft/2020-12/vocab/unevaluated"
	// VocabValidation identifies the assertion keywords (type, enum, ...).
	VocabValidation = "https://json-schema.org/draft/2020-12/vocab/validation"
	// VocabFormatAnnot identifies the annotation-only flavor of the
	// "format" keyword (the Draft 2020-12 default).
	VocabFormatAnnot = "https://json-schema.org/draft/2020-12/vocab/format-annotation"
	// VocabFormatAssert identifies the assertion flavor of "format"
	// (opt-in via [WithFormatAssertion]).
	VocabFormatAssert = "https://json-schema.org/draft/2020-12/vocab/format-assertion"
	// VocabContent identifies contentEncoding / contentMediaType /
	// contentSchema.
	VocabContent = "https://json-schema.org/draft/2020-12/vocab/content"
	// VocabMetaData identifies title / description / default / examples /
	// readOnly / writeOnly / deprecated.
	VocabMetaData = "https://json-schema.org/draft/2020-12/vocab/meta-data"
	// VocabOAS identifies the OpenAPI 3.1 base vocabulary that ships with
	// the OAS dialect ([OASDialectURL]). It contributes the four annotation-
	// only keywords [discriminator], [xml], [externalDocs], and [example].
	// The package registers this vocabulary unconditionally so OpenAPI 3.1
	// schemas compile cleanly whether or not their $schema points at the
	// OAS dialect.
	VocabOAS = "https://spec.openapis.org/oas/3.1/vocab/base"
)

// OAS dialect identifiers for OpenAPI 3.1.
const (
	// OASDialectURL is the canonical URI of the OpenAPI 3.1 Schema Object
	// dialect's meta-schema. A schema declaring this URL as $schema opts
	// into Draft 2020-12 plus the [VocabOAS] vocabulary.
	OASDialectURL = "https://spec.openapis.org/oas/3.1/dialect/base"
	// OASBaseSchemaURL is the canonical $id of the upstream OpenAPI 3.1
	// document schema (the schema that validates OpenAPI documents
	// themselves, distinct from the dialect that those schemas use).
	OASBaseSchemaURL = "https://spec.openapis.org/oas/3.1/schema/2022-10-07"
)

// simpleKeyword is the metadata-only implementation of [Keyword] used by the
// standard vocabularies. The evaluator graph for each keyword is registered
// separately via the per-keyword evaluator builders in eval_*.go.
type simpleKeyword struct {
	name    string
	since   Draft
	retired Draft
}

// Name implements [Keyword].
func (k simpleKeyword) Name() string { return k.name }

// SinceDraft implements [Keyword].
func (k simpleKeyword) SinceDraft() Draft { return k.since }

// RetiredInDraft implements [Keyword].
func (k simpleKeyword) RetiredInDraft() Draft { return k.retired }

// kw is a small constructor that keeps the registry tables compact.
func kw(name string, since, retired Draft) Keyword {
	return simpleKeyword{name: name, since: since, retired: retired}
}

// stdVocabularies is the registered list of standard 2020-12 vocabularies
// plus the historical-draft keyword aliases that this package recognizes.
// Built once at package init and exposed read-only by [Vocabularies].
var stdVocabularies = []Vocabulary{
	{
		URI: VocabCore,
		Keywords: []Keyword{
			kw("$schema", Draft6, DraftUnknown),
			kw("$id", Draft6, DraftUnknown),
			kw("$ref", Draft4, DraftUnknown),
			kw("$dynamicRef", Draft202012, DraftUnknown),
			kw("$defs", Draft201909, DraftUnknown),
			kw("$anchor", Draft201909, DraftUnknown),
			kw("$dynamicAnchor", Draft202012, DraftUnknown),
			kw("$comment", Draft7, DraftUnknown),
			kw("$vocabulary", Draft201909, DraftUnknown),
			// Legacy aliases: "id" / "definitions" predate "$id" / "$defs";
			// $recursiveRef / $recursiveAnchor were retired in 2020-12.
			kw("id", Draft4, Draft6),
			kw("definitions", Draft4, Draft201909),
			kw("$recursiveRef", Draft201909, Draft202012),
			kw("$recursiveAnchor", Draft201909, Draft202012),
		},
	},
	{
		URI: VocabApplicator,
		Keywords: []Keyword{
			kw("allOf", Draft4, DraftUnknown),
			kw("anyOf", Draft4, DraftUnknown),
			kw("oneOf", Draft4, DraftUnknown),
			kw("not", Draft4, DraftUnknown),
			kw("if", Draft7, DraftUnknown),
			kw("then", Draft7, DraftUnknown),
			kw("else", Draft7, DraftUnknown),
			kw("dependentSchemas", Draft201909, DraftUnknown),
			kw("prefixItems", Draft202012, DraftUnknown),
			kw("items", Draft4, DraftUnknown),
			kw("contains", Draft6, DraftUnknown),
			kw("properties", Draft4, DraftUnknown),
			kw("patternProperties", Draft4, DraftUnknown),
			kw("additionalProperties", Draft4, DraftUnknown),
			kw("propertyNames", Draft6, DraftUnknown),
			// Legacy aliases retired by later drafts.
			kw("additionalItems", Draft4, Draft202012),
			kw("dependencies", Draft4, Draft201909),
		},
	},
	{
		URI: VocabUnevaluated,
		Keywords: []Keyword{
			kw("unevaluatedItems", Draft201909, DraftUnknown),
			kw("unevaluatedProperties", Draft201909, DraftUnknown),
		},
	},
	{
		URI: VocabValidation,
		Keywords: []Keyword{
			kw("type", Draft4, DraftUnknown),
			kw("enum", Draft4, DraftUnknown),
			kw("const", Draft6, DraftUnknown),
			kw("multipleOf", Draft4, DraftUnknown),
			kw("maximum", Draft4, DraftUnknown),
			kw("exclusiveMaximum", Draft4, DraftUnknown),
			kw("minimum", Draft4, DraftUnknown),
			kw("exclusiveMinimum", Draft4, DraftUnknown),
			kw("maxLength", Draft4, DraftUnknown),
			kw("minLength", Draft4, DraftUnknown),
			kw("pattern", Draft4, DraftUnknown),
			kw("maxItems", Draft4, DraftUnknown),
			kw("minItems", Draft4, DraftUnknown),
			kw("uniqueItems", Draft4, DraftUnknown),
			kw("maxContains", Draft201909, DraftUnknown),
			kw("minContains", Draft201909, DraftUnknown),
			kw("maxProperties", Draft4, DraftUnknown),
			kw("minProperties", Draft4, DraftUnknown),
			kw("required", Draft4, DraftUnknown),
			kw("dependentRequired", Draft201909, DraftUnknown),
		},
	},
	{
		// "format" appears in both the annotation and assertion
		// vocabularies; the dialect's $vocabulary opts into one or the
		// other.
		URI: VocabFormatAnnot,
		Keywords: []Keyword{
			kw("format", Draft4, DraftUnknown),
		},
	},
	{
		URI: VocabFormatAssert,
		Keywords: []Keyword{
			kw("format", Draft4, DraftUnknown),
		},
	},
	{
		URI: VocabContent,
		Keywords: []Keyword{
			kw("contentEncoding", Draft7, DraftUnknown),
			kw("contentMediaType", Draft7, DraftUnknown),
			kw("contentSchema", Draft201909, DraftUnknown),
		},
	},
	{
		URI: VocabMetaData,
		Keywords: []Keyword{
			kw("title", Draft4, DraftUnknown),
			kw("description", Draft4, DraftUnknown),
			kw("default", Draft4, DraftUnknown),
			kw("deprecated", Draft201909, DraftUnknown),
			kw("readOnly", Draft7, DraftUnknown),
			kw("writeOnly", Draft7, DraftUnknown),
			kw("examples", Draft6, DraftUnknown),
		},
	},
	{
		URI: VocabOAS,
		Keywords: []Keyword{
			kw("discriminator", Draft202012, DraftUnknown),
			kw("xml", Draft202012, DraftUnknown),
			kw("externalDocs", Draft202012, DraftUnknown),
			kw("example", Draft202012, DraftUnknown),
		},
	},
}

// Vocabularies returns the registered standard vocabularies in declaration
// order. The returned slice is a defensive copy; the underlying Keyword
// values are immutable so the per-element copy does not deep-copy them.
func Vocabularies() []Vocabulary {
	out := make([]Vocabulary, len(stdVocabularies))
	for i, v := range stdVocabularies {
		kws := make([]Keyword, len(v.Keywords))
		copy(kws, v.Keywords)
		out[i] = Vocabulary{URI: v.URI, Keywords: kws}
	}
	return out
}

// KeywordsForDraft returns every keyword that is recognized when the active
// draft is d, deduplicated by name. A keyword belongs to the result iff
// its [Keyword.SinceDraft] is ≤ d and its [Keyword.RetiredInDraft] is either
// [DraftUnknown] or > d. Returns nil for [DraftUnknown].
func KeywordsForDraft(d Draft) []Keyword {
	if d == DraftUnknown {
		return nil
	}
	seen := make(map[string]struct{})
	var out []Keyword
	for _, v := range stdVocabularies {
		for _, k := range v.Keywords {
			if !keywordActiveIn(k, d) {
				continue
			}
			if _, dup := seen[k.Name()]; dup {
				continue
			}
			seen[k.Name()] = struct{}{}
			out = append(out, k)
		}
	}
	return out
}

// LookupKeyword finds a keyword by name within the active set for draft d.
// Returns ok=false when the keyword is not recognized by the package, when
// it does not exist in the requested draft, or when d is [DraftUnknown].
func LookupKeyword(name string, d Draft) (Keyword, bool) {
	if d == DraftUnknown || name == "" {
		return nil, false
	}
	for _, v := range stdVocabularies {
		for _, k := range v.Keywords {
			if k.Name() != name {
				continue
			}
			if keywordActiveIn(k, d) {
				return k, true
			}
		}
	}
	return nil, false
}

// keywordActiveIn reports whether k is part of the active keyword set for d.
// A keyword is active when it has shipped (SinceDraft ≤ d) AND has not yet
// been retired (RetiredInDraft is DraftUnknown or > d).
func keywordActiveIn(k Keyword, d Draft) bool {
	if k.SinceDraft() > d {
		return false
	}
	if r := k.RetiredInDraft(); r != DraftUnknown && d >= r {
		return false
	}
	return true
}
