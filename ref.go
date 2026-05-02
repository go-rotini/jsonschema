package jsonschema

import (
	"errors"
	"fmt"
	"net/url"
	"slices"
	"strconv"
	"strings"
)

// Static error sentinels for ref.go's value-shaped errors. Wrapping these via
// %w keeps the err113 lint clean while still surfacing useful context to the
// caller.
var (
	errNoLoaderForExternalRef   = errors.New("unknown resource and no loader configured")
	errLoaderEmptyDocument      = errors.New("loader returned a document with no resource")
	errAnchorNotFound           = errors.New("anchor not found in resource")
	errInvalidFragment          = errors.New("invalid fragment")
	errInvalidJSONPointer       = errors.New("invalid JSON Pointer")
	errPointerTokenMissing      = errors.New("pointer token not found")
	errPointerInvalidArrayIndex = errors.New("invalid array index")
	errPointerIndexOutOfRange   = errors.New("index out of range")
	errPointerCannotDescend     = errors.New("cannot descend into scalar")
)

// keyword constants used in multiple branches; extracted so goconst stays
// quiet and the descent rules stay easy to scan.
const (
	keyDefs        = "$defs"
	keyDefinitions = "definitions"
)

// resolveURI resolves ref against base per RFC 3986. Either argument may be
// empty: an empty ref returns base, an empty base returns ref. The function
// never errors on a syntactically valid URI; it only surfaces parse failures
// from net/url.
func resolveURI(base, ref string) (string, error) {
	if base == "" && ref == "" {
		return "", nil
	}
	if base == "" {
		// Validate the ref parses, then return it verbatim.
		if _, err := url.Parse(ref); err != nil {
			return "", fmt.Errorf("parse ref: %w", err)
		}
		return ref, nil
	}
	baseURL, err := url.Parse(base)
	if err != nil {
		return "", fmt.Errorf("parse base: %w", err)
	}
	if ref == "" {
		return base, nil
	}
	refURL, err := url.Parse(ref)
	if err != nil {
		return "", fmt.Errorf("parse ref: %w", err)
	}
	return baseURL.ResolveReference(refURL).String(), nil
}

// splitFragment partitions uri into (basePart, fragmentPart). fragmentPart
// includes the leading "#" when present so callers can distinguish between
// "no fragment" ("") and "empty fragment" ("#").
func splitFragment(uri string) (string, string) {
	idx := strings.Index(uri, "#")
	if idx < 0 {
		return uri, ""
	}
	return uri[:idx], uri[idx:]
}

// resourceMap collects every $id-bounded resource in a compiled schema graph.
// Keys are the absolute URI of each resource; values carry the parsed root,
// the anchor and dynamic-anchor indexes, and the active draft.
type resourceMap struct {
	// byURI is keyed by absolute URI. The empty string is reserved for the
	// root resource when no $id is declared and no base URI is supplied.
	byURI map[string]*resource
	// rootURI is the URI of the schema's root resource (the entry point
	// for fragment-less refs and for [Schema.Resources]).
	rootURI string
	// order is the declaration order of every URI in byURI, so the public
	// [Schema.Resources] accessor can return root-first plus nested in
	// declaration order.
	order []string
}

// resource is a single $id-bounded subtree within a compiled schema graph.
// Anchors (plain-name and dynamic) are scoped to the enclosing resource: a
// $ref with a plain-name fragment looks up the anchor in the resource that
// the ref's base URI resolves to, not in the document root.
type resource struct {
	// baseURI is the absolute URI of this resource. The root resource may
	// have an empty baseURI if neither $id nor [WithBaseURI] supplied one.
	baseURI string
	// root is the parsed schema value at this resource boundary (the
	// object that owns the $id keyword that opened the resource).
	root any
	// anchors maps each plain-name $anchor to its target subschema.
	anchors map[string]any
	// dynamicAnchors maps each $dynamicAnchor name to its target. Lookup
	// at validation time walks the dynamic scope; here we only build the
	// per-resource index.
	dynamicAnchors map[string]any
	// draft is the effective draft for this resource. Inherited from the
	// containing resource unless an explicit $schema is declared.
	draft Draft
}

// newResourceMap allocates an empty [resourceMap] ready for [walkResource].
func newResourceMap() *resourceMap {
	return &resourceMap{byURI: make(map[string]*resource)}
}

