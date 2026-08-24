# onvifsim — a small ONVIF PTZ device, so W3-5 can be benched against something real.
#
# WHY THIS EXISTS. Every other bench in this programme drives real software over a real
# network, and the harness's cameras are mediamtx RTSP sources with NO ONVIF service at all.
# PTZ presets, home and guard tours are ENTIRELY an ONVIF conversation: without a device that
# answers GetPresets, SetPreset, GotoPreset and RemovePreset, the only thing a bench could
# check is that the appliance produces an error, which is a test of the error path.
#
# So this answers the SOAP calls the product makes, keeps the state a real dome keeps
# (a preset table, a home position, where it is pointing), and — the part that makes the
# bench worth running — RECORDS EVERY CALL, so a bench can assert that the appliance sent
# GotoPreset for the stops of a tour, in order, at roughly the right times. A dome that
# merely accepted the commands would let a broken patrol pass.
#
# It is deliberately lenient about the envelope and strict about the semantics: it does not
# validate namespaces (that is not what is under test), but it DOES refuse a preset token it
# does not have, with a real SOAP fault — because "the camera says no" is an ordinary answer
# a preset feature has to handle, and the appliance's job is to show what the camera said.
#
# Stdlib only, so it runs in a bare python:3-slim container on the bench network.
#
# Endpoints, all on one port:
#   POST /onvif/device_service   GetCapabilities, GetServices, GetDeviceInformation
#   POST /onvif/media_service    GetProfiles
#   POST /onvif/ptz_service      the PTZ calls
#   GET  /journal                what the device was asked to do (JSON) — for the bench
#   POST /journal/reset          forget it
#   POST /presets/wipe           delete every preset, as somebody would from the camera's
#                                own web page — which is the case a tour has to survive
#   POST /onvif/event_service    CreatePullPointSubscription (W3-5b)
#   POST /onvif/subscription/ID  PullMessages / Renew / Unsubscribe
#   POST /inputs/<token>         flip a digital input, as a door contact would
#   POST /subscriptions/expire   drop every subscription WITHOUT telling anybody, which is
#                                exactly what a camera does when a lease is not renewed
#   POST /onvif/media2_service   Media2: privacy masks (W3-6)
#   POST /masks/mode/<mode>      honest | shifted | rectangle | drop - make the camera store
#                                something OTHER than what it was sent, which is the case
#                                the product's read-back verification exists to catch
#   POST /masks/support/<on|off> turn Media2 mask support off entirely
#   POST /masks/limit/<n>        how many masks the camera will hold
import json
import re
import sys
import threading
import time
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

HOST_NAME = "onvifsim"
PORT = 8080

# An RLock, not a Lock. `note()` takes the lock to append to the journal, and several
# handlers want to record something WHILE already holding it — with a plain Lock that is a
# deadlock, and the symptom is the whole simulator going silent mid-bench, which reads as a
# product failure. Found exactly that way.
LOCK = threading.RLock()
JOURNAL = []
PRESETS = {}          # token -> {"name": str, "pan": float, "tilt": float, "zoom": float}
POSITION = {"pan": 0.0, "tilt": 0.0, "zoom": 0.0}
HOME = {"pan": 0.0, "tilt": 0.0, "zoom": 0.0}
NEXT_TOKEN = [1]

# --- W3-5b: events, inputs and relays ----------------------------------------------------
#
# RELAYS carry the two properties that decide how the product must drive them: Mode
# (Monostable returns to idle by itself, Bistable stays put) and IdleState. RELAY_1 is
# bistable and REFUSES to be reconfigured, which is the case that forces the appliance to
# hold the output itself; RELAY_2 accepts SetRelayOutputSettings, which is the case where
# the device takes the responsibility back.
RELAYS = {
    "RELAY_1": {"mode": "Bistable", "delay": 0, "idle": "closed", "active": False,
                "refuse_settings": True},
    "RELAY_2": {"mode": "Bistable", "delay": 0, "idle": "closed", "active": False,
                "refuse_settings": False},
}

# INPUTS are what a door contact, a beam or a panic button looks like from here.
INPUTS = {"DIGIT_INPUT_000": False, "DIGIT_INPUT_001": False}

