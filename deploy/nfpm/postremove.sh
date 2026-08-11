#!/bin/sh
# Runs after the MyMataSan package is removed or upgraded.
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

# Clean wipe is opt-in and explicit. `mymatasan-uninstall --purge-data` exports this, and so
# can an operator: `sudo KOPIV2_PURGE_DATA=1 apt-get purge mymatasan`. Without it the
# writable state under /opt/mymatasan (recordings, database, key) is left in place, so an
# accidental remove never destroys footage.
purge_data=0
if [ "${KOPIV2_PURGE_DATA:-0}" = "1" ]; then
    purge_data=1
fi

# The service account goes on a deb purge (as it always has) or whenever the data is wiped —
# leaving an orphan account behind after a clean wipe is just litter.
if [ "$1" = "purge" ] || [ "$purge_data" = "1" ]; then
    if getent passwd mymatasan >/dev/null 2>&1; then
        userdel mymatasan || true
    fi
    if getent group mymatasan >/dev/null 2>&1; then
        groupdel mymatasan || true
    fi
fi

if [ "$purge_data" = "1" ]; then
    rm -rf /opt/mymatasan
    cat <<'EOF'

MyMataSan removed and /opt/mymatasan erased (recordings, database, settings and the
at-rest encryption key).

Recordings stored on another disk, if you configured one, were NOT touched.

EOF
    exit 0
fi

cat <<'EOF'

MyMataSan removed. Its data was kept at /opt/mymatasan.

That directory holds your recordings, the database and the at-rest encryption key, so a
reinstall picks up exactly where you left off. To erase it now:  rm -rf /opt/mymatasan

EOF

exit 0
