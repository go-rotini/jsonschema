package jsonschema

import (
	"encoding/json"
	"fmt"
	"math"
	"math/big"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"
)

type typeEval struct {
	loc     string
	allowed []string
	// msg is the error message for a value of none of the allowed types.
	msg string
	// draft4 selects Draft 4's integer rule (see isDraft4Integer).
	draft4 bool
	// one backs allowed when the keyword names a single type.
	one [1]string
}

func (e *typeEval) keyword() string { return "type" }

func (e *typeEval) eval(ctx *runCtx, instance any) {
	for _, t := range e.allowed {
		if t == "integer" && e.draft4 {
			if isDraft4Integer(instance) {
				return
			}
			continue
		}
		if matchesType(instance, t) {
			return
		}
	}
	ctx.addError(e.loc, "type", "", e.msg)
}

func matchesType(v any, t string) bool {
	switch t {
	case "null":
		return v == nil
	case "boolean":
		_, ok := v.(bool)
		return ok
	case "string":
		_, ok := v.(string)
		return ok
	case "array":
		_, ok := v.([]any)
		return ok
	case "object":
		_, ok := v.(map[string]any)
		return ok
	case "number":
		return isJSONNumber(v)
	case "integer":
		return isJSONInteger(v)
	}
	return false
}

// isJSONNumber reports whether v is one of the Go types that hold a JSON
// number: float64, float32, [Number], every integer kind and a non-nil
// *[big.Int].
func isJSONNumber(v any) bool {
	switch n := v.(type) {
	case json.Number, float64, float32,
		int, int8, int16, int32, int64,
		uint, uint8, uint16, uint32, uint64:
		return true
	case *big.Int:
		return n != nil
	}
	return false
}

// isJSONInteger reports whether v is a number with no fractional part (1.0
// counts).
func isJSONInteger(v any) bool {
	d, ok := parseNumber(v)
	return ok && d.isInteger()
}

// isDraft4Integer applies Draft 4's stricter rule: number text with a
// fraction or exponent part is not an integer, even when its value is.
func isDraft4Integer(v any) bool {
	if n, ok := v.(json.Number); ok && strings.ContainsAny(string(n), ".eE") {
		return false
	}
	return isJSONInteger(v)
}

func registerTypeKeyword() {
	registerEvaluator("type", buildType)
}

func buildType(_ *evalBuilder, f *buildFrame, raw any, loc string) (evaluator, error) {
	var allowed []string
	switch t := raw.(type) {
	case string:
		e := &typeEval{loc: loc, one: [1]string{t}, msg: "value is not of type " + t, draft4: f != nil && f.draft == Draft4}
		e.allowed = e.one[:]
		return e, nil
	case []any:
		for _, x := range t {
			if s, ok := x.(string); ok {
				allowed = append(allowed, s)
			}
		}
	default:
		return nil, &CompileError{KeywordLocation: loc, Message: "type must be a string or array of strings"}
	}
	return &typeEval{loc: loc, allowed: allowed, msg: "value is not of type " + strings.Join(allowed, "/"), draft4: f != nil && f.draft == Draft4}, nil
}

type enumEval struct {
	loc  string
	vals []any
}

func (e *enumEval) keyword() string { return "enum" }

func (e *enumEval) eval(ctx *runCtx, instance any) {
	for _, v := range e.vals {
		if canonicalEqual(instance, v) {
			return
		}
	}
	ctx.addError(e.loc, "enum", "", "value is not in enum")
}

type constEval struct {
	loc string
	val any
}

func (e *constEval) keyword() string { return "const" }

func (e *constEval) eval(ctx *runCtx, instance any) {
	if !canonicalEqual(instance, e.val) {
		ctx.addError(e.loc, "const", "", "value does not match const")
	}
}

func registerEnumConstKeywords() {
	registerEvaluator("enum", func(_ *evalBuilder, _ *buildFrame, raw any, loc string) (evaluator, error) {
		arr, ok := raw.([]any)
		if !ok {
			return nil, &CompileError{KeywordLocation: loc, Message: "enum must be an array"}
		}
		return &enumEval{loc: loc, vals: arr}, nil
	})
	registerEvaluator("const", func(_ *evalBuilder, _ *buildFrame, raw any, loc string) (evaluator, error) {
		return &constEval{loc: loc, val: raw}, nil
	})
}

