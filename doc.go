// Package jsonschema implements [JSON Schema] validation, schema generation
// from Go types, and multi-format input via the rotini package family.
//
// The package follows the conventions of [encoding/json]: a small set of
// package-level entry points ([Compile], [Validate], [Generate]) for one-shot
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
// older drafts via the schema's $schema keyword. Conformance baseline against
// the official JSON Schema Test Suite at v0.1.0:
//
//	Draft        Mode               Test-suite pass rate
//	Draft 4      read-only          608 / 616  (98.7%)
//	Draft 6      read-only          829 / 835  (99.3%)
//	Draft 7      read-only          917 / 923  (99.3%)
//	Draft 2019-09  read-only        1239 / 1251 (99.0%)
//	Draft 2020-12  read + write     1274 / 1291 (98.7%)  ← primary target
//
// "Read-only" means the package compiles and validates schemas authored
// against that draft; the schema generator ([Generate], [GenerateBytes],
// [FromType]) always emits Draft 2020-12 output. A schema's effective draft
// is determined by (in order): the $schema keyword at the schema root; the
// value passed via [WithDefaultDraft]; [DraftDefault].
//
// # Type Mapping (Schema Generation)
//
// [Generate] walks a Go value (or [reflect.Type] via [FromType]) and emits a
// JSON Schema describing it.
//
//	Go kind                          Generated schema
//	bool                             {"type": "boolean"}
//	intN, uintN                      {"type": "integer"} (with min/max on width)
//	float32, float64                 {"type": "number"}
//	string                           {"type": "string"}
//	[]byte, [N]byte                  {"type": "string", "contentEncoding": "base64"}
//	[]T, [N]T                        {"type": "array", "items": <T>}
//	map[string]V                     {"type": "object", "additionalProperties": <V>}
//	struct                           {"type": "object", "properties": {...}, "required": [...]}
//	time.Time                        {"type": "string", "format": "date-time"}
//	time.Duration                    {"type": "integer"} or "format": "duration" via [WithGenerateDurationAsString]
//	*T                               <T> by default; anyOf:[null, T] under [WithGenerateNullablePointers]
//	interface{} / any                {} by default; configurable via [WithGenerateInterfaceAsAny]
//	json.Number                      {"type": ["number", "string"]}
//	json.RawMessage                  {} (any)
//	encoding.TextMarshaler           {"type": "string"}
//	chan / func / unsafe.Pointer     generation error
//
// Self-referential and mutually recursive types are emitted via $defs entries
// with internal $ref links. Custom emitters (registered via
// [WithCustomEmitter]) override the default kind-based mapping for a
// specific Go type.
//
// # Struct Tags
//
// Two tags drive schema generation:
//
//   - "json" — standard library semantics: property name, "omitempty"
//     (excluded from "required"), and "-" (skipped).
//   - "jsonschema" — schema-specific options.
//
// Example:
//
//	type User struct {
//	    Name  string `json:"name" jsonschema:"required,minLength=1,maxLength=100"`
//	    Email string `json:"email" jsonschema:"required,format=email"`
//	    Age   int    `json:"age,omitempty" jsonschema:"minimum=0,maximum=150"`
//	    Roles []string `json:"roles" jsonschema:"minItems=1,uniqueItems,enum=admin|editor|viewer"`
//	}
//
// Options are comma-separated; multi-value options use pipe (`enum=a|b|c`).
// See [the README] for the complete option vocabulary.
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
// from [Draft 2020-12 §12]: [OutputFlag], [OutputBasic], [OutputDetailed],
// and [OutputVerbose]. The wire shape matches the spec's output meta-schema
// (also embedded and exposed via [OutputMetaSchema]) and is suitable for
// sending over the network or feeding into downstream tools.
//
// # Error Handling
//
// The package surfaces five typed errors, each carrying structured fields
// and supporting [errors.Is] / [errors.As]:
//
//   - [*CompileError] — schema-document problems (malformed JSON, bad
//     keyword shape, unknown vocabulary, ref resolution failure).
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
// via [errors.Is]. Specific failure conditions are surfaced via the
// package-level sentinels in [errors.go]: [ErrUnknownDraft],
// [ErrUnknownKeyword], [ErrUnknownFormat], [ErrRefCycle], [ErrMaxRefDepth],
// [ErrMaxValidationDepth], [ErrInstanceTooLarge], [ErrLoaderRejected],
// [ErrSchemaNotCompiled], [ErrValidationFailed], [ErrNilReader],
// [ErrUnsupportedSchemaShape]. Multi-format adapters add [ErrInvalidYAML]
// and [ErrInvalidTOML].
//
// [RenderError] produces a human-readable error string with the (forward-
// looking) signature for source-line-pointer formatting.
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
//   - [Schema.MarshalJSON] returns canonical schema bytes that round-trip
//     through [encoding/json.Unmarshal] + [CompileValue].
//   - [Schema.ValidateAndUnmarshal] runs validation and then decodes the
//     validated bytes via [encoding/json.Unmarshal] in a single call, so a
//     caller can move from "is this JSON valid against my schema?" to
//     "decode it into my struct" without two-pass parsing.
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
//     and emit Go type declarations via [GenerateGo], suitable for a
//     //go:generate directive.
//   - bowtie — a standards-conformance connector for the [Bowtie]
//     cross-implementation JSON Schema test harness, speaking Bowtie's
//     stdin/stdout protocol.
//
// # DoS Protection
//
// The package ships with four independent guards against adversarial input:
// [WithMaxRefDepth] (default 100) caps $ref hop depth per keyword;
// [WithMaxValidationDepth] (alias [WithMaxDepth], default 1000) caps recursion
// into nested instances; [WithMaxInstanceSize] (alias [WithMaxDocumentSize])
// caps instance bytes before parsing; the compiler detects ref cycles at
// compile time and turns them into lazy edges so they cannot stack-overflow
// the validator.
//
// [JSON Schema]: https://json-schema.org/
// [Draft 2020-12]: https://json-schema.org/draft/2020-12/schema
// [Draft 2020-12 §12]: https://json-schema.org/draft/2020-12/json-schema-core#section-12
// [the README]: https://github.com/go-rotini/jsonschema#struct-tags
// [go-rotini/jsonc]: https://github.com/go-rotini/jsonc
// [go-rotini/yaml]: https://github.com/go-rotini/yaml
// [go-rotini/toml]: https://github.com/go-rotini/toml
// [Bowtie]: https://github.com/bowtie-json-schema/bowtie
package jsonschema
