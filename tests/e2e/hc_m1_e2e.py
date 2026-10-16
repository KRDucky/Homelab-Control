#!/usr/bin/env python3
"""M1 browser e2e: capability-aware navigation, Services, Hardware, Legacy,
and graceful degradation when the broker stops. Driven by
tests/e2e/run-m1-e2e.sh against a real stack in a disposable systemd host.

Env: HC_E2E_URL, HC_E2E_PASSWORD, HC_E2E_ARTIFACTS, HC_E2E_BROKER_STOP (shell
command run by the harness to stop the broker; test-harness only).
"""
import os
import subprocess
import sys

from playwright.sync_api import expect, sync_playwright

URL = os.environ["HC_E2E_URL"]
PASSWORD = os.environ["HC_E2E_PASSWORD"]
ART = os.environ.get("HC_E2E_ARTIFACTS", "tests/e2e/artifacts")
STOP = os.environ.get("HC_E2E_BROKER_STOP")
AXE = os.path.join(os.path.dirname(__file__), "../../frontend/app/node_modules/axe-core/axe.min.js")
os.makedirs(ART, exist_ok=True)
results = []
axe_report = []


def axe(page, name):
    """Run axe-core (WCAG 2.1 A/AA rules) on the current page; serious/critical violations fail."""
    if not os.path.exists(AXE):
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
    check("nav: Services, Hardware, Legacy Webmin are links", all(
        nav.get_by_role("link", name=n).count() == 1 for n in ["Overview", "Services", "Hardware", "Legacy Webmin"]))

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

    nav.get_by_role("link", name="Hardware").click()
    expect(page.get_by_role("heading", name="Storage devices")).to_be_visible(timeout=15000)
    check("hardware: disk identity confidence shown", page.get_by_text("Medium — serial number").count() >= 1
          or page.get_by_text("High — WWN").count() >= 1)
    check("hardware: kernel names labelled volatile", page.get_by_text("changes between boots").count() >= 1)
    check("hardware: missing DMI explained, not invented", page.get_by_text("No SMBIOS/DMI tables").count() >= 1)
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

failed = [n for n, ok in results if not ok]
print(f"m1 browser e2e: {len(results) - len(failed)} passed, {len(failed)} failed")
sys.exit(1 if failed else 0)
