#!/bin/sh
# Runs after the MyIotSan package is installed or upgraded.
set -e

# Dedicated unprivileged service account that owns the install/data dir.
if ! getent group myiotsan >/dev/null 2>&1; then
    groupadd --system myiotsan
fi
if ! getent passwd myiotsan >/dev/null 2>&1; then
    useradd --system --gid myiotsan --home-dir /opt/myiotsan \
        --shell /usr/sbin/nologin --comment "MyIotSan IoT device hub" myiotsan
fi

# The flat install dir holds writable state (db, telemetry, logs, certs, and
# secret/atrest.key — which encrypts the fleet key and device credentials), so the service
# user must own it.
chown -R myiotsan:myiotsan /opt/myiotsan
chmod 750 /opt/myiotsan

# Reload units and (re)start the service.
if command -v systemctl >/dev/null 2>&1; then
    systemctl daemon-reload || true
    systemctl enable myiotsan.service || true
    if systemctl is-active --quiet myiotsan.service; then
        systemctl restart myiotsan.service || true
    else
        systemctl start myiotsan.service || true
    fi
fi

# Tell the operator where the first-run admin login is. On a fresh install the app
# generates a one-time password and prints it to the service log; it is also saved to a
# recovery file in the data dir. (Nothing is seeded on an upgrade — the app only seeds
# on an empty database.)
cat <<'EOF'

MyIotSan is installed. Open https://localhost:3003 to sign in.

First-run superadmin login (fresh install only):
  - Username: admin
  - Password: shown in the service log and saved to
              /opt/myiotsan/INITIAL_ADMIN_LOGIN.txt
  - View the log:  journalctl -u myiotsan --no-pager | tail -40
  - You will be asked to set your own password on first sign-in.

The embedded MQTT broker listens on 1883/tcp. Devices cannot connect until that port
is allowed through the host firewall, e.g.:
  ufw allow 1883/tcp          (Debian/Ubuntu)
  firewall-cmd --add-port=1883/tcp --permanent && firewall-cmd --reload   (RHEL/Fedora)

A device that is not in the inventory cannot connect at all — onboard devices through
the time-boxed enrollment window in the UI (Discovery), not by editing config.

To uninstall later:  myiotsan-uninstall
  Your telemetry history, devices and rules under /opt/myiotsan are KEPT by default.
  Add --purge-data to erase them too (a clean wipe; add -y for unattended).

EOF

exit 0
