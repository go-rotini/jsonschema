package jsonschema

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
)

// suiteCase mirrors one of the JSON Schema Test Suite's group entries.
type suiteCase struct {
	Description string          `json:"description"`
	Schema      json.RawMessage `json:"schema"`
	Tests       []suiteTestRun  `json:"tests"`
}

type suiteTestRun struct {
	Description string          `json:"description"`
	Data        json.RawMessage `json:"data"`
	Valid       bool            `json:"valid"`
}

// suiteRoot is the path to the cloned JSON-Schema-Test-Suite. The Makefile's
// clone-test-suite target populates it.
const suiteRoot = "testdata/JSON-Schema-Test-Suite"

// expectedPassesPath is the canonical location of the committed pass-count
// regression baseline. It lives under testdata/ (not the gitignored cloned
// suite directory) so the file is checked in.
const expectedPassesPath = "testdata/.expected-passes.json"

// expectedPassesFile is the on-disk shape of the regression baseline.
type expectedPassesFile struct {
	Doc    string                       `json:"_doc,omitempty"`
	Drafts map[string]expectedPassEntry `json:"drafts"`
}

type expectedPassEntry struct {
	Pass  int `json:"pass"`
	Total int `json:"total"`
}

// suiteResults is the per-draft outcome accumulator the regression guard
// consumes after the suite runs.
type suiteResults struct {
	mu      sync.Mutex
	results map[string]expectedPassEntry
}

func (r *suiteResults) record(name string, pass, total int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.results == nil {
		r.results = map[string]expectedPassEntry{}
	}
	r.results[name] = expectedPassEntry{Pass: pass, Total: total}
}

// TestJSONSchemaTestSuite runs the cloned JSON Schema Test Suite against the
// validator. We currently target Draft 2020-12 (primary) and report per-draft
// pass rates; the optional/ subdirectory is excluded (formats / content
// assertion live in Phase 6).
func TestJSONSchemaTestSuite(t *testing.T) {
	if _, err := os.Stat(suiteRoot); os.IsNotExist(err) {
		t.Skipf("test suite not cloned at %s; run `make clone-test-suite`", suiteRoot)
	}
	type draftCfg struct {
		dir   string
		draft Draft
	}
	drafts := []draftCfg{
		{dir: "draft2020-12", draft: Draft202012},
		{dir: "draft2019-09", draft: Draft201909},
		{dir: "draft7", draft: Draft7},
		{dir: "draft6", draft: Draft6},
		{dir: "draft4", draft: Draft4},
	}
	results := &suiteResults{}
	for _, cfg := range drafts {
		t.Run(cfg.dir, func(t *testing.T) {
			runSuiteDraft(t, cfg.dir, cfg.draft, results)
		})
	}
	checkExpectedPasses(t, results)
}

