# trafico-cli Makefile.
# All checking logic lives in scripts/check.sh: it is the single source of
# truth for "green", so this file never duplicates the linter list.

BIN_DIR := bin

.PHONY: help build test check golden bench fixtures-trim fmt tidy

## help: list available targets (default)
help:
	@echo "trafico-cli targets:"
	@echo "  build          build the trafico binary into ./bin"
	@echo "  test           run the test suite"
	@echo "  check          full quality gate (delegates to scripts/check.sh)"
	@echo "  golden         run golden-file contract tests"
	@echo "  bench          run benchmarks (parser and hot paths)"
	@echo "  fixtures-trim  trim feed snapshots in testdata/feeds"
	@echo "  fmt            format all Go sources"
	@echo "  tidy           run go mod tidy"

## build: build the trafico binary
build:
	go build -o $(BIN_DIR)/ ./cmd/trafico

## test: run the test suite
test:
	go test ./...

## check: full quality gate; the logic lives in scripts/check.sh
check:
	bash scripts/check.sh

## golden: run golden-file contract tests
golden:
	go test ./... -run Golden

## bench: run benchmarks
bench:
	go test ./... -run '^$$' -bench . -benchmem

## fixtures-trim: trim the feed snapshots under testdata/feeds
fixtures-trim:
	bash scripts/trim-fixtures.sh

## fmt: format all Go sources
fmt:
	gofmt -w .

## tidy: tidy the Go module files
tidy:
	go mod tidy
