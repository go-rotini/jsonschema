package jsonschema

import (
	"io"
	"io/fs"
	"reflect"
)

// CompileOption configures how [Compile], [CompileURL], [CompileValue], and
// the [Compiler] type process schema documents. Apply options at compiler
// construction (or per-call); they shape draft selection, ref loading,
// strictness, and meta-schema handling.
type CompileOption func(*compileOptions)

// compileOptions carries the compile-time configuration for a single
// compilation (or compiler instance). Fields are read-only after compilation
// begins; the compiler does not mutate them while walking a schema.
type compileOptions struct {
	defaultDraft         Draft
	loader               Loader
	baseURI              string
	maxRefDepth          int
	strictKeywords       bool
	customVocabularies   []Vocabulary
	regexEngine          RegexEngine
	metaSchemaValidation bool
	refCollisionPolicy   RefCollisionPolicy
	loaderTrace          io.Writer
	defaultDraftSet      bool
	loaderSet            bool
	regexEngineSet       bool
	maxRefDepthSet       bool
}

// defaultCompileOptions returns a freshly-initialized [*compileOptions] with
// sensible defaults. Callers (the [Compiler] constructor or a one-shot
// [Compile] call) apply [CompileOption] mutations on top.
func defaultCompileOptions() *compileOptions {
	return &compileOptions{
		defaultDraft:       DraftDefault,
		loader:             nil, // resolved lazily to DefaultLoader() on first use
		maxRefDepth:        100,
		refCollisionPolicy: RefCollisionError,
	}
}

// WithDefaultDraft sets the draft used when a schema's $schema keyword is
// absent. Default: [DraftDefault] (Draft 2020-12).
func WithDefaultDraft(d Draft) CompileOption {
	return func(o *compileOptions) {
		o.defaultDraft = d
		o.defaultDraftSet = true
	}
}

// WithLoader sets the [Loader] used to fetch external $refs. Default: a
// [ChainLoader] of the embedded meta-schemas plus an HTTPS-only [HTTPLoader].
func WithLoader(l Loader) CompileOption {
	return func(o *compileOptions) {
		o.loader = l
		o.loaderSet = true
	}
}

// WithBaseURI sets the base URI for the root schema when the schema does not
// declare an $id. Useful when compiling a schema fragment loaded from a known
// URL so that relative refs resolve correctly.
func WithBaseURI(uri string) CompileOption {
	return func(o *compileOptions) { o.baseURI = uri }
}

// WithMaxRefDepth limits how many $ref hops a single keyword evaluation may
// follow. Guards against pathological recursive schemas. Default: 100.
func WithMaxRefDepth(n int) CompileOption {
	return func(o *compileOptions) {
		o.maxRefDepth = n
		o.maxRefDepthSet = true
	}
}

// WithStrictKeywords causes unknown keywords to raise a [*CompileError] at
// compile time. Default: unknown keywords are kept as annotations and ignored
// at validation.
func WithStrictKeywords(b bool) CompileOption {
	return func(o *compileOptions) { o.strictKeywords = b }
}

// WithVocabulary registers a custom [Vocabulary] (a set of additional
// keywords). Custom vocabularies take effect when their URI appears in the
// schema's $vocabulary block.
func WithVocabulary(v Vocabulary) CompileOption {
	return func(o *compileOptions) {
		o.customVocabularies = append(o.customVocabularies, v)
	}
}

// RegexEngine swaps the regex engine used to compile the "pattern" keyword.
// The default engine uses Go's [regexp] in Perl mode, which covers a useful
// subset of ECMA-262 — incompatible patterns surface as compile errors.
type RegexEngine interface {
	// Compile parses pattern and returns a compiled matcher.
	Compile(pattern string) (RegexMatcher, error)
}

// RegexMatcher is the minimal interface that a regex engine must expose for
// the validator's "pattern" keyword. Implementations should be safe for
// concurrent use.
type RegexMatcher interface {
	// MatchString reports whether pattern matches anywhere in s.
	MatchString(s string) bool
}

// WithRegexEngine swaps the regex engine used by the "pattern" keyword.
func WithRegexEngine(e RegexEngine) CompileOption {
	return func(o *compileOptions) {
		o.regexEngine = e
		o.regexEngineSet = true
	}
}

// WithMetaSchemaValidation enables compile-time validation of each schema
// against its declared meta-schema. Catches typos and structural errors
// early. Default: false (off for performance; recommended in CI).
//
// When the schema declares the OpenAPI 3.1 dialect URL ([OASDialectURL])
// the embedded dialect meta-schema is used; otherwise the standard
// meta-schema for the schema's draft is used.
func WithMetaSchemaValidation(b bool) CompileOption {
	return func(o *compileOptions) { o.metaSchemaValidation = b }
}

