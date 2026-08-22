# Module: apps/myseliasan/services/backup.go

## Purpose

The control plane's disaster-recovery bundle: a portable, passphrase-encrypted `.selbackup` file holding everything myseliasan cannot rebuild from anywhere else in the fleet, and the restore that applies it onto a fresh install.

It exists for one failure in particular. The fleet certificate authority's PRIVATE KEY lives in a `control_setting` row (`pairing.caKey`, sealed by `services/secret_store.go.md`), every adopted node's certificate chains to it, and every node's heartbeat authenticates against a per-node token in `managed_node`. Losing this database does not degrade the fleet, it orphans it: each node must be physically re-adopted with a fresh claim code. `apps/myseliasan/app/app.go` already refuses to boot when the at-rest key protecting that row is missing, rather than quietly minting a new CA and resetting fleet trust underneath a running fleet — this module is the other half of that decision, so the loud failure has an answer.

The envelope and section machinery deliberately mirror myidsan's `.idbackup` (`apps/myidsan/services/backup.go.md`), down to the magic-plus-version header and the id-remapping restore. A second backup format would be a second thing to get subtly wrong about sealed columns.

## File format

```
magic "SELB" (4 bytes) || formatVersion (1 byte) || atrest.EncryptWithPassphrase(json(backupFile))
```

`backupApp = "myseliasan"`, `backupSchemaVersion = 1`, `backupFormatVersion = 1`. The magic is checked before any passphrase work, so a `.idbackup` — which shares the envelope and would otherwise decrypt — is rejected with an answer the operator can act on. `BackupManifest` (app, appVersion, schemaVersion, createdAt, sections, per-section counts) is the plaintext-inside-the-envelope header that `Preview` surfaces before anything is applied.

## Responsibilities

- `IBackupService` — `AvailableSections` (section ids + current row counts, for the export UI), `Export`, `Preview` (manifest only), `Restore`.
- `NewBackupService(db, cipher, planDir, setup, appVersion)` — builds its own repos from `dbsql.IDbCrud`, matching the constructor convention of the other myseliasan services. `cipher` and `planDir` MUST be the same ones `NewSiteService` and the fleet CA use, or exported secrets and plan images will not decrypt.
- Sections, in canonical order (which is also collection and apply order, because later sections depend on ids remapped by earlier ones):
  - `access` — `AccessRole` + `AccessRolePermission`.
  - `users` — `ControlUser` (password hashes included). Depends on `access`: `RoleId` is remapped.
  - `fleetca` — the `pairing.*` `ControlSetting` rows: CA cert and private key, the control plane's own leaf and key, the revocation list, and the fleet PSK. **The section the file exists for.**
  - `fleet` — `ManagedNode` + `NodeAccessGrant` + the uptime record (`NodeStateEvent` + `FleetMonitorGap`, W2-2). The history rides here because it is meaningless without the nodes it describes, and because a control plane rebuilt after a disk failure that reported a flawless, EMPTY SLA history for the month it just lost would be worse than one reporting nothing. Same lesson as the correlation rules' clauses: a section that brings back its headline rows and drops what hangs off them restores into something that looks complete and answers nothing. History keys on `NodeId`, so unlike grants there is nothing to remap. Grants depend on `access` for their `RoleId`; node ids are the node's own stable string id and are never remapped, which is why a restored node still authenticates.
  - `sites` — `Site` + `FloorPlan` (including the plan images, which live encrypted on disk rather than in the database) + `NodePlacement`. Site → floor → placement ids are remapped in that order.
  - `rules` — `FleetRule` **and its `FleetRuleClause` rows**. Children are wiped before parents in replace mode and clauses are remapped onto the ids the rule inserts actually issue, the same create-then-remap dance sites/floors/placements already do (see "Rule clauses" below — this half of the section was missing until the fleet-policy work landed beside it).
  - `policies` — `FleetPolicy` + `FleetPolicyItem` (fleet configuration policy, `entities/fleet_policy.go.md`, W2-1). Without this section a restored control plane reports every node "unmanaged" — the estate's configuration standard is exactly the kind of thing nobody has written down anywhere else. Items are remapped onto the ids the policy inserts issue, same pattern as rule clauses. `LastEvaluatedAt` comes back as whatever the bundle held but is not meaningful yet — the reconciler overwrites it on its first pass after boot (within a minute), so until then the UI shows the policy's own list with no compliance verdict beside it, which is honest.
  - `settings` — every remaining `ControlSetting` row (deployment mode, agent schedule, the `settings.defaults` first-run snapshot). Deliberately EXCLUDES the `pairing.*` keys, which belong to `fleetca`.
  - `audit` — `AuditLog`. Append-only; see Notes.
- `RestoreResult` reports per-section restored/skipped counts, a schema-version warning when the file was made by a different schema, and `RestartRequired`/`RestartReason` — set whenever `fleetca` was applied.

## Sealed columns and portable at-rest keys

A backup that only restores onto the machine that made it is worthless: the disaster it has to survive is the one where that host is gone and the fresh install generates a NEW at-rest key.

