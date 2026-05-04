package jsonschema

import (
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
			// Annotation already emitted; emit a one-line diagnostic to
			// the configured warning sink (if any), deduplicated within
			// this Validate call.
			ctx.emitFormatWarning(e.format)
		case UnknownFormatError:
			ctx.addErrorWithCause(e.loc, "format", "format",
				"unknown format: "+e.format,
				&FormatError{Format: e.format, Value: s, Cause: ErrUnknownFormat})
		}
		return
	}
	if err := fn(s); err != nil {
		ctx.addErrorWithCause(e.loc, "format", "format",
			"value does not match format "+strconv.Quote(e.format)+": "+err.Error(),
			&FormatError{Format: e.format, Value: s, Cause: err})
	}
}

//nolint:gochecknoinits // evaluator registry is built at package init by design.
func init() {
	registerEvaluator("format", func(_ *evalBuilder, _ *buildFrame, raw any, loc string) (evaluator, error) {
		s, ok := raw.(string)
		if !ok {
			return nil, &CompileError{KeywordLocation: loc, Message: "format must be a string"}
		}
		return &formatEval{loc: loc, format: s}, nil
	})
	registerEvaluator("contentEncoding", func(_ *evalBuilder, _ *buildFrame, raw any, loc string) (evaluator, error) {
		s, ok := raw.(string)
		if !ok {
			return nil, &CompileError{KeywordLocation: loc, Message: "contentEncoding must be a string"}
		}
		return &contentEncodingEval{loc: loc, encoding: s}, nil
	})
	registerEvaluator("contentMediaType", func(_ *evalBuilder, _ *buildFrame, raw any, loc string) (evaluator, error) {
		s, ok := raw.(string)
		if !ok {
			return nil, &CompileError{KeywordLocation: loc, Message: "contentMediaType must be a string"}
		}
		return &contentMediaTypeEval{loc: loc, mediaType: s}, nil
	})
	registerEvaluator("contentSchema", func(b *evalBuilder, f *buildFrame, raw any, loc string) (evaluator, error) {
		sub, err := b.buildSubschemaFrame(f, raw, loc, f.base, f.resource)
		if err != nil {
			return nil, err
		}
		return &contentSchemaEval{loc: loc, sub: sub}, nil
	})
	for _, n := range []string{"title", "description", "default", "deprecated",
		"readOnly", "writeOnly", "examples"} {
		name := n
		registerEvaluator(name, func(_ *evalBuilder, _ *buildFrame, raw any, loc string) (evaluator, error) {
			return &annotationEval{loc: loc, kw: name, value: raw}, nil
		})
	}
}
