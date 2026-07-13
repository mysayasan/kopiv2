#!/bin/sh
# Runs after the MySeliaSan package is installed or upgraded.
set -e

# Dedicated unprivileged service account that owns the install/data dir.
if ! getent group myseliasan >/dev/null 2>&1; then
    groupadd --system myseliasan
fi
if ! getent passwd myseliasan >/dev/null 2>&1; then
    useradd --system --gid myseliasan --home-dir /opt/myseliasan \
        --shell /usr/sbin/nologin --comment "MySeliaSan fleet control plane" myseliasan
fi

# The flat install dir holds writable state (db, logs, certs, and secret/atrest.key —
# which encrypts the fleet CA key and PSK), so the service user must own it.
chown -R myseliasan:myseliasan /opt/myseliasan
chmod 750 /opt/myseliasan

# Reload units and (re)start the service.
if command -v systemctl >/dev/null 2>&1; then
    systemctl daemon-reload || true
    systemctl enable myseliasan.service || true
    if systemctl is-active --quiet myseliasan.service; then
        systemctl restart myseliasan.service || true
    else
        systemctl start myseliasan.service || true
    fi
fi

# Tell the operator where the first-run admin login is. On a fresh install the app
# generates a one-time password and prints it to the service log; it is also saved to a
# recovery file in the data dir. (Nothing is seeded on an upgrade — the app only seeds
# on an empty database.)
cat <<'EOF'

MySeliaSan is installed. Open https://localhost:3002 to sign in.

First-run superadmin login (fresh install only):
  - Username: admin
  - Password: shown in the service log and saved to
              /opt/myseliasan/INITIAL_ADMIN_LOGIN.txt
  - View the log:  journalctl -u myseliasan --no-pager | grep -A6 'MySeliaSan is ready'
  - You will be asked to set your own password on first sign-in.

Before adopting nodes, edit /opt/myseliasan/config.json and set
pairing.parentBaseUrl to a LAN-reachable URL for THIS host (e.g.
https://192.168.1.10:3002) — adopted nodes store it and dial back to it, so
leaving it as localhost silently breaks adoption from other machines.

EOF

exit 0
