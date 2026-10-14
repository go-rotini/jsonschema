package jsonschema

import (
	"errors"
	"fmt"
	"net/url"
	"reflect"
	"slices"
	"strconv"
	"strings"
)

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

const (
	keyDefs              = "$defs"
	keyDefinitions       = "definitions"
	keyDependentSchemas  = "dependentSchemas"
	keyItems             = "items"
	keyAllOf             = "allOf"
	keyDependencies      = "dependencies"
	keyPatternProperties = "patternProperties"
	keyPrefixItems       = "prefixItems"
	keyAnyOf             = "anyOf"
	keyOneOf             = "oneOf"
	keyProperties        = "properties"
	keyDynamicRef        = "$dynamicRef"
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
	// policy decides what happens when a second resource claims a URI
	// already in byURI.
	policy RefCollisionPolicy
	// locations maps each walked schema object, by map identity, to its
	// canonical location. It feeds the absolute keyword locations in
	// validation output.
	locations map[uintptr]schemaLocation
}

// add registers res under uri, applying rm.policy when a different resource
// already holds uri. A resource with the same content as the holder is not a
// collision and replaces it.
func (rm *resourceMap) add(uri string, res *resource, idKey string) error {
	existing, dup := rm.byURI[uri]
	if !dup || existing == nil {
		rm.byURI[uri] = res
		rm.order = append(rm.order, uri)
		return nil
	}
	if reflect.DeepEqual(existing.root, res.root) {
		rm.byURI[uri] = res
		return nil
	}
	switch rm.policy {
	case RefCollisionFirstWins:
		return nil
	case RefCollisionLastWins:
		rm.byURI[uri] = res
		return nil
	default:
		return &CompileError{
			KeywordLocation: idKey,
			Message:         fmt.Sprintf("duplicate $id %q", uri),
		}
	}
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
	// recursiveAnchor is true when the resource root carries
	// "$recursiveAnchor": true (Draft 2019-09 only). $recursiveRef walks
	// the dynamic scope from outermost to innermost looking for the first
	// resource whose recursiveAnchor flag is set.
	recursiveAnchor bool
	// draft is the effective draft for this resource. Inherited from the
	// containing resource unless an explicit $schema is declared.
	draft Draft
	// external is true for a resource that came from a registered or
	// fetched document rather than the schema being compiled.
	external bool
}

// newResourceMap allocates an empty [resourceMap] ready for [walkResource].
func newResourceMap() *resourceMap {
	return &resourceMap{byURI: make(map[string]*resource)}
}

// walkResource walks node and populates rm with every resource boundary
// rooted at node. baseURI is the absolute URI for the root resource; draft
// drives keyword-name selection (id vs. $id, etc.).
// The root resource replaces any resource already registered under baseURI,
// and baseURI becomes rm's root URI.
func walkResource(rm *resourceMap, node any, baseURI string, draft Draft) error {
	root := newResource(baseURI, node, draft)
	if _, dup := rm.byURI[baseURI]; !dup {
		rm.order = append(rm.order, baseURI)
	}
	rm.byURI[baseURI] = root
	rm.rootURI = baseURI
	return walkNode(rm, root, node, draft, "")
}

// walkDocument walks a document other than the schema being compiled (a
// registered or fetched resource) into rm under uri, applying rm.policy if
// uri is already taken. rm's root URI is left unchanged.
func walkDocument(rm *resourceMap, node any, uri string, draft Draft) error {
	res := newResource(uri, node, draft)
	res.external = true
	if err := rm.add(uri, res, "$id"); err != nil {
		return err
	}
	return walkNode(rm, res, node, draft, "")
}

func newResource(uri string, node any, draft Draft) *resource {
	return &resource{
		baseURI:        uri,
		root:           node,
		anchors:        map[string]any{},
		dynamicAnchors: map[string]any{},
		draft:          draft,
	}
}

