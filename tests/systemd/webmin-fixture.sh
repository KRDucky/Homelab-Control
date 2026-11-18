#!/usr/bin/env bash

# SPDX-FileCopyrightText: 2026 The Homelab Control Authors
#
# SPDX-License-Identifier: GPL-3.0-or-later

# Sourceable helpers for the upstream-Webmin fixture used by the disposable-host
# smoke harness (tests/systemd/m1-checks.sh). Test-only. Paths are overridable
# via the HC_WEBMIN_* environment variables so the fail-closed guard and the
# cleanup can be unit-tested without root or a real Webmin.
#
# The three state flags below are owned by the caller (m1-checks.sh or a test)
# and read here with a default so this file stays usable on its own:
#   hc_webmin_installed       1 once this run started installing the fixture
#   hc_webmin_started         1 once this run started miniserv
#   hc_perl_symlink_created   1 once this run created the perl symlink
# shellcheck disable=SC2154,SC2034

hc_webmin_config=${HC_WEBMIN_CONFIG_DIR:-/etc/webmin}
hc_webmin_var=${HC_WEBMIN_VAR_DIR:-/var/webmin}
hc_webmin_unit_lib=${HC_WEBMIN_UNIT_LIB:-/usr/lib/systemd/system/webmin.service}
hc_webmin_unit_deb=${HC_WEBMIN_UNIT_DEB:-/lib/systemd/system/webmin.service}
hc_webmin_unit_etc=${HC_WEBMIN_UNIT_ETC:-/etc/systemd/system/webmin.service}
hc_webmin_perl_link=${HC_WEBMIN_PERL_LINK:-/usr/local/bin/perl}
hc_webmin_perl_target=${HC_WEBMIN_PERL_TARGET:-/usr/bin/perl}

# Exit 0 (true) when any pre-existing Webmin state exists. The harness must
# then fail closed rather than install over it, so an orphan /var/webmin or a
# stray webmin.service is never overwritten or deleted.
hc_webmin_pre_existing() {
  [ -e "$hc_webmin_config" ] || [ -e "$hc_webmin_var" ] || \
    [ -e "$hc_webmin_unit_lib" ] || [ -e "$hc_webmin_unit_deb" ] || \
    [ -e "$hc_webmin_unit_etc" ]
}

# Remove only the fixture state this run created, as indicated by the flags.
# Idempotent: each flag is cleared after its cleanup runs, so a second call is
# a no-op. A pre-existing Webmin (flags all 0) is never touched.
hc_webmin_cleanup() {
  if [ "${hc_webmin_started:-0}" = 1 ] && [ -x "$hc_webmin_config/stop" ]; then
    "$hc_webmin_config/stop" >/dev/null 2>&1 || true
    hc_webmin_started=0
  fi
  if [ "${hc_webmin_installed:-0}" = 1 ]; then
    systemctl disable webmin.service >/dev/null 2>&1 || true
    rm -f "$hc_webmin_unit_lib" "$hc_webmin_unit_deb" "$hc_webmin_unit_etc"
    systemctl daemon-reload >/dev/null 2>&1 || true
    rm -rf "$hc_webmin_config" "$hc_webmin_var"
    hc_webmin_installed=0
  fi
  if [ "${hc_perl_symlink_created:-0}" = 1 ]; then
    rm -f "$hc_webmin_perl_link"
    hc_perl_symlink_created=0
  fi
}
