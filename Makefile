.PHONY: build web-build web-check web-test test vet fmt-check docs-check check svu-version-check bump

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

# Lint (ESLint), formatting (Prettier) and type checks of the app, the tests
# and the Vite config. The web build itself runs only vite build.
web-check: web/node_modules/.install-stamp
	$(PNPM) --dir web run check

test:
	go test -race ./...

vet:
	go vet ./...

fmt-check:
	@test -z "$$(gofmt -l cmd internal)" || (gofmt -l cmd internal; exit 1)

docs-check:
	go run ./internal/doccheck

check: web-build web-check web-test fmt-check vet test docs-check build

# svu's release, read from mise.toml; make bump checks it (ADR-0015).
SVU_VERSION := $(strip $(shell sed -n 's/^svu = "\(.*\)"/\1/p' mise.toml))
SVU ?= svu

svu-version-check:
	@test -n "$(SVU_VERSION)" || { echo "mise.toml pins no svu"; exit 1; }
	@if ! command -v "$(SVU)" >/dev/null 2>&1; then \
		echo "svu $(SVU_VERSION) is required for make bump (not installed; run: mise install)"; \
		exit 1; \
	fi; \
	installed="$$($(SVU) --version 2>&1 | sed -n 's/^GitVersion:[[:space:]]*//p' | head -1)"; \
	if [ "$$installed" != "$(SVU_VERSION)" ]; then \
		echo "expected svu $(SVU_VERSION), found $${installed:-unknown} (run: mise install)"; \
		exit 1; \
	fi

# bump is the only sanctioned way to tag a release (ADR-0015): on main, equal
# to origin/main, with a clean tree and a passing gate, it tags svu's next
# version and pushes the tag. The tag starts the Check workflow's release path,
# which publishes and attests ghcr.io/bketelsen/trimlights:vX.Y.Z.
bump: svu-version-check
	@test "$$(git rev-parse --abbrev-ref HEAD)" = main || { echo "make bump runs on main"; exit 1; }
	@git fetch origin main
	@test "$$(git rev-parse HEAD)" = "$$(git rev-parse origin/main)" || { echo "HEAD is not origin/main"; exit 1; }
	@test -z "$$(git status --porcelain)" || { echo "the working tree is not clean"; exit 1; }
	$(MAKE) check
	@test -z "$$(git status --porcelain)" || { echo "the gate changed the tree; commit it first"; exit 1; }
	@set -eu; \
	version=$$($(SVU) next); \
	printf '%s\n' "$$version" | grep -Eqx 'v[0-9]+\.[0-9]+\.[0-9]+' || { echo "svu next gave '$$version', not vX.Y.Z"; exit 1; }; \
	git tag -a "$$version" -m "Version $$version"; \
	echo "Tagged $$version; pushing it to origin..."; \
	git push origin "refs/tags/$$version"
