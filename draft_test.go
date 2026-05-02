package jsonschema

import "testing"

func TestDraftString(t *testing.T) {
	cases := []struct {
		d    Draft
		want string
	}{
		{DraftUnknown, "Draft Unknown"},
		{Draft4, "Draft 4"},
		{Draft6, "Draft 6"},
		{Draft7, "Draft 7"},
		{Draft201909, "Draft 2019-09"},
		{Draft202012, "Draft 2020-12"},
		{Draft(99), "Draft Unknown"},
	}
	for _, c := range cases {
		if got := c.d.String(); got != c.want {
			t.Errorf("Draft(%d).String() = %q, want %q", int(c.d), got, c.want)
		}
	}
}

func TestDraftDefaultIs2020_12(t *testing.T) {
	if DraftDefault != Draft202012 {
		t.Errorf("DraftDefault = %v, want Draft202012", DraftDefault)
	}
}

func TestDraftIDKeyword(t *testing.T) {
	cases := map[Draft]string{
		Draft4:      "id",
		Draft6:      "$id",
		Draft7:      "$id",
		Draft201909: "$id",
		Draft202012: "$id",
	}
	for d, want := range cases {
		if got := d.IDKeyword(); got != want {
			t.Errorf("%s.IDKeyword() = %q, want %q", d, got, want)
		}
	}
}

func TestDraftDefsKeyword(t *testing.T) {
	cases := map[Draft]string{
		Draft4:      "definitions",
		Draft6:      "definitions",
		Draft7:      "definitions",
		Draft201909: "$defs",
		Draft202012: "$defs",
	}
	for d, want := range cases {
		if got := d.DefsKeyword(); got != want {
			t.Errorf("%s.DefsKeyword() = %q, want %q", d, got, want)
		}
	}
}

func TestDraftMetaSchemaURL(t *testing.T) {
	cases := map[Draft]string{
		Draft4:       "http://json-schema.org/draft-04/schema#",
		Draft6:       "http://json-schema.org/draft-06/schema#",
		Draft7:       "http://json-schema.org/draft-07/schema#",
		Draft201909:  "https://json-schema.org/draft/2019-09/schema",
		Draft202012:  "https://json-schema.org/draft/2020-12/schema",
		DraftUnknown: "",
	}
	for d, want := range cases {
		if got := d.MetaSchemaURL(); got != want {
			t.Errorf("%s.MetaSchemaURL() = %q, want %q", d, got, want)
		}
	}
}

func TestDraftMetaSchemaURLPackageFunction(t *testing.T) {
	// MetaSchemaURL (package level) is documented as equivalent to the
	// method on Draft. Verify the alignment for every supported draft.
	for _, d := range []Draft{Draft4, Draft6, Draft7, Draft201909, Draft202012} {
		if MetaSchemaURL(d) != d.MetaSchemaURL() {
			t.Errorf("MetaSchemaURL(%s) = %q, want %q",
				d, MetaSchemaURL(d), d.MetaSchemaURL())
		}
	}
}

func TestDraftFromMetaSchemaURL(t *testing.T) {
	type tc struct {
		input string
		want  Draft
	}
	cases := []tc{
		// Canonical forms from the spec.
		{"http://json-schema.org/draft-04/schema#", Draft4},
		{"http://json-schema.org/draft-06/schema#", Draft6},
		{"http://json-schema.org/draft-07/schema#", Draft7},
		{"https://json-schema.org/draft/2019-09/schema", Draft201909},
		{"https://json-schema.org/draft/2020-12/schema", Draft202012},
		// HTTPS variants of legacy drafts (some tools rewrite to https).
		{"https://json-schema.org/draft-04/schema#", Draft4},
		{"https://json-schema.org/draft-06/schema#", Draft6},
		{"https://json-schema.org/draft-07/schema#", Draft7},
		// Without trailing # for legacy drafts.
		{"http://json-schema.org/draft-04/schema", Draft4},
		{"https://json-schema.org/draft-04/schema", Draft4},
		// With trailing # for modern drafts.
		{"https://json-schema.org/draft/2019-09/schema#", Draft201909},
		{"https://json-schema.org/draft/2020-12/schema#", Draft202012},
		// HTTP variants of modern drafts (rare but plausible).
		{"http://json-schema.org/draft/2019-09/schema", Draft201909},
		{"http://json-schema.org/draft/2020-12/schema", Draft202012},
		// Whitespace tolerance.
		{"  https://json-schema.org/draft/2020-12/schema  ", Draft202012},
		// Unknown URLs.
		{"", DraftUnknown},
		{"https://example.com/schema", DraftUnknown},
		{"https://json-schema.org/draft/2099-99/schema", DraftUnknown},
	}
	for _, c := range cases {
		if got := DraftFromMetaSchemaURL(c.input); got != c.want {
			t.Errorf("DraftFromMetaSchemaURL(%q) = %s, want %s",
				c.input, got, c.want)
		}
	}
}

func TestDraftRoundTrip(t *testing.T) {
	// Every supported draft must round-trip through
	// MetaSchemaURL → DraftFromMetaSchemaURL.
	for _, d := range []Draft{Draft4, Draft6, Draft7, Draft201909, Draft202012} {
		round := DraftFromMetaSchemaURL(d.MetaSchemaURL())
		if round != d {
			t.Errorf("round-trip %s: MetaSchemaURL %q → DraftFromMetaSchemaURL = %s",
				d, d.MetaSchemaURL(), round)
		}
	}
}
