# W2-5 bench: staged version rollout, against a real two-node fleet.
#
# WHAT THIS PROVES AND WHAT IT DOES NOT. The download-and-swap half of a node self-update
# needs a real GitHub release, which a bench cannot conjure. What it CAN exercise is
# everything the rollout itself is: version capture off the control-channel hello, ring
# discipline, the health gate, the settle window, the halt, and the audit trail. So:
#
#   * the HALT path is fully real — the rollout asks for a version that was never
#     published, the node's own updater refuses it, and the node's words come back in the
#     halt reason. Nothing is simulated.
#   * the SUCCESS path swaps the node's binary for one built at the target version and
#     restarts the container. That is precisely what a self-update does at the end, and it
#     is the only part the control plane can observe — the gate judges what a node REPORTS.
#
# The bench compresses only caller-supplied values (settle window, node timeout), which an
# operator can set too. No shipped default is weakened.
import io, json, os, re, subprocess, sys, time
import urllib3, requests

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from fleet_harness import CP, CP_PORT, NODE_PORTS, ROOT, REPO, result_of, sh, PASSWORDS

urllib3.disable_warnings()

CHECKS = []
# The driver ticks every 30s (a shipped constant), so every wait has to allow for it.
TICK = 30


def check(name, ok, detail=""):
    CHECKS.append((name, bool(ok), detail))
    print(("PASS  " if ok else "FAIL  ") + name + ("   " + detail if detail else ""))


def report():
    passed = sum(1 for _, ok, _ in CHECKS if ok)
    print("")
    print("%d/%d checks passed" % (passed, len(CHECKS)))
    for name, ok, detail in CHECKS:
        if not ok:
            print("  FAILED: " + name + ("   " + detail if detail else ""))
    return 0 if passed == len(CHECKS) else 1


def login():
    cp = CP("https://127.0.0.1:%d" % CP_PORT)
    for pw in PASSWORDS:
        if cp.login(pw=pw).status_code == 200:
            return cp
    raise SystemExit("cannot log in to the control plane")


def nodes(cp):
    return cp.get("/api/nodes").json().get("result") or []


def paged_result(r):
    """SendPagingResult is double-wrapped: {message, data:{result, paging}}."""
    body = r.json()
    if isinstance(body, dict):
        if isinstance(body.get("data"), dict) and "result" in body["data"]:
            return body["data"]["result"] or []
        if "result" in body:
            return body["result"] or []
    return []


def settle(cp, timeout=240):
    """Both nodes online across three consecutive sweeps — the same gate every liveness
    bench in this programme uses, because adoption sets 'online' by itself."""
    streak = 0
    deadline = time.time() + timeout
    while time.time() < deadline:
        st = {n["name"]: n["status"] for n in nodes(cp)}
        if st and all(v == "online" for v in st.values()):
            streak += 1
            if streak >= 3:
                return True
        else:
            streak = 0
        time.sleep(10)
    return False


def node_reported_version(cp, node_id):
    """Ask the NODE itself what it is running, over the tunnel.

    This is the independent source of truth the control plane's own record is checked
    against — and it makes the bench hermetic. Reading the repo manifest instead would make
    the run depend on which binary a previous run happened to leave in the shared bin dir,
    which is exactly how this bench first produced eight confident, meaningless failures.
    """
    r = cp.get("/api/nodes/%s/proxy/api/system/update" % node_id)
    if r.status_code != 200:
        return ""
    return (result_of(r) or {}).get("current", "")


def bump(version, by=1):
    major, minor, patch = (int(x) for x in version.split("."))
    return "%d.%d.%d" % (major, minor + by, 0)


def build_node_binary(version, dest):
    """Cross-compile a mymatasan whose EMBEDDED version is `version`.

    The node reports the version baked into its binary (infra/versioning/version.json is
    go:embed-ed), which is exactly why this is a faithful stand-in for a completed
    self-update: after one, the node is running a different binary that says a different
    number, and that is the whole of what the control plane can see.

    The manifest is restored immediately; a bench that left the repo's version bumped would
    be a bench that cuts a release.
    """
    manifest_path = os.path.join(REPO, "infra", "versioning", "version.json")
    # Read and restore in BINARY mode. Restoring through a text write re-encodes the line
    # endings, which on Windows leaves version.json permanently "modified" in git with an
    # empty diff — a bench that quietly dirties the repository it is benching.
    original = io.open(manifest_path, "rb").read()
    try:
        manifest = json.loads(original.decode("utf-8-sig"))
        manifest["apps"]["mymatasan"]["version"] = version
        io.open(manifest_path, "w", encoding="utf-8", newline="\n").write(json.dumps(manifest, indent=2) + "\n")
        env = dict(os.environ, GOOS="linux", GOARCH="amd64", CGO_ENABLED="0")
        p = subprocess.run(["go", "build", "-o", dest, "./cmd/mymatasan"], cwd=REPO, env=env,
                           capture_output=True, text=True)
        if p.returncode != 0:
            raise SystemExit("building mymatasan %s failed:\n%s" % (version, p.stderr))
    finally:
        io.open(manifest_path, "wb").write(original)