# --- W3-6: privacy masks ------------------------------------------------------------------
#
# MASK_MODE is the knob that makes this simulator worth having for W3-6. A real camera can
# accept a mask with HTTP 200 and store something else, and the product's whole camera-side
# guarantee rests on noticing that. The bench flips this to reproduce it on demand:
#
#   "honest"    stores what it was sent (the confirmed case)
#   "shifted"   stores a DIFFERENT coordinate space - accepted, applied to the wrong part of
#               the picture, and indistinguishable from success without a read-back
#   "rectangle" reduces any polygon to its bounding box, as a rectangle-only camera does
#   "drop"      accepts the write and stores NOTHING, the loudest silent failure
MASK_MODE = ["honest"]
MASKS = {}
NEXT_MASK = [1]
# MASK_SUPPORT lets the bench turn a camera into one with no Media2 mask support at all,
# which is the case the product has to REPORT rather than retry.
MASK_SUPPORT = [True]
MAX_MASKS = [4]

# SUBSCRIPTIONS is the PullPoint state. Each holds a QUEUE of pending notification messages
# and a termination time, because the lease is the thing the product has to keep alive and
# a bench has to be able to let one lapse on purpose.
SUBSCRIPTIONS = {}
NEXT_SUB = [1]

# A subscription that has just been created owes the client the CURRENT state of everything
# the device publishes, as PropertyOperation="Initialized". This is the trap the product is
# arranged around — treated as events, every reconnect raises an alarm for every door that
# happens to be closed — so the simulator reproduces it faithfully rather than conveniently.
def initial_messages():
    out = []
    for token, state in sorted(INPUTS.items()):
        out.append(("tns1:Device/Trigger/DigitalInput", "Initialized",
                    {"InputToken": token}, {"LogicalState": str(state).lower()}))
    for token, relay in sorted(RELAYS.items()):
        out.append(("tns1:Device/Trigger/Relay", "Initialized",
                    {"RelayToken": token}, {"LogicalState": "active" if relay["active"] else "inactive"}))
    return out


def queue_for_all(topic, operation, source, data):
    for sub in SUBSCRIPTIONS.values():
        sub["queue"].append((topic, operation, source, data))

ENV_OPEN = (
    '<?xml version="1.0" encoding="UTF-8"?>'
    '<s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope"'
    ' xmlns:tds="http://www.onvif.org/ver10/device/wsdl"'
    ' xmlns:trt="http://www.onvif.org/ver10/media/wsdl"'
    ' xmlns:tptz="http://www.onvif.org/ver20/ptz/wsdl"'
    ' xmlns:tt="http://www.onvif.org/ver10/schema"><s:Body>'
)
ENV_CLOSE = "</s:Body></s:Envelope>"


def base_url(host_header):
    # The subscription address the device hands out has to be reachable BY THE APPLIANCE,
    # which is inside the bench network — so it is built from the simulator's own network
    # alias rather than from whatever Host header the request happened to carry.
    host = (host_header or "%s:%d" % (HOST_NAME, PORT)).strip()
    return "http://%s" % host


def note(action, detail=None):
    with LOCK:
        JOURNAL.append({"at": time.time(), "action": action, "detail": detail or {}})


def element(body, name):
    """Pull one element's text out of a SOAP body, namespace prefix and all."""
    m = re.search(r"<(?:\w+:)?%s[^>]*>(.*?)</(?:\w+:)?%s>" % (name, name), body, re.S)
    return m.group(1).strip() if m else ""


def fault(reason, subcode="ter:InvalidArgVal"):
    return (
        '<?xml version="1.0" encoding="UTF-8"?>'
        '<s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope"><s:Body><s:Fault>'
        "<s:Code><s:Value>s:Sender</s:Value><s:Subcode><s:Value>%s</s:Value></s:Subcode></s:Code>"
        '<s:Reason><s:Text xml:lang="en">%s</s:Text></s:Reason>'
        "</s:Fault></s:Body></s:Envelope>" % (subcode, reason)
    )


