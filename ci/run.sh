#!/usr/bin/env bash

# SPDX-FileCopyrightText: 2026 The Homelab Control Authors
#
# SPDX-License-Identifier: GPL-3.0-or-later

# Runs one command on a copy of this checkout in a throwaway container (Jenkins, ci/README.md).
#
#   ci/run.sh [--image toolchain|e2e|IMAGE] [--root] [--out PATH]... [--env NAME]... -- COMMAND [ARG]...
#
# toolchain and e2e are built from ci/Dockerfile with the pinned versions. Any other IMAGE (for
# example debian:stable) is used as it is. The checkout, including .git, is streamed into /src. No
# bind mount is used, so the agent's paths never reach the Docker daemon. Afterwards each --out path
# is copied back from /src (also when COMMAND failed, so reports survive), the container is removed,
# and the exit status is COMMAND's. Nothing runs with --privileged; without --root, the command runs
# unprivileged with no capabilities. --env passes the named variable from this environment; nothing
# else from the agent's environment (or any credential) reaches the container. Rootless daemon only.
set -euo pipefail
cd "$(git rev-parse --show-toplevel)"

image=toolchain root=0 outs=() envs=()
while [ $# -gt 0 ]; do
  case "$1" in
    --image) image=$2; shift 2 ;;
    --root) root=1; shift ;;
    --out) outs+=("$2"); shift 2 ;;
    --env) envs+=(--env "$2"); shift 2 ;;
    --) shift; break ;;
    *) echo "ci/run.sh: unknown option $1" >&2; exit 2 ;;
  esac
done
[ $# -gt 0 ] || { echo "ci/run.sh: no command" >&2; exit 2; }

# The production rootful daemon must never run CI code.
docker info --format '{{json .SecurityOptions}}' | grep -q rootless \
  || { echo "ci/run.sh: DOCKER_HOST is not a rootless Docker daemon; refusing" >&2; exit 2; }

# Every version comes from its existing source of truth; a missing one is an error.
pin() { # pin NAME VALUE
  [ -n "$2" ] || { echo "ci/run.sh: cannot read the $1 pin" >&2; exit 2; }
  printf '%s' "$2"
}
env_pin() { sed -n "s/^$1=//p" packaging/release/toolchain.env; }
ci=.github/workflows/ci.yml

build_image() { # build_image TARGET -> prints the tag
  local args=(
    --build-arg "GO_VERSION=$(pin GO_VERSION "$(env_pin GO_VERSION)")"
    --build-arg "NODE_VERSION=$(pin NODE_VERSION "$(env_pin NODE_VERSION)")"
    --build-arg "RUST_VERSION=$(pin RUST_VERSION "$(env_pin RUST_VERSION)")"
    --build-arg "GOVULNCHECK_VERSION=$(pin govulncheck "$(grep -o 'govulncheck@v[0-9.]*' "$ci" | head -n1 | cut -d@ -f2)")"
    --build-arg "SPDX_TOOLS_VERSION=$(pin spdx-tools "$(grep -o 'spdx-tools==[0-9.]*' "$ci" | head -n1 | cut -d= -f3)")"
    --build-arg "REUSE_VERSION=$(pin reuse "$(grep -o 'reuse==[0-9.]*' "$ci" | head -n1 | cut -d= -f3)")"
    --build-arg "PLAYWRIGHT_VERSION=$(pin playwright "$(grep -o 'playwright==[0-9.]*' "$ci" | head -n1 | cut -d= -f3)")"
  )
  local tag
  tag="hc-ci-$1:$( { cat ci/Dockerfile; printf '%s\n' "${args[@]}"; } | sha256sum | cut -c1-16)"
  if ! docker image inspect "$tag" >/dev/null 2>&1; then
    echo "ci/run.sh: building $tag" >&2
    docker build --progress=plain --pull --target "$1" --label hc-ci=1 "${args[@]}" -t "$tag" -f ci/Dockerfile ci >&2
  fi
  printf '%s' "$tag"
}

case "$image" in
  toolchain|e2e) ref=$(build_image "$image") ;;
  *) ref=$image ;;
esac

name="hc-ci-$(printf '%s' "${BUILD_TAG:-local}" | tr -c 'a-zA-Z0-9_.-' '-' | cut -c1-80)-$$-$RANDOM"
trap 'docker rm -f "$name" >/dev/null 2>&1 || true' EXIT
trap 'exit 143' TERM INT

opts=(--name "$name" --label hc-ci=1 --label "hc-ci.build=${BUILD_TAG:-local}"
  --cpus "${HC_CI_CPUS:-4}" --memory "${HC_CI_MEMORY:-8g}" --pids-limit 8192
  --security-opt no-new-privileges --shm-size 1g -i "${envs[@]}")
if [ "$root" = 1 ]; then
  opts+=(--user 0)
else
  opts+=(--cap-drop ALL)
fi
echo "ci/run.sh: $ref ($(docker image inspect --format '{{.Id}}' "$ref" 2>/dev/null || echo 'pulled on first use')): $*" >&2

set +e
# shellcheck disable=SC2016 # expanded by the container's shell
tar -C . --exclude=./ci-out -cf - . | docker run "${opts[@]}" "$ref" \
  sh -c 'mkdir -p /src && tar -xf - -C /src --no-same-owner && cd /src && exec "$@"' sh "$@"
status=$?
set -e

for out in "${outs[@]}"; do
  mkdir -p "$out"
  docker cp "$name:/src/$out/." "$out/" >/dev/null 2>&1 || echo "ci/run.sh: no $out in the container" >&2
done
exit "$status"
