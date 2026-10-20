# SPDX-FileCopyrightText: 2026 The Homelab Control Authors
#
# SPDX-License-Identifier: GPL-3.0-or-later

# Homelab Control — reproducible local build.
# Requirements: Go >= 1.24, Node >= 22 with npm, GNU make, git. See docs/dev/DEVELOPMENT.md.

SHELL := /bin/bash
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo 0.0.0-dev)
SOURCE_DATE_EPOCH ?= $(shell git log -1 --format=%ct 2>/dev/null || echo 0)
export SOURCE_DATE_EPOCH
GO ?= go
GOFLAGS_BUILD := -trimpath -buildvcs=false
LDFLAGS := -s -w -buildid= -X homelab-control/backend/internal/version.Version=$(VERSION)
OUT := build

.PHONY: all deps frontend backend build test test-go test-frontend test-release lint fmt check security \
        verify-upstream repro-check deb rpm systemd-smoke e2e clean

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

build: frontend backend
	cd $(OUT) && sha256sum hc-api hc-broker hlctl > SHA256SUMS && cat SHA256SUMS

test: test-go test-frontend test-release

test-go:
	cd backend && $(GO) test -count=1 -cover ./...

test-frontend: deps
	cd frontend/app && npm test

# Release tooling and workflow policy (docs/release/RELEASING.md); stdlib + PyYAML, no network.
test-release:
	python3 -m unittest discover -s tests/release -t . -v
	tests/release/version-order-test.sh

lint: deps
	@test -z "$$(cd backend && gofmt -l .)" || { echo "gofmt needed:"; cd backend && gofmt -l .; exit 1; }
	cd backend && $(GO) vet ./...
	cd frontend/app && npm run -s typecheck && npm run -s lint && npm run -s format:check

fmt:
	cd backend && gofmt -w .
	cd frontend/app && npm run -s format

security:
	scripts/security-check.sh

verify-upstream:
	scripts/verify-upstream.sh

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
