#!/usr/bin/env python3
# SPDX-FileCopyrightText: 2026 The Homelab Control Authors
#
# SPDX-License-Identifier: GPL-3.0-or-later

"""M2-3 final safety review: does the boot-time review depend on what is
running NOW, or miss runtime/unreloaded configuration, so that an enable or
disable it ALLOWS changes what starts at the next boot for a protected unit?

Disposable systemd hosts only (/etc/hc-disposable). Phase "pre" (after the
package is installed and the API runs; HC_PW and HC_SID as in the smoke test)
writes hc-rr-* fixtures and homelab-control-hctest-rr* protected-name
stand-ins (never real access services), asks the PRODUCT for each change and
applies whatever it allows. The host is then rebooted (a fresh PID 1, /run
emptied) and phase "post" records what systemd started. Each scenario prints
one DECISION line (ALLOWED/REFUSED with the reason) and, after the boot, one
BOOT line. Controls (same fixtures, change not made, or made by the harness
with systemctl) show what the boot does without the product's decision.

This is a measurement, not a pass/fail suite: the expected decision of the
corrected product is printed as EXPECT for comparison.
"""

import json
import os
import subprocess
import sys
import time
import urllib.error
import urllib.request

BASE = "http://127.0.0.1:9180"
ORIGIN = "https://127.0.0.1:9180"
SID = os.environ.get("HC_SID", "")
PW = os.environ.get("HC_PW", "")
ETC = "/etc/systemd/system"
LIB = "/usr/lib/systemd/system"
RUN = "/run/systemd/system"
USERS = "/etc/homelab-control/users.json"
STATE = "/var/lib/hc-rr-state.json"
SLEEP = "[Service]\nType=simple\nExecStart=/bin/sleep infinity\n"
WANTED = "[Install]\nWantedBy=multi-user.target\n"
P = "homelab-control-hctest-rr"   # protected-name stand-in prefix


def sh(*args):
    return subprocess.run(args, capture_output=True, text=True).stdout.strip()


def run(*args):
    r = subprocess.run(args, capture_output=True, text=True)
    if r.returncode != 0:
        print(f"HARNESS {' '.join(args)}: rc={r.returncode} {r.stderr.strip()[:200]}")
    return r.returncode


def active(u):
    return sh("systemctl", "is-active", "--", u) or "unknown"


def enabled(u):
    return sh("systemctl", "is-enabled", "--", u) or "unknown"


def write(path, body):
    os.makedirs(os.path.dirname(path), exist_ok=True)
    with open(path, "w") as f:
        f.write(body)


def link(target, path):
    os.makedirs(os.path.dirname(path), exist_ok=True)
    if os.path.lexists(path):
        os.unlink(path)
    os.symlink(target, path)


def unit(desc, extra=""):
    return f"[Unit]\nDescription=hc-rr {desc}\n{extra}" + SLEEP


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
            return r.status, json.loads(r.read() or b"{}")
    except urllib.error.HTTPError as e:
        try:
            return e.code, json.loads(e.read() or b"{}")
        except ValueError:
            return e.code, {}


def login():
    req = urllib.request.Request(BASE + "/api/v1/auth/login", method="POST",
                                 data=json.dumps({"username": "admin", "password": PW}).encode())
    req.add_header("Origin", ORIGIN)
    req.add_header("Content-Type", "application/json")
    with urllib.request.urlopen(req, timeout=30) as r:
        for h in r.headers.get_all("Set-Cookie") or []:
            if h.startswith("__Host-hc_session="):
                return h.split(";", 1)[0].split("=", 1)[1]
    raise RuntimeError("no session cookie")


def reload():
    time.sleep(0.3)
    run("systemctl", "daemon-reload")
    time.sleep(0.3)


CSRF = ""
RESULTS = {}