// canonicalEqual reports whether a and b represent the same JSON value.
// Numbers compare by value, so 1 == 1.0 == 1e0.
func canonicalEqual(a, b any) bool {
	if isJSONNumber(a) && isJSONNumber(b) {
		da, oka := parseNumber(a)
		db, okb := parseNumber(b)
		if oka && okb {
			return da.equal(db)
		}
	}
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	switch av := a.(type) {
	case bool:
		bv, ok := b.(bool)
		return ok && av == bv
	case string:
		bv, ok := b.(string)
		return ok && av == bv
	case []any:
		bv, ok := b.([]any)
		if !ok || len(av) != len(bv) {
			return false
		}
		for i := range av {
			if !canonicalEqual(av[i], bv[i]) {
				return false
			}
		}
		return true
	case map[string]any:
		bv, ok := b.(map[string]any)
		if !ok || len(av) != len(bv) {
			return false
		}
		for k, va := range av {
			vb, ok := bv[k]
			if !ok {
				return false
			}
			if !canonicalEqual(va, vb) {
				return false
			}
		}
		return true
	}
	return reflect.DeepEqual(a, b)
}

// numberText returns the source text of a schema number for messages.
func numberText(v any) string {
	if n, ok := v.(json.Number); ok {
		return string(n)
	}
	return fmt.Sprint(v)
}

type multipleOfEval struct {
	loc  string
	val  divisor
	text string
}

func (e *multipleOfEval) keyword() string { return "multipleOf" }

func (e *multipleOfEval) eval(ctx *runCtx, instance any) {
	if !isJSONNumber(instance) {
		return
	}
	x, ok := parseNumber(instance)
	if !ok {
		ctx.addError(e.loc, "multipleOf", "", "value is not a valid number")
		return
	}
	multiple, ok := e.val.divides(x)
	switch {
	case !ok:
		ctx.addError(e.loc, "multipleOf", "", fmt.Sprintf("value cannot be checked as a multiple of %s", e.text))
	case !multiple:
		ctx.addError(e.loc, "multipleOf", "", fmt.Sprintf("value is not a multiple of %s", e.text))
	}
}

type rangeEval struct {
	loc       string
	kw        string
	val       decimal
	text      string
	exclusive bool
	upper     bool // true for max{,Exclusive}; false for min{,Exclusive}
}

func (e *rangeEval) keyword() string { return e.kw }

func (e *rangeEval) eval(ctx *runCtx, instance any) {
	if !isJSONNumber(instance) {
		return
	}
	x, ok := parseNumber(instance)
	if !ok {
		ctx.addError(e.loc, e.kw, "", "value is not a valid number")
		return
	}
	cmp, ok := x.cmp(e.val)
	if !ok {
		ctx.addError(e.loc, e.kw, "", fmt.Sprintf("value cannot be compared with %s", e.text))
		return
	}
	if e.upper {
		if e.exclusive {
			if cmp >= 0 {
				ctx.addError(e.loc, e.kw, "", fmt.Sprintf("value must be < %s", e.text))
			}
		} else if cmp > 0 {
			ctx.addError(e.loc, e.kw, "", fmt.Sprintf("value must be <= %s", e.text))
		}
	} else {
		if e.exclusive {
			if cmp <= 0 {
				ctx.addError(e.loc, e.kw, "", fmt.Sprintf("value must be > %s", e.text))
			}
		} else if cmp < 0 {
			ctx.addError(e.loc, e.kw, "", fmt.Sprintf("value must be >= %s", e.text))
		}
	}
}

// schemaNumber parses a numeric keyword value, rejecting NaN and infinities.
func schemaNumber(raw any) (decimal, bool) {
	d, ok := parseNumber(raw)
	if !ok || d.nan || d.inf {
		return decimal{}, false
	}
	return d, true
}

// buildRange returns the builder for maximum or minimum. In Draft 4 a
// sibling "exclusiveMaximum": true (or "exclusiveMinimum": true) makes the
// bound exclusive.
func buildRange(kw, exclusiveKw string, upper bool) builderFn {
	return func(_ *evalBuilder, f *buildFrame, raw any, loc string) (evaluator, error) {
		d, ok := schemaNumber(raw)
		if !ok {
			return nil, &CompileError{KeywordLocation: loc, Message: kw + " must be a number"}
		}
		exclusive := false
		if parent, ok := f.parent.(map[string]any); ok && f.draft <= Draft4 {
			exclusive, _ = parent[exclusiveKw].(bool)
		}
		return &rangeEval{loc: loc, kw: kw, val: d, text: numberText(raw), upper: upper, exclusive: exclusive}, nil
	}
}

