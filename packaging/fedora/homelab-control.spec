# SPDX-FileCopyrightText: 2026 The Homelab Control Authors
#
# SPDX-License-Identifier: GPL-3.0-or-later

# DRAFT — untested in M0. Used for Fedora and (with minor macro differences) openSUSE.
Name:           homelab-control
Version:        0.1.0
Release:        0.m0%{?dist}
Summary:        Homelab Control API and web UI (unprivileged, read-only in M0)
License:        GPL-3.0-or-later AND MIT AND OFL-1.1 AND BSD-3-Clause
URL:            https://github.com/KRDucky/Homelab-Control---Webmin-Fork
Source0:        %{name}-%{version}.tar.gz
BuildRequires:  golang >= 1.24, nodejs >= 22, npm, make, git, systemd-rpm-macros
%{?sysusers_requires_compat}

%description
Unprivileged Homelab Control API server with a read-only host overview.
Coexists with upstream Webmin; does not modify /etc/webmin.

%prep
%autosetup

%build
# npm needs network access for `npm ci`; for Koji/OBS builds vendor node_modules into Source1.
make build VERSION=%{version}

%install
install -Dm0755 build/hc-api %{buildroot}%{_bindir}/hc-api
install -Dm0755 build/hlctl %{buildroot}%{_bindir}/hlctl
install -Dm0644 packaging/common/homelab-control.service %{buildroot}%{_unitdir}/homelab-control.service
install -Dm0644 packaging/common/sysusers.conf %{buildroot}%{_sysusersdir}/homelab-control.conf
install -Dm0640 packaging/common/hc-api.env %{buildroot}%{_sysconfdir}/homelab-control/hc-api.env

%pre
%sysusers_create_compat packaging/common/sysusers.conf

%post
%systemd_post homelab-control.service

%preun
%systemd_preun homelab-control.service

%postun
%systemd_postun_with_restart homelab-control.service

%files
%license LICENSE.md NOTICE.md docs/compliance/THIRD-PARTY-NOTICES.md
%doc README.md
%{_bindir}/hc-api
%{_bindir}/hlctl
%{_unitdir}/homelab-control.service
%{_sysusersdir}/homelab-control.conf
%dir %attr(0750,root,homelab-control) %{_sysconfdir}/homelab-control
%config(noreplace) %attr(0640,root,homelab-control) %{_sysconfdir}/homelab-control/hc-api.env
