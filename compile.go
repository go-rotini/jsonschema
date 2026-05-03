package jsonschema

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
)

// errTrailingContent is the static sentinel used by [decodeSchemaBytes] and
// [decodeInstanceBytes] when the document is followed by extra
// non-whitespace bytes (a "concatenated documents" smuggle attempt).
var errTrailingContent = errors.New("trailing content after document")

// traceLoaderFetch emits one line per successful loader fetch to w. A nil
// writer is a no-op so the option can be safely left unset.
func traceLoaderFetch(w io.Writer, uri string) {
	if w == nil {
		return
	}
	_, _ = fmt.Fprintf(w, "jsonschema: loader fetched %s\n", uri)
}

// keywordBinding ties a recognized keyword at a specific subschema location
// to its raw value and (for refs) its pre-resolved target. The validator
// graph is built from these bindings; bindings persist on the compiled
// [*Schema] so introspection helpers can enumerate them.
type keywordBinding struct {
	// Name is the keyword identifier (e.g. "minLength", "$ref").
	Name string
	// Location is the JSON Pointer of the keyword in the source schema.
	Location string
	// RawValue is the parsed value of the keyword (json.Unmarshal'd).
	RawValue any
	// Resolved is non-nil for $ref / $dynamicRef bindings: it carries the
	// pre-resolved target the validator walks when crossing the edge.
	Resolved *resolvedRef
}

// Compile parses a JSON Schema document and returns a [*Schema] ready for
// validation.
//
// The schema's effective draft is determined by the $schema keyword if
// present; otherwise the package falls back to the value passed via
// [WithDefaultDraft], then [DraftDefault].
func Compile(schemaJSON []byte, opts ...CompileOption) (*Schema, error) {
	c := NewCompiler(opts...)
	return c.Compile(schemaJSON)
}

// MustCompile is the panic-on-error variant of [Compile]. Intended for
// package-init use of static, well-known schemas.
func MustCompile(schemaJSON []byte, opts ...CompileOption) *Schema {
	s, err := Compile(schemaJSON, opts...)
	if err != nil {
		panic(err)
	}
	return s
}

// CompileValue compiles a schema already represented as a Go value (the
// result of json.Unmarshal into any, or a jsonc/yaml/toml decode). Useful
// when the schema is constructed in code or arrives from a non-JSON source.
func CompileValue(schemaValue any, opts ...CompileOption) (*Schema, error) {
	c := NewCompiler(opts...)
	return c.CompileValue(schemaValue)
}

// MustCompileValue is the panic-on-error variant of [CompileValue].
func MustCompileValue(schemaValue any, opts ...CompileOption) *Schema {
	s, err := CompileValue(schemaValue, opts...)
	if err != nil {
		panic(err)
	}
	return s
}

// CompileURL fetches the schema at uri using the configured Loader (or the
// default chain) and compiles it.
func CompileURL(uri string, opts ...CompileOption) (*Schema, error) {
	c := NewCompiler(opts...)
	return c.CompileURL(uri)
}

// MustCompileURL is the panic-on-error variant of [CompileURL].
func MustCompileURL(uri string, opts ...CompileOption) *Schema {
	s, err := CompileURL(uri, opts...)
	if err != nil {
		panic(err)
	}
	return s
}

// Validate compiles schemaJSON and validates instanceJSON against it in one
// call. This is a convenience for one-shot use; callers performing multiple
// validations against the same schema should call [Compile] once and use
// [*Schema.Validate] to amortize compilation cost.
func Validate(schemaJSON, instanceJSON []byte, opts ...Option) (*Result, error) {
	s, err := Compile(schemaJSON)
	if err != nil {
		return nil, err
	}
	return s.Validate(instanceJSON, opts...)
}

