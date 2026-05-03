package jsonschema

type unevaluatedItemsEval struct {
	loc string
	sub *subschema
}

func (e *unevaluatedItemsEval) keyword() string { return "unevaluatedItems" }

func (e *unevaluatedItemsEval) eval(ctx *runCtx, instance any) {
	arr, ok := instance.([]any)
	if !ok {
		return
	}
	covered := make(map[int]bool)
	if v, ok := ctx.getAnnotation("prefixItems"); ok {
		if iv, ok := v.(evaluatedItems); ok {
			for i := 0; i <= int(iv); i++ {
				covered[i] = true
			}
		}
	}
	if v, ok := ctx.getAnnotation("items"); ok {
		switch iv := v.(type) {
		case evaluatedItemsAll:
			return
		case evaluatedItems:
			for i := 0; i <= int(iv); i++ {
				covered[i] = true
			}
		}
	}
	if v, ok := ctx.getAnnotation("additionalItems"); ok {
		if _, ok := v.(evaluatedItemsAll); ok {
			return
		}
	}
	if v, ok := ctx.getAnnotation("contains"); ok {
		if k, ok := v.(evaluatedKeys); ok {
			for kk := range k {
				if i, ok := atoiSafe(kk); ok {
					covered[i] = true
				}
			}
		}
	}
	if v, ok := ctx.getAnnotation("unevaluatedItems"); ok {
		if _, ok := v.(evaluatedItemsAll); ok {
			return
		}
	}
	visited := false
	for i := range arr {
		if covered[i] {
			continue
		}
		ctx.evaluateChild(e.sub, arr[i], itoaInt(i), "unevaluatedItems")
		visited = true
	}
	if visited {
		recordItemsAnno(ctx, "unevaluatedItems", e.loc, evaluatedItemsAll{})
	}
}

type unevaluatedPropertiesEval struct {
	loc string
	sub *subschema
}

func (e *unevaluatedPropertiesEval) keyword() string { return "unevaluatedProperties" }

func (e *unevaluatedPropertiesEval) eval(ctx *runCtx, instance any) {
	obj, ok := instance.(map[string]any)
	if !ok {
		return
	}
	covered := evaluatedKeys{}
	for _, key := range []string{"properties", "patternProperties", "additionalProperties", "unevaluatedProperties"} {
		if v, ok := ctx.getAnnotation(key); ok {
			if k, ok := v.(evaluatedKeys); ok {
				for kk := range k {
					covered[kk] = struct{}{}
				}
			}
		}
	}
	evaluated := evaluatedKeys{}
	for k, v := range obj {
		if _, c := covered[k]; c {
			continue
		}
		ctx.evaluateChild(e.sub, v, k, "unevaluatedProperties")
		evaluated[k] = struct{}{}
	}
	if len(evaluated) > 0 {
		mergeKeysAnnotation(ctx, "unevaluatedProperties", e.loc, evaluated)
	}
}

//nolint:gochecknoinits // evaluator registry is built at package init by design.
func init() {
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

// atoiSafe parses s as a non-negative int. Returns (n, true) on success.
func atoiSafe(s string) (int, bool) {
	if s == "" {
		return 0, false
	}
	n := 0
	for i := range len(s) {
		c := s[i]
		if c < '0' || c > '9' {
			return 0, false
		}
		n = n*10 + int(c-'0')
	}
	return n, true
}
