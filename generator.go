package jsonschema

import (
	"bytes"
	"encoding"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"math/big"
	"reflect"
	"sync"
	"time"
)

// Generate returns a [*Schema] describing the type of v. The walker honors
// `json` and `jsonschema` struct tags (see §6 of the requirements doc).
//
// Generate accepts any Go value; only its runtime type is consulted, the
// value itself is not inspected. Pass a nil-typed value (e.g. `(*MyType)(nil)`)
// to describe a type without constructing one.
func Generate(v any, opts ...GenerateOption) (*Schema, error) {
	return NewGenerator(opts...).Generate(v)
}

// MustGenerate is the panic-on-error variant of [Generate]. Intended for
// package-init use of static, well-known types.
func MustGenerate(v any, opts ...GenerateOption) *Schema {
	s, err := Generate(v, opts...)
	if err != nil {
		panic(err)
	}
	return s
}

// GenerateBytes returns the JSON-encoded schema for v. Equivalent to
// [Generate] followed by [Schema.MarshalJSON] but exposes the bytes directly
// for callers that want the wire form (e.g. to write to disk).
func GenerateBytes(v any, opts ...GenerateOption) ([]byte, error) {
	return NewGenerator(opts...).GenerateBytes(v)
}

// FromType is the type-only counterpart to [Generate]; useful when the caller
// has a [reflect.Type] but no value.
func FromType(t reflect.Type, opts ...GenerateOption) (*Schema, error) {
	return NewGenerator(opts...).FromType(t)
}

// Generator is the configurable schema-from-Go-types worker. Reuse a single
// Generator when you have many types to describe with the same options;
// option parsing happens once at construction.
//
// A Generator is safe for concurrent use: it carries only immutable
// configuration plus a lazy doc-comment cache that is built on first use
// behind a sync.Once.
type Generator struct {
	opts *generateOptions

	docOnce  sync.Once
	docCache map[string]string
}

// NewGenerator returns a fresh [*Generator] with the supplied options
// applied. Options that are unset inherit their documented defaults
// (matching the package-level [Generate] entry points).
func NewGenerator(opts ...GenerateOption) *Generator {
	go2 := defaultGenerateOptions()
	for _, o := range opts {
		o(go2)
	}
	resolveGenerateDefaults(go2)
	return &Generator{opts: go2}
}

// defaultGenerateOptions returns a freshly-allocated [*generateOptions]; the
// caller layers user-supplied options on top.
func defaultGenerateOptions() *generateOptions {
	return &generateOptions{}
}

// resolveGenerateDefaults applies documented defaults to unset options.
// The *Set flags distinguish "default" from "user passed false".
func resolveGenerateDefaults(o *generateOptions) {
	if !o.draftSet {
		o.draft = DraftDefault
	}
	if !o.orderedPropertiesSet {
		o.orderedProperties = true
	}
	if !o.emitSchemaDeclarationSet {
		o.emitSchemaDeclaration = true
	}
	if !o.interfaceAsAnySet {
		o.interfaceAsAny = true
	}
}

// Generate generates the schema for the runtime type of v.
func (g *Generator) Generate(v any) (*Schema, error) {
	if g == nil {
		return nil, ErrSchemaNotCompiled
	}
	if v == nil {
		return nil, &CompileError{Message: "Generate: nil value"}
	}
	return g.FromType(reflect.TypeOf(v))
}

// MustGenerate is the panic-on-error variant of [*Generator.Generate].
func (g *Generator) MustGenerate(v any) *Schema {
	s, err := g.Generate(v)
	if err != nil {
		panic(err)
	}
	return s
}

// GenerateBytes returns the JSON-encoded schema for the runtime type of v.
func (g *Generator) GenerateBytes(v any) ([]byte, error) {
	if g == nil {
		return nil, ErrSchemaNotCompiled
	}
	if v == nil {
		return nil, &CompileError{Message: "GenerateBytes: nil value"}
	}
	return g.bytesFromType(reflect.TypeOf(v))
}

