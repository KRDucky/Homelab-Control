# SPDX-FileCopyrightText: 2026 The Homelab Control Authors
#
# SPDX-License-Identifier: GPL-3.0-or-later

# Homelab Control — reproducible local build.
# Requirements: Go >= 1.26, Node >= 22 with npm, GNU make, git. See docs/dev/DEVELOPMENT.md.

SHELL := /bin/bash
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo 0.0.0-dev)
SOURCE_DATE_EPOCH ?= $(shell git log -1 --format=%ct 2>/dev/null || echo 0)
export SOURCE_DATE_EPOCH
GO ?= go
# Rust audit-retention worker (ADR 0006 amendment 4): static musl build, pinned by
# packaging/release/toolchain.env and backend/audit-retention/rust-toolchain.toml.
CARGO ?= cargo
RUST_TARGET ?= x86_64-unknown-linux-musl
RUST_DIR := backend/audit-retention
CARGO_TARGET_DIR ?= $(CURDIR)/$(RUST_DIR)/target
export CARGO_TARGET_DIR
CARGO_HOME_DIR := $(or $(CARGO_HOME),$(HOME)/.cargo)
# Reproducible: crate sources map to /crates whether they come from the registry (git build) or
# vendor-sources/cargo (release source archive); no build paths, one codegen unit (Cargo.toml).
RUSTFLAGS_BUILD := --remap-path-prefix=$(CURDIR)=/src --remap-path-prefix=$(CURDIR)/vendor-sources/cargo=/crates
RUST_BIN := $(CARGO_TARGET_DIR)/$(RUST_TARGET)/release/hc-audit-retention
GOFLAGS_BUILD := -trimpath -buildvcs=false
LDFLAGS := -s -w -buildid= -X homelab-control/backend/internal/version.Version=$(VERSION)
OUT := build

.PHONY: all deps frontend backend build test test-go test-rust test-retention-xcheck test-frontend test-release lint fmt check security \
        verify-upstream verify-extensions repro-check deb rpm systemd-smoke e2e clean

all: build

# Installs frontend dependencies only when the lockfile changed (stamp file),
# so lint/test work on a fresh clone without reinstalling every time.
deps: frontend/app/node_modules/.hc-stamp

frontend/app/node_modules/.hc-stamp: frontend/app/package-lock.json frontend/app/package.json
	cd frontend/app && npm ci --ignore-scripts --no-fund
	touch $@

frontend: deps
	cd frontend/app && npm run build
	find backend/internal/webui/dist -mindepth 1 ! -name README.md -delete
	cp -r frontend/app/dist/. backend/internal/webui/dist/

backend:
	mkdir -p $(OUT)
	cd backend && CGO_ENABLED=0 $(GO) build $(GOFLAGS_BUILD) -ldflags '$(LDFLAGS)' -o ../$(OUT)/hc-api ./cmd/hc-api
	cd backend && CGO_ENABLED=0 $(GO) build $(GOFLAGS_BUILD) -ldflags '$(LDFLAGS)' -o ../$(OUT)/hlctl ./cmd/hlctl
	cd backend && CGO_ENABLED=0 $(GO) build $(GOFLAGS_BUILD) -ldflags '$(LDFLAGS)' -o ../$(OUT)/hc-broker ./cmd/hc-broker
	cd $(RUST_DIR) && $(CARGO) fetch --locked
	cd $(RUST_DIR) && reg=$$(ls -d "$(CARGO_HOME_DIR)"/registry/src/index.crates.io-* 2>/dev/null | head -1); \
	  RUSTFLAGS="$(RUSTFLAGS_BUILD)$${reg:+ --remap-path-prefix=$$reg=/crates}" \
	  CFLAGS_$(subst -,_,$(RUST_TARGET))="-ffile-prefix-map=$(CURDIR)=/src$${reg:+ -ffile-prefix-map=$$reg=/crates} -ffile-prefix-map=$(CURDIR)/vendor-sources/cargo=/crates" \
	  $(CARGO) build --release --locked --target $(RUST_TARGET)
	cp $(RUST_BIN) $(OUT)/hc-audit-retention

build: frontend backend
	cd $(OUT) && sha256sum hc-api hc-broker hlctl hc-audit-retention > SHA256SUMS && cat SHA256SUMS

test: test-go test-rust test-frontend test-release

test-go:
	cd backend && $(GO) test -count=1 -cover ./...

# Rust unit and fixture tests, including the Go golden digest vectors.
test-rust:
	cd $(RUST_DIR) && $(CARGO) test --locked --target $(RUST_TARGET)

# Go writes real records, the release binary archives and prunes them, Go verifies (needs `make build`).
test-retention-xcheck:
	tests/retention/go-cross-check.sh $(OUT)/hc-audit-retention $(OUT)/hlctl

test-frontend: deps
	cd frontend/app && npm test
	node --test sdk/theme/tdk.test.mjs
	node --test sdk/plugin/tdk.test.mjs
	node --test scripts/build-extensions.test.mjs

# Release tooling and workflow policy (docs/release/RELEASING.md); stdlib + PyYAML, no network.
test-release:
	python3 -m unittest discover -s tests/release -t . -v
	tests/release/version-order-test.sh

lint: deps
	node scripts/build-theme-sdk.mjs --check
	node scripts/build-plugin-sdk.mjs --check
	node scripts/build-extensions.mjs --check
	python3 -I docs/ui/hcis/tokens/gen-css.py --check
	@test -z "$$(cd backend && gofmt -l .)" || { echo "gofmt needed:"; cd backend && gofmt -l .; exit 1; }
	cd backend && $(GO) vet ./...
	cd $(RUST_DIR) && $(CARGO) fmt --check && $(CARGO) clippy --locked --target $(RUST_TARGET) --all-targets -- -D warnings
	cd frontend/app && npm run -s typecheck && npm run -s lint && npm run -s format:check

fmt:
	cd backend && gofmt -w .
	cd $(RUST_DIR) && $(CARGO) fmt
	cd frontend/app && npm run -s format

security:
	scripts/security-check.sh

verify-upstream:
	scripts/verify-upstream.sh

verify-extensions:
	scripts/verify-extensions.sh

check: lint test security

# Builds twice from clean trees and compares artifact hashes.
repro-check:
	scripts/repro-check.sh

deb: build
	packaging/debian/build-deb.sh $(VERSION)

rpm: build
	packaging/fedora/build-rpm.sh $(VERSION)

# Real-systemd package test in a disposable Ubuntu container (root + docker + debootstrap).
# REL=resolute (26.04) | noble (24.04, default) | jammy (22.04)
REL ?= noble
systemd-smoke: deb
	dev/disposable/systemd-container.sh $(REL) $$(ls -t build/*.deb | head -1) \
	  HC_SMOKE_EXTRA_UNITS=homelab-control-broker.socket HC_SMOKE_EXTRA_CHECKS=/opt/hc/tests/m1-checks.sh \
	  HC_WEBMIN_SRC=/opt/hc/webmin HC_SMOKE_SCOPES=system.read,services.read,hardware.read

e2e: build
	tests/integration/run-hc-e2e.sh

clean:
	rm -rf $(OUT) frontend/app/dist
	find backend/internal/webui/dist -mindepth 1 ! -name README.md -delete
