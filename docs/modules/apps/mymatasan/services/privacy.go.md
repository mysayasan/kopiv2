# Module: apps/mymatasan/services/privacy.go

## Purpose

`privacyService` / `IPrivacyService`: regions of a camera's view that must not be seen
(W3-6) — the window next door, the pavement outside the gate, the keypad somebody types a
PIN into.

## One region, two mechanisms, and the whole design is not confusing them

| | What it guarantees | Depends on |
|---|---|---|
| **The camera masks it** | The pixels never leave the sensor. Not on disk, not in an export, nothing on a stolen drive. | The camera's ONVIF Media2 support |
| **The export redacts it** | The pixels are recorded, and do not leave the building. | Nothing — always available |

**A mask that is not burned in by the camera is a courtesy, not a privacy control**, and
which one an operator has is a fact about their hardware rather than about this software. So
it is reported per camera, in those terms, rather than implying the stronger claim.

The four states are `confirmed`, `unconfirmed`, `unsupported` and `unreachable`. The last two
are deliberately distinct: one is a fact about the camera, the other about the network, and
telling somebody "could not be reached" about a camera with no ONVIF sends them to check a
network for nothing. (A plain RTSP camera reported exactly that until the live bench ran.)

## What was deliberately not built

**Masking inside the recording pipeline.** It would give the strong guarantee on every
camera, and it would cost the product its architecture: recording is `-c copy` today
(`infra/recording/encode.go`), and masking mid-stream means decoding, filtering and
re-encoding every camera, continuously. The capacity story changes by an order of magnitude
for something most cameras will do for free. It is written down here because *"why not just
do it ourselves"* is the first question anybody asks.

**Masking only in the viewer** is the trap, not an option: an overlay drawn over a video
element looks identical to the operator and protects nothing.

## Read it back — the point of the camera half

A camera can accept a mask with **HTTP 200 and store something else**: a different
coordinate space, a bounding rectangle instead of the polygon, or nothing at all.
`infra/onvif/encoder.go` already carries this scar for H.265 — *"many cameras accept a
Media1 set with HTTP 200 but silently keep H.264"*.

So every write is read back and compared (`onvif.MasksMatch`, tolerance 0.06). Anything that
does not round-trip is reported as **unconfirmed**, which is treated as *not masked*. **A
privacy mask believed to be applied and not applied is worse than none, because somebody
relies on it.**

The tolerance is generous on purpose: cameras quantise mask coordinates to their own grid,
and a few pixels is the device being a device. A different coordinate space is out by a
factor, not by a rounding.

## Rules the tests pin

| Rule | Why |
|------|-----|
| A zone needs **three corners and some area** | A zone covering nothing is the worst row this table can hold: it reads as protection on every screen that lists it. |
| Zones are pushed **on save**, not on a later "Apply" | A zone that is saved and not applied says "protected" and protects nothing, and the gap is where somebody stops paying attention. |
| Editing **updates the mask in place** | On some cameras delete-then-create leaves the view briefly *unmasked* — a privacy control with a gap every time somebody adjusts it. |
| Deleting clears the **camera's** mask first | The other order leaves a region masked that nothing in the product knows about — removable only from the camera's own web page. |
| A rectangle-only camera is sent a **rectangle**, reduced here | Letting the camera square it off silently would fail verification forever and report a working mask as unconfirmed. |
| Over the camera's mask limit → the extras are **named in the log** and the status drops off confirmed | The camera would take the first few and ignore the rest, and *which* were dropped would be invisible. |
| Only **our** masks are removed | A mask set up on the camera's own web page is somebody else's decision; deleting it would be this product removing a privacy control it did not create. |
| A camera-side style the device cannot do **falls back to Color** | A solid box is still a mask; no mask is not. |
| Camera deletion drops our **rows**, not the camera's masks | The camera is leaving this appliance, not being decommissioned. |

## The status sentence belongs to the client

`PrivacyStatus.Detail` is English and exists for the API and the log. The **screen composes
its own sentence** from `Masking`, `UnconfirmedZones` and `HasZones`.

That split exists because the first version rendered `Detail` directly, and an Arabic screen
pass found the single most important line on the page printed in English in a non-English
UI — the same defect W3-4 shipped with its rule-schedule summaries. The screen check now
asserts, in any non-English run, that the banner is *not* the API's English text.

## Export redaction

`ExportRegions` feeds `evidence_export.go`. A redacted export is **re-encoded**, which
directly contradicts that file's own rule — *"an export must not re-encode, because
re-encoding changes every pixel and hands the other side an obvious argument that the footage
was processed"*. The answer is not to pretend otherwise: the bundle declares itself a
derivative in the manifest, the filename and VERIFY.txt, still carries the source digests,
and names the regions destroyed. See `ExportManifest.Redaction`.

**Not built:** face blur on export. The pipeline it needs now exists (a redacting export
already decodes, filters and re-encodes), but per-frame face detection is a different order
of cost and a separate failure mode. It is the natural W3-6b.