// FromType generates the schema for the named [reflect.Type].
func (g *Generator) FromType(t reflect.Type) (*Schema, error) {
	if g == nil {
		return nil, ErrSchemaNotCompiled
	}
	data, err := g.bytesFromType(t)
	if err != nil {
		return nil, err
	}
	s, err := Compile(data)
	if err != nil {
		return nil, &CompileError{Message: "Generate: compile generated schema", Cause: err}
	}
	return s, nil
}

// bytesFromType is the inner entry point shared by every public Generate
// variant.
func (g *Generator) bytesFromType(t reflect.Type) ([]byte, error) {
	if t == nil {
		return nil, &CompileError{Message: "Generate: nil reflect.Type"}
	}
	ctx := newGenCtx(g)
	root, err := ctx.schemaForType(t, "$")
	if err != nil {
		return nil, err
	}
	rootMap, _ := root.(*orderedMap)
	if rootMap == nil {
		// Defensive: a custom emitter may inline a boolean schema at the
		// root; promote to an empty object so $schema / $id can attach.
		rootMap = newOrderedMap()
	}
	if g.opts.emitSchemaDeclaration {
		rootMap.setHead("$schema", g.opts.draft.MetaSchemaURL())
	}
	if g.opts.id != "" {
		rootMap.setHead("$id", g.opts.id)
	}
	if len(ctx.defs) > 0 {
		defsKey := g.opts.draft.DefsKeyword()
		defsMap := newOrderedMap()
		for _, name := range ctx.defsOrder {
			defsMap.set(name, ctx.defs[name])
		}
		rootMap.set(defsKey, defsMap)
	}
	return marshalAny(rootMap)
}

// genCtx is the per-walk state. seenName drives the $defs/$ref strategy
// (its presence on a type means "next visit emits a $ref"); stack tracks
// the active descent path so direct self-cycles are detected even when
// expandedRefs forces inlining.
type genCtx struct {
	g         *Generator
	seenName  map[reflect.Type]string
	stack     map[reflect.Type]struct{}
	defs      map[string]any
	defsOrder []string
}

func newGenCtx(g *Generator) *genCtx {
	return &genCtx{
		g:         g,
		seenName:  make(map[reflect.Type]string),
		stack:     make(map[reflect.Type]struct{}),
		defs:      make(map[string]any),
		defsOrder: nil,
	}
}

// well-known reflect.Types used by the kind switch.
var (
	timeTimeType      = reflect.TypeFor[time.Time]()
	timeDurationType  = reflect.TypeFor[time.Duration]()
	bigIntPtrType     = reflect.TypeFor[*big.Int]()
	bigFloatPtrType   = reflect.TypeFor[*big.Float]()
	jsonNumberType    = reflect.TypeFor[json.Number]()
	jsonRawMessageTyp = reflect.TypeFor[json.RawMessage]()
	textMarshalerType = reflect.TypeFor[encoding.TextMarshaler]()
	jsonMarshalerType = reflect.TypeFor[json.Marshaler]()
)

var errInternalGenerator = errors.New("jsonschema: internal generator error")

// schemaForType returns the schema for t as an any (either an
// *orderedMap, or — for boolean schemas — a bool). path is the reflection
// location used in error messages. Each tryX helper returns (value, hit,
// err) where hit signals that the helper produced the answer.
func (c *genCtx) schemaForType(t reflect.Type, path string) (any, error) {
	if t == nil {
		return newOrderedMap(), nil
	}
	if v, hit, err := c.tryCustomEmitter(t, path); hit {
		return v, err
	}
	if v, hit, err := c.tryPointer(t, path); hit {
		return v, err
	}
	if v, hit := tryWellKnown(t, c.g.opts.durationAsString); hit {
		return v, nil
	}
	if v, hit := tryMarshaler(t); hit {
		return v, nil
	}
	if v, hit, err := c.tryRecursion(t, path); hit {
		return v, err
	}
	return c.dispatchKind(t, path)
}

