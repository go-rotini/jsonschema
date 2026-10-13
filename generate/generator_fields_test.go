package generate

import (
	"bytes"
	"encoding/json"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/go-rotini/jsonschema"
)

type fieldsSelfEmbed struct {
	*fieldsSelfEmbed
	V int `json:"v"`
}

type fieldsInner struct {
	X int `json:"x"`
}

type fieldsUnexportedEmbed struct {
	fieldsInner
	Y int `json:"y"`
}

type FieldsBase struct {
	Name int `json:"name"`
	Only int `json:"only"`
}

type fieldsShadow struct {
	Name string `json:"name"`
	FieldsBase
}

type FieldsLeft struct {
	Dup  int
	Bare int
}

type FieldsRight struct {
	Dup  string
	Bare string `json:"Bare"`
}

type fieldsAmbiguous struct {
	FieldsLeft
	FieldsRight
}

type fieldsQuoted struct {
	I int64    `json:"i,string"`
	U uint     `json:"u,string"`
	F float64  `json:"f,string"`
	B bool     `json:"b,string"`
	P *int     `json:"p,string"`
	T fieldsTM `json:"t,string"`
}

type fieldsTM int

func (fieldsTM) MarshalText() ([]byte, error) { return []byte("tm"), nil }

type FieldsTree[T any] struct {
	V    T                `json:"v"`
	Kids []*FieldsTree[T] `json:"kids"`
}

