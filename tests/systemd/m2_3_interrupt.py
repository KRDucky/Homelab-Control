#!/usr/bin/env python3
# SPDX-FileCopyrightText: 2026 The Homelab Control Authors
#
# SPDX-License-Identifier: GPL-3.0-or-later

"""M2-3 real broker interruption during a boot-time change.

DISPOSABLE CONTAINERS/VMs ONLY (refuses without /etc/hc-disposable and
HC_M2_INTERRUPT=1). It temporarily replaces /usr/bin/systemctl with a
root-owned wrapper that delays ONLY `enable`/`disable` of the fixture
hc-test-int.service, either before or after running the real systemctl, and
SIGKILLs the real broker during the delay. Every other systemctl call passes
straight through. The original binary is restored afterwards (and checked).

Asserts, for both variants:
  - the client gets an error, not an outcome;
  - the broker is restarted by systemd (Restart=on-failure) and records
    exactly one intent and one "interrupted" record for the operation, with
    the observed state ("unchanged" when killed before systemctl ran,
    "changed, not verified" when killed after), and no outcome;
  - nothing is retried (the unchanged fixture stays disabled);
  - the operation journal hash chain verifies.
"""

import json
import os
import shutil
import signal
import subprocess
import sys
import time

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import m2_3_enablement as m  # noqa: E402  (shares helpers; its main() does not run)

UNIT = "hc-test-int.service"
REAL = "/usr/bin/systemctl"
MOVED = "/usr/bin/systemctl.hc-test-real"
MODE = "/run/hc-test-interrupt-mode"
OPS = "/var/log/homelab-control-broker/operations.jsonl"
SLEEP = "47.25"  # unique, so the delay is found by pgrep

WRAPPER = f"""#!/bin/sh
# Test wrapper (tests/systemd/m2_3_interrupt.py, disposable hosts only).
case " $* " in
  *" enable -- {UNIT} "*|*" disable -- {UNIT} "*) ;;
  *) exec {MOVED} "$@" ;;
esac
mode=$(cat {MODE} 2>/dev/null)
[ "$mode" = before ] && sleep {SLEEP}
{MOVED} "$@"; rc=$?
[ "$mode" = after ] && sleep {SLEEP}
exit $rc
"""


def ops_records():
    out = []
    with open(OPS) as f:
        for line in f:
            try:
                ev = json.loads(line)
            except ValueError:
                continue
            if ev.get("event", "").startswith("op.") and UNIT in line:
                out.append(ev)
    return out


def broker_pid():
    return m.prop("homelab-control-broker.service", "MainPID")


def install_wrapper():
    if os.path.lexists(MOVED):
        raise RuntimeError(f"{MOVED} exists; refusing")
    st = os.stat(REAL)
    shutil.move(REAL, MOVED)
    with open(REAL, "w") as f:
        f.write(WRAPPER)
    os.chown(REAL, 0, 0)
    os.chmod(REAL, 0o755)
    return st


def restore_wrapper(orig):
    if os.path.exists(MOVED):
        os.replace(MOVED, REAL)
    st = os.stat(REAL)
    m.check("systemctl binary restored (size, mode, owner)", (orig.st_size, orig.st_mode, orig.st_uid),
            (st.st_size, st.st_mode, st.st_uid))


