package jsonschema

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestMapLoaderHit(t *testing.T) {
	m := MapLoader{
		"https://example.com/a": []byte(`{"type":"string"}`),
	}
	data, err := m.Load("https://example.com/a")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if string(data) != `{"type":"string"}` {
		t.Errorf("Load = %s", data)
	}
}

func TestMapLoaderMiss(t *testing.T) {
	m := MapLoader{}
	_, err := m.Load("https://example.com/missing")
	if !errors.Is(err, ErrLoaderRejected) {
		t.Errorf("Load err = %v, want ErrLoaderRejected", err)
	}
}

func TestMapLoaderTrailingHashTolerant(t *testing.T) {
	m := MapLoader{
		"https://example.com/a": []byte(`{}`),
	}
	if _, err := m.Load("https://example.com/a#"); err != nil {
		t.Errorf("trailing-hash lookup: %v", err)
	}
}

func TestMapLoaderTrailingHashMatchesStoredHash(t *testing.T) {
	m := MapLoader{
		"https://example.com/a#": []byte(`{}`),
	}
	if _, err := m.Load("https://example.com/a"); err != nil {
		t.Errorf("non-hash lookup: %v", err)
	}
}

func TestMapLoaderReturnsCopy(t *testing.T) {
	src := []byte(`{}`)
	m := MapLoader{"u": src}
	data, _ := m.Load("u")
	data[0] = 'X'
	again, _ := m.Load("u")
	if again[0] == 'X' {
		t.Errorf("Load did not return a copy")
	}
}

func TestFileLoaderRejectsWithoutRoot(t *testing.T) {
	l := FileLoader{}
	_, err := l.Load("file:///etc/passwd")
	if !errors.Is(err, ErrLoaderRejected) {
		t.Errorf("FileLoader without Root err = %v, want ErrLoaderRejected", err)
	}
}

func TestFileLoaderInsideRoot(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "a.json")
	if err := os.WriteFile(target, []byte(`{"type":"string"}`), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	l := FileLoader{Root: dir}
	data, err := l.Load("file:///a.json")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if string(data) != `{"type":"string"}` {
		t.Errorf("Load = %s", data)
	}
}

func TestFileLoaderRejectsEscape(t *testing.T) {
	dir := t.TempDir()
	l := FileLoader{Root: dir}
	for _, uri := range []string{
		"file:///../etc/passwd",
		"file:///./../etc/passwd",
	} {
		_, err := l.Load(uri)
		if !errors.Is(err, ErrLoaderRejected) {
			t.Errorf("FileLoader.Load(%q) err = %v, want ErrLoaderRejected", uri, err)
		}
	}
}

func TestFileLoaderRejectsNonFileScheme(t *testing.T) {
	l := FileLoader{Root: t.TempDir()}
	_, err := l.Load("https://example.com/a")
	if !errors.Is(err, ErrLoaderRejected) {
		t.Errorf("FileLoader err = %v, want ErrLoaderRejected", err)
	}
}

func TestFileLoaderMissingFile(t *testing.T) {
	l := FileLoader{Root: t.TempDir()}
	_, err := l.Load("file:///does-not-exist.json")
	if err == nil {
		t.Error("FileLoader.Load on missing file should error")
	}
	if errors.Is(err, ErrLoaderRejected) {
		t.Error("missing file should not be ErrLoaderRejected")
	}
}

func TestHTTPLoaderRejectsHTTPByDefault(t *testing.T) {
	l := &HTTPLoader{}
	_, err := l.Load("http://example.com/a")
	if !errors.Is(err, ErrLoaderRejected) {
		t.Errorf("HTTPLoader.Load(http://...) err = %v, want ErrLoaderRejected", err)
	}
}

func TestHTTPLoaderAllowHTTP(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	t.Cleanup(srv.Close)
	l := &HTTPLoader{AllowHTTP: true}
	data, err := l.Load(srv.URL + "/a")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if string(data) != `{"ok":true}` {
		t.Errorf("Load = %s", data)
	}
}

func TestHTTPLoaderRejectsNon4xx5xx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)
	l := &HTTPLoader{AllowHTTP: true}
	_, err := l.Load(srv.URL + "/a")
	if err == nil {
		t.Error("expected error on 404")
	}
}

func TestHTTPLoaderMaxBodySize(t *testing.T) {
	big := strings.Repeat("a", 5000)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(big))
	}))
	t.Cleanup(srv.Close)
	l := &HTTPLoader{AllowHTTP: true, MaxBodySize: 100}
	_, err := l.Load(srv.URL + "/big")
	if err == nil {
		t.Error("expected error from oversized body")
	}
}

func TestHTTPLoaderCacheHit(t *testing.T) {
	var hits atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	t.Cleanup(srv.Close)
	l := &HTTPLoader{AllowHTTP: true, Cache: time.Minute}
	for i := 0; i < 3; i++ {
		if _, err := l.Load(srv.URL + "/a"); err != nil {
			t.Fatalf("Load: %v", err)
		}
	}
	if hits.Load() != 1 {
		t.Errorf("hits = %d, want 1 (cache should serve subsequent calls)", hits.Load())
	}
}

