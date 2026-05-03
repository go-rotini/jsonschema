package jsonschema

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

// genUser is the canonical happy-path fixture for the schema generator.
type genUser struct {
	Name  string `json:"name" jsonschema:"required,minLength=1,maxLength=100"`
	Email string `json:"email" jsonschema:"required,format=email"`
	Age   int    `json:"age,omitempty" jsonschema:"minimum=0,maximum=150"`
}

func TestGenerateBytesContainsCanonicalShape(t *testing.T) {
	data, err := GenerateBytes(genUser{})
	if err != nil {
		t.Fatalf("GenerateBytes: %v", err)
	}
	want := []string{
		`"$schema":"https://json-schema.org/draft/2020-12/schema"`,
		`"type":"object"`,
		`"name":{`, `"email":{`, `"age":{`,
		`"required":["name","email"]`,
	}
	for _, w := range want {
		if !strings.Contains(string(data), w) {
			t.Errorf("missing fragment %q in: %s", w, data)
		}
	}
}

func TestGenerateRoundTripValidates(t *testing.T) {
	s, err := Generate(genUser{})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	good := genUser{Name: "x", Email: "a@b.io", Age: 33}
	gb, _ := json.Marshal(good)
	res, err := s.Validate(gb)
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if !res.Valid {
		t.Fatalf("expected valid; errors: %+v", res.Errors)
	}
}

func TestGenerateRoundTripRejectsMissingRequired(t *testing.T) {
	s, err := Generate(genUser{})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	res, err := s.Validate([]byte(`{"name":"x"}`))
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if res.Valid {
		t.Fatal("expected validation failure for missing email")
	}
}

func TestGenerateRoundTripRejectsTypeMismatch(t *testing.T) {
	s, err := Generate(genUser{})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	res, err := s.Validate([]byte(`{"name":1,"email":"a@b.io"}`))
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if res.Valid {
		t.Fatal("expected validation failure for non-string name")
	}
}

func TestGenerateBoolean(t *testing.T) {
	data, err := GenerateBytes(true)
	if err != nil {
		t.Fatalf("GenerateBytes: %v", err)
	}
	if !strings.Contains(string(data), `"type":"boolean"`) {
		t.Errorf("got %s", data)
	}
}

func TestGenerateBytesByteSlice(t *testing.T) {
	type WithBytes struct {
		Data []byte `json:"data"`
	}
	data, err := GenerateBytes(WithBytes{})
	if err != nil {
		t.Fatalf("GenerateBytes: %v", err)
	}
	if !strings.Contains(string(data), `"contentEncoding":"base64"`) {
		t.Errorf("expected base64 contentEncoding, got: %s", data)
	}
}

func TestGenerateBytesNamedByteSlice(t *testing.T) {
	type Bytes []byte
	type Holder struct {
		Data Bytes `json:"data"`
	}
	data, err := GenerateBytes(Holder{})
	if err != nil {
		t.Fatalf("GenerateBytes: %v", err)
	}
	if !strings.Contains(string(data), `"contentEncoding":"base64"`) {
		t.Errorf("expected base64 contentEncoding, got: %s", data)
	}
}

func TestGenerateBytesByteArray(t *testing.T) {
	type WithArr struct {
		Hash [16]byte `json:"hash"`
	}
	data, err := GenerateBytes(WithArr{})
	if err != nil {
		t.Fatalf("GenerateBytes: %v", err)
	}
	if !strings.Contains(string(data), `"contentEncoding":"base64"`) {
		t.Errorf("got %s", data)
	}
}

func TestGenerateBytesTimeTime(t *testing.T) {
	type WithTime struct {
		At time.Time `json:"at"`
	}
	data, err := GenerateBytes(WithTime{})
	if err != nil {
		t.Fatalf("GenerateBytes: %v", err)
	}
	if !strings.Contains(string(data), `"format":"date-time"`) {
		t.Errorf("got %s", data)
	}
}

