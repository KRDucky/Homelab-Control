#!/usr/bin/env python3

# SPDX-FileCopyrightText: 2026 The Homelab Control Authors
#
# SPDX-License-Identifier: GPL-3.0-or-later

"""M1 browser e2e: capability-aware navigation, Services, Hardware, Legacy,
and graceful degradation when the broker stops. Driven by
tests/e2e/run-m1-e2e.sh against a real stack in a disposable systemd host.

Env: HC_E2E_URL, HC_E2E_PASSWORD, HC_E2E_ARTIFACTS, HC_E2E_BROKER_STOP (shell
command run by the harness to stop the broker; test-harness only),
HC_E2E_REQUIRE_AXE=1 (a missing axe-core is a failure, not a skip).
"""
import os
import subprocess
import sys

from playwright.sync_api import expect, sync_playwright

URL = os.environ["HC_E2E_URL"]
PASSWORD = os.environ["HC_E2E_PASSWORD"]
ART = os.environ.get("HC_E2E_ARTIFACTS", "tests/e2e/artifacts")
STOP = os.environ.get("HC_E2E_BROKER_STOP")
REQUIRE_AXE = os.environ.get("HC_E2E_REQUIRE_AXE") == "1"
AXE = os.path.join(os.path.dirname(__file__), "../../frontend/app/node_modules/axe-core/axe.min.js")
os.makedirs(ART, exist_ok=True)
results = []
axe_report = []


def axe(page, name):
    """Run axe-core (WCAG 2.1 A/AA rules) on the current page; serious/critical violations fail."""
    if not os.path.exists(AXE):
        if REQUIRE_AXE:
            check(f"a11y {name}: axe-core available (HC_E2E_REQUIRE_AXE=1)", False)
        else:
            print("SKIP  axe (frontend/app/node_modules/axe-core missing)")
        return
    # Evaluated over the DevTools protocol: the page's strict CSP (correctly)
    # refuses injected <script> tags, and the test must not weaken it.
    page.evaluate(open(AXE, encoding="utf-8").read() + "\n;0")
    res = page.evaluate("""async () => (await axe.run(document, {runOnly: {type: 'tag', values: ['wcag2a','wcag2aa','wcag21a','wcag21aa']}})).violations
        .map(v => ({id: v.id, impact: v.impact, nodes: v.nodes.length}))""")
    bad = [v for v in res if v["impact"] in ("serious", "critical")]
    axe_report.append((name, res))
    check(f"a11y {name}: no serious/critical WCAG 2.1 AA violations" + (f" {bad}" if bad else ""), not bad)
    for v in res:
        if v not in bad:
            print(f"      minor/moderate: {v}")


def check(name, cond):
    results.append((name, bool(cond)))
    print(("PASS  " if cond else "FAIL  ") + name)


