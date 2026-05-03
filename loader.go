package jsonschema

import (
	"bytes"
	"context"
	"embed"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
)

// Static error sentinels for loader.go's value-shaped failures.
var (
	errInternalInflightInvalid = errors.New("internal: invalid in-flight entry")
	errEmbedEmptyPath          = errors.New("embed: empty path")
	errHTTPNon2xx              = errors.New("http: non-2xx status")
	errHTTPBodyTooLarge        = errors.New("http: body exceeds MaxBodySize")
	errPathEscapesRoot         = errors.New("file: path escapes root")
)

// Loader fetches the contents of a schema referenced by URI. Implementations
// must be safe for concurrent use; the compiler may invoke a single Loader
// from multiple goroutines while resolving a graph of refs.
type Loader interface {
	// Load returns the schema bytes for the given URI, or an error
	// describing why the URI could not be served. Implementations that do
	// not handle a URI should return [ErrLoaderRejected] (wrapped in a
	// [*LoaderError]) so a [ChainLoader] can fall through to the next.
	Load(uri string) ([]byte, error)
}

// MapLoader is a [Loader] backed by a static map of URI to bytes. It is
// useful in tests, in [Compiler.AddResource], and as the storage for the
// embedded standard meta-schemas in the default loader chain.
//
// Lookups tolerate a trailing # on the requested URI (so an `$id` that ends
// in `#` matches an entry stored without it, and vice versa).
type MapLoader map[string][]byte

// Load implements [Loader].
func (m MapLoader) Load(uri string) ([]byte, error) {
	if data, ok := m[uri]; ok {
		out := make([]byte, len(data))
		copy(out, data)
		return out, nil
	}
	// Tolerate trailing-hash mismatches.
	trimmed := strings.TrimSuffix(uri, "#")
	if trimmed != uri {
		if data, ok := m[trimmed]; ok {
			out := make([]byte, len(data))
			copy(out, data)
			return out, nil
		}
	} else if data, ok := m[uri+"#"]; ok {
		out := make([]byte, len(data))
		copy(out, data)
		return out, nil
	}
	return nil, &LoaderError{URI: uri, Cause: ErrLoaderRejected}
}

// FileLoader resolves file:// URIs against the local filesystem. The Root
// field is mandatory: any path that escapes Root (via .., absolute paths
// outside Root, etc.) is rejected. When Root is the empty string, FileLoader
// refuses every URI (returning [ErrLoaderRejected]) — callers must opt in
// explicitly to file-system access.
type FileLoader struct {
	// Root is the directory file:// URIs resolve against. Required; an
	// empty Root disables the loader entirely.
	Root string
}

// Load implements [Loader].
func (l FileLoader) Load(uri string) ([]byte, error) {
	if l.Root == "" {
		return nil, &LoaderError{URI: uri, Cause: ErrLoaderRejected}
	}
	parsed, err := url.Parse(uri)
	if err != nil {
		return nil, &LoaderError{URI: uri, Cause: fmt.Errorf("parse: %w", err)}
	}
	if parsed.Scheme != "file" {
		return nil, &LoaderError{URI: uri, Cause: ErrLoaderRejected}
	}
	// url.Path is decoded; on Windows this also drops the leading slash
	// before the drive letter, but rotini targets Unix-y filesystems for
	// its tests.
	rel := parsed.Path
	if rel == "" {
		rel = parsed.Opaque
	}
	rootAbs, err := filepath.Abs(l.Root)
	if err != nil {
		return nil, &LoaderError{URI: uri, Cause: fmt.Errorf("resolve root: %w", err)}
	}
	// Reject any explicit ".." segment in the URI path BEFORE joining so a
	// crafted file:///../etc/passwd cannot turn into rootAbs/etc/passwd
	// after filepath.Clean swallows the upward traversal.
	rawRel := strings.TrimPrefix(rel, "/")
	if slices.Contains(strings.Split(rawRel, "/"), "..") {
		return nil, &LoaderError{URI: uri, Cause: fmt.Errorf("%w: %w", errPathEscapesRoot, ErrLoaderRejected)}
	}
	joined := filepath.Join(rootAbs, rawRel)
	joined = filepath.Clean(joined)
	// Defense in depth: the resolved path must still live under rootAbs.
	rel2, err := filepath.Rel(rootAbs, joined)
	if err != nil || strings.HasPrefix(rel2, "..") || rel2 == ".." {
		return nil, &LoaderError{URI: uri, Cause: fmt.Errorf("%w: %w", errPathEscapesRoot, ErrLoaderRejected)}
	}
	data, err := readFile(joined)
	if err != nil {
		return nil, &LoaderError{URI: uri, Cause: err}
	}
	return data, nil
}

