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

// TestStartUnsupportedVersion covers the !version=1 branch of handleStart.
func TestStartUnsupportedVersion(t *testing.T) {
	var in bytes.Buffer
	in.WriteString(`{"cmd":"start","version":2}` + "\n")
	var out bytes.Buffer
	err := dispatch(&in, &out)
	if err == nil {
		t.Fatal("expected error for unsupported version")
	}
	if !errors.Is(err, errUnsupportedProtocol) {
		t.Errorf("err = %v, want errUnsupportedProtocol", err)
	}
}

// TestRunBeforeStart covers the run-without-start path: the case
// processes successfully but uses DraftUnknown as default. Mainly we
// verify dispatch tolerates a run command before start.
func TestRunBeforeStart(t *testing.T) {
	caseObj := map[string]any{
		"description": "ok",
		"schema":      map[string]any{"type": "string"},
		"tests": []any{
			map[string]any{"description": "ok", "instance": "x"},
		},
	}
	var in bytes.Buffer
	enc := json.NewEncoder(&in)
	_ = enc.Encode(map[string]any{"cmd": "run", "seq": 1, "case": caseObj})
	_ = enc.Encode(map[string]any{"cmd": "stop"})
	var out bytes.Buffer
	if err := dispatch(&in, &out); err != nil && !errors.Is(err, errStop) {
		t.Fatalf("dispatch: %v", err)
	}
}

// TestRunCaseDecodeFailure covers the malformed-case branch of handleRun.
func TestRunCaseDecodeFailure(t *testing.T) {
	// Use a "case" value that isn't a JSON object.
	var in bytes.Buffer
	in.WriteString(`{"cmd":"start","version":1}` + "\n")
	in.WriteString(`{"cmd":"run","seq":42,"case":"not-an-object"}` + "\n")
	in.WriteString(`{"cmd":"stop"}` + "\n")
	var out bytes.Buffer
	if err := dispatch(&in, &out); err != nil && !errors.Is(err, errStop) {
		t.Fatalf("dispatch: %v", err)
	}
	// Parse responses: expect an errored envelope as the second one.
	dec := json.NewDecoder(&out)
	var responses []map[string]any
	for {
		var m map[string]any
		if err := dec.Decode(&m); err != nil {
			break
		}
		responses = append(responses, m)
	}
	if len(responses) < 2 {
		t.Fatalf("want >=2 responses, got %d: %v", len(responses), responses)
	}
	run := responses[1]
	if errored, _ := run["errored"].(bool); !errored {
		t.Errorf("expected errored=true, got %v", run)
	}
}

// TestRunEmptySchema covers the errEmptyCaseSchema path of compileCaseSchema.
func TestRunEmptySchema(t *testing.T) {
	// "case" with no schema field.
	caseObj := map[string]any{
		"description": "no schema",
		"tests": []any{
			map[string]any{"description": "x", "instance": 1},
		},
	}
	var in bytes.Buffer
	enc := json.NewEncoder(&in)
	_ = enc.Encode(map[string]any{"cmd": "start", "version": 1})
	_ = enc.Encode(map[string]any{"cmd": "run", "seq": 5, "case": caseObj})
	_ = enc.Encode(map[string]any{"cmd": "stop"})
	var out bytes.Buffer
	_ = dispatch(&in, &out)
	dec := json.NewDecoder(&out)
	var responses []map[string]any
	for {
		var m map[string]any
		if err := dec.Decode(&m); err != nil {
			break
		}
		responses = append(responses, m)
	}
	// Last response should be errored.
	if len(responses) == 0 {
		t.Fatal("no responses")
	}
	last := responses[len(responses)-1]
	if errored, _ := last["errored"].(bool); !errored {
		t.Errorf("expected errored=true, got %v", last)
	}
}

// TestEvaluateOneInstanceDecodeFailure covers the decode-error branch of
// evaluateOne by calling it directly with a malformed raw message.
func TestEvaluateOneInstanceDecodeFailure(t *testing.T) {
	// We need a *jsonschema.Schema but its only role is to be non-nil; the
	// decode-error path returns before calling ValidateValue.
	res := evaluateOne(nil, json.RawMessage(`not json`))
	if !res.Errored {
		t.Fatalf("want errored=true, got %+v", res)
	}
	if !strings.Contains(res.Context.Message, "decode instance") {
		t.Errorf("want 'decode instance' in msg, got %q", res.Context.Message)
	}
}

