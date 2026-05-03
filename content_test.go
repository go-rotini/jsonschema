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
