// The plan workspace's URL route — the app's ONLY route.
//
// Every other screen in myseliasan is a state branch in App.js, remembered per tab in
// sessionStorage (@shared/stickyTab). That works because the operator navigates with the side-nav
// and never needs an address. The plan editor is the exception: it opens in a browser tab of its
// own, and a tab starts from a URL and nothing else.
//
// So this is deliberately ONE route, not a router. Adding a second screen here should be a
// decision, not a reflex — the moment there are three, this file should become a real router
// instead of growing a switch.
//
//   /plan/{siteId}                       the site's plan workspace, on its first area
//   /plan/{siteId}?area={floorId}        …on a specific area
//   /plan/{siteId}?area=3&pick=n1::7     …arriving already carrying camera 7 of node n1
//
// Keyed on the SITE, not the floor, because the workspace is a site editor with an area bar: the
// header names the site, the tabs are its areas. A site with no areas yet still has a URL, which a
// floor-keyed route could not express.
//
// The server needs no route of its own: apphost's spaHandler already serves index.html for any
// unmatched path, and webpack's publicPath is '/' so a deep path still resolves the hashed bundle
// chunks. A relative publicPath would serve index.html AS JavaScript here — a blank page and a
// syntax error — which is the usual way this breaks.

export const PLAN_PREFIX = '/plan/';

// planHref builds the address for a site's workspace. Used as a real href so ctrl-click,
// middle-click, "open in new window" and "copy link address" all behave — a JS window.open()
// gives up every one of those.
export function planHref(siteId, opts = {}) {
  const q = new URLSearchParams();
  if (opts.floorId) q.set('area', String(opts.floorId));
  if (opts.pick && opts.pick.nodeId) {
    q.set('pick', `${opts.pick.nodeId}::${opts.pick.cameraId || ''}`);
    // The display name rides along so the "placing" banner can name what you are carrying on the
    // very first frame. Without it the banner shows a raw camera id until the palette has fetched
    // that node's camera list over the tunnel, which can take a while on a slow link.
    if (opts.pick.name) q.set('name', opts.pick.name);
  }
  const s = q.toString();
  return `${PLAN_PREFIX}${siteId}${s ? `?${s}` : ''}`;
}

// parsePlanRoute reads the current address, or null when this is not the plan route.
// Returns { siteId, floorId, pick } with floorId/pick null when absent.
export function parsePlanRoute(loc) {
  const l = loc || (typeof window !== 'undefined' ? window.location : null);
  if (!l) return null;
  const m = /^\/plan\/(\d+)\/?$/.exec(l.pathname || '');
  if (!m) return null;
  const q = new URLSearchParams(l.search || '');
  const area = Number(q.get('area'));
  const raw = q.get('pick') || '';
  let pick = null;
  if (raw) {
    const i = raw.indexOf('::');
    const nodeId = i >= 0 ? raw.slice(0, i) : raw;
    const cameraId = i >= 0 ? raw.slice(i + 2) : '';
    if (nodeId) pick = { nodeId, cameraId, name: q.get('name') || cameraId || nodeId };
  }
  return { siteId: Number(m[1]), floorId: Number.isFinite(area) && area > 0 ? area : null, pick };
}

// ---- back to the app ---------------------------------------------------------------------------
//
// The workspace opens in a tab of its own, and BOTH ways in pass `noopener` — window.open below,
// and the inspector's <a target="_blank" rel="noopener noreferrer">. noopener means the new tab
// gets a FRESH sessionStorage rather than a copy of the opener's, so the plan tab remembers no
// section at all. A plain href="/" back link therefore landed on the app's default section — the
// dashboard — and never on the fleet map the operator had just come from.
//
// So the way back has to SAY where it is going. The section rides as a one-shot query hint that
// App.js consumes and then strips from the address, which keeps the promise stickyTab.js makes
// (sections are not bookmarkable URLs) while still letting one screen hand another a destination.
export const TAB_PARAM = 'tab';