def product(case, u, action, expect, apply=True):
    """Ask the product; apply what it allows (step-up first)."""
    t0 = time.monotonic()
    st, p = call("POST", f"/api/v1/services/units/{u}/enablement/plan", {"action": action}, CSRF)
    took = time.monotonic() - t0
    if st != 200:
        msg = (p.get("message") or p.get("error") or "")[:400]
        print(f"DECISION {case}: {action} {u}: REFUSED ({st}) plan {took:.2f}s — {msg}")
        print(f"EXPECT   {case}: {expect}")
        RESULTS[case] = "REFUSED"
        return "REFUSED"
    if not apply:
        print(f"DECISION {case}: {action} {u}: ALLOWED (plan only) plan {took:.2f}s starts={p.get('starts')} stops={p.get('stops')}")
        print(f"EXPECT   {case}: {expect}")
        RESULTS[case] = "ALLOWED-plan"
        return "ALLOWED"
    call("POST", "/api/v1/auth/step-up", {"password": PW}, CSRF)
    st, o = call("POST", f"/api/v1/services/units/{u}/enablement/apply", {"action": action, "token": p.get("token")}, CSRF)
    print(f"DECISION {case}: {action} {u}: ALLOWED, applied ({st} {o.get('outcome')}) plan {took:.2f}s "
          f"starts={p.get('starts')} stops={p.get('stops')}")
    print(f"EXPECT   {case}: {expect}")
    RESULTS[case] = f"ALLOWED-{o.get('outcome')}"
    return "ALLOWED"