// walkNode recurses through node, opening a new resource whenever a nested
// object declares its own $id. ptr is node's JSON Pointer within current.
// Boolean schemas contribute no anchors or refs and are skipped silently.
func walkNode(rm *resourceMap, current *resource, node any, draft Draft, ptr string) error {
	obj, ok := node.(map[string]any)
	if !ok {
		return nil
	}

	idKey, subDraft := resolveDraftAndIDKey(obj, current.draft, draft)
	target, opened, err := openResourceIfNeeded(rm, current, obj, idKey, subDraft)
	if err != nil {
		return err
	}
	if target != current {
		ptr = ""
	}
	rm.recordLocation(obj, target.baseURI, ptr)
	isResourceRoot := opened || sameMap(target.root, obj)
	registerAnchors(target, obj, isResourceRoot)
	// Sorted keys make the walk order, and so the [RefCollisionPolicy]
	// outcome, deterministic.
	for _, k := range orderKeys(obj) {
		if !descendsInto(k, draft) {
			continue
		}
		if err := walkChild(rm, target, k, obj[k], subDraft, ptr+"/"+escapePointerToken(k)); err != nil {
			return err
		}
	}
	return nil
}

// schemaLocation is the canonical location of a subschema: the URI of the
// resource that holds it and its JSON Pointer within that resource.
type schemaLocation struct {
	uri string
	ptr string
}

// recordLocation notes obj's canonical location, keeping the first one
// recorded.
func (rm *resourceMap) recordLocation(obj map[string]any, uri, ptr string) {
	if rm.locations == nil {
		rm.locations = map[uintptr]schemaLocation{}
	}
	key := reflect.ValueOf(obj).Pointer()
	if _, seen := rm.locations[key]; !seen {
		rm.locations[key] = schemaLocation{uri: uri, ptr: ptr}
	}
}

