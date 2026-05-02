package jsonschema

import (
	"fmt"
)

// =====================================================================
// $ref
// =====================================================================.

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
		// Lazy edge: resolve at run time.
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
	// Build (or look up) a subschema for target.
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
	// Try cache first.
	if e.builder != nil {
		key := e.absolute
		if cached, ok := e.builder.cache[key]; ok {
			return cached
		}
		// Build at runtime; cache on the builder.
		baseURI := e.targetURI
		sub, err := e.builder.buildSubschema(target, key, baseURI, baseURI, true)
		if err != nil {
			return nil
		}
		return sub
	}
	return nil
}

// =====================================================================
// $dynamicRef
// =====================================================================.

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
	ctx.refDepth++
	defer func() { ctx.refDepth-- }()
	maxDepth := 100
	if ctx.schema != nil && ctx.schema.compileOpts != nil {
		maxDepth = ctx.schema.compileOpts.maxRefDepth
	}
	if ctx.refDepth > maxDepth {
		ctx.addError(e.loc, "$dynamicRef", "", ErrMaxRefDepth.Error())
		return
	}
	// Walk dynamic scope from outermost to innermost looking for a matching
	// $dynamicAnchor.
	target := e.findDynamic(ctx)
	if target == nil {
		// Fall back to static $ref-style resolution.
		target = e.target
	}
	if target == nil {
		ctx.addError(e.loc, "$dynamicRef", "", fmt.Sprintf("cannot resolve %q", e.source))
		return
	}
	sub := e.subschemaFor(target)
	if sub == nil {
		ctx.addError(e.loc, "$dynamicRef", "", fmt.Sprintf("cannot build subschema for %q", e.source))
		return
	}
	ctx.evaluate(sub, instance)
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
	if cached, ok := e.builder.cache[key]; ok {
		return cached
	}
	sub, err := e.builder.buildSubschema(target, key, e.targetURI, e.targetURI, true)
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

//nolint:gochecknoinits // evaluator registry is built at package init by design.
func init() {
	registerEvaluator("$ref", func(b *evalBuilder, raw any, loc string) (evaluator, error) {
		ref, ok := raw.(string)
		if !ok {
			return nil, &CompileError{KeywordLocation: loc, Message: "$ref must be a string"}
		}
		// Resolve at compile time when possible.
		baseURI := b.currentBase
		if baseURI == "" {
			baseURI = b.schema.id
		}
		resolved, err := resolveRef(b.rm, b.loader, baseURI, ref, nil, b.draft)
		if err != nil {
			// Make this a lazy edge so validation can attempt at runtime.
			// The runtime evaluator re-attempts resolution and surfaces a
			// validation error if it still cannot find the target — so
			// swallowing the compile-time error here is intentional.
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
	registerEvaluator("$dynamicRef", func(b *evalBuilder, raw any, loc string) (evaluator, error) {
		ref, ok := raw.(string)
		if !ok {
			return nil, &CompileError{KeywordLocation: loc, Message: "$dynamicRef must be a string"}
		}
		baseURI := b.currentBase
		if baseURI == "" {
			baseURI = b.schema.id
		}
		resolved, err := resolveRef(b.rm, b.loader, baseURI, ref, nil, b.draft)
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
	// $dynamicAnchor / $anchor / $defs / $id / $schema / $vocabulary /
	// $comment / definitions are no-ops at evaluation time (handled by
	// resource resolution at compile time).
	for _, name := range []string{"$dynamicAnchor", "$anchor", "$defs", "$id", "id",
		"$schema", "$vocabulary", "$comment", "definitions",
		"$recursiveRef", "$recursiveAnchor"} {
		n := name
		registerEvaluator(n, func(_ *evalBuilder, _ any, _ string) (evaluator, error) {
			return &noopEval{name: n}, nil
		})
	}
}