// checkExpectedPasses compares the per-draft pass counts against the committed
// baseline at testdata/.expected-passes.json. Drops fail loudly with a diff;
// increases print a warning so the maintainer can bump the baseline; a
// missing file emits a notice and snapshots the current counts.
func checkExpectedPasses(t *testing.T, results *suiteResults) {
	t.Helper()
	results.mu.Lock()
	defer results.mu.Unlock()
	if len(results.results) == 0 {
		return
	}
	current := expectedPassesFile{
		Doc:    "Per-draft pass counts for the JSON Schema Test Suite. Updated when conformance pass counts change. A drop fails CI; an increase warns and prints the new count for the maintainer to commit.",
		Drafts: results.results,
	}
	data, err := os.ReadFile(expectedPassesPath)
	if err != nil {
		// Missing file: snapshot the current counts to a sibling
		// .actual-passes.json and emit a notice.
		t.Logf("NOTICE: %s missing; current counts: %s", expectedPassesPath, formatExpectedPasses(current))
		writeActualPasses(t, current)
		return
	}
	var prev expectedPassesFile
	if err := json.Unmarshal(data, &prev); err != nil {
		t.Errorf("parse %s: %v", expectedPassesPath, err)
		return
	}
	regressed := false
	improved := false
	keys := make([]string, 0, len(current.Drafts))
	for k := range current.Drafts {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, name := range keys {
		got := current.Drafts[name]
		want, ok := prev.Drafts[name]
		if !ok {
			t.Logf("NOTICE: new draft %s not in baseline (%d/%d). Add to %s.", name, got.Pass, got.Total, expectedPassesPath)
			improved = true
			continue
		}
		switch {
		case got.Pass < want.Pass:
			t.Errorf("REGRESSION: draft %s: %d/%d (expected ≥ %d/%d). Drop of %d test(s).",
				name, got.Pass, got.Total, want.Pass, want.Total, want.Pass-got.Pass)
			regressed = true
		case got.Pass > want.Pass:
			t.Logf("IMPROVEMENT: draft %s: %d/%d (was %d/%d). Bump %s to capture the new baseline.",
				name, got.Pass, got.Total, want.Pass, want.Total, expectedPassesPath)
			improved = true
		case got.Total != want.Total:
			t.Logf("NOTE: draft %s total changed: %d/%d (was %d/%d).",
				name, got.Pass, got.Total, want.Pass, want.Total)
		}
	}
	if !regressed && !improved {
		t.Logf("expected-passes baseline matched: %s", formatExpectedPasses(current))
	}
}

func formatExpectedPasses(f expectedPassesFile) string {
	keys := make([]string, 0, len(f.Drafts))
	for k := range f.Drafts {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	for i, k := range keys {
		if i > 0 {
			b.WriteString(", ")
		}
		v := f.Drafts[k]
		fmt.Fprintf(&b, "%s=%d/%d", k, v.Pass, v.Total)
	}
	return b.String()
}

func writeActualPasses(t *testing.T, f expectedPassesFile) {
	t.Helper()
	out := filepath.Join(filepath.Dir(expectedPassesPath), ".actual-passes.json")
	data, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		t.Logf("marshal actual-passes: %v", err)
		return
	}
	if err := os.WriteFile(out, append(data, '\n'), 0o644); err != nil {
		t.Logf("write actual-passes: %v", err)
		return
	}
	t.Logf("wrote actual counts to %s", out)
}

func runSuiteDraft(t *testing.T, dir string, draft Draft, results *suiteResults) {
	t.Helper()
	dirPath := filepath.Join(suiteRoot, "tests", dir)
	if _, err := os.Stat(dirPath); os.IsNotExist(err) {
		t.Skipf("draft directory not present: %s", dirPath)
	}
	loader := buildSuiteLoader(t)

	var pass, fail int
	failByCase := map[string]int{}
	walkErr := filepath.Walk(dirPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			// Skip the optional/ tree (formats + content assertion = Phase 6).
			if info.Name() == "optional" {
				return filepath.SkipDir
			}
			return nil
		}
		if filepath.Ext(path) != ".json" {
			return nil
		}
		rel, _ := filepath.Rel(dirPath, path)
		groupPass, groupFail := runSuiteFile(t, path, draft, loader, failByCase)
		pass += groupPass
		fail += groupFail
		_ = rel
		return nil
	})
	if walkErr != nil {
		t.Fatalf("walk %s: %v", dirPath, walkErr)
	}
	total := pass + fail
	if total == 0 {
		t.Skipf("no test cases found for %s", dir)
	}
	rate := float64(pass) / float64(total) * 100
	t.Logf("%s: %d/%d cases pass (%.1f%%)", dir, pass, total, rate)
	if results != nil {
		results.record(dir, pass, total)
	}
	if testing.Verbose() {
		printSuiteFailures(t, failByCase)
	}
}

func printSuiteFailures(t *testing.T, failByCase map[string]int) {
	t.Helper()
	type kv struct {
		k string
		v int
	}
	var top []kv
	for k, v := range failByCase {
		top = append(top, kv{k, v})
	}
	sort.Slice(top, func(i, j int) bool {
		if top[i].v != top[j].v {
			return top[i].v > top[j].v
		}
		return top[i].k < top[j].k
	})
	limit := 30
	if len(top) < limit {
		limit = len(top)
	}
	for i := 0; i < limit; i++ {
		t.Logf("  failing: %s (%d)", top[i].k, top[i].v)
	}
}

func runSuiteFile(t *testing.T, path string, draft Draft, loader Loader, failByCase map[string]int) (int, int) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Errorf("read %s: %v", path, err)
		return 0, 0
	}
	var groups []suiteCase
	if err := json.Unmarshal(data, &groups); err != nil {
		t.Errorf("parse %s: %v", path, err)
		return 0, 0
	}
	pass, fail := 0, 0
	for gi, g := range groups {
		schema, err := Compile([]byte(g.Schema), WithDefaultDraft(draft), WithLoader(loader))
		if err != nil {
			// Compile failure is treated as failing every test in the group.
			fail += len(g.Tests)
			key := fmt.Sprintf("%s#%d (%s)", filepath.Base(path), gi, g.Description)
			failByCase[key] += len(g.Tests)
			continue
		}
		for _, ts := range g.Tests {
			res, err := schema.Validate([]byte(ts.Data))
			if err != nil || res == nil {
				fail++
				key := fmt.Sprintf("%s: %s / %s", filepath.Base(path), g.Description, ts.Description)
				failByCase[key]++
				continue
			}
			if res.Valid == ts.Valid {
				pass++
				continue
			}
			fail++
			key := fmt.Sprintf("%s: %s / %s", filepath.Base(path), g.Description, ts.Description)
			failByCase[key]++
		}
	}
	return pass, fail
}

