package jsonschema

import (
	"fmt"
)

type refEval struct {
	loc       string
	source    string
	absolute  string
	targetURI string
	// target is the parsed schema node; nil for lazy edges (cycles).
	target any
	// builderRef is the builder used to construct target subschemas at
	// validation time when the ref is lazy.
	builder *evalBuilder
}

func (e *refEval) keyword() string { return "$ref" }

func (e *refEval) eval(ctx *runCtx, instance any) {
	ctx.refDepth++
	defer func() { ctx.refDepth-- }()
	maxDepth := 100
	if ctx.schema != nil && ctx.schema.compileOpts != nil {
		maxDepth = ctx.schema.compileOpts.maxRefDepth
	}
	if ctx.refDepth > maxDepth {
		ctx.addError(e.loc, "$ref", "", ErrMaxRefDepth.Error())
		return
	}
	target := e.target
	if target == nil {
		if ctx.schema == nil || ctx.schema.resources == nil {
			ctx.addError(e.loc, "$ref", "", fmt.Sprintf("cannot resolve %q", e.source))
			return
		}
		resolved, err := resolveRef(ctx.schema.resources, e.builderLoader(), ctx.schema.id, e.source, nil, ctx.schema.draft)
		if err != nil {
			ctx.addError(e.loc, "$ref", "", err.Error())
			return
		}
		target = resolved.Target
	}
	if target == nil {
		ctx.addError(e.loc, "$ref", "", fmt.Sprintf("cannot resolve %q", e.source))
		return
	}
	sub := e.subschemaFor(target)
	if sub == nil {
		ctx.addError(e.loc, "$ref", "", fmt.Sprintf("cannot build subschema for %q", e.source))
		return
	}
	ctx.evaluate(sub, instance)
}

func (e *refEval) builderLoader() Loader {
	if e.builder == nil {
		return nil
	}
	return e.builder.loader
}

func (e *refEval) subschemaFor(target any) *subschema {
	if e.builder == nil {
		return nil
	}
	// buildSubschema acts as its own cache and serializes concurrent
	// builders so foreign goroutines never see a partial subschema.
	key := e.absolute
	baseURI := e.targetURI
	sub, err := e.builder.buildSubschema(target, key, baseURI, baseURI)
	if err != nil {
		return nil
	}
	return sub
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
	subschemaFor func(any) *subschema,
	instance any,
) {
	ctx.refDepth++
	defer func() { ctx.refDepth-- }()
	maxDepth := 100
	if ctx.schema != nil && ctx.schema.compileOpts != nil {
		maxDepth = ctx.schema.compileOpts.maxRefDepth
	}
	if ctx.refDepth > maxDepth {
		ctx.addError(loc, kw, "", ErrMaxRefDepth.Error())
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
	sub := subschemaFor(target)
	if sub == nil {
		ctx.addError(loc, kw, "", fmt.Sprintf("cannot build subschema for %q", source))
		return
	}
	ctx.evaluate(sub, instance)
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
}

func (e *dynamicRefEval) keyword() string { return "$dynamicRef" }

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

func (e *dynamicRefEval) subschemaFor(target any) *subschema {
	if e.builder == nil {
		return nil
	}
	key := fmt.Sprintf("dyn:%p", rawIdentity(target))
	sub, err := e.builder.buildSubschema(target, key, e.targetURI, e.targetURI)
	if err != nil {
		return nil
	}
	return sub
}

// rawIdentity returns a value suitable for %p formatting: a pointer-shaped
// identity for maps/slices, a stable address for booleans, and the value
// itself otherwise.
func rawIdentity(v any) any {
	switch t := v.(type) {
	case map[string]any:
		return &t
	case []any:
		return &t
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

func (e *recursiveRefEval) subschemaFor(target any) *subschema {
	if e.builder == nil {
		return nil
	}
	key := fmt.Sprintf("rec:%p", rawIdentity(target))
	sub, err := e.builder.buildSubschema(target, key, e.targetURI, e.targetURI)
	if err != nil {
		return nil
	}
	return sub
}

//nolint:gochecknoinits // evaluator registry is built at package init by design.
func init() {
	registerEvaluator("$ref", func(b *evalBuilder, f *buildFrame, raw any, loc string) (evaluator, error) {
		ref, ok := raw.(string)
		if !ok {
			return nil, &CompileError{KeywordLocation: loc, Message: "$ref must be a string"}
		}
		baseURI := f.base
		if baseURI == "" {
			baseURI = b.schema.id
		}
		resolved, err := resolveRef(b.rm, b.loader, baseURI, ref, nil, f.draft)
		if err != nil {
			// Defer to a lazy edge: validation re-attempts resolution and
			// surfaces an error if the target is still missing.
			return &refEval{loc: loc, source: ref, absolute: ref, builder: b}, nil //nolint:nilerr // intentional fallback to runtime resolution
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
		baseURI := f.base
		if baseURI == "" {
			baseURI = b.schema.id
		}
		resolved, err := resolveRef(b.rm, b.loader, baseURI, ref, nil, f.draft)
		var fragName string
		if abs, frag := splitFragment(ref); abs == "" && len(frag) > 1 && frag[0] == '#' && frag[1] != '/' {
			fragName = frag[1:]
		}
		ev := &dynamicRefEval{loc: loc, source: ref, fragmentName: fragName, builder: b}
		if err == nil {
			ev.absolute = resolved.AbsoluteURI
			ev.targetURI = resolved.TargetURI
			ev.target = resolved.Target
		}
		return ev, nil
	})
	registerEvaluator("$recursiveRef", func(b *evalBuilder, f *buildFrame, raw any, loc string) (evaluator, error) {
		ref, ok := raw.(string)
		if !ok {
			return nil, &CompileError{KeywordLocation: loc, Message: "$recursiveRef must be a string"}
		}
		baseURI := f.base
		if baseURI == "" {
			baseURI = b.schema.id
		}
		resolved, err := resolveRef(b.rm, b.loader, baseURI, ref, nil, f.draft)
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