// Compiler holds compile-time configuration and a cache of compiled remote
// schemas. Reuse a Compiler when compiling many schemas that share remote
// references — the cache amortizes loader I/O.
//
// A Compiler is safe for concurrent use after construction. The cache is
// keyed by absolute URI; concurrent [*Compiler.CompileURL] calls for the
// same URI share a single fetch+compile pipeline via an inline single-flight.
type Compiler struct {
	opts      *compileOptions
	cache     sync.Map // map[string]*Schema
	resources sync.Map // map[string][]byte — pre-registered AddResource entries
	flight    sync.Map // map[string]*compileInflight — in-flight CompileURL calls
}

// compileInflight is the single-flight record used by [*Compiler.CompileURL]
// so two goroutines requesting the same URI share one network round-trip
// plus one compile pass.
type compileInflight struct {
	wg     sync.WaitGroup
	schema *Schema
	err    error
}

// NewCompiler returns a fresh [*Compiler] with the supplied options applied.
func NewCompiler(opts ...CompileOption) *Compiler {
	co := defaultCompileOptions()
	for _, o := range opts {
		o(co)
	}
	if co.loader == nil {
		co.loader = DefaultLoader()
	}
	if co.loaderTrace != nil {
		co.loader = &tracingLoader{inner: co.loader, w: co.loaderTrace}
	}
	return &Compiler{opts: co}
}

// tracingLoader wraps a [Loader] and writes a one-line trace per successful
// fetch to its [io.Writer]. Failed fetches do not emit a line; callers can
// observe failures via the returned error.
type tracingLoader struct {
	inner Loader
	w     io.Writer
}

// Load implements [Loader].
func (l *tracingLoader) Load(uri string) ([]byte, error) {
	data, err := l.inner.Load(uri)
	if err != nil {
		return nil, fmt.Errorf("trace loader: %w", err)
	}
	traceLoaderFetch(l.w, uri)
	return data, nil
}

// Compile parses and compiles schemaJSON. The result is cached by $id so
// subsequent calls referencing the same document via $ref short-circuit.
func (c *Compiler) Compile(schemaJSON []byte) (*Schema, error) {
	if c == nil {
		return nil, ErrSchemaNotCompiled
	}
	value, err := decodeSchemaBytes(schemaJSON)
	if err != nil {
		return nil, &CompileError{Message: "decode schema", Cause: err}
	}
	return c.compile(value, schemaJSON, c.opts.baseURI)
}

// MustCompile is the panic-on-error variant of [*Compiler.Compile].
func (c *Compiler) MustCompile(schemaJSON []byte) *Schema {
	s, err := c.Compile(schemaJSON)
	if err != nil {
		panic(err)
	}
	return s
}

// CompileValue compiles an already-decoded Go value (an `any` produced by
// json.Unmarshal — typically a map[string]any tree).
func (c *Compiler) CompileValue(v any) (*Schema, error) {
	if c == nil {
		return nil, ErrSchemaNotCompiled
	}
	return c.compile(v, nil, c.opts.baseURI)
}

// MustCompileValue is the panic-on-error variant of [*Compiler.CompileValue].
func (c *Compiler) MustCompileValue(v any) *Schema {
	s, err := c.CompileValue(v)
	if err != nil {
		panic(err)
	}
	return s
}

// CompileURL fetches uri via the compiler's loader and compiles the
// resulting bytes. Concurrent calls for the same URI share a single
// fetch+compile pipeline (single-flight); the result is cached so subsequent
// calls return immediately.
func (c *Compiler) CompileURL(uri string) (*Schema, error) {
	if c == nil {
		return nil, ErrSchemaNotCompiled
	}
	if cached, ok := c.cache.Load(uri); ok {
		if s, ok := cached.(*Schema); ok {
			return s, nil
		}
	}

	// Single-flight: concurrent calls for the same URI share one fetch+compile.
	flight := &compileInflight{}
	flight.wg.Add(1)
	actual, loaded := c.flight.LoadOrStore(uri, flight)
	if loaded {
		other, ok := actual.(*compileInflight)
		if !ok {
			return nil, &CompileError{KeywordLocation: uri, Message: "compile flight: invalid state"}
		}
		other.wg.Wait()
		return other.schema, other.err
	}
	defer func() {
		c.flight.Delete(uri)
		flight.wg.Done()
	}()

	// Re-check after winning the flight slot: another goroutine may have
	// populated the cache between the first check and LoadOrStore.
	if cached, ok := c.cache.Load(uri); ok {
		if s, ok := cached.(*Schema); ok {
			flight.schema = s
			return s, nil
		}
	}

	loader := c.opts.loader
	if loader == nil {
		loader = DefaultLoader()
	}
	data, err := loader.Load(uri)
	if err != nil {
		flight.err = &CompileError{KeywordLocation: uri, Message: "load", Cause: err}
		return nil, flight.err
	}
	value, err := decodeSchemaBytes(data)
	if err != nil {
		flight.err = &CompileError{KeywordLocation: uri, Message: "decode schema", Cause: err}
		return nil, flight.err
	}
	s, err := c.compile(value, data, uri)
	if err != nil {
		flight.err = err
		return nil, err
	}
	c.cache.Store(uri, s)
	flight.schema = s
	return s, nil
}

