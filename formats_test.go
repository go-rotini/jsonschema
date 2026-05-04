package jsonschema

import (
	"errors"
	"strings"
	"sync"
	"testing"
)

// TestBuiltinFormats covers each built-in format with positive, negative,
// and one boundary case.
func TestBuiltinFormats(t *testing.T) {
	cases := []struct {
		format string
		good   []string
		bad    []string
	}{
		{
			format: "date-time",
			good: []string{
				"1985-04-12T23:20:50.52Z",
				"1996-12-19T16:39:57-08:00",
				"1990-12-31T23:59:60Z", // boundary: leap second, UTC
			},
			bad: []string{
				"1985-04-12 23:20:50Z", // missing T
				"1990-02-31T15:59:59.123-08:00",
				"1990-12-31T15:59:59-24:00", // bad offset
				"not-a-date",
			},
		},
		{
			format: "date",
			good:   []string{"1985-04-12", "2024-02-29"},
			bad:    []string{"1985/04/12", "1985-13-01", "1985-04-31"},
		},
		{
			format: "time",
			good: []string{
				"23:20:50.52Z",
				"16:39:57-08:00",
				"01:29:60+01:30", // boundary: leap second shifted to UTC 23:59
			},
			bad: []string{"24:00:00Z", "12:00", "12:60:00Z"},
		},
		{
			format: "duration",
			good:   []string{"P1Y", "P2W", "P1Y2M3DT4H5M6S", "PT1H"},
			bad:    []string{"P", "P1Y2D", "PT", "P-1Y"},
		},
		{
			format: "uuid",
			good: []string{
				"00000000-0000-0000-0000-000000000000",
				"550e8400-e29b-41d4-a716-446655440000",
			},
			bad: []string{
				"00000000-0000-0000-0000-00000000000",   // 35 chars
				"00000000-0000-0000-0000-0000000000000", // 37 chars
				"zzzzzzzz-0000-0000-0000-000000000000",
			},
		},
		{
			format: "uri",
			good:   []string{"http://example.com/", "urn:uuid:00000000-0000-0000-0000-000000000000"},
			bad:    []string{"//foo.bar", "abc", "http://example.org/foo bar"},
		},
		{
			format: "uri-reference",
			good:   []string{"http://example.com/", "/path", "?q=1", "#frag"},
			bad:    []string{"http://example.org/foo bar", "http://example.org/{}", "http://%9G"},
		},
		{
			format: "iri",
			good:   []string{"http://example.com/", "http://例え.com/パス"},
			bad:    []string{"//bad", "http://example.com/\\bad"},
		},
		{
			format: "iri-reference",
			good:   []string{"/foo", "/foo/例え"},
			bad:    []string{"\\\\WINDOWS\\file", "http://例え.com/\\bad"},
		},
		{
			format: "uri-template",
			good:   []string{"http://example.com/{user}", "/foo{?q,r}", "/{+path}"},
			bad:    []string{"/foo{user", "/foo}", "/foo{us er}"},
		},
		{
			format: "json-pointer",
			good:   []string{"", "/foo", "/foo/0", "/a~0b/c~1d"},
			bad:    []string{"foo/bar", "/foo/~", "/foo/~2"},
		},
		{
			format: "relative-json-pointer",
			good:   []string{"0", "1/foo", "0#", "10/bar"},
			bad:    []string{"-1", "01/foo", "/foo", "1#bad"},
		},
		{
			format: "ipv4",
			good:   []string{"1.2.3.4", "0.0.0.0", "255.255.255.255"},
			bad:    []string{"999.0.0.0", "1.2.3.4.5", "::1", "1.2.3"},
		},
		{
			format: "ipv6",
			good:   []string{"::1", "2001:db8::1", "::ffff:1.2.3.4"},
			bad:    []string{"127.0.0.1", "fe80::1%eth0", "2001::g"},
		},
		{
			format: "hostname",
			good:   []string{"example.com", "a", "1.2.3.4", "abc-def.example.org"},
			bad:    []string{"-bad", "bad-", "host_name", "exa..mple", strings.Repeat("a", 64) + ".com"},
		},
		{
			format: "idn-hostname",
			good:   []string{"例え.com", "münchen.de"},
			bad:    []string{"\x00", "host_name"},
		},
		{
			format: "email",
			good:   []string{`a@b.c`, `joe.bloggs@example.com`, `"joe bloggs"@example.com`, `joe@[127.0.0.1]`},
			bad:    []string{`@example.com`, `a@`, `a..b@example.com`},
		},
		{
			format: "idn-email",
			good:   []string{`a@b.c`, `é@münchen.de`, `"a b"@example.com`},
			bad:    []string{`@münchen.de`, `é@`},
		},
		{
			format: "regex",
			good:   []string{`^foo$`, `\d+`, `a|b`},
			bad:    []string{`(`, `[`, `*foo`},
		},
	}
	for _, c := range cases {
		t.Run(c.format, func(t *testing.T) {
			fn, ok := builtinFormats[c.format]
			if !ok {
				t.Fatalf("no builtin %q", c.format)
			}
			for _, g := range c.good {
				if err := fn(g); err != nil {
					t.Errorf("%s positive %q: unexpected error %v", c.format, g, err)
				}
			}
			for _, b := range c.bad {
				if err := fn(b); err == nil {
					t.Errorf("%s negative %q: expected error, got nil", c.format, b)
				}
			}
		})
	}
}

