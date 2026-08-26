# myidsan bench: the disaster recovery the whole suite depends on.
#
# WHY THIS ONE. myidsan is the single app where losing the database locks every employee
# out of every OTHER app at once. It holds every account, every role, every registered SSO
# client, the SSO CA private key, every TOTP secret and the LDAP bind password. Its backup
# is therefore not a convenience feature; it is the recovery plan for the whole estate, and
# it has never been exercised end to end by anything but hand-driven curl.
#
# THE CLAIM THAT MATTERS, and the one that cannot be tested on a single host: TOTP secrets
# and the LDAP bind password are sealed with the HOST's at-rest key. Copy the sealed bytes
# into an archive and the restore reports success, then fails every second-factor check —
# discovered only when someone tries to log in, on the worst day. So the design unseals on
# export (into the passphrase-encrypted archive) and RE-SEALS with the destination host's
# own key on restore. Proving that needs a SECOND SERVER WITH A DIFFERENT KEY, which is
# what this bench stands up.
#
# THE CLAIMS UNDER TEST:
#
#   1. a backup taken on one host restores onto a DIFFERENT host with a DIFFERENT at-rest
#      key, and afterwards the pre-disaster password, the pre-disaster authenticator and
#      the pre-disaster recovery codes all still work;
#   2. the replay guard travels with it — a code already spent before the disaster is still
#      refused after it;
#   3. the restored server is still a working identity server: its superadmin is still a
#      superadmin, its relying apps are still registered, and all of that survives a REBOOT
#      rather than only holding until the process restarts;
#   4. every refusal refuses: a wrong passphrase, an EMPTY passphrase, a file that is not a
#      backup, a truncated archive, an export below the passphrase minimum, and an export
#      without a fresh credential;
#   5. merge mode ("Keep both", offered in the UI in four languages) actually works against
#      a server that already holds records — and when a restore does fail, it says what it
#      had already written instead of leaking a database constraint string;
#   6. a record whose parent is absent is SKIPPED and counted, never attached to whatever
#      row happens to occupy that id on the new host;
#   7. the audit trail is not part of the backup and is NOT erased by a restore — so a
#      restore cannot be used to wipe the record of itself.
#
#   python tools/fleetbench/idsan_harness.py       # the primary + a relying app + redis
#   python tools/fleetbench/bench_idsan_backup.py  # stands up its own DR host, tears it down
import base64
import hashlib
import hmac
import io
import json
import os
import shutil
import struct
import sys
import time

import requests
import urllib3

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from fleet_harness import ROOT, result_list, sh, wait_up
from idsan_harness import (BENCH_PASS, IDSAN_URL, INTERNAL_TOKEN, LOCAL_LOGIN, NET, WORK,
                           Client, admin, app_config, host_ip, start, write)

urllib3.disable_warnings()
CHECKS = []

DR_NAME = "idsan-dr"
DR_TLS_PORT = 3003
DR_HOST_PORT = 18453
DR_URL = "https://%s:%d" % (host_ip(), DR_HOST_PORT)

# The archive passphrase. Its 12-character minimum is the BACKUP's own rule, not the
# password policy's — a different control with a different number, and using one to test
# the other proves the wrong refusal.
ARCHIVE_PASS = "bench-archive-passphrase"
ALL_SECTIONS = ["access", "identity", "mfa", "apps", "federation", "ssoca"]


def check(name, ok, detail=""):
    CHECKS.append((name, bool(ok), detail))
    print(("PASS  " if ok else "FAIL  ") + name + ("   " + detail if detail else ""))


def report():
    ok = sum(1 for _, good, _ in CHECKS if good)
    print("\n%d/%d checks passed" % (ok, len(CHECKS)))
    for name, good, detail in CHECKS:
        if not good:
            print("  FAILED: %s   %s" % (name, detail))
    return 0 if ok == len(CHECKS) else 1


def brief(r):
    return "%d %s" % (r.status_code, r.text[:170].replace("\n", " "))


def result_of(r):
    try:
        return r.json().get("result")
    except ValueError:
        return None


# ---- TOTP ------------------------------------------------------------------------------
#
# Hand-rolled rather than pyotp so the bench cannot pass by sharing a library with the thing
# under test.

