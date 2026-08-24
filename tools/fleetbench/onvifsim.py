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
import json
import re
import sys
import threading
import time
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

HOST_NAME = "onvifsim"
PORT = 8080

LOCK = threading.Lock()
JOURNAL = []
PRESETS = {}          # token -> {"name": str, "pan": float, "tilt": float, "zoom": float}
POSITION = {"pan": 0.0, "tilt": 0.0, "zoom": 0.0}
HOME = {"pan": 0.0, "tilt": 0.0, "zoom": 0.0}
NEXT_TOKEN = [1]

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
            "</tds:Capabilities></tds:GetCapabilitiesResponse>" % (url, url)
        ) + ENV_CLOSE
    if "GetServices" in body:
        return ENV_OPEN + (
            "<tds:GetServicesResponse>"
            "<tds:Service><tds:Namespace>http://www.onvif.org/ver10/media/wsdl</tds:Namespace>"
            "<tds:XAddr>%s/onvif/media_service</tds:XAddr></tds:Service>"
            "<tds:Service><tds:Namespace>http://www.onvif.org/ver20/ptz/wsdl</tds:Namespace>"
            "<tds:XAddr>%s/onvif/ptz_service</tds:XAddr></tds:Service>"
            "<tds:Service><tds:Namespace>http://www.onvif.org/ver10/device/wsdl</tds:Namespace>"
            "<tds:XAddr>%s/onvif/device_service</tds:XAddr></tds:Service>"
            "</tds:GetServicesResponse>" % (url, url, url)
        ) + ENV_CLOSE
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
    return ENV_OPEN + ENV_CLOSE


def media_response(body):
    if "GetProfiles" in body:
        return ENV_OPEN + (
            '<trt:GetProfilesResponse><trt:Profiles token="MainProfile" fixed="true">'
            "<tt:Name>Main</tt:Name>"
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
                                   "position": dict(POSITION), "home": dict(HOME)})
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

        if "ptz_service" in self.path:
            out = ptz_response(body)
        elif "media_service" in self.path:
            out = media_response(body)
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
