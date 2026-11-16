#!/usr/bin/env python3

# SPDX-FileCopyrightText: 2026 The Homelab Control Authors
#
# SPDX-License-Identifier: GPL-3.0-or-later

"""Rust dependency policy for backend/audit-retention (ADR 0006 amendment 4, proposed).

    scripts/check-rust-deps.py            check (security gate)
    scripts/check-rust-deps.py --update   rewrite the allow-list from Cargo.lock (review the diff!)

Fails unless:
  * every crates.io package in Cargo.lock is in rust-deps-allowlist.json at exactly that
    version and checksum, and the allow-list lists nothing the lock does not;
  * every package's licence expression is satisfiable with the approved permissive licences;
  * no package comes from anywhere but crates.io (no git or path dependencies);
  * when cargo is available: the crates compiled into the shipped binary (normal dependencies
    for x86_64-unknown-linux-musl, excluding proc-macros) are exactly those marked linked=true.
Stdlib only (tomllib, Python >= 3.11).
"""
import glob
import json
import os
import shutil
import subprocess
import sys
import tomllib

ROOT = subprocess.run(["git", "rev-parse", "--show-toplevel"], capture_output=True, text=True, check=True).stdout.strip()
CRATE = os.path.join(ROOT, "backend", "audit-retention")
LOCK = os.path.join(CRATE, "Cargo.lock")
ALLOW = os.path.join(CRATE, "rust-deps-allowlist.json")
TARGET = "x86_64-unknown-linux-musl"
CRATES_IO = "registry+https://github.com/rust-lang/crates.io-index"
APPROVED = {"MIT", "Apache-2.0", "Apache-2.0 WITH LLVM-exception", "BSD-2-Clause", "BSD-3-Clause", "ISC",
            "Zlib", "Unicode-3.0", "Unlicense", "0BSD", "CC0-1.0", "BSL-1.0"}


def licence_ok(expr):
    """True if the SPDX expression (OR, AND, WITH, parentheses; legacy "/" = OR) is
    satisfiable using only approved licences."""
    toks = expr.replace("/", " OR ").replace("(", " ( ").replace(")", " ) ").split()
    pos = 0

    def peek():
        return toks[pos] if pos < len(toks) else None

    def take():
        nonlocal pos
        pos += 1
        return toks[pos - 1]

    def atom():
        t = take() if peek() else None
        if t == "(":
            v = disj()
            if take() != ")":
                raise ValueError("unbalanced")
            return v
        if t is None or t in ("AND", "OR", "WITH", ")"):
            raise ValueError("unexpected token")
        if peek() == "WITH":
            take()
            t = f"{t} WITH {take()}"
        return t in APPROVED

    def conj():
        v = atom()
        while peek() == "AND":
            take()
            v = atom() and v
        return v

    def disj():
        v = conj()
        while peek() == "OR":
            take()
            v = conj() or v
        return v

    try:
        ok = disj()
        return ok and pos == len(toks)
    except (ValueError, IndexError):
        return False


def lock_packages():
    with open(LOCK, "rb") as f:
        lock = tomllib.load(f)
    pk, bad = {}, []
    for p in lock["package"]:
        src = p.get("source")
        if src is None:
            if p["name"] != "hc-audit-retention":
                bad.append(f"{p['name']} {p['version']}: path dependency")
            continue
        if src != CRATES_IO:
            bad.append(f"{p['name']} {p['version']}: source {src} is not crates.io")
            continue
        pk[(p["name"], p["version"])] = p.get("checksum", "")
    return pk, bad


def cargo():
    return shutil.which(os.environ.get("CARGO", "cargo"))


def linked_from_cargo():
    out = subprocess.run([cargo(), "tree", "--locked", "--target", TARGET, "-e", "normal", "--prefix", "none",
                          "--format", "{p} {lib}"], cwd=CRATE, capture_output=True, text=True)
    if out.returncode:
        sys.exit("cargo tree failed:\n" + out.stderr)
    names = set()
    for line in out.stdout.splitlines():
        parts = line.replace(" (*)", "").split()
        if len(parts) >= 2 and parts[0] != "hc-audit-retention":
            names.add((parts[0], parts[1].lstrip("v")))
    return names


