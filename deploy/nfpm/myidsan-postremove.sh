#!/bin/sh
# Runs after the MyIDSan package is removed or upgraded.
set -e

if command -v systemctl >/dev/null 2>&1; then
    systemctl daemon-reload || true
fi

# On a full purge (deb "purge") drop the service account. Writable state under
# /opt/myidsan is intentionally LEFT IN PLACE.
#
# Every relying app in the suite authenticates against this database (users, roles,
# registered SSO clients), and /opt/myidsan/secret/atrest.key encrypts the LDAP
# directory bind password stored in it. Keep the key and the database together.
#
# To fully clean up, remove /opt/myidsan by hand once you are certain.
if [ "$1" = "purge" ]; then
    if getent passwd myidsan >/dev/null 2>&1; then
        userdel myidsan || true
    fi
    if getent group myidsan >/dev/null 2>&1; then
        groupdel myidsan || true
    fi
    cat <<'EOF'

MyIDSan removed. Its data was kept at /opt/myidsan.

The database holds every suite user, role, and registered SSO client; deleting it
means re-registering every relying app. Delete /opt/myidsan by hand only when you
are sure you want that.

EOF
fi

exit 0
