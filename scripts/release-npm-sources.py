#!/usr/bin/env python3

# SPDX-FileCopyrightText: 2026 The Homelab Control Authors
#
# SPDX-License-Identifier: GPL-3.0-or-later

"""Fetch the published sources of the npm packages compiled into the release binaries.

The release source archive carries these tarballs (vendor-sources/npm/) so that the
complete corresponding source of the web UI bundle embedded in hc-api is published
with every release, not only the lockfile that names it. Each tarball is checked
against the sha512 integrity recorded in frontend/app/package-lock.json.

    scripts/release-npm-sources.py <dest-dir>

Only production (non-dev) packages are fetched: those are exactly the ones in the
bundle (docs/compliance/LICENSE-AUDIT.md §2). Development tools (Vite, TypeScript, ...)
are generally available build tools, pinned by name, version and integrity in the
lockfile. Stdlib only; needs npm and registry access.
"""
import base64
import hashlib
import json
import os
import subprocess
import sys
import tempfile

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
LOCK = os.path.join(ROOT, "frontend", "app", "package-lock.json")


def main():
    if len(sys.argv) != 2:
        sys.exit("usage: release-npm-sources.py <dest-dir>")
    dest = os.path.abspath(sys.argv[1])
    os.makedirs(dest, exist_ok=True)
    lock = json.load(open(LOCK, encoding="utf-8"))
    manifest = ["# npm sources of the packages compiled into the Homelab Control web UI bundle",
                "# name version file sha512-integrity (verified against frontend/app/package-lock.json)"]
    for key, meta in sorted(lock["packages"].items()):
        if not key or meta.get("dev"):
            continue
        name, version, integrity = key.split("node_modules/")[-1], meta["version"], meta["integrity"]
        if not integrity.startswith("sha512-"):
            sys.exit(f"{name}: lockfile integrity is not sha512")
        with tempfile.TemporaryDirectory() as tmp:  # neutral cwd: no project .npmrc or scripts
            res = subprocess.run(["npm", "pack", f"{name}@{version}", "--pack-destination", dest,
                                  "--ignore-scripts", "--json", "--silent"],
                                 cwd=tmp, capture_output=True, text=True, check=True)
        filename = json.loads(res.stdout)[0]["filename"]
        data = open(os.path.join(dest, filename), "rb").read()
        got = "sha512-" + base64.b64encode(hashlib.sha512(data).digest()).decode()
        if got != integrity:
            sys.exit(f"{name}@{version}: integrity mismatch ({got} != {integrity})")
        manifest.append(f"{name} {version} {filename} {integrity}")
        print(f"OK  {name}@{version} -> {filename}")
    with open(os.path.join(dest, "MANIFEST.txt"), "w", encoding="utf-8") as f:
        f.write("\n".join(manifest) + "\n")


if __name__ == "__main__":
    main()
