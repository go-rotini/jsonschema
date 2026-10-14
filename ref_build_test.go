package jsonschema

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type countingLoader struct {
	m MapLoader
	n atomic.Int64
}

func (c *countingLoader) Load(uri string) ([]byte, error) {
	c.n.Add(1)
	return c.m.Load(uri)
}

// TestRemoteRefsResolvedAtCompile checks that Compile fetches every
// document a schema can reach, so concurrent validations never call the
// loader or change shared state (run with -race).
func TestRemoteRefsResolvedAtCompile(t *testing.T) {
	l := &countingLoader{m: MapLoader{}}
	const n = 20
	for i := range n {
		next := `true`
		if i < n-1 {
			next = `{"$ref":"http://x/` + itoa(i+1) + `.json"}`
		}
		l.m["http://x/"+itoa(i)+".json"] = []byte(`{"type":"object","properties":{"p":` + next + `}}`)
	}
	s, err := Compile([]byte(`{"$ref":"http://x/0.json"}`), WithLoader(l))
	if err != nil {
		t.Fatal(err)
	}
	if got := l.n.Load(); got != n {
		t.Fatalf("fetches during compile = %d, want %d", got, n)
	}
	inst := strings.Repeat(`{"p":`, n) + `1` + strings.Repeat(`}`, n)
	var wg sync.WaitGroup
	for range 16 {
		wg.Go(func() {
			if r, err := s.Validate([]byte(inst)); err != nil || !r.Valid {
				t.Errorf("err=%v r=%+v", err, r)
			}
		})
	}
	wg.Wait()
	if got := l.n.Load(); got != n {
		t.Fatalf("fetches after validation = %d, want %d", got, n)
	}
}

// TestUnresolvableRefFailsReachingInstances checks that a $ref that cannot
// be resolved, or whose target cannot be built, compiles and fails only the
// instances that reach it, without calling the loader during validation.
func TestUnresolvableRefFailsReachingInstances(t *testing.T) {
	l := &countingLoader{m: MapLoader{
		"http://x/a.json": []byte(`{"$ref":"missing.json"}`),
	}}
	s, err := Compile([]byte(`{"properties":{
		"a":{"$ref":"http://x/a.json"},
		"b":{"$ref":"#/$defs/look"}},
		"$defs":{"look":{"pattern":"^(?!0)[0-9]+$"}}}`), WithLoader(l))
	if err != nil {
		t.Fatal(err)
	}
	fetched := l.n.Load()
	if res, err := s.Validate([]byte(`{"x":1}`)); err != nil || !res.Valid {
		t.Fatalf("instance reaching no reference: res=%+v err=%v", res, err)
	}
	for _, inst := range []string{`{"a":1}`, `{"b":"1"}`} {
		res, err := s.Validate([]byte(inst))
		if err != nil || res.Valid || len(res.Errors) != 1 || res.Errors[0].Keyword != "$ref" {
			t.Fatalf("%s: res=%+v err=%v", inst, res, err)
		}
	}
	res, _ := s.Validate([]byte(`{"a":1}`))
	if re, ok := errors.AsType[*RefError](&res.Errors[0]); !ok || re.BaseURI != "http://x/a.json" {
		t.Fatalf("cause = %v, want a RefError against the fetched document's base", res.Errors[0].Cause)
	}
	res, _ = s.Validate([]byte(`{"b":"1"}`))
	if !errors.Is(&res.Errors[0], errECMAUnsupported) {
		t.Fatalf("cause = %v, want the unsupported-pattern error", res.Errors[0].Cause)
	}
	if got := l.n.Load(); got != fetched {
		t.Fatalf("validation fetched %d documents", got-fetched)
	}
}

// TestLegacyPlainNameIDNextToRef checks that a Draft 7 plain-name $id next
// to $ref still names that schema, as schemas such as NIST OSCAL expect.
func TestLegacyPlainNameIDNextToRef(t *testing.T) {
	const schema = `{"$schema":"http://json-schema.org/draft-07/schema#",
		"properties":{"v":{"$ref":"#value"}},
		"definitions":{"value":{"$id":"#value","$ref":"#/definitions/str"},"str":{"type":"string"}}}`
	if !validOf(t, schema, `{"v":"a"}`) || validOf(t, schema, `{"v":1}`) {
		t.Error("plain-name $id next to $ref not honoured")
	}
}

