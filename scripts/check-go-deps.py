#!/usr/bin/env python3

# SPDX-FileCopyrightText: 2026 The Homelab Control Authors
#
# SPDX-License-Identifier: GPL-3.0-or-later

"""Go dependency policy (ADR 0006 amendment 1).

Fails unless:
  * every module required by backend/go.mod is in backend/go-deps-allowlist.json at
    exactly that version, and the allow-list has nothing go.mod does not require;
  * each module's go.sum h1 hash equals the reviewed one;
  * the modules linked into hc-api, hlctl and hc-broker (go list -deps, linux/amd64,
    CGO disabled) are exactly the allow-list entries marked linked=true;
  * hc-broker links no third-party module at all (the root process stays stdlib-only).
"""
import json
import os
import re
import subprocess
import sys

ROOT = subprocess.run(["git", "rev-parse", "--show-toplevel"], capture_output=True, text=True, check=True).stdout.strip()
BACKEND = os.path.join(ROOT, "backend")


def linked(pkgs):
    env = dict(os.environ, GOOS="linux", GOARCH="amd64", CGO_ENABLED="0", GOFLAGS="-mod=readonly")
    out = subprocess.run(["go", "list", "-deps", "-f", "{{with .Module}}{{if not .Main}}{{.Path}} {{.Version}}{{end}}{{end}}", *pkgs],
                         cwd=BACKEND, capture_output=True, text=True, env=env)
    if out.returncode:
        sys.exit("go list failed:\n" + out.stderr)
    return {tuple(l.split()) for l in out.stdout.splitlines() if l.strip()}


def main():
    allow = json.load(open(os.path.join(BACKEND, "go-deps-allowlist.json"), encoding="utf-8"))["modules"]
    want = {(m["path"], m["version"]) for m in allow}
    gomod = open(os.path.join(BACKEND, "go.mod"), encoding="utf-8").read()
    req = set(re.findall(r"^\s*(?:require\s+)?([a-z0-9.\-]+\.[a-z]{2,}/\S+)\s+(v\S+)", gomod, re.M))
    gosum = open(os.path.join(BACKEND, "go.sum"), encoding="utf-8").read()
    problems = []
    for p, v in sorted(req - want):
        problems.append(f"go.mod requires {p} {v}, which is not in the reviewed allow-list")
    for p, v in sorted(want - req):
        problems.append(f"allow-list entry {p} {v} is not required by go.mod (stale review?)")
    for m in allow:
        got = re.search(r"^" + re.escape(m["path"]) + " " + re.escape(m["version"]) + r" (h1:\S+)$", gosum, re.M)
        if not got or got.group(1) != m["h1"]:
            problems.append(f"{m['path']} {m['version']}: go.sum hash differs from the reviewed {m['h1']}")
    link = linked(["./cmd/..."])
    want_link = {(m["path"], m["version"]) for m in allow if m["linked"]}
    for p, v in sorted(link - want_link):
        problems.append(f"{p} {v} is linked into a binary but not reviewed as linked")
    for p, v in sorted(want_link - link):
        problems.append(f"{p} {v} is marked linked but no binary links it")
    broker = linked(["./cmd/hc-broker"])
    if broker:
        problems.append("hc-broker (root) links third-party modules: " + ", ".join(sorted(p for p, _ in broker)))
    if problems:
        print("\n".join(problems))
        sys.exit(1)
    print(f"OK: {len(allow)} reviewed modules, {len(link)} linked into hc-api/hlctl; hc-broker links the standard library only")


if __name__ == "__main__":
    main()
