package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

// runScript drives the dispatch loop with a pre-built request transcript and
// returns the line-delimited JSON responses for inspection by the caller.
func runScript(t *testing.T, requests ...any) []map[string]any {
	t.Helper()
	var in bytes.Buffer
	enc := json.NewEncoder(&in)
	enc.SetEscapeHTML(false)
	for _, req := range requests {
		if err := enc.Encode(req); err != nil {
			t.Fatalf("encode request: %v", err)
		}
	}
	var out bytes.Buffer
	if err := dispatch(&in, &out); err != nil && !errors.Is(err, errStop) {
		t.Fatalf("dispatch: %v", err)
	}
	var responses []map[string]any
	dec := json.NewDecoder(&out)
	for {
		var m map[string]any
		if err := dec.Decode(&m); err != nil {
			break
		}
		responses = append(responses, m)
	}
	return responses
}

func TestStartHandshake(t *testing.T) {
	resps := runScript(t, map[string]any{"cmd": "start", "version": 1}, map[string]any{"cmd": "stop"})
	if len(resps) != 1 {
		t.Fatalf("want 1 response, got %d: %v", len(resps), resps)
	}
	r := resps[0]
	if v, _ := r["version"].(float64); int(v) != 1 {
		t.Errorf("version: got %v", r["version"])
	}
	impl, ok := r["implementation"].(map[string]any)
	if !ok {
		t.Fatalf("implementation: missing or wrong type: %T", r["implementation"])
	}
	if got, _ := impl["language"].(string); got != "go" {
		t.Errorf("language: got %q", impl["language"])
	}
	if got, _ := impl["name"].(string); got != implementationName {
		t.Errorf("name: got %q want %q", impl["name"], implementationName)
	}
	dialects, ok := impl["dialects"].([]any)
	if !ok || len(dialects) == 0 {
		t.Fatalf("dialects: missing or empty: %v", impl["dialects"])
	}
	if got, _ := dialects[0].(string); got != "https://json-schema.org/draft/2020-12/schema" {
		t.Errorf("first dialect: got %q", got)
	}
}

func TestDialectAcceptAndReject(t *testing.T) {
	resps := runScript(t,
		map[string]any{"cmd": "start", "version": 1},
		map[string]any{"cmd": "dialect", "dialect": "https://json-schema.org/draft/2020-12/schema"},
		map[string]any{"cmd": "dialect", "dialect": "https://example.com/not-a-draft"},
		map[string]any{"cmd": "stop"},
	)
	if len(resps) != 3 {
		t.Fatalf("want 3 responses, got %d: %v", len(resps), resps)
	}
	if ok, _ := resps[1]["ok"].(bool); !ok {
		t.Errorf("known dialect: ok=%v want true", resps[1]["ok"])
	}
	if ok, _ := resps[2]["ok"].(bool); ok {
		t.Errorf("unknown dialect: ok=%v want false", resps[2]["ok"])
	}
}

func TestRunBasicValidation(t *testing.T) {
	caseObj := map[string]any{
		"description": "minLength enforcement",
		"schema":      map[string]any{"type": "string", "minLength": 3},
		"tests": []any{
			map[string]any{"description": "long enough", "instance": "hello", "valid": true},
			map[string]any{"description": "too short", "instance": "hi", "valid": false},
			map[string]any{"description": "wrong type", "instance": float64(7), "valid": false},
		},
	}
	resps := runScript(t,
		map[string]any{"cmd": "start", "version": 1},
		map[string]any{"cmd": "dialect", "dialect": "https://json-schema.org/draft/2020-12/schema"},
		map[string]any{"cmd": "run", "seq": 7, "case": caseObj},
		map[string]any{"cmd": "stop"},
	)
	if len(resps) != 3 {
		t.Fatalf("want 3 responses, got %d: %v", len(resps), resps)
	}
	run := resps[2]
	if seq, _ := run["seq"].(float64); int(seq) != 7 {
		t.Errorf("seq: got %v want 7", run["seq"])
	}
	results, ok := run["results"].([]any)
	if !ok || len(results) != 3 {
		t.Fatalf("results: got %v", run["results"])
	}
	wantValid := []bool{true, false, false}
	for i, r := range results {
		entry, ok := r.(map[string]any)
		if !ok {
			t.Fatalf("results[%d]: %T", i, r)
		}
		got, ok := entry["valid"].(bool)
		if !ok {
			t.Fatalf("results[%d]: missing valid: %v", i, entry)
		}
		if got != wantValid[i] {
			t.Errorf("results[%d]: valid=%v want %v", i, got, wantValid[i])
		}
	}
}