// MustCompileURL is the panic-on-error variant of [*Compiler.CompileURL].
func (c *Compiler) MustCompileURL(uri string) *Schema {
	s, err := c.CompileURL(uri)
	if err != nil {
		panic(err)
	}
	return s
}

// AddResource registers a schema document under uri so subsequent
// compilations resolving that URI can find it without invoking the Loader.
// The bytes are validated as JSON; the document is parsed lazily when first
// referenced.
func (c *Compiler) AddResource(uri string, schemaJSON []byte) error {
	if c == nil {
		return ErrSchemaNotCompiled
	}
	if uri == "" {
		return &CompileError{Message: "AddResource: empty URI"}
	}
	if _, err := decodeSchemaBytes(schemaJSON); err != nil {
		return &CompileError{KeywordLocation: uri, Message: "AddResource: invalid JSON", Cause: err}
	}
	stored := make([]byte, len(schemaJSON))
	copy(stored, schemaJSON)
	c.resources.Store(uri, stored)
	return nil
}

// compile is the inner entry point shared by every public Compile* call.
// rawSource may be nil; in that case the source bytes are synthesized via
// json.Marshal of value.
func (c *Compiler) compile(value any, rawSource []byte, baseURI string) (*Schema, error) {
	draft := c.opts.defaultDraft
	if draft == DraftUnknown {
		draft = DraftDefault
	}
	idKey := draft.IDKeyword()
	metaSchemaURI := ""
	if obj, ok := value.(map[string]any); ok {
		if v, ok := obj["$schema"]; ok {
			if s, ok := v.(string); ok {
				if d := DraftFromMetaSchemaURL(s); d != DraftUnknown {
					draft = d
					idKey = draft.IDKeyword()
				}
				metaSchemaURI = s
			}
		}
	}

	rootID := ""
	if obj, ok := value.(map[string]any); ok {
		if v, ok := obj[idKey]; ok {
			if s, ok := v.(string); ok {
				rootID = s
			}
		}
	}
	rootURI := baseURI
	if rootID != "" {
		abs, err := resolveURI(baseURI, rootID)
		if err != nil {
			return nil, &CompileError{KeywordLocation: idKey, Message: "invalid " + idKey, Cause: err}
		}
		// Drop fragment so the resource URI identifies the document only.
		rootURI, _ = splitFragment(abs)
	}

	rm := newResourceMap()
	c.seedResources(rm)
	if err := walkResource(rm, value, rootURI, draft); err != nil {
		return nil, err
	}

	bindings, err := c.bindAndResolve(rm, value, rootURI, "#", draft, nil)
	if err != nil {
		return nil, err
	}

	if rawSource == nil {
		buf, err := json.Marshal(value)
		if err != nil {
			return nil, &CompileError{Message: "marshal schema for source bytes", Cause: err}
		}
		rawSource = buf
	}
	source := make([]byte, len(rawSource))
	copy(source, rawSource)

	resolvedID, _ := splitFragment(rootURI)
	schema := &Schema{
		source:        source,
		draft:         draft,
		id:            resolvedID,
		metaSchemaURI: metaSchemaURI,
		resources:     rm,
		bindings:      bindings,
		compileOpts:   c.opts,
	}
	eb := &evalBuilder{
		schema: schema,
		rm:     rm,
		loader: c.opts.loader,
		draft:  draft,
		cache:  map[string]*subschema{},
	}
	root, err := eb.buildSubschema(value, "#", rootURI, rootURI)
	if err != nil {
		return nil, err
	}
	schema.root = root

	if c.opts.metaSchemaValidation {
		if err := validateAgainstMetaSchema(schema, value, draft, metaSchemaURI); err != nil {
			return nil, err
		}
	}

	return schema, nil
}

