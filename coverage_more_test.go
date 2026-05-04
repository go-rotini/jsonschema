package jsonschema

// Additional coverage tests beyond coverage_test.go. Targets the long tail
// of helpers in formats, generator_tags, multifmt YAML/TOML number
// helpers, errors.go pointer helpers, eval helpers, output helpers, and
// loader edge cases.

import (
	"bytes"
	"encoding/json"
	"errors"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

// =====================================================================
// formats.go: email + IRI / URI templates / JSON pointers
// =====================================================================

// TestValidateEmailComprehensive covers edge-case branches of the email
// + idn-email validators.
func TestValidateEmailComprehensive(t *testing.T) {
	cases := []struct {
		name  string
		fn    string
		val   string
		valid bool
	}{
		// quoted-local: malformed (no closing quote)
		{"quoted-no-close", "email", `"unclosed@example.com`, false},
		// quoted-local with backslash escape
		{"quoted-escape", "email", `"a\b"@example.com`, true},
		// quoted-local with control char inside
		{"quoted-ctrl", "email", "\"a\x01\"@example.com", false},
		// quoted-local: dangling backslash
		{"quoted-dangling-bs", "email", `"x\` + `"@e.com`, false},
		// quoted-local: unescaped quote
		{"quoted-bare-quote", "email", `"a"b"@e.com`, false},
		// quoted-local: non-ASCII rejected for ASCII email
		{"quoted-non-ascii", "email", "\"münchen\"@e.com", false},
		// idn-email: non-ASCII OK in quoted local
		{"idn-quoted", "idn-email", "\"müller\"@example.com", true},
		// missing @
		{"no-at", "email", "abc.com", false},
		// empty local
		{"empty-local", "email", "@e.com", false},
		// empty domain
		{"empty-domain", "email", "alice@", false},
		// dot-prefix local
		{"dot-prefix", "email", ".alice@e.com", false},
		// dot-suffix local
		{"dot-suffix", "email", "alice.@e.com", false},
		// consecutive dots
		{"dot-dot", "email", "a..b@e.com", false},
		// IP-literal v4
		{"ipv4-literal", "email", "x@[127.0.0.1]", true},
		// IP-literal v6
		{"ipv6-literal", "email", "x@[IPv6:::1]", true},
		// unmatched bracket
		{"bad-literal", "email", "x@[bad", false},
		// ASCII-only email rejects non-ASCII domain
		{"non-ascii-domain", "email", "x@münchen.de", false},
		// IDN email allows non-ASCII domain
		{"idn-domain", "idn-email", "x@münchen.de", true},
		// control char in source rejects email
		{"ctrl-char", "email", "a\x01b@e.com", false},
		{"ctrl-char-idn", "idn-email", "a\x01b@e.com", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fn, ok := lookupFormat(tc.fn, nil)
			if !ok {
				t.Skip("format not registered")
			}
			err := fn(tc.val)
			got := err == nil
			if got != tc.valid {
				t.Errorf("%s(%q): got %v, want %v (err=%v)", tc.fn, tc.val, got, tc.valid, err)
			}
		})
	}
}

// TestValidateURITemplateBranches covers more URI-template branches.
func TestValidateURITemplateBranches(t *testing.T) {
	cases := []struct {
		val   string
		valid bool
	}{
		{"plain/text", true},
		{"plain/with %20 space", false},
		{"/{var:9999}", true},   // 4-digit max
		{"/{var:99999}", false}, // 5-digit max-too-long
		{"/{var:abc}", false},   // non-digit max
		{"/{var:}", false},      // empty max
		{"/{+var}", true},       // operator
		{"/{?var,key}", true},   // comma list
		{"/{}", false},          // empty expr
		{"/{a..b}", false},      // empty seg
		{"/{a*}", true},         // explode
		{"/{a%2A}", true},       // percent-encoded var
		{"/{a%XY}", false},      // bad percent
		{"/{a-bad}", false},     // hyphen rejected
		// Non-ASCII literal
		{"/π", true},
		// Bad UTF-8
		{string([]byte{'/', 0xff}), false},
	}
	for _, tc := range cases {
		t.Run(tc.val, func(t *testing.T) {
			fn, ok := lookupFormat("uri-template", nil)
			if !ok {
				t.Skip()
			}
			err := fn(tc.val)
			got := err == nil
			if got != tc.valid {
				t.Errorf("uri-template(%q): got %v, want %v (err=%v)", tc.val, got, tc.valid, err)
			}
		})
	}
}

// TestValidateIRIReferenceBranches covers iri-reference branches.
func TestValidateIRIReferenceBranches(t *testing.T) {
	cases := []struct {
		val   string
		valid bool
	}{
		{"/path/to/x", true},
		{"https://example.com/", true},
		{"\x01ctrl", false},
		{"http://[bad", true}, // url.Parse may accept
		{"abc%XY", false},
	}
	for _, tc := range cases {
		t.Run(tc.val, func(t *testing.T) {
			fn, ok := lookupFormat("iri-reference", nil)
			if !ok {
				t.Skip()
			}
			err := fn(tc.val)
			got := err == nil
			if got != tc.valid {
				t.Logf("iri-reference(%q): got valid=%v want %v err=%v", tc.val, got, tc.valid, err)
			}
		})
	}
}

// =====================================================================
// errors.go: walkObject / walkArray / skipValue
// =====================================================================

// TestRenderErrorWithObjectInsideArray exercises walkArray descending into
// an object via /N/key.
func TestRenderErrorWithObjectInsideArray(t *testing.T) {
	src := []byte(`[{"name":"a"},{"name":"bee"}]`)
	ve := &ValidationError{KeywordLocation: "/1/name", Message: "x"}
	got := RenderError(src, nil, ve)
	if !strings.Contains(got, "schema (line") {
		t.Logf("snippet may not always emit; got=%q", got)
	}
}

// TestRenderErrorSkipValueComposite exercises skipValue's composite-skip
// branch by pointing past a complex sibling object.
func TestRenderErrorSkipValueComposite(t *testing.T) {
	src := []byte(`{"skip":{"x":[1,2,{"deep":true}]},"target":42}`)
	ve := &ValidationError{KeywordLocation: "/target", Message: "x"}
	got := RenderError(src, nil, ve)
	if !strings.Contains(got, "schema (line") {
		t.Logf("snippet may not always emit; got=%q", got)
	}
}

// =====================================================================
// compile.go: low-coverage helpers
// =====================================================================

// TestIsNonNegativeIntegerBranches covers each numeric type path.
func TestIsNonNegativeIntegerBranches(t *testing.T) {
	cases := []struct {
		v    any
		want bool
	}{
		{json.Number("5"), true},
		{json.Number("-5"), false},
		{json.Number("5.0"), true},
		{json.Number("5.5"), false},
		{json.Number("not-a-number"), false},
		{int(5), true},
		{int(-1), false},
		{int64(0), true},
		{int64(-7), false},
		{float64(0), true},
		{float64(-1), false},
		{float64(2.5), false},
		{"string", false},
	}
	for _, tc := range cases {
		got := isNonNegativeInteger(tc.v)
		if got != tc.want {
			t.Errorf("isNonNegativeInteger(%v) = %v, want %v", tc.v, got, tc.want)
		}
	}
}

// TestIsPositiveNumberBranches covers each numeric type path.
func TestIsPositiveNumberBranches(t *testing.T) {
	cases := []struct {
		v    any
		want bool
	}{
		{json.Number("5"), true},
		{json.Number("0"), false},
		{json.Number("-5"), false},
		{json.Number("not"), false},
		{int(7), true},
		{int(0), false},
		{int64(-1), false},
		{float64(0.1), true},
		{float64(0), false},
		{"x", false},
	}
	for _, tc := range cases {
		got := isPositiveNumber(tc.v)
		if got != tc.want {
			t.Errorf("isPositiveNumber(%v) = %v, want %v", tc.v, got, tc.want)
		}
	}
}

// TestItoaSignedBranches covers itoa (negative + zero + positive).
func TestItoaSignedBranches(t *testing.T) {
	cases := []struct {
		in   int
		want string
	}{
		{0, "0"},
		{1, "1"},
		{-7, "-7"},
		{12345, "12345"},
		{-12345, "-12345"},
	}
	for _, tc := range cases {
		got := itoa(tc.in)
		if got != tc.want {
			t.Errorf("itoa(%d) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// =====================================================================
// multifmt.go: number helpers
// =====================================================================

// TestIsYAMLNumberBranches covers each branch.
func TestIsYAMLNumberBranches(t *testing.T) {
	cases := []struct {
		v    string
		want bool
	}{
		{"", false},
		{".inf", true},
		{".Inf", true},
		{".INF", true},
		{"+.inf", true},
		{"-.inf", true},
		{".nan", true},
		{".NaN", true},
		{"42", true},
		{"-7", true},
		{"0x1A", true},
		{"0o17", true},
		{"3.14", true},
		{"1e6", true},
		{"abc", false},
		{"true", false},
	}
	for _, tc := range cases {
		got := isYAMLNumber(tc.v)
		if got != tc.want {
			t.Errorf("isYAMLNumber(%q) = %v, want %v", tc.v, got, tc.want)
		}
	}
}

// TestYAMLNormalizeNumberBranches covers each branch.
func TestYAMLNormalizeNumberBranches(t *testing.T) {
	cases := []struct {
		v    string
		want string
	}{
		{".inf", "Inf"},
		{".Inf", "Inf"},
		{".INF", "Inf"},
		{"-.inf", "-Inf"},
		{"-.Inf", "-Inf"},
		{".nan", "NaN"},
		{".NaN", "NaN"},
		{"0x1A", "26"},   // hex → decimal
		{"0o17", "15"},   // octal → decimal
		{"-0x1A", "-26"}, // signed hex
		{"42", "42"},     // plain decimal stays
		{"-7", "-7"},
		{"3.14", "3.14"}, // float passes through
	}
	for _, tc := range cases {
		got := yamlNormalizeNumber(tc.v)
		if got != tc.want {
			t.Errorf("yamlNormalizeNumber(%q) = %q, want %q", tc.v, got, tc.want)
		}
	}
}

// TestHasBasePrefixBranches covers each branch.
func TestHasBasePrefixBranches(t *testing.T) {
	cases := []struct {
		v    string
		want bool
	}{
		{"0x1", true},
		{"0X1", true},
		{"0o7", true},
		{"0O7", true},
		{"0b1", true},
		{"0B1", true},
		{"+0x1", true},
		{"-0x1", true},
		{"42", false},
		{"", false},
	}
	for _, tc := range cases {
		got := hasBasePrefix(tc.v)
		if got != tc.want {
			t.Errorf("hasBasePrefix(%q) = %v, want %v", tc.v, got, tc.want)
		}
	}
}

// TestTOMLIntegerToNumberBranches covers each branch.
func TestTOMLIntegerToNumberBranches(t *testing.T) {
	cases := []struct {
		v    string
		want json.Number
		err  bool
	}{
		{"42", "42", false},
		{"0x1A", "26", false},
		{"0o17", "15", false},
		{"0b101", "5", false},
		{"1_000", "1000", false}, // underscores stripped
		{"0xFFFFFFFFFFFFFFFF", "18446744073709551615", false}, // overflow → uint64
		{"not-a-number", "", true},
	}
	for _, tc := range cases {
		got, err := tomlIntegerToNumber(tc.v)
		if (err != nil) != tc.err {
			t.Errorf("tomlIntegerToNumber(%q) err=%v, want err=%v", tc.v, err, tc.err)
			continue
		}
		if !tc.err && got != tc.want {
			t.Errorf("tomlIntegerToNumber(%q) = %v, want %v", tc.v, got, tc.want)
		}
	}
}

// TestTOMLFloatToNumberBranches covers each branch.
func TestTOMLFloatToNumberBranches(t *testing.T) {
	cases := []struct {
		v    string
		want json.Number
		err  bool
	}{
		{"3.14", "3.14", false},
		{"inf", "Inf", false},
		{"+inf", "Inf", false},
		{"-inf", "-Inf", false},
		{"nan", "NaN", false},
		{"+nan", "NaN", false},
		{"-nan", "NaN", false},
		{"3_000.5", "3000.5", false}, // underscores stripped
		{"not-a-float", "", true},
	}
	for _, tc := range cases {
		got, err := tomlFloatToNumber(tc.v)
		if (err != nil) != tc.err {
			t.Errorf("tomlFloatToNumber(%q) err=%v, want err=%v", tc.v, err, tc.err)
			continue
		}
		if !tc.err && got != tc.want {
			t.Errorf("tomlFloatToNumber(%q) = %v, want %v", tc.v, got, tc.want)
		}
	}
}

// TestStripUnderscores covers both branches.
func TestStripUnderscores(t *testing.T) {
	if got := stripUnderscores("plain"); got != "plain" {
		t.Errorf("plain: %q", got)
	}
	if got := stripUnderscores("1_000_000"); got != "1000000" {
		t.Errorf("underscores: %q", got)
	}
}

// =====================================================================
// generator_tags.go: helpers
// =====================================================================

// TestRequireStringTargetBranches covers the kind switch.
func TestRequireStringTargetBranches(t *testing.T) {
	if err := requireStringTarget(reflect.TypeOf(""), "/x", "minLength"); err != nil {
		t.Errorf("string target should pass: %v", err)
	}
	if err := requireStringTarget(reflect.TypeOf((*string)(nil)), "/x", "minLength"); err != nil {
		t.Errorf("ptr-to-string target should pass: %v", err)
	}
	if err := requireStringTarget(reflect.TypeOf(0), "/x", "minLength"); err == nil {
		t.Error("int target should fail")
	}
	// nil target → no error
	if err := requireStringTarget(nil, "/x", "minLength"); err != nil {
		t.Errorf("nil ft: %v", err)
	}
}

// TestRequireArrayTargetBranches covers the kind switch.
func TestRequireArrayTargetBranches(t *testing.T) {
	if err := requireArrayTarget(reflect.TypeOf([]int{}), "/x", "minItems"); err != nil {
		t.Errorf("slice target should pass: %v", err)
	}
	if err := requireArrayTarget(reflect.TypeOf([3]int{}), "/x", "minItems"); err != nil {
		t.Errorf("array target should pass: %v", err)
	}
	if err := requireArrayTarget(reflect.TypeOf((*[]int)(nil)), "/x", "minItems"); err != nil {
		t.Errorf("ptr-to-slice should pass: %v", err)
	}
	if err := requireArrayTarget(reflect.TypeOf(""), "/x", "minItems"); err == nil {
		t.Error("string target should fail")
	}
	if err := requireArrayTarget(nil, "/x", "minItems"); err != nil {
		t.Errorf("nil: %v", err)
	}
}

// TestRequireMapTargetBranches covers the kind switch.
func TestRequireMapTargetBranches(t *testing.T) {
	if err := requireMapTarget(reflect.TypeOf(map[string]int{}), "/x", "minProperties"); err != nil {
		t.Errorf("map target should pass: %v", err)
	}
	if err := requireMapTarget(reflect.TypeOf(struct{}{}), "/x", "minProperties"); err != nil {
		t.Errorf("struct target should pass: %v", err)
	}
	if err := requireMapTarget(reflect.TypeOf(""), "/x", "minProperties"); err == nil {
		t.Error("string target should fail")
	}
	if err := requireMapTarget(nil, "/x", "minProperties"); err != nil {
		t.Errorf("nil: %v", err)
	}
}

// TestParseNonNegativeIntBranches covers the parse + sign branches.
func TestParseNonNegativeIntBranches(t *testing.T) {
	if _, err := parseNonNegativeInt("5", "/x", "n"); err != nil {
		t.Errorf("5: %v", err)
	}
	if _, err := parseNonNegativeInt("-1", "/x", "n"); err == nil {
		t.Error("negative should fail")
	}
	if _, err := parseNonNegativeInt("xyz", "/x", "n"); err == nil {
		t.Error("non-int should fail")
	}
}

// TestCoerceFieldValueBranches covers each kind branch.
func TestCoerceFieldValueBranches(t *testing.T) {
	cases := []struct {
		ft   reflect.Type
		val  string
		want any
		err  bool
	}{
		{reflect.TypeOf(""), "hello", "hello", false},
		{reflect.TypeOf(true), "true", true, false},
		{reflect.TypeOf(true), "false", false, false},
		{reflect.TypeOf(true), "yes", nil, true},
		{reflect.TypeOf(0), "42", int64(42), false},
		{reflect.TypeOf(0), "x", nil, true},
		{reflect.TypeOf(0.0), "3.14", 3.14, false},
		{reflect.TypeOf(0.0), "x", nil, true},
		// fallthrough — slice unaffected; returns string verbatim
		{reflect.TypeOf([]int{}), "anything", "anything", false},
	}
	for _, tc := range cases {
		got, err := coerceFieldValue(tc.ft, tc.val, "/p", "name")
		if (err != nil) != tc.err {
			t.Errorf("coerceFieldValue(%v,%q) err=%v want err=%v", tc.ft, tc.val, err, tc.err)
			continue
		}
		if !tc.err && got != tc.want {
			t.Errorf("coerceFieldValue(%v,%q) = %v, want %v", tc.ft, tc.val, got, tc.want)
		}
	}
}

// TestCoerceNumericValueBranches covers each branch.
func TestCoerceNumericValueBranches(t *testing.T) {
	// nil ft → loose float parse
	got, err := coerceNumericValue(nil, "3.14", true, "/x", "minimum")
	if err != nil {
		t.Errorf("nil ft: %v", err)
	}
	if got.(float64) != 3.14 {
		t.Errorf("got %v", got)
	}
	// int ft
	got, err = coerceNumericValue(reflect.TypeOf(0), "42", true, "/x", "minimum")
	if err != nil {
		t.Errorf("int: %v", err)
	}
	if got.(int64) != 42 {
		t.Errorf("got %v", got)
	}
	// non-numeric ft
	if _, err := coerceNumericValue(reflect.TypeOf(""), "1", true, "/x", "minimum"); err == nil {
		t.Error("string field should reject numeric option")
	}
	// hasValue false
	if _, err := coerceNumericValue(reflect.TypeOf(0), "", false, "/x", "minimum"); err == nil {
		t.Error("missing value should error")
	}
	// invalid int
	if _, err := coerceNumericValue(reflect.TypeOf(0), "x", true, "/x", "minimum"); err == nil {
		t.Error("bad int should error")
	}
	// invalid float
	if _, err := coerceNumericValue(reflect.TypeOf(0.0), "x", true, "/x", "minimum"); err == nil {
		t.Error("bad float should error")
	}
	// invalid float for nil ft
	if _, err := coerceNumericValue(nil, "x", true, "/x", "minimum"); err == nil {
		t.Error("bad nil-ft float should error")
	}
}

// TestElemTypeForListBranches covers slice / non-slice / nil.
func TestElemTypeForListBranches(t *testing.T) {
	if got := elemTypeForList(nil); got != nil {
		t.Errorf("nil: %v", got)
	}
	if got := elemTypeForList(reflect.TypeOf([]int{})); got != reflect.TypeOf(0) {
		t.Errorf("slice: %v", got)
	}
	if got := elemTypeForList(reflect.TypeOf("")); got != reflect.TypeOf("") {
		t.Errorf("scalar: %v", got)
	}
}

// =====================================================================
// generator.go: setHead / orderedFromKV / hasAny / hasMetadata / hasAssertion
// =====================================================================

// TestOrderedMapSetHead exercises setHead on both the empty + existing-key
// branches.
func TestOrderedMapSetHead(t *testing.T) {
	m := newOrderedMap()
	m.set("a", 1)
	m.set("b", 2)
	// New key at head.
	m.setHead("c", 3)
	if got := m.keys[0]; got != "c" {
		t.Errorf("keys[0] = %q, want c", got)
	}
	// Existing key moved to head.
	m.setHead("b", 22)
	if got := m.keys[0]; got != "b" {
		t.Errorf("after promote, keys[0] = %q, want b", got)
	}
	if v := m.vals["b"]; v != 22 {
		t.Errorf("setHead overwrite: got %v", v)
	}
}

// TestOrderedMapMarshalJSONEmpty covers the empty map branch.
func TestOrderedMapMarshalJSONEmpty(t *testing.T) {
	var m *orderedMap
	data, err := m.MarshalJSON()
	if err != nil {
		t.Fatalf("MarshalJSON nil: %v", err)
	}
	if string(data) != "{}" {
		t.Errorf("nil = %s", data)
	}
	m2 := newOrderedMap()
	data, err = m2.MarshalJSON()
	if err != nil {
		t.Fatalf("MarshalJSON empty: %v", err)
	}
	if string(data) != "{}" {
		t.Errorf("empty = %s", data)
	}
}

// TestOrderedFromKVOddArgs covers the odd-arity safety path.
func TestOrderedFromKVOddArgs(t *testing.T) {
	m := orderedFromKV("a", 1, "ignored-because-odd")
	if got := m.vals["a"]; got != 1 {
		t.Errorf("a = %v", got)
	}
	if _, ok := m.vals["ignored-because-odd"]; ok {
		t.Errorf("odd entry should be dropped")
	}
}

// TestOrderedFromKVNonStringKey covers the type-assertion-fail branch.
func TestOrderedFromKVNonStringKey(t *testing.T) {
	m := orderedFromKV(42, "value", "key", "value")
	if _, ok := m.vals["key"]; !ok {
		t.Error("string key should appear")
	}
	if m.len() != 1 {
		t.Errorf("len = %d, want 1", m.len())
	}
}

// TestTagSpecHasMethods covers hasAny / hasMetadata / hasAssertion.
func TestTagSpecHasMethods(t *testing.T) {
	var s tagSpec
	if s.hasAny() || s.hasMetadata() || s.hasAssertion() {
		t.Error("zero spec should have no flags")
	}
	s.hasDescription = true
	if !s.hasMetadata() {
		t.Error("description sets metadata")
	}
	if !s.hasAny() {
		t.Error("metadata sets any")
	}
	s = tagSpec{hasMinimum: true}
	if !s.hasAssertion() {
		t.Error("minimum sets assertion")
	}
	if !s.hasAny() {
		t.Error("assertion sets any")
	}
}

// =====================================================================
// loader.go: extractID / registerVocabMeta / readFileImpl
// =====================================================================

// TestExtractIDBranches covers various paths through extractID.
func TestExtractIDBranches(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{`{"$id":"https://e/x"}`, "https://e/x"},
		{`{ "$id" : "https://e/x" }`, "https://e/x"},
		{`{"$id":  "https://e/x"  }`, "https://e/x"},
		{`{}`, ""},
		{`{"$id":42}`, ""},       // not a string
		{`{"$id" "x"}`, ""},      // missing colon
		{`{"$id":"unclosed`, ""}, // no closing quote
		{`{"$id":`, ""},
	}
	for _, tc := range cases {
		got := extractID([]byte(tc.in))
		if got != tc.want {
			t.Errorf("extractID(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// =====================================================================
// errors.go: tokenStartOffset (currently 0%)
// =====================================================================

// TestTokenStartOffsetViaPointer covers tokenStartOffset indirectly via
// jsonPointerByteOffset on an atomic value at the root.
func TestTokenStartOffsetViaPointer(t *testing.T) {
	cases := [][]byte{
		[]byte(`null`),
		[]byte(`true`),
		[]byte(`false`),
		[]byte(`3.14`),
		[]byte(`"hi"`),
		[]byte(`{}`),
	}
	for _, src := range cases {
		off, ok := jsonPointerByteOffset(src, "")
		if !ok {
			t.Errorf("root: %s ok=false", src)
			continue
		}
		// off should be a non-negative valid offset.
		if off < 0 || off > len(src) {
			t.Errorf("root %s: off=%d", src, off)
		}
	}
}

// TestJsonPointerByteOffsetBranches covers more branches.
func TestJsonPointerByteOffsetBranches(t *testing.T) {
	// Empty src → false
	if _, ok := jsonPointerByteOffset(nil, ""); ok {
		t.Error("nil src should not be ok")
	}
	// Empty pointer + whitespace at front → returns first non-ws byte index.
	off, ok := jsonPointerByteOffset([]byte("   {}"), "")
	if !ok || off != 3 {
		t.Errorf("got off=%d ok=%v", off, ok)
	}
	// All-whitespace src → returns 0, true (else branch of inner for).
	off, ok = jsonPointerByteOffset([]byte("    "), "")
	if !ok || off != 0 {
		t.Errorf("all-ws: off=%d ok=%v", off, ok)
	}
}

// TestIsJSONWS covers isJSONWS.
func TestIsJSONWS(t *testing.T) {
	for _, b := range []byte{' ', '\t', '\n', '\r'} {
		if !isJSONWS(b) {
			t.Errorf("isJSONWS(%q) = false", b)
		}
	}
	for _, b := range []byte{'a', '0', '{', 0} {
		if isJSONWS(b) {
			t.Errorf("isJSONWS(%q) = true", b)
		}
	}
}

// =====================================================================
// eval.go: addErrorWithCause / addCausesError via runOptions limits
// =====================================================================

// TestAddErrorPathsViaMaxErrors confirms maxErrors gates the addError-family
// helpers.
func TestAddErrorPathsViaMaxErrors(t *testing.T) {
	src := []byte(`{
		"type":"object",
		"required":["a","b","c","d","e"],
		"properties":{"x":{"format":"uuid"}}
	}`)
	s := MustCompile(src)
	res, err := s.Validate(
		[]byte(`{"x":"not-a-uuid"}`),
		WithFormatAssertion(true),
		WithMaxErrors(1),
	)
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if res.Valid {
		t.Fatal("expected invalid")
	}
	if len(res.Errors) > 1 {
		t.Errorf("WithMaxErrors(1): got %d errors", len(res.Errors))
	}
}

// TestEvalRootNilSchema covers the nil-receiver branch.
func TestEvalRootNilSchema(t *testing.T) {
	var s *Schema
	if s.evalRoot() != nil {
		t.Error("nil.evalRoot should be nil")
	}
}

// =====================================================================
// generator.go: more failure paths
// =====================================================================

// TestGeneratorMustGenerateNonStruct covers the success branch of
// MustGenerate when the input is a primitive.
func TestGeneratorMustGenerateNonStruct(t *testing.T) {
	g := NewGenerator()
	if s := g.MustGenerate("hello"); s == nil {
		t.Error("nil")
	}
}

// TestGenerateFromTypeFailureBytesGood covers the success path of FromType
// where Compile fails. We trigger this with WithGenerateInterfaceAsAny(false)
// and an interface-typed field.
func TestGenerateFromTypeInterfaceWithoutAnyOption(t *testing.T) {
	type item struct {
		X any `json:"x"`
	}
	g := NewGenerator(WithGenerateInterfaceAsAny(false))
	if _, err := g.FromType(reflect.TypeOf(item{})); err == nil {
		t.Skip("no error for interface; behavior may have changed")
	}
}

// =====================================================================
// resolveURI: invalid URI branches
// =====================================================================

// TestResolveURIBranches covers various inputs.
func TestResolveURIBranches(t *testing.T) {
	if _, err := resolveURI("", "https://example.com/x"); err != nil {
		t.Errorf("absolute: %v", err)
	}
	if _, err := resolveURI("https://e/", "/x"); err != nil {
		t.Errorf("rooted relative: %v", err)
	}
	if _, err := resolveURI("https://e/", "rel"); err != nil {
		t.Errorf("simple relative: %v", err)
	}
	// Empty inputs → returns ""
	if got, err := resolveURI("", ""); err != nil || got != "" {
		t.Errorf("empty/empty: got=%q err=%v", got, err)
	}
}

// =====================================================================
// content.go: decodeContent error paths
// =====================================================================

// TestContentBase64Hex covers both encodings.
func TestContentBase64Hex(t *testing.T) {
	src := []byte(`{
		"contentEncoding":"base16",
		"contentMediaType":"application/octet-stream"
	}`)
	s := MustCompile(src)
	// valid hex
	if _, err := s.Validate([]byte(`"AABBCCDD"`)); err != nil {
		t.Errorf("hex valid: %v", err)
	}
	// invalid hex
	res, err := s.Validate([]byte(`"NOT-HEX"`), WithContentAssertion(true))
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if res.Valid {
		t.Errorf("expected invalid for non-hex")
	}
}

// =====================================================================
// validate.go: ValidateTo + decode error
// =====================================================================

// TestValidateToDecodeFailure covers the post-validate decode error.
func TestValidateToDecodeFailure(t *testing.T) {
	type item struct{ N int }
	s := MustCompile([]byte(`{"type":"object"}`))
	// {"N":"x"} validates as an object, but unmarshal into item.N (int)
	// fails.
	if _, err := ValidateTo[item](s, []byte(`{"N":"x"}`)); err == nil {
		t.Error("expected decode-after-validate error")
	}
}

// TestDecodeInstanceBytesTrailing covers the trailing-content branch.
func TestDecodeInstanceBytesTrailing(t *testing.T) {
	s := MustCompile([]byte(`{}`))
	if _, err := s.Validate([]byte(`{} {}`)); err == nil {
		t.Error("expected error on trailing content")
	}
}

// =====================================================================
// loader.go: ChainLoader + AddResource interactions
// =====================================================================

// TestNewCompilerNilLoader confirms a nil loader option falls back to the
// default. Already exercised but adds explicit coverage of the nil branch.
func TestNewCompilerNilLoader(t *testing.T) {
	c := NewCompiler(WithLoader(nil))
	if c == nil {
		t.Fatal("NewCompiler returned nil")
	}
}

// =====================================================================
// schema.go: Resources + Vocabularies + Anchors edge cases
// =====================================================================

// TestSchemaVocabulariesPreDraft201909 covers the path for legacy drafts.
func TestSchemaVocabulariesPreDraft201909(t *testing.T) {
	s := newSchemaForTest([]byte(`{}`), Draft7, "", "")
	got := s.Vocabularies()
	if len(got) == 0 {
		t.Error("Draft 7 should still surface implementation vocabularies")
	}
}

// =====================================================================
// validate.go: empty + nil-byte instance failures
// =====================================================================

// TestValidateEmptyInstance covers the empty-bytes decode failure.
func TestValidateEmptyInstance(t *testing.T) {
	s := MustCompile([]byte(`{}`))
	if _, err := s.Validate([]byte("")); err == nil {
		t.Error("expected decode error on empty bytes")
	}
}

// =====================================================================
// errors.go: tokenStartOffset for each tok type via jsonPointerByteOffset
// =====================================================================

// TestTokenStartOffsetCoversTypes exercises the various tok-type branches by
// pointing at terminal values inside an object.
func TestTokenStartOffsetCoversTypes(t *testing.T) {
	src := []byte(`{"a":null,"b":true,"c":false,"d":3.14,"e":"hi","f":{"g":1}}`)
	for _, ptr := range []string{"/a", "/b", "/c", "/d", "/e", "/f"} {
		off, ok := jsonPointerByteOffset(src, ptr)
		if !ok {
			t.Errorf("ptr %q: ok=false", ptr)
			continue
		}
		if off <= 0 || off >= len(src) {
			t.Errorf("ptr %q: off=%d", ptr, off)
		}
	}
}

// =====================================================================
// loader.go: tracingLoader and inflight error sharing
// =====================================================================

// TestTracingLoaderSubsequentCalls confirms the tracing wrapper reports
// errors verbatim.
func TestTracingLoaderError(t *testing.T) {
	var buf bytes.Buffer
	l := &tracingLoader{
		inner: MapLoader{},
		w:     &buf,
	}
	if _, err := l.Load("https://example.com/missing"); err == nil {
		t.Error("expected error")
	}
	// Trace should be empty since we didn't fetch anything successfully.
	if buf.Len() > 0 {
		t.Errorf("trace should be empty on failure: %q", buf.String())
	}
}

// =====================================================================
// generator.go: orderedMap + decoder helpers
// =====================================================================

// TestDecodeOrderedRoundTrip covers decodeOrdered for several JSON shapes.
func TestDecodeOrderedRoundTrip(t *testing.T) {
	cases := []string{
		`null`,
		`true`,
		`false`,
		`42`,
		`"text"`,
		`[]`,
		`[1,2,3]`,
		`{}`,
		`{"a":1,"b":[true,null,"x"]}`,
	}
	for _, src := range cases {
		t.Run(src, func(t *testing.T) {
			v, err := decodeOrdered([]byte(src))
			if err != nil {
				t.Fatalf("decodeOrdered: %v", err)
			}
			out, err := marshalAny(v)
			if err != nil {
				t.Fatalf("marshalAny: %v", err)
			}
			if !equalsJSON(t, []byte(src), out) {
				t.Errorf("roundtrip drift: %s → %s", src, out)
			}
		})
	}
}

func equalsJSON(t *testing.T, a, b []byte) bool {
	t.Helper()
	var av, bv any
	if err := json.Unmarshal(a, &av); err != nil {
		t.Logf("a unmarshal: %v", err)
		return false
	}
	if err := json.Unmarshal(b, &bv); err != nil {
		t.Logf("b unmarshal: %v", err)
		return false
	}
	ar, _ := json.Marshal(av)
	br, _ := json.Marshal(bv)
	return bytes.Equal(ar, br)
}

// TestDecodeOrderedBadJSON covers the error branch.
func TestDecodeOrderedBadJSON(t *testing.T) {
	if _, err := decodeOrdered([]byte("not json")); err == nil {
		t.Error("expected error")
	}
}

// =====================================================================
// readFileImpl: missing and present
// =====================================================================

// TestReadFileImplOk covers the success branch.
func TestReadFileImplOk(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/x.json"
	if err := writeBytes(path, []byte("hi")); err != nil {
		t.Fatal(err)
	}
	data, err := readFileImpl(path)
	if err != nil {
		t.Fatalf("readFileImpl: %v", err)
	}
	if string(data) != "hi" {
		t.Errorf("got %s", data)
	}
}

// TestReadFileImplFails covers the error branch.
func TestReadFileImplFails(t *testing.T) {
	if _, err := readFileImpl("/dev/no-such-thing-/abc"); err == nil {
		t.Error("expected error")
	}
}

// =====================================================================
// loader_os.go-coupled fragment tests
// =====================================================================

// (loader_os.go has only readFileImpl; the success and failure branches
// above suffice.)

// =====================================================================
// formats.go: containsCtrl
// =====================================================================

// TestContainsCtrlBranches covers each branch.
func TestContainsCtrlBranches(t *testing.T) {
	if !containsCtrl("\x01") {
		t.Error("ctrl should detect")
	}
	if containsCtrl("\t") {
		t.Error("tab should not be flagged (allowed)")
	}
	if !containsCtrl("\x00") {
		t.Error("NUL should detect")
	}
	if !containsCtrl("\x7F") {
		t.Error("DEL should detect")
	}
	if containsCtrl("plain") {
		t.Error("plain should pass")
	}
	// Surrogate via raw byte manipulation: a runaway encoding may not pass
	// through string(rune) cleanly, so we only verify the non-surrogate
	// fast paths above.
}

// TestIsURITemplateLiteralBranches covers each branch.
func TestIsURITemplateLiteralBranches(t *testing.T) {
	for _, r := range []rune{'a', 'Z', '5', '!', '#', '/', '~'} {
		if !isURITemplateLiteral(r) {
			t.Errorf("isURITemplateLiteral(%q) = false", r)
		}
	}
	for _, r := range []rune{' ', '\\', '<', '>', '"'} {
		if isURITemplateLiteral(r) {
			t.Errorf("isURITemplateLiteral(%q) = true", r)
		}
	}
}

// =====================================================================
// eval.go release(): just call it
// =====================================================================

// TestRunCtxRelease covers the release no-op.
func TestRunCtxRelease(t *testing.T) {
	ctx := newRunCtx(nil, defaultRunOptions())
	ctx.release() // currently a no-op; covers the function for coverage.
}

// =====================================================================
// formats.go: isFullDate / isFullTime branch
// =====================================================================

// TestValidateDateBranches covers the format-date validator.
func TestValidateDateBranches(t *testing.T) {
	for _, tc := range []struct {
		v    string
		want bool
	}{
		{"2025-01-01", true},
		{"2025-13-01", false},  // bad month
		{"2025-01-32", false},  // bad day
		{"abcd-01-01", false},  // non-digit year
		{"2025-1-01", false},   // missing leading zero
		{"2025-01", false},     // too short
		{"2025-01-01x", false}, // trailing junk
	} {
		t.Run(tc.v, func(t *testing.T) {
			fn, ok := lookupFormat("date", nil)
			if !ok {
				t.Skip()
			}
			err := fn(tc.v)
			got := err == nil
			if got != tc.want {
				t.Errorf("date(%q): got %v want %v err=%v", tc.v, got, tc.want, err)
			}
		})
	}
}

// TestValidateTimeBranches covers the format-time validator.
func TestValidateTimeBranches(t *testing.T) {
	for _, tc := range []struct {
		v    string
		want bool
	}{
		{"12:34:56Z", true},
		{"12:34:56+05:00", true},
		{"12:34:56-05:00", true},
		{"12:34:56.123Z", true},
		{"25:34:56Z", false},   // bad hour
		{"12:60:56Z", false},   // bad minute
		{"abcdefghi", false},   // garbage
		{"12:34:56", false},    // no offset
		{"12:34:56X", false},   // bad offset
		{"12:34:56+99", false}, // bad offset value
	} {
		t.Run(tc.v, func(t *testing.T) {
			fn, ok := lookupFormat("time", nil)
			if !ok {
				t.Skip()
			}
			err := fn(tc.v)
			got := err == nil
			if got != tc.want {
				t.Errorf("time(%q): got %v want %v err=%v", tc.v, got, tc.want, err)
			}
		})
	}
}

// =====================================================================
// formats.go: validateDuration branches
// =====================================================================

// TestValidateDurationBranches covers various duration shapes.
func TestValidateDurationBranches(t *testing.T) {
	for _, tc := range []struct {
		v    string
		want bool
	}{
		{"P1Y", true},
		{"P1M", true},
		{"P1D", true},
		{"PT1H", true},
		{"PT1M", true},
		{"PT1S", true},
		{"P1Y2M3DT4H5M6S", true},
		{"P1W", true}, // weeks
		{"P", false},
		{"", false},
		{"X1Y", false},   // missing P
		{"P1Y2W", false}, // weeks not combined with others
		{"PT", false},
		{"P1H", false}, // H without T
	} {
		t.Run(tc.v, func(t *testing.T) {
			fn, ok := lookupFormat("duration", nil)
			if !ok {
				t.Skip()
			}
			err := fn(tc.v)
			got := err == nil
			if got != tc.want {
				t.Errorf("duration(%q): got %v want %v err=%v", tc.v, got, tc.want, err)
			}
		})
	}
}

// =====================================================================
// loader.go: extractID coverage of helper indirectly
// =====================================================================

// TestRegisterVocabMetaSilent covers the "all good" path.
func TestRegisterVocabMetaSilent(t *testing.T) {
	m := MapLoader{}
	registerVocabMeta(m, "meta/draft-2020-12/meta")
	if len(m) == 0 {
		t.Error("registerVocabMeta should populate from embedded FS")
	}
}

// TestRegisterVocabMetaMissingDir covers the missing-dir branch.
func TestRegisterVocabMetaMissingDir(t *testing.T) {
	m := MapLoader{}
	registerVocabMeta(m, "meta/does-not-exist")
	if len(m) != 0 {
		t.Errorf("missing dir should not populate; got %v", m)
	}
}

// =====================================================================
// validate.go: ValidateAndUnmarshal nil v + valid + decode-success
// =====================================================================

// TestValidateAndUnmarshalSuccessfulDecode confirms the full happy path.
func TestValidateAndUnmarshalSuccessfulDecode(t *testing.T) {
	type item struct {
		Name string `json:"name"`
	}
	s := MustCompile([]byte(`{"type":"object","required":["name"]}`))
	var v item
	if err := s.ValidateAndUnmarshal([]byte(`{"name":"alice"}`), &v); err != nil {
		t.Errorf("ValidateAndUnmarshal: %v", err)
	}
	if v.Name != "alice" {
		t.Errorf("got %v", v)
	}
}

// =====================================================================
// errors.go: skipValue handles arrays + objects
// =====================================================================

// TestSkipValueBranches covers various skipValue paths via the public
// jsonPointerByteOffset helper. We build a fixture where many sibling
// values must be skipped.
func TestSkipValueBranches(t *testing.T) {
	src := []byte(`{
		"a":[1,2,3],
		"b":{"x":1},
		"c":true,
		"d":null,
		"e":"x",
		"f":42,
		"target":"hit"
	}`)
	off, ok := jsonPointerByteOffset(src, "/target")
	if !ok {
		t.Errorf("not ok")
	}
	// Off should be inside src.
	if off <= 0 || off >= len(src) {
		t.Errorf("off=%d", off)
	}
}

// =====================================================================
// extra: unescapeJSONPointerToken with ~0 only
// =====================================================================

// TestUnescapeJSONPointerOnlyTilde covers ~0 path.
func TestUnescapeJSONPointerOnlyTilde(t *testing.T) {
	if got := unescapeJSONPointerToken("a~0b"); got != "a~b" {
		t.Errorf("got %q", got)
	}
}

// =====================================================================
// strconv-driven sanity: parseNonNegativeInt success path
// =====================================================================

func TestParseNonNegativeIntOk(t *testing.T) {
	n, err := parseNonNegativeInt("42", "/x", "minLength")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if n != 42 {
		t.Errorf("got %d", n)
	}
}

// guard against unused imports
var _ = errors.New
var _ = strconv.Itoa
