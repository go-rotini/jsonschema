package jsonschema

import (
	"strings"
	"testing"
)

// TestEdgeSelfLoopRef verifies that a top-level {"$ref": "#"} terminates
// (via WithMaxRefDepth) instead of recursing forever.
func TestEdgeSelfLoopRef(t *testing.T) {
	schema, err := Compile([]byte(`{"$ref":"#"}`))
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	res, err := schema.Validate([]byte(`null`))
	if err != nil {
		// Some configurations surface the depth limit as an error rather
		// than a result error; both are acceptable.
		if !strings.Contains(err.Error(), "max ref depth") {
			t.Fatalf("validate: %v", err)
		}
		return
	}
	if res == nil {
		t.Fatal("nil result")
	}
	// Either Valid or surfacing a max-ref-depth error is acceptable —
	// the important thing is termination.
	if res.Valid {
		return
	}
	found := false
	for _, e := range res.Errors {
		if strings.Contains(e.Message, "max ref depth") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected max-ref-depth error in result, got: %+v", res.Errors)
	}
}

// TestEdgeBillionLaughsRef builds a schema where each $defs entry references
// the next several times. The expansion would be exponential under naive
// inlining; with WithMaxRefDepth in place the validator must terminate
// cleanly.
func TestEdgeBillionLaughsRef(t *testing.T) {
	// Use a small fan-out per level so the test runs fast but still
	// would explode under naive inlining.
	const levels = 12
	var b strings.Builder
	b.WriteString(`{"$defs":{`)
	for i := 0; i <= levels; i++ {
		if i > 0 {
			b.WriteString(",")
		}
		if i == levels {
			// Leaf level: a plain string schema.
			writeFf(&b, `"l%d":{"type":"string"}`, i)
			continue
		}
		// Each level allOf's three references to the next level.
		writeFf(&b, `"l%d":{"allOf":[{"$ref":"#/$defs/l%d"},{"$ref":"#/$defs/l%d"},{"$ref":"#/$defs/l%d"}]}`, i, i+1, i+1, i+1)
	}
	b.WriteString(`},"$ref":"#/$defs/l0"}`)
	schema, err := Compile([]byte(b.String()), WithMaxRefDepth(50))
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	// Validate something that would force descent through the whole chain.
	res, err := schema.Validate([]byte(`"hi"`))
	if err != nil {
		// max-ref-depth errors arriving as Go errors are also acceptable.
		if strings.Contains(err.Error(), "max ref depth") {
			return
		}
		t.Fatalf("validate: %v", err)
	}
	if res == nil {
		t.Fatal("nil result")
	}
	// Either the validator terminated and produced a Valid:true result
	// (no recursion limit hit because validation is bounded) or produced a
	// max-ref-depth error. Both are acceptable; the only failure mode is
	// stack overflow / OOM, which the test guards against by having
	// terminated at all.
	_ = res
}

// TestEdgeMutuallyRecursiveAcrossIDs exercises two $id-bounded resources that
// reference one another via absolute refs, then validates an instance that
// alternates between them.
func TestEdgeMutuallyRecursiveAcrossIDs(t *testing.T) {
	schema, err := Compile([]byte(`{
		"$id": "https://example.com/root",
		"$schema": "https://json-schema.org/draft/2020-12/schema",
		"$defs": {
			"a": {
				"$id": "https://example.com/a",
				"type": "object",
				"properties": {"next": {"$ref": "https://example.com/b"}}
			},
			"b": {
				"$id": "https://example.com/b",
				"type": "object",
				"properties": {"next": {"$ref": "https://example.com/a"}}
			}
		},
		"$ref": "https://example.com/a"
	}`))
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	// Three-deep alternation a → b → a → leaf.
	res, err := schema.Validate([]byte(`{"next":{"next":{"next":{}}}}`))
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	if !res.Valid {
		t.Fatalf("expected valid; got %+v", res.Errors)
	}
}

// TestEdgePlainNameAnchor exercises plain-name fragment refs (#name) that
// land on a $anchor declaration in a nested $defs entry.
func TestEdgePlainNameAnchor(t *testing.T) {
	schema, err := Compile([]byte(`{
		"$schema": "https://json-schema.org/draft/2020-12/schema",
		"$id": "https://example.com/anchored",
		"$defs": {
			"named": {
				"$anchor": "myThing",
				"type": "string",
				"minLength": 2
			}
		},
		"$ref": "#myThing"
	}`))
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	res, err := schema.Validate([]byte(`"ok"`))
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	if !res.Valid {
		t.Fatalf("expected valid; got errors: %+v", res.Errors)
	}
	res, err = schema.Validate([]byte(`"x"`))
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	if res.Valid {
		t.Fatal("expected anchor-targeted minLength=2 to reject single-char string")
	}
}

// TestEdgeRecursiveDynamicRef exercises a $dynamicRef that recurses through
// itself across nested instance values. The canonical use case is a tree
// schema where each node carries a list of children referencing the same
// schema. With WithMaxRefDepth and instance-bounded recursion, the walk
// must terminate.
func TestEdgeRecursiveDynamicRef(t *testing.T) {
	schema, err := Compile([]byte(`{
		"$schema": "https://json-schema.org/draft/2020-12/schema",
		"$id": "https://example.com/tree",
		"$dynamicAnchor": "node",
		"type": "object",
		"properties": {
			"value": {"type": "string"},
			"children": {
				"type": "array",
				"items": {"$dynamicRef": "#node"}
			}
		}
	}`))
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	// Three-level tree.
	tree := []byte(`{"value":"root","children":[{"value":"a","children":[]},{"value":"b","children":[{"value":"b1","children":[]}]}]}`)
	res, err := schema.Validate(tree)
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	if !res.Valid {
		t.Fatalf("expected valid; got errors: %+v", res.Errors)
	}
}

// writeFf is a tiny fmt.Fprintf helper that writes into a strings.Builder
// without forcing the test file to depend on the stdlib fmt package only
// for one call site. It supports %d and %s.
func writeFf(b *strings.Builder, format string, args ...any) {
	ai := 0
	for i := 0; i < len(format); i++ {
		c := format[i]
		if c != '%' || i+1 >= len(format) {
			b.WriteByte(c)
			continue
		}
		i++
		switch format[i] {
		case 'd':
			b.WriteString(itoaForTest(args[ai].(int)))
			ai++
		case 's':
			b.WriteString(args[ai].(string))
			ai++
		default:
			b.WriteByte('%')
			b.WriteByte(format[i])
		}
	}
}

// itoaForTest converts an int to its decimal string form. The package
// already exports an internal `itoa` in compile.go but using it from a
// test file would couple the test to internal helpers; this is a tiny
// duplicate that keeps edge_test.go self-contained.
func itoaForTest(i int) string {
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
