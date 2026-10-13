package httploader

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"slices"
	"sync"
	"syscall"
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
	errInsecureRedirect = errors.New("http: redirect from https to http without AllowHTTP")
)

// Errors a [Loader] with DenyPrivate set returns, wrapped in a
// [*jsonschema.LoaderError]; test for them with [errors.Is].
var (
	// ErrPrivateAddress reports a fetch refused because the host resolved
	// to a loopback, private, link-local or unspecified address.
	ErrPrivateAddress = errors.New("http: address is loopback, private, link-local or unspecified")
	// ErrDenyPrivateClient reports DenyPrivate set with a custom Client,
	// whose dialer the Loader cannot check.
	ErrDenyPrivateClient = errors.New("http: DenyPrivate needs the built-in client; Client is set")
)

// Loader is a [jsonschema.Loader] that resolves http:// and https:// URIs
// over the network. It is HTTPS-only unless AllowHTTP is set, and that
// covers redirects too. Concurrent requests for the same URI share one
// round-trip, and a non-2xx response is an error.
//
// Set the exported fields at construction; treat the value as read-only
// afterwards. Pass *Loader (not a copy) so the cache, in-flight state and
// connection pool are shared across goroutines.
type Loader struct {
	// Client is the http.Client used to perform requests. nil uses a
	// built-in client, created once per Loader, that caps redirects at 5,
	// refuses an https-to-http redirect unless AllowHTTP is set, and strips
	// headers added by RequestDecorator when a redirect changes scheme or
	// host. A custom Client keeps its own redirect policy, but a response
	// that ends on plain http is still refused unless AllowHTTP is set.
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
	// DenyPrivate, when true, makes the built-in client refuse to connect
	// to loopback, private, link-local (including cloud metadata
	// endpoints), shared-address and unspecified IP addresses. The check
	// runs on the address actually dialed, so it covers redirects and DNS
	// answers, and an IPv6 address that embeds an IPv4 one (NAT64, 6to4)
	// is judged by the IPv4 address. Set it when schemas come from
	// untrusted sources. With a custom Client every fetch fails, since its
	// dialer cannot be checked; with a proxy configured it applies to the
	// proxy's address. Default: false.
	DenyPrivate bool
	// Cache is the in-memory cache TTL for successful responses. 0 disables
	// caching (the default).
	Cache time.Duration
	// RequestDecorator is an optional hook invoked on every outbound
	// request — useful for adding authentication headers or API tokens.
	RequestDecorator func(*http.Request)

	clientOnce    sync.Once
	defaultClient *http.Client

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

// fetchState is the per-request data the built-in client's redirect policy
// reads from the request context.
type fetchState struct {
	allowHTTP bool
	// decorated holds the header names RequestDecorator set on this request.
	decorated []string
}

type fetchStateKey struct{}

func (l *Loader) fetch(uri string) ([]byte, error) {
	timeout := l.Timeout
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	maxBody := l.MaxBodySize
	if maxBody <= 0 {
		maxBody = defaultMaxBody
	}
	client := l.Client
	if client == nil {
		client = l.builtinClient()
	} else if l.DenyPrivate {
		return nil, &jsonschema.LoaderError{URI: uri, Cause: ErrDenyPrivateClient}
	}
	state := &fetchState{allowHTTP: l.AllowHTTP}
	ctx, cancel := context.WithTimeout(context.WithValue(context.Background(), fetchStateKey{}, state), timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, uri, http.NoBody)
	if err != nil {
		return nil, &jsonschema.LoaderError{URI: uri, Cause: fmt.Errorf("build request: %w", err)}
	}
	if l.RequestDecorator != nil {
		state.decorated = decorate(req, l.RequestDecorator)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, &jsonschema.LoaderError{URI: uri, Cause: fmt.Errorf("get: %w", err)}
	}
	defer func() { _ = resp.Body.Close() }()
	if !l.AllowHTTP && resp.Request != nil && resp.Request.URL.Scheme != "https" {
		return nil, &jsonschema.LoaderError{URI: uri, Cause: errInsecureRedirect}
	}
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

// builtinClient returns the Loader's built-in client, creating it on first
// use so every fetch shares one connection pool.
func (l *Loader) builtinClient() *http.Client {
	l.clientOnce.Do(func() {
		l.defaultClient = &http.Client{
			Transport:     defaultHTTPTransport(l.DenyPrivate),
			CheckRedirect: checkRedirect,
		}
	})
	return l.defaultClient
}

// decorate runs decorator on req and returns the names of the headers it
// added or changed.
func decorate(req *http.Request, decorator func(*http.Request)) []string {
	before := req.Header.Clone()
	decorator(req)
	var names []string
	for name, vals := range req.Header {
		old, existed := before[name]
		if !existed || !slices.Equal(old, vals) {
			names = append(names, name)
		}
	}
	return names
}

// defaultHTTPTransport returns a tightened [*http.Transport] inheriting
// proxy + dialer settings from [http.DefaultTransport] but with stricter
// connection-pool and handshake timeouts. When denyPrivate is set, the
// dialer refuses non-public addresses.
func defaultHTTPTransport(denyPrivate bool) *http.Transport {
	var t *http.Transport
	if base, ok := http.DefaultTransport.(*http.Transport); ok {
		t = base.Clone()
	} else {
		t = &http.Transport{}
	}
	t.MaxIdleConns = 100
	t.MaxIdleConnsPerHost = 10
	t.IdleConnTimeout = 90 * time.Second
	t.TLSHandshakeTimeout = 10 * time.Second
	t.ExpectContinueTimeout = 1 * time.Second
	if denyPrivate {
		d := &net.Dialer{Timeout: 30 * time.Second, KeepAlive: 30 * time.Second, Control: denyPrivateControl}
		t.DialContext = d.DialContext
		t.DialTLSContext = nil
	}
	return t
}

// denyPrivateControl is a [net.Dialer] Control hook that refuses to connect
// to a non-public IP address.
func denyPrivateControl(_, address string, _ syscall.RawConn) error {
	ap, err := netip.ParseAddrPort(address)
	if err != nil {
		return fmt.Errorf("%w: %s", ErrPrivateAddress, address)
	}
	if !isPublicAddr(ap.Addr()) {
		return fmt.Errorf("%w: %s", ErrPrivateAddress, ap.Addr())
	}
	return nil
}

// sharedAddressSpace is the carrier-grade NAT range (RFC 6598).
var (
	sharedAddressSpace = netip.MustParsePrefix("100.64.0.0/10")
	// IPv6 prefixes that carry an IPv4 address in their last 32 bits:
	// NAT64 (RFC 6052), IPv4-translated and IPv4-compatible addresses.
	nat64Prefix          = netip.MustParsePrefix("64:ff9b::/96")
	ipv4TranslatedPrefix = netip.MustParsePrefix("::ffff:0:0:0/96")
	ipv4CompatPrefix     = netip.MustParsePrefix("::/96")
	// 6to4 (RFC 3056) carries an IPv4 address in bits 16 to 47.
	sixToFourPrefix = netip.MustParsePrefix("2002::/16")
	// Prefixes refused outright: local-use NAT64 (RFC 8215), whose
	// embedded address may sit anywhere; Teredo, which tunnels to an
	// obfuscated IPv4 address; and deprecated site-local addresses.
	nonPublicPrefixes = []netip.Prefix{
		netip.MustParsePrefix("64:ff9b:1::/48"),
		netip.MustParsePrefix("2001::/32"),
		netip.MustParsePrefix("fec0::/10"),
	}
)

// embeddedIPv4 returns the IPv4 address an IPv6 address carries under
// NAT64, 6to4, or the IPv4-translated or IPv4-compatible forms.
func embeddedIPv4(a netip.Addr) (netip.Addr, bool) {
	if !a.Is6() {
		return netip.Addr{}, false
	}
	b := a.As16()
	switch {
	case nat64Prefix.Contains(a), ipv4TranslatedPrefix.Contains(a), ipv4CompatPrefix.Contains(a):
		return netip.AddrFrom4([4]byte(b[12:16])), true
	case sixToFourPrefix.Contains(a):
		return netip.AddrFrom4([4]byte(b[2:6])), true
	}
	return netip.Addr{}, false
}

// isPublicAddr reports whether a is a globally routable unicast address.
// An IPv6 address carrying an IPv4 address is judged by that address.
func isPublicAddr(a netip.Addr) bool {
	a = a.Unmap()
	if a.IsLoopback() || a.IsUnspecified() {
		return false
	}
	if v4, ok := embeddedIPv4(a); ok {
		a = v4
	}
	for _, p := range nonPublicPrefixes {
		if p.Contains(a) {
			return false
		}
	}
	switch {
	case !a.IsValid(),
		a.IsUnspecified(),
		a.IsLoopback(),
		a.IsPrivate(),
		a.IsLinkLocalUnicast(),
		a.IsLinkLocalMulticast(),
		a.IsInterfaceLocalMulticast(),
		a.IsMulticast(),
		sharedAddressSpace.Contains(a),
		a.Is4() && a.As4()[0] == 0:
		return false
	}
	return true
}

// checkRedirect is the built-in client's redirect policy. It caps the chain
// at [maxRedirects], refuses https-to-http unless the fetch allows plain
// http, and strips the headers RequestDecorator set when a redirect changes
// scheme or host, so tokens are not sent to a server the caller never named
// or sent in clear.
func checkRedirect(req *http.Request, via []*http.Request) error {
	if len(via) >= maxRedirects {
		return errTooManyRedirects
	}
	state, _ := req.Context().Value(fetchStateKey{}).(*fetchState)
	if state == nil || len(via) == 0 {
		return nil
	}
	if req.URL.Scheme != "https" && !state.allowHTTP {
		return errInsecureRedirect
	}
	origin := via[0].URL
	if req.URL.Scheme == origin.Scheme && req.URL.Host == origin.Host {
		return nil
	}
	for _, name := range state.decorated {
		req.Header.Del(name)
	}
	return nil
}

func (l *Loader) cacheGet(uri string) ([]byte, bool) {
	l.cacheMu.RLock()
	entry, ok := l.entries[uri]
	l.cacheMu.RUnlock()
	if !ok {
		return nil, false
	}
	if time.Now().After(entry.expires) {
		l.cacheMu.Lock()
		if l.entries[uri] == entry {
			delete(l.entries, uri)
		}
		l.cacheMu.Unlock()
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
	now := time.Now()
	for k, e := range l.entries {
		if now.After(e.expires) {
			delete(l.entries, k)
		}
	}
	stored := make([]byte, len(data))
	copy(stored, data)
	l.entries[uri] = &cacheEntry{data: stored, expires: now.Add(l.Cache)}
}
