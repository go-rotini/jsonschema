package jsonschema

import (
	"sort"
	"strings"
)

// subschema is the runtime representation of a single schema location: the
// boolean schema variant, plus the parsed keyword evaluators that apply at
// this location. Subschemas are built once by the compiler and walked by the
// validator.
type subschema struct {
	// boolValue is non-nil when the schema is the JSON literal true or false.
	// A *bool of true matches everything; *bool of false matches nothing.
	boolValue *bool
	// raw is the underlying parsed map[string]any (or boolean). Used by
	// applicators such as `enum`/`const` that need the raw value tree.
	raw any
	// location is the JSON Pointer fragment of this subschema in the source.
	location string
	// baseURI is the base URI in effect for this subschema (after $id).
	baseURI string
	// resourceURI is the URI of the enclosing $id-bounded resource. Pushed
	// onto the dynamic scope on entry and popped on exit.
	resourceURI string
	// crossesResource is true when entering this subschema crosses a $id
	// boundary (a new resource opens). The dispatch loop uses this to push
	// the resource onto the dynamic scope only when needed.
	crossesResource bool
	// evaluators are the per-keyword evaluators for this subschema in
	// declaration-stable order. allOf/anyOf/oneOf/$ref/applicator evaluators
	// come before unevaluated* / format / metadata.
	evaluators []evaluator
	// keywords records the recognized keyword names so unevaluated* can
	// detect sibling keywords without rescanning the raw map.
	keywords map[string]struct{}
	// schema is the parent compiled schema; used by evaluators to follow
	// refs into other resources.
	schema *Schema
}

// evaluator is the runtime form of one keyword binding at one schema
// location. Implementations append validation errors to ctx.errors and
// annotations to ctx.annotations; they return no error for keyword-level
// failures (the spec keeps validation a partial-functional walk so that all
// branches contribute their failures to the output).
type evaluator interface {
	// keyword returns the JSON Schema keyword name this evaluator handles.
	keyword() string
	// eval validates instance under this keyword's binding.
	eval(ctx *runCtx, instance any)
}

// builderFn constructs an evaluator for keyword name from raw keyword value.
// keywordBuilders maps every supported keyword to its builder.
type builderFn func(b *evalBuilder, raw any, loc string) (evaluator, error)

var keywordBuilders = map[string]builderFn{}

// registerEvaluator wires builder b under keyword name. Called from per-file
// init blocks to avoid one giant switch table here.
func registerEvaluator(name string, b builderFn) {
	keywordBuilders[name] = b
}

// evalBuilder is the per-compile context that subschema constructors share
// while building the evaluator graph.
type evalBuilder struct {
	schema *Schema
	rm     *resourceMap
	loader Loader
	draft  Draft
	cache  map[string]*subschema // location → subschema, for cycle-breaking
	// currentParent is the raw map[string]any of the schema location
	// currently being constructed. Builders for keywords with sibling
	// dependencies (if/then/else, contains/maxContains/minContains) read
	// from it.
	currentParent any
	// currentLoc is the JSON Pointer of currentParent.
	currentLoc string
	// currentBase is the base URI in effect at the current build position
	// (after applying any nested $id along the path). Used by ref builders
	// so nested $refs resolve against the correct base.
	currentBase string
	// currentResource is the absolute URI of the resource owning the
	// current build position.
	currentResource string
}