// TestDispatchScannerError covers the scanner-error branch.
func TestDispatchScannerError(t *testing.T) {
	// Provide a line longer than the maxScanBufferBytes cap so the scanner
	// surfaces an error.
	big := strings.Repeat("a", maxScanBufferBytes+10)
	var in bytes.Buffer
	in.WriteString(big + "\n")
	var out bytes.Buffer
	err := dispatch(&in, &out)
	if err == nil {
		t.Fatal("expected scanner error for oversize input")
	}
}

// TestDispatchInvalidJSON covers handleLine's decode-error branch.
func TestDispatchInvalidJSON(t *testing.T) {
	var in bytes.Buffer
	in.WriteString("not json\n")
	var out bytes.Buffer
	err := dispatch(&in, &out)
	if err == nil {
		t.Fatal("expected decode error")
	}
	if !strings.Contains(err.Error(), "decode") {
		t.Errorf("err = %v, want containing 'decode'", err)
	}
}

// TestDialectStateRecorded confirms that handleDialect updates dialect
// state and a subsequent run uses it. We pick a draft-7 schema that Compile
// would parse equivalently to 2020-12 (so this is mainly a state-flow
// check, not a validation-divergence one).
func TestDialectStateRecorded(t *testing.T) {
	caseObj := map[string]any{
		"description": "draft-7 schema",
		"schema": map[string]any{
			"type":      "string",
			"minLength": 3,
		},
		"tests": []any{
			map[string]any{"description": "ok", "instance": "abcd"},
			map[string]any{"description": "too short", "instance": "ab"},
		},
	}
	var in bytes.Buffer
	enc := json.NewEncoder(&in)
	_ = enc.Encode(map[string]any{"cmd": "start", "version": 1})
	_ = enc.Encode(map[string]any{"cmd": "dialect", "dialect": "http://json-schema.org/draft-07/schema#"})
	_ = enc.Encode(map[string]any{"cmd": "run", "seq": 1, "case": caseObj})
	_ = enc.Encode(map[string]any{"cmd": "stop"})
	var out bytes.Buffer
	if err := dispatch(&in, &out); err != nil && !errors.Is(err, errStop) {
		t.Fatalf("dispatch: %v", err)
	}
	dec := json.NewDecoder(&out)
	var responses []map[string]any
	for {
		var m map[string]any
		if err := dec.Decode(&m); err != nil {
			break
		}
		responses = append(responses, m)
	}
	if len(responses) < 3 {
		t.Fatalf("want >=3 responses, got %d", len(responses))
	}
	results, ok := responses[2]["results"].([]any)
	if !ok || len(results) != 2 {
		t.Fatalf("results: %v", responses[2])
	}
	if v, _ := results[0].(map[string]any)["valid"].(bool); !v {
		t.Errorf("test 0: want valid=true, got %v", results[0])
	}
	if v, _ := results[1].(map[string]any)["valid"].(bool); v {
		t.Errorf("test 1: want valid=false, got %v", results[1])
	}
}

// TestEvaluateOneValidateError covers evaluateOne's ValidateValue-error
// branch: a nil schema makes ValidateValue return an error (not a panic),
// which evaluateOne reports as an errored result.
func TestEvaluateOneValidateError(t *testing.T) {
	res := evaluateOne(nil, json.RawMessage(`"x"`))
	if !res.Errored {
		t.Errorf("expected errored=true, got %+v", res)
	}
}

// failingWriter always returns an error from Write.
type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("write fail") }

// TestHandleStartEncodeFailure covers the encode-error branch of handleStart.
func TestHandleStartEncodeFailure(t *testing.T) {
	enc := json.NewEncoder(failingWriter{})
	st := &state{}
	cmd := command{Cmd: "start", Version: 1}
	err := handleStart(cmd, st, enc)
	if err == nil {
		t.Fatal("expected encode error")
	}
	if !strings.Contains(err.Error(), "encode start response") {
		t.Errorf("err = %v", err)
	}
}

// TestHandleDialectEncodeFailureUnknownAndKnown covers both encode-error
// branches of handleDialect.
func TestHandleDialectEncodeFailureUnknownAndKnown(t *testing.T) {
	enc := json.NewEncoder(failingWriter{})
	st := &state{}
	// Unknown dialect path.
	if err := handleDialect(command{Cmd: "dialect", Dialect: "https://nope/"}, st, enc); err == nil {
		t.Error("expected encode error on unknown dialect")
	}
	// Known dialect path.
	if err := handleDialect(command{Cmd: "dialect", Dialect: "https://json-schema.org/draft/2020-12/schema"}, st, enc); err == nil {
		t.Error("expected encode error on known dialect")
	}
}