// buildExclusiveRange returns the builder for exclusiveMaximum or
// exclusiveMinimum. The Draft 4 boolean form only modifies the sibling
// maximum or minimum, so it has no evaluator of its own.
func buildExclusiveRange(kw string, upper bool) builderFn {
	return func(_ *evalBuilder, f *buildFrame, raw any, loc string) (evaluator, error) {
		if _, ok := raw.(bool); ok && f.draft <= Draft4 {
			// The Draft 4 boolean form is applied by the maximum / minimum builder.
			return nil, nil
		}
		d, ok := schemaNumber(raw)
		if !ok {
			return nil, &CompileError{KeywordLocation: loc, Message: kw + " must be a number"}
		}
		return &rangeEval{loc: loc, kw: kw, val: d, text: numberText(raw), upper: upper, exclusive: true}, nil
	}
}

func registerNumericKeywords() {
	registerEvaluator("multipleOf", func(_ *evalBuilder, _ *buildFrame, raw any, loc string) (evaluator, error) {
		d, ok := schemaNumber(raw)
		if !ok || d.huge != 0 {
			return nil, &CompileError{KeywordLocation: loc, Message: "multipleOf must be a number"}
		}
		if d.sign() <= 0 {
			return nil, &CompileError{KeywordLocation: loc, Message: "multipleOf must be > 0"}
		}
		return &multipleOfEval{loc: loc, val: newDivisor(d), text: numberText(raw)}, nil
	})
	registerEvaluator("maximum", buildRange("maximum", "exclusiveMaximum", true))
	registerEvaluator("minimum", buildRange("minimum", "exclusiveMinimum", false))
	registerEvaluator("exclusiveMaximum", buildExclusiveRange("exclusiveMaximum", true))
	registerEvaluator("exclusiveMinimum", buildExclusiveRange("exclusiveMinimum", false))
}

type lengthEval struct {
	loc   string
	kw    string
	bound int
	upper bool
}

func (e *lengthEval) keyword() string { return e.kw }

func (e *lengthEval) eval(ctx *runCtx, instance any) {
	s, ok := instance.(string)
	if !ok {
		return
	}
	n := utf8.RuneCountInString(s)
	if e.upper && n > e.bound {
		ctx.addError(e.loc, e.kw, "", fmt.Sprintf("string length %d exceeds %s %d", n, e.kw, e.bound))
	}
	if !e.upper && n < e.bound {
		ctx.addError(e.loc, e.kw, "", fmt.Sprintf("string length %d is below %s %d", n, e.kw, e.bound))
	}
}

type patternEval struct {
	loc string
	re  *regexp.Regexp
	src string
}

func (e *patternEval) keyword() string { return "pattern" }

func (e *patternEval) eval(ctx *runCtx, instance any) {
	s, ok := instance.(string)
	if !ok {
		return
	}
	if !e.re.MatchString(s) {
		ctx.addError(e.loc, "pattern", "", fmt.Sprintf("value does not match pattern %q", e.src))
	}
}

func registerStringKeywords() {
	registerEvaluator("maxLength", func(_ *evalBuilder, _ *buildFrame, raw any, loc string) (evaluator, error) {
		n, ok := toInt(raw)
		if !ok || n < 0 {
			return nil, &CompileError{KeywordLocation: loc, Message: "maxLength must be a non-negative integer"}
		}
		return &lengthEval{loc: loc, kw: "maxLength", bound: n, upper: true}, nil
	})
	registerEvaluator("minLength", func(_ *evalBuilder, _ *buildFrame, raw any, loc string) (evaluator, error) {
		n, ok := toInt(raw)
		if !ok || n < 0 {
			return nil, &CompileError{KeywordLocation: loc, Message: "minLength must be a non-negative integer"}
		}
		return &lengthEval{loc: loc, kw: "minLength", bound: n, upper: false}, nil
	})
	registerEvaluator("pattern", func(b *evalBuilder, _ *buildFrame, raw any, loc string) (evaluator, error) {
		s, ok := raw.(string)
		if !ok {
			return nil, &CompileError{KeywordLocation: loc, Message: "pattern must be a string"}
		}
		re, err := b.compileRegex(s)
		if err != nil {
			return nil, &CompileError{KeywordLocation: loc, Message: "invalid pattern", Cause: err}
		}
		return &patternEval{loc: loc, re: re, src: s}, nil
	})
}