// readFile is split out so tests can substitute it without touching the
// filesystem. The default uses [os.ReadFile] via [fs.ReadFile] on the OS root.
var readFile = readFileImpl

// HTTPLoader resolves http:// and https:// URIs over the network. It is
// HTTPS-only by default; AllowHTTP must be set to true to follow plain http://
// URLs. Concurrent requests for the same URI share a single network round-trip
// via an inline single-flight.
type HTTPLoader struct {
	// Client is the http.Client used to perform requests. nil falls back
	// to a per-call client built from Timeout.
	Client *http.Client
	// Timeout caps the duration of a single Load. Default: 10 s.
	Timeout time.Duration
	// MaxBodySize caps the response body size in bytes. Default: 10 MiB.
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

	cacheMu    sync.RWMutex
	cacheEntry map[string]*httpCacheEntry
	flight     sync.Map // uri → *httpInflight
}

type httpCacheEntry struct {
	data    []byte
	expires time.Time
}

type httpInflight struct {
	wg   sync.WaitGroup
	data []byte
	err  error
}

// httpDefaultTimeout / httpDefaultMaxBody are exposed as variables so tests
// can shrink them without wiring options through every fixture.
var (
	httpDefaultTimeout = 10 * time.Second
	httpDefaultMaxBody = int64(10 * 1024 * 1024) // 10 MiB
)

