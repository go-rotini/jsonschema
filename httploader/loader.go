package httploader

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/go-rotini/jsonschema"
)

// Default returns a loader chain for schemas with remote refs: the embedded
// meta-schemas ([jsonschema.MetaLoader]), so meta-schema refs stay offline,
// followed by an HTTPS-only [Loader] with the default timeout and body cap.
// The returned value is shared across calls, so its cache and in-flight
// state are too.
//
//	schema, err := jsonschema.Compile(src, jsonschema.WithLoader(httploader.Default()))
func Default() jsonschema.Loader {
	return defaultChain()
}

var defaultChain = sync.OnceValue(func() jsonschema.Loader {
	return jsonschema.ChainLoader{
		jsonschema.MetaLoader(),
		&Loader{Timeout: defaultTimeout, MaxBodySize: defaultMaxBody},
	}
})

// maxRedirects caps the redirect chain on a single fetch (lower than the
// stdlib default of 10) so redirect loops cannot stall it.
const maxRedirects = 5

// Defaults for a zero Timeout and MaxBodySize. Variables (not consts) so
// tests can shrink them.
var (
	defaultTimeout = 10 * time.Second
	defaultMaxBody = int64(10 * 1024 * 1024) // 10 MiB
)

var (
	errInflightInvalid  = errors.New("internal: invalid in-flight entry")
	errNon2xx           = errors.New("http: non-2xx status")
	errBodyTooLarge     = errors.New("http: body exceeds MaxBodySize")
	errTooManyRedirects = errors.New("http: too many redirects")
)

// Loader is a [jsonschema.Loader] that resolves http:// and https:// URIs
// over the network. It is HTTPS-only unless AllowHTTP is set. Concurrent
// requests for the same URI share one round-trip, and a non-2xx response
// is an error.
//
// Set the exported fields at construction; treat the value as read-only
// afterwards. Pass *Loader (not a copy) so the cache and in-flight state
// are shared across goroutines.
type Loader struct {
	// Client is the http.Client used to perform requests. nil uses a
	// built-in client that caps redirects at 5 and strips headers added by
	// RequestDecorator when a redirect crosses to another host; a custom
	// Client keeps its own redirect policy.
	Client *http.Client
	// Timeout caps the duration of a single fetch, with either client.
	// Default: 10 s.
	Timeout time.Duration
	// MaxBodySize caps the response body size in bytes; a larger body
	// fails the fetch. Default: 10 MiB.
	MaxBodySize int64
	// AllowHTTP, when true, permits http:// URLs in addition to https://.
	// Default: false.
	AllowHTTP bool
	// Cache is the in-memory cache TTL for successful responses. 0 disables
	// caching (the default).
	Cache time.Duration
	// RequestDecorator is an optional hook invoked on every outbound
	// request — useful for adding authentication headers or API tokens.
	RequestDecorator func(*http.Request)

	cacheMu sync.RWMutex
	entries map[string]*cacheEntry
	flight  sync.Map // uri → *inflight
}

type cacheEntry struct {
	data    []byte
	expires time.Time
}

type inflight struct {
	wg   sync.WaitGroup
	data []byte
	err  error
}

// Load implements [jsonschema.Loader]. A URI with any other scheme, or an
// http:// URI when AllowHTTP is false, is rejected with
// [jsonschema.ErrLoaderRejected] so a [jsonschema.ChainLoader] falls through.
func (l *Loader) Load(uri string) ([]byte, error) {
	parsed, err := url.Parse(uri)
	if err != nil {
		return nil, &jsonschema.LoaderError{URI: uri, Cause: fmt.Errorf("parse: %w", err)}
	}
	switch parsed.Scheme {
	case "https":
	case "http":
		if !l.AllowHTTP {
			return nil, &jsonschema.LoaderError{URI: uri, Cause: jsonschema.ErrLoaderRejected}
		}
	default:
		return nil, &jsonschema.LoaderError{URI: uri, Cause: jsonschema.ErrLoaderRejected}
	}

	if l.Cache > 0 {
		if data, ok := l.cacheGet(uri); ok {
			out := make([]byte, len(data))
			copy(out, data)
			return out, nil
		}
	}

	// Single-flight: concurrent fetches for the same URI share one round-trip.
	flight := &inflight{}
	flight.wg.Add(1)
	actual, loaded := l.flight.LoadOrStore(uri, flight)
	if loaded {
		other, ok := actual.(*inflight)
		if !ok {
			return nil, &jsonschema.LoaderError{URI: uri, Cause: errInflightInvalid}
		}
		other.wg.Wait()
		if other.err != nil {
			return nil, other.err
		}
		out := make([]byte, len(other.data))
		copy(out, other.data)
		return out, nil
	}

	defer func() {
		l.flight.Delete(uri)
		flight.wg.Done()
	}()

	data, err := l.fetch(uri)
	if err != nil {
		flight.err = err
		return nil, err
	}
	flight.data = data
	if l.Cache > 0 {
		l.cachePut(uri, data)
	}
	out := make([]byte, len(data))
	copy(out, data)
	return out, nil
}

