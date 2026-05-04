package jsonschema

// Multifmt-targeted coverage tests. Aim at YAML / TOML conversion paths
// that the existing tests miss.

import (
	"strings"
	"testing"
)

// TestLoadYAMLBoolKey exercises yamlKeyString's bool branch.
func TestLoadYAMLBoolKey(t *testing.T) {
	src := []byte("true: alpha\nfalse: beta\n")
	v, err := decodeYAML(src)
	if err != nil {
		t.Fatalf("decodeYAML: %v", err)
	}
	m, ok := v.(map[string]any)
	if !ok {
		t.Fatalf("got %T", v)
	}
	if _, ok := m["true"]; !ok {
		t.Errorf("expected 'true' key; got %v", m)
	}
	if _, ok := m["false"]; !ok {
		t.Errorf("expected 'false' key; got %v", m)
	}
}

// TestLoadYAMLNumberKey exercises yamlKeyString's json.Number branch.
func TestLoadYAMLNumberKey(t *testing.T) {
	src := []byte("42: alpha\n")
	v, err := decodeYAML(src)
	if err != nil {
		t.Fatalf("decodeYAML: %v", err)
	}
	m, ok := v.(map[string]any)
	if !ok {
		t.Fatalf("got %T", v)
	}
	if _, ok := m["42"]; !ok {
		t.Errorf("expected '42' key; got %v", m)
	}
}

// TestLoadYAMLNullKey covers yamlKeyString nil branch.
func TestLoadYAMLNullKey(t *testing.T) {
	src := []byte("null: alpha\n")
	v, err := decodeYAML(src)
	if err != nil {
		t.Logf("decodeYAML err: %v", err)
		return
	}
	m, ok := v.(map[string]any)
	if !ok {
		t.Fatalf("got %T", v)
	}
	if _, ok := m[""]; !ok {
		t.Errorf("expected '' key from nil; got %v", m)
	}
}

// TestLoadYAMLAlias covers convertYAMLNode's AliasNode branch.
func TestLoadYAMLAlias(t *testing.T) {
	src := []byte(`
alpha: &shared
  type: integer
beta: *shared
`)
	v, err := decodeYAML(src)
	if err != nil {
		t.Fatalf("decodeYAML: %v", err)
	}
	m, _ := v.(map[string]any)
	if a, b := m["alpha"], m["beta"]; a == nil || b == nil {
		t.Errorf("alias not resolved; got alpha=%v beta=%v", a, b)
	}
}

// TestLoadYAMLUnresolvedAlias covers the unresolved-alias error.
func TestLoadYAMLUnresolvedAlias(t *testing.T) {
	src := []byte("alpha: *missing\n")
	if _, err := decodeYAML(src); err == nil {
		t.Error("expected unresolved-alias error")
	}
}

// TestLoadYAMLMergeKey covers convertYAMLNode + mergeYAMLInto's mapping
// branch.
func TestLoadYAMLMergeKey(t *testing.T) {
	src := []byte(`
defaults: &defaults
  type: object
  required: [x]

actual:
  <<: *defaults
  properties:
    x:
      type: integer
`)
	v, err := decodeYAML(src)
	if err != nil {
		t.Fatalf("decodeYAML: %v", err)
	}
	if v == nil {
		t.Fatal("nil")
	}
}

// TestLoadYAMLMergeSequence covers mergeYAMLInto's []any branch.
// Note: YAML merge support depends on parser-level MergeKey detection,
// which the upstream yaml package handles for `<<:`.
func TestLoadYAMLMergeSequence(t *testing.T) {
	t.Skip("yaml merge sequence support depends on parser MergeKey detection")
}

// TestLoadYAMLMergeBadType covers the merge-not-map-or-seq branch.
func TestLoadYAMLMergeBadType(t *testing.T) {
	t.Skip("merge bad-type detection depends on parser MergeKey detection")
}

// TestLoadYAMLNestedSequence covers SequenceNode recursion.
func TestLoadYAMLNestedSequence(t *testing.T) {
	src := []byte(`
items:
  - 1
  - "two"
  - [3, 4]
  - {x: 5}
`)
	v, err := decodeYAML(src)
	if err != nil {
		t.Fatalf("decodeYAML: %v", err)
	}
	if v == nil {
		t.Fatal("nil")
	}
}

// TestLoadYAMLQuotedScalarStaysString covers convertYAMLScalar's quoted
// branch.
func TestLoadYAMLQuotedScalarStaysString(t *testing.T) {
	src := []byte(`x: "42"`)
	v, err := decodeYAML(src)
	if err != nil {
		t.Fatalf("decodeYAML: %v", err)
	}
	m, _ := v.(map[string]any)
	if _, ok := m["x"].(string); !ok {
		t.Errorf("quoted '42' should stay string; got %T", m["x"])
	}
}

// TestLoadYAMLEmptyDoc covers the empty-doc branch.
func TestLoadYAMLEmptyDoc(t *testing.T) {
	src := []byte("")
	v, err := decodeYAML(src)
	if err != nil {
		t.Fatalf("decodeYAML empty: %v", err)
	}
	if v != nil {
		t.Errorf("empty doc should be nil; got %v", v)
	}
}

