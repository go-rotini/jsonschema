package jsonschema

import (
	"encoding/base64"
	"sync"
	"testing"
)

// TestContentAnnotationOnly: default mode never errors regardless of bad
// encoding / bad media type / bad sub-schema.
func TestContentAnnotationOnly(t *testing.T) {
	cases := []struct {
		name   string
		schema string
		data   string
	}{
		{"bad base64", `{"contentEncoding":"base64"}`, `"not-base64-?"`},
		{"bad json", `{"contentMediaType":"application/json"}`, `"{invalid json"`},
		{"failing subschema", `{
			"contentMediaType":"application/json",
			"contentSchema":{"type":"integer"}
		}`, `"{\"foo\":1}"`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s, err := Compile([]byte(c.schema))
			if err != nil {
				t.Fatal(err)
			}
			res, err := s.Validate([]byte(c.data))
			if err != nil {
				t.Fatal(err)
			}
			if !res.Valid {
				t.Errorf("annotation-only should pass; got %v", res.Errors)
			}
		})
	}
}

// TestContentAssertionMatrix: encoding/mediaType/schema crossings under
// assertion mode.
func TestContentAssertionMatrix(t *testing.T) {
	type row struct {
		name     string
		schema   string
		data     string
		wantPass bool
	}
	json := `{"foo":1}`
	jsonB64 := base64.StdEncoding.EncodeToString([]byte(json))
	rows := []row{
		{
			name:     "good base64",
			schema:   `{"contentEncoding":"base64"}`,
			data:     `"` + jsonB64 + `"`,
			wantPass: true,
		},
		{
			name:     "bad base64",
			schema:   `{"contentEncoding":"base64"}`,
			data:     `"!@#$"`,
			wantPass: false,
		},
		{
			name:     "good json mediaType",
			schema:   `{"contentMediaType":"application/json"}`,
			data:     `"{\"foo\":1}"`,
			wantPass: true,
		},
		{
			name:     "bad json mediaType",
			schema:   `{"contentMediaType":"application/json"}`,
			data:     `"{not json"`,
			wantPass: false,
		},
		{
			name:     "non-JSON mediaType ignored",
			schema:   `{"contentMediaType":"text/plain"}`,
			data:     `"any string"`,
			wantPass: true,
		},
		{
			name:     "base64 + json",
			schema:   `{"contentEncoding":"base64","contentMediaType":"application/json"}`,
			data:     `"` + jsonB64 + `"`,
			wantPass: true,
		},
		{
			name:     "base64 ok but decoded json bad",
			schema:   `{"contentEncoding":"base64","contentMediaType":"application/json"}`,
			data:     `"` + base64.StdEncoding.EncodeToString([]byte("nope")) + `"`,
			wantPass: false,
		},
		{
			name: "schema-of-decoded passes",
			schema: `{
				"contentMediaType":"application/json",
				"contentSchema":{"type":"object","required":["foo"]}
			}`,
			data:     `"{\"foo\":1}"`,
			wantPass: true,
		},
		{
			name: "schema-of-decoded fails",
			schema: `{
				"contentMediaType":"application/json",
				"contentSchema":{"type":"object","required":["bar"]}
			}`,
			data:     `"{\"foo\":1}"`,
			wantPass: false,
		},
		{
			name:     "unknown encoding silent",
			schema:   `{"contentEncoding":"weird-blob"}`,
			data:     `"anything"`,
			wantPass: true,
		},
		{
			name:     "non-string ignored",
			schema:   `{"contentEncoding":"base64"}`,
			data:     `42`,
			wantPass: true,
		},
		{
			name:     "vendor +json",
			schema:   `{"contentMediaType":"application/vnd.api+json"}`,
			data:     `"{\"foo\":1}"`,
			wantPass: true,
		},
	}
	for _, r := range rows {
		t.Run(r.name, func(t *testing.T) {
			s, err := Compile([]byte(r.schema))
			if err != nil {
				t.Fatal(err)
			}
			res, err := s.Validate([]byte(r.data), WithContentAssertion(true))
			if err != nil {
				t.Fatal(err)
			}
			if res.Valid != r.wantPass {
				t.Errorf("got valid=%v want=%v errors=%v", res.Valid, r.wantPass, res.Errors)
			}
		})
	}
}

// TestContentBase16Hex covers the base16 / hex alternate encoding name.
func TestContentBase16Hex(t *testing.T) {
	s, _ := Compile([]byte(`{"contentEncoding":"base16","contentMediaType":"application/json"}`))
	// hex-encoded {"a":1}
	res, _ := s.Validate([]byte(`"7b2261223a317d"`), WithContentAssertion(true))
	if !res.Valid {
		t.Errorf("hex+JSON should pass: %v", res.Errors)
	}
}

// TestContentQuotedPrintable covers quoted-printable.
func TestContentQuotedPrintable(t *testing.T) {
	s, _ := Compile([]byte(`{"contentEncoding":"quoted-printable","contentMediaType":"application/json"}`))
	// {"a":1} encoded — base ASCII passes through identically.
	res, _ := s.Validate([]byte(`"{\"a\":1}"`), WithContentAssertion(true))
	if !res.Valid {
		t.Errorf("QP+JSON should pass: %v", res.Errors)
	}
}