def device_response(body, host):
    url = base_url(host)
    if "GetCapabilities" in body:
        return ENV_OPEN + (
            "<tds:GetCapabilitiesResponse><tds:Capabilities>"
            "<tt:Media><tt:XAddr>%s/onvif/media_service</tt:XAddr></tt:Media>"
            "<tt:PTZ><tt:XAddr>%s/onvif/ptz_service</tt:XAddr></tt:PTZ>"
            "<tt:Events><tt:XAddr>%s/onvif/event_service</tt:XAddr></tt:Events>"
            "</tds:Capabilities></tds:GetCapabilitiesResponse>" % (url, url, url)
        ) + ENV_CLOSE
    if "GetServices" in body:
        # Built by concatenation rather than one big format string: the Media2 entry is
        # CONDITIONAL (the bench turns mask support off to test the unsupported path), and
        # `%` binds tighter than `+`, so folding a conditional into the literal chain
        # silently applies the arguments to only the last fragment.
        services = (
            "<tds:GetServicesResponse>"
            "<tds:Service><tds:Namespace>http://www.onvif.org/ver10/media/wsdl</tds:Namespace>"
            "<tds:XAddr>%s/onvif/media_service</tds:XAddr></tds:Service>"
            "<tds:Service><tds:Namespace>http://www.onvif.org/ver20/ptz/wsdl</tds:Namespace>"
            "<tds:XAddr>%s/onvif/ptz_service</tds:XAddr></tds:Service>"
            "<tds:Service><tds:Namespace>http://www.onvif.org/ver10/device/wsdl</tds:Namespace>"
            "<tds:XAddr>%s/onvif/device_service</tds:XAddr></tds:Service>"
            "<tds:Service><tds:Namespace>http://www.onvif.org/ver10/events/wsdl</tds:Namespace>"
            "<tds:XAddr>%s/onvif/event_service</tds:XAddr></tds:Service>"
        ) % (url, url, url, url)
        if MASK_SUPPORT[0]:
            services += (
                "<tds:Service><tds:Namespace>http://www.onvif.org/ver20/media/wsdl</tds:Namespace>"
                "<tds:XAddr>%s/onvif/media2_service</tds:XAddr></tds:Service>" % url
            )
        services += "</tds:GetServicesResponse>"
        return ENV_OPEN + services + ENV_CLOSE
    if "GetDeviceInformation" in body:
        return ENV_OPEN + (
            "<tds:GetDeviceInformationResponse>"
            "<tds:Manufacturer>fleetbench</tds:Manufacturer>"
            "<tds:Model>PTZ sim</tds:Model>"
            "<tds:FirmwareVersion>1.0</tds:FirmwareVersion>"
            "<tds:SerialNumber>SIM-0001</tds:SerialNumber>"
            "<tds:HardwareId>SIM</tds:HardwareId>"
            "</tds:GetDeviceInformationResponse>"
        ) + ENV_CLOSE
    if "GetRelayOutputs" in body:
        note("GetRelayOutputs")
        with LOCK:
            items = "".join(
                '<tds:RelayOutputs token="%s"><tt:Properties>'
                "<tt:Mode>%s</tt:Mode><tt:DelayTime>PT%dS</tt:DelayTime>"
                "<tt:IdleState>%s</tt:IdleState></tt:Properties></tds:RelayOutputs>"
                % (token, r["mode"], r["delay"], r["idle"])
                for token, r in sorted(RELAYS.items())
            )
        return ENV_OPEN + "<tds:GetRelayOutputsResponse>%s</tds:GetRelayOutputsResponse>" % items + ENV_CLOSE

    if "SetRelayOutputSettings" in body:
        token = element(body, "RelayOutputToken")
        mode = element(body, "Mode")
        delay = element(body, "DelayTime")
        with LOCK:
            relay = RELAYS.get(token)
            if relay is None:
                return fault("The relay token is not valid", "ter:InvalidArgVal")
            if relay["refuse_settings"]:
                # A real device that will not be reconfigured. This is the case that makes
                # the appliance hold the output itself, and the bench needs one.
                note("SetRelayOutputSettings", {"token": token, "refused": True})
                return fault("This output is not configurable", "ter:ActionNotSupported")
            relay["mode"] = mode or "Monostable"
            seconds = 0
            digits = "".join(ch for ch in delay if ch.isdigit())
            if digits:
                seconds = int(digits)
            relay["delay"] = seconds
        note("SetRelayOutputSettings", {"token": token, "mode": mode, "delay": delay})
        return ENV_OPEN + "<tds:SetRelayOutputSettingsResponse/>" + ENV_CLOSE

    if "SetRelayOutputState" in body:
        token = element(body, "RelayOutputToken")
        state = element(body, "LogicalState")
        with LOCK:
            relay = RELAYS.get(token)
            if relay is None:
                JOURNAL.append({"at": time.time(), "action": "SetRelayOutputState",
                                "detail": {"token": token, "refused": True}})
                return fault("The relay token is not valid", "ter:InvalidArgVal")
            relay["active"] = (state == "active")
            JOURNAL.append({"at": time.time(), "action": "SetRelayOutputState",
                            "detail": {"token": token, "state": state}})
            queue_for_all("tns1:Device/Trigger/Relay", "Changed",
                          {"RelayToken": token}, {"LogicalState": state})
        return ENV_OPEN + "<tds:SetRelayOutputStateResponse/>" + ENV_CLOSE

    return ENV_OPEN + ENV_CLOSE


