# M2-3 probe 5: version-sensitive output used by the read-only views.
systemctl --version | head -1
echo "--- list-timers json"; systemctl list-timers --all --output=json --no-pager | head -c 300; echo; echo "rc=$?"
echo "--- list-sockets json"; systemctl list-sockets --all --output=json --no-pager | head -c 300; echo
echo "--- list-units/list-unit-files with pattern after --"
systemctl list-units --all --output=json --no-pager -- ssh.service | head -c 200; echo
systemctl list-unit-files --output=json --no-pager -- ssh.service; echo
echo "--- is-enabled / show props"; systemctl is-enabled -- ssh.service sshd.service 2>&1
systemctl show --no-pager -p Id,NRestarts,ExecMainCode,ExecMainExitTimestamp,UnitFilePreset,DropInPaths,NeedDaemonReload,Transient -- ssh.service
echo "--- TimersCalendar"; systemctl show -p TimersCalendar -p TimersMonotonic --value -- "$(systemctl list-timers --all --no-legend | awk '{for(i=1;i<=NF;i++) if($i ~ /\.timer$/){print $i; exit}}')"
echo "--- enable --no-reload accepted"; printf '[Service]\nExecStart=/bin/sleep 1\n[Install]\nWantedBy=multi-user.target\n' > /etc/systemd/system/hc-p5.service; systemctl daemon-reload; systemctl --no-ask-password --no-reload enable -- hc-p5.service; echo "rc=$? is-enabled=$(systemctl is-enabled hc-p5.service)"; systemctl --no-reload disable hc-p5.service >/dev/null 2>&1
