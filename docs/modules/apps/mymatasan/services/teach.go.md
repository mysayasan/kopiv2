# Module: apps/mymatasan/services/teach.go

## Purpose

The **Teach wizard** service — the registry and orchestration behind the
zero-ML-knowledge camera-teaching feature. It owns `TeachSkill` rows and
coordinates the training, detection, and rule services so a non-expert can teach
a camera a new skill end to end. The `teachService` is split across several files
by concern:

| File | Responsibility |
|------|----------------|
| `teach.go` | Skill CRUD, class-slug derivation, stock-label collision guard, forget/cleanup. |
| `teach_capture.go` | Session capture engine: presence-gated, deduped auto-capture into a hidden dataset; per-class counts + sample-coach hints; one session at a time. |
| `teach_eval.go` | "Accuracy check": quick-train via the training runner, evaluate on the held-back split, persist a plain-language report + F1-tuned suggested threshold. |
| `teach_activate.go` | Test drive (a second detector worker on candidate weights via an env override) and activate/deactivate (hot-swap the model, auto-create the detection rule). |
| `teach_package.go` | Passphrase-encrypted `.mmskill` export / preview / import (cross-node sharing). |
| `teach_feedback.go` | "Keep teaching" loop — file ✓/✗ verdicts on live alerts back into the dataset. |
| `teach_anomaly.go` | "Spot anything unusual": fit a normal-only embedding memory bank and manage the per-camera anomaly manifest the worker reads. |

## Skill kinds and classes

A skill's `SkillType` selects both its capture behaviour and its class list
(`teachSkillClasses`):

- **object** → a single class (the skill's name slug); recognize the object anywhere.
- **inspect** → a contrast pair `[<slug>, "not <slug>"]`; tell good from bad in a spot.
- **anomaly** → normal-only `["not <slug>"]`; the deviating side has no samples — the
  fitted memory bank flags anything unlike the learned normal. Anomaly models live
  outside the single custom-YOLO slot (a manifest, one entry per camera), so several
  run at once alongside object/custom detection.

## Design notes

- **No new detection "mode".** All three kinds activate into an ordinary
  **Presence** rule (`detectionType = <slug>`); the intelligence that distinguishes
  them is entirely in the model (which label it emits), not the rule.
- **Single active custom-model slot.** Object/inspect skills activate through the
  training service's `ActivateModel` hot-swap (one custom YOLO model at a time);
  anomaly skills are exempt (own manifest slot). Merged multi-skill training is the
  known follow-up for running several taught object skills together.
- **Collision guard.** A skill name whose slug matches a stock detection label is
  rejected — the worker's stock+custom merge would otherwise dedupe it away.
- Dataset images and captured frames reuse the training service's at-rest
  encryption; alert snapshots (for feedback) are decrypted with the shared
  `atrest` cipher.