func TestHTTPLoaderSingleFlight(t *testing.T) {
	var hits atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		// Sleep so concurrent goroutines have time to coalesce.
		time.Sleep(50 * time.Millisecond)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	t.Cleanup(srv.Close)
	l := &HTTPLoader{AllowHTTP: true}
	const N = 10
	var wg sync.WaitGroup
	wg.Add(N)
	for i := 0; i < N; i++ {
		go func() {
			defer wg.Done()
			if _, err := l.Load(srv.URL + "/a"); err != nil {
				t.Errorf("Load: %v", err)
			}
		}()
	}
	wg.Wait()
	if hits.Load() != 1 {
		t.Errorf("hits = %d, want 1 (single-flight should coalesce concurrent requests)", hits.Load())
	}
}

func TestHTTPLoaderRequestDecorator(t *testing.T) {
	var seen string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = r.Header.Get("X-Token")
		_, _ = w.Write([]byte(`{}`))
	}))
	t.Cleanup(srv.Close)
	l := &HTTPLoader{
		AllowHTTP: true,
		RequestDecorator: func(r *http.Request) {
			r.Header.Set("X-Token", "secret")
		},
	}
	if _, err := l.Load(srv.URL + "/a"); err != nil {
		t.Fatalf("Load: %v", err)
	}
	if seen != "secret" {
		t.Errorf("decorator did not set header (saw %q)", seen)
	}
}

func TestHTTPLoaderRejectsBadScheme(t *testing.T) {
	l := &HTTPLoader{}
	_, err := l.Load("ftp://example.com/x")
	if !errors.Is(err, ErrLoaderRejected) {
		t.Errorf("ftp scheme err = %v, want ErrLoaderRejected", err)
	}
}

func TestChainLoaderFallthrough(t *testing.T) {
	a := MapLoader{}
	b := MapLoader{"u": []byte("hi")}
	c := ChainLoader{a, b}
	data, err := c.Load("u")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if string(data) != "hi" {
		t.Errorf("Load = %s", data)
	}
}

func TestChainLoaderEmpty(t *testing.T) {
	c := ChainLoader{}
	_, err := c.Load("u")
	if !errors.Is(err, ErrLoaderRejected) {
		t.Errorf("empty chain err = %v, want ErrLoaderRejected", err)
	}
}

func TestChainLoaderShortCircuitsOnRealError(t *testing.T) {
	// Loader that always returns a non-rejected error.
	bad := loaderFunc(func(uri string) ([]byte, error) {
		return nil, &LoaderError{URI: uri, Cause: errors.New("boom")}
	})
	good := MapLoader{"u": []byte("hi")}
	c := ChainLoader{bad, good}
	_, err := c.Load("u")
	if err == nil {
		t.Fatal("expected error to short-circuit")
	}
	if errors.Is(err, ErrLoaderRejected) {
		t.Errorf("expected non-rejected error to propagate, got %v", err)
	}
}

type loaderFunc func(string) ([]byte, error)

func (f loaderFunc) Load(uri string) ([]byte, error) { return f(uri) }

func TestDefaultLoaderResolvesEmbeddedMetaSchemas(t *testing.T) {
	l := DefaultLoader()
	for _, d := range []Draft{Draft4, Draft6, Draft7, Draft201909, Draft202012} {
		data, err := l.Load(d.MetaSchemaURL())
		if err != nil {
			t.Errorf("DefaultLoader %s: %v", d, err)
			continue
		}
		if len(data) == 0 {
			t.Errorf("DefaultLoader %s: empty bytes", d)
		}
	}
}

func TestDefaultLoaderResolvesHTTPSAndHTTPVariants(t *testing.T) {
	l := DefaultLoader()
	// Draft 4's canonical URL is http://; the embedded MapLoader should
	// also serve the https:// alias for tooling that auto-upgrades.
	if _, err := l.Load("https://json-schema.org/draft-04/schema"); err != nil {
		t.Errorf("https Draft 4 alias: %v", err)
	}
	// Likewise the canonical https:// URLs for the modern drafts should
	// resolve via http:// for legacy callers.
	if _, err := l.Load("http://json-schema.org/draft/2020-12/schema"); err != nil {
		t.Errorf("http Draft 2020-12 alias: %v", err)
	}
}

func TestDefaultLoaderResolvesPerVocabularyMeta(t *testing.T) {
	l := DefaultLoader()
	for _, uri := range []string{
		"https://json-schema.org/draft/2020-12/meta/core",
		"https://json-schema.org/draft/2020-12/meta/applicator",
		"https://json-schema.org/draft/2020-12/meta/validation",
		"https://json-schema.org/draft/2019-09/meta/core",
	} {
		if _, err := l.Load(uri); err != nil {
			t.Errorf("DefaultLoader %s: %v", uri, err)
		}
	}
}

func TestEmbedLoaderRejectsNonEmbedScheme(t *testing.T) {
	l := EmbedLoader{FS: metaSchemaFS}
	_, err := l.Load("https://example.com/a")
	if !errors.Is(err, ErrLoaderRejected) {
		t.Errorf("EmbedLoader err = %v, want ErrLoaderRejected", err)
	}
}

func TestEmbedLoaderResolvesEmbeddedFile(t *testing.T) {
	l := EmbedLoader{FS: metaSchemaFS}
	data, err := l.Load("embed://meta/draft-2020-12.json")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(data) == 0 {
		t.Error("Load returned empty bytes")
	}
}

func TestEmbedLoaderMissingFile(t *testing.T) {
	l := EmbedLoader{FS: metaSchemaFS}
	_, err := l.Load("embed://meta/does-not-exist.json")
	if err == nil {
		t.Error("expected error on missing file")
	}
}
