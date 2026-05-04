package jsonschema

import (
	"fmt"
	"regexp"
)

type allOfEval struct {
	loc  string
	subs []*subschema
}

func (e *allOfEval) keyword() string { return "allOf" }

func (e *allOfEval) eval(ctx *runCtx, instance any) {
	for i, sub := range e.subs {
		ctx.pushSchema("allOf")
		ctx.pushSchema(itoaInt(i))
		ctx.evaluate(sub, instance)
		ctx.popSchema()
		ctx.popSchema()
	}
}

type anyOfEval struct {
	loc  string
	subs []*subschema
}

func (e *anyOfEval) keyword() string { return "anyOf" }

func (e *anyOfEval) eval(ctx *runCtx, instance any) {
	var allCauses []ValidationError
	passing := -1
	var passingAnnos []annotationEntry
	for i, sub := range e.subs {
		errs, annos := ctx.evaluateBranch(sub, instance)
		if len(errs) == 0 {
			if passing < 0 {
				passing = i
				passingAnnos = annos
				// Continue to merge annotations from all passing branches
				ctx.addBranchAnnotations(annos)
			} else {
				ctx.addBranchAnnotations(annos)
			}
		} else {
			allCauses = append(allCauses, errs...)
		}
	}
	_ = passingAnnos
	if passing < 0 {
		ctx.addCausesError(e.loc, "anyOf", "no anyOf branch matched", allCauses)
	}
}

type oneOfEval struct {
	loc  string
	subs []*subschema
}

func (e *oneOfEval) keyword() string { return "oneOf" }

func (e *oneOfEval) eval(ctx *runCtx, instance any) {
	passCount := 0
	var passingAnnos []annotationEntry
	var passingIdx []int
	var allCauses []ValidationError
	for i, sub := range e.subs {
		errs, annos := ctx.evaluateBranch(sub, instance)
		if len(errs) == 0 {
			passCount++
			if passCount == 1 {
				passingAnnos = annos
			}
			passingIdx = append(passingIdx, i)
		} else {
			allCauses = append(allCauses, errs...)
		}
	}
	switch passCount {
	case 0:
		ctx.addCausesError(e.loc, "oneOf", "no oneOf branch matched", allCauses)
	case 1:
		ctx.addBranchAnnotations(passingAnnos)
	default:
		ctx.addError(e.loc, "oneOf", "", fmt.Sprintf("oneOf matched %d branches (indices %v); expected exactly one", passCount, passingIdx))
	}
}

type notEval struct {
	loc string
	sub *subschema
}

func (e *notEval) keyword() string { return "not" }

func (e *notEval) eval(ctx *runCtx, instance any) {
	errs, _ := ctx.evaluateBranch(e.sub, instance)
	if len(errs) == 0 {
		ctx.addError(e.loc, "not", "", "value matched 'not' subschema")
	}
}

