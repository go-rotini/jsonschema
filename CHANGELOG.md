# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

#### Compile / validate
- JSON Schema compiler and validator engine for Drafts 4, 6, 7, 2019-09, and 2020-12 (primary target: 2020-12)
- Top-level entry points `Compile` / `MustCompile` / `CompileValue` / `MustCompileValue` / `CompileURL` / `MustCompileURL` / `Validate`
- `*Schema` methods `Validate` / `ValidateValue` / `ValidateReader` / `ValidateAndUnmarshal`
- Reusable `Compiler` with remote-schema cache and per-URI single-flight (`NewCompiler`, `AddResource`, `Compile` / `MustCompile` / `CompileURL` / `MustCompileURL` / `CompileValue` / `MustCompileValue`)
- Generic typed-decode in one call: `ValidateTo[T]`, `MustValidateTo[T]`
- `*Schema` accessors: `Draft`, `ID`, `MetaSchemaURI`, `MarshalJSON`, `String`, `Resources`, `Anchors`, `Vocabularies`, `Bindings` (with exported `KeywordBinding` projection)

#### Reference resolution
- `$ref`, `$dynamicRef`, `$recursiveRef`, plain-name anchors (`$anchor`, `$dynamicAnchor`), URN base URIs, JSON Pointer fragments, location-independent identifiers
- Cycle detection at compile time via lazy edges; runtime ref resolution is concurrent-safe (per-call build frames + inFlight cache so concurrent `Validate` against a shared `*Schema` is correct under lazy resolution)

#### Loaders
- `Loader` interface with `MapLoader`, `FileLoader` (sandboxed; rejects path-escape and symlink-escape via `filepath.EvalSymlinks` + prefix re-check), `HTTPLoader`, `EmbedLoader`, `ChainLoader`
- `HTTPLoader` is HTTPS-only by default; opt-in HTTP via `AllowHTTP`; in-flight single-flight per URI; configurable cache TTL, max body size, request decorator, redirect cap (5), and cross-host header scrubbing on redirect (so `RequestDecorator`-set tokens never leak)
- Tightened HTTP transport: `MaxIdleConns: 100`, `MaxIdleConnsPerHost: 10`, `IdleConnTimeout: 90s`, `TLSHandshakeTimeout: 10s`, `ExpectContinueTimeout: 1s`
- `DefaultLoader()` ships every standard meta-schema embedded via `//go:embed` for offline operation

#### Output formats
- All four output formats from Draft 2020-12 §12 (Flag, Basic, Detailed, Verbose) via `Result.Output`
- Output meta-schema embedded and exposed via `OutputMetaSchema`

#### Format and content vocabularies
- 19 built-in format validators (`date-time`, `date`, `time`, `duration`, `email`, `idn-email`, `hostname`, `idn-hostname`, `ipv4`, `ipv6`, `uri`, `uri-reference`, `iri`, `iri-reference`, `uri-template`, `json-pointer`, `relative-json-pointer`, `uuid`, `regex`)
- Content vocabulary (`contentEncoding`, `contentMediaType`, `contentSchema`) — annotation-only by default; assertion mode via `WithContentAssertion`
- Custom format registration via `WithCustomFormat`; format assertion via `WithFormatAssertion`; unknown-format policy via `WithUnknownFormat`

#### Schema generation from Go types
- Reflection-based generation: `Generate`, `GenerateBytes`, `FromType`, `MustGenerate`, and reusable `Generator` via `NewGenerator`
- `json` and `jsonschema` struct tags drive output; `WithCustomEmitter[T]` for type-specific overrides; `WithDocReader` to extract Go-source comments as `description`
- Generator options: `WithGenerateDraft`, `WithGenerateID`, `WithGenerateExpandedRefs`, `WithGenerateOmitDescriptions`, `WithGenerateDurationAsString`, `WithGenerateNullablePointers`, `WithGenerateOrderedProperties`, `WithGenerateAdditionalPropertiesFalse`, `WithGenerateSchemaDeclaration`, `WithGenerateInterfaceAsAny`