// buildSubschema produces (or returns from cache) the subschema rooted at
// node, located at loc within the schema, with the given base + resource URI.
func (b *evalBuilder) buildSubschema(node any, loc, baseURI, resourceURI string, crosses bool) (*subschema, error) {
	if existing, ok := b.cache[loc]; ok {
		return existing, nil
	}
	// b.cache[loc] is set below before the keyword loop recurses, so the
	// shared pointer covers cycle re-entry transparently — every recursive
	// build of the same loc returns the same (partially built) *subschema
	// that becomes fully populated once the outer call returns.

	sub := &subschema{
		raw:             node,
		location:        loc,
		baseURI:         baseURI,
		resourceURI:     resourceURI,
		crossesResource: crosses,
		schema:          b.schema,
		keywords:        map[string]struct{}{},
	}
	b.cache[loc] = sub

	switch t := node.(type) {
	case bool:
		v := t
		sub.boolValue = &v
		return sub, nil
	case map[string]any:
		// per-resource $schema override
		subDraft := b.draft
		idKey := subDraft.IDKeyword()
		if v, ok := t["$schema"]; ok {
			if s, ok := v.(string); ok {
				if d := DraftFromMetaSchemaURL(s); d != DraftUnknown {
					subDraft = d
					idKey = subDraft.IDKeyword()
				}
			}
		}
		// Update base URI if $id is set.
		if rawID, ok := t[idKey]; ok {
			if s, ok := rawID.(string); ok && s != "" {
				if abs, err := resolveURI(baseURI, s); err == nil {
					abs, _ = splitFragment(abs)
					baseURI = abs
					resourceURI = abs
					sub.baseURI = baseURI
					sub.resourceURI = resourceURI
					sub.crossesResource = true
				}
			}
		}

		savedDraft := b.draft
		b.draft = subDraft
		savedParent := b.currentParent
		savedLoc := b.currentLoc
		savedBase := b.currentBase
		savedRes := b.currentResource
		b.currentParent = t
		b.currentLoc = loc
		b.currentBase = baseURI
		b.currentResource = resourceURI
		defer func() {
			b.draft = savedDraft
			b.currentParent = savedParent
			b.currentLoc = savedLoc
			b.currentBase = savedBase
			b.currentResource = savedRes
		}()

		// Collect & order keys: applicator/ref keys first, unevaluated last.
		keys := orderKeys(t)
		for _, key := range keys {
			if _, known := LookupKeyword(key, subDraft); !known {
				continue
			}
			sub.keywords[key] = struct{}{}
			builder, ok := keywordBuilders[key]
			if !ok {
				continue
			}
			ev, err := builder(b, t[key], loc+"/"+escapePointerToken(key))
			if err != nil {
				return nil, err
			}
			if ev != nil {
				sub.evaluators = append(sub.evaluators, ev)
			}
		}
		// Stable sort: applicators/refs before unevaluated*; format/metadata
		// last; everything else preserves builder order.
		sort.SliceStable(sub.evaluators, func(i, j int) bool {
			return evalPriority(sub.evaluators[i].keyword()) <
				evalPriority(sub.evaluators[j].keyword())
		})
		return sub, nil
	default:
		// Unknown shape — treat as the always-pass schema.
		passing := true
		sub.boolValue = &passing
		return sub, nil
	}
}

// orderKeys returns the keys of m in a deterministic, stable order. The order
// is alphabetical so the evaluator graph is reproducible.
func orderKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// evalPriority groups keywords so applicators run before unevaluated*. Lower
// priority runs first. Within a priority bracket the order is stable.
func evalPriority(name string) int {
	switch name {
	case "$ref", "$dynamicRef":
		return 0
	case "type", "enum", "const":
		return 5
	// `properties` and `patternProperties` MUST run before
	// `additionalProperties` (per §12.12) so additionalProperties can see
	// their evaluated-keys annotations.
	case "properties", "patternProperties":
		return 8
	case "prefixItems":
		return 8
	case "items", "additionalItems":
		return 9
	case "additionalProperties":
		return 10
	case "allOf", "anyOf", "oneOf", "not", "if",
		"dependentSchemas", "dependentRequired",
		"propertyNames",
		"contains", "maxContains", "minContains",
		"required", "maxProperties", "minProperties",
		"maxItems", "minItems", "uniqueItems",
		"maxLength", "minLength", "pattern",
		"multipleOf", "maximum", "minimum",
		"exclusiveMaximum", "exclusiveMinimum",
		"dependencies":
		return 12
	case "unevaluatedItems", "unevaluatedProperties":
		return 20
	default:
		// title / description / default / format / etc — annotation tail.
		return 30
	}
}

// evalRoot returns the root subschema of s, or nil when the schema was not
// compiled with the runtime evaluator graph.
func (s *Schema) evalRoot() *subschema {
	if s == nil {
		return nil
	}
	return s.root
}

// runCtx carries per-call validation state.
type runCtx struct {
	schema          *Schema
	opts            *runOptions
	instanceLoc     []string
	schemaLoc       []string
	dynamicScope    []string // chain of in-scope resource URIs
	errors          []ValidationError
	annotations     map[string]map[string]any // instanceLoc → keyword → value
	annoEntries     []annotationEntry         // ordered annotation log for output
	refDepth        int
	validationDepth int
	// stopFired is set when stopOnFirstError sees its first error so that
	// nested branches (which capture errors into a fresh slice via
	// evaluateBranch) still short-circuit at the outer-most level.
	stopFired bool
}

// annotationEntry is the internal record of one annotation. We keep the
// instance location and keyword so unevaluated* keywords can read the graph.
type annotationEntry struct {
	keywordLoc  string
	instanceLoc string
	keyword     string
	value       any
}

// dispatch walks every evaluator on sub against instance.
func (ctx *runCtx) evaluate(sub *subschema, instance any) {
	if sub == nil {
		return
	}
	if sub.boolValue != nil {
		if !*sub.boolValue {
			ctx.addError(sub.location, "false", "false", "schema is false; nothing matches")
		}
		return
	}
	// Push dynamic scope when entering a new resource.
	if sub.crossesResource && sub.resourceURI != "" {
		ctx.pushDynamicScope(sub.resourceURI)
		defer ctx.popDynamicScope()
	}
	if ctx.validationDepth > ctx.opts.maxValidationDepth {
		ctx.addError(sub.location, "", "", ErrMaxValidationDepth.Error())
		return
	}
	ctx.validationDepth++
	defer func() { ctx.validationDepth-- }()
	for _, ev := range sub.evaluators {
		if ctx.shouldStop() {
			return
		}
		ev.eval(ctx, instance)
	}
}

