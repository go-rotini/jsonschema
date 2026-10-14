package generate

import (
	"reflect"
	"slices"
	"strings"
	"unicode"
)

// jsonField is one struct field that encoding/json encodes, found by
// [jsonFields].
type jsonField struct {
	name   string
	tagged bool
	// index is the field's index sequence from the outer struct; its length
	// is the embedding depth.
	index []int
	sf    reflect.StructField
	// owner is the struct type that declares sf.
	owner reflect.Type
	// opts is the comma-separated option list of the field's json tag.
	opts string
	// quoted is true when the ",string" option applies to the field's kind.
	quoted bool
	// path is the reflection path used in error messages.
	path string
}

// embeddedStruct is an embedded struct type whose fields [jsonFields]
// promotes at the next depth.
type embeddedStruct struct {
	typ   reflect.Type
	index []int
	path  string
}

// jsonFields returns the fields encoding/json encodes for struct type t, in
// index order. It follows encoding/json's rules: fields of embedded structs
// are promoted (also from unexported embedded struct types), each embedded
// type is visited once, a shallower field hides deeper fields with the same
// name, and fields with the same name at the same depth cancel out unless
// exactly one of them is named by a json tag.
func jsonFields(t reflect.Type, path string) []jsonField {
	var fields []jsonField
	next := []embeddedStruct{{typ: t, path: path}}
	var count, nextCount map[reflect.Type]int
	visited := map[reflect.Type]bool{}
	for len(next) > 0 {
		current := next
		next = nil
		count, nextCount = nextCount, map[reflect.Type]int{}
		for _, e := range current {
			if visited[e.typ] {
				continue
			}
			visited[e.typ] = true
			for i := range e.typ.NumField() {
				f, embed, ok := fieldAt(e, i)
				switch {
				case !ok:
				case embed != nil:
					nextCount[embed.typ]++
					if nextCount[embed.typ] == 1 {
						next = append(next, *embed)
					}
				default:
					fields = append(fields, f)
					if count[e.typ] > 1 {
						// The same embedded type appears more than once at
						// this depth; a duplicate makes the field cancel out.
						fields = append(fields, f)
					}
				}
			}
		}
	}
	return dominantFields(fields)
}

// fieldAt inspects field i of the embedded struct e. It returns the field
// when encoding/json encodes it directly, or the embedded struct whose
// fields are promoted, or ok=false when the field is not encoded.
func fieldAt(e embeddedStruct, i int) (jsonField, *embeddedStruct, bool) {
	sf := e.typ.Field(i)
	if sf.Anonymous {
		et := sf.Type
		if et.Kind() == reflect.Pointer {
			et = et.Elem()
		}
		if !sf.IsExported() && et.Kind() != reflect.Struct {
			return jsonField{}, nil, false
		}
	} else if !sf.IsExported() {
		return jsonField{}, nil, false
	}
	tag := sf.Tag.Get("json")
	if tag == "-" {
		return jsonField{}, nil, false
	}
	name, opts := parseJSONTag(tag)
	if !isValidJSONTagName(name) {
		name = ""
	}
	index := append(slices.Clip(e.index), i)
	ft := sf.Type
	if ft.Name() == "" && ft.Kind() == reflect.Pointer {
		ft = ft.Elem()
	}
	fieldPath := e.path + "." + sf.Name
	if name == "" && sf.Anonymous && ft.Kind() == reflect.Struct {
		return jsonField{}, &embeddedStruct{typ: ft, index: index, path: fieldPath}, true
	}
	f := jsonField{
		name:   name,
		tagged: name != "",
		index:  index,
		sf:     sf,
		owner:  e.typ,
		opts:   opts,
		quoted: hasJSONTagOption(opts, "string") && quotableKind(ft.Kind()),
		path:   fieldPath,
	}
	if f.name == "" {
		f.name = sf.Name
	}
	return f, nil, true
}

// dominantFields keeps, for each JSON name, the field encoding/json encodes
// under it (the shallowest, with a tagged field beating an untagged one at
// the same depth) and drops names that stay ambiguous. The result is in
// index order.
func dominantFields(fields []jsonField) []jsonField {
	slices.SortStableFunc(fields, func(a, b jsonField) int {
		if c := strings.Compare(a.name, b.name); c != 0 {
			return c
		}
		if c := len(a.index) - len(b.index); c != 0 {
			return c
		}
		if a.tagged != b.tagged {
			if a.tagged {
				return -1
			}
			return 1
		}
		return slices.Compare(a.index, b.index)
	})
	out := fields[:0]
	for i := 0; i < len(fields); {
		j := i + 1
		for j < len(fields) && fields[j].name == fields[i].name {
			j++
		}
		group := fields[i:j]
		if len(group) == 1 || len(group[0].index) != len(group[1].index) || group[0].tagged != group[1].tagged {
			out = append(out, group[0])
		}
		i = j
	}
	slices.SortFunc(out, func(a, b jsonField) int { return slices.Compare(a.index, b.index) })
	return out
}

