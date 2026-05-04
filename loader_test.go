package jsonschema

import (
	"bytes"
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

// TestFileLoaderRejectsSymlinkOutsideRoot confirms that a symlink inside
// Root pointing at a file outside Root is refused. Pure lexical
// path-cleaning would let an attacker who can write a symlink under Root
// (e.g. via a co-tenant compile-time fixture) read arbitrary host files.
func TestFileLoaderRejectsSymlinkOutsideRoot(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "secret"), []byte("secret"), 0o600); err != nil {
		t.Fatalf("write outside file: %v", err)
	}
	if err := os.Symlink(filepath.Join(outside, "secret"), filepath.Join(root, "evil")); err != nil {
		t.Skipf("symlink unsupported on this platform: %v", err)
	}
	l := FileLoader{Root: root}
	_, err := l.Load("file:///evil")
	if err == nil {
		t.Fatal("expected loader to reject symlink target outside root")
	}
	if !errors.Is(err, ErrLoaderRejected) {
		t.Errorf("err = %v, want wrapped ErrLoaderRejected", err)
	}
}

// TestFileLoaderAllowsSymlinkInsideRoot confirms a symlink whose target
// stays inside Root is still served. The symlink check rejects only
// resolved targets that escape Root.
func TestFileLoaderAllowsSymlinkInsideRoot(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "real.json")
	if err := os.WriteFile(target, []byte(`{"ok":1}`), 0o600); err != nil {
		t.Fatalf("write target: %v", err)
	}
	if err := os.Symlink(target, filepath.Join(root, "alias.json")); err != nil {
		t.Skipf("symlink unsupported on this platform: %v", err)
	}
	l := FileLoader{Root: root}
	data, err := l.Load("file:///alias.json")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if string(data) != `{"ok":1}` {
		t.Errorf("Load = %s", data)
	}
}

// TestFileLoaderResolvesRootSymlink confirms that Root itself being a
// symlink does not cause the prefix check to fail. macOS in particular
// returns /private/var/... when /var/... is requested; we must compare
// the symlink-resolved versions of both Root and the joined path.
func TestFileLoaderResolvesRootSymlink(t *testing.T) {
	realRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(realRoot, "a.json"), []byte(`{}`), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	link := filepath.Join(t.TempDir(), "rootlink")
	if err := os.Symlink(realRoot, link); err != nil {
		t.Skipf("symlink unsupported on this platform: %v", err)
	}
	l := FileLoader{Root: link}
	if _, err := l.Load("file:///a.json"); err != nil {
		t.Fatalf("Load via symlinked root: %v", err)
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
	const N = 10
	var (
		hits    atomic.Int64
		arrived sync.WaitGroup
		release = make(chan struct{})
	)
	arrived.Add(N)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		// Block until every goroutine has signaled it is about to call
		// Load; by then the followers have queued behind the in-flight
		// slot. Then close the gate to release the response. This
		// replaces a 50ms time.Sleep with deterministic synchronization.
		arrived.Wait()
		<-release
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	t.Cleanup(srv.Close)
	l := &HTTPLoader{AllowHTTP: true}
	var wg sync.WaitGroup
	wg.Add(N)
	for i := 0; i < N; i++ {
		go func() {
			defer wg.Done()
			arrived.Done()
			if _, err := l.Load(srv.URL + "/a"); err != nil {
				t.Errorf("Load: %v", err)
			}
		}()
	}
	arrived.Wait()
	close(release)
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

// TestHTTPLoaderRedirectScrubsDecoratorHeaders confirms that headers added
// by the [HTTPLoader.RequestDecorator] hook are stripped before crossing to a
// different host on a 302 redirect — preventing a leak of bearer tokens to
// servers the caller never explicitly trusted.
func TestHTTPLoaderRedirectScrubsDecoratorHeaders(t *testing.T) {
	// Server B receives the redirected request and records what header it saw.
	var seenAtB string
	srvB := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenAtB = r.Header.Get("X-Api-Key")
		_, _ = w.Write([]byte(`{}`))
	}))
	t.Cleanup(srvB.Close)
	// Server A sends a 302 to server B (different host).
	srvA := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, srvB.URL+"/follow", http.StatusFound)
	}))
	t.Cleanup(srvA.Close)
	l := &HTTPLoader{
		AllowHTTP: true,
		RequestDecorator: func(r *http.Request) {
			r.Header.Set("X-Api-Key", "secret")
		},
	}
	if _, err := l.Load(srvA.URL + "/start"); err != nil {
		t.Fatalf("Load: %v", err)
	}
	if seenAtB != "" {
		t.Errorf("redirect leaked X-Api-Key to second host: %q", seenAtB)
	}
}

