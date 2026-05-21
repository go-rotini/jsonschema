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

func TestRun_stdinToStdout(t *testing.T) {
	schema := `{"title":"Widget","type":"object","required":["id"],"properties":{"id":{"type":"string"}}}`
	var out, errOut bytes.Buffer
	code := run([]string{"-package", "widgets"}, strings.NewReader(schema), &out, &errOut)
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

func TestRun_invalidSchema(t *testing.T) {
	var out, errOut bytes.Buffer
	code := run([]string{}, strings.NewReader("not json"), &out, &errOut)
	if code == 0 {
		t.Fatalf("expected non-zero exit for invalid schema; stderr=%s", errOut.String())
	}
}

func TestRun_fileOutput(t *testing.T) {
	dir := t.TempDir()
	outFile := filepath.Join(dir, "models.go")
	schema := `{"title":"Thing","type":"object","properties":{"name":{"type":"string"}}}`
	var out, errOut bytes.Buffer
	code := run([]string{"-o", outFile}, strings.NewReader(schema), &out, &errOut)
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

func TestRun_rootFlag(t *testing.T) {
	schema := `{"type":"object","properties":{"x":{"type":"string"}}}`
	var out, errOut bytes.Buffer
	code := run([]string{"-root", "Custom"}, strings.NewReader(schema), &out, &errOut)
	if code != 0 {
		t.Fatalf("run exit=%d stderr=%s", code, errOut.String())
	}
	if !strings.Contains(out.String(), "type Custom struct") {
		t.Errorf("output missing type Custom:\n%s", out.String())
	}
}

func TestRun_flagError(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := run([]string{"-nonexistent"}, strings.NewReader(""), &out, &errOut); code != 2 {
		t.Fatalf("expected exit 2 for unknown flag, got %d", code)
	}
}
