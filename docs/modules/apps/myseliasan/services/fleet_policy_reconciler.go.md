# Module: apps/myseliasan/services/fleet_policy_reconciler.go

## Purpose

Compares every node's LIVE settings against the policies that apply to it
(`services/fleet_policy.go.md`'s `ResolveEffectivePolicy`), and — only where a winning
policy has `Enforce` on — writes the difference back. Reads and writes go through the SAME
control channel the operator's own node screens use (`ControlSender`, the same interface
`apis/node_proxy.go` tunnels through), so the reconciler is subject to the same connectivity,
the same node-side authorization, and the same audit choke point as a human operator — it
gets no private path to an appliance.

## Compliance states

`ComplianceCompliant` / `ComplianceDrifted` / `ComplianceUnknown` / `ComplianceUnmanaged`.
**`unknown` is explicitly not `compliant`** — the single most important decision in this
file. A configuration tool that reports an unreachable machine as compliant is worse than no
tool: the estate shows all-green while the node that has been off the network for three
weeks — the one most likely to have been reimaged, downgraded, or restored from an ancient
backup — is the one contributing the reassurance.

Per-field outcomes: `FieldMatch` / `FieldDrift` / `FieldMissing` (the node's response had no
such field at all — an older or different build; reported separately from drift because the
fix is different: upgrade the node, not enforce a value its decoder would reject).

Per-section enforcement outcomes: `EnforceSkipped` (nothing drifted, or nothing enforcing) /
`EnforceApplied` / `EnforceFailed` / `EnforceUnverified` (the node accepted the write but the
read-back did not confirm it stuck).

## `FleetPolicyReconciler`

`NewFleetPolicyReconciler(policies, nodes PolicyNodeLister, sender ControlSender, audit, logf)`.
`PolicyNodeLister` is deliberately narrowed to just `List(ctx) ([]*ManagedNode, error)` —
not the full `INodeRegistry` (adoption/enrollment/revocation) — so the reconciler cannot
release a node by refactoring accident, and so it is testable without stubbing methods that
have nothing to do with configuration. `audit` may be `nil` in tests.

- `ReconcileAll(ctx) (FleetCompliance, error)` — sweeps every node, stores the result behind
  a mutex, and stamps `MarkEvaluated` on the policies that actually contributed to a node
  that was reached (never on policies whose only targets were offline).
- `ReconcileNode(ctx, nodeID) (NodeCompliance, error)` — the "check now" button; sweeps one
  node on demand without touching the stored fleet-wide result.
- `Last() FleetCompliance` — returns the most recent completed pass. **`GET
  /api/fleet-policies/compliance` serves this, not a live sweep** — a sweep is one tunneled
  round trip per section per node, and doing it inside a page load would make the screen as
  slow as the slowest appliance in the estate.

### `reconcileNode` / `reconcileSection`

For each catalog section the node's effective policy governs: `readSection` (GET over the
control channel, envelope-unwrapped via `decodeSectionBody`, descending `ReadAt` when the
section's read/write surfaces differ), then per field: missing / match / drift, comparing
with `policyValuesEqual` (`services/policy_catalog.go.md`).

If any field drifted AND its winning policy enforces, `enforceSection` runs: the section is
re-read, the drifted fields are overlaid onto the CURRENT full object
(`cloneJSONObject` + `policySetPath`), and the WHOLE merged object is PUT back — never just
the governed fields. This merge is not a nicety: every settings endpoint on the node decodes
the whole section struct with `DisallowUnknownFields`, so a PUT carrying only the governed
fields would zero every other field in the section — enforcing "alert after 2 bad hours"
would also zero the sweep interval and the minimum coverage percent, silently disabling the
very monitor the policy was tightening.

After a successful PUT, the section is **read back and re-verified** — a `200` is not proof
the value stuck, since every node settings service normalizes what it is given (clamping,
substituting a default for a zero), and trusting the `200` would let the control plane report
"applied" forever while the node holds a different number, exactly the failure a compliance
tool exists to make impossible. Verified fields flip to `FieldMatch`; anything still
disagreeing marks the section `EnforceUnverified`.