def run_variant(mode, csrf, before_state, action):
    with open(MODE, "w") as f:
        f.write(mode)
    m.stepup(csrf)
    st, p = m.eplan(UNIT, action, csrf)
    m.check(f"[{mode}] plan {action}: 200", 200, st)
    n_before = len(ops_records())
    pid_before = broker_pid()
    result = {}

    import threading

    def apply():
        result["r"] = m.eapply(UNIT, action, p.get("token"), csrf)

    t = threading.Thread(target=apply)
    t.start()
    # Wait until the wrapper is in its delay, then SIGKILL the broker.
    waited = False
    for _ in range(200):
        if subprocess.run(["pgrep", "-fx", f"sleep {SLEEP}"], capture_output=True).returncode == 0:
            waited = True
            break
        time.sleep(0.05)
    m.check(f"[{mode}] wrapper reached its delay", True, waited)
    os.kill(int(pid_before), signal.SIGKILL)
    t.join(60)
    st, body = result.get("r", (0, {}))
    m.check(f"[{mode}] client gets an error, not an outcome", (True, None), (st >= 500 or st == 0, body.get("outcome")))
    # systemd restarts the broker (Restart=on-failure); recovery runs at start.
    restarted = False
    for _ in range(100):
        pid = broker_pid()
        if pid not in ("", "0", pid_before):
            restarted = True
            break
        time.sleep(0.1)
    if not restarted:
        m.call("GET", "/api/v1/services")  # socket activation as a fallback
    recs = []
    for _ in range(100):
        recs = ops_records()[n_before:]
        if any(r.get("event") == "op.interrupted" for r in recs):
            break
        time.sleep(0.1)
    kinds = [r.get("event") for r in recs]
    m.check(f"[{mode}] journal: one intent, one interrupted, no outcome", ["op.intent", "op.interrupted"], kinds)
    text = json.dumps(recs[-1]) if recs else ""
    verdict = "unchanged" if mode == "before" else "changed, not verified"
    m.check(f"[{mode}] interrupted record says '{verdict}'", True, verdict in text and "not retried" in text)
    print(f"INFO  [{mode}] interrupted record: {text[:600]}")
    time.sleep(3)
    want = before_state if mode == "before" else {"enable": "enabled", "disable": "disabled"}[action]
    m.check(f"[{mode}] host is-enabled {want} (nothing retried)", want, m.is_enabled(UNIT))
    m.check(f"[{mode}] no wrapper delay left running", 1,
            subprocess.run(["pgrep", "-fx", f"sleep {SLEEP}"], capture_output=True).returncode)


def main():
    if not os.path.exists("/etc/hc-disposable") or os.environ.get("HC_M2_INTERRUPT") != "1":
        print("SKIP  M2-3 broker interruption (disposable hosts only; set HC_M2_INTERRUPT=1)")
        return 0
    path = f"{m.ETC}/{UNIT}"
    if os.path.lexists(path):
        raise RuntimeError(f"{path} exists; refusing")
    with open(path, "w") as f:
        f.write("[Unit]\nDescription=M2-3 disposable interruption fixture\n" + m._SLEEP + "[Install]\nWantedBy=multi-user.target\n")
    before_links = m.links()
    subprocess.run([REAL, "daemon-reload"], check=True)
    orig = install_wrapper()
    try:
        m.SID = m.login()
        st, sess = m.call("GET", "/api/v1/auth/session")
        csrf = sess.get("csrf_token")
        m.check("session has services.configure", True, "services.configure" in (sess.get("scopes") or []))
        run_variant("before", csrf, "disabled", "enable")
        run_variant("after", csrf, "disabled", "enable")
        # After recovery the unit is enabled and consistent: disabling it is
        # reviewed normally and succeeds (no wrapper delay).
        with open(MODE, "w") as f:
            f.write("none")
        m.stepup(csrf)
        st, p = m.eplan(UNIT, "disable", csrf)
        st, o = m.eapply(UNIT, "disable", p.get("token"), csrf)
        m.check("after recovery: disable reviewed and succeeded", (200, "succeeded"), (st, o.get("outcome")))
    finally:
        restore_wrapper(orig)
        if os.path.exists(MODE):
            os.remove(MODE)
        subprocess.run([REAL, "--no-reload", "disable", "--", UNIT], capture_output=True)
        if os.path.isfile(path) and not os.path.islink(path):
            os.remove(path)
        subprocess.run([REAL, "daemon-reload"], capture_output=True)
    v = subprocess.run(["hlctl", "audit", "verify", "--file", OPS], capture_output=True, text=True)
    m.check("operation journal hash chain verifies", 0, v.returncode)
    m.check("links under /etc/systemd/system as before", [], sorted(before_links ^ m.links()))
    print(f"m2-3-interrupt: {m.passed} passed, {m.failed} failed")
    return 1 if m.failed else 0


if __name__ == "__main__":
    sys.exit(main())
