#!/bin/sh
# Runs after the MyIDSan package is installed or upgraded.
set -e

# Dedicated unprivileged service account that owns the install/data dir.
if ! getent group myidsan >/dev/null 2>&1; then
    groupadd --system myidsan
fi
if ! getent passwd myidsan >/dev/null 2>&1; then
    useradd --system --gid myidsan --home-dir /opt/myidsan \
        --shell /usr/sbin/nologin --comment "MyIDSan identity provider" myidsan
fi

# The flat install dir holds writable state (db, logs, certs, and secret/atrest.key —
# which encrypts the LDAP directory bind password), so the service user must own it.
chown -R myidsan:myidsan /opt/myidsan
chmod 750 /opt/myidsan

# Reload units and (re)start the service.
if command -v systemctl >/dev/null 2>&1; then
    systemctl daemon-reload || true
    systemctl enable myidsan.service || true
    if systemctl is-active --quiet myidsan.service; then
        systemctl restart myidsan.service || true
    else
        systemctl start myidsan.service || true
    fi
fi

# Tell the operator where the first-run admin login is. On a fresh install the app
# generates a one-time password and prints it to the service log; it is also saved to a
# recovery file in the data dir. (Nothing is seeded on an upgrade — the app only seeds
# on an empty database.)
cat <<'EOF'

MyIDSan is installed. Open https://localhost:3001 to sign in.

First-run superadmin login (fresh install only):
  - Username: admin
  - Password: shown in the service log and saved to
              /opt/myidsan/INITIAL_ADMIN_LOGIN.txt
  - View the log:  journalctl -u myidsan --no-pager | grep -A6 'MyIDSan is ready'
  - You will be asked to set your own password on first sign-in, then a short
    setup wizard registers your first relying app (myseliasan, mymatasan, ...).

Locked out? Create an empty file /opt/myidsan/RESET_ADMIN and restart the
service — the bootstrap login is force-reset and announced again.

To uninstall later:  myidsan-uninstall
  Your users, roles and registered apps under /opt/myidsan are KEPT by default.
  Add --purge-data to erase them too (a clean wipe; add -y for unattended).

EOF

exit 0