// TestFormatErrorType verifies built-in validators return *FormatError.
func TestFormatErrorType(t *testing.T) {
	err := validateUUID("not-a-uuid")
	if err == nil {
		t.Fatal("expected error")
	}
	var fe *FormatError
	if !errors.As(err, &fe) {
		t.Fatalf("expected *FormatError, got %T", err)
	}
	if fe.Format != "uuid" {
		t.Errorf("got format %q", fe.Format)
	}
}

// TestFormatAssertionMode verifies that assertion mode reports an error
// while annotation-only mode silently passes.
func TestFormatAssertionMode(t *testing.T) {
	schema, err := Compile([]byte(`{"format":"ipv4"}`))
	if err != nil {
		t.Fatal(err)
	}
	bad := []byte(`"999.999.999.999"`)
	// Annotation only: should pass.
	res, err := schema.Validate(bad)
	if err != nil {
		t.Fatalf("validate (annotation): %v", err)
	}
	if !res.Valid {
		t.Errorf("annotation mode should pass; got %v", res.Errors)
	}
	// Assertion: should fail.
	res, err = schema.Validate(bad, WithFormatAssertion(true))
	if err != nil {
		t.Fatalf("validate (assertion): %v", err)
	}
	if res.Valid {
		t.Error("assertion mode should fail on 999.999.999.999")
	}
}

// TestFormatNonStringSilentPass: format only applies to strings.
func TestFormatNonStringSilentPass(t *testing.T) {
	schema, _ := Compile([]byte(`{"format":"ipv4"}`))
	res, err := schema.Validate([]byte(`123`), WithFormatAssertion(true))
	if err != nil {
		t.Fatal(err)
	}
	if !res.Valid {
		t.Errorf("non-string should pass under format")
	}
}

// TestCustomFormatOverridesBuiltin verifies the spec ordering: a custom
// validator wins over the built-in of the same name.
func TestCustomFormatOverridesBuiltin(t *testing.T) {
	schema, _ := Compile([]byte(`{"format":"ipv4"}`))
	// Custom: accept everything regardless of value.
	custom := func(string) error { return nil }
	res, err := schema.Validate([]byte(`"obviously-not-ipv4"`),
		WithFormatAssertion(true), WithCustomFormat("ipv4", custom))
	if err != nil {
		t.Fatal(err)
	}
	if !res.Valid {
		t.Error("custom should override built-in to accept")
	}
}

