// A tiny persisted store for myseliasan's own "Live Views" wall. Unlike a single node,
// the control plane's wall can hold cameras from ANY node, so each tile is keyed by
// (nodeId, cameraId). Backed by localStorage with a pub/sub so the wall, the nav count,
// and the per-camera "Add/Remove" button all stay in sync.
const KEY = 'myseliasan_live_views';
const listeners = new Set();

function read() {
  try {
    const raw = localStorage.getItem(KEY);
    const arr = raw ? JSON.parse(raw) : [];
    return Array.isArray(arr) ? arr : [];
  } catch (_) { return []; }
}

function write(list) {
  try { localStorage.setItem(KEY, JSON.stringify(list)); } catch (_) { /* quota/private mode */ }
  listeners.forEach((fn) => { try { fn(list); } catch (_) {} });
}

const tileKey = (nodeId, cameraId) => `${nodeId}::${cameraId}`;

export function getLiveViews() { return read(); }

export function isInLiveViews(nodeId, cameraId) {
  const k = tileKey(nodeId, cameraId);
  return read().some((t) => tileKey(t.nodeId, t.cameraId) === k);
}

export function addLiveView(entry) {
  const k = tileKey(entry.nodeId, entry.cameraId);
  const list = read().filter((t) => tileKey(t.nodeId, t.cameraId) !== k);
  list.push({ nodeId: entry.nodeId, cameraId: entry.cameraId, name: entry.name || '', nodeName: entry.nodeName || '' });
  write(list);
}

export function removeLiveView(nodeId, cameraId) {
  const k = tileKey(nodeId, cameraId);
  write(read().filter((t) => tileKey(t.nodeId, t.cameraId) !== k));
}

export function subscribeLiveViews(fn) {
  listeners.add(fn);
  return () => listeners.delete(fn);
}
