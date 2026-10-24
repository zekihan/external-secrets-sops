GO := go
GOLANGCI_LINT_VERSION := 2.15.0
GORELEASER_VERSION := 2.19.0
RELEASE_VERSION := $(shell cat VERSION)

.PHONY: fmt tidy lint test test/cover build run audit build/local build/all build/release/all build/release/docker release check-tools check-release-tools
fmt:
	gofmt -w main.go internal
	golangci-lint fmt

tidy:
	$(GO) mod tidy

check-tools:
	@golangci-lint version | grep -q 'version $(GOLANGCI_LINT_VERSION) ' || (echo 'Install golangci-lint $(GOLANGCI_LINT_VERSION)' && exit 1)

check-release-tools:
	@goreleaser --version | grep -Eq 'GitVersion: +$(GORELEASER_VERSION)$$' || (echo 'Install GoReleaser $(GORELEASER_VERSION)' && exit 1)

lint: check-tools
	golangci-lint run

test:
	./scripts/go_test.sh -buildvcs -vet=all ./...
	./scripts/go_test.sh -buildvcs -vet=all -race ./...

test/cover:
	mkdir -p tmp
	./scripts/go_test.sh -race -coverprofile=tmp/coverage.out ./...

build: build/local
build/local:
	mkdir -p dist
	$(GO) build -trimpath -ldflags "-X main.version=$(RELEASE_VERSION)" -o dist/external-secrets-sops .

run: build
	./dist/external-secrets-sops serve --config config.yaml

audit: lint test
	$(GO) mod verify

build/all: check-release-tools
	RELEASE_VERSION=$(RELEASE_VERSION) goreleaser build --clean --snapshot --id all

build/release/all: check-release-tools
	goreleaser build --clean --id all --skip=validate

build/release/docker: check-release-tools
	goreleaser build --clean --id docker --skip=validate

release: check-release-tools
	goreleaser release --clean
