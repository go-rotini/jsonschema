package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/go-rotini/jsonschema"
)

// implementationName is the Bowtie identifier reported by the "start"
// handshake. It is intentionally distinct from the Go module path so the
// dashboard can label rows independently from the import path.
const implementationName = "go-rotini-jsonschema"

// implementationVersion is the connector's protocol version. It tracks the
// parent module's tagged release.
const implementationVersion = "v0.1.0"

// maxScanBufferBytes caps the per-line scanner buffer. Bowtie cases can
// embed large schemas (the Test Suite has cases over 100 KB), so the
// default 64 KB MaxScanTokenSize is insufficient.
const maxScanBufferBytes = 10 * 1024 * 1024

// supportedDialects lists the meta-schema URIs the connector advertises in
// its "start" response. The order is informational only.
var supportedDialects = []string{
	"https://json-schema.org/draft/2020-12/schema",
	"https://json-schema.org/draft/2019-09/schema",
	"http://json-schema.org/draft-07/schema#",
	"http://json-schema.org/draft-06/schema#",
	"http://json-schema.org/draft-04/schema#",
}

// errStop is the sentinel returned by the dispatch loop when the harness
// sends a "stop" command. runBowtie treats it as a graceful exit.
var errStop = errors.New("bowtie: stop")

// errEmptyCaseSchema is returned by compileCaseSchema when the case
// envelope does not include a "schema" field.
var errEmptyCaseSchema = errors.New("bowtie: case schema is empty")

// errUnknownCommand wraps an unrecognized "cmd" value at the protocol
// boundary; the caller annotates it with the actual command name.
var errUnknownCommand = errors.New("bowtie: unknown command")

// errUnsupportedProtocol is returned when the harness opens with a
// "start" version this connector cannot speak.
var errUnsupportedProtocol = errors.New("bowtie: unsupported protocol version")

// command is the discriminated request envelope. Only the fields the
// connector inspects are pulled out here; the rest stay in raw form so the
// case handler can decode them at the right granularity.
type command struct {
	Cmd     string          `json:"cmd"`
	Version int             `json:"version,omitempty"`
	Dialect string          `json:"dialect,omitempty"`
	Seq     json.RawMessage `json:"seq,omitempty"`
	Case    json.RawMessage `json:"case,omitempty"`
}

// implementation is the descriptor reported by "start". The shape mirrors
// Bowtie's documented schema so the dashboard can render the row.
type implementation struct {
	Language  string   `json:"language"`
	Name      string   `json:"name"`
	Version   string   `json:"version"`
	Homepage  string   `json:"homepage"`
	Issues    string   `json:"issues"`
	Source    string   `json:"source"`
	Dialects  []string `json:"dialects"`
	OS        string   `json:"os,omitempty"`
	OSVersion string   `json:"os_version,omitempty"`
}

// startResponse is the wire form of the "start" reply.
type startResponse struct {
	Version        int            `json:"version"`
	Implementation implementation `json:"implementation"`
}

// dialectResponse is the wire form of the "dialect" reply.
type dialectResponse struct {
	OK bool `json:"ok"`
}

// testCase is one Bowtie case as decoded from "case".
type testCase struct {
	Description string                     `json:"description"`
	Comment     string                     `json:"comment,omitempty"`
	Schema      json.RawMessage            `json:"schema"`
	Tests       []testInstance             `json:"tests"`
	Registry    map[string]json.RawMessage `json:"registry,omitempty"`
}

// testInstance is one (instance, expected-validity) pair inside a case.
type testInstance struct {
	Description string          `json:"description"`
	Comment     string          `json:"comment,omitempty"`
	Instance    json.RawMessage `json:"instance"`
	Valid       bool            `json:"valid,omitempty"`
}

// runResponse is the wire form of the "run" reply for a successful compile.
type runResponse struct {
	Seq     json.RawMessage `json:"seq"`
	Results []testResult    `json:"results"`
}

// testResult is one entry inside runResponse.Results — either {"valid":...}
// for a normal evaluation, or {"errored":true,"context":...} for a panic.
type testResult struct {
	Valid    *bool        `json:"valid,omitempty"`
	Errored  bool         `json:"errored,omitempty"`
	Skipped  bool         `json:"skipped,omitempty"`
	Context  *errorReport `json:"context,omitempty"`
	Message  string       `json:"message,omitempty"`
	IssueURL string       `json:"issue_url,omitempty"`
}

