package jsonschema

import (
	"bytes"
	"encoding/json"
	"sort"
	"strings"
	"sync"
)

// outputNode is one node in the Detailed / Verbose tree. Leaf nodes carry
// either error (failure) or annotation (passing keyword); group nodes
// aggregate child failures or annotations under a shared path prefix.
type outputNode struct {
	valid                   bool
	keywordLocation         string
	absoluteKeywordLocation string
	instanceLocation        string
	error                   string
	annotation              any
	// hasAnnotation distinguishes a real annotation value (which may be
	// nil or false) from "no annotation set on this node".
	hasAnnotation bool
	errors        []*outputNode
	annotations   []*outputNode
}

// Output renders the result in the requested format per Draft 2020-12 §12.
// The four formats are:
//
//   - [OutputFlag]: {"valid": true|false} only.
//   - [OutputBasic]: a flat list of assertion outcomes with location info.
//   - [OutputDetailed]: a nested tree of failures, collapsing passing groups.
//   - [OutputVerbose]: the full nested tree, including passing groups.
//
// The returned bytes are valid JSON and validate against the Draft 2020-12
// output meta-schema (https://json-schema.org/draft/2020-12/output/schema).
func (r *Result) Output(format OutputFormat) []byte {
	if r == nil {
		return []byte(`{"valid":false}`)
	}
	switch format {
	case OutputFlag:
		return renderFlag(r)
	case OutputBasic:
		return renderBasic(r)
	case OutputDetailed:
		return renderDetailed(r)
	case OutputVerbose:
		return renderVerbose(r)
	default:
		return renderFlag(r)
	}
}

// renderFlag emits {"valid": true|false}.
func renderFlag(r *Result) []byte {
	if r.Valid {
		return []byte(`{"valid":true}`)
	}
	return []byte(`{"valid":false}`)
}

// flatBasicEntry is the JSON shape emitted for one entry in a Basic-format
// rendering.
type flatBasicEntry struct {
	Valid                   bool   `json:"valid"`
	KeywordLocation         string `json:"keywordLocation"`
	AbsoluteKeywordLocation string `json:"absoluteKeywordLocation,omitempty"`
	InstanceLocation        string `json:"instanceLocation"`
	Error                   string `json:"error,omitempty"`
	Annotation              any    `json:"annotation,omitempty"`
}

// normalizeKeywordLocation strips the internal "#" prefix so the output's
// keywordLocation conforms to the spec's json-pointer format. "" stays "".
func normalizeKeywordLocation(s string) string {
	if s == "" {
		return ""
	}
	if s == "#" {
		return ""
	}
	if strings.HasPrefix(s, "#/") {
		return s[1:]
	}
	return s
}

// sanitizeAnnotation converts the validator's internal annotation types
// (evaluatedKeys / evaluatedItems / evaluatedItemsAll) into JSON-friendly
// shapes for the rendered output.
func sanitizeAnnotation(v any) any {
	switch t := v.(type) {
	case evaluatedKeys:
		out := make([]string, 0, len(t))
		for k := range t {
			out = append(out, k)
		}
		sort.Strings(out)
		return out
	case evaluatedItems:
		return int(t)
	case evaluatedItemsAll:
		return true
	}
	return v
}

// renderBasic emits a flat-list output. When the result is invalid the list
// is `errors`; when valid the list is `annotations`. The top-level node is
// always `{"valid": ..., "keywordLocation": "", "instanceLocation": "", ...}`.
func renderBasic(r *Result) []byte {
	type basicShape struct {
		Valid            bool             `json:"valid"`
		KeywordLocation  string           `json:"keywordLocation"`
		InstanceLocation string           `json:"instanceLocation"`
		Errors           []flatBasicEntry `json:"errors,omitempty"`
		Annotations      []flatBasicEntry `json:"annotations,omitempty"`
	}
	out := basicShape{
		Valid:            r.Valid,
		KeywordLocation:  "",
		InstanceLocation: "",
	}
	if !r.Valid {
		out.Errors = make([]flatBasicEntry, 0, len(r.Errors)+1)
		// Header entry per spec example, followed by per-error entries.
		out.Errors = append(out.Errors, flatBasicEntry{
			Valid:            false,
			KeywordLocation:  "",
			InstanceLocation: "",
			Error:            "validation failed",
		})
		for i := range r.Errors {
			e := &r.Errors[i]
			appendFlatErrorEntries(&out.Errors, e)
		}
	} else {
		out.Annotations = make([]flatBasicEntry, 0, len(r.Annotations))
		for i := range r.Annotations {
			a := &r.Annotations[i]
			out.Annotations = append(out.Annotations, flatBasicEntry{
				Valid:                   true,
				KeywordLocation:         normalizeKeywordLocation(a.KeywordLocation),
				AbsoluteKeywordLocation: a.AbsoluteKeywordLocation,
				InstanceLocation:        a.InstanceLocation,
				Annotation:              sanitizeAnnotation(a.Value),
			})
		}
	}
	return marshalCompact(out)
}