#### Multi-format input
- JSON: `LoadJSON` / `LoadJSONURL` / `LoadJSONValue` / `MustLoadJSON` / `MustLoadJSONURL` / `MustLoadJSONValue` (aliases of `Compile*` for naming symmetry)
- JSONC / YAML / TOML: `Load{Format}` / `Load{Format}URL` / `Load{Format}Value` / `MustLoad{Format}*` and `Validate{Format}` for each format
- Numeric precision preserved through `json.Number` so `multipleOf` / `minimum` / `maximum` evaluate correctly across all four input formats

#### OpenAPI 3.1 dialect
- `VocabOAS` vocabulary and `OASDialectURL` recognized; OAS dialect meta-schema embedded
- `WithMetaSchemaValidation` validates schemas declaring the OAS dialect URL against the embedded dialect meta-schema

#### Bowtie connector
- `bowtie/` (`package main`) speaks the [Bowtie](https://bowtie.report) stdin/stdout protocol so this implementation can be benchmarked alongside other JSON Schema implementations

#### Errors
- Typed errors with structured fields and full `errors.Is` / `errors.As` support: `*CompileError`, `*ValidationError`, `*RefError`, `*LoaderError`, `*FormatError`
- Pointer-typed sentinels `ErrCompile` / `ErrValidation` / `ErrRef` / `ErrLoader` / `ErrFormat`
- Package-level sentinels for specific failure modes: `ErrSchemaNotCompiled`, `ErrInstanceTooLarge`, `ErrMaxRefDepth`, `ErrMaxValidationDepth`, `ErrMaxKeyCount`, `ErrUnknownFormat`, `ErrLoaderRejected`, `ErrInvalidYAML`, `ErrInvalidTOML`, `ErrUnsupportedSchemaShape`, `ErrRefCycle`, `ErrValidationFailed`, `ErrNilReader`
- `RenderError(schemaSrc, instanceSrc, err, color...)` produces human-readable error output with source-line context, JSON-pointer-anchored caret pointers, and optional ANSI color
- `ValidationError` carries a `Cause` field surfaced via `Unwrap` so format failures, depth-limit failures, and other typed causes flow through `errors.As`

#### Validation options
- `WithMetaSchemaValidation` (compile-time meta-schema check)
- `WithStrictKeywords(b bool)` plus zero-arg `WithStrict()` alias
- `WithStopOnFirstError`, `WithMaxErrors`, `WithCollectAnnotations`, `WithUnknownFormat`
- `WithReadOnly` / `WithWriteOnly` — direction-aware filters that skip required-field checks when the schema annotates a field as the opposite direction
- `WithWarningSink(io.Writer)` — diagnostic sink for unknown-format warnings under `UnknownFormatWarn`
- `WithFormatAssertion`, `WithContentAssertion`, `WithCustomFormat`

#### Compile options
- `WithDefaultDraft`, `WithLoader`, `WithBaseURI`, `WithMaxRefDepth`, `WithRefCollisionPolicy(RefCollisionError)`, `WithLoaderTrace(io.Writer)`

#### DoS protection
- Max ref depth (default 100) — `WithMaxRefDepth`
- Max validation depth (default 1000) — `WithMaxValidationDepth` (alias `WithMaxDepth` for sister-package parity)
- Max instance size — `WithMaxInstanceSize` (alias `WithMaxDocumentSize` for sister-package parity)
- Max object key count — `WithMaxKeyCount`
- Compile-time ref-cycle detection (lazy edges)
- HTTPLoader response-size cap; redirect cap; cross-host header scrubbing

#### Concurrency
- `*Schema` is immutable post-Compile and safe for concurrent `Validate` from any number of goroutines, including against schemas with runtime-resolved refs
- `Compiler` is safe for concurrent use; `CompileURL` shares one fetch+compile pipeline per URI via inline single-flight
- `HTTPLoader` shares network round-trips via single-flight