def _step_now():
    return int(time.time() // 30)


def _code_for(secret, step):
    key = base64.b32decode(secret.upper() + "=" * (-len(secret) % 8))
    mac = hmac.new(key, struct.pack(">Q", step), hashlib.sha1).digest()
    off = mac[-1] & 0x0F
    return "%06d" % ((struct.unpack(">I", mac[off:off + 4])[0] & 0x7FFFFFFF) % 1000000)


class Totp:
    """A code generator that respects the server's REPLAY GUARD.

    THIS COST AN HOUR, so it is written down. myidsan refuses any time-step <= the last one
    it accepted, and the accepted step RIDES ALONG IN THE BACKUP. A first draft of this
    bench spent a code for step N+1, restored, waited 31 seconds and presented a code for
    step N — which the restored host correctly refused as a replay. Read naively that looks
    exactly like the at-rest re-sealing having failed, which is the headline defect this
    whole bench exists to detect. It would have been reported as one.

    So: never spend a future step, and never generate a code without waiting for a step
    strictly beyond the last one consumed."""

    def __init__(self, secret):
        self.secret = secret
        self.last = -1
        self.last_code = ""

    def fresh(self):
        while _step_now() <= self.last:
            time.sleep(1)
        self.last = _step_now()
        self.last_code = _code_for(self.secret, self.last)
        return self.last_code

    def spent(self):
        """The most recently consumed code — for testing that a replay is refused."""
        return self.last_code


# ---- the DR host -----------------------------------------------------------------------

def start_dr():
    """A SECOND myidsan, with its own data directory and therefore its own at-rest key.

    That last part is the entire point and is worth being explicit about: the key is minted
    per data directory on first boot, so two containers with two volumes disagree about it
    without anything having to arrange that. The bench asserts the disagreement rather than
    assuming it — if the two hosts ever shared a key, every cross-host claim below would
    pass for the wrong reason and prove nothing at all.

    Its redis DB index is 1, not 0. A restore wipes the `sso:session:` prefix, and sharing
    an index would let this host's recovery silently sign every user out of the OTHER one —
    modelling two independent servers, which is the situation being recovered from."""
    sh("docker", "rm", "-f", DR_NAME, check=False)
    shutil.rmtree(os.path.join(ROOT, DR_NAME), ignore_errors=True)

    data = os.path.join(ROOT, DR_NAME)
    certs = os.path.join(data, "certs")
    os.makedirs(certs, exist_ok=True)
    for src, dst in ((os.path.join(WORK, "bench.crt"), "cert.pem"),
                     (os.path.join(WORK, "bench.key"), "key.pem")):
        io.open(os.path.join(certs, dst), "wb").write(io.open(src, "rb").read())

    cfg = app_config("myidsan", DR_TLS_PORT)
    cfg["sso"]["internalToken"] = INTERNAL_TOKEN
    cfg["cache"] = dict(cfg.get("cache") or {})
    cfg["cache"]["provider"] = "redis"
    cfg["cache"]["redis"] = {"address": "idsan-redis:6379", "password": "", "db": 1}
    write(os.path.join(data, "config.json"), cfg)
    start(DR_NAME, "myidsan", DR_TLS_PORT, DR_HOST_PORT)
    return wait_up(DR_URL + "/api/auth/session")


def atrest_key(name):
    path = os.path.join(ROOT, name, "secret", "atrest.key")
    if not os.path.exists(path):
        return ""
    return hashlib.sha256(io.open(path, "rb").read()).hexdigest()


def sign_in_dr(totp, label="dr"):
    """Sign in to the restored host with the PRE-DISASTER credentials."""
    c = Client(DR_URL)
    r = c.s.post(DR_URL + "/api/login/default",
                 json={"username": "admin", "password": BENCH_PASS}, timeout=30)
    out = result_of(r) or {}
    if not out.get("mfaToken"):
        return c, r, out
    r = c.s.post(DR_URL + "/api/login/mfa",
                 json={"mfaToken": out["mfaToken"], "code": totp.fresh()}, timeout=30)
    return c, r, out


def api(base, cli, path, body=None, method="POST", timeout=120):
    return cli.s.request(method, base + path, json=body,
                         headers={"X-CSRF-Token": cli.csrf()}, timeout=timeout)


def elevate(base, cli, totp=None):
    """Step-up, which export and restore both sit behind."""
    body = {"password": BENCH_PASS}
    if totp is not None:
        body["code"] = totp.fresh()
    return api(base, cli, "/api/step-up", body)


def main():
    # ---- (0) two hosts, two keys -----------------------------------------------------------
    check("a second myidsan stands up as the recovery target", start_dr())
    primary_key, dr_key = atrest_key("idsan"), atrest_key(DR_NAME)
    check("and it has a DIFFERENT at-rest key — without this every cross-host claim below "
          "would pass for the wrong reason",
          bool(primary_key) and bool(dr_key) and primary_key != dr_key,
          "%s vs %s" % (primary_key[:16], dr_key[:16]))

    # ---- (1) give the primary something worth recovering ------------------------------------
    p = admin(IDSAN_URL, *LOCAL_LOGIN["myidsan"])

    roles = result_of(api(IDSAN_URL, p, "/api/access-rbac/roles", method="GET")) or []
    role_names = sorted(r.get("name") for r in roles if isinstance(r, dict))
    viewer = next((r["id"] for r in roles if isinstance(r, dict) and r.get("name") == "viewer"), 0)
    check("the primary has the stock roles to recover", len(roles) >= 2, json.dumps(role_names))

    r = api(IDSAN_URL, p, "/api/user-credential", {
        "email": "recovered.user@bench.test", "userpwd": "Bench!2345678",
        "firstName": "Recovered", "lastName": "User", "userRoleId": viewer, "isActive": True})
    check("a second account exists to be recovered alongside the superadmin",
          r.status_code == 200, brief(r))

    r = api(IDSAN_URL, p, "/api/app-registry", {
        "code": "benchapp2", "title": "Bench App Two", "description": "a second relying app",
        "baseUrl": "https://benchapp2.invalid", "audience": "benchapp2", "isActive": True})
    check("and a second relying app, so the app section carries more than one row",
          r.status_code == 200, brief(r))

    # A second factor on the superadmin — the thing the at-rest key seals, and the whole
    # reason a cross-host restore is hard.
    enroll = result_of(api(IDSAN_URL, p, "/api/mfa/enroll", {"label": "pre-disaster"})) or {}
    secret = enroll.get("secret") or ""
    totp = Totp(secret)
    r = api(IDSAN_URL, p, "/api/mfa/enroll/verify", {"code": totp.fresh()})
    recovery_codes = (result_of(r) or {}).get("recoveryCodes") or []
    check("the superadmin has an enrolled authenticator and recovery codes to recover",
          bool(secret) and len(recovery_codes) >= 8, "%d codes  %s" % (len(recovery_codes), brief(r)))

    # ---- (4a) export refusals ---------------------------------------------------------------
    r = api(IDSAN_URL, p, "/api/backup/export", {"sections": ALL_SECTIONS, "passphrase": ARCHIVE_PASS})
    check("exporting the identity store is refused without a fresh credential — it hands "
          "the WHOLE store over in one file",
          r.status_code == 403 and "step_up_required" in r.text, brief(r))

    check("the operator can re-authenticate", elevate(IDSAN_URL, p, totp).status_code == 200)

    r = api(IDSAN_URL, p, "/api/backup/export", {"sections": ALL_SECTIONS, "passphrase": "short"})
    check("an export passphrase below the minimum is refused, with the rule stated",
          r.status_code == 400 and "12" in r.text, brief(r))

    r = api(IDSAN_URL, p, "/api/backup/export", {"sections": ALL_SECTIONS, "passphrase": ARCHIVE_PASS})
    blob = (result_of(r) or {}).get("dataBase64") or ""
    raw = base64.b64decode(blob) if blob else b""
    check("the export produces an archive", r.status_code == 200 and len(raw) > 500,
          "%d bytes  %s" % (len(raw), brief(r)))
    check("which is encrypted, not a zip of readable rows — the archive carries every "
          "password hash and every unsealed TOTP secret in the estate",
          b"recovered.user@bench.test" not in raw and b"myidsan" not in raw[8:],
          "magic %r" % raw[:4])

    # ---- (4b) preview refusals --------------------------------------------------------------
    r = api(IDSAN_URL, p, "/api/backup/preview", {"dataBase64": blob, "passphrase": ARCHIVE_PASS})
    manifest = result_of(r) or {}
    check("preview reads the manifest so an operator can look before they leap",
          r.status_code == 200 and manifest.get("app") == "myidsan"
          and sorted(manifest.get("sections") or []) == sorted(ALL_SECTIONS), brief(r))

    r = api(IDSAN_URL, p, "/api/backup/preview", {"dataBase64": blob, "passphrase": "wrong-passphrase"})
    check("a WRONG passphrase is refused", r.status_code != 200, brief(r))
    r = api(IDSAN_URL, p, "/api/backup/preview", {"dataBase64": blob, "passphrase": ""})
    check("an EMPTY passphrase is refused — the equivalent restore on mymatasan shipped "
          "accepting one, so this exact shape has burned us before",
          r.status_code != 200, brief(r))
    r = api(IDSAN_URL, p, "/api/backup/preview",
            {"dataBase64": base64.b64encode(b"this is not a backup file at all").decode(),
             "passphrase": ARCHIVE_PASS})
    check("a file that is not a backup is refused on its magic, before any passphrase work",
          r.status_code != 200 and "not a myidsan backup" in r.text, brief(r))
    r = api(IDSAN_URL, p, "/api/backup/preview",
            {"dataBase64": base64.b64encode(raw[:len(raw) // 2]).decode(), "passphrase": ARCHIVE_PASS})
    check("a truncated archive is refused rather than half-read", r.status_code != 200, brief(r))

    # ---- (1) the cross-key restore ----------------------------------------------------------
    d = admin(DR_URL, *LOCAL_LOGIN["myidsan"])
    check("the recovery host can be signed in to with its own stock credentials",
          bool(d.csrf()), "csrf present")
    check("and it re-authenticates", elevate(DR_URL, d).status_code == 200)

    r = api(DR_URL, d, "/api/backup/restore",
            {"dataBase64": blob, "passphrase": ARCHIVE_PASS, "mode": "replace"})
    restore = result_of(r) or {}
    check("the archive restores onto the OTHER host", r.status_code == 200, brief(r))
    check("reporting what it applied, section by section",
          (restore.get("restored") or {}).get("identity", 0) >= 2
          and (restore.get("restored") or {}).get("access", 0) >= 2
          and (restore.get("restored") or {}).get("apps", 0) >= 2,
          json.dumps(restore.get("restored")))
    check("and that it dropped every live session — a session predating the restore would "
          "carry pre-restore authority", restore.get("sessionsInvalidated") is True,
          json.dumps({k: restore.get(k) for k in ("sessionsInvalidated", "setupCompleted")}))
    r = d.s.get(DR_URL + "/api/backup/sections", timeout=30)
    check("including the caller's own, so the operator is signed out rather than left "
          "holding a cookie that no longer means anything", r.status_code != 200, brief(r))

    # THE CLAIM. A different host, a different at-rest key, and the authenticator enrolled
    # before the disaster still works.
    c, r, out = sign_in_dr(totp)
    check("on the restored host the PRE-DISASTER password is accepted",
          out.get("mfaRequired") is True, brief(r))
    check("AND THE PRE-DISASTER AUTHENTICATOR STILL WORKS — the sealed secret was unsealed "
          "into the archive and re-sealed with THIS host's key, which is the one step that "
          "separates a backup from an inert file",
          r.status_code == 200 and any(n.endswith("kopiv2_access") for n in c.s.cookies.keys()),
          brief(r))

    # The break-glass half. Recovery codes are bcrypt hashes rather than sealed secrets, so
    # they travel by a different route than the TOTP secret and are worth their own check —
    # they are also the only way back in for the person whose authenticator died in the same
    # incident that took the server.
    rescue = Client(DR_URL)
    r = rescue.s.post(DR_URL + "/api/login/default",
                      json={"username": "admin", "password": BENCH_PASS}, timeout=30)
    tok = (result_of(r) or {}).get("mfaToken")
    r = rescue.s.post(DR_URL + "/api/login/mfa",
                      json={"mfaToken": tok, "code": recovery_codes[0]}, timeout=30)
    check("a PRE-DISASTER recovery code also still works on the restored host",
          r.status_code == 200
          and any(n.endswith("kopiv2_access") for n in rescue.s.cookies.keys()), brief(r))
    st = result_of(rescue.s.get(DR_URL + "/api/mfa", timeout=30)) or {}
    check("and spending it draws the count down, so the codes came back as CODES rather "
          "than as an unusable row",
          st.get("recoveryRemaining") == len(recovery_codes) - 1,
          "%s of %d" % (st.get("recoveryRemaining"), len(recovery_codes)))

    # ---- (2) the replay guard travels ------------------------------------------------------
    c2 = Client(DR_URL)
    r = c2.s.post(DR_URL + "/api/login/default",
                  json={"username": "admin", "password": BENCH_PASS}, timeout=30)
    tok = (result_of(r) or {}).get("mfaToken")
    r = c2.s.post(DR_URL + "/api/login/mfa", json={"mfaToken": tok, "code": totp.spent()}, timeout=30)
    check("a code ALREADY SPENT before the disaster is still refused after it — the replay "
          "guard's accepted time-step rides along in the backup, so a code shoulder-surfed "
          "the week before does not become live again by way of the recovery",
          r.status_code != 200
          and not any(n.endswith("kopiv2_access") for n in c2.s.cookies.keys()), brief(r))

    # ---- (3) the restored host is a working identity server ---------------------------------
    def superadmin_probes(cli, label):
        out = {}
        for path in ("/api/backup/sections", "/api/user-credential?limit=50",
                     "/api/access-rbac/roles", "/api/app-registry?limit=50"):
            out[path] = cli.s.get(DR_URL + path, timeout=30).status_code
        check("the restored superadmin is still a superadmin %s — a restore renumbers every "
              "role, and an operator who cannot administer the server they just recovered "
              "has not recovered it" % label,
              all(v == 200 for v in out.values()), json.dumps(out))

    superadmin_probes(c, "immediately after the restore")
    users = result_list(c.s.get(DR_URL + "/api/user-credential?limit=50", timeout=30), "items")
    emails = sorted(u.get("email") for u in users if isinstance(u, dict))
    check("both accounts came back", "admin" in emails and "recovered.user@bench.test" in emails,
          json.dumps(emails))
    second = next((u for u in users if u.get("email") == "recovered.user@bench.test"), {})
    dr_roles = result_of(api(DR_URL, c, "/api/access-rbac/roles", method="GET")) or []
    dr_viewer = next((r["id"] for r in dr_roles if isinstance(r, dict) and r.get("name") == "viewer"), -1)
    check("and the second account kept its ROLE across the renumbering, rather than landing "
          "on whatever role now occupies the old id",
          second.get("userRoleId") == dr_viewer and dr_viewer > 0,
          "user role %s, viewer is %s" % (second.get("userRoleId"), dr_viewer))
    apps = sorted(a.get("code") for a in result_list(
        c.s.get(DR_URL + "/api/app-registry?limit=50", timeout=30), "items") if isinstance(a, dict))
    check("the registered relying apps came back — without them every OTHER app in the "
          "suite is still locked out after the recovery",
          "myseliasan" in apps and "benchapp2" in apps, json.dumps(apps))

    # A reboot is part of any real recovery, and the boot path re-seeds a stock superadmin
    # from config. If that overwrote the restored account the recovery would silently undo
    # itself on the first restart.
    sh("docker", "restart", DR_NAME)
    check("the recovered host comes back up after a reboot", wait_up(DR_URL + "/api/auth/session"))
    c3, r, _ = sign_in_dr(totp)
    check("and the recovery SURVIVES the reboot — password and authenticator both",
          r.status_code == 200 and any(n.endswith("kopiv2_access") for n in c3.s.cookies.keys()),
          brief(r))
    superadmin_probes(c3, "after the reboot")
    stock = Client(DR_URL)
    r = stock.s.post(DR_URL + "/api/login/default",
                     json={"username": "admin", "password": "admin123"}, timeout=30)
    check("and the config file's bootstrap password did NOT quietly come back with it",
          r.status_code != 200, brief(r))

    # ---- (7) the trail is not part of the backup --------------------------------------------
    trail = result_list(c3.s.get(DR_URL + "/api/audit?limit=200", timeout=30), "items")
    actions = {}
    for row in trail:
        actions[row.get("action")] = actions.get(row.get("action"), 0) + 1
    check("the restore is recorded on the host that was restored",
          actions.get("backup.restore", 0) > 0, json.dumps(actions))
    check("and the trail still holds THIS host's own history from before the restore — the "
          "audit table is not in the backup and is not cleared by one, so a doctored archive "
          "cannot be used to erase the record of itself",
          actions.get("login.success", 0) > 0 and len(trail) > 2, json.dumps(actions))
    check("the export is recorded on the host it was taken from",
          any(row.get("action") == "backup.export"
              for row in result_list(p.s.get(IDSAN_URL + "/api/audit?limit=200", timeout=30), "items")))

    # ---- (5) merge mode ----------------------------------------------------------------------
    #
    # "Keep both" is offered in the UI in four languages and described as adding the backup's
    # records alongside what is already here. Every install seeds the same stock role names
    # and the same bootstrap admin email, and each of those columns is UNIQUE — so against
    # any real target this used to hit the constraint on its very first row and abort,
    # handing the operator the driver's own text mid-recovery.
    check("the operator re-authenticates for the merge", elevate(DR_URL, c3, totp).status_code == 200)
    before_users = len(result_list(c3.s.get(DR_URL + "/api/user-credential?limit=50", timeout=30), "items"))
    r = api(DR_URL, c3, "/api/backup/restore",
            {"dataBase64": blob, "passphrase": ARCHIVE_PASS, "mode": "merge"})
    merged = result_of(r) or {}
    check("MERGE mode succeeds against a host that already holds these records, instead of "
          "aborting on a unique-constraint error", r.status_code == 200, brief(r))
    check("and it says what it declined to overwrite rather than silently doing nothing",
          sum((merged.get("skipped") or {}).values()) > 0, json.dumps(merged.get("skipped")))
    check("no database constraint text reaches the operator",
          "UNIQUE constraint" not in r.text and "2067" not in r.text, brief(r))

    c4, r, _ = sign_in_dr(totp)
    after_users = len(result_list(c4.s.get(DR_URL + "/api/user-credential?limit=50", timeout=30), "items"))
    check("a merge of a backup the host already holds duplicates nobody — an identity server "
          "with two rows for one person is worse than one that declined the import",
          after_users == before_users, "%d -> %d" % (before_users, after_users))
    check("and the accounts still work afterwards", r.status_code == 200, brief(r))

    # ---- (6) orphans are skipped, never re-parented ------------------------------------------
    #
    # Restoring a section whose PARENT section was not selected is the case where a careless
    # implementation attaches rows to whatever id happens to be free. On an identity server
    # that means someone else's second factor, or a role grant nobody asked for.
    check("the operator re-authenticates once more", elevate(DR_URL, c4, totp).status_code == 200)
    r = api(DR_URL, c4, "/api/backup/restore",
            {"dataBase64": blob, "passphrase": ARCHIVE_PASS, "mode": "merge", "sections": ["mfa"]})
    only_mfa = result_of(r) or {}
    check("restoring the second factors WITHOUT the accounts they belong to attaches nothing",
          r.status_code == 200 and (only_mfa.get("restored") or {}).get("mfa", 0) == 0,
          brief(r))
    check("and counts every one of them as skipped, so the operator is told rather than "
          "left to infer it from silence",
          (only_mfa.get("skipped") or {}).get("mfa", 0) > 0, json.dumps(only_mfa.get("skipped")))

    c5, r, _ = sign_in_dr(totp)
    check("the superadmin's own authenticator is untouched by that",
          r.status_code == 200 and any(n.endswith("kopiv2_access") for n in c5.s.cookies.keys()),
          brief(r))

    # The destructive twin of the same shape. "Replace what is here" with only the mfa
    # section ticked used to wipe every factor and every recovery code on the server, find
    # no account mapping to place the backup's on, skip all of them and report SUCCESS —
    # leaving every account with no second factor at all. Under a required-MFA policy that
    # is the whole estate locked out; otherwise it is a silent downgrade of everyone.
    check("the operator re-authenticates for the section-only restore",
          elevate(DR_URL, c5, totp).status_code == 200)
    r = api(DR_URL, c5, "/api/backup/restore",
            {"dataBase64": blob, "passphrase": ARCHIVE_PASS, "mode": "replace", "sections": ["mfa"]})
    mfa_only = result_of(r) or {}
    check("restoring ONLY the second factors, in replace mode, puts them back on the "
          "accounts already here instead of wiping every factor on the server",
          r.status_code == 200 and (mfa_only.get("restored") or {}).get("mfa", 0) > 0,
          brief(r))

    c6, r, _ = sign_in_dr(totp)
    check("and the authenticator still works afterwards — which is the whole difference "
          "between restoring a section and destroying it",
          r.status_code == 200 and any(n.endswith("kopiv2_access") for n in c6.s.cookies.keys()),
          brief(r))
    st = result_of(c6.s.get(DR_URL + "/api/mfa", timeout=30)) or {}
    # The full set is back, INCLUDING the code spent earlier on this host — which is right,
    # and worth stating rather than glossing: this archive was taken before that code was
    # spent, so restoring it restores the world as it was then. A restore is a rewind, and
    # an operator rewinding the second factors gets the recovery codes of that moment too.
    check("with the whole recovery set back as it stood when the backup was taken",
          st.get("enrolled") is True and st.get("recoveryRemaining") == len(recovery_codes),
          json.dumps(st))

    return report()


if __name__ == "__main__":
    try:
        sys.exit(main())
    finally:
        # The DR host is this bench's own, so it cleans up after itself — the shared harness
        # knows nothing about it and would leave it running forever.
        sh("docker", "rm", "-f", DR_NAME, check=False)
        shutil.rmtree(os.path.join(ROOT, DR_NAME), ignore_errors=True)