// walkResource walks node and populates rm with every resource boundary
// rooted at node. baseURI is the absolute URI for the root resource; draft
// drives keyword-name selection (id vs. $id, etc.).
func walkResource(rm *resourceMap, node any, baseURI string, draft Draft) error {
	root := &resource{
		baseURI:        baseURI,
		root:           node,
		anchors:        map[string]any{},
		dynamicAnchors: map[string]any{},
		draft:          draft,
	}
	rm.byURI[baseURI] = root
	rm.rootURI = baseURI
	rm.order = append(rm.order, baseURI)
	return walkNode(rm, root, node, draft)
}

// walkNode recurses through node (a parsed schema value) using current as
// the active resource. When a nested object declares its own $id, walkNode
// opens a new resource and continues the walk inside it.
//
// A schema is, syntactically, either an object (a real schema) or a boolean
// (the "true"/"false" shorthand). Booleans contribute no anchors or refs so
// they are simply ignored at the resource level.
func walkNode(rm *resourceMap, current *resource, node any, draft Draft) error {
	obj, ok := node.(map[string]any)
	if !ok {
		return nil
	}

	idKey, subDraft := resolveDraftAndIDKey(obj, current.draft, draft)
	target, err := openResourceIfNeeded(rm, current, obj, idKey, subDraft)
	if err != nil {
		return err
	}
	registerAnchors(target, obj)
	for k, v := range obj {
		if !descendsInto(k, draft) {
			continue
		}
		if err := walkChild(rm, target, k, v, subDraft); err != nil {
			return err
		}
	}
	return nil
}

// resolveDraftAndIDKey pulls a per-resource $schema override out of obj (if
// present) and returns the matching draft + id-keyword pair. The fallback
// is the inherited draft.
func resolveDraftAndIDKey(obj map[string]any, inherited, descent Draft) (string, Draft) {
	subDraft := inherited
	idKey := descent.IDKeyword()
	if v, ok := obj["$schema"]; ok {
		if s, ok := v.(string); ok {
			if d := DraftFromMetaSchemaURL(s); d != DraftUnknown {
				subDraft = d
				idKey = subDraft.IDKeyword()
			}
		}
	}
	return idKey, subDraft
}

// openResourceIfNeeded returns the resource that owns obj. If obj declares a
// nested $id, a fresh resource is allocated and registered on rm; otherwise
// current is returned unchanged.
func openResourceIfNeeded(rm *resourceMap, current *resource, obj map[string]any, idKey string, subDraft Draft) (*resource, error) {
	rawID, ok := obj[idKey]
	if !ok {
		return current, nil
	}
	idStr, ok := rawID.(string)
	if !ok || idStr == "" {
		return current, nil
	}
	absID, err := resolveURI(current.baseURI, idStr)
	if err != nil {
		return nil, &CompileError{KeywordLocation: idKey, Message: "invalid " + idKey, Cause: err}
	}
	absID, _ = splitFragment(absID)
	res := &resource{
		baseURI:        absID,
		root:           obj,
		anchors:        map[string]any{},
		dynamicAnchors: map[string]any{},
		draft:          subDraft,
	}
	rm.byURI[absID] = res
	rm.order = append(rm.order, absID)
	return res, nil
}

// registerAnchors records $anchor and $dynamicAnchor entries on target.
func registerAnchors(target *resource, obj map[string]any) {
	if v, ok := obj["$anchor"]; ok {
		if name, ok := v.(string); ok && name != "" {
			target.anchors[name] = obj
		}
	}
	if v, ok := obj["$dynamicAnchor"]; ok {
		if name, ok := v.(string); ok && name != "" {
			target.dynamicAnchors[name] = obj
			if _, dup := target.anchors[name]; !dup {
				target.anchors[name] = obj
			}
		}
	}
}

// walkChild dispatches descent into k's value v. The dispatch list mirrors
// JSON Schema's per-keyword child shape (object-of-schemas, array-of-schemas,
// or a single subschema).
func walkChild(rm *resourceMap, target *resource, k string, v any, subDraft Draft) error {
	switch k {
	case "properties", "patternProperties", keyDefs, keyDefinitions, "dependentSchemas":
		return walkSchemaMap(rm, target, v, subDraft)
	case "items", "prefixItems":
		return walkItemsLike(rm, target, v, subDraft)
	case "allOf", "anyOf", "oneOf":
		return walkSchemaArray(rm, target, v, subDraft)
	case "dependencies":
		return walkDependencies(rm, target, v, subDraft)
	default:
		return walkNode(rm, target, v, subDraft)
	}
}

