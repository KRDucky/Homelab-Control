#!/usr/bin/env python3
# SPDX-FileCopyrightText: 2026 The Homelab Control Authors
#
# SPDX-License-Identifier: MIT
"""Generate CSS from tokens.json.

- docs/ui/hcis/tokens/hcis-tokens.css: the full reference stylesheet (all token families).
- frontend/design-system/tokens.css, between the "BEGIN/END generated: HCIS colour roles" markers:
  the HCIS semantic colours for the shipped app (hex values, one block per theme) and the M1
  colour names as aliases (migration step 1, docs/ui/hcis/README.md).
- frontend/design-system/tokens.css, between the "BEGIN/END generated: HCIS typography" markers:
  the four font-family stacks as --hc-font-<family> and the M1 name --hc-font-family as an alias of
  --hc-font-ui (migration step 2). Typography is the same in every theme.

Usage: python3 -I gen-css.py [--check]
"""
import json
import pathlib
import re
import sys

HERE = pathlib.Path(__file__).resolve().parent
APP_TOKENS = HERE.parents[3] / 'frontend' / 'design-system' / 'tokens.css'
BEGIN = '/* BEGIN generated: HCIS colour roles (docs/ui/hcis/tokens/gen-css.py) */'
END = '/* END generated: HCIS colour roles */'
TYPE_BEGIN = '/* BEGIN generated: HCIS typography (docs/ui/hcis/tokens/gen-css.py) */'
TYPE_END = '/* END generated: HCIS typography */'
# Selectors per theme. Midnight is the default; Daybreak follows a light OS preference and Graphite
# a dark one with prefers-contrast: more, unless data-hc-theme names a theme. 'dark' keeps the M1
# meaning (force the default dark theme).
APP_THEMES = [
    ('midnight', 'dark', [":root", ":root[data-hc-theme='midnight']", ":root[data-hc-theme='dark']"], None),
    ('daybreak', 'light', [":root:not([data-hc-theme])"], '(prefers-color-scheme: light)'),
    ('graphite', 'dark', [":root:not([data-hc-theme])"], '(prefers-color-scheme: dark) and (prefers-contrast: more)'),
    ('daybreak', 'light', [":root[data-hc-theme='daybreak']", ":root[data-hc-theme='light']"], None),
    ('graphite', 'dark', [":root[data-hc-theme='graphite']"], None),
    ('oled', 'dark', [":root[data-hc-theme='oled']"], None),
]


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
    for fam in ['spacing', 'radius', 'fontSize', 'fontWeight', 'textScale', 'density', 'glass', 'duration']:
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


def resolve(tokens, name, theme, first):
    t = tokens[name]
    v = t['value'] if isinstance(t['value'], str) else t['value'].get(theme, t['value'][first])
    m = re.fullmatch(r'\{(.+)\}', v)
    return resolve(tokens, m.group(1), theme, first) if m else v


def render_app(t):
    first = t['color']['themes'][0]['id']
    tokens = {x['name']: x for x in t['color']['tokens']}
    semantic = [n for n, x in tokens.items() if not n.startswith('hc-palette-') and not isinstance(x['value'], str)]
    aliases = [(n, x['value'][1:-1]) for n, x in tokens.items() if isinstance(x['value'], str) and n.startswith('hc-color-')]
    out = [BEGIN, '/* HCIS semantic colours (docs/ui/hcis). Edit tokens.json and regenerate; do not edit by hand. */']
    for theme, scheme, sels, media in APP_THEMES:
        ind = '  ' if media else ''
        body = [f'{ind}  color-scheme: {scheme};'] + [f'{ind}  --{n}: {resolve(tokens, n, theme, first)};' for n in semantic]
        block = f'{ind}' + f',\n{ind}'.join(sels) + ' {\n' + '\n'.join(body) + f'\n{ind}}}'
        out.append(f'@media {media} {{\n{block}\n}}' if media else block)
    out.append('/* M1 colour names, kept for existing styles (docs/ui/02 §2.6). New code uses the semantic names. */')
    out.append(':root {\n' + '\n'.join(f'  --{n}: var(--{a});' for n, a in aliases) + '\n}')
    out.append(END)
    return '\n'.join(out)


def render_app_type(t):
    fams = t['type']['families']
    lines = [f'  --hc-font-{k}: {v};' for k, v in fams.items()]
    lines.append('  /* M1 name, kept for existing styles. New code uses --hc-font-ui / --hc-font-mono. */')
    lines.append('  --hc-font-family: var(--hc-font-ui);')
    return '\n'.join([TYPE_BEGIN,
                      '/* HCIS font families (docs/ui/hcis). Independent of the colour theme. Inter and JetBrains Mono',
                      ' * are bundled (OFL-1.1, main.tsx); every stack ends in system fonts. Edit tokens.json and regenerate. */',
                      ':root {', *lines, '}', TYPE_END])


def splice(current, region, begin=BEGIN, end=END):
    a, b = current.index(begin), current.index(end) + len(end)
    return current[:a] + region + current[b:]


def main():
    t = json.loads((HERE / 'tokens.json').read_text())
    css = render(t)
    target = HERE / 'hcis-tokens.css'
    app = APP_TOKENS.read_text()
    app_new = splice(splice(app, render_app(t)), render_app_type(t), TYPE_BEGIN, TYPE_END)
    if '--check' in sys.argv:
        bad = []
        if target.read_text() != css:
            bad.append('docs/ui/hcis/tokens/hcis-tokens.css')
        if app != app_new:
            bad.append('frontend/design-system/tokens.css (HCIS regions)')
        if bad:
            sys.exit('out of date: ' + ', '.join(bad) + '; run python3 -I docs/ui/hcis/tokens/gen-css.py')
        print('OK: HCIS CSS matches tokens.json')
    else:
        target.write_text(css)
        APP_TOKENS.write_text(app_new)


if __name__ == '__main__':
    main()