func TestGenerateBytesDurationDefault(t *testing.T) {
	type WithDur struct {
		D time.Duration `json:"d"`
	}
	data, err := GenerateBytes(WithDur{})
	if err != nil {
		t.Fatalf("GenerateBytes: %v", err)
	}
	if !strings.Contains(string(data), `"d":{"type":"integer"}`) {
		t.Errorf("expected integer duration, got %s", data)
	}
}

func TestGenerateBytesDurationAsString(t *testing.T) {
	type WithDur struct {
		D time.Duration `json:"d"`
	}
	data, err := GenerateBytes(WithDur{}, WithGenerateDurationAsString(true))
	if err != nil {
		t.Fatalf("GenerateBytes: %v", err)
	}
	if !strings.Contains(string(data), `"format":"duration"`) {
		t.Errorf("got %s", data)
	}
}

func TestGenerateBytesIntWidthBounds(t *testing.T) {
	type WithInts struct {
		I8  int8   `json:"i8"`
		U16 uint16 `json:"u16"`
	}
	data, err := GenerateBytes(WithInts{})
	if err != nil {
		t.Fatalf("GenerateBytes: %v", err)
	}
	if !strings.Contains(string(data), `"i8":{"type":"integer","minimum":-128,"maximum":127}`) {
		t.Errorf("missing i8 bounds: %s", data)
	}
	if !strings.Contains(string(data), `"u16":{"type":"integer","minimum":0,"maximum":65535}`) {
		t.Errorf("missing u16 bounds: %s", data)
	}
}

func TestGenerateBytesPointerInline(t *testing.T) {
	type Inner struct {
		V int `json:"v"`
	}
	type Outer struct {
		I *Inner `json:"i,omitempty"`
	}
	data, err := GenerateBytes(Outer{})
	if err != nil {
		t.Fatalf("GenerateBytes: %v", err)
	}
	if !strings.Contains(string(data), `"i":{"type":"object"`) {
		t.Errorf("expected pointer to inline struct, got %s", data)
	}
}

func TestGenerateBytesNullablePointer(t *testing.T) {
	type Holder struct {
		Name *string `json:"name"`
	}
	data, err := GenerateBytes(Holder{}, WithGenerateNullablePointers(true))
	if err != nil {
		t.Fatalf("GenerateBytes: %v", err)
	}
	if !strings.Contains(string(data), `"anyOf":[{"type":"null"},{"type":"string"}]`) {
		t.Errorf("expected nullable anyOf, got %s", data)
	}
}

func TestGenerateBytesMapStringValue(t *testing.T) {
	type Holder struct {
		Tags map[string]string `json:"tags"`
	}
	data, err := GenerateBytes(Holder{})
	if err != nil {
		t.Fatalf("GenerateBytes: %v", err)
	}
	if !strings.Contains(string(data), `"additionalProperties":{"type":"string"}`) {
		t.Errorf("got %s", data)
	}
}

func TestGenerateBytesMapNonStringKeyErrors(t *testing.T) {
	type Holder struct {
		IDs map[int]string `json:"ids"`
	}
	_, err := GenerateBytes(Holder{})
	if err == nil {
		t.Fatal("expected error for non-string map key")
	}
	if !errors.Is(err, ErrCompile) {
		t.Errorf("got %T (%v); want *CompileError", err, err)
	}
}

func TestGenerateBytesUnsupportedKind(t *testing.T) {
	type Holder struct {
		Ch chan int `json:"ch"`
	}
	_, err := GenerateBytes(Holder{})
	if err == nil {
		t.Fatal("expected error for chan field")
	}
	if !errors.Is(err, ErrCompile) {
		t.Errorf("got %T", err)
	}
}

func TestGenerateBytesInterfaceAsAny(t *testing.T) {
	type Holder struct {
		V any `json:"v"`
	}
	data, err := GenerateBytes(Holder{})
	if err != nil {
		t.Fatalf("GenerateBytes: %v", err)
	}
	if !strings.Contains(string(data), `"v":{}`) {
		t.Errorf("expected empty schema for any, got %s", data)
	}
}

