package jsonschema

import (
	"io"
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

// WithLoader sets the [Loader] used to fetch external $refs and the
// documents named by [CompileURL]. Default: [DefaultLoader], which serves
// only the embedded meta-schemas. For https:// refs, pass the loader from
// the github.com/go-rotini/jsonschema/httploader package.
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

// WithMaxRefDepth limits how many $ref, $dynamicRef and $recursiveRef hops
// validation may nest. Exceeding it records a [ValidationError] for the
// ref keyword with Cause [ErrMaxRefDepth]. Guards against
// pathological recursive schemas. Default: 100.
func WithMaxRefDepth(n int) CompileOption {
	return func(o *compileOptions) {
		o.maxRefDepth = n
		o.maxRefDepthSet = true
	}
}

// WithStrictKeywords causes a keyword unknown to the schema's draft to fail
// compilation with a [*CompileError] wrapping [ErrUnknownKeyword]. Default:
// unknown keywords are ignored.
func WithStrictKeywords(b bool) CompileOption {
	return func(o *compileOptions) { o.strictKeywords = b }
}

// WithStrict is shorthand for [WithStrictKeywords](true).
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

// WithRefCollisionPolicy sets how a duplicate $id, or a second
// [Compiler.AddResource] for the same URI, is handled. Resources are seen in
// order: registered resources first, in registration order, then the
// schema being compiled, depth-first with object members in name order.
// Default: [RefCollisionError].
func WithRefCollisionPolicy(p RefCollisionPolicy) CompileOption {
	return func(o *compileOptions) { o.refCollisionPolicy = p }
}

// WithLoaderTrace writes one line per successful [Loader] fetch to w; failed
// fetches are reported only through the returned error. Useful for
// diagnosing $ref resolution. Default: nil (no trace).
func WithLoaderTrace(w io.Writer) CompileOption {
	return func(o *compileOptions) { o.loaderTrace = w }
}

// Option configures a single validation call. Pass options to the [*Schema]
// Validate methods and the package-level [Validate] function. Options apply
// left to right; a later option overrides an earlier one for the same
// setting.
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
// only). When true, a string that fails its format validator becomes a
// [ValidationError] whose Cause is a [*FormatError]; how unknown format
// names are handled is set by [WithUnknownFormat].
func WithFormatAssertion(b bool) Option {
	return func(o *runOptions) { o.formatAssertion = b }
}

// WithContentAssertion makes contentEncoding, contentMediaType and
// contentSchema assertions (default: annotation only). A string must then
// decode under its contentEncoding; when contentMediaType names a JSON media
// type it must parse as JSON, and the parsed value must validate against
// contentSchema. Unknown encodings and non-JSON media types still pass.
func WithContentAssertion(b bool) Option {
	return func(o *runOptions) { o.contentAssertion = b }
}

// WithStopOnFirstError stops validation once the first error is recorded,
// and leaves [Result.Annotations] empty.
//
// Applicator branches (anyOf, oneOf, if/then/else) still try every
// alternative, because the keyword's outcome depends on which branches
// pass; the short-circuit applies to sibling keywords and to the outer walk.
func WithStopOnFirstError(b bool) Option {
	return func(o *runOptions) { o.stopOnFirstError = b }
}

// WithMaxInstanceSize makes [Schema.Validate] (and the methods built on it)
// reject instance documents larger than n bytes with [ErrInstanceTooLarge]
// before parsing. It does not apply to [Schema.ValidateValue] or the
// JSONC, YAML and TOML adapters. n <= 0 disables the limit. Default: 0.
func WithMaxInstanceSize(n int) Option {
	return func(o *runOptions) { o.maxInstanceSize = n }
}

// WithMaxDocumentSize is an alias for [WithMaxInstanceSize].
func WithMaxDocumentSize(n int) Option {
	return WithMaxInstanceSize(n)
}

// WithMaxValidationDepth limits how deeply subschema evaluation may nest, to
// guard against recursive schemas meeting deeply nested instances. Exceeding
// it records a [*ValidationError] with Keyword "$maxValidationDepth" and
// Cause [ErrMaxValidationDepth]. Default: 1000.
func WithMaxValidationDepth(n int) Option {
	return func(o *runOptions) { o.maxValidationDepth = n }
}

// WithMaxDepth is an alias for [WithMaxValidationDepth].
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

// WithWarningSink installs a writer for warnings emitted during validation:
// one line per unknown format name under the [UnknownFormatWarn] policy,
// deduplicated within a single Validate call. Default: nil (warnings are
// dropped).
func WithWarningSink(w io.Writer) Option {
	return func(o *runOptions) { o.warningSink = w }
}

// WithUnknownFormat controls handling of unrecognized "format" names when
// [WithFormatAssertion] is enabled. Default: [UnknownFormatIgnore].
func WithUnknownFormat(p UnknownFormatPolicy) Option {
	return func(o *runOptions) { o.unknownFormatPolicy = p }
}

// WithCustomFormat registers a format validator for the given format name,
// taking precedence over a built-in validator of the same name. It applies
// only when [WithFormatAssertion] is enabled.
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
// (the value is being produced as output): "required" does not enforce
// properties whose schema is annotated `"writeOnly": true`, because such
// fields should not appear in output documents.
func WithReadOnly(b bool) Option {
	return func(o *runOptions) { o.readOnly = b }
}

// WithWriteOnly enables direction-aware validation in the "write" direction
// (the value is being submitted as input): "required" does not enforce
// properties whose schema is annotated `"readOnly": true`, because such
// fields should not appear in input documents.
func WithWriteOnly(b bool) Option {
	return func(o *runOptions) { o.writeOnly = b }
}

// WithCollectAnnotations controls whether passing keyword annotations are
// reported in [Result.Annotations]. Default: true. [WithStopOnFirstError]
// suppresses them regardless.
func WithCollectAnnotations(b bool) Option {
	return func(o *runOptions) { o.collectAnnotations = b }
}
