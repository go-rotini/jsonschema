package jsonschema

import (
	"log"
	"strconv"
)

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

// formatEval evaluates the format keyword. It always emits an annotation;
// when WithFormatAssertion is set and a built-in (or custom) validator exists
// for the format name, a failing validation surfaces a ValidationError.
type formatEval struct {
	loc    string
	format string
}

func (e *formatEval) keyword() string { return "format" }

func (e *formatEval) eval(ctx *runCtx, instance any) {
	s, ok := instance.(string)
	if !ok {
		// format only applies to strings; silent pass otherwise (per §2.5).
		return
	}
	ctx.addAnnotation(e.loc, "format", e.format)
	if !ctx.opts.formatAssertion {
		return
	}
	fn, ok := lookupFormat(e.format, ctx.opts.customFormats)
	if !ok {
		switch ctx.opts.unknownFormatPolicy {
		case UnknownFormatWarn:
			if ctx.formatWarned == nil {
				ctx.formatWarned = map[string]struct{}{}
			}
			if _, seen := ctx.formatWarned[e.format]; !seen {
				ctx.formatWarned[e.format] = struct{}{}
				log.Printf("jsonschema: unknown format %q (assertion mode)", e.format)
			}
		case UnknownFormatError:
			ctx.addError(e.loc, "format", "format",
				"unknown format: "+e.format)
		}
		return
	}
	if err := fn(s); err != nil {
		ctx.addError(e.loc, "format", "format",
			"value does not match format "+strconv.Quote(e.format)+": "+err.Error())
	}
}

//nolint:gochecknoinits // evaluator registry is built at package init by design.
func init() {
	// format: emits an annotation always, asserts under WithFormatAssertion.
	registerEvaluator("format", func(_ *evalBuilder, raw any, loc string) (evaluator, error) {
		s, ok := raw.(string)
		if !ok {
			return nil, &CompileError{KeywordLocation: loc, Message: "format must be a string"}
		}
		return &formatEval{loc: loc, format: s}, nil
	})
	// contentEncoding: assertion-aware evaluator.
	registerEvaluator("contentEncoding", func(_ *evalBuilder, raw any, loc string) (evaluator, error) {
		s, ok := raw.(string)
		if !ok {
			return nil, &CompileError{KeywordLocation: loc, Message: "contentEncoding must be a string"}
		}
		return &contentEncodingEval{loc: loc, encoding: s}, nil
	})
	// contentMediaType: assertion-aware evaluator.
	registerEvaluator("contentMediaType", func(_ *evalBuilder, raw any, loc string) (evaluator, error) {
		s, ok := raw.(string)
		if !ok {
			return nil, &CompileError{KeywordLocation: loc, Message: "contentMediaType must be a string"}
		}
		return &contentMediaTypeEval{loc: loc, mediaType: s}, nil
	})
	// contentSchema: build the subschema (also surfaces compile-time errors)
	// and assert against the decoded JSON when assertion mode is on.
	registerEvaluator("contentSchema", func(b *evalBuilder, raw any, loc string) (evaluator, error) {
		sub, err := b.buildSubschema(raw, loc, b.currentBase, b.currentResource, false)
		if err != nil {
			return nil, err
		}
		return &contentSchemaEval{loc: loc, sub: sub}, nil
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
