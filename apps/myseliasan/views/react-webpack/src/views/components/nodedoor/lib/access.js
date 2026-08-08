// mypintusan's typed API surface, ported into myseliasan's DOOR-CONTROLLER embed.
//
// This is the file that makes the embed an embed. The pages in ../ are mypintusan's real pages,
// and they were written for mypintusan, where the API is the same origin they were served from.
// Here they render inside the CONTROL PLANE, against a node that is not this origin and — the
// whole point of the fleet design — is not reachable from the browser at all: it sits behind NAT
// with no inbound rule, and it dials US.
//
// So every call goes through the commander proxy for the node being managed:
//
//     /api/nodes/{nodeId}/proxy      +      /api/doors          (what the page asks for)
//     ------------------------------        ----------
//     myseliasan, this origin               the node's own path
//
// THE BROWSER NEVER TALKS TO THE NODE. Nothing in this directory may ever fetch a node address
// directly; if you find yourself wanting to, the answer is a new proxied path, not a new origin.
//
// Unlike the nodeiot helpers (which return {ok, status, body}), these THROW on failure — because
// mypintusan's pages were written against a throwing client and keeping that contract means the
// pages port with their error handling intact. A 403 through the proxy is a legitimate answer
// ("your grant on this node does not permit that") and is thrown with those words, never bounced
// to a login the caller is not actually logged out of.

/* global config */

// setNodeProxyBase is called by the hosting wrapper (node_door_manager.js) BEFORE the copied
// pages render, so their first fetch already goes down the tunnel. One node is managed at a
// time, so a module-level base is enough (this mirrors the camera and sensor-hub embeds).
let __nodeProxyBase = '';
export function setNodeProxyBase(base) { __nodeProxyBase = base || ''; }

// __t lets the wrapper hand the active translator to the thrown-error messages below without
// threading it through every page call.
let __t = (k) => k;
export function setTranslator(t) { __t = t || ((k) => k); }

export function apiBase() {
  if (__nodeProxyBase) return __nodeProxyBase;
  const origin = window.location.origin;
  if (origin.includes(':4001') && typeof config !== 'undefined' && config.apiUrl) {
    return config.apiUrl;
  }
  return origin;
}

function readCookie(name) {
  const match = document.cookie.match(new RegExp('(?:^|; )' + name.replace(/([.$?*|{}()[\]\\/+^])/g, '\\$1') + '=([^;]*)'));
  return match ? decodeURIComponent(match[1]) : '';
}

function csrfToken() {
  return readCookie('__Host-kopiv2_csrf') || readCookie('kopiv2_csrf');
}

const STATE_CHANGING = new Set(['POST', 'PUT', 'PATCH', 'DELETE']);

// unwrap pulls the payload out of the shared envelope, exactly as mypintusan's own client does.
function unwrap(res) {
  if (res && res.data && res.data.result !== undefined) return res.data.result;
  if (res && res.result !== undefined) return res.result;
  return res;
}

