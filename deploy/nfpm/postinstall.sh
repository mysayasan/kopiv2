#!/bin/sh
# Runs after the MyMataSan package is installed or upgraded.
set -e

# Dedicated unprivileged service account that owns the install/data dir.
if ! getent group mymatasan >/dev/null 2>&1; then
    groupadd --system mymatasan
fi
if ! getent passwd mymatasan >/dev/null 2>&1; then
    useradd --system --gid mymatasan --home-dir /opt/mymatasan \
        --shell /usr/sbin/nologin --comment "MyMataSan NVR" mymatasan
fi

# The flat install dir holds writable state (db, recordings, logs, key), so the
# service user must own it.
chown -R mymatasan:mymatasan /opt/mymatasan
chmod 750 /opt/mymatasan

# Reload units and (re)start the service.
if command -v systemctl >/dev/null 2>&1; then
    systemctl daemon-reload || true
    systemctl enable mymatasan.service || true
    if systemctl is-active --quiet mymatasan.service; then
        systemctl restart mymatasan.service || true
    else
        systemctl start mymatasan.service || true
    fi
fi

# Tell the operator where the first-run admin login is. On a fresh install the app
# generates a one-time password and prints it to the service log; it is also saved
# to a recovery file in the data dir. (Nothing is printed here on an upgrade — the
# app only seeds on an empty database.)
cat <<'EOF'

MyMataSan is installed. Open https://localhost:3000 to sign in.

First-run admin login (fresh install only):
  - Username: admin
  - Password: shown in the service log and saved to
              /opt/mymatasan/INITIAL_ADMIN_LOGIN.txt
  - View the log:  journalctl -u mymatasan --no-pager | grep -A6 'MyMataSan is ready'
  - You will be asked to set your own password on first sign-in.

To uninstall later:  mymatasan-uninstall
  Your recordings, database and settings under /opt/mymatasan are KEPT by default.
  Add --purge-data to erase them too (a clean wipe; add -y for unattended).

EOF

exit 0