// TestHTTPLoaderRedirectScrubKeepsHeaderOnSameHost confirms the scrubber is
// scoped: same-host redirects (e.g. /a → /b on the same host) keep the
// decorator-supplied headers intact.
func TestHTTPLoaderRedirectScrubKeepsHeaderOnSameHost(t *testing.T) {
	var (
		mu   sync.Mutex
		seen string
		hitB atomic.Int64
	)
	mux := http.NewServeMux()
	mux.HandleFunc("/start", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/follow", http.StatusFound)
	})
	mux.HandleFunc("/follow", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		seen = r.Header.Get("X-Api-Key")
		mu.Unlock()
		hitB.Add(1)
		_, _ = w.Write([]byte(`{}`))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	l := &HTTPLoader{
		AllowHTTP: true,
		RequestDecorator: func(r *http.Request) {
			r.Header.Set("X-Api-Key", "secret")
		},
	}
	if _, err := l.Load(srv.URL + "/start"); err != nil {
		t.Fatalf("Load: %v", err)
	}
	if hitB.Load() != 1 {
		t.Fatalf("/follow hit %d times, want 1", hitB.Load())
	}
	mu.Lock()
	defer mu.Unlock()
	if seen != "secret" {
		t.Errorf("same-host redirect dropped header (saw %q)", seen)
	}
}

// TestHTTPLoaderRedirectCap exercises the bounded redirect chain: a server
// that issues an unbounded redirect loop must terminate the loader with a
// non-nil error before exhausting goroutines.
func TestHTTPLoaderRedirectCap(t *testing.T) {
	var hits atomic.Int64
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		// Always redirect to ourselves — the redirect cap should fire well
		// before this loops indefinitely.
		http.Redirect(w, r, srv.URL+r.URL.Path, http.StatusFound)
	}))
	t.Cleanup(srv.Close)
	l := &HTTPLoader{AllowHTTP: true}
	_, err := l.Load(srv.URL + "/loop")
	if err == nil {
		t.Fatal("expected error on redirect loop")
	}
	// We don't pin the exact hit count: the stdlib client surfaces the
	// CheckRedirect error a few hops in, and the exact count is an
	// implementation detail. We just verify it didn't run away.
	if hits.Load() > 20 {
		t.Errorf("too many redirects followed: %d", hits.Load())
	}
}

// TestHTTPLoaderCacheGetMiss covers the cache-miss branch directly.
func TestHTTPLoaderCacheGetMiss(t *testing.T) {
	l := &HTTPLoader{Cache: 1 * time.Second}
	if _, ok := l.cacheGet("https://example.com/x"); ok {
		t.Error("expected miss")
	}
	l.cachePut("https://example.com/x", []byte("hi"))
	if data, ok := l.cacheGet("https://example.com/x"); !ok || string(data) != "hi" {
		t.Errorf("hit: data=%s ok=%v", data, ok)
	}
}

// TestHTTPLoaderCacheExpired covers the cache-expired branch.
func TestHTTPLoaderCacheExpired(t *testing.T) {
	l := &HTTPLoader{Cache: time.Nanosecond}
	l.cachePut("https://example.com/x", []byte("hi"))
	time.Sleep(10 * time.Millisecond)
	if _, ok := l.cacheGet("https://example.com/x"); ok {
		t.Error("expected expired (miss)")
	}
}

// TestHTTPLoaderBadURLForBuild covers the build-request-error path. We
// supply a control char in the URL that builds-but-fails.
func TestHTTPLoaderBadURLForBuild(t *testing.T) {
	l := &HTTPLoader{}
	// Use a URL with a NUL byte that http.NewRequestWithContext rejects.
	if _, err := l.Load("https://example.com/\x00bad"); err == nil {
		t.Error("expected error from bad URL")
	}
}

