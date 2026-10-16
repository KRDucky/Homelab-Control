# 7. Accessibility and reduced-motion requirements (planning)

**Target: WCAG 2.2 Level AA** for every screen, including the Theme Studio and every built-in theme.
M1 baseline: axe-core WCAG 2.1 A/AA scans with zero violations on four pages (`docs/evidence/m1/vm-26.04/`).

## 7.1 Requirements
- **Keyboard:** everything operable without a pointer; visible focus (2 px ring, ≥ 3:1 against adjacent
  colours); logical order; no keyboard traps; skip link to main content; drag interactions have
  keyboard alternatives (`04` §4.3).
- **Screen readers:** semantic landmarks and headings; native elements first; ARIA only per WAI-ARIA
  APG patterns; tables have captions and header cells; live regions for toasts, operation progress and
  validation results — rate-limited so telemetry updates are not announced.
- **Colour:** text ≥ 4.5:1 (≥ 3:1 for large text); UI component boundaries, focus indicators, chart
  marks and status indicators ≥ 3:1 against adjacent colours; status always has a text label or icon,
  never colour alone; charts offer a data table view.
- **Themes:** every built-in theme passes all contrast checks in CI; user themes cannot be activated
  with contrast errors (`03` §3.3); `prefers-contrast: more` selects the high-contrast mode; Windows
  forced-colors mode is respected (`forced-color-adjust` left at default, focus and borders use
  system colours).
- **Text:** zoom to 200 % and reflow at 320 CSS px without loss; user font-size changes respected (type
  scale in `rem`); no text in images.
- **Targets:** ≥ 24 × 24 CSS px (WCAG 2.2 2.5.8); 44 px on touch layouts.
- **Time:** sessions warn before idle expiry (30 min) and allow extension; live data never moves content
  the user is reading (no reflow on update).

## 7.2 Motion
- Motion is optional decoration: the UI is fully usable with all motion off.
- User setting **Full · Reduced · None** (default follows `prefers-reduced-motion`). Reduced keeps only
  opacity fades ≤ 150 ms; None sets all durations to 0 (the M1 token already does this).
- No parallax, no auto-playing animation, nothing flashes more than three times per second; spinners
  become static "working…" text in None.
- Chart transitions are disabled under Reduced/None; live charts update in place.

## 7.3 Verification
axe-core on every page and Theme Studio state in CI (as M1); keyboard-only e2e paths per primary
journey; contrast unit tests for every built-in theme; manual screen-reader pass (NVDA + Firefox, Orca +
Firefox, VoiceOver + Safari) before each release.
