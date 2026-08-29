# Measured on the CI runner, which is amd64. The arm64 JIT backend does not
# compile there, but the portable planner, tracer, and JIT core do - and the
# tests that exercise them skip on amd64, so they count as uncovered. The
# number is therefore lower than an arm64 run reports. Raising it needs
# portable tests for those files, not a different threshold.
coverage-min ?= 72.5
benchmark-pr-time ?= 100ms
benchmark-time ?= 1s
benchmark-count ?= 5
fuzz-parallel ?= 4
GOIMPORTS ?= goimports
-include .env

PROJECT = $(shell basename -s .git $(shell git config --get remote.origin.url))

.PHONY: init install-tools install-modules generate build clean tidy update clean-sum clean-cache sync check check-generated check-tidy check-fmt check-arm64 test coverage coverage-check benchmark benchmark-pr benchmark-core benchmark-nightly benchmark-compare lint fmt vet doc fuzz
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
	@go run ./internal/cmd/geninterp

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

check: check-generated check-tidy check-fmt vet test check-arm64
	@go build ./...

check-generated:
	@go run ./internal/cmd/geninterp -check

check-tidy:
	@go mod tidy -diff

check-fmt:
	@command -v $(GOIMPORTS) >/dev/null
	@test -z "$$(gofmt -l .)"
	@test -z "$$($(GOIMPORTS) -l .)"

check-arm64:
	@GOOS=linux GOARCH=arm64 go build ./...
	@GOOS=linux GOARCH=arm64 go test -exec=true ./...

test:
	@go test -race $(test-options) ./...

coverage:
	@go test -count=1 -race --coverprofile=coverage.out --covermode=atomic -coverpkg=./... $(test-options) ./...

coverage-check: coverage
	@coverage="$$(go tool cover -func=coverage.out | awk '/^total:/ {gsub("%", "", $$3); print $$3}')"; \
	awk -v coverage="$$coverage" -v minimum="$(coverage-min)" 'BEGIN { \
		if (coverage + 0 < minimum + 0) { \
			printf "coverage %.1f%% is below baseline %.1f%%\n", coverage, minimum; \
			exit 1; \
		} \
		printf "coverage %.1f%% meets baseline %.1f%%\n", coverage, minimum; \
	}'

benchmark: benchmark-core

benchmark-pr:
	@root="$$( \
		go test -run='^$$' -bench='^BenchmarkNew$$' -benchmem -benchtime=$(benchmark-pr-time) $(test-options) ./interp && \
		go test -run='^$$' -bench='^BenchmarkInterpreter_Run$$/^(i32\.const_0x00000001;_nop|unreachable|const\.get_0x0000;_call|i32\.const_0x00000001;_array\.new_default_0x0000;_i32\.const_0x00000005;_array\.get)$$/^(Threaded|Fused|JITWarm)$$' -benchmem -benchtime=$(benchmark-pr-time) $(test-options) ./interp && \
		go test -run='^$$' -bench='^BenchmarkInterpreter_Reset$$/^(Scalar|Heap)$$' -benchmem -benchtime=$(benchmark-pr-time) $(test-options) ./interp && \
		go test -run='^$$' -bench='^BenchmarkPool_(Get|Put)$$/^Uncontended$$' -benchmem -benchtime=$(benchmark-pr-time) $(test-options) ./interp \
	)" || { status=$$?; printf '%s\n' "$$root"; exit $$status; }; \
	printf '%s\n' "$$root"; \
	for name in \
		'BenchmarkNew/Empty' \
		'BenchmarkInterpreter_Run/i32.const_0x00000001;_nop/Threaded' \
		'BenchmarkInterpreter_Run/i32.const_0x00000001;_nop/Fused' \
		'BenchmarkInterpreter_Run/unreachable/Threaded' \
		'BenchmarkInterpreter_Run/unreachable/Fused' \
		'BenchmarkInterpreter_Run/const.get_0x0000;_call/Threaded' \
		'BenchmarkInterpreter_Run/i32.const_0x00000001;_array.new_default_0x0000;_i32.const_0x00000005;_array.get/Threaded' \
		'BenchmarkInterpreter_Reset/Scalar' \
		'BenchmarkInterpreter_Reset/Heap' \
		'BenchmarkPool_Get/Uncontended' \
		'BenchmarkPool_Put/Uncontended'; do \
		printf '%s\n' "$$root" | grep -q "^$$name-" || { printf 'missing benchmark %s\n' "$$name"; exit 1; }; \
	done; \
	if [ "$$(go env GOARCH)" = arm64 ]; then \
		printf '%s\n' "$$root" | grep -q '^BenchmarkInterpreter_Run/i32.const_0x00000001;_nop/JITWarm-' || { printf '%s\n' 'missing ARM64 JIT warm benchmark'; exit 1; }; \
	fi
	@kernels="$$(cd benchmarks && \
		go test -run='^$$' -bench='^(BenchmarkControl_IterativeFib|BenchmarkMemory_TypedArraySum|BenchmarkNumeric_BranchTree)$$/^threaded$$' -benchmem -benchtime=$(benchmark-pr-time) $(test-options) ./... && \
		go test -run='^$$' -bench='^BenchmarkCall_RecursiveFib$$/^(20|35)$$/^threaded$$' -benchmem -benchtime=$(benchmark-pr-time) $(test-options) ./...)" || { status=$$?; printf '%s\n' "$$kernels"; exit $$status; }; \
	printf '%s\n' "$$kernels"; \
	for name in \
		BenchmarkControl_IterativeFib/threaded \
		BenchmarkCall_RecursiveFib/20/threaded \
		BenchmarkCall_RecursiveFib/35/threaded \
		BenchmarkMemory_TypedArraySum/threaded \
		BenchmarkNumeric_BranchTree/threaded; do \
		printf '%s\n' "$$kernels" | grep -q "^$$name-" || { printf 'missing benchmark %s\n' "$$name"; exit 1; }; \
	done