async function request(path, options = {}) {
  const method = (options.method || 'GET').toUpperCase();
  const headers = { Accept: 'application/json', ...(options.headers || {}) };
  if (options.body !== undefined) headers['Content-Type'] = 'application/json';
  if (STATE_CHANGING.has(method)) {
    const token = csrfToken();
    if (token) headers['X-CSRF-Token'] = token;
  }

  let resp;
  try {
    resp = await fetch(`${apiBase()}${path}`, {
      method,
      credentials: 'same-origin',
      headers,
      body: options.body !== undefined ? JSON.stringify(options.body) : undefined,
    });
  } catch (_) {
    // A dead network is not an empty estate. Surface it as a failure, not as "no doors".
    throw new Error(__t('ndoor.unreachable'));
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

  if (!resp.ok) {
    let message = payload && typeof payload.message === 'string' && payload.message.trim()
      ? payload.message : '';
    if (resp.status === 403) message = message || __t('ndoor.forbidden');
    // The proxy answers 404/502/503 when the node has no live tunnel to forward down.
    else if (resp.status === 404 || resp.status === 502 || resp.status === 503) {
      message = message || __t('ndoor.unreachable');
    } else message = message || `Request failed with ${resp.status}`;
    const err = new Error(message);
    err.status = resp.status;
    throw err;
  }
  return unwrap(payload);
}

const get = (path) => request(path);
const send = (path, method, body) => request(path, { method, body });

// --- doors --------------------------------------------------------------------------------------

export const listDoors = () => get('/api/doors');
// unlockDoor is the operator's remote-open — over the tunnel it is STILL logged on the node
// exactly like a badge, attributed to the control-plane operator by name ("cp:alice").
export const unlockDoor = (id) => send(`/api/doors/${id}/unlock`, 'POST');
export const listReaders = () => get('/api/readers');

// --- lockdown -----------------------------------------------------------------------------------

export const getLockdown = () => get('/api/lockdown');
export const setLockdown = (on) => send('/api/lockdown', 'POST', { lockdown: !!on });

// --- people and badges --------------------------------------------------------------------------

export const listHolders = () => get('/api/holders');
export const createHolder = (holder) => send('/api/holders', 'POST', holder);
export const listCredentials = (holderId) => get(`/api/holders/${holderId}/credentials`);
export const issueCredential = (holderId, credential) => send(`/api/holders/${holderId}/credentials`, 'POST', credential);
export const revokeCredential = (holderId, credId, status, reason) => send(`/api/holders/${holderId}/credentials/${credId}/revoke`, 'POST', { status, reason });

// --- the access log -----------------------------------------------------------------------------

export function listEvents({ limit = 200, offset = 0, doorId, holderId, decision, reason, since } = {}) {
  const q = new URLSearchParams();
  q.set('limit', String(limit));
  if (offset) q.set('offset', String(offset));
  if (doorId) q.set('doorId', String(doorId));
  if (holderId) q.set('holderId', String(holderId));
  if (decision) q.set('decision', decision);
  if (reason) q.set('reason', reason);
  if (since) q.set('since', String(since));
  return get(`/api/events?${q.toString()}`);
}

// --- settings -----------------------------------------------------------------------------------

// Settings come back with security keys REDACTED — each reader carries hasScbk/usingDefaultKey
// booleans instead of the key itself, and the node carries stored keys forward on save. The
// embed inherits that property: a control-plane operator can rename a reader without the site
// key ever crossing the tunnel.
export const getSettings = () => get('/api/settings/access');
export const saveSettings = (settings) => send('/api/settings/access', 'PUT', settings);
export const resetSettings = () => send('/api/settings/access/reset', 'POST');

// --- display helpers ----------------------------------------------------------------------------

// The node's enumerated denial reasons, mapped to the embed's i18n keys (same closed set as
// mypintusan's own REASON_KEYS, under the ndoor. prefix).
export const REASON_KEYS = {
  ok: 'ndoor.reason.ok',
  'unknown-credential': 'ndoor.reason.unknownCredential',
  'credential-inactive': 'ndoor.reason.credentialInactive',
  'credential-expired': 'ndoor.reason.credentialExpired',
  'credential-revoked': 'ndoor.reason.credentialRevoked',
  'holder-suspended': 'ndoor.reason.holderSuspended',
  'holder-expired': 'ndoor.reason.holderExpired',
  'no-grant': 'ndoor.reason.noGrant',
  'out-of-schedule': 'ndoor.reason.outOfSchedule',
  holiday: 'ndoor.reason.holiday',
  lockdown: 'ndoor.reason.lockdown',
  antipassback: 'ndoor.reason.antipassback',
  'offline-cache-expired': 'ndoor.reason.offlineStale',
  'offline-not-allowed': 'ndoor.reason.offlineNotAllowed',
  'offline-denied': 'ndoor.reason.offlineDenied',
  'secure-channel-failed': 'ndoor.reason.secureChannel',
  'reader-offline': 'ndoor.reason.readerOffline',
  'door-disabled': 'ndoor.reason.doorDisabled',
  'bad-pin': 'ndoor.reason.badPin',
  duress: 'ndoor.reason.duress',
  'door-forced': 'ndoor.reason.doorForced',
  'door-held-open': 'ndoor.reason.doorHeldOpen',
  'door-closed': 'ndoor.reason.doorClosed',
};

export function formatTime(unix) {
  if (!unix) return '—';
  return new Date(unix * 1000).toLocaleString();
}
