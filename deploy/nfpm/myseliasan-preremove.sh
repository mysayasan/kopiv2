#!/bin/sh
# Runs before the MySeliaSan package is removed or upgraded.
set -e

# On a full removal ($1 = "remove" for deb, "0" for rpm) stop and disable the service.
# During an upgrade the postinstall restarts it.
if command -v systemctl >/dev/null 2>&1; then
    if [ "$1" = "remove" ] || [ "$1" = "0" ]; then
        systemctl stop myseliasan.service || true
        systemctl disable myseliasan.service || true
    fi
fi

exit 0