// WithRefCollisionPolicy controls behavior when two compiled documents
// declare the same $id. Default: [RefCollisionError] (compile fails).
func WithRefCollisionPolicy(p RefCollisionPolicy) CompileOption {
	return func(o *compileOptions) { o.refCollisionPolicy = p }
}

// WithLoaderTrace writes one line per [Loader] call to w (URI, outcome,
// duration, cache hit/miss). Useful for diagnosing $ref resolution.
func WithLoaderTrace(w io.Writer) CompileOption {
	return func(o *compileOptions) { o.loaderTrace = w }
}

// Option configures a single validation call. Applied to [*Schema] Validate
// methods and the package-level [Validate] function.
//
// Options are evaluated left-to-right; later options override earlier ones
// for the same field. Most callers will mix [WithFormatAssertion],
// [WithStopOnFirstError], [WithMaxValidationDepth], and [WithCustomFormat].
type Option func(*runOptions)

// runOptions carries the per-call validation configuration.
type runOptions struct {
	formatAssertion     bool
	contentAssertion    bool
	stopOnFirstError    bool
	maxInstanceSize     int
	maxValidationDepth  int
	unknownFormatPolicy UnknownFormatPolicy
	customFormats       map[string]func(string) error
	maxErrors           int
	readOnly            bool
	writeOnly           bool
	collectAnnotations  bool
}

// defaultRunOptions returns a freshly-initialized [*runOptions] with sensible
// defaults: format / content as annotation-only, validation depth at 1000,
// annotation collection enabled, no instance-size cap.
func defaultRunOptions() *runOptions {
	return &runOptions{
		formatAssertion:     false,
		contentAssertion:    false,
		stopOnFirstError:    false,
		maxInstanceSize:     0,
		maxValidationDepth:  1000,
		unknownFormatPolicy: UnknownFormatIgnore,
		customFormats:       nil,
		maxErrors:           0,
		readOnly:            false,
		writeOnly:           false,
		collectAnnotations:  true,
	}
}

// WithFormatAssertion enables "format" as an assertion (default: annotation
// only). When true, format-validator failures become validation errors.
func WithFormatAssertion(b bool) Option {
	return func(o *runOptions) { o.formatAssertion = b }
}

// WithContentAssertion enables contentSchema as an assertion (default:
// annotation only).
func WithContentAssertion(b bool) Option {
	return func(o *runOptions) { o.contentAssertion = b }
}

// WithStopOnFirstError short-circuits validation as soon as the first error
// is found. Equivalent to selecting OutputFlag at the collector level.
func WithStopOnFirstError(b bool) Option {
	return func(o *runOptions) { o.stopOnFirstError = b }
}

// WithMaxInstanceSize rejects instance documents larger than n bytes before
// parsing. Default: 0 (no limit).
func WithMaxInstanceSize(n int) Option {
	return func(o *runOptions) { o.maxInstanceSize = n }
}

// WithMaxValidationDepth limits recursion into nested objects/arrays to
// guard against unbounded recursive schemas with adversarial instances.
// Default: 1000.
func WithMaxValidationDepth(n int) Option {
	return func(o *runOptions) { o.maxValidationDepth = n }
}

// WithUnknownFormat controls handling of unrecognized "format" names.
func WithUnknownFormat(p UnknownFormatPolicy) Option {
	return func(o *runOptions) { o.unknownFormatPolicy = p }
}

// WithCustomFormat registers a format validator for the given format name.
func WithCustomFormat(name string, fn func(string) error) Option {
	return func(o *runOptions) {
		if o.customFormats == nil {
			o.customFormats = make(map[string]func(string) error)
		}
		o.customFormats[name] = fn
	}
}

// WithMaxErrors caps the number of [ValidationError] entries collected.
// After the cap is reached, validation continues (unless
// [WithStopOnFirstError] is also set) but additional errors are dropped.
// Default: 0 (unlimited).
func WithMaxErrors(n int) Option {
	return func(o *runOptions) { o.maxErrors = n }
}

// WithReadOnly enables direction-aware validation in the "output" direction.
// readOnly properties remain required if listed in required; writeOnly
// properties become optional and are skipped if present.
func WithReadOnly(b bool) Option {
	return func(o *runOptions) { o.readOnly = b }
}

// WithWriteOnly is the inverse of [WithReadOnly] for the "input" direction.
func WithWriteOnly(b bool) Option {
	return func(o *runOptions) { o.writeOnly = b }
}

