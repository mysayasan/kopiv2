#!/bin/sh
# Runs after the MyIotSan package is removed or upgraded.
set -e

if command -v systemctl >/dev/null 2>&1; then
    systemctl daemon-reload || true
fi

# On a full purge (deb "purge") drop the service account. Writable state under
# /opt/myiotsan is intentionally LEFT IN PLACE.
#
# /opt/myiotsan/secret/atrest.key encrypts the fleet key and the device credentials held
# in the database. The app fails closed if that key goes missing while encrypted rows
# remain — it refuses to start rather than silently resetting trust, which would mean
# re-provisioning every device (and re-adopting this node into its control plane).
#
# The database also holds the telemetry history, which is the whole point of the appliance
# and is not recoverable from anywhere else.
#
# To fully clean up, remove /opt/myiotsan by hand once you are certain.
if [ "$1" = "purge" ]; then
    if getent passwd myiotsan >/dev/null 2>&1; then
        userdel myiotsan || true
    fi
    if getent group myiotsan >/dev/null 2>&1; then
        groupdel myiotsan || true
    fi
    cat <<'EOF'

MyIotSan removed. Its data was kept at /opt/myiotsan.

That directory holds your telemetry history AND /opt/myiotsan/secret/atrest.key, which
encrypts the fleet key and device credentials in the database. Keep the key and the
database together — losing the key means re-provisioning every device. Delete
/opt/myiotsan by hand only when you are sure you want that.

EOF
fi

exit 0
