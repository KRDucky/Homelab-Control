# M4-1a — Software Center adapter contract and parsers (working note)

Not acceptance, not a merge, not production, not KVM. Pure stdlib parsing only.

## Scope

`backend/internal/pkgcenter` parses distribution package-manager output into one
normalized model. No spawn, no broker op, no route, no scope, no UI. The adapter
is selected by the existing `hostinfo.DistroFamily`; `unknown` is reported and
never guessed.

## Model

- `Package` — `name`, `version`, `arch`, `origin`, `installed`, `upgradable`, `summary`.
- `Update` — `name`, `arch`, `origin`, `current` (old), `new`. `current` is empty
  when the tool does not report it in the same output (dnf `check-update`).
- `Source` — `name`, `origin`, `enabled`, `signed`, `third_party`.

`summary` strips control/bidi characters (the plugin manifest `UNSAFE_TEXT` set),
normalizes whitespace, and is bounded to 512 characters. Structural fields
(`name`, `version`, `arch`, `origin`) refuse NUL/control/bidi rather than strip,
so hostile or localized non-C output is refused, not misparsed.

## Inputs per family (fixtures)

The exact per-release command flags are **verified in M4-1b** against the
installed tools; these are the fixture formats for the parsers:

| Family | installed | search | updates | sources |
|---|---|---|---|---|
| debian | `dpkg-query -W` `name\tversion\tarch` | `apt-cache search` `name - summary` | `apt list --upgradable` | `deb` one-line stanzas |
| fedora | `rpm -qa --qf` `name\tversion\tarch` | `dnf repoquery --qf` `name\tversion\tarch\trepo\tsummary` | `dnf check-update` `name.arch version repo` | `.repo` INI sections |
| suse | `zypper -x search --installed-only` | `zypper -x search` | `zypper -x list-updates` | `zypper -x lr` |
| arch | `pacman -Q` `name version` | `pacman -Ss` `repo/name version [flags]` | `pacman -Qu` `name old -> new` | `pacman.conf` `[repo]` sections |

`third_party` is a per-family heuristic: a source whose origin/repository is not
one of the family's vendor archives is flagged third-party. Refined against
`07-DISTRO-PACKAGING` in M4-1b.

## Bounds

4 MiB total input, 256 KiB per line, 65536 packages, 4096 sources; name 256 B,
version 512 B, arch 64 B, origin 1 KiB. Oversized input is refused, never
truncated into a plausible result.