// WithCollectAnnotations controls whether passing keyword annotations are
// accumulated in [Result.Annotations]. Default: true.
func WithCollectAnnotations(b bool) Option {
	return func(o *runOptions) { o.collectAnnotations = b }
}

// GenerateOption configures the schema generator. Pass options to [Generate],
// [GenerateBytes], [FromType], or [NewGenerator]; later constructors return a
// reusable [*Generator] that amortizes option parsing across many types.
type GenerateOption func(*generateOptions)

// generateOptions carries the generator's configuration.
type generateOptions struct {
	draft                     Draft
	id                        string
	expandedRefs              bool
	omitDescriptions          bool
	durationAsString          bool
	nullablePointers          bool
	orderedProperties         bool
	additionalPropertiesFalse bool
	emitSchemaDeclaration     bool
	interfaceAsAny            bool
	customEmitters            map[reflect.Type]func(reflect.Type) *Schema
	docReaderFS               fs.FS
	draftSet                  bool
	orderedPropertiesSet      bool
	emitSchemaDeclarationSet  bool
	interfaceAsAnySet         bool
}

// WithGenerateDraft sets the schema draft to emit. Default: Draft 2020-12.
func WithGenerateDraft(d Draft) GenerateOption {
	return func(o *generateOptions) {
		o.draft = d
		o.draftSet = true
	}
}

// WithGenerateID sets the $id of the generated root schema.
func WithGenerateID(id string) GenerateOption {
	return func(o *generateOptions) { o.id = id }
}

// WithGenerateExpandedRefs inlines all referenced types instead of using
// $ref + $defs. Default: false.
func WithGenerateExpandedRefs(b bool) GenerateOption {
	return func(o *generateOptions) { o.expandedRefs = b }
}

// WithGenerateOmitDescriptions suppresses extracting Go doc comments as
// description strings. Default: false.
func WithGenerateOmitDescriptions(b bool) GenerateOption {
	return func(o *generateOptions) { o.omitDescriptions = b }
}

// WithGenerateDurationAsString emits time.Duration as
// {"type":"string","format":"duration"} instead of integer nanoseconds.
//
// The function name has the Generate prefix to disambiguate from any future
// runtime option of the same intent; the underlying generator option is
// equivalent to the requirements doc's "WithDurationAsString".
func WithGenerateDurationAsString(b bool) GenerateOption {
	return func(o *generateOptions) { o.durationAsString = b }
}

// WithGenerateNullablePointers emits *T as anyOf:[null, T]. Default: false.
func WithGenerateNullablePointers(b bool) GenerateOption {
	return func(o *generateOptions) { o.nullablePointers = b }
}

// WithGenerateOrderedProperties uses [MapSlice] ordering for properties so
// the emitted schema preserves Go field declaration order. Default: true.
func WithGenerateOrderedProperties(b bool) GenerateOption {
	return func(o *generateOptions) {
		o.orderedProperties = b
		o.orderedPropertiesSet = true
	}
}

// WithGenerateAdditionalPropertiesFalse emits "additionalProperties": false
// on every generated struct schema. Default: false.
func WithGenerateAdditionalPropertiesFalse(b bool) GenerateOption {
	return func(o *generateOptions) { o.additionalPropertiesFalse = b }
}

// WithGenerateSchemaDeclaration emits "$schema" on the root of generated
// schemas. Default: true.
func WithGenerateSchemaDeclaration(b bool) GenerateOption {
	return func(o *generateOptions) {
		o.emitSchemaDeclaration = b
		o.emitSchemaDeclarationSet = true
	}
}

// WithGenerateInterfaceAsAny controls how interface{} / any is rendered.
// true (default): {} (any-value). false: error at generation time so the
// caller is forced to register a custom emitter.
func WithGenerateInterfaceAsAny(b bool) GenerateOption {
	return func(o *generateOptions) {
		o.interfaceAsAny = b
		o.interfaceAsAnySet = true
	}
}

// WithCustomEmitter registers a function that emits the schema for values of
// type T, overriding the default kind-based mapping.
func WithCustomEmitter[T any](fn func(reflect.Type) *Schema) GenerateOption {
	return func(o *generateOptions) {
		if o.customEmitters == nil {
			o.customEmitters = make(map[reflect.Type]func(reflect.Type) *Schema)
		}
		o.customEmitters[reflect.TypeFor[T]()] = fn
	}
}

// WithDocReader extracts Go doc comments from src files (passed as an
// [fs.FS]) and uses them as the description annotation for matching struct
// fields. The generator parses the source files lazily on first use; if the
// FS contains no Go sources the option is a no-op.
func WithDocReader(src fs.FS) GenerateOption {
	return func(o *generateOptions) { o.docReaderFS = src }
}
