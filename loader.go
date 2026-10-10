package jsonschema

import (
	"bytes"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"path/filepath"
	"slices"
	"strings"
	"sync"
)

// Static error sentinels for loader.go's value-shaped failures.
var (
	errEmbedEmptyPath     = errors.New("embed: empty path")
	errPathEscapesRoot    = errors.New("file: path escapes root")
	errSymlinkEscapesRoot = errors.New("file: symlink target escapes root")
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
	rel := parsed.Path
	if rel == "" {
		rel = parsed.Opaque
	}
	rootAbs, err := filepath.Abs(l.Root)
	if err != nil {
		return nil, &LoaderError{URI: uri, Cause: fmt.Errorf("resolve root: %w", err)}
	}
	// Reject ".." segments before joining: filepath.Clean would otherwise
	// resolve file:///../etc/passwd to rootAbs/etc/passwd silently.
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
	// Symlink-aware second pass: filepath.Clean is purely lexical, so a
	// symlink inside Root pointing at /etc/passwd would still be followed
	// by os.ReadFile. Resolve symlinks on both Root and the joined path
	// (Root itself may be a symlink) and re-verify the prefix.
	resolvedRoot, err := evalSymlinks(rootAbs)
	if err != nil {
		return nil, &LoaderError{URI: uri, Cause: fmt.Errorf("resolve root: %w", err)}
	}
	resolvedPath, err := evalSymlinks(joined)
	if err != nil {
		// Pass through the underlying I/O error (covers ENOENT) so callers
		// can distinguish "not found" from "rejected".
		return nil, &LoaderError{URI: uri, Cause: fmt.Errorf("resolve path: %w", err)}
	}
	rel3, err := filepath.Rel(resolvedRoot, resolvedPath)
	if err != nil || rel3 == ".." || strings.HasPrefix(rel3, ".."+string(filepath.Separator)) {
		return nil, &LoaderError{URI: uri, Cause: fmt.Errorf("%w: %w", errSymlinkEscapesRoot, ErrLoaderRejected)}
	}
	data, err := readFile(resolvedPath)
	if err != nil {
		return nil, &LoaderError{URI: uri, Cause: err}
	}
	return data, nil
}

// evalSymlinks is overridable so tests can substitute it without touching
// the filesystem.
var evalSymlinks = filepath.EvalSymlinks

// readFile is overridable so tests can substitute it without touching the
// filesystem.
var readFile = readFileImpl

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

// MetaLoader returns a [Loader] that serves the embedded standard
// meta-schemas (every supported draft, its per-vocabulary meta-schemas, the
// 2020-12 output schema and the OpenAPI 3.1 dialect) under both their http://
// and https:// URIs. It never touches the file system or the network. The
// returned Loader is shared across calls.
//
// Chain it in front of another loader to keep meta-schema refs offline:
//
//	jsonschema.ChainLoader{jsonschema.MetaLoader(), myLoader}
func MetaLoader() Loader {
	return metaLoader()
}

var metaLoader = sync.OnceValue(func() Loader { return embeddedMetaMapLoader() })

// DefaultLoader returns the package's default [Loader], used when no
// [WithLoader] option is given: the embedded standard meta-schemas only
// ([MetaLoader]). Any other $ref target fails with a [*LoaderError] that
// wraps [ErrLoaderRejected] and says how to load it; to resolve https://
// refs, pass a loader from the github.com/go-rotini/jsonschema/httploader
// package:
//
//	jsonschema.Compile(src, jsonschema.WithLoader(httploader.Default()))
//
// The returned Loader is shared across calls.
func DefaultLoader() Loader {
	return defaultLoader{}
}

// defaultLoader serves [MetaLoader] and turns a miss into an error that
// names the fix.
type defaultLoader struct{}

// Load implements [Loader].
func (defaultLoader) Load(uri string) ([]byte, error) {
	// MetaLoader is a MapLoader, whose only failure is a rejection.
	data, err := MetaLoader().Load(uri)
	if err != nil {
		return nil, &LoaderError{URI: uri, Cause: defaultLoaderMissError{uri: uri}}
	}
	return data, nil
}

// defaultLoaderMissError is the cause of a [DefaultLoader] miss. It wraps
// [ErrLoaderRejected] so a [ChainLoader] still falls through.
type defaultLoaderMissError struct {
	uri string
}

func (e defaultLoaderMissError) Error() string {
	const served = "not loaded: the default loader only serves the embedded meta-schemas; "
	switch {
	case strings.HasPrefix(e.uri, "https://"):
		return "remote $ref " + served + "use jsonschema.WithLoader(httploader.Default()) to fetch it"
	case strings.HasPrefix(e.uri, "http://"):
		return "remote $ref " + served +
			"use jsonschema.WithLoader(&httploader.Loader{AllowHTTP: true}) to fetch it over plain HTTP"
	default:
		return "$ref " + served + "register it with Compiler.AddResource or pass a Loader with jsonschema.WithLoader"
	}
}

func (defaultLoaderMissError) Unwrap() error { return ErrLoaderRejected }

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
	registerVocabMeta(m, "meta/draft-2019-09/meta")
	registerVocabMeta(m, "meta/draft-2020-12/meta")
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

// extractID pulls the "$id" property out of a JSON document via a minimal
// scan, avoiding a full json.Unmarshal so the init cost stays small.
func extractID(data []byte) string {
	const key = `"$id"`
	idx := indexBytesString(data, key)
	if idx < 0 {
		return ""
	}
	rest := data[idx+len(key):]
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