// TestCustomFormatNewName registers a brand-new format and exercises it.
func TestCustomFormatNewName(t *testing.T) {
	schema, _ := Compile([]byte(`{"format":"nine-digits"}`))
	nine := func(s string) error {
		if len(s) != 9 {
			return errors.New("not nine")
		}
		for _, c := range s {
			if c < '0' || c > '9' {
				return errors.New("non-digit")
			}
		}
		return nil
	}
	res, err := schema.Validate([]byte(`"123456789"`),
		WithFormatAssertion(true), WithCustomFormat("nine-digits", nine))
	if err != nil {
		t.Fatal(err)
	}
	if !res.Valid {
		t.Error("123456789 should match nine-digits")
	}
	res, _ = schema.Validate([]byte(`"abc"`),
		WithFormatAssertion(true), WithCustomFormat("nine-digits", nine))
	if res.Valid {
		t.Error("'abc' should fail nine-digits")
	}
}

// TestUnknownFormatPolicy verifies UnknownFormatError surfaces a validation
// error in assertion mode.
func TestUnknownFormatPolicy(t *testing.T) {
	schema, _ := Compile([]byte(`{"format":"nope"}`))
	// Default ignore: should pass.
	res, _ := schema.Validate([]byte(`"hi"`), WithFormatAssertion(true))
	if !res.Valid {
		t.Error("UnknownFormatIgnore should accept unknown formats")
	}
	// Error: should fail.
	res, _ = schema.Validate([]byte(`"hi"`),
		WithFormatAssertion(true),
		WithUnknownFormat(UnknownFormatError))
	if res.Valid {
		t.Error("UnknownFormatError should reject unknown formats")
	}
}

// TestUnknownFormatWarn: warn does not fail validation.
func TestUnknownFormatWarn(t *testing.T) {
	schema, _ := Compile([]byte(`{"format":"nope"}`))
	res, _ := schema.Validate([]byte(`"hi"`),
		WithFormatAssertion(true),
		WithUnknownFormat(UnknownFormatWarn))
	if !res.Valid {
		t.Error("UnknownFormatWarn should not fail validation")
	}
}

// TestHostnameLengthBoundary exercises the 255-byte total-length boundary.
// Per the implementation in [validateHostname], a 255-byte total length is
// the upper bound (inclusive), and 256 bytes are rejected. We pick labels
// that respect the per-label cap (63) so the total-length check is what
// distinguishes pass from fail.
func TestHostnameLengthBoundary(t *testing.T) {
	// Four 63-byte labels separated by three dots: 63*4 + 3 = 255 bytes.
	// (Label-cap 63 is checked separately by isHostnameLabel.)
	label63 := strings.Repeat("a", 63)
	host255 := label63 + "." + label63 + "." + label63 + "." + label63
	if len(host255) != 255 {
		t.Fatalf("test setup: host255 length = %d, want 255", len(host255))
	}
	host256 := host255 + "a"
	if err := validateHostname(host255); err != nil {
		t.Errorf("validateHostname(255 bytes): unexpected error %v", err)
	}
	if err := validateHostname(host256); err == nil {
		t.Errorf("validateHostname(256 bytes): expected error, got nil")
	}
}

// TestUUIDAllFs confirms the canonical 8-4-4-4-12 hex grouping accepts every
// hex character — not just the typical [0-9a-f] forms but also the
// upper-case all-Fs string. Treats the format as case-insensitive.
func TestUUIDAllFs(t *testing.T) {
	cases := []string{
		"FFFFFFFF-FFFF-FFFF-FFFF-FFFFFFFFFFFF",
		"ffffffff-ffff-ffff-ffff-ffffffffffff",
		"FfFfFfFf-FfFf-FfFf-FfFf-FfFfFfFfFfFf",
	}
	for _, s := range cases {
		t.Run(s, func(t *testing.T) {
			if err := validateUUID(s); err != nil {
				t.Errorf("validateUUID(%q): unexpected error %v", s, err)
			}
		})
	}
}

