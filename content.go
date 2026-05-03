package jsonschema

import (
	"encoding/base32"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/quotedprintable"
	"strings"
)

// contentEncodingEval is the evaluator for the contentEncoding keyword. In
// annotation mode it just records the keyword. In assertion mode it attempts
// to decode the string and (on success) stashes the decoded bytes on
// runCtx.contentDecoded so a sibling contentMediaType / contentSchema can
// pick them up.
type contentEncodingEval struct {
	loc      string
	encoding string
}

func (e *contentEncodingEval) keyword() string { return "contentEncoding" }

func (e *contentEncodingEval) eval(ctx *runCtx, instance any) {
	s, ok := instance.(string)
	if !ok {
		return
	}
	ctx.addAnnotation(e.loc, "contentEncoding", e.encoding)
	if !ctx.opts.contentAssertion {
		return
	}
	decoded, err := decodeContent(e.encoding, s)
	if err != nil {
		// Unknown encoding is a silent pass per spec.
		if errors.Is(err, errUnknownEncoding) {
			return
		}
		ctx.addError(e.loc, "contentEncoding", "contentEncoding",
			"value does not decode as "+e.encoding+": "+err.Error())
		return
	}
	ctx.stashContent(decoded)
}

// contentMediaTypeEval annotates the keyword and (in assertion mode + when
// the type is a JSON variant) parses the decoded payload.
type contentMediaTypeEval struct {
	loc       string
	mediaType string
}

func (e *contentMediaTypeEval) keyword() string { return "contentMediaType" }

func (e *contentMediaTypeEval) eval(ctx *runCtx, instance any) {
	s, ok := instance.(string)
	if !ok {
		return
	}
	ctx.addAnnotation(e.loc, "contentMediaType", e.mediaType)
	if !ctx.opts.contentAssertion {
		return
	}
	if !isJSONMediaType(e.mediaType) {
		return
	}
	// Bytes to parse: prefer the decoded form a sibling contentEncoding may
	// have stashed; fall back to the raw string.
	bytes := ctx.takeContent()
	if bytes == nil {
		bytes = []byte(s)
	}
	var v any
	if err := json.Unmarshal(bytes, &v); err != nil {
		ctx.addError(e.loc, "contentMediaType", "contentMediaType",
			"value is not valid "+e.mediaType+": "+err.Error())
		return
	}
	ctx.stashContentParsed(v)
}

// contentSchemaEval validates the parsed JSON content (when assertion is on
// and a sibling contentMediaType has identified JSON) against the subschema.
type contentSchemaEval struct {
	loc string
	sub *subschema
}

func (e *contentSchemaEval) keyword() string { return "contentSchema" }

func (e *contentSchemaEval) eval(ctx *runCtx, instance any) {
	s, ok := instance.(string)
	if !ok {
		return
	}
	ctx.addAnnotation(e.loc, "contentSchema", e.sub.raw)
	if !ctx.opts.contentAssertion {
		return
	}
	parsed, ok := ctx.takeContentParsed()
	if !ok {
		// Without a sibling contentMediaType resolving to JSON we have
		// nothing decoded to validate against — silent pass.
		_ = s
		return
	}
	branch, annos := ctx.evaluateBranch(e.sub, parsed)
	if len(branch) > 0 {
		ctx.addCausesError(e.loc, "contentSchema",
			"decoded content does not validate against contentSchema", branch)
		return
	}
	ctx.addBranchAnnotations(annos)
}

// errUnknownEncoding sentinels an unknown contentEncoding name; the
// evaluator treats it as a silent pass per spec.
var errUnknownEncoding = errors.New("unknown content encoding")

// decodeContent decodes s using encoding. Recognized encodings are base64,
// base32, base16/hex, and quoted-printable. 7bit / 8bit / binary pass through
// as raw bytes.
func decodeContent(encoding, s string) ([]byte, error) {
	switch strings.ToLower(encoding) {
	case "base64":
		b, err := base64.StdEncoding.DecodeString(s)
		if err != nil {
			return nil, fmt.Errorf("base64: %w", err)
		}
		return b, nil
	case "base32":
		b, err := base32.StdEncoding.DecodeString(s)
		if err != nil {
			return nil, fmt.Errorf("base32: %w", err)
		}
		return b, nil
	case "base16", "hex":
		b, err := hex.DecodeString(s)
		if err != nil {
			return nil, fmt.Errorf("hex: %w", err)
		}
		return b, nil
	case "quoted-printable":
		r := quotedprintable.NewReader(strings.NewReader(s))
		b, err := io.ReadAll(r)
		if err != nil {
			return nil, fmt.Errorf("quoted-printable: %w", err)
		}
		return b, nil
	case "7bit", "8bit", "binary", "":
		return []byte(s), nil
	default:
		return nil, errUnknownEncoding
	}
}

// isJSONMediaType reports whether mt is JSON-flavored: application/json or
// any */*+json subtype (with optional ;parameters).
func isJSONMediaType(mt string) bool {
	main, _, err := mime.ParseMediaType(mt)
	if err != nil {
		// Fall back to a string compare; some test inputs lack a strict mime
		// shape.
		main = strings.TrimSpace(strings.ToLower(strings.SplitN(mt, ";", 2)[0]))
	}
	main = strings.ToLower(main)
	if main == "application/json" || main == "text/json" {
		return true
	}
	return strings.HasSuffix(main, "+json")
}

// stashContent records the decoded bytes for the current instance location so
// a sibling contentMediaType evaluator can pick them up.
func (ctx *runCtx) stashContent(b []byte) {
	if ctx.contentDecoded == nil {
		ctx.contentDecoded = map[string][]byte{}
	}
	ctx.contentDecoded[ctx.instanceLocation()] = b
}

// takeContent removes and returns any decoded bytes stashed for the current
// instance location.
func (ctx *runCtx) takeContent() []byte {
	loc := ctx.instanceLocation()
	if ctx.contentDecoded == nil {
		return nil
	}
	b, ok := ctx.contentDecoded[loc]
	if !ok {
		return nil
	}
	delete(ctx.contentDecoded, loc)
	return b
}

// stashContentParsed records the parsed JSON value for the current instance
// location so contentSchema can pick it up.
func (ctx *runCtx) stashContentParsed(v any) {
	if ctx.contentParsed == nil {
		ctx.contentParsed = map[string]any{}
	}
	ctx.contentParsed[ctx.instanceLocation()] = v
}

// takeContentParsed removes and returns any parsed JSON value stashed for the
// current instance location.
func (ctx *runCtx) takeContentParsed() (any, bool) {
	loc := ctx.instanceLocation()
	if ctx.contentParsed == nil {
		return nil, false
	}
	v, ok := ctx.contentParsed[loc]
	if !ok {
		return nil, false
	}
	delete(ctx.contentParsed, loc)
	return v, true
}
