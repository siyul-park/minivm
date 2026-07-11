-include .env

PROJECT = $(shell basename -s .git $(shell git config --get remote.origin.url))

.PHONY: init generate build clean tidy update sync check check-generated check-tidy check-fmt check-arm64 test coverage benchmark benchmark-fusion lint fmt vet doc
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
	@go run ./internal/cmd/genfusion

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
	@go run ./internal/cmd/genfusion -check

check-tidy:
	@go mod tidy -diff

check-fmt:
	@test -z "$$(gofmt -l .)"
	@test -z "$$(goimports -l .)"

check-arm64:
	@GOOS=linux GOARCH=arm64 go build ./...
	@GOOS=linux GOARCH=arm64 go test -exec=true ./...

test:
	@go test -race $(test-options) ./...

coverage:
	@go test -race --coverprofile=coverage.out --covermode=atomic $(test-options) ./...

benchmark:
	@go test -run="-" -bench=".*" -benchmem $(test-options) ./...
	@(cd benchmarks && go test -run="-" -bench=".*" -benchmem $(test-options) ./...)

benchmark-fusion:
	@(cd benchmarks && go test -run="^$$" -bench="Fusion|RefFusion" -benchmem -count=10 ./...)

lint: fmt vet

fmt:
	@goimports -w .

vet:
	@go vet ./...

doc: init
	@godoc -http=:6060
