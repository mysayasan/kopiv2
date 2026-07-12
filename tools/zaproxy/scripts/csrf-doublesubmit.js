// ZAP HttpSender script — myseliasan double-submit CSRF.
//
// myseliasan's federated auth middleware rejects any state-changing request
// (POST/PUT/PATCH/DELETE) unless the X-CSRF-Token header equals the value of
// the non-HttpOnly CSRF cookie (__Host-kopiv2_csrf over HTTPS, kopiv2_csrf in
// dev). ZAP's cookie session manager replays the cookie but does NOT set the
// header, so without this every active-scan write would 403. This script reads
// the CSRF cookie off the outgoing request and mirrors it into the header, so
// active scanning actually reaches the write handlers.
//
// Loaded + enabled by the active plans (myseliasan-api.yaml / -full.yaml) via a
// `script` job. Harmless on GET requests (the header is simply added).

var CSRF_COOKIES = ["__Host-kopiv2_csrf", "kopiv2_csrf"];
var CSRF_HEADER = "X-CSRF-Token";

function sendingRequest(msg, initiator, helper) {
  var header = msg.getRequestHeader();
  var cookies = header.getHttpCookies();
  var token = null;
  for (var i = 0; i < cookies.size(); i++) {
    var c = cookies.get(i);
    for (var j = 0; j < CSRF_COOKIES.length; j++) {
      if (c.getName() == CSRF_COOKIES[j]) {
        token = c.getValue();
        break;
      }
    }
    if (token != null) {
      break;
    }
  }
  if (token != null && token.length > 0) {
    header.setHeader(CSRF_HEADER, token);
  }
}

function responseReceived(msg, initiator, helper) {
  // no-op
}