// appHref addresses the main app, naming the section to open on and — for the map — which place
// and area to open ON. "Back" from the plan editor means "the floor I was just editing", not "the
// map, somewhere": returning to an unselected world map makes the operator re-find their own work.
//
//   appHref('map')                              the fleet map, wherever it last was
//   appHref('map', { siteId: 4 })               that place, on its first area
//   appHref('map', { siteId: 4, floorId: 9 })   that place, on that area's floor view
export function appHref(tab, opts = {}) {
  if (!tab) return '/';
  const q = new URLSearchParams();
  q.set(TAB_PARAM, tab);
  // Same names the plan route uses for the same two things, so one address vocabulary covers
  // both screens and `?area=` never means one thing here and another there.
  if (opts.siteId) q.set('site', String(opts.siteId));
  if (opts.siteId && opts.floorId) q.set('area', String(opts.floorId));
  return `/?${q.toString()}`;
}

// parseAppTab reads what the address is asking the app to open: { tab, siteId, floorId }, with
// tab '' when nothing is asked for and the ids null unless they are usable numbers. The caller
// decides whether the section name is one it can render — this only reads the address.
export function parseAppTab(loc) {
  const l = loc || (typeof window !== 'undefined' ? window.location : null);
  const none = { tab: '', siteId: null, floorId: null };
  if (!l) return none;
  try {
    const q = new URLSearchParams(l.search || '');
    const tab = q.get(TAB_PARAM) || '';
    if (!tab) return none;
    const num = (k) => { const n = Number(q.get(k)); return Number.isFinite(n) && n > 0 ? n : null; };
    const siteId = num('site');
    // An area without a place is meaningless — a floor id says nothing about which building it is
    // in — so it is dropped rather than passed on as half an instruction.
    return { tab, siteId, floorId: siteId ? num('area') : null };
  } catch (_) {
    return none;
  }
}

// openPlanTab is the fallback for the flows where there is no element to hang an href on — the
// tail of "add a building" (the map's own click handler continues into the editor) and a finished
// drag-and-drop. Anywhere the operator clicks a visible control, use a real <a> instead.
export function openPlanTab(siteId, opts = {}) {
  try { window.open(planHref(siteId, opts), '_blank', 'noopener'); } catch (_) { /* popup blocked */ }
}

// setPlanArea rewrites ?area= as the operator switches area tabs, so the address bar always
// describes what is on screen and the link is worth copying. replaceState, not pushState: flicking
// through five areas should not bury the map five entries deep in the back button.
export function setPlanArea(siteId, floorId) {
  try {
    if (!window.history || !window.history.replaceState) return;
    window.history.replaceState(null, '', planHref(siteId, { floorId }));
  } catch (_) { /* history is unavailable in some embedded contexts */ }
}

// ---- cross-tab notification -----------------------------------------------------------------
//
// The workspace and the fleet map now live in different tabs, so the map can no longer be told
// about an edit by a React callback. BroadcastChannel is a same-origin browser API — no server
// round trip, nothing leaves the machine, so it holds under the intranet/air-gap rule.
//
// Not every browser we support has it, and it throws in some embedded contexts, so every use is
// guarded and the map ALSO refetches when its tab regains visibility. That fallback is what makes
// this an optimisation rather than a dependency.
const CHANNEL = 'myseliasan_plan_edits';

export function publishPlanEdit(siteId) {
  try {
    if (typeof BroadcastChannel === 'undefined') return;
    const ch = new BroadcastChannel(CHANNEL);
    ch.postMessage({ type: 'plan-edited', siteId, at: Date.now() });
    ch.close();
  } catch (_) { /* the visibilitychange refetch covers us */ }
}

// subscribePlanEdits calls onEdit() whenever a workspace tab reports a change. Returns an
// unsubscribe function; safe to call when BroadcastChannel is missing (it just never fires).
export function subscribePlanEdits(onEdit) {
  let ch = null;
  try {
    if (typeof BroadcastChannel === 'undefined') return () => {};
    ch = new BroadcastChannel(CHANNEL);
    ch.onmessage = (e) => { if (e && e.data && e.data.type === 'plan-edited') onEdit(e.data.siteId); };
  } catch (_) { return () => {}; }
  return () => { try { ch.close(); } catch (_) { /* already gone */ } };
}