// erroredResponse is the wire form of the "run" reply when compilation
// itself failed and no per-test results can be produced.
type erroredResponse struct {
	Seq     json.RawMessage `json:"seq"`
	Errored bool            `json:"errored"`
	Context *errorReport    `json:"context"`
}

// errorReport carries diagnostic detail to the harness.
type errorReport struct {
	Message     string `json:"message"`
	Traceback   string `json:"traceback,omitempty"`
	Stderr      string `json:"stderr,omitempty"`
	Description string `json:"description,omitempty"`
}

// state tracks per-process protocol state shared across commands. The
// connector is single-threaded by design (Bowtie issues commands serially
// over stdin), so a plain struct without a mutex is sufficient.
type state struct {
	started bool
	dialect jsonschema.Draft
}

// runBowtie is the "bowtie" subcommand: the stdin/stdout adapter that exposes
// go-rotini/jsonschema to the Bowtie cross-implementation conformance harness
// (https://bowtie.report). It takes no flags; the harness drives it over a
// single-line-JSON request/response protocol on stdin/stdout. Recognized
// commands are "start" (handshake → implementation descriptor), "dialect" (pin
// a meta-schema URI), "run" (one case: compile + per-test results), and "stop"
// (exit 0). The dispatch loop is factored out as dispatch(in, out) so tests can
// drive it over bytes.Buffer pipes.
func runBowtie(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	// bowtie takes no flags, so any argument is either a help request or a
	// mistake — reject it rather than silently blocking on stdin.
	if len(args) > 0 {
		switch args[0] {
		case "-h", "--help", "help":
			bowtieUsage(stdout)
			return 0
		default:
			fmt.Fprintf(stderr, "bowtie: unexpected argument %q\n", args[0])
			bowtieUsage(stderr)
			return 2
		}
	}
	if err := dispatch(stdin, stdout); err != nil && !errors.Is(err, errStop) {
		fmt.Fprintln(stderr, "bowtie:", err)
		return 1
	}
	return 0
}

// bowtieUsage writes the bowtie subcommand summary.
func bowtieUsage(w io.Writer) {
	fmt.Fprintln(w, "usage: jsonschema bowtie")
	fmt.Fprintln(w, "Bowtie conformance-harness connector. Takes no flags; the harness drives")
	fmt.Fprintln(w, "it over a single-line-JSON request/response protocol on stdin/stdout.")
}

// dispatch is the read-eval-print loop; tests drive it over bytes.Buffer
// pipes instead of invoking the binary.
func dispatch(in io.Reader, out io.Writer) error {
	scanner := bufio.NewScanner(in)
	scanner.Buffer(make([]byte, 0, 64*1024), maxScanBufferBytes)
	enc := json.NewEncoder(out)
	enc.SetEscapeHTML(false)
	st := &state{}
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		if err := handleLine(line, st, enc); err != nil {
			return err
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("scan stdin: %w", err)
	}
	return nil
}

// handleLine decodes one JSON line and dispatches it. Per-test panics
// are recovered downstream in handleRun; this function only surfaces
// protocol-level failures.
func handleLine(line []byte, st *state, enc *json.Encoder) error {
	var cmd command
	if err := json.Unmarshal(line, &cmd); err != nil {
		return fmt.Errorf("decode command: %w", err)
	}
	switch cmd.Cmd {
	case "start":
		return handleStart(cmd, st, enc)
	case "dialect":
		return handleDialect(cmd, st, enc)
	case "run":
		return handleRun(cmd, st, enc)
	case "stop":
		return errStop
	default:
		return fmt.Errorf("%w: %q", errUnknownCommand, cmd.Cmd)
	}
}

// handleStart writes the implementation descriptor.
func handleStart(cmd command, st *state, enc *json.Encoder) error {
	if cmd.Version != 1 {
		return fmt.Errorf("%w: %d", errUnsupportedProtocol, cmd.Version)
	}
	st.started = true
	resp := startResponse{
		Version: 1,
		Implementation: implementation{
			Language: "go",
			Name:     implementationName,
			Version:  implementationVersion,
			Homepage: "https://github.com/go-rotini/jsonschema",
			Issues:   "https://github.com/go-rotini/jsonschema/issues",
			Source:   "https://github.com/go-rotini/jsonschema",
			Dialects: supportedDialects,
		},
	}
	if err := enc.Encode(resp); err != nil {
		return fmt.Errorf("encode start response: %w", err)
	}
	return nil
}

