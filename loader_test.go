package jsonschema

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
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
