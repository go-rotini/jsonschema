// Package generate produces a JSON Schema from a Go type, for
// [github.com/go-rotini/jsonschema].
//
//	schema, err := generate.Generate((*User)(nil))
//	data, err := generate.Bytes(User{}, generate.WithAdditionalPropertiesFalse(true))
//
// The result of [Generate] and [FromType] is a compiled
// [*jsonschema.Schema], ready to validate; [Bytes] returns the JSON form.
// Output is Draft 2020-12 unless [WithDraft] says otherwise.
//
// # Type Mapping
//
// [Generate] walks a Go value (or [reflect.Type] via [FromType]) and emits a
// JSON Schema describing it.
//
//	Go kind                          Generated schema
//	bool                             {"type": "boolean"}
//	intN, uintN                      {"type": "integer"} (with min/max on width)
//	float32, float64                 {"type": "number"}
//	string                           {"type": "string"}
//	[]byte                           {"type": ["string", "null"], "contentEncoding": "base64"}
//	[N]byte                          {"type": "string", "contentEncoding": "base64"}
//	[]T                              {"type": ["array", "null"], "items": <T>}
//	[N]T                             {"type": "array", "items": <T>}
//	map[string]V                     {"type": ["object", "null"], "additionalProperties": <V>}
//	map[int]V, map[uint]V            as map[string]V, with "propertyNames" a decimal pattern
//	map[K]V, K a TextMarshaler       as map[string]V
//	struct                           {"type": "object", "properties": {...}, "required": [...]}
//	time.Time                        {"type": "string", "format": "date-time"}
//	time.Duration                    {"type": "integer"} or "format": "duration" via [WithDurationAsString]
//	*T                               anyOf:[null, <T>]; see below and [WithNullablePointers]
//	interface{} / any                {} by default; configurable via [WithInterfaceAsAny]
//	json.Number                      {"type": ["number", "string"]}
//	json.RawMessage                  {} (any)
//	*big.Int, *big.Float             {"type": "string"}
//	encoding.TextMarshaler           {"type": "string"} (non-struct types)
//	json.Marshaler                   {} (non-struct types)
//	chan / func / unsafe.Pointer     generation error
//
// A nil slice, map or pointer marshals to null, so those types also allow
// null, except in a struct field tagged omitempty or omitzero, which leaves
// a nil value out. The root type is not made nullable: Generate(&T{})
// describes a T. A jsonschema tag's assertions on a pointer field apply to
// the pointed-to value, so null still passes them.
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
//     (excluded from "required"), "-" (skipped), and "string" (a string
//     holding the value's JSON form). Fields of embedded structs are
//     promoted, and name conflicts are settled, as encoding/json does.
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
// [the README]: https://github.com/go-rotini/jsonschema#schema-generation-from-go-types
package generate
