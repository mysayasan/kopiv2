/* global config */

// apiBase resolves the API origin. When served by the webpack dev server (:4001)
// it points at the real backend; otherwise it uses the current origin.
export function apiBase() {
  const origin = window.location.origin;
  if (origin.includes(':4001') && config.apiUrl) {
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

// csrfToken returns the double-submit CSRF token the federated auth middleware
// requires on state-changing requests. The cookie is intentionally not HttpOnly so
// the SPA can echo it in the X-CSRF-Token header. Prefer the secure (__Host-) name.
export function csrfToken() {
  return readCookie('__Host-kopiv2_csrf') || readCookie('kopiv2_csrf');
}

const CSRF_HEADER = 'X-CSRF-Token';
const STATE_CHANGING = new Set(['POST', 'PUT', 'PATCH', 'DELETE']);

// api performs a same-origin fetch with the envelope unwrapped and CSRF handled.
// On 401/403 it redirects to the SSO login (the session expired or is missing).
// Returns { ok, status, body, message }.
export async function api(path, options = {}) {
  const method = (options.method || 'GET').toUpperCase();
  const headers = { ...(options.headers || {}) };
  if (options.body && !headers['Content-Type']) {
    headers['Content-Type'] = 'application/json';
  }
  // Double-submit CSRF: echo the cookie token on state-changing requests, or the
  // federated auth middleware rejects them with 403 (which previously bounced the
  // user to the SSO login and back to the dashboard).
  if (STATE_CHANGING.has(method)) {
    const token = csrfToken();
    if (token) headers[CSRF_HEADER] = token;
  }
  const resp = await fetch(`${apiBase()}${path}`, { credentials: 'same-origin', ...options, headers });
  if (resp.status === 401 || resp.status === 403) {
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

export function formatTimestamp(value) {
  if (!value) return '';
  const ms = value < 1e12 ? value * 1000 : value;
  try { return new Date(ms).toLocaleString(); } catch (_) { return ''; }
}
