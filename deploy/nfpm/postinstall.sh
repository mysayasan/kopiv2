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

exit 0