def rollout_get(cp, rid):
    return result_of(cp.get("/api/fleet-rollouts/%d" % rid))


def wait_for(cp, rid, predicate, timeout=300, label=""):
    """Poll one rollout until predicate(view) holds. Returns the last view either way."""
    deadline = time.time() + timeout
    view = rollout_get(cp, rid)
    while time.time() < deadline:
        if predicate(view):
            return view
        time.sleep(5)
        view = rollout_get(cp, rid)
    return view


def node_states(view):
    return {n["nodeName"]: n["state"] for n in view.get("nodes", [])}


def main():
    cp = login()
    if not settle(cp):
        check("the fleet is genuinely being watched before anything is upgraded", False,
              str({n["name"]: n["status"] for n in nodes(cp)}))
        return report()
    check("the fleet is genuinely being watched before anything is upgraded", True)

    # --- 1. the control plane learned each node's version, without being told ----------
    #
    # This costs nothing on the wire: the version already rode on every control-channel
    # hello and used to be logged and discarded. It is checked against what each node says
    # about ITSELF over the tunnel, which is an independent answer.
    fleet = nodes(cp)
    versions = {n["name"]: n.get("version", "") for n in fleet}
    own = {n["name"]: node_reported_version(cp, n["nodeId"]) for n in fleet}
    check("every node's running version is recorded from its control-channel hello",
          len(versions) == 2 and all(v and v == own[name] for name, v in versions.items()),
          "recorded %s vs each node's own answer %s" % (versions, own))
    running = sorted(own.values())[0]
    check("the version carries a freshness stamp, so a stale reading is visible as one",
          all(n.get("versionSeenAt", 0) > 0 for n in fleet),
          str({n["name"]: n.get("versionSeenAt") for n in fleet}))

    # --- 2. planning ------------------------------------------------------------------
    ghost = bump(running, 40)  # a version that was certainly never published
    r = cp.post("/api/fleet-rollouts", {"targetVersion": ghost, "ringSize": 1,
                                        "settleSeconds": 20, "nodeTimeoutSeconds": 90,
                                        "note": "bench: halt path"})
    plan = result_of(r)
    rid = plan.get("id")
    check("a rollout plans one ring per node with the canary first",
          r.status_code == 200 and plan.get("ringCount") == 2 and plan.get("state") == "draft"
          and [n["ring"] for n in plan["nodes"]] == [1, 2],
          "status=%s rings=%s state=%s" % (r.status_code, plan.get("ringCount"), plan.get("state")))
    first_canary = plan["nodes"][0]["nodeId"]

    # A draft is a plan an operator can look at, not a decision. Nothing may move.
    time.sleep(TICK + 10)
    view = rollout_get(cp, rid)
    check("a DRAFT rollout is not driven — planning is not starting",
          view["state"] == "draft" and set(node_states(view).values()) == {"pending"},
          "state=%s nodes=%s" % (view["state"], node_states(view)))

    # Re-planning must produce the same canary, or no two rollouts are comparable.
    r2 = cp.post("/api/fleet-rollouts", {"targetVersion": ghost, "ringSize": 1})
    check("a second rollout is refused while one is open", r2.status_code != 200,
          "status=%s" % r2.status_code)

    # --- 3. the halt path, entirely real ----------------------------------------------
    r = cp.post("/api/fleet-rollouts/%d/start" % rid)
    check("starting the rollout is accepted", r.status_code == 200, r.text[:160])

    view = wait_for(cp, rid, lambda v: v["state"] in ("halted", "completed", "cancelled"), timeout=300)
    states = node_states(view)
    check("a rollout to a version that was never published HALTS", view["state"] == "halted",
          "state=%s reason=%s" % (view["state"], view.get("haltReason", "")[:120]))
    check("the halt names what the node came back running, not just that it failed",
          re.search(r"came back running \S+, not " + re.escape(ghost), json.dumps(view)) is not None
          or "no release published" in json.dumps(view),
          view.get("haltReason", "")[:200])
    check("the halt reason is readable, not the node's raw JSON envelope",
          "statsCode" not in view.get("haltReason", ""),
          view.get("haltReason", "")[:200])
    ring2 = [n for n in view["nodes"] if n["ring"] == 2]
    check("the second ring was never touched", all(n["state"] == "pending" for n in ring2),
          str(states))
    check("the fleet is still on the version it started on",
          all(n.get("version") == running for n in nodes(cp)),
          str({n["name"]: n.get("version") for n in nodes(cp)}))

    # --- 4. the success path ----------------------------------------------------------
    #
    # A rollout to a version node-a will actually end up on. The node's own updater will
    # fail to download it (there is no such release), so the bench performs the half a real
    # self-update performs at the end: swap the binary, restart. The control plane judges
    # what the node reports, which is exactly what it would see either way.
    target = bump(running, 1)
    upgraded = os.path.join(ROOT, "bin", "mymatasan-%s" % target)
    print("-- building a mymatasan that reports %s --" % target)
    build_node_binary(target, upgraded)

    r = cp.post("/api/fleet-rollouts", {"targetVersion": target, "ringSize": 1,
                                        "settleSeconds": 20, "nodeTimeoutSeconds": 600,
                                        "note": "bench: success path"})
    plan = result_of(r)
    rid2 = plan.get("id")
    check("a new rollout can be planned once the previous one is closed", r.status_code == 200,
          "status=%s" % r.status_code)
    check("the canary is the same node as last time — ring composition is deterministic",
          plan["nodes"][0]["nodeId"] == first_canary,
          "%s vs %s" % (plan["nodes"][0]["nodeId"][:8], first_canary[:8]))

    canary_name = plan["nodes"][0]["nodeName"]
    cp.post("/api/fleet-rollouts/%d/start" % rid2)
    view = wait_for(cp, rid2, lambda v: node_states(v).get(canary_name) in ("updating", "failed"), timeout=120)
    check("the canary is asked to update and the other ring waits",
          node_states(view).get(canary_name) == "updating"
          and all(n["state"] == "pending" for n in view["nodes"] if n["ring"] == 2),
          str(node_states(view)))

    # Perform the swap the real updater would have performed, on the CANARY ONLY.
    #
    # The harness gives every node its own binary (bin/mymatasan-<name>), so this touches
    # nothing the other node is executing. That matters for more than tidiness: replacing a
    # shared binary in place truncates the inode a running process is executing from, and on
    # Windows the rename is refused outright because the other container holds a lock. Both
    # failure modes cost this bench a run.
    import shutil
    print("-- swapping %s onto the upgraded binary --" % canary_name)
    sh("docker", "stop", canary_name)
    node_bin = os.path.join(ROOT, "bin", "mymatasan-%s" % canary_name)
    shutil.copyfile(upgraded, node_bin)
    os.chmod(node_bin, 0o755)
    sh("docker", "start", canary_name)

    view = wait_for(cp, rid2, lambda v: node_states(v).get(canary_name) in ("succeeded", "failed"), timeout=420)
    # Nothing to put back: the other node has its own binary and was never touched, which is
    # what makes "upgrade exactly one appliance" testable in the first place.
    check("the canary passes only once it REPORTS the target version",
          node_states(view).get(canary_name) == "succeeded",
          "state=%s reported=%s" % (node_states(view).get(canary_name),
                                    {n["name"]: n.get("version") for n in nodes(cp)}))

    # The settle window must actually hold the next ring back.
    view = rollout_get(cp, rid2)
    check("the next ring does not start the instant the canary reports in",
          view["currentRing"] == 1 and all(n["state"] == "pending" for n in view["nodes"] if n["ring"] == 2),
          "ring=%s nodes=%s" % (view["currentRing"], node_states(view)))

    view = wait_for(cp, rid2, lambda v: v["currentRing"] == 2 or v["state"] != "running", timeout=240)
    check("the second ring starts once the canary has settled",
          view["currentRing"] >= 2,
          "ring=%s state=%s" % (view["currentRing"], view["state"]))

    # node-b is left on the old binary, so it can never report the target: the rollout must
    # halt rather than quietly conclude. That is the same gate as before, one ring later.
    view = wait_for(cp, rid2, lambda v: v["state"] in ("halted", "completed"), timeout=900)
    check("a ring whose node never reaches the target halts the rollout, one ring in",
          view["state"] == "halted" and view["currentRing"] == 2,
          "state=%s ring=%s reason=%s" % (view["state"], view["currentRing"], view.get("haltReason", "")[:100]))
    check("the node that DID upgrade keeps its success recorded",
          node_states(view).get(canary_name) == "succeeded", str(node_states(view)))

    # --- 5. the trail ------------------------------------------------------------------
    rows = paged_result(cp.get("/api/audit?limit=100"))
    actions = [x.get("action") for x in rows if str(x.get("action", "")).startswith("fleet.rollout")]
    check("planning, starting and halting a rollout are all audited",
          "fleet.rollout.plan" in actions and "fleet.rollout.start" in actions
          and "fleet.rollout.halted" in actions,
          str(sorted(set(actions))))
    halted = [x for x in rows if x.get("action") == "fleet.rollout.halted"]
    check("a halt is audited as an error, not a success",
          bool(halted) and all(x.get("outcome") == "error" for x in halted),
          str([x.get("outcome") for x in halted]))

    return report()


def v_state_ok(view):
    """A rollout that already finished a ring may have moved past 2 by the time we look."""
    return view.get("state") in ("halted", "completed") and view.get("currentRing", 0) >= 2


if __name__ == "__main__":
    sys.exit(main())
