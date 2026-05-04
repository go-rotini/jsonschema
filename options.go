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
	metaSchemaValidation bool
	refCollisionPolicy   RefCollisionPolicy
	loaderTrace          io.Writer
	defaultDraftSet      bool
	loaderSet            bool
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

// WithStrict is a zero-argument shorthand for [WithStrictKeywords](true).
// Provided for consistency with the rotini sister packages.
func WithStrict() CompileOption {
	return WithStrictKeywords(true)
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
//
// In v0.1, only [RefCollisionError] is wired — other policy values are
// reserved for v0.2 and currently behave the same as [RefCollisionError].
func WithRefCollisionPolicy(p RefCollisionPolicy) CompileOption {
	return func(o *compileOptions) { o.refCollisionPolicy = p }
}

// WithLoaderTrace writes one line per [Loader] fetch to w. Useful for
// diagnosing $ref resolution. Default: nil (no trace).
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
	maxKeyCount         int
	unknownFormatPolicy UnknownFormatPolicy
	customFormats       map[string]func(string) error
	maxErrors           int
	readOnly            bool
	writeOnly           bool
	collectAnnotations  bool
	warningSink         io.Writer
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
//
// Note: applicator branches (anyOf, oneOf, if/then/else) always evaluate
// every alternative regardless of this option, because the keyword's
// outcome depends on knowing which branches passed. Short-circuiting
// applies only at the top-level evaluator and to sibling keywords within
// a single subschema.
func WithStopOnFirstError(b bool) Option {
	return func(o *runOptions) { o.stopOnFirstError = b }
}

// WithMaxInstanceSize rejects instance documents larger than n bytes before
// parsing. n <= 0 disables the limit. Default: 0 (no limit). Returns
// [ErrInstanceTooLarge] when an instance exceeds the cap.
func WithMaxInstanceSize(n int) Option {
	return func(o *runOptions) { o.maxInstanceSize = n }
}

// WithMaxDocumentSize is a sister-package alias for [WithMaxInstanceSize].
// Both options write to the same underlying field; later calls win.
func WithMaxDocumentSize(n int) Option {
	return WithMaxInstanceSize(n)
}

// WithMaxValidationDepth limits recursion into nested objects/arrays to
// guard against unbounded recursive schemas with adversarial instances.
// Default: 1000.
func WithMaxValidationDepth(n int) Option {
	return func(o *runOptions) { o.maxValidationDepth = n }
}

// WithMaxDepth is a sister-package alias for [WithMaxValidationDepth].
func WithMaxDepth(n int) Option {
	return WithMaxValidationDepth(n)
}

// WithMaxKeyCount caps the number of object keys the validator will visit
// on any single object instance. When an object has more keys than n,
// validation surfaces a [*ValidationError] with Keyword "$maxKeyCount" and
// Cause [ErrMaxKeyCount]. Default: 0 (unlimited).
//
// Mitigates DoS via instances with millions of object keys.
// [WithMaxInstanceSize] transitively bounds the same attack surface;
// WithMaxKeyCount is a finer-grain guard for cases where the instance
// bytes are not the limiting factor (e.g. repeated short keys).
func WithMaxKeyCount(n int) Option {
	return func(o *runOptions) { o.maxKeyCount = n }
}

// WithWarningSink installs a writer that receives diagnostic messages
// emitted during validation. v0.1 emits one line per unknown format under
// the [UnknownFormatWarn] policy, deduplicated within a single Validate
// call. Future warning-class diagnostics will write to the same sink.
// Default: nil (warnings dropped silently).
func WithWarningSink(w io.Writer) Option {
	return func(o *runOptions) { o.warningSink = w }
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

// WithReadOnly enables direction-aware validation in the "read" direction
// (the value is being produced as output). Required properties whose schema
// is annotated `"writeOnly": true` are not enforced because such fields
// should not appear in output documents.
func WithReadOnly(b bool) Option {
	return func(o *runOptions) { o.readOnly = b }
}

// WithWriteOnly enables direction-aware validation in the "write" direction
// (the value is being submitted as input). Required properties whose schema
// is annotated `"readOnly": true` are not enforced because such fields
// should not appear in input documents.
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
func WithGenerateDurationAsString(b bool) GenerateOption {
	return func(o *generateOptions) { o.durationAsString = b }
}

// WithGenerateNullablePointers emits *T as anyOf:[null, T]. Default: false.
func WithGenerateNullablePointers(b bool) GenerateOption {
	return func(o *generateOptions) { o.nullablePointers = b }
}

// WithGenerateOrderedProperties controls whether emitted struct schemas
// preserve Go field declaration order. When true (default), `properties`
// keys are emitted in declaration order. When false, the generator emits a
// plain map[string]any whose key order is unspecified.
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