// validateAgainstMetaSchema validates value (the user schema) against the
// embedded meta-schema for draft, or — when metaSchemaURI names a known
// dialect ([dialectMetaSchemaPaths]) — against the dialect's meta-schema.
// Failures are returned as a [*CompileError] with the validation errors as
// a cause.
func validateAgainstMetaSchema(_ *Schema, value any, draft Draft, metaSchemaURI string) error {
	var ms *Schema
	if metaSchemaURI != "" {
		if dialectMS, ok := metaSchemaForDialect(metaSchemaURI); ok {
			ms = dialectMS
		}
	}
	if ms == nil {
		s, err := MetaSchema(draft)
		if err != nil {
			return &CompileError{Message: "load meta-schema", Cause: err}
		}
		ms = s
	}
	res, err := ms.ValidateValue(value)
	if err != nil {
		return &CompileError{Message: "meta-schema validation", Cause: err}
	}
	if !res.Valid {
		var msg strings.Builder
		msg.WriteString("schema does not match meta-schema")
		for i, ve := range res.Errors {
			if i >= 5 {
				msg.WriteString("; ...")
				break
			}
			msg.WriteString("; ")
			msg.WriteString(ve.Keyword)
			msg.WriteString(" at ")
			msg.WriteString(ve.InstanceLocation)
			msg.WriteString(": ")
			msg.WriteString(ve.Message)
		}
		causes := make([]ValidationError, len(res.Errors))
		copy(causes, res.Errors)
		var cause error
		if len(causes) > 0 {
			ve := causes[0]
			cause = &ve
		}
		return &CompileError{Message: msg.String(), Cause: cause}
	}
	return nil
}

// seedResources copies any entries registered via [Compiler.AddResource]
// into rm as parsed-but-unwalked entries. The first $ref to such a resource
// will materialize it via the existing external-load path; the seed here
// simply skips the loader call.
func (c *Compiler) seedResources(rm *resourceMap) {
	c.resources.Range(func(key, val any) bool {
		uri, ok := key.(string)
		if !ok {
			return true
		}
		data, ok := val.([]byte)
		if !ok {
			return true
		}
		parsed, err := decodeSchemaBytes(data)
		if err != nil {
			return true
		}
		// Seeding is best-effort: walk failures surface when the resource
		// is actually consulted.
		if walkErr := walkResource(rm, parsed, uri, DraftDefault); walkErr != nil {
			_ = walkErr
		}
		return true
	})
}

// bindAndResolve walks node and emits keyword bindings plus resolved $ref /
// $dynamicRef edges. It also performs the value-shape checks that
// distinguish "this schema is malformed" from "this schema has unknown
// keywords" — the strict-keyword option determines which side of that line
// unknown keys fall on.
func (c *Compiler) bindAndResolve(rm *resourceMap, node any, baseURI, location string, draft Draft, refStack []string) ([]keywordBinding, error) {
	v, ok := node.(map[string]any)
	if !ok {
		return nil, nil
	}
	idKey, subDraft := bindResolveDraftKey(v, draft)
	newBaseURI, err := bindResolveBaseURI(v, idKey, baseURI, location)
	if err != nil {
		return nil, err
	}
	var out []keywordBinding
	for key, raw := range v {
		loc := location + "/" + escapePointerToken(key)
		if _, known := LookupKeyword(key, draft); !known && c.opts.strictKeywords {
			return nil, &CompileError{KeywordLocation: loc, Message: fmt.Sprintf("unknown keyword %q", key), Cause: ErrUnknownKeyword}
		}
		if err := validateKeywordShape(key, raw, loc); err != nil {
			return nil, err
		}
		binding, err := c.buildBinding(rm, key, raw, newBaseURI, loc, subDraft, refStack)
		if err != nil {
			return nil, err
		}
		out = append(out, binding)
		if descendsInto(key, draft) {
			children, err := c.bindAndResolveChild(rm, raw, newBaseURI, loc, subDraft, append(refStack, newBaseURI), key)
			if err != nil {
				return nil, err
			}
			out = append(out, children...)
		}
	}
	return out, nil
}