def setup_fixtures():
    # E1: enable a unit whose dependency is RUNNING now (started by hand, not
    # at boot); that dependency pulls in a unit that conflicts with a
    # protected stand-in that is enabled.
    write(f"{ETC}/{P}1.service", unit("E1 protected stand-in") + WANTED)
    write(f"{ETC}/hc-rr-e1.service", unit("E1 enabled by the product", "Wants=hc-rr-e1x.service\n") + WANTED)
    write(f"{ETC}/hc-rr-e1x.service", unit("E1 running intermediate", "Wants=hc-rr-e1y.service\n"))
    write(f"{ETC}/hc-rr-e1y.service", unit("E1 conflicts with the stand-in", f"Conflicts={P}1.service\nAfter={P}1.service\n"))
    # E1c: control, same graph, enabled by the harness (systemctl) — what the boot does.
    write(f"{ETC}/{P}1c.service", unit("E1c protected stand-in") + WANTED)
    write(f"{ETC}/hc-rr-e1c.service", unit("E1c control", "Wants=hc-rr-e1cx.service\n") + WANTED)
    write(f"{ETC}/hc-rr-e1cx.service", unit("E1c intermediate", "Wants=hc-rr-e1cy.service\n"))
    write(f"{ETC}/hc-rr-e1cy.service", unit("E1c conflicts", f"Conflicts={P}1c.service\nAfter={P}1c.service\n"))
    # E2: a running dependency with FailureAction=reboot (never fails here).
    write(f"{ETC}/hc-rr-e2.service", unit("E2 enabled by the product", "Wants=hc-rr-e2x.service\n") + WANTED)
    write(f"{ETC}/hc-rr-e2x.service", unit("E2 running dependency", "FailureAction=reboot\n"))
    # E3: a conflict with a unit that is enabled (started at boot) but stopped
    # now; the protected stand-in BindsTo= it and is stopped with it.
    write(f"{ETC}/{P}3.service", unit("E3 protected stand-in", "BindsTo=hc-rr-e3z.service\nAfter=hc-rr-e3z.service\n") + WANTED)
    write(f"{ETC}/hc-rr-e3z.service", unit("E3 bound unit"))
    write(f"{ETC}/hc-rr-e3.service", unit("E3 enabled by the product", "Conflicts=hc-rr-e3z.service\n") + WANTED)
    write(f"{ETC}/{P}3c.service", unit("E3c protected stand-in", "BindsTo=hc-rr-e3cz.service\nAfter=hc-rr-e3cz.service\n") + WANTED)
    write(f"{ETC}/hc-rr-e3cz.service", unit("E3c bound unit"))
    write(f"{ETC}/hc-rr-e3c.service", unit("E3c control", "Conflicts=hc-rr-e3cz.service\n") + WANTED)
    # E5: socket activation: the enabled unit pulls in a socket whose service
    # conflicts with an enabled protected stand-in (nothing is running).
    write(f"{ETC}/{P}5.service", unit("E5 protected stand-in") + WANTED)
    write(f"{ETC}/hc-rr-e5.service", unit("E5 enabled by the product", "Wants=hc-rr-e5.socket\n") + WANTED)
    write(f"{ETC}/hc-rr-e5.socket", "[Unit]\nDescription=hc-rr E5 socket\n[Socket]\nListenStream=127.0.0.1:47991\nAccept=no\n"
          "Service=hc-rr-e5s.service\n")
    write(f"{ETC}/hc-rr-e5s.service", unit("E5 socket-activated", f"Conflicts={P}5.service\n"))
    # E7e: a runtime drop-in with the same name hides a persistent drop-in
    # (Conflicts= with an enabled protected stand-in) until the next boot.
    write(f"{ETC}/{P}7e.service", unit("E7e protected stand-in") + WANTED)
    write(f"{ETC}/hc-rr-e7.service", unit("E7e enabled by the product", "Wants=hc-rr-e7m.service\n") + WANTED)
    write(f"{LIB}/hc-rr-e7m.service", unit("E7e dependency"))
    write(f"{LIB}/hc-rr-e7m.service.d/10-hc.conf", f"[Unit]\nConflicts={P}7e.service\n")
    write(f"{RUN}/hc-rr-e7m.service.d/10-hc.conf", "[Unit]\nDescription=hc-rr E7e runtime drop-in\n")
    # E8: a unit started at boot anyway (the enabled stand-in) has a persistent
    # drop-in Conflicts= with a unit the change starts, hidden by a same-name
    # runtime drop-in (set-property --runtime writes /run/systemd/system.control).
    # E8b: the same with /run/systemd/system (precedence measurement).
    for s8, rdir in (("8", "/run/systemd/system.control"), ("8b", RUN)):
        write(f"{ETC}/{P}{s8}.service", unit(f"E{s8} protected stand-in") + WANTED)
        write(f"{ETC}/{P}{s8}.service.d/50-hc.conf", f"[Unit]\nConflicts=hc-rr-e{s8}y.service\n")
        write(f"{rdir}/{P}{s8}.service.d/50-hc.conf", "[Unit]\nDescription=hc-rr runtime drop-in\n")
        write(f"{ETC}/hc-rr-e{s8}.service", unit(f"E{s8} enabled by the product", f"Wants=hc-rr-e{s8}y.service\n") + WANTED)
        write(f"{ETC}/hc-rr-e{s8}y.service", unit(f"E{s8} pulled in"))
    # D1: disable a unit whose RUNNING intermediate pulls in a protected stand-in.
    write(f"{ETC}/{P}d1.service", unit("D1 protected stand-in"))
    write(f"{ETC}/hc-rr-d1.service", unit("D1 disabled by the product", "Wants=hc-rr-d1m.service\n") + WANTED)
    write(f"{ETC}/hc-rr-d1m.service", unit("D1 running intermediate", f"Wants={P}d1.service\n"))
    # D6: a runtime alias over a persistent unit name hides what that name
    # pulls in at boot (the protected stand-in).
    for s in ("6", "6c"):
        write(f"{ETC}/{P}d{s}.service", unit(f"D{s} protected stand-in"))
        write(f"{LIB}/hc-rr-d{s}foo.service", unit(f"D{s} persistent unit", f"Wants={P}d{s}.service\n"))
        write(f"{ETC}/hc-rr-d{s}bar.service", unit(f"D{s} runtime alias target"))
        link(f"{ETC}/hc-rr-d{s}bar.service", f"{RUN}/hc-rr-d{s}foo.service")
        write(f"{ETC}/hc-rr-d{s}.service", unit(f"D{s} disabled by the product", f"Wants=hc-rr-d{s}foo.service\n") + WANTED)
    # D7: a runtime drop-in with the same name hides a persistent drop-in
    # whose Wants= pulls in the protected stand-in at boot.
    for s in ("7", "7c"):
        write(f"{ETC}/{P}d{s}.service", unit(f"D{s} protected stand-in"))
        write(f"{LIB}/hc-rr-d{s}m.service", unit(f"D{s} intermediate"))
        write(f"{LIB}/hc-rr-d{s}m.service.d/10-hc.conf", f"[Unit]\nWants={P}d{s}.service\n")
        write(f"{RUN}/hc-rr-d{s}m.service.d/10-hc.conf", "[Unit]\nDescription=hc-rr runtime drop-in\n")
        write(f"{ETC}/hc-rr-d{s}.service", unit(f"D{s} disabled by the product", f"Wants=hc-rr-d{s}m.service\n") + WANTED)
    # S1: successive --no-reload operations: the product enables an alias
    # (not loaded afterwards); a unit that wants that name is then disabled.
    for s in ("1", "1c"):
        write(f"{ETC}/{P}s{s}.service", unit(f"S{s} protected stand-in"))
        write(f"{ETC}/hc-rr-s{s}v.service", unit(f"S{s} aliased unit", f"Wants={P}s{s}.service\n") +
              f"[Install]\nAlias=hc-rr-s{s}w.service\n")
        write(f"{ETC}/hc-rr-s{s}p.service", unit(f"S{s} puller", f"Wants=hc-rr-s{s}w.service\n") + WANTED)
    # U1: unreloaded persistent edit of a running dependency.
    write(f"{ETC}/{P}u1.service", unit("U1 protected stand-in") + WANTED)
    write(f"{ETC}/hc-rr-u1.service", unit("U1 enabled by the product", "Wants=hc-rr-u1x.service\n") + WANTED)
    write(f"{ETC}/hc-rr-u1x.service", unit("U1 running dependency"))