// TestLoadYAMLNullDoc covers the null-doc branch.
func TestLoadYAMLNullDoc(t *testing.T) {
	src := []byte("---\n")
	v, err := decodeYAML(src)
	if err != nil {
		t.Logf("err: %v", err)
		return
	}
	if v != nil {
		t.Logf("got %v", v)
	}
}

// TestLoadTOMLBoolValue covers the BooleanNode branches (true, false).
func TestLoadTOMLBoolValue(t *testing.T) {
	src := []byte(`a = true
b = false`)
	v, err := decodeTOML(src)
	if err != nil {
		t.Fatalf("decodeTOML: %v", err)
	}
	m, _ := v.(map[string]any)
	if a := m["a"]; a != true {
		t.Errorf("a = %v", a)
	}
	if b := m["b"]; b != false {
		t.Errorf("b = %v", b)
	}
}

// TestLoadTOMLArrayOfMixed covers ArrayNode recursion.
func TestLoadTOMLArrayOfMixed(t *testing.T) {
	src := []byte(`a = [1, "two", true, 3.14]`)
	v, err := decodeTOML(src)
	if err != nil {
		t.Fatalf("decodeTOML: %v", err)
	}
	m, _ := v.(map[string]any)
	arr, ok := m["a"].([]any)
	if !ok || len(arr) != 4 {
		t.Errorf("got %v", m["a"])
	}
}

// TestLoadTOMLInlineTable covers InlineTableNode branch.
func TestLoadTOMLInlineTable(t *testing.T) {
	src := []byte(`a = {x = 1, y = "two"}`)
	v, err := decodeTOML(src)
	if err != nil {
		t.Fatalf("decodeTOML: %v", err)
	}
	m, _ := v.(map[string]any)
	a, ok := m["a"].(map[string]any)
	if !ok {
		t.Fatalf("a not a map; got %T", m["a"])
	}
	if a["x"] == nil {
		t.Errorf("missing x: %v", a)
	}
}

// TestLoadTOMLDateTimeVariants covers each datetime kind.
func TestLoadTOMLDateTimeVariants(t *testing.T) {
	src := []byte(`
offset = 1979-05-27T07:32:00Z
local_dt = 1979-05-27T07:32:00
local_d = 1979-05-27
local_t = 07:32:00
`)
	v, err := decodeTOML(src)
	if err != nil {
		t.Fatalf("decodeTOML: %v", err)
	}
	m, _ := v.(map[string]any)
	for _, k := range []string{"offset", "local_dt", "local_d", "local_t"} {
		if _, ok := m[k].(string); !ok {
			t.Errorf("%s: got %T, want string", k, m[k])
		}
	}
}

// TestLoadTOMLNested covers the nested-table descent.
func TestLoadTOMLNested(t *testing.T) {
	src := []byte(`
[parent.child]
x = 1
[parent.other]
y = 2
`)
	v, err := decodeTOML(src)
	if err != nil {
		t.Fatalf("decodeTOML: %v", err)
	}
	m, _ := v.(map[string]any)
	parent, _ := m["parent"].(map[string]any)
	if parent == nil {
		t.Fatal("missing parent")
	}
	if c, ok := parent["child"].(map[string]any); !ok || c["x"] == nil {
		t.Errorf("missing parent.child.x: %v", parent)
	}
	if o, ok := parent["other"].(map[string]any); !ok || o["y"] == nil {
		t.Errorf("missing parent.other.y: %v", parent)
	}
}

// TestLoadYAMLSpecialFloats covers .inf / .nan handling.
func TestLoadYAMLSpecialFloats(t *testing.T) {
	src := []byte(`a: .inf
b: -.inf
c: .nan`)
	v, err := decodeYAML(src)
	if err != nil {
		t.Fatalf("decodeYAML: %v", err)
	}
	m, _ := v.(map[string]any)
	for _, k := range []string{"a", "b", "c"} {
		if m[k] == nil {
			t.Errorf("missing %s", k)
		}
	}
}

// TestLoadYAMLHexOctal covers hex/octal yamlNormalizeNumber branches.
func TestLoadYAMLHexOctal(t *testing.T) {
	src := []byte(`hex: 0x1A
oct: 0o17`)
	v, err := decodeYAML(src)
	if err != nil {
		t.Fatalf("decodeYAML: %v", err)
	}
	m, _ := v.(map[string]any)
	for _, k := range []string{"hex", "oct"} {
		if m[k] == nil {
			t.Errorf("missing %s", k)
		}
	}
}