// tryCustomEmitter handles the WithCustomEmitter override. Returns
// (value, true, err) when an emitter is registered for t.
func (c *genCtx) tryCustomEmitter(t reflect.Type, path string) (any, bool, error) {
	fn, ok := c.g.opts.customEmitters[t]
	if !ok {
		return nil, false, nil
	}
	raw, err := customEmitterToValue(fn(t))
	if err != nil {
		return nil, true, &CompileError{
			KeywordLocation: path,
			Message:         fmt.Sprintf("custom emitter for %s: %v", t, err),
		}
	}
	return raw, true, nil
}

// tryPointer unwraps a pointer type into its underlying schema, applying
// the WithGenerateNullablePointers wrapping when configured.
func (c *genCtx) tryPointer(t reflect.Type, path string) (any, bool, error) {
	if t.Kind() != reflect.Ptr {
		return nil, false, nil
	}
	if t == bigIntPtrType || t == bigFloatPtrType {
		m := newOrderedMap()
		m.set("type", "string")
		return m, true, nil
	}
	inner, err := c.schemaForType(t.Elem(), path)
	if err != nil {
		return nil, true, err
	}
	if c.g.opts.nullablePointers {
		out := newOrderedMap()
		out.set("anyOf", []any{
			orderedFromKV("type", "null"),
			inner,
		})
		return out, true, nil
	}
	return inner, true, nil
}

// tryWellKnown handles types where the JSON shape is fixed by convention:
// time.Time, time.Duration, json.Number, json.RawMessage.
func tryWellKnown(t reflect.Type, durationAsString bool) (any, bool) {
	switch t {
	case timeTimeType:
		m := newOrderedMap()
		m.set("type", "string")
		m.set("format", "date-time")
		return m, true
	case timeDurationType:
		m := newOrderedMap()
		if durationAsString {
			m.set("type", "string")
			m.set("format", "duration")
		} else {
			m.set("type", "integer")
		}
		return m, true
	case jsonNumberType:
		m := newOrderedMap()
		m.set("type", []any{"number", "string"})
		return m, true
	case jsonRawMessageTyp:
		return newOrderedMap(), true
	}
	return nil, false
}

// tryMarshaler handles types implementing encoding.TextMarshaler /
// json.Marshaler. TextMarshaler types are emitted as strings; json.Marshaler
// types fall through unless they're not structs (in which case `{}` is
// safest because the runtime JSON shape is unknown).
func tryMarshaler(t reflect.Type) (any, bool) {
	if t.Implements(textMarshalerType) || reflect.PointerTo(t).Implements(textMarshalerType) {
		if t.Kind() != reflect.Struct || t == timeTimeType {
			m := newOrderedMap()
			m.set("type", "string")
			return m, true
		}
	}
	if t.Implements(jsonMarshalerType) || reflect.PointerTo(t).Implements(jsonMarshalerType) {
		if t.Kind() != reflect.Struct {
			return newOrderedMap(), true
		}
	}
	return nil, false
}

// tryRecursion checks whether t is on the descent stack or already
// referenced via $ref, and returns a $ref schema in either case.
func (c *genCtx) tryRecursion(t reflect.Type, path string) (any, bool, error) {
	if t.Name() == "" || t.PkgPath() == "" {
		return nil, false, nil
	}
	if name, ok := c.seenName[t]; ok {
		return c.refToDef(name), true, nil
	}
	if _, onStack := c.stack[t]; onStack {
		if c.g.opts.expandedRefs {
			return nil, true, &CompileError{
				KeywordLocation: path,
				Message:         fmt.Sprintf("Generate: cannot inline self-referential type %s with WithGenerateExpandedRefs(true)", t),
			}
		}
		name := c.allocateDefName(t)
		return c.refToDef(name), true, nil
	}
	return nil, false, nil
}

