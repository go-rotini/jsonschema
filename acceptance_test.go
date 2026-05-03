package jsonschema

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// acceptanceFixtureDir is the source of every real-world schema fixture
// exercised by [TestAcceptance]. Fixtures are stored alongside their
// passing/failing instance pair in instances/<name>.{pass,fail}.json.
const acceptanceFixtureDir = "testdata/acceptance"

// TestAcceptance walks every JSON Schema in testdata/acceptance/ and verifies
// that:
//
//  1. The schema compiles cleanly via [Compile].
//  2. The schema's bytes round-trip through [Schema.MarshalJSON] +
//     [CompileValue], producing an equivalent compiled artifact.
//  3. The matched-by-name passing instance validates successfully.
//  4. The matched-by-name failing instance is rejected with at least one
//     [ValidationError].
//  5. Every output format ([OutputFlag], [OutputBasic], [OutputDetailed],
//     [OutputVerbose]) emits valid JSON for both pass and fail outcomes.
//
// The fixtures cover well-known real-world schemas (OpenAPI 3.1, AsyncAPI
// 2.6, GeoJSON, Avro, JSON Patch) plus a hand-rolled "kitchen-sink" exercising
// every keyword.
func TestAcceptance(t *testing.T) {
	matches, err := filepath.Glob(filepath.Join(acceptanceFixtureDir, "*.json"))
	if err != nil {
		t.Fatalf("glob acceptance dir: %v", err)
	}
	if len(matches) == 0 {
		t.Fatalf("no acceptance fixtures under %s", acceptanceFixtureDir)
	}
	for _, path := range matches {
		name := strings.TrimSuffix(filepath.Base(path), ".json")
		t.Run(name, func(t *testing.T) {
			runAcceptance(t, name, path)
		})
	}
}

func runAcceptance(t *testing.T, name, schemaPath string) {
	t.Helper()
	schemaBytes, err := os.ReadFile(schemaPath)
	if err != nil {
		t.Fatalf("read schema: %v", err)
	}

	// (1) Compile.
	schema, err := Compile(schemaBytes)
	if err != nil {
		t.Fatalf("compile %s: %v", name, err)
	}

	// (2) Marshal + recompile round-trip.
	marshaled, err := schema.MarshalJSON()
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var asValue any
	if err := json.Unmarshal(marshaled, &asValue); err != nil {
		t.Fatalf("unmarshal marshaled bytes: %v", err)
	}
	recompiled, err := CompileValue(asValue)
	if err != nil {
		t.Fatalf("recompile from MarshalJSON output: %v", err)
	}
	if got, want := recompiled.Draft(), schema.Draft(); got != want {
		t.Errorf("recompile draft mismatch: got %v want %v", got, want)
	}
	if got, want := recompiled.ID(), schema.ID(); got != want {
		t.Errorf("recompile $id mismatch: got %q want %q", got, want)
	}

	// (3, 4) Pass + fail instances.
	for _, suffix := range []string{"pass", "fail"} {
		instPath := filepath.Join(acceptanceFixtureDir, "instances", name+"."+suffix+".json")
		instBytes, err := os.ReadFile(instPath)
		if err != nil {
			t.Fatalf("read instance %s: %v", instPath, err)
		}
		res, err := schema.Validate(instBytes)
		if err != nil {
			t.Fatalf("validate %s instance: %v", suffix, err)
		}
		if res == nil {
			t.Fatalf("nil result for %s instance", suffix)
		}
		switch suffix {
		case "pass":
			if !res.Valid {
				for i, e := range res.Errors {
					t.Logf("  unexpected error[%d] at %s: %s", i, e.InstanceLocation, e.Message)
					if i >= 4 {
						break
					}
				}
				t.Fatalf("expected pass instance valid, got %d errors", len(res.Errors))
			}
		case "fail":
			if res.Valid {
				t.Fatalf("expected fail instance invalid, got valid result")
			}
			if len(res.Errors) == 0 {
				t.Fatalf("fail instance produced 0 errors but Valid=false")
			}
		}

		// (5) Output formats.
		for _, format := range []OutputFormat{OutputFlag, OutputBasic, OutputDetailed, OutputVerbose} {
			rendered := res.Output(format)
			if len(rendered) == 0 {
				t.Errorf("%s: %s output empty", suffix, format)
				continue
			}
			var sink any
			if err := json.Unmarshal(rendered, &sink); err != nil {
				t.Errorf("%s: %s output is not valid JSON: %v\n%s", suffix, format, err, rendered)
				continue
			}
		}
	}
}
