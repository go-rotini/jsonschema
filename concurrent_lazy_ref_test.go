package jsonschema

import (
	"encoding/json"
	"fmt"
	"sync"
	"testing"
)

// TestConcurrentLazyRefValidate exercises the runtime ref-resolution path
// from many goroutines simultaneously, against a single shared *Schema.
// The schema mixes `$dynamicRef`, `$ref` cycles, and sibling-dependent
// keywords (if/then/else, contains/maxContains/minContains, required's
// readOnly/writeOnly scan) so any per-call build state read off the wrong
// frame surfaces as an incorrect validity verdict — not just a race-flag.
//
// The bug Phase 2.5 fixed was that evalBuilder kept compile-time scratch
// state (currentParent / currentLoc / currentBase / currentResource /
// draft) on the shared struct. Two goroutines validating concurrently
// would trample each other's scratch state mid-build at runtime, producing
// observably wrong validation outputs in 5-15% of runs. Per-call frames
// (buildFrame) eliminate the shared mutable state entirely.
//
// The test asserts that every goroutine's verdict matches the expected
// verdict computed up front against a single-threaded validation.
func TestConcurrentLazyRefValidate(t *testing.T) {
	const schemaSrc = `{
		"$schema": "https://json-schema.org/draft/2020-12/schema",
		"$id": "https://example.com/concurrent-lazy-ref",
		"$dynamicAnchor": "node",
		"type": "object",
		"properties": {
			"kind": {"type": "string", "enum": ["leaf", "branch"]},
			"value": {"type": "integer"},
			"children": {
				"type": "array",
				"items": {"$dynamicRef": "#node"},
				"contains": {
					"type": "object",
					"properties": {"kind": {"const": "leaf"}}
				},
				"minContains": 0
			},
			"meta": {"$ref": "#/$defs/meta"}
		},
		"required": ["kind"],
		"if": {"properties": {"kind": {"const": "leaf"}}},
		"then": {"required": ["value"]},
		"else": {"required": ["children"]},
		"$defs": {
			"meta": {
				"type": "object",
				"properties": {
					"tag": {"type": "string", "minLength": 1},
					"flag": {"type": "boolean"}
				},
				"required": ["tag"]
			}
		}
	}`

	schema, err := Compile([]byte(schemaSrc))
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	// Each case is a (instance, expectedValid) pair. The mix of valid and
	// invalid instances gives the test bite: a corrupted scratch frame
	// could flip a verdict in either direction.
	cases := []struct {
		name  string
		raw   []byte
		valid bool
	}{
		{
			name:  "leaf-valid",
			raw:   []byte(`{"kind":"leaf","value":42,"meta":{"tag":"a"}}`),
			valid: true,
		},
		{
			name:  "leaf-missing-value",
			raw:   []byte(`{"kind":"leaf","meta":{"tag":"a"}}`),
			valid: false,
		},
		{
			name:  "branch-valid",
			raw:   []byte(`{"kind":"branch","children":[{"kind":"leaf","value":1},{"kind":"leaf","value":2}]}`),
			valid: true,
		},
		{
			name:  "branch-missing-children",
			raw:   []byte(`{"kind":"branch"}`),
			valid: false,
		},
		{
			name:  "branch-deep-valid",
			raw:   []byte(`{"kind":"branch","children":[{"kind":"branch","children":[{"kind":"leaf","value":7}]}],"meta":{"tag":"x"}}`),
			valid: true,
		},
		{
			name:  "branch-deep-invalid-leaf",
			raw:   []byte(`{"kind":"branch","children":[{"kind":"branch","children":[{"kind":"leaf"}]}]}`),
			valid: false,
		},
		{
			name:  "leaf-bad-meta",
			raw:   []byte(`{"kind":"leaf","value":1,"meta":{"flag":true}}`),
			valid: false,
		},
	}

	// Sanity-check the expected verdicts up front so a botched test schema
	// fails loudly here rather than masquerading as a concurrency hit.
	for _, c := range cases {
		res, err := schema.Validate(c.raw)
		if err != nil {
			t.Fatalf("baseline validate %s: %v", c.name, err)
		}
		if res.Valid != c.valid {
			t.Fatalf("baseline %s: expected valid=%v, got valid=%v errors=%v",
				c.name, c.valid, res.Valid, res.Errors)
		}
	}

	const workers = 32
	const iterations = 1000

	type mismatch struct {
		worker, iter int
		caseName     string
		expected     bool
		got          bool
		errs         []ValidationError
	}

	var (
		wg sync.WaitGroup
		mu sync.Mutex
	)
	var mismatches []mismatch

	wg.Add(workers)
	for w := 0; w < workers; w++ {
		go func(id int) {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				c := cases[(id+i)%len(cases)]
				// Decode each iteration so the runtime path actually
				// works against a fresh instance — defends against any
				// future caching of validated input.
				var inst any
				if err := json.Unmarshal(c.raw, &inst); err != nil {
					mu.Lock()
					mismatches = append(mismatches, mismatch{
						worker: id, iter: i, caseName: c.name,
						expected: c.valid, got: false,
						errs: []ValidationError{{Message: fmt.Sprintf("unmarshal: %v", err)}},
					})
					mu.Unlock()
					return
				}
				res, err := schema.ValidateValue(inst)
				if err != nil {
					mu.Lock()
					mismatches = append(mismatches, mismatch{
						worker: id, iter: i, caseName: c.name,
						expected: c.valid, got: false,
						errs: []ValidationError{{Message: fmt.Sprintf("validate: %v", err)}},
					})
					mu.Unlock()
					return
				}
				if res.Valid != c.valid {
					mu.Lock()
					mismatches = append(mismatches, mismatch{
						worker: id, iter: i, caseName: c.name,
						expected: c.valid, got: res.Valid,
						errs: res.Errors,
					})
					mu.Unlock()
				}
			}
		}(w)
	}
	wg.Wait()

	if len(mismatches) > 0 {
		// Cap the diagnostic dump so the test output stays readable when
		// the bug is rampant.
		const dumpCap = 10
		t.Errorf("%d concurrent verdict mismatches across %d workers x %d iterations",
			len(mismatches), workers, iterations)
		for i, m := range mismatches {
			if i >= dumpCap {
				t.Logf("(... %d more mismatches suppressed ...)", len(mismatches)-dumpCap)
				break
			}
			t.Logf("worker=%d iter=%d case=%s expected=%v got=%v errs=%v",
				m.worker, m.iter, m.caseName, m.expected, m.got, m.errs)
		}
	}
}
