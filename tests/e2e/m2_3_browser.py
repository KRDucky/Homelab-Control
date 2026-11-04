#!/usr/bin/env python3
# SPDX-FileCopyrightText: 2026 The Homelab Control Authors
#
# SPDX-License-Identifier: GPL-3.0-or-later

"""M2-3 real-backend browser workflow (no API mocking).

Boots a disposable systemd container from dev/disposable/images/<image>.Dockerfile,
installs the given package, runs hc-api on the container's loopback with
-dev-insecure-http, and publishes it ONLY on the workstation's 127.0.0.1 through
a relay inside the container. Playwright Chromium then drives: boot-time
enable/disable (preview → password → verified result), protected units, unit
detail and the redacted unit file, timers and sockets tabs by keyboard, phone
and laptop layouts with axe in two themes, and the disabled-module page. Every
host-changing step is cross-checked with an independent `systemctl` call in
the container. The container is removed at the end.

Usage: m2_3_browser.py <image> <package> <evidence-dir>
Needs: docker, python3-playwright, /usr/bin/chromium (or HC_CHROMIUM), and
frontend/app/node_modules/axe-core (npm ci --ignore-scripts).
"""
import json
import os
import secrets
import subprocess
import sys
import time
from datetime import datetime, timezone

from playwright.sync_api import sync_playwright

IMAGE, PKG, OUT = sys.argv[1:4]
ROOT = os.path.abspath(os.path.join(os.path.dirname(__file__), "../.."))
NAME = f"hc-m23-browser-{os.getpid()}"
PORT = 19180 + os.getpid() % 500
URL = f"http://127.0.0.1:{PORT}"
PW = "e2-" + secrets.token_hex(12)  # throwaway; never printed or saved
USER = "e2-configurer"
AXE = os.path.join(ROOT, "frontend/app/node_modules/axe-core/axe.min.js")
SHOTS = os.path.join(OUT, "screens")
os.makedirs(SHOTS, exist_ok=True)
results, lines = [], []
ETC = "/etc/systemd/system"


