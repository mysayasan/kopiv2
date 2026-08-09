---
title: Adding cameras
category: cameras
categoryLabel: Cameras
summary: Discover cameras on the network or add one by address, and get past the credential gate.
order: 210
---

# Adding cameras

The Cameras entry in the rail opens the discovery page; each camera underneath it opens that
camera's own pages.

## Scanning the network {#scan}

**Scan network** looks for ONVIF cameras and lists what answers, showing which are already saved
and which are not.

By default it works out your local subnet and scans that. You can point it somewhere specific in
CIDR notation instead:

```
192.168.1.0/24   scan 192.168.1.1 to .254
10.10.20.0/24    scan a VLAN
```

Set the **subnet** explicitly when your cameras are on a different VLAN from the appliance, or
when the appliance has several network interfaces and auto-detection picks the wrong one. The
**scan timeout** is worth raising on a large or slow network — a camera that answers late looks
identical to one that is not there.

## When the scan finds nothing {#scan-empty}

In rough order of likelihood:

- **The cameras are on another subnet or VLAN.** Discovery uses multicast, which routers do not
  forward by default. Enter the subnet by hand, or add the cameras by address.
- **ONVIF discovery is disabled on the camera.** Many cameras ship with it off. Turn it on in the
  camera's own web interface, or add it by address.
- **A firewall is dropping the discovery traffic.**
- **The camera does not do ONVIF at all.** Plenty of cameras only speak RTSP. Add them by address.

A camera the scan cannot find is not a camera the appliance cannot use. Discovery is a
convenience, not a requirement.

## Adding by address {#by-address}

Probe a specific address instead of scanning. This is the reliable path, and the one to reach for
whenever discovery is inconvenient — a different VLAN, a camera reached through a NAT, or a camera
whose ONVIF discovery is off.

You need the camera's address and its credentials. If the camera speaks ONVIF, the appliance
works out its stream URLs itself; if it does not, give it the RTSP URL.

## Credentials {#credentials}

Nearly every camera needs a username and password to stream. Enter the camera's own credentials —
the ones you would use in its web interface — not any account on this appliance.

**Credentials are verified before the camera is saved.** A wrong password fails here, immediately,
with a message. This is deliberate: the alternative is a camera that appears to save fine and then
shows a black tile hours later, when nobody remembers what was typed.

If a camera later stops authenticating — somebody rotated its password — its pages block until the
stored credentials work again. Fix them on the camera's **Access** tab.

> [!TIP]
> Give the appliance its own account on each camera rather than sharing the admin login. When a
> password has to change, you change one thing in one place, and you can see in the camera's own
> logs which access was the recorder's.

## Naming {#naming}

Name a camera the way somebody would say it over a radio: *Front Door*, *Loading Bay*, *Car Park
North*. Not `CAM-04`, and not the model number.

Every alert, every notification and every recording carries this name, and it is what somebody
reads at 3am. Renaming later is easy — it is a field on the camera's Details tab — so there is no
cost to fixing a bad one.

## After adding {#after}

A newly added camera does nothing on its own. It is visible in live view; it is not recording, and
it is not detecting anything.

Two more steps make it useful:

1. Turn on recording — [Recording configuration](recording-configuration).
2. Add at least one detection rule — [Creating detection rules](detection-rules).

## Removing a camera {#removing}

Removing a camera deletes it and everything configured on it: its streams, its recording
configuration and its AI rules. It cannot be undone, and you type the camera's name to confirm.

Removal is also the escape hatch when a camera's password has been lost — you cannot open its
pages without working credentials, but you can always remove it and add it again.