func TestRunBooleanSchema(t *testing.T) {
	caseObj := map[string]any{
		"description": "boolean false rejects everything",
		"schema":      false,
		"tests": []any{
			map[string]any{"description": "anything fails", "instance": 1, "valid": false},
			map[string]any{"description": "even null fails", "instance": nil, "valid": false},
		},
	}
	resps := runScript(t,
		map[string]any{"cmd": "start", "version": 1},
		map[string]any{"cmd": "dialect", "dialect": "https://json-schema.org/draft/2020-12/schema"},
		map[string]any{"cmd": "run", "seq": 1, "case": caseObj},
		map[string]any{"cmd": "stop"},
	)
	run := resps[2]
	results, _ := run["results"].([]any)
	if len(results) != 2 {
		t.Fatalf("results len: %d", len(results))
	}
	for i, r := range results {
		entry, _ := r.(map[string]any)
		if v, _ := entry["valid"].(bool); v {
			t.Errorf("results[%d]: valid=true, want false (boolean schema false)", i)
		}
	}
}

func TestRunCompileFailure(t *testing.T) {
	// "minimum" must be a number; using a non-numeric string triggers a
	// structural compile error before any test runs.
	caseObj := map[string]any{
		"description": "broken schema",
		"schema":      map[string]any{"minimum": "not-a-number"},
		"tests": []any{
			map[string]any{"description": "won't run", "instance": 5, "valid": true},
		},
	}
	resps := runScript(t,
		map[string]any{"cmd": "start", "version": 1},
		map[string]any{"cmd": "dialect", "dialect": "https://json-schema.org/draft/2020-12/schema"},
		map[string]any{"cmd": "run", "seq": 99, "case": caseObj},
		map[string]any{"cmd": "stop"},
	)
	run := resps[2]
	if errored, _ := run["errored"].(bool); !errored {
		t.Fatalf("want errored=true, got %v (full: %v)", run["errored"], run)
	}
	if seq, _ := run["seq"].(float64); int(seq) != 99 {
		t.Errorf("seq: got %v want 99", run["seq"])
	}
	ctx, _ := run["context"].(map[string]any)
	if msg, _ := ctx["message"].(string); msg == "" {
		t.Errorf("context.message: empty")
	}
}

func TestRunWithRegistry(t *testing.T) {
	// External-resource schema referenced via $ref. The registry entry is
	// passed alongside the case so AddResource can pre-register it.
	caseObj := map[string]any{
		"description": "ref into registry",
		"schema": map[string]any{
			"$ref": "https://example.com/positive-int.json",
		},
		"registry": map[string]any{
			"https://example.com/positive-int.json": map[string]any{
				"$id":     "https://example.com/positive-int.json",
				"type":    "integer",
				"minimum": 1,
			},
		},
		"tests": []any{
			map[string]any{"description": "positive", "instance": 5, "valid": true},
			map[string]any{"description": "zero rejected", "instance": 0, "valid": false},
		},
	}
	resps := runScript(t,
		map[string]any{"cmd": "start", "version": 1},
		map[string]any{"cmd": "dialect", "dialect": "https://json-schema.org/draft/2020-12/schema"},
		map[string]any{"cmd": "run", "seq": 11, "case": caseObj},
		map[string]any{"cmd": "stop"},
	)
	run := resps[2]
	if errored, _ := run["errored"].(bool); errored {
		t.Fatalf("unexpected errored=true: %v", run)
	}
	results, _ := run["results"].([]any)
	if len(results) != 2 {
		t.Fatalf("results: %v", results)
	}
	if v, _ := results[0].(map[string]any)["valid"].(bool); !v {
		t.Errorf("results[0]: want valid=true, got %v", results[0])
	}
	if v, _ := results[1].(map[string]any)["valid"].(bool); v {
		t.Errorf("results[1]: want valid=false, got %v", results[1])
	}
}

func TestStopExitsCleanly(t *testing.T) {
	var in bytes.Buffer
	in.WriteString(`{"cmd":"stop"}` + "\n")
	var out bytes.Buffer
	err := dispatch(&in, &out)
	if !errors.Is(err, errStop) {
		t.Fatalf("dispatch: want errStop, got %v", err)
	}
	if strings.TrimSpace(out.String()) != "" {
		t.Errorf("stop emitted output: %q", out.String())
	}
}

func TestUnknownCommand(t *testing.T) {
	var in bytes.Buffer
	in.WriteString(`{"cmd":"chunky-bacon"}` + "\n")
	var out bytes.Buffer
	err := dispatch(&in, &out)
	if err == nil {
		t.Fatalf("dispatch: want error for unknown command")
	}
	if !strings.Contains(err.Error(), "unknown command") {
		t.Errorf("error: %v", err)
	}
}

func TestEOFExitsCleanly(t *testing.T) {
	// No commands; dispatch should return nil on EOF.
	err := dispatch(&bytes.Buffer{}, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("dispatch on empty input: %v", err)
	}
}
