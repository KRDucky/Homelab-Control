#!/usr/bin/env bash

# SPDX-FileCopyrightText: 2026 The Homelab Control Authors
#
# SPDX-License-Identifier: GPL-3.0-or-later

# Negative tests for `scripts/release-tool.py verify` on a REAL release candidate: the unmodified
# candidate must pass, and every mutation below must make verification fail. Works on copies; the
# candidate itself is never changed.
#   tests/release/negative-tests.sh <candidate-dir> <version> <commit> [--require-tools]
set -euo pipefail
cand=${1:?candidate dir}; ver=${2:?version}; commit=${3:?commit}; req=${4:-}
root=$(cd "$(dirname "$0")/../.." && pwd)
tool="$root/scripts/release-tool.py"
# HC_RELEASE_TOOLCHAIN: only for local runs whose toolchain differs from the pins (CI never sets it).
tc=${HC_RELEASE_TOOLCHAIN:-$root/packaging/release/toolchain.env}
mode=release; [[ "$ver" == 0.0.0-dryrun.* ]] && mode=dry-run
work=$(mktemp -d); trap 'rm -rf "$work"' EXIT
fail=0; n=0

extra=(); [ -n "$req" ] && extra=("$req")
verify() { python3 -I "$tool" verify --dir "$1" --version "${2:-$ver}" --commit "${3:-$commit}" --toolchain "${4:-$tc}" --mode "$mode" "${extra[@]}" >"$work/out.txt" 2>&1; }
fresh() { rm -rf "$work/c"; cp -a "$cand" "$work/c"; }
resum() { (cd "$work/c" && rm SHA256SUMS && sha256sum -- * > "$work/sums" && mv "$work/sums" SHA256SUMS); }
expect_fail() { # description, then the mutation has already been applied to $work/c
  n=$((n + 1))
  if verify "$work/c" "${@:2}"; then echo "FAIL  not detected: $1"; fail=1
  else echo "PASS  detected: $1 ($(grep -m1 '^FAIL' "$work/out.txt" | cut -c1-140))"; fi
}

fresh
if verify "$work/c"; then echo "PASS  unmodified candidate verifies"; else echo "FAIL  unmodified candidate does not verify:"; cat "$work/out.txt"; exit 1; fi

fresh; rm "$work/c"/homelab-control_*_amd64.deb; resum
expect_fail "missing .deb"

fresh; rm "$work/c"/*.pkg.tar.zst; resum
expect_fail "missing Arch package"

fresh; rm "$work/c/THIRD-PARTY-NOTICES.md"; resum
expect_fail "missing THIRD-PARTY-NOTICES.md"

fresh; rpm=$(ls "$work/c"/*.rpm)
python3 -I -c 'import os,sys; p=sys.argv[1]; f=open(p,"r+b"); f.seek(os.path.getsize(p)//2); b=f.read(1); f.seek(-1,1); f.write(bytes([b[0]^0xFF]))' "$rpm"
expect_fail "corrupted .rpm (checksum mismatch)"

fresh; echo extra > "$work/c/extra.txt"; resum
expect_fail "unexpected extra file"

fresh; sed -i '1d' "$work/c/SHA256SUMS"
expect_fail "SHA256SUMS missing an entry"

fresh
expect_fail "wrong version argument" "${ver%.*}.999"

fresh
expect_fail "wrong commit" "$ver" "0000000000000000000000000000000000000000"

fresh; sed 's/^NODE_VERSION=.*/NODE_VERSION=0.0.1/' "$tc" > "$work/tc.env"
expect_fail "toolchain differs from the pins (R6)" "$ver" "$commit" "$work/tc.env"

fresh; cp "$root/docs/compliance/sbom/homelab-control.spdx.json" "$work/c/homelab-control-$ver.spdx.json"; resum
expect_fail "SBOM not stamped with the version (R4)"

fresh; python3 -I - "$work/c/homelab-control-$ver.spdx.json" <<'PY'
import json, sys
p = sys.argv[1]; d = json.load(open(p)); d["spdxVersion"] = "SPDX-9.9"; d["packages"][0].pop("SPDXID")
open(p, "w").write(json.dumps(d))
PY
resum
expect_fail "invalid SBOM"

fresh; mkdir -p "$work/x"; tar -xzf "$work/c/homelab-control-$ver-source.tar.gz" -C "$work/x"
mkdir -p "$work/x/homelab-control-$ver/compat/webmin" && echo 'print "x";' > "$work/x/homelab-control-$ver/compat/webmin/miniserv.pl"
tar -C "$work/x" -czf "$work/c/homelab-control-$ver-source.tar.gz" "homelab-control-$ver"; rm -rf "$work/x"; resum
expect_fail "Webmin file in the source archive"

fresh; mkdir -p "$work/x"; tar -xzf "$work/c/homelab-control-$ver-source.tar.gz" -C "$work/x"
rm -rf "$work/x/homelab-control-$ver/vendor-sources"
tar -C "$work/x" -czf "$work/c/homelab-control-$ver-source.tar.gz" "homelab-control-$ver"; rm -rf "$work/x"; resum
expect_fail "source archive without the npm sources (incomplete corresponding source)"

fresh; sed -i 's/^commit=.*/commit=0000000000000000000000000000000000000000/' "$work/c/RELEASE-BUILDINFO"; resum
expect_fail "RELEASE-BUILDINFO commit differs (and differs from the archive copy)"

fresh; mkdir -p "$work/x"; tar -xzf "$work/c/homelab-control-$ver-linux-amd64.tar.gz" -C "$work/x"
echo '#!/bin/sh' > "$work/x/homelab-control-$ver-linux-amd64/webmin-helper.pl"
tar -C "$work/x" -czf "$work/c/homelab-control-$ver-linux-amd64.tar.gz" "homelab-control-$ver-linux-amd64"; rm -rf "$work/x"; resum
expect_fail "extra file in the binaries tarball"

fresh; rpm=$(ls "$work/c"/*.rpm); cp "$work/c"/homelab-control_*_amd64.deb "$rpm"; resum
expect_fail "RPM replaced by another file (package inventory)"

echo "negative tests: $n mutations, $([ "$fail" = 0 ] && echo 'all detected' || echo 'SOME NOT DETECTED')"
exit "$fail"