// appendFlatErrorEntries flattens a ValidationError tree into the Basic
// errors list. Causes are inlined as additional entries under their parent's
// keywordLocation.
func appendFlatErrorEntries(out *[]flatBasicEntry, e *ValidationError) {
	*out = append(*out, flatBasicEntry{
		Valid:                   false,
		KeywordLocation:         normalizeKeywordLocation(e.KeywordLocation),
		AbsoluteKeywordLocation: e.AbsoluteKeywordLocation,
		InstanceLocation:        e.InstanceLocation,
		Error:                   errorMessage(e),
	})
	for i := range e.Causes {
		appendFlatErrorEntries(out, &e.Causes[i])
	}
}

// errorMessage returns a non-empty message for an error, falling back to
// "validation failed" when the error has no message of its own.
func errorMessage(e *ValidationError) string {
	if e.Message != "" {
		return e.Message
	}
	if e.Keyword != "" {
		return e.Keyword + " validation failed"
	}
	return "validation failed"
}

// renderDetailed emits the nested tree, with passing branches pruned. The
// tree is built from the flat error list using keywordLocation paths.
func renderDetailed(r *Result) []byte {
	root := buildOutputTree(r)
	if r.Valid {
		// Detailed format for a valid result is the simple top-level node
		// with annotations attached.
		root = pruneToValid(root)
	} else {
		root = pruneToFailing(root)
	}
	return marshalCompact(nodeToJSON(root, false))
}

// renderVerbose emits the full nested tree including passing groups.
func renderVerbose(r *Result) []byte {
	root := buildOutputTree(r)
	propagateValidity(root)
	return marshalCompact(nodeToJSON(root, true))
}

// propagateValidity sets each group node's validity from its descendants:
// any failing descendant flips the group invalid.
func propagateValidity(n *outputNode) bool {
	if n == nil {
		return true
	}
	if n.error != "" {
		n.valid = false
		return false
	}
	allValid := true
	for _, c := range n.errors {
		if !propagateValidity(c) {
			allValid = false
		}
	}
	for _, c := range n.annotations {
		if !propagateValidity(c) {
			allValid = false
		}
	}
	if !allValid {
		n.valid = false
	}
	return n.valid
}

// buildOutputTree reconstructs a tree from the flat error/annotation lists
// on r, grouping entries that share keywordLocation prefixes.
func buildOutputTree(r *Result) *outputNode {
	root := &outputNode{
		valid:            r.Valid,
		keywordLocation:  "",
		instanceLocation: "",
	}
	for i := range r.Errors {
		insertErrorIntoTree(root, &r.Errors[i])
	}
	for i := range r.Annotations {
		insertAnnotationIntoTree(root, &r.Annotations[i])
	}
	return root
}

// insertErrorIntoTree adds e (and its nested causes) into root. The error
// is a leaf at its keywordLocation; missing prefix segments become groups.
func insertErrorIntoTree(root *outputNode, e *ValidationError) {
	leafLoc := normalizeKeywordLocation(e.KeywordLocation)
	leaf := &outputNode{
		valid:                   false,
		keywordLocation:         leafLoc,
		absoluteKeywordLocation: e.AbsoluteKeywordLocation,
		instanceLocation:        e.InstanceLocation,
		error:                   errorMessage(e),
	}
	for i := range e.Causes {
		insertErrorIntoTree(leaf, &e.Causes[i])
	}
	parent := ensureParentForPath(root, leafLoc, e.InstanceLocation)
	parent.errors = append(parent.errors, leaf)
}

// insertAnnotationIntoTree adds an Annotation as a leaf at its
// keywordLocation, creating prefix groups along the way.
func insertAnnotationIntoTree(root *outputNode, a *Annotation) {
	leafLoc := normalizeKeywordLocation(a.KeywordLocation)
	leaf := &outputNode{
		valid:                   true,
		keywordLocation:         leafLoc,
		absoluteKeywordLocation: a.AbsoluteKeywordLocation,
		instanceLocation:        a.InstanceLocation,
		annotation:              sanitizeAnnotation(a.Value),
		hasAnnotation:           true,
	}
	parent := ensureParentForPath(root, leafLoc, a.InstanceLocation)
	parent.annotations = append(parent.annotations, leaf)
}

