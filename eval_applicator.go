package jsonschema

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

type allOfEval struct {
	loc  string
	subs []*subschema
}

func (e *allOfEval) keyword() string { return "allOf" }

func (e *allOfEval) eval(ctx *runCtx, instance any) {
	for _, sub := range e.subs {
		ctx.evaluate(sub, instance)
	}
}

type anyOfEval struct {
	loc  string
	subs []*subschema
}

func (e *anyOfEval) keyword() string { return "anyOf" }

// eval evaluates every branch while an unevaluated* keyword may read the
// annotations of passing branches, or the annotations are reported;
// otherwise it stops at the first passing branch.
func (e *anyOfEval) eval(ctx *runCtx, instance any) {
	var allCauses []ValidationError
	causesCost := 0
	passed := false
	for _, sub := range e.subs {
		errs, annos, cost := ctx.evaluateBranch(sub, instance)
		if len(errs) == 0 || passed {
			ctx.refundErrors(cost)
		}
		if len(errs) == 0 {
			if !passed {
				passed = true
				ctx.refundErrors(causesCost)
				allCauses, causesCost = nil, 0
			}
			ctx.addBranchAnnotations(annos)
			if !ctx.needScope && !ctx.public {
				return
			}
			continue
		}
		if !passed {
			allCauses = append(allCauses, errs...)
			causesCost += cost
		}
	}
	if !passed {
		ctx.addCausesError(e.loc, "anyOf", "no anyOf branch matched", allCauses, causesCost)
	}
}

type oneOfEval struct {
	loc  string
	subs []*subschema
}

func (e *oneOfEval) keyword() string { return "oneOf" }

// eval stops at the second passing branch: the keyword has failed by then.
func (e *oneOfEval) eval(ctx *runCtx, instance any) {
	var passingAnnos branchAnnotations
	var passingIdx []int
	var allCauses []ValidationError
	causesCost := 0
	for i, sub := range e.subs {
		errs, annos, cost := ctx.evaluateBranch(sub, instance)
		if len(errs) == 0 || len(passingIdx) > 0 {
			// Once a branch has passed, no branch's errors are kept.
			ctx.refundErrors(cost)
		}
		if len(errs) == 0 {
			passingIdx = append(passingIdx, i)
			if len(passingIdx) == 2 {
				ctx.addError(e.loc, "oneOf", "", fmt.Sprintf("oneOf matched more than one branch (indices %d and %d); expected exactly one", passingIdx[0], passingIdx[1]))
				return
			}
			ctx.refundErrors(causesCost)
			allCauses, causesCost = nil, 0
			passingAnnos = annos
		} else if len(passingIdx) == 0 {
			allCauses = append(allCauses, errs...)
			causesCost += cost
		}
	}
	if len(passingIdx) == 0 {
		ctx.addCausesError(e.loc, "oneOf", "no oneOf branch matched", allCauses, causesCost)
		return
	}
	ctx.addBranchAnnotations(passingAnnos)
}

type notEval struct {
	loc string
	sub *subschema
}

func (e *notEval) keyword() string { return "not" }

func (e *notEval) eval(ctx *runCtx, instance any) {
	errs, _, cost := ctx.evaluateBranch(e.sub, instance)
	ctx.refundErrors(cost)
	if len(errs) == 0 {
		ctx.addError(e.loc, "not", "", "value matched 'not' subschema")
	}
}