def store_mask(polygon):
    """Apply MASK_MODE, modelling a camera that keeps something other than what it was sent."""
    mode = MASK_MODE[0]
    if mode == "drop":
        return []
    if mode == "shifted":
        return [(x / 2, y / 2) for x, y in polygon]
    if mode == "rectangle" and polygon:
        xs = [p[0] for p in polygon]
        ys = [p[1] for p in polygon]
        return [(min(xs), min(ys)), (max(xs), min(ys)), (max(xs), max(ys)), (min(xs), max(ys))]
    return list(polygon)


def mask_points(body):
    out = []
    for m in re.finditer(r'<(?:\w+:)?Point\s+x="([-\d.]+)"\s+y="([-\d.]+)"', body):
        out.append((float(m.group(1)), float(m.group(2))))
    return out


def media2_response(body):
    """ONVIF Media2: privacy masks (W3-6)."""
    if not MASK_SUPPORT[0]:
        return fault("The Media2 service is not supported", "ter:ActionNotSupported")

    if "GetMaskOptions" in body:
        note("GetMaskOptions")
        return ENV_OPEN + (
            "<tr2:GetMaskOptionsResponse><tr2:Options>"
            "<tr2:MaxMasks>%d</tr2:MaxMasks><tr2:MaxPoints>8</tr2:MaxPoints>"
            "<tr2:RectangleOnly>%s</tr2:RectangleOnly>"
            "<tr2:Types>Color</tr2:Types><tr2:Types>Blurred</tr2:Types>"
            "</tr2:Options></tr2:GetMaskOptionsResponse>"
            % (MAX_MASKS[0], "true" if MASK_MODE[0] == "rectangle" else "false")
        ) + ENV_CLOSE

    if "GetMasks" in body:
        note("GetMasks")
        with LOCK:
            items = "".join(
                '<tr2:Masks token="%s"><tr2:ConfigurationToken>%s</tr2:ConfigurationToken>'
                "<tr2:Polygon>%s</tr2:Polygon><tr2:Type>%s</tr2:Type>"
                "<tr2:Enabled>%s</tr2:Enabled></tr2:Masks>"
                % (token, m["config"],
                   "".join('<tt:Point x="%.4f" y="%.4f"/>' % (x, y) for x, y in m["polygon"]),
                   m["type"], "true" if m["enabled"] else "false")
                for token, m in sorted(MASKS.items())
            )
        return ENV_OPEN + "<tr2:GetMasksResponse>%s</tr2:GetMasksResponse>" % items + ENV_CLOSE

    if "CreateMask" in body:
        with LOCK:
            if len(MASKS) >= MAX_MASKS[0]:
                note("CreateMask", {"refused": "full"})
                return fault("The maximum number of masks has been reached", "ter:TooManyItems")
            token = "MASK_%d" % NEXT_MASK[0]
            NEXT_MASK[0] += 1
            MASKS[token] = {
                "config": element(body, "ConfigurationToken") or "VSC_1",
                "polygon": store_mask(mask_points(body)),
                "type": element(body, "Type") or "Color",
                "enabled": "true" in element(body, "Enabled").lower(),
            }
        note("CreateMask", {"token": token, "mode": MASK_MODE[0]})
        return ENV_OPEN + (
            "<tr2:CreateMaskResponse><tr2:Token>%s</tr2:Token></tr2:CreateMaskResponse>" % token
        ) + ENV_CLOSE

    if "SetMask" in body:
        m = re.search(r'<(?:\w+:)?Mask\s+token="([^"]+)"', body)
        token = m.group(1) if m else ""
        with LOCK:
            if token not in MASKS:
                note("SetMask", {"token": token, "refused": True})
                return fault("No mask with that token", "ter:NoConfig")
            MASKS[token]["polygon"] = store_mask(mask_points(body))
            MASKS[token]["type"] = element(body, "Type") or MASKS[token]["type"]
            MASKS[token]["enabled"] = "true" in element(body, "Enabled").lower()
        note("SetMask", {"token": token, "mode": MASK_MODE[0]})
        return ENV_OPEN + "<tr2:SetMaskResponse/>" + ENV_CLOSE

    if "DeleteMask" in body:
        token = element(body, "Token")
        with LOCK:
            MASKS.pop(token, None)
        note("DeleteMask", {"token": token})
        return ENV_OPEN + "<tr2:DeleteMaskResponse/>" + ENV_CLOSE

    return ENV_OPEN + ENV_CLOSE


