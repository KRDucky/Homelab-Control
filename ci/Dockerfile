# SPDX-FileCopyrightText: 2026 The Homelab Control Authors
#
# SPDX-License-Identifier: GPL-3.0-or-later

# Build environment for the Jenkins pipeline (ci/README.md). It holds the same tools as the GitHub
# Actions jobs, at the same versions. ci/run.sh passes every version: Go, Node and Rust from
# packaging/release/toolchain.env, the tool pins from .github/workflows/ci.yml. There are no
# defaults here, so a version is never pinned twice. Only official images are used.

ARG GO_VERSION
ARG NODE_VERSION
ARG RUST_VERSION
ARG PLAYWRIGHT_VERSION

FROM docker.io/library/golang:${GO_VERSION}-trixie AS go
FROM docker.io/library/node:${NODE_VERSION}-trixie AS node

# Lint, build, tests, security gate, packages and the release dry run.
FROM docker.io/library/rust:${RUST_VERSION}-trixie AS toolchain
ARG GOVULNCHECK_VERSION
ARG SPDX_TOOLS_VERSION
ARG REUSE_VERSION
RUN test -n "$GOVULNCHECK_VERSION" && test -n "$SPDX_TOOLS_VERSION" && test -n "$REUSE_VERSION"
# Distribution packages at the release's current versions, as the GitHub job installs them (rpm,
# as on the GitHub runner, for the RPM ordering check in tests/release/version-order-test.sh); pinning
# Debian versions would break when the archive drops the old ones.
# hadolint ignore=DL3008
RUN apt-get update && apt-get install -y --no-install-recommends \
      file lintian musl-tools python3-venv python3-yaml rpm shellcheck zstd \
    && rm -rf /var/lib/apt/lists/*
# Same components and target as backend/audit-retention/rust-toolchain.toml, installed now so a
# build never writes to RUSTUP_HOME.
RUN rustup component add rustfmt clippy && rustup target add x86_64-unknown-linux-musl
COPY --from=go /usr/local/go /usr/local/go
COPY --from=node /usr/local/bin/node /usr/local/bin/node
COPY --from=node /usr/local/lib/node_modules /usr/local/lib/node_modules
RUN ln -s ../lib/node_modules/npm/bin/npm-cli.js /usr/local/bin/npm \
    && ln -s ../lib/node_modules/npm/bin/npx-cli.js /usr/local/bin/npx
ENV PATH=/usr/local/go/bin:/opt/ci-py/bin:$PATH \
    GOTOOLCHAIN=local \
    CC_x86_64_unknown_linux_musl=musl-gcc
RUN GOBIN=/usr/local/bin go install "golang.org/x/vuln/cmd/govulncheck@${GOVULNCHECK_VERSION}" \
    && rm -rf /root/go /root/.cache
RUN python3 -m venv /opt/ci-py \
    && /opt/ci-py/bin/pip install --no-cache-dir -q "spdx-tools==${SPDX_TOOLS_VERSION}" "reuse==${REUSE_VERSION}"
# Graphify for the "graphify" stage, in its own environment, every package pinned by hash.
COPY graphify-requirements.txt /opt/graphify-requirements.txt
RUN python3 -m venv /opt/graphify \
    && /opt/graphify/bin/pip install --no-cache-dir -q --require-hashes --only-binary :all: \
       -r /opt/graphify-requirements.txt \
    && /opt/graphify/bin/python -c 'import graphify.extract, graphify.build'
# Builds and tests run as this unprivileged user (Go tests must not run as root; hc-api refuses root).
RUN useradd --create-home --uid 1000 ci && install -d -o ci -g ci /src
USER ci
ENV CARGO_HOME=/home/ci/.cargo
WORKDIR /src

# Browser e2e: the official Playwright image for the pinned Playwright version (Ubuntu 24.04, as the
# GitHub job), plus Node for the accessibility test dependency. The image has the browsers and their
# libraries but not the Python package, which is installed here at the same version.
FROM mcr.microsoft.com/playwright/python:v${PLAYWRIGHT_VERSION}-noble AS e2e
ARG PLAYWRIGHT_VERSION
RUN python3 -m pip install --no-cache-dir --break-system-packages "playwright==${PLAYWRIGHT_VERSION}" \
    && python3 -c 'import playwright'
COPY --from=node /usr/local/bin/node /usr/local/bin/node
COPY --from=node /usr/local/lib/node_modules /usr/local/lib/node_modules
RUN ln -s ../lib/node_modules/npm/bin/npm-cli.js /usr/local/bin/npm \
    && install -d -o pwuser -g pwuser /src
USER pwuser
WORKDIR /src
