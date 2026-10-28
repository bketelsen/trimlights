.PHONY: build web-build web-test test vet fmt-check docs-check check

PNPM ?= pnpm

web/node_modules/.install-stamp: web/package.json web/pnpm-lock.yaml
	$(PNPM) --dir web install --frozen-lockfile
	touch $@

web-build: web/node_modules/.install-stamp
	$(PNPM) --dir web run build
	touch internal/webui/dist/.gitkeep

VERSION ?= dev
COMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
DATE ?= unknown
BUILT_BY ?= local
LDFLAGS = -X main.version=$(VERSION) -X main.commit=$(COMMIT) -X main.date=$(DATE) -X main.builtBy=$(BUILT_BY)

build: web-build
	mkdir -p build/$(shell go env GOARCH)
	CGO_ENABLED=0 go build -trimpath -ldflags '$(LDFLAGS)' -o build/trimlight ./cmd/trimlight
	cp build/trimlight build/$(shell go env GOARCH)/trimlight

web-test: web/node_modules/.install-stamp
	$(PNPM) --dir web test

test:
	go test -race ./...

vet:
	go vet ./...

fmt-check:
	@test -z "$$(gofmt -l cmd internal)" || (gofmt -l cmd internal; exit 1)

docs-check:
	go run ./internal/doccheck

check: web-build web-test fmt-check vet test docs-check build