def event_response(body):
    """The PullPoint surface: subscribe, pull, renew, unsubscribe."""
    if "CreatePullPointSubscription" in body:
        lease = duration_seconds(element(body, "InitialTerminationTime")) or 60
        with LOCK:
            sub_id = "SUB_%d" % NEXT_SUB[0]
            NEXT_SUB[0] += 1
            SUBSCRIPTIONS[sub_id] = {
                "expires": time.time() + lease,
                # The initial state of everything, exactly as a real device sends it.
                "queue": list(initial_messages()),
            }
        note("CreatePullPointSubscription", {"id": sub_id, "lease": lease})
        return ENV_OPEN + (
            "<tev:CreatePullPointSubscriptionResponse>"
            "<tev:SubscriptionReference><wsa:Address>%s/onvif/subscription/%s</wsa:Address>"
            "</tev:SubscriptionReference>"
            "<wsnt:CurrentTime>%s</wsnt:CurrentTime>"
            "<wsnt:TerminationTime>%s</wsnt:TerminationTime>"
            "</tev:CreatePullPointSubscriptionResponse>"
            % (base_url(None), sub_id, iso(time.time()), iso(time.time() + lease))
        ) + ENV_CLOSE
    return ENV_OPEN + ENV_CLOSE


def subscription_response(sub_id, body):
    with LOCK:
        sub = SUBSCRIPTIONS.get(sub_id)
        # A LAPSED subscription is gone, and saying so is the whole point of the lease: the
        # product has to notice, and the failure it has to notice is not an error on the
        # wire, it is this.
        if sub is None or sub["expires"] < time.time():
            SUBSCRIPTIONS.pop(sub_id, None)
            return fault("The subscription does not exist or has expired", "ter:InvalidArgVal")

    if "Unsubscribe" in body:
        with LOCK:
            SUBSCRIPTIONS.pop(sub_id, None)
        note("Unsubscribe", {"id": sub_id})
        return ENV_OPEN + "<wsnt:UnsubscribeResponse/>" + ENV_CLOSE

    if "Renew" in body:
        lease = duration_seconds(element(body, "TerminationTime")) or 60
        with LOCK:
            sub["expires"] = time.time() + lease
        note("Renew", {"id": sub_id, "lease": lease})
        return ENV_OPEN + (
            "<wsnt:RenewResponse><wsnt:CurrentTime>%s</wsnt:CurrentTime>"
            "<wsnt:TerminationTime>%s</wsnt:TerminationTime></wsnt:RenewResponse>"
            % (iso(time.time()), iso(time.time() + lease))
        ) + ENV_CLOSE

    if "PullMessages" in body:
        timeout = duration_seconds(element(body, "Timeout")) or 10
        note("PullMessages", {"id": sub_id})
        # A REAL LONG POLL: hold the request open until something happens or the timeout
        # expires. Answering instantly would let a broken client look fine — and would hide
        # the whole reason the ONVIF client needed a per-call HTTP deadline.
        deadline = time.time() + min(timeout, 30)
        while time.time() < deadline:
            with LOCK:
                if SUBSCRIPTIONS.get(sub_id) is None:
                    return fault("The subscription does not exist", "ter:InvalidArgVal")
                if sub["queue"]:
                    break
            time.sleep(0.2)
        with LOCK:
            pending, sub["queue"] = sub["queue"], []
            expires = sub["expires"]
        items = "".join(
            "<wsnt:NotificationMessage><wsnt:Topic>%s</wsnt:Topic><wsnt:Message>"
            '<tt:Message UtcTime="%s" PropertyOperation="%s">'
            "<tt:Source>%s</tt:Source><tt:Data>%s</tt:Data>"
            "</tt:Message></wsnt:Message></wsnt:NotificationMessage>"
            % (topic, iso(time.time()), operation, simple_items(source), simple_items(data))
            for topic, operation, source, data in pending
        )
        return ENV_OPEN + (
            "<tev:PullMessagesResponse><tev:CurrentTime>%s</tev:CurrentTime>"
            "<tev:TerminationTime>%s</tev:TerminationTime>%s</tev:PullMessagesResponse>"
            % (iso(time.time()), iso(expires), items)
        ) + ENV_CLOSE

    return ENV_OPEN + ENV_CLOSE


