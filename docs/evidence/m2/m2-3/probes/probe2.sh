echo "--- symlinks written by PID1? (/etc read-only in caller's mount ns)"
unshare -m sh -c 'mount -o bind,ro /etc /etc && touch /etc/x 2>&1 | head -1; systemctl --no-ask-password --no-reload enable -- hc-probe-a.service; echo rc=$?'
systemctl is-enabled hc-probe-a.service; systemctl --no-reload disable hc-probe-a.service >/dev/null 2>&1
echo "--- sysv"; ls /usr/lib/systemd/system-generators/ | tr '\n' ' '; echo; ls -l /usr/lib/systemd/systemd-sysv-install 2>&1
echo "--- DropInPaths/SourcePath/NRestarts/ExecMain*"
systemctl show -p DropInPaths,SourcePath,NRestarts,ExecMainCode,ExecMainStatus,ExecMainExitTimestamp,ExecMainStartTimestamp,UnitFilePreset,NeedDaemonReload,Transient,Triggers,TriggeredBy,WantedBy,UpheldBy -- hc-probe-c.service ssh.socket 2>&1
echo "--- show of a socket for type-specific props"; systemctl show -p NRestarts,ExecMainStatus,Listen,Triggers -- dbus.socket
