TEST_SUITE_DIR  := testdata/JSON-Schema-Test-Suite
# JSON Schema Test Suite uses 'main' as canonical; upstream does not tag releases.
TEST_SUITE_REPO := https://github.com/json-schema-org/JSON-Schema-Test-Suite.git

# The development tools (the linter, the license and vulnerability checkers and the mutation
# tester) are declared in tools.mod rather than go.mod, so a module that requires this one never
# sees them in its own dependency graph. Run them through TOOL, and upgrade them with
# `make tools-upgrade`, never `go mod tidy -modfile=tools.mod`: tidy scans this module's own
# packages and would copy their dependencies into the tools file.
TOOL := go tool -modfile=tools.mod

.PHONY: all clean clean-suite clean-all clone-test-suite lint test test-acceptance test-bench \
        test-conformance test-fuzz test-mutation test-race tools-upgrade vuln-tools \
        refresh-acceptance-fixtures

all: clean-all clone-test-suite lint test test-acceptance test-bench \
     test-conformance test-fuzz test-mutation test-race

clean:
	@rm -f *.out test_mutation.json cov.out

clean-suite:
	@rm -rf $(TEST_SUITE_DIR)

clean-all: clean clean-suite

clone-test-suite: $(TEST_SUITE_DIR)

$(TEST_SUITE_DIR):
	@git clone --quiet --branch main --depth 1 $(TEST_SUITE_REPO) $(TEST_SUITE_DIR)

lint:
	@gofmt_unformatted=$$(gofmt -l . 2>/dev/null | grep -v '^testdata/' || true); \
	test -z "$$gofmt_unformatted" || (echo "files not formatted:" && echo "$$gofmt_unformatted" && exit 1)
	go vet ./...
	go mod verify
	go mod verify -modfile=tools.mod
	$(TOOL) golangci-lint run ./...
	$(TOOL) go-licenses check ./...
	$(TOOL) govulncheck ./...

test: clone-test-suite
	@go test -v -count=1 -coverprofile=test.out ./...
	@go tool cover -func=test.out

test-acceptance:
	@go test -v -count=1 -run TestAcceptance -coverprofile=test_acceptance.out ./...
	@go tool cover -func=test_acceptance.out

test-bench:
	@go test -bench=. -benchmem -count=1 ./... | tee test_bench.out

test-conformance: clone-test-suite
	@go test -v -count=1 -run 'TestJSONSchemaTestSuite|TestJSONSchemaEdgeCases' -coverprofile=test_conformance.out ./...
	@go tool cover -func=test_conformance.out

test-fuzz:
	@go test -fuzz='^FuzzCompile$$'  -fuzztime=60s -run=^$$ .
	@go test -fuzz='^FuzzValidate$$' -fuzztime=60s -run=^$$ .
	@go test -fuzz='^FuzzGenerate$$' -fuzztime=60s -run=^$$ ./generate
	@go test -fuzz='^FuzzGenerate$$' -fuzztime=60s -run=^$$ ./gogen

test-mutation: clone-test-suite
	@$(TOOL) github.com/go-gremlins/gremlins/cmd/gremlins unleash --config .gremlins.yaml

test-race:
	@go test -race -count=1 -coverprofile=test_race.out ./...
	@go tool cover -func=test_race.out

# Scans each tool in tools.mod as a built binary: that is exactly the code that runs.
vuln-tools:
	@bin=$$(mktemp -d); trap 'rm -rf "$$bin"' EXIT; \
	for pkg in $$(go list -modfile=tools.mod tool); do \
		name=$$(echo "$$pkg" | tr / _); \
		echo "→ $$pkg"; \
		go build -modfile=tools.mod -o "$$bin/$$name" "$$pkg" || exit 1; \
		$(TOOL) govulncheck -mode=binary "$$bin/$$name" || exit 1; \
	done

# Upgrades every tool in tools.mod to its latest release. tools.mod is rebuilt from scratch, so it
# holds exactly what today's tools require; the module, go and toolchain lines are copied from
# go.mod. On any failure the previous tools.mod and tools.sum are restored. Review the diff and
# commit tools.mod and tools.sum together.
tools-upgrade:
	@tools=$$(go list -modfile=tools.mod tool) || exit 1; \
	backup=$$(mktemp -d); cp tools.mod tools.sum "$$backup"/; \
	restore() { cp "$$backup"/tools.mod "$$backup"/tools.sum .; rm -rf "$$backup"; echo "tools-upgrade failed; tools.mod and tools.sum restored"; exit 1; }; \
	{ grep -E '^module ' go.mod; echo; grep -E '^go ' go.mod; echo; grep -E '^toolchain ' go.mod; } > tools.mod; \
	rm -f tools.sum; \
	go get -modfile=tools.mod -tool $$(for t in $$tools; do printf '%s@latest ' "$$t"; done) || restore; \
	go mod verify -modfile=tools.mod || restore; \
	rm -rf "$$backup"
	@$(MAKE) --no-print-directory vuln-tools
	@echo "tools upgraded; review: git diff tools.mod"

# Re-fetches the version-pinned upstream acceptance fixtures. Run intentionally
# at release time when bumping pinned versions; not part of `make all`.
# json-patch.json and kitchen-sink.json are hand-rolled and not refreshed here.
refresh-acceptance-fixtures:
	@curl -fsSL https://spec.openapis.org/oas/3.1/schema/2022-10-07 \
		-o testdata/acceptance/openapi-3.1.json
	@curl -fsSL https://raw.githubusercontent.com/asyncapi/spec-json-schemas/master/schemas/2.6.0.json \
		-o testdata/acceptance/asyncapi-2.6.json
	@curl -fsSL https://geojson.org/schema/GeoJSON.json \
		-o testdata/acceptance/geojson.json
	@curl -fsSL https://json.schemastore.org/avro-avsc.json \
		-o testdata/acceptance/avro.json