// Load implements [Loader].
func (l *HTTPLoader) Load(uri string) ([]byte, error) {
	parsed, err := url.Parse(uri)
	if err != nil {
		return nil, &LoaderError{URI: uri, Cause: fmt.Errorf("parse: %w", err)}
	}
	switch parsed.Scheme {
	case "https":
		// Always allowed.
	case "http":
		if !l.AllowHTTP {
			return nil, &LoaderError{URI: uri, Cause: ErrLoaderRejected}
		}
	default:
		return nil, &LoaderError{URI: uri, Cause: ErrLoaderRejected}
	}

	// Cache check.
	if l.Cache > 0 {
		if data, ok := l.cacheGet(uri); ok {
			out := make([]byte, len(data))
			copy(out, data)
			return out, nil
		}
	}

	// Single-flight: identical concurrent requests for the same URI share
	// one network round-trip.
	flight := &httpInflight{}
	flight.wg.Add(1)
	actual, loaded := l.flight.LoadOrStore(uri, flight)
	if loaded {
		other, ok := actual.(*httpInflight)
		if !ok {
			return nil, &LoaderError{URI: uri, Cause: errInternalInflightInvalid}
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

func (l *HTTPLoader) fetch(uri string) ([]byte, error) {
	timeout := l.Timeout
	if timeout <= 0 {
		timeout = httpDefaultTimeout
	}
	maxBody := l.MaxBodySize
	if maxBody <= 0 {
		maxBody = httpDefaultMaxBody
	}
	client := l.Client
	if client == nil {
		client = &http.Client{Timeout: timeout}
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, uri, http.NoBody)
	if err != nil {
		return nil, &LoaderError{URI: uri, Cause: fmt.Errorf("build request: %w", err)}
	}
	if l.RequestDecorator != nil {
		l.RequestDecorator(req)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, &LoaderError{URI: uri, Cause: fmt.Errorf("get: %w", err)}
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, &LoaderError{URI: uri, Cause: fmt.Errorf("%w: %d", errHTTPNon2xx, resp.StatusCode)}
	}
	limited := io.LimitReader(resp.Body, maxBody+1)
	data, err := io.ReadAll(limited)
	if err != nil {
		return nil, &LoaderError{URI: uri, Cause: fmt.Errorf("read body: %w", err)}
	}
	if int64(len(data)) > maxBody {
		return nil, &LoaderError{URI: uri, Cause: fmt.Errorf("%w: %d bytes", errHTTPBodyTooLarge, maxBody)}
	}
	return data, nil
}

func (l *HTTPLoader) cacheGet(uri string) ([]byte, bool) {
	l.cacheMu.RLock()
	defer l.cacheMu.RUnlock()
	entry, ok := l.cacheEntry[uri]
	if !ok {
		return nil, false
	}
	if time.Now().After(entry.expires) {
		return nil, false
	}
	return entry.data, true
}

func (l *HTTPLoader) cachePut(uri string, data []byte) {
	l.cacheMu.Lock()
	defer l.cacheMu.Unlock()
	if l.cacheEntry == nil {
		l.cacheEntry = make(map[string]*httpCacheEntry)
	}
	stored := make([]byte, len(data))
	copy(stored, data)
	l.cacheEntry[uri] = &httpCacheEntry{data: stored, expires: time.Now().Add(l.Cache)}
}

// EmbedLoader wraps an [embed.FS] so schemas bundled into the binary can be
// referenced via embed:// URIs (e.g. embed://schemas/user.json resolves to
// the FS path "schemas/user.json").
type EmbedLoader struct {
	// FS is the embedded filesystem to serve from. Required.
	FS embed.FS
}

// Load implements [Loader].
func (l EmbedLoader) Load(uri string) ([]byte, error) {
	parsed, err := url.Parse(uri)
	if err != nil {
		return nil, &LoaderError{URI: uri, Cause: fmt.Errorf("parse: %w", err)}
	}
	if parsed.Scheme != "embed" {
		return nil, &LoaderError{URI: uri, Cause: ErrLoaderRejected}
	}
	// embed://host/path → take "host/path" as the FS path; embed:///path
	// (host empty) → take "path".
	path := strings.TrimPrefix(parsed.Host+parsed.Path, "/")
	if path == "" {
		return nil, &LoaderError{URI: uri, Cause: errEmbedEmptyPath}
	}
	data, err := fs.ReadFile(l.FS, path)
	if err != nil {
		return nil, &LoaderError{URI: uri, Cause: err}
	}
	return data, nil
}

// ChainLoader tries each [Loader] in order and returns the first non-rejected
// result. A rejection is any error that wraps [ErrLoaderRejected]; any other
// error short-circuits the chain so that a real I/O failure surfaces.
type ChainLoader []Loader

// Load implements [Loader].
func (c ChainLoader) Load(uri string) ([]byte, error) {
	if len(c) == 0 {
		return nil, &LoaderError{URI: uri, Cause: ErrLoaderRejected}
	}
	var lastErr error
	for _, l := range c {
		data, err := l.Load(uri)
		if err == nil {
			return data, nil
		}
		if !errors.Is(err, ErrLoaderRejected) {
			return nil, fmt.Errorf("loader: %w", err)
		}
		lastErr = err
	}
	return nil, lastErr
}

// defaultLoaderOnce ensures we build the default loader exactly once and
// hand the same singleton out to every caller. The chain combines the
// embedded standard meta-schemas with an HTTPS-only HTTPLoader.
var (
	defaultLoaderOnce  sync.Once
	defaultLoaderValue Loader
)

// DefaultLoader returns the package's default [Loader]: a [ChainLoader]
// containing the embedded standard meta-schemas (so meta-schema refs resolve
// without network access) followed by an HTTPS-only [HTTPLoader] with sane
// defaults.
//
// The returned Loader is shared across calls; callers that need different
// behavior should build their own [ChainLoader].
func DefaultLoader() Loader {
	defaultLoaderOnce.Do(func() {
		meta := embeddedMetaMapLoader()
		httpLoader := &HTTPLoader{
			Timeout:     httpDefaultTimeout,
			MaxBodySize: httpDefaultMaxBody,
			AllowHTTP:   false,
		}
		defaultLoaderValue = ChainLoader{meta, httpLoader}
	})
	return defaultLoaderValue
}

// embeddedMetaMapLoader returns a [MapLoader] preloaded with the canonical
// meta-schema URI → bytes for every supported draft, plus the per-vocabulary
// meta-schemas embedded under meta/draft-2019-09 / meta/draft-2020-12.
//
// The mapping uses both the canonical URLs (https://...) and the http://
// variants for legacy drafts so a Draft 4 / 6 / 7 schema can resolve $schema
// to the embedded copy without flipping schemes.
func embeddedMetaMapLoader() MapLoader {
	m := MapLoader{}
	for d, path := range metaSchemaPaths {
		data, err := fs.ReadFile(metaSchemaFS, path)
		if err != nil {
			continue
		}
		metaURL := d.MetaSchemaURL()
		m[metaURL] = data
		// Trailing-hash variant.
		if trimmed, found := strings.CutSuffix(metaURL, "#"); found {
			m[trimmed] = data
		} else {
			m[metaURL+"#"] = data
		}
		// http <-> https swap for the legacy drafts.
		switch {
		case strings.HasPrefix(metaURL, "http://"):
			alt := "https://" + strings.TrimPrefix(metaURL, "http://")
			m[alt] = data
		case strings.HasPrefix(metaURL, "https://"):
			alt := "http://" + strings.TrimPrefix(metaURL, "https://")
			m[alt] = data
		}
	}
	// Per-vocabulary meta-schemas. Walk the embedded FS and, for every
	// .json file under meta/draft-*/, parse its $id and register the URI.
	registerVocabMeta(m, "meta/draft-2019-09/meta")
	registerVocabMeta(m, "meta/draft-2020-12/meta")
	// Output-format meta-schema (Draft 2020-12). Register by its canonical
	// $id so tests and callers can $ref it without network access.
	if data, err := fs.ReadFile(metaSchemaFS, "meta/output-2020-12.json"); err == nil {
		if id := extractID(data); id != "" {
			m[id] = data
			if trimmed, found := strings.CutSuffix(id, "#"); found {
				m[trimmed] = data
			} else {
				m[id+"#"] = data
			}
		}
	}
	// OpenAPI 3.1 Schema Object dialect meta-schema. Registers under its
	// canonical $id ([OASDialectURL]) so callers declaring
	// `$schema: "https://spec.openapis.org/oas/3.1/dialect/base"` can
	// resolve the dialect offline.
	if data, err := fs.ReadFile(metaSchemaFS, "meta/openapi-3.1-dialect.json"); err == nil {
		if id := extractID(data); id != "" {
			m[id] = data
			if trimmed, found := strings.CutSuffix(id, "#"); found {
				m[trimmed] = data
			} else {
				m[id+"#"] = data
			}
		}
	}
	return m
}

// registerVocabMeta reads every JSON file under dir from [metaSchemaFS],
// pulls out its $id, and adds (URL → bytes) entries to m. Files without a
// readable $id are skipped silently — they are not part of the spec set.
func registerVocabMeta(m MapLoader, dir string) {
	entries, err := fs.ReadDir(metaSchemaFS, dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		path := dir + "/" + e.Name()
		data, err := fs.ReadFile(metaSchemaFS, path)
		if err != nil {
			continue
		}
		id := extractID(data)
		if id == "" {
			continue
		}
		m[id] = data
		if trimmed, found := strings.CutSuffix(id, "#"); found {
			m[trimmed] = data
		} else {
			m[id+"#"] = data
		}
	}
}

// extractID is a tiny helper that pulls the "$id" property out of a JSON
// document via a minimal scan. We avoid a full json.Unmarshal here so the
// init cost stays small even with the full meta-schema corpus.
func extractID(data []byte) string {
	const key = `"$id"`
	idx := indexBytesString(data, key)
	if idx < 0 {
		return ""
	}
	rest := data[idx+len(key):]
	// Skip whitespace + colon + whitespace.
	for len(rest) > 0 && (rest[0] == ' ' || rest[0] == '\t' || rest[0] == '\n' || rest[0] == '\r') {
		rest = rest[1:]
	}
	if len(rest) == 0 || rest[0] != ':' {
		return ""
	}
	rest = rest[1:]
	for len(rest) > 0 && (rest[0] == ' ' || rest[0] == '\t' || rest[0] == '\n' || rest[0] == '\r') {
		rest = rest[1:]
	}
	if len(rest) == 0 || rest[0] != '"' {
		return ""
	}
	rest = rest[1:]
	end := indexBytesString(rest, `"`)
	if end < 0 {
		return ""
	}
	return string(rest[:end])
}

func indexBytesString(b []byte, s string) int {
	return bytes.Index(b, []byte(s))
}
