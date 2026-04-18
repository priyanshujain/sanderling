SHELL := /bin/bash

ANDROID_HOME ?= /opt/homebrew/share/android-commandlinetools
GRADLE := ./gradlew
GO := go
BUF := buf

GO_PACKAGES := ./...
SIDECAR_JAR := sidecar/build/libs/sidecar-all.jar
SIDECAR_EMBED := internal/sidecar/assets/sidecar-all.jar
SDK_AAR := sdk/android/build/outputs/aar/sdk-android-release.aar
UATU_BIN := bin/uatu

.PHONY: bootstrap proto sidecar sdk-android sdk-android-publish uatu test test-go test-kotlin test-spec-api docs clean release-cli release-android-local release-npm-dry

bootstrap:
	$(GO) mod download
	$(BUF) generate
	cd pkg/spec-api && npm install --silent

proto:
	$(BUF) lint
	$(BUF) generate

sidecar:
	ANDROID_HOME=$(ANDROID_HOME) $(GRADLE) :sidecar:shadowJar

sdk-android:
	ANDROID_HOME=$(ANDROID_HOME) $(GRADLE) :sdk-android:assembleRelease

sdk-android-publish:
	ANDROID_HOME=$(ANDROID_HOME) $(GRADLE) :sdk-android:publishToMavenLocal

uatu: $(UATU_BIN)

$(UATU_BIN): $(SIDECAR_JAR)
	mkdir -p bin $(dir $(SIDECAR_EMBED))
	cp $(SIDECAR_JAR) $(SIDECAR_EMBED)
	$(GO) build -tags withsidecar -o $(UATU_BIN) ./cmd/uatu

$(SIDECAR_JAR):
	$(MAKE) sidecar

test: test-go test-kotlin test-spec-api

test-go:
	$(GO) test $(GO_PACKAGES)

test-kotlin:
	ANDROID_HOME=$(ANDROID_HOME) $(GRADLE) :sidecar:test :sdk-android:testDebugUnitTest

test-spec-api:
	cd pkg/spec-api && npm test --silent

docs:
	./scripts/build-docs.sh

clean:
	$(GO) clean
	rm -rf bin dist pkg/spec-api/dist build/site
	$(GRADLE) clean

# Local release dry-runs. None of these touch remote registries.

release-cli:
	$(MAKE) sidecar
	goreleaser release --snapshot --clean

release-android-local:
	@if [ -f .env.local ]; then set -a; . ./.env.local; set +a; fi; \
	  ANDROID_HOME=$(ANDROID_HOME) $(GRADLE) :sdk-android:publishToMavenLocal -Puatu.version=0.0.0-local

release-npm-dry:
	cd pkg/spec-api && npm ci && npm run build && npm pack --dry-run
