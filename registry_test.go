package jsonschema

import (
	"sync"
	"testing"
)

// resetKeywordRegistry empties the keyword table as if no schema had been
// compiled yet, and restores the previous state when the test ends.
func resetKeywordRegistry(t *testing.T) {
	t.Helper()
	saved := keywordBuilders
	t.Cleanup(func() {
		keywordBuilders = saved
		keywordBuildersOnce = sync.Once{}
		if saved != nil {
			keywordBuildersOnce.Do(func() {})
		}
	})
	keywordBuilders = nil
	keywordBuildersOnce = sync.Once{}
}

func TestKeywordRegistry_FilledOnFirstLookup(t *testing.T) {
	resetKeywordRegistry(t)
	if keywordBuilders != nil {
		t.Fatal("registry filled before first lookup")
	}
	for _, kw := range []string{"$ref", "allOf", "if", "properties", "items", "type", "enum",
		"multipleOf", "pattern", "uniqueItems", "required", "format", "unevaluatedProperties",
		"discriminator", "$defs"} {
		if _, ok := lookupBuilder(kw); !ok {
			t.Errorf("no builder for %q", kw)
		}
	}
	if _, ok := lookupBuilder("notAKeyword"); ok {
		t.Error("builder registered for an unknown keyword")
	}
}

func TestKeywordRegistry_EarlyRegistrationSurvivesBuiltins(t *testing.T) {
	resetKeywordRegistry(t)
	var calls int
	registerEvaluator("minLength", func(_ *evalBuilder, _ *buildFrame, _ any, loc string) (evaluator, error) {
		calls++
		return &lengthEval{loc: loc, kw: "minLength", bound: 100}, nil
	})

	s, err := Compile([]byte(`{"minLength": 1, "maxLength": 2}`))
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("early builder called %d times, want 1", calls)
	}
	res, err := s.Validate([]byte(`"ab"`))
	if err != nil {
		t.Fatal(err)
	}
	if res.Valid {
		t.Error(`"ab" valid: the early minLength builder was overwritten by the built-in`)
	}
	res, err = s.Validate([]byte(`"abc"`))
	if err != nil {
		t.Fatal(err)
	}
	if res.Valid {
		t.Error(`"abc" valid: the built-in maxLength builder is missing`)
	}
}