func registerCombinatorKeywords() {
	registerEvaluator("allOf", func(b *evalBuilder, f *buildFrame, raw any, loc string) (evaluator, error) {
		arr, ok := raw.([]any)
		if !ok {
			return nil, &CompileError{KeywordLocation: loc, Message: "allOf must be an array"}
		}
		subs := make([]*subschema, 0, len(arr))
		for i, item := range arr {
			itemLoc := loc + "/" + itoaInt(i)
			sub, err := b.buildSubschemaFrame(f, item, itemLoc, f.base, f.resource)
			if err != nil {
				return nil, err
			}
			subs = append(subs, sub)
		}
		return &allOfEval{loc: loc, subs: subs}, nil
	})
	registerEvaluator("anyOf", func(b *evalBuilder, f *buildFrame, raw any, loc string) (evaluator, error) {
		arr, ok := raw.([]any)
		if !ok {
			return nil, &CompileError{KeywordLocation: loc, Message: "anyOf must be an array"}
		}
		subs := make([]*subschema, 0, len(arr))
		for i, item := range arr {
			itemLoc := loc + "/" + itoaInt(i)
			sub, err := b.buildSubschemaFrame(f, item, itemLoc, f.base, f.resource)
			if err != nil {
				return nil, err
			}
			subs = append(subs, sub)
		}
		return &anyOfEval{loc: loc, subs: subs}, nil
	})
	registerEvaluator("oneOf", func(b *evalBuilder, f *buildFrame, raw any, loc string) (evaluator, error) {
		arr, ok := raw.([]any)
		if !ok {
			return nil, &CompileError{KeywordLocation: loc, Message: "oneOf must be an array"}
		}
		subs := make([]*subschema, 0, len(arr))
		for i, item := range arr {
			itemLoc := loc + "/" + itoaInt(i)
			sub, err := b.buildSubschemaFrame(f, item, itemLoc, f.base, f.resource)
			if err != nil {
				return nil, err
			}
			subs = append(subs, sub)
		}
		return &oneOfEval{loc: loc, subs: subs}, nil
	})
	registerEvaluator("not", func(b *evalBuilder, f *buildFrame, raw any, loc string) (evaluator, error) {
		sub, err := b.buildSubschemaFrame(f, raw, loc, f.base, f.resource)
		if err != nil {
			return nil, err
		}
		return &notEval{loc: loc, sub: sub}, nil
	})
}

type ifThenElseEval struct {
	loc     string
	ifSub   *subschema
	thenSub *subschema
	elseSub *subschema
	thenLoc string
	elseLoc string
}

func (e *ifThenElseEval) keyword() string { return "if" }

func (e *ifThenElseEval) eval(ctx *runCtx, instance any) {
	errs, annos, cost := ctx.evaluateBranch(e.ifSub, instance)
	ctx.refundErrors(cost)
	ifPassed := len(errs) == 0
	if ifPassed {
		ctx.addBranchAnnotations(annos)
		if e.thenSub != nil {
			ctx.evaluate(e.thenSub, instance)
		}
	} else if e.elseSub != nil {
		ctx.evaluate(e.elseSub, instance)
	}
}

func registerConditionalKeywords() {
	registerEvaluator("if", func(b *evalBuilder, f *buildFrame, raw any, loc string) (evaluator, error) {
		// then / else are read off the parent map and folded into this
		// ifThenElseEval (the dispatcher sees them as no-ops).
		ifSub, err := b.buildSubschemaFrame(f, raw, loc, f.base, f.resource)
		if err != nil {
			return nil, err
		}
		ev := &ifThenElseEval{loc: loc, ifSub: ifSub}
		if parent, ok := f.parent.(map[string]any); ok {
			if rawThen, ok := parent["then"]; ok {
				thenLoc := strParentLoc(f.loc) + "/then"
				thenSub, err := b.buildSubschemaFrame(f, rawThen, thenLoc, f.base, f.resource)
				if err != nil {
					return nil, err
				}
				ev.thenSub = thenSub
				ev.thenLoc = thenLoc
			}
			if rawElse, ok := parent["else"]; ok {
				elseLoc := strParentLoc(f.loc) + "/else"
				elseSub, err := b.buildSubschemaFrame(f, rawElse, elseLoc, f.base, f.resource)
				if err != nil {
					return nil, err
				}
				ev.elseSub = elseSub
				ev.elseLoc = elseLoc
			}
		}
		return ev, nil
	})
	// then / else are folded into the parent ifThenElseEval when "if" is
	// seen. Standalone (no sibling "if") they have no validation effect, so
	// returning nil here keeps the dispatcher table tidy without a no-op
	// evaluator type — populateSubschema filters nil evaluators.
	registerEvaluator("then", func(_ *evalBuilder, _ *buildFrame, _ any, _ string) (evaluator, error) {
		return nil, nil //nolint:nilnil // intentional: keyword has no runtime effect when standalone.
	})
	registerEvaluator("else", func(_ *evalBuilder, _ *buildFrame, _ any, _ string) (evaluator, error) {
		return nil, nil //nolint:nilnil // intentional: keyword has no runtime effect when standalone.
	})
}

