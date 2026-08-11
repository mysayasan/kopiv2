#!/bin/sh
# One-command uninstaller for the kopiv2 suite appliances, installed by the .deb/.rpm
# packages as /usr/sbin/<app>-uninstall (mymatasan, myseliasan, myiotsan, myidsan).
#
# WHY THIS EXISTS. `apt-get remove` / `dnf remove` deliberately leave /opt/<app> behind —
# an accidental remove must never destroy footage, a fleet CA, or a telemetry history. That
# is the right default, but it left no automated path to the other outcome: a genuine clean
# wipe (test rigs, decommissioning a box, handing hardware on). Deleting /opt/<app> by hand
# was the only option, and nothing told the operator that. This script is the front door for
# both outcomes, mirroring the Windows uninstaller's /CLEANDATA and /KEEPDATA switches
# (packaging/windows/<app>.iss) so the two platforms behave the same way.
#
#   <app>-uninstall                 remove the package, KEEP /opt/<app>   (default)
#   <app>-uninstall --purge-data    remove the package and ERASE /opt/<app>
#   <app>-uninstall --purge-data -y unattended clean wipe (no prompt)
#
# The wipe itself is done by the package's postremove, which honours KOPIV2_PURGE_DATA=1
# (exported below) — so `sudo KOPIV2_PURGE_DATA=1 apt-get purge <app>` works identically
# without this script. The post-check at the end covers the paths where the environment
# does not reach the scriptlet.
#
# ONE FILE, FOUR PACKAGES: the app is derived from the installed name (argv[0]), so there is
# no per-app copy to drift. Everything app-specific is the case block in AppFacts().
set -e

# --- identify which product this copy is uninstalling -----------------------------------
# After the re-exec below argv[0] is a temp path, so the resolved name is carried in the
# environment instead.
if [ -n "${KOPIV2_UNINSTALL_APP:-}" ]; then
    app=$KOPIV2_UNINSTALL_APP
else
    prog=$(basename "$0")
    app=${prog%-uninstall}
fi

case "$app" in
    mymatasan|myseliasan|myiotsan|myidsan) ;;
    *)
        echo "$(basename "$0"): cannot tell which product to uninstall." >&2
        echo "Run this as /usr/sbin/<app>-uninstall, or set KOPIV2_UNINSTALL_APP=<app>." >&2
        exit 2
        ;;
esac

data_dir=/opt/$app

# What the operator loses by wiping the data dir. Deliberately concrete: "the database" is
# not a warning, "every adopted node must be re-adopted" is.
case "$app" in
    mymatasan)
        product="MyMataSan"
        stakes="all recordings, the database, settings and the at-rest encryption key"
        extra="Recordings stored OUTSIDE $data_dir (a separate disk configured in the app)
are NOT touched — delete those by hand."
        ;;
    myseliasan)
        product="MySeliaSan"
        stakes="the database, users, settings and the fleet encryption key"
        extra="The key encrypts the fleet CA key and the pairing PSK: erasing it means every
adopted node must be re-adopted."
        ;;
    myiotsan)
        product="MyIotSan"
        stakes="the database, your telemetry history, devices, rules, users and the key"
        extra="The key encrypts the fleet key and device credentials: erasing it means every
device must be re-provisioned. The telemetry history is not stored anywhere else."
        ;;
    myidsan)
        product="MyIDSan"
        stakes="the database of users, roles and registered apps, and the at-rest key"
        extra="Every relying app in the suite authenticates against this database: erasing it
means re-registering every app."
        ;;
esac

# --- re-exec from a private copy --------------------------------------------------------
# This script is package-owned, so the package manager unlinks it partway through the run.
# A running shell keeps reading the unlinked inode on Linux, but a destructive script should
# not lean on that, so the real work happens from a temp copy.
if [ "${KOPIV2_UNINSTALL_REEXEC:-}" != "1" ]; then
    tmp=$(mktemp 2>/dev/null) || tmp=/tmp/$app-uninstall.$$
    cat "$0" >"$tmp"
    chmod 700 "$tmp"
    KOPIV2_UNINSTALL_REEXEC=1
    KOPIV2_UNINSTALL_APP=$app
    export KOPIV2_UNINSTALL_REEXEC KOPIV2_UNINSTALL_APP
    exec /bin/sh "$tmp" "$@"
fi
# From here on $0 is the temp copy — remove it however we exit.
trap 'rm -f "$0"' EXIT INT TERM

usage() {
    cat <<EOF
Usage: $app-uninstall [--purge-data] [--yes] [--dry-run]

Removes the $product package. By default your data in $data_dir is KEPT, so a
later reinstall picks up where you left off.

  --purge-data   also erase $data_dir
                 ($stakes)
  --keep-data    keep $data_dir (the default; stated explicitly for scripts)
  -y, --yes      do not ask for confirmation (required when there is no terminal)
  -n, --dry-run  print what would run, change nothing
  -h, --help     this text
EOF
}

purge_data=0
assume_yes=0
dry_run=0
while [ $# -gt 0 ]; do
    case "$1" in
        --purge-data|--purge) purge_data=1 ;;
        --keep-data|--keep) purge_data=0 ;;
        -y|--yes|--assume-yes) assume_yes=1 ;;
        -n|--dry-run) dry_run=1 ;;
        -h|--help) usage; exit 0 ;;
        *) echo "$app-uninstall: unknown option '$1'" >&2; usage >&2; exit 2 ;;
    esac
    shift
done

# A dry run only reads and prints, so it does not need root — being able to preview the
# destructive command without sudo is the point of it.
if [ "$dry_run" != "1" ] && [ "$(id -u)" != "0" ]; then
    echo "$app-uninstall: must run as root — try: sudo $app-uninstall" >&2
    exit 1
