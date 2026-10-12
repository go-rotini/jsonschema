// Package jsonschema implements [JSON Schema] validation and multi-format
// input via the rotini package family.
//
// The package follows the conventions of [encoding/json]: a small set of
// package-level entry points ([Compile], [Validate]) for one-shot
// use, and a compiled [*Schema] value for amortizing compilation across many
// validations. Functional options shape compile-time and run-time behavior.
//
//	schema, err := jsonschema.Compile(schemaJSON)
//	if err != nil { return err }
//	result, err := schema.Validate(instanceJSON)
//	if err != nil { return err }
//	if !result.Valid { /* inspect result.Errors */ }
//
// # Draft Support
//
// The package targets [Draft 2020-12] as its primary draft and supports
// older drafts via the schema's $schema keyword. Conformance against the
// official JSON Schema Test Suite (main branch, optional tests excluded):
//
//	Draft        Mode               Test-suite pass rate
//	Draft 4      read-only          610 / 618  (98.7%)
//	Draft 6      read-only          835 / 841  (99.3%)
//	Draft 7      read-only          923 / 929  (99.4%)
//	Draft 2019-09  read-only        1249 / 1261 (99.0%)
//	Draft 2020-12  read + write     1284 / 1301 (98.7%)  ← primary target
//
// "Read-only" means the package compiles and validates schemas authored
// against that draft; the schema generator in the generate subpackage
// emits Draft 2020-12 output by default. A schema's effective draft
// is determined by (in order): the $schema keyword at the schema root; the
// value passed via [WithDefaultDraft]; [DraftDefault].
//
// # Subpackages
//
// The root package validates; tooling and network access live in
// subpackages, so a program that only validates links neither the Go
// toolchain packages nor net/http:
//
//   - [github.com/go-rotini/jsonschema/generate] — a JSON Schema from a Go
//     type, driven by `json` and `jsonschema` struct tags.
//   - [github.com/go-rotini/jsonschema/gogen] — Go type declarations from a
//     JSON Schema.
//   - [github.com/go-rotini/jsonschema/httploader] — a [Loader] that fetches
//     remote $refs over HTTPS.
//
// # Loading Referenced Schemas
//
// A $ref outside the document is resolved through the compiler's [Loader].
// The default, [DefaultLoader], serves only the embedded standard
// meta-schemas ([MetaLoader]); any other URI is rejected with
// [ErrLoaderRejected]. Register schemas up front with [Compiler.AddResource]
// or a [MapLoader], read them from disk with [FileLoader] or from an
// [embed.FS] with [EmbedLoader], or opt into the network:
//
//	schema, err := jsonschema.Compile(src, jsonschema.WithLoader(httploader.Default()))
//
// # Strict Modes
//
// Two independent strictness modes are supported:
//
//   - [WithMetaSchemaValidation] — at compile time, validate each schema
//     against its declared $schema meta-schema. Catches typos and malformed
//     keyword values. Default: off (recommended for CI).
//   - [WithFormatAssertion] — at validation time, treat the "format" keyword
//     as an assertion rather than an annotation. Default: annotation-only,
//     matching the Draft 2020-12 specification's default vocabulary set.
//
// # Output Formats
//
// [Result.Output] renders a validation result in any of the four formats
// defined in [JSON Schema Core 2020-12 §12]: [OutputFlag], [OutputBasic],
// [OutputDetailed], and [OutputVerbose]. The wire shape matches the output
// meta-schema
// (also embedded and exposed via [OutputMetaSchema]) and is suitable for
// sending over the network or feeding into downstream tools.
//
// # Error Handling
//
// The package surfaces five typed errors, each carrying structured fields
// and supporting [errors.Is] / [errors.As]:
//
//   - [*CompileError] — schema-document problems (malformed JSON, bad
//     keyword shape, duplicate $id, meta-schema violations).
//   - [*ValidationError] — assertion failure during validation. Multi-error
//     unwrap (Go 1.20+) walks nested causes from compound applicators
//     (anyOf, oneOf, allOf, $ref, ...). Switch on [ValidationError.Keyword]
//     for stable error classification.
//   - [*RefError] — $ref or $dynamicRef cannot be resolved.
//   - [*LoaderError] — a [Loader] returned an I/O / network error.
//   - [*FormatError] — a value with a "format" keyword failed its validator
//     while format assertion is enabled.
//
// Pointer-typed sentinels ([ErrCompile], [ErrValidation], [ErrRef],
// [ErrLoader], [ErrFormat]) match instances of their concrete error type
// via [errors.Is]. Specific failure conditions have their own sentinels,
// such as [ErrLoaderRejected], [ErrInstanceTooLarge], [ErrUnknownKeyword]
// and [ErrSchemaNotCompiled]; the multi-format adapters add
// [ErrInvalidYAML] and [ErrInvalidTOML].
//
// [RenderError] formats an error for people, with a snippet of the
// offending schema or instance line when the source bytes are supplied.
//
// # Multi-Format Input
//
// In addition to JSON, the package can load schemas and instances written
// as JSONC, YAML, or TOML via the [LoadJSONC], [LoadYAML], [LoadTOML],
// [ValidateJSONC], [ValidateYAML], and [ValidateTOML] entry points. These
// adapters delegate to [go-rotini/jsonc], [go-rotini/yaml], and
// [go-rotini/toml]; numeric literals are preserved via [encoding/json.Number]
// so number-precision keywords (multipleOf, minimum, maximum, const)
// evaluate against the original wire form. Multi-format support is part of
// the main package — there is no dedicated sub-module to import.
//
// # Compatibility With encoding/json
//
// The standard library has no schema layer, so there is no "drop-in" surface
// to mirror. The two libraries integrate at the boundary instead:
//
//   - [Schema.MarshalJSON] returns the schema's source bytes, so a [*Schema]
//     embeds in [encoding/json.Marshal] output and round-trips through
//     [encoding/json.Unmarshal] + [CompileValue].
//   - [Schema.ValidateAndUnmarshal] validates and then decodes the validated
//     bytes via [encoding/json.Unmarshal] in a single call.
//   - [ValidateTo] is the generic counterpart to [Schema.ValidateAndUnmarshal]
//     and returns the decoded value of type T directly.
//
// # OpenAPI 3.1
//
// The [VocabOAS] vocabulary and the [OASDialectURL] meta-schema are
// registered unconditionally so OpenAPI 3.1 schemas compile cleanly out of
// the box. A schema declaring [OASDialectURL] as $schema runs against
// Draft 2020-12 plus the OAS-specific annotation keywords (discriminator,
// xml, externalDocs, example).
//
// # Command-Line Tool
//
// A companion command lives in the cmd/jsonschema sub-package: a single
// `package main` binary (run via `go tool jsonschema` or `go install`) that
// dispatches on a subcommand:
//
//   - generate — read a JSON Schema (from a file argument or standard input)
//     and emit Go type declarations via the gogen package, suitable for a
//     //go:generate directive.
//   - bowtie — a standards-conformance connector for the [Bowtie]
//     cross-implementation JSON Schema test harness, speaking Bowtie's
//     stdin/stdout protocol.
//
// # DoS Protection
//
// The package guards against adversarial input in several independent ways:
//
//   - [WithMaxRefDepth] (default 100) caps nested $ref, $dynamicRef and
//     $recursiveRef hops during validation.
//   - [WithMaxValidationDepth] (alias [WithMaxDepth], default 1000) caps how
//     deeply subschema evaluation nests.
//   - [WithMaxInstanceSize] (alias [WithMaxDocumentSize]) caps instance
//     bytes before parsing.
//   - [WithMaxKeyCount] caps the number of keys visited on one object.
//   - The compiler turns $ref cycles into lazy edges that are followed at
//     validation time, bounded by [WithMaxRefDepth], so they cannot
//     overflow the stack.
//
// [JSON Schema]: https://json-schema.org/
// [Draft 2020-12]: https://json-schema.org/draft/2020-12/schema
// [JSON Schema Core 2020-12 §12]: https://json-schema.org/draft/2020-12/json-schema-core#section-12
// [go-rotini/jsonc]: https://github.com/go-rotini/jsonc
// [go-rotini/yaml]: https://github.com/go-rotini/yaml
// [go-rotini/toml]: https://github.com/go-rotini/toml
// [Bowtie]: https://github.com/bowtie-json-schema/bowtie
package jsonschema
