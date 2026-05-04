package jsonschema

import (
	"fmt"
	"reflect"
	"slices"
	"strconv"
	"strings"
)

const (
	kwFormat     = "format"
	kwRequired   = "required"
	kwMultipleOf = "multipleOf"
	kwMinimum    = "minimum"
)

// tagSpec is the parsed result of one struct field's `jsonschema:"..."`
// tag. Each option's hasX flag distinguishes "set" from "zero default".
type tagSpec struct {
	required bool
	hasReq   bool

	description    string
	hasDescription bool

	title    string
	hasTitle bool

	defaultVal any
	hasDefault bool

	examples    []any
	hasExamples bool

	deprecated    bool
	hasDeprecated bool

	readOnly    bool
	hasReadOnly bool

	writeOnly    bool
	hasWriteOnly bool

	enum    []any
	hasEnum bool

	constVal any
	hasConst bool

	format    string
	hasFormat bool

	minimum    any
	hasMinimum bool

	maximum    any
	hasMaximum bool

	exclusiveMinimum    any
	hasExclusiveMinimum bool

	exclusiveMaximum    any
	hasExclusiveMaximum bool

	multipleOf    any
	hasMultipleOf bool

	minLength    int
	hasMinLength bool

	maxLength    int
	hasMaxLength bool

	pattern    string
	hasPattern bool

	minItems    int
	hasMinItems bool

	maxItems    int
	hasMaxItems bool

	uniqueItems    bool
	hasUniqueItems bool

	minProperties    int
	hasMinProperties bool

	maxProperties    int
	hasMaxProperties bool

	additionalPropertiesFalse    bool
	hasAdditionalPropertiesFalse bool

	id    string
	hasID bool

	ref    string
	hasRef bool
}

// parseJSONTag splits a `json:"..."` tag into (name, options). options
// is the comma-joined remainder for cheap "omitempty"-style lookup.
func parseJSONTag(tag string) (name, options string) {
	if before, after, ok := strings.Cut(tag, ","); ok {
		return before, after
	}
	return tag, ""
}

// hasJSONTagOption reports whether opt is one of the comma-separated options
// in optionsString (the second return value of [parseJSONTag]).
func hasJSONTagOption(optionsString, opt string) bool {
	if optionsString == "" {
		return false
	}
	return slices.Contains(strings.Split(optionsString, ","), opt)
}

// parseJSONSchemaTag parses a `jsonschema:"..."` tag. The host field
// type drives value coercion (so `minLength` on a non-string field is
// rejected). fieldType may be nil for paths that want only universal
// options like description.
func parseJSONSchemaTag(tag string, fieldType reflect.Type, fieldPath string) (tagSpec, error) {
	var spec tagSpec
	if tag == "" {
		return spec, nil
	}
	tokens := tokenizeJSONSchemaTag(tag)
	for _, tok := range tokens {
		name, value, hasValue := splitTagOption(tok)
		if name == "" {
			continue
		}
		if err := applyTagOption(&spec, name, value, hasValue, fieldType, fieldPath); err != nil {
			return spec, err
		}
	}
	return spec, nil
}

// tokenizeJSONSchemaTag splits a tag value on unescaped commas, honoring
// the `\,`, `\|`, `\\` escapes from §6.5.
func tokenizeJSONSchemaTag(tag string) []string {
	var out []string
	var cur strings.Builder
	escaped := false
	for i := range len(tag) {
		c := tag[i]
		if escaped {
			cur.WriteByte('\\')
			cur.WriteByte(c)
			escaped = false
			continue
		}
		if c == '\\' {
			escaped = true
			continue
		}
		if c == ',' {
			out = append(out, cur.String())
			cur.Reset()
			continue
		}
		cur.WriteByte(c)
	}
	if escaped {
		cur.WriteByte('\\')
	}
	if cur.Len() > 0 || len(out) > 0 {
		out = append(out, cur.String())
	}
	return out
}

// splitTagOption splits an option token at its first unescaped `=` into
// (name, value, hasValue), unescaping both halves.
func splitTagOption(tok string) (name, value string, hasValue bool) {
	for i := 0; i < len(tok); i++ {
		if tok[i] == '\\' {
			i++
			continue
		}
		if tok[i] == '=' {
			return strings.TrimSpace(unescapeTagValue(tok[:i])),
				unescapeTagValue(tok[i+1:]),
				true
		}
	}
	return strings.TrimSpace(unescapeTagValue(tok)), "", false
}