fi

run() {
    if [ "$dry_run" = "1" ]; then
        echo "  would run: $*"
        return 0
    fi
    "$@"
}

# --- how was it installed? --------------------------------------------------------------
# Package-manager detection is by what actually owns the package, not by which binaries
# exist: some hosts have both dpkg and rpm.

# deb_state prints the third field of dpkg's Status ("<desired> <error> <state>") — the
# current state. `set --` is scoped to the function, so the caller's arguments survive.
deb_state() {
    st=$(dpkg-query -W -f='${Status}' "$1" 2>/dev/null) || return 0
    # shellcheck disable=SC2086  # deliberate word split: Status is three plain words.
    set -- $st
    [ $# -ge 3 ] && printf '%s' "$3"
    return 0
}

pm=none
if command -v dpkg-query >/dev/null 2>&1; then
    # Anything other than "not-installed" means dpkg still owns something here — including
    # `half-configured` (a postinst that failed) and `config-files` (removed, not purged).
    # Matching only the happy "installed" state would strand exactly the broken installs
    # that most need removing.
    case "$(deb_state "$app")" in
        ''|not-installed) ;;
        *) pm=deb ;;
    esac
fi
if [ "$pm" = "none" ] && command -v rpm >/dev/null 2>&1 && rpm -q "$app" >/dev/null 2>&1; then
    pm=rpm
fi

if [ "$pm" = "none" ] && [ ! -d "$data_dir" ]; then
    echo "$product is not installed as a package and $data_dir does not exist. Nothing to do."
    exit 0
fi

# --- confirm ----------------------------------------------------------------------------
echo
if [ "$purge_data" = "1" ]; then
    echo "About to remove $product AND ERASE $data_dir."
    echo "This deletes $stakes."
    echo
    printf '%s\n' "$extra"
    echo
    echo "This cannot be undone."
else
    echo "About to remove $product. Your data in $data_dir will be KEPT."
    echo "(Re-run with --purge-data to erase it as well.)"
fi
echo

if [ "$assume_yes" != "1" ] && [ "$dry_run" != "1" ]; then
    if [ ! -t 0 ]; then
        echo "$app-uninstall: no terminal to confirm on — pass --yes to proceed." >&2
        exit 1
    fi
    if [ "$purge_data" = "1" ]; then
        printf 'Type ERASE to confirm, anything else to abort: '
        read -r answer
        [ "$answer" = "ERASE" ] || { echo "Aborted. Nothing was changed."; exit 1; }
    else
        printf 'Continue? [y/N] '
        read -r answer
        case "$answer" in y|Y|yes|YES) ;; *) echo "Aborted. Nothing was changed."; exit 1 ;; esac
    fi
fi

# The postremove scriptlet reads this; on deb it also decides whether the service account is
# dropped. Exported before the package manager runs so it reaches the scriptlet through
# apt -> dpkg (or dnf -> rpm).
if [ "$purge_data" = "1" ]; then
    KOPIV2_PURGE_DATA=1
    export KOPIV2_PURGE_DATA
fi

# --- remove the package -----------------------------------------------------------------
case "$pm" in
    deb)
        # `purge` (not `remove`) so the config.json marked config|noreplace goes too — a
        # kept config would otherwise resurrect stale settings on a reinstall.
        if [ "$purge_data" = "1" ]; then
            if command -v apt-get >/dev/null 2>&1; then
                run env DEBIAN_FRONTEND=noninteractive apt-get purge -y "$app"
            else
                run dpkg --purge "$app"
            fi
        else
            if command -v apt-get >/dev/null 2>&1; then
                run env DEBIAN_FRONTEND=noninteractive apt-get remove -y "$app"
            else
                run dpkg --remove "$app"
            fi
        fi
        ;;
    rpm)
        if command -v dnf >/dev/null 2>&1; then
            run dnf remove -y "$app"
        elif command -v yum >/dev/null 2>&1; then
            run yum remove -y "$app"
        elif command -v zypper >/dev/null 2>&1; then
            run zypper --non-interactive remove "$app"
        else
            run rpm -e "$app"
        fi
        ;;
    none)
        echo "No $app package is installed — only the data directory will be handled."
        ;;
esac

# --- finish the wipe --------------------------------------------------------------------
# Belt and braces: the postremove already did this when it saw KOPIV2_PURGE_DATA, but the
# environment does not reach every scriptlet on every distro, and the `none` path has no
# scriptlet at all. Removing an already-removed tree is a no-op.
if [ "$purge_data" = "1" ]; then
    if [ -d "$data_dir" ]; then
        run rm -rf "$data_dir"
    fi
    if [ "$dry_run" != "1" ]; then
        if getent passwd "$app" >/dev/null 2>&1; then
            userdel "$app" >/dev/null 2>&1 || true
        fi
        if getent group "$app" >/dev/null 2>&1; then
            groupdel "$app" >/dev/null 2>&1 || true
        fi
    fi
fi

if command -v systemctl >/dev/null 2>&1; then
    run systemctl daemon-reload || true
fi

echo
if [ "$dry_run" = "1" ]; then
    echo "Dry run only — nothing was changed."
elif [ "$purge_data" = "1" ]; then
    echo "$product removed and $data_dir erased."
    case "$app" in
        mymatasan) echo "Recordings on other disks (if any) were left alone." ;;
    esac
else
    echo "$product removed. Your data was kept at $data_dir."
    echo "Reinstalling the package picks it up again."
    echo "To erase it later:  rm -rf $data_dir"
fi
echo

exit 0
