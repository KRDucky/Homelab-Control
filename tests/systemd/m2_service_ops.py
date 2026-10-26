#!/usr/bin/env python3
# SPDX-FileCopyrightText: 2026 The Homelab Control Authors
#
# SPDX-License-Identifier: GPL-3.0-or-later

"""M2-2 real-systemd scenario (M2-SECURITY-ACCEPTANCE E1, roadmap §3).

Runs INSIDE a disposable systemd host (container or VM) after the package is
installed and hc-api/hc-broker are running (tests/systemd/m2-service-ops.sh).
It only ever starts, stops or restarts the disposable hc-test-* units it
creates; protected units (ssh, the broker, dbus) are only *planned* to prove
refusal and are verified to be untouched.

Environment: HC_PW (admin password), HC_SID (session cookie),
HC_M2_LONG=1 to also wait for plan-token (120 s) and step-up (5 min) expiry.
"""

import json
import os
import subprocess
import sys
import threading
import time
import urllib.error
import urllib.request

BASE = "http://127.0.0.1:9180"
ORIGIN = "https://127.0.0.1:9180"
SID = os.environ["HC_SID"]
PW = os.environ["HC_PW"]
LONG = os.environ.get("HC_M2_LONG") == "1"

passed = failed = 0


def check(name, want, got):
    global passed, failed
    if want == got:
        print(f"PASS  {name} ({got})")
        passed += 1
    else:
        print(f"FAIL  {name} (want {want!r}, got {got!r})")
        failed += 1


def sh(*args):
    return subprocess.run(args, capture_output=True, text=True).stdout.strip()


def is_active(unit):
    return sh("systemctl", "is-active", unit)


def call(method, path, body=None, csrf=None):
    data = json.dumps(body).encode() if body is not None else None
    req = urllib.request.Request(BASE + path, data=data, method=method)
    req.add_header("Cookie", f"__Host-hc_session={SID}")
    if method != "GET":
        req.add_header("Origin", ORIGIN)
        req.add_header("Content-Type", "application/json")
        if csrf:
            req.add_header("X-CSRF-Token", csrf)
    try:
        with urllib.request.urlopen(req, timeout=120) as r:
            return r.status, json.loads(r.read() or b"{}")
    except urllib.error.HTTPError as e:
        try:
            return e.code, json.loads(e.read() or b"{}")
        except ValueError:
            return e.code, {}


UNITS = {
    "hc-test-dummy.service": "[Unit]\nDescription=Homelab Control disposable test unit\n[Service]\nType=simple\nExecStart=/bin/sleep infinity\n",
    "hc-test-fail.service": "[Unit]\nDescription=Homelab Control disposable failing unit\n[Service]\nType=simple\nExecStart=/bin/false\n",
    "hc-test-slow.service": "[Unit]\nDescription=Homelab Control disposable slow unit\n[Service]\nType=simple\nExecStartPre=/bin/sleep 15\nExecStart=/bin/sleep infinity\n",
}


CREATED = []  # stand-in units created by setup(), removed at the end
SSH_UNIT = "ssh.service"
_SLEEP = "[Service]\nType=simple\nExecStart=/bin/sleep infinity\n"
CHAINS = {
    "hc-test-base.service": "[Unit]\nDescription=Homelab Control disposable chain base\n" + _SLEEP,
    "hc-test-mid.service": "[Unit]\nDescription=Homelab Control disposable chain middle\n"
                           "Requires=hc-test-base.service\nAfter=hc-test-base.service\n" + _SLEEP,
    "homelab-control-hctest.service": "[Unit]\nDescription=Homelab Control disposable protected-name dependent\n"
                                      "Requires=hc-test-mid.service\nAfter=hc-test-mid.service\n" + _SLEEP,
    "hc-test-front.service": "[Unit]\nDescription=Homelab Control disposable front\n"
                             "Wants=hc-test-chain.service\n" + _SLEEP,
    "hc-test-chain.service": "[Unit]\nDescription=Homelab Control disposable chain to reboot.target\n"
                             "Wants=reboot.target\n" + _SLEEP,
    "hc-test-needs.service": "[Unit]\nDescription=Homelab Control disposable unit with a dependency\n"
                             "Requires=hc-test-base.service\nAfter=hc-test-base.service\n" + _SLEEP,
}


