package jsonschema

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"testing"
)

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
