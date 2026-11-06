# M2-3 probe 6: drop-in handling on older systemd (cat as an unprivileged user; show DropInPaths; Alias in drop-in).
U=/etc/systemd/system
systemctl --version | head -1
printf '[Service]\nExecStart=/bin/sleep 1000\n[Install]\nWantedBy=multi-user.target\n' > $U/hc-p6.service
mkdir -p $U/hc-p6.service.d
printf '[Service]\nEnvironment=X=fixture\n[Install]\nAlias=hc-p6-alias.service\n' > $U/hc-p6.service.d/secret.conf; chmod 600 $U/hc-p6.service.d/secret.conf
systemctl daemon-reload
echo "--- show DropInPaths/FragmentPath"; systemctl show --no-pager -p Id,FragmentPath,DropInPaths,SourcePath -- hc-p6.service
echo "--- cat as nobody: stdout"; su -s /bin/sh nobody -c 'systemctl cat --no-pager -- hc-p6.service 2>/tmp/err; echo "rc=$?"'; echo "--- stderr:"; cat /tmp/err
echo "--- is-enabled of free alias"; systemctl is-enabled -- hc-p6-alias.service; echo "rc=$?"
echo "--- show of free alias"; systemctl show --no-pager -p Id,LoadState -- hc-p6-alias.service; echo "rc=$?"
echo "--- enable (alias from drop-in?)"; systemctl --no-ask-password --no-reload enable -- hc-p6.service; echo "rc=$?"; ls $U | grep hc-p6