def setup():
    for name, body in UNITS.items():
        with open(f"/etc/systemd/system/{name}", "w") as f:
            f.write(body)
    # An alias that resolves to the distribution's SSH unit (ssh.service on
    # Debian/Ubuntu, sshd.service on Fedora/Arch/openSUSE): the broker must see
    # through it. Minimal images have no OpenSSH; then a disposable stand-in
    # named ssh.service is created (this harness only runs on hosts marked
    # with /etc/hc-disposable), so the alias check always runs.
    global SSH_UNIT
    for cand in ("ssh.service", "sshd.service"):
        if sh("systemctl", "show", "-p", "LoadState", "--value", cand) == "loaded":
            SSH_UNIT = cand
            break
    else:
        with open("/etc/systemd/system/ssh.service", "w") as f:
            f.write("[Unit]\nDescription=Homelab Control test stand-in for OpenSSH\n"
                    "[Service]\nExecStart=/bin/sleep infinity\n")
        CREATED.append("ssh.service")
        SSH_UNIT = "ssh.service"
        subprocess.run(["systemctl", "daemon-reload"], check=True)
    alias = "/etc/systemd/system/hc-test-innocent.service"
    if os.path.lexists(alias):
        os.remove(alias)
    target = sh("systemctl", "show", "-p", "FragmentPath", "--value", SSH_UNIT)
    if target:
        os.symlink(target, alias)
    # Dependency chains for the transitive policy (planned only, never run):
    #   homelab-control-hctest.service Requires= hc-test-mid.service
    #   hc-test-mid.service            Requires= hc-test-base.service
    # so stopping hc-test-base would stop a protected-name unit two hops away;
    #   hc-test-front.service Wants= hc-test-chain.service Wants= reboot.target
    # so starting hc-test-front would pull in reboot.target.
    for name, body in CHAINS.items():
        with open(f"/etc/systemd/system/{name}", "w") as f:
            f.write(body)
    subprocess.run(["systemctl", "daemon-reload"], check=True)


def login():
    """Signs in again as admin and returns the new session cookie value."""
    req = urllib.request.Request(BASE + "/api/v1/auth/login", method="POST",
                                 data=json.dumps({"username": "admin", "password": PW}).encode())
    req.add_header("Origin", ORIGIN)
    req.add_header("Content-Type", "application/json")
    with urllib.request.urlopen(req, timeout=30) as r:
        for h in r.headers.get_all("Set-Cookie") or []:
            if h.startswith("__Host-hc_session="):
                return h.split(";", 1)[0].split("=", 1)[1]
    raise RuntimeError("login returned no session cookie")


def wait_health():
    for _ in range(100):
        try:
            with urllib.request.urlopen(BASE + "/api/v1/health", timeout=2):
                return
        except OSError:
            time.sleep(0.2)


def plan(unit, action, csrf):
    return call("POST", f"/api/v1/services/units/{unit}/plan", {"action": action}, csrf)


def apply(unit, action, token, csrf):
    return call("POST", f"/api/v1/services/units/{unit}/apply", {"action": action, "token": token}, csrf)


