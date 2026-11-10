# SPDX-FileCopyrightText: 2026 The Homelab Control Authors
# SPDX-License-Identifier: GPL-3.0-or-later

# Development validation only. No service is deployed or host package installed.
FROM docker.io/library/golang:1.26.10-bookworm@sha256:5de7c99eb78ca5e515a4c340235d0955ec5964a5071c492c15503d7c267da473 AS go
FROM docker.io/library/node:22.23.3-bookworm@sha256:69a5713e7f4997e96d579aafd78734810e0dac0dd5ee4621d72c27a2891a4088 AS tools
COPY --from=go /usr/local/go /usr/local/go
ENV PATH=/usr/local/go/bin:$PATH GOTOOLCHAIN=local GOFLAGS=-mod=readonly CGO_ENABLED=0
WORKDIR /src
RUN chown node:node /src
USER node
RUN test "$(go env GOVERSION)" = "go1.26.10" && test "$(node --version)" = "v22.23.3" && test "$(npm --version)" = "10.9.9"
COPY --chown=node:node frontend/app/package*.json ./frontend/app/
RUN cd frontend/app && npm ci --ignore-scripts --no-fund
COPY --chown=node:node backend/go.mod backend/go.sum ./backend/
RUN cd backend && go mod download
FROM tools AS checks
COPY --chown=node:node . .
# Check-only: formatting must already be applied in the checkout so that built
# artifacts and reviewed source are byte-identical (no in-container rewrite).
RUN unformatted="$(gofmt -l backend/internal/dockerbroker backend/cmd/hc-docker-metadata backend/internal/composediscovery backend/internal/api/compose_preview* backend/internal/traefikclient backend/internal/api/traefik* backend/internal/api/server.go backend/internal/api/capabilities.go backend/internal/auth/users.go backend/internal/modules backend/internal/guard/openapi_test.go backend/cmd/hc-api/main.go backend/internal/platform backend/internal/api/platforms.go backend/internal/api/platforms_test.go backend/internal/api/platforms_compat_test.go backend/internal/api/routing_warnings_test.go backend/internal/routingwarnings backend/internal/guard/broker_link_test.go)" && if [ -n "$unformatted" ]; then echo "gofmt required: $unformatted"; exit 1; fi
RUN cd frontend/app && npx --no-install prettier --check src/pages/Traefik.tsx src/pages/Traefik.css src/traefik.test.tsx src/api.ts src/App.tsx src/icons.tsx src/m1.test.tsx src/shell.test.tsx src/account.test.tsx src/pages/PlatformProxy.tsx src/platforms.test.tsx src/preferences.ts src/AppearanceMenu.tsx
RUN cd backend && go test -count=1 ./...
RUN cd backend && go vet ./...
RUN cd frontend/app && npm test && npm run build && npm run lint && npm run format:check
RUN node --test sdk/plugin/tdk.test.mjs sdk/theme/tdk.test.mjs
RUN make build VERSION=0.1.0-traefik-dev SOURCE_DATE_EPOCH=1794160000
RUN cd backend && go build -trimpath -buildvcs=false -o ../build/hc-docker-metadata ./cmd/hc-docker-metadata && cd ../build && sha256sum hc-docker-metadata >> SHA256SUMS
RUN cd backend && go run ./internal/dockerbroker/testdata/export > ../build/phase7-ui-fixtures.json
FROM scratch AS artifacts
COPY --from=checks /src/backend /backend
COPY --from=checks /src/frontend/app/src /frontend/app/src
COPY --from=checks /src/build /build
COPY --from=checks /src/frontend/app/dist /frontend/app/dist

# Separate read-only readiness checks; audit exit codes are exported, never hidden as pass.
FROM tools AS readiness
COPY --chown=node:node . .
RUN mkdir -p /src/audit && (make lint > /src/audit/lint.log 2>&1; echo $? > /src/audit/lint.exit)
RUN mkdir -p /src/audit && cd frontend/app && (npm audit --json > /src/audit/all.json; echo $? > /src/audit/all.exit) && (npm audit --omit=dev --json > /src/audit/production.json; echo $? > /src/audit/production.exit)
FROM scratch AS readiness-artifacts
COPY --from=readiness /src/audit /audit
