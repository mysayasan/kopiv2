// myiotsan's helpers, ported into myseliasan's sensor-hub embed.
//
// This is the file that makes the embed an embed. The pages in ../ are myiotsan's real pages,
// and they were written for myiotsan, where `apiBase()` is the same origin they were served
// from. Here they render inside the CONTROL PLANE, against a node that is not this origin and
// — the whole point of the fleet design — is not reachable from the browser at all: it sits
// behind NAT with no inbound rule, and it dials US.
//
// So apiBase() is overridden to return the commander proxy for the node being managed:
//
//     /api/nodes/{nodeId}/proxy      +      /api/devices        (what the page asks for)
//     ------------------------------        ------------
//     myseliasan, this origin               the node's own path
//
// and every `${apiBase()}/api/...` in the copied pages tunnels to the node over its own
// control channel. THE BROWSER NEVER TALKS TO THE NODE. Nothing in this directory may ever
// fetch a node address directly; if you find yourself wanting to, the answer is a new proxied
// path, not a new origin.

/* global config */

// setNodeProxyBase is called by the hosting wrapper (node_iot_manager.js) BEFORE the copied
// pages render, so their first fetch already goes down the tunnel. One node is managed at a
// time, so a module-level base is enough (this mirrors the camera embed).
let __nodeProxyBase = '';
export function setNodeProxyBase(base) { __nodeProxyBase = base || ''; }

export function apiBase() {
  if (__nodeProxyBase) return __nodeProxyBase;
  // No base set = not inside an embed. Fall back to this origin (dev server -> real backend),
  // which is what myseliasan's own helpers do.
  const origin = window.location.origin;
  if (origin.includes(':4001') && typeof config !== 'undefined' && config.apiUrl) {
    return config.apiUrl;
  }
  return origin;
}

// unwrap pulls the payload out of the standard {data:{result}} / {result} envelope.
export function unwrap(payload) {
  if (payload && payload.data && Object.prototype.hasOwnProperty.call(payload.data, 'result')) {
    return payload.data.result;
  }
  if (payload && Object.prototype.hasOwnProperty.call(payload, 'result')) {
    return payload.result;
  }
  return payload;
}

export function errorMessage(payload, fallback) {
  if (typeof payload?.message === 'string' && payload.message.trim()) {
    return payload.message;
  }
  return fallback;
}

// readCookie returns a cookie value by name, or '' when absent.
export function readCookie(name) {
  const match = document.cookie.match(new RegExp('(?:^|; )' + name.replace(/([.$?*|{}()[\]\\/+^])/g, '\\$1') + '=([^;]*)'));
  return match ? decodeURIComponent(match[1]) : '';
}

// csrfToken returns the double-submit CSRF token myseliasan's federated auth middleware
// requires on state-changing requests — including the ones that are only passing THROUGH it
// on their way to a node.
export function csrfToken() {
  return readCookie('__Host-kopiv2_csrf') || readCookie('kopiv2_csrf');
}

const CSRF_HEADER = 'X-CSRF-Token';
const STATE_CHANGING = new Set(['POST', 'PUT', 'PATCH', 'DELETE']);

// api performs the proxied fetch. Returns { ok, status, body, message }.
//
// A 403 is NOT bounced to the SSO login the way myseliasan's own api() does for its own
// endpoints, and that distinction matters: through the proxy, 403 is a legitimate answer —
// "you are signed in, and your grant on this node does not permit that" — and the pages here
// are built to say so. Bouncing it to a login would tell the operator they are logged out,
// which is a lie, and would hide the one fact they need.
export async function api(path, options = {}) {
  const method = (options.method || 'GET').toUpperCase();
  const headers = { ...(options.headers || {}) };
  if (options.body && !headers['Content-Type']) {
    headers['Content-Type'] = 'application/json';
  }
  if (STATE_CHANGING.has(method)) {
    const token = csrfToken();
    if (token) headers[CSRF_HEADER] = token;
  }
  let resp;
  try {
    resp = await fetch(`${apiBase()}${path}`, { credentials: 'same-origin', ...options, headers });
  } catch (_) {
    // A dead network is not an empty estate. Surface it as a failure, not as "no devices".
    return { ok: false, status: 0, body: null, message: '' };
  }
  // A 401 IS the session being gone — there is nothing this page can do about that.
  if (resp.status === 401) {
    window.location.href = '/api/auth/start';
    throw new Error('unauthenticated');
  }
  const text = await resp.text();
  let payload = null;
  if (text) {
    try { payload = JSON.parse(text); } catch (_) { payload = { message: text }; }
  }
  return { ok: resp.ok, status: resp.status, body: unwrap(payload), message: payload?.message };
}

