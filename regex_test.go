package jsonschema

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

// uEsc turns each "%u" in s into a backslash-u, so the cases below can spell
// \u escapes in ECMA-262 patterns and JSON text.
func uEsc(s string) string { return strings.ReplaceAll(s, "%u", `\`+"u") }

// TestPatternECMASemantics checks that pattern follows ECMA-262 meaning
// where it differs from RE2.
func TestPatternECMASemantics(t *testing.T) {
	cases := []struct {
		pattern, instance string
		want              bool
	}{
		{`^%u0041$`, `"A"`, true},
		{`^[^%u0000-%u001f]*$`, `"ok"`, true},
		{`^[^%u0000-%u001f]*$`, `"a%u0001"`, false},
		{`^\cJ$`, `"\n"`, true},
		{`^%u{1F600}$`, `"%ud83d%ude00"`, true},
		{`^%uD83D%uDE00$`, `"%ud83d%ude00"`, true},
		{`^a.b$`, `"a\rb"`, false},
		{`^a.b$`, `"a%u2028b"`, false},
		{`^a.b$`, `"axb"`, true},
		{`^\s$`, `"%u00a0"`, true},
		{`^\s$`, `"%u2003"`, true},
		{`^\S$`, `"%u00a0"`, false},
		{`^[\s]$`, `"%ufeff"`, true},
		{`^[]$`, `""`, false},
		{`^[^]$`, `"\n"`, true},
		{`^\x41$`, `"A"`, true},
		{`^\0$`, `"%u0000"`, true},
		{`^(?<y>\d+)$`, `"12"`, true},
		{`^[\b]$`, `"\b"`, true},
		{`^\d$`, `"%u0663"`, false},
		{`^\/$`, `"/"`, true},
	}
	for _, tc := range cases {
		pattern, instance := uEsc(tc.pattern), uEsc(tc.instance)
		schema := `{"pattern":` + jsonString(pattern) + `}`
		if got := validOf(t, schema, instance); got != tc.want {
			t.Errorf("pattern %s with %s: valid=%v, want %v", pattern, instance, got, tc.want)
		}
	}
}

// TestPatternUnsupportedECMA checks that backreferences and lookaround,
// which RE2 cannot run, fail Compile with a clear cause.
func TestPatternUnsupportedECMA(t *testing.T) {
	for _, p := range []string{`(a)\1`, `(?=a)`, `(?<!a)b`, `(?<n>a)\k<n>`} {
		_, err := Compile([]byte(`{"pattern":` + jsonString(p) + `}`))
		if !errors.Is(err, errECMAUnsupported) {
			t.Errorf("%s: err = %v, want errECMAUnsupported", p, err)
		}
		_, err = Compile([]byte(`{"patternProperties":{` + jsonString(p) + `:{}}}`))
		if !errors.Is(err, errECMAUnsupported) {
			t.Errorf("patternProperties %s: err = %v", p, err)
		}
	}
}

// TestRegexFormatIsECMA checks the "regex" format against ECMA-262 syntax
// rather than RE2's.
func TestRegexFormatIsECMA(t *testing.T) {
	valid := []string{`(?<n>a)\k<n>`, `(?<=a)b`, `[]`, `\cA`, uEsc(`%u{41}`), `a{2}`}
	invalid := []string{`(?P<n>a)`, `(?i)a`, `\a`, `[a`, `\c1`, `\01`}
	for _, s := range valid {
		if err := validateRegex(s); err != nil {
			t.Errorf("%s rejected: %v", s, err)
		}
	}
	for _, s := range invalid {
		if err := validateRegex(s); err == nil {
			t.Errorf("%s accepted", s)
		}
	}
}

// TestFormatGrammarFixes covers email, uri, uri-reference and uri-template
// cases the grammar allows or forbids.
func TestFormatGrammarFixes(t *testing.T) {
	cases := []struct {
		format, value string
		want          bool
	}{
		{"email", "joe@[ipv6:::1]", true},
		{"email", "joe@[IPv6:::1]", true},
		{"uri", "http://example.com/a[b]", false},
		{"uri", "http://[::1]:80/a", true},
		{"uri", "http://u@[::1]/", true},
		{"uri-reference", "a[b]", false},
		{"uri-reference", "//a@b@c/", false},
		{"uri-reference", "//user@host/x?y", true},
		{"uri-template", "{v:0}", false},
		{"uri-template", "{v:01}", false},
		{"uri-template", "{v:10}", true},
	}
	for _, tc := range cases {
		schema := `{"format":"` + tc.format + `"}`
		s := MustCompile([]byte(schema))
		res, err := s.Validate([]byte(jsonString(tc.value)), WithFormatAssertion(true))
		if err != nil {
			t.Fatal(err)
		}
		if res.Valid != tc.want {
			t.Errorf("%s %q: valid=%v, want %v", tc.format, tc.value, res.Valid, tc.want)
		}
	}
}

func jsonString(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

// TestRegexFormatChecksPastUnsupported checks that the regex format keeps
// checking syntax after a lookaround or backreference.
func TestRegexFormatChecksPastUnsupported(t *testing.T) {
	for p, valid := range map[string]bool{
		`(?=a)[`:        false,
		`\1(`:           false,
		`(?<!x)a{2,1}`:  false,
		`\k<n>)`:        false,
		`(?=a)b`:        true,
		`(?<!x)a{1,2}`:  true,
		`(a)\1`:         true,
		`(?<n>a)\k<n>b`: true,
		`(?=a(?!b))c`:   true,
		`[^\S]x`:        true,
		`[a\S]`:         true,
	} {
		if got := validateRegex(p) == nil; got != valid {
			t.Errorf("validateRegex(%q) valid=%v, want %v", p, got, valid)
		}
	}
}

// TestECMANonSpaceInClass checks that \S inside a class keeps the ECMA
// meaning, in which U+00A0 and the other Unicode spaces are spaces.
func TestECMANonSpaceInClass(t *testing.T) {
	for _, tc := range []struct {
		pattern, s string
		match      bool
	}{
		{`^[\S]$`, " ", false},
		{`^[\S]$`, "x", true},
		{`^[\S]$`, " ", false},
		{`^[a\S]$`, " ", false},
		{`^[a\S]$`, "b", true},
		{`^[\s\S]$`, " ", true},
		{`^[^\S]$`, " ", true},
		{`^[^\S]$`, "x", false},
		{`^[^a\S]$`, "\u00a0", true},
		{`^[^a\S]$`, "a", false},
	} {
		re, err := compileECMA(tc.pattern)
		if err != nil {
			t.Fatalf("%s: %v", tc.pattern, err)
		}
		if got := re.MatchString(tc.s); got != tc.match {
			t.Errorf("%s on %q = %v, want %v", tc.pattern, tc.s, got, tc.match)
		}
	}
	for _, tc := range []struct {
		pattern, s string
		match      bool
	}{
		{`^[^\t\S]$`, "\t", false},
		{`^[^\t\S]$`, "\u00a0", true},
		{`^[^\t\S]$`, "x", false},
		{`^[^\s\S]$`, " ", false},
	} {
		re, err := compileECMA(tc.pattern)
		if err != nil {
			t.Fatalf("%s: %v", tc.pattern, err)
		}
		if got := re.MatchString(tc.s); got != tc.match {
			t.Errorf("%s on %q = %v, want %v", tc.pattern, tc.s, got, tc.match)
		}
	}
}

// TestPatternAcceptsAnnexBAndRE2 checks that patterns written with the
// identity escapes ECMA-262 Annex B allows, or with RE2 inline flags,
// compile and match as they did when patterns were plain RE2.
func TestPatternAcceptsAnnexBAndRE2(t *testing.T) {
	cases := []struct {
		pattern, instance string
		want              bool
	}{
		{`^[a-z\_]+$`, `"a_b"`, true},
		{`^[^\/~\^\: \[\]\\]+$`, `"refs"`, true},
		{`^[^\/~\^\: \[\]\\]+$`, `"a:b"`, false},
		{`^\-\@\#\%\!\"\ $`, `"-@#%!\" "`, true},
		{`^a\éb$`, `"aéb"`, true},
		{`(?i)^abc$`, `"ABC"`, true},
		{`(?i:x)y`, `"Xy"`, true},
		{`(?ms)^a.b$`, `"a\nb"`, true},
		{`^\s$`, `" "`, true},
	}
	for _, tc := range cases {
		schema := `{"pattern":` + jsonString(tc.pattern) + `}`
		if got := validOf(t, schema, tc.instance); got != tc.want {
			t.Errorf("pattern %s with %s: valid=%v, want %v", tc.pattern, tc.instance, got, tc.want)
		}
		if _, err := Compile([]byte(`{"patternProperties":{` + jsonString(tc.pattern) + `:{}}}`)); err != nil {
			t.Errorf("patternProperties %s: %v", tc.pattern, err)
		}
	}
}

// TestQuantifiedAssertion checks that the regex format rejects a quantifier
// on an assertion, which ECMA-262 forbids, while pattern still compiles it.
func TestQuantifiedAssertion(t *testing.T) {
	for _, p := range []string{`\b*`, `\B{2}`, `^*`, `$+`, `a^?`, `(?<=a)*b`, `(?<!a){1,2}b`} {
		if validateRegex(p) == nil {
			t.Errorf("regex format accepted %s", p)
		}
	}
	for _, p := range []string{`\b*`, `^*`, `$+`} {
		if _, err := Compile([]byte(`{"pattern":` + jsonString(p) + `}`)); err != nil {
			t.Errorf("pattern %s: %v", p, err)
		}
	}
	for _, p := range []string{`(?=a)*a`, `^a*`, `\ba{2}`, `(a)*`, `[$]*`, `[\b]+`, `\b{`, `\_\-`, `(?:^)a`} {
		if err := validateRegex(p); err != nil {
			t.Errorf("regex format rejected %s: %v", p, err)
		}
	}
}

// TestModifierGroups checks that ECMA-262 modifier groups apply only inside
// the group and leave the rest of the pattern its ECMA-262 meaning.
func TestModifierGroups(t *testing.T) {
	cases := []struct {
		pattern, instance string
		want              bool
	}{
		{`^(?i:a)b$`, `"Ab"`, true},
		{`^(?i:a)b$`, `"AB"`, false},
		{`^(?i:a(?-i:b))$`, `"Ab"`, true},
		{`^(?i:a(?-i:b))$`, `"AB"`, false},
		{`^(?i:a)\.$`, `"a."`, true},
		{`(?i:a).`, `"a\r"`, false},
		{`^(?i:a)?\S$`, `"%u00a0"`, false},
		{`^(?i:a)?\S$`, `"\u000b"`, false},
		{`[\s](?i:a)`, `" a"`, true},
		{`^(?s:.)$`, `"\n"`, true},
		{`^(?s:a)(.)$`, `"a\n"`, false},
		{`^(?s:(?-s:.))$`, `"\n"`, false},
		{`^(?is:A.)$`, `"a%u2028"`, true},
		{`(?m:^b)`, `"a\nb"`, true},
		{`^b`, `"a\nb"`, false},
		{`^(?i-:a)$`, `"A"`, true},
	}
	for _, tc := range cases {
		pattern, instance := uEsc(tc.pattern), uEsc(tc.instance)
		schema := `{"pattern":` + jsonString(pattern) + `}`
		if got := validOf(t, schema, instance); got != tc.want {
			t.Errorf("pattern %s with %s: valid=%v, want %v", pattern, instance, got, tc.want)
		}
	}
	for _, p := range []string{`(?i:a)*`, `(?-i:a)`, `(?ims:a)`, `(?i-ms:a)`, `(?s-:a)`} {
		if err := validateRegex(p); err != nil {
			t.Errorf("regex format rejected %s: %v", p, err)
		}
	}
	for _, p := range []string{`(?-:a)`, `(?ii:a)`, `(?i-i:a)`, `(?x:a)`, `(?I:a)`, `(?i`, `(?i-s-m:a)`} {
		if validateRegex(p) == nil {
			t.Errorf("regex format accepted %s", p)
		}
	}
}
