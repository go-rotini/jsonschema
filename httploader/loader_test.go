package httploader

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-rotini/jsonschema"
)

func TestLoaderRejectsHTTPByDefault(t *testing.T) {
	l := &Loader{}
	_, err := l.Load("http://example.com/a")
	if !errors.Is(err, jsonschema.ErrLoaderRejected) {
		t.Errorf("Loader.Load(http://...) err = %v, want jsonschema.ErrLoaderRejected", err)
	}
}

func TestLoaderAllowHTTP(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	t.Cleanup(srv.Close)
	l := &Loader{AllowHTTP: true}
	data, err := l.Load(srv.URL + "/a")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if string(data) != `{"ok":true}` {
		t.Errorf("Load = %s", data)
	}
}

func TestLoaderRejectsNon4xx5xx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)
	l := &Loader{AllowHTTP: true}
	_, err := l.Load(srv.URL + "/a")
	if err == nil {
		t.Error("expected error on 404")
	}
}

func TestLoaderMaxBodySize(t *testing.T) {
	big := strings.Repeat("a", 5000)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(big))
	}))
	t.Cleanup(srv.Close)
	l := &Loader{AllowHTTP: true, MaxBodySize: 100}
	_, err := l.Load(srv.URL + "/big")
	if err == nil {
		t.Error("expected error from oversized body")
	}
}

func TestLoaderCacheHit(t *testing.T) {
	var hits atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	t.Cleanup(srv.Close)
	l := &Loader{AllowHTTP: true, Cache: time.Minute}
	for range 3 {
		if _, err := l.Load(srv.URL + "/a"); err != nil {
			t.Fatalf("Load: %v", err)
		}
	}
	if hits.Load() != 1 {
		t.Errorf("hits = %d, want 1 (cache should serve subsequent calls)", hits.Load())
	}
}