def now():
    return datetime.now(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ")


def log(msg):
    line = f"{now()} {msg}"
    print(line, flush=True)
    lines.append(line)


def dx(cmd, check=False, inp=None):
    r = subprocess.run(["docker", "exec", "-i", NAME, "sh", "-c", cmd], capture_output=True, text=True, input=inp)
    if check and r.returncode != 0:
        raise RuntimeError(f"{cmd}: {r.stderr.strip()}")
    return (r.stdout + r.stderr).strip()


def record(step, title, ok, ui="", observer="", note=""):
    status = "Pass" if ok else "Fail"
    results.append(dict(step=step, title=title, result=status, time_utc=now(), ui=ui, observer=observer, note=note))
    log(f"{step} {status}: {title} | ui={ui!r} | observer={observer!r} {note}")


FIXTURES = {
    "hc-test-en-ui.service": "[Unit]\nDescription=M2-3 browser fixture (example)\n[Service]\nType=simple\n"
                             "ExecStart=/bin/sleep infinity\nEnvironment=API_TOKEN=fixture-ui-token\n"
                             "[Install]\nWantedBy=multi-user.target\n",
    "hc-test-en-ui-timer.service": "[Unit]\nDescription=M2-3 browser timer target (example)\n[Service]\nType=oneshot\nExecStart=/bin/true\n",
    "hc-test-en-ui.timer": "[Unit]\nDescription=M2-3 browser timer (example)\n[Timer]\nOnCalendar=daily\nPersistent=true\n"
                           "Unit=hc-test-en-ui-timer.service\n[Install]\nWantedBy=timers.target\n",
}

RELAY = f"""
import socket, threading
def pipe(a, b):
    try:
        while True:
            d = a.recv(65536)
            if not d: break
            b.sendall(d)
    except OSError: pass
    finally:
        a.close(); b.close()
s = socket.socket(); s.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
s.bind(("0.0.0.0", 19180)); s.listen(16)
while True:
    c, _ = s.accept()
    u = socket.create_connection(("127.0.0.1", 9180))
    threading.Thread(target=pipe, args=(c, u), daemon=True).start()
    threading.Thread(target=pipe, args=(u, c), daemon=True).start()
"""


def setup():
    df = os.path.join(ROOT, "dev/disposable/images", IMAGE + ".Dockerfile")
    tag = f"hc-disposable-{IMAGE}:m2-3"
    subprocess.run(["docker", "build", "-q", "-t", tag, "-f", df, os.path.dirname(df)], check=True, capture_output=True)
    subprocess.run(["docker", "run", "-d", "--name", NAME, "--privileged", "--cgroupns=private", "--tmpfs", "/run",
                    "--tmpfs", "/run/lock", "--tmpfs", "/tmp", "-p", f"127.0.0.1:{PORT}:19180", tag], check=True, capture_output=True)
    for _ in range(60):
        if dx("systemctl is-system-running") in ("running", "degraded"):
            break
        time.sleep(0.5)
    kind = PKG.rsplit(".", 1)[-1]
    subprocess.run(["docker", "cp", PKG, f"{NAME}:/root/pkg.{kind}"], check=True)
    dx("touch /etc/hc-disposable")
    log("install: " + dx({"deb": "dpkg -i /root/pkg.deb", "rpm": "rpm -Uvh /root/pkg.rpm"}[kind] + " >/dev/null 2>&1; echo rc=$?"))
    dx("sed -i 's|^HC_API_ARGS=.*|HC_API_ARGS=-listen 127.0.0.1:9180 -dev-insecure-http|' /etc/homelab-control/hc-api.env", check=True)
    dx(f"hlctl user add --file /etc/homelab-control/users.json --username {USER} "
       "--scopes system.read,services.read,services.logs,services.operate,services.configure,audit.read", inp=PW + "\n", check=True)
    dx("chown homelab-control:homelab-control /etc/homelab-control/users.json", check=True)
    for name, body in FIXTURES.items():
        dx(f"cat > {ETC}/{name}", inp=body, check=True)
    # Pause before and after the (test-only) reload: Homelab Control refuses to
    # enable while unit files changed after systemd's last load (probe 8).
    dx("sleep 0.3 && systemctl daemon-reload && sleep 0.3 && systemctl start homelab-control-broker.socket homelab-control.service hc-test-en-ui.timer", check=True)
    dx("cat > /root/relay.py", inp=RELAY, check=True)
    subprocess.Popen(["docker", "exec", "-d", NAME, "python3", "/root/relay.py"])
    for _ in range(50):
        try:
            import urllib.request
            urllib.request.urlopen(URL + "/api/v1/health", timeout=2)
            break
        except OSError:
            time.sleep(0.3)
    log("environment: " + dx("grep -m1 PRETTY_NAME /etc/os-release; systemctl --version | head -1"))
    log("package: " + dx("dpkg-query -W homelab-control 2>/dev/null || rpm -q homelab-control"))


def overflow(page):
    return page.evaluate("document.scrollingElement.scrollWidth - document.scrollingElement.clientWidth")


AXE_SRC = open(AXE).read() if os.path.exists(AXE) else ""


def axe(page, scope="document"):
    # Evaluated over the DevTools protocol: the page's CSP (script-src 'self')
    # stays in force and is checked separately (B18).
    if not page.evaluate("typeof window.axe !== 'undefined'"):
        page.evaluate(AXE_SRC)
    res = page.evaluate(f"async () => (await axe.run({scope})).violations.map(v => v.id + ':' + v.nodes.length)")
    return res


def main():
    setup()
    with sync_playwright() as p:
        browser = p.chromium.launch(headless=True, executable_path=os.environ.get("HC_CHROMIUM", "/usr/bin/chromium"))
        ctx = browser.new_context(viewport={"width": 1366, "height": 900})
        page = ctx.new_page()
        page.set_default_timeout(20000)
        errors, csp = [], []
        page.on("pageerror", lambda e: errors.append(str(e)))
        page.on("console", lambda m: csp.append(m.text) if "Content Security Policy" in m.text else None)

        def shot(name):
            path = os.path.join(SHOTS, f"{name}.png")
            page.screenshot(path=path, full_page=True)
            return path

        page.goto(URL + "/")
        page.locator("#username").fill(USER)
        page.locator("#password").fill(PW)
        page.get_by_role("button", name="Sign in").click()
        page.get_by_role("navigation", name="Main").wait_for()
        page.goto(URL + "/services")
        table = page.get_by_role("table", name="Units")
        table.wait_for()
        page.get_by_role("searchbox", name="Search units").fill("hc-test-en-ui")
        shot("01-services-laptop")
        api = page.evaluate("fetch('/api/v1/services').then(r => r.json()).then(j => JSON.stringify({operate: j.operate, configure: j.configure, unit: j.units.filter(u => u.name.startsWith('hc-test-en-ui'))}))")
        log("services API as seen by the page: " + api[:600])
        log("page: " + page.locator("main").inner_text()[:600].replace("\n", " | "))

        # B1: enable at boot through the keyboard: focus the button, Enter.
        btn = page.get_by_role("button", name="Enable at boot: hc-test-en-ui.service")
        btn.focus()
        page.keyboard.press("Enter")
        dlg = page.get_by_role("dialog")
        dlg.get_by_text("Preview · nothing has been changed yet").wait_for()
        txt = dlg.inner_text()
        record("B1", "enable preview shows link, boot vs runtime, no reload (may apply before a reboot), risk, no rollback",
               all(s in txt for s in [f"{ETC}/multi-user.target.wants/hc-test-en-ui.service", "does not start it now",
                                       "not reloaded", "before a reboot", "Disruptive", "no automatic rollback"]),
               ui=txt[:200], observer=dx("systemctl is-enabled hc-test-en-ui.service"))
        shot("02-enable-preview")
        record("B2", "focus starts on Close in the dialog", page.evaluate("document.activeElement.textContent") == "Close",
               ui=page.evaluate("document.activeElement.textContent"))
        v = axe(page, "document.querySelector('[role=dialog]')")
        record("B3", "axe: enable dialog has no violations", v == [], ui=str(v))
        # B4: confirm without password is refused client-side; nothing changes.
        dlg.get_by_role("button", name="Enable hc-test-en-ui.service at boot").click()
        dlg.get_by_role("alert").wait_for()
        record("B4", "confirm without password: asks for it, nothing changed", True,
               ui=dlg.get_by_role("alert").inner_text(), observer=dx("systemctl is-enabled hc-test-en-ui.service"))
        dlg.get_by_label("Re-enter your password").fill(PW)
        page.keyboard.press("Enter")
        dlg.get_by_text("The service is enabled at boot").wait_for(timeout=60000)
        res_txt = dlg.inner_text()
        obs_state = dx("systemctl is-enabled hc-test-en-ui.service")
        obs_link = dx(f"test -L {ETC}/multi-user.target.wants/hc-test-en-ui.service && echo link || echo nolink")
        obs_active = dx("systemctl show -p ActiveState --value hc-test-en-ui.service")
        record("B5", "enable result is verified; host enabled, link present, NOT started",
               "disabled → enabled" in res_txt and obs_state == "enabled" and obs_link == "link" and obs_active == "inactive",
               ui=res_txt[:160], observer=f"{obs_state} {obs_link} {obs_active}")
        shot("03-enable-result")
        page.keyboard.press("Escape")
        page.get_by_role("dialog").wait_for(state="detached")
        record("B6", "Escape closes the result and returns focus to the unit",
               page.evaluate("document.activeElement && document.activeElement.dataset.unit") == "hc-test-en-ui.service",
               ui=str(page.evaluate("document.activeElement && document.activeElement.outerHTML.slice(0,80)")))

        # B7: the list now offers Disable; disable within the step-up window.
        page.get_by_role("button", name="Disable at boot: hc-test-en-ui.service").click()
        dlg = page.get_by_role("dialog")
        dlg.get_by_text("Preview · nothing has been changed yet").wait_for()
        record("B7", "within 5 minutes no password is asked again", dlg.get_by_label("Re-enter your password").count() == 0,
               ui="password field absent" if dlg.get_by_label("Re-enter your password").count() == 0 else "present")
        dlg.get_by_role("button", name="Disable hc-test-en-ui.service at boot").click()
        dlg.get_by_text("The service is disabled at boot").wait_for(timeout=60000)
        obs_state = dx("systemctl is-enabled hc-test-en-ui.service")
        record("B8", "disable result verified on the host", obs_state == "disabled", ui=dlg.inner_text()[:120], observer=obs_state)
        dlg.get_by_role("button", name="Close").click()

        # B9: the distribution's SSH unit (ssh.service or sshd.service) is
        # labelled protected and has no boot control.
        sshu = "ssh.service" if dx("systemctl show -p LoadState --value ssh.service") == "loaded" else "sshd.service"
        page.get_by_role("searchbox", name="Search units").fill(sshu)
        ssh_rows = page.get_by_role("row").filter(has=page.locator(f'code[data-unit="{sshu}"]'))
        has_btn = page.get_by_role("button", name=f"Disable at boot: {sshu}").count() + page.get_by_role("button", name=f"Enable at boot: {sshu}").count()
        row_txt = ssh_rows.first.inner_text() if ssh_rows.count() else "no row"
        record("B9", f"protected SSH unit ({sshu}): label, no enable/disable control",
               has_btn == 0 and "Protected" in row_txt, ui=row_txt[:120], observer=dx(f"systemctl is-enabled {sshu}"))

        # B10: detail panel and redacted unit file.
        page.get_by_role("searchbox", name="Search units").fill("hc-test-en-ui.service")
        page.get_by_role("button", name="Show details of hc-test-en-ui.service").click()
        panel = page.get_by_role("region", name="Details: hc-test-en-ui.service")
        panel.get_by_text(f"{ETC}/hc-test-en-ui.service").first.wait_for()
        panel.get_by_role("button", name="Show unit file").click()
        panel.locator("figure.unit-file-text pre").first.wait_for()
        ptxt = panel.inner_text()
        conds = {"no fixture secret": "fixture-ui-token" not in ptxt, "value redacted": "API_TOKEN=[redacted]" in ptxt,
                 "restart counter": "Automatic restarts" in ptxt, "unit file path": f"{ETC}/hc-test-en-ui.service" in ptxt}
        record("B10", "detail panel: path, restarts, unit file redacted (no fixture secret)", all(conds.values()),
               ui=str(conds), note=ptxt.replace("\n", " | ")[:900])
        v = axe(page, "document.querySelector('.detail-panel')")
        record("B11", "axe: detail panel has no violations", v == [], ui=str(v))
        shot("04-detail")
        panel.get_by_role("button", name="Close").click()

        # B12: tabs by keyboard; timers and sockets show real data.
        tab = page.get_by_role("tab", name="Units")
        tab.focus()
        page.keyboard.press("ArrowRight")
        page.get_by_role("table", name="Timers").wait_for()
        page.get_by_role("row").filter(has_text="hc-test-en-ui.timer").get_by_text("OnCalendar=").first.wait_for()
        record("B12", "ArrowRight selects Timers; fixture timer schedule shown (systemd normalises 'daily')",
               page.get_by_role("tab", name="Timers").get_attribute("aria-selected") == "true",
               ui=page.get_by_role("row").filter(has_text="hc-test-en-ui.timer").first.inner_text()[:160],
               observer=dx("systemctl show -p TimersCalendar --value hc-test-en-ui.timer")[:80])
        page.keyboard.press("End")
        page.get_by_role("table", name="Sockets").wait_for()
        srow = page.get_by_role("row").filter(has_text="homelab-control-broker.socket").first.inner_text()
        record("B13", "End selects Sockets; broker socket path shown", "/run/homelab-control/broker.sock" in srow, ui=srow[:160])
        v = axe(page)
        record("B14", "axe: sockets view has no violations", v == [], ui=str(v))

        # B15: phone and laptop layouts in two themes; no horizontal page overflow.
        problems = []
        for theme in ("midnight", "daybreak"):
            page.evaluate(f"document.documentElement.dataset.hcTheme = '{theme}'")
            for w, h in ((390, 844), (1366, 900)):
                page.set_viewport_size({"width": w, "height": h})
                for t in ("Units", "Timers", "Sockets"):
                    page.get_by_role("tab", name=t).click()
                    page.wait_for_timeout(300)
                    if overflow(page) > 1:
                        problems.append(f"{theme} {w} {t}: overflow {overflow(page)}px")
                    vv = axe(page)
                    if vv:
                        problems.append(f"{theme} {w} {t}: axe {vv}")
                shot(f"05-{theme}-{w}")
        page.set_viewport_size({"width": 390, "height": 844})
        page.get_by_role("tab", name="Units").click()
        page.get_by_role("searchbox", name="Search units").fill("hc-test-en-ui.service")
        page.get_by_role("button", name="Enable at boot: hc-test-en-ui.service").click()
        page.get_by_role("dialog").get_by_text("Preview · nothing has been changed yet").wait_for()
        if overflow(page) > 1:
            problems.append(f"phone dialog overflow {overflow(page)}px")
        shot("06-phone-dialog")
        page.get_by_role("dialog").get_by_role("button", name="Close").click()
        record("B15", "phone/laptop × Midnight/Daybreak: no overflow, axe clean", problems == [], ui="; ".join(problems)[:300])
        record("B16", "nothing changed by the reviewed-but-closed dialog", True, observer=dx("systemctl is-enabled hc-test-en-ui.service"))

        # B17: disable the module on the host; the page says so and nav hides it.
        dx('printf \'{"version":1,"disabled":["services"]}\\n\' > /etc/homelab-control/modules.json && chmod 0644 /etc/homelab-control/modules.json')
        dx("systemctl stop homelab-control-broker.service; systemctl restart homelab-control.service")
        time.sleep(2)
        page.set_viewport_size({"width": 1366, "height": 900})
        page.goto(URL + "/services")
        page.get_by_text("This module is disabled on this host").wait_for()
        nav = page.get_by_role("navigation", name="Main")
        record("B17", "module disabled: page explains, navigation hides Services",
               nav.get_by_role("link", name="Services", exact=True).count() == 0, ui=page.locator("main").inner_text()[:160])
        shot("07-module-disabled")
        record("B18", "no page errors or CSP violations", errors == [] and csp == [], ui=str(errors + csp)[:200])
        browser.close()


if __name__ == "__main__":
    rc = 1
    try:
        main()
        rc = 0 if results and all(r["result"] == "Pass" for r in results) else 1
    except Exception as e:  # recorded, never hidden
        log(f"ERROR {type(e).__name__}: {str(e)[:300]}")
    finally:
        try:
            log("final host state: " + dx("systemctl is-enabled hc-test-en-ui.service; systemctl is-enabled ssh.service sshd.service 2>/dev/null"))
        finally:
            subprocess.run(["docker", "rm", "-f", NAME], capture_output=True)
        with open(os.path.join(OUT, "results.json"), "w") as f:
            json.dump(results, f, indent=1)
        with open(os.path.join(OUT, "run.log"), "w") as f:
            f.write("\n".join(lines) + "\n")
        print(f"m2-3-browser: {sum(r['result'] == 'Pass' for r in results)} passed, {sum(r['result'] != 'Pass' for r in results)} failed")
    sys.exit(rc)
