/* eslint-env serviceworker */

// The myseliasan service worker (W3-9).
//
// It exists for ONE reason: a push message can only be shown by a service worker, and a push
// message is the only way to wake somebody who is not looking at a screen. That is the whole
// job. Everything it does not do is deliberate:
//
// NO OFFLINE CACHE. Not one byte. A fleet control plane that served yesterday's node list from
// a cache would be showing an operator a green estate that went dark an hour ago, and they
// would have no way to tell. "The page will not load" is an honest failure; "everything looks
// fine" when it is not is the failure this whole hardening programme exists to prevent. The
// fetch handler below is intentionally empty — it registers so the browser counts this as an
// installable app, and it never calls respondWith, so every request goes to the network
// exactly as it would with no service worker at all.
//
// NO BACKGROUND SYNC, NO PERIODIC FETCH. Same reason, plus: on an intranet install this
// appliance is unreachable from anywhere but the site, so background traffic would be error
// noise on somebody's phone.

// The file is served from the site root, so this scope covers the whole SPA.

// Take over immediately rather than waiting for every tab to close. Somebody who has just
// turned notifications on and pressed "send a test" must get the test — not discover next
// week that it started working after they closed the last tab.
self.addEventListener('install', (event) => {
  event.waitUntil(self.skipWaiting());
});

self.addEventListener('activate', (event) => {
  event.waitUntil(self.clients.claim());
});

// Deliberately empty. See the note at the top of this file: this registers a fetch handler
// (which is part of what makes the app installable) without intercepting anything.
self.addEventListener('fetch', () => {});

const ICON = '/assets/icon-192.png';

// A notification with no data at all. Chrome delivers a push event even when the payload
// cannot be read (and other user agents send "no payload" pushes on purpose), and a silent
// drop would mean a woken phone that shows nothing — which reads exactly like the feature not
// working. Something honest and vague beats nothing.
const FALLBACK = {
  title: 'MySeliaSan',
  body: 'The fleet needs attention. Open the control plane to see what changed.',
  severity: 'warning',
};

function parsePayload(event) {
  if (!event.data) return FALLBACK;
  try {
    const raw = event.data.json();
    if (!raw || typeof raw !== 'object' || !raw.title) return FALLBACK;
    return raw;
  } catch (_) {
    return FALLBACK;
  }
}

self.addEventListener('push', (event) => {
  const payload = parsePayload(event);
  const severity = String(payload.severity || 'warning').toLowerCase();
  const critical = severity === 'critical';

  const options = {
    body: payload.body || '',
    icon: ICON,
    badge: ICON,
    // Group by SOURCE, so a node flapping produces one entry that re-buzzes rather than
    // forty stacked lines somebody has to swipe away one at a time before they can see the
    // rest of the estate. renotify keeps the buzz: a replaced notification that arrives
    // silently is a notification nobody learns about.
    tag: `myseliasan:${payload.source || payload.category || 'fleet'}`,
    renotify: true,
    // A critical alert stays on screen until it is acted on. This is the 3am case the whole
    // feature is for, and a banner that auto-dismisses after four seconds while the phone is
    // face-down on a table has told nobody anything.
    requireInteraction: critical,
    timestamp: Number(payload.at || 0) * 1000 || Date.now(),
    data: {
      severity,
      category: payload.category || '',
      source: payload.source || '',
      at: Number(payload.at || 0),
    },
  };

  event.waitUntil(self.registration.showNotification(payload.title || FALLBACK.title, options));
});

// Tapping the notification must land the operator IN the control plane. Focusing an already
// open tab (rather than opening a second one) matters on a phone, where a stack of duplicate
// tabs is how a person loses the one they had signed in on.
self.addEventListener('notificationclick', (event) => {
  event.notification.close();
  event.waitUntil((async () => {
    const clients = await self.clients.matchAll({ type: 'window', includeUncontrolled: true });
    for (const client of clients) {
      if (new URL(client.url).origin === self.location.origin) {
        await client.focus();
        return;
      }
    }
    await self.clients.openWindow('/');
  })());
});

// A browser may rotate a subscription on its own. This worker CANNOT re-register it: the API
// requires the double-submit CSRF token, which lives in a cookie the page reads and a worker
// cannot. So rotation is healed from the two ends instead, and both are load-bearing:
//
//   1. the SPA re-posts the current subscription on every load, which upserts by endpoint;
//   2. the old endpoint answers 410 Gone on the next delivery and the server deletes the row.
//
// The gap is real and worth naming: between a rotation and the next time somebody opens the
// app, that device is not reachable. It is recorded in the module docs rather than papered
// over here.
self.addEventListener('pushsubscriptionchange', () => {});
