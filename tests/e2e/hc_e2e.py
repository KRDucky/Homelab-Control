#!/usr/bin/env python3

# SPDX-FileCopyrightText: 2026 The Homelab Control Authors
#
# SPDX-License-Identifier: GPL-3.0-or-later

"""Browser end-to-end test for the M0 shell (Playwright, Chromium).

Driven by tests/integration/run-hc-e2e.sh, which starts a throwaway hc-api.
Env: HC_E2E_URL, HC_E2E_PASSWORD, HC_E2E_ARTIFACTS (screenshot dir, optional),
     HC_LEGACY_URL (optional; screenshots the legacy Webmin login page).
"""
import os
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
