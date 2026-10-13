package jsonschema

import (
	"errors"
	"fmt"
	"reflect"
)

var errNoBuilder = errors.New("no evaluator builder")

type refEval struct {
	loc       string
	source    string
	absolute  string
	targetURI string
	// target is the parsed schema node the reference resolves to.
	target any
	// builder builds (or finds in its cache) the target's subschema.
	builder *evalBuilder
	// sub is the built target, set by Compile before the schema is
	// returned and read without locking from then on.
	sub *subschema
	// err is why Compile could not resolve or build the target. It is
	// reported when an instance reaches the reference.
	err error
}

func (e *refEval) keyword() string { return "$ref" }

// enterRef counts one more reference hop at the current instance location
// and reports false, after recording the error, when the hops exceed the
// [WithMaxRefDepth] limit.
func enterRef(ctx *runCtx, kw, loc string) bool {
	ctx.refDepth++
	maxDepth := 100
	if ctx.schema != nil && ctx.schema.compileOpts != nil {
		maxDepth = ctx.schema.compileOpts.maxRefDepth
	}
	if ctx.refDepth > maxDepth {
		ctx.addErrorWithCause(loc, kw, "", ErrMaxRefDepth.Error(), ErrMaxRefDepth)
		return false
	}
	return true
}

func (e *refEval) eval(ctx *runCtx, instance any) {
	defer func() { ctx.refDepth-- }()
	if !enterRef(ctx, "$ref", e.loc) {
		return
	}
	if e.err != nil {
		ctx.addErrorWithCause(e.loc, "$ref", "", e.err.Error(), e.err)
		return
	}
	if e.target == nil {
		ctx.addError(e.loc, "$ref", "", fmt.Sprintf("cannot resolve %q", e.source))
		return
	}
	sub := e.subschemaFor(e.target)
	if sub == nil {
		ctx.addError(e.loc, "$ref", "", fmt.Sprintf("cannot build subschema for %q", e.source))
		return
	}
	ctx.evaluateRef(e.loc, sub, instance)
}

func (e *refEval) subschemaFor(target any) *subschema {
	if e.sub != nil {
		return e.sub
	}
	sub, err := e.build(target)
	if err != nil {
		return nil
	}
	return sub
}

// build returns the subschema for target. Compile builds every target in
// advance, so during validation this is a cache lookup.
func (e *refEval) build(target any) (*subschema, error) {
	if e.builder == nil {
		return nil, errNoBuilder
	}
	baseURI := e.builder.targetBase(target, e.targetURI)
	return e.builder.buildSubschema(target, e.absolute, baseURI, baseURI)
}

// dynamicScopeRefEval is the shared evaluation core for $dynamicRef and
// $recursiveRef. Both keywords differ only in the scope-walk predicate
// (lookupTarget) and in the static fallback target. The depth-limit, error
// shape, subschema-build, and dispatch are identical.
func dynamicScopeRefEval(
	ctx *runCtx,
	kw, loc, source string,
	staticTarget any,
	lookupTarget func() any,
	subschemaFor func(any) (*subschema, error),
	instance any,
) {
	defer func() { ctx.refDepth-- }()
	if !enterRef(ctx, kw, loc) {
		return
	}
	target := lookupTarget()
	if target == nil {
		target = staticTarget
	}
	if target == nil {
		ctx.addError(loc, kw, "", fmt.Sprintf("cannot resolve %q", source))
		return
	}
	sub, err := subschemaFor(target)
	if sub == nil {
		msg := fmt.Sprintf("cannot build subschema for %q", source)
		if err != nil {
			msg += ": " + err.Error()
		}
		ctx.addErrorWithCause(loc, kw, "", msg, err)
		return
	}
	ctx.evaluateRef(loc, sub, instance)
}

type dynamicRefEval struct {
	loc       string
	source    string
	absolute  string
	targetURI string
	target    any
	// fragmentName is set when source is a plain-name fragment ("#name").
	fragmentName string
	builder      *evalBuilder
	// built maps the identity of each target Compile built to its
	// subschema, and failed each target it could not build to the error.
	// Both are written only while compiling and read without locking from
	// then on.
	built  map[any]*subschema
	failed map[any]error
}

func (e *dynamicRefEval) keyword() string { return keyDynamicRef }

func (e *dynamicRefEval) eval(ctx *runCtx, instance any) {
	dynamicScopeRefEval(ctx, "$dynamicRef", e.loc, e.source, e.target,
		func() any { return e.findDynamic(ctx) },
		e.subschemaFor, instance)
}

