# Homelab Control — Claude Project Setup

## Project Name
Homelab Control

## Short Description
Open-source, homelab-first Linux server administration platform forked from Webmin, with a modern modular UI, advanced storage workflows, native software management, hardware control, reverse proxy tooling, and installable plugins/themes.

## Full Description
Homelab Control modernizes Webmin into a secure, distribution-aware management platform for Linux servers and NAS systems. It preserves proven Webmin administration capabilities where useful while introducing a React/TypeScript UI, a versioned management API, least-privilege operations, standardized plugins and themes, and guided workflows for disks, filesystems, ZFS, mdraid, Btrfs, mergerfs, SnapRAID, networking, containers, users, packages, cooling, and reverse proxies. Ship source code and native Debian, Fedora, openSUSE and Arch packages.

## Claude Project Instructions
Read `CLAUDE.md`, `01-PROJECT-CONTEXT.md`, `02-ARCHITECTURE.md`, `03-SECURITY-AND-OPERATIONS.md`, and `04-ROADMAP.md` before proposing or making architectural changes. Treat `05-FEATURE-CATALOG.md`, `06-THEME-AND-PLUGIN-SDK.md`, `07-DISTRO-PACKAGING.md`, and `08-TESTING-AND-ACCEPTANCE.md` as normative specifications. Keep `09-PROJECT-MEMORY.md` updated only with verified decisions, blockers, and results. Start work using `10-INITIAL-PROMPT.md`. Do not claim code has been built, tested, or deployed unless actually verified. **Every fully functioning implementation must be demonstrated on real disposable VMs for each claimed supported distro before milestone acceptance; partial/design checkpoints do not qualify.** See `08-TESTING-AND-ACCEPTANCE.md`. Stop at milestone boundaries and report results.

Owner 2026-11-20: until M2 is production ready, do not stop to ask. Follow the owner stance in `CLAUDE.md`. A system-account login over HTTPS is enough. Do the simple thing.

## Recommended Claude Code usage
Place `CLAUDE.md` at the repository root. Add the other Markdown files under `docs/project/` if preferred, and update their paths in `CLAUDE.md`. Use a fresh Git branch and disposable VM for the first milestone.
