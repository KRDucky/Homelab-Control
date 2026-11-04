#!/usr/bin/env python3
# SPDX-FileCopyrightText: 2026 The Homelab Control Authors
#
# SPDX-License-Identifier: GPL-3.0-or-later

"""M2-3 real-systemd scenario: boot-time enable/disable, read-only unit views
and the module registry (docs/m2/M2-3-DESIGN-REVIEW.md §6).

Runs INSIDE a disposable systemd host (container or VM marked with
/etc/hc-disposable) after the package is installed and hc-api/hc-broker run,
sourced by tests/systemd/m2-3-checks.sh after the M2-2 scenario. It only ever
enables or disables the disposable hc-test-en-* units it creates; protected
units are only *planned* to prove refusal. Cleanup disables nothing but those
fixtures (regular files this test wrote), and verifies that the set of
symbolic links under /etc/systemd/system, the boot setting of SSH, the broker
and the API, and the SSH main PID are unchanged.

Environment: HC_PW (admin password), HC_SID (a session cookie of admin).
"""

import atexit
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
SID = os.environ.get("HC_SID", "")
PW = os.environ.get("HC_PW", "")
ETC = "/etc/systemd/system"
USERS = "/etc/homelab-control/users.json"
MODULES = "/etc/homelab-control/modules.json"
SECRET_FRAGMENT = "fixture-token-xyz-M23"   # test fixture values, never real secrets
SECRET_DROPIN = "fixture-secret-abc-M23"
passed = failed = 0
_cleaned = False
CREATED = []          # fixture unit files written by this run
RESPONSES = []        # every API response body, scanned for fixture secrets


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


def is_enabled(u):
    return sh("systemctl", "is-enabled", "--", u) or "unknown"


def prop(u, p):
    return sh("systemctl", "show", "-p", p, "--value", "--", u)


def links():
    out = set()
    for root, dirs, files in os.walk(ETC):
        for n in dirs + files:
            p = os.path.join(root, n)
            if os.path.islink(p):
                out.add(p + " -> " + os.readlink(p))
    return out


def call(method, path, body=None, csrf=None, sid=None):
    data = json.dumps(body).encode() if body is not None else None
    req = urllib.request.Request(BASE + path, data=data, method=method)
    req.add_header("Cookie", f"__Host-hc_session={sid or SID}")
    if method != "GET":
        req.add_header("Origin", ORIGIN)
        req.add_header("Content-Type", "application/json")
        if csrf:
            req.add_header("X-CSRF-Token", csrf)
    try:
        with urllib.request.urlopen(req, timeout=120) as r:
            raw = r.read() or b"{}"
            RESPONSES.append(raw.decode(errors="replace"))
            return r.status, json.loads(raw)
    except urllib.error.HTTPError as e:
        raw = e.read() or b"{}"
        RESPONSES.append(raw.decode(errors="replace"))
        try:
            return e.code, json.loads(raw)
        except ValueError:
            return e.code, {}


def login(user="admin", pw=None):
    req = urllib.request.Request(BASE + "/api/v1/auth/login", method="POST",
                                 data=json.dumps({"username": user, "password": pw or PW}).encode())
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


def restart_api():
    subprocess.run(["systemctl", "restart", "homelab-control.service"], check=True)
    wait_health()


def restart_broker():
    # Socket-activated: stopping the service is enough; the next call starts it.
    subprocess.run(["systemctl", "stop", "homelab-control-broker.service"], check=True)


def eplan(unit, action, csrf, sid=None):
    return call("POST", f"/api/v1/services/units/{unit}/enablement/plan", {"action": action}, csrf, sid)


def eapply(unit, action, token, csrf, sid=None):
    return call("POST", f"/api/v1/services/units/{unit}/enablement/apply", {"action": action, "token": token}, csrf, sid)


def stepup(csrf, sid=None):
    return call("POST", "/api/v1/auth/step-up", {"password": PW}, csrf, sid)


def journal_cursor():
    out = sh("journalctl", "--no-pager", "-n", "0", "--show-cursor")
    return out.rsplit("cursor: ", 1)[-1].strip() if "cursor: " in out else ""