// location returns obj's canonical location, if it was walked.
func (rm *resourceMap) location(obj map[string]any) (schemaLocation, bool) {
	l, ok := rm.locations[reflect.ValueOf(obj).Pointer()]
	return l, ok
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
// current is returned unchanged. The boolean is true when a fresh resource
// was opened.
func openResourceIfNeeded(rm *resourceMap, current *resource, obj map[string]any, idKey string, subDraft Draft) (*resource, bool, error) {
	rawID, ok := obj[idKey]
	if !ok {
		return current, false, nil
	}
	idStr, ok := rawID.(string)
	if !ok || idStr == "" {
		return current, false, nil
	}
	// Pre-2019-09 drafts allowed a plain-name $id (e.g. {"$id": "#foo"})
	// as an alternative anchor declaration; from 2019-09 on, $id must be
	// absolute and a "#name" $id is treated as a sibling anchor instead.
	// The name is honored next to $ref too: it changes no base URI, and
	// schemas commonly name a $ref this way.
	if subDraft <= Draft7 && strings.HasPrefix(idStr, "#") && !strings.HasPrefix(idStr, "#/") {
		name := idStr[1:]
		if name != "" {
			current.anchors[name] = obj
		}
		return current, false, nil
	}
	// Before 2019-09, $ref replaces every other sibling keyword.
	if _, hasRef := obj["$ref"]; hasRef && subDraft <= Draft7 {
		return current, false, nil
	}
	absID, err := resolveURI(current.baseURI, idStr)
	if err != nil {
		return nil, false, &CompileError{KeywordLocation: idKey, Message: "invalid " + idKey, Cause: err}
	}
	absID, _ = splitFragment(absID)
	if existing := rm.byURI[absID]; existing != nil && sameMap(existing.root, obj) {
		// A document root re-entering its own $id.
		return existing, true, nil
	}
	// Under [RefCollisionFirstWins] a losing resource is still walked, so
	// its anchors stay scoped to it rather than leaking into the winner.
	res := newResource(absID, obj, subDraft)
	res.external = current.external
	if err := rm.add(absID, res, idKey); err != nil {
		return nil, false, err
	}
	return res, true, nil
}

// registerAnchors records $anchor and $dynamicAnchor entries on target.
// When isResourceRoot is true, "$recursiveAnchor": true on obj also flips the
// resource's recursiveAnchor flag (Draft 2019-09).
func registerAnchors(target *resource, obj map[string]any, isResourceRoot bool) {
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
	if isResourceRoot {
		if v, ok := obj["$recursiveAnchor"]; ok {
			if b, ok := v.(bool); ok && b {
				target.recursiveAnchor = true
			}
		}
	}
}

// walkChild dispatches descent into k's value v. The dispatch list mirrors
// JSON Schema's per-keyword child shape (object-of-schemas, array-of-schemas,
// or a single subschema).
func walkChild(rm *resourceMap, target *resource, k string, v any, subDraft Draft, ptr string) error {
	switch k {
	case keyProperties, keyPatternProperties, keyDefs, keyDefinitions, keyDependentSchemas:
		return walkSchemaMap(rm, target, v, subDraft, ptr)
	case keyItems, keyPrefixItems:
		return walkItemsLike(rm, target, v, subDraft, ptr)
	case keyAllOf, keyAnyOf, keyOneOf:
		return walkSchemaArray(rm, target, v, subDraft, ptr)
	case keyDependencies:
		return walkDependencies(rm, target, v, subDraft, ptr)
	default:
		return walkNode(rm, target, v, subDraft, ptr)
	}
}

func walkSchemaMap(rm *resourceMap, target *resource, v any, subDraft Draft, ptr string) error {
	m, ok := v.(map[string]any)
	if !ok {
		return nil
	}
	for _, k := range orderKeys(m) {
		if err := walkNode(rm, target, m[k], subDraft, ptr+"/"+escapePointerToken(k)); err != nil {
			return err
		}
	}
	return nil
}

func walkSchemaArray(rm *resourceMap, target *resource, v any, subDraft Draft, ptr string) error {
	arr, ok := v.([]any)
	if !ok {
		return nil
	}
	for i, child := range arr {
		if err := walkNode(rm, target, child, subDraft, ptr+"/"+itoa(i)); err != nil {
			return err
		}
	}
	return nil
}

func walkItemsLike(rm *resourceMap, target *resource, v any, subDraft Draft, ptr string) error {
	switch t := v.(type) {
	case []any:
		return walkSchemaArray(rm, target, t, subDraft, ptr)
	case map[string]any:
		return walkNode(rm, target, t, subDraft, ptr)
	}
	return nil
}

func walkDependencies(rm *resourceMap, target *resource, v any, subDraft Draft, ptr string) error {
	m, ok := v.(map[string]any)
	if !ok {
		return nil
	}
	for _, k := range orderKeys(m) {
		child := m[k]
		// Pre-2019-09 dependencies could be either a schema or a list of
		// property names; only the schema variant carries subschemas.
		if _, ok := child.(map[string]any); ok {
			if err := walkNode(rm, target, child, subDraft, ptr+"/"+escapePointerToken(k)); err != nil {
				return err
			}
		}
	}
	return nil
}

// descendsInto reports whether the keyword k may carry a subschema (or a
// container of subschemas). The list is the union of applicators across
// every supported draft.
func descendsInto(k string, _ Draft) bool {
	switch k {
	case
		keyProperties, keyPatternProperties, "additionalProperties",
		"propertyNames", keyItems, keyPrefixItems, "additionalItems",
		"contains", "not", "if", "then", "else",
		keyAllOf, keyAnyOf, keyOneOf,
		keyDefs, keyDefinitions,
		keyDependentSchemas, keyDependencies,
		"unevaluatedItems", "unevaluatedProperties",
		"contentSchema":
		return true
	}
	return false
}

// resolvedRef is the compile-time representation of a resolved $ref edge.
// The validator consults the Target / TargetURI to evaluate the target
// subschema and follows lazy edges (cycles) at run time bounded by
// [WithMaxRefDepth].
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

	// A repeat URI on the stack signals a cycle: defer to a lazy edge.
	if slices.Contains(stack, abs) {
		return &resolvedRef{Source: ref, AbsoluteURI: abs, TargetURI: docPart, Lazy: true}, nil
	}

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
	if err := walkDocument(rm, parsed, docPart, extDraft); err != nil {
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
		// Fragment is URI-escaped (RFC 3986); pointer tokens are then
		// ~0/~1-escaped (RFC 6901).
		ptr, err := url.PathUnescape(frag[1:])
		if err != nil {
			ptr = frag[1:]
		}
		return jsonPointer(res.root, ptr)
	case strings.HasPrefix(frag, "#"):
		name := frag[1:]
		if decoded, err := url.PathUnescape(name); err == nil {
			name = decoded
		}
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

// sameMap reports whether the two values share the same map[string]any
// header. Avoids the runtime panic from comparing maps with ==.
func sameMap(a any, b map[string]any) bool {
	am, ok := a.(map[string]any)
	if !ok {
		return false
	}
	return reflect.ValueOf(am).UnsafePointer() == reflect.ValueOf(b).UnsafePointer()
}
