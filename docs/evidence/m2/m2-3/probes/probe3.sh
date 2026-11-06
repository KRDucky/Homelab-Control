U=/etc/systemd/system
printf '[Service]\nExecStart=/bin/sleep 1000\n[Install]\nWantedBy=multi-user.target\n' > $U/hc-p3.service
printf '[Service]\nExecStart=/bin/sleep 1000\n[Install]\nWantedBy=multi-user.target\n' > $U/hc-p4.service
mkdir -p $U/hc-p4.service.d; printf '[Install]\nAlias=hc-p4-dropin-alias.service\n' > $U/hc-p4.service.d/inst.conf
systemctl daemon-reload
echo "fresh NeedDaemonReload p3=$(systemctl show -p NeedDaemonReload --value hc-p3.service) ssh.socket=$(systemctl show -p NeedDaemonReload --value ssh.socket 2>/dev/null) sshd.service=$(systemctl show -p NeedDaemonReload --value sshd.service)"
systemctl --no-reload enable hc-p3.service >/dev/null 2>&1
echo "after --no-reload enable NeedDaemonReload p3=$(systemctl show -p NeedDaemonReload --value hc-p3.service)"
systemctl --no-reload disable hc-p3.service >/dev/null 2>&1
echo "after --no-reload disable NeedDaemonReload p3=$(systemctl show -p NeedDaemonReload --value hc-p3.service)"
echo "--- drop-in [Install]"; systemctl --no-reload enable hc-p4.service 2>&1; ls $U | grep hc-p4
systemctl --no-reload disable hc-p4.service >/dev/null 2>&1