def crate_src(name, version):
    home = os.environ.get("CARGO_HOME") or os.path.join(os.path.expanduser("~"), ".cargo")
    for d in glob.glob(os.path.join(home, "registry", "src", "index.crates.io-*", f"{name}-{version}")):
        return d
    vendored = os.path.join(ROOT, "vendor-sources", "cargo", f"{name}-{version}")
    return vendored if os.path.isdir(vendored) else None


def manifest(name, version):
    d = crate_src(name, version)
    if not d:
        sys.exit(f"{name} {version}: crate source not found (run `cargo fetch --locked` in backend/audit-retention)")
    with open(os.path.join(d, "Cargo.toml"), "rb") as f:
        return tomllib.load(f)


def update():
    pk, bad = lock_packages()
    if bad:
        sys.exit("\n".join(bad))
    linked = linked_from_cargo()
    # Reviewer-maintained fields survive an update.
    prev = {}
    if os.path.exists(ALLOW):
        prev = {(c["name"], c["version"]): c for c in json.load(open(ALLOW, encoding="utf-8"))["crates"]}
    crates = []
    for (name, version), checksum in sorted(pk.items()):
        m = manifest(name, version)
        pkg = m.get("package", {})
        proc = bool(m.get("lib", {}).get("proc-macro"))
        kind = "proc-macro" if proc else ("normal" if (name, version) in linked else "not-linked")
        entry = {"name": name, "version": version, "checksum": checksum, "license": pkg.get("license", ""),
                 "linked": kind == "normal", "kind": kind}
        for keep in ("elected", "note"):
            if keep in prev.get((name, version), {}):
                entry[keep] = prev[(name, version)][keep]
        crates.append(entry)
    doc = {"comment": "Reviewed crates for backend/audit-retention (ADR 0006 amendment 4, proposed). "
                      "scripts/check-rust-deps.py fails if Cargo.lock differs from this list (name, version, checksum), "
                      "a licence is not approved, or the crates linked into hc-audit-retention differ from linked=true. "
                      "Regenerate with --update only as part of a reviewed dependency change.",
           "target": TARGET, "crates": crates}
    with open(ALLOW, "w", encoding="utf-8") as f:
        json.dump(doc, f, indent=2)
        f.write("\n")
    print(f"wrote {os.path.relpath(ALLOW, ROOT)}: {len(crates)} crates, {sum(c['linked'] for c in crates)} linked")


def check():
    pk, problems = lock_packages()
    allow = json.load(open(ALLOW, encoding="utf-8"))["crates"]
    want = {(c["name"], c["version"]): c for c in allow}
    for k in sorted(set(pk) - set(want)):
        problems.append(f"Cargo.lock has {k[0]} {k[1]}, which is not in the reviewed allow-list")
    for k in sorted(set(want) - set(pk)):
        problems.append(f"allow-list entry {k[0]} {k[1]} is not in Cargo.lock (stale review?)")
    for k, c in sorted(want.items()):
        if k in pk and pk[k] != c["checksum"]:
            problems.append(f"{k[0]} {k[1]}: Cargo.lock checksum differs from the reviewed one")
        if c.get("elected") and not (licence_ok(c["elected"]) and c["elected"] in c["license"].replace("/", " OR ").split(" OR ")):
            problems.append(f"{k[0]} {k[1]}: elected licence {c['elected']!r} is not one of {c['license']!r}")
        if not licence_ok(c["license"]):
            problems.append(f"{k[0]} {k[1]}: licence {c['license']!r} is not approved")
        if c["linked"] != (c["kind"] == "normal"):
            problems.append(f"{k[0]} {k[1]}: linked/kind inconsistent")
    note = "linked set not re-derived (cargo not installed)"
    if cargo():
        got = {k for k in linked_from_cargo() if want.get(k, {}).get("kind") != "proc-macro"}
        rev = {k for k, c in want.items() if c["linked"]}
        for k in sorted(got - rev):
            problems.append(f"{k[0]} {k[1]} is compiled into hc-audit-retention but not reviewed as linked")
        for k in sorted(rev - got):
            problems.append(f"{k[0]} {k[1]} is marked linked but not compiled in")
        note = f"{len(got)} linked into hc-audit-retention"
    if problems:
        print("\n".join(problems))
        sys.exit(1)
    print(f"OK: {len(allow)} reviewed crates; {note}")


if __name__ == "__main__":
    update() if "--update" in sys.argv else check()
