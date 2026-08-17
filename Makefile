SHELL := /bin/bash

ANDROID_HOME ?= /opt/homebrew/share/android-commandlinetools
GRADLE := ./gradlew
GO := go
BUF := buf

GO_PACKAGES := ./...
SIDECAR_JAR := sidecar/build/libs/sidecar-all.jar
SIDECAR_EMBED := internal/sidecarassets/assets/sidecar-all.jar
COMPANION_EMBED := internal/driver/ioscompanion/companionassets/assets/companion-1.1.8.tar.gz
COMPANION_PREPARE := internal/driver/ioscompanion/companionassets/prepare.sh
RUNNER_EMBED := internal/driver/ioscompanion/runnerassets/assets/runner-1.0.0.tar.gz
RUNNER_PREPARE := companion/prepare.sh
RUNNER_SRC := $(shell find companion/Sources -type f -name '*.swift' 2>/dev/null) companion/project.yml
SIDECAR_SRC := $(shell find sidecar/src -type f \( -name '*.kt' -o -name '*.kts' \) 2>/dev/null) sidecar/build.gradle.kts build.gradle.kts settings.gradle.kts
SANDERLING_BIN := bin/sanderling

DOCS_SRC      := $(shell find docs -type f -name '*.md' -not -path 'docs/_*')
INDEX_SRC     := $(filter %index.md,$(DOCS_SRC))
PAGE_SRC      := $(filter-out %index.md,$(DOCS_SRC))
INDEX_OUT     := $(patsubst docs/%.md,build/site/%.html,$(INDEX_SRC))
PAGE_OUT      := $(patsubst docs/%.md,build/site/%/index.html,$(PAGE_SRC))
DOCS_OUT      := $(INDEX_OUT) $(PAGE_OUT)
DOCS_TEMPLATE := docs/_template/page.html

REPLAY_DIST := internal/replay/dist
WEB_DIST := replay-ui/dist

GOLINES := $(shell $(GO) env GOPATH)/bin/golines

.PHONY: bootstrap proto sidecar sidecar-embed sanderling build sanderling-web sanderling-android sanderling-ios install test test-go test-browser test-companion test-kotlin test-folio test-spec-api test-ci-scripts spec-typecheck web-test web-typecheck web-build web-dev replay-dev docs clean release-cli release-npm-dry fmt fmt-go fmt-kotlin fmt-ts fmt-swift

bootstrap:
	$(GO) mod download
	$(BUF) generate
	cd pkg/spec && npm install --silent

proto:
	$(BUF) lint
	$(BUF) generate

sidecar: $(SIDECAR_JAR)

# Stages the JAR where //go:embed expects it. Release tooling calls this rather
# than copying the JAR itself, so the embed path is spelled out in one place.
sidecar-embed: $(SIDECAR_EMBED)

sanderling: $(SANDERLING_BIN)

# `build` is a directory at the repository root, so `make build` matched it and
# reported nothing to be done while leaving a stale binary in bin/ for the next
# command to use. Aliasing it is cheaper than expecting everyone to remember
# that the target is named after the binary.
build: sanderling

$(SANDERLING_BIN): $(SIDECAR_EMBED) $(COMPANION_EMBED) $(RUNNER_EMBED) web-build
	mkdir -p bin
	$(GO) build -tags "withsidecar withcompanion" -o $(SANDERLING_BIN) ./cmd/sanderling

# Per-platform builds, each taking only the tags that platform needs. `make
# sanderling` cannot run on Linux at all: preparing the iOS companion asset
# shells out to a macOS homebrew path. The CI workflows build through these so
# a job and a developer produce the same binary.
sanderling-web: web-build
	mkdir -p bin
	$(GO) build -o $(SANDERLING_BIN) ./cmd/sanderling

sanderling-android: $(SIDECAR_EMBED) web-build
	mkdir -p bin
	$(GO) build -tags withsidecar -o $(SANDERLING_BIN) ./cmd/sanderling

sanderling-ios: $(COMPANION_EMBED) $(RUNNER_EMBED) web-build
	mkdir -p bin
	$(GO) build -tags withcompanion -o $(SANDERLING_BIN) ./cmd/sanderling

# Installs `sanderling` into $GOBIN (or $GOPATH/bin) so it's directly on PATH for
# anyone with a standard Go toolchain setup.
install: $(SIDECAR_EMBED) $(COMPANION_EMBED) $(RUNNER_EMBED) web-build
	$(GO) install -tags "withsidecar withcompanion" ./cmd/sanderling
	@dest="$$($(GO) env GOBIN)"; [ -n "$$dest" ] || dest="$$($(GO) env GOPATH)/bin"; echo "installed sanderling to $$dest"

