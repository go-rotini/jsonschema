package jsonschema

import (
	"fmt"
	"net/url"
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
	// absURI and absPtr are the canonical location of this subschema: the
	// absolute URI of its resource and its JSON Pointer within it. absURI
	// is empty when the resource has no absolute URI.
	absURI string
	absPtr string
	// evaluators are the per-keyword evaluators for this subschema in
	// declaration-stable order. allOf/anyOf/oneOf/$ref/applicator evaluators
	// come before unevaluated* / format / metadata.
	evaluators []evaluator
	// keywords records the recognized keyword names so unevaluated* can
	// detect sibling keywords without rescanning the raw map.
	keywords map[string]struct{}
	// readsScope is true when an unevaluated* keyword here reads the
	// annotations of this subschema's in-place evaluation.
	readsScope bool
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

// keywordBuilders maps keyword names to their builders. It is filled on
// first use rather than at package init, so a program that imports the
// package without compiling a schema pays nothing for it and the linker
// can drop every evaluator.
var (
	keywordBuilders     map[string]builderFn
	keywordBuildersOnce sync.Once
)

// lookupBuilder returns the builder registered for keyword name.
func lookupBuilder(name string) (builderFn, bool) {
	keywordBuildersOnce.Do(registerBuiltinKeywords)
	b, ok := keywordBuilders[name]
	return b, ok
}

// registerBuiltinKeywords fills the keyword table. The per-file
// register* functions keep the table distributed next to the evaluators.
func registerBuiltinKeywords() {
	registerRefKeywords()
	registerCombinatorKeywords()
	registerConditionalKeywords()
	registerDependencyKeywords()
	registerObjectApplicatorKeywords()
	registerArrayApplicatorKeywords()
	registerUnevaluatedKeywords()
	registerTypeKeyword()
	registerEnumConstKeywords()
	registerNumericKeywords()
	registerStringKeywords()
	registerArrayKeywords()
	registerObjectKeywords()
	registerFormatAndContentKeywords()
	registerOpenAPIKeywords()
}

// registerEvaluator wires builder b under keyword name. A name that is
// already registered keeps its builder, so an entry added before the
// built-ins are filled in is not overwritten.
func registerEvaluator(name string, b builderFn) {
	if keywordBuilders == nil {
		keywordBuilders = make(map[string]builderFn, 64)
	}
	if _, taken := keywordBuilders[name]; taken {
		return
	}
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
	// absURI and absPtr are the parent subschema's canonical location.
	absURI string
	absPtr string
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
	// frozen is set once compilation has built every reachable
	// subschema; from then on references resolve without the loader.
	frozen bool
	// vocabOff holds the standard vocabularies the root's custom meta-schema
	// leaves out; their keywords do not apply in the compiled document.
	vocabOff map[string]bool
	// formatAsserts is set when the root's custom meta-schema lists the
	// format-assertion vocabulary: format asserts in the compiled document.
	formatAsserts bool
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
	absURI, absPtr := b.canonicalLocation(node, loc, resourceURI)
	return b.buildSubschemaIn(node, loc, baseURI, resourceURI, absURI, absPtr, true, nil)
}

// canonicalLocation returns the canonical location of node, built under
// location key loc in resource resourceURI: from the resource walk for an
// object, otherwise from loc when it is "#" or an absolute URI with a JSON
// Pointer fragment. absURI is empty when the location is unknown or not
// absolute.
func (b *evalBuilder) canonicalLocation(node any, loc, resourceURI string) (absURI, absPtr string) {
	if m, ok := node.(map[string]any); ok && b.rm != nil {
		if l, ok := b.rm.location(m); ok {
			return absoluteOrEmpty(l.uri), l.ptr
		}
	}
	if loc == "#" {
		return absoluteOrEmpty(resourceURI), ""
	}
	doc, frag := splitFragment(loc)
	switch {
	case frag == "" || frag == "#":
		return absoluteOrEmpty(doc), ""
	case strings.HasPrefix(frag, "#/"):
		ptr, err := url.PathUnescape(frag[1:])
		if err != nil {
			return "", ""
		}
		return absoluteOrEmpty(doc), ptr
	}
	return "", ""
}

// absoluteOrEmpty returns uri when it is an absolute URI, otherwise "".
func absoluteOrEmpty(uri string) string {
	if uri == "" {
		return ""
	}
	u, err := url.Parse(uri)
	if err != nil || !u.IsAbs() {
		return ""
	}
	return uri
}

// buildSubschemaFrame is the entry point for keyword builders descending
// inside a parent schema. The keyword descent never crosses an $id
// boundary on its own; only a nested $id flips crossesResource (handled
// by populateSubschema). The chain's ours set is threaded through so
// cycles within the chain resolve to the same partial *subschema.
func (b *evalBuilder) buildSubschemaFrame(f *buildFrame, node any, loc, baseURI, resourceURI string) (*subschema, error) {
	absURI, absPtr := "", ""
	if rest, ok := strings.CutPrefix(loc, f.loc); ok && f.absURI != "" {
		absURI, absPtr = f.absURI, f.absPtr+rest
	}
	return b.buildSubschemaIn(node, loc, baseURI, resourceURI, absURI, absPtr, false, f.ours)
}

// buildSubschemaIn does the actual work. ours holds the loc keys whose
// builds are owned by this call chain. On re-entry:
//   - ours[loc] set: cycle within this chain — return the partially-built
//     *subschema (it becomes complete when the outer call returns).
//   - another goroutine owns loc: wait on done, then read from cache.
//
// Foreign goroutines never observe a partially-populated *subschema.
func (b *evalBuilder) buildSubschemaIn(node any, loc, baseURI, resourceURI, absURI, absPtr string, crosses bool, ours map[string]struct{}) (*subschema, error) {
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
			absURI:          absURI,
			absPtr:          absPtr,
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
		subDraft := b.draftAt(t)
		_, hasRef := t["$ref"]
		// Before 2019-09, $ref replaces every sibling keyword, $id included.
		refOnly := hasRef && subDraft <= Draft7
		if !refOnly {
			baseURI, resourceURI = b.applyID(sub, t, subDraft.IDKeyword(), baseURI, resourceURI)
		}

		frame := &buildFrame{
			parent:   t,
			loc:      sub.location,
			base:     baseURI,
			resource: resourceURI,
			draft:    subDraft,
			absURI:   sub.absURI,
			absPtr:   sub.absPtr,
			ours:     ours,
		}

		keys := orderKeys(t)
		if refOnly {
			keys = []string{"$ref"}
		}
		for _, key := range keys {
			if _, known := LookupKeyword(key, subDraft); !known {
				continue
			}
			if b.vocabularyOff(key, t) {
				continue
			}
			sub.keywords[key] = struct{}{}
			if key == "unevaluatedProperties" || key == "unevaluatedItems" {
				sub.readsScope = true
			}
			builder, ok := lookupBuilder(key)
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
		return &CompileError{
			KeywordLocation: sub.location,
			Message:         "schema must be an object or a boolean",
			Cause:           ErrUnsupportedSchemaShape,
		}
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
	case "$ref", keyDynamicRef, "$recursiveRef":
		return 0
	case "type", "enum", "const":
		return 5
	// properties / patternProperties run before additionalProperties, the
	// order JSON Schema Core 2020-12 §10.3.2.3 describes, so their errors
	// come first.
	case keyProperties, keyPatternProperties:
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
	schema *Schema
	opts   *runOptions
	// cur is the subschema being evaluated; its canonical location turns a
	// keyword location into an absolute one.
	cur *subschema
	// refPath holds the references crossed to reach cur, innermost last;
	// it maps a keyword's static location to its evaluation path.
	// refBuf backs it so shallow ref chains need no allocation.
	refPath []refFrame
	refBuf  [4]refFrame
	// instancePtr is the escaped JSON Pointer of the current instance;
	// instanceLoc holds the pointers of its ancestors.
	instancePtr  string
	instanceLoc  []string
	schemaLoc    []string
	dynamicScope []string // chain of in-scope resource URIs
	errors       []ValidationError
	// failCount counts every failure reported, including those dropped by
	// the error cap, so a subschema can tell whether it passed.
	failCount int
	// scope holds the evaluated-location annotations of the subschema
	// being evaluated, which unevaluated* keywords read. It covers only
	// that subschema's keywords and the in-place subschemas that passed.
	scope *annoScope
	// needScope reports whether an unevaluated* keyword may read the
	// current scope; when it is false anyOf and oneOf may stop early.
	needScope bool
	// public reports whether the annotation log is built for the result.
	public          bool
	annoEntries     []annotationEntry // ordered annotation log for output
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
	// evals counts subschema evaluations against evalLimit (no limit when
	// evalLimit is negative); overBudget is set once evals exceeds it and
	// stops the walk.
	evals      int
	evalLimit  int
	evalAuto   bool
	overBudget bool
}

// Evaluation budget defaults: evalsPerValue evaluations for each value in
// the instance, and at least minEvalBudget.
const (
	evalsPerValue = 100
	minEvalBudget = 1_000_000
)

// setEvalLimit sets the evaluation budget for validating instance.
func (ctx *runCtx) setEvalLimit(instance any) {
	if ctx.opts.maxEvaluations != 0 {
		ctx.evalLimit = ctx.opts.maxEvaluations
		return
	}
	ctx.evalAuto = true
	ctx.evalLimit = max(minEvalBudget, evalsPerValue*countValues(instance))
}

// growEvalLimit extends a default budget for decoded content about to be
// validated.
func (ctx *runCtx) growEvalLimit(content any) {
	if ctx.evalAuto {
		ctx.evalLimit += evalsPerValue * countValues(content)
	}
}

// chargeError counts the error just appended against the evaluation
// budget, one evaluation per 32 bytes it holds, so the budget also bounds
// the memory that errors from a branching schema can take.
func (ctx *runCtx) chargeError() {
	e := &ctx.errors[len(ctx.errors)-1]
	ctx.evals += (128 + len(e.KeywordLocation) + len(e.AbsoluteKeywordLocation) +
		len(e.InstanceLocation) + len(e.Message)) / 32
	if ctx.evalLimit >= 0 && ctx.evals > ctx.evalLimit {
		ctx.overBudget = true
	}
}

// countValues returns the number of JSON values in v, v included, up to
// maxCountedValues; the cap also ends the walk of a Go value that contains
// itself.
func countValues(v any) int {
	n := 0
	stack := []any{v}
	for len(stack) > 0 && n < maxCountedValues {
		x := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		n++
		switch t := x.(type) {
		case []any:
			stack = append(stack, t...)
		case map[string]any:
			for _, c := range t {
				stack = append(stack, c)
			}
		}
	}
	return n
}

// maxCountedValues bounds [countValues].
const maxCountedValues = 1 << 26

// refFrame records one $ref, $dynamicRef or $recursiveRef crossed during
// evaluation.
type refFrame struct {
	// from is the static location of the reference keyword.
	from string
	// root is the static location of the subschema the reference entered.
	root string
	// path is the evaluation path of root; set once hasPath is true.
	path    string
	hasPath bool
}

// annotationEntry is the internal record of one annotation. unevaluated*
// keywords scan this log to discover sibling-applicator coverage.
type annotationEntry struct {
	keywordLoc  string
	instanceLoc string
	keyword     string
	value       any
	// sub is the subschema that produced the annotation.
	sub *subschema
	// When pathCut > 0, the evaluation path of the keyword is pathPrefix
	// followed by keywordLoc[pathCut:]; otherwise it is keywordLoc.
	pathPrefix string
	pathCut    int
}

// evaluate applies sub in place: to the instance the current subschema is
// evaluating. When sub passes, its annotations join the current scope.
func (ctx *runCtx) evaluate(sub *subschema, instance any) {
	outer := ctx.scope
	ctx.scope = nil
	fails := ctx.failCount
	ctx.run(sub, instance)
	inner := ctx.scope
	ctx.scope = outer
	if inner != nil && ctx.failCount == fails {
		ctx.adoptScope(inner)
	}
}

// adoptScope merges s into the current scope.
func (ctx *runCtx) adoptScope(s *annoScope) {
	if ctx.scope == nil {
		ctx.scope = s
		return
	}
	ctx.scope.merge(s)
}

// run walks every evaluator on sub against instance.
func (ctx *runCtx) run(sub *subschema, instance any) {
	if sub == nil || ctx.overBudget {
		return
	}
	ctx.evals++
	if ctx.evalLimit >= 0 && ctx.evals > ctx.evalLimit {
		ctx.overBudget = true
		return
	}
	if sub.readsScope && !ctx.needScope {
		ctx.needScope = true
		defer func() { ctx.needScope = false }()
	}
	prev := ctx.cur
	ctx.cur = sub
	defer func() { ctx.cur = prev }()
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

// evaluateChild evaluates sub against instance, a child of the current
// instance, with the instance and schema pointer-token fragments pushed
// temporarily. The child's annotations describe a different instance, so
// they never join the current scope.
func (ctx *runCtx) evaluateChild(sub *subschema, instance any, instanceTok, schemaTok string) {
	ctx.pushInstance(instanceTok)
	defer ctx.popInstance()
	ctx.pushSchema(schemaTok)
	defer ctx.popSchema()
	outer, need, depth := ctx.scope, ctx.needScope, ctx.refDepth
	// The $ref depth limit counts hops at one instance location.
	ctx.scope, ctx.needScope, ctx.refDepth = nil, false, 0
	ctx.run(sub, instance)
	ctx.scope, ctx.needScope, ctx.refDepth = outer, need, depth
}

// evaluateRef evaluates sub, the target of the reference keyword at static
// location refLoc, recording the crossing so keyword locations inside sub
// report the evaluation path through refLoc.
func (ctx *runCtx) evaluateRef(refLoc string, sub *subschema, instance any) {
	ctx.refPath = append(ctx.refPath, refFrame{from: refLoc, root: sub.location})
	defer func() { ctx.refPath = ctx.refPath[:len(ctx.refPath)-1] }()
	ctx.evaluate(sub, instance)
}

// keywordPath returns the evaluation path (JSON Schema Core 2020-12
// §12.3.1) of the keyword at static location loc in the subschema being
// evaluated.
func (ctx *runCtx) keywordPath(loc string) string {
	n := len(ctx.refPath)
	if n == 0 {
		return loc
	}
	if rest, ok := pathRest(loc, ctx.refPath[n-1].root); ok {
		return ctx.anchorPath(n-1) + rest
	}
	return loc
}

// anchorPath returns the evaluation path of refPath[i].root, computing it
// on first use.
func (ctx *runCtx) anchorPath(i int) string {
	f := &ctx.refPath[i]
	if !f.hasPath {
		f.path = f.from
		if i > 0 {
			if rest, ok := pathRest(f.from, ctx.refPath[i-1].root); ok {
				f.path = ctx.anchorPath(i-1) + rest
			}
		}
		f.hasPath = true
	}
	return f.path
}

// pathRest returns the part of static location loc below the subschema at
// static location root, reporting false when loc is not root or under it.
func pathRest(loc, root string) (string, bool) {
	rest, ok := strings.CutPrefix(loc, root)
	return rest, ok && (rest == "" || rest[0] == '/')
}

// branchAnnotations are the annotations a branch produced: its log entries
// and its scope.
type branchAnnotations struct {
	entries []annotationEntry
	scope   *annoScope
}

// evaluateBranch evaluates sub in place with fresh error and annotation
// buffers, so branch failures (oneOf / anyOf / if / not) do not contaminate
// the outer context. The captured errors and annotations are returned for
// the caller to merge. The outer stopFired flag is preserved across the
// call.
func (ctx *runCtx) evaluateBranch(sub *subschema, instance any) ([]ValidationError, branchAnnotations) {
	saved := ctx.errors
	savedAnno := ctx.annoEntries
	savedScope := ctx.scope
	savedStop := ctx.stopFired
	savedFails := ctx.failCount
	ctx.errors = nil
	ctx.annoEntries = nil
	ctx.scope = nil
	// Branches are speculative; their stopFired must not affect siblings.
	ctx.stopFired = false
	ctx.run(sub, instance)
	br := ctx.errors
	annos := branchAnnotations{entries: ctx.annoEntries, scope: ctx.scope}
	ctx.errors = saved
	ctx.annoEntries = savedAnno
	ctx.scope = savedScope
	ctx.stopFired = savedStop
	ctx.failCount = savedFails
	return br, annos
}

// addError appends a ValidationError at the current instance/schema location.
func (ctx *runCtx) addError(keywordLoc, keyword, _, msg string) {
	ctx.failCount++
	if ctx.opts.maxErrors > 0 && len(ctx.errors) >= ctx.opts.maxErrors {
		return
	}
	ctx.errors = append(ctx.errors, ValidationError{
		KeywordLocation:         ctx.keywordPath(keywordLoc),
		AbsoluteKeywordLocation: ctx.absoluteKeywordLocation(keywordLoc),
		InstanceLocation:        ctx.instanceLocation(),
		Keyword:                 keyword,
		Message:                 msg,
	})
	ctx.chargeError()
	if ctx.opts.stopOnFirstError {
		ctx.stopFired = true
	}
}

// addErrorWithCause is like addError but attaches a typed [Cause] so
// callers can use [errors.As] to extract the underlying error
// (e.g. *FormatError) from a [*ValidationError].
func (ctx *runCtx) addErrorWithCause(keywordLoc, keyword, _, msg string, cause error) {
	ctx.failCount++
	if ctx.opts.maxErrors > 0 && len(ctx.errors) >= ctx.opts.maxErrors {
		return
	}
	ctx.errors = append(ctx.errors, ValidationError{
		KeywordLocation:         ctx.keywordPath(keywordLoc),
		AbsoluteKeywordLocation: ctx.absoluteKeywordLocation(keywordLoc),
		InstanceLocation:        ctx.instanceLocation(),
		Keyword:                 keyword,
		Message:                 msg,
		Cause:                   cause,
	})
	ctx.chargeError()
	if ctx.opts.stopOnFirstError {
		ctx.stopFired = true
	}
}

// addCausesError appends a compound error with nested causes.
func (ctx *runCtx) addCausesError(keywordLoc, keyword, msg string, causes []ValidationError) {
	ctx.failCount++
	if ctx.opts.maxErrors > 0 && len(ctx.errors) >= ctx.opts.maxErrors {
		return
	}
	ctx.errors = append(ctx.errors, ValidationError{
		KeywordLocation:         ctx.keywordPath(keywordLoc),
		AbsoluteKeywordLocation: ctx.absoluteKeywordLocation(keywordLoc),
		InstanceLocation:        ctx.instanceLocation(),
		Keyword:                 keyword,
		Message:                 msg,
		Causes:                  causes,
	})
	ctx.chargeError()
	if ctx.opts.stopOnFirstError {
		ctx.stopFired = true
	}
}

// absoluteKeywordLocation returns the absolute form of keywordLoc, a keyword
// location within the subschema being evaluated: the canonical URI of the
// subschema's resource with a JSON Pointer fragment (JSON Schema Core
// 2020-12 §12.3.2). It returns "" when the resource has no absolute URI.
func (ctx *runCtx) absoluteKeywordLocation(keywordLoc string) string {
	return absoluteKeywordLocation(ctx.cur, keywordLoc)
}

func absoluteKeywordLocation(sub *subschema, keywordLoc string) string {
	if sub == nil || sub.absURI == "" {
		return ""
	}
	rest, ok := strings.CutPrefix(keywordLoc, sub.location)
	if !ok || (rest != "" && rest[0] != '/') {
		return ""
	}
	return sub.absURI + "#" + (&url.URL{Fragment: sub.absPtr + rest}).EscapedFragment()
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
	if ctx.overBudget {
		return true
	}
	if ctx.opts.stopOnFirstError && (ctx.stopFired || len(ctx.errors) > 0) {
		return true
	}
	return false
}

// instanceLocation returns the current instance location as a JSON Pointer.
func (ctx *runCtx) instanceLocation() string {
	return ctx.instancePtr
}

// pushInstance / popInstance manage the instance-location pointer, which is
// kept escaped so reading it costs nothing.
func (ctx *runCtx) pushInstance(tok string) {
	ctx.instanceLoc = append(ctx.instanceLoc, ctx.instancePtr)
	ctx.instancePtr += "/" + escapePointerToken(tok)
}
func (ctx *runCtx) popInstance() {
	if n := len(ctx.instanceLoc); n > 0 {
		ctx.instancePtr = ctx.instanceLoc[n-1]
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
		schema:    schema,
		opts:      ro,
		evalLimit: -1,
		public:    ro.collectAnnotations && !ro.stopOnFirstError,
	}
	ctx.refPath = ctx.refBuf[:0]
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

// addAnnotation logs an annotation at the current instance location.
func (ctx *runCtx) addAnnotation(keywordLoc, keyword string, value any) {
	if !ctx.public {
		return
	}
	ctx.appendAnnotationEntry(keywordLoc, keyword, value)
}

// appendAnnotationEntry logs an annotation produced by the keyword at static
// location keywordLoc in the subschema being evaluated. Nothing is logged
// when the public annotations will not be built.
func (ctx *runCtx) appendAnnotationEntry(keywordLoc, keyword string, value any) {
	if !ctx.public {
		return
	}
	e := annotationEntry{
		sub:         ctx.cur,
		keywordLoc:  keywordLoc,
		instanceLoc: ctx.instanceLocation(),
		keyword:     keyword,
		value:       value,
	}
	if n := len(ctx.refPath); n > 0 {
		root := ctx.refPath[n-1].root
		if _, ok := pathRest(keywordLoc, root); ok {
			e.pathPrefix = ctx.anchorPath(n - 1)
			e.pathCut = len(root)
		}
	}
	ctx.annoEntries = append(ctx.annoEntries, e)
}

// publicAnnotations converts the internal entry log into Result.Annotations.
func (ctx *runCtx) publicAnnotations() []Annotation {
	out := make([]Annotation, 0, len(ctx.annoEntries))
	for _, e := range ctx.annoEntries {
		path := e.keywordLoc
		if e.pathCut > 0 {
			path = e.pathPrefix + e.keywordLoc[e.pathCut:]
		}
		out = append(out, Annotation{
			KeywordLocation:         path,
			AbsoluteKeywordLocation: absoluteKeywordLocation(e.sub, e.keywordLoc),
			InstanceLocation:        e.instanceLoc,
			Keyword:                 e.keyword,
			Value:                   sanitizeAnnotation(e.value),
		})
	}
	return out
}

// addBranchAnnotations merges the annotations of a passing branch captured
// by evaluateBranch into the running context.
func (ctx *runCtx) addBranchAnnotations(b branchAnnotations) {
	ctx.annoEntries = append(ctx.annoEntries, b.entries...)
	if b.scope != nil {
		ctx.adoptScope(b.scope)
	}
}

// annoScope records which parts of the instance a subschema evaluated,
// for unevaluatedProperties and unevaluatedItems.
type annoScope struct {
	// props holds the evaluated property names.
	props map[string]struct{}
	// prefix is the number of leading array items evaluated.
	prefix int
	// allItems is true when every array item was evaluated.
	allItems bool
	// contained holds the indexes of array items that matched contains.
	contained map[int]struct{}
}

// scopeForWrite returns the current scope, creating it when needed.
func (ctx *runCtx) scopeForWrite() *annoScope {
	if ctx.scope == nil {
		ctx.scope = &annoScope{}
	}
	return ctx.scope
}

// tracksEvaluated reports whether anything reads the locations a keyword
// evaluates: an unevaluated* keyword through the scope, or the public
// annotation log.
func (ctx *runCtx) tracksEvaluated() bool { return ctx.needScope || ctx.public }

// The mark* methods record evaluated locations in the scope. They do
// nothing while needScope is false: no unevaluated* keyword can then read
// the scope, since needScope stays set below any subschema that reads it
// until the walk moves to a child instance, which starts a fresh scope.

// markProps records evaluated property names.
func (ctx *runCtx) markProps(keys evaluatedKeys) {
	if !ctx.needScope {
		return
	}
	s := ctx.scopeForWrite()
	if s.props == nil {
		s.props = make(map[string]struct{}, len(keys))
	}
	for k := range keys {
		s.props[k] = struct{}{}
	}
}

// markPrefix records that the first n array items were evaluated.
func (ctx *runCtx) markPrefix(n int) {
	if !ctx.needScope {
		return
	}
	s := ctx.scopeForWrite()
	s.prefix = max(s.prefix, n)
}

// markAllItems records that every array item was evaluated.
func (ctx *runCtx) markAllItems() {
	if !ctx.needScope {
		return
	}
	ctx.scopeForWrite().allItems = true
}

// markContained records the array items that matched contains.
func (ctx *runCtx) markContained(idx []int) {
	if !ctx.needScope {
		return
	}
	s := ctx.scopeForWrite()
	if s.contained == nil {
		s.contained = make(map[int]struct{}, len(idx))
	}
	for _, i := range idx {
		s.contained[i] = struct{}{}
	}
}

// merge adds o's evaluated locations to s.
func (s *annoScope) merge(o *annoScope) {
	if len(o.props) > 0 {
		if s.props == nil {
			s.props = make(map[string]struct{}, len(o.props))
		}
		for k := range o.props {
			s.props[k] = struct{}{}
		}
	}
	s.prefix = max(s.prefix, o.prefix)
	s.allItems = s.allItems || o.allItems
	if len(o.contained) > 0 {
		if s.contained == nil {
			s.contained = make(map[int]struct{}, len(o.contained))
		}
		for i := range o.contained {
			s.contained[i] = struct{}{}
		}
	}
}

// evaluatedKeys is the annotation value of properties /
// patternProperties / additionalProperties / unevaluatedProperties and
// contains: the property names (or item indexes) evaluated. It is reported
// publicly as a sorted []string.
type evaluatedKeys map[string]struct{}

// evaluatedItems is the annotation value of prefixItems (and the
// array form of items): the largest index covered. It is reported publicly
// as an int.
type evaluatedItems int

// evaluatedItemsAll is the annotation value of items, additionalItems and
// unevaluatedItems when they applied to every remaining item. It is
// reported publicly as true.
type evaluatedItemsAll struct{}

// resourceRoot returns the canonical location the resource walk recorded
// for node, if it walked node.
func (b *evalBuilder) resourceRoot(node map[string]any) (schemaLocation, bool) {
	if b.rm == nil {
		return schemaLocation{}, false
	}
	return b.rm.location(node)
}

// targetBase returns the base URI in effect at target: the URI of the
// resource the walk found it in, or fallback when target was not walked.
func (b *evalBuilder) targetBase(target any, fallback string) string {
	if m, ok := target.(map[string]any); ok {
		if l, ok := b.resourceRoot(m); ok && l.uri != "" {
			return l.uri
		}
	}
	return fallback
}

// buildAll builds every subschema validation can reach: the targets of
// every reference, and for dynamic references every schema in the resource
// map that the dynamic scope could select. Remote documents are fetched
// here, so a reference that cannot be resolved fails compilation. Once it
// returns, the builder is frozen: validation only reads the subschema cache
// and the resource map.
func (b *evalBuilder) buildAll() error {
	var w refWork
	done := map[*subschema]bool{}
	for {
		pending := b.unvisited(done)
		if len(pending) == 0 {
			break
		}
		for _, sub := range pending {
			done[sub] = true
			if err := w.buildTargets(sub); err != nil {
				return err
			}
		}
		if err := w.buildDynamicTargets(b.rm); err != nil {
			return err
		}
	}
	b.frozen = true
	return nil
}

// unvisited returns the cached subschemas not in done, sorted by location
// so the order of fetches does not depend on map order.
func (b *evalBuilder) unvisited(done map[*subschema]bool) []*subschema {
	b.cacheMu.Lock()
	pending := make([]*subschema, 0, len(b.cache))
	for _, sub := range b.cache {
		if !done[sub] {
			pending = append(pending, sub)
		}
	}
	b.cacheMu.Unlock()
	sort.Slice(pending, func(i, j int) bool { return pending[i].location < pending[j].location })
	return pending
}

// refWork collects the dynamic and recursive references buildAll has seen.
type refWork struct {
	dynamic   []*dynamicRefEval
	recursive []*recursiveRefEval
}

// buildTargets builds the static targets of sub's reference keywords.
func (w *refWork) buildTargets(sub *subschema) error {
	for _, ev := range sub.evaluators {
		var err error
		switch e := ev.(type) {
		case *refEval:
			if e.target == nil {
				return &CompileError{KeywordLocation: e.loc, Message: fmt.Sprintf("cannot resolve $ref %q", e.source)}
			}
			e.sub, err = e.build(e.target)
		case *dynamicRefEval:
			w.dynamic = append(w.dynamic, e)
			if e.target != nil {
				_, err = e.build(e.target)
			}
		case *recursiveRefEval:
			w.recursive = append(w.recursive, e)
			if e.target != nil {
				_, err = e.build(e.target)
			}
		}
		if err != nil {
			return err
		}
	}
	return nil
}

// buildDynamicTargets builds every schema the dynamic scope could select:
// each matching $dynamicAnchor for a $dynamicRef, and each resource root
// with "$recursiveAnchor": true for a $recursiveRef.
func (w *refWork) buildDynamicTargets(rm *resourceMap) error {
	for _, uri := range rm.order {
		res := rm.byURI[uri]
		if res == nil {
			continue
		}
		for _, e := range w.dynamic {
			if t, ok := res.dynamicAnchors[e.fragmentName]; ok && e.fragmentName != "" {
				if _, err := e.build(t); err != nil {
					return err
				}
			}
		}
		if !res.recursiveAnchor {
			continue
		}
		for _, e := range w.recursive {
			if _, err := e.build(res.root); err != nil {
				return err
			}
		}
	}
	return nil
}

// draftAt returns the draft in effect at node: its own $schema, else the
// draft of the resource the walk found it in, else the compile's draft.
func (b *evalBuilder) draftAt(node map[string]any) Draft {
	if s, ok := node["$schema"].(string); ok {
		if d := DraftFromMetaSchemaURL(s); d != DraftUnknown {
			return d
		}
	}
	if l, ok := b.resourceRoot(node); ok {
		if res := b.rm.byURI[l.uri]; res != nil && res.draft != DraftUnknown {
			return res.draft
		}
	}
	return b.draft
}

// applyID applies node's $id (keyword idKey) to sub and returns the base
// and resource URIs in effect inside it.
func (b *evalBuilder) applyID(sub *subschema, node map[string]any, idKey, baseURI, resourceURI string) (string, string) {
	rawID, ok := node[idKey]
	if !ok {
		return baseURI, resourceURI
	}
	uri := ""
	if l, walked := b.resourceRoot(node); walked {
		// The resource walk already resolved this $id; resolving it again
		// against a base that includes it would apply a relative $id twice.
		if l.ptr == "" && l.uri != "" {
			uri = l.uri
		}
	} else if s, ok := rawID.(string); ok && s != "" {
		if abs, err := resolveURI(baseURI, s); err == nil {
			uri, _ = splitFragment(abs)
		}
	}
	if uri == "" {
		return baseURI, resourceURI
	}
	sub.baseURI, sub.resourceURI = uri, uri
	sub.crossesResource = true
	sub.absURI, sub.absPtr = absoluteOrEmpty(uri), ""
	return uri, uri
}

// vocabularyOff reports whether keyword belongs to a vocabulary the root's
// custom meta-schema leaves out. That applies to the compiled document
// only, not to registered or fetched documents.
func (b *evalBuilder) vocabularyOff(keyword string, node map[string]any) bool {
	if len(b.vocabOff) == 0 || !b.vocabOff[keywordVocabulary(keyword)] {
		return false
	}
	return b.inCompiledDocument(node)
}

// inCompiledDocument reports whether node belongs to the document being
// compiled rather than to a registered or fetched one.
func (b *evalBuilder) inCompiledDocument(node map[string]any) bool {
	if l, ok := b.resourceRoot(node); ok {
		if res := b.rm.byURI[l.uri]; res != nil && res.external {
			return false
		}
	}
	return true
}
