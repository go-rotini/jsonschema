package jsonschema

import (
	"strings"
	"testing"
)

func TestVocabularyConstants(t *testing.T) {
	// The package's vocabulary URI constants are part of the public API
	// and must remain stable across releases.
	cases := map[string]string{
		"VocabCore":         "https://json-schema.org/draft/2020-12/vocab/core",
		"VocabApplicator":   "https://json-schema.org/draft/2020-12/vocab/applicator",
		"VocabUnevaluated":  "https://json-schema.org/draft/2020-12/vocab/unevaluated",
		"VocabValidation":   "https://json-schema.org/draft/2020-12/vocab/validation",
		"VocabFormatAnnot":  "https://json-schema.org/draft/2020-12/vocab/format-annotation",
		"VocabFormatAssert": "https://json-schema.org/draft/2020-12/vocab/format-assertion",
		"VocabContent":      "https://json-schema.org/draft/2020-12/vocab/content",
		"VocabMetaData":     "https://json-schema.org/draft/2020-12/vocab/meta-data",
	}
	got := map[string]string{
		"VocabCore":         VocabCore,
		"VocabApplicator":   VocabApplicator,
		"VocabUnevaluated":  VocabUnevaluated,
		"VocabValidation":   VocabValidation,
		"VocabFormatAnnot":  VocabFormatAnnot,
		"VocabFormatAssert": VocabFormatAssert,
		"VocabContent":      VocabContent,
		"VocabMetaData":     VocabMetaData,
	}
	for name, want := range cases {
		if got[name] != want {
			t.Errorf("%s = %q, want %q", name, got[name], want)
		}
	}
}

func TestVocabulariesReturnsEightStdEntries(t *testing.T) {
	v := Vocabularies()
	if len(v) != 8 {
		t.Fatalf("Vocabularies len = %d, want 8", len(v))
	}
	want := []string{
		VocabCore, VocabApplicator, VocabUnevaluated, VocabValidation,
		VocabFormatAnnot, VocabFormatAssert, VocabContent, VocabMetaData,
	}
	for i, w := range want {
		if v[i].URI != w {
			t.Errorf("Vocabularies[%d].URI = %q, want %q", i, v[i].URI, w)
		}
		if len(v[i].Keywords) == 0 {
			t.Errorf("Vocabularies[%d] (%s) has no keywords", i, w)
		}
	}
}

func TestVocabulariesIsDefensiveCopy(t *testing.T) {
	a := Vocabularies()
	b := Vocabularies()
	if &a[0] == &b[0] {
		t.Errorf("Vocabularies returned same backing array")
	}
}

func TestKeywordsForDraft202012(t *testing.T) {
	got := KeywordsForDraft(Draft202012)
	names := make(map[string]Keyword, len(got))
	for _, k := range got {
		names[k.Name()] = k
	}
	// Modern keywords that must be present.
	for _, want := range []string{
		"$id", "$ref", "$dynamicRef", "$dynamicAnchor", "$defs", "$anchor",
		"prefixItems", "unevaluatedItems", "unevaluatedProperties",
		"contains", "if", "then", "else", "const", "format",
	} {
		if _, ok := names[want]; !ok {
			t.Errorf("Draft 2020-12 set missing keyword %q", want)
		}
	}
	// Retired keywords that must NOT appear.
	for _, retired := range []string{
		"id", "definitions", "$recursiveRef", "$recursiveAnchor",
		"additionalItems", "dependencies",
	} {
		if _, ok := names[retired]; ok {
			t.Errorf("Draft 2020-12 set unexpectedly contains retired keyword %q", retired)
		}
	}
}

func TestKeywordsForDraft4(t *testing.T) {
	got := KeywordsForDraft(Draft4)
	names := make(map[string]Keyword, len(got))
	for _, k := range got {
		names[k.Name()] = k
	}
	// Draft 4 must use the legacy spelling and not the modern aliases.
	for _, want := range []string{"id", "definitions", "additionalItems", "dependencies", "$ref", "type", "minLength"} {
		if _, ok := names[want]; !ok {
			t.Errorf("Draft 4 set missing keyword %q", want)
		}
	}
	for _, modern := range []string{"$id", "$defs", "$anchor", "$dynamicRef", "prefixItems", "if", "then", "else", "const", "$comment"} {
		if _, ok := names[modern]; ok {
			t.Errorf("Draft 4 set unexpectedly contains modern keyword %q", modern)
		}
	}
}