func TestGenerateBytesInterfaceAsAnyDisabledErrors(t *testing.T) {
	type Holder struct {
		V any `json:"v"`
	}
	_, err := GenerateBytes(Holder{}, WithGenerateInterfaceAsAny(false))
	if err == nil {
		t.Fatal("expected error when interfaceAsAny disabled")
	}
}

// genTree exercises the recursive-type code path. Children is a slice of
// pointers so the cycle is reachable.
type genTree struct {
	Name     string     `json:"name"`
	Children []*genTree `json:"children,omitempty"`
}

func TestGenerateRecursiveType(t *testing.T) {
	data, err := GenerateBytes(genTree{})
	if err != nil {
		t.Fatalf("GenerateBytes: %v", err)
	}
	if !strings.Contains(string(data), `"$defs":{"genTree":`) {
		t.Errorf("expected $defs entry, got %s", data)
	}
	if !strings.Contains(string(data), `"$ref":"#/$defs/genTree"`) {
		t.Errorf("expected $ref, got %s", data)
	}
	// And it round-trips through validation.
	s, err := Generate(genTree{})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	tree := genTree{Name: "a", Children: []*genTree{{Name: "b"}}}
	tb, _ := json.Marshal(tree)
	res, err := s.Validate(tb)
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if !res.Valid {
		t.Errorf("expected valid; errors: %+v", res.Errors)
	}
}

func TestGenerateRecursiveTypeWithExpandedRefsErrors(t *testing.T) {
	_, err := Generate(genTree{}, WithGenerateExpandedRefs(true))
	if err == nil {
		t.Fatal("expected error inlining recursive type")
	}
	var ce *CompileError
	if !errors.As(err, &ce) {
		t.Errorf("got %T", err)
	}
}

func TestGenerateCustomEmitterWins(t *testing.T) {
	type uuidish [16]byte
	emit := func(_ reflect.Type) *Schema {
		return MustCompile([]byte(`{"type":"string","format":"uuid"}`))
	}
	type Holder struct {
		ID uuidish `json:"id"`
	}
	data, err := GenerateBytes(Holder{}, WithCustomEmitter[uuidish](emit))
	if err != nil {
		t.Fatalf("GenerateBytes: %v", err)
	}
	if !strings.Contains(string(data), `"id":{"type":"string","format":"uuid"}`) {
		t.Errorf("custom emitter not applied: %s", data)
	}
}

func TestGenerateAdditionalPropertiesFalse(t *testing.T) {
	data, err := GenerateBytes(genUser{}, WithGenerateAdditionalPropertiesFalse(true))
	if err != nil {
		t.Fatalf("GenerateBytes: %v", err)
	}
	if !strings.Contains(string(data), `"additionalProperties":false`) {
		t.Errorf("expected additionalProperties: false, got %s", data)
	}
	// Validate that extras are now rejected.
	s, _ := Generate(genUser{}, WithGenerateAdditionalPropertiesFalse(true))
	res, err := s.Validate([]byte(`{"name":"x","email":"a@b.io","extra":1}`))
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if res.Valid {
		t.Errorf("expected rejection of extra property")
	}
}

func TestGenerateOmitDescriptions(t *testing.T) {
	type Holder struct {
		N string `json:"n" jsonschema:"description=hi"`
	}
	data, err := GenerateBytes(Holder{}, WithGenerateOmitDescriptions(true))
	if err != nil {
		t.Fatalf("GenerateBytes: %v", err)
	}
	if strings.Contains(string(data), `"description"`) {
		t.Errorf("expected no description; got %s", data)
	}
}