// unescapeTagValue resolves the `\,`, `\|`, `\\` escapes from §6.5.
// Unknown `\x` sequences collapse to the literal x.
func unescapeTagValue(s string) string {
	if !strings.ContainsRune(s, '\\') {
		return s
	}
	var out strings.Builder
	out.Grow(len(s))
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+1 < len(s) {
			out.WriteByte(s[i+1])
			i++
			continue
		}
		out.WriteByte(s[i])
	}
	return out.String()
}

// splitTagList splits an enum / examples value on unescaped `|`, using
// the same escape rules as [tokenizeJSONSchemaTag].
func splitTagList(value string) []string {
	var out []string
	var cur strings.Builder
	escaped := false
	for i := range len(value) {
		c := value[i]
		if escaped {
			cur.WriteByte(c)
			escaped = false
			continue
		}
		if c == '\\' {
			escaped = true
			continue
		}
		if c == '|' {
			out = append(out, cur.String())
			cur.Reset()
			continue
		}
		cur.WriteByte(c)
	}
	out = append(out, cur.String())
	return out
}

// applyTagOption dispatches a parsed (name, value) pair to the matching
// helper. Each helper returns (handled, err): handled=true means the
// option was recognized (success or failure).
func applyTagOption(spec *tagSpec, name, value string, hasValue bool, ft reflect.Type, fieldPath string) error {
	if handled, err := applyTagFlagOption(spec, name, hasValue, fieldPath); handled {
		return err
	}
	if handled, err := applyTagStringOption(spec, name, value, hasValue, fieldPath); handled {
		return err
	}
	if handled, err := applyTagCoercedOption(spec, name, value, hasValue, ft, fieldPath); handled {
		return err
	}
	if handled, err := applyTagListOption(spec, name, value, hasValue, ft, fieldPath); handled {
		return err
	}
	if handled, err := applyTagNumericOption(spec, name, value, hasValue, ft, fieldPath); handled {
		return err
	}
	if handled, err := applyTagLengthOption(spec, name, value, ft, fieldPath); handled {
		return err
	}
	if name == "additionalProperties" {
		if !hasValue || value != "false" {
			return tagErr(fieldPath, "additionalProperties: only =false is supported")
		}
		spec.additionalPropertiesFalse = true
		spec.hasAdditionalPropertiesFalse = true
		return nil
	}
	return tagErr(fieldPath, fmt.Sprintf("unknown jsonschema tag option %q", name))
}

// applyTagFlagOption handles boolean-presence options (required,
// deprecated, readOnly, writeOnly, uniqueItems).
func applyTagFlagOption(spec *tagSpec, name string, hasValue bool, path string) (bool, error) {
	switch name {
	case kwRequired:
		if hasValue {
			return true, tagErr(path, kwRequired+": flag option does not take a value")
		}
		spec.required = true
		spec.hasReq = true
	case "deprecated":
		if hasValue {
			return true, tagErr(path, "deprecated: flag option does not take a value")
		}
		spec.deprecated = true
		spec.hasDeprecated = true
	case "readOnly":
		if hasValue {
			return true, tagErr(path, "readOnly: flag option does not take a value")
		}
		spec.readOnly = true
		spec.hasReadOnly = true
	case "writeOnly":
		if hasValue {
			return true, tagErr(path, "writeOnly: flag option does not take a value")
		}
		spec.writeOnly = true
		spec.hasWriteOnly = true
	case "uniqueItems":
		if hasValue {
			return true, tagErr(path, "uniqueItems: flag option does not take a value")
		}
		spec.uniqueItems = true
		spec.hasUniqueItems = true
	default:
		return false, nil
	}
	return true, nil
}

// applyTagStringOption handles the string-valued passthrough options
// (description, title, format, pattern, $id, $ref).
//
// The `format=` value passes through verbatim to the emitted schema; the
// generator does not validate that the name is a registered or known
// format. At validation time the standard "format" keyword behaves as an
// annotation by default (see [WithFormatAssertion]), so unknown format
// values are tolerated unless the caller explicitly opts into assertion
// mode and registers a [CustomFormat] for the name.
func applyTagStringOption(spec *tagSpec, name, value string, hasValue bool, path string) (bool, error) {
	target := struct {
		set     func(string)
		seenSet func()
	}{}
	switch name {
	case "description":
		target.set = func(s string) { spec.description = s }
		target.seenSet = func() { spec.hasDescription = true }
	case "title":
		target.set = func(s string) { spec.title = s }
		target.seenSet = func() { spec.hasTitle = true }
	case kwFormat:
		target.set = func(s string) { spec.format = s }
		target.seenSet = func() { spec.hasFormat = true }
	case "pattern":
		target.set = func(s string) { spec.pattern = s }
		target.seenSet = func() { spec.hasPattern = true }
	case "$id":
		target.set = func(s string) { spec.id = s }
		target.seenSet = func() { spec.hasID = true }
	case "$ref":
		target.set = func(s string) { spec.ref = s }
		target.seenSet = func() { spec.hasRef = true }
	default:
		return false, nil
	}
	if !hasValue {
		return true, tagErr(path, name+": requires a value")
	}
	target.set(value)
	target.seenSet()
	return true, nil
}

