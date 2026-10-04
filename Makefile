# Measured on the coverage runner after the JIT removal; the next rebuild may
# change this baseline only with measured coverage evidence.
coverage-min ?= 83.7
benchmark-level ?= standard
fuzz-parallel ?= 4
GOIMPORTS ?= goimports
-include .env

ifeq ($(benchmark-level),quick)
benchmark-time := 100ms
else ifeq ($(benchmark-level),standard)
benchmark-time := 300ms
else ifeq ($(benchmark-level),deep)
benchmark-time := 1s
else
$(error invalid benchmark-level '$(benchmark-level)'; use quick, standard, or deep)
endif

PROJECT = $(shell basename -s .git $(shell git config --get remote.origin.url))

.PHONY: init install-tools install-modules generate build clean tidy update clean-sum clean-cache sync check check-generated check-tidy check-fmt check-arm64 check-inline test coverage coverage-check benchmark vigil lint fmt vet doc fuzz
all: lint test build

init:
	@$(MAKE) install-tools
	@$(MAKE) install-modules

install-tools:
	@go install golang.org/x/tools/cmd/godoc@latest
	@go install golang.org/x/tools/cmd/goimports@latest

install-modules:
	@go install -v ./...

generate:
	@go run ./internal/cmd/codegen

build:
	@go clean -cache
	@mkdir -p dist
	@go build -ldflags "-s -w" -o ./dist/ ./cmd/...

clean:
	@go clean -cache
	@rm -rf dist

tidy:
	@go mod tidy

update:
	@go get -u all

clean-sum:
	@rm go.sum

clean-cache:
	@go clean -modcache

sync:
	@go work sync

check: check-generated check-tidy check-fmt lint check-inline test check-arm64
	@go build ./...
	@(cd benchmarks && go test ./...)

check-generated:
	@go run ./internal/cmd/codegen -check

check-tidy:
	@go mod tidy -diff
	@(cd benchmarks && go mod tidy -diff)

check-fmt:
	@command -v $(GOIMPORTS) >/dev/null
	@test -z "$$(gofmt -l .)"
	@test -z "$$($(GOIMPORTS) -l .)"
	@(cd benchmarks && test -z "$$(gofmt -l .)")
	@(cd benchmarks && test -z "$$($(GOIMPORTS) -l .)")

check-arm64:
	@GOOS=linux GOARCH=arm64 go build ./...
	@GOOS=linux GOARCH=arm64 go test -exec=true ./...
	@(cd benchmarks && GOOS=linux GOARCH=arm64 go test -exec=true ./...)

check-inline:
	@go build -gcflags=-m ./interp 2>&1 | grep -q 'can inline (\*native).call$$' || 		{ echo "(*native).call no longer inlines"; exit 1; }

test:
	@go test -race $(test-options) ./...
	@(cd benchmarks && go test -race $(test-options) ./...)

coverage:
	@go test -count=1 -race --coverprofile=coverage.out --covermode=atomic -coverpkg=./... $(test-options) ./...

coverage-check: coverage
	@coverage="$$(go tool cover -func=coverage.out | awk '/^total:/ {gsub("%", "", $$3); print $$3}')"; \
	awk -v coverage="$$coverage" -v minimum="$(coverage-min)" 'BEGIN { \
		if (coverage + 0 < minimum + 0) { \
			printf "coverage %.1f%% is below baseline %.1f%%\\n", coverage, minimum; \
			exit 1; \
		} \
		printf "coverage %.1f%% meets baseline %.1f%%\\n", coverage, minimum; \
	}'

benchmark:
	@tmp="$$(mktemp)"; \
	trap 'rm -f "$$tmp"' EXIT; \
	(cd benchmarks && go test -run='^$$' -bench='^BenchmarkKernels$$' -benchmem -benchtime=$(benchmark-time) $(test-options) ./... > "$$tmp"); \
	status=$$?; \
	cat "$$tmp"; \
	test $$status -eq 0; \
	(cd benchmarks && go run ./cmd/benchreport -input "$$tmp" -output ../docs/benchmarks.md)

vigil:
	@bin="$$(mktemp)"; \
	trap 'rm -f "$$bin"' EXIT; \
	go build -o "$$bin" ./internal/cmd/vigil; \
	"$$bin" -diff ./...; \
	(cd benchmarks && "$$bin" -diff ./...)

lint: vet vigil

fmt:
	@command -v $(GOIMPORTS) >/dev/null
	@$(GOIMPORTS) -w .
	@bin="$$(mktemp)"; \
	trap 'rm -f "$$bin"' EXIT; \
	go build -o "$$bin" ./internal/cmd/vigil; \
	status=0; "$$bin" -fix ./... || status=$$?; test $$status -le 1; \
	(cd benchmarks && status=0; "$$bin" -fix ./... || status=$$?; test $$status -le 1)

vet:
	@go vet ./...
	@(cd benchmarks && go vet ./...)

doc: init
	@godoc -http=:6060

fuzz:
	@go test -run='^$$' -fuzz='^FuzzInstructionRoundTrip$$' -fuzztime=10s -parallel=$(fuzz-parallel) $(test-options) ./instr
	@go test -run='^$$' -fuzz='^FuzzParse$$' -fuzztime=10s -parallel=$(fuzz-parallel) $(test-options) ./instr
