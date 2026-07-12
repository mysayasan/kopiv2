// A tiny persisted store for myseliasan's own "Live Views" wall. Unlike a single node,
// the control plane's wall can hold cameras from ANY node, so each tile is keyed by
// (nodeId, cameraId). It mirrors mymatasan's Live Views: besides the ordered tile list
// it also persists the chosen grid layout. Backed by localStorage with a pub/sub so the
// wall, the nav count, and the per-camera "Add/Remove" button all stay in sync.
const KEY = 'myseliasan_live_views';
const DEFAULT_LAYOUT = '2x2';
const listeners = new Set();

// read returns the normalized { layout, tiles } state. It also migrates the legacy shape
// (a bare tiles array) that earlier builds stored, so upgrading never loses a wall.
function read() {
  try {
    const raw = localStorage.getItem(KEY);
    const parsed = raw ? JSON.parse(raw) : null;
    if (Array.isArray(parsed)) return { layout: DEFAULT_LAYOUT, tiles: parsed };
    if (parsed && typeof parsed === 'object') {
      return {
        layout: typeof parsed.layout === 'string' ? parsed.layout : DEFAULT_LAYOUT,
        tiles: Array.isArray(parsed.tiles) ? parsed.tiles : [],
      };
    }
  } catch (_) { /* corrupt/quota */ }
  return { layout: DEFAULT_LAYOUT, tiles: [] };
}

function write(state) {
  try { localStorage.setItem(KEY, JSON.stringify(state)); } catch (_) { /* quota/private mode */ }
  listeners.forEach((fn) => { try { fn(state); } catch (_) {} });
}

const tileKey = (nodeId, cameraId) => `${nodeId}::${cameraId}`;

export function getLiveViewsState() { return read(); }

// getLiveViews returns just the ordered tiles (kept for callers that only need the list,
// e.g. the nav count and the per-camera Add/Remove button).
export function getLiveViews() { return read().tiles; }

export function getLiveViewLayout() { return read().layout; }

export function setLiveViewLayout(layout) {
  const state = read();
  write({ ...state, layout: typeof layout === 'string' ? layout : DEFAULT_LAYOUT });
}

export function isInLiveViews(nodeId, cameraId) {
  const k = tileKey(nodeId, cameraId);
  return read().tiles.some((t) => tileKey(t.nodeId, t.cameraId) === k);
}

export function addLiveView(entry) {
  const k = tileKey(entry.nodeId, entry.cameraId);
  const state = read();
  const tiles = state.tiles.filter((t) => tileKey(t.nodeId, t.cameraId) !== k);
  tiles.push({
    nodeId: entry.nodeId,
    cameraId: entry.cameraId,
    name: entry.name || '',
    nodeName: entry.nodeName || '',
    ptzSupported: !!entry.ptzSupported,
  });
  write({ ...state, tiles });
}

export function removeLiveView(nodeId, cameraId) {
  const k = tileKey(nodeId, cameraId);
  const state = read();
  write({ ...state, tiles: state.tiles.filter((t) => tileKey(t.nodeId, t.cameraId) !== k) });
}

// moveLiveView reorders the flat tile list, moving the tile at `from` to sit at `to`.
// Indices are absolute positions in the full (un-paginated) list, matching mymatasan's
// drag-to-reorder on its Live Views grid.
export function moveLiveView(from, to) {
  const state = read();
  const tiles = [...state.tiles];
  if (from < 0 || from >= tiles.length || to < 0 || to >= tiles.length || from === to) return;
  const [moved] = tiles.splice(from, 1);
  tiles.splice(to, 0, moved);
  write({ ...state, tiles });
}

export function subscribeLiveViews(fn) {
  listeners.add(fn);
  return () => listeners.delete(fn);
}
