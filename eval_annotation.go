package jsonschema

import (
	"errors"
	"net/url"
	"strconv"
	"strings"
	"unicode/utf8"
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
	// assert is set when the meta-schema's vocabulary makes format an
	// assertion, whatever [WithFormatAssertion] says.
	assert bool
	// rfc1123 is set in Drafts 4 and 6, where "hostname" means RFC 1123
	// without the later IDNA label rules.
	rfc1123 bool
}

func (e *formatEval) keyword() string { return "format" }

func (e *formatEval) eval(ctx *runCtx, instance any) {
	s, ok := instance.(string)
	if !ok {
		// format only applies to strings; other instance types pass
		// (JSON Schema Validation 2020-12 §7.1).
		return
	}
	ctx.addAnnotation(e.loc, "format", e.format)
	if !ctx.opts.formatAssertion && !e.assert {
		return
	}
	fn, ok := lookupFormat(e.format, ctx.opts.customFormats)
	if _, custom := ctx.opts.customFormats[e.format]; e.rfc1123 && e.format == "hostname" && !custom {
		fn = validateHostnameRFC1123
	}
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
			"value does not match format "+strconv.Quote(e.format)+": "+formatDetail(err),
			&FormatError{Format: e.format, Value: s, Cause: err})
	}
}

func registerFormatAndContentKeywords() {
	registerEvaluator("format", func(b *evalBuilder, f *buildFrame, raw any, loc string) (evaluator, error) {
		s, ok := raw.(string)
		if !ok {
			return nil, &CompileError{KeywordLocation: loc, Message: "format must be a string"}
		}
		parent, _ := f.parent.(map[string]any)
		return &formatEval{loc: loc, format: s, assert: b.formatAsserts && b.inCompiledDocument(parent),
			rfc1123: f.draft == Draft4 || f.draft == Draft6}, nil
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

// maxFormatDetail is the most bytes of a format failure's detail kept in a
// ValidationError message.
const maxFormatDetail = 256

// formatDetail returns why a value failed a format, for a ValidationError
// message: the cause of a [*FormatError] (and of a [*url.Error] within it),
// so the message names neither the format nor the value twice, cut to
// maxFormatDetail bytes. The full value stays in the FormatError.
func formatDetail(err error) string {
	if fe, ok := errors.AsType[*FormatError](err); ok && fe.Cause != nil {
		err = fe.Cause
	}
	if ue, ok := errors.AsType[*url.Error](err); ok {
		err = ue.Err
	}
	d := strings.TrimPrefix(err.Error(), errFormatInvalid.Error()+": ")
	if len(d) <= maxFormatDetail {
		return d
	}
	cut := maxFormatDetail
	for cut > 0 && !utf8.RuneStart(d[cut]) {
		cut--
	}
	return d[:cut] + "…"
}
