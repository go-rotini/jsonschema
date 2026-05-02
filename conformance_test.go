package jsonschema

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
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
	for _, cfg := range drafts {
		t.Run(cfg.dir, func(t *testing.T) {
			runSuiteDraft(t, cfg.dir, cfg.draft)
		})
	}
}

func runSuiteDraft(t *testing.T, dir string, draft Draft) {
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