// ensureParentForPath walks root creating group nodes for each prefix of
// keywordLocation and returns the parent node that the leaf at
// keywordLocation should attach to.
func ensureParentForPath(root *outputNode, keywordLocation, instanceLocation string) *outputNode {
	parentLoc := parentPointer(keywordLocation)
	if parentLoc == keywordLocation || parentLoc == "" {
		return root
	}
	segments := splitPointer(parentLoc)
	cur := root
	var prefixBuf strings.Builder
	for _, seg := range segments {
		prefixBuf.WriteByte('/')
		prefixBuf.WriteString(seg)
		prefix := prefixBuf.String()
		var found *outputNode
		for _, child := range cur.errors {
			if child.keywordLocation == prefix && child.error == "" {
				found = child
				break
			}
		}
		if found == nil {
			for _, child := range cur.annotations {
				if child.keywordLocation == prefix && !child.hasAnnotation {
					found = child
					break
				}
			}
		}
		if found == nil {
			found = &outputNode{
				valid:            true,
				keywordLocation:  prefix,
				instanceLocation: instanceLocation,
			}
			// Attach to errors so pruneToFailing can find groups uniformly.
			cur.errors = append(cur.errors, found)
		}
		cur = found
	}
	return cur
}

// parentPointer returns the JSON Pointer parent of p. parentPointer("") and
// parentPointer("/minimum") both return "".
func parentPointer(p string) string {
	if p == "" {
		return ""
	}
	idx := strings.LastIndex(p, "/")
	if idx <= 0 {
		return ""
	}
	return p[:idx]
}

// splitPointer splits a non-empty JSON Pointer into segments and drops the
// leading empty segment from the leading slash.
func splitPointer(p string) []string {
	if p == "" {
		return nil
	}
	parts := strings.Split(p, "/")
	if len(parts) > 0 && parts[0] == "" {
		parts = parts[1:]
	}
	return parts
}

// pruneToFailing collapses any subtree whose every leaf is a passing
// annotation, matching the spec's Detailed format.
func pruneToFailing(node *outputNode) *outputNode {
	if node == nil {
		return nil
	}
	pruned := make([]*outputNode, 0, len(node.errors))
	for _, c := range node.errors {
		if c.error != "" {
			pruned = append(pruned, c)
			continue
		}
		pc := pruneToFailing(c)
		if pc != nil && (pc.error != "" || len(pc.errors) > 0) {
			pruned = append(pruned, pc)
		}
	}
	// Collapse: a group with exactly one failing child becomes the child
	// (matching the spec's "collapse passing parent" example).
	if node.error == "" && len(pruned) == 1 && node.keywordLocation != "" {
		child := pruned[0]
		if child.instanceLocation == "" {
			child.instanceLocation = node.instanceLocation
		}
		if len(node.annotations) == 0 && !node.hasAnnotation {
			out := *child
			return &out
		}
	}
	out := *node
	out.errors = pruned
	out.annotations = nil
	// Group nodes with failing children are themselves invalid.
	if len(pruned) > 0 && out.error == "" {
		out.valid = false
	}
	return &out
}

// pruneToValid keeps only the annotation tree (used when Valid=true).
func pruneToValid(node *outputNode) *outputNode {
	if node == nil {
		return nil
	}
	prunedAnno := make([]*outputNode, 0, len(node.annotations))
	for _, c := range node.annotations {
		if c.hasAnnotation {
			prunedAnno = append(prunedAnno, c)
			continue
		}
		pc := pruneToValid(c)
		if pc != nil && (pc.hasAnnotation || len(pc.annotations) > 0) {
			prunedAnno = append(prunedAnno, pc)
		}
	}
	// On a passing tree, the errors slice carries only group nodes;
	// recurse so annotations under a group are kept.
	prunedGroups := make([]*outputNode, 0, len(node.errors))
	for _, c := range node.errors {
		if c.error != "" {
			continue
		}
		pc := pruneToValid(c)
		if pc != nil && (pc.hasAnnotation || len(pc.annotations) > 0) {
			prunedGroups = append(prunedGroups, pc)
		}
	}
	out := *node
	out.annotations = prunedAnno
	out.errors = nil
	if len(prunedGroups) > 0 {
		out.annotations = append(out.annotations, prunedGroups...)
	}
	return &out
}

