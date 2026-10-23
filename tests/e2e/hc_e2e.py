#!/usr/bin/env python3

# SPDX-FileCopyrightText: 2026 The Homelab Control Authors
#
# SPDX-License-Identifier: GPL-3.0-or-later

"""Browser end-to-end test for the app shell (Playwright, Chromium): sign-in, overview, the HCIS
responsive shell and appearance preferences.

Driven by tests/integration/run-hc-e2e.sh, which starts a throwaway hc-api.
Env: HC_E2E_URL, HC_E2E_PASSWORD, HC_E2E_ARTIFACTS (screenshot dir, optional),
     HC_LEGACY_URL (optional; screenshots the legacy Webmin login page).
"""
import os
import re
import sys

from playwright.sync_api import expect, sync_playwright

URL = os.environ["HC_E2E_URL"]
PASSWORD = os.environ["HC_E2E_PASSWORD"]
ART = os.environ.get("HC_E2E_ARTIFACTS", "tests/e2e/artifacts")
LEGACY = os.environ.get("HC_LEGACY_URL")
os.makedirs(ART, exist_ok=True)

results = []


def check(name, cond):
    results.append((name, bool(cond)))
    print(("PASS  " if cond else "FAIL  ") + name)


with sync_playwright() as p:
    browser = p.chromium.launch()
    ctx = browser.new_context(viewport={"width": 1440, "height": 900}, color_scheme="dark")
    page = ctx.new_page()
    console_errors = []
    page.on("console", lambda m: console_errors.append(m.text) if m.type == "error" else None)

    page.goto(URL)
    expect(page.get_by_label("Username")).to_be_visible()
    page.keyboard.press("Tab")
    check("keyboard: first Tab focuses the username field",
          page.evaluate("document.activeElement && document.activeElement.id") == "username")
    page.screenshot(path=f"{ART}/01-login.png")

    page.get_by_label("Username").fill("admin")
    page.get_by_label("Password").fill("definitely-wrong")
    page.get_by_role("button", name="Sign in").click()
    expect(page.get_by_role("alert")).to_contain_text("don’t match an account")
    check("wrong password shows a specific, non-revealing error", True)
    page.screenshot(path=f"{ART}/02-login-error.png")

    page.get_by_label("Password").fill(PASSWORD)
    page.get_by_role("button", name="Sign in").click()
    heading = page.locator("h1.faceplate-host")
    expect(heading).to_be_visible(timeout=10000)
    expect(page.get_by_role("heading", name="Filesystems")).to_be_visible()
    host = heading.inner_text()
    real_host = open("/proc/sys/kernel/hostname").read().strip()
    check(f"overview shows the real hostname ({real_host})", host == real_host)
    cookies = {c["name"]: c for c in ctx.cookies()}
    sc = cookies.get("hc_session", {})
    check("session cookie is HttpOnly + SameSite=Strict", sc.get("httpOnly") and sc.get("sameSite") == "Strict")
    check("no element uses inline style attributes", page.locator("[style]").count() == 0)
    page.wait_for_timeout(300)
    page.screenshot(path=f"{ART}/03-overview-desktop.png", full_page=True)

    mobile = browser.new_context(viewport={"width": 390, "height": 844}, color_scheme="dark",
                                 storage_state=ctx.storage_state())
    mp = mobile.new_page()
    mp.goto(URL)
    expect(mp.locator("h1.faceplate-host")).to_be_visible(timeout=10000)
    overflow = mp.evaluate("document.documentElement.scrollWidth > window.innerWidth + 1")
    check("mobile layout has no horizontal page scroll", not overflow)
    mp.screenshot(path=f"{ART}/04-overview-mobile.png", full_page=True)

    light = browser.new_context(viewport={"width": 1440, "height": 900}, color_scheme="light",
                                storage_state=ctx.storage_state())
    lp = light.new_page()
    lp.goto(URL)
    expect(lp.locator("h1.faceplate-host")).to_be_visible(timeout=10000)
    lp.screenshot(path=f"{ART}/05-overview-light.png")

    # HCIS shell (step 3): appearance preferences are independent of the theme and persist.
    layout_js = """() => { const r = (s) => { const b = document.querySelector(s).getBoundingClientRect();
        return [Math.round(b.x), Math.round(b.width)]; };
      return {nav: r('#main-nav'), main: r('main'), sidebar: document.querySelector('.shell').dataset.sidebar,
              links: [...document.querySelectorAll('#main-nav a')].map(a => a.textContent)}; }"""
    attrs_js = "() => Object.fromEntries([...document.documentElement.attributes].map(a => [a.name, a.value]))"
    before = page.evaluate(layout_js)
    appearance = page.get_by_role("button", name="Appearance")
    appearance.focus()
    page.keyboard.press("Enter")
    menu = page.get_by_role("dialog", name="Appearance")
    expect(menu).to_be_visible()
    check("appearance: Enter opens the menu with focus on the current theme",
          page.evaluate("document.activeElement.type === 'radio' && document.activeElement.checked"))
    menu.get_by_role("group", name="Theme").get_by_role("radio", name=re.compile("^Graphite")).check()
    page.wait_for_timeout(100)
    check("appearance: theme applies without changing layout or navigation",
          page.evaluate(attrs_js).get("data-hc-theme") == "graphite" and page.evaluate(layout_js) == before)
    menu.get_by_role("group", name="Density").get_by_role("radio", name="Compact").check()
    menu.get_by_role("group", name="Text size").get_by_role("radio", name="112%").check()
    page.keyboard.press("Escape")
    expect(menu).to_be_hidden()
    check("appearance: Escape closes the menu and returns focus to its button",
          page.evaluate("document.activeElement.getAttribute('aria-haspopup')") == "dialog")
    page.reload()
    expect(heading).to_be_visible(timeout=10000)
    a = page.evaluate(attrs_js)
    check("appearance: preferences persist after reload",
          a.get("data-hc-theme") == "graphite" and a.get("data-hc-density") == "compact"
          and a.get("data-hc-text-scale") == "112")
    check("appearance: no inline style attributes", page.locator("[style]").count() == 0)
    page.screenshot(path=f"{ART}/06-shell-graphite-compact.png")
    page.get_by_role("button", name="Collapse sidebar").click()
    check("sidebar: collapses to the icon rail", page.evaluate(layout_js)["sidebar"] == "rail")
    page.reload()
    expect(heading).to_be_visible(timeout=10000)
    check("sidebar: collapsed state persists", page.evaluate(layout_js)["sidebar"] == "rail")
    page.get_by_role("button", name="Expand sidebar").click()
    page.get_by_role("button", name="Appearance").click()
    page.get_by_role("button", name="Reset to defaults").click()
    page.keyboard.press("Escape")
    check("appearance: reset restores the System theme",
          "data-hc-theme" not in page.evaluate(attrs_js) and page.evaluate(layout_js)["sidebar"] == "expanded")

    # Responsive shell: no page-level horizontal scroll at any size; drawer on phones.
    for w, h, mode in [(390, 844, "drawer"), (768, 1024, "rail"), (1024, 768, "rail"),
                       (1366, 768, "expanded"), (2560, 1080, "expanded")]:
        rc = browser.new_context(viewport={"width": w, "height": h}, storage_state=ctx.storage_state())
        rp = rc.new_page()
        rp.goto(URL)
        expect(rp.locator("h1.faceplate-host")).to_be_visible(timeout=10000)
        check(f"responsive {w}px: {mode} navigation, no horizontal page scroll",
              rp.evaluate("document.querySelector('.shell').dataset.sidebar") == mode
              and not rp.evaluate("document.documentElement.scrollWidth > window.innerWidth + 1"))
        if mode == "drawer":
            opener = rp.get_by_role("button", name="Open navigation")
            opener.click()
            nav = rp.get_by_role("navigation", name="Main")
            expect(nav.get_by_role("link", name="Overview")).to_be_visible()
            check("drawer: focus moves into the navigation",
                  rp.evaluate("document.getElementById('main-nav').contains(document.activeElement)"))
            rp.screenshot(path=f"{ART}/07-drawer-mobile.png")
            rp.keyboard.press("Escape")
            check("drawer: Escape closes it and returns focus to the menu button",
                  rp.evaluate("!document.querySelector('.shell').dataset.drawer")
                  and rp.evaluate("document.activeElement.getAttribute('aria-controls')") == "main-nav")
        rc.close()

    page.get_by_role("button", name="Sign out").click()
    expect(page.get_by_label("Username")).to_be_visible()
    check("sign out returns to the login form", True)

    csp = [e for e in console_errors if "Content Security Policy" in e or "Refused to" in e]
    check("no CSP violations in the console", not csp)
    for e in csp:
        print("   CSP:", e)

    if LEGACY:
        lg = browser.new_context(viewport={"width": 1280, "height": 800}).new_page()
        lg.goto(LEGACY)
        lg.screenshot(path=f"{ART}/06-legacy-webmin-login.png")
        check("legacy Webmin login page reachable", "session_login.cgi" in lg.content())
    browser.close()

failed = [n for n, ok in results if not ok]
print(f"browser e2e: {len(results) - len(failed)} passed, {len(failed)} failed")
sys.exit(1 if failed else 0)
