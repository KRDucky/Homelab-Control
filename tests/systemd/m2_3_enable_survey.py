#!/usr/bin/env python3
# SPDX-FileCopyrightText: 2026 The Homelab Control Authors
#
# SPDX-License-Identifier: GPL-3.0-or-later

"""M2-3 enable survey (measurement, PLAN ONLY): how the enable review treats
the services a real host ships disabled, and three typical homelab fixtures
(wanting network-online.target, wanting a running service, Type=dbus). Used
to compare the boot mode (P-9) with the earlier start-walk model. Nothing is
ever applied; the fixtures are written to /etc/systemd/system, loaded with
daemon-reload by the harness, planned, and removed again. Disposable hosts
only (/etc/hc-disposable). Prints one line per unit and a tally.
"""

import os
import subprocess
import sys
import time

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import m2_3_enablement as m  # noqa: E402

FIXTURES = {
    "hc-test-sv-netonline.service": "Wants=network-online.target\nAfter=network-online.target\n",
    "hc-test-sv-running.service": "Wants=systemd-journald.service cron.service crond.service systemd-timesyncd.service\n",
    "hc-test-sv-dbus.service": "Requires=dbus.socket\nAfter=dbus.socket\n",
}


def category(st, msg):
    if st == 200:
        return "allowed"
    for k, c in (("changed on disk after systemd last loaded", "gate"), ("is protected", "protected"), ("protected:", "protected"),
                 ("only until the next boot", "runtime"), ("hidden", "runtime"), ("would stop", "conflict"),
                 ("reboot or power off", "emergency action"), ("shutdown, reboot or recovery", "dangerous target"),
                 ("cannot be inspected", "uninspectable"), ("related units to check", "budget"), ("nothing to enable", "no [Install]")):
        if k in msg:
            return "refused: " + c
    return f"refused: other ({msg[:80]})"


def main():
    if not os.path.exists("/etc/hc-disposable"):
        print("SKIP  enable survey (disposable hosts only)")
        return 0
    m.SID = m.login()
    st, sess = m.call("GET", "/api/v1/auth/session")
    if "services.configure" not in (sess.get("scopes") or []):
        grant = ",".join(sorted(set(sess.get("scopes") or []) | {"services.read", "services.configure"}))
        subprocess.run(["hlctl", "user", "scopes", "--file", m.USERS, "--username", "admin", "--scopes", grant], check=True, capture_output=True)
        subprocess.run(["chown", "homelab-control:homelab-control", m.USERS], check=True)
        m.restart_api()
        m.SID = m.login()
        st, sess = m.call("GET", "/api/v1/auth/session")
    csrf = sess.get("csrf_token")
    written = []
    for name, deps in FIXTURES.items():
        path = f"{m.ETC}/{name}"
        if os.path.lexists(path):
            raise RuntimeError(f"{path} exists; refusing")
        with open(path, "w") as f:
            f.write(f"[Unit]\nDescription=M2-3 enable survey fixture\n{deps}" + m._SLEEP + "[Install]\nWantedBy=multi-user.target\n")
        written.append(path)
    try:
        m.harness_reload()
        out = m.sh("systemctl", "list-unit-files", "--type=service", "--state=disabled", "--no-legend", "--no-pager")
        units = sorted({l.split()[0] for l in out.splitlines() if l.strip() and "@" not in l.split()[0]})
        tally, worst = {}, 0
        for u in units:
            t0 = time.monotonic()
            st, e = m.eplan(u, "enable", csrf)
            ms = int((time.monotonic() - t0) * 1000)
            worst = max(worst, ms)
            c = category(st, e.get("message", ""))
            tally[c] = tally.get(c, 0) + 1
            print(f"SURVEY  {u}: {st} {c} ({ms} ms) {e.get('message', '')[:220]}")
        print(f"SURVEY  tally ({len(units)} disabled services): {dict(sorted(tally.items()))}; slowest plan {worst} ms")
    finally:
        for p in written:
            os.unlink(p)
        m.harness_reload()
    return 0


if __name__ == "__main__":
    sys.exit(main())
