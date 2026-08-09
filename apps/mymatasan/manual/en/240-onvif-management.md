---
title: Managing a camera over ONVIF
category: cameras
categoryLabel: Cameras
summary: Read a camera's identity, set its clock and network, manage its accounts, reboot or reset it.
order: 240
---

# Managing a camera over ONVIF

A camera's **ONVIF** tab manages the camera itself, not how this appliance uses it. It saves
walking to the camera's own web interface for the things you actually do.

Everything here needs working camera credentials and a camera that supports the operation. The tab
shows what each camera advertises, so a control that is missing means the firmware does not offer
it.

## Identity {#identity}

Manufacturer, model, firmware version, hardware and serial identifiers, MAC address, ONVIF
version, and the services the camera advertises.

Two practical uses. **Firmware version** is the first thing to check when one camera behaves
differently from an identical one beside it. **MAC address** is how you find the camera in your
switch or DHCP server when its IP has moved.

## Clock {#clock}

Reads the camera's current time and lets you set the source.

- **NTP (automatic)** — the camera syncs from a time server. Use this.
- **Manual** — sets the camera's clock to this appliance's current time on save.

Also visible: time zone, daylight-savings handling, and whether NTP servers come from DHCP or are
set explicitly.

This matters more than it looks. **A camera with the wrong clock produces footage with the wrong
timestamps**, and when you are correlating a recording against a door log or a witness statement,
a camera that is eleven minutes out is worse than no camera. If you fix one thing on this tab, fix
this — and fix it on every camera, not just the one you were looking at.

## Network {#network}

Reads the camera's IP configuration and can change it: DHCP on or off, IPv4 address, prefix
length, gateway and DNS servers.

> [!WARNING]
> A wrong address, prefix or gateway makes the camera unreachable, and the only way back is
> usually a physical reset button. The appliance asks you to confirm for exactly this reason.

If you want cameras at fixed addresses, a DHCP reservation on your server is safer than a static
address on the camera. It survives a factory reset of the camera and it is changed from somewhere
you can still reach.

If you do set a static address, change it from a machine on the same subnet, and check the camera
answers on the new address before you close the page.

## Camera users {#users}

The camera's own ONVIF accounts: list them, add one, change a password, remove one. Roles are the
camera's — Administrator, Operator, User.

The useful practice is to give this appliance a dedicated non-administrator account with enough
rights to stream and to do whatever management you need, and keep the camera's admin login for
people. When the recorder's password has to change, exactly one thing changes, and the camera's
own logs tell you which access was the recorder.

## Maintenance {#maintenance}

**Reboot** restarts the camera. It is the correct first move for a camera that answers but streams
badly, and it costs a minute of footage.

**Soft reset** restores the camera to defaults but keeps its network settings. The camera stays
reachable; its profiles, credentials and image settings do not survive.

**Hard reset** restores full factory defaults **including network**. The camera will very likely
come back on a different address — possibly DHCP when you had a static — and you may have to
rediscover it. Do not do this remotely on a camera you cannot physically reach.

After any reset, re-check the camera's [Stream tab](camera-properties#stream): profiles are
usually renumbered, and the assignments this appliance was using no longer point where you think.

## When ONVIF controls are missing or fail {#limits}

- **The control is not shown.** The camera does not advertise that service. Use its own web
  interface.
- **The control is shown and fails.** Usually the account lacks the rights — ONVIF operator
  accounts often cannot change network settings. Retry with an administrator account on the
  camera.
- **Everything on the tab fails.** Suspect stored credentials first; see
  [Camera health](camera-health#troubleshooting).