def main():
    global SID
    setup()
    # 0. The default account has no services.operate: refused, no controls.
    st, sess = call("GET", "/api/v1/auth/session")
    scopes = sess.get("scopes") or []
    check("default account lacks services.operate", False, "services.operate" in scopes)
    st, e = plan("hc-test-dummy.service", "start", sess.get("csrf_token"))
    check("plan without services.operate: 403 forbidden", (403, "forbidden"), (st, e.get("error")))
    st, svc = call("GET", "/api/v1/services")
    check("services: operate unavailable, reason given", (False, True),
          (svc.get("operate", {}).get("available"), "services.operate" in svc.get("operate", {}).get("reason", "")))
    # Grant it explicitly (D-7: never granted by default), restart, sign in again.
    grant = ",".join(sorted(set(scopes) | {"services.operate", "audit.read"}))
    subprocess.run(["hlctl", "user", "scopes", "--file", "/etc/homelab-control/users.json",
                    "--username", "admin", "--scopes", grant], check=True, capture_output=True)
    subprocess.run(["chown", "homelab-control:homelab-control", "/etc/homelab-control/users.json"], check=True)
    subprocess.run(["systemctl", "restart", "homelab-control.service"], check=True)
    wait_health()
    st, e = plan("hc-test-dummy.service", "start", sess.get("csrf_token"))
    check("old session does not gain the new scope", 403, st)
    SID = login()
    st, sess = call("GET", "/api/v1/auth/session")
    csrf = sess.get("csrf_token")
    check("new session has services.operate", True, "services.operate" in (sess.get("scopes") or []))

    # hc-api caches broker state for 5 s; wait for it rather than assume it.
    available = False
    for _ in range(30):
        st, svc = call("GET", "/api/v1/services")
        available = svc.get("operate", {}).get("available")
        if available:
            break
        time.sleep(0.5)
    check("services: operate available", True, available)
    check(f"services: {SSH_UNIT} labelled protected", True, svc.get("protected", {}).get(SSH_UNIT, {}).get("protected"))
    check("services: test unit not protected", False, "hc-test-dummy.service" in svc.get("protected", {}))

    ssh_before = is_active(SSH_UNIT)
    broker_pid_before = sh("systemctl", "show", "-p", "MainPID", "--value", "homelab-control-broker.service")

    # 1. start (reversible, no step-up)
    u = "hc-test-dummy.service"
    st, p = plan(u, "start", csrf)
    check("plan start: 200", 200, st)
    check("plan start: reversible", "reversible", p.get("risk"))
    check("plan start: canonical unit", u, p.get("unit"))
    st, o = apply(u, "start", p.get("token"), csrf)
    check("apply start: 200", 200, st)
    check("apply start: verified succeeded", "succeeded", o.get("outcome"))
    check("apply start: observed active", "active", (o.get("observed") or {}).get("active_state"))
    check("systemd agrees: active", "active", is_active(u))
    pid1 = sh("systemctl", "show", "-p", "MainPID", "--value", u)

    # 2. stop (disruptive: step-up)
    st, p = plan(u, "stop", csrf)
    check("plan stop: disruptive, step-up required", ("disruptive", True), (p.get("risk"), p.get("step_up_required")))
    st, e = apply(u, "stop", p.get("token"), csrf)
    check("apply stop without step-up: 403 step_up_required", (403, "step_up_required"), (st, e.get("error")))
    check("unit untouched after refused stop", "active", is_active(u))
    st, _ = call("POST", "/api/v1/auth/step-up", {"password": "wrong-" + PW}, csrf)
    check("step-up with wrong password: 403", 403, st)
    st, su = call("POST", "/api/v1/auth/step-up", {"password": PW}, csrf)
    check("step-up: 200", 200, st)
    stepup_at = time.time()
    st, o = apply(u, "stop", p.get("token"), csrf)
    check("apply stop: verified succeeded", (200, "succeeded"), (st, o.get("outcome")))
    check("systemd agrees: inactive", "inactive", is_active(u))

    # 3. restart (from inactive it starts; then a real restart changes the PID)
    st, p = plan(u, "start", csrf)
    apply(u, "start", p.get("token"), csrf)
    pid_a = sh("systemctl", "show", "-p", "MainPID", "--value", u)
    st, p = plan(u, "restart", csrf)
    restart_token = p.get("token")
    st, o = apply(u, "restart", restart_token, csrf)
    pid_b = sh("systemctl", "show", "-p", "MainPID", "--value", u)
    check("apply restart: verified succeeded", (200, "succeeded"), (st, o.get("outcome")))
    check("restart: new main PID", True, pid_a != pid_b and pid_b != "0" and pid1 != "")
    check("restart: observed PID matches systemd", pid_b, str((o.get("observed") or {}).get("main_pid")))

    # 4. replay
    st, e = apply(u, "restart", restart_token, csrf)
    check("replayed token: 409 plan_invalid", (409, "plan_invalid"), (st, e.get("error")))

    # 5. target substitution: a dummy token for another unit
    st, p = plan(u, "stop", csrf)
    st, e = apply("hc-test-fail.service", "stop", p.get("token"), csrf)
    check("token used for another unit: 409 plan_invalid", (409, "plan_invalid"), (st, e.get("error")))
    st, e = apply(u, "restart", p.get("token"), csrf)
    check("token used for another action: 409 plan_invalid", (409, "plan_invalid"), (st, e.get("error")))

    # 6. protected units — never executed
    for unit, action in [("ssh.service", "stop"), ("homelab-control-broker.service", "restart"),
                         ("homelab-control.service", "stop"), ("dbus.service", "restart"),
                         ("systemd-journald.service", "stop")]:
        st, e = plan(unit, action, csrf)
        check(f"protected {unit} {action}: 403 policy_denied", (403, "policy_denied"), (st, e.get("error")))
    check("alias fixture present", True, os.path.lexists("/etc/systemd/system/hc-test-innocent.service"))
    print(f"      (SSH unit on this host: {SSH_UNIT})")
    if os.path.lexists("/etc/systemd/system/hc-test-innocent.service"):
        st, e = plan("hc-test-innocent.service", "stop", csrf)
        check("alias resolving to ssh.service: 403 policy_denied (broker)", (403, "policy_denied"), (st, e.get("error")))
        check("alias refusal names the protected unit", True, SSH_UNIT in e.get("message", ""))
    check(f"{SSH_UNIT} untouched", ssh_before, is_active(SSH_UNIT))
    check("broker not restarted by any of this", broker_pid_before,
          sh("systemctl", "show", "-p", "MainPID", "--value", "homelab-control-broker.service"))

    # 6b. transitive dependency policy (D-5: no bypass through dependencies);
    #     planned only — nothing here is executed.
    subprocess.run(["systemctl", "start", "homelab-control-hctest.service"], check=True)
    for unit, action in [("hc-test-base.service", "stop"), ("hc-test-base.service", "restart")]:
        st, e = plan(unit, action, csrf)
        check(f"{action} {unit} would stop a protected unit two hops away: 403 policy_denied",
              (403, "policy_denied", True), (st, e.get("error"), "homelab-control-hctest.service" in e.get("message", "")))
    st, e = plan("hc-test-front.service", "start", csrf)
    check("start pulling in reboot.target two hops away: 403 policy_denied",
          (403, "policy_denied", True), (st, e.get("error"), "reboot.target" in e.get("message", "")))
    check("chain units untouched", ("active", "inactive"),
          (is_active("hc-test-base.service"), is_active("hc-test-front.service")))
    subprocess.run(["systemctl", "stop", "homelab-control-hctest.service", "hc-test-mid.service", "hc-test-base.service"])
    st, p = plan("hc-test-needs.service", "start", csrf)
    check("plan start lists the inactive dependency it starts", (200, ["hc-test-base.service"]),
          (st, p.get("also_started")))
    st, o = apply("hc-test-needs.service", "start", p.get("token"), csrf)
    check("start with dependency: verified succeeded, dependency active", ("succeeded", "active"),
          (o.get("outcome"), is_active("hc-test-base.service")))
    st, p = plan("hc-test-base.service", "stop", csrf)
    check("plan stop lists the running dependent it stops", (200, ["hc-test-needs.service"]),
          (st, p.get("also_affected")))

    # 7. invalid targets and actions
    for path, body, want in [
        ("hc-test-dummy.socket", {"action": "stop"}, 400), ("-x.service", {"action": "stop"}, 400),
        ("a%3Breboot.service", {"action": "stop"}, 400), ("foo@.service", {"action": "start"}, 400),
        (u, {"action": "enable"}, 400), (u, {"action": "daemon-reload"}, 400), (u, {"action": "mask"}, 400),
        ("hc-test-does-not-exist.service", {"action": "start"}, 404),
    ]:
        st, _ = call("POST", f"/api/v1/services/units/{path}/plan", body, csrf)
        check(f"invalid plan {path} {body['action']}: {want}", want, st)

    # 8. a unit that fails right after systemd accepts the start
    st, p = plan("hc-test-fail.service", "start", csrf)
    st, o = apply("hc-test-fail.service", "start", p.get("token"), csrf)
    check("failing unit: outcome failed (not success)", "failed", o.get("outcome"))
    check("failing unit: observed failed", "failed", (o.get("observed") or {}).get("active_state"))
    check("failing unit: recovery guidance, no rollback", True, any("No automatic rollback" in r for r in o.get("recovery") or []))

    # 9. out-of-band change between plan and apply
    st, p = plan(u, "stop", csrf)
    subprocess.run(["systemctl", "restart", u], check=True)
    st, e = apply(u, "stop", p.get("token"), csrf)
    check("state changed after review: 409 conflict", (409, "conflict"), (st, e.get("error")))
    check("unit untouched after conflict", "active", is_active(u))

    # 10. concurrent applies on one unit: exactly one executes
    _, p1 = plan(u, "restart", csrf)
    _, p2 = plan(u, "restart", csrf)
    results = []

    def run(tok):
        results.append(apply(u, "restart", tok, csrf))

    threads = [threading.Thread(target=run, args=(t,)) for t in (p1.get("token"), p2.get("token"))]
    for t in threads:
        t.start()
    for t in threads:
        t.join()
    codes = sorted(r[0] for r in results)
    errs = sorted(r[1].get("error", "") for r in results if r[0] != 200)
    check("concurrent restarts: one 200, one 409", [200, 409], codes)
    check("concurrent restarts: loser refused as busy or conflict", True, all(x in ("busy", "conflict") for x in errs))

    # 11. interrupted operation: kill the broker while systemd runs a slow start
    st, p = plan("hc-test-slow.service", "start", csrf)
    holder = {}
    t = threading.Thread(target=lambda: holder.update(r=apply("hc-test-slow.service", "start", p.get("token"), csrf)))
    t.start()
    time.sleep(4)
    subprocess.run(["systemctl", "kill", "-s", "SIGKILL", "homelab-control-broker.service"])
    t.join()
    check("apply interrupted by broker kill: not reported as success", True, holder["r"][0] != 200)
    time.sleep(3)  # Restart=on-failure (2 s); socket activation would also start it
    st, ops = call("GET", "/api/v1/operations")
    slow = [x for x in ops.get("operations", []) if x["unit"] == "hc-test-slow.service"]
    check("operations journal lists the slow start", True, len(slow) >= 1)
    if slow:
        check("interrupted operation reported as interrupted", "interrupted", slow[0]["outcome"])
        check("interrupted operation carries observed state", True, bool(slow[0].get("observed")))
        check("interrupted operation was not retried", True, "not retried" in slow[0].get("message", ""))
    log = sh("journalctl", "-u", "homelab-control-broker", "--no-pager", "-o", "cat")
    check("broker logged the recovery", True, "interrupted operation(s)" in log)

    # 12. audit and journal completeness; no secrets
    st, aud = call("GET", "/api/v1/audit?event=services.op&limit=200")
    outcomes = {(a["event"], a["outcome"]) for a in aud.get("events", [])}
    for pair in [("services.op.plan", "success"), ("services.op.plan", "policy_denied"),
                 ("services.op.apply", "requested"), ("services.op.apply", "succeeded"),
                 ("services.op.apply", "failed"), ("services.op.apply", "step_up_required"),
                 ("services.op.apply", "invalid_token"), ("services.op.apply", "conflict")]:
        check(f"API audit has {pair[0]}/{pair[1]}", True, pair in outcomes)
    journal = open("/var/log/homelab-control-broker/operations.jsonl").read()
    baudit = open("/var/log/homelab-control-broker/broker-audit.jsonl").read()
    check("journal has intent records", True, '"event":"op.intent"' in journal)
    check("journal has outcome records", True, '"event":"op.outcome"' in journal)
    check("journal has interrupted record", True, '"event":"op.interrupted"' in journal)
    for name, text in [("operation journal", journal), ("broker audit", baudit)]:
        check(f"{name}: no plan tokens", False, "hcp1." in text)
        check(f"{name}: no password", False, PW in text)
    v = subprocess.run(["hlctl", "audit", "verify", "--file", "/var/log/homelab-control-broker/operations.jsonl"],
                       capture_output=True, text=True)
    check("operation journal hash chain verifies", 0, v.returncode)
    check("journal file mode 0600 root", "600 root", sh("stat", "-c", "%a %U", "/var/log/homelab-control-broker/operations.jsonl"))

    # 13. optional: token and step-up expiry (slow)
    if LONG:
        st, p = plan(u, "stop", csrf)
        time.sleep(121)
        st, e = apply(u, "stop", p.get("token"), csrf)
        check("token after 121 s: 409 plan_invalid", (409, "plan_invalid"), (st, e.get("error")))
        wait = 301 - (time.time() - stepup_at)
        if wait > 0:
            time.sleep(wait)
        st, p = plan(u, "stop", csrf)
        st, e = apply(u, "stop", p.get("token"), csrf)
        check("step-up after 5 min: 403 step_up_required", (403, "step_up_required"), (st, e.get("error")))
        check("unit untouched after expired step-up", "active", is_active(u))
    else:
        print("SKIP  token/step-up expiry waits (set HC_M2_LONG=1)")

    # Clean up the disposable units.
    for name in list(CHAINS) + list(UNITS) + ["hc-test-innocent.service"] + CREATED:
        subprocess.run(["systemctl", "stop", name], capture_output=True)
        if os.path.lexists(f"/etc/systemd/system/{name}"):
            os.remove(f"/etc/systemd/system/{name}")
    subprocess.run(["systemctl", "daemon-reload"])
    subprocess.run(["systemctl", "reset-failed"])
    print(f"m2-service-ops: {passed} passed, {failed} failed")
    return 1 if failed else 0


if __name__ == "__main__":
    sys.exit(main())