// TestRelativeIDEnteredThroughRef checks that a relative $id with a folder
// is applied once when its schema is entered through $ref.
func TestRelativeIDEnteredThroughRef(t *testing.T) {
	const schema = `{"$id":"https://ex.com/root/base.json","$ref":"sub/item.json","$defs":{
		"item":{"$id":"sub/item.json","$ref":"other.json"},
		"other":{"$id":"sub/other.json","type":"number"}}}`
	if !validOf(t, schema, `5`) {
		t.Error("5 rejected")
	}
	s := MustCompile([]byte(schema))
	res, err := s.Validate([]byte(`"a"`))
	if err != nil || res.Valid {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	if got := res.Errors[0].AbsoluteKeywordLocation; got != "https://ex.com/root/sub/other.json#/type" {
		t.Errorf("AbsoluteKeywordLocation = %q", got)
	}
}

// TestLegacyRefIgnoresSiblings checks that before 2019-09 $ref overrides
// its sibling keywords, $id included.
func TestLegacyRefIgnoresSiblings(t *testing.T) {
	const d7 = `"$schema":"http://json-schema.org/draft-07/schema#",`
	if !validOf(t, `{`+d7+`"definitions":{"a":{"type":"array"}},"$ref":"#/definitions/a","maxItems":1}`, `[1,2,3]`) {
		t.Error("draft 7: maxItems next to $ref was applied")
	}
	if validOf(t, `{"$defs":{"a":{"type":"array"}},"$ref":"#/$defs/a","maxItems":1}`, `[1,2,3]`) {
		t.Error("2020-12: maxItems next to $ref was ignored")
	}
	const sibling = `{` + d7 + `"$id":"http://localhost:1234/sibling_id/base/",
		"definitions":{"foo":{"$id":"http://localhost:1234/sibling_id/foo.json","type":"string"},
			"base_foo":{"$id":"foo.json","type":"number"}},
		"allOf":[{"$id":"http://localhost:1234/sibling_id/","$ref":"foo.json"}]}`
	if !validOf(t, sibling, `1`) || validOf(t, sibling, `"a"`) {
		t.Error("draft 7: a sibling $id changed the base of $ref")
	}
}

// TestRefDepthCountsHopsPerInstanceLocation checks that the $ref depth
// limit counts reference hops at one instance location, so deep data that
// a recursive schema follows one level at a time is not cut off.
func TestRefDepthCountsHopsPerInstanceLocation(t *testing.T) {
	s := MustCompile([]byte(`{"$defs":{"n":{"type":"array","items":{"$ref":"#/$defs/n"}}},"$ref":"#/$defs/n"}`))
	inst := strings.Repeat("[", 500) + strings.Repeat("]", 500)
	res, err := s.Validate([]byte(inst))
	if err != nil || !res.Valid {
		t.Fatalf("500-deep instance: res=%+v err=%v", res, err)
	}
	loop := MustCompile([]byte(`{"$defs":{"a":{"$ref":"#/$defs/b"},"b":{"$ref":"#/$defs/a"}},"$ref":"#/$defs/a"}`))
	res, err = loop.Validate([]byte(`1`))
	if err != nil || res.Valid || !errors.Is(&res.Errors[0], ErrMaxRefDepth) {
		t.Fatalf("ref loop: res=%+v err=%v", res, err)
	}
}

// TestErrorOrderIsStable checks that errors come out in the same order on
// every run.
func TestErrorOrderIsStable(t *testing.T) {
	s := MustCompile([]byte(`{"properties":{"a":{"type":"string"},"b":{"type":"string"},"c":{"type":"string"},
		"d":{"type":"string"},"e":{"type":"string"}},"patternProperties":{"^x":{"type":"string"}},
		"additionalProperties":{"type":"string"},"propertyNames":{"maxLength":3},
		"dependentRequired":{"a":["q1"],"b":["q2"],"c":["q3"]}}`))
	inst := []byte(`{"a":1,"b":2,"c":3,"d":4,"e":5,"x1":6,"x2":7,"z1":8,"z2":9,"long1":1,"long2":2}`)
	var first string
	for i := range 50 {
		res, err := s.Validate(inst)
		if err != nil {
			t.Fatal(err)
		}
		var sb strings.Builder
		for _, e := range res.Errors {
			fmt.Fprintf(&sb, "%s %s %s\n", e.KeywordLocation, e.InstanceLocation, e.Message)
		}
		if i == 0 {
			first = sb.String()
		} else if sb.String() != first {
			t.Fatalf("run %d order differs:\n%s\nvs\n%s", i, sb.String(), first)
		}
	}
}

// TestFetchedDocumentKeepsItsDraft checks that a fetched document is
// evaluated by the draft its own $schema names, not the referring schema's.
func TestFetchedDocumentKeepsItsDraft(t *testing.T) {
	l := MapLoader{"http://x/d7.json": []byte(`{"$schema":"http://json-schema.org/draft-07/schema#",
		"definitions":{"tuple":{"type":"array","items":[{"type":"string"}],"additionalItems":false}}}`)}
	s, err := Compile([]byte(`{"$ref":"http://x/d7.json#/definitions/tuple"}`), WithLoader(l))
	if err != nil {
		t.Fatal(err)
	}
	for inst, want := range map[string]bool{`["a"]`: true, `[1]`: false, `["a","b"]`: false} {
		res, err := s.Validate([]byte(inst))
		if err != nil || res.Valid != want {
			t.Errorf("%s: res=%+v err=%v, want valid=%v", inst, res, err, want)
		}
	}
}

// TestCustomMetaSchemaVocabulary checks that a custom meta-schema leaving
// out the validation vocabulary switches off its keywords.
func TestCustomMetaSchemaVocabulary(t *testing.T) {
	l := MapLoader{"http://x/meta.json": []byte(`{"$schema":"https://json-schema.org/draft/2020-12/schema",
		"$id":"http://x/meta.json","$vocabulary":{
			"https://json-schema.org/draft/2020-12/vocab/applicator":true,
			"https://json-schema.org/draft/2020-12/vocab/core":true}}`)}
	s, err := Compile([]byte(`{"$schema":"http://x/meta.json","properties":{"bad":false,"n":{"minimum":10}}}`), WithLoader(l))
	if err != nil {
		t.Fatal(err)
	}
	for inst, want := range map[string]bool{`{"n":1}`: true, `{"bad":1}`: false} {
		res, err := s.Validate([]byte(inst))
		if err != nil || res.Valid != want {
			t.Errorf("%s: res=%+v err=%v, want valid=%v", inst, res, err, want)
		}
	}
}

// TestDynamicRefWithoutMatchingAnchorIsStatic checks that a $dynamicRef
// whose initial target has no matching $dynamicAnchor behaves like $ref.
func TestDynamicRefWithoutMatchingAnchorIsStatic(t *testing.T) {
	const schema = `{"$id":"https://ex.com/root","$ref":"list",
		"$defs":{"foo":{"$dynamicAnchor":"items","type":"string"},
		"list":{"$id":"list","type":"array","items":{"$dynamicRef":"#items"},
			"$defs":{"items":{"$anchor":"items","$dynamicAnchor":"foo"}}}}}`
	if !validOf(t, schema, `["a",1]`) {
		t.Error("the $dynamicRef followed the dynamic scope")
	}
}

// TestValidateTakesNoBuilderLock checks that validation follows $ref,
// $dynamicRef and $recursiveRef to the subschemas Compile built without
// touching the builder's lock, so parallel validations do not serialize.
func TestValidateTakesNoBuilderLock(t *testing.T) {
	for _, src := range []string{
		`{"$id":"https://example.com/t","$ref":"#/$defs/n","$defs":{"n":{"type":"object","properties":{"c":{"type":"array","items":{"$ref":"#/$defs/n"}}}}}}`,
		`{"$id":"https://example.com/d","$dynamicAnchor":"x","type":"object","properties":{"c":{"$dynamicRef":"#x"}}}`,
		`{"$schema":"https://json-schema.org/draft/2019-09/schema","$id":"https://example.com/r","$recursiveAnchor":true,"type":"object","properties":{"c":{"$recursiveRef":"#"}}}`,
	} {
		s := MustCompile([]byte(src))
		b := findBuilder(s.root)
		if b == nil {
			t.Fatalf("%s: no reference evaluator", src)
		}
		b.cacheMu.Lock()
		done := make(chan error, 1)
		go func() {
			_, err := s.Validate([]byte(`{"c":[{"c":[]}]}`))
			if err == nil {
				_, err = s.Validate([]byte(`{"c":{"c":{}}}`))
			}
			done <- err
		}()
		select {
		case err := <-done:
			if err != nil {
				t.Fatal(err)
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("%s: validation waited on the builder lock", src)
		}
		b.cacheMu.Unlock()
	}
}

func findBuilder(sub *subschema) *evalBuilder {
	for _, ev := range sub.evaluators {
		switch e := ev.(type) {
		case *refEval:
			return e.builder
		case *dynamicRefEval:
			return e.builder
		case *recursiveRefEval:
			return e.builder
		case *propertiesEval:
			for _, c := range e.subs {
				if b := findBuilder(c); b != nil {
					return b
				}
			}
		}
	}
	return nil
}

// TestCustomMetaSchemaVocabularyByDraft checks that a custom meta-schema's
// $vocabulary is read by full URI per draft: 2019-09's applicator covers
// the unevaluated* keywords and its format vocabulary is "format";
// format-assertion makes format assert; an unknown required vocabulary
// fails Compile.
func TestCustomMetaSchemaVocabularyByDraft(t *testing.T) {
	metas := MapLoader{
		"http://m/full-2019.json": []byte(`{"$schema":"https://json-schema.org/draft/2019-09/schema","$id":"http://m/full-2019.json",
		 "$vocabulary":{"https://json-schema.org/draft/2019-09/vocab/core":true,"https://json-schema.org/draft/2019-09/vocab/applicator":true,
		 "https://json-schema.org/draft/2019-09/vocab/validation":true,"https://json-schema.org/draft/2019-09/vocab/meta-data":true,
		 "https://json-schema.org/draft/2019-09/vocab/format":false,"https://json-schema.org/draft/2019-09/vocab/content":true},
		 "$recursiveAnchor":true,"allOf":[{"$ref":"https://json-schema.org/draft/2019-09/schema"}]}`),
		"http://m/fa-2020.json": []byte(`{"$schema":"https://json-schema.org/draft/2020-12/schema","$id":"http://m/fa-2020.json",
		 "$vocabulary":{"https://json-schema.org/draft/2020-12/vocab/core":true,"https://json-schema.org/draft/2020-12/vocab/applicator":true,
		 "https://json-schema.org/draft/2020-12/vocab/unevaluated":true,"https://json-schema.org/draft/2020-12/vocab/validation":true,
		 "https://json-schema.org/draft/2020-12/vocab/format-assertion":true},
		 "$dynamicAnchor":"meta","allOf":[{"$ref":"https://json-schema.org/draft/2020-12/schema"}]}`),
		"http://m/unknown.json": []byte(`{"$schema":"https://json-schema.org/draft/2020-12/schema","$id":"http://m/unknown.json",
		 "$vocabulary":{"https://json-schema.org/draft/2020-12/vocab/core":true,"https://example.com/vocab/x":true},
		 "$dynamicAnchor":"meta","allOf":[{"$ref":"https://json-schema.org/draft/2020-12/schema"}]}`),
		"http://m/optional.json": []byte(`{"$schema":"https://json-schema.org/draft/2020-12/schema","$id":"http://m/optional.json",
		 "$vocabulary":{"https://json-schema.org/draft/2020-12/vocab/core":true,"https://json-schema.org/draft/2020-12/vocab/validation":true,
		 "https://example.com/vocab/x":false},
		 "$dynamicAnchor":"meta","allOf":[{"$ref":"https://json-schema.org/draft/2020-12/schema"}]}`),
	}
	l := WithLoader(ChainLoader{metas, DefaultLoader()})
	for _, tc := range []struct {
		schema, inst string
		opts         []Option
		valid        bool
	}{
		{`{"$schema":"http://m/full-2019.json","properties":{"a":true},"unevaluatedProperties":false}`, `{"a":1,"b":2}`, nil, false},
		{`{"$schema":"http://m/full-2019.json","prefixItems":[true],"unevaluatedItems":false}`, `[1,2]`, nil, false},
		{`{"$schema":"http://m/full-2019.json","format":"ipv4"}`, `"nope"`, []Option{WithFormatAssertion(true)}, false},
		{`{"$schema":"http://m/fa-2020.json","format":"ipv4"}`, `"nope"`, nil, false},
		{`{"$schema":"http://m/fa-2020.json","format":"ipv4"}`, `"1.2.3.4"`, nil, true},
		{`{"$schema":"http://m/optional.json","type":"string","properties":{"a":false}}`, `{"a":1}`, nil, false},
		{`{"$schema":"http://m/optional.json","properties":{"a":false}}`, `{"a":1}`, nil, true},
	} {
		s, err := Compile([]byte(tc.schema), l)
		if err != nil {
			t.Fatalf("%s: %v", tc.schema, err)
		}
		res, err := s.Validate([]byte(tc.inst), tc.opts...)
		if err != nil || res.Valid != tc.valid {
			t.Errorf("%s with %s: valid=%v err=%v; want %v", tc.schema, tc.inst, res.Valid, err, tc.valid)
		}
	}
	_, err := Compile([]byte(`{"$schema":"http://m/unknown.json","type":"string"}`), l)
	if !errors.Is(err, ErrUnknownVocabulary) {
		t.Errorf("unknown required vocabulary: err=%v", err)
	}
}
