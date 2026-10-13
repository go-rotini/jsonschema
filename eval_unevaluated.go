package jsonschema

type unevaluatedItemsEval struct {
	loc string
	sub *subschema
}

func (e *unevaluatedItemsEval) keyword() string { return "unevaluatedItems" }

// eval applies sub to the items that neither this subschema's keywords nor
// its passing in-place subschemas evaluated.
func (e *unevaluatedItemsEval) eval(ctx *runCtx, instance any) {
	arr, ok := instance.([]any)
	if !ok {
		return
	}
	var s annoScope
	if ctx.scope != nil {
		s = *ctx.scope
	}
	if s.allItems {
		return
	}
	visited := false
	for i := s.prefix; i < len(arr); i++ {
		if _, c := s.contained[i]; c {
			continue
		}
		ctx.evaluateChild(e.sub, arr[i], itoaInt(i), "unevaluatedItems")
		visited = true
	}
	if visited {
		recordAllItems(ctx, "unevaluatedItems", e.loc)
	}
}

type unevaluatedPropertiesEval struct {
	loc string
	sub *subschema
}

func (e *unevaluatedPropertiesEval) keyword() string { return "unevaluatedProperties" }

// eval applies sub to the properties that neither this subschema's
// keywords nor its passing in-place subschemas evaluated.
func (e *unevaluatedPropertiesEval) eval(ctx *runCtx, instance any) {
	obj, ok := instance.(map[string]any)
	if !ok {
		return
	}
	var covered map[string]struct{}
	if ctx.scope != nil {
		covered = ctx.scope.props
	}
	var evaluated evaluatedKeys
	for _, k := range sortedKeys(obj) {
		if _, c := covered[k]; c {
			continue
		}
		ctx.evaluateChild(e.sub, obj[k], k, "unevaluatedProperties")
		if evaluated == nil {
			evaluated = evaluatedKeys{}
		}
		evaluated[k] = struct{}{}
	}
	recordKeys(ctx, "unevaluatedProperties", e.loc, evaluated)
}

func registerUnevaluatedKeywords() {
	registerEvaluator("unevaluatedItems", func(b *evalBuilder, f *buildFrame, raw any, loc string) (evaluator, error) {
		sub, err := b.buildSubschemaFrame(f, raw, loc, f.base, f.resource)
		if err != nil {
			return nil, err
		}
		return &unevaluatedItemsEval{loc: loc, sub: sub}, nil
	})
	registerEvaluator("unevaluatedProperties", func(b *evalBuilder, f *buildFrame, raw any, loc string) (evaluator, error) {
		sub, err := b.buildSubschemaFrame(f, raw, loc, f.base, f.resource)
		if err != nil {
			return nil, err
		}
		return &unevaluatedPropertiesEval{loc: loc, sub: sub}, nil
	})
}
