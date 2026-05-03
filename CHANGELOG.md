# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## 0.1.0

### Added
- JSON Schema compiler and validator engine for Drafts 4, 6, 7, 2019-09, and 2020-12 (primary target: 2020-12)
- Compile/validate split via `Compile` / `MustCompile` / `CompileValue` / `CompileURL` and `*Schema.Validate` / `ValidateValue` / `ValidateReader` / `ValidateAndUnmarshal`
- Reusable `Compiler` with remote-schema cache; `NewCompiler`, `AddResource`
- Generic `ValidateTo[T]` typed-decode in one call
- Reference resolution: `$ref`, `$dynamicRef`, `$recursiveRef`, plain-name anchors (`$anchor`, `$dynamicAnchor`), URN base URIs, JSON Pointer fragments, location-independent identifiers
- Pluggable `Loader` interface with `MapLoader`, `FileLoader` (sandboxed), `HTTPLoader` (HTTPS-only by default; opt-in HTTP, single-flight, configurable cache TTL and request decorator), `EmbedLoader`, `ChainLoader`; `DefaultLoader()` ships embedded meta-schemas
- All four output formats from Draft 2020-12 §12 (Flag, Basic, Detailed, Verbose) via `Result.Output`; output meta-schema embedded and exposed via `OutputMetaSchema`
- 19 built-in format validators (`date-time`, `date`, `time`, `duration`, `email`, `idn-email`, `hostname`, `idn-hostname`, `ipv4`, `ipv6`, `uri`, `uri-reference`, `iri`, `iri-reference`, `uri-template`, `json-pointer`, `relative-json-pointer`, `uuid`, `regex`)
- Content vocabulary (`contentEncoding`, `contentMediaType`, `contentSchema`) — annotation-only by default; assertion mode via `WithContentAssertion`
- Custom format registration via `WithCustomFormat`; format assertion via `WithFormatAssertion`
- Custom keyword / vocabulary registration via `WithVocabulary`
- Pluggable regex engine via `WithRegexEngine` (default: Go's `regexp` in Perl mode)
- Schema generation from Go types via reflection: `Generate`, `GenerateBytes`, `FromType`, `MustGenerate`, and reusable `Generator` via `NewGenerator`
- `json` and `jsonschema` struct tags for generation; `WithCustomEmitter[T]` for type-specific overrides; `WithDocReader` for description extraction from Go source
- Generator options: `WithGenerateDraft`, `WithGenerateID`, `WithGenerateExpandedRefs`, `WithGenerateOmitDescriptions`, `WithGenerateDurationAsString`, `WithGenerateNullablePointers`, `WithGenerateOrderedProperties`, `WithGenerateAdditionalPropertiesFalse`, `WithGenerateSchemaDeclaration`, `WithGenerateInterfaceAsAny`
- Multi-format input via `LoadJSONC` / `LoadYAML` / `LoadTOML` and `ValidateJSONC` / `ValidateYAML` / `ValidateTOML`; numeric precision preserved through `json.Number`
- `Schema.MarshalJSON` for round-trip through `encoding/json.Unmarshal` + `CompileValue`
- OpenAPI 3.1 dialect: `VocabOAS` vocabulary and `OASDialectURL` meta-schema embedded and recognized; `WithMetaSchemaValidation` validates against the dialect when in scope
- Bowtie connector at `bowtie/` (`package main`) for cross-implementation conformance testing
- Typed errors with structured fields and `errors.Is` / `errors.As` support: `*CompileError`, `*ValidationError`, `*RefError`, `*LoaderError`, `*FormatError`; pointer-typed sentinels `ErrCompile` / `ErrValidation` / `ErrRef` / `ErrLoader` / `ErrFormat`; package-level sentinels for specific failure modes
- `RenderError` for human-readable error output (signature ready for source-line pointer formatting)
- Strict modes: `WithMetaSchemaValidation` (compile-time meta-schema check), `WithStrictKeywords` (unknown keyword → `*CompileError`)
- Validation options: `WithStopOnFirstError`, `WithMaxInstanceSize`, `WithMaxValidationDepth`, `WithMaxErrors`, `WithReadOnly`, `WithWriteOnly`, `WithCollectAnnotations`, `WithUnknownFormat`
- DoS protection: max ref depth (default 100), max validation depth (default 1000), max instance size, ref-cycle detection at compile time
- Concurrency: `*Schema` is immutable and safe for concurrent validation; `HTTPLoader` shares network round-trips via inline single-flight
- Conformance: 1274/1291 (98.7%) on Draft 2020-12, 1239/1251 (99.0%) on 2019-09, 917/923 (99.3%) on Draft 7, 829/835 (99.3%) on Draft 6, 608/616 (98.7%) on Draft 4
- 326 tests across the parent package and the Bowtie connector
- Fuzz corpus and targets: `FuzzCompile`, `FuzzValidate`, `FuzzGenerate`
- Acceptance fixtures: openapi-3.1, asyncapi-2.6, json-patch, geojson, avro, kitchen-sink
