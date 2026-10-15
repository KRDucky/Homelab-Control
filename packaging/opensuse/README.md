# openSUSE

openSUSE uses **RPM**; zypper is the package manager, not a package format. The Fedora spec
(`../fedora/homelab-control.spec`) is the starting point; differences to handle in M13:
`%{_sysusersdir}`/`%sysusers_requires` macros, `BuildRequires: go >= 1.24` naming, and OBS offline
builds (vendor npm dependencies). Draft only — untested in M0.
