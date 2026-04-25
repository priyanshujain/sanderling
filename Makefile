SHELL := /bin/bash

ANDROID_HOME ?= /opt/homebrew/share/android-commandlinetools
GRADLE := ./gradlew
GO := go
BUF := buf

GO_PACKAGES := ./...
SIDECAR_JAR := sidecar/build/libs/sidecar-all.jar
SIDECAR_EMBED := internal/sidecar/assets/sidecar-all.jar
SANDERLING_BIN := bin/sanderling

DOCS_SRC      := $(shell find docs -type f -name '*.md' -not -path 'docs/_*')
INDEX_SRC     := $(filter %index.md,$(DOCS_SRC))
PAGE_SRC      := $(filter-out %index.md,$(DOCS_SRC))
INDEX_OUT     := $(patsubst docs/%.md,build/site/%.html,$(INDEX_SRC))
PAGE_OUT      := $(patsubst docs/%.md,build/site/%/index.html,$(PAGE_SRC))
DOCS_OUT      := $(INDEX_OUT) $(PAGE_OUT)
DOCS_TEMPLATE := docs/_template/page.html

INSPECT_DIST := internal/inspect/dist
WEB_DIST := inspect-ui/dist

.PHONY: bootstrap proto sidecar sanderling install test test-go test-kotlin test-spec-api web-typecheck web-build web-dev inspect-dev docs clean release-cli release-npm-dry

bootstrap:
	$(GO) mod download
	$(BUF) generate
	cd pkg/spec && npm install --silent

proto:
	$(BUF) lint
	$(BUF) generate

sidecar: $(SIDECAR_JAR)

sanderling: $(SANDERLING_BIN)

$(SANDERLING_BIN): $(SIDECAR_EMBED) web-build
	mkdir -p bin
	$(GO) build -tags withsidecar -o $(SANDERLING_BIN) ./cmd/sanderling

# Installs `sanderling` into $GOBIN (or $GOPATH/bin) so it's directly on PATH for
# anyone with a standard Go toolchain setup.
install: $(SIDECAR_EMBED) web-build
	$(GO) install -tags withsidecar ./cmd/sanderling
	@dest="$$($(GO) env GOBIN)"; [ -n "$$dest" ] || dest="$$($(GO) env GOPATH)/bin"; echo "installed sanderling to $$dest"

web-build:
	cd inspect-ui && bun install --frozen-lockfile && bun run build
	mkdir -p $(INSPECT_DIST)
	rm -rf $(INSPECT_DIST)/assets $(INSPECT_DIST)/fonts
	cp -R $(WEB_DIST)/. $(INSPECT_DIST)/

web-dev:
	cd inspect-ui && bun run dev

inspect-dev: $(SIDECAR_EMBED)
	$(GO) run -tags withsidecar ./cmd/sanderling inspect --dev

web-typecheck:
	cd inspect-ui && bun install --frozen-lockfile && bun run typecheck

$(SIDECAR_JAR):
	ANDROID_HOME=$(ANDROID_HOME) $(GRADLE) :sidecar:shadowJar

$(SIDECAR_EMBED): $(SIDECAR_JAR)
	mkdir -p $(dir $@)
	cp $< $@

test: test-go test-spec-api web-typecheck

test-go:
	$(GO) test $(GO_PACKAGES)

test-kotlin:
	ANDROID_HOME=$(ANDROID_HOME) $(GRADLE) :sidecar:test

test-spec-api:
	cd pkg/spec && npm test --silent

docs: $(DOCS_OUT) build/site/_assets
	@echo "built $(words $(DOCS_OUT)) pages to build/site"

build/site/_assets: docs/_assets
	@mkdir -p build/site
	@rm -rf $@
	@cp -R $< $@

define build_page
	@mkdir -p $(dir $@)
	@pandoc $< --from=gfm --to=html5 --standalone \
	  --highlight-style=tango --template=$(DOCS_TEMPLATE) -o $@
	@rel=$$(echo $(patsubst build/site/%,%,$@) | awk -F/ '{for(i=1;i<NF;i++)printf "../"}'); \
	  sed -i.bak "s|__ROOT__|$$rel|g" $@ && rm $@.bak
endef

$(INDEX_OUT): build/site/%.html: docs/%.md $(DOCS_TEMPLATE)
	$(build_page)

$(PAGE_OUT): build/site/%/index.html: docs/%.md $(DOCS_TEMPLATE)
	$(build_page)

clean:
	$(GO) clean
	rm -rf bin dist pkg/spec-api/dist build/site
	$(GRADLE) clean

# Local release dry-runs. None of these touch remote registries.

release-cli: $(SIDECAR_JAR)
	goreleaser release --snapshot --clean

release-npm-dry:
	cd pkg/spec && npm ci && npm run build && npm pack --dry-run