// applyTagCoercedOption handles single-value options coerced by field type
// (default, const).
func applyTagCoercedOption(spec *tagSpec, name, value string, hasValue bool, ft reflect.Type, path string) (bool, error) {
	switch name {
	case "default":
		if !hasValue {
			return true, tagErr(path, "default: requires a value")
		}
		v, err := coerceFieldValue(ft, value, path, "default")
		if err != nil {
			return true, err
		}
		spec.defaultVal = v
		spec.hasDefault = true
	case "const":
		if !hasValue {
			return true, tagErr(path, "const: requires a value")
		}
		v, err := coerceFieldValue(ft, value, path, "const")
		if err != nil {
			return true, err
		}
		spec.constVal = v
		spec.hasConst = true
	default:
		return false, nil
	}
	return true, nil
}

// applyTagListOption handles list-valued options (enum, examples).
func applyTagListOption(spec *tagSpec, name, value string, hasValue bool, ft reflect.Type, path string) (bool, error) {
	switch name {
	case "enum", "examples":
	default:
		return false, nil
	}
	if !hasValue {
		return true, tagErr(path, name+": requires a value")
	}
	items := splitTagList(value)
	out := make([]any, 0, len(items))
	elemT := elemTypeForList(ft)
	for _, item := range items {
		v, err := coerceFieldValue(elemT, item, path, name)
		if err != nil {
			return true, err
		}
		out = append(out, v)
	}
	if name == "enum" {
		spec.enum = out
		spec.hasEnum = true
	} else {
		spec.examples = out
		spec.hasExamples = true
	}
	return true, nil
}

// applyTagNumericOption handles numeric-bound options (minimum, maximum,
// exclusiveMinimum, exclusiveMaximum, multipleOf).
func applyTagNumericOption(spec *tagSpec, name, value string, hasValue bool, ft reflect.Type, path string) (bool, error) {
	switch name {
	case kwMinimum, "maximum", "exclusiveMinimum", "exclusiveMaximum", kwMultipleOf:
	default:
		return false, nil
	}
	v, err := coerceNumericValue(ft, value, hasValue, path, name)
	if err != nil {
		return true, err
	}
	switch name {
	case kwMinimum:
		spec.minimum = v
		spec.hasMinimum = true
	case "maximum":
		spec.maximum = v
		spec.hasMaximum = true
	case "exclusiveMinimum":
		spec.exclusiveMinimum = v
		spec.hasExclusiveMinimum = true
	case "exclusiveMaximum":
		spec.exclusiveMaximum = v
		spec.hasExclusiveMaximum = true
	case kwMultipleOf:
		spec.multipleOf = v
		spec.hasMultipleOf = true
	}
	return true, nil
}

// applyTagLengthOption handles non-negative-integer length options
// (minLength, maxLength, minItems, maxItems, minProperties, maxProperties).
func applyTagLengthOption(spec *tagSpec, name, value string, ft reflect.Type, path string) (bool, error) {
	switch name {
	case "minLength", "maxLength":
		if err := requireStringTarget(ft, path, name); err != nil {
			return true, err
		}
	case "minItems", "maxItems":
		if err := requireArrayTarget(ft, path, name); err != nil {
			return true, err
		}
	case "minProperties", "maxProperties":
		if err := requireMapTarget(ft, path, name); err != nil {
			return true, err
		}
	default:
		return false, nil
	}
	n, err := parseNonNegativeInt(value, path, name)
	if err != nil {
		return true, err
	}
	switch name {
	case "minLength":
		spec.minLength = n
		spec.hasMinLength = true
	case "maxLength":
		spec.maxLength = n
		spec.hasMaxLength = true
	case "minItems":
		spec.minItems = n
		spec.hasMinItems = true
	case "maxItems":
		spec.maxItems = n
		spec.hasMaxItems = true
	case "minProperties":
		spec.minProperties = n
		spec.hasMinProperties = true
	case "maxProperties":
		spec.maxProperties = n
		spec.hasMaxProperties = true
	}
	return true, nil
}

// tagErr returns a [*CompileError] tagged with "Tag" plus the field path.
func tagErr(fieldPath, msg string) error {
	return &CompileError{
		KeywordLocation: fieldPath,
		Message:         "jsonschema tag: " + msg,
	}
}

