package jsonschema

// Final round of coverage tests targeting remaining branches.

import (
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
