# Icons

The original HCIS icon set: 54 icons on a 20px grid with 1.5px round strokes, drawn for HCIS and copied from no third-party set (docs/ui/01 §1.1). It aims for Papirus-like clarity: one idea per glyph, recognizable at 16px. When the set moves into the repository, license it with the design tokens (MIT) and add it to `REUSE.toml`.

These files are drawn in a single ink, `#596A76`, so they read on light and dark grounds through `<img>`. In the app, use `<HCIS.Icon name="…">` so icons inherit `currentColor`.

Module icons: Overview `overview`, Health `health`, System `system`, Storage `storage`, Docker `containers`, Virtualization `vm`, Networking `network`, File Sharing `share`, Packages `packages`, Hardware `hardware`, Audit `audit`, Settings `settings`. Status: `check` ok, `warning` warn, `critical` crit, `info` info/notice, `unknown` unknown. Drive types: `drive` HDD, `hardware` SSD, `system` NVMe.