benchmark-core:
	@go test -run='^$$' -bench='^Benchmark' -benchmem -benchtime=$(benchmark-time) $(test-options) ./...
	@(cd benchmarks && go test -run='^$$' -bench='^(BenchmarkControl|BenchmarkCall|BenchmarkMemory|BenchmarkNumeric)' -benchmem -benchtime=$(benchmark-time) $(test-options) ./...)

benchmark-nightly:
	@go test -run='^$$' -bench='^Benchmark' -benchmem -benchtime=$(benchmark-time) -count=$(benchmark-count) $(test-options) ./...
	@(cd benchmarks && go test -run='^$$' -bench='^(BenchmarkControl|BenchmarkCall|BenchmarkMemory|BenchmarkNumeric)' -benchmem -benchtime=$(benchmark-time) -count=$(benchmark-count) $(test-options) ./...)

benchmark-compare:
	@comparisons="$$(cd benchmarks && go test -tags=compare -run='^$$' -bench='^(BenchmarkControl|BenchmarkCall|BenchmarkMemory|BenchmarkNumeric)' -benchmem -benchtime=$(benchmark-time) $(test-options) ./...)" || { status=$$?; printf '%s\n' "$$comparisons"; exit $$status; }; \
	printf '%s\n' "$$comparisons"; \
	for scenario in \
		BenchmarkControl_IterativeFib \
		BenchmarkControl_Sieve \
		BenchmarkCall_RecursiveFib/20 \
		BenchmarkCall_IndirectRecursiveFib \
		BenchmarkCall_ClosureCounter \
		BenchmarkMemory_TypedArraySum \
		BenchmarkMemory_AllocationGraph \
		BenchmarkNumeric_BranchTree; do \
		for runtime in native tengo gopher_lua goja; do \
			printf '%s\n' "$$comparisons" | grep -q "^$$scenario/$$runtime-" || { printf 'missing comparison %s/%s\n' "$$scenario" "$$runtime"; exit 1; }; \
		done; \
	done; \
	for scenario in \
		BenchmarkControl_IterativeFib \
		BenchmarkControl_Sieve \
		BenchmarkCall_RecursiveFib/20 \
		BenchmarkCall_IndirectRecursiveFib \
		BenchmarkMemory_TypedArraySum \
		BenchmarkNumeric_BranchTree; do \
		printf '%s\n' "$$comparisons" | grep -q "^$$scenario/wazero-" || { printf 'missing comparison %s/wazero\n' "$$scenario"; exit 1; }; \
	done; \
	for scenario in \
		BenchmarkCall_RecursiveFib/20 \
		BenchmarkCall_NQueens \
		BenchmarkCall_Fannkuch \
		BenchmarkNumeric_NBody \
		BenchmarkNumeric_SpectralNorm \
		BenchmarkNumeric_Mandelbrot \
		BenchmarkNumeric_MatMul \
		BenchmarkMemory_BinaryTrees \
		BenchmarkMemory_SortStress \
		BenchmarkMemory_StringBuild; do \
		for runtime in native cpython; do \
			printf '%s\n' "$$comparisons" | grep -q "^$$scenario/$$runtime-" || { printf 'missing comparison %s/%s\n' "$$scenario" "$$runtime"; exit 1; }; \
		done; \
	done

lint: fmt vet

fmt:
	@command -v $(GOIMPORTS) >/dev/null
	@$(GOIMPORTS) -w .

vet:
	@go vet ./...

doc: init
	@godoc -http=:6060

fuzz:
	@go test -run='^$$' -fuzz='^FuzzInstructionRoundTrip$$' -fuzztime=10s -parallel=$(fuzz-parallel) $(test-options) ./instr
	@go test -run='^$$' -fuzz='^FuzzParse$$' -fuzztime=10s -parallel=$(fuzz-parallel) $(test-options) ./instr
	@go test -run='^$$' -fuzz='^FuzzOptimizerParity$$' -fuzztime=10s -parallel=$(fuzz-parallel) $(test-options) ./optimize
	@go test -run='^$$' -fuzz='^FuzzParseProgram$$' -fuzztime=10s -parallel=$(fuzz-parallel) $(test-options) ./program
	@go test -run='^$$' -fuzz='^FuzzVerify$$' -fuzztime=10s -parallel=$(fuzz-parallel) $(test-options) ./program
	@go test -run='^$$' -fuzz='^FuzzParseFunction$$' -fuzztime=10s -parallel=$(fuzz-parallel) $(test-options) ./types
	@go test -run='^$$' -fuzz='^FuzzParseType$$' -fuzztime=10s -parallel=$(fuzz-parallel) $(test-options) ./types