def manager_reloads(cursor):
    """PID 1's own reload messages after cursor ("Reloading requested from
    client PID …", "Reloading finished in …"); other lines are printed for the
    record but not counted."""
    subprocess.run(["journalctl", "--flush"], capture_output=True)
    out = sh("journalctl", "--no-pager", "-o", "cat", "_PID=1", f"--after-cursor={cursor}")
    hits = [line for line in out.splitlines() if line.startswith("Reloading requested") or line.startswith("Reloading finished")]
    for line in out.splitlines():
        print(f"INFO  PID 1 during apply: {line[:160]}")
    return hits


SSH = "ssh.service"
_SLEEP = "[Service]\nType=simple\nExecStart=/bin/sleep infinity\n"
FIXTURES = {}


def fixtures(ssh):
    return {
        "hc-test-en-a.service": "[Unit]\nDescription=M2-3 disposable fixture A\n" + _SLEEP +
                                "[Install]\nWantedBy=multi-user.target\n",
        "hc-test-en-run.service": "[Unit]\nDescription=M2-3 disposable running fixture\n" + _SLEEP +
                                  "[Install]\nWantedBy=multi-user.target\nAlias=hc-test-en-run-alias.service\n",
        "hc-test-en-stale.service": "[Unit]\nDescription=M2-3 disposable stale-plan fixture\n" + _SLEEP +
                                    "[Install]\nWantedBy=multi-user.target\n",
        "hc-test-en-secret.service": "[Unit]\nDescription=M2-3 disposable redaction fixture\n[Service]\nType=simple\n"
                                     f"Environment=API_TOKEN={SECRET_FRAGMENT}\n"
                                     f"ExecStart=/bin/sleep infinity --password={SECRET_FRAGMENT}\n"
                                     "[Install]\nWantedBy=multi-user.target\n",
        "hc-test-en-also.service": "[Unit]\nDescription=M2-3 Also= fixture\n" + _SLEEP +
                                   "[Install]\nWantedBy=multi-user.target\nAlso=hc-test-en-a.service\n",
        "hc-test-en-reqby.service": "[Unit]\nDescription=M2-3 RequiredBy= fixture\n" + _SLEEP +
                                    "[Install]\nRequiredBy=multi-user.target\n",
        "hc-test-en-static.service": "[Unit]\nDescription=M2-3 static fixture\n" + _SLEEP,
        "hc-test-en-protalias.service": "[Unit]\nDescription=M2-3 protected install alias fixture\n" + _SLEEP +
                                        "[Install]\nWantedBy=multi-user.target\nAlias=sshd-hctest.service\n",
        "hc-test-en-wantssh.service": "[Unit]\nDescription=M2-3 wanted-by-SSH fixture\n" + _SLEEP +
                                      f"[Install]\nWantedBy={ssh}\n",
        "hc-test-en-conflict.service": "[Unit]\nDescription=M2-3 boot conflict with SSH fixture\n"
                                       f"Conflicts={ssh}\n" + _SLEEP + "[Install]\nWantedBy=multi-user.target\n",
        "hc-test-en-spec.service": "[Unit]\nDescription=M2-3 specifier fixture\n" + _SLEEP +
                                   "[Install]\nWantedBy=multi-user.target\nAlias=%N-x.service\n",
    }


def setup():
    global SSH, FIXTURES
    for cand in ("ssh.service", "sshd.service"):
        if prop(cand, "LoadState") == "loaded":
            SSH = cand
            break
    FIXTURES = fixtures(SSH)
    for name, body in FIXTURES.items():
        path = f"{ETC}/{name}"
        if os.path.lexists(path):
            raise RuntimeError(f"{path} exists before the test; refusing to overwrite it")
        with open(path, "w") as f:
            f.write(body)
        CREATED.append(name)
    # A root-only drop-in that carries a secret AND an [Install] alias: the
    # broker must honour the alias (it reads as root) without disclosing it.
    os.makedirs(f"{ETC}/hc-test-en-secret.service.d", exist_ok=True)
    dropin = f"{ETC}/hc-test-en-secret.service.d/secret.conf"
    with open(dropin, "w") as f:
        f.write(f"[Service]\nEnvironment=DB_PASSWORD={SECRET_DROPIN}\n[Install]\nAlias=hc-test-en-secret-alias.service\n")
    os.chmod(dropin, 0o600)
    subprocess.run(["systemctl", "daemon-reload"], check=True)