def simple_items(values):
    return "".join('<tt:SimpleItem Name="%s" Value="%s"/>' % (k, v) for k, v in sorted(values.items()))


def iso(epoch):
    return time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime(epoch))


def duration_seconds(value):
    """Read the seconds out of the xs:duration subset ONVIF uses ("PT60S")."""
    v = (value or "").strip().upper()
    if not v.startswith("PT"):
        return 0
    total, digits = 0, ""
    for ch in v[2:]:
        if ch.isdigit():
            digits += ch
        elif ch in "HMS":
            if not digits:
                return 0
            n = int(digits)
            digits = ""
            total += n * {"H": 3600, "M": 60, "S": 1}[ch]
        else:
            return 0
    return total


def media_response(body):
    if "GetProfiles" in body:
        return ENV_OPEN + (
            '<trt:GetProfilesResponse><trt:Profiles token="MainProfile" fixed="true">'
            "<tt:Name>Main</tt:Name>"
            # A mask attaches to a video source CONFIGURATION, not to a profile - which is
            # what the product resolves before it can write one.
            '<tt:VideoSourceConfiguration token="VSC_1"><tt:Name>VSC</tt:Name>'
            "<tt:SourceToken>VS_1</tt:SourceToken></tt:VideoSourceConfiguration>"
            "<tt:VideoEncoderConfiguration><tt:Encoding>H264</tt:Encoding>"
            "<tt:Resolution><tt:Width>1920</tt:Width><tt:Height>1080</tt:Height></tt:Resolution>"
            "</tt:VideoEncoderConfiguration>"
            "</trt:Profiles></trt:GetProfilesResponse>"
        ) + ENV_CLOSE
    if "GetStreamUri" in body:
        return ENV_OPEN + (
            "<trt:GetStreamUriResponse><trt:MediaUri>"
            "<tt:Uri>rtsp://ptzcam:8554/cam</tt:Uri>"
            "</trt:MediaUri></trt:GetStreamUriResponse>"
        ) + ENV_CLOSE
    return ENV_OPEN + ENV_CLOSE