// toInt converts an integral JSON number to an int. Values beyond the int
// range are clamped to math.MaxInt or math.MinInt: as a length or count
// bound no instance can reach them, so the clamp keeps their meaning.
func toInt(raw any) (int, bool) {
	d, ok := parseNumber(raw)
	if !ok || !d.isInteger() {
		return 0, false
	}
	if d.isZero() {
		return 0, true
	}
	limit := math.MaxInt
	if d.neg {
		limit = math.MinInt
	}
	if d.huge != 0 || d.exp+int64(len(d.digits)) > 19 {
		return limit, true
	}
	text := d.digits + strings.Repeat("0", int(d.exp))
	if d.neg {
		text = "-" + text
	}
	n, err := strconv.ParseInt(text, 10, strconv.IntSize)
	if err != nil {
		return limit, true
	}
	return int(n), true
}

type itemsCountEval struct {
	loc   string
	kw    string
	bound int
	upper bool
}

func (e *itemsCountEval) keyword() string { return e.kw }

func (e *itemsCountEval) eval(ctx *runCtx, instance any) {
	arr, ok := instance.([]any)
	if !ok {
		return
	}
	n := len(arr)
	if e.upper && n > e.bound {
		ctx.addError(e.loc, e.kw, "", fmt.Sprintf("array length %d exceeds %s %d", n, e.kw, e.bound))
	}
	if !e.upper && n < e.bound {
		ctx.addError(e.loc, e.kw, "", fmt.Sprintf("array length %d is below %s %d", n, e.kw, e.bound))
	}
}

type uniqueItemsEval struct {
	loc string
	on  bool
}

func (e *uniqueItemsEval) keyword() string { return "uniqueItems" }

func (e *uniqueItemsEval) eval(ctx *runCtx, instance any) {
	if !e.on {
		return
	}
	arr, ok := instance.([]any)
	if !ok {
		return
	}
	if i, j, dup := firstDuplicate(arr); dup {
		ctx.addError(e.loc, "uniqueItems", "", fmt.Sprintf("array items %d and %d are not unique", i, j))
	}
}

// firstDuplicate returns the first pair i < j, in (i, j) order, of equal
// items in arr. Items are compared through their canonical keys, so the
// cost is linear in the size of arr. Only items holding a Go type outside
// the JSON value shapes have no key; such an item can equal only another
// one like it, so those few are compared with each other pairwise.
func firstDuplicate(arr []any) (int, int, bool) {
	first := make(map[string]int, len(arr))
	bi, bj := -1, -1
	var sb strings.Builder
	var unkeyed []int
	nan := 0
	for j, item := range arr {
		sb.Reset()
		if !canonicalKey(&sb, item, &nan) {
			unkeyed = append(unkeyed, j)
			continue
		}
		k := sb.String()
		i, seen := first[k]
		if !seen {
			first[k] = j
			continue
		}
		// For a given i the first j found is the smallest.
		if bi < 0 || i < bi {
			bi, bj = i, j
		}
	}
	for x, i := range unkeyed {
		if bi >= 0 && i > bi {
			break
		}
		for _, j := range unkeyed[x+1:] {
			if canonicalEqual(arr[i], arr[j]) {
				return i, j, true
			}
		}
	}
	return bi, bj, bi >= 0
}