func walkSchemaMap(rm *resourceMap, target *resource, v any, subDraft Draft) error {
	m, ok := v.(map[string]any)
	if !ok {
		return nil
	}
	for _, child := range m {
		if err := walkNode(rm, target, child, subDraft); err != nil {
			return err
		}
	}
	return nil
}

func walkSchemaArray(rm *resourceMap, target *resource, v any, subDraft Draft) error {
	arr, ok := v.([]any)
	if !ok {
		return nil
	}
	for _, child := range arr {
		if err := walkNode(rm, target, child, subDraft); err != nil {
			return err
		}
	}
	return nil
}

func walkItemsLike(rm *resourceMap, target *resource, v any, subDraft Draft) error {
	switch t := v.(type) {
	case []any:
		for _, child := range t {
			if err := walkNode(rm, target, child, subDraft); err != nil {
				return err
			}
		}
		return nil
	case map[string]any:
		return walkNode(rm, target, t, subDraft)
	}
	return nil
}

func walkDependencies(rm *resourceMap, target *resource, v any, subDraft Draft) error {
	m, ok := v.(map[string]any)
	if !ok {
		return nil
	}
	for _, child := range m {
		// Pre-2019-09 dependencies were either a schema or a list of
		// property names. Only the schema variant carries subschemas.
		if _, ok := child.(map[string]any); ok {
			if err := walkNode(rm, target, child, subDraft); err != nil {
				return err
			}
		}
	}
	return nil
}

// descendsInto reports whether the keyword k may carry a subschema (or a
// container of subschemas) for the given draft. The list is the union of
// every applicator across drafts; per-draft retirement is enforced by the
// caller (a retired keyword in this draft simply has no value in the parsed
// schema, so the descent is a no-op).
func descendsInto(k string, _ Draft) bool {
	switch k {
	case
		"properties", "patternProperties", "additionalProperties",
		"propertyNames", "items", "prefixItems", "additionalItems",
		"contains", "not", "if", "then", "else",
		"allOf", "anyOf", "oneOf",
		keyDefs, keyDefinitions,
		"dependentSchemas", "dependencies",
		"unevaluatedItems", "unevaluatedProperties",
		"contentSchema":
		return true
	}
	return false
}

// resolvedRef is the compile-time representation of a resolved $ref edge.
// The validator (Phase 4) consults the Target / TargetURI to evaluate the
// target subschema; until then the struct is built and stored on the
// schema's keyword bindings without being evaluated.
type resolvedRef struct {
	// Source is the literal $ref value as written in the schema.
	Source string
	// AbsoluteURI is the resolved absolute URI (including any fragment).
	AbsoluteURI string
	// Target is the parsed subschema the ref points at, or nil when the
	// ref is a lazy edge (cycle) that the validator must follow at run
	// time.
	Target any
	// TargetURI is the absolute URI of the resource that owns Target,
	// without fragment.
	TargetURI string
	// Lazy is true when the ref forms a compile-time cycle and Target is
	// intentionally nil. The validator follows the edge at run time
	// bounded by [WithMaxRefDepth].
	Lazy bool
}

// resolveRef looks up ref against rm and (if necessary) loader. baseURI is
// the URI of the resource the ref is written in; stack carries the URIs of
// in-flight resolutions so cycles can be detected and turned into lazy
// edges.
func resolveRef(rm *resourceMap, loader Loader, baseURI, ref string, stack []string, draft Draft) (*resolvedRef, error) {
	abs, err := resolveURI(baseURI, ref)
	if err != nil {
		return nil, &RefError{Ref: ref, BaseURI: baseURI, Cause: err}
	}
	docPart, frag := splitFragment(abs)

	// Cycle detection: the same absolute URI appearing twice on the stack
	// becomes a lazy edge (the validator walks it at run time).
	if slices.Contains(stack, abs) {
		return &resolvedRef{Source: ref, AbsoluteURI: abs, TargetURI: docPart, Lazy: true}, nil
	}

	// Locate the resource owning docPart.
	res, ok := rm.byURI[docPart]
	if !ok {
		fetched, err := fetchExternalResource(rm, loader, ref, baseURI, docPart, draft)
		if err != nil {
			return nil, err
		}
		res = fetched
	}

	target, err := resolveFragment(res, frag)
	if err != nil {
		return nil, &RefError{Ref: ref, BaseURI: baseURI, Cause: err}
	}
	return &resolvedRef{Source: ref, AbsoluteURI: abs, Target: target, TargetURI: docPart}, nil
}