func (e *dynamicRefEval) findDynamic(ctx *runCtx) any {
	if e.fragmentName == "" {
		return nil
	}
	if ctx.schema == nil || ctx.schema.resources == nil {
		return nil
	}
	for _, uri := range ctx.dynamicScope {
		res, ok := ctx.schema.resources.byURI[uri]
		if !ok {
			continue
		}
		if v, ok := res.dynamicAnchors[e.fragmentName]; ok {
			return v
		}
	}
	return nil
}

func (e *dynamicRefEval) subschemaFor(target any) (*subschema, error) {
	id := rawIdentity(target)
	if sub, ok := e.built[id]; ok {
		return sub, nil
	}
	if err, ok := e.failed[id]; ok {
		return nil, err
	}
	return e.build(target)
}

// prebuild builds target while compiling; a target that fails is recorded
// in failed and reported when validation reaches it.
func (e *dynamicRefEval) prebuild(target any) {
	if _, err := e.build(target); err != nil {
		e.failed = recordFailure(e.failed, target, err)
	}
}

func (e *dynamicRefEval) build(target any) (*subschema, error) {
	if e.builder == nil {
		return nil, errNoBuilder
	}
	sub, err := buildDynamicTarget(e.builder, e.built, "dyn:", target, e.targetURI)
	if e.built == nil && sub != nil && !e.builder.frozen {
		e.built = map[any]*subschema{rawIdentity(target): sub}
	}
	return sub, err
}

// buildDynamicTarget builds target under the cache key prefix plus its
// identity and, while the schema is still compiling, records the result
// in built (when the caller has one) for lock-free lookups during
// validation.
func buildDynamicTarget(b *evalBuilder, built map[any]*subschema, prefix string, target any, targetURI string) (*subschema, error) {
	id := rawIdentity(target)
	key := fmt.Sprintf("%s%v", prefix, id)
	base := b.targetBase(target, targetURI)
	sub, err := b.buildSubschema(target, key, base, base)
	if err == nil && built != nil && !b.frozen {
		built[id] = sub
	}
	return sub, err
}

// rawIdentity returns a value whose %v form identifies schema node v: the
// address of a map or slice, or the value itself for a boolean.
func rawIdentity(v any) any {
	switch t := v.(type) {
	case map[string]any:
		return reflect.ValueOf(t).Pointer()
	case []any:
		return reflect.ValueOf(t).Pointer()
	}
	return v
}

// recursiveRefEval implements Draft 2019-09's $recursiveRef. It walks the
// dynamic scope outermost-first for a resource carrying
// "$recursiveAnchor": true; without such a resource it falls back to the
// statically-captured target (i.e. behaves as a plain $ref).
type recursiveRefEval struct {
	loc       string
	source    string
	absolute  string
	targetURI string
	target    any
	builder   *evalBuilder
	// built and failed are as for [dynamicRefEval].
	built  map[any]*subschema
	failed map[any]error
}

func (e *recursiveRefEval) keyword() string { return "$recursiveRef" }

func (e *recursiveRefEval) eval(ctx *runCtx, instance any) {
	dynamicScopeRefEval(ctx, "$recursiveRef", e.loc, e.source, e.target,
		func() any { return e.findRecursive(ctx) },
		e.subschemaFor, instance)
}

// findRecursive walks ctx.dynamicScope outermost-first, returning the
// root of the first resource whose recursiveAnchor flag is set. Per the
// 2019-09 spec the recursion only fires when the static target resource
// itself carries $recursiveAnchor: true; otherwise nil signals "behave
// like static $ref" to the caller.
func (e *recursiveRefEval) findRecursive(ctx *runCtx) any {
	if ctx.schema == nil || ctx.schema.resources == nil {
		return nil
	}
	if e.targetURI != "" {
		tgtRes, ok := ctx.schema.resources.byURI[e.targetURI]
		if !ok || !tgtRes.recursiveAnchor {
			return nil
		}
	} else {
		return nil
	}
	for _, uri := range ctx.dynamicScope {
		res, ok := ctx.schema.resources.byURI[uri]
		if !ok {
			continue
		}
		if res.recursiveAnchor {
			return res.root
		}
	}
	return nil
}

func (e *recursiveRefEval) subschemaFor(target any) (*subschema, error) {
	id := rawIdentity(target)
	if sub, ok := e.built[id]; ok {
		return sub, nil
	}
	if err, ok := e.failed[id]; ok {
		return nil, err
	}
	return e.build(target)
}