// quotableKind reports whether the json ",string" option applies to a field
// of kind k.
func quotableKind(k reflect.Kind) bool {
	switch k {
	case reflect.Bool, reflect.String,
		reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr,
		reflect.Float32, reflect.Float64:
		return true
	}
	return false
}

// isValidJSONTagName reports whether encoding/json accepts s as a json tag
// name; an invalid name falls back to the Go field name.
func isValidJSONTagName(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		switch {
		case strings.ContainsRune("!#$%&()*+-./:;<=>?@[]^_{|}~ ", c):
		case !unicode.IsLetter(c) && !unicode.IsDigit(c):
			return false
		}
	}
	return true
}

// collectStructFields populates props and required from the fields
// encoding/json encodes for struct type t (see [jsonFields]).
func (c *genCtx) collectStructFields(t reflect.Type, path string, props *orderedMap, required *[]any, requiredSeen map[string]bool) error {
	for _, f := range jsonFields(t, path) {
		schemaTag, _ := f.sf.Tag.Lookup("jsonschema")
		spec, err := parseJSONSchemaTag(schemaTag, f.sf.Type, f.path)
		if err != nil {
			return err
		}

		var fieldSchema any
		switch {
		case spec.hasRef:
			refMap := newOrderedMap()
			refMap.set("$ref", spec.ref)
			fieldSchema = refMap
		default:
			if q, ok := c.quotedSchema(f); ok {
				fieldSchema = q
			} else if fieldSchema, err = c.schemaForType(f.sf.Type, f.path); err != nil {
				return err
			}
			omits := hasJSONTagOption(f.opts, "omitempty") || hasJSONTagOption(f.opts, "omitzero")
			switch f.sf.Type.Kind() {
			case reflect.Slice, reflect.Map:
				if omits {
					dropNull(fieldSchema)
				}
			case reflect.Pointer:
				fieldSchema = c.omitsNilPointer(fieldSchema, omits)
			}
		}
		fieldSchema = c.applyTagSpecToSchema(fieldSchema, &spec, f.owner, f.sf, f.path)

		props.set(f.name, fieldSchema)

		if spec.hasReq && !hasJSONTagOption(f.opts, "omitempty") && !requiredSeen[f.name] {
			requiredSeen[f.name] = true
			*required = append(*required, f.name)
		}
	}
	return nil
}

// Patterns for the string forms encoding/json writes under ",string".
const (
	quotedIntPattern   = `^-?(0|[1-9][0-9]*)$`
	quotedUintPattern  = `^(0|[1-9][0-9]*)$`
	quotedFloatPattern = `^-?(0|[1-9][0-9]*)(\.[0-9]+)?([eE][+-]?[0-9]+)?$`
)

// quotedSchema returns the schema for a field encoded with the json ",string"
// option: a string holding the value's JSON form. It reports false when the
// option has no effect, because the field's type has a custom emitter or its
// own marshaler.
func (c *genCtx) quotedSchema(f jsonField) (any, bool) {
	if !f.quoted {
		return nil, false
	}
	t := f.sf.Type
	ptr := false
	if t.Name() == "" && t.Kind() == reflect.Pointer {
		t = t.Elem()
		ptr = true
	}
	if _, ok := c.g.opts.customEmitters[t]; ok {
		return nil, false
	}
	if t.Implements(jsonMarshalerType) || reflect.PointerTo(t).Implements(jsonMarshalerType) ||
		t.Implements(textMarshalerType) || reflect.PointerTo(t).Implements(textMarshalerType) {
		return nil, false
	}
	m := newOrderedMap()
	m.set("type", "string")
	switch t.Kind() {
	case reflect.Bool:
		m.set("enum", []any{"true", "false"})
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		m.set("pattern", quotedIntPattern)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		m.set("pattern", quotedUintPattern)
	case reflect.Float32, reflect.Float64:
		m.set("pattern", quotedFloatPattern)
	}
	if ptr {
		return c.nullablePointer(m, f.path), true
	}
	return m, true
}