// strParentLoc returns its argument; the caller appends /then or /else.
// Kept as a function for parity with future location-derivation logic.
func strParentLoc(loc string) string { return loc }

type dependentSchemasEval struct {
	loc  string
	keys []string // sorted, so errors come in a stable order
	deps map[string]*subschema
}

func (e *dependentSchemasEval) keyword() string { return "dependentSchemas" }

func (e *dependentSchemasEval) eval(ctx *runCtx, instance any) {
	obj, ok := instance.(map[string]any)
	if !ok {
		return
	}
	for _, k := range e.keys {
		if _, present := obj[k]; !present {
			continue
		}
		ctx.evaluate(e.deps[k], instance)
	}
}

type dependenciesEval struct {
	loc      string
	keys     []string // sorted, so errors come in a stable order
	schemas  map[string]*subschema
	required map[string][]string
}

func (e *dependenciesEval) keyword() string { return "dependencies" }

func (e *dependenciesEval) eval(ctx *runCtx, instance any) {
	obj, ok := instance.(map[string]any)
	if !ok {
		return
	}
	for _, k := range e.keys {
		if _, present := obj[k]; !present {
			continue
		}
		if sub, ok := e.schemas[k]; ok {
			ctx.evaluate(sub, instance)
			continue
		}
		for _, r := range e.required[k] {
			if _, p := obj[r]; !p {
				ctx.addError(e.loc, "dependencies", "", fmt.Sprintf("property %q requires %q", k, r))
			}
		}
	}
}

func registerDependencyKeywords() {
	registerEvaluator("dependentSchemas", func(b *evalBuilder, f *buildFrame, raw any, loc string) (evaluator, error) {
		m, ok := raw.(map[string]any)
		if !ok {
			return nil, &CompileError{KeywordLocation: loc, Message: "dependentSchemas must be an object"}
		}
		keys := orderKeys(m)
		deps := make(map[string]*subschema, len(m))
		for _, k := range keys {
			subLoc := loc + "/" + escapePointerToken(k)
			sub, err := b.buildSubschemaFrame(f, m[k], subLoc, f.base, f.resource)
			if err != nil {
				return nil, err
			}
			deps[k] = sub
		}
		return &dependentSchemasEval{loc: loc, keys: keys, deps: deps}, nil
	})
	registerEvaluator("dependencies", func(b *evalBuilder, f *buildFrame, raw any, loc string) (evaluator, error) {
		m, ok := raw.(map[string]any)
		if !ok {
			return nil, &CompileError{KeywordLocation: loc, Message: "dependencies must be an object"}
		}
		schemas := map[string]*subschema{}
		required := map[string][]string{}
		keys := orderKeys(m)
		for _, k := range keys {
			subLoc := loc + "/" + escapePointerToken(k)
			switch t := m[k].(type) {
			case map[string]any, bool:
				sub, err := b.buildSubschemaFrame(f, t, subLoc, f.base, f.resource)
				if err != nil {
					return nil, err
				}
				schemas[k] = sub
			case []any:
				names := make([]string, 0, len(t))
				for _, item := range t {
					if s, ok := item.(string); ok {
						names = append(names, s)
					}
				}
				required[k] = names
			}
		}
		return &dependenciesEval{loc: loc, keys: keys, schemas: schemas, required: required}, nil
	})
}

type propertiesEval struct {
	loc  string
	keys []string // sorted, so errors come in a stable order
	subs map[string]*subschema
}

func (e *propertiesEval) keyword() string { return keyProperties }

func (e *propertiesEval) eval(ctx *runCtx, instance any) {
	obj, ok := instance.(map[string]any)
	if !ok {
		return
	}
	if !ctx.checkMaxKeyCount(obj, e.loc) {
		return
	}
	var evaluated []string
	track := ctx.tracksEvaluated()
	for _, k := range e.keys {
		v, present := obj[k]
		if !present {
			continue
		}
		ctx.evaluateChild(e.subs[k], v, k)
		if !track {
			continue
		}
		if evaluated == nil {
			evaluated = make([]string, 0, min(len(e.keys), len(obj)))
		}
		evaluated = append(evaluated, k)
	}
	recordKeys(ctx, keyProperties, e.loc, evaluated)
}

