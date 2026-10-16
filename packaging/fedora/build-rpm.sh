#!/usr/bin/env bash
# Builds a binary development RPM (Fedora, openSUSE) from ./build. Unsigned.
# The source-RPM spec for distribution builds remains packaging/fedora/homelab-control.spec (M13).
set -euo pipefail
ver_in=${1:-0.0.0}
root=$(cd "$(dirname "$0")/../.." && pwd)
for f in hc-api hc-broker hlctl; do [ -x "$root/build/$f" ] || { echo "missing build/$f (run make build)"; exit 1; }; done
# RPM Version may not contain '-'; keep the git part in Release.
base=$(sed -E 's/^v//; s/[-+].*$//' <<<"$ver_in"); [[ "$base" =~ ^[0-9]+(\.[0-9]+)*$ ]] || base=0.1.0
rel="0.m1.$(sed -E 's/[^A-Za-z0-9]+/_/g' <<<"$ver_in")"
top=$(mktemp -d); trap 'rm -rf "$top"' EXIT
mkdir -p "$top"/{BUILD,RPMS,SPECS,SOURCES,BUILDROOT}
cat > "$top/SPECS/homelab-control.spec" <<SPEC
Name:           homelab-control
Version:        $base
Release:        $rel
Summary:        Homelab Control API, web UI and privileged broker (development build)
License:        GPL-3.0-or-later AND MIT AND OFL-1.1
URL:            https://github.com/KRDucky/Homelab-Control---Webmin-Fork
BuildArch:      x86_64
Requires(pre):  /usr/sbin/useradd, /usr/sbin/groupadd
%global debug_package %{nil}
%global _build_id_links none
%undefine _missing_build_ids_terminate_build

%description
Unprivileged API with authenticated sessions and a root broker that exposes
only typed, read-only operations. Coexists with upstream Webmin.

%install
install -Dm0755 $root/build/hc-api %{buildroot}/usr/bin/hc-api
install -Dm0755 $root/build/hlctl %{buildroot}/usr/bin/hlctl
install -Dm0755 $root/build/hc-broker %{buildroot}/usr/lib/homelab-control/hc-broker
for u in homelab-control.service homelab-control-broker.service homelab-control-broker.socket; do
  install -Dm0644 $root/packaging/common/\$u %{buildroot}/usr/lib/systemd/system/\$u
done
install -Dm0644 $root/packaging/common/sysusers.conf %{buildroot}/usr/lib/sysusers.d/homelab-control.conf
install -Dm0640 $root/packaging/common/hc-api.env %{buildroot}/etc/homelab-control/hc-api.env
install -Dm0644 $root/LICENSE.md %{buildroot}/usr/share/licenses/homelab-control/LICENSE.md
install -Dm0644 $root/NOTICE.md %{buildroot}/usr/share/licenses/homelab-control/NOTICE.md
install -Dm0644 $root/README.md %{buildroot}/usr/share/doc/homelab-control/README.md

%pre
getent group homelab-control >/dev/null || groupadd -r homelab-control
getent passwd homelab-control >/dev/null || useradd -r -g homelab-control -d /nonexistent -s /sbin/nologin -c "Homelab Control API" homelab-control
exit 0

%post
if [ -d /run/systemd/system ]; then systemctl daemon-reload >/dev/null 2>&1 || :; fi
if [ \$1 -eq 1 ]; then
  echo "homelab-control installed but NOT enabled. See /usr/share/doc/homelab-control/README.md"
fi

%preun
if [ \$1 -eq 0 ] && [ -d /run/systemd/system ]; then
  systemctl --no-reload disable --now homelab-control.service homelab-control-broker.socket homelab-control-broker.service >/dev/null 2>&1 || :
fi

%postun
if [ -d /run/systemd/system ]; then systemctl daemon-reload >/dev/null 2>&1 || :; fi

%files
%license /usr/share/licenses/homelab-control/LICENSE.md
%license /usr/share/licenses/homelab-control/NOTICE.md
%doc /usr/share/doc/homelab-control/README.md
/usr/bin/hc-api
/usr/bin/hlctl
%dir /usr/lib/homelab-control
/usr/lib/homelab-control/hc-broker
/usr/lib/systemd/system/homelab-control.service
/usr/lib/systemd/system/homelab-control-broker.service
/usr/lib/systemd/system/homelab-control-broker.socket
/usr/lib/sysusers.d/homelab-control.conf
%dir %attr(0750,root,homelab-control) /etc/homelab-control
%config(noreplace) %attr(0640,root,homelab-control) /etc/homelab-control/hc-api.env
SPEC
rpmbuild --quiet -bb --define "_topdir $top" --define "_source_date_epoch_from_changelog 0" \
  --define "use_source_date_epoch_as_buildtime 1" "$top/SPECS/homelab-control.spec"
out=$(find "$top/RPMS" -name '*.rpm' | head -1)
cp "$out" "$root/build/"
echo "$root/build/$(basename "$out")"