def pre():
    global SID, CSRF
    print("== M2-3 boot-time review: running state, runtime and unreloaded configuration (phase pre)")
    print(sh("systemctl", "--version").splitlines()[0])
    print(f"PRODUCT  {sh('hlctl', 'version') or sh('dpkg-query', '-W', 'homelab-control') or sh('rpm', '-q', 'homelab-control')}")
    st, sess = call("GET", "/api/v1/auth/session")
    grant = ",".join(sorted(set(sess.get("scopes") or []) | {"services.read", "services.operate", "services.configure"}))
    subprocess.run(["hlctl", "user", "scopes", "--file", USERS, "--username", "admin", "--scopes", grant], check=True, capture_output=True)
    subprocess.run(["chown", "homelab-control:homelab-control", USERS], check=True)
    subprocess.run(["systemctl", "restart", "homelab-control.service"], check=True)
    for _ in range(100):
        try:
            urllib.request.urlopen(BASE + "/api/v1/health", timeout=2)
            break
        except OSError:
            time.sleep(0.2)
    SID = login()
    st, sess = call("GET", "/api/v1/auth/session")
    CSRF = sess.get("csrf_token")
    for _ in range(30):
        st, svc = call("GET", "/api/v1/services")
        if svc.get("configure", {}).get("available"):
            break
        time.sleep(0.5)

    setup_fixtures()
    reload()
    # Boot settings made by the harness (systemctl, with its reload).
    for u in (f"{P}1", f"{P}1c", f"{P}3", f"{P}3c", f"{P}5", f"{P}7e", f"{P}8", f"{P}8b", f"{P}u1", "hc-rr-d1", "hc-rr-d6", "hc-rr-d6c",
              "hc-rr-d7", "hc-rr-d7c", "hc-rr-s1p", "hc-rr-s1cp", "hc-rr-e1c", "hc-rr-e3c"):
        run("systemctl", "enable", f"{u}.service")
    run("systemctl", "enable", "hc-rr-s1cv.service")  # control: alias enabled and kept
    for u in (f"{P}1", f"{P}1c", f"{P}3", f"{P}3c", f"{P}5", f"{P}7e", f"{P}8", f"{P}8b", f"{P}u1"):
        run("systemctl", "start", f"{u}.service")

    # --- U1: an unreloaded persistent edit of a running dependency.
    run("systemctl", "start", "hc-rr-u1x.service")
    time.sleep(0.3)
    write(f"{ETC}/hc-rr-u1x.service", unit("U1 running dependency, edited", f"Conflicts={P}u1.service\n"))
    product("U1", "hc-rr-u1.service", "enable", "REFUSED (unloaded change on disk)", apply=False)
    write(f"{ETC}/hc-rr-u1x.service", unit("U1 running dependency"))
    reload()

    # --- E1: the same change, dependency stopped (control) and running.
    product("E1-stopped", "hc-rr-e1.service", "enable", "REFUSED (conflict with protected stand-in)", apply=False)
    run("systemctl", "start", "hc-rr-e1x.service")   # also starts e1y, which stops the stand-in
    run("systemctl", "start", f"{P}1.service")       # the stand-in back; this stops e1y
    print(f"STATE    E1: e1x={active('hc-rr-e1x.service')} e1y={active('hc-rr-e1y.service')} stand-in={active(P + '1.service')}")
    product("E1-running", "hc-rr-e1.service", "enable", "REFUSED (conflict with protected stand-in at boot)")

    # --- E2: FailureAction=reboot on a dependency, stopped and running.
    product("E2-stopped", "hc-rr-e2.service", "enable", "REFUSED (FailureAction=reboot)", apply=False)
    run("systemctl", "start", "hc-rr-e2x.service")
    product("E2-running", "hc-rr-e2.service", "enable", "REFUSED (FailureAction=reboot)")

    # --- E3: conflict with a unit that is enabled but stopped now.
    product("E3-running", "hc-rr-e3.service", "enable", "REFUSED (stop of bound unit stops protected stand-in)", apply=False)
    run("systemctl", "stop", "hc-rr-e3z.service")    # BindsTo=: the stand-in stops too
    run("systemctl", "stop", "hc-rr-e3cz.service")
    print(f"STATE    E3: e3z={active('hc-rr-e3z.service')} stand-in={active(P + '3.service')}")
    product("E3-stopped", "hc-rr-e3.service", "enable", "REFUSED (the bound unit runs at boot)")

    # --- E5: socket-activated service conflicting with an enabled stand-in.
    product("E5", "hc-rr-e5.service", "enable", "REFUSED (triggered service conflicts with protected stand-in)")

    # --- E7e: runtime drop-in hiding a persistent drop-in's Conflicts=.
    print(f"STATE    E7e: e7m Conflicts={sh('systemctl', 'show', '-p', 'Conflicts', '--value', 'hc-rr-e7m.service')!r} "
          f"DropInPaths={sh('systemctl', 'show', '-p', 'DropInPaths', '--value', 'hc-rr-e7m.service')!r}")
    product("E7e", "hc-rr-e7.service", "enable", "REFUSED (runtime drop-in hides a persistent one)")

    # --- E8/E8b: hidden persistent drop-in on a unit the walk does not evaluate.
    for s8 in ("8", "8b"):
        print(f"STATE    E{s8}: stand-in Conflicts={sh('systemctl', 'show', '-p', 'Conflicts', '--value', P + s8 + '.service')!r} "
              f"DropInPaths={sh('systemctl', 'show', '-p', 'DropInPaths', '--value', P + s8 + '.service')!r}")
        product(f"E{s8}", f"hc-rr-e{s8}.service", "enable", "REFUSED (hidden persistent Conflicts= / or loaded conflict)")

    # --- D1: disable with a running intermediate.
    run("systemctl", "start", "hc-rr-d1m.service")
    product("D1-running", "hc-rr-d1.service", "disable", "REFUSED (protected stand-in started at boot only through it)", apply=False)
    run("systemctl", "stop", "hc-rr-d1m.service")
    product("D1-stopped", "hc-rr-d1.service", "disable", "REFUSED (same, independent of running state)", apply=False)
    run("systemctl", "start", "hc-rr-d1m.service")
    run("systemctl", "start", f"{P}d1.service")
    product("D1-all-running", "hc-rr-d1.service", "disable", "REFUSED (same)", apply=False)

    # --- D6: runtime alias over a persistent name.
    print(f"STATE    D6: d6 Wants={sh('systemctl', 'show', '-p', 'Wants', '--value', 'hc-rr-d6.service')!r} "
          f"d6foo Id={sh('systemctl', 'show', '-p', 'Id', '--value', 'hc-rr-d6foo.service')!r}")
    product("D6", "hc-rr-d6.service", "disable", "REFUSED (runtime alias hides a persistent unit)")

    # --- D7: runtime drop-in hiding a persistent drop-in's Wants=.
    print(f"STATE    D7: d7m Wants={sh('systemctl', 'show', '-p', 'Wants', '--value', 'hc-rr-d7m.service')!r} "
          f"DropInPaths={sh('systemctl', 'show', '-p', 'DropInPaths', '--value', 'hc-rr-d7m.service')!r}")
    product("D7", "hc-rr-d7.service", "disable", "REFUSED (runtime drop-in hides a persistent one)")

    # --- S1: successive --no-reload operations (an own alias not yet loaded).
    product("S1-enable-alias", "hc-rr-s1v.service", "enable", "ALLOWED (enabling the alias itself)")
    product("S1-disable-puller", "hc-rr-s1p.service", "disable", "REFUSED (the name resolves at boot to a unit not loaded now)")

    # Controls made by the harness so that the boot shows the effect.
    print(f"INFO     runtime entries before reboot: {sorted(e for e in os.listdir(RUN) if 'hc-rr' in e)}")
    with open(STATE, "w") as f:
        json.dump(RESULTS, f)
    run("sync")