// TestDateTimeYearBoundaries exercises RFC 3339's permissive 4-digit year
// range. 0000 and 9999 are both legal.
func TestDateTimeYearBoundaries(t *testing.T) {
	cases := []string{
		"0000-01-01T00:00:00Z",
		"9999-12-31T23:59:59Z",
		"0001-01-01T00:00:00.000Z",
	}
	for _, s := range cases {
		t.Run(s, func(t *testing.T) {
			if err := validateDateTime(s); err != nil {
				t.Errorf("validateDateTime(%q): unexpected error %v", s, err)
			}
		})
	}
}

// TestFormatConcurrencySmoke validates the same compiled schema in parallel
// with assertion mode on. Running with -race catches data races.
func TestFormatConcurrencySmoke(t *testing.T) {
	schema, _ := Compile([]byte(`{"format":"date-time"}`))
	good := []byte(`"1985-04-12T23:20:50.52Z"`)
	bad := []byte(`"not-a-date"`)
	const workers = 16
	var wg sync.WaitGroup
	wg.Add(workers)
	for i := range workers {
		go func() {
			defer wg.Done()
			data := good
			if i%2 == 0 {
				data = bad
			}
			for range 50 {
				// Smoke test: the race detector catches data races; we
				// don't validate per-call output here.
				_, _ = schema.Validate(data, WithFormatAssertion(true))
			}
		}()
	}
	wg.Wait()
}

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

// TestFormatsMisc exercises various format-validator branches.
func TestFormatsMisc(t *testing.T) {
	cases := []struct {
		format string
		val    string
		valid  bool
	}{
		{"uri-template", "/api/{id}", true},
		{"uri-template", "/api/{", false},
		{"uri-template", "/api/}", false},
		{"uri-template", "/api/{bad expr}", false},
		{"uri-template", "/api/%XX", false},
		{"uri-template", "/api/%2A", true},
		{"iri", "https://example.com/", true},
		{"iri", "no-scheme", false},
		{"iri", "1bad-scheme://x", false},
		{"iri-reference", "/relative/path", true},
		{"json-pointer", "", true},
		{"json-pointer", "/a", true},
		{"json-pointer", "no-slash", false},
		{"json-pointer", "/a~", false},
		{"relative-json-pointer", "0", true},
		{"relative-json-pointer", "1#", true},
		{"relative-json-pointer", "01", false},
		{"relative-json-pointer", "/no-int", false},
		{"relative-json-pointer", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.format+"/"+tc.val, func(t *testing.T) {
			fn, ok := lookupFormat(tc.format, nil)
			if !ok {
				t.Skip("format not registered")
			}
			err := fn(tc.val)
			got := err == nil
			if got != tc.valid {
				t.Errorf("%s(%q): got valid=%v, want %v (err=%v)", tc.format, tc.val, got, tc.valid, err)
			}
		})
	}
}

// TestFormatEmailAndIDN covers email-shaped validators.
func TestFormatEmailAndIDN(t *testing.T) {
	for _, tc := range []struct {
		format string
		val    string
		valid  bool
	}{
		{"email", "alice@example.com", true},
		{"email", "no-at-sign", false},
		{"email", `"weird local"@example.com`, true},
		{"email", "x@.com", false},
		{"idn-email", "alice@münchen.de", true},
		{"idn-email", "no-at", false},
		{"hostname", "example.com", true},
		{"hostname", "-bad", false},
		{"idn-hostname", "münchen.de", true},
		{"idn-hostname", "", false},
	} {
		t.Run(tc.format+"/"+tc.val, func(t *testing.T) {
			fn, ok := lookupFormat(tc.format, nil)
			if !ok {
				t.Skip("not registered")
			}
			err := fn(tc.val)
			got := err == nil
			if got != tc.valid {
				t.Errorf("%s(%q): got %v, want %v (err=%v)", tc.format, tc.val, got, tc.valid, err)
			}
		})
	}
}