def ptz_response(body):
    if "GetPresets" in body:
        note("GetPresets")
        with LOCK:
            items = "".join(
                '<tptz:Preset token="%s"><tt:Name>%s</tt:Name>'
                '<tt:PTZPosition><tt:PanTilt x="%.3f" y="%.3f"/><tt:Zoom x="%.3f"/></tt:PTZPosition>'
                "</tptz:Preset>" % (token, p["name"], p["pan"], p["tilt"], p["zoom"])
                for token, p in sorted(PRESETS.items())
            )
        return ENV_OPEN + "<tptz:GetPresetsResponse>%s</tptz:GetPresetsResponse>" % items + ENV_CLOSE

    if "SetPreset" in body:
        name = element(body, "PresetName")
        token = element(body, "PresetToken")
        with LOCK:
            if token and token not in PRESETS:
                return fault("No preset with token %s" % token, "ter:NoPreset")
            if not token:
                # A real device caps its preset table. Kept low on purpose: an appliance
                # that cannot show the camera's own refusal is the defect being hunted.
                if len(PRESETS) >= 16:
                    return fault("Maximum number of presets reached", "ter:TooManyPresets")
                token = "PRESET_%d" % NEXT_TOKEN[0]
                NEXT_TOKEN[0] += 1
            PRESETS[token] = {"name": name or token, "pan": POSITION["pan"],
                              "tilt": POSITION["tilt"], "zoom": POSITION["zoom"]}
        note("SetPreset", {"token": token, "name": name})
        return ENV_OPEN + (
            "<tptz:SetPresetResponse><tptz:PresetToken>%s</tptz:PresetToken>"
            "</tptz:SetPresetResponse>" % token
        ) + ENV_CLOSE

    if "RemovePreset" in body:
        token = element(body, "PresetToken")
        with LOCK:
            if token not in PRESETS:
                return fault("The requested preset token does not exist", "ter:NoPreset")
            del PRESETS[token]
        note("RemovePreset", {"token": token})
        return ENV_OPEN + "<tptz:RemovePresetResponse/>" + ENV_CLOSE

    if "GotoPreset" in body:
        token = element(body, "PresetToken")
        with LOCK:
            preset = PRESETS.get(token)
            if preset is None:
                # Recorded even though it FAILED: a bench has to be able to tell "the
                # appliance never sent the command" from "the camera refused it".
                JOURNAL.append({"at": time.time(), "action": "GotoPreset",
                                "detail": {"token": token, "refused": True}})
                return fault("The requested preset token does not exist", "ter:NoPreset")
            POSITION.update({k: preset[k] for k in ("pan", "tilt", "zoom")})
            JOURNAL.append({"at": time.time(), "action": "GotoPreset",
                            "detail": {"token": token, "name": preset["name"]}})
        return ENV_OPEN + "<tptz:GotoPresetResponse/>" + ENV_CLOSE

    if "GotoHomePosition" in body:
        with LOCK:
            POSITION.update(HOME)
        note("GotoHomePosition")
        return ENV_OPEN + "<tptz:GotoHomePositionResponse/>" + ENV_CLOSE

    if "SetHomePosition" in body:
        with LOCK:
            HOME.update(POSITION)
        note("SetHomePosition", dict(HOME))
        return ENV_OPEN + "<tptz:SetHomePositionResponse/>" + ENV_CLOSE

    if "GetStatus" in body:
        with LOCK:
            pos = dict(POSITION)
        return ENV_OPEN + (
            "<tptz:GetStatusResponse><tptz:PTZStatus>"
            '<tt:Position><tt:PanTilt x="%.3f" y="%.3f"/><tt:Zoom x="%.3f"/></tt:Position>'
            "<tt:MoveStatus><tt:PanTilt>IDLE</tt:PanTilt><tt:Zoom>IDLE</tt:Zoom></tt:MoveStatus>"
            "<tt:UtcTime>%s</tt:UtcTime>"
            "</tptz:PTZStatus></tptz:GetStatusResponse>"
            % (pos["pan"], pos["tilt"], pos["zoom"],
               time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime()))
        ) + ENV_CLOSE

    if "ContinuousMove" in body:
        # A jog. The simulated dome does not integrate velocity; what matters to the bench
        # is that the appliance sent one, because a jog is what claims the camera for an
        # operator and suspends its patrol.
        note("ContinuousMove")
        return ENV_OPEN + "<tptz:ContinuousMoveResponse/>" + ENV_CLOSE

    if "Stop" in body:
        note("Stop")
        return ENV_OPEN + "<tptz:StopResponse/>" + ENV_CLOSE

    if "AbsoluteMove" in body or "RelativeMove" in body:
        note("AbsoluteMove")
        return ENV_OPEN + "<tptz:AbsoluteMoveResponse/>" + ENV_CLOSE

    return ENV_OPEN + ENV_CLOSE


