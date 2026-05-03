// Command bowtie is the stdin/stdout adapter that exposes
// github.com/go-rotini/jsonschema to the Bowtie cross-implementation
// conformance harness (https://bowtie.report).
//
// The Bowtie protocol is a single-line JSON request / single-line JSON
// response handshake. Each request is one JSON object on stdin terminated
// by a newline; each response is one JSON object on stdout. The recognized
// commands are:
//
//   - "start"   — handshake; the connector replies with its implementation
//     descriptor (language, name, version, supported dialects, ...).
//   - "dialect" — the harness pins a meta-schema URI for subsequent runs.
//   - "run"     — one test case (schema + tests + optional registry); the
//     connector compiles the schema and emits one result per test.
//   - "stop"    — the harness closes the connector; the binary exits 0.
//
// To exercise the connector locally without Docker:
//
//	$ go build -o ./bin/bowtie ./bowtie
//	$ printf '%s\n%s\n%s\n' \
//	    '{"cmd":"start","version":1}' \
//	    '{"cmd":"dialect","dialect":"https://json-schema.org/draft/2020-12/schema"}' \
//	    '{"cmd":"stop"}' | ./bin/bowtie
//
// See https://docs.bowtie.report/en/stable/implementers/ for the full
// protocol spec and the steps to register a connector image with the
// public dashboard.
//
// Implementation note: the dispatch loop is factored out as a private
// dispatch(in, out) helper so the adapter can be exercised over
// bytes.Buffer pipes from tests in main_test.go without spawning a
// subprocess.
package main
