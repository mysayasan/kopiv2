/* global config */

// apiBase resolves the API origin. When served by the webpack dev server (:4002)
// it points at the real backend; otherwise it uses the current origin.
export function apiBase() {
  const origin = window.location.origin;
  if (origin.includes(':4002') && config.apiUrl) {
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

// csrfToken returns the double-submit CSRF token the auth middleware requires on
// state-changing requests. The cookie is intentionally not HttpOnly so the SPA can
// echo it in the X-CSRF-Token header. Prefer the secure (__Host-) name.
export function csrfToken() {
  return readCookie('__Host-kopiv2_csrf') || readCookie('kopiv2_csrf');
}

const CSRF_HEADER = 'X-CSRF-Token';
const STATE_CHANGING = new Set(['POST', 'PUT', 'PATCH', 'DELETE']);

// api performs a same-origin fetch with the envelope unwrapped and CSRF handled.
// Returns { ok, status, body, message }. Unlike the federated apps there is no SSO
// login to bounce to — myiotsan signs in locally — so a 401/403 is returned to the
// caller and App flips the shell back to the login card.
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
  const resp = await fetch(`${apiBase()}${path}`, { credentials: 'same-origin', ...options, headers });
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

// roleLabel maps a stored role name to its translated label. The names are stable
// server-side identifiers ("superadmin" / "operator" / "viewer") shared across the suite;
// only the label is localised. The admin role is stored as "superadmin" but shown as
// "Administrator" everywhere, matching mymatasan — a custom role falls back to its own name.
export function roleLabel(t, name) {
  switch (name) {
    case 'superadmin': return t('role.admin');
    case 'operator': return t('role.operator');
    case 'viewer': return t('role.viewer');
    default: return name;
  }
}

// Notification-destination helpers (mirrors mymatasan's constants, scaled to the IoT hub). A
// destination is one delivery target; empty `categories` = subscribed to everything.
export const notificationCategories = [
  ['device.alert', 'Device alerts', 'When an IoT rule fires on a sensor reading (including a device going offline).'],
  ['system', 'System events', 'Enrollment, actuation, sign-in security, and the Test button.'],
];

export const notificationSeverityOptions = [
  ['info', 'Info and above'],
  ['warning', 'Warning and above'],
  ['critical', 'Critical only'],
];

export function defaultDestination(type = 'webhook') {
  return {
    id: '',
    name: type === 'telegram' ? 'Telegram' : type === 'mqtt' ? 'MQTT' : 'Webhook',
    type,
    enabled: true,
    minSeverity: 'warning',
    url: '',
    botToken: '',
    chatId: '',
    categories: [], // empty = all
    mqtt: { brokerUrl: '', topic: '', clientId: '', qos: 1, retain: false, username: '', password: '', caCert: '', clientCert: '', clientKey: '', insecureSkipVerify: false },
  };
}