//nolint:gochecknoinits // evaluator registry is built at package init by design.
func init() {
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
	errs, annos := ctx.evaluateBranch(e.ifSub, instance)
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

//nolint:gochecknoinits // evaluator registry is built at package init by design.
func init() {
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
	deps map[string]*subschema
}

func (e *dependentSchemasEval) keyword() string { return "dependentSchemas" }

func (e *dependentSchemasEval) eval(ctx *runCtx, instance any) {
	obj, ok := instance.(map[string]any)
	if !ok {
		return
	}
	for k, sub := range e.deps {
		if _, present := obj[k]; !present {
			continue
		}
		ctx.evaluate(sub, instance)
	}
}

type dependenciesEval struct {
	loc      string
	schemas  map[string]*subschema
	required map[string][]string
}

func (e *dependenciesEval) keyword() string { return "dependencies" }

func (e *dependenciesEval) eval(ctx *runCtx, instance any) {
	obj, ok := instance.(map[string]any)
	if !ok {
		return
	}
	for k, sub := range e.schemas {
		if _, present := obj[k]; !present {
			continue
		}
		ctx.evaluate(sub, instance)
	}
	for k, req := range e.required {
		if _, present := obj[k]; !present {
			continue
		}
		for _, r := range req {
			if _, p := obj[r]; !p {
				ctx.addError(e.loc, "dependencies", "", fmt.Sprintf("property %q requires %q", k, r))
			}
		}
	}
}

//nolint:gochecknoinits // evaluator registry is built at package init by design.
func init() {
	registerEvaluator("dependentSchemas", func(b *evalBuilder, f *buildFrame, raw any, loc string) (evaluator, error) {
		m, ok := raw.(map[string]any)
		if !ok {
			return nil, &CompileError{KeywordLocation: loc, Message: "dependentSchemas must be an object"}
		}
		deps := map[string]*subschema{}
		for k, v := range m {
			subLoc := loc + "/" + escapePointerToken(k)
			sub, err := b.buildSubschemaFrame(f, v, subLoc, f.base, f.resource)
			if err != nil {
				return nil, err
			}
			deps[k] = sub
		}
		return &dependentSchemasEval{loc: loc, deps: deps}, nil
	})
	registerEvaluator("dependencies", func(b *evalBuilder, f *buildFrame, raw any, loc string) (evaluator, error) {
		m, ok := raw.(map[string]any)
		if !ok {
			return nil, &CompileError{KeywordLocation: loc, Message: "dependencies must be an object"}
		}
		schemas := map[string]*subschema{}
		required := map[string][]string{}
		for k, v := range m {
			subLoc := loc + "/" + escapePointerToken(k)
			switch t := v.(type) {
			case map[string]any, bool:
				sub, err := b.buildSubschemaFrame(f, t, subLoc, f.base, f.resource)
				if err != nil {
					return nil, err
				}
				schemas[k] = sub
			case []any:
				keys := make([]string, 0, len(t))
				for _, item := range t {
					if s, ok := item.(string); ok {
						keys = append(keys, s)
					}
				}
				required[k] = keys
			}
		}
		return &dependenciesEval{loc: loc, schemas: schemas, required: required}, nil
	})
}

type propertiesEval struct {
	loc  string
	subs map[string]*subschema
}

func (e *propertiesEval) keyword() string { return "properties" }

func (e *propertiesEval) eval(ctx *runCtx, instance any) {
	obj, ok := instance.(map[string]any)
	if !ok {
		return
	}
	if !ctx.checkMaxKeyCount(obj, e.loc) {
		return
	}
	evaluated := evaluatedKeys{}
	for k, sub := range e.subs {
		v, present := obj[k]
		if !present {
			continue
		}
		ctx.evaluateChild(sub, v, k, "properties/"+escapePointerToken(k))
		evaluated[k] = struct{}{}
	}
	if len(evaluated) > 0 {
		mergeKeysAnnotation(ctx, "properties", e.loc, evaluated)
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

func (e *patternPropertiesEval) keyword() string { return "patternProperties" }

func (e *patternPropertiesEval) eval(ctx *runCtx, instance any) {
	obj, ok := instance.(map[string]any)
	if !ok {
		return
	}
	if !ctx.checkMaxKeyCount(obj, e.loc) {
		return
	}
	evaluated := evaluatedKeys{}
	for k, v := range obj {
		for _, pp := range e.patterns {
			if pp.re.MatchString(k) {
				ctx.evaluateChild(pp.sub, v, k, "patternProperties/"+escapePointerToken(pp.src))
				evaluated[k] = struct{}{}
			}
		}
	}
	if len(evaluated) > 0 {
		mergeKeysAnnotation(ctx, "patternProperties", e.loc, evaluated)
	}
}

type additionalPropertiesEval struct {
	loc string
	sub *subschema
}

func (e *additionalPropertiesEval) keyword() string { return "additionalProperties" }

func (e *additionalPropertiesEval) eval(ctx *runCtx, instance any) {
	obj, ok := instance.(map[string]any)
	if !ok {
		return
	}
	if !ctx.checkMaxKeyCount(obj, e.loc) {
		return
	}
	covered := evaluatedKeys{}
	if v, ok := ctx.getAnnotation("properties"); ok {
		if k, ok := v.(evaluatedKeys); ok {
			for kk := range k {
				covered[kk] = struct{}{}
			}
		}
	}
	if v, ok := ctx.getAnnotation("patternProperties"); ok {
		if k, ok := v.(evaluatedKeys); ok {
			for kk := range k {
				covered[kk] = struct{}{}
			}
		}
	}
	evaluated := evaluatedKeys{}
	for k, v := range obj {
		if _, c := covered[k]; c {
			continue
		}
		ctx.evaluateChild(e.sub, v, k, "additionalProperties")
		evaluated[k] = struct{}{}
	}
	if len(evaluated) > 0 {
		mergeKeysAnnotation(ctx, "additionalProperties", e.loc, evaluated)
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
	for k := range obj {
		ctx.evaluateChild(e.sub, k, k, "propertyNames")
	}
}

//nolint:gochecknoinits // evaluator registry is built at package init by design.
func init() {
	registerEvaluator("properties", func(b *evalBuilder, f *buildFrame, raw any, loc string) (evaluator, error) {
		m, ok := raw.(map[string]any)
		if !ok {
			return nil, &CompileError{KeywordLocation: loc, Message: "properties must be an object"}
		}
		subs := map[string]*subschema{}
		for k, v := range m {
			subLoc := loc + "/" + escapePointerToken(k)
			sub, err := b.buildSubschemaFrame(f, v, subLoc, f.base, f.resource)
			if err != nil {
				return nil, err
			}
			subs[k] = sub
		}
		return &propertiesEval{loc: loc, subs: subs}, nil
	})
	registerEvaluator("patternProperties", func(b *evalBuilder, f *buildFrame, raw any, loc string) (evaluator, error) {
		m, ok := raw.(map[string]any)
		if !ok {
			return nil, &CompileError{KeywordLocation: loc, Message: "patternProperties must be an object"}
		}
		patterns := make([]patternProp, 0, len(m))
		for k, v := range m {
			re, err := regexp.Compile(translateECMA(k))
			if err != nil {
				return nil, &CompileError{KeywordLocation: loc, Message: "invalid pattern in patternProperties", Cause: err}
			}
			subLoc := loc + "/" + escapePointerToken(k)
			sub, err := b.buildSubschemaFrame(f, v, subLoc, f.base, f.resource)
			if err != nil {
				return nil, err
			}
			patterns = append(patterns, patternProp{src: k, re: re, sub: sub})
		}
		return &patternPropertiesEval{loc: loc, patterns: patterns}, nil
	})
	registerEvaluator("additionalProperties", func(b *evalBuilder, f *buildFrame, raw any, loc string) (evaluator, error) {
		sub, err := b.buildSubschemaFrame(f, raw, loc, f.base, f.resource)
		if err != nil {
			return nil, err
		}
		return &additionalPropertiesEval{loc: loc, sub: sub}, nil
	})
	registerEvaluator("propertyNames", func(b *evalBuilder, f *buildFrame, raw any, loc string) (evaluator, error) {
		sub, err := b.buildSubschemaFrame(f, raw, loc, f.base, f.resource)
		if err != nil {
			return nil, err
		}
		return &propertyNamesEval{loc: loc, sub: sub}, nil
	})
}

// mergeKeysAnnotation records `keyword`'s evaluatedKeys annotation, unioning
// with any existing entry so multiple branches can contribute.
func mergeKeysAnnotation(ctx *runCtx, keyword, loc string, keys evaluatedKeys) {
	loc1 := ctx.instanceLocation()
	if ctx.annotations[loc1] == nil {
		ctx.annotations[loc1] = map[string]any{}
	}
	if existing, ok := ctx.annotations[loc1][keyword]; ok {
		if existingKeys, ok := existing.(evaluatedKeys); ok {
			for k := range keys {
				existingKeys[k] = struct{}{}
			}
			return
		}
	}
	ctx.annotations[loc1][keyword] = keys
	ctx.annoEntries = append(ctx.annoEntries, annotationEntry{
		keywordLoc:  loc,
		instanceLoc: loc1,
		keyword:     keyword,
		value:       keys,
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
		ctx.evaluateChild(e.subs[i], arr[i], itoaInt(i), "prefixItems/"+itoaInt(i))
	}
	if n > 0 {
		recordItemsAnno(ctx, "prefixItems", e.loc, evaluatedItems(n-1))
	}
}

// itemsEval handles 2020-12 `items` (a single schema applied past the
// prefixItems prefix) AND the legacy single-schema `items` (Draft 4-2019-09).
type itemsEval struct {
	loc      string
	sub      *subschema
	isPrefix bool // legacy form: items is an array (treat like prefixItems)
	subs     []*subschema
}

func (e *itemsEval) keyword() string { return "items" }

func (e *itemsEval) eval(ctx *runCtx, instance any) {
	arr, ok := instance.([]any)
	if !ok {
		return
	}
	if e.isPrefix {
		// Pre-2020-12 items-as-array form (now spelled prefixItems).
		n := min(len(e.subs), len(arr))
		for i := range n {
			ctx.evaluateChild(e.subs[i], arr[i], itoaInt(i), "items/"+itoaInt(i))
		}
		if n > 0 {
			recordItemsAnno(ctx, "items", e.loc, evaluatedItems(n-1))
		}
		return
	}
	start := 0
	if v, ok := ctx.getAnnotation("prefixItems"); ok {
		if iv, ok := v.(evaluatedItems); ok {
			start = int(iv) + 1
		}
	}
	for i := start; i < len(arr); i++ {
		ctx.evaluateChild(e.sub, arr[i], itoaInt(i), "items")
	}
	if len(arr) > start {
		recordItemsAnno(ctx, "items", e.loc, evaluatedItemsAll{})
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

// additionalItemsEval implements pre-2019-09 "additionalItems": it covers
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
		ctx.evaluateChild(e.sub, arr[i], itoaInt(i), "additionalItems")
	}
	recordItemsAnno(ctx, "additionalItems", e.loc, evaluatedItemsAll{})
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
	count := 0
	matched := evaluatedKeys{}
	for i, item := range arr {
		errs, _ := ctx.evaluateBranch(e.sub, item)
		if len(errs) == 0 {
			count++
			matched[itoaInt(i)] = struct{}{}
		}
	}
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
	if count > 0 {
		// Annotate matched indices so unevaluatedItems can read them.
		mergeContainsAnnotation(ctx, e.loc, matched)
	}
}

func mergeContainsAnnotation(ctx *runCtx, loc string, keys evaluatedKeys) {
	loc1 := ctx.instanceLocation()
	if ctx.annotations[loc1] == nil {
		ctx.annotations[loc1] = map[string]any{}
	}
	if existing, ok := ctx.annotations[loc1]["contains"]; ok {
		if existingKeys, ok := existing.(evaluatedKeys); ok {
			for k := range keys {
				existingKeys[k] = struct{}{}
			}
			return
		}
	}
	ctx.annotations[loc1]["contains"] = keys
	ctx.annoEntries = append(ctx.annoEntries, annotationEntry{
		keywordLoc:  loc,
		instanceLoc: loc1,
		keyword:     "contains",
		value:       keys,
	})
}

func recordItemsAnno(ctx *runCtx, keyword, loc string, value any) {
	loc1 := ctx.instanceLocation()
	if ctx.annotations[loc1] == nil {
		ctx.annotations[loc1] = map[string]any{}
	}
	if existing, ok := ctx.annotations[loc1][keyword]; ok {
		ctx.annotations[loc1][keyword] = mergeAnnotation(existing, value)
	} else {
		ctx.annotations[loc1][keyword] = value
	}
	ctx.annoEntries = append(ctx.annoEntries, annotationEntry{
		keywordLoc:  loc,
		instanceLoc: loc1,
		keyword:     keyword,
		value:       value,
	})
}

//nolint:gochecknoinits // evaluator registry is built at package init by design.
func init() {
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
	registerEvaluator("items", func(b *evalBuilder, f *buildFrame, raw any, loc string) (evaluator, error) {
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
		return &itemsEval{loc: loc, sub: sub}, nil
	})
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