// TestContentConcurrencySmoke runs assertion mode in parallel; -race must be
// clean.
func TestContentConcurrencySmoke(t *testing.T) {
	s, _ := Compile([]byte(`{
		"contentEncoding":"base64",
		"contentMediaType":"application/json",
		"contentSchema":{"type":"object","required":["foo"]}
	}`))
	good := []byte(`"` + base64.StdEncoding.EncodeToString([]byte(`{"foo":1}`)) + `"`)
	bad := []byte(`"` + base64.StdEncoding.EncodeToString([]byte(`{"bar":1}`)) + `"`)
	const workers = 8
	var wg sync.WaitGroup
	wg.Add(workers)
	for i := range workers {
		go func() {
			defer wg.Done()
			data := good
			if i%2 == 0 {
				data = bad
			}
			for range 30 {
				// Smoke test: the race detector catches data races; we
				// don't validate per-call output here.
				_, _ = s.Validate(data, WithContentAssertion(true))
			}
		}()
	}
	wg.Wait()
}

// TestContentSchemaAssertion covers the contentSchema validation branch.
func TestContentSchemaAssertion(t *testing.T) {
	src := []byte(`{
		"type":"string",
		"contentEncoding":"base64",
		"contentMediaType":"application/json",
		"contentSchema":{"type":"object","required":["x"]}
	}`)
	s := MustCompile(src)
	// Valid: base64 of {"x":1}
	res, err := s.Validate(
		[]byte(`"eyJ4IjoxfQ=="`),
		WithContentAssertion(true),
	)
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if !res.Valid {
		t.Errorf("expected valid; errors=%v", res.Errors)
	}
	// Invalid: base64 of {"y":1} (missing required x)
	res, err = s.Validate(
		[]byte(`"eyJ5IjoxfQ=="`),
		WithContentAssertion(true),
	)
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if res.Valid {
		t.Errorf("expected invalid for missing required field")
	}
}

// TestContentEncodingBase32 covers base32 + media-type non-JSON.
func TestContentEncodingBase32(t *testing.T) {
	src := []byte(`{"contentEncoding":"base32","contentMediaType":"application/octet-stream"}`)
	s := MustCompile(src)
	// Annotation only by default → valid.
	if _, err := s.Validate([]byte(`"NBSWY3DP"`)); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	// With assertion + bad base32:
	res, err := s.Validate([]byte(`"!!!"`), WithContentAssertion(true))
	if err != nil {
		t.Fatalf("Validate(assert): %v", err)
	}
	if res.Valid {
		t.Error("expected invalid for bad base32")
	}
}

// TestContentEncodingQuotedPrintable covers quoted-printable.
func TestContentEncodingQuotedPrintable(t *testing.T) {
	src := []byte(`{"contentEncoding":"quoted-printable","contentMediaType":"text/plain"}`)
	s := MustCompile(src)
	if _, err := s.Validate([]byte(`"hello=20world"`)); err != nil {
		t.Fatalf("Validate: %v", err)
	}
}

// TestContentEncoding7bit covers the pass-through case.
func TestContentEncoding7bit(t *testing.T) {
	for _, enc := range []string{"7bit", "8bit", "binary"} {
		t.Run(enc, func(t *testing.T) {
			src := []byte(`{"contentEncoding":"` + enc + `","contentMediaType":"text/plain"}`)
			s := MustCompile(src)
			if _, err := s.Validate([]byte(`"plain text"`)); err != nil {
				t.Errorf("%s: %v", enc, err)
			}
		})
	}
}

// TestContentUnknownEncoding covers the silent-pass fallback.
func TestContentUnknownEncoding(t *testing.T) {
	src := []byte(`{"contentEncoding":"made-up-encoding","contentMediaType":"text/plain"}`)
	s := MustCompile(src)
	res, err := s.Validate([]byte(`"x"`), WithContentAssertion(true))
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if !res.Valid {
		t.Errorf("unknown encoding should silent-pass; errors=%v", res.Errors)
	}
}

// TestContentMediaTypeJSONVariants covers the */+json suffix path.
func TestContentMediaTypeJSONVariants(t *testing.T) {
	// application/vnd.api+json
	src := []byte(`{"contentMediaType":"application/vnd.api+json"}`)
	s := MustCompile(src)
	if _, err := s.Validate([]byte(`"{\"a\":1}"`), WithContentAssertion(true)); err != nil {
		t.Errorf("Validate: %v", err)
	}
}

// TestContentBase64Hex covers the base16 (hex) encoding.
func TestContentBase64Hex(t *testing.T) {
	src := []byte(`{
		"contentEncoding":"base16",
		"contentMediaType":"application/octet-stream"
	}`)
	s := MustCompile(src)
	// valid hex
	if _, err := s.Validate([]byte(`"AABBCCDD"`)); err != nil {
		t.Errorf("hex valid: %v", err)
	}
	// invalid hex
	res, err := s.Validate([]byte(`"NOT-HEX"`), WithContentAssertion(true))
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if res.Valid {
		t.Errorf("expected invalid for non-hex")
	}
}

// TestContentEncodingBadBase64 covers the decodeContent error path: when
// contentMediaType is set, an undecodable contentEncoding still validates as
// annotation-only by default.
func TestContentEncodingBadBase64(t *testing.T) {
	src := []byte(`{"contentEncoding":"base64","contentMediaType":"application/json"}`)
	s := MustCompile(src)
	// Annotation-only by default → should be valid.
	res, err := s.Validate([]byte(`"!@#"`))
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if !res.Valid {
		t.Errorf("default mode: bad base64 should still validate as annotation-only; errors=%v", res.Errors)
	}
	// With assertion: should fail.
	res, err = s.Validate([]byte(`"!@#"`), WithContentAssertion(true))
	if err != nil {
		t.Fatalf("Validate(assert): %v", err)
	}
	if res.Valid {
		t.Errorf("WithContentAssertion: expected invalid for bad base64")
	}
}
