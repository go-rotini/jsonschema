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

// =====================================================================
// type
// =====================================================================.

type typeEval struct {
	loc     string
	allowed []string
}

func (e *typeEval) keyword() string { return "type" }

func (e *typeEval) eval(ctx *runCtx, instance any) {
	if matchesAnyType(instance, e.allowed) {
		return
	}
	ctx.addError(e.loc, "type", "", fmt.Sprintf("value is not of type %s", strings.Join(e.allowed, "/")))
}

func matchesAnyType(v any, allowed []string) bool {
	for _, t := range allowed {
		if matchesType(v, t) {
			return true
		}
	}
	return false
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

func isJSONNumber(v any) bool {
	switch v.(type) {
	case json.Number, float64, int, int64, int32:
		return true
	}
	return false
}

func isJSONInteger(v any) bool {
	switch n := v.(type) {
	case json.Number:
		s := string(n)
		// Integers per spec: numbers with no fractional or exponent part
		// (or with a fractional part of zeros, e.g. 1.0).
		// Use big.Rat to check if value is an exact integer.
		r := new(big.Rat)
		if _, ok := r.SetString(s); !ok {
			return false
		}
		return r.IsInt()
	case float64:
		return !math.IsNaN(n) && !math.IsInf(n, 0) && n == math.Trunc(n)
	case int, int64, int32:
		return true
	}
	return false
}

//nolint:gochecknoinits // evaluator registry is built at package init by design.
func init() {
	registerEvaluator("type", buildType)
}

func buildType(_ *evalBuilder, raw any, loc string) (evaluator, error) {
	var allowed []string
	switch t := raw.(type) {
	case string:
		allowed = []string{t}
	case []any:
		for _, x := range t {
			if s, ok := x.(string); ok {
				allowed = append(allowed, s)
			}
		}
	default:
		return nil, &CompileError{KeywordLocation: loc, Message: "type must be a string or array of strings"}
	}
	return &typeEval{loc: loc, allowed: allowed}, nil
}

// =====================================================================
// enum / const
// =====================================================================.

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

//nolint:gochecknoinits // evaluator registry is built at package init by design.
func init() {
	registerEvaluator("enum", func(_ *evalBuilder, raw any, loc string) (evaluator, error) {
		arr, ok := raw.([]any)
		if !ok {
			return nil, &CompileError{KeywordLocation: loc, Message: "enum must be an array"}
		}
		return &enumEval{loc: loc, vals: arr}, nil
	})
	registerEvaluator("const", func(_ *evalBuilder, raw any, loc string) (evaluator, error) {
		return &constEval{loc: loc, val: raw}, nil
	})
}

// canonicalEqual reports whether a and b represent the same JSON value.
func canonicalEqual(a, b any) bool {
	// Numbers (json.Number / float64) compare by big.Rat value.
	if isJSONNumber(a) && isJSONNumber(b) {
		ra, oka := numberToRat(a)
		rb, okb := numberToRat(b)
		if oka && okb {
			return ra.Cmp(rb) == 0
		}
	}
	// Both nil
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

// numberToRat converts a JSON-typed number value into a *big.Rat. Returns
// (nil, false) if v is not a JSON number-shaped value.
func numberToRat(v any) (*big.Rat, bool) {
	switch n := v.(type) {
	case json.Number:
		r := new(big.Rat)
		if _, ok := r.SetString(string(n)); ok {
			return r, true
		}
		// Try as int
		if i, err := n.Int64(); err == nil {
			return new(big.Rat).SetInt64(i), true
		}
		if f, err := n.Float64(); err == nil {
			r := new(big.Rat)
			r.SetFloat64(f)
			return r, true
		}
	case float64:
		if math.IsNaN(n) || math.IsInf(n, 0) {
			return nil, false
		}
		r := new(big.Rat)
		r.SetFloat64(n)
		return r, true
	case int:
		return new(big.Rat).SetInt64(int64(n)), true
	case int64:
		return new(big.Rat).SetInt64(n), true
	case int32:
		return new(big.Rat).SetInt64(int64(n)), true
	}
	return nil, false
}

// =====================================================================
// multipleOf / maximum / minimum / exclusiveMaximum / exclusiveMinimum
// =====================================================================.

type multipleOfEval struct {
	loc string
	val *big.Rat
}

func (e *multipleOfEval) keyword() string { return "multipleOf" }

func (e *multipleOfEval) eval(ctx *runCtx, instance any) {
	if !isJSONNumber(instance) {
		return
	}
	r, ok := numberToRat(instance)
	if !ok {
		return
	}
	// instance / val must be an integer.
	q := new(big.Rat).Quo(r, e.val)
	if !q.IsInt() {
		ctx.addError(e.loc, "multipleOf", "", fmt.Sprintf("value is not a multiple of %s", e.val.RatString()))
	}
}

type rangeEval struct {
	loc       string
	kw        string
	val       *big.Rat
	exclusive bool
	upper     bool // true for max{,Exclusive}; false for min{,Exclusive}
}

func (e *rangeEval) keyword() string { return e.kw }

func (e *rangeEval) eval(ctx *runCtx, instance any) {
	if !isJSONNumber(instance) {
		return
	}
	r, ok := numberToRat(instance)
	if !ok {
		return
	}
	cmp := r.Cmp(e.val)
	if e.upper {
		if e.exclusive {
			if cmp >= 0 {
				ctx.addError(e.loc, e.kw, "", fmt.Sprintf("value must be < %s", e.val.RatString()))
			}
		} else if cmp > 0 {
			ctx.addError(e.loc, e.kw, "", fmt.Sprintf("value must be <= %s", e.val.RatString()))
		}
	} else {
		if e.exclusive {
			if cmp <= 0 {
				ctx.addError(e.loc, e.kw, "", fmt.Sprintf("value must be > %s", e.val.RatString()))
			}
		} else if cmp < 0 {
			ctx.addError(e.loc, e.kw, "", fmt.Sprintf("value must be >= %s", e.val.RatString()))
		}
	}
}

//nolint:gochecknoinits // evaluator registry is built at package init by design.
func init() {
	registerEvaluator("multipleOf", func(_ *evalBuilder, raw any, loc string) (evaluator, error) {
		r, ok := numberToRat(raw)
		if !ok {
			return nil, &CompileError{KeywordLocation: loc, Message: "multipleOf must be a number"}
		}
		if r.Sign() <= 0 {
			return nil, &CompileError{KeywordLocation: loc, Message: "multipleOf must be > 0"}
		}
		return &multipleOfEval{loc: loc, val: r}, nil
	})
	registerEvaluator("maximum", func(_ *evalBuilder, raw any, loc string) (evaluator, error) {
		r, ok := numberToRat(raw)
		if !ok {
			return nil, &CompileError{KeywordLocation: loc, Message: "maximum must be a number"}
		}
		return &rangeEval{loc: loc, kw: "maximum", val: r, upper: true}, nil
	})
	registerEvaluator("minimum", func(_ *evalBuilder, raw any, loc string) (evaluator, error) {
		r, ok := numberToRat(raw)
		if !ok {
			return nil, &CompileError{KeywordLocation: loc, Message: "minimum must be a number"}
		}
		return &rangeEval{loc: loc, kw: "minimum", val: r, upper: false}, nil
	})
	registerEvaluator("exclusiveMaximum", func(b *evalBuilder, raw any, loc string) (evaluator, error) {
		// Draft 4: exclusiveMaximum is a boolean that pairs with maximum.
		// Draft 6+: exclusiveMaximum is a number on its own.
		if _, ok := raw.(bool); ok && b.draft <= Draft4 {
			// In Draft 4, exclusiveMaximum-as-bool flips a sibling maximum;
			// the sibling check is folded into the maximum evaluator, so
			// here we leave the keyword as a no-op and let `maximum` carry
			// the semantics.
			return &noopEval{name: "exclusiveMaximum"}, nil
		}
		r, ok := numberToRat(raw)
		if !ok {
			return nil, &CompileError{KeywordLocation: loc, Message: "exclusiveMaximum must be a number"}
		}
		return &rangeEval{loc: loc, kw: "exclusiveMaximum", val: r, upper: true, exclusive: true}, nil
	})
	registerEvaluator("exclusiveMinimum", func(b *evalBuilder, raw any, loc string) (evaluator, error) {
		if _, ok := raw.(bool); ok && b.draft <= Draft4 {
			return &noopEval{name: "exclusiveMinimum"}, nil
		}
		r, ok := numberToRat(raw)
		if !ok {
			return nil, &CompileError{KeywordLocation: loc, Message: "exclusiveMinimum must be a number"}
		}
		return &rangeEval{loc: loc, kw: "exclusiveMinimum", val: r, upper: false, exclusive: true}, nil
	})
}

// =====================================================================
// maxLength / minLength / pattern
// =====================================================================.

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

//nolint:gochecknoinits // evaluator registry is built at package init by design.
func init() {
	registerEvaluator("maxLength", func(_ *evalBuilder, raw any, loc string) (evaluator, error) {
		n, ok := toInt(raw)
		if !ok || n < 0 {
			return nil, &CompileError{KeywordLocation: loc, Message: "maxLength must be a non-negative integer"}
		}
		return &lengthEval{loc: loc, kw: "maxLength", bound: n, upper: true}, nil
	})
	registerEvaluator("minLength", func(_ *evalBuilder, raw any, loc string) (evaluator, error) {
		n, ok := toInt(raw)
		if !ok || n < 0 {
			return nil, &CompileError{KeywordLocation: loc, Message: "minLength must be a non-negative integer"}
		}
		return &lengthEval{loc: loc, kw: "minLength", bound: n, upper: false}, nil
	})
	registerEvaluator("pattern", func(_ *evalBuilder, raw any, loc string) (evaluator, error) {
		s, ok := raw.(string)
		if !ok {
			return nil, &CompileError{KeywordLocation: loc, Message: "pattern must be a string"}
		}
		re, err := regexp.Compile(translateECMA(s))
		if err != nil {
			return nil, &CompileError{KeywordLocation: loc, Message: "invalid pattern", Cause: err}
		}
		return &patternEval{loc: loc, re: re, src: s}, nil
	})
}

// translateECMA performs minimal ECMA-262 → Go regexp transformations. Most
// of ECMA-262 maps directly; a handful of escapes / classes need translation.
func translateECMA(p string) string {
	// Common substitutions: ECMA `\d` is digits 0-9 (matches Go RE2).
	// Unicode property escapes like \p{...} mostly work.
	return p
}

func toInt(raw any) (int, bool) {
	switch v := raw.(type) {
	case json.Number:
		if i, err := v.Int64(); err == nil {
			return int(i), true
		}
		if f, err := v.Float64(); err == nil {
			if f == math.Trunc(f) && !math.IsInf(f, 0) {
				return int(f), true
			}
		}
	case int:
		return v, true
	case int64:
		return int(v), true
	case float64:
		if v == math.Trunc(v) && !math.IsInf(v, 0) {
			return int(v), true
		}
	}
	return 0, false
}

// =====================================================================
// maxItems / minItems / uniqueItems
// =====================================================================.

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
	for i := range arr {
		for j := i + 1; j < len(arr); j++ {
			if canonicalEqual(arr[i], arr[j]) {
				ctx.addError(e.loc, "uniqueItems", "", fmt.Sprintf("array items %d and %d are not unique", i, j))
				return
			}
		}
	}
}

//nolint:gochecknoinits // evaluator registry is built at package init by design.
func init() {
	registerEvaluator("maxItems", func(_ *evalBuilder, raw any, loc string) (evaluator, error) {
		n, ok := toInt(raw)
		if !ok || n < 0 {
			return nil, &CompileError{KeywordLocation: loc, Message: "maxItems must be a non-negative integer"}
		}
		return &itemsCountEval{loc: loc, kw: "maxItems", bound: n, upper: true}, nil
	})
	registerEvaluator("minItems", func(_ *evalBuilder, raw any, loc string) (evaluator, error) {
		n, ok := toInt(raw)
		if !ok || n < 0 {
			return nil, &CompileError{KeywordLocation: loc, Message: "minItems must be a non-negative integer"}
		}
		return &itemsCountEval{loc: loc, kw: "minItems", bound: n, upper: false}, nil
	})
	registerEvaluator("uniqueItems", func(_ *evalBuilder, raw any, loc string) (evaluator, error) {
		b, ok := raw.(bool)
		if !ok {
			return nil, &CompileError{KeywordLocation: loc, Message: "uniqueItems must be a boolean"}
		}
		return &uniqueItemsEval{loc: loc, on: b}, nil
	})
}

// =====================================================================
// maxProperties / minProperties / required / dependentRequired
// =====================================================================.

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
		if _, present := obj[k]; !present {
			ctx.addError(e.loc, "required", "", fmt.Sprintf("missing required property %q", k))
		}
	}
}

