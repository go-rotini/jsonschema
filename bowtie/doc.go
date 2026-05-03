// Package bowtie hosts the future stdin/stdout adapter that implements the
// Bowtie protocol (https://bowtie.report/) so this implementation's
// conformance results appear on the public dashboard.
//
// The connector lands in v0.2 (a follow-up to the Phase 9 conformance work).
// Phase 9 ships the acceptance fixtures, expected-passes regression guard,
// fuzz targets, and stdlib compatibility tests; the Bowtie wire format
// adapter is meaningful additional work and warrants its own phase.
//
// When implemented, this package will expose a `cmd/bowtie-connector`
// binary that:
//   - reads protocol messages from stdin (one JSON document per line),
//   - dispatches "start", "dialect", "run", "stop" commands,
//   - compiles each test schema via [jsonschema.Compile],
//   - validates each test instance via [jsonschema.Schema.Validate],
//   - emits the per-test results in Bowtie's expected JSON shape.
//
// Tracking issue: https://github.com/go-rotini/jsonschema/issues/TBD
package bowtie