// dispatchKind is the kind switch for "ordinary" types.
func (c *genCtx) dispatchKind(t reflect.Type, path string) (any, error) {
	switch t.Kind() {
	case reflect.Bool:
		m := newOrderedMap()
		m.set("type", "boolean")
		return m, nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return integerSchemaForKind(t.Kind()), nil
	case reflect.Float32, reflect.Float64:
		m := newOrderedMap()
		m.set("type", "number")
		return m, nil
	case reflect.String:
		m := newOrderedMap()
		m.set("type", "string")
		return m, nil
	case reflect.Slice, reflect.Array:
		return c.schemaForArray(t, path)
	case reflect.Map:
		return c.schemaForMap(t, path)
	case reflect.Struct:
		return c.schemaForStruct(t, path)
	case reflect.Interface:
		if c.g.opts.interfaceAsAny {
			return newOrderedMap(), nil
		}
		return nil, &CompileError{
			KeywordLocation: path,
			Message:         fmt.Sprintf("Generate: interface type %s requires WithCustomEmitter (interfaceAsAny disabled)", t),
		}
	case reflect.Chan, reflect.Func, reflect.UnsafePointer:
		return nil, &CompileError{
			KeywordLocation: path,
			Message:         fmt.Sprintf("Generate: unsupported kind %s for type %s", t.Kind(), t),
		}
	default:
		return nil, &CompileError{
			KeywordLocation: path,
			Message:         fmt.Sprintf("Generate: unsupported kind %s for type %s", t.Kind(), t),
		}
	}
}

// integerSchemaForKind returns {"type":"integer"} with width-based
// minimum / maximum so a JSON number fitting the Go field validates.
func integerSchemaForKind(k reflect.Kind) *orderedMap {
	m := newOrderedMap()
	m.set("type", "integer")
	switch k {
	case reflect.Int8:
		m.set("minimum", int64(math.MinInt8))
		m.set("maximum", int64(math.MaxInt8))
	case reflect.Int16:
		m.set("minimum", int64(math.MinInt16))
		m.set("maximum", int64(math.MaxInt16))
	case reflect.Int32:
		m.set("minimum", int64(math.MinInt32))
		m.set("maximum", int64(math.MaxInt32))
	case reflect.Uint8:
		m.set("minimum", int64(0))
		m.set("maximum", int64(math.MaxUint8))
	case reflect.Uint16:
		m.set("minimum", int64(0))
		m.set("maximum", int64(math.MaxUint16))
	case reflect.Uint32:
		m.set("minimum", int64(0))
		m.set("maximum", int64(math.MaxUint32))
	case reflect.Uint, reflect.Uint64:
		m.set("minimum", int64(0))
	}
	return m
}

// schemaForArray builds an array schema. Detects the byte-slice and byte-
// array shapes and emits the contentEncoding-string variant.
func (c *genCtx) schemaForArray(t reflect.Type, path string) (any, error) {
	elem := t.Elem()
	if elem.Kind() == reflect.Uint8 {
		m := newOrderedMap()
		m.set("type", "string")
		m.set("contentEncoding", "base64")
		return m, nil
	}
	itemSchema, err := c.schemaForType(elem, path+"/[]")
	if err != nil {
		return nil, err
	}
	m := newOrderedMap()
	m.set("type", "array")
	m.set("items", itemSchema)
	return m, nil
}

// schemaForMap builds an object schema with `additionalProperties` describing
// the value type. Only string-keyed maps are supported (JSON objects can't
// have non-string keys).
func (c *genCtx) schemaForMap(t reflect.Type, path string) (any, error) {
	if t.Key().Kind() != reflect.String {
		return nil, &CompileError{
			KeywordLocation: path,
			Message:         fmt.Sprintf("Generate: map key must be string-kinded (got %s) for %s", t.Key().Kind(), t),
		}
	}
	valSchema, err := c.schemaForType(t.Elem(), path+"/{}")
	if err != nil {
		return nil, err
	}
	m := newOrderedMap()
	m.set("type", "object")
	m.set("additionalProperties", valSchema)
	return m, nil
}