// nodeJSON is the on-wire shape of one tree node.
type nodeJSON struct {
	Valid                   bool        `json:"valid"`
	KeywordLocation         string      `json:"keywordLocation"`
	AbsoluteKeywordLocation string      `json:"absoluteKeywordLocation,omitempty"`
	InstanceLocation        string      `json:"instanceLocation"`
	Error                   string      `json:"error,omitempty"`
	Annotation              any         `json:"annotation,omitempty"`
	Errors                  []*nodeJSON `json:"errors,omitempty"`
	Annotations             []*nodeJSON `json:"annotations,omitempty"`
	hasAnnotation           bool        // internal: tracks explicit annotation
}

// MarshalJSON emits the nodeJSON shape, including "annotation" when the
// node carries an explicit annotation (so a value of false / nil survives
// rather than being dropped by omitempty).
func (n *nodeJSON) MarshalJSON() ([]byte, error) {
	type outShape struct {
		Valid                   bool        `json:"valid"`
		KeywordLocation         string      `json:"keywordLocation"`
		AbsoluteKeywordLocation string      `json:"absoluteKeywordLocation,omitempty"`
		InstanceLocation        string      `json:"instanceLocation"`
		Error                   string      `json:"error,omitempty"`
		Annotation              any         `json:"annotation,omitempty"`
		Errors                  []*nodeJSON `json:"errors,omitempty"`
		Annotations             []*nodeJSON `json:"annotations,omitempty"`
	}
	out := outShape{
		Valid:                   n.Valid,
		KeywordLocation:         n.KeywordLocation,
		AbsoluteKeywordLocation: n.AbsoluteKeywordLocation,
		InstanceLocation:        n.InstanceLocation,
		Error:                   n.Error,
		Errors:                  n.Errors,
		Annotations:             n.Annotations,
	}
	if n.hasAnnotation {
		out.Annotation = n.Annotation
	}
	return json.Marshal(out)
}

// nodeToJSON converts an outputNode tree to its JSON shape. The verbose
// flag is a placeholder so the renderDetailed / renderVerbose call sites
// remain symmetric; the structural difference lives in the pruning the
// caller performs before invoking this function.
func nodeToJSON(n *outputNode, verbose bool) *nodeJSON { //nolint:unparam // see doc
	if n == nil {
		return nil
	}
	j := &nodeJSON{
		Valid:                   n.valid,
		KeywordLocation:         n.keywordLocation,
		AbsoluteKeywordLocation: n.absoluteKeywordLocation,
		InstanceLocation:        n.instanceLocation,
		Error:                   n.error,
		hasAnnotation:           n.hasAnnotation,
	}
	if n.hasAnnotation {
		j.Annotation = n.annotation
	}
	for _, c := range n.errors {
		cj := nodeToJSON(c, verbose)
		if cj == nil {
			continue
		}
		j.Errors = append(j.Errors, cj)
	}
	for _, c := range n.annotations {
		cj := nodeToJSON(c, verbose)
		if cj == nil {
			continue
		}
		j.Annotations = append(j.Annotations, cj)
	}
	// Stable order for reproducible output.
	sort.SliceStable(j.Errors, func(i, k int) bool {
		return j.Errors[i].KeywordLocation < j.Errors[k].KeywordLocation
	})
	sort.SliceStable(j.Annotations, func(i, k int) bool {
		return j.Annotations[i].KeywordLocation < j.Annotations[k].KeywordLocation
	})
	return j
}

// marshalCompact JSON-encodes v with HTML-escaping disabled (so embedded
// regex characters survive) and trims the trailing newline that
// json.Encoder adds.
func marshalCompact(v any) []byte {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		b, err := json.Marshal(v)
		if err != nil {
			return []byte(`{"valid":false}`)
		}
		return b
	}
	out := buf.Bytes()
	if len(out) > 0 && out[len(out)-1] == '\n' {
		out = out[:len(out)-1]
	}
	return out
}

// outputMetaSchema lazily compiles the embedded Draft 2020-12 output
// meta-schema. Memoized via sync.OnceValue.
var outputMetaSchema = sync.OnceValue(func() *Schema {
	data, err := metaSchemaFS.ReadFile("meta/output-2020-12.json")
	if err != nil {
		return nil
	}
	c := NewCompiler(WithLoader(embeddedMetaMapLoader()), WithDefaultDraft(Draft202012))
	s, err := c.Compile(data)
	if err != nil {
		return nil
	}
	return s
})

// OutputMetaSchema returns the compiled Draft 2020-12 output-format
// meta-schema (memoized; embedded). It returns nil only when the embedded
// bytes fail to compile, which would indicate a build-time mistake.
func OutputMetaSchema() *Schema {
	return outputMetaSchema()
}
