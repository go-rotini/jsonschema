package jsonschema

import (
	"fmt"
	"sort"
	"strings"
	"sync"
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

// builderFn constructs the evaluator for one keyword binding. The frame
// carries per-call build state (parent map, location, base URI, resource
// URI, active draft) so concurrent lazy-ref builds do not share scratch
// state on evalBuilder.
type builderFn func(b *evalBuilder, f *buildFrame, raw any, loc string) (evaluator, error)

var keywordBuilders = map[string]builderFn{}

// registerEvaluator wires builder b under keyword name. Called from
// per-file init blocks to keep the keyword table distributed.
func registerEvaluator(name string, b builderFn) {
	keywordBuilders[name] = b
}

// buildFrame is the per-call build context threaded through buildSubschema
// and the keyword builders. Builders that consult sibling keywords (if /
// then / else, contains / max-min-Contains, etc.) read from parent.
type buildFrame struct {
	parent   any
	loc      string
	base     string
	resource string
	draft    Draft
	// ours is the chain's set of in-flight build keys. Cycles within the
	// same chain return the partially-built *subschema; foreign goroutines
	// wait for the build to finish.
	ours map[string]struct{}
}

// evalBuilder is the shared context for assembling the evaluator graph.
// Per-call scratch state lives on a [buildFrame]; cache and inFlight are
// guarded by cacheMu so concurrent validators materializing lazy refs
// share the same subschema map without exposing partial entries.
type evalBuilder struct {
	schema *Schema
	rm     *resourceMap
	loader Loader
	// draft is the root draft for this compile. Per-position drafts flow
	// via buildFrame.draft; this field is the fallback dialect used when
	// runtime ref resolution loads a fresh resource.
	draft Draft
	// cache maps a location key (or synthesized "dyn:..." / "rec:..." key)
	// to its fully built *subschema. cacheMu guards reads and writes.
	cache map[string]*subschema
	// inFlight tracks builds whose evaluator slice has not yet been
	// populated. The pending *subschema pointer is the same instance that
	// eventually lands in cache; done is closed when the build completes.
	inFlight map[string]*pendingSubschema
	cacheMu  sync.Mutex
}

// pendingSubschema is the inFlight value: the *subschema currently being
// built plus a channel that closes once the build promotes it into cache.
type pendingSubschema struct {
	sub  *subschema
	done chan struct{}
}

// buildSubschema enters a fresh resource ($id-bounded) scope: the
// compile-time root entry and the runtime $ref / $dynamicRef /
// $recursiveRef materialization paths.
func (b *evalBuilder) buildSubschema(node any, loc, baseURI, resourceURI string) (*subschema, error) {
	return b.buildSubschemaIn(node, loc, baseURI, resourceURI, true, nil)
}

// buildSubschemaFrame is the entry point for keyword builders descending
// inside a parent schema. The keyword descent never crosses an $id
// boundary on its own; only a nested $id flips crossesResource (handled
// by populateSubschema). The chain's ours set is threaded through so
// cycles within the chain resolve to the same partial *subschema.
func (b *evalBuilder) buildSubschemaFrame(f *buildFrame, node any, loc, baseURI, resourceURI string) (*subschema, error) {
	return b.buildSubschemaIn(node, loc, baseURI, resourceURI, false, f.ours)
}

// buildSubschemaIn does the actual work. ours holds the loc keys whose
// builds are owned by this call chain. On re-entry:
//   - ours[loc] set: cycle within this chain — return the partially-built
//     *subschema (it becomes complete when the outer call returns).
//   - another goroutine owns loc: wait on done, then read from cache.
//
// Foreign goroutines never observe a partially-populated *subschema.
func (b *evalBuilder) buildSubschemaIn(node any, loc, baseURI, resourceURI string, crosses bool, ours map[string]struct{}) (*subschema, error) {
	for {
		b.cacheMu.Lock()
		if existing, ok := b.cache[loc]; ok {
			b.cacheMu.Unlock()
			return existing, nil
		}
		if pending, ok := b.inFlight[loc]; ok {
			if _, mine := ours[loc]; mine {
				b.cacheMu.Unlock()
				return pending.sub, nil
			}
			done := pending.done
			b.cacheMu.Unlock()
			<-done
			continue
		}

		sub := &subschema{
			raw:             node,
			location:        loc,
			baseURI:         baseURI,
			resourceURI:     resourceURI,
			crossesResource: crosses,
			schema:          b.schema,
			keywords:        map[string]struct{}{},
		}
		pending := &pendingSubschema{sub: sub, done: make(chan struct{})}
		if b.inFlight == nil {
			b.inFlight = map[string]*pendingSubschema{}
		}
		b.inFlight[loc] = pending
		b.cacheMu.Unlock()

		// Mark loc as ours so cycles re-entering from our keyword loop
		// recognize the in-flight build as their own.
		if ours == nil {
			ours = map[string]struct{}{}
		}
		ours[loc] = struct{}{}
		err := b.populateSubschema(sub, node, baseURI, resourceURI, ours)
		delete(ours, loc)

		// Promote into cache and remove from inFlight under the same
		// lock acquisition, then signal waiters.
		b.cacheMu.Lock()
		if err == nil {
			b.cache[loc] = sub
		}
		delete(b.inFlight, loc)
		b.cacheMu.Unlock()
		close(pending.done)
		if err != nil {
			return nil, err
		}
		return sub, nil
	}
}

// populateSubschema fills in sub's evaluator slice by walking node's
// keywords. The caller has already published sub into inFlight; ours is
// the chain's set of in-flight keys, threaded through nested builds via
// the buildFrame.
func (b *evalBuilder) populateSubschema(sub *subschema, node any, baseURI, resourceURI string, ours map[string]struct{}) error {
	switch t := node.(type) {
	case bool:
		v := t
		sub.boolValue = &v
		return nil
	case map[string]any:
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

		frame := &buildFrame{
			parent:   t,
			loc:      sub.location,
			base:     baseURI,
			resource: resourceURI,
			draft:    subDraft,
			ours:     ours,
		}

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
			ev, err := builder(b, frame, t[key], sub.location+"/"+escapePointerToken(key))
			if err != nil {
				return err
			}
			if ev != nil {
				sub.evaluators = append(sub.evaluators, ev)
			}
		}
		// Stable sort: applicators / refs first, unevaluated* last,
		// format / metadata at the tail.
		sort.SliceStable(sub.evaluators, func(i, j int) bool {
			return evalPriority(sub.evaluators[i].keyword()) <
				evalPriority(sub.evaluators[j].keyword())
		})
		return nil
	default:
		// Unknown shape — treat as the always-pass schema.
		passing := true
		sub.boolValue = &passing
		return nil
	}
}