func TestKeywordsForDraft201909(t *testing.T) {
	got := KeywordsForDraft(Draft201909)
	names := make(map[string]Keyword, len(got))
	for _, k := range got {
		names[k.Name()] = k
	}
	// 2019-09 introduces $recursiveRef/$recursiveAnchor; 2020-12 retires
	// them.
	for _, want := range []string{"$recursiveRef", "$recursiveAnchor", "$defs", "unevaluatedItems"} {
		if _, ok := names[want]; !ok {
			t.Errorf("Draft 2019-09 set missing keyword %q", want)
		}
	}
	for _, modern := range []string{"$dynamicRef", "$dynamicAnchor", "prefixItems"} {
		if _, ok := names[modern]; ok {
			t.Errorf("Draft 2019-09 set unexpectedly contains 2020-12 keyword %q", modern)
		}
	}
}

func TestKeywordsForDraftUnknown(t *testing.T) {
	if got := KeywordsForDraft(DraftUnknown); got != nil {
		t.Errorf("KeywordsForDraft(DraftUnknown) = %v, want nil", got)
	}
}

func TestKeywordsForDraftDeduplicatesByName(t *testing.T) {
	// "format" lives in both VocabFormatAnnot and VocabFormatAssert; the
	// active set must list it exactly once per draft.
	got := KeywordsForDraft(Draft202012)
	count := 0
	for _, k := range got {
		if k.Name() == "format" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("Draft 2020-12 'format' count = %d, want 1", count)
	}
}

func TestLookupKeyword(t *testing.T) {
	cases := []struct {
		name      string
		draft     Draft
		wantFound bool
	}{
		{"$id", Draft202012, true},
		{"$id", Draft4, false},
		{"id", Draft4, true},
		{"id", Draft202012, false},
		{"$defs", Draft201909, true},
		{"$defs", Draft7, false},
		{"definitions", Draft7, true},
		{"definitions", Draft202012, false},
		{"prefixItems", Draft202012, true},
		{"prefixItems", Draft201909, false},
		{"$dynamicRef", Draft202012, true},
		{"$dynamicRef", Draft201909, false},
		{"$recursiveRef", Draft201909, true},
		{"$recursiveRef", Draft202012, false},
		{"format", Draft202012, true},
		{"format", Draft4, true},
		{"nonsenseKeyword", Draft202012, false},
		{"$id", DraftUnknown, false},
		{"", Draft202012, false},
	}
	for _, c := range cases {
		k, ok := LookupKeyword(c.name, c.draft)
		if ok != c.wantFound {
			t.Errorf("LookupKeyword(%q, %s) found = %v, want %v",
				c.name, c.draft, ok, c.wantFound)
			continue
		}
		if ok && k.Name() != c.name {
			t.Errorf("LookupKeyword(%q, %s).Name() = %q, want %q",
				c.name, c.draft, k.Name(), c.name)
		}
	}
}

func TestKeywordSinceAndRetired(t *testing.T) {
	// Spot-check a representative keyword in each shape.
	type tc struct {
		name        string
		draft       Draft
		wantSince   Draft
		wantRetired Draft
	}
	cases := []tc{
		{"$id", Draft202012, Draft6, DraftUnknown},
		{"id", Draft4, Draft4, Draft6},
		{"definitions", Draft7, Draft4, Draft201909},
		{"$dynamicRef", Draft202012, Draft202012, DraftUnknown},
		{"additionalItems", Draft7, Draft4, Draft202012},
		{"$recursiveRef", Draft201909, Draft201909, Draft202012},
		{"prefixItems", Draft202012, Draft202012, DraftUnknown},
	}
	for _, c := range cases {
		k, ok := LookupKeyword(c.name, c.draft)
		if !ok {
			t.Errorf("LookupKeyword(%q, %s): not found", c.name, c.draft)
			continue
		}
		if k.SinceDraft() != c.wantSince {
			t.Errorf("%s.SinceDraft = %s, want %s", c.name, k.SinceDraft(), c.wantSince)
		}
		if k.RetiredInDraft() != c.wantRetired {
			t.Errorf("%s.RetiredInDraft = %s, want %s", c.name, k.RetiredInDraft(), c.wantRetired)
		}
	}
}

func TestVocabularyURIsAreUnique(t *testing.T) {
	seen := make(map[string]bool)
	for _, v := range Vocabularies() {
		if seen[v.URI] {
			t.Errorf("duplicate vocabulary URI %q", v.URI)
		}
		seen[v.URI] = true
		if !strings.HasPrefix(v.URI, "https://json-schema.org/draft/2020-12/vocab/") {
			t.Errorf("vocabulary URI %q does not match the 2020-12 prefix", v.URI)
		}
	}
}