// bindResolveDraftKey returns the draft and id-keyword pair active inside v.
// A per-resource $schema overrides the inherited draft.
func bindResolveDraftKey(v map[string]any, draft Draft) (string, Draft) {
	subDraft := draft
	idKey := draft.IDKeyword()
	if rawSchema, ok := v["$schema"]; ok {
		if s, ok := rawSchema.(string); ok {
			if d := DraftFromMetaSchemaURL(s); d != DraftUnknown {
				subDraft = d
				idKey = subDraft.IDKeyword()
			}
		}
	}
	return idKey, subDraft
}

// bindResolveBaseURI returns the new base URI after applying any $id in v.
// If no $id is present (or it is empty / non-string), the input baseURI is
// returned unchanged.
func bindResolveBaseURI(v map[string]any, idKey, baseURI, location string) (string, error) {
	rawID, ok := v[idKey]
	if !ok {
		return baseURI, nil
	}
	s, ok := rawID.(string)
	if !ok || s == "" {
		return baseURI, nil
	}
	abs, err := resolveURI(baseURI, s)
	if err != nil {
		return "", &CompileError{KeywordLocation: location + "/" + idKey, Message: "invalid " + idKey, Cause: err}
	}
	abs, _ = splitFragment(abs)
	return abs, nil
}

// buildBinding constructs the keyword binding for (key, raw). For $ref and
// $dynamicRef it also performs compile-time resolution.
func (c *Compiler) buildBinding(rm *resourceMap, key string, raw any, baseURI, loc string, subDraft Draft, refStack []string) (keywordBinding, error) {
	binding := keywordBinding{Name: key, Location: loc, RawValue: raw, Resolved: nil}
	switch key {
	case "$ref":
		ref, ok := raw.(string)
		if !ok {
			return binding, &CompileError{KeywordLocation: loc, Message: "$ref must be a string"}
		}
		resolved, err := resolveRef(rm, c.opts.loader, baseURI, ref, append(refStack, baseURI), subDraft)
		if err != nil {
			return binding, err
		}
		binding.Resolved = resolved
	case "$dynamicRef":
		ref, ok := raw.(string)
		if !ok {
			return binding, &CompileError{KeywordLocation: loc, Message: "$dynamicRef must be a string"}
		}
		resolved, err := resolveRef(rm, c.opts.loader, baseURI, ref, append(refStack, baseURI), subDraft)
		if err != nil {
			// Static resolution failed: defer to a lazy edge so the
			// dynamic scope can supply a target at run time.
			binding.Resolved = &resolvedRef{Source: ref, AbsoluteURI: ref, Lazy: true, Target: nil, TargetURI: ""}
		} else {
			binding.Resolved = resolved
		}
	}
	return binding, nil
}

