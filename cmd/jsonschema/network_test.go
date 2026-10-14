package main

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
)

func TestGenerate_offlineByDefault(t *testing.T) {
	var hits atomic.Int64
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		_, _ = w.Write([]byte(`{"type":"object","properties":{"n":{"type":"integer"}}}`))
	}))
	t.Cleanup(srv.Close)
	orig := http.DefaultTransport
	http.DefaultTransport = srv.Client().Transport
	t.Cleanup(func() { http.DefaultTransport = orig })

	schema := `{"title":"Wrap","$ref":"` + srv.URL + `/thing.json"}`
	var out, errOut bytes.Buffer
	if code := run([]string{"generate"}, strings.NewReader(schema), &out, &errOut); code != 1 {
		t.Fatalf("offline generate: exit=%d, want 1; stderr=%s", code, errOut.String())
	}
	if n := hits.Load(); n != 0 {
		t.Errorf("generate without --allow-network made %d requests", n)
	}
	if !strings.Contains(errOut.String(), "--allow-network") {
		t.Errorf("stderr does not suggest --allow-network:\n%s", errOut.String())
	}

	out.Reset()
	errOut.Reset()
	if code := run([]string{"generate", "--allow-network"}, strings.NewReader(schema), &out, &errOut); code != 0 {
		t.Fatalf("--allow-network generate: exit=%d stderr=%s", code, errOut.String())
	}
	if hits.Load() == 0 {
		t.Error("--allow-network did not fetch the remote $ref")
	}
}

func TestGenerate_outputFileIsWorldReadable(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permission bits")
	}
	outFile := filepath.Join(t.TempDir(), "models.go")
	var out, errOut bytes.Buffer
	if code := run([]string{"generate", "-o", outFile}, strings.NewReader(`{"title":"T","type":"object"}`), &out, &errOut); code != 0 {
		t.Fatalf("exit=%d stderr=%s", code, errOut.String())
	}
	info, err := os.Stat(outFile)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0o044 == 0 {
		t.Errorf("output mode %v is owner-only", info.Mode().Perm())
	}
}