// elemTypeForList returns the per-item Go type for enum / examples on a
// host field: slices and arrays use their element type; scalars use the
// host type directly.
func elemTypeForList(ft reflect.Type) reflect.Type {
	if ft == nil {
		return nil
	}
	switch ft.Kind() {
	case reflect.Slice, reflect.Array:
		return ft.Elem()
	}
	return ft
}

// requireStringTarget enforces that minLength / maxLength / pattern apply to
// string fields. Returns a [*CompileError] for non-string targets.
func requireStringTarget(ft reflect.Type, path, name string) error {
	if ft == nil {
		return nil
	}
	t := ft
	for t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	if t.Kind() == reflect.String {
		return nil
	}
	return tagErr(path, fmt.Sprintf("%s applies only to string fields (field is %s)", name, t.Kind()))
}

// requireArrayTarget enforces that minItems / maxItems apply to slice or
// array fields. Returns a [*CompileError] otherwise.
func requireArrayTarget(ft reflect.Type, path, name string) error {
	if ft == nil {
		return nil
	}
	t := ft
	for t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	switch t.Kind() {
	case reflect.Slice, reflect.Array:
		return nil
	}
	return tagErr(path, fmt.Sprintf("%s applies only to slice/array fields (field is %s)", name, t.Kind()))
}

// requireMapTarget enforces that minProperties / maxProperties apply to map
// or struct fields.
func requireMapTarget(ft reflect.Type, path, name string) error {
	if ft == nil {
		return nil
	}
	t := ft
	for t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	switch t.Kind() {
	case reflect.Map, reflect.Struct:
		return nil
	}
	return tagErr(path, fmt.Sprintf("%s applies only to map/struct fields (field is %s)", name, t.Kind()))
}

// parseNonNegativeInt parses a tag value as a non-negative integer.
func parseNonNegativeInt(value, path, name string) (int, error) {
	n, err := strconv.Atoi(value)
	if err != nil {
		return 0, tagErr(path, fmt.Sprintf("%s: %q is not a valid integer", name, value))
	}
	if n < 0 {
		return 0, tagErr(path, fmt.Sprintf("%s: must be non-negative (got %d)", name, n))
	}
	return n, nil
}

// coerceFieldValue coerces a raw tag value string into the JSON-serializable
// shape that matches ft's kind. Strings stay strings; integers become int64
// or float64; bools become bools.
func coerceFieldValue(ft reflect.Type, value, path, name string) (any, error) {
	if ft == nil {
		return value, nil
	}
	t := ft
	for t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	switch t.Kind() {
	case reflect.String:
		return value, nil
	case reflect.Bool:
		switch value {
		case "true":
			return true, nil
		case "false":
			return false, nil
		}
		return nil, tagErr(path, fmt.Sprintf("%s: %q is not a valid bool", name, value))
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		i, err := strconv.ParseInt(value, 10, 64)
		if err != nil {
			return nil, tagErr(path, fmt.Sprintf("%s: %q is not a valid integer", name, value))
		}
		return i, nil
	case reflect.Float32, reflect.Float64:
		f, err := strconv.ParseFloat(value, 64)
		if err != nil {
			return nil, tagErr(path, fmt.Sprintf("%s: %q is not a valid number", name, value))
		}
		return f, nil
	}
	// For slices/arrays of scalars the caller already passes the element
	// type through [elemTypeForList]; struct/map/etc. fall back to string.
	return value, nil
}

// coerceNumericValue is like [coerceFieldValue] but specialized for the
// numeric-only tag options (minimum / maximum / multipleOf / exclusive*).
// Strict: target must be an int or float field; otherwise the option errors.
func coerceNumericValue(ft reflect.Type, value string, hasValue bool, path, name string) (any, error) {
	if !hasValue {
		return nil, tagErr(path, name+": requires a value")
	}
	if ft == nil {
		// type-less context: parse loosely as float.
		f, err := strconv.ParseFloat(value, 64)
		if err != nil {
			return nil, tagErr(path, fmt.Sprintf("%s: %q is not a valid number", name, value))
		}
		return f, nil
	}
	t := ft
	for t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	switch t.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		i, err := strconv.ParseInt(value, 10, 64)
		if err != nil {
			return nil, tagErr(path, fmt.Sprintf("%s: %q is not a valid integer", name, value))
		}
		return i, nil
	case reflect.Float32, reflect.Float64:
		f, err := strconv.ParseFloat(value, 64)
		if err != nil {
			return nil, tagErr(path, fmt.Sprintf("%s: %q is not a valid number", name, value))
		}
		return f, nil
	}
	return nil, tagErr(path, fmt.Sprintf("%s applies only to numeric fields (field is %s)", name, t.Kind()))
}