web-build:
	cd replay-ui && bun install --frozen-lockfile && bun run build
	mkdir -p $(REPLAY_DIST)
	rm -rf $(REPLAY_DIST)/assets
	cp -R $(WEB_DIST)/. $(REPLAY_DIST)/

web-dev:
	cd replay-ui && bun run dev

replay-dev: $(SIDECAR_EMBED)
	$(GO) run -tags withsidecar ./cmd/sanderling replay --dev

web-typecheck:
	cd replay-ui && bun install --frozen-lockfile && bun run typecheck

$(SIDECAR_JAR): $(SIDECAR_SRC)
	ANDROID_HOME=$(ANDROID_HOME) $(GRADLE) :sidecar:shadowJar

$(SIDECAR_EMBED): $(SIDECAR_JAR)
	mkdir -p $(dir $@)
	cp $< $@

$(COMPANION_EMBED): $(COMPANION_PREPARE)
	$(COMPANION_PREPARE)

$(RUNNER_EMBED): $(RUNNER_SRC) $(RUNNER_PREPARE)
	$(RUNNER_PREPARE)

# Each language enforces an 80-column limit through its own formatter config:
# Go via golines flags (gofmt has no width option), Kotlin via .editorconfig
# (ktlint), TypeScript/JS via .prettierrc.json, Swift via .swift-format.
fmt: fmt-go fmt-kotlin fmt-ts fmt-swift

fmt-go:
	$(GOLINES) -m 80 --ignore-generated -w ./internal ./cmd

fmt-kotlin:
	ktlint -F "sidecar/src/**/*.kt" "sidecar/src/**/*.kts"

fmt-ts:
	cd replay-ui && bunx prettier --write "src/**/*.{ts,tsx}"
	cd examples/folio-web && bunx prettier --write "src/**/*.{ts,tsx}"
	cd pkg/spec && npx --yes prettier --write "src/**/*.ts" "test/**/*.ts"

fmt-swift:
	xcrun swift-format format -i -r companion/Sources

test: test-go test-kotlin spec-typecheck test-spec-api web-typecheck web-test test-ci-scripts

test-go:
	$(GO) test $(GO_PACKAGES)

web-test:
	cd replay-ui && bun install --frozen-lockfile && bun test

# Drives small web fixtures and the chrome driver through real headless Chrome.
# Kept out of `test` because it needs a Chrome binary on PATH. -p 1 runs the two
# packages one after the other: both launch Chrome, and launching two at once
# has failed with "Launch: context canceled".
test-browser:
	$(GO) test -p 1 -tags browser ./test/browser/... ./internal/driver/chrome/...

# Runs the withcompanion-tagged tests (asset embedding, extraction, checksum
# reuse) against the real companion and runner bundles. Kept out of `test`
# because preparing the companion bundle needs the darwin toolchain.
test-companion: $(COMPANION_EMBED) $(RUNNER_EMBED)
	$(GO) test -tags withcompanion ./internal/driver/ioscompanion/...

test-kotlin:
	ANDROID_HOME=$(ANDROID_HOME) $(GRADLE) :sidecar:test

# folio is its own gradle build, so nothing in the root build runs its tests.
# Kept out of `test` because the metro plugin folio compiles with needs a 21+
# runtime, where the sidecar toolchain pins 17: folding this in would raise the
# JDK floor of the target everyone runs constantly. CI runs it as its own step.
test-folio:
	cd examples/folio && ANDROID_HOME=$(ANDROID_HOME) $(GRADLE) \
		:core:testDebugUnitTest :app:shared:testDebugUnitTest

# The CI scripts that read a trace and decide whether a green leg is
# evidence. bash and python3 only, which is all a runner has.
test-ci-scripts:
	.github/scripts/replay-ui-summary-test.sh
	.github/scripts/folio-run-test.sh
	.github/scripts/next-version-test.sh

test-spec-api:
	cd pkg/spec && npm test --silent

spec-typecheck:
	cd pkg/spec && npm run check --silent

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
	rm -rf bin dist pkg/spec/dist build/site
	$(GRADLE) clean

# Local release dry-runs. None of these touch remote registries.

release-cli: $(SIDECAR_JAR)
	goreleaser release --snapshot --clean

release-npm-dry:
	cd pkg/spec && npm ci && npm run build && npm pack --dry-run
