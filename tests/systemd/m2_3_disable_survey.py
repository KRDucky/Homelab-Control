#!/usr/bin/env python3
# SPDX-FileCopyrightText: 2026 The Homelab Control Authors
#
# SPDX-License-Identifier: GPL-3.0-or-later

"""M2-3 disable survey (measurement, PLAN ONLY): how the disable review treats
the services a real host has enabled (B-1 boot-pull check over-refusal, change
gate, timing). It never applies a change to a host unit: every vendor unit is
only planned. One disposable fixture that wants network-online.target is
enabled and then disabled again by the harness (systemctl, with its reload).

Disposable hosts only (/etc/hc-disposable). Prints one line per unit and a
tally; exits non-zero only if the survey itself could not run.
"""

import os
import subprocess
import sys
import time

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import m2_3_enablement as m  # noqa: E402

FIX = "hc-test-en-netonline.service"


def category(st, msg):
    if st == 200:
        return "allowed"
    if "started at boot only through it" in msg:
        return "refused: boot pull (B-1)"
    if "changed on disk after systemd last loaded" in msg or "runtime unit configuration" in msg:
        return "refused: unloaded change gate"
    if "is protected" in msg or "protected:" in msg:
        return "refused: protected"
    return f"refused: other ({msg[:80]})"


def main():
    if not os.path.exists("/etc/hc-disposable"):
        print("SKIP  disable survey (disposable hosts only)")
        return 0
    m.SID = m.login()
    st, sess = m.call("GET", "/api/v1/auth/session")
    if "services.configure" not in (sess.get("scopes") or []):
        grant = ",".join(sorted(set(sess.get("scopes") or []) | {"services.read", "services.configure", "audit.read"}))
        subprocess.run(["hlctl", "user", "scopes", "--file", m.USERS, "--username", "admin", "--scopes", grant], check=True, capture_output=True)
        subprocess.run(["chown", "homelab-control:homelab-control", m.USERS], check=True)
        m.restart_api()
        m.SID = m.login()
        st, sess = m.call("GET", "/api/v1/auth/session")
    csrf = sess.get("csrf_token")
    out = m.sh("systemctl", "list-unit-files", "--type=service", "--state=enabled", "--no-legend", "--no-pager")
    units = sorted({l.split()[0] for l in out.splitlines() if l.strip() and "@" not in l.split()[0]})
    tally = {}
    for u in units:
        t0 = time.monotonic()
        st, e = m.eplan(u, "disable", csrf)
        ms = int((time.monotonic() - t0) * 1000)
        c = category(st, e.get("message", ""))
        tally[c] = tally.get(c, 0) + 1
        print(f"SURVEY  {u}: {st} {c} ({ms} ms) {e.get('message', '')[:200]}")
    # A fixture that wants network-online.target (common for web services).
    path = f"{m.ETC}/{FIX}"
    if os.path.lexists(path):
        raise RuntimeError(f"{path} exists; refusing")
    with open(path, "w") as f:
        f.write("[Unit]\nDescription=M2-3 survey fixture\nWants=network-online.target\nAfter=network-online.target\n" + m._SLEEP +
                "[Install]\nWantedBy=multi-user.target\n")
    try:
        m.harness_reload()
        m.stepup(csrf)
        st, p = m.eplan(FIX, "enable", csrf)
        st, o = m.eapply(FIX, "enable", p.get("token"), csrf)
        print(f"SURVEY  {FIX} enable: {st} {o.get('outcome')}")
        t0 = time.monotonic()
        st, e = m.eplan(FIX, "disable", csrf)
        ms = int((time.monotonic() - t0) * 1000)
        print(f"SURVEY  {FIX} (Wants=network-online.target): {st} {category(st, e.get('message', ''))} ({ms} ms) {e.get('message', '')[:240]}")
        print(f"SURVEY  network-online.target wants: {m.prop('network-online.target', 'Wants')}")
        # The over-refusal case of a real host: a wait-online unit (protected by
        # name) wanted by network-online.target, and the fixture its only puller.
        for wait in ("systemd-networkd-wait-online.service", "NetworkManager-wait-online.service"):
            if m.prop(wait, "LoadState") != "loaded":
                continue
            was = m.is_enabled(wait)
            if was not in ("enabled", "disabled"):
                print(f"SURVEY  {wait}: is-enabled {was}; not changed")
                continue
            if was == "disabled":
                subprocess.run(["systemctl", "enable", "--", wait], check=True, capture_output=True)  # harness, with reload
            try:
                t0 = time.monotonic()
                st, e = m.eplan(FIX, "disable", csrf)
                ms = int((time.monotonic() - t0) * 1000)
                print(f"SURVEY  {FIX} with {wait} enabled (network-online.target wants: {m.prop('network-online.target', 'Wants')}): "
                      f"{st} {category(st, e.get('message', ''))} ({ms} ms) {e.get('message', '')[:240]}")
            finally:
                if was == "disabled":
                    subprocess.run(["systemctl", "disable", "--", wait], capture_output=True)
                print(f"SURVEY  {wait} restored: is-enabled {m.is_enabled(wait)} (was {was})")
    finally:
        subprocess.run(["systemctl", "disable", "--", FIX], capture_output=True)
        if os.path.isfile(path) and not os.path.islink(path):
            os.remove(path)
        subprocess.run(["systemctl", "daemon-reload"], capture_output=True)
    print("SURVEY  tally: " + "; ".join(f"{k}: {v}" for k, v in sorted(tally.items())) + f" (of {len(units)} enabled services)")
    return 0


if __name__ == "__main__":
    sys.exit(main())
