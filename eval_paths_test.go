package jsonschema

import (
	"net/url"
	"strings"
	"testing"
)

// eagerInstancePath is the instance-location bookkeeping the validator used
// before pointers were built lazily: the pointer is extended on every push.
type eagerInstancePath struct {
	ptr   string
	stack []string
}

func (p *eagerInstancePath) push(tok string) {
	p.stack = append(p.stack, p.ptr)
	p.ptr += "/" + escapePointerToken(tok)
}

func (p *eagerInstancePath) pop() {
	if n := len(p.stack); n > 0 {
		p.ptr = p.stack[n-1]
		p.stack = p.stack[:n-1]
	}
}

// FuzzInstanceLocation drives the lazy instance path and the eager one
// with the same push / pop / read sequence and requires the same pointer
// at every read.
func FuzzInstanceLocation(f *testing.F) {
	f.Add([]byte{0, 1, 2, 3, 4, 5}, "a/b~c")
	f.Add([]byte{9, 9, 3, 8, 3, 3, 3, 0, 1}, "")
	f.Add([]byte{7, 200, 3, 255, 3, 3}, "é x")
	f.Fuzz(func(t *testing.T, ops []byte, key string) {
		var ctx runCtx
		var eager eagerInstancePath
		for i, op := range ops {
			switch op % 4 {
			case 0:
				k := key
				if i%2 == 1 && len(k) > 0 {
					k = k[:len(k)/2]
				}
				ctx.pushInstance(k)
				eager.push(k)
			case 1:
				idx := int(op) * (i + 1)
				ctx.pushIndex(idx)
				eager.push(itoaInt(idx))
			case 2:
				ctx.popInstance()
				eager.pop()
			case 3:
				if got := ctx.instanceLocation(); got != eager.ptr {
					t.Fatalf("op %d: instanceLocation %q, eager %q", i, got, eager.ptr)
				}
			}
		}
		if got := ctx.instanceLocation(); got != eager.ptr {
			t.Fatalf("end: instanceLocation %q, eager %q", got, eager.ptr)
		}
	})
}

// oldAbsoluteKeywordLocation is absoluteKeywordLocation as it was before the
// escaped prefix was kept on the subschema.
func oldAbsoluteKeywordLocation(sub *subschema, keywordLoc string) string {
	if sub == nil || sub.absURI == "" {
		return ""
	}
	rest, ok := strings.CutPrefix(keywordLoc, sub.location)
	if !ok || (rest != "" && rest[0] != '/') {
		return ""
	}
	return sub.absURI + "#" + (&url.URL{Fragment: sub.absPtr + rest}).EscapedFragment()
}

func FuzzAbsoluteKeywordLocation(f *testing.F) {
	f.Add("https://example.com/s.json", "/$defs/a b", "#/$defs/a b", "#/$defs/a b/properties/%x/type")
	f.Add("urn:x", "", "#", "#/items/0")
	f.Add("https://e.com/x", "/p", "#/p", "#/q")
	f.Add("", "/p", "#/p", "#/p/type")
	f.Fuzz(func(t *testing.T, absURI, absPtr, loc, keywordLoc string) {
		for _, withPrefix := range []bool{false, true} {
			sub := &subschema{location: loc, absURI: absURI, absPtr: absPtr}
			if withPrefix && absURI != "" {
				sub.absPrefix = absURI + "#" + escapeFragment(absPtr)
			}
			want := oldAbsoluteKeywordLocation(sub, keywordLoc)
			if got := absoluteKeywordLocation(sub, keywordLoc); got != want {
				t.Fatalf("absoluteKeywordLocation = %q, old %q", got, want)
			}
			head, tail := absoluteKeywordLocationParts(sub, keywordLoc)
			if head+tail != want {
				t.Fatalf("parts = %q + %q, old %q", head, tail, want)
			}
		}
	})
}

// oldPublicAnnotations is publicAnnotations as it was before the keyword
// locations shared one string.
func oldPublicAnnotations(ctx *runCtx) []Annotation {
	out := make([]Annotation, 0, len(ctx.annoEntries))
	for _, e := range ctx.annoEntries {
		path := e.keywordLoc
		if e.pathCut > 0 {
			path = e.pathPrefix + e.keywordLoc[e.pathCut:]
		}
		out = append(out, Annotation{
			KeywordLocation:         path,
			AbsoluteKeywordLocation: oldAbsoluteKeywordLocation(e.sub, e.keywordLoc),
			InstanceLocation:        e.instanceLoc,
			Keyword:                 e.keyword,
			Value:                   sanitizeAnnotation(e.value),
		})
	}
	return out
}

func TestPublicAnnotationsMatchesOld(t *testing.T) {
	cases := []struct{ schema, instance string }{
		{refBenchSchema, `{"name":"a","children":[{"name":"b","children":[{"name":"c"}]},{"name":"d"}]}`},
		{`{"$id":"https://e.com/a b.json","properties":{"x y":{"$ref":"#/$defs/d"}},"patternProperties":{"^p":{"title":"t"}},"additionalProperties":{"description":"d"},"$defs":{"d":{"type":"string","format":"email","default":"q"}}}`,
			`{"x y":"a@b.co","p1":1,"p2":2,"other":true}`},
		{`{"prefixItems":[{"title":"a"}],"items":{"title":"b"},"contains":{"type":"integer"},"unevaluatedItems":false}`, `["x",1,2,3]`},
		{`{"allOf":[{"properties":{"a":{"title":"x"}}}],"anyOf":[{"properties":{"b":true}},{"required":["b"]}],"unevaluatedProperties":false}`, `{"a":1,"b":2}`},
	}
	for _, c := range cases {
		s := MustCompile([]byte(c.schema))
		v, err := decodeInstanceBytes([]byte(c.instance))
		if err != nil {
			t.Fatal(err)
		}
		ctx := newRunCtx(s, defaultRunOptions())
		ctx.setEvalLimit(v)
		ctx.evaluate(s.evalRoot(), v)
		got, want := ctx.publicAnnotations(), oldPublicAnnotations(ctx)
		if len(want) == 0 || len(got) != len(want) {
			t.Fatalf("%s: %d annotations, old %d", c.schema, len(got), len(want))
		}
		for i := range want {
			g, w := got[i], want[i]
			if g.KeywordLocation != w.KeywordLocation || g.AbsoluteKeywordLocation != w.AbsoluteKeywordLocation ||
				g.InstanceLocation != w.InstanceLocation || g.Keyword != w.Keyword || !equalAnnotationValue(g.Value, w.Value) {
				t.Fatalf("%s: annotation %d = %+v, old %+v", c.schema, i, g, w)
			}
		}
	}
}

func equalAnnotationValue(a, b any) bool {
	as, aok := a.([]string)
	bs, bok := b.([]string)
	if aok || bok {
		return aok && bok && strings.Join(as, "\x00") == strings.Join(bs, "\x00") && len(as) == len(bs)
	}
	return a == b
}