// buildSuiteLoader returns a loader that maps the "remotes/" tree of the
// test suite to a MapLoader keyed under the canonical http://localhost:1234
// URI prefix the suite uses.
func buildSuiteLoader(t *testing.T) Loader {
	t.Helper()
	remotesDir := filepath.Join(suiteRoot, "remotes")
	m := MapLoader{}
	_ = filepath.Walk(remotesDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil //nolint:nilerr // skip unreadable / directory entries silently
		}
		if filepath.Ext(path) != ".json" {
			return nil
		}
		rel, err := filepath.Rel(remotesDir, path)
		if err != nil {
			return nil //nolint:nilerr // skip files outside remotesDir
		}
		// JSON Schema Test Suite serves remotes/ at http://localhost:1234/.
		uri := "http://localhost:1234/" + filepath.ToSlash(rel)
		data, err := os.ReadFile(path)
		if err != nil {
			return nil //nolint:nilerr // skip unreadable file
		}
		m[uri] = data
		// Also register draft-namespaced variants (e.g.
		// http://localhost:1234/draft2020-12/foo.json) — already covered by
		// the rel walk.
		return nil
	})
	chain := ChainLoader{m, DefaultLoader()}
	return chain
}

// TestJSONSchemaEdgeCases is a small set of bespoke conformance probes that
// the official suite doesn't cover but our spec § matrix calls for.
func TestJSONSchemaEdgeCases(t *testing.T) {
	cases := []struct {
		name   string
		schema string
		data   string
		valid  bool
	}{
		// `{"$ref":"#"}` is an infinite self-loop; validation must terminate
		// via WithMaxRefDepth and surface a failure rather than spinning.
		{"self-loop ref terminates", `{"$ref":"#"}`, `null`, false},
		{"trivial true", `true`, `null`, true},
		{"trivial false", `false`, `null`, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s, err := Compile([]byte(c.schema))
			if err != nil {
				t.Fatalf("compile: %v", err)
			}
			res, err := s.Validate([]byte(c.data))
			if err != nil {
				if !strings.Contains(err.Error(), "max ref depth") && c.valid {
					t.Fatalf("validate: %v", err)
				}
				return
			}
			if res.Valid != c.valid {
				t.Errorf("got valid=%v, want %v", res.Valid, c.valid)
			}
		})
	}
}

// TestJSONSchemaTestSuiteOptionalFormat walks the optional/format/ subtree of
// the JSON Schema Test Suite for every supported draft and runs each case
// with WithFormatAssertion(true).
func TestJSONSchemaTestSuiteOptionalFormat(t *testing.T) {
	if _, err := os.Stat(suiteRoot); os.IsNotExist(err) {
		t.Skipf("test suite not cloned at %s; run `make clone-test-suite`", suiteRoot)
	}
	drafts := []struct {
		dir   string
		draft Draft
	}{
		{"draft2020-12", Draft202012},
		{"draft2019-09", Draft201909},
		{"draft7", Draft7},
		{"draft6", Draft6},
		{"draft4", Draft4},
	}
	for _, cfg := range drafts {
		t.Run(cfg.dir, func(t *testing.T) {
			runOptionalFormat(t, cfg.dir, cfg.draft)
		})
	}
}

