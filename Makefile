# The one place for pinned tool versions: init and CI install exactly these.
# LIGHTHOUSE_VERSION is read from .lighthouse-version.
LIGHTHOUSE_VERSION := $(shell cat .lighthouse-version)
PRETTIER_VERSION := 3.9.10
MARKDOWNLINT_CLI2_VERSION := 0.24.0
RUFF_VERSION := 0.17.3
LYCHEE_VERSION := 0.24.2
# Where install-lighthouse and install-lint-tools put executables (<prefix>/bin).
PREFIX ?= $(HOME)/.local

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

.PHONY: init install-tools install-lighthouse install-lint-tools install-modules generate build clean tidy update clean-sum clean-cache sync check check-generated check-tidy check-fmt check-arm64 check-inline test coverage coverage-check benchmark lighthouse lint fmt vet doc fuzz
all: lint test build

init:
	@$(MAKE) install-tools
	@$(MAKE) install-modules
	@$(MAKE) install-lighthouse
	@$(MAKE) install-lint-tools

install-tools:
	@go install golang.org/x/tools/cmd/godoc@latest
	@go install golang.org/x/tools/cmd/goimports@latest

install-lighthouse:
	@work="$$(mktemp -d)"; \
	trap 'rm -rf "$$work"' EXIT; \
	curl -fsSL -o "$$work/install.sh" https://github.com/siyul-park/lighthouse/releases/download/v$(LIGHTHOUSE_VERSION)/install.sh; \
	sh "$$work/install.sh" --version $(LIGHTHOUSE_VERSION) --prefix $(PREFIX)

# ruff comes from pipx: ubuntu-24.04 (runner and desktop) marks the system
# Python externally managed, so pip install --user fails there, and the runner
# image ships pipx. lychee is a release tarball for the host OS and arch.
install-lint-tools:
	@npm install -g prettier@$(PRETTIER_VERSION) markdownlint-cli2@$(MARKDOWNLINT_CLI2_VERSION)
	@pipx install --force ruff==$(RUFF_VERSION)
	@case "$$(uname -s)-$$(uname -m)" in \
		Linux-x86_64) target=x86_64-unknown-linux-gnu ;; \
		Linux-aarch64|Linux-arm64) target=aarch64-unknown-linux-gnu ;; \
		Darwin-arm64) target=aarch64-apple-darwin ;; \
		Darwin-x86_64) target=x86_64-apple-darwin ;; \
		*) echo "lychee has no release for $$(uname -s)-$$(uname -m)" >&2; exit 1 ;; \
	esac; \
	work="$$(mktemp -d)"; \
	trap 'rm -rf "$$work"' EXIT; \
	curl -fsSL -o "$$work/lychee.tar.gz" https://github.com/lycheeverse/lychee/releases/download/lychee-v$(LYCHEE_VERSION)/lychee-$$target.tar.gz; \
	tar -xzf "$$work/lychee.tar.gz" -C "$$work"; \
	mkdir -p $(PREFIX)/bin; \
	install "$$(find "$$work" -name lychee -type f | head -n 1)" $(PREFIX)/bin/lychee

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

lighthouse:
	@lighthouse check

lint: vet lighthouse

fmt:
	@command -v $(GOIMPORTS) >/dev/null
	@$(GOIMPORTS) -w .

vet:
	@go vet ./...
	@(cd benchmarks && go vet ./...)

doc: init
	@godoc -http=:6060

fuzz:
	@go test -run='^$$' -fuzz='^FuzzInstructionRoundTrip$$' -fuzztime=10s -parallel=$(fuzz-parallel) $(test-options) ./instr
	@go test -run='^$$' -fuzz='^FuzzParse$$' -fuzztime=10s -parallel=$(fuzz-parallel) $(test-options) ./instr