func (l *Loader) fetch(uri string) ([]byte, error) {
	timeout := l.Timeout
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	maxBody := l.MaxBodySize
	if maxBody <= 0 {
		maxBody = defaultMaxBody
	}
	// Probe RequestDecorator's header set so cross-host redirects can scrub
	// it (preventing token leaks to unintended hosts).
	decoratedHeaders := decoratorHeaderNames(l.RequestDecorator)
	client := l.Client
	if client == nil {
		client = &http.Client{
			Timeout:       timeout,
			Transport:     defaultHTTPTransport(),
			CheckRedirect: redirectScrubber(decoratedHeaders),
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, uri, http.NoBody)
	if err != nil {
		return nil, &jsonschema.LoaderError{URI: uri, Cause: fmt.Errorf("build request: %w", err)}
	}
	if l.RequestDecorator != nil {
		l.RequestDecorator(req)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, &jsonschema.LoaderError{URI: uri, Cause: fmt.Errorf("get: %w", err)}
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, &jsonschema.LoaderError{URI: uri, Cause: fmt.Errorf("%w: %d", errNon2xx, resp.StatusCode)}
	}
	limited := io.LimitReader(resp.Body, maxBody+1)
	data, err := io.ReadAll(limited)
	if err != nil {
		return nil, &jsonschema.LoaderError{URI: uri, Cause: fmt.Errorf("read body: %w", err)}
	}
	if int64(len(data)) > maxBody {
		return nil, &jsonschema.LoaderError{URI: uri, Cause: fmt.Errorf("%w: %d bytes", errBodyTooLarge, maxBody)}
	}
	return data, nil
}

// defaultHTTPTransport returns a tightened [*http.Transport] inheriting
// proxy + dialer settings from [http.DefaultTransport] but with stricter
// connection-pool and handshake timeouts. The returned transport is
// reusable across requests; callers that need different limits should
// construct their own [http.Client].
func defaultHTTPTransport() *http.Transport {
	base, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		return &http.Transport{
			MaxIdleConns:          100,
			MaxIdleConnsPerHost:   10,
			IdleConnTimeout:       90 * time.Second,
			TLSHandshakeTimeout:   10 * time.Second,
			ExpectContinueTimeout: 1 * time.Second,
		}
	}
	t := base.Clone()
	t.MaxIdleConns = 100
	t.MaxIdleConnsPerHost = 10
	t.IdleConnTimeout = 90 * time.Second
	t.TLSHandshakeTimeout = 10 * time.Second
	t.ExpectContinueTimeout = 1 * time.Second
	return t
}

// decoratorHeaderNames returns the set of header names added (or replaced)
// by decorator. We probe a no-op request and diff its header set
// before/after running the hook.
func decoratorHeaderNames(decorator func(*http.Request)) map[string]struct{} {
	if decorator == nil {
		return nil
	}
	probe, err := http.NewRequest(http.MethodGet, "http://invalid.example/", http.NoBody)
	if err != nil {
		return nil
	}
	before := make(map[string]struct{}, len(probe.Header))
	for name := range probe.Header {
		before[name] = struct{}{}
	}
	decorator(probe)
	added := map[string]struct{}{}
	for name := range probe.Header {
		if _, existed := before[name]; !existed {
			added[name] = struct{}{}
		}
	}
	return added
}

// redirectScrubber returns a CheckRedirect function that strips
// decorator-added headers from requests that cross to a different host
// (preventing leak of bearer tokens to unintended servers) and caps the
// redirect chain at [maxRedirects].
func redirectScrubber(decoratedHeaders map[string]struct{}) func(*http.Request, []*http.Request) error {
	return func(req *http.Request, via []*http.Request) error {
		if len(via) >= maxRedirects {
			return errTooManyRedirects
		}
		if len(via) == 0 || len(decoratedHeaders) == 0 {
			return nil
		}
		origin := via[0].URL.Host
		if req.URL.Host == origin {
			return nil
		}
		for name := range decoratedHeaders {
			req.Header.Del(name)
		}
		return nil
	}
}

func (l *Loader) cacheGet(uri string) ([]byte, bool) {
	l.cacheMu.RLock()
	defer l.cacheMu.RUnlock()
	entry, ok := l.entries[uri]
	if !ok {
		return nil, false
	}
	if time.Now().After(entry.expires) {
		return nil, false
	}
	return entry.data, true
}

func (l *Loader) cachePut(uri string, data []byte) {
	l.cacheMu.Lock()
	defer l.cacheMu.Unlock()
	if l.entries == nil {
		l.entries = make(map[string]*cacheEntry)
	}
	stored := make([]byte, len(data))
	copy(stored, data)
	l.entries[uri] = &cacheEntry{data: stored, expires: time.Now().Add(l.Cache)}
}
