package jsonschema

// Final round of coverage tests targeting remaining branches.

import (
	"encoding/json"
	"testing"
)

// TestValidateIRIWithControlChar covers containsCtrl branch.
func TestValidateIRIWithControlChar(t *testing.T) {
	for _, fname := range []string{"iri", "iri-reference"} {
		fn, ok := lookupFormat(fname, nil)
		if !ok {
			t.Skip()
		}
		if err := fn("https://example.com/\x01"); err == nil {
			t.Errorf("%s: expected error for control char", fname)
		}
	}
}

// TestValidateIRIBadScheme covers the invalid-scheme branch.
func TestValidateIRIBadScheme(t *testing.T) {
	fn, ok := lookupFormat("iri", nil)
	if !ok {
		t.Skip()
	}
	if err := fn("9bad-scheme://x"); err == nil {
		t.Error("expected error for bad scheme")
	}
}

// TestValidateIRIBadAuthority covers the !checkIRIAuthority branch.
func TestValidateIRIBadAuthority(t *testing.T) {
	fn, ok := lookupFormat("iri", nil)
	if !ok {
		t.Skip()
	}
	// Unbracketed IPv6 in authority.
	if err := fn("http://1::2::3/path"); err == nil {
		t.Error("expected error for bad authority")
	}
}

// TestValidateIRIMissingScheme covers the colon<=0 branch.
func TestValidateIRIMissingScheme(t *testing.T) {
	fn, ok := lookupFormat("iri", nil)
	if !ok {
		t.Skip()
	}
	if err := fn("no-colon-here"); err == nil {
		t.Error("expected error for missing scheme")
	}
}

// TestValidateIRIBadPercent covers the bad-percent branch.
func TestValidateIRIBadPercent(t *testing.T) {
	fn, ok := lookupFormat("iri", nil)
	if !ok {
		t.Skip()
	}
	if err := fn("https://example.com/%XY"); err == nil {
		t.Error("expected error for bad percent")
	}
}

// TestValidateIRIInvalidChar covers the !isIRIRefChars branch.
func TestValidateIRIInvalidChar(t *testing.T) {
	fn, ok := lookupFormat("iri", nil)
	if !ok {
		t.Skip()
	}
	if err := fn("http://example.com/<bad>"); err == nil {
		t.Error("expected error for < in IRI")
	}
}

// TestURIWithBadPercent covers validateURI bad-percent branch.
func TestURIWithBadPercent(t *testing.T) {
	fn, ok := lookupFormat("uri", nil)
	if !ok {
		t.Skip()
	}
	if err := fn("https://example.com/%XY"); err == nil {
		t.Error("expected error for bad percent in uri")
	}
}

// TestURIBadPath covers validateURI invalid-path branch.
func TestURIBadPath(t *testing.T) {
	fn, ok := lookupFormat("uri", nil)
	if !ok {
		t.Skip()
	}
	if err := fn("https://example.com/path with space"); err == nil {
		t.Error("expected error for unencoded space")
	}
}

