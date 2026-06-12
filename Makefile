.PHONY: generate test test-integration check-integration lint fmt coverage tidy examples refresh-fixtures

# generate re-applies the OpenAPI fixes (if a script is present) and regenerates
# the typed client via the //go:generate directives.
generate:
	@if [ -x scripts/fix-openapi.sh ]; then ./scripts/fix-openapi.sh; \
	elif [ -x scripts/convert-openapi.sh ]; then ./scripts/convert-openapi.sh; fi
	go generate ./...

test:
	go test -race -count=1 ./...

test-integration:
	go test -race -count=1 -tags=integration ./...

# check-integration verifies every exported Client endpoint method has a
# per-endpoint TestIntegration<Method> in a //go:build integration test file.
check-integration:
	./scripts/check-integration-coverage.sh

lint:
	golangci-lint run

fmt:
	gofmt -w .

coverage:
	go test -race -count=1 -coverprofile=coverage.out ./...
	go tool cover -func=coverage.out | tail -n 1

tidy:
	go mod tidy

# examples runs the demo against the live API to refresh demo/examples (recorded
# request/response pairs). Requires the API credentials in the environment.
# The weekly response-watch workflow runs this and diffs the result.
# The "examples" arg is required: the demo only records when os.Args[1] == "examples".
examples:
	cd demo && go run . examples

# refresh-fixtures deliberately rebuilds the committed golden set in
# testdata/examples from the live demo/examples recordings. Run it (after
# `make examples`) ONLY when a human intends to update the goldens to a newer
# real response.
#
# The deterministic tests (TestFixtures in decode_examples_test.go) read the
# COMMITTED testdata/examples copies, never demo/examples, so re-running the demo
# does not change test behaviour until the goldens are refreshed here and the diff
# is reviewed and committed.
refresh-fixtures:
	./scripts/refresh-fixtures.sh
