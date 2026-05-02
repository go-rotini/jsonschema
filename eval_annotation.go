package jsonschema

// annotationEval is the evaluator for purely-annotation keywords. It records
// the keyword's value as an annotation and never produces a validation error.
type annotationEval struct {
	loc        string
	kw         string
	value      any
	stringOnly bool // true means: only annotate when instance is a string
}

func (e *annotationEval) keyword() string { return e.kw }

func (e *annotationEval) eval(ctx *runCtx, instance any) {
	if e.stringOnly {
		if _, ok := instance.(string); !ok {
			return
		}
	}
	ctx.addAnnotation(e.loc, e.kw, e.value)
}

//nolint:gochecknoinits // evaluator registry is built at package init by design.
func init() {
	// format: annotation-only in Phase 4 (assertion mode lands in Phase 6).
	// Per §12.14, format only applies to string instances.
	registerEvaluator("format", func(_ *evalBuilder, raw any, loc string) (evaluator, error) {
		s, ok := raw.(string)
		if !ok {
			return nil, &CompileError{KeywordLocation: loc, Message: "format must be a string"}
		}
		return &annotationEval{loc: loc, kw: "format", value: s, stringOnly: true}, nil
	})
	// Content vocabulary: annotation-only by default. assertion via
	// WithContentAssertion is Phase 6.
	for _, n := range []string{"contentEncoding", "contentMediaType"} {
		name := n
		registerEvaluator(name, func(_ *evalBuilder, raw any, loc string) (evaluator, error) {
			return &annotationEval{loc: loc, kw: name, value: raw, stringOnly: true}, nil
		})
	}
	registerEvaluator("contentSchema", func(b *evalBuilder, raw any, loc string) (evaluator, error) {
		// Annotation-only: build the subschema so its structure is checked
		// at compile time, but evaluate as no-op.
		if _, err := b.buildSubschema(raw, loc, b.currentBase, b.currentResource, false); err != nil {
			return nil, err
		}
		return &annotationEval{loc: loc, kw: "contentSchema", value: raw, stringOnly: true}, nil
	})
	// Meta-data vocabulary: pure annotations. Always emit.
	for _, n := range []string{"title", "description", "default", "deprecated",
		"readOnly", "writeOnly", "examples"} {
		name := n
		registerEvaluator(name, func(_ *evalBuilder, raw any, loc string) (evaluator, error) {
			return &annotationEval{loc: loc, kw: name, value: raw}, nil
		})
	}
}
