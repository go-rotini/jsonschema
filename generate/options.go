package generate

import (
	"io/fs"
	"reflect"

	"github.com/go-rotini/jsonschema"
)

// Option configures the schema generator. Pass options to [Generate],
// [Bytes], [FromType], or [New], which returns a reusable [*Generator].
type Option func(*options)

// options carries the generator's configuration.
type options struct {
	draft                     jsonschema.Draft
	id                        string
	expandedRefs              bool
	omitDescriptions          bool
	durationAsString          bool
	nullablePointers          bool
	nullablePointersSet       bool
	orderedProperties         bool
	additionalPropertiesFalse bool
	emitSchemaDeclaration     bool
	interfaceAsAny            bool
	customEmitters            map[reflect.Type]func(reflect.Type) *jsonschema.Schema
	docReaderFS               fs.FS
	draftSet                  bool
	orderedPropertiesSet      bool
	emitSchemaDeclarationSet  bool
	interfaceAsAnySet         bool
}

// WithDraft sets the schema draft to emit. Default: Draft 2020-12.
func WithDraft(d jsonschema.Draft) Option {
	return func(o *options) {
		o.draft = d
		o.draftSet = true
	}
}

// WithID sets the $id of the generated root schema.
func WithID(id string) Option {
	return func(o *options) { o.id = id }
}

// WithExpandedRefs inlines all referenced types instead of using
// $ref + $defs. Default: false.
func WithExpandedRefs(b bool) Option {
	return func(o *options) { o.expandedRefs = b }
}

// WithOmitDescriptions suppresses extracting Go doc comments as
// description strings. Default: false.
func WithOmitDescriptions(b bool) Option {
	return func(o *options) { o.omitDescriptions = b }
}

// WithDurationAsString emits time.Duration as
// {"type":"string","format":"duration"} instead of integer nanoseconds.
// Default: false.
func WithDurationAsString(b bool) Option {
	return func(o *options) { o.durationAsString = b }
}

// WithNullablePointers sets how *T is emitted for every pointer, root
// type and omitempty fields included: true emits anyOf:[null, <T>] and
// false emits <T>. Without this option a pointer allows null where
// encoding/json can write one: everywhere except the root type and a
// struct field tagged omitempty or omitzero, which leaves a nil pointer
// out.
func WithNullablePointers(b bool) Option {
	return func(o *options) {
		o.nullablePointers = b
		o.nullablePointersSet = true
	}
}

// WithOrderedProperties controls whether emitted struct schemas
// preserve Go field declaration order. When true (default), `properties`
// keys are emitted in declaration order. When false, the generator emits a
// plain map[string]any whose key order is unspecified.
func WithOrderedProperties(b bool) Option {
	return func(o *options) {
		o.orderedProperties = b
		o.orderedPropertiesSet = true
	}
}

// WithAdditionalPropertiesFalse emits "additionalProperties": false
// on every generated struct schema. Default: false.
func WithAdditionalPropertiesFalse(b bool) Option {
	return func(o *options) { o.additionalPropertiesFalse = b }
}

// WithSchemaDeclaration emits "$schema" on the root of generated
// schemas. Default: true.
func WithSchemaDeclaration(b bool) Option {
	return func(o *options) {
		o.emitSchemaDeclaration = b
		o.emitSchemaDeclarationSet = true
	}
}

// WithInterfaceAsAny controls how interface{} / any is rendered.
// true (default): {} (any-value). false: error at generation time so the
// caller is forced to register a custom emitter.
func WithInterfaceAsAny(b bool) Option {
	return func(o *options) {
		o.interfaceAsAny = b
		o.interfaceAsAnySet = true
	}
}

// WithCustomEmitter registers a function that emits the schema for values of
// type T, overriding the default kind-based mapping.
func WithCustomEmitter[T any](fn func(reflect.Type) *jsonschema.Schema) Option {
	return func(o *options) {
		if o.customEmitters == nil {
			o.customEmitters = make(map[reflect.Type]func(reflect.Type) *jsonschema.Schema)
		}
		o.customEmitters[reflect.TypeFor[T]()] = fn
	}
}

// WithDocReader extracts Go doc comments from src files (passed as an
// [fs.FS]) and uses them as the description annotation for matching struct
// fields. The generator parses the source files lazily on first use; if the
// FS contains no Go sources the option is a no-op.
func WithDocReader(src fs.FS) Option {
	return func(o *options) { o.docReaderFS = src }
}