// evaluateChild evaluates sub against instance with the instance and schema
// pointer-token fragments pushed temporarily.
func (ctx *runCtx) evaluateChild(sub *subschema, instance any, instanceTok, schemaTok string) {
	ctx.pushInstance(instanceTok)
	defer ctx.popInstance()
	ctx.pushSchema(schemaTok)
	defer ctx.popSchema()
	ctx.evaluate(sub, instance)
}

// evaluateBranch evaluates sub against instance using a fresh, isolated
// error/annotation buffer. Used by oneOf/anyOf/if/not so each branch's
// failures do not contaminate the outer context. Returns the captured
// errors+annotations for the caller to merge.
//
// The outer-context stopFired flag is preserved across the call so that
// stop-on-first-error short-circuits the parent walk even when a branch
// produces an isolated error.
func (ctx *runCtx) evaluateBranch(sub *subschema, instance any) ([]ValidationError, []annotationEntry) {
	saved := ctx.errors
	savedAnno := ctx.annoEntries
	savedAnnoMap := ctx.annotations
	savedStop := ctx.stopFired
	ctx.errors = nil
	ctx.annoEntries = nil
	ctx.annotations = make(map[string]map[string]any)
	// branches are speculative — they collect their own errors but should
	// not propagate stopFired to siblings; we restore the saved flag below.
	ctx.stopFired = false
	ctx.evaluate(sub, instance)
	br := ctx.errors
	annos := ctx.annoEntries
	ctx.errors = saved
	ctx.annoEntries = savedAnno
	ctx.annotations = savedAnnoMap
	ctx.stopFired = savedStop
	return br, annos
}

// failure helpers ---------------------------------------------------------.

// addError appends a ValidationError at the current instance/schema location.
func (ctx *runCtx) addError(keywordLoc, keyword, _, msg string) {
	if ctx.opts.maxErrors > 0 && len(ctx.errors) >= ctx.opts.maxErrors {
		return
	}
	ctx.errors = append(ctx.errors, ValidationError{
		KeywordLocation:  keywordLoc,
		InstanceLocation: ctx.instanceLocation(),
		Keyword:          keyword,
		Message:          msg,
	})
	if ctx.opts.stopOnFirstError {
		ctx.stopFired = true
	}
}

// addCausesError appends a compound error with nested causes.
func (ctx *runCtx) addCausesError(keywordLoc, keyword, msg string, causes []ValidationError) {
	if ctx.opts.maxErrors > 0 && len(ctx.errors) >= ctx.opts.maxErrors {
		return
	}
	ctx.errors = append(ctx.errors, ValidationError{
		KeywordLocation:  keywordLoc,
		InstanceLocation: ctx.instanceLocation(),
		Keyword:          keyword,
		Message:          msg,
		Causes:           causes,
	})
	if ctx.opts.stopOnFirstError {
		ctx.stopFired = true
	}
}

// shouldStop reports whether validation should stop early.
func (ctx *runCtx) shouldStop() bool {
	if ctx.opts.stopOnFirstError && (ctx.stopFired || len(ctx.errors) > 0) {
		return true
	}
	return false
}

// instanceLocation returns the current instance location as a JSON Pointer.
func (ctx *runCtx) instanceLocation() string {
	if len(ctx.instanceLoc) == 0 {
		return ""
	}
	var b strings.Builder
	for _, tok := range ctx.instanceLoc {
		b.WriteByte('/')
		b.WriteString(escapePointerToken(tok))
	}
	return b.String()
}

// pushInstance / popInstance manage the instance-location pointer.
func (ctx *runCtx) pushInstance(tok string) {
	ctx.instanceLoc = append(ctx.instanceLoc, tok)
}
func (ctx *runCtx) popInstance() {
	if n := len(ctx.instanceLoc); n > 0 {
		ctx.instanceLoc = ctx.instanceLoc[:n-1]
	}
}

func (ctx *runCtx) pushSchema(tok string) {
	ctx.schemaLoc = append(ctx.schemaLoc, tok)
}
func (ctx *runCtx) popSchema() {
	if n := len(ctx.schemaLoc); n > 0 {
		ctx.schemaLoc = ctx.schemaLoc[:n-1]
	}
}

