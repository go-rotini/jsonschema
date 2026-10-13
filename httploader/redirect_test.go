package httploader

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"sync/atomic"
	"testing"
	"time"
)

// trustTLSServer points http.DefaultTransport, which the built-in client
// clones, at srv's client transport so the test certificate is trusted.
func trustTLSServer(t *testing.T, srv *httptest.Server) {
	t.Helper()
	orig := http.DefaultTransport
	http.DefaultTransport = srv.Client().Transport
	t.Cleanup(func() { http.DefaultTransport = orig })
}

func TestLoaderRefusesHTTPSToHTTPRedirect(t *testing.T) {
	var plainHits atomic.Int64
	plain := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		plainHits.Add(1)
		_, _ = w.Write([]byte(`{"type":"string"}`))
	}))
	t.Cleanup(plain.Close)
	secure := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, plain.URL+"/s.json", http.StatusFound)
	}))
	t.Cleanup(secure.Close)
	trustTLSServer(t, secure)

	l := &Loader{}
	if _, err := l.Load(secure.URL + "/s.json"); !errors.Is(err, errInsecureRedirect) {
		t.Fatalf("Load err = %v, want errInsecureRedirect", err)
	}
	if n := plainHits.Load(); n != 0 {
		t.Errorf("plain-http server was contacted %d times", n)
	}

	allowed := &Loader{AllowHTTP: true}
	if _, err := allowed.Load(secure.URL + "/s.json"); err != nil {
		t.Errorf("AllowHTTP Load: %v", err)
	}
}

func TestLoaderRefusesHTTPEndWithCustomClient(t *testing.T) {
	plain := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{}`))
	}))
	t.Cleanup(plain.Close)
	secure := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, plain.URL+"/s.json", http.StatusFound)
	}))
	t.Cleanup(secure.Close)
	l := &Loader{Client: secure.Client()}
	if _, err := l.Load(secure.URL + "/s.json"); !errors.Is(err, errInsecureRedirect) {
		t.Fatalf("Load err = %v, want errInsecureRedirect", err)
	}
}

// A decorator that sets a header only for one host must still have that
// header scrubbed when a redirect leaves the host.
func TestLoaderRedirectScrubsHostConditionalHeader(t *testing.T) {
	var seenAtB atomic.Value
	seenAtB.Store("")
	srvB := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenAtB.Store(r.Header.Get("X-Api-Key"))
		_, _ = w.Write([]byte(`{}`))
	}))
	t.Cleanup(srvB.Close)
	srvA := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, srvB.URL+"/follow", http.StatusFound)
	}))
	t.Cleanup(srvA.Close)
	hostA := srvA.Listener.Addr().String()
	l := &Loader{
		AllowHTTP: true,
		RequestDecorator: func(r *http.Request) {
			if r.URL.Host == hostA {
				r.Header.Set("X-Api-Key", "secret")
			}
		},
	}
	if _, err := l.Load(srvA.URL + "/start"); err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := seenAtB.Load(); got != "" {
		t.Errorf("redirect leaked X-Api-Key to second host: %q", got)
	}
}

func TestCheckRedirectScrubsOnSchemeChange(t *testing.T) {
	state := &fetchState{allowHTTP: true, decorated: []string{"X-Api-Key"}}
	first, err := http.NewRequest(http.MethodGet, "https://a.example/s.json", http.NoBody)
	if err != nil {
		t.Fatal(err)
	}
	next, err := http.NewRequestWithContext(
		contextWithState(state), http.MethodGet, "http://a.example/s.json", http.NoBody)
	if err != nil {
		t.Fatal(err)
	}
	next.Header.Set("X-Api-Key", "secret")
	if err := checkRedirect(next, []*http.Request{first}); err != nil {
		t.Fatalf("checkRedirect: %v", err)
	}
	if got := next.Header.Get("X-Api-Key"); got != "" {
		t.Errorf("same-host https→http redirect kept X-Api-Key %q", got)
	}
}

func TestLoaderReusesConnections(t *testing.T) {
	var conns atomic.Int64
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{}`))
	}))
	srv.Config.ConnState = func(_ net.Conn, s http.ConnState) {
		if s == http.StateNew {
			conns.Add(1)
		}
	}
	srv.Start()
	t.Cleanup(srv.Close)
	l := &Loader{AllowHTTP: true}
	for i := range 10 {
		if _, err := l.Load(srv.URL + "/" + string(rune('a'+i)) + ".json"); err != nil {
			t.Fatal(err)
		}
	}
	if n := conns.Load(); n != 1 {
		t.Errorf("10 sequential fetches opened %d connections, want 1", n)
	}
}