// recordKeys records the property names a keyword evaluated: in the scope
// for unevaluatedProperties, and in the annotation log.
func recordKeys(ctx *runCtx, keyword, loc string, keys []string) {
	if len(keys) == 0 {
		return
	}
	ctx.markProps(keys)
	if ctx.public {
		ctx.appendAnnotationEntry(loc, keyword, keys)
	}
}

type patternPropertiesEval struct {
	loc      string
	patterns []patternProp
}

type patternProp struct {
	src string
	re  *regexp.Regexp
	sub *subschema
}

func (e *patternPropertiesEval) keyword() string { return keyPatternProperties }

func (e *patternPropertiesEval) eval(ctx *runCtx, instance any) {
	obj, ok := instance.(map[string]any)
	if !ok {
		return
	}
	if !ctx.checkMaxKeyCount(obj, e.loc) {
		return
	}
	// Only the names some pattern matches are walked, in sorted order.
	var matched []string
	for k := range obj {
		for _, pp := range e.patterns {
			if pp.re.MatchString(k) {
				matched = append(matched, k)
				break
			}
		}
	}
	if len(matched) == 0 {
		return
	}
	sort.Strings(matched)
	var evaluated []string
	track := ctx.tracksEvaluated()
	for _, k := range matched {
		for _, pp := range e.patterns {
			if len(e.patterns) == 1 || pp.re.MatchString(k) {
				ctx.evaluateChild(pp.sub, obj[k], k)
				if !track {
					continue
				}
				if n := len(evaluated); n == 0 || evaluated[n-1] != k {
					if evaluated == nil {
						evaluated = make([]string, 0, len(matched))
					}
					evaluated = append(evaluated, k)
				}
			}
		}
	}
	recordKeys(ctx, keyPatternProperties, e.loc, evaluated)
}

