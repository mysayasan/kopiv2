#!/bin/sh
# Runs after the MySeliaSan package is removed or upgraded.
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

# Clean wipe is opt-in and explicit. `myseliasan-uninstall --purge-data` exports this, and so
# can an operator: `sudo KOPIV2_PURGE_DATA=1 apt-get purge myseliasan`.
#
# Without it the writable state under /opt/myseliasan is LEFT IN PLACE, which matters more
# here than it does on a node: /opt/myseliasan/secret/atrest.key encrypts the fleet CA
# private key and the pairing PSK stored in the database. The app fails closed if that key
# goes missing while encrypted rows remain — it refuses to start rather than silently
# resetting the fleet's trust, which would orphan every adopted node.
purge_data=0
if [ "${KOPIV2_PURGE_DATA:-0}" = "1" ]; then
    purge_data=1
fi

# The service account goes on a deb purge (as it always has) or whenever the data is wiped —
# leaving an orphan account behind after a clean wipe is just litter.
if [ "$1" = "purge" ] || [ "$purge_data" = "1" ]; then
    if getent passwd myseliasan >/dev/null 2>&1; then
        userdel myseliasan || true
    fi
    if getent group myseliasan >/dev/null 2>&1; then
        groupdel myseliasan || true
    fi
fi

if [ "$purge_data" = "1" ]; then
    rm -rf /opt/myseliasan
    cat <<'EOF'

MySeliaSan removed and /opt/myseliasan erased (database, users, settings and the fleet
encryption key).

The fleet CA key and pairing PSK are gone with it: every previously adopted node must be
re-adopted into a new control plane.

EOF
    exit 0
fi

cat <<'EOF'

MySeliaSan removed. Its data was kept at /opt/myseliasan.

/opt/myseliasan/secret/atrest.key encrypts the fleet CA key and pairing PSK held in the
database. Keep it (and the database) together — losing the key means every adopted node
must be re-adopted. To erase it now:  rm -rf /opt/myseliasan

EOF

exit 0