// canonicalKey writes a key for v to sb such that two values have the same
// key exactly when [canonicalEqual] holds. NaN equals nothing, so each NaN
// gets a key of its own, numbered through nan. It reports false for a Go
// type outside the JSON value shapes.
func canonicalKey(sb *strings.Builder, v any, nan *int) bool {
	switch t := v.(type) {
	case nil:
		sb.WriteByte('z')
	case bool:
		if t {
			sb.WriteByte('t')
		} else {
			sb.WriteByte('f')
		}
	case string:
		sb.WriteByte('s')
		sb.WriteString(strconv.Itoa(len(t)))
		sb.WriteByte(':')
		sb.WriteString(t)
	case []any:
		sb.WriteByte('[')
		for _, item := range t {
			if !canonicalKey(sb, item, nan) {
				return false
			}
		}
		sb.WriteByte(']')
	case map[string]any:
		sb.WriteByte('{')
		for _, k := range orderKeys(t) {
			sb.WriteString(strconv.Itoa(len(k)))
			sb.WriteByte(':')
			sb.WriteString(k)
			if !canonicalKey(sb, t[k], nan) {
				return false
			}
		}
		sb.WriteByte('}')
	default:
		if !isJSONNumber(t) {
			return false
		}
		d, ok := parseNumber(t)
		switch {
		case !ok:
			// Number text that does not parse equals only the same text.
			n, _ := t.(json.Number)
			s := string(n)
			sb.WriteByte('u')
			sb.WriteString(strconv.Itoa(len(s)))
			sb.WriteByte(':')
			sb.WriteString(s)
		case d.nan:
			sb.WriteByte('N')
			sb.WriteString(strconv.Itoa(*nan))
			sb.WriteByte(';')
			*nan++
		default:
			sb.WriteByte('n')
			d.key(sb)
			sb.WriteByte(';')
		}
	}
	return true
}

func registerArrayKeywords() {
	registerEvaluator("maxItems", func(_ *evalBuilder, _ *buildFrame, raw any, loc string) (evaluator, error) {
		n, ok := toInt(raw)
		if !ok || n < 0 {
			return nil, &CompileError{KeywordLocation: loc, Message: "maxItems must be a non-negative integer"}
		}
		return &itemsCountEval{loc: loc, kw: "maxItems", bound: n, upper: true}, nil
	})
	registerEvaluator("minItems", func(_ *evalBuilder, _ *buildFrame, raw any, loc string) (evaluator, error) {
		n, ok := toInt(raw)
		if !ok || n < 0 {
			return nil, &CompileError{KeywordLocation: loc, Message: "minItems must be a non-negative integer"}
		}
		return &itemsCountEval{loc: loc, kw: "minItems", bound: n, upper: false}, nil
	})
	registerEvaluator("uniqueItems", func(_ *evalBuilder, _ *buildFrame, raw any, loc string) (evaluator, error) {
		b, ok := raw.(bool)
		if !ok {
			return nil, &CompileError{KeywordLocation: loc, Message: "uniqueItems must be a boolean"}
		}
		return &uniqueItemsEval{loc: loc, on: b}, nil
	})
}

type propsCountEval struct {
	loc   string
	kw    string
	bound int
	upper bool
}

func (e *propsCountEval) keyword() string { return e.kw }

func (e *propsCountEval) eval(ctx *runCtx, instance any) {
	obj, ok := instance.(map[string]any)
	if !ok {
		return
	}
	n := len(obj)
	if e.upper && n > e.bound {
		ctx.addError(e.loc, e.kw, "", fmt.Sprintf("property count %d exceeds %s %d", n, e.kw, e.bound))
	}
	if !e.upper && n < e.bound {
		ctx.addError(e.loc, e.kw, "", fmt.Sprintf("property count %d is below %s %d", n, e.kw, e.bound))
	}
}

type requiredEval struct {
	loc  string
	keys []string
	// readOnlyKeys / writeOnlyKeys collect properties whose subschema
	// declared "readOnly": true / "writeOnly": true at compile time, so
	// the corresponding direction option ([WithWriteOnly] / [WithReadOnly])
	// can skip them.
	readOnlyKeys  map[string]struct{}
	writeOnlyKeys map[string]struct{}
}

func (e *requiredEval) keyword() string { return "required" }

func (e *requiredEval) eval(ctx *runCtx, instance any) {
	obj, ok := instance.(map[string]any)
	if !ok {
		return
	}
	for _, k := range e.keys {
		if ctx.shouldStop() {
			return
		}
		if ctx.opts.writeOnly {
			if _, ro := e.readOnlyKeys[k]; ro {
				continue
			}
		}
		if ctx.opts.readOnly {
			if _, wo := e.writeOnlyKeys[k]; wo {
				continue
			}
		}
		if _, present := obj[k]; !present {
			ctx.addError(e.loc, "required", "", fmt.Sprintf("missing required property %q", k))
		}
	}
}

type dependentRequiredEval struct {
	loc  string
	keys []string // sorted, so errors come in a stable order
	deps map[string][]string
}

func (e *dependentRequiredEval) keyword() string { return "dependentRequired" }

