#!/usr/bin/env python3
# SPDX-FileCopyrightText: 2026 The Homelab Control Authors
# SPDX-License-Identifier: GPL-3.0-or-later
"""Run the Node fixture matrix with the already pinned Python Playwright driver, no extra package."""
import os
from pathlib import Path
import subprocess
import playwright

root = Path(__file__).resolve().parents[2]
driver = Path(playwright.__file__).parent / 'driver'
for engine in os.environ.get('HC_CSP_BROWSERS', 'chromium').split(','):
    env = dict(os.environ, HC_BROWSER=engine.strip(), HC_PLAYWRIGHT_MODULE=str(driver / 'package' / 'index.mjs'))
    env['HC_E2E_ARTIFACTS'] = str(root / 'work' / 'hcis-ui-e2e' / engine.strip())
    subprocess.run([str(driver / 'node'), str(root / 'tests/e2e/hcis-ui.mjs')], cwd=root, env=env, check=True)
