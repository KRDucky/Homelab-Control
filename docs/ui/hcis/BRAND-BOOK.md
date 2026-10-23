HCIS, the Homelab Control Interface System, is the approved design direction for Homelab Control. Its visual identity is **Adaptive Glass Linux Admin**: a desktop-quality Linux administration application that runs in the browser. It combines restrained frosted glass, Material-style interaction discipline, Nordic/Arc calm and dense, readable technical data. The repository is the source of truth for what exists today; HCIS defines where the interface is going.

Every component card states one of four statuses:

- **Production:** in the app today.
- **Production markup:** the app's markup and CSS, packaged here as a component.
- **Planned:** in `docs/ui/` but not implemented.
- **Proposed (HCIS):** new reference design.

Cards for anything that writes to the host also say **Implementation: awaiting authorization** (M2+). No preview ever executes an operation.

## Who it serves and principles

The UI serves homelab administrators, from beginners who need plain language and guard rails to experts who want density, keyboard control and fast scanning.

1. **Responsive by default.** Layout follows the container, not the device (`AppShell`, `DataTable`, `DriveBayGrid` use container queries).
2. **Customizable without breaking usability.** Workspaces, density, navigation order, visible columns, font family and text size are user preferences, never theme content.
3. **Desktop polish, data first.** Use quiet surfaces and hairline borders, with one accent for the next action. Don't use giant decorative cards or large empty whitespace.
4. **Status is a word.** Every state has text and an icon shape. Colour only reinforces it.
5. **Safety is visible.** Every operation shows its risk class, a preview, an explicit confirmation and the observed result.
6. **Progressive disclosure.** Summaries come first and evidence sits one click away (`Section` sources, `DiagnosticFinding`, `Sources`).
7. **Fast.** Live data updates in place, blur only touches small fixed surfaces, and long lists virtualize.

Inspiration comes from KDE Plasma (customization), GNOME/Libadwaita (consistent adaptive behaviour), Material (feedback and elevation), Nord/Nordic and Arc (muted cool tones, clean surfaces), Papirus (clear icons) and glassmorphism. These are stylistic references only: HCIS copies no third-party assets, icons, code or protected treatments.

## Voice and content

- Use sentence case: “Fix common problems”, “Start at boot”. Proper names keep their case (ZFS, SnapRAID, Docker Compose); `overline` labels are uppercase by style.
- Say what happens and to what: “Restart nginx.service…”, not “Submit”. Add “…” when a button opens a preview or dialog.
- Explain missing things in place. Existing app copy: “Read-only build. Nothing here changes your system.”
- Errors name the cause and the next step. Existing app copy: “Too many failed attempts. Wait 15 minutes, then try again.”
- Address the person as “you”. No exclamation marks, no emoji, no jokes in errors.
- Set identifiers in mono (`mono`): unit names, paths, WWN/serial/EUI, UIDs, image tags and commands.
- Never present inference as fact. Drive locations are Verified, Inferred or Unknown, and findings are Confirmed or Suspected. High RAM use alone is never called a memory leak. An unchecked system is “Unknown”, never healthy.
- Never claim success until the backend verifies it: write “Finished · awaiting verification”, then “Succeeded · verified”.
- Label every sample as “Example data”. Production views never show fabricated data.

## Themes

There are four official themes. They share one semantic token set and one component system. Themes change **appearance only** (ADR 0010): never layout, density, navigation, workspaces, behaviour, permissions or safety controls. Themes are colour themes: typography is chosen separately (see Typography), so every font and text size works in every theme.

| Theme | Character | Glass |
|---|---|---|
| **Midnight** (default dark) | Navy surfaces, teal accent `#4ED4C8`, blue secondary `#567CE5`, teal focus | Frosted, 85% (panels) / 92% (overlays) |
| **Daybreak** (light, Nordic) | `#F2F5F8` ground, white surfaces, deep teal `#167E82` | Restrained, 90% / 92% |
| **Graphite** (high-contrast dark) | Near-black, pale steel accent, **yellow focus ring** | Minimal: 95–98%, borders instead of shadows, no backdrop glow |
| **OLED** (true black) | `#000000` ground with near-black panels from the repo's planned OLED theme; Midnight's teal, text and status colours | Dark glass over black, no glows, no shadows: borders separate layers |

How colour is built:

- **Raw palette** (`hc-palette-*`) holds the owner-specified values. Components never use it.
- **Semantic tokens** (`hc-color-*`) alias the palette per theme and fill the gaps: success, info, notice, unknown, selected, hover, disabled, borders, focus, input grounds, charts and glass.
- Theme JSON (`.hltheme`) maps one-to-one onto the semantic names (docs/ui/02 §2.2: `color.surface.raised` → `--hc-color-surface-raised`). All values are `#RRGGBB` or `#RRGGBBAA`, the only form the schema accepts.

Contrast rules (checked for all four themes):

- `text-primary` and `text-secondary` pass 4.5:1 on every `surface-*`, `input-bg` and `glass-fallback`.
- On glass they pass 4.5:1 even over the brightest **unblurred** content behind it, which is why glass opacity is 85% or more.
- Status words use `status-*-text` (4.5:1). Status marks use `status-*` (3:1). Daybreak warning `#B17A26` is a mark colour only.
- Control boundaries use `border-strong` (3:1). `border-subtle` and `glass-border` are decorative separation only.
- Focus is `focus-ring`: a 2px outline offset by 2px, at least 3:1 on every surface.
- Success and critical differ mainly in hue, especially in Daybreak. The safeguard is the fixed icon shape plus the word (check, triangle, octagon, circle-i), so status never relies on colour.
- `text-disabled` is exempt, so always pair a disabled control with a visible reason (`disabledReason`).

## Glass

Glass is a layer, not a style for everything.