A section not read at all (node offline, wrong role, 404 for "this node doesn't have that
section") makes the WHOLE node `ComplianceUnknown`, never `compliant` and never `drifted` (a
drift claim asserts a value was seen and disagreed — nothing was seen here).

## `enforceSection` — role and audit

The write asserts `Role: "admin"`, `Actor: "fleet-policy"` on the tunneled request — the node
resolves that role against its OWN roles and evaluates its OWN matrix exactly as the
operator-driven proxy does; a node that does not grant admin to this name refuses, and the
section reports an error rather than the control plane acquiring a private capability.

`recordEnforcement` audits every enforced write via `ActionPolicyEnforce`
(`services/audit.go.md`) — the entry an investigator needs and the one nobody would think to
write on their own: the operator-driven tunnel is already audited at `apis/node_proxy.go`,
but an enforced policy change has no operator behind it (it happens on a timer, possibly
weeks after the policy was written), so without this the node's own trail would say only
"admin changed the setting" with no admin anywhere near the building.

## `FleetCompliance` / `NodeCompliance` / `SectionReport` / `FieldReport`

The nested report shape served to the UI, each level carrying which policy/scope won a field
(`PolicyId`/`PolicyName`/`Scope`) so an operator looking at an unexpected desired value can
go straight to the policy that set it, rather than guessing among several that could have.
`NodeCompliance.DriftCount` lets the fleet list rank by severity.

## Notes

- `reconcileTimeout = 20s` bounds one node's whole reconcile — a connected-but-wedged node
  must not hold the sweep open, since the sweep visits every node in turn.
- Wired in `apps/myseliasan/app/app.go`: a leader-gated 15-minute sweep
  (`leaderTicker(bgCtx, deps.Leader, 15*time.Minute, ...)`, the same deployment-mode
  guard every other myseliasan scheduled singleton uses — see `app/app.go.md`'s "Deployment
  mode" section) plus one pass 90 seconds after boot so the compliance screen has an answer
  before the first tick. The initial pass is deliberately delayed, not immediate: nodes dial
  the control channel only after `RegisterAppRoutes` returns, and a sweep run immediately
  would report the entire fleet unreachable and store that as the last known state.
- `apis/fleet_policy_api.go.md` is the HTTP surface over this and `services/fleet_policy.go.md`.
- Covered by `fleet_policy_reconciler_test.go` against a fake `ControlSender`/node lister.
- **Built, not yet live-benched** — see `docs/FLAGSHIP_HARDENING_PLAN.md` W2-1.

## `LastFor` — a report is only valid for the policies it was computed against

`Last()` returns the stored pass verbatim. **`LastFor(ctx)` returns it checked against the
policies in force now**, and it is what the API serves.

**The defect it closes.** Delete the last policy and the fleet went on reporting every node
`compliant` — on the screen *and* from `/api/fleet-policies/compliance` — because the stored
snapshot knew nothing about the policies being gone. It survived until somebody happened to
press Check now. A green estate governed by nothing is precisely the misreading this feature's
own hint sentence warns about, and it shipped. Found by the first screen pass W2-1 ever had,
by **looking at the screenshot** of a run in which every assertion had passed.

Two situations, answered differently because they are known with different certainty:

| Situation | Answer | Why |
|---|---|---|
| No policy is **in force** (none exist, or every one is parked) | Verdicts **replaced** with `unmanaged` | Provable here without asking a single appliance. Not doubt — knowledge. |
| The policy set **changed** | Verdicts kept, `Stale: true` + `StaleSince` | They may still be right, and re-deriving them is a tunneled round trip per section per node. So they are flagged, not discarded. |
| The policy list **cannot be read** | `Stale: true` | A report we cannot validate is not one to dress up as current. |

Re-sweeping on read was rejected: a sweep is deliberately something an operator asks for, with
a spinner, rather than something a page load does behind their back.

`policyFingerprint` is `id:updatedAt:enabled` per policy, sorted. All three parts earn their
place — a count-and-max-timestamp shortcut aliases a delete paired with a create in the same
second, dropping the id misses one policy swapped for another, and dropping `enabled` misses
somebody parking one of two policies. Each is covered by its own test in
`fleet_policy_staleness_test.go`, and all are mutation-checked.

`CheckedAt` survives the rewrite: the sweep did happen, and when is still a fact worth showing.