// sortedKeys returns obj's keys in order, so evaluators that walk an
// instance object report errors in a stable order.
func sortedKeys(obj map[string]any) []string {
	keys := make([]string, 0, len(obj))
	for k := range obj {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// additionalPropertiesEval applies sub to the properties that the sibling
// properties and patternProperties keywords do not cover. The cover is
// taken from those siblings at build time, so annotations from any other
// subschema cannot affect it.
type additionalPropertiesEval struct {
	loc      string
	sub      *subschema
	named    map[string]struct{}
	patterns []*regexp.Regexp
}

func (e *additionalPropertiesEval) keyword() string { return "additionalProperties" }

func (e *additionalPropertiesEval) covered(k string) bool {
	if _, ok := e.named[k]; ok {
		return true
	}
	for _, re := range e.patterns {
		if re.MatchString(k) {
			return true
		}
	}
	return false
}

func (e *additionalPropertiesEval) eval(ctx *runCtx, instance any) {
	obj, ok := instance.(map[string]any)
	if !ok {
		return
	}
	if !ctx.checkMaxKeyCount(obj, e.loc) {
		return
	}
	// Only the names the siblings leave uncovered are walked, in sorted
	// order.
	var extra []string
	for k := range obj {
		if !e.covered(k) {
			extra = append(extra, k)
		}
	}
	if len(extra) == 0 {
		return
	}
	sort.Strings(extra)
	for _, k := range extra {
		ctx.evaluateChild(e.sub, obj[k], k)
	}
	if ctx.tracksEvaluated() {
		recordKeys(ctx, "additionalProperties", e.loc, extra)
	}
}

type propertyNamesEval struct {
	loc string
	sub *subschema
}

func (e *propertyNamesEval) keyword() string { return "propertyNames" }

func (e *propertyNamesEval) eval(ctx *runCtx, instance any) {
	obj, ok := instance.(map[string]any)
	if !ok {
		return
	}
	if !ctx.checkMaxKeyCount(obj, e.loc) {
		return
	}
	for _, k := range sortedKeys(obj) {
		ctx.evaluateChild(e.sub, k, k)
	}
}

// compilePatternKeys compiles the keys of a patternProperties object.
func (b *evalBuilder) compilePatternKeys(m map[string]any, loc string) (map[string]*regexp.Regexp, error) {
	out := make(map[string]*regexp.Regexp, len(m))
	for k := range m {
		re, err := b.compileRegex(k)
		if err != nil {
			return nil, &CompileError{KeywordLocation: loc, Message: "invalid pattern in patternProperties", Cause: err}
		}
		out[k] = re
	}
	return out, nil
}

func registerObjectApplicatorKeywords() {
	registerEvaluator("properties", func(b *evalBuilder, f *buildFrame, raw any, loc string) (evaluator, error) {
		m, ok := raw.(map[string]any)
		if !ok {
			return nil, &CompileError{KeywordLocation: loc, Message: "properties must be an object"}
		}
		keys := orderKeys(m)
		subs := make(map[string]*subschema, len(m))
		for _, k := range keys {
			subLoc := loc + "/" + escapePointerToken(k)
			sub, err := b.buildSubschemaFrame(f, m[k], subLoc, f.base, f.resource)
			if err != nil {
				return nil, err
			}
			subs[k] = sub
		}
		return &propertiesEval{loc: loc, keys: keys, subs: subs}, nil
	})
	registerEvaluator("patternProperties", func(b *evalBuilder, f *buildFrame, raw any, loc string) (evaluator, error) {
		m, ok := raw.(map[string]any)
		if !ok {
			return nil, &CompileError{KeywordLocation: loc, Message: "patternProperties must be an object"}
		}
		res, err := b.compilePatternKeys(m, loc)
		if err != nil {
			return nil, err
		}
		patterns := make([]patternProp, 0, len(m))
		for _, k := range orderKeys(m) {
			subLoc := loc + "/" + escapePointerToken(k)
			sub, err := b.buildSubschemaFrame(f, m[k], subLoc, f.base, f.resource)
			if err != nil {
				return nil, err
			}
			patterns = append(patterns, patternProp{src: k, re: res[k], sub: sub})
		}
		return &patternPropertiesEval{loc: loc, patterns: patterns}, nil
	})
	registerEvaluator("additionalProperties", func(b *evalBuilder, f *buildFrame, raw any, loc string) (evaluator, error) {
		sub, err := b.buildSubschemaFrame(f, raw, loc, f.base, f.resource)
		if err != nil {
			return nil, err
		}
		ev := &additionalPropertiesEval{loc: loc, sub: sub}
		if parent, ok := f.parent.(map[string]any); ok {
			if props, ok := parent[keyProperties].(map[string]any); ok {
				ev.named = make(map[string]struct{}, len(props))
				for k := range props {
					ev.named[k] = struct{}{}
				}
			}
			if pats, ok := parent[keyPatternProperties].(map[string]any); ok {
				res, err := b.compilePatternKeys(pats, strings.TrimSuffix(loc, "additionalProperties")+keyPatternProperties)
				if err != nil {
					return nil, err
				}
				for _, k := range orderKeys(pats) {
					ev.patterns = append(ev.patterns, res[k])
				}
			}
		}
		return ev, nil
	})
	registerEvaluator("propertyNames", func(b *evalBuilder, f *buildFrame, raw any, loc string) (evaluator, error) {
		sub, err := b.buildSubschemaFrame(f, raw, loc, f.base, f.resource)
		if err != nil {
			return nil, err
		}
		return &propertyNamesEval{loc: loc, sub: sub}, nil
	})
}

type prefixItemsEval struct {
	loc  string
	subs []*subschema
}

func (e *prefixItemsEval) keyword() string { return "prefixItems" }

func (e *prefixItemsEval) eval(ctx *runCtx, instance any) {
	arr, ok := instance.([]any)
	if !ok {
		return
	}
	n := min(len(e.subs), len(arr))
	for i := range n {
		ctx.evaluateItem(e.subs[i], arr[i], i)
	}
	recordPrefix(ctx, "prefixItems", e.loc, n)
}

// recordPrefix records that the first n items were evaluated.
func recordPrefix(ctx *runCtx, keyword, loc string, n int) {
	if n == 0 {
		return
	}
	ctx.markPrefix(n)
	ctx.appendAnnotationEntry(loc, keyword, evaluatedItems(n-1))
}

// recordAllItems records that a keyword evaluated every remaining item.
func recordAllItems(ctx *runCtx, keyword, loc string) {
	ctx.markAllItems()
	ctx.appendAnnotationEntry(loc, keyword, evaluatedItemsAll{})
}

// itemsEval handles 2020-12 `items` (a single schema applied past the
// sibling prefixItems) and the legacy `items` of earlier drafts (a single
// schema for every item, or an array of schemas for a prefix).
type itemsEval struct {
	loc      string
	sub      *subschema
	isPrefix bool // legacy form: items is an array (treat like prefixItems)
	subs     []*subschema
	// start is the number of sibling prefixItems.
	start int
}

func (e *itemsEval) keyword() string { return "items" }

func (e *itemsEval) eval(ctx *runCtx, instance any) {
	arr, ok := instance.([]any)
	if !ok {
		return
	}
	if e.isPrefix {
		n := min(len(e.subs), len(arr))
		for i := range n {
			ctx.evaluateItem(e.subs[i], arr[i], i)
		}
		recordPrefix(ctx, "items", e.loc, n)
		return
	}
	for i := e.start; i < len(arr); i++ {
		ctx.evaluateItem(e.sub, arr[i], i)
	}
	if len(arr) > e.start {
		recordAllItems(ctx, "items", e.loc)
	}
}

// siblingItemsArrayLen returns the length of the sibling "items" array,
// or -1 if items is missing or a single schema (additionalItems is a
// no-op per Draft 4–7 in those cases).
func siblingItemsArrayLen(f *buildFrame) int {
	parent, ok := f.parent.(map[string]any)
	if !ok {
		return -1
	}
	rawItems, ok := parent["items"]
	if !ok {
		return -1
	}
	arr, ok := rawItems.([]any)
	if !ok {
		return -1
	}
	return len(arr)
}

// additionalItemsEval implements pre-2020-12 "additionalItems": it covers
// indices past the sibling "items" array prefix. itemsArrayLen is -1 when
// items is missing or a single schema (the evaluator is then a no-op).
type additionalItemsEval struct {
	loc           string
	sub           *subschema
	itemsArrayLen int
}

func (e *additionalItemsEval) keyword() string { return "additionalItems" }

func (e *additionalItemsEval) eval(ctx *runCtx, instance any) {
	if e.itemsArrayLen < 0 {
		return
	}
	arr, ok := instance.([]any)
	if !ok {
		return
	}
	start := e.itemsArrayLen
	if start >= len(arr) {
		return
	}
	for i := start; i < len(arr); i++ {
		ctx.evaluateItem(e.sub, arr[i], i)
	}
	recordAllItems(ctx, "additionalItems", e.loc)
}

type containsEval struct {
	loc         string
	sub         *subschema
	maxContains int
	minContains int
	hasMax      bool
	hasMin      bool
}

func (e *containsEval) keyword() string { return "contains" }

func (e *containsEval) eval(ctx *runCtx, instance any) {
	arr, ok := instance.([]any)
	if !ok {
		return
	}
	var matched []int
	for i, item := range arr {
		ctx.pushIndex(i)
		depth := ctx.refDepth
		ctx.refDepth = 0
		errs, _, cost := ctx.evaluateBranch(e.sub, item)
		ctx.refundErrors(cost)
		ctx.refDepth = depth
		ctx.popInstance()
		if len(errs) == 0 {
			matched = append(matched, i)
		}
	}
	count := len(matched)
	minC := 1
	if e.hasMin {
		minC = e.minContains
	}
	if count < minC {
		ctx.addError(e.loc, "contains", "", fmt.Sprintf("contains matched %d items; minContains is %d", count, minC))
	}
	if e.hasMax && count > e.maxContains {
		ctx.addError(e.loc, "contains", "", fmt.Sprintf("contains matched %d items; maxContains is %d", count, e.maxContains))
	}
	if count == 0 {
		return
	}
	ctx.markContained(matched)
	if ctx.public {
		keys := make(evaluatedKeys, count)
		for _, i := range matched {
			keys[itoaInt(i)] = struct{}{}
		}
		ctx.appendAnnotationEntry(e.loc, "contains", keys)
	}
}

func registerArrayApplicatorKeywords() {
	registerEvaluator("prefixItems", func(b *evalBuilder, f *buildFrame, raw any, loc string) (evaluator, error) {
		arr, ok := raw.([]any)
		if !ok {
			return nil, &CompileError{KeywordLocation: loc, Message: "prefixItems must be an array"}
		}
		subs := make([]*subschema, 0, len(arr))
		for i, item := range arr {
			itemLoc := loc + "/" + itoaInt(i)
			sub, err := b.buildSubschemaFrame(f, item, itemLoc, f.base, f.resource)
			if err != nil {
				return nil, err
			}
			subs = append(subs, sub)
		}
		return &prefixItemsEval{loc: loc, subs: subs}, nil
	})
	registerEvaluator("items", buildItems)
	registerEvaluator("additionalItems", func(b *evalBuilder, f *buildFrame, raw any, loc string) (evaluator, error) {
		sub, err := b.buildSubschemaFrame(f, raw, loc, f.base, f.resource)
		if err != nil {
			return nil, err
		}
		return &additionalItemsEval{loc: loc, sub: sub, itemsArrayLen: siblingItemsArrayLen(f)}, nil
	})
	registerEvaluator("contains", func(b *evalBuilder, f *buildFrame, raw any, loc string) (evaluator, error) {
		sub, err := b.buildSubschemaFrame(f, raw, loc, f.base, f.resource)
		if err != nil {
			return nil, err
		}
		ev := &containsEval{loc: loc, sub: sub}
		if parent, ok := f.parent.(map[string]any); ok {
			if v, ok := parent["maxContains"]; ok {
				if n, ok := toInt(v); ok {
					ev.maxContains = n
					ev.hasMax = true
				}
			}
			if v, ok := parent["minContains"]; ok {
				if n, ok := toInt(v); ok {
					ev.minContains = n
					ev.hasMin = true
				}
			}
		}
		return ev, nil
	})
	// maxContains / minContains are folded into containsEval at build
	// time; standalone they have no runtime effect (and populateSubschema
	// filters nil evaluators).
	registerEvaluator("maxContains", func(_ *evalBuilder, _ *buildFrame, _ any, _ string) (evaluator, error) {
		return nil, nil //nolint:nilnil // intentional: keyword has no runtime effect when standalone.
	})
	registerEvaluator("minContains", func(_ *evalBuilder, _ *buildFrame, _ any, _ string) (evaluator, error) {
		return nil, nil //nolint:nilnil // intentional: keyword has no runtime effect when standalone.
	})
}

// buildItems builds "items": a schema for the items past the sibling
// prefixItems, or before 2020-12 also an array of schemas for a prefix.
func buildItems(b *evalBuilder, f *buildFrame, raw any, loc string) (evaluator, error) {
	// 2020-12 items is always a schema; pre-2020-12 also allowed an
	// array of schemas (the legacy "tuple" form).
	if arr, ok := raw.([]any); ok && f.draft < Draft202012 {
		subs := make([]*subschema, 0, len(arr))
		for i, item := range arr {
			itemLoc := loc + "/" + itoaInt(i)
			sub, err := b.buildSubschemaFrame(f, item, itemLoc, f.base, f.resource)
			if err != nil {
				return nil, err
			}
			subs = append(subs, sub)
		}
		return &itemsEval{loc: loc, isPrefix: true, subs: subs}, nil
	}
	sub, err := b.buildSubschemaFrame(f, raw, loc, f.base, f.resource)
	if err != nil {
		return nil, err
	}
	ev := &itemsEval{loc: loc, sub: sub}
	if parent, ok := f.parent.(map[string]any); ok && f.draft >= Draft202012 {
		if prefix, ok := parent[keyPrefixItems].([]any); ok {
			ev.start = len(prefix)
		}
	}
	return ev, nil
}
