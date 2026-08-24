# Module: infra/onvif/ptz.go

## Purpose

PTZ beyond jogging: **presets, home and absolute position** (W3-5). Response parsing lives in
`ptz_parse.go`.

`client.go` stopped at `ContinuousMove` / `Stop`, which is the whole of "an operator is
holding a button down". Everything an unattended appliance does with a PTZ camera needs a
**named place** instead: a guard tour visits places, an alarm sends the camera to a place,
and "put it back" means the home place.

## The places live on the camera, not in our database

ONVIF presets are stored by the device and recalled by a token the device issues. Mirroring
them into a local table would create a second answer to "where can this camera point", and
the two would part company the first time somebody used the camera's own web page — which is
how a large share of PTZ cameras in the field get set up.

So presets are read live, and anything of ours that refers to one (a tour stop, an alarm
recall) stores the device's token and copes with it having gone away. See
`apps/mymatasan/services/ptz.go`.

## Calls

| Method | ONVIF |
|--------|-------|
| `GetPresets` | `tptz:GetPresets` |
| `SetPreset` | `tptz:SetPreset` — stores where the camera **is now** |
| `GotoPreset` | `tptz:GotoPreset` |
| `RemovePreset` | `tptz:RemovePreset` |
| `GotoHome` / `SetHome` | `tptz:GotoHomePosition` / `tptz:SetHomePosition` |
| `PTZGetStatus` | `tptz:GetStatus` — position + whether it is still moving |
| `PTZGoto` | `tptz:AbsoluteMove` or `tptz:RelativeMove` |

`SetPreset` stores *where the camera is now*, not a set of coordinates, because that is the
only gesture an operator can perform accurately: they drive the camera until the picture is
right and then say "here". Absolute coordinates are for a machine.

## Decisions the tests pin

| Rule | Why |
|------|-----|
| **A zero speed is omitted, never sent as 0** | An ONVIF speed of 0 is a valid vector meaning "do not move", so a defaulted speed produces a preset recall that is accepted and never arrives — the hardest kind of failure to diagnose, because every layer reports success. |
| `AbsoluteMove` says `Position`, `RelativeMove` says `Translation` | Lenient devices accept the wrong element name and strict ones silently ignore it, which looks exactly like a camera that cannot move. |
| A create with no returned token is an **error**; an overwrite with no token is not | A new preset nothing can address is a position we cannot recall or delete. On an overwrite the caller already knows the token, and several devices answer with an empty `SetPresetResponse`. |
| Presets with no token are **dropped** from the list | The token is the only handle; a tokenless row is one an operator can see and cannot use, and it would make an empty-string tour stop look valid. |
| A preset the device did not name shows as its token | A blank row in a list of places is unusable. |
| `HasPosition` is separate from `Position` | A device that omits `PanTilt`/`Zoom` parses as all-zeroes, which is a legitimate position — "did it say anything" has to be answered separately from "what did it say", or an absent position renders as dead centre. |
| `MoveStatus` `UNKNOWN` is **not** moving | A device that never reports `MOVING` would otherwise leave every recall looking permanently in flight, and a tour would never take its next step. |
| Preset names are XML-escaped | They are operator-supplied text that ends up inside a SOAP body. |

## The camera's own words

A SOAP fault is how a device says *"that preset does not exist"*, *"the preset store is
full"*, or *"this profile has no PTZ configuration"* — all ordinary answers when a person is
managing presets, and all arriving as HTTP 500. `postSOAP` turns that into
`ONVIF SOAP endpoint returned status 500`, which tells an operator nothing and sends an
installer to check the network.

`ParseSOAPFault` (in `ptz_parse.go`) reads SOAP 1.2 `Reason/Text`, falls back to SOAP 1.1
`faultstring` (such devices exist in the field), and finally to the innermost `Code/Subcode`
with its namespace prefix stripped — `ter:NoPreset` becomes `NoPreset`. `ptzError` wraps
every PTZ call with it, so what reaches the screen is the sentence the camera sent.

Live-benched against `tools/fleetbench/onvifsim.py`: recalling a preset the device does not
have surfaces *"recall PTZ preset failed: The requested preset token does not exist"*, with
no HTTP status in the message.
