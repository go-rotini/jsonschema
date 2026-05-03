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
				_, _ = schema.Validate(data, WithFormatAssertion(true))
			}
		}()
	}
	wg.Wait()
}