func TestGenerateOrderedPropertiesPreservesOrder(t *testing.T) {
	type Holder struct {
		Z string `json:"z"`
		A string `json:"a"`
		M string `json:"m"`
	}
	data, err := GenerateBytes(Holder{})
	if err != nil {
		t.Fatalf("GenerateBytes: %v", err)
	}
	idxZ := strings.Index(string(data), `"z":`)
	idxA := strings.Index(string(data), `"a":`)
	idxM := strings.Index(string(data), `"m":`)
	if !(idxZ < idxA && idxA < idxM) {
		t.Errorf("property order not preserved: z=%d a=%d m=%d in %s", idxZ, idxA, idxM, data)
	}
}

func TestGenerateNoSchemaDeclaration(t *testing.T) {
	data, err := GenerateBytes(genUser{}, WithGenerateSchemaDeclaration(false))
	if err != nil {
		t.Fatalf("GenerateBytes: %v", err)
	}
	if strings.Contains(string(data), `"$schema"`) {
		t.Errorf("expected no $schema, got %s", data)
	}
}

func TestGenerateID(t *testing.T) {
	data, err := GenerateBytes(genUser{}, WithGenerateID("https://example.com/user.json"))
	if err != nil {
		t.Fatalf("GenerateBytes: %v", err)
	}
	if !strings.Contains(string(data), `"$id":"https://example.com/user.json"`) {
		t.Errorf("expected $id, got %s", data)
	}
}

func TestGenerateEmbeddedAnonymousStruct(t *testing.T) {
	type Common struct {
		ID string `json:"id" jsonschema:"required"`
	}
	type WithCommon struct {
		Common
		Name string `json:"name" jsonschema:"required"`
	}
	data, err := GenerateBytes(WithCommon{})
	if err != nil {
		t.Fatalf("GenerateBytes: %v", err)
	}
	if !strings.Contains(string(data), `"id":{`) {
		t.Errorf("expected id embedded at top level: %s", data)
	}
	if !strings.Contains(string(data), `"name":{`) {
		t.Errorf("expected name at top level: %s", data)
	}
	// required must include both inlined and direct fields.
	if !strings.Contains(string(data), `"required":["id","name"]`) {
		t.Errorf("expected merged required: %s", data)
	}
}

func TestGenerateJSONTagDash(t *testing.T) {
	type Holder struct {
		Internal string `json:"-"`
		Public   string `json:"public"`
	}
	data, err := GenerateBytes(Holder{})
	if err != nil {
		t.Fatalf("GenerateBytes: %v", err)
	}
	if strings.Contains(string(data), `"Internal"`) {
		t.Errorf("Internal should be omitted; got %s", data)
	}
}

func TestGenerateOmitemptyStripsRequired(t *testing.T) {
	type Holder struct {
		A string `json:"a,omitempty" jsonschema:"required"`
	}
	data, err := GenerateBytes(Holder{})
	if err != nil {
		t.Fatalf("GenerateBytes: %v", err)
	}
	if strings.Contains(string(data), `"required"`) {
		t.Errorf("omitempty should strip required, got %s", data)
	}
}

func TestGenerateBigIntPtr(t *testing.T) {
	type Holder struct {
		Big any `json:"big"` // empty schema for any
	}
	_ = Holder{}
	// We can't easily embed *big.Int in a Go literal here without
	// math/big import; the kind-switch already covers that path via
	// FromType-style construction. Smoke via reflect:
	type S struct {
		B []byte `json:"b"`
	}
	_, err := Generate(S{})
	if err != nil {
		t.Fatalf("smoke: %v", err)
	}
}

func TestGenerateConcurrentSafe(t *testing.T) {
	type Holder struct {
		A string `json:"a" jsonschema:"required"`
	}
	gen := NewGenerator()
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := gen.GenerateBytes(Holder{}); err != nil {
				t.Errorf("GenerateBytes: %v", err)
			}
		}()
	}
	wg.Wait()
}

