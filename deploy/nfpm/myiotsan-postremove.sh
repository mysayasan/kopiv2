#!/bin/sh
# Runs after the MyIotSan package is removed or upgraded.
set -e

if command -v systemctl >/dev/null 2>&1; then
    systemctl daemon-reload || true
fi

# "$1" is the deb action ("remove"/"purge"/"upgrade"/"failed-upgrade"/...) or, on rpm, the
# number of copies left behind (0 on the final erase, 1+ mid-upgrade). Only a final removal
# may touch anything on disk — an upgrade runs this too, and must be a no-op.
case "$1" in
    remove|purge|0) ;;
    *) exit 0 ;;
esac

# Clean wipe is opt-in and explicit. `myiotsan-uninstall --purge-data` exports this, and so
# can an operator: `sudo KOPIV2_PURGE_DATA=1 apt-get purge myiotsan`.
#
# Without it the writable state under /opt/myiotsan is LEFT IN PLACE.
# /opt/myiotsan/secret/atrest.key encrypts the fleet key and the device credentials held in
# the database; the app fails closed if that key goes missing while encrypted rows remain.
# The database also holds the telemetry history, which is the whole point of the appliance
# and is not recoverable from anywhere else.
purge_data=0
if [ "${KOPIV2_PURGE_DATA:-0}" = "1" ]; then
    purge_data=1
fi

# The service account goes on a deb purge (as it always has) or whenever the data is wiped —
# leaving an orphan account behind after a clean wipe is just litter.
if [ "$1" = "purge" ] || [ "$purge_data" = "1" ]; then
    if getent passwd myiotsan >/dev/null 2>&1; then
        userdel myiotsan || true
    fi
    if getent group myiotsan >/dev/null 2>&1; then
        groupdel myiotsan || true
    fi
fi

if [ "$purge_data" = "1" ]; then
    rm -rf /opt/myiotsan
    cat <<'EOF'

MyIotSan removed and /opt/myiotsan erased (database, telemetry history, devices, rules,
users and the encryption key).

Every device must be re-provisioned against a fresh install, and this node must be
re-adopted into its control plane.

EOF
    exit 0
fi

cat <<'EOF'

MyIotSan removed. Its data was kept at /opt/myiotsan.

That directory holds your telemetry history AND /opt/myiotsan/secret/atrest.key, which
encrypts the fleet key and device credentials in the database. Keep the key and the
database together — losing the key means re-provisioning every device. To erase it now:
rm -rf /opt/myiotsan

EOF

exit 0