// schemaForStruct walks the field set of a struct, honoring json + jsonschema
// tags.
func (c *genCtx) schemaForStruct(t reflect.Type, path string) (any, error) {
	c.stack[t] = struct{}{}
	defer delete(c.stack, t)

	props := newOrderedMap()
	required := []any{}
	requiredSeen := map[string]bool{}

	if err := c.collectStructFields(t, path, props, &required, requiredSeen); err != nil {
		return nil, err
	}

	m := newOrderedMap()
	m.set("type", "object")
	if props.len() > 0 {
		// orderedProperties=false drops the ordered map so encoders see
		// unspecified key order (the caller's explicit request).
		if c.g.opts.orderedPropertiesSet && !c.g.opts.orderedProperties {
			plain := make(map[string]any, props.len())
			for _, k := range props.keys {
				plain[k] = props.vals[k]
			}
			m.set("properties", plain)
		} else {
			m.set("properties", props)
		}
	}
	if len(required) > 0 {
		m.set(kwRequired, required)
	}
	if c.g.opts.additionalPropertiesFalse {
		m.set("additionalProperties", false)
	}

	// If recursive descent allocated a $defs entry for t, install the
	// materialized schema there and return a $ref to it.
	if !c.g.opts.expandedRefs && t.Name() != "" && t.PkgPath() != "" {
		if name, ok := c.seenName[t]; ok {
			c.defs[name] = m
			return c.refToDef(name), nil
		}
	}
	return m, nil
}

// collectStructFields walks the field set and populates props / required.
// Embedded anonymous structs are inlined (matches encoding/json semantics).
func (c *genCtx) collectStructFields(t reflect.Type, path string, props *orderedMap, required *[]any, requiredSeen map[string]bool) error {
	for f := range t.Fields() {
		if !f.IsExported() {
			continue
		}
		jsonTag, _ := f.Tag.Lookup("json")
		jsonName, jsonOpts := parseJSONTag(jsonTag)
		if jsonName == "-" && jsonOpts == "" {
			continue
		}

		// Embedded anonymous struct without an explicit json name: inline
		// its fields (matches encoding/json semantics).
		if f.Anonymous && jsonName == "" {
			ft := f.Type
			for ft.Kind() == reflect.Ptr {
				ft = ft.Elem()
			}
			if ft.Kind() == reflect.Struct {
				if err := c.collectStructFields(ft, path+"."+f.Name, props, required, requiredSeen); err != nil {
					return err
				}
				continue
			}
		}

		name := jsonName
		if name == "" {
			name = f.Name
		}

		fieldPath := path + "." + f.Name
		schemaTag, _ := f.Tag.Lookup("jsonschema")
		spec, err := parseJSONSchemaTag(schemaTag, f.Type, fieldPath)
		if err != nil {
			return err
		}

		var fieldSchema any
		if spec.hasRef {
			refMap := newOrderedMap()
			refMap.set("$ref", spec.ref)
			fieldSchema = refMap
		} else {
			fieldSchema, err = c.schemaForType(f.Type, fieldPath)
			if err != nil {
				return err
			}
		}
		fieldSchema = c.applyTagSpecToSchema(fieldSchema, &spec, t, f, fieldPath)

		props.set(name, fieldSchema)

		omitempty := hasJSONTagOption(jsonOpts, "omitempty")
		if spec.hasReq && !omitempty && !requiredSeen[name] {
			requiredSeen[name] = true
			*required = append(*required, name)
		}
	}
	return nil
}