func runOptionalFormat(t *testing.T, dir string, draft Draft) {
	t.Helper()
	dirPath := filepath.Join(suiteRoot, "tests", dir, "optional", "format")
	if _, err := os.Stat(dirPath); os.IsNotExist(err) {
		t.Skipf("optional/format not present for %s", dir)
	}
	loader := buildSuiteLoader(t)

	pass, fail := 0, 0
	perFile := map[string][2]int{} // file → {pass, fail}
	failByCase := map[string]int{}
	walkErr := filepath.Walk(dirPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() || filepath.Ext(path) != ".json" {
			return nil
		}
		gp, gf := runOptionalSuiteFile(t, path, draft, loader, true, false, failByCase)
		base := filepath.Base(path)
		cur := perFile[base]
		perFile[base] = [2]int{cur[0] + gp, cur[1] + gf}
		pass += gp
		fail += gf
		return nil
	})
	if walkErr != nil {
		t.Fatalf("walk %s: %v", dirPath, walkErr)
	}
	total := pass + fail
	if total == 0 {
		t.Skip("no optional/format cases")
	}
	rate := float64(pass) / float64(total) * 100
	t.Logf("%s optional/format: %d/%d (%.1f%%)", dir, pass, total, rate)
	// Per-file breakdown for the closeout report.
	files := make([]string, 0, len(perFile))
	for f := range perFile {
		files = append(files, f)
	}
	sort.Strings(files)
	for _, f := range files {
		v := perFile[f]
		t.Logf("  %s: %d/%d", f, v[0], v[0]+v[1])
	}
}

// TestJSONSchemaTestSuiteOptionalContent walks the content tests in two
// passes: the suite's content.json files codify annotation-only behavior
// (every test expects valid: true regardless of decoded shape), so we run
// them in default mode. Then a synthetic assertion-mode subtest exercises
// our internal content-decoding path.
func TestJSONSchemaTestSuiteOptionalContent(t *testing.T) {
	if _, err := os.Stat(suiteRoot); os.IsNotExist(err) {
		t.Skipf("test suite not cloned at %s", suiteRoot)
	}
	cases := []struct {
		dir       string
		path      string
		draft     Draft
		assertion bool
	}{
		// Draft 2020-12 / 2019-09 codified content as annotation-only, and
		// the suite tests reflect that (every test expects valid:true).
		{"draft2020-12", "content.json", Draft202012, false},
		{"draft2019-09", "content.json", Draft201909, false},
		// Draft 7 specified content as assertion behavior; the suite tests
		// expect failures on bad encoding / bad JSON.
		{"draft7", filepath.Join("optional", "content.json"), Draft7, true},
	}
	for _, cfg := range cases {
		t.Run(cfg.dir, func(t *testing.T) {
			full := filepath.Join(suiteRoot, "tests", cfg.dir, cfg.path)
			if _, err := os.Stat(full); os.IsNotExist(err) {
				t.Skipf("content tests not present for %s", cfg.dir)
			}
			loader := buildSuiteLoader(t)
			failByCase := map[string]int{}
			pass, fail := runOptionalSuiteFile(t, full, cfg.draft, loader, false, cfg.assertion, failByCase)
			total := pass + fail
			if total == 0 {
				t.Skip("no content cases")
			}
			rate := float64(pass) / float64(total) * 100
			label := "annotation"
			if cfg.assertion {
				label = "assertion"
			}
			t.Logf("%s content (%s): %d/%d (%.1f%%)", cfg.dir, label, pass, total, rate)
		})
	}
}

func runOptionalSuiteFile(t *testing.T, path string, draft Draft, loader Loader,
	formatAssert, contentAssert bool, failByCase map[string]int,
) (int, int) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Errorf("read %s: %v", path, err)
		return 0, 0
	}
	var groups []suiteCase
	if err := json.Unmarshal(data, &groups); err != nil {
		t.Errorf("parse %s: %v", path, err)
		return 0, 0
	}
	pass, fail := 0, 0
	for gi, g := range groups {
		schema, err := Compile([]byte(g.Schema), WithDefaultDraft(draft), WithLoader(loader))
		if err != nil {
			fail += len(g.Tests)
			key := fmt.Sprintf("%s#%d (%s)", filepath.Base(path), gi, g.Description)
			failByCase[key] += len(g.Tests)
			continue
		}
		var opts []Option
		if formatAssert {
			opts = append(opts, WithFormatAssertion(true))
		}
		if contentAssert {
			opts = append(opts, WithContentAssertion(true))
		}
		for _, ts := range g.Tests {
			res, err := schema.Validate([]byte(ts.Data), opts...)
			if err != nil || res == nil {
				fail++
				key := fmt.Sprintf("%s: %s / %s", filepath.Base(path), g.Description, ts.Description)
				failByCase[key]++
				continue
			}
			if res.Valid == ts.Valid {
				pass++
				continue
			}
			fail++
			key := fmt.Sprintf("%s: %s / %s", filepath.Base(path), g.Description, ts.Description)
			failByCase[key]++
		}
	}
	return pass, fail
}