def invariants():
    return {
        "links": links(),
        "ssh_enabled": is_enabled(SSH),
        "ssh_socket_enabled": is_enabled(SSH[:-8] + ".socket"),
        "ssh_pid": prop(SSH, "MainPID"),
        "api_enabled": is_enabled("homelab-control.service"),
        "broker_socket_enabled": is_enabled("homelab-control-broker.socket"),
        "broker_enabled": is_enabled("homelab-control-broker.service"),
    }


def cleanup(before):
    """Disable and remove ONLY fixtures this run wrote; verify invariants."""
    global _cleaned, failed
    if _cleaned:
        return
    _cleaned = True
    if os.path.exists(MODULES):
        os.remove(MODULES)
    for name in CREATED:
        path = f"{ETC}/{name}"
        # Only regular files this test wrote, never a symlink or an alias.
        if not os.path.isfile(path) or os.path.islink(path) or prop(name, "Id") not in ("", name):
            print(f"FAIL  cleanup refused to touch {name} (not a fixture this run wrote)")
            failed += 1
            continue
        subprocess.run(["systemctl", "--no-reload", "disable", "--", name], capture_output=True)
        subprocess.run(["systemctl", "stop", "--", name], capture_output=True)
        os.remove(path)
    subprocess.run(["rm", "-rf", "--", f"{ETC}/hc-test-en-secret.service.d"])
    subprocess.run(["systemctl", "daemon-reload"], capture_output=True)
    subprocess.run(["systemctl", "reset-failed"], capture_output=True)
    after = invariants()
    gained = sorted(after["links"] - before["links"])
    lost = sorted(before["links"] - after["links"])
    check("cleanup: no symbolic link gained under /etc/systemd/system", [], gained)
    check("cleanup: no symbolic link lost under /etc/systemd/system", [], lost)
    for k in ("ssh_enabled", "ssh_socket_enabled", "ssh_pid", "api_enabled", "broker_socket_enabled", "broker_enabled"):
        check(f"cleanup: {k} unchanged", before[k], after[k])