// generatedProps returns the property names of the root schema generated
// for v, in order.
func generatedProps(t *testing.T, v any) (map[string]any, []string) {
	t.Helper()
	data, err := Bytes(v)
	if err != nil {
		t.Fatalf("Bytes: %v", err)
	}
	var doc struct {
		Properties json.RawMessage `json:"properties"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	dec := json.NewDecoder(bytes.NewReader(doc.Properties))
	var names []string
	if _, err := dec.Token(); err != nil {
		t.Fatalf("properties of %s: %v", data, err)
	}
	for dec.More() {
		tok, _ := dec.Token()
		names = append(names, tok.(string))
		var skip json.RawMessage
		_ = dec.Decode(&skip)
	}
	var props map[string]any
	_ = json.Unmarshal(doc.Properties, &props)
	return props, names
}

// assertOwnJSONValid checks that encoding/json's output for v validates
// against the schema generated for v.
func assertOwnJSONValid(t *testing.T, v any) {
	t.Helper()
	s, err := Generate(v)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	r, err := s.Validate(data)
	if err != nil {
		t.Fatal(err)
	}
	if !r.Valid {
		b, _ := s.MarshalJSON()
		t.Errorf("%s does not validate against its own schema %s: %v", data, b, r.Errors)
	}
}

func TestGenerateSelfEmbeddedPointer(t *testing.T) {
	_, names := generatedProps(t, fieldsSelfEmbed{})
	if !slices.Equal(names, []string{"v"}) {
		t.Fatalf("properties = %v, want [v]", names)
	}
	assertOwnJSONValid(t, fieldsSelfEmbed{fieldsSelfEmbed: &fieldsSelfEmbed{V: 2}, V: 1})
}

func TestGenerateFieldPromotion(t *testing.T) {
	t.Run("unexported embedded struct", func(t *testing.T) {
		_, names := generatedProps(t, fieldsUnexportedEmbed{})
		if !slices.Equal(names, []string{"x", "y"}) {
			t.Fatalf("properties = %v, want [x y]", names)
		}
		assertOwnJSONValid(t, fieldsUnexportedEmbed{fieldsInner{1}, 2})
	})
	t.Run("shallower field wins", func(t *testing.T) {
		props, names := generatedProps(t, fieldsShadow{})
		if !slices.Equal(names, []string{"name", "only"}) {
			t.Fatalf("properties = %v, want [name only]", names)
		}
		if got := props["name"].(map[string]any)["type"]; got != "string" {
			t.Fatalf("name type = %v, want string", got)
		}
		assertOwnJSONValid(t, fieldsShadow{Name: "a", FieldsBase: FieldsBase{Name: 1}})
	})
	t.Run("ambiguous names drop, tagged name wins", func(t *testing.T) {
		props, names := generatedProps(t, fieldsAmbiguous{})
		if !slices.Equal(names, []string{"Bare"}) {
			t.Fatalf("properties = %v, want [Bare]", names)
		}
		if got := props["Bare"].(map[string]any)["type"]; got != "string" {
			t.Fatalf("Bare type = %v, want string (the tagged field)", got)
		}
		assertOwnJSONValid(t, fieldsAmbiguous{FieldsLeft{1, 2}, FieldsRight{"a", "b"}})
	})
}

func TestGenerateStringOption(t *testing.T) {
	props, _ := generatedProps(t, fieldsQuoted{})
	for _, name := range []string{"i", "u", "f", "b"} {
		if got := props[name].(map[string]any)["type"]; got != "string" {
			t.Errorf("%s type = %v, want string", name, got)
		}
	}
	// A nil pointer marshals to null even under ",string".
	if got := props["p"].(map[string]any)["anyOf"].([]any)[1].(map[string]any)["type"]; got != "string" {
		t.Errorf("p type = %v, want string", got)
	}
	if got := props["t"].(map[string]any)["pattern"]; got != nil {
		t.Errorf("t pattern = %v, want none (,string does not apply to a TextMarshaler)", got)
	}
	n := 7
	assertOwnJSONValid(t, fieldsQuoted{I: -5, U: 3, F: 1.5e300, B: true, P: &n})

	s, err := Generate(fieldsQuoted{})
	if err != nil {
		t.Fatal(err)
	}
	r, err := s.Validate([]byte(`{"i":5,"u":"-1","f":"x","b":"yes"}`))
	if err != nil {
		t.Fatal(err)
	}
	if r.Valid || len(r.Errors) < 4 {
		t.Fatalf("non-quoted forms should fail, got valid=%v errors=%v", r.Valid, r.Errors)
	}
}

func TestGenerateGenericDefName(t *testing.T) {
	v := FieldsTree[json.Number]{V: "1", Kids: []*FieldsTree[json.Number]{{V: "2", Kids: []*FieldsTree[json.Number]{}}}}
	data, err := Bytes(v)
	if err != nil {
		t.Fatalf("Bytes: %v", err)
	}
	var doc struct {
		Defs map[string]any `json:"$defs"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	if _, ok := doc.Defs["FieldsTree_json.Number"]; !ok {
		t.Fatalf("$defs = %v, want key FieldsTree_json.Number", reflect.ValueOf(doc.Defs).MapKeys())
	}
	if _, err := jsonschema.Compile(data); err != nil {
		t.Fatalf("Compile: %v\n%s", err, data)
	}
	assertOwnJSONValid(t, v)
}

func TestDefNameBase(t *testing.T) {
	for in, want := range map[string]string{
		"Plain":                            "Plain",
		"Tree[encoding/json.Number]":       "Tree_json.Number",
		"Pair[example.com/a/b.K,*os.File]": "Pair_b.K_os.File",
		"M[map[string]github.com/x/y.V]":   "M_map_string_y.V",
		"Weird~Name":                       "Weird_Name",
	} {
		if got := defNameBase(in); got != want {
			t.Errorf("defNameBase(%q) = %q, want %q", in, got, want)
		}
	}
}

type fieldsNilable struct {
	List   []string          `json:"list"`
	Map    map[string]int    `json:"map"`
	Bytes  []byte            `json:"bytes"`
	Fixed  [2]int            `json:"fixed"`
	OList  []string          `json:"olist,omitempty"`
	OMap   map[string]int    `json:"omap,omitempty"`
	Nested map[string][]bool `json:"nested"`
}

// TestGenerateNilSlicesAndMaps checks that slice and map schemas allow the
// null encoding/json writes for nil values, except in omitempty fields,
// which leave nil values out.
func TestGenerateNilSlicesAndMaps(t *testing.T) {
	assertOwnJSONValid(t, fieldsNilable{})
	assertOwnJSONValid(t, fieldsNilable{Nested: map[string][]bool{"a": nil}})
	assertOwnJSONValid(t, []string(nil))
	props, _ := generatedProps(t, fieldsNilable{})
	want := map[string]any{
		"list":  []any{"array", "null"},
		"map":   []any{"object", "null"},
		"bytes": []any{"string", "null"},
		"fixed": "array",
		"olist": "array",
		"omap":  "object",
	}
	for name, typ := range want {
		got := props[name].(map[string]any)["type"]
		if !reflect.DeepEqual(got, typ) {
			t.Errorf("%s: type = %v, want %v", name, got, typ)
		}
	}
}

type ptrNode struct {
	Name     string     `json:"name"`
	Next     *ptrNode   `json:"next"`
	Children []*ptrNode `json:"children"`
}

type ptrHolder struct {
	P     *fieldsInner `json:"p"`
	Opt   *fieldsInner `json:"opt,omitempty"`
	Zero  *int         `json:"zero,omitzero"`
	Color *string      `json:"color" jsonschema:"enum=red|green"`
	Tree  ptrNode      `json:"tree"`
}

// TestGenerateNilPointersValidate checks that values with nil pointers
// validate against their own schema: encoding/json writes a nil pointer as
// null, unless omitempty or omitzero leaves it out.
func TestGenerateNilPointersValidate(t *testing.T) {
	red := "red"
	assertOwnJSONValid(t, ptrHolder{})
	assertOwnJSONValid(t, ptrNode{})
	assertOwnJSONValid(t, ptrHolder{
		P:     &fieldsInner{1},
		Color: &red,
		Tree:  ptrNode{Next: &ptrNode{}, Children: []*ptrNode{{Name: "a"}, nil}},
	})
	assertOwnJSONValid(t, &ptrNode{})

	props, _ := generatedProps(t, ptrHolder{})
	for _, name := range []string{"opt", "zero"} {
		if _, ok := props[name].(map[string]any)["anyOf"]; ok {
			t.Errorf("%s allows null, but its nil pointer is omitted", name)
		}
	}
	s := MustGenerate(ptrHolder{})
	if r, _ := s.Validate([]byte(`{"color":"blue"}`)); r.Valid {
		t.Error("enum tag on a pointer no longer applies")
	}
	if r, _ := s.Validate([]byte(`{"opt":null}`)); r.Valid {
		t.Error("omitempty pointer accepts null")
	}
	root, _ := Bytes(&ptrNode{})
	if strings.HasPrefix(string(root), `{"anyOf"`) {
		t.Errorf("root pointer is nullable: %s", root)
	}

	strict := MustGenerate(ptrHolder{}, WithNullablePointers(false))
	if r, _ := strict.Validate([]byte(`{"p":null}`)); r.Valid {
		t.Error("WithNullablePointers(false) accepts null")
	}
	all, _ := Bytes(ptrHolder{}, WithNullablePointers(true))
	if !strings.Contains(string(all), `"opt":{"anyOf"`) {
		t.Errorf("WithNullablePointers(true) leaves omitempty pointers non-null: %s", all)
	}
}

type mapKeyTM struct{ a, b int }

func (k mapKeyTM) MarshalText() ([]byte, error) { return []byte("k"), nil }

type mapKeyHolder struct {
	Ints  map[int]string      `json:"ints"`
	Uints map[uint8]int       `json:"uints"`
	TM    map[mapKeyTM]string `json:"tm"`
}

// TestGenerateMapKeys checks integer and TextMarshaler map keys, which
// encoding/json writes as decimal and MarshalText strings.
func TestGenerateMapKeys(t *testing.T) {
	assertOwnJSONValid(t, mapKeyHolder{
		Ints:  map[int]string{-3: "a", 0: "b"},
		Uints: map[uint8]int{7: 1},
		TM:    map[mapKeyTM]string{{1, 2}: "x"},
	})
	s := MustGenerate(mapKeyHolder{})
	for inst, valid := range map[string]bool{
		`{"ints":{"-12":"a"}}`: true,
		`{"ints":{"x":"a"}}`:   false,
		`{"uints":{"-1":1}}`:   false,
		`{"tm":{"x":"a"}}`:     true,
	} {
		if r, _ := s.Validate([]byte(inst)); r.Valid != valid {
			t.Errorf("%s: valid=%v, want %v", inst, r.Valid, valid)
		}
	}
}