// TestHandleRunEncodeFailure covers the encode-error branch of the
// successful-results path of handleRun.
func TestHandleRunEncodeFailure(t *testing.T) {
	enc := json.NewEncoder(failingWriter{})
	st := &state{}
	caseObj := map[string]any{
		"description": "ok",
		"schema":      map[string]any{"type": "string"},
		"tests": []any{
			map[string]any{"description": "x", "instance": "y"},
		},
	}
	caseRaw, _ := json.Marshal(caseObj)
	cmd := command{Cmd: "run", Seq: json.RawMessage(`1`), Case: caseRaw}
	if err := handleRun(cmd, st, enc); err == nil {
		t.Error("expected encode error")
	}
}

// TestWriteErroredEncodeFailure covers the encode-error branch of
// writeErrored.
func TestWriteErroredEncodeFailure(t *testing.T) {
	enc := json.NewEncoder(failingWriter{})
	if err := writeErrored(enc, json.RawMessage(`1`), "msg"); err == nil {
		t.Error("expected encode error")
	}
}

// TestNewCaseCompilerInvalidRegistryEntry covers the AddResource-failure
// branch of newCaseCompiler.
func TestNewCaseCompilerInvalidRegistryEntry(t *testing.T) {
	registry := map[string]json.RawMessage{
		"https://example.com/x": json.RawMessage(`not json`),
	}
	if _, err := newCaseCompiler(0, registry); err == nil {
		t.Fatal("expected error from invalid JSON in registry")
	}
}

// TestRunDispatchUnsupportedProtocolDoesNotCrash covers the dispatch-error
// branch when a downstream protocol-handshake error is returned.
func TestRunDispatchUnsupportedProtocolDoesNotCrash(t *testing.T) {
	var in bytes.Buffer
	in.WriteString(`{"cmd":"start","version":99}` + "\n")
	var out bytes.Buffer
	err := dispatch(&in, &out)
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, errUnsupportedProtocol) {
		t.Errorf("err = %v, want errUnsupportedProtocol", err)
	}
}

// TestDispatchSkipsBlankLines covers the empty-line short-circuit.
func TestDispatchSkipsBlankLines(t *testing.T) {
	var in bytes.Buffer
	in.WriteString("\n\n   \n" + `{"cmd":"stop"}` + "\n")
	var out bytes.Buffer
	err := dispatch(&in, &out)
	if !errors.Is(err, errStop) {
		t.Fatalf("err = %v, want errStop", err)
	}
	if strings.TrimSpace(out.String()) != "" {
		t.Errorf("blank lines emitted output: %q", out.String())
	}
}

// TestRunHelperHappyPath covers the runBowtie helper's success path.
func TestRunHelperHappyPath(t *testing.T) {
	in := bytes.NewBufferString(`{"cmd":"stop"}` + "\n")
	var out, errOut bytes.Buffer
	if code := runBowtie(nil, in, &out, &errOut); code != 0 {
		t.Errorf("run = %d, want 0; err=%q", code, errOut.String())
	}
}

// TestRunHelperErrorPath covers the runBowtie helper's non-stop-error path.
func TestRunHelperErrorPath(t *testing.T) {
	in := bytes.NewBufferString("not json\n")
	var out, errOut bytes.Buffer
	if code := runBowtie(nil, in, &out, &errOut); code != 1 {
		t.Errorf("run = %d, want 1", code)
	}
	if !strings.Contains(errOut.String(), "bowtie:") {
		t.Errorf("expected 'bowtie:' prefix in errOut: %q", errOut.String())
	}
}

// TestRunBowtie_help confirms -h/--help/help print the bowtie usage to stdout
// and exit 0 without blocking on stdin.
func TestRunBowtie_help(t *testing.T) {
	for _, arg := range []string{"-h", "--help", "help"} {
		var out, errOut bytes.Buffer
		// errReader guarantees the protocol loop is never entered: help must
		// short-circuit before any stdin read.
		if code := runBowtie([]string{arg}, errReader{}, &out, &errOut); code != 0 {
			t.Fatalf("%s: exit=%d, want 0", arg, code)
		}
		if !strings.Contains(out.String(), "usage: jsonschema bowtie") {
			t.Errorf("%s: stdout missing the bowtie usage:\n%s", arg, out.String())
		}
	}
}

// TestRunBowtie_unexpectedArg confirms an unrecognized argument is rejected
// (exit 2) instead of being ignored and blocking on stdin.
func TestRunBowtie_unexpectedArg(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := runBowtie([]string{"--nope"}, errReader{}, &out, &errOut); code != 2 {
		t.Fatalf("unexpected arg: exit=%d, want 2", code)
	}
	if !strings.Contains(errOut.String(), `unexpected argument "--nope"`) {
		t.Errorf("unexpected arg: stderr missing the rejection:\n%s", errOut.String())
	}
}