// applyTagSpecToSchema folds the parsed tag spec into fieldSchema and
// returns the updated schema; callers must reassign because boolean
// schemas are promoted to objects when the tag adds keywords.
//
// v0.1 attaches metadata as siblings to $ref. 2019-09+ allows this; older
// drafts forbid it but most validators accept it in practice.
func (c *genCtx) applyTagSpecToSchema(fieldSchema any, spec *tagSpec, owner reflect.Type, sf reflect.StructField, fieldPath string) any {
	m, ok := fieldSchema.(*orderedMap)
	if !ok {
		if !spec.hasAny() {
			return fieldSchema
		}
		m = newOrderedMap()
	}
	c.applyTagDescription(m, spec, owner, sf)
	applyTagMetadata(m, spec)
	applyTagAssertions(m, spec)
	_ = fieldPath
	return m
}

// applyTagDescription resolves the description annotation: tag wins, then
// docReader, then nothing.
func (c *genCtx) applyTagDescription(m *orderedMap, spec *tagSpec, owner reflect.Type, sf reflect.StructField) {
	if c.g.opts.omitDescriptions {
		return
	}
	if spec.hasDescription {
		m.set("description", spec.description)
		return
	}
	if doc := c.lookupFieldDoc(owner, sf); doc != "" {
		m.set("description", doc)
	}
}

// applyTagMetadata copies the JSON Schema metadata-vocabulary keywords
// (title / default / examples / readOnly / writeOnly / deprecated / $id)
// from spec to m.
func applyTagMetadata(m *orderedMap, spec *tagSpec) {
	if spec.hasTitle {
		m.set("title", spec.title)
	}
	if spec.hasDefault {
		m.set("default", spec.defaultVal)
	}
	if spec.hasExamples {
		m.set("examples", anySliceCopy(spec.examples))
	}
	if spec.hasDeprecated {
		m.set("deprecated", true)
	}
	if spec.hasReadOnly {
		m.set("readOnly", true)
	}
	if spec.hasWriteOnly {
		m.set("writeOnly", true)
	}
	if spec.hasID {
		m.set("$id", spec.id)
	}
}

// applyTagAssertions copies the assertion-vocabulary keywords (enum,
// const, format, numeric/length/items/properties bounds, uniqueItems,
// pattern, additionalProperties=false) from spec to m.
func applyTagAssertions(m *orderedMap, spec *tagSpec) {
	if spec.hasEnum {
		m.set("enum", anySliceCopy(spec.enum))
	}
	if spec.hasConst {
		m.set("const", spec.constVal)
	}
	if spec.hasFormat {
		m.set("format", spec.format)
	}
	applyTagNumeric(m, spec)
	applyTagLengths(m, spec)
	if spec.hasUniqueItems {
		m.set("uniqueItems", true)
	}
	if spec.hasAdditionalPropertiesFalse {
		m.set("additionalProperties", false)
	}
	if spec.hasPattern {
		m.set("pattern", spec.pattern)
	}
}

// applyTagNumeric copies the numeric-bound keywords.
func applyTagNumeric(m *orderedMap, spec *tagSpec) {
	if spec.hasMinimum {
		m.set("minimum", spec.minimum)
	}
	if spec.hasMaximum {
		m.set("maximum", spec.maximum)
	}
	if spec.hasExclusiveMinimum {
		m.set("exclusiveMinimum", spec.exclusiveMinimum)
	}
	if spec.hasExclusiveMaximum {
		m.set("exclusiveMaximum", spec.exclusiveMaximum)
	}
	if spec.hasMultipleOf {
		m.set(kwMultipleOf, spec.multipleOf)
	}
}

// applyTagLengths copies the length-bound keywords (string length, array
// items, object properties).
func applyTagLengths(m *orderedMap, spec *tagSpec) {
	if spec.hasMinLength {
		m.set("minLength", int64(spec.minLength))
	}
	if spec.hasMaxLength {
		m.set("maxLength", int64(spec.maxLength))
	}
	if spec.hasMinItems {
		m.set("minItems", int64(spec.minItems))
	}
	if spec.hasMaxItems {
		m.set("maxItems", int64(spec.maxItems))
	}
	if spec.hasMinProperties {
		m.set("minProperties", int64(spec.minProperties))
	}
	if spec.hasMaxProperties {
		m.set("maxProperties", int64(spec.maxProperties))
	}
}

