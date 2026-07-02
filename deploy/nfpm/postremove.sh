#!/bin/sh
# Runs after the MyMataSan package is removed or upgraded.
set -e

if command -v systemctl >/dev/null 2>&1; then
    systemctl daemon-reload || true
fi

# On a full purge (deb "purge") drop the service account. Writable state under
# /opt/mymatasan (recordings, database, key) is intentionally left in place so an
# accidental remove never destroys footage; delete it manually to fully clean up.
if [ "$1" = "purge" ]; then
    if getent passwd mymatasan >/dev/null 2>&1; then
        userdel mymatasan || true
    fi
    if getent group mymatasan >/dev/null 2>&1; then
        groupdel mymatasan || true
    fi
fi

exit 0
