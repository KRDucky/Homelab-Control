#!/usr/bin/env python3
# SPDX-FileCopyrightText: 2026 The Homelab Control Authors
#
# SPDX-License-Identifier: MIT
"""Generate hcis-tokens.css from tokens.json. Usage: python3 -I gen-css.py [--check]"""
import json
import pathlib
import re
import sys

HERE = pathlib.Path(__file__).resolve().parent


def render(t):
    themes = [x['id'] for x in t['color']['themes']]

    def val(tok, theme):
        v = tok['value']
        if isinstance(v, str):
            v = {themes[0]: v}
        x = v.get(theme, v[themes[0]])
        m = re.fullmatch(r'\{(.+)\}', x)
        return f'var(--{m.group(1)})' if m else x

    out = ['/*\n * SPDX-FileCopyrightText: 2026 The Homelab Control Authors\n *\n * SPDX-License-Identifier: MIT\n */\n',
           '/* HCIS — generated from tokens.json (docs/ui/hcis/tokens/tokens.json). Reference only: not imported by\n'
           ' * frontend/app. Themes: ' + ', '.join(themes) + '. Select with data-theme on <html> or any element.\n'
           ' * Density, transparency and motion are layout preferences set by components/reference/hcis.css. */\n']
    for i, th in enumerate(themes):
        sel = f':root,\n[data-theme="{th}"]' if i == 0 else f'[data-theme="{th}"]'
        lines = [f'  --{k["name"]}: {val(k, th)};' for k in t['color']['tokens'] + t['shadow']['tokens']]
        out.append(sel + ' {\n' + '\n'.join(lines) + '\n}\n')
    root = []
    for fam in ['spacing', 'radius', 'fontSize', 'fontWeight', 'density', 'glass', 'duration']:
        root += [f'  --{k["name"]}: {k["value"]};' for k in t[fam]['tokens']]
    for k, v in t['type']['families'].items():
        root.append(f'  --font-{k}: {v};')
    out.append(':root {\n' + '\n'.join(root) + '\n}\n')
    out.append('/* Type styles */')
    for g in t['type']['groups']:
        for s in g['styles']:
            fam = s.get('family', g['family'])
            ls = f" letter-spacing: {s['letterSpacing']};" if 'letterSpacing' in s else ''
            out.append(f".{s['name']} {{ font-family: var(--font-{fam}); font-size: {s['fontSize']}; "
                       f"line-height: {s['lineHeight']}; font-weight: {s['fontWeight']};{ls} }}")
    return '\n'.join(out) + '\n'


def main():
    css = render(json.loads((HERE / 'tokens.json').read_text()))
    target = HERE / 'hcis-tokens.css'
    if '--check' in sys.argv:
        if target.read_text() != css:
            sys.exit('hcis-tokens.css is out of date; run gen-css.py')
        print('OK: hcis-tokens.css matches tokens.json')
    else:
        target.write_text(css)


if __name__ == '__main__':
    main()