with sync_playwright() as p:
    browser = p.chromium.launch()
    ctx = browser.new_context(viewport={"width": 1440, "height": 900}, color_scheme="dark")
    page = ctx.new_page()
    errors = []
    page.on("console", lambda m: errors.append(m.text) if m.type == "error" else None)

    page.goto(URL)
    page.get_by_label("Username").fill("admin")
    page.get_by_label("Password").fill(PASSWORD)
    page.get_by_role("button", name="Sign in").click()
    nav = page.get_by_role("navigation", name="Main")
    expect(nav.get_by_role("link", name="Services")).to_be_visible(timeout=15000)
    check("nav: Services, Hardware, Legacy Webmin, Audit are links", all(
        nav.get_by_role("link", name=n).count() == 1 for n in ["Overview", "Services", "Hardware", "Legacy Webmin", "Audit"]))

    nav.get_by_role("link", name="Services").click()
    expect(page.get_by_role("heading", name="Services", level=1)).to_be_visible()
    expect(page.get_by_role("table")).to_be_visible(timeout=15000)
    check("services: URL is /services (client-side routing)", page.url.endswith("/services"))
    table = page.get_by_role("table")
    check("services: homelab-control.service listed", table.get_by_text("homelab-control.service").count() >= 1)
    check("services: broker listed", table.get_by_text("homelab-control-broker.service").count() >= 1)
    page.get_by_label("Search units").fill("homelab")
    check("services: search filters", page.get_by_text("Showing 2 of").count() == 1 or page.get_by_text("Showing 3 of").count() == 1)
    page.get_by_label("Search units").fill("")
    page.screenshot(path=f"{ART}/m1-01-services.png", full_page=False)
    axe(page, "services")

    # M2 PR-1b: read-only service logs (account has services.logs).
    check("nav: one Services entry although service logs share the page",
          nav.get_by_text("Service logs").count() == 0)
    page.get_by_label("Search units").fill("homelab-control.service")
    page.get_by_role("button", name="Show logs for homelab-control.service").click()
    panel = page.get_by_role("region", name="Logs: homelab-control.service")
    expect(panel.get_by_text("listening on 127.0.0.1:9180").first).to_be_visible(timeout=15000)
    check("logs: real journal entries shown for homelab-control.service",
          panel.get_by_role("row").count() >= 2)
    check("logs: panel heading receives focus", page.evaluate("document.activeElement?.id") == "h-logs")
    panel.get_by_label("Entries").select_option("50")
    expect(panel.get_by_text("most recent")).to_be_visible(timeout=15000)
    panel.locator(".logs-wrap").scroll_into_view_if_needed()
    page.screenshot(path=f"{ART}/m2-01-service-logs.png", full_page=True)
    axe(page, "services+logs")
    panel.get_by_role("button", name="Close").click()
    check("logs: panel closes", page.get_by_role("region", name="Logs: homelab-control.service").count() == 0)
    page.get_by_label("Search units").fill("")

    # M2 PR-1a: Audit page (audit.read) and own sessions.
    nav.get_by_role("link", name="Audit").click()
    expect(page.get_by_role("heading", name="Audit", level=1)).to_be_visible(timeout=15000)
    table = page.get_by_role("table")
    expect(table.get_by_text("services.logs.read").first).to_be_visible(timeout=15000)
    check("audit: sign-in recorded", table.get_by_text("auth.login").count() >= 1)
    check("audit: log read recorded with unit, no content",
          table.get_by_text("homelab-control.service lines=50").count() >= 1 and table.get_by_text("listening on").count() == 0)
    page.screenshot(path=f"{ART}/m2-02-audit.png", full_page=False)
    axe(page, "audit")
    page.get_by_role("link", name="Sessions").click()
    expect(page.get_by_role("heading", name="Sessions", level=1)).to_be_visible(timeout=15000)
    check("sessions: this browser listed", page.get_by_text("this browser").count() == 1)
    check("sessions: persistent", page.get_by_text("Sessions survive a restart").count() == 1)
    page.screenshot(path=f"{ART}/m2-03-sessions.png", full_page=False)
    axe(page, "sessions")
    nav.get_by_role("link", name="Services").click()
    expect(page.get_by_role("table")).to_be_visible(timeout=15000)

    nav.get_by_role("link", name="Hardware").click()
    expect(page.get_by_role("heading", name="Storage devices")).to_be_visible(timeout=15000)
    # Compare the page with what the API reports for THIS host, so the checks hold on bare metal,
    # containers (host disks, no DMI) and VMs (virtio disks without serials, QEMU SMBIOS) alike:
    # the UI must show exactly the backend's identity confidence and DMI state, inventing nothing.
    hw = page.evaluate("async () => (await fetch('/api/v1/hardware', {credentials: 'same-origin'})).json()")
    conf_text = {"high": "High — WWN", "medium": "Medium — serial number",
                 "low": "Low — by-id link only", "none": "None — no stable identifier found"}
    disks = hw["inventory"]["disks"].get("devices") or []
    levels = {d["identity_confidence"] for d in disks}
    check(f"hardware: API reports disks ({len(disks)})", len(disks) >= 1)
    check(f"hardware: each reported identity confidence shown ({sorted(levels)})",
          all(page.get_by_text(conf_text[lv]).count() >= 1 for lv in levels))
    check("hardware: no identity confidence shown that the API did not report",
          all(page.get_by_text(t).count() == 0 for lv, t in conf_text.items() if lv not in levels))
    check("hardware: kernel names labelled volatile", page.get_by_text("changes between boots").count() >= 1)
    sysinfo = hw["inventory"]["system"]
    no_dmi = any("No SMBIOS/DMI tables" in n for n in (sysinfo.get("notes") or []))
    if no_dmi:
        check("hardware: missing DMI explained, not invented", page.get_by_text("No SMBIOS/DMI tables").count() >= 1)
    else:
        check(f"hardware: DMI present, model shown ({sysinfo.get('product')!r})",
              bool(sysinfo.get("product")) and page.get_by_text(sysinfo["product"]).count() >= 1)
        check("hardware: no 'missing DMI' note when DMI is present", page.get_by_text("No SMBIOS/DMI tables").count() == 0)
    page.screenshot(path=f"{ART}/m1-02-hardware.png", full_page=True)
    axe(page, "hardware")

    nav.get_by_role("link", name="Legacy Webmin").click()
    expect(page.get_by_role("heading", name="Security review")).to_be_visible(timeout=15000)
    check("legacy: version 2.670 from real Webmin", page.get_by_text("2.670", exact=True).count() >= 1)
    check("legacy: plain-HTTP finding shown", page.get_by_text("serves plain HTTP").count() == 1)
    check("legacy: RPC finding shown", page.get_by_text("RPC-capable accounts").count() == 1)
    page.screenshot(path=f"{ART}/m1-03-legacy.png", full_page=True)
    axe(page, "legacy")

    page.go_back()
    expect(page.get_by_role("heading", name="Storage devices")).to_be_visible()
    check("history: back button returns to Hardware", page.url.endswith("/hardware"))
    page.reload()
    expect(page.get_by_role("heading", name="Storage devices")).to_be_visible(timeout=15000)
    check("deep link: reload on /hardware works", True)

    m = browser.new_context(viewport={"width": 390, "height": 844}, color_scheme="dark", storage_state=ctx.storage_state())
    mp = m.new_page()
    mp.goto(URL + "/services")
    expect(mp.get_by_role("table")).to_be_visible(timeout=15000)
    check("mobile: no page-level horizontal scroll", not mp.evaluate("document.documentElement.scrollWidth > window.innerWidth + 1"))
    mp.screenshot(path=f"{ART}/m1-04-services-mobile.png")

    if STOP:
        subprocess.run(STOP, shell=True, check=True)  # harness-controlled
        page.wait_for_timeout(6000)  # API caches broker state for 5 s
        page.goto(URL + "/")
        expect(page.get_by_text("Privileged helper not running")).to_be_visible(timeout=40000)
        nav = page.get_by_role("navigation", name="Main")
        check("degraded: Services is no longer a link", nav.get_by_role("link", name="Services").count() == 0)
        check("degraded: reason explains how to start the helper", nav.get_by_text("systemctl enable --now homelab-control-broker.socket").count() >= 1)
        check("degraded: Hardware still usable but marked Limited", "Limited" in nav.get_by_role("link", name="Hardware").inner_text())
        expect(page.locator("h1.faceplate-host")).to_be_visible(timeout=15000)
        page.screenshot(path=f"{ART}/m1-05-broker-stopped.png")
        axe(page, "overview+degraded-nav")

    csp = [e for e in errors if "Content Security Policy" in e or "Refused to" in e]
    check("no CSP violations", not csp)
    browser.close()

if axe_report:
    import json
    with open(os.path.join(ART, "axe-report.json"), "w", encoding="utf-8") as f:
        json.dump([{"page": n, "violations": v} for n, v in axe_report], f, indent=2)
failed = [n for n, ok in results if not ok]
print(f"m1 browser e2e: {len(results) - len(failed)} passed, {len(failed)} failed")
sys.exit(1 if failed else 0)