func (e *dependentRequiredEval) eval(ctx *runCtx, instance any) {
	obj, ok := instance.(map[string]any)
	if !ok {
		return
	}
	for _, k := range e.keys {
		if _, present := obj[k]; !present {
			continue
		}
		for _, r := range e.deps[k] {
			if _, p := obj[r]; !p {
				ctx.addError(e.loc, "dependentRequired", "", fmt.Sprintf("property %q requires %q", k, r))
			}
		}
	}
}

func registerObjectKeywords() {
	registerEvaluator("maxProperties", func(_ *evalBuilder, _ *buildFrame, raw any, loc string) (evaluator, error) {
		n, ok := toInt(raw)
		if !ok || n < 0 {
			return nil, &CompileError{KeywordLocation: loc, Message: "maxProperties must be a non-negative integer"}
		}
		return &propsCountEval{loc: loc, kw: "maxProperties", bound: n, upper: true}, nil
	})
	registerEvaluator("minProperties", func(_ *evalBuilder, _ *buildFrame, raw any, loc string) (evaluator, error) {
		n, ok := toInt(raw)
		if !ok || n < 0 {
			return nil, &CompileError{KeywordLocation: loc, Message: "minProperties must be a non-negative integer"}
		}
		return &propsCountEval{loc: loc, kw: "minProperties", bound: n, upper: false}, nil
	})
	registerEvaluator("required", func(_ *evalBuilder, f *buildFrame, raw any, loc string) (evaluator, error) {
		arr, ok := raw.([]any)
		if !ok {
			return nil, &CompileError{KeywordLocation: loc, Message: "required must be an array"}
		}
		keys := make([]string, 0, len(arr))
		for _, v := range arr {
			s, ok := v.(string)
			if !ok {
				return nil, &CompileError{KeywordLocation: loc, Message: "required entries must be strings"}
			}
			keys = append(keys, s)
		}
		ro, wo := scanDirectionalAnnotations(f)
		return &requiredEval{loc: loc, keys: keys, readOnlyKeys: ro, writeOnlyKeys: wo}, nil
	})
	registerEvaluator("dependentRequired", func(_ *evalBuilder, _ *buildFrame, raw any, loc string) (evaluator, error) {
		m, ok := raw.(map[string]any)
		if !ok {
			return nil, &CompileError{KeywordLocation: loc, Message: "dependentRequired must be an object"}
		}
		deps := map[string][]string{}
		for k, v := range m {
			arr, ok := v.([]any)
			if !ok {
				return nil, &CompileError{KeywordLocation: loc, Message: "dependentRequired entries must be arrays"}
			}
			vals := make([]string, 0, len(arr))
			for _, item := range arr {
				s, ok := item.(string)
				if !ok {
					return nil, &CompileError{KeywordLocation: loc, Message: "dependentRequired entries must contain strings"}
				}
				vals = append(vals, s)
			}
			deps[k] = vals
		}
		return &dependentRequiredEval{loc: loc, keys: orderKeys(m), deps: deps}, nil
	})
}

// itoaInt wraps strconv.Itoa so the eval files share one helper.
func itoaInt(i int) string { return strconv.Itoa(i) }

// scanDirectionalAnnotations collects property names flagged with
// "readOnly": true or "writeOnly": true in the parent's properties map.
// Both return values are nil when no flagged property is found, so the
// evaluator's hot path skips the lookup entirely.
func scanDirectionalAnnotations(f *buildFrame) (map[string]struct{}, map[string]struct{}) {
	parent, ok := f.parent.(map[string]any)
	if !ok {
		return nil, nil
	}
	rawProps, ok := parent["properties"]
	if !ok {
		return nil, nil
	}
	propMap, ok := rawProps.(map[string]any)
	if !ok {
		return nil, nil
	}
	var readOnly, writeOnly map[string]struct{}
	for name, raw := range propMap {
		schema, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if v, ok := schema["readOnly"]; ok {
			if flag, ok := v.(bool); ok && flag {
				if readOnly == nil {
					readOnly = map[string]struct{}{}
				}
				readOnly[name] = struct{}{}
			}
		}
		if v, ok := schema["writeOnly"]; ok {
			if flag, ok := v.(bool); ok && flag {
				if writeOnly == nil {
					writeOnly = map[string]struct{}{}
				}
				writeOnly[name] = struct{}{}
			}
		}
	}
	return readOnly, writeOnly
}
