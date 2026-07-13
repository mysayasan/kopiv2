#!/bin/sh
# Runs after the MySeliaSan package is removed or upgraded.
set -e

if command -v systemctl >/dev/null 2>&1; then
    systemctl daemon-reload || true
fi

# On a full purge (deb "purge") drop the service account. Writable state under
# /opt/myseliasan is intentionally LEFT IN PLACE.
#
# This matters more here than it does for a node: /opt/myseliasan/secret/atrest.key
# encrypts the fleet CA private key and the pairing PSK stored in the database. The app
# fails closed if that key goes missing while encrypted rows remain — it refuses to
# start rather than silently resetting the fleet's trust, which would orphan every
# adopted node. Deleting it means re-adopting the entire fleet.
#
# To fully clean up, remove /opt/myseliasan by hand once you are certain.
if [ "$1" = "purge" ]; then
    if getent passwd myseliasan >/dev/null 2>&1; then
        userdel myseliasan || true
    fi
    if getent group myseliasan >/dev/null 2>&1; then
        groupdel myseliasan || true
    fi
    cat <<'EOF'

MySeliaSan removed. Its data was kept at /opt/myseliasan.

/opt/myseliasan/secret/atrest.key encrypts the fleet CA key and pairing PSK held in
the database. Keep it (and the database) together — losing the key means every
adopted node must be re-adopted. Delete /opt/myseliasan by hand only when you are
sure you want that.

EOF
fi

exit 0
