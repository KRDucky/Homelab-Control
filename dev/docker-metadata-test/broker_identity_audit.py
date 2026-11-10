#!/usr/bin/env python3
# SPDX-FileCopyrightText: 2026 The Homelab Control Authors
# SPDX-License-Identifier: GPL-3.0-or-later
"""Read-only identity audit of a running metadata-broker process.

Reports the process's UIDs/GIDs, supplementary groups (with host names),
effective capabilities, no_new_privs and seccomp mode, and flags groups that
grant authority beyond a metadata broker's purpose (container daemons,
hypervisors, disks, sudo, logs). It reads only /proc/<pid>/status and the
group database; it never reads the process environment, command line,
memory or any credential, and changes nothing.

Exit status: 0 = production-ready identity, 1 = findings (for example
privileged supplementary groups), 2 = usage or read error.

Usage: broker_identity_audit.py (PID | --pid PID) [--proc-root /proc] [--group-file /etc/group] [--json]
"""
import json
import os
import sys

# Same names as CORE's containeridentity.PrivilegedGroupNames. GIDs are never assumed.
PRIVILEGED_GROUPS = {"root", "wheel", "sudo", "admin", "adm", "docker", "podman", "lxd", "incus",
                     "incus-admin", "libvirt", "kvm", "disk", "shadow", "systemd-journal"}


def parse_groups(path):
    names = {}
    with open(path, encoding="utf-8", errors="replace") as f:
        for line in f:
            parts = line.rstrip("\n").split(":")
            if len(parts) >= 3 and parts[2].isdigit():
                names.setdefault(int(parts[2]), parts[0])
    return names


def parse_status(path):
    fields = {}
    with open(path, encoding="utf-8", errors="replace") as f:
        for line in f:
            key, _, value = line.partition(":")
            if key in ("Uid", "Gid", "Groups", "CapEff", "CapPrm", "CapBnd", "NoNewPrivs", "Seccomp"):
                fields[key] = value.strip()
    return fields


def audit(pid, proc_root="/proc", group_file="/etc/group"):
    if not str(pid).isdigit():
        raise ValueError("PID must be numeric")
    status = parse_status(os.path.join(proc_root, str(pid), "status"))
    names = parse_groups(group_file)
    uids = [int(x) for x in status.get("Uid", "").split()]
    gids = [int(x) for x in status.get("Gid", "").split()]
    groups = sorted({int(x) for x in status.get("Groups", "").split()})
    cap_eff = int(status.get("CapEff", "0"), 16)
    findings = []
    if uids and (0 in uids):
        findings.append({"code": "root_uid", "message": "The broker runs with a root UID."})
    privileged = [{"gid": g, "name": names.get(g, "")} for g in groups
                  if g == 0 or names.get(g, "") in PRIVILEGED_GROUPS]
    for p in privileged:
        findings.append({"code": "privileged_group", "gid": p["gid"], "name": p["name"],
                         "message": "Supplementary group %d (%s) grants authority beyond metadata access; "
                                    "a compromised broker inherits it." % (p["gid"], p["name"] or "unnamed")})
    if cap_eff:
        findings.append({"code": "effective_capabilities", "message": "Effective capabilities are non-empty (0x%x)." % cap_eff})
    if status.get("NoNewPrivs") != "1":
        findings.append({"code": "new_privileges_allowed", "message": "no_new_privs is not set."})
    if status.get("Seccomp") not in ("2",):
        findings.append({"code": "no_seccomp_filter", "message": "No seccomp filter is active."})
    return {
        "pid": int(pid),
        "uids": uids,
        "gids": gids,
        "supplementaryGroups": [{"gid": g, "name": names.get(g, "")} for g in groups],
        "capEff": "0x%x" % cap_eff,
        "noNewPrivs": status.get("NoNewPrivs") == "1",
        "seccomp": status.get("Seccomp", ""),
        "findings": findings,
        "productionReady": not findings,
    }


def main(argv):
    args = list(argv)
    as_json = "--json" in args
    if as_json:
        args.remove("--json")
    opts = {"--proc-root": "/proc", "--group-file": "/etc/group"}
    pid = None
    while args:
        a = args.pop(0)
        if a in opts and args:
            opts[a] = args.pop(0)
        elif a == "--pid" and args and pid is None:
            pid = args.pop(0)
        elif pid is None and not a.startswith("--"):
            pid = a
        else:
            print(__doc__, file=sys.stderr)
            return 2
    if pid is None:
        print(__doc__, file=sys.stderr)
        return 2
    try:
        result = audit(pid, opts["--proc-root"], opts["--group-file"])
    except (OSError, ValueError) as e:
        print("audit failed: %s" % e.__class__.__name__, file=sys.stderr)
        return 2
    if as_json:
        print(json.dumps(result, indent=2))
    else:
        print("broker pid %d uid=%s gid=%s" % (result["pid"], result["uids"], result["gids"]))
        print("supplementary groups: " + ", ".join("%d(%s)" % (g["gid"], g["name"] or "?") for g in result["supplementaryGroups"]))
        for f in result["findings"]:
            print("FINDING %s: %s" % (f["code"], f["message"]))
        print("production-ready identity: %s" % ("yes" if result["productionReady"] else "no"))
    return 0 if result["productionReady"] else 1


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