def main():
    global SID
    if not SID or not PW:
        print("HC_SID and HC_PW are required", file=sys.stderr)
        return 2
    setup()
    before = invariants()
    atexit.register(cleanup, before)
    print(f"== M2-3 enablement (SSH unit {SSH}, is-enabled {before['ssh_enabled']})")

    # 0. services.configure is never implied; grant it explicitly.
    st, sess = call("GET", "/api/v1/auth/session")
    scopes = set(sess.get("scopes") or [])
    st, e = eplan("hc-test-en-a.service", "enable", sess.get("csrf_token"))
    check("plan without services.configure: 403 forbidden", (403, "forbidden"), (st, e.get("error")))
    grant = ",".join(sorted(scopes | {"services.read", "services.logs", "services.operate", "services.configure", "audit.read"}))
    subprocess.run(["hlctl", "user", "scopes", "--file", USERS, "--username", "admin", "--scopes", grant], check=True, capture_output=True)
    # A second account with services.operate but not services.configure.
    subprocess.run(["hlctl", "user", "add", "--file", USERS, "--username", "operator", "--scopes",
                    "services.read,services.operate"], input=PW + "\n", text=True, check=True, capture_output=True)
    subprocess.run(["chown", "homelab-control:homelab-control", USERS], check=True)
    restart_api()
    SID = login()
    st, sess = call("GET", "/api/v1/auth/session")
    csrf = sess.get("csrf_token")
    check("session has services.configure", True, "services.configure" in (sess.get("scopes") or []))
    op_sid = login("operator")
    st, op_sess = call("GET", "/api/v1/auth/session", sid=op_sid)
    st, e = eplan("hc-test-en-a.service", "enable", op_sess.get("csrf_token"), op_sid)
    check("services.operate alone cannot plan enablement: 403", (403, "forbidden"), (st, e.get("error")))
    configured = False
    for _ in range(30):
        st, svc = call("GET", "/api/v1/services")
        configured = svc.get("configure", {}).get("available")
        if configured:
            break
        time.sleep(0.5)
    check("services: configure available", True, configured)

    # 1. Enable: preview, step-up, no --now, no reload, independent read-back.
    u = "hc-test-en-a.service"
    st, p = eplan(u, "enable", csrf)
    check("plan enable: 200 disruptive, step-up, boot_state disabled", (200, "disruptive", True, "disabled"),
          (st, p.get("risk"), p.get("step_up_required"), p.get("boot_state")))
    check("plan enable lists the link", [f"{ETC}/multi-user.target.wants/{u}"], p.get("links"))
    check("plan says the running state is not changed", True, any("does not start it now" in c for c in p.get("consequences", [])))
    st, e = eapply(u, "enable", p.get("token"), csrf)
    check("apply without step-up: 403 step_up_required", (403, "step_up_required"), (st, e.get("error")))
    check("nothing changed without step-up", "disabled", is_enabled(u))
    st, _ = stepup(csrf)
    check("step-up", 200, st)
    cur = journal_cursor()
    st, o = eapply(u, "enable", p.get("token"), csrf)
    reloads = manager_reloads(cur)
    check("apply enable: succeeded", (200, "succeeded"), (st, o.get("outcome")))
    check("outcome boot_state enabled (read back)", ("disabled", "enabled"), (o.get("boot_state_before"), o.get("boot_state")))
    check("host: is-enabled enabled", "enabled", is_enabled(u))
    check("host: link exists", True, os.path.islink(f"{ETC}/multi-user.target.wants/{u}"))
    check("host: unit NOT started (no --now)", "inactive", prop(u, "ActiveState"))
    check("host: no manager reload during the change", [], reloads)
    check("host: systemd reports NeedDaemonReload (expected without reload)", "yes", prop(u, "NeedDaemonReload"))
    st, e = eapply(u, "enable", p.get("token"), csrf)
    check("replayed token: 409 plan_invalid", (409, "plan_invalid"), (st, e.get("error")))
    st, e = eplan(u, "enable", csrf)
    check("enable again: 403 already enabled", (403, True), (st, "already enabled" in e.get("message", "")))

    # 2. Disable a RUNNING unit with an alias: it keeps running; links removed.
    r = "hc-test-en-run.service"
    subprocess.run(["systemctl", "--no-reload", "enable", "--", r], check=True, capture_output=True)
    subprocess.run(["systemctl", "start", "--", r], check=True)
    pid = prop(r, "MainPID")
    st, p = eplan(r, "disable", csrf)
    check("plan disable: links include the alias", sorted([f"{ETC}/multi-user.target.wants/{r}", f"{ETC}/hc-test-en-run-alias.service"]),
          sorted(p.get("links") or []))
    st, o = eapply(r, "disable", p.get("token"), csrf)
    check("apply disable: succeeded", (200, "succeeded", "disabled"), (st, o.get("outcome"), o.get("boot_state")))
    check("host: alias link removed", False, os.path.lexists(f"{ETC}/hc-test-en-run-alias.service"))
    check("host: still running with the same PID (no stop)", ("active", pid), (prop(r, "ActiveState"), prop(r, "MainPID")))

    # 3. Token isolation from start/stop/restart.
    st, p = eplan(u, "disable", csrf)
    st, e = call("POST", f"/api/v1/services/units/{u}/apply", {"action": "start", "token": p.get("token")}, csrf)
    check("enablement token on start/stop apply: 409 plan_invalid", (409, "plan_invalid"), (st, e.get("error")))
    check("unit not started by a foreign token", "inactive", prop(u, "ActiveState"))
    st, o = eapply(u, "disable", p.get("token"), csrf)
    check("token still valid for its own op afterwards", (200, "succeeded"), (st, o.get("outcome")))

    # 4. Stale plans: [Install] edited, or enabled on the host meanwhile.
    s = "hc-test-en-stale.service"
    st, p = eplan(s, "enable", csrf)
    with open(f"{ETC}/{s}", "a") as f:
        f.write("Alias=hc-test-en-stale-alias.service\n")
    st, e = eapply(s, "enable", p.get("token"), csrf)
    check("[Install] changed after review: 409 conflict, nothing done", (409, "conflict", "disabled"), (st, e.get("error"), is_enabled(s)))
    st, p = eplan(s, "enable", csrf)
    subprocess.run(["systemctl", "--no-reload", "enable", "--", s], check=True, capture_output=True)
    st, e = eapply(s, "enable", p.get("token"), csrf)
    check("enabled on the host after review: refused", (True, True), (st in (403, 409), "Nothing was done" in e.get("message", "") or "nothing was done" in e.get("message", "")))
    subprocess.run(["systemctl", "--no-reload", "disable", "--", s], check=True, capture_output=True)

    # 5. Concurrency: one token, eight parallel applies → exactly one change.
    st, p = eplan(u, "enable", csrf)
    results = []

    def worker():
        results.append(eapply(u, "enable", p.get("token"), csrf)[0])
    ts = [threading.Thread(target=worker) for _ in range(8)]
    [t.start() for t in ts]
    [t.join() for t in ts]
    check("parallel applies of one token: exactly one 200", 1, results.count(200))
    check("the others: 409", 7, results.count(409))
    st, p = eplan(u, "disable", csrf)
    eapply(u, "disable", p.get("token"), csrf)

    # 6. Refusals (planned only; nothing is changed).
    refusals = [
        (SSH, "disable", "protected SSH unit"),
        ("homelab-control.service", "disable", "own API unit"),
        ("homelab-control-broker.service", "disable", "own broker unit"),
        ("hc-test-en-protalias.service", "enable", "[Install] alias with a protected name"),
        ("hc-test-en-wantssh.service", "enable", "WantedBy= the SSH unit"),
        ("hc-test-en-conflict.service", "enable", "boot-time Conflicts= with SSH"),
        ("hc-test-en-also.service", "enable", "Also="),
        ("hc-test-en-reqby.service", "enable", "RequiredBy="),
        ("hc-test-en-static.service", "enable", "static unit"),
        ("hc-test-en-spec.service", "enable", "specifier in [Install]"),
    ]
    snapshot = links()
    for unit, action, why in refusals:
        st, e = eplan(unit, action, csrf)
        check(f"refused: {why} ({unit}): 403 policy_denied", (403, "policy_denied"), (st, e.get("error")))
    for unit, body in (("getty@tty1.service", "enable"), ("hc-test-en-a.socket", "enable"), ("hc-test-en-a.service", "mask"),
                       ("hc-test-en-a.service", "enable --now")):
        st, e = eplan(unit, body, csrf)
        check(f"refused: {unit} {body}: 400", 400, st)
    check("refusals changed no link", snapshot, links())

    # 7. Root-only drop-in: the [Install] alias in it is honoured, its secret never disclosed.
    sec = "hc-test-en-secret.service"
    stepup(csrf)  # keep well inside the 5-minute step-up window
    st, p = eplan(sec, "enable", csrf)
    check("drop-in [Install] alias read as root", True, f"{ETC}/hc-test-en-secret-alias.service" in (p.get("links") or []))
    st, o = eapply(sec, "enable", p.get("token"), csrf)
    check("enable with drop-in alias: succeeded", (200, "succeeded"), (st, o.get("outcome")))
    check("host: drop-in alias link exists", True, os.path.islink(f"{ETC}/hc-test-en-secret-alias.service"))
    st, p = eplan(sec, "disable", csrf)
    st, o = eapply(sec, "disable", p.get("token"), csrf)
    check("disable removes the drop-in alias", (200, "succeeded", False), (st, o.get("outcome"), os.path.lexists(f"{ETC}/hc-test-en-secret-alias.service")))

    # 8. Read-only views.
    st, c = call("GET", f"/api/v1/services/units/{sec}/contents")
    texts = "\n".join(f.get("text", "") for f in c.get("files", []))
    check("contents: 200 with redactions", (200, True), (st, c.get("redactions", 0) >= 2))
    check("contents: root-only drop-in reported unreadable", True, f"{ETC}/{sec}.d/secret.conf" in (c.get("unreadable") or []))
    check("contents: no fixture secret", (False, False), (SECRET_FRAGMENT in texts, SECRET_DROPIN in texts))
    st, d = call("GET", f"/api/v1/services/units/{u}")
    check("detail: unit, path, restarts counter", (200, "unit", f"{ETC}/{u}", True),
          (st, d.get("kind"), d.get("fragment_path"), isinstance(d.get("restarts"), int)))
    check("detail: boot state from disk", "disabled", d.get("boot_state"))
    alias_name = "sshd.service" if SSH == "ssh.service" else None
    if alias_name and prop(alias_name, "Id") == SSH:
        st, d = call("GET", f"/api/v1/services/units/{alias_name}")
        check("detail: alias resolved, protected label", (200, "alias", SSH, True),
              (st, d.get("kind"), d.get("id"), (d.get("protected") or {}).get("protected")))
    st, d = call("GET", "/api/v1/services/units/getty@.service")
    check("detail: template has no runtime state", (200, "template", None), (st, d.get("kind"), d.get("active_state")))
    st, d = call("GET", "/api/v1/services/units/hc-test-en-nonexistent.service")
    check("detail: not in inventory: 404", 404, st)
    st, t = call("GET", "/api/v1/services/timers")
    check("timers: 200 list", (200, True), (st, isinstance(t.get("timers"), list)))
    st, so = call("GET", "/api/v1/services/sockets")
    check("sockets: 200 includes the broker socket", (200, True),
          (st, any(x.get("unit") == "homelab-control-broker.socket" for x in so.get("sockets", []))))

    # 9. Journal and audit: records present, no tokens or fixture secrets.
    st, ops = call("GET", "/api/v1/operations")
    en = [x for x in ops.get("operations", []) if x.get("op") == "services.enablement"]
    check("operation journal lists enablement changes", True, len(en) >= 6)
    journal = open("/var/log/homelab-control-broker/operations.jsonl").read()
    baudit = open("/var/log/homelab-control-broker/broker-audit.jsonl").read()
    db = open("/var/lib/homelab-control/state.db", "rb").read().decode(errors="replace")
    jctl = sh("journalctl", "--no-pager", "-o", "cat", "-u", "homelab-control.service", "-u", "homelab-control-broker.service")
    for name, text in [("operation journal", journal), ("broker audit", baudit), ("API state db", db), ("service journal", jctl),
                       ("API responses", "\n".join(RESPONSES))]:
        check(f"{name}: no plan tokens or fixture secrets", (False, False, False),
              ("hcp1." in text and name != "API responses", SECRET_FRAGMENT in text, SECRET_DROPIN in text))
    v = subprocess.run(["hlctl", "audit", "verify", "--file", "/var/log/homelab-control-broker/operations.jsonl"], capture_output=True, text=True)
    check("operation journal hash chain verifies", 0, v.returncode)

    # 10. Module registry: disable services → routes 404, broker hides its ops.
    with open(MODULES, "w") as f:
        f.write('{"version":1,"disabled":["services"]}\n')
    os.chmod(MODULES, 0o644)
    restart_broker()
    restart_api()
    SID = login()
    st, sess = call("GET", "/api/v1/auth/session")
    csrf = sess.get("csrf_token")
    st, unknown = call("GET", "/api/v1/no-such-route")
    for path in ("/api/v1/services", "/api/v1/services/timers", f"/api/v1/services/units/{u}"):
        st, b = call("GET", path)
        check(f"module disabled: GET {path} 404 like an unknown route", (404, unknown), (st, b))
    st, b = eplan(u, "enable", csrf)
    check("module disabled: enablement plan 404", 404, st)
    st, cps = call("GET", "/api/v1/capabilities")
    check("module disabled: capabilities omit services", False, any(c.get("id") == "services" for c in cps.get("capabilities", [])))
    check("module disabled: listed as disabled", [False], [m.get("enabled") for m in cps.get("modules", [])])
    info = sh("hlctl", "broker", "info")
    check("module disabled: broker.info lists no services ops", False, "services.systemd.list" in info)
    with open(MODULES, "w") as f:
        f.write("not json\n")
    restart_broker()
    restart_api()
    SID = login()
    st, b = call("GET", "/api/v1/services")
    check("invalid module setting fails closed: 404", 404, st)
    os.remove(MODULES)
    restart_broker()
    restart_api()
    SID = login()
    st, b = call("GET", "/api/v1/services")
    check("module setting removed: services back", 200, st)

    cleanup(before)
    print(f"m2-3-enablement: {passed} passed, {failed} failed")
    return 1 if failed else 0


if __name__ == "__main__":
    sys.exit(main())