// handleDialect records the harness-selected dialect for subsequent run
// calls. Unknown dialect URIs return ok=false.
func handleDialect(cmd command, st *state, enc *json.Encoder) error {
	d := jsonschema.DraftFromMetaSchemaURL(cmd.Dialect)
	if d == jsonschema.DraftUnknown {
		if err := enc.Encode(dialectResponse{OK: false}); err != nil {
			return fmt.Errorf("encode dialect response: %w", err)
		}
		return nil
	}
	st.dialect = d
	if err := enc.Encode(dialectResponse{OK: true}); err != nil {
		return fmt.Errorf("encode dialect response: %w", err)
	}
	return nil
}

// handleRun compiles the case schema, validates each test instance, and
// emits the per-test results (or an "errored" envelope if compile fails).
func handleRun(cmd command, st *state, enc *json.Encoder) error {
	var tc testCase
	if err := json.Unmarshal(cmd.Case, &tc); err != nil {
		return writeErrored(enc, cmd.Seq, fmt.Sprintf("decode case: %v", err))
	}
	compiler, err := newCaseCompiler(st.dialect, tc.Registry)
	if err != nil {
		return writeErrored(enc, cmd.Seq, err.Error())
	}
	schema, err := compileCaseSchema(compiler, tc.Schema)
	if err != nil {
		return writeErrored(enc, cmd.Seq, err.Error())
	}
	results := evaluateTests(schema, tc.Tests)
	if err := enc.Encode(runResponse{Seq: cmd.Seq, Results: results}); err != nil {
		return fmt.Errorf("encode run response: %w", err)
	}
	return nil
}

// newCaseCompiler builds a per-case Compiler with the harness-selected
// dialect as the default draft and any registry entries pre-registered as
// resources so $ref resolution succeeds without a network loader.
func newCaseCompiler(dialect jsonschema.Draft, registry map[string]json.RawMessage) (*jsonschema.Compiler, error) {
	opts := []jsonschema.CompileOption{}
	if dialect != jsonschema.DraftUnknown {
		opts = append(opts, jsonschema.WithDefaultDraft(dialect))
	}
	c := jsonschema.NewCompiler(opts...)
	for uri, raw := range registry {
		if err := c.AddResource(uri, []byte(raw)); err != nil {
			return nil, fmt.Errorf("add registry resource %q: %w", uri, err)
		}
	}
	return c, nil
}

// compileCaseSchema compiles the case's raw schema bytes; boolean
// schemas round-trip through Compile transparently.
func compileCaseSchema(c *jsonschema.Compiler, raw json.RawMessage) (*jsonschema.Schema, error) {
	if len(raw) == 0 {
		return nil, errEmptyCaseSchema
	}
	schema, err := c.Compile([]byte(raw))
	if err != nil {
		return nil, fmt.Errorf("compile schema: %w", err)
	}
	return schema, nil
}

// evaluateTests runs each test instance through the compiled schema. A
// panic inside a single test is recovered and reported per-test so one bad
// fixture does not abort the case.
func evaluateTests(schema *jsonschema.Schema, tests []testInstance) []testResult {
	results := make([]testResult, len(tests))
	for i, t := range tests {
		results[i] = evaluateOne(schema, t.Instance)
	}
	return results
}

// evaluateOne validates one instance with panic recovery, decoding via
// json.Number so the validator sees the wire form.
func evaluateOne(schema *jsonschema.Schema, raw json.RawMessage) (res testResult) {
	defer func() {
		if r := recover(); r != nil {
			res = testResult{Errored: true, Context: &errorReport{Message: fmt.Sprintf("panic: %v", r)}}
		}
	}()
	var instance any
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	if err := dec.Decode(&instance); err != nil {
		return testResult{Errored: true, Context: &errorReport{Message: fmt.Sprintf("decode instance: %v", err)}}
	}
	out, err := schema.ValidateValue(instance)
	if err != nil {
		return testResult{Errored: true, Context: &errorReport{Message: err.Error()}}
	}
	v := out.Valid
	return testResult{Valid: &v}
}

// writeErrored emits the case-level errored envelope when compile or
// case-decode fails before any test could run.
func writeErrored(enc *json.Encoder, seq json.RawMessage, msg string) error {
	resp := erroredResponse{
		Seq:     seq,
		Errored: true,
		Context: &errorReport{Message: msg},
	}
	if err := enc.Encode(resp); err != nil {
		return fmt.Errorf("encode errored response: %w", err)
	}
	return nil
}
