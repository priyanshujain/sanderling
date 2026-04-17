SHELL := /bin/bash

ANDROID_HOME ?= /opt/homebrew/share/android-commandlinetools
GRADLE := ./gradlew
GO := go
BUF := buf

GO_PACKAGES := ./...
SIDECAR_JAR := sidecar/build/libs/sidecar-all.jar
SDK_AAR := sdk/android/build/outputs/aar/sdk-android-release.aar
UATU_BIN := bin/uatu

.PHONY: bootstrap proto sidecar sdk-android sdk-android-publish uatu test test-go test-kotlin test-spec-api clean

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
	@if [ -z "$$GH_TOKEN" ]; then echo "GH_TOKEN must be set" >&2; exit 1; fi
	ANDROID_HOME=$(ANDROID_HOME) $(GRADLE) :sdk-android:publish

uatu: $(UATU_BIN)

$(UATU_BIN): $(SIDECAR_JAR)
	mkdir -p bin
	$(GO) build -o $(UATU_BIN) ./cmd/uatu

$(SIDECAR_JAR):
	$(MAKE) sidecar

test: test-go test-kotlin test-spec-api

test-go:
	$(GO) test $(GO_PACKAGES)

test-kotlin:
	ANDROID_HOME=$(ANDROID_HOME) $(GRADLE) :sidecar:test :sdk-android:testDebugUnitTest

test-spec-api:
	cd pkg/spec-api && npm test --silent

clean:
	$(GO) clean
	rm -rf bin
	$(GRADLE) clean