// orderKeys returns m's keys in alphabetical order so the evaluator graph
// is deterministic across builds.
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
	case "$ref", "$dynamicRef", "$recursiveRef":
		return 0
	case "type", "enum", "const":
		return 5
	// properties / patternProperties must run before additionalProperties
	// (§12.12) so the latter can read their evaluated-keys annotations.
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
	// stopFired propagates stop-on-first-error across evaluateBranch so
	// the outer walk short-circuits even when a branch's failure is
	// captured into an isolated slice.
	stopFired bool
	// contentDecoded / contentParsed pass intermediate values from
	// contentEncoding → contentMediaType → contentSchema at the same
	// instance location. Lazily allocated.
	contentDecoded map[string][]byte
	contentParsed  map[string]any
	// keyCountFired records instance locations that have already surfaced
	// a $maxKeyCount error so sibling object applicators don't emit
	// duplicate failures for the same overlong instance.
	keyCountFired map[string]bool
	// formatWarned records unknown-format names already written to
	// runOptions.warningSink so a single Validate call dedupes warnings.
	formatWarned map[string]bool
}

// annotationEntry is the internal record of one annotation. unevaluated*
// keywords scan this log to discover sibling-applicator coverage.
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
	if sub.crossesResource && sub.resourceURI != "" {
		ctx.pushDynamicScope(sub.resourceURI)
		defer ctx.popDynamicScope()
	}
	if ctx.validationDepth > ctx.opts.maxValidationDepth {
		// "$maxValidationDepth" is a reserved keyword identifier so
		// callers can switch on ValidationError.Keyword instead of
		// parsing the message.
		ctx.addErrorWithCause(sub.location, "$maxValidationDepth", "",
			ErrMaxValidationDepth.Error(), ErrMaxValidationDepth)
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

// evaluateBranch evaluates sub with a fresh error/annotation buffer so
// branch failures (oneOf / anyOf / if / not) do not contaminate the outer
// context. The captured errors and annotations are returned for the
// caller to merge. The outer stopFired flag is preserved across the call.
func (ctx *runCtx) evaluateBranch(sub *subschema, instance any) ([]ValidationError, []annotationEntry) {
	saved := ctx.errors
	savedAnno := ctx.annoEntries
	savedAnnoMap := ctx.annotations
	savedStop := ctx.stopFired
	ctx.errors = nil
	ctx.annoEntries = nil
	ctx.annotations = make(map[string]map[string]any)
	// Branches are speculative; their stopFired must not affect siblings.
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

// addErrorWithCause is like addError but attaches a typed [Cause] so
// callers can use [errors.As] to extract the underlying error
// (e.g. *FormatError) from a [*ValidationError].
func (ctx *runCtx) addErrorWithCause(keywordLoc, keyword, _, msg string, cause error) {
	if ctx.opts.maxErrors > 0 && len(ctx.errors) >= ctx.opts.maxErrors {
		return
	}
	ctx.errors = append(ctx.errors, ValidationError{
		KeywordLocation:  keywordLoc,
		InstanceLocation: ctx.instanceLocation(),
		Keyword:          keyword,
		Message:          msg,
		Cause:            cause,
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

// checkMaxKeyCount enforces the [WithMaxKeyCount] cap. When obj has more
// keys than the configured cap, an error is appended at the current
// instance location and the function returns false to instruct the calling
// applicator to skip its iteration. Returns true (caller should proceed)
// when the cap is unset or not exceeded. The error is emitted at most
// once per instance location so sibling applicators don't pile on.
func (ctx *runCtx) checkMaxKeyCount(obj map[string]any, keywordLoc string) bool {
	if ctx.opts.maxKeyCount <= 0 {
		return true
	}
	if len(obj) <= ctx.opts.maxKeyCount {
		return true
	}
	loc := ctx.instanceLocation()
	if ctx.keyCountFired == nil {
		ctx.keyCountFired = map[string]bool{}
	}
	if ctx.keyCountFired[loc] {
		return false
	}
	ctx.keyCountFired[loc] = true
	ctx.addErrorWithCause(keywordLoc, "$maxKeyCount", "",
		ErrMaxKeyCount.Error(), ErrMaxKeyCount)
	return false
}

// emitFormatWarning writes a single deduplicated unknown-format warning to
// the configured [WithWarningSink]. No-op when the sink is unset.
func (ctx *runCtx) emitFormatWarning(format string) {
	if ctx.opts == nil || ctx.opts.warningSink == nil {
		return
	}
	if ctx.formatWarned == nil {
		ctx.formatWarned = map[string]bool{}
	}
	if ctx.formatWarned[format] {
		return
	}
	ctx.formatWarned[format] = true
	_, _ = fmt.Fprintf(ctx.opts.warningSink,
		"jsonschema: unknown format %q\n", format)
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

// release is a sync.Pool placeholder so future pooling can be added
// without a public-API churn.
func (ctx *runCtx) release() {}

// addAnnotation records an annotation at the current instance location.
// The collectAnnotations option only gates the public surface; the
// internal record always feeds unevaluated* and branch merging.
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

// publicAnnotations converts the internal entry log into Result.Annotations.
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
		// Sets union; scalars last-write-wins.
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
