set -u
U=/etc/systemd/system
cat > $U/hc-probe-a.service <<X
[Unit]
Description=probe a
[Service]
Type=oneshot
ExecStart=/bin/true
RemainAfterExit=yes
[Install]
WantedBy=multi-user.target
Alias=hc-probe-alias.service
X
cat > $U/hc-probe-b.service <<X
[Service]
ExecStart=/bin/sleep 1000
[Install]
WantedBy=multi-user.target
Also=hc-probe-c.service
X
cat > $U/hc-probe-c.service <<X
[Service]
ExecStart=/bin/sleep 1000
[Install]
WantedBy=multi-user.target
X
cat > $U/hc-probe-static.service <<X
[Service]
ExecStart=/bin/true
X
mkdir -p $U/hc-probe-c.service.d
printf '[Service]\nEnvironment=SECRET=hunter2\n' > $U/hc-probe-c.service.d/secret.conf; chmod 600 $U/hc-probe-c.service.d/secret.conf
systemctl daemon-reload
echo "--- implicit reload? (journal Reloading lines count before/after)"
b=$(journalctl -b -o cat -u init.scope 2>/dev/null | grep -c -i 'reloading'); b2=$(journalctl -b -o cat | grep -ci 'Reloading requested\|Reloading\.\.\.\|Reloading finished')
systemctl --no-ask-password enable -- hc-probe-a.service; echo "rc=$?"
a2=$(journalctl -b -o cat | grep -ci 'Reloading requested\|Reloading\.\.\.\|Reloading finished'); echo "reload lines before=$b2 after enable=$a2"
systemctl --no-ask-password --no-reload disable -- hc-probe-a.service; echo "disable --no-reload rc=$?"
a3=$(journalctl -b -o cat | grep -ci 'Reloading requested\|Reloading\.\.\.\|Reloading finished'); echo "after --no-reload disable=$a3"
echo "--- freshness after enable --no-reload"
systemctl --no-ask-password --no-reload enable -- hc-probe-a.service 2>&1; echo rc=$?
echo "show UnitFileState=$(systemctl show -p UnitFileState --value hc-probe-a.service) is-enabled=$(systemctl is-enabled -- hc-probe-a.service)"
ls -l $U/multi-user.target.wants/ | grep probe; ls -l $U | grep alias
systemctl --no-ask-password --no-reload disable -- hc-probe-a.service 2>&1; echo rc=$?
echo "after disable: show=$(systemctl show -p UnitFileState --value hc-probe-a.service) is-enabled=$(systemctl is-enabled -- hc-probe-a.service); alias exists? $(ls $U/hc-probe-alias.service 2>&1)"
echo "--- Also="
systemctl --no-ask-password --no-reload enable -- hc-probe-b.service 2>&1; echo rc=$?
echo "b=$(systemctl is-enabled hc-probe-b.service) c=$(systemctl is-enabled hc-probe-c.service)"
systemctl --no-ask-password --no-reload disable -- hc-probe-b.service 2>&1; echo rc=$?
echo "b=$(systemctl is-enabled hc-probe-b.service) c=$(systemctl is-enabled hc-probe-c.service)"
echo "--- static"
systemctl --no-ask-password --no-reload enable -- hc-probe-static.service 2>&1; echo rc=$?
echo "is-enabled static=$(systemctl is-enabled hc-probe-static.service) rc=$?"
echo "--- masked"
systemctl mask hc-probe-c.service >/dev/null 2>&1;  systemctl --no-ask-password --no-reload enable -- hc-probe-c.service 2>&1; echo rc=$?; systemctl is-enabled hc-probe-c.service; systemctl unmask hc-probe-c.service >/dev/null 2>&1
echo "--- enable via alias name"
systemctl --no-ask-password --no-reload enable -- hc-probe-a.service >/dev/null 2>&1; systemctl daemon-reload
systemctl --no-ask-password --no-reload disable -- hc-probe-alias.service 2>&1; echo rc=$?
systemctl is-enabled hc-probe-a.service; systemctl is-enabled hc-probe-alias.service
systemctl --no-ask-password --no-reload disable -- hc-probe-a.service >/dev/null 2>&1
echo "--- is-enabled on various"
for u in ssh.service sshd.service ssh.socket systemd-journald.service hc-nonexistent.service getty@.service getty@tty1.service; do echo "$u: $(systemctl is-enabled -- $u 2>&1) rc=$?"; done
echo "--- runtime enable"
systemctl --runtime --no-reload enable hc-probe-c.service >/dev/null 2>&1; echo "runtime: $(systemctl is-enabled hc-probe-c.service)"; systemctl --runtime --no-reload disable hc-probe-c.service >/dev/null 2>&1
echo "--- cat as nobody (0600 drop-in)"
su -s /bin/sh nobody -c 'systemctl cat -- hc-probe-c.service'; echo "rc=$?"
echo "--- cat template"; su -s /bin/sh nobody -c 'systemctl cat -- getty@.service | head -3'; echo rc=$?
echo "--- show on unloaded"
cat > $U/hc-probe-unloaded.service <<X
[Service]
ExecStart=/bin/true
X
systemctl daemon-reload; echo "listed before: $(systemctl list-units --all --no-legend | grep -c hc-probe-unloaded)"; systemctl show -p LoadState,ActiveState hc-probe-unloaded.service; echo "listed after: $(systemctl list-units --all --no-legend | grep -c hc-probe-unloaded)"
echo "--- list-units with pattern after --"; systemctl list-units --all --output=json --no-pager -- hc-probe-a.service | head -c 300; echo
systemctl list-unit-files --output=json --no-pager -- hc-probe-a.service; echo
echo "--- timers json"; systemctl list-timers --all --output=json --no-pager | head -c 600; echo
echo "--- sockets json"; systemctl list-sockets --all --output=json --no-pager | head -c 600; echo
echo "--- TimersCalendar show"; t=$(systemctl list-timers --all --no-legend | awk '{for(i=1;i<=NF;i++) if($i ~ /\.timer$/){print $i; exit}}'); echo "timer=$t"; [ -n "$t" ] && systemctl show -p TimersCalendar -p TimersMonotonic -p NextElapseUSecRealtime -p LastTriggerUSec -p Unit -p Persistent "$t"
echo "--- show not-found"; systemctl show -p Id,LoadState,ActiveState,UnitFileState hc-nope.service
echo "--- template show"; systemctl show -p Id,LoadState getty@.service 2>&1; echo rc=$?
