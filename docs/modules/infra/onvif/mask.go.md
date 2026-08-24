# Module: infra/onvif/mask.go

## Purpose

ONVIF **privacy masks**: regions the camera itself refuses to show anybody (W3-6), over the
Media2 service.

## Three places a mask could be applied, and only one is a privacy mask

- **In the camera.** The pixels never leave the sensor. The recording does not contain them,
  an export cannot leak them, a stolen drive has nothing. This file.
- **In our recording pipeline.** It would work, and it would cost the product its
  architecture: recording is `-c copy`, and masking mid-stream means decoding, filtering and
  re-encoding every camera, continuously.
- **In the viewer.** The trap. An overlay looks identical to the operator and protects
  nothing — the pixels are on disk, in the export, and in every copy of the file.

## Decisions the tests pin

| Rule | Why |
|------|-----|
| A camera with no Media2 is **not an error** | "This camera cannot mask" is an answer the product must show; an error would make the privacy screen refuse to load for exactly the cameras that need the warning. |
| `MaskPointFromUnit` / `MaskPointToUnit` are one pair, used both ways | Our space is 0..1 with the origin top-left; ONVIF's is -1..1 with the origin centre and **y inverted**. Three chances to be wrong in a way that still draws a plausible rectangle somewhere on the picture. The round trip is what the camera read-back is compared against — and the test also asserts the conversion is *not* the identity, because a round-trip test passes happily on a no-op. |
| `MasksMatch` fails on a **different point count** and on an **empty** read-back | A camera that reduced the polygon to a bounding box, or stored nothing, must never read as applied. |
| The tolerance is generous (0.05 default) | Cameras quantise to their own grid; a few pixels is the device being a device. A different coordinate space is out by a factor, not a rounding. |
| A **create** carries no token; an **update** does | Some devices treat a token on create as an update of a mask that does not exist and refuse the whole call. |
| A create with **no returned token** is an error | The mask may then exist on the camera and be uneditable and unremovable from here. |
| Tokenless masks are dropped from the list | The token is the only handle: listing one offers the operator a control that cannot work. |
| A mask with no type defaults to **Color** | Every camera that supports masks supports it. |

`MediaProfile.VideoSourceToken` was added to `ParseProfiles` for this: a mask attaches to a
**video source configuration**, not to a profile and not to the device. It matters most on a
multi-sensor camera, where masking the wrong configuration masks a lens nobody was worried
about and leaves the one they were worried about clear — and both look like success.

## The camera's own words

Every call goes through `maskError`, which prefers `ParseSOAPFault` over the HTTP status —
"the maximum number of masks has been reached" is an ordinary answer when somebody is
managing masks, and it arrives as HTTP 500.

Live-benched against `tools/fleetbench/onvifsim.py`, which can be told to be a **dishonest**
camera on demand: to store a mask in a different coordinate space, to reduce it to a
rectangle, or to accept it with HTTP 200 and store nothing at all.