// fetchExternalResource pulls docPart from loader, parses it, and walks the
// resulting tree into rm. Used by [resolveRef] when the doc is not already
// in the resource map.
func fetchExternalResource(rm *resourceMap, loader Loader, ref, baseURI, docPart string, draft Draft) (*resource, error) {
	if loader == nil || docPart == "" {
		return nil, &RefError{Ref: ref, BaseURI: baseURI, Cause: errNoLoaderForExternalRef}
	}
	data, err := loader.Load(docPart)
	if err != nil {
		return nil, &RefError{Ref: ref, BaseURI: baseURI, Cause: err}
	}
	parsed, err := decodeSchemaBytes(data)
	if err != nil {
		return nil, &RefError{Ref: ref, BaseURI: baseURI, Cause: err}
	}
	extDraft := draft
	if obj, ok := parsed.(map[string]any); ok {
		if v, ok := obj["$schema"]; ok {
			if s, ok := v.(string); ok {
				if d := DraftFromMetaSchemaURL(s); d != DraftUnknown {
					extDraft = d
				}
			}
		}
	}
	if err := walkResource(rm, parsed, docPart, extDraft); err != nil {
		return nil, &RefError{Ref: ref, BaseURI: baseURI, Cause: err}
	}
	res := rm.byURI[docPart]
	if res == nil {
		return nil, &RefError{Ref: ref, BaseURI: baseURI, Cause: errLoaderEmptyDocument}
	}
	return res, nil
}

// resolveFragment dispatches a fragment against res. An empty fragment (or
// just "#") points at the resource root; "#/..." is JSON-Pointer; "#name"
// is a plain-name anchor.
func resolveFragment(res *resource, frag string) (any, error) {
	switch {
	case frag == "" || frag == "#":
		return res.root, nil
	case strings.HasPrefix(frag, "#/"):
		return jsonPointer(res.root, frag[1:])
	case strings.HasPrefix(frag, "#"):
		name := frag[1:]
		if v, ok := res.anchors[name]; ok {
			return v, nil
		}
		if v, ok := res.dynamicAnchors[name]; ok {
			return v, nil
		}
		return nil, fmt.Errorf("%w: %q in resource %q", errAnchorNotFound, name, res.baseURI)
	default:
		return nil, fmt.Errorf("%w: %q", errInvalidFragment, frag)
	}
}

// jsonPointer walks pointer (in RFC 6901 form, leading slash) against root
// and returns the addressed value.
func jsonPointer(root any, pointer string) (any, error) {
	if pointer == "" || pointer == "/" {
		return root, nil
	}
	if !strings.HasPrefix(pointer, "/") {
		return nil, fmt.Errorf("%w: %q", errInvalidJSONPointer, pointer)
	}
	cur := root
	for raw := range strings.SplitSeq(pointer[1:], "/") {
		token := unescapePointerToken(raw)
		switch v := cur.(type) {
		case map[string]any:
			next, ok := v[token]
			if !ok {
				return nil, fmt.Errorf("%w: %q", errPointerTokenMissing, token)
			}
			cur = next
		case []any:
			i, err := strconv.Atoi(token)
			if err != nil {
				return nil, fmt.Errorf("%w: %q", errPointerInvalidArrayIndex, token)
			}
			if i < 0 || i >= len(v) {
				return nil, fmt.Errorf("%w: %d", errPointerIndexOutOfRange, i)
			}
			cur = v[i]
		default:
			return nil, fmt.Errorf("%w: token %q", errPointerCannotDescend, token)
		}
	}
	return cur, nil
}

// unescapePointerToken decodes the two RFC 6901 escapes (~1 → /, ~0 → ~).
// Order matters: ~1 must be substituted before ~0 to avoid double-decoding.
func unescapePointerToken(s string) string {
	s = strings.ReplaceAll(s, "~1", "/")
	s = strings.ReplaceAll(s, "~0", "~")
	return s
}