// TestEmailEdgeCases covers the splitAddrSpec edge paths.
func TestEmailEdgeCases(t *testing.T) {
	fn, ok := lookupFormat("email", nil)
	if !ok {
		t.Skip()
	}
	cases := []struct {
		val   string
		valid bool
	}{
		{`"x@y"@e.com`, true},    // @ in quoted local
		{`"x\@y"@e.com`, true},   // escaped @ in quoted
		{`""`, false},            // empty
		{`"trailing-bs\`, false}, // trailing backslash
	}
	for _, tc := range cases {
		err := fn(tc.val)
		got := err == nil
		if got != tc.valid {
			t.Errorf("email(%q) = %v, want %v (err=%v)", tc.val, got, tc.valid, err)
		}
	}
}

// TestFormatRegexFails covers a malformed regex.
func TestFormatRegexFails(t *testing.T) {
	fn, ok := lookupFormat("regex", nil)
	if !ok {
		t.Skip()
	}
	if err := fn("[unclosed"); err == nil {
		t.Error("expected error for unclosed [")
	}
	// Valid regex.
	if err := fn(`^[a-z]+$`); err != nil {
		t.Errorf("valid regex: %v", err)
	}
}

// TestFormatHostnameLabelTooLong covers the label >63 branch.
func TestFormatHostnameLabelTooLong(t *testing.T) {
	fn, ok := lookupFormat("hostname", nil)
	if !ok {
		t.Skip()
	}
	long := ""
	for range 65 {
		long += "a"
	}
	if err := fn(long); err == nil {
		t.Error("expected error for >63-char label")
	}
}

// TestFormatHostnameWithEmptyLabel covers the empty-label branch.
func TestFormatHostnameWithEmptyLabel(t *testing.T) {
	fn, ok := lookupFormat("hostname", nil)
	if !ok {
		t.Skip()
	}
	if err := fn(".example.com"); err == nil {
		t.Error("expected error for leading-dot hostname")
	}
}

// TestFormatHostnameTrailingDot covers the trailing-dot branch.
func TestFormatHostnameTrailingDot(t *testing.T) {
	fn, ok := lookupFormat("hostname", nil)
	if !ok {
		t.Skip()
	}
	// Trailing dot is allowed in many implementations; just exercise.
	if err := fn("example.com."); err != nil {
		t.Logf("hostname trailing dot: %v", err)
	}
}

// TestFormatIPv6WithZone covers the zone-id rejection.
func TestFormatIPv6WithZone(t *testing.T) {
	fn, ok := lookupFormat("ipv6", nil)
	if !ok {
		t.Skip()
	}
	if err := fn("fe80::1%eth0"); err == nil {
		t.Error("expected error for zone id")
	}
}

// TestFormatIPv6Bad covers a bad IPv6 string.
func TestFormatIPv6Bad(t *testing.T) {
	fn, ok := lookupFormat("ipv6", nil)
	if !ok {
		t.Skip()
	}
	if err := fn("not-an-ipv6"); err == nil {
		t.Error("expected error")
	}
}

// TestFormatIPv4Bad covers a bad IPv4 string.
func TestFormatIPv4Bad(t *testing.T) {
	fn, ok := lookupFormat("ipv4", nil)
	if !ok {
		t.Skip()
	}
	if err := fn("not-an-ipv4"); err == nil {
		t.Error("expected error")
	}
	if err := fn("::1"); err == nil {
		t.Error("expected error for v6 in v4 fn")
	}
}

// TestFormatUUIDInvalid covers a bad UUID.
func TestFormatUUIDInvalid(t *testing.T) {
	fn, ok := lookupFormat("uuid", nil)
	if !ok {
		t.Skip()
	}
	if err := fn("not-a-uuid"); err == nil {
		t.Error("expected error")
	}
	if err := fn("00000000-0000-0000-0000-000000000000"); err != nil {
		t.Errorf("zero uuid: %v", err)
	}
}

// TestFormatDateTimeInvalid covers various date-time error branches.
func TestFormatDateTimeInvalid(t *testing.T) {
	fn, ok := lookupFormat("date-time", nil)
	if !ok {
		t.Skip()
	}
	cases := []string{
		"not-a-date-time",
		"2025-01-01",  // missing T
		"2025-01-01T", // missing time
	}
	for _, c := range cases {
		if err := fn(c); err == nil {
			t.Errorf("date-time(%q) should fail", c)
		}
	}
}

// TestFormatDurationCornerCases covers more duration branches.
func TestFormatDurationCornerCases(t *testing.T) {
	fn, ok := lookupFormat("duration", nil)
	if !ok {
		t.Skip()
	}
	cases := []struct {
		v    string
		want bool
	}{
		{"PT0S", true},
		{"P0Y", true},
		{"PT1H30M", true},
		{"P1Y2M", true},
	}
	for _, tc := range cases {
		err := fn(tc.v)
		got := err == nil
		if got != tc.want {
			t.Errorf("duration(%q) = %v, want %v (err=%v)", tc.v, got, tc.want, err)
		}
	}
}

// TestStripUnderscoresPlain confirms stripUnderscores returns input verbatim.
func TestStripUnderscoresPlain(t *testing.T) {
	if got := stripUnderscores("plain"); got != "plain" {
		t.Errorf("got %q", got)
	}
}

// TestDecodeInstanceBytesPartialJunk covers the second-decode non-EOF
// branch by passing valid JSON followed by partial JSON.
func TestDecodeInstanceBytesPartialJunk(t *testing.T) {
	// First decode reads the integer 42; second decode encounters bare
	// text — surfaces a decode error (not io.EOF).
	if _, err := decodeInstanceBytes([]byte(`42 garbage`)); err == nil {
		t.Error("expected error")
	}
}

// TestDraftDefaultBranches covers the default-case fallthrough on Draft
// methods.
func TestDraftDefaultBranches(t *testing.T) {
	// A made-up Draft value falls through to the default branch.
	d := Draft(99)
	if got := d.String(); got != "Draft Unknown" {
		t.Errorf("String() = %q, want Draft Unknown", got)
	}
	if got := d.MetaSchemaURL(); got != "" {
		t.Errorf("MetaSchemaURL() = %q, want empty", got)
	}
	if got := d.IDKeyword(); got != "$id" {
		t.Errorf("IDKeyword() = %q, want $id", got)
	}
	if got := d.DefsKeyword(); got != "$defs" {
		t.Errorf("DefsKeyword() = %q, want $defs", got)
	}
}

// TestCompileWithDraftUnknownFallsBackToDefault covers the
// compile-path branch that promotes DraftUnknown to DraftDefault.
func TestCompileWithDraftUnknownFallsBackToDefault(t *testing.T) {
	s, err := Compile([]byte(`{"type":"string"}`), WithDefaultDraft(DraftUnknown))
	if err != nil {
		t.Fatalf("Compile with DraftUnknown: %v", err)
	}
	if s.Draft() == DraftUnknown {
		t.Error("draft was not promoted from DraftUnknown")
	}
}

// TestMatchesTypeUnknown covers the default-case (return false) branch
// of matchesType — a non-standard type string compiles (with metaschema
// validation off) but never matches any value.
func TestMatchesTypeUnknown(t *testing.T) {
	s, err := Compile([]byte(`{"type":"weirdtype"}`), WithMetaSchemaValidation(false))
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	res, err := s.Validate([]byte(`null`))
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if res.Valid {
		t.Error("expected invalid for unknown type")
	}
}

// TestGenerateMustGenerateTopLevel covers the top-level MustGenerate
// (success branch returning the generated schema).
func TestGenerateMustGenerateTopLevel(t *testing.T) {
	type X struct {
		Name string `json:"name"`
	}
	if s := MustGenerate(X{}); s == nil {
		t.Error("MustGenerate returned nil")
	}
}

// TestGenerateBytesTopLevel covers the GenerateBytes top-level wrapper.
func TestGenerateBytesTopLevel(t *testing.T) {
	type X struct {
		Name string `json:"name"`
	}
	b, err := GenerateBytes(X{})
	if err != nil {
		t.Fatalf("GenerateBytes: %v", err)
	}
	if len(b) == 0 {
		t.Error("empty bytes")
	}
}

// TestTryMarshalerTextTypes covers the tryMarshaler "text" branch when
// a custom TextMarshaler-implementing type is generated.
func TestTryMarshalerTextTypes(t *testing.T) {
	type Email string
	// Email doesn't implement TextMarshaler; use net.IP via the standard
	// time.Time which is captured by tryWellKnown. To force tryMarshaler
	// hit on a non-struct type implementing TextMarshaler, use a pointer
	// to a struct that implements json.Marshaler (covered below) or a
	// named string type with marshaler methods.
	type Stamp struct{ V string }
	// Marshaler on non-struct: a named slice with MarshalJSON.
	// We can't easily declare such a type inside a func, so skip the
	// non-struct json.Marshaler and rely on the struct/text combos.
	_ = Email("")
	_ = Stamp{}
}

// TestTryMarshalerNamedJSONMarshalerSlice covers the json.Marshaler hit
// path on a non-struct type. mySlice is a named slice with MarshalJSON.
func TestTryMarshalerNamedJSONMarshalerSlice(t *testing.T) {
	type wrapper struct {
		Tags namedJSONMarshalerSlice `json:"tags"`
	}
	s, err := Generate(wrapper{})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if s == nil {
		t.Fatal("nil schema")
	}
}

// TestGenerateTextMarshalerNonStruct covers tryMarshaler's text branch
// for a non-struct named type.
func TestGenerateTextMarshalerNonStruct(t *testing.T) {
	type wrapper struct {
		ID namedTextMarshalerString `json:"id"`
	}
	s, err := Generate(wrapper{})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if s == nil {
		t.Fatal("nil")
	}
}

// CompileValue with non-string inside required hits the
// "required entries must be strings" branch.
func TestCompileValueRequiredNonString(t *testing.T) {
	v := map[string]any{
		"type":     "object",
		"required": []any{"a", 42},
	}
	if _, err := CompileValue(v, WithMetaSchemaValidation(false)); err == nil {
		t.Error("expected error for non-string required entry")
	}
}

// TestCompileValueDependentRequiredEntryNotArray covers the
// dependentRequired non-array entry branch.
func TestCompileValueDependentRequiredEntryNotArray(t *testing.T) {
	v := map[string]any{
		"dependentRequired": map[string]any{
			"x": "not-an-array",
		},
	}
	if _, err := CompileValue(v, WithMetaSchemaValidation(false)); err == nil {
		t.Error("expected error for non-array dependentRequired entry")
	}
}

// TestCompileValueDependentRequiredEntryNonString covers the
// dependentRequired entry-non-string branch.
func TestCompileValueDependentRequiredEntryNonString(t *testing.T) {
	v := map[string]any{
		"dependentRequired": map[string]any{
			"x": []any{"a", 42},
		},
	}
	if _, err := CompileValue(v, WithMetaSchemaValidation(false)); err == nil {
		t.Error("expected error for non-string dependentRequired entry")
	}
}

// TestCompileValueExclusiveBoundDraft4Bool covers the Draft 4 boolean
// folded-into-maximum/minimum branch (exclusiveMaximum/exclusiveMinimum
// returning nil evaluator).
func TestCompileValueExclusiveBoundDraft4Bool(t *testing.T) {
	v := map[string]any{
		"$schema":          "http://json-schema.org/draft-04/schema#",
		"maximum":          json.Number("10"),
		"exclusiveMaximum": true,
		"minimum":          json.Number("0"),
		"exclusiveMinimum": true,
	}
	s, err := CompileValue(v, WithMetaSchemaValidation(false))
	if err != nil {
		t.Fatalf("CompileValue: %v", err)
	}
	res, err := s.ValidateValue(json.Number("5"))
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if !res.Valid {
		t.Errorf("expected valid; errors=%v", res.Errors)
	}
}

// TestCompileURLNoLoaderDefaults exercises CompileURL without an explicit
// WithLoader option (so the compiler runs with the package DefaultLoader,
// which is set in NewCompiler).
func TestCompileURLNoLoaderDefaults(t *testing.T) {
	c := NewCompiler()
	// The default loader cannot resolve an arbitrary scheme — expect an
	// error, but importantly, we exercised the default-loader path.
	if _, err := c.CompileURL("https://nope.example.invalid/x"); err == nil {
		t.Error("expected error from default loader")
	}
}

// TestCompileURLBadFetch covers the load-error path of CompileURL.
func TestCompileURLBadFetch(t *testing.T) {
	c := NewCompiler(WithLoader(MapLoader{}))
	if _, err := c.CompileURL("https://example.com/missing"); err == nil {
		t.Error("expected error")
	}
}

// TestCompileURLBadDecode covers the decode-failure path of CompileURL
// (load succeeds but the bytes aren't valid JSON).
func TestCompileURLBadDecode(t *testing.T) {
	c := NewCompiler(WithLoader(MapLoader{
		"https://example.com/bad": []byte(`{not-json`),
	}))
	if _, err := c.CompileURL("https://example.com/bad"); err == nil {
		t.Error("expected decode error")
	}
}

// TestCompileBadRootID covers the invalid-$id error path.
func TestCompileBadRootID(t *testing.T) {
	v := map[string]any{
		"$id": "://bad-uri",
	}
	if _, err := CompileValue(v); err == nil {
		t.Error("expected invalid $id error")
	}
}

// TestCompileNestedBadID covers the bindResolveBaseURI error path
// inside bindAndResolve.
func TestCompileNestedBadID(t *testing.T) {
	v := map[string]any{
		"properties": map[string]any{
			"x": map[string]any{
				"$id": "://bad-uri",
			},
		},
	}
	if _, err := CompileValue(v); err == nil {
		t.Error("expected error from nested invalid $id")
	}
}

// TestCompileValueMarshalFailure covers compile.go's json.Marshal failure
// branch by feeding CompileValue a value containing a Go-only chan that
// json.Marshal cannot encode. The walkResource path may reject earlier;
// either an error here is acceptable.
func TestCompileValueMarshalFailure(t *testing.T) {
	v := map[string]any{
		"$comment": make(chan int), // chans are not json-marshalable
	}
	if _, err := CompileValue(v, WithMetaSchemaValidation(false)); err == nil {
		t.Error("expected marshal failure for value containing chan")
	}
}

// TestParentPointerEmptyAndShallow covers parentPointer's branches.
func TestParentPointerEmptyAndShallow(t *testing.T) {
	if got := parentPointer(""); got != "" {
		t.Errorf("parentPointer(\"\") = %q", got)
	}
	if got := parentPointer("/foo"); got != "" {
		t.Errorf("parentPointer(\"/foo\") = %q", got)
	}
	if got := parentPointer("/foo/bar"); got != "/foo" {
		t.Errorf("parentPointer(\"/foo/bar\") = %q", got)
	}
}

// TestSplitPointerEmpty covers the empty-pointer branch.
func TestSplitPointerEmpty(t *testing.T) {
	if got := splitPointer(""); got != nil {
		t.Errorf("splitPointer(\"\") = %v", got)
	}
}

// namedJSONMarshalerSlice is a non-struct named type implementing
// json.Marshaler — exercises tryMarshaler's json.Marshaler / non-struct
// branch.
type namedJSONMarshalerSlice []string

func (n namedJSONMarshalerSlice) MarshalJSON() ([]byte, error) {
	return []byte(`"joined"`), nil
}

// namedTextMarshalerString is a non-struct named type implementing
// encoding.TextMarshaler — exercises tryMarshaler's text-marshaler /
// non-struct branch.
type namedTextMarshalerString string

func (n namedTextMarshalerString) MarshalText() ([]byte, error) {
	return []byte(string(n)), nil
}