// bindAndResolveChild dispatches descent into keyword-specific child shapes.
func (c *Compiler) bindAndResolveChild(rm *resourceMap, raw any, baseURI, location string, draft Draft, refStack []string, key string) ([]keywordBinding, error) {
	var out []keywordBinding
	switch key {
	case "properties", "patternProperties", keyDefs, keyDefinitions, "dependentSchemas":
		m, ok := raw.(map[string]any)
		if !ok {
			return nil, nil
		}
		for k, child := range m {
			loc := location + "/" + escapePointerToken(k)
			children, err := c.bindAndResolve(rm, child, baseURI, loc, draft, refStack)
			if err != nil {
				return nil, err
			}
			out = append(out, children...)
		}
	case "items", "prefixItems":
		switch t := raw.(type) {
		case []any:
			for i, child := range t {
				loc := location + "/" + escapePointerToken(itoa(i))
				children, err := c.bindAndResolve(rm, child, baseURI, loc, draft, refStack)
				if err != nil {
					return nil, err
				}
				out = append(out, children...)
			}
		default:
			children, err := c.bindAndResolve(rm, t, baseURI, location, draft, refStack)
			if err != nil {
				return nil, err
			}
			out = append(out, children...)
		}
	case "allOf", "anyOf", "oneOf":
		arr, ok := raw.([]any)
		if !ok {
			return nil, nil
		}
		for i, child := range arr {
			loc := location + "/" + escapePointerToken(itoa(i))
			children, err := c.bindAndResolve(rm, child, baseURI, loc, draft, refStack)
			if err != nil {
				return nil, err
			}
			out = append(out, children...)
		}
	case "dependencies":
		m, ok := raw.(map[string]any)
		if !ok {
			return nil, nil
		}
		for k, child := range m {
			loc := location + "/" + escapePointerToken(k)
			if _, ok := child.(map[string]any); ok {
				children, err := c.bindAndResolve(rm, child, baseURI, loc, draft, refStack)
				if err != nil {
					return nil, err
				}
				out = append(out, children...)
			}
		}
	default:
		children, err := c.bindAndResolve(rm, raw, baseURI, location, draft, refStack)
		if err != nil {
			return nil, err
		}
		out = append(out, children...)
	}
	return out, nil
}

// validateKeywordShape verifies that a keyword's raw JSON value has the
// right shape (string vs. number vs. object, ...). Full meta-schema
// validation is opt-in via [WithMetaSchemaValidation].
func validateKeywordShape(key string, raw any, loc string) error {
	if shape, ok := shapeIntegers[key]; ok {
		return shape.check(key, raw, loc)
	}
	switch key {
	case "multipleOf":
		if !isPositiveNumber(raw) {
			return &CompileError{KeywordLocation: loc, Message: "multipleOf must be a positive number"}
		}
	case "minimum", "maximum", "exclusiveMinimum", "exclusiveMaximum":
		return checkNumberOrBool(key, raw, loc)
	case "pattern":
		return checkString(key, raw, loc)
	case "type":
		return checkType(raw, loc)
	case "required":
		return checkStringArray(key, raw, loc)
	case "enum":
		if _, ok := raw.([]any); !ok {
			return &CompileError{KeywordLocation: loc, Message: "enum must be an array"}
		}
	case "uniqueItems":
		if _, ok := raw.(bool); !ok {
			return &CompileError{KeywordLocation: loc, Message: "uniqueItems must be a boolean"}
		}
	case "properties", "patternProperties", keyDefs, keyDefinitions,
		"dependentSchemas", "dependencies":
		return checkObject(key, raw, loc)
	case "allOf", "anyOf", "oneOf":
		return checkNonEmptyArray(key, raw, loc)
	case "$id", "$schema", "$ref", "$anchor", "$dynamicAnchor", "$dynamicRef", "format":
		return checkString(key, raw, loc)
	}
	return nil
}

// shapeChecker is one entry in the per-keyword shape table.
type shapeChecker struct {
	check func(key string, raw any, loc string) error
}

// shapeIntegers groups all "non-negative integer" keywords behind one entry.
var shapeIntegers = map[string]shapeChecker{
	"minLength":     {check: checkNonNegativeInt},
	"maxLength":     {check: checkNonNegativeInt},
	"minItems":      {check: checkNonNegativeInt},
	"maxItems":      {check: checkNonNegativeInt},
	"minProperties": {check: checkNonNegativeInt},
	"maxProperties": {check: checkNonNegativeInt},
	"minContains":   {check: checkNonNegativeInt},
	"maxContains":   {check: checkNonNegativeInt},
}