func TestLoaderDenyPrivate(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{}`))
	}))
	t.Cleanup(srv.Close)
	l := &Loader{AllowHTTP: true, DenyPrivate: true}
	if _, err := l.Load(srv.URL + "/a.json"); !errors.Is(err, errPrivateAddress) {
		t.Errorf("DenyPrivate Load of loopback err = %v, want errPrivateAddress", err)
	}
	open := &Loader{AllowHTTP: true}
	if _, err := open.Load(srv.URL + "/a.json"); err != nil {
		t.Errorf("default Load of loopback: %v", err)
	}
}

func TestIsPublicAddr(t *testing.T) {
	for addr, want := range map[string]bool{
		"8.8.8.8":          true,
		"2606:4700::1111":  true,
		"127.0.0.1":        false,
		"10.1.2.3":         false,
		"172.16.0.1":       false,
		"192.168.1.1":      false,
		"169.254.169.254":  false,
		"100.64.0.1":       false,
		"0.0.0.0":          false,
		"0.1.2.3":          false,
		"::1":              false,
		"::":               false,
		"fe80::1":          false,
		"fd00:ec2::254":    false,
		"::ffff:127.0.0.1": false,
		"224.0.0.1":        false,
		"64:ff9b::a00:1":   false,
		"64:ff9b::808:808": true,
		"64:ff9b:1::1":     false,
		"2002:a00:1::1":    false,
		"2002:808:808::1":  true,
		"::10.0.0.1":       false,
		"::ffff:0:a00:1":   false,
		"::ffff:0:808:808": true,
		"2001:0:1::1":      false,
		"fec0::1":          false,
	} {
		if got := isPublicAddr(netip.MustParseAddr(addr)); got != want {
			t.Errorf("isPublicAddr(%s) = %v, want %v", addr, got, want)
		}
	}
}

// TestDenyPrivateWithCustomClientFails checks that DenyPrivate is not
// silently ignored when a custom Client is set.
func TestDenyPrivateWithCustomClientFails(t *testing.T) {
	l := &Loader{Client: &http.Client{}, DenyPrivate: true}
	if _, err := l.Load("https://example.com/s.json"); !errors.Is(err, errDenyPrivateClient) {
		t.Errorf("err = %v; want errDenyPrivateClient", err)
	}
}

func TestLoaderCacheEvictsExpired(t *testing.T) {
	l := &Loader{Cache: time.Nanosecond}
	l.cachePut("https://example.com/a", []byte("a"))
	time.Sleep(time.Millisecond)
	if _, ok := l.cacheGet("https://example.com/a"); ok {
		t.Fatal("expired entry served")
	}
	if n := len(l.entries); n != 0 {
		t.Errorf("expired hit left %d entries", n)
	}
	l.cachePut("https://example.com/b", []byte("b"))
	time.Sleep(time.Millisecond)
	l.cachePut("https://example.com/c", []byte("c"))
	if _, ok := l.entries["https://example.com/b"]; ok {
		t.Error("cachePut kept an expired entry")
	}
}

func contextWithState(state *fetchState) context.Context {
	return context.WithValue(context.Background(), fetchStateKey{}, state)
}
