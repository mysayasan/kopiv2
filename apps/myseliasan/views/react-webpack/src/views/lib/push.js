// Browser-side plumbing for mobile push (W3-9).
//
// Everything here answers one of two questions: CAN this browser be woken, and IS it. They are
// separate on purpose — a browser that cannot subscribe and a browser that has not subscribed
// need completely different sentences on screen, and collapsing them into "notifications are
// off" is how somebody spends an afternoon on a setting that was never going to work.

// Why a browser cannot be enrolled. Each of these has its own remedy, so each gets its own
// answer rather than a shared "unsupported".
export const PUSH_UNSUPPORTED = 'unsupported'; // no service worker / no PushManager
export const PUSH_INSECURE = 'insecure'; // page is not a secure context
export const PUSH_BLOCKED = 'blocked'; // the user (or policy) denied notifications
export const PUSH_READY = 'ready'; // can be enrolled, and is not yet
export const PUSH_ENABLED = 'enabled'; // enrolled in this browser

// browserCapability reports what stands between this browser and a notification, WITHOUT
// asking for permission — reading the state must never produce a prompt, or opening the page
// becomes an interruption.
export function browserCapability() {
  if (typeof window === 'undefined') return PUSH_UNSUPPORTED;
  if (!('serviceWorker' in navigator) || !('PushManager' in window) || !('Notification' in window)) {
    return PUSH_UNSUPPORTED;
  }
  // A service worker only registers in a secure context. On an intranet install this is a
  // real, common case: the control plane reached over plain http, or over https with a
  // certificate the browser does not trust. Saying "your browser does not support this"
  // there would be false and would send somebody looking at the wrong thing entirely.
  if (!window.isSecureContext) return PUSH_INSECURE;
  if (Notification.permission === 'denied') return PUSH_BLOCKED;
  return PUSH_READY;
}

// registerWorker installs the service worker and waits for it to be usable. Registration
// resolving is not enough: pushManager lives on the ACTIVE registration, and asking too early
// is a race that shows up as an intermittent failure to subscribe.
export async function registerWorker() {
  const reg = await navigator.serviceWorker.register('/sw.js', { scope: '/' });
  await navigator.serviceWorker.ready;
  return reg;
}

// currentSubscription returns this browser's existing subscription, or null. Never prompts.
export async function currentSubscription() {
  if (browserCapability() === PUSH_UNSUPPORTED) return null;
  const reg = await navigator.serviceWorker.getRegistration('/');
  if (!reg) return null;
  return reg.pushManager.getSubscription();
}

// urlBase64ToUint8Array converts the server's VAPID public key into the raw bytes
// PushManager wants. The padding matters: the server sends base64url with the '=' stripped
// (that is what the spec puts on the wire), and atob rejects it without them.
function urlBase64ToUint8Array(base64String) {
  const padding = '='.repeat((4 - (base64String.length % 4)) % 4);
  const base64 = (base64String + padding).replace(/-/g, '+').replace(/_/g, '/');
  const raw = window.atob(base64);
  const out = new Uint8Array(raw.length);
  for (let i = 0; i < raw.length; i += 1) out[i] = raw.charCodeAt(i);
  return out;
}

function bufferToBase64Url(buffer) {
  const bytes = new Uint8Array(buffer);
  let binary = '';
  for (let i = 0; i < bytes.length; i += 1) binary += String.fromCharCode(bytes[i]);
  return window.btoa(binary).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/, '');
}

// subscriptionPayload flattens a PushSubscription into what the API stores.
export function subscriptionPayload(sub) {
  if (!sub) return null;
  return {
    endpoint: sub.endpoint,
    p256dh: bufferToBase64Url(sub.getKey('p256dh')),
    auth: bufferToBase64Url(sub.getKey('auth')),
  };
}

// enrol asks for permission (if it has not been granted) and subscribes this browser.
//
// Returns { ok, reason?, payload? }. It NEVER throws for the ordinary refusals — a person
// declining the browser prompt is not an error, it is an answer, and the screen has a
// sentence for it.
export async function enrol(publicKey) {
  const state = browserCapability();
  if (state === PUSH_UNSUPPORTED || state === PUSH_INSECURE || state === PUSH_BLOCKED) {
    return { ok: false, reason: state };
  }
  if (!publicKey) return { ok: false, reason: 'notConfigured' };

  const permission = Notification.permission === 'granted'
    ? 'granted'
    : await Notification.requestPermission();
  if (permission !== 'granted') return { ok: false, reason: PUSH_BLOCKED };

  const reg = await registerWorker();
  let sub = await reg.pushManager.getSubscription();
  // A subscription bound to a DIFFERENT application server key cannot receive anything we
  // send, and it fails silently — the browser accepts the message and drops it. So an
  // existing subscription is only reused when its key matches; otherwise it is torn down.
  if (sub) {
    const bound = sub.options && sub.options.applicationServerKey
      ? bufferToBase64Url(sub.options.applicationServerKey)
      : '';
    if (bound && bound !== publicKey) {
      await sub.unsubscribe().catch(() => {});
      sub = null;
    }
  }
  if (!sub) {
    sub = await reg.pushManager.subscribe({
      // Required by every browser that implements push: we promise every message produces
      // something the person can see. The service worker keeps that promise.
      userVisibleOnly: true,
      applicationServerKey: urlBase64ToUint8Array(publicKey),
    });
  }
  return { ok: true, payload: subscriptionPayload(sub) };
}

// forget tears down this browser's subscription. Best-effort by design: the server row is the
// thing that matters, and a browser that cannot unsubscribe locally must not block the
// operator from removing the device from the control plane.
export async function forget() {
  try {
    const sub = await currentSubscription();
    if (sub) await sub.unsubscribe();
  } catch (_) { /* the row is what counts */ }
}