def post():
    print("== M2-3 boot-time review (phase post: after a fresh boot)")
    print(f"INFO     runtime hc-rr entries after boot: {sorted(e for e in os.listdir(RUN) if 'hc-rr' in e) if os.path.isdir(RUN) else []}")
    res = json.load(open(STATE)) if os.path.exists(STATE) else {}
    time.sleep(3)

    def boot(case, units):
        print(f"BOOT     {case} (decision {res.get(case, 'n/a')}): " + " ".join(f"{u}={active(u)}/{enabled(u)}" for u in units))
    boot("E1-running", ["hc-rr-e1.service", "hc-rr-e1x.service", "hc-rr-e1y.service", f"{P}1.service"])
    boot("E1c(control)", ["hc-rr-e1c.service", "hc-rr-e1cx.service", "hc-rr-e1cy.service", f"{P}1c.service"])
    boot("E2-running", ["hc-rr-e2.service", "hc-rr-e2x.service"])
    boot("E3-stopped", ["hc-rr-e3.service", "hc-rr-e3z.service", f"{P}3.service"])
    boot("E3c(control)", ["hc-rr-e3c.service", "hc-rr-e3cz.service", f"{P}3c.service"])
    boot("E5", ["hc-rr-e5.service", "hc-rr-e5.socket", f"{P}5.service"])
    subprocess.run(["bash", "-c", "exec 3<>/dev/tcp/127.0.0.1/47991; sleep 1"], capture_output=True)
    time.sleep(1)
    boot("E5 after a connection", ["hc-rr-e5s.service", f"{P}5.service"])
    boot("E7e", ["hc-rr-e7.service", "hc-rr-e7m.service", f"{P}7e.service"])
    print(f"INFO     E7e at boot: e7m Conflicts={sh('systemctl', 'show', '-p', 'Conflicts', '--value', 'hc-rr-e7m.service')!r}")
    for s8 in ("8", "8b"):
        boot(f"E{s8}", [f"hc-rr-e{s8}.service", f"hc-rr-e{s8}y.service", f"{P}{s8}.service"])
    boot("D1", ["hc-rr-d1.service", "hc-rr-d1m.service", f"{P}d1.service"])
    boot("D6", ["hc-rr-d6.service", "hc-rr-d6foo.service", f"{P}d6.service"])
    boot("D6c(control, not disabled)", ["hc-rr-d6c.service", "hc-rr-d6cfoo.service", f"{P}d6c.service"])
    boot("D7", ["hc-rr-d7.service", "hc-rr-d7m.service", f"{P}d7.service"])
    boot("D7c(control, not disabled)", ["hc-rr-d7c.service", "hc-rr-d7cm.service", f"{P}d7c.service"])
    boot("S1-disable-puller", ["hc-rr-s1p.service", "hc-rr-s1v.service", f"{P}s1.service"])
    boot("S1c(control, not disabled)", ["hc-rr-s1cp.service", "hc-rr-s1cv.service", f"{P}s1c.service"])
    print("== journal: conflicts and jobs of hc-rr units at boot")
    out = sh("journalctl", "-b", "--no-pager", "-o", "cat", "-p", "info")
    for line in out.splitlines():
        if ("hc-rr" in line or "hctest-rr" in line) and any(k in line.lower() for k in ("conflict", "deleted", "transaction", "fail", "stopp")):
            print("JOURNAL  " + line[:300])


if __name__ == "__main__":
    if not os.path.exists("/etc/hc-disposable"):
        print("refusing: not a disposable host")
        sys.exit(2)
    if len(sys.argv) > 1 and sys.argv[1] == "post":
        post()
    else:
        if not SID or not PW:
            print("HC_SID and HC_PW are required", file=sys.stderr)
            sys.exit(2)
        pre()
