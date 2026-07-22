#!/bin/sh
# Runs before the MyIDSan package is removed or upgraded.
set -e

# On a full removal ($1 = "remove" for deb, "0" for rpm) stop and disable the service.
# During an upgrade the postinstall restarts it.
if command -v systemctl >/dev/null 2>&1; then
    if [ "$1" = "remove" ] || [ "$1" = "0" ]; then
        systemctl stop myidsan.service || true
        systemctl disable myidsan.service || true
    fi
fi

exit 0