// forbidden reports the one status that means "the node said no to YOU".
//
// The tunnel asserts the caller's role NAME at the node and the node evaluates its OWN
// permission matrix, so a myseliasan user's per-node grant (viewer / operator / admin) is what
// they are over there. There is no endpoint that tells the browser which of the three it got,
// and so this embed DOES NOT GUESS: it asks, and when the answer is 403 it says so. A
// client-side guess that disagreed with the server would be worse than the 403 — it would hide
// a control the user actually has, or offer one they do not.
export function forbidden(r) {
  return !!r && r.status === 403;
}

// unreachable is the proxy's answer when the node is not currently connected: it has no
// tunnel to forward the call down. Distinguishing it from a real 404 (a device id that does
// not exist) is why it is worth its own helper.
export function unreachable(r) {
  return !!r && (r.status === 404 || r.status === 502 || r.status === 503 || r.status === 0);
}

// accessError renders a failed proxied call the way an operator needs to read it: "you may
// not", "it is not reachable", or the server's own words. `fallback` is used only when the
// server said nothing useful.
export function accessError(r, t, fallback) {
  if (!r) return fallback;
  if (forbidden(r)) return t('niot.forbidden');
  if (unreachable(r) && !r.message) return t('niot.unreachable');
  return errorMessage(r, fallback);
}

export function formatTimestamp(value) {
  if (!value) return '';
  const ms = value < 1e12 ? value * 1000 : value;
  try { return new Date(ms).toLocaleString(); } catch (_) { return ''; }
}

// formatClock renders a chart tick: the time of day, plus the date once the span is
// wide enough that a bare clock would be ambiguous.
export function formatClock(ms, withDate = false) {
  try {
    const d = new Date(ms);
    const time = d.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' });
    if (!withDate) return time;
    return `${d.toLocaleDateString([], { month: 'short', day: 'numeric' })} ${time}`;
  } catch (_) { return ''; }
}

// formatAgo turns a unix-seconds instant into "4m ago" / "2h ago". `t` is the caller's
// translate fn so the phrasing stays localized (and the unit words come from i18n.js).
export function formatAgo(unixSec, t) {
  if (!unixSec) return t('time.never');
  const secs = Math.max(0, Math.floor(Date.now() / 1000 - unixSec));
  if (secs < 60) return t('time.agoSeconds', { n: secs });
  if (secs < 3600) return t('time.agoMinutes', { n: Math.floor(secs / 60) });
  if (secs < 86400) return t('time.agoHours', { n: Math.floor(secs / 3600) });
  return t('time.agoDays', { n: Math.floor(secs / 86400) });
}

// formatNumber trims float noise without forcing decimals onto whole numbers.
export function formatNumber(v) {
  if (typeof v !== 'number' || !isFinite(v)) return '—';
  const abs = Math.abs(v);
  const digits = abs >= 100 ? 1 : abs >= 1 ? 2 : 3;
  return String(Number(v.toFixed(digits)));
}

// formatCount groups thousands so a 5-million-sample ingest counter stays readable.
export function formatCount(v) {
  const n = Number(v || 0);
  try { return n.toLocaleString(); } catch (_) { return String(n); }
}