class Handler(BaseHTTPRequestHandler):
    protocol_version = "HTTP/1.1"

    def log_message(self, *_args):
        pass

    def _send(self, status, body, content_type="application/soap+xml; charset=utf-8"):
        raw = body.encode("utf-8")
        self.send_response(status)
        self.send_header("Content-Type", content_type)
        self.send_header("Content-Length", str(len(raw)))
        self.end_headers()
        self.wfile.write(raw)

    def do_GET(self):
        if self.path.startswith("/journal"):
            with LOCK:
                body = json.dumps({"journal": list(JOURNAL), "presets": dict(PRESETS),
                                   "position": dict(POSITION), "home": dict(HOME),
                                   "relays": {k: dict(v) for k, v in RELAYS.items()},
                                   "inputs": dict(INPUTS),
                                   "subscriptions": len(SUBSCRIPTIONS),
                                   "masks": {k: {"polygon": v["polygon"], "type": v["type"],
                                                 "enabled": v["enabled"]}
                                             for k, v in MASKS.items()},
                                   "maskMode": MASK_MODE[0],
                                   "maskSupport": MASK_SUPPORT[0]})
            self._send(200, body, "application/json")
            return
        self._send(404, "not found", "text/plain")

    def do_POST(self):
        length = int(self.headers.get("Content-Length") or 0)
        body = self.rfile.read(length).decode("utf-8", "replace") if length else ""

        if self.path.startswith("/journal/reset"):
            with LOCK:
                del JOURNAL[:]
            self._send(200, "{}", "application/json")
            return
        if self.path.startswith("/presets/wipe"):
            # Somebody clearing the presets from the camera's own web page, which is the
            # event a guard tour has to notice and survive.
            with LOCK:
                PRESETS.clear()
            self._send(200, "{}", "application/json")
            return

        if self.path.startswith("/inputs/"):
            # The bench's way of opening a door: flip an input and let the subscription
            # deliver it exactly as the camera would.
            token = self.path.rsplit("/", 1)[-1]
            with LOCK:
                if token not in INPUTS:
                    self._send(404, "{}", "application/json")
                    return
                INPUTS[token] = not INPUTS[token]
                state = str(INPUTS[token]).lower()
                queue_for_all("tns1:Device/Trigger/DigitalInput", "Changed",
                              {"InputToken": token}, {"LogicalState": state})
            self._send(200, json.dumps({"token": token, "state": state}), "application/json")
            return
        if self.path.startswith("/masks/mode/"):
            # Make the camera dishonest on demand. This is the whole reason the product
            # reads its masks back instead of trusting a 200.
            mode = self.path.rsplit("/", 1)[-1]
            if mode not in ("honest", "shifted", "rectangle", "drop"):
                self._send(400, "{}", "application/json")
                return
            MASK_MODE[0] = mode
            self._send(200, json.dumps({"mode": mode}), "application/json")
            return
        if self.path.startswith("/masks/support/"):
            MASK_SUPPORT[0] = self.path.rsplit("/", 1)[-1] == "on"
            self._send(200, json.dumps({"support": MASK_SUPPORT[0]}), "application/json")
            return
        if self.path.startswith("/masks/limit/"):
            try:
                MAX_MASKS[0] = int(self.path.rsplit("/", 1)[-1])
            except ValueError:
                self._send(400, "{}", "application/json")
                return
            self._send(200, json.dumps({"maxMasks": MAX_MASKS[0]}), "application/json")
            return
        if self.path.startswith("/subscriptions/expire"):
            # Drop every subscription without telling anybody, which is precisely what a
            # camera does when a lease is not renewed — and the failure the product has to
            # turn from silence into an alert.
            with LOCK:
                SUBSCRIPTIONS.clear()
            self._send(200, "{}", "application/json")
            return

        if "ptz_service" in self.path:
            out = ptz_response(body)
        elif "media_service" in self.path:
            out = media_response(body)
        elif "media2_service" in self.path:
            out = media2_response(body)
        elif "event_service" in self.path:
            out = event_response(body)
        elif "/onvif/subscription/" in self.path:
            out = subscription_response(self.path.rsplit("/", 1)[-1], body)
        else:
            out = device_response(body, self.headers.get("Host"))

        # A SOAP fault travels with HTTP 500, which is what makes "keep the device's own
        # words" load-bearing on the appliance side.
        self._send(500 if "<s:Fault>" in out else 200, out)


def main():
    port = int(sys.argv[1]) if len(sys.argv) > 1 else PORT
    server = ThreadingHTTPServer(("0.0.0.0", port), Handler)
    print("onvifsim listening on %d" % port, flush=True)
    server.serve_forever()


if __name__ == "__main__":
    main()