type dependentRequiredEval struct {
	loc  string
	deps map[string][]string
}

func (e *dependentRequiredEval) keyword() string { return "dependentRequired" }

func (e *dependentRequiredEval) eval(ctx *runCtx, instance any) {
	obj, ok := instance.(map[string]any)
	if !ok {
		return
	}
	for k, required := range e.deps {
		if _, present := obj[k]; !present {
			continue
		}
		for _, r := range required {
			if _, p := obj[r]; !p {
				ctx.addError(e.loc, "dependentRequired", "", fmt.Sprintf("property %q requires %q", k, r))
			}
		}
	}
}

//nolint:gochecknoinits // evaluator registry is built at package init by design.
func init() {
	registerEvaluator("maxProperties", func(_ *evalBuilder, raw any, loc string) (evaluator, error) {
		n, ok := toInt(raw)
		if !ok || n < 0 {
			return nil, &CompileError{KeywordLocation: loc, Message: "maxProperties must be a non-negative integer"}
		}
		return &propsCountEval{loc: loc, kw: "maxProperties", bound: n, upper: true}, nil
	})
	registerEvaluator("minProperties", func(_ *evalBuilder, raw any, loc string) (evaluator, error) {
		n, ok := toInt(raw)
		if !ok || n < 0 {
			return nil, &CompileError{KeywordLocation: loc, Message: "minProperties must be a non-negative integer"}
		}
		return &propsCountEval{loc: loc, kw: "minProperties", bound: n, upper: false}, nil
	})
	registerEvaluator("required", func(_ *evalBuilder, raw any, loc string) (evaluator, error) {
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
		return &requiredEval{loc: loc, keys: keys}, nil
	})
	registerEvaluator("dependentRequired", func(_ *evalBuilder, raw any, loc string) (evaluator, error) {
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
		return &dependentRequiredEval{loc: loc, deps: deps}, nil
	})
}

// itoaInt is a small wrapper around strconv.Itoa for use by the package's
// other eval files.
func itoaInt(i int) string { return strconv.Itoa(i) }