// hasAny reports whether the tagSpec carries any non-zero option.
func (s *tagSpec) hasAny() bool {
	return s.hasMetadata() || s.hasAssertion()
}

// hasMetadata reports whether the spec carries any metadata-vocabulary
// option (description / title / default / examples / readOnly / writeOnly /
// deprecated / $id / $ref / required).
func (s *tagSpec) hasMetadata() bool {
	return s.hasReq || s.hasDescription || s.hasTitle || s.hasDefault ||
		s.hasExamples || s.hasDeprecated || s.hasReadOnly || s.hasWriteOnly ||
		s.hasID || s.hasRef
}

// hasAssertion reports whether the spec carries any assertion-vocabulary
// option (enum / const / format / numeric bounds / lengths / pattern /
// uniqueItems / additionalProperties).
func (s *tagSpec) hasAssertion() bool {
	return s.hasEnum || s.hasConst || s.hasFormat ||
		s.hasMinimum || s.hasMaximum || s.hasExclusiveMinimum || s.hasExclusiveMaximum ||
		s.hasMultipleOf || s.hasMinLength || s.hasMaxLength || s.hasPattern ||
		s.hasMinItems || s.hasMaxItems || s.hasUniqueItems ||
		s.hasMinProperties || s.hasMaxProperties || s.hasAdditionalPropertiesFalse
}

// allocateDefName picks a stable $defs name for t, disambiguating
// cross-package name collisions with a numeric suffix.
func (c *genCtx) allocateDefName(t reflect.Type) string {
	base := t.Name()
	if base == "" {
		base = "anon"
	}
	name := base
	for i := 2; ; i++ {
		_, taken := c.defs[name]
		alreadyMine := false
		for other, used := range c.seenName {
			if used == name && other == t {
				alreadyMine = true
				break
			}
		}
		if alreadyMine {
			break
		}
		if !taken {
			break
		}
		name = fmt.Sprintf("%s_%d", base, i)
	}
	c.seenName[t] = name
	if _, exists := c.defs[name]; !exists {
		c.defs[name] = nil
		c.defsOrder = append(c.defsOrder, name)
	}
	return name
}

// refToDef returns a $ref schema pointing at #/$defs/<name> (or
// #/definitions/<name> for legacy drafts).
func (c *genCtx) refToDef(name string) *orderedMap {
	defsKey := c.g.opts.draft.DefsKeyword()
	m := newOrderedMap()
	m.set("$ref", "#/"+defsKey+"/"+name)
	return m
}

// customEmitterToValue converts a *Schema returned by a custom emitter
// into the generator's internal value shape, preserving object key order.
func customEmitterToValue(s *Schema) (any, error) {
	if s == nil {
		return newOrderedMap(), nil
	}
	data, err := s.MarshalJSON()
	if err != nil {
		return nil, err
	}
	if bytes.Equal(bytes.TrimSpace(data), []byte("null")) {
		return newOrderedMap(), nil
	}
	return decodeOrdered(data)
}

// decodeOrdered parses a JSON document into the generator's internal
// value shape (*orderedMap for objects, []any for arrays). Uses the
// encoding/json token stream to preserve object key order.
func decodeOrdered(data []byte) (any, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	v, err := decodeOrderedValue(dec)
	if err != nil {
		return nil, err
	}
	return v, nil
}

func decodeOrderedValue(dec *json.Decoder) (any, error) {
	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	return decodeOrderedFromToken(dec, tok)
}