- **Use glass for** the global header, sidebar, menus, popovers, the command palette, non-destructive dialogs, floating inspectors (`DetailPanel floating`), summary panels (`HealthSummary`, `BackgroundJobPanel`, `Section glass`) and toasts.
- **Keep these opaque:** logs, terminals, dense tables, configuration forms, permission editors, risk callouts, destructive dialogs and long troubleshooting text.
- **Tokens:** `hc-color-glass-surface` and `-elevated` (the alpha is the theme's glass opacity), `glass-border`, `glass-highlight`, `glass-fallback`, `hc-shadow-glass`, `hc-glass-blur` (16px), `hc-glass-blur-strong` (24px) and `hc-glass-saturate`. The `backdrop-tint-*` glows sit behind the shell so the glass has something to frost; Graphite turns them off.
- **Fallbacks:** without `backdrop-filter` support, every glass surface uses solid `glass-fallback`. **Reduced transparency** comes from the Appearance menu (`data-hc-transparency="reduced"`) or the OS (`prefers-reduced-transparency`) and does the same everywhere.
- **Performance:** blur only fixed, small surfaces. Never put `backdrop-filter` on scrolling regions, logs, charts or table bodies.

## Typography

Typography is **independent of colour themes**. Font family and text size are user preferences, like density, so a user can pick a font or enlarge text and stay in Midnight, Daybreak, Graphite or OLED.

### Font families

| Token | Preferred face | Stack (CSS) | Use for |
|---|---|---|---|
| `--font-ui` (`ui`) | **Inter** (variable, self-hosted) | `'Inter Variable', Inter, system-ui, -apple-system, 'Segoe UI', 'Noto Sans', 'Liberation Sans', sans-serif` | Navigation, tables, controls, labels, all general UI |
| `--font-mono` (`mono`) | **JetBrains Mono** (variable, self-hosted) | `'JetBrains Mono Variable', 'JetBrains Mono', ui-monospace, 'SFMono-Regular', 'DejaVu Sans Mono', 'Liberation Mono', Menlo, Consolas, monospace` | Logs, terminals, commands, paths, identifiers, configuration, code |
| `--font-ui-system` (`ui-system`) | The OS's own UI font | `system-ui, -apple-system, 'Segoe UI', 'Noto Sans', 'Liberation Sans', 'Helvetica Neue', Arial, sans-serif` | The **System** font preference, and the fallback when bundled fonts can't be shipped |
| `--font-mono-system` (`mono-system`) | The OS's monospace font | `ui-monospace, 'SFMono-Regular', 'DejaVu Sans Mono', 'Liberation Mono', Menlo, Consolas, monospace` | Monospace partner of `ui-system` |

- Inter and JetBrains Mono are the preferred faces for their appearance, readability and established open-source licensing. Other compatible fonts may be offered later as alternatives or through themes, under the licensing policy below.
- Components use only `var(--font-ui)` and `var(--font-mono)`, never a font name. The **Interface font** preference (`data-hc-font="system"` on the shell) remaps both to the system stacks, so nothing else changes.
- Every stack ends in system fonts. If a bundled file is missing or blocked, text still renders with a similar face and the same layout.

### Text size

- **Text size** is an accessibility preference: `hc-text-scale-90`, `-100` (default), `-112` and `-125`. It sets the root font size, and every type token is in rem, so all text scales together.
- Browser zoom and OS font-size settings still apply on top. Layouts must stay usable at 125% and at 200% browser zoom, and text reflows at 320 CSS px (WCAG 1.4.4, 1.4.10).
- Text size never changes with the theme, and the theme never changes it.

### Hierarchy and usage

- **Styles:**
  - `title-1` (24/32) page titles
  - `title-2` (18/24) section headings and dialog titles
  - `title-3` (15/22) panel headings
  - `body` (14/20) default
  - `small` (13/18) dense tables
  - `label` (13/18, 600) form and property labels
  - `caption` (12/16) help, timestamps and pills
  - `overline` group labels
  - `metric` KPI figures
  - `display` (32/40) the resource name in a header
  - `mono` technical data
  - `mono-small` log entries
  - `terminal` terminal output
- Use `mono` for anything a person might copy or compare character by character: paths, unit names, WWN/serial/EUI, UIDs, image tags, commands. Never use it for prose.
- All numbers use tabular figures, so columns and live values don't jitter.
- Keep component dimensions in tokens (spacing, control height), not in font metrics, so swapping Inter for a system font changes letterforms, not layout.
- Archivo, the current app's font, is legacy styling. It is replaced in migration step 2.

### Font licensing policy (owner-approved)

Bundled or custom fonts must use one of these licence types:

1. **Open source / free software:** licences that permit use, redistribution and modification, such as SIL OFL 1.1.
2. **Public domain / CC0:** fonts genuinely dedicated to the public domain or released under equivalent unrestricted terms.
3. **Royalty-free:** only with verified terms that explicitly permit commercial use, self-hosting, redistribution inside applications, and inclusion in distributable software packages.

Being a free download, or being described as "royalty-free", is not enough to bundle a font.

Before bundling any font:

- Verify its source, copyright holder and exact licence text.
- Confirm that it may be bundled and redistributed with GPL-licensed software (Homelab Control is GPL-3.0-or-later).
- Note any conditions on modification or redistribution.
- Keep the required licence notice and attribution with the font files and in the app's third-party notices.
- Respect naming restrictions on derivative works (OFL Reserved Font Names).
- Reject fonts whose terms forbid software redistribution or require recurring payments.
- If the rights can't be verified, use the system stacks instead.
- Never load fonts from an external CDN. Fonts ship with the app or come from the OS.

The preferred fonts as verified (records in `fonts/README.md`):

| Font | Licence | Copyright | Reserved Font Name | Source checked |
|---|---|---|---|---|
| Inter (variable) | SIL OFL 1.1 | The Inter Project Authors | None declared | `@fontsource-variable/inter` 5.3.0 `LICENSE`, upstream github.com/rsms/inter |
| JetBrains Mono (variable) | SIL OFL 1.1 | The JetBrains Mono Project Authors | None declared | `@fontsource-variable/jetbrains-mono` 5.3.0 `LICENSE`, upstream github.com/JetBrains/JetBrainsMono |

OFL fonts may be bundled with GPL software. They may not be sold on their own, and the licence text must travel with them.

## Spacing, density, radius and elevation

- **Spacing** is a 4px grid: `hc-space-1` (4) … `hc-space-16` (64). Use `space-2` between related controls, `space-4` inside panels, `space-6` between panels and `space-10` between sections.
- **Density** is a user layout preference (`data-hc-density`): Compact, Standard (default) or Spacious. Rows are 28 / 36 / 44px and controls 28 / 32 / 40px; cell padding and gaps follow. Compact still meets the 24px WCAG 2.2 target size.
- **Radii:**
  - `hc-radius-sm` 4px: small controls
  - `hc-radius-md` 6px: inputs and menus
  - `hc-radius-lg` 8px: panels and cards
  - `hc-radius-xl` 12px: dialogs, resource headers and the bay chassis
  - `hc-radius-round`: dots and switches only
- **Elevation**, prefer borders and surface contrast first:

  | Level | Treatment |
  |---|---|
  | Base surface | No shadow |
  | Raised panel | `hc-shadow-1` |
  | Floating panel and popover | `hc-shadow-2` |
  | Dialog | `hc-shadow-3` over `scrim` |
  | Critical overlay | `hc-shadow-4`: opaque, critical ring, over `scrim-critical` |
  | Glass | `hc-shadow-glass` |

  Graphite replaces shadows with borders.

## Layout and navigation

The **shell** adapts to its own width:

| Width | Navigation and content |
|---|---|
| ≥1100px | Expanded sidebar; multi-panel layouts, side-by-side inspectors and logs |
| 720–1100px | Icon rail; two-column layouts |
| <720px | Navigation drawer (menu button, Esc or scrim closes it); stacked panels; tables become labelled cards |

- **Global header:** Homelab Control identity, active host, health chip (always with a word), workspace switcher, global search (Ctrl/Cmd+K), background jobs, notifications, appearance and account.
- **Primary navigation:** Overview, Health & Diagnostics · Manage: System, Storage, Docker, Virtualization, Networking, File Sharing, Packages, Hardware · Govern: Audit, Settings.
  - It is capability-aware: unavailable modules are hidden or listed muted with the reason, never shown as working.
  - Grouping and order are configurable.
- **Secondary navigation:** breadcrumbs, tabs, `ResourceTree` and contextual `DetailPanel`s.
- **Resource detail pattern:** `Breadcrumbs`, then `ResourceHeader`, then tabs drawn from Overview, Status/Health, Configuration, Logs, Diagnostics, Dependencies, Related, History and Actions.
  - Show only the tabs that have content; never pad for consistency.
  - It applies to systemd services, containers, Compose projects, ZFS pools and datasets, Btrfs, mdadm, LVM2, physical drives, VMs, interfaces, shares and packages.
- **Workspaces** (future, never required for basic navigation) are named layouts: System Monitoring, Storage Administration, Docker Management, Virtualization, Networking and Diagnostics. They save widgets, panel sizes, columns, landing page and density, never colours.

## Responsive vs reactive

- **Responsive:** the UI adapts to container size, density and layout preferences. On small screens the drive layout keeps working through scroll, zoom or the accessible List view.
- **Reactive:** the UI reflects system state changes: a container stopping, a unit failing, a pool degrading, temperatures, job progress, new findings.
  - `LiveIndicator` shows Live, Refreshing, Stale, Paused or Disconnected.
  - Stale or disconnected content dims in place (`hc-stale`) and is never blanked.
  - Updates keep focus, scroll position and selections, and never reflow what the user is reading.
  - Polling is throttled, nothing flickers, and screen readers hear state changes, not every tick.

## Privileged operation safety

| Risk class | Meaning | Badge |
|---|---|---|
| Read-only | No host modification | eye, neutral |
| Reversible | Can generally be undone | restart arrow, info |
| Disruptive | May interrupt services or availability | triangle, warning |
| Destructive | May cause irreversible data loss | octagon, critical, heavier border |

The flow for every write:

1. **Preview.** `OperationPreview` shows the canonical target, every change and its counts, dependency warnings, risk and consequences, under a “Preview · nothing has been changed yet” bar.
2. **Confirm.** `ConfirmDialog` adds re-authentication where required and type-to-confirm for destructive actions. Destructive dialogs are opaque and focus starts on Close.
3. **Progress.** `JobProgress` / `BackgroundJobPanel` shows steps.
4. **Observed result.** Only a backend-verified result counts as success.
5. **Audit reference.** Every operation gets an `AuditTimeline` entry.

Never claim rollback for non-atomic operations. Destructive menu items lead to step 1, never straight to execution.

## Health, diagnostics and troubleshooting

- **Fix common problems** is system-wide: CPU and throttling, memory pressure and suspected leaks, swap/OOM, disks and SMART/NVMe, ZFS, Btrfs, mdadm, LVM2, mergerfs, SnapRAID, systemd, Docker health and crash loops, packages and security updates, network/DNS, ECC, thermals and fans, backups, UPS/NUT and security configuration.
- **Each finding (`DiagnosticFinding`)** shows its severity (Critical, Warning or Notice), whether it is Confirmed or Suspected, its module, first and last seen times, evidence, how to investigate, a suggested solution and whether the resolution has been verified. Findings can be acknowledged or suppressed.
- **`HealthSummary`** counts Critical, Warning, Notice, Healthy and Unknown, and modules that weren't checked count as Unknown.
- **Diagnose & Resolve** for failed services (`ServiceDetail` + `ResolutionGuide`): failure category, exit code, restart count, journal evidence, dependencies, Investigate → Fix → Verify steps with copyable commands, and documentation links. Nothing runs automatically, and automatic repair is not part of the initial UI.

## Theme manager and custom themes

**Appearance → Themes** (`ThemeManager`):

- Browse, preview and select built-in themes.
- Import `.hltheme` files, inspect their metadata and see detailed validation errors.
- Revert to a built-in theme. Theme Studio comes later.

An information icon beside theme import and custom-theme selection shows this text:

> All custom themes must follow HCIS requirements for responsive layouts, accessibility, readable contrast, component consistency, and security. Themes may customize appearance but cannot alter functionality, permissions, safety controls, or application layout.

**View theme requirements** lists the thirteen requirements:

1. Preserve responsive behaviour.
2. Meet HCIS accessibility requirements.
3. Maintain readable contrast.
4. Preserve component usability.
5. Support keyboard navigation and focus visibility.
6. Respect reduced motion.
7. Provide usable fallback surfaces.
8. Preserve safety warnings.
9. Stay compatible with density and layout preferences.
10. Never change behaviour or authorization.
11. Use validated `.hltheme` JSON.
12. Contain no JavaScript or unrestricted CSS.
13. Use only approved typography tokens: a theme may choose `ui`/`mono` from the approved families (`inter` or `system`; `jetbrains-mono` or `system`). It may never ship font files, `@font-face` rules or font URLs, or bypass the licensing, security or accessibility rules.

Built-in themes meet the same requirements.

- **Safe preview:** a 15-second timed trial that reverts unless kept; Esc reverts immediately. Users can always recover to Midnight.
- **Validation** rejects layout and code keys, non-hex colours, functions and URLs, and failing key contrast pairs. It doesn't promise full accessibility compliance.

## Iconography

- HCIS has an original icon set, Papirus-like in clarity: 20px grid, 1.5px stroke, round caps and joins, `currentColor`. Use `<Icon name="…">`. There is no icon font and no third-party set.
- Module icons: Overview `overview`, Health `health`, System `system`, Storage `storage`, Docker `containers`, Virtualization `vm`, Networking `network`, File Sharing `share`, Packages `packages`, Hardware `hardware`, Audit `audit`, Settings `settings`.
- Status shapes are fixed: `check` ok, `warning` warn, `critical` crit, `info` info and notice, `unknown` unknown. Drive bays show `drive` (HDD), `hardware` (SSD) and `system` (NVMe).
- An icon that stands alone needs a label. An icon next to text is hidden from assistive technology.

## Motion

- Durations: `hc-motion-fast` 120ms, `-normal` 200ms, `-slow` 280ms (drawers and dialogs only), all on `hc-motion-ease`. Nothing loops except progress, live pulses and the locate blink, and all three stop under reduced motion.
- The motion preference has three settings: Full, Reduced (opacity fades of 150ms or less only) and None. `prefers-reduced-motion` maps to None for durations.

## Accessibility

The target is WCAG 2.2 AA. That means:

- Full keyboard operation, with visible focus everywhere.
- Native elements first, with WAI-ARIA patterns for tabs, tree, combobox, menu and dialog.
- Dialogs trap focus and return it on close. Tables have captions and row headers.
- Status updates go through polite live regions.
- Drag and drop always has a keyboard alternative (Hold → press a bay, or Assign drive).
- Charts have a text summary and a table view.
- Reduced-motion and reduced-transparency preferences are respected.
- Glass is tested against real content behind it.

## Using the components

- Load `tokens.css`, the fonts, `components/bundle.css`, React 18+ and `components/bundle.js`. That exposes `window.HCIS.<Component>`. Wrap the app in an element with class `hc`.
- Switch themes with `data-theme="midnight" | "daybreak" | "graphite" | "oled"` on `<html>`, or on any element to scope a preview. Set `data-hc-density`, `data-hc-transparency`, `data-hc-motion` and `data-hc-font` on the shell. Text size sets the root font size.
- Props are documented in `components/index.d.ts`.
- **Catalogue names → HCIS components:**

  | Spec name | HCIS component |
  |---|---|
  | TabBar | `Tabs` |
  | PropertyList | `KeyValueList` |
  | SortableColumn | `DataTable` columns |
  | StatusIndicator / dot | `StatusIndicator` / `StateDot` |
  | ProgressIndicator | `ProgressBar` |
  | Toast | `ToastRegion` |
  | ConfirmationDialog / RiskConfirmation | `ConfirmDialog` |
  | PermissionMatrix / FileModeEditor | `PermissionEditor` |
  | DirectoryBrowser | `FileBrowser` |
  | ServiceDetails | `ServiceDetail` |
  | Docker Permissions Manager | `SafePermissionsManager` |
  | Physical Drive Layout Designer | `DriveBayGrid` |

  All of these aliases are also exported on `window.HCIS`.
- No component needs inline `<style>`. Dynamic geometry (meters, charts, bay placement) is set through the CSSOM, which works under the app's `style-src 'self'` CSP (ADR 0010 §4).

## Compatibility with Homelab Control

- **Token names** follow the app's `--hc-*` convention and the v1 taxonomy in docs/ui/02. The new glass, input, notice and unknown roles are additions for the `.hltheme` schema. Adding tokens is a minor engine change (§2.5).
- **M1 aliases:** `--hc-color-ink`, `panel`, `panel-raised`, `rule`, `text`, `muted`, `signal`, `signal-ink`, `ok`, `warn`, `crit` and `focus` alias the HCIS roles, so today's `styles.css` renders in HCIS colours unchanged.
- **Fonts:** themes can only pick an approved font enum, never a URL or file (`typography.family.ui|mono`; the validator rejects anything else). Inter and JetBrains Mono ship with the engine once migration step 2 lands, under the font licensing policy. Font choice and text size are user preferences stored with layout preferences, not in themes.
- **Density names:** HCIS uses `compact | standard | spacious` (owner decision 2026-10-22). docs/ui/06 §6.5 currently says `compact | comfortable | spacious`, so its middle value (`comfortable`) becomes `standard` in a separately approved update.
- **Theme safety:** themes contain no executable code and are applied as allow-listed custom properties through the CSSOM. They are kept separate from layout and workspace preferences.

## Current implementation vs HCIS

| Area | App today (main@6e0f621) | HCIS |
|---|---|---|
| Visual style | Flat panels, gradient “faceplate” | Adaptive Glass: glass shell and overlays, opaque data surfaces |
| Themes | Dark + light via `prefers-color-scheme` | Midnight, Daybreak, Graphite, OLED; reduced transparency |
| Accent | Periwinkle `#7C8CFF` / `#4453D6` | Teal `#4ED4C8` / `#167E82`, blue secondary |
| Type | Archivo Variable (legacy) | Inter + JetBrains Mono |
| Spacing / density | 0.25–2rem, one density | 4px grid, three densities |
| Radius | 3 / 8 / 14px | 4 / 6 / 8 / 12px |
| Components | `Section`, `Sources`, `StateDot`, `Meter` + CSS patterns | 81 components and showcases |
| Navigation | Capability-driven rail, 4 pages | Glass shell, 12 sections, rail/drawer, palette, workspaces |
| Operations | Read-only (M1) | Risk classes and preview → confirm → verify → audit (awaiting M2 authorization) |

Adopt HCIS incrementally, through separately approved PRs. This design system doesn't replace the frontend, remove behaviour or change the architecture.

## Not synced

- Not carried over from `frontend/design-system/tokens.css`: `--hc-font-stretch-*` (Archivo-only). Motion is now tokenized as `hc-motion-*`.
- Archivo is not included. It is legacy styling, still loaded by the app from `@fontsource-variable/archivo`.
- Components were not built from source. `Section`, `StateDot` and `Meter` are hand-ported from `ui.tsx` and `Meter.tsx`; everything else is a hand-written reference.
- The planned example themes in `docs/ui/examples/themes/` are not imported as files. Graphite covers high contrast, and the OLED theme uses `oled.json`'s surface, border and meter values.
