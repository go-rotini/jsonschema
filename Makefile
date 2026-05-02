TEST_SUITE_DIR  := testdata/JSON-Schema-Test-Suite
TEST_SUITE_REPO := https://github.com/json-schema-org/JSON-Schema-Test-Suite.git

.PHONY: all clean clone-test-suite lint test test-acceptance test-bench \
        test-conformance test-fuzz test-mutation test-race

all: clean clone-test-suite lint test test-acceptance test-bench \
     test-conformance test-fuzz test-mutation test-race

clean:
	@rm -rf $(TEST_SUITE_DIR) *.out test_mutation.json

clone-test-suite: $(TEST_SUITE_DIR)

$(TEST_SUITE_DIR):
	@git clone --quiet --branch main --depth 1 $(TEST_SUITE_REPO) $(TEST_SUITE_DIR)

lint:
	@gofmt_unformatted=$$(gofmt -l . 2>/dev/null | grep -v '^testdata/' || true); \
	test -z "$$gofmt_unformatted" || (echo "files not formatted:" && echo "$$gofmt_unformatted" && exit 1)
	go vet ./...
	go mod verify
	go tool golangci-lint run ./...
	go tool go-licenses check ./...
	go tool govulncheck ./...

test: clone-test-suite
	@go test -v -count=1 -coverprofile=test.out ./...
	@go tool cover -func=test.out | tail -1

test-acceptance:
	@go test -v -count=1 -run TestAcceptance -coverprofile=test_acceptance.out ./...
	@go tool cover -func=test_acceptance.out | tail -1

test-bench:
	@go test -bench=. -benchmem -count=1 ./... | tee test_bench.out

test-conformance: clone-test-suite
	@go test -v -count=1 -run 'TestJSONSchemaTestSuite|TestJSONSchemaEdgeCases' -coverprofile=test_conformance.out ./...
	@go tool cover -func=test_conformance.out | tail -1

test-fuzz:
	@go test -fuzz=FuzzCompile  -fuzztime=60s -run=^$$ ./...
	@go test -fuzz=FuzzValidate -fuzztime=60s -run=^$$ ./...
	@go test -fuzz=FuzzGenerate -fuzztime=60s -run=^$$ ./...

test-mutation: clone-test-suite
	@go tool github.com/go-gremlins/gremlins/cmd/gremlins unleash --config .gremlins.yaml

test-race:
	@go test -race -count=1 -coverprofile=test_race.out ./...
	@go tool cover -func=test_race.out | tail -1