func decodeOrderedFromToken(dec *json.Decoder, tok json.Token) (any, error) {
	switch v := tok.(type) {
	case json.Delim:
		switch v {
		case '{':
			out := newOrderedMap()
			for dec.More() {
				kt, err := dec.Token()
				if err != nil {
					return nil, err
				}
				key, ok := kt.(string)
				if !ok {
					return nil, fmt.Errorf("%w: object key not string: %T", errInternalGenerator, kt)
				}
				val, err := decodeOrderedValue(dec)
				if err != nil {
					return nil, err
				}
				out.set(key, val)
			}
			if _, err := dec.Token(); err != nil {
				return nil, err
			}
			return out, nil
		case '[':
			var out []any
			for dec.More() {
				val, err := decodeOrderedValue(dec)
				if err != nil {
					return nil, err
				}
				out = append(out, val)
			}
			if _, err := dec.Token(); err != nil {
				return nil, err
			}
			if out == nil {
				return []any{}, nil
			}
			return out, nil
		}
		return nil, fmt.Errorf("%w: unexpected delim %q", errInternalGenerator, v)
	default:
		return v, nil
	}
}

// orderedMap is the generator's insertion-ordered map: ordered keys plus
// an O(1) overwrite map. Satisfies [json.Marshaler].
type orderedMap struct {
	keys []string
	vals map[string]any
}

func newOrderedMap() *orderedMap {
	return &orderedMap{vals: make(map[string]any)}
}

// orderedFromKV builds a small orderedMap literal from alternating
// key / value arguments.
func orderedFromKV(kv ...any) *orderedMap {
	m := newOrderedMap()
	for i := 0; i+1 < len(kv); i += 2 {
		k, ok := kv[i].(string)
		if !ok {
			continue
		}
		m.set(k, kv[i+1])
	}
	return m
}

// set appends (key, val); on overwrite the original position is kept.
func (m *orderedMap) set(key string, val any) {
	if _, ok := m.vals[key]; !ok {
		m.keys = append(m.keys, key)
	}
	m.vals[key] = val
}

// setHead is like set but moves the key to the front of the order, so
// root-level $schema / $id can appear first in the emitted JSON.
func (m *orderedMap) setHead(key string, val any) {
	if _, ok := m.vals[key]; ok {
		for i, k := range m.keys {
			if k == key {
				m.keys = append(m.keys[:i], m.keys[i+1:]...)
				break
			}
		}
	}
	m.keys = append([]string{key}, m.keys...)
	m.vals[key] = val
}

// len returns the number of entries.
func (m *orderedMap) len() int { return len(m.keys) }

// MarshalJSON emits the entries in insertion order.
func (m *orderedMap) MarshalJSON() ([]byte, error) {
	if m == nil || len(m.keys) == 0 {
		return []byte("{}"), nil
	}
	var buf bytes.Buffer
	buf.WriteByte('{')
	for i, k := range m.keys {
		if i > 0 {
			buf.WriteByte(',')
		}
		kb, err := json.Marshal(k)
		if err != nil {
			return nil, err
		}
		buf.Write(kb)
		buf.WriteByte(':')
		vb, err := marshalAny(m.vals[k])
		if err != nil {
			return nil, err
		}
		buf.Write(vb)
	}
	buf.WriteByte('}')
	return buf.Bytes(), nil
}

// marshalAny marshals v while honoring *orderedMap (recursive into slices
// and orderedMap children) and defers to encoding/json for everything
// else. Avoids re-entering the stdlib Marshaler dispatch on each recursion.
func marshalAny(v any) ([]byte, error) {
	switch t := v.(type) {
	case nil:
		return []byte("null"), nil
	case *orderedMap:
		return t.MarshalJSON()
	case []any:
		var buf bytes.Buffer
		buf.WriteByte('[')
		for i, item := range t {
			if i > 0 {
				buf.WriteByte(',')
			}
			ib, err := marshalAny(item)
			if err != nil {
				return nil, err
			}
			buf.Write(ib)
		}
		buf.WriteByte(']')
		return buf.Bytes(), nil
	default:
		return json.Marshal(v)
	}
}

// anySliceCopy returns a shallow copy of in. The generator copies before
// embedding in the schema so subsequent mutations (e.g. by the test harness)
// don't bleed into the schema's source bytes.
func anySliceCopy(in []any) []any {
	out := make([]any, len(in))
	copy(out, in)
	return out
}