// prebuild is as for [dynamicRefEval].
func (e *recursiveRefEval) prebuild(target any) {
	if _, err := e.build(target); err != nil {
		e.failed = recordFailure(e.failed, target, err)
	}
}

// recordFailure adds target's build error to failed, allocating it when
// nil, and returns the map.
func recordFailure(failed map[any]error, target any, err error) map[any]error {
	if failed == nil {
		failed = map[any]error{}
	}
	failed[rawIdentity(target)] = err
	return failed
}

func (e *recursiveRefEval) build(target any) (*subschema, error) {
	if e.builder == nil {
		return nil, errNoBuilder
	}
	sub, err := buildDynamicTarget(e.builder, e.built, "rec:", target, e.targetURI)
	if e.built == nil && sub != nil && !e.builder.frozen {
		e.built = map[any]*subschema{rawIdentity(target): sub}
	}
	return sub, err
}

// resolve resolves ref, written at a location whose base URI is base. Once
// compilation has finished the loader is not used, so validation never
// fetches documents or changes the resource map.
func (b *evalBuilder) resolve(base, ref string, draft Draft) (*resolvedRef, error) {
	if base == "" {
		base = b.schema.id
	}
	loader := b.loader
	if b.frozen {
		loader = nil
	}
	return resolveRef(b.rm, loader, base, ref, nil, draft)
}

func registerRefKeywords() {
	registerEvaluator("$ref", func(b *evalBuilder, f *buildFrame, raw any, loc string) (evaluator, error) {
		ref, ok := raw.(string)
		if !ok {
			return nil, &CompileError{KeywordLocation: loc, Message: "$ref must be a string"}
		}
		resolved, err := b.resolve(f.base, ref, f.draft)
		if err != nil {
			// A reference that cannot be resolved fails only the
			// instances that reach it.
			return &refEval{loc: loc, source: ref, builder: b,
				err: fmt.Errorf("cannot resolve %q: %w", ref, err)}, nil
		}
		return &refEval{
			loc:       loc,
			source:    ref,
			absolute:  resolved.AbsoluteURI,
			targetURI: resolved.TargetURI,
			target:    resolved.Target,
			builder:   b,
		}, nil
	})
	registerEvaluator("$dynamicRef", func(b *evalBuilder, f *buildFrame, raw any, loc string) (evaluator, error) {
		ref, ok := raw.(string)
		if !ok {
			return nil, &CompileError{KeywordLocation: loc, Message: "$dynamicRef must be a string"}
		}
		resolved, err := b.resolve(f.base, ref, f.draft)
		ev := &dynamicRefEval{loc: loc, source: ref, builder: b}
		if err == nil {
			ev.absolute = resolved.AbsoluteURI
			ev.targetURI = resolved.TargetURI
			ev.target = resolved.Target
		}
		// The reference is dynamic only when its fragment is a plain name
		// and the schema it first resolves to declares that name as its
		// $dynamicAnchor; otherwise it behaves like $ref.
		if _, frag := splitFragment(ref); len(frag) > 1 && frag[1] != '/' {
			name := frag[1:]
			if m, ok := ev.target.(map[string]any); ok && m["$dynamicAnchor"] == name {
				ev.fragmentName = name
			}
		}
		return ev, nil
	})
	registerEvaluator("$recursiveRef", func(b *evalBuilder, f *buildFrame, raw any, loc string) (evaluator, error) {
		ref, ok := raw.(string)
		if !ok {
			return nil, &CompileError{KeywordLocation: loc, Message: "$recursiveRef must be a string"}
		}
		resolved, err := b.resolve(f.base, ref, f.draft)
		ev := &recursiveRefEval{loc: loc, source: ref, builder: b}
		if err == nil {
			ev.absolute = resolved.AbsoluteURI
			ev.targetURI = resolved.TargetURI
			ev.target = resolved.Target
		}
		return ev, nil
	})
	// These keywords are resolved at compile time; the dispatcher needs
	// builder entries so they don't trip the unknown-key path. Returning
	// nil leaves no runtime evaluator (populateSubschema filters nil).
	for _, name := range []string{"$dynamicAnchor", "$anchor", "$defs", "$id", "id",
		"$schema", "$vocabulary", "$comment", "definitions",
		"$recursiveAnchor"} {
		registerEvaluator(name, func(_ *evalBuilder, _ *buildFrame, _ any, _ string) (evaluator, error) {
			return nil, nil //nolint:nilnil // intentional: compile-time-only keyword has no runtime evaluator.
		})
	}
}