func checkNonNegativeInt(key string, raw any, loc string) error {
	if !isNonNegativeInteger(raw) {
		return &CompileError{KeywordLocation: loc, Message: key + " must be a non-negative integer"}
	}
	return nil
}

func checkNumberOrBool(key string, raw any, loc string) error {
	switch raw.(type) {
	case json.Number, float64, int, int64, bool:
		return nil
	default:
		return &CompileError{KeywordLocation: loc, Message: key + " must be a number"}
	}
}

func checkString(key string, raw any, loc string) error {
	if _, ok := raw.(string); !ok {
		return &CompileError{KeywordLocation: loc, Message: key + " must be a string"}
	}
	return nil
}

func checkType(raw any, loc string) error {
	switch t := raw.(type) {
	case string:
		return nil
	case []any:
		for _, item := range t {
			if _, ok := item.(string); !ok {
				return &CompileError{KeywordLocation: loc, Message: "type array entries must be strings"}
			}
		}
		return nil
	default:
		return &CompileError{KeywordLocation: loc, Message: "type must be a string or array of strings"}
	}
}

func checkStringArray(key string, raw any, loc string) error {
	arr, ok := raw.([]any)
	if !ok {
		return &CompileError{KeywordLocation: loc, Message: key + " must be an array"}
	}
	for _, item := range arr {
		if _, ok := item.(string); !ok {
			return &CompileError{KeywordLocation: loc, Message: key + " entries must be strings"}
		}
	}
	return nil
}

func checkObject(key string, raw any, loc string) error {
	if _, ok := raw.(map[string]any); !ok {
		return &CompileError{KeywordLocation: loc, Message: key + " must be an object"}
	}
	return nil
}

func checkNonEmptyArray(key string, raw any, loc string) error {
	arr, ok := raw.([]any)
	if !ok || len(arr) == 0 {
		return &CompileError{KeywordLocation: loc, Message: key + " must be a non-empty array"}
	}
	return nil
}

// isNonNegativeInteger reports whether v is a JSON number ≥ 0 with no
// fractional part. Numbers like 2.0 count as integers per the spec.
func isNonNegativeInteger(v any) bool {
	switch t := v.(type) {
	case json.Number:
		if i, err := t.Int64(); err == nil {
			return i >= 0
		}
		if f, err := t.Float64(); err == nil {
			return f >= 0 && f == float64(int64(f))
		}
		return false
	case int:
		return t >= 0
	case int64:
		return t >= 0
	case float64:
		return t >= 0 && t == float64(int64(t))
	}
	return false
}

func isPositiveNumber(v any) bool {
	switch t := v.(type) {
	case json.Number:
		f, err := t.Float64()
		return err == nil && f > 0
	case int:
		return t > 0
	case int64:
		return t > 0
	case float64:
		return t > 0
	}
	return false
}

// escapePointerToken escapes ~ and / within a JSON Pointer reference
// token. Inverse of [unescapePointerToken].
func escapePointerToken(s string) string {
	out := make([]byte, 0, len(s))
	for i := range s {
		switch s[i] {
		case '~':
			out = append(out, '~', '0')
		case '/':
			out = append(out, '~', '1')
		default:
			out = append(out, s[i])
		}
	}
	return string(out)
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	neg := false
	if i < 0 {
		neg = true
		i = -i
	}
	var buf [20]byte
	pos := len(buf)
	for i > 0 {
		pos--
		buf[pos] = byte('0' + i%10)
		i /= 10
	}
	if neg {
		pos--
		buf[pos] = '-'
	}
	return string(buf[pos:])
}

// decodeSchemaBytes parses schemaJSON via [encoding/json.Decoder] with
// UseNumber set so multipleOf and friends preserve full precision.
func decodeSchemaBytes(schemaJSON []byte) (any, error) {
	dec := json.NewDecoder(bytes.NewReader(schemaJSON))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return nil, err
	}
	// Reject trailing content (matches encoding/json one-doc behavior).
	var trailing any
	if err := dec.Decode(&trailing); err == nil {
		return nil, errTrailingContent
	} else if !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("decode: %w", err)
	}
	return v, nil
}
