#!/bin/sh
# Runs after the MyIDSan package is removed or upgraded.
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

# Clean wipe is opt-in and explicit. `myidsan-uninstall --purge-data` exports this, and so
# can an operator: `sudo KOPIV2_PURGE_DATA=1 apt-get purge myidsan`.
#
# Without it the writable state under /opt/myidsan is LEFT IN PLACE. Every relying app in
# the suite authenticates against this database (users, roles, registered SSO clients), and
# /opt/myidsan/secret/atrest.key encrypts the LDAP directory bind password stored in it.
# Keep the key and the database together.
purge_data=0
if [ "${KOPIV2_PURGE_DATA:-0}" = "1" ]; then
    purge_data=1
fi

# The service account goes on a deb purge (as it always has) or whenever the data is wiped —
# leaving an orphan account behind after a clean wipe is just litter.
if [ "$1" = "purge" ] || [ "$purge_data" = "1" ]; then
    if getent passwd myidsan >/dev/null 2>&1; then
        userdel myidsan || true
    fi
    if getent group myidsan >/dev/null 2>&1; then
        groupdel myidsan || true
    fi
fi

if [ "$purge_data" = "1" ]; then
    rm -rf /opt/myidsan
    cat <<'EOF'

MyIDSan removed and /opt/myidsan erased (users, roles, registered SSO clients and the
at-rest encryption key).

Every relying app in the suite must be re-registered against a fresh install, and any app
still pointing at this IdP will fail to sign users in.

EOF
    exit 0
fi

cat <<'EOF'

MyIDSan removed. Its data was kept at /opt/myidsan.

The database holds every suite user, role, and registered SSO client; deleting it means
re-registering every relying app. To erase it now:  rm -rf /opt/myidsan

EOF

exit 0
