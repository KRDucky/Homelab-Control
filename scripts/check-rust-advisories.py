#!/usr/bin/env python3

# SPDX-FileCopyrightText: 2026 The Homelab Control Authors
#
# SPDX-License-Identifier: GPL-3.0-or-later

"""Check backend/audit-retention/Cargo.lock against the RustSec advisory database.

    scripts/check-rust-advisories.py --db PATH   (a checkout of https://github.com/rustsec/advisory-db)

Fails on any non-withdrawn vulnerability advisory that affects a locked version (a version not
matched by the advisory's `patched` or `unaffected` requirements). Informational advisories
(unmaintained, unsound, notice) are printed as warnings. Same data and rules as `cargo audit`,
without building it; stdlib only (tomllib, Python >= 3.11).
"""
import datetime
import glob
import json
import os
import re
import subprocess
import sys
import tomllib

ROOT = subprocess.run(["git", "rev-parse", "--show-toplevel"], capture_output=True, text=True, check=True).stdout.strip()
LOCK = os.path.join(ROOT, "backend", "audit-retention", "Cargo.lock")
EXCEPTIONS = os.path.join(ROOT, "backend", "audit-retention", "rust-advisory-exceptions.json")


def parse_version(v):
    core = v.split("+", 1)[0].split("-", 1)[0]
    parts = [int(x) for x in core.split(".")]
    return tuple((parts + [0, 0, 0])[:3])


def matches_one(op, ver, v):
    """One comparator of a Cargo version requirement."""
    nums = [int(x) for x in ver.split("-", 1)[0].split(".") if x != "*"]
    full = tuple((nums + [0, 0, 0])[:3])
    if op in ("", "^"):
        if not nums:
            return True
        major, minor = full[0], full[1]
        upper = (major + 1, 0, 0) if major > 0 or len(nums) == 1 else ((0, minor + 1, 0) if minor > 0 or len(nums) == 2 else (0, 0, full[2] + 1))
        return full <= v < upper
    if op == "~":
        upper = (full[0] + 1, 0, 0) if len(nums) == 1 else (full[0], full[1] + 1, 0)
        return full <= v < upper
    if op == "=":
        return v[: len(nums)] == full[: len(nums)]
    return {">": v > full, ">=": v >= full, "<": v < full, "<=": v <= full}[op]


def matches(req, v):
    for part in req.split(","):
        m = re.fullmatch(r"\s*(>=|<=|>|<|=|\^|~)?\s*([0-9][0-9A-Za-z.*+-]*)\s*", part)
        if not m:
            raise ValueError(f"unsupported version requirement {req!r}")
        if not matches_one(m.group(1) or "", m.group(2), v):
            return False
    return True


def advisory(path):
    text = open(path, encoding="utf-8").read()
    m = re.match(r"\s*```toml\n(.*?)\n```", text, re.S)
    if not m:
        raise ValueError(f"{path}: no TOML front matter")
    a = tomllib.loads(m.group(1))
    t = re.search(r"^# (.+)$", text[m.end():], re.M)
    a.setdefault("advisory", {}).setdefault("title", t.group(1) if t else "")
    return a


def main():
    if "--db" not in sys.argv:
        sys.exit(__doc__)
    db = sys.argv[sys.argv.index("--db") + 1]
    if not os.path.isdir(os.path.join(db, "crates")):
        sys.exit(f"{db}: not a RustSec advisory-db checkout")
    with open(LOCK, "rb") as f:
        pkgs = [(p["name"], p["version"]) for p in tomllib.load(f)["package"] if p.get("source")]
    vulns, warns, n = [], [], 0
    for name, version in pkgs:
        v = parse_version(version)
        for path in sorted(glob.glob(os.path.join(db, "crates", name, "*.md"))):
            a = advisory(path)
            adv, vers = a.get("advisory", {}), a.get("versions", {})
            n += 1
            if adv.get("withdrawn"):
                continue
            ok = [r for r in vers.get("patched", []) + vers.get("unaffected", [])]
            if any(matches(r, v) for r in ok):
                continue
            line = f"{adv.get('id')} {name} {version}: {adv.get('title', '').strip()}"
            (warns if adv.get("informational") else vulns).append(line)
    today = datetime.date.today().isoformat()
    exc = json.load(open(EXCEPTIONS, encoding="utf-8"))["exceptions"] if os.path.exists(EXCEPTIONS) else []
    accepted, pending = [], []
    for x in list(vulns):
        adv_id, name, version = x.split(":")[0].split()
        e = next((e for e in exc if (e["id"], e["crate"], e["version"]) == (adv_id, name, version)), None)
        if e and e.get("approved_by") and e["expires"] >= today:
            vulns.remove(x)
            accepted.append(f"{x} (accepted by {e['approved_by']} until {e['expires']}: {e['mitigation']})")
        elif e:
            pending.append(f"{adv_id}: exception proposed but {'not approved' if not e.get('approved_by') else 'expired ' + e['expires']}")
    for a in accepted:
        print("accepted exception: " + a)
    for p in pending:
        print("note: " + p)
    for w in warns:
        print("warning (informational): " + w)
    for x in vulns:
        print("VULNERABLE: " + x)
    if vulns:
        sys.exit(1)
    rev = subprocess.run(["git", "-C", db, "rev-parse", "--short", "HEAD"], capture_output=True, text=True).stdout.strip()
    print(f"OK: {len(pkgs)} locked crates, {n} advisories checked (advisory-db {rev or 'unknown revision'}), no vulnerabilities")


if __name__ == "__main__":
    main()
