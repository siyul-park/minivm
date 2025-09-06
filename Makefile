-include .env

PROJECT = $(shell basename -s .git $(shell git config --get remote.origin.url))

.PHONY: init generate build clean tidy update sync check test coverage benchmark lint fmt vet doc
all: lint test build

init:
	@$(MAKE) install-tools
	@$(MAKE) install-modules

install-tools:
	@go install golang.org/x/tools/cmd/godoc@latest
	@go install golang.org/x/tools/cmd/goimports@latest
	@go install honnef.co/go/tools/cmd/staticcheck@latest

install-modules:
	@go install -v ./...

generate:
	@go generate ./...

build:
	@go clean -cache
	@mkdir -p dist
	@go build -ldflags "-s -w" -o ./dist/$(PROJECT) ./cmd/...

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

check: lint test staticcheck

test:
	@go test -race $(test-options) ./...

coverage:
	@go test -race --coverprofile=coverage.out --covermode=atomic $(test-options) ./...

benchmark:
	@go test -run="-" -bench=".*" -benchmem $(test-options) ./...

lint: fmt vet staticcheck

fmt:
	@goimports -w .

vet:
	@go vet ./...

staticcheck:
	@staticcheck ./...

doc: init
	@godoc -http=:6060