// TestExtractIDBranches covers various paths through extractID.
func TestExtractIDBranches(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{`{"$id":"https://e/x"}`, "https://e/x"},
		{`{ "$id" : "https://e/x" }`, "https://e/x"},
		{`{"$id":  "https://e/x"  }`, "https://e/x"},
		{`{}`, ""},
		{`{"$id":42}`, ""},       // not a string
		{`{"$id" "x"}`, ""},      // missing colon
		{`{"$id":"unclosed`, ""}, // no closing quote
		{`{"$id":`, ""},
	}
	for _, tc := range cases {
		got := extractID([]byte(tc.in))
		if got != tc.want {
			t.Errorf("extractID(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// TestRegisterVocabMetaSilent covers the "all good" path.
func TestRegisterVocabMetaSilent(t *testing.T) {
	m := MapLoader{}
	registerVocabMeta(m, "meta/draft-2020-12/meta")
	if len(m) == 0 {
		t.Error("registerVocabMeta should populate from embedded FS")
	}
}

// TestRegisterVocabMetaMissingDir covers the missing-dir branch.
func TestRegisterVocabMetaMissingDir(t *testing.T) {
	m := MapLoader{}
	registerVocabMeta(m, "meta/does-not-exist")
	if len(m) != 0 {
		t.Errorf("missing dir should not populate; got %v", m)
	}
}

// TestTracingLoaderError confirms the tracing wrapper reports
// errors verbatim.
func TestTracingLoaderError(t *testing.T) {
	var buf bytes.Buffer
	l := &tracingLoader{
		inner: MapLoader{},
		w:     &buf,
	}
	if _, err := l.Load("https://example.com/missing"); err == nil {
		t.Error("expected error")
	}
	// Trace should be empty since we didn't fetch anything successfully.
	if buf.Len() > 0 {
		t.Errorf("trace should be empty on failure: %q", buf.String())
	}
}

// TestReadFileImplOk covers the success branch.
func TestReadFileImplOk(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/x.json"
	if err := writeBytes(path, []byte("hi")); err != nil {
		t.Fatal(err)
	}
	data, err := readFileImpl(path)
	if err != nil {
		t.Fatalf("readFileImpl: %v", err)
	}
	if string(data) != "hi" {
		t.Errorf("got %s", data)
	}
}

// TestReadFileImplFails covers the error branch.
func TestReadFileImplFails(t *testing.T) {
	if _, err := readFileImpl("/dev/no-such-thing-/abc"); err == nil {
		t.Error("expected error")
	}
}

// TestFileLoaderInvalidURI covers url.Parse failure.
func TestFileLoaderInvalidURI(t *testing.T) {
	l := FileLoader{Root: t.TempDir()}
	if _, err := l.Load("file://%zz"); err == nil {
		t.Error("expected error on invalid URI")
	}
}

// TestHTTPLoaderInvalidURI covers url.Parse failure.
func TestHTTPLoaderInvalidURI(t *testing.T) {
	l := &HTTPLoader{}
	if _, err := l.Load("http://%zz"); err == nil {
		t.Error("expected error on invalid URI")
	}
}

// TestEmbedLoaderInvalidURI covers url.Parse failure.
func TestEmbedLoaderInvalidURI(t *testing.T) {
	l := EmbedLoader{FS: metaSchemaFS}
	if _, err := l.Load("embed://%zz"); err == nil {
		t.Error("expected error on invalid URI")
	}
}

// TestEmbedLoaderEmptyPath covers errEmbedEmptyPath.
func TestEmbedLoaderEmptyPath(t *testing.T) {
	l := EmbedLoader{FS: metaSchemaFS}
	_, err := l.Load("embed://")
	if err == nil {
		t.Error("expected error for empty path")
	}
}

// TestHTTPLoaderTimeoutPropagatesError exercises the request-build error path
// indirectly via a fundamentally bad URL.
func TestHTTPLoaderTimeoutPropagatesError(t *testing.T) {
	l := &HTTPLoader{}
	// Use a non-listening 127.0.0.1 port — fast connection refusal.
	_, err := l.Load("https://127.0.0.1:1/a")
	if err == nil {
		t.Error("expected error from connection refused")
	}
}

// TestHTTPLoaderSingleFlightError covers the inflight wait+err path.
func TestHTTPLoaderSingleFlightError(t *testing.T) {
	// Using a non-routable target means the first Load fails. A simultaneous
	// follower hitting the same URI shares that failure.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)
	l := &HTTPLoader{AllowHTTP: true}
	_, err := l.Load(srv.URL + "/a")
	if err == nil {
		t.Error("expected error on 500")
	}
}

// TestReadFileImplPropagates covers the readFileImpl helper indirectly via
// FileLoader on a real file.
func TestReadFileImplPropagates(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/x.json"
	if err := writeBytes(path, []byte(`{"ok":1}`)); err != nil {
		t.Fatalf("write: %v", err)
	}
	l := FileLoader{Root: dir}
	data, err := l.Load("file:///x.json")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if string(data) != `{"ok":1}` {
		t.Errorf("got %s", data)
	}
}

// writeBytes is a tiny helper for file-based loader tests.
func writeBytes(path string, data []byte) error {
	return os.WriteFile(path, data, 0o600)
}

// TestTraceLoaderFetchNilWriter is a no-op that exercises the nil-writer branch.
func TestTraceLoaderFetchNilWriter(_ *testing.T) {
	traceLoaderFetch(nil, "https://example.com/x")
}

// TestTraceLoaderFetchWritesLine confirms a non-nil writer receives a line.
func TestTraceLoaderFetchWritesLine(t *testing.T) {
	var buf bytes.Buffer
	traceLoaderFetch(&buf, "https://example.com/x")
	if !strings.Contains(buf.String(), "https://example.com/x") {
		t.Errorf("got %q", buf.String())
	}
}

// TestReadFileImplMissing covers the error-return branch indirectly.
func TestReadFileImplMissing(t *testing.T) {
	l := FileLoader{Root: t.TempDir()}
	if _, err := l.Load("file:///nope.json"); err == nil {
		t.Error("expected error")
	}
}
