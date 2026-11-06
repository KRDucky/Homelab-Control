#!/usr/bin/env python3
# SPDX-FileCopyrightText: 2026 The Homelab Control Authors
#
# SPDX-License-Identifier: GPL-3.0-or-later

"""OIDC single sign-on in a real browser against the isolated loopback fixture.

Starts backend/internal/oidclogin/testidp/fixture (test IdP on https://127.0.0.1,
real hc-api Server + embedded UI on http://localhost), then Playwright Chromium
clicks "Sign in with single sign-on" and checks that the cross-site round trip
ends in the ordinary session (SameSite cookies as a browser applies them).
No host changes, no external network. Needs: make frontend, python3-playwright,
/usr/bin/chromium (or HC_CHROMIUM).
"""
import json
import os
import subprocess
import sys

from playwright.sync_api import sync_playwright

ROOT = os.path.abspath(os.path.join(os.path.dirname(__file__), "../.."))
fx = subprocess.Popen(["go", "run", "./internal/oidclogin/testidp/fixture"], cwd=os.path.join(ROOT, "backend"),
                      stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True)
fails = []


def check(name, ok, detail=""):
    print(("PASS " if ok else "FAIL ") + name + (f" ({detail})" if detail and not ok else ""), flush=True)
    if not ok:
        fails.append(name)


try:
    urls = json.loads(fx.stdout.readline())
    base = urls["url"]
    with sync_playwright() as p:
        b = p.chromium.launch(executable_path=os.environ.get("HC_CHROMIUM", "/usr/bin/chromium"))
        # The test IdP's certificate is a throwaway httptest CA (fixture only).
        ctx = b.new_context(ignore_https_errors=True)
        page = ctx.new_page()
        page.goto(base + "/")
        link = page.get_by_role("link", name="Sign in with single sign-on")
        link.wait_for(timeout=10000)
        check("local login form still shown", page.get_by_label("Password").is_visible())
        link.click()
        # The user acts on the provider's page (a cross-site initiator, like a real login form).
        page.get_by_role("link", name="Continue").click(timeout=10000)
        page.wait_for_url(base + "/", timeout=15000)
        page.get_by_text("alice").first.wait_for(timeout=10000)
        check("signed in as bound local account after cross-site round trip", True)
        cookies = {c["name"]: c for c in ctx.cookies()}
        s = cookies.get("hc_session")
        check("session cookie HttpOnly SameSite=Strict", bool(s and s["httpOnly"] and s["sameSite"] == "Strict"), str(s and s["sameSite"]))
        check("binding cookie cleared", "hc_oidc" not in cookies)
        r = page.evaluate("fetch('/api/v1/auth/session').then(r => r.json())")
        check("SPA fetch carries the Strict session cookie", r.get("username") == "alice" and "step_up_until" not in r)
        # Replaying the provider callback in the same browser must not sign in again.
        ctx.clear_cookies()
        page.goto(base + "/api/v1/auth/oidc/callback?state=forged&code=x")
        check("forged callback lands on fixed failure URL", page.url == base + "/?login=sso_failed", page.url)
        page.get_by_role("alert").wait_for(timeout=5000)
        check("failure explained, local login offered", "local account" in page.get_by_role("alert").inner_text())
        b.close()
finally:
    fx.stdin.close()
    fx.terminate()
    audit = fx.stderr.read()
    check("audit has success and failure, no tokens", '"auth.oidc.login","user":"alice"' in audit and "unknown or expired state" in audit
          and "test-access-token" not in audit and "eyJ" not in audit)
print("RESULT", "FAIL" if fails else "PASS", f"{len(fails)} failed")
sys.exit(1 if fails else 0)