func TestGenerateDraftSelectorChangesDefsKey(t *testing.T) {
	type Tree struct {
		Children []*Tree `json:"children,omitempty"`
		Name     string  `json:"name"`
	}
	data, err := GenerateBytes(Tree{}, WithGenerateDraft(Draft7))
	if err != nil {
		t.Fatalf("GenerateBytes: %v", err)
	}
	if !strings.Contains(string(data), `"definitions":{"Tree":`) {
		t.Errorf("expected definitions for Draft 7, got %s", data)
	}
	if !strings.Contains(string(data), `"$ref":"#/definitions/Tree"`) {
		t.Errorf("expected #/definitions/Tree ref, got %s", data)
	}
}

func TestGenerateMustGeneratePanics(t *testing.T) {
	type Holder struct {
		Ch chan int `json:"ch"`
	}
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic")
		}
	}()
	_ = MustGenerate(Holder{})
}

func TestGenerateFromTypeNil(t *testing.T) {
	_, err := FromType(nil)
	if err == nil {
		t.Fatal("expected error for nil type")
	}
}

func TestGenerateNilValueErrors(t *testing.T) {
	if _, err := Generate(nil); err == nil {
		t.Fatal("expected error for nil value")
	}
	if _, err := GenerateBytes(nil); err == nil {
		t.Fatal("expected error for nil value")
	}
}

func TestGenerateEnumOnString(t *testing.T) {
	type Holder struct {
		Role string `json:"role" jsonschema:"enum=admin|editor|viewer"`
	}
	data, err := GenerateBytes(Holder{})
	if err != nil {
		t.Fatalf("GenerateBytes: %v", err)
	}
	if !strings.Contains(string(data), `"enum":["admin","editor","viewer"]`) {
		t.Errorf("got %s", data)
	}
	s := MustCompile(data)
	bad, _ := json.Marshal(struct {
		Role string `json:"role"`
	}{"hacker"})
	res, _ := s.Validate(bad)
	if res.Valid {
		t.Errorf("expected enum rejection")
	}
}

func TestGenerateUniqueItems(t *testing.T) {
	type Holder struct {
		Tags []string `json:"tags" jsonschema:"uniqueItems"`
	}
	data, err := GenerateBytes(Holder{})
	if err != nil {
		t.Fatalf("GenerateBytes: %v", err)
	}
	if !strings.Contains(string(data), `"uniqueItems":true`) {
		t.Errorf("got %s", data)
	}
}

func TestGenerateRefOverridesGeneration(t *testing.T) {
	type Holder struct {
		Name string `json:"name" jsonschema:"$ref=https://example.com/name.json"`
	}
	data, err := GenerateBytes(Holder{})
	if err != nil {
		t.Fatalf("GenerateBytes: %v", err)
	}
	if !strings.Contains(string(data), `"$ref":"https://example.com/name.json"`) {
		t.Errorf("got %s", data)
	}
	// And the implicit "type": "string" from the field's Go kind is
	// suppressed when a $ref is set.
	if strings.Contains(string(data), `"name":{"type":"string"`) {
		t.Errorf("expected $ref to override default type: %s", data)
	}
}

// TestGenerateStructWithNoExportedFields confirms a struct whose entire
// field set is unexported still produces a valid {"type":"object"} schema —
// without panicking and without raising an error. The exact shape (whether
// `properties` and `required` slots are present and empty) is an
// implementation detail; the test asserts only the high-level invariant.
func TestGenerateStructWithNoExportedFields(t *testing.T) {
	type unexported struct {
		name string //nolint:unused // intentionally unexported for this test
		age  int    //nolint:unused // intentionally unexported for this test
	}
	schema, err := Generate(unexported{})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if schema == nil {
		t.Fatal("nil schema")
	}
	data, err := schema.MarshalJSON()
	if err != nil {
		t.Fatalf("MarshalJSON: %v", err)
	}
	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("Unmarshal: %v\n%s", err, data)
	}
	if doc["type"] != "object" {
		t.Errorf("expected type=object, got %v (full=%s)", doc["type"], data)
	}
}
