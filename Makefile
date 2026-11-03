# The one place for pinned tool versions: init and CI install exactly these.
# LIGHTHOUSE_VERSION is read from .lighthouse-version; the npm tools
# (prettier, markdownlint-cli2) are pinned in package.json and package-lock.json.
# Every release archive is verified against the SHA-256 below before it is
# extracted. A version bump also updates that tool's four digests: read them from
# the release's SHA256SUMS or `gh api repos/<owner>/<repo>/releases/tags/<tag> --jq '.assets[].digest'`.
LIGHTHOUSE_VERSION := $(shell cat .lighthouse-version)
RUFF_VERSION := 0.17.3
LYCHEE_VERSION := 0.24.2
TARGETS := x86_64-unknown-linux-gnu aarch64-unknown-linux-gnu aarch64-apple-darwin x86_64-apple-darwin
LIGHTHOUSE_SHA256_x86_64-unknown-linux-gnu := 4680a8b51935ad9146841c38a938bfddc310c00d6cecedc64b45be60bba55e44
LIGHTHOUSE_SHA256_aarch64-unknown-linux-gnu := bb1a2a67b2f87ce0815e0595488bab21906a59beb65e5f0a67d593ae8a05c3b6
LIGHTHOUSE_SHA256_aarch64-apple-darwin := f47469673e6168d7e2fee0f5c1c7a97451476669767c924eb8acef0f4ff6010f
LIGHTHOUSE_SHA256_x86_64-apple-darwin := de15a4bac397311494b3b23a58153a9e44759c57f4813e3235bcf6b954262075
RUFF_SHA256_x86_64-unknown-linux-gnu := 2b0890f5a55900af741f133918c2656919bbf18c2ab7741b4ca8403a90e647a9
RUFF_SHA256_aarch64-unknown-linux-gnu := 3f4639152c0e42bb5938e6c23d53d527a38f3133e000102934518f90529fa299
RUFF_SHA256_aarch64-apple-darwin := 118f7121aa838785672bf612487f755a89108bb5f44e0f3df96c5967a72614f9
RUFF_SHA256_x86_64-apple-darwin := 17f95b1bee24d70de62ffe5beec7f71bb541c7e3d147a1c3d84f7cd6525f6fdf
LYCHEE_SHA256_x86_64-unknown-linux-gnu := 1f4e0ef7f6554a6ed33dd7ac144fb2e1bbed98598e7af973042fc5cd43951c9a
LYCHEE_SHA256_aarch64-unknown-linux-gnu := 91a7bd65685da41b90ccb9bc867a3d649a7818042dae04ff405e55a25bddee4c
LYCHEE_SHA256_aarch64-apple-darwin := c9d3740ea2d891854d37116c9fba840f37b6e7c89d330e7db84ac333631c4977
LYCHEE_SHA256_x86_64-apple-darwin := 887503a9cff667d322b8d0892b40bf49976eb9507af8483220a3706cdad55978
# Where install-lighthouse and install-lint-tools put executables (<prefix>/bin).
PREFIX ?= $(HOME)/.local
export PATH := $(CURDIR)/node_modules/.bin:$(PREFIX)/bin:$(PATH)

# fetch-release(TOOL,url,archive): sets $$target, downloads the archive into a
# fresh $$work, and checks it against TOOL's pinned digest for $$target.
# The caller extracts and places the result, and $$work is removed on exit.
define fetch-release
case "$$(uname -s)-$$(uname -m)" in \
	Linux-x86_64) target=x86_64-unknown-linux-gnu ;; \
	Linux-aarch64|Linux-arm64) target=aarch64-unknown-linux-gnu ;; \
	Darwin-arm64) target=aarch64-apple-darwin ;; \
	Darwin-x86_64) target=x86_64-apple-darwin ;; \
	*) echo "$(1) has no release for $$(uname -s)-$$(uname -m)" >&2; exit 1 ;; \
esac; \
$(foreach t,$(TARGETS),test "$$target" != $(t) || sha=$($(1)_SHA256_$(t)); )\
work="$$(mktemp -d)"; \
trap 'rm -rf "$$work"' EXIT; \
curl -fsSL -o "$$work/archive.tar.gz" $(2) && \
echo "$$sha  $$work/archive.tar.gz" | shasum -a 256 -c - && \
mkdir -p "$$work/out"
endef

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

# Lighthouse keeps its plugins next to the real executable, so the archive goes
# to <prefix>/lib/lighthouse/<version>/ and bin/lighthouse links to it.
install-lighthouse:
	@$(call fetch-release,LIGHTHOUSE,https://github.com/siyul-park/lighthouse/releases/download/v$(LIGHTHOUSE_VERSION)/lighthouse-$(LIGHTHOUSE_VERSION)-$$target.tar.gz) && \
	home="$(PREFIX)/lib/lighthouse/$(LIGHTHOUSE_VERSION)" && \
	rm -rf "$$home" && mkdir -p "$$home" "$(PREFIX)/bin" && \
	tar -xzf "$$work/archive.tar.gz" -C "$$home" --strip-components=1 && \
	ln -sf "$$home/lighthouse" "$(PREFIX)/bin/lighthouse"

# ruff and lychee are single executables from release archives for the host OS
# and arch (ubuntu-24.04 marks the system Python externally managed, so no pip).
# npm ci --include=dev installs the lockfile's tools even under NODE_ENV=production.
install-lint-tools:
	@npm ci --include=dev
	@$(call fetch-release,RUFF,https://github.com/astral-sh/ruff/releases/download/$(RUFF_VERSION)/ruff-$$target.tar.gz) && \
	tar -xzf "$$work/archive.tar.gz" -C "$$work/out" && \
	mkdir -p $(PREFIX)/bin && \
	install "$$(find "$$work/out" -name ruff -type f | head -n 1)" $(PREFIX)/bin/ruff
	@$(call fetch-release,LYCHEE,https://github.com/lycheeverse/lychee/releases/download/lychee-v$(LYCHEE_VERSION)/lychee-$$target.tar.gz) && \
	tar -xzf "$$work/archive.tar.gz" -C "$$work/out" && \
	mkdir -p $(PREFIX)/bin && \
	install "$$(find "$$work/out" -name lychee -type f | head -n 1)" $(PREFIX)/bin/lychee

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
