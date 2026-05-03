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
// older drafts via the schema's $schema keyword:
//
//	Draft 4       — read-only
//	Draft 6       — read-only
//	Draft 7       — read-only
//	Draft 2019-09 — read-only
//	Draft 2020-12 — read + write (default for schema generation)
//
// A schema's effective draft is determined by (in order): the $schema keyword
// at the schema root; the value passed via [WithDefaultDraft]; Draft 2020-12.
//
// # Compatibility With encoding/json
//
// The standard library has no schema layer, so there is no "drop-in" surface
// to mirror. The two libraries integrate at the boundary instead:
// [Schema.ValidateAndUnmarshal] runs validation and then decodes the validated
// bytes via [encoding/json.Unmarshal] in a single call, so a caller can move
// from "is this JSON valid against my schema?" to "decode it into my struct"
// without two-pass parsing. [Schema.MarshalJSON] returns canonical schema
// bytes that round-trip through [encoding/json.Unmarshal] + [CompileValue].
//
// # Type Mapping (Schema Generation)
//
// [Generate] walks a Go value (or [reflect.Type] via [FromType]) and emits a
// JSON Schema describing it:
//
//	Go bool                                  → {"type": "boolean"}
//	Go intN/uintN                            → {"type": "integer"}
//	Go float32/float64                       → {"type": "number"}
//	Go string                                → {"type": "string"}
//	Go []byte                                → {"type": "string", "contentEncoding": "base64"}
//	Go time.Time                             → {"type": "string", "format": "date-time"}
//	Go time.Duration                         → {"type": "string", "format": "duration"}
//	Go *T                                    → schema for T (nullability via "type": ["T", "null"] when applicable)
//	Go []T / [N]T                            → {"type": "array", "items": <schema for T>}
//	Go map[string]V                          → {"type": "object", "additionalProperties": <schema for V>}
//	Go struct                                → {"type": "object", "properties": {...}, "required": [...]}
//	Go interface{}                           → {} (any)
//	Go json.Number                           → {"type": ["number", "string"]}
//	Go json.RawMessage                       → {} (any)
//
// Self-referential and mutually recursive types are emitted via $defs entries
// with internal $ref links to break cycles.
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
// from [Draft 2020-12 §12]: Flag, Basic, Detailed, and Verbose. The wire
// shape matches the spec's output meta-schema and is suitable for sending
// over the network or feeding into downstream tools.
//
// # Schema Generation From Go Types
//
// [Generate], [GenerateBytes], and [FromType] produce a [*Schema] (or its
// JSON byte form) from a Go type. The generator honors the "json" and
// "jsonschema" tags described above and supports recursive types via $defs.
// The output is always Draft 2020-12.
//
// # Multi-Format Input
//
// In addition to JSON, the package can load schemas and instances written
// as JSONC, YAML, or TOML via the [LoadJSONC], [LoadYAML], [LoadTOML],
// [ValidateJSONC], [ValidateYAML], and [ValidateTOML] entry points. These
// adapters delegate to [go-rotini/jsonc], [go-rotini/yaml], and
// [go-rotini/toml]; numeric literals are preserved via [encoding/json.Number]
// so number-precision keywords (multipleOf, minimum, maximum, const)
// evaluate against the original wire form.
//
// # Error Handling
//
// Compile errors are returned as [*CompileError] and support [errors.Is] /
// [errors.As]. Validation errors are accumulated on [Result.Errors] as
// [ValidationError] values; the underlying type also implements the error
// interface for compatibility with single-error consumers. [RenderError]
// produces a human-readable error string with a source pointer.
//
// [JSON Schema]: https://json-schema.org/
// [Draft 2020-12]: https://json-schema.org/draft/2020-12/schema
// [Draft 2020-12 §12]: https://json-schema.org/draft/2020-12/json-schema-core#section-12
// [the README]: https://github.com/go-rotini/jsonschema#struct-tags
// [go-rotini/jsonc]: https://github.com/go-rotini/jsonc
// [go-rotini/yaml]: https://github.com/go-rotini/yaml
// [go-rotini/toml]: https://github.com/go-rotini/toml
package jsonschema
