package main

import (
	"bytes"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGenerate_stdinToStdout(t *testing.T) {
	schema := `{"title":"Widget","type":"object","required":["id"],"properties":{"id":{"type":"string"}}}`
	var out, errOut bytes.Buffer
	code := run([]string{"generate", "-package", "widgets"}, strings.NewReader(schema), &out, &errOut)
	if code != 0 {
		t.Fatalf("run exit=%d stderr=%s", code, errOut.String())
	}
	got := out.String()
	for _, w := range []string{"package widgets", "type Widget struct", "Id string"} {
		if !strings.Contains(got, w) {
			t.Errorf("output missing %q\n%s", w, got)
		}
	}
}

func TestGenerate_schemaFromFile(t *testing.T) {
	dir := t.TempDir()
	schemaFile := filepath.Join(dir, "schema.json")
	if err := os.WriteFile(schemaFile, []byte(`{"title":"Gadget","type":"object","properties":{"n":{"type":"integer"}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	var out, errOut bytes.Buffer
	code := run([]string{"generate", schemaFile}, strings.NewReader(""), &out, &errOut)
	if code != 0 {
		t.Fatalf("run exit=%d stderr=%s", code, errOut.String())
	}
	if !strings.Contains(out.String(), "type Gadget struct") {
		t.Errorf("output missing type Gadget:\n%s", out.String())
	}
}

func TestGenerate_schemaFileNotFound(t *testing.T) {
	var out, errOut bytes.Buffer
	code := run([]string{"generate", filepath.Join(t.TempDir(), "nope.json")}, strings.NewReader(""), &out, &errOut)
	if code != 1 {
		t.Fatalf("missing schema file: exit=%d, want 1", code)
	}
	if !strings.Contains(errOut.String(), "read schema file") {
		t.Errorf("missing schema file: stderr missing the read error:\n%s", errOut.String())
	}
}

func TestGenerate_invalidSchema(t *testing.T) {
	var out, errOut bytes.Buffer
	code := run([]string{"generate"}, strings.NewReader("not json"), &out, &errOut)
	if code != 1 {
		t.Fatalf("invalid schema: exit=%d, want 1; stderr=%s", code, errOut.String())
	}
}

func TestGenerate_fileOutput(t *testing.T) {
	dir := t.TempDir()
	outFile := filepath.Join(dir, "models.go")
	schema := `{"title":"Thing","type":"object","properties":{"name":{"type":"string"}}}`
	var out, errOut bytes.Buffer
	code := run([]string{"generate", "-o", outFile}, strings.NewReader(schema), &out, &errOut)
	if code != 0 {
		t.Fatalf("run exit=%d stderr=%s", code, errOut.String())
	}
	data, err := os.ReadFile(outFile)
	if err != nil {
		t.Fatalf("read output file: %v", err)
	}
	if _, err := parser.ParseFile(token.NewFileSet(), "models.go", data, 0); err != nil {
		t.Fatalf("output file is not valid Go: %v\n%s", err, data)
	}
	if !strings.Contains(string(data), "type Thing struct") {
		t.Errorf("output file missing type Thing:\n%s", data)
	}
}

func TestGenerate_outputWriteError(t *testing.T) {
	schema := `{"type":"object","properties":{"x":{"type":"string"}}}`
	badOut := filepath.Join(t.TempDir(), "no-such-dir", "models.go") // parent dir does not exist
	var out, errOut bytes.Buffer
	code := run([]string{"generate", "-o", badOut}, strings.NewReader(schema), &out, &errOut)
	if code != 1 {
		t.Fatalf("bad output path: exit=%d, want 1", code)
	}
	if !strings.Contains(errOut.String(), "write output file") {
		t.Errorf("bad output path: stderr missing the write error:\n%s", errOut.String())
	}
}

func TestGenerate_rootFlag(t *testing.T) {
	schema := `{"type":"object","properties":{"x":{"type":"string"}}}`
	var out, errOut bytes.Buffer
	code := run([]string{"generate", "-root", "Custom"}, strings.NewReader(schema), &out, &errOut)
	if code != 0 {
		t.Fatalf("run exit=%d stderr=%s", code, errOut.String())
	}
	if !strings.Contains(out.String(), "type Custom struct") {
		t.Errorf("output missing type Custom:\n%s", out.String())
	}
}

func TestGenerate_flagError(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := run([]string{"generate", "-nonexistent"}, strings.NewReader(""), &out, &errOut); code != 2 {
		t.Fatalf("expected exit 2 for unknown flag, got %d", code)
	}
}

func TestGenerate_stdinReadError(t *testing.T) {
	var out, errOut bytes.Buffer
	code := run([]string{"generate"}, errReader{}, &out, &errOut)
	if code != 1 {
		t.Fatalf("stdin read error: exit=%d, want 1", code)
	}
	if !strings.Contains(errOut.String(), "read stdin") {
		t.Errorf("stdin read error: stderr missing the read error:\n%s", errOut.String())
	}
}

func TestGenerate_stdoutWriteError(t *testing.T) {
	schema := `{"type":"object","properties":{"x":{"type":"string"}}}`
	var errOut bytes.Buffer
	code := run([]string{"generate"}, strings.NewReader(schema), errWriter{}, &errOut)
	if code != 1 {
		t.Fatalf("stdout write error: exit=%d, want 1", code)
	}
	if !strings.Contains(errOut.String(), "write stdout") {
		t.Errorf("stdout write error: stderr missing the write error:\n%s", errOut.String())
	}
}

// TestGenerate_shortFlags exercises the short forms -p / -r (the long forms
// -package / -root are covered above).
func TestGenerate_shortFlags(t *testing.T) {
	schema := `{"type":"object","properties":{"x":{"type":"string"}}}`
	var out, errOut bytes.Buffer
	code := run([]string{"generate", "-p", "widgets", "-r", "Custom"}, strings.NewReader(schema), &out, &errOut)
	if code != 0 {
		t.Fatalf("run exit=%d stderr=%s", code, errOut.String())
	}
	for _, w := range []string{"package widgets", "type Custom struct"} {
		if !strings.Contains(out.String(), w) {
			t.Errorf("output missing %q\n%s", w, out.String())
		}
	}
}

// TestGenerate_longOutput exercises --output (the short form -o is covered above).
func TestGenerate_longOutput(t *testing.T) {
	dir := t.TempDir()
	outFile := filepath.Join(dir, "out.go")
	schema := `{"title":"Thing","type":"object","properties":{"name":{"type":"string"}}}`
	var out, errOut bytes.Buffer
	code := run([]string{"generate", "--output", outFile}, strings.NewReader(schema), &out, &errOut)
	if code != 0 {
		t.Fatalf("run exit=%d stderr=%s", code, errOut.String())
	}
	data, err := os.ReadFile(outFile)
	if err != nil {
		t.Fatalf("read output file: %v", err)
	}
	if !strings.Contains(string(data), "type Thing struct") {
		t.Errorf("output file missing type Thing:\n%s", data)
	}
}

func TestGenerate_help(t *testing.T) {
	for _, arg := range []string{"-h", "--help"} {
		var out, errOut bytes.Buffer
		if code := run([]string{"generate", arg}, strings.NewReader(""), &out, &errOut); code != 0 {
			t.Fatalf("generate %s: exit=%d, want 0", arg, code)
		}
		if !strings.Contains(errOut.String(), "usage: jsonschema generate") {
			t.Errorf("generate %s: stderr missing the generate usage:\n%s", arg, errOut.String())
		}
	}
}
