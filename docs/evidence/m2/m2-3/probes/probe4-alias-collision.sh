# M2-3 probe 4: does Alias= let enabling one unit shadow another existing unit?
U=/etc/systemd/system
printf '[Unit]\nDescription=victim in /usr/lib\n[Service]\nExecStart=/bin/sleep 1000\n' > /usr/lib/systemd/system/hc-victim-lib.service
printf '[Unit]\nDescription=victim in /etc\n[Service]\nExecStart=/bin/sleep 1000\n' > $U/hc-victim-etc.service
printf '[Service]\nExecStart=/bin/sleep 1000\n[Install]\nWantedBy=multi-user.target\nAlias=hc-victim-lib.service\n' > $U/hc-shadow-lib.service
printf '[Service]\nExecStart=/bin/sleep 1000\n[Install]\nWantedBy=multi-user.target\nAlias=hc-victim-etc.service\n' > $U/hc-shadow-etc.service
systemctl daemon-reload
for s in lib etc; do
  echo "--- enable hc-shadow-$s (Alias=hc-victim-$s.service)"
  systemctl --no-ask-password --no-reload enable -- hc-shadow-$s.service; echo "rc=$?"
  ls -l $U/hc-victim-$s.service 2>&1
  systemctl daemon-reload
  echo "after reload: hc-victim-$s.service Id=$(systemctl show -p Id --value hc-victim-$s.service) Fragment=$(systemctl show -p FragmentPath --value hc-victim-$s.service) is-enabled=$(systemctl is-enabled hc-victim-$s.service)"
  systemctl --no-reload disable -- hc-shadow-$s.service >/dev/null 2>&1; echo "after disable: $(ls -l $U/hc-victim-$s.service 2>&1 | cut -c1-120)"
done
