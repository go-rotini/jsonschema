package jsonschema

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// stdDecode decodes data the way decodeInstanceBytes did before the fast
// path: encoding/json with UseNumber, one document, no trailing content.
func stdDecode(data []byte) (any, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return nil, err
	}
	var trailing any
	if err := dec.Decode(&trailing); err == nil {
		return nil, errTrailingContent
	} else if !errors.Is(err, io.EOF) {
		return nil, err
	}
	return v, nil
}

// checkFastDecode fails when decodeJSONFast accepts data but its value is
// not exactly the one encoding/json produces.
func checkFastDecode(t *testing.T, data []byte) bool {
	t.Helper()
	got, count, ok := decodeJSONFastCount(data)
	if !ok {
		return false
	}
	want, err := stdDecode(data)
	if err != nil {
		t.Fatalf("fast decoder accepted %q; encoding/json: %v", data, err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("fast decoder on %q:\n got %#v\nwant %#v", data, got, want)
	}
	if n := countValues(want); n != count {
		t.Fatalf("fast decoder on %q: counted %d values, countValues %d", data, count, n)
	}
	return true
}

var fastDecodeSeeds = []string{
	``, ` `, `null`, `true`, `false`, `0`, `-0`, `01`, `-`, `1.`, `1.5`, `.5`, `1e5`, `1E+5`, `1e-05`, `1e`, `-1.25E-3`,
	`123456789012345678901234567890`, `"a"`, `""`, `"\u00e9"`, `"\ud83d\ude00"`, `"\ud83d"`, `"\ude00"`, `"\ud83dx"`,
	`"\ud83d\u0041"`, `"\u0000"`, `"\uZZZZ"`, `"a\"b\\c\/d\be\ff\ng\rh\ti"`, `"\x"`, "\"\x01\"", "\"\x7f\"",
	"\"\xff\"", "\"\xed\xa0\x80\"", "\"\xef\xbf\xbd\"", `"é—"`, `[]`, `{}`, `[ ]`, `{ }`, `[1,2,3]`, `[1,]`, `[,1]`,
	`{"a":1,"b":[true,false,null],"c":{"d":"e"}}`, `{"a":1,"a":2}`, `{"a":{"x":1},"a":{"y":2}}`, `{"a" : 1 }`,
	`{"a":1,}`, `{a:1}`, `{"a"}`, `[1 2]`, `1 2`, `{} {}`, `[] x`, "\t\n\r [1]\n", "\ufeff{}", `[[[[[]]]]]`,
	`{"":""}`, `{"\u0041":1}`, `{"A":1,"\u0041":2}`, `truex`, `nul`, `[tru]`,
	` {"a" : [1, -2.5e3, 0, true, false, null, "x"]} `, `"é\n\"\\\/"`, `{"a":{"b":{"c":[{}]}}}`, `{"a":1}{}`, `"😀"`, `"é"`,
	`{"$ref":"#/definitions/X","definitions":{"X":{"type":"string","pattern":"^[a-z]+$"}}}`, `{"k":"v"} x`, "\"a\x01\"",
}

func TestDecodeJSONFastMatchesEncodingJSON(t *testing.T) {
	for _, s := range fastDecodeSeeds {
		checkFastDecode(t, []byte(s))
	}
	deep := bytes.Repeat([]byte("["), fastDecodeMaxDepth+10)
	deep = append(deep, bytes.Repeat([]byte("]"), fastDecodeMaxDepth+10)...)
	if checkFastDecode(t, deep) {
		t.Fatal("fast decoder accepted nesting past its limit")
	}
	// Real documents must take the fast path.
	paths, _ := filepath.Glob(filepath.Join(acceptanceFixtureDir, "*.json"))
	more, _ := filepath.Glob(filepath.Join(acceptanceFixtureDir, "instances", "*.json"))
	metas, _ := filepath.Glob(filepath.Join("meta", "*.json"))
	for _, p := range append(append(paths, more...), metas...) {
		data, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		if !checkFastDecode(t, data) {
			t.Errorf("%s: fast decoder declined a valid document", p)
		}
	}
}

func FuzzDecodeJSONFast(f *testing.F) {
	for _, s := range fastDecodeSeeds {
		f.Add([]byte(s))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		checkFastDecode(t, data)
	})
}