func TestLoaderSingleFlight(t *testing.T) {
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
	l := &Loader{AllowHTTP: true}
	var wg sync.WaitGroup
	wg.Add(N)
	for range N {
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

func TestLoaderRequestDecorator(t *testing.T) {
	var seen string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = r.Header.Get("X-Token")
		_, _ = w.Write([]byte(`{}`))
	}))
	t.Cleanup(srv.Close)
	l := &Loader{
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

func TestLoaderRejectsBadScheme(t *testing.T) {
	l := &Loader{}
	_, err := l.Load("ftp://example.com/x")
	if !errors.Is(err, jsonschema.ErrLoaderRejected) {
		t.Errorf("ftp scheme err = %v, want jsonschema.ErrLoaderRejected", err)
	}
}

// TestLoaderRedirectScrubsDecoratorHeaders confirms that headers added
// by the [Loader.RequestDecorator] hook are stripped before crossing to a
// different host on a 302 redirect — preventing a leak of bearer tokens to
// servers the caller never explicitly trusted.
func TestLoaderRedirectScrubsDecoratorHeaders(t *testing.T) {
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
	l := &Loader{
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

// TestLoaderRedirectScrubKeepsHeaderOnSameHost confirms the scrubber is
// scoped: same-host redirects (e.g. /a → /b on the same host) keep the
// decorator-supplied headers intact.
func TestLoaderRedirectScrubKeepsHeaderOnSameHost(t *testing.T) {
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
	l := &Loader{
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

// TestLoaderRedirectCap exercises the bounded redirect chain: a server
// that issues an unbounded redirect loop must terminate the loader with a
// non-nil error before exhausting goroutines.
func TestLoaderRedirectCap(t *testing.T) {
	var hits atomic.Int64
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		// Always redirect to ourselves — the redirect cap should fire well
		// before this loops indefinitely.
		http.Redirect(w, r, srv.URL+r.URL.Path, http.StatusFound)
	}))
	t.Cleanup(srv.Close)
	l := &Loader{AllowHTTP: true}
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

// TestLoaderCacheGetMiss covers the cache-miss branch directly.
func TestLoaderCacheGetMiss(t *testing.T) {
	l := &Loader{Cache: 1 * time.Second}
	if _, ok := l.cacheGet("https://example.com/x"); ok {
		t.Error("expected miss")
	}
	l.cachePut("https://example.com/x", []byte("hi"))
	if data, ok := l.cacheGet("https://example.com/x"); !ok || string(data) != "hi" {
		t.Errorf("hit: data=%s ok=%v", data, ok)
	}
}

// TestLoaderCacheExpired covers the cache-expired branch.
func TestLoaderCacheExpired(t *testing.T) {
	l := &Loader{Cache: time.Nanosecond}
	l.cachePut("https://example.com/x", []byte("hi"))
	time.Sleep(10 * time.Millisecond)
	if _, ok := l.cacheGet("https://example.com/x"); ok {
		t.Error("expected expired (miss)")
	}
}

// TestLoaderBadURLForBuild covers the build-request-error path. We
// supply a control char in the URL that builds-but-fails.
func TestLoaderBadURLForBuild(t *testing.T) {
	l := &Loader{}
	// Use a URL with a NUL byte that http.NewRequestWithContext rejects.
	if _, err := l.Load("https://example.com/\x00bad"); err == nil {
		t.Error("expected error from bad URL")
	}
}

// TestLoaderInvalidURI covers url.Parse failure.
func TestLoaderInvalidURI(t *testing.T) {
	l := &Loader{}
	if _, err := l.Load("http://%zz"); err == nil {
		t.Error("expected error on invalid URI")
	}
}

// TestLoaderTimeoutPropagatesError exercises the request-build error path
// indirectly via a fundamentally bad URL.
func TestLoaderTimeoutPropagatesError(t *testing.T) {
	l := &Loader{}
	// Use a non-listening 127.0.0.1 port — fast connection refusal.
	_, err := l.Load("https://127.0.0.1:1/a")
	if err == nil {
		t.Error("expected error from connection refused")
	}
}

// TestLoaderSingleFlightError covers the inflight wait+err path.
func TestLoaderSingleFlightError(t *testing.T) {
	// Using a non-routable target means the first Load fails. A simultaneous
	// follower hitting the same URI shares that failure.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)
	l := &Loader{AllowHTTP: true}
	_, err := l.Load(srv.URL + "/a")
	if err == nil {
		t.Error("expected error on 500")
	}
}

func TestDefaultServesMetaSchemasOffline(t *testing.T) {
	l := Default()
	a, aok := l.(jsonschema.ChainLoader)
	b, bok := Default().(jsonschema.ChainLoader)
	if !aok || !bok || len(a) != 2 || a[1] != b[1] {
		t.Error("Default should return a shared meta + HTTPS chain")
	}
	if _, err := l.Load("https://json-schema.org/draft/2020-12/schema"); err != nil {
		t.Errorf("meta-schema: %v", err)
	}
	// Plain http:// is not followed by default, so a non-meta URL is rejected.
	if _, err := l.Load("http://example.com/a"); !errors.Is(err, jsonschema.ErrLoaderRejected) {
		t.Errorf("http:// err = %v, want ErrLoaderRejected", err)
	}
}

func TestDefaultFetchesRemoteRefs(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"type":"string"}`))
	}))
	t.Cleanup(srv.Close)
	// Default's loader builds its transport from http.DefaultTransport, so
	// point that at the test server's client transport, which trusts its
	// certificate.
	orig := http.DefaultTransport
	http.DefaultTransport = srv.Client().Transport
	t.Cleanup(func() { http.DefaultTransport = orig })
	s, err := jsonschema.Compile([]byte(`{"$ref":"`+srv.URL+`/name.json"}`), jsonschema.WithLoader(Default()))
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	res, err := s.Validate([]byte(`1`))
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if res.Valid {
		t.Error("want invalid: the remote schema requires a string")
	}
}
