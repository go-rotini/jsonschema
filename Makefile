TEST_SUITE_DIR  := testdata/JSON-Schema-Test-Suite
# The JSON Schema Test Suite does not tag releases, so the clone is pinned to a commit: upstream
# additions change the totals in testdata/.expected-passes.json. Bump both together.
TEST_SUITE_REPO := https://github.com/json-schema-org/JSON-Schema-Test-Suite.git
TEST_SUITE_SHA  := 7de0e6a06031ede80028583dd45d0cd41105ae5b

# The development tools (the linter, the license and vulnerability checkers and the mutation
# tester) are declared in tools.mod rather than go.mod, so a module that requires this one never
# sees them in its own dependency graph. Run them through TOOL, and upgrade them with
# `make tools-upgrade`, never `go mod tidy -modfile=tools.mod`: tidy scans this module's own
# packages and would copy their dependencies into the tools file.
TOOL := go tool -modfile=tools.mod

.PHONY: all clean clean-suite clean-all clone-test-suite lint test test-acceptance test-bench \
        test-conformance test-fuzz test-mutation test-race tools-upgrade vuln vuln-tools \
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
	@git init --quiet $(TEST_SUITE_DIR)
	@git -C $(TEST_SUITE_DIR) fetch --quiet --depth 1 $(TEST_SUITE_REPO) $(TEST_SUITE_SHA)
	@git -C $(TEST_SUITE_DIR) checkout --quiet FETCH_HEAD

lint:
	@gofmt_unformatted=$$(gofmt -l . 2>/dev/null | grep -v '^testdata/' || true); \
	test -z "$$gofmt_unformatted" || (echo "files not formatted:" && echo "$$gofmt_unformatted" && exit 1)
	go vet ./...
	go mod verify
	go mod verify -modfile=tools.mod
	$(TOOL) golangci-lint run ./...
	$(TOOL) go-licenses check ./...
	@$(MAKE) --no-print-directory vuln
	@$(MAKE) --no-print-directory vuln-tools

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

# Every FuzzXxx target, one at a time (go test -fuzz takes a single target), for FUZZTIME each.
# The targets are listed from the test files, so a new or renamed one runs without editing this
# file, and a package that fails to build fails the run. When -fuzztime runs out, go test can lose
# a race with its own deadline and fail with a bare "context deadline exceeded" and no failing
# input; that outcome counts as a pass.
FUZZTIME ?= 60s

test-fuzz:
	@log=$$(mktemp); trap 'rm -f "$$log"' EXIT; \
	for pkg in $$(go list ./...); do \
		list=$$(go test -list '^Fuzz' "$$pkg") || { echo "$$list"; exit 1; }; \
		for name in $$(echo "$$list" | grep '^Fuzz'); do \
			echo "→ $$pkg $$name"; \
			go test -fuzz="^$$name$$" -fuzztime=$(FUZZTIME) -run=^$$ "$$pkg" >"$$log" 2>&1; status=$$?; \
			cat "$$log"; \
			if [ $$status -ne 0 ]; then \
				if grep -q '^    context deadline exceeded$$' "$$log" && ! grep -q 'Failing input written' "$$log"; then \
					echo "→ $$name: fuzz time elapsed; deadline race in go test, no failing input"; \
				else \
					exit 1; \
				fi; \
			fi; \
		done; \
	done

test-mutation: clone-test-suite
	@$(TOOL) github.com/go-gremlins/gremlins/cmd/gremlins unleash --config .gremlins.yaml

test-race: clone-test-suite
	@go test -race -count=1 -coverprofile=test_race.out ./...
	@go tool cover -func=test_race.out

# govulncheck, one target per dependency graph; lint runs both. vuln scans go.mod from source,
# test files included, which reports only vulnerable code this module can reach. vuln-tools scans
# each tool in tools.mod as a built binary: that is exactly the code that runs.
vuln:
	@$(TOOL) govulncheck -test ./...

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