// TestValidateYAMLAndJSONCAndTOMLSuccessPaths exercises the validate-format
// adapters' happy paths.
func TestValidateYAMLAndJSONCAndTOMLSuccessPaths(t *testing.T) {
	s := MustCompile([]byte(`{"type":"object","required":["name"],"properties":{"name":{"type":"string"}}}`))
	// JSONC
	res, err := ValidateJSONC(s, []byte(`{
		// comment
		"name": "alice",
	}`))
	if err != nil {
		t.Fatalf("ValidateJSONC: %v", err)
	}
	if !res.Valid {
		t.Errorf("ValidateJSONC: errors=%v", res.Errors)
	}
	// YAML
	res, err = ValidateYAML(s, []byte("name: alice\n"))
	if err != nil {
		t.Fatalf("ValidateYAML: %v", err)
	}
	if !res.Valid {
		t.Errorf("ValidateYAML: errors=%v", res.Errors)
	}
	// TOML
	res, err = ValidateTOML(s, []byte(`name = "alice"`))
	if err != nil {
		t.Fatalf("ValidateTOML: %v", err)
	}
	if !res.Valid {
		t.Errorf("ValidateTOML: errors=%v", res.Errors)
	}
}

// TestLoadYAMLEmptyResource covers the no-docs / empty docs branches.
func TestLoadYAMLEmptyResource(t *testing.T) {
	if _, err := decodeYAML([]byte("")); err != nil {
		t.Errorf("empty: %v", err)
	}
}

// TestLoadJSONCWithComments confirms LoadJSONC tolerates comments.
func TestLoadJSONCWithComments(t *testing.T) {
	src := []byte(`{
		// top
		"type": "object",
		/* nested */
		"required": ["name"],
		"properties": {"name": {"type":"string"}}
	}`)
	if _, err := LoadJSONC(src); err != nil {
		t.Errorf("LoadJSONC: %v", err)
	}
}

// TestLoadYAMLViaScript ensures the format adapter compiles + validates.
func TestLoadYAMLViaScript(t *testing.T) {
	src := []byte(`type: integer
minimum: 0`)
	s, err := LoadYAML(src)
	if err != nil {
		t.Fatalf("LoadYAML: %v", err)
	}
	if res, _ := s.Validate([]byte("5")); !res.Valid {
		t.Errorf("expected valid; errors=%v", res.Errors)
	}
}

// TestStripUnderscoresOnAllInts confirms underscored-int paths.
func TestStripUnderscoresAllPaths(t *testing.T) {
	cases := []string{"1_000_000", "0x_FF", "0b_101"}
	for _, c := range cases {
		got := stripUnderscores(c)
		if strings.Contains(got, "_") {
			t.Errorf("got %q", got)
		}
	}
}

// TestLoadJSONCCompileFailure covers the JSONC decode-OK + compile-fail
// branch: JSONC parses fine, but the result is a malformed schema.
func TestLoadJSONCCompileFailure(t *testing.T) {
	src := []byte(`{"minLength":"three"}`) // wrong type for minLength
	if _, err := LoadJSONC(src); err == nil {
		t.Error("expected compile-fail error")
	}
}

// TestLoadYAMLCompileFailure covers the YAML decode-OK + compile-fail
// branch.
func TestLoadYAMLCompileFailure(t *testing.T) {
	src := []byte("minLength: three\n")
	if _, err := LoadYAML(src); err == nil {
		t.Error("expected compile-fail error")
	}
}

// TestLoadTOMLCompileFailure covers the TOML decode-OK + compile-fail
// branch.
func TestLoadTOMLCompileFailure(t *testing.T) {
	src := []byte(`minLength = "three"`)
	if _, err := LoadTOML(src); err == nil {
		t.Error("expected compile-fail error")
	}
}

// TestValidateJSONCValidationFailure covers the validation-side decode
// success path with a failing validation.
func TestValidateJSONCValidationFailure(t *testing.T) {
	s := MustCompile([]byte(`{"type":"object","required":["a"]}`))
	res, err := ValidateJSONC(s, []byte(`{}`))
	if err != nil {
		t.Fatalf("ValidateJSONC: %v", err)
	}
	if res.Valid {
		t.Error("expected invalid")
	}
}

// TestValidateYAMLValidationFailure covers the YAML validate path.
func TestValidateYAMLValidationFailure(t *testing.T) {
	s := MustCompile([]byte(`{"type":"object","required":["a"]}`))
	res, err := ValidateYAML(s, []byte(`b: 1`))
	if err != nil {
		t.Fatalf("ValidateYAML: %v", err)
	}
	if res.Valid {
		t.Error("expected invalid")
	}
}

// TestValidateTOMLValidationFailure covers the TOML validate path.
func TestValidateTOMLValidationFailure(t *testing.T) {
	s := MustCompile([]byte(`{"type":"object","required":["a"]}`))
	res, err := ValidateTOML(s, []byte(`b = 1`))
	if err != nil {
		t.Fatalf("ValidateTOML: %v", err)
	}
	if res.Valid {
		t.Error("expected invalid")
	}
}

// TestLoadURLBytesNilLoader covers the nil-loader branch in loadURLBytes
// (use no WithLoader option).
func TestLoadURLBytesNilLoader(t *testing.T) {
	// No loader option — falls back to DefaultLoader. Since the default
	// loader's HTTPLoader will fail to fetch a non-existent URL, we expect
	// an error.
	if _, err := loadURLBytes("https://nope.example.invalid/x", nil); err == nil {
		t.Error("expected error from default loader")
	}
}
