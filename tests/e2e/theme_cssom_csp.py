#!/usr/bin/env python3

# SPDX-FileCopyrightText: 2026 The Homelab Control Authors
#
# SPDX-License-Identifier: GPL-3.0-or-later

"""Feasibility probe for the planned theme engine (docs/ui/09-THEME-SECURITY.md §9.2, ADR 0010).

Loads the real hc-api UI page, served with hc-api's real Content-Security-Policy, and checks in each
browser engine that:
  1. setting a CSS custom property through the CSSOM (documentElement.style.setProperty) applies
     and causes no CSP violation  -> the planned way to apply themes works without 'unsafe-inline';
  2. a CSSOM write to an element's style applies without violation;
  3. a <style> element is blocked and reported (negative control: the CSP is in force);
  4. a style="" attribute set via setAttribute is blocked and reported (negative control).
Constructable stylesheets (adoptedStyleSheets) are reported for information only.

Read-only: it only loads the page; no login, no API writes.
Env: HC_E2E_URL (base URL, e.g. http://127.0.0.1:9180), HC_CSP_BROWSERS (default "chromium").
"""
import json
import os
import sys

from playwright.sync_api import sync_playwright

BASE = os.environ.get("HC_E2E_URL", "http://127.0.0.1:9180").rstrip("/")
BROWSERS = [b.strip() for b in os.environ.get("HC_CSP_BROWSERS", "chromium").split(",") if b.strip()]

LISTEN = """() => {
  window.__hcViolations = [];
  document.addEventListener('securitypolicyviolation', (e) =>
    window.__hcViolations.push({ directive: e.effectiveDirective || e.violatedDirective, sample: e.sample || '' }));
}"""

PROBE = """async () => {
  const wait = () => new Promise((r) => setTimeout(r, 400));
  const out = {};
  const root = document.documentElement;
  const v0 = window.__hcViolations.length;

  // 1. Theme path: custom property on :root via CSSOM.
  const before = getComputedStyle(document.body).backgroundColor;
  root.style.setProperty('--hc-color-ink', '#123456');
  await wait();
  out.customProperty = {
    before, after: getComputedStyle(document.body).backgroundColor,
    value: getComputedStyle(root).getPropertyValue('--hc-color-ink').trim(),
    violations: window.__hcViolations.length - v0,
  };
  root.style.removeProperty('--hc-color-ink');
  out.customProperty.restored = getComputedStyle(document.body).backgroundColor === before;

  // 2. CSSOM write on an element.
  const a = document.createElement('div'); document.body.appendChild(a);
  const v1 = window.__hcViolations.length;
  a.style.color = 'rgb(1, 2, 3)';
  await wait();
  out.elementCssom = { color: getComputedStyle(a).color, violations: window.__hcViolations.length - v1 };

  // 3. Negative control: <style> element.
  const v2 = window.__hcViolations.length;
  const st = document.createElement('style');
  st.textContent = 'body { outline: 7px solid rgb(9, 8, 7); }';
  document.head.appendChild(st);
  await wait();
  out.styleElement = { outlineWidth: getComputedStyle(document.body).outlineWidth,
                       violations: window.__hcViolations.slice(v2) };
  st.remove();

  // 4. Negative control: style attribute via setAttribute.
  const b = document.createElement('div'); document.body.appendChild(b);
  const v3 = window.__hcViolations.length;
  b.setAttribute('style', 'color: rgb(4, 5, 6)');
  await wait();
  out.styleAttribute = { color: getComputedStyle(b).color, violations: window.__hcViolations.slice(v3) };

  // 5. Information only: constructable stylesheet.
  try {
    const v4 = window.__hcViolations.length;
    const sheet = new CSSStyleSheet();
    sheet.replaceSync('.hc-probe-adopted { color: rgb(7, 7, 7); }');
    document.adoptedStyleSheets = [...document.adoptedStyleSheets, sheet];
    const c = document.createElement('div'); c.className = 'hc-probe-adopted'; document.body.appendChild(c);
    await wait();
    out.adoptedStyleSheet = { applied: getComputedStyle(c).color === 'rgb(7, 7, 7)',
                              violations: window.__hcViolations.length - v4 };
  } catch (e) { out.adoptedStyleSheet = { error: String(e) }; }
  a.remove(); b.remove();
  return out;
}"""


def run(p, name):
    browser = getattr(p, name).launch()
    try:
        page = browser.new_page()
        page.add_init_script(f"({LISTEN})()")
        resp = page.goto(BASE + "/", wait_until="load")
        csp = resp.headers.get("content-security-policy", "")
        r = page.evaluate(PROBE)
        r["browser"], r["version"], r["csp"] = name, browser.version, csp
    finally:
        browser.close()
    checks = [
        ("CSP header present, no 'unsafe-inline'", "style-src 'self'" in csp and "unsafe-inline" not in csp),
        ("CSSOM custom property applied", r["customProperty"]["after"] == "rgb(18, 52, 86)"
         and r["customProperty"]["value"] == "#123456"),
        ("CSSOM custom property: no CSP violation", r["customProperty"]["violations"] == 0),
        ("custom property removal restores theme", r["customProperty"]["restored"]),
        ("CSSOM element style applied without violation", r["elementCssom"]["color"] == "rgb(1, 2, 3)"
         and r["elementCssom"]["violations"] == 0),
        ("<style> element blocked (control)", r["styleElement"]["outlineWidth"] != "7px"),
        ("<style> element reported (control)", len(r["styleElement"]["violations"]) > 0),
        ("style attribute blocked (control)", r["styleAttribute"]["color"] != "rgb(4, 5, 6)"),
        ("style attribute reported (control)", len(r["styleAttribute"]["violations"]) > 0),
    ]
    return r, checks


def main():
    fails = 0
    with sync_playwright() as p:
        for name in BROWSERS:
            r, checks = run(p, name)
            print(f"== {name} {r['version']}")
            for label, ok in checks:
                print(f"{'PASS' if ok else 'FAIL'}  {label}")
                fails += 0 if ok else 1
            print("info  adoptedStyleSheets:", json.dumps(r["adoptedStyleSheet"]))
            if os.environ.get("GITHUB_ACTIONS"):  # annotations stay readable through the checks API
                passed = sum(ok for _, ok in checks)
                print(f"::notice title=theme CSSOM/CSP probe::{name} {r['version']}: {passed}/{len(checks)} checks passed")
            print("info  violations reported:", json.dumps(
                [v["directive"] for v in r["styleElement"]["violations"] + r["styleAttribute"]["violations"]]))
    print(f"theme CSSOM/CSP probe: {'OK' if fails == 0 else f'{fails} failed'} ({', '.join(BROWSERS)})")
    sys.exit(1 if fails else 0)


if __name__ == "__main__":
    main()