- **Export** unseals through `decodeSecret(cipher, …)`, so the secret travels as plaintext INSIDE the passphrase-encrypted envelope. `backupSetting.Sealed` records which rows were on that side of the line.
- **Restore** re-seals through `encodeSecret(cipher, …)` under the destination host's key.

`sealedSettingKeys` is an explicit list (`pairing.caKey`, `pairing.parentKey`, `pairing.fleetKey`, `settings.defaults`) rather than "seal everything": rows outside it are read with a plain repo call that never calls `decodeSecret`, so sealing one would hand its reader base64 ciphertext and break the feature it configures.

Floor plan images get the same treatment for the same reason — they are AES-GCM encrypted on disk with the same cipher, so they are decrypted to base64 on export and re-encrypted on restore. Paths name the row id, which is not known until the insert, so restore creates the floor row, writes `floor-<newid>.img`, then updates the row — the same create-then-name dance `services/sites.go` does on upload.

## Rule clauses (bug fix, found alongside the fleet-policy work)

`BackupSectionRules` exported `FleetRule` rows but never `FleetRuleClause` — the clause was
never exported/restored at all. This was invisible in exactly the way a backup defect is
worst: the export succeeded, the restore reported the right number of rules, and the UI
listed them all enabled, each with nothing left to match on. A rule with no required clause
fires on nothing (which is why the correlation-rule editor refuses to create one in the first
place — `validateFleetRule` in `correlate_crud.go`); restore was creating them anyway. A
control plane brought back after a disk failure would have shown a complete set of
correlation rules and detected no correlation at all. Fixed: `restoreRules` now wipes
clauses before rules in replace mode (children first, so a wiped rule never leaves clause
rows pointing at an id the next restore could hand to an unrelated rule), creates rules
first to learn their new ids, then creates clauses remapped onto those ids — the same
create-then-remap pattern every other cross-referencing section in this file already uses. A
clause whose rule is not in the bundle is skipped (counted, not silently dropped and not
attached to nothing) rather than orphaned. Covered by `backup_test.go`.

## Notes

- **Restoring `fleetca` requires a restart, and says so.** The running `fleetCA` caches `ca *fleetca.CA` in memory and only reloads on construction, so a hot swap would leave the process serving mTLS from the OLD CA while the database holds the new one — rejecting every node until the next restart. `RestoreResult.RestartRequired` drives the banner in the Settings → Backup tab.
- **`ControlUser.PasswordHash` is `json:"-"`**, so `backupUser` carries it explicitly. Without that wrapper the bundle's JSON encoding drops it and every restored account comes back with an empty password — the restore reports success, the fleet is intact, and nobody can sign in. Found by benching a real restore, not by the unit tests, because an in-memory fake round-trips the struct without ever marshalling it.
- **`ManagedNode.Token` is `json:"-"`** so it never reaches a browser, which means a naive round-trip silently drops it. `backupNode` re-exposes it. Without the token a restored node is adopted-but-unauthenticated: it fails its next heartbeat and falls out of the fleet while the registry still lists it as present.
- **The `audit` section ignores restore mode and never wipes.** The audit service has no update or delete path precisely so the trail cannot be edited after the fact; honouring `replace` here would hand an operator a supported way to erase it by restoring an empty backup over a populated one. Entries are appended; duplicates on a repeated restore are the lesser evil.
- **Replace mode on the two `control_setting` sections is scoped by key.** `restoreSettingRows` takes the key set it is allowed to delete, so restoring `settings` cannot take the CA private key with it as a side effect of applying a display preference.
- A user whose role did not travel with the backup lands with `RoleId = 0` (pending clearance) rather than keeping a stale id that names a different role on the destination host. Same rule as an orphaned permission or node grant: no grant beats a grant pointing at whatever now holds that id.
- Deliberately excluded, with the reason stated in the source: `relayed_notif` (a short-lived dedup ledger, not a record), `agent_digest` (regenerable), basemap PMTiles and `llm/models/` (large and re-downloadable).
- Covered by `backup_test.go`, whose centrepiece is `TestBackupRestoreAcrossDifferentAtRestKeys` — export under one cipher, restore under another, and assert the CA key both decrypts with the destination key AND no longer decrypts with the source one. The suite also covers the node token round-trip, the fleetca/settings replace-scope split, append-only audit, unmapped-role drop, foreign/corrupt/wrong-passphrase rejection, that the exported blob contains no plaintext secret, the encryption-disabled install, and merge-mode upsert on the unique `key` column, and — new alongside the fleet-policy work — the rule-clause export/remap/orphan-skip fix and the `policies` section's create-then-remap restore.
- **Live-benched 2026-08-19 and passing** — but that bench predates the `policies` section and the rule-clause fix, which are **built, not yet re-benched**. A containerised control plane with two nodes holding real CA-signed certificates, destroyed by deleting the database *and* the at-rest key, restored onto a fresh install with a newly generated key: the CA came back byte-identical, its private key re-sealed under the new key, both node tokens intact, and a node certificate issued before the backup completed an mTLS handshake against the restored control channel while a rogue certificate was rejected. See `docs/FLAGSHIP_BENCH_CHECKLIST.md` for the run and its findings.
