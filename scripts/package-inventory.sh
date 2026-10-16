#!/usr/bin/env bash
# Lists the payload of a built release package and enforces release-compliance rules:
#   - no Webmin code or assets (paths from the upstream tree, Perl/CGI, Webmin themes)
#   - nothing outside the expected install locations
#   - license and notice files present
# Usage: scripts/package-inventory.sh <package.deb|package.rpm|package.pkg.tar.zst> [inventory-out.txt]
set -euo pipefail
pkg=${1:?package}; out=${2:-}
case "$pkg" in
  *.deb) list=$(dpkg-deb -c "$pkg" | awk '{print $1" "$3" "$6}'); paths=$(dpkg-deb -c "$pkg" | awk '{print $6}' | sed 's#^\./#/#') ;;
  *.rpm) list=$(rpm -qlvp "$pkg" | awk '{print $1" "$5" "$9}'); paths=$(rpm -qlp "$pkg") ;;
  *.pkg.tar.zst) list=$(tar --zstd -tvf "$pkg" | awk '{print $1" "$3" "$6}'); paths=$(tar --zstd -tf "$pkg" | sed 's#^#/#') ;;
  *) echo "unknown package type: $pkg"; exit 2 ;;
esac
fail=0
bad() { echo "FAIL  $*"; fail=1
  if [ -n "${GITHUB_ACTIONS:-}" ]; then echo "::error title=package-inventory $(basename "$pkg")::$*"; fi; }

files=$(grep -v '/$' <<<"$paths" | grep -vE '^/\.(PKGINFO|BUILDINFO|MTREE|INSTALL)$' || true)
# 1. No Webmin code or assets.
if hits=$(grep -iE 'webmin|miniserv|authentic|gray-theme|\.cgi$|\.pl$|\.pm$|/compat/' <<<"$files"); then
  bad "Webmin-derived or Perl/CGI paths in payload: $(tr '\n' ' ' <<<"$hits")"
fi
# 2. Only expected install locations.
if extra=$(grep -vE '^/(usr/bin/(hc-api|hlctl)|usr/lib/homelab-control/hc-broker|usr/lib/systemd/system/homelab-control[a-z-]*\.(service|socket)|usr/lib/sysusers\.d/homelab-control\.conf|etc/homelab-control/hc-api\.env|usr/share/(doc|licenses)/homelab-control/[A-Za-z0-9._-]+)$' <<<"$files"); then
  bad "unexpected files in payload: $(tr '\n' ' ' <<<"$extra")"
fi
# 3. License and notices.
for f in LICENSE.md NOTICE.md THIRD-PARTY-NOTICES.md; do
  grep -qE "^/usr/share/(doc|licenses)/homelab-control/$f$" <<<"$files" || bad "missing $f"
done

report=$(printf '# Payload of %s\n# %s files\n%s\n' "$(basename "$pkg")" "$(wc -l <<<"$files")" "$list")
if [ -n "$out" ]; then printf '%s\n' "$report" > "$out"; fi
printf '%s\n' "$report"
[ "$fail" -eq 0 ] && echo "OK: $(basename "$pkg") payload passes release-compliance checks"
exit "$fail"