// pushDynamicScope / popDynamicScope manage the dynamic scope chain.
func (ctx *runCtx) pushDynamicScope(uri string) {
	ctx.dynamicScope = append(ctx.dynamicScope, uri)
}
func (ctx *runCtx) popDynamicScope() {
	if n := len(ctx.dynamicScope); n > 0 {
		ctx.dynamicScope = ctx.dynamicScope[:n-1]
	}
}

func newRunCtx(schema *Schema, ro *runOptions) *runCtx {
	ctx := &runCtx{
		schema:      schema,
		opts:        ro,
		annotations: map[string]map[string]any{},
	}
	if schema != nil && schema.id != "" {
		ctx.dynamicScope = append(ctx.dynamicScope, schema.id)
	} else if schema != nil && schema.resources != nil && schema.resources.rootURI != "" {
		ctx.dynamicScope = append(ctx.dynamicScope, schema.resources.rootURI)
	}
	return ctx
}

// release lets the ctx be returned to a pool. Currently no pooling — the
// method exists so we can plug a sync.Pool later without a public-API churn.
func (ctx *runCtx) release() {}

// addAnnotation records an annotation for the keyword at the current
// instance location. The collectAnnotations option only gates the public
// surface (publicAnnotations), not the internal record — unevaluated* and
// branch-merging both consult ctx.annoEntries / ctx.annotations regardless.
func (ctx *runCtx) addAnnotation(keywordLoc, keyword string, value any) {
	loc := ctx.instanceLocation()
	if ctx.annotations[loc] == nil {
		ctx.annotations[loc] = map[string]any{}
	}
	ctx.annotations[loc][keyword] = value
	ctx.annoEntries = append(ctx.annoEntries, annotationEntry{
		keywordLoc:  keywordLoc,
		instanceLoc: loc,
		keyword:     keyword,
		value:       value,
	})
}

// publicAnnotations converts the internal entry log into the public-Result
// shape, filtering out internal-only entries.
func (ctx *runCtx) publicAnnotations() []Annotation {
	out := make([]Annotation, 0, len(ctx.annoEntries))
	for _, e := range ctx.annoEntries {
		out = append(out, Annotation{
			KeywordLocation:  e.keywordLoc,
			InstanceLocation: e.instanceLoc,
			Keyword:          e.keyword,
			Value:            e.value,
		})
	}
	return out
}

// getAnnotation fetches the annotation for keyword at the current instance
// location, if any.
func (ctx *runCtx) getAnnotation(keyword string) (any, bool) {
	loc := ctx.instanceLocation()
	if m, ok := ctx.annotations[loc]; ok {
		if v, ok := m[keyword]; ok {
			return v, true
		}
	}
	return nil, false
}

// addBranchAnnotations merges annotations captured in evaluateBranch back
// into the running context.
func (ctx *runCtx) addBranchAnnotations(annos []annotationEntry) {
	for _, e := range annos {
		if ctx.annotations[e.instanceLoc] == nil {
			ctx.annotations[e.instanceLoc] = map[string]any{}
		}
		// Merge: arrays/sets are unioned where applicable; otherwise the
		// last-write-wins for scalars.
		if existing, ok := ctx.annotations[e.instanceLoc][e.keyword]; ok {
			ctx.annotations[e.instanceLoc][e.keyword] = mergeAnnotation(existing, e.value)
		} else {
			ctx.annotations[e.instanceLoc][e.keyword] = e.value
		}
		ctx.annoEntries = append(ctx.annoEntries, e)
	}
}

// mergeAnnotation combines two annotation values from sibling applicator
// branches. For evaluated-set annotations (returned as map[string]struct{}
// or map[int]struct{}) we union; otherwise last-wins.
func mergeAnnotation(a, b any) any {
	switch av := a.(type) {
	case evaluatedKeys:
		if bv, ok := b.(evaluatedKeys); ok {
			out := make(evaluatedKeys, len(av)+len(bv))
			for k := range av {
				out[k] = struct{}{}
			}
			for k := range bv {
				out[k] = struct{}{}
			}
			return out
		}
	case evaluatedItems:
		if bv, ok := b.(evaluatedItems); ok {
			if av < bv {
				return bv
			}
			return av
		}
	case evaluatedItemsAll:
		return a
	}
	if _, ok := a.(evaluatedItemsAll); ok {
		return a
	}
	if _, ok := b.(evaluatedItemsAll); ok {
		return b
	}
	return b
}

// evaluatedKeys is the annotation value used by properties /
// patternProperties / additionalProperties / unevaluatedProperties to track
// which property names have been evaluated. Stored as a set for O(1) lookup.
type evaluatedKeys map[string]struct{}

// evaluatedItems is the annotation value used by prefixItems to record the
// largest index it covered (so unevaluatedItems knows where to start).
type evaluatedItems int

// evaluatedItemsAll is the annotation marker used by `items` (or
// `additionalItems`) to indicate every index past prefixItems was evaluated.
type evaluatedItemsAll struct{}
