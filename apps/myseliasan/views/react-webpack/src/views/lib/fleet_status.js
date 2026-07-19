// Fleet status tone — the single mapping from a node's raw status onto the
// online / warning / critical / idle vocabulary the map pins and legends use. Shared by
// the geographic map and the floor-plan view so the two surfaces can never disagree about
// what colour a node is.
//
//   critical  the node is lost (unreachable past the heartbeat grace window)
//   warning   the node is online but its management certificate is expiring soon
//   online    the node is online and healthy
//   idle      self-dropped, or a legacy/unknown status — deliberately neutral, not alarming
//
// The cert-expiring tier reuses the SAME signal the backend already tracks (CertExpiresAt +
// the fleet-status "certs expiring" rollup); we surface it per-node here so a healthy-but-
// aging node reads amber before it ever goes critical.

// A node whose cert expires within this horizon reads as "warning". Chosen to surface a
// renewal problem with comfortable lead time; the backend's own warn window is shorter, so
// anything the backend flags is already amber here.
export const CERT_WARN_SECONDS = 7 * 24 * 3600;

export const TONES = {
  online: { key: 'online', color: '#22c55e', ring: '#16a34a' },
  warning: { key: 'warning', color: '#f59e0b', ring: '#d97706' },
  critical: { key: 'critical', color: '#ef4444', ring: '#dc2626' },
  idle: { key: 'idle', color: '#94a3b8', ring: '#64748b' },
};

// certExpiringSoon reports whether a node's issued cert is within the warning horizon.
export function certExpiringSoon(node, nowSeconds) {
  const exp = Number(node?.certExpiresAt || 0);
  if (exp <= 0) return false;
  const now = nowSeconds || Math.floor(Date.now() / 1000);
  return exp - now <= CERT_WARN_SECONDS;
}

// nodeToneKey returns 'online' | 'warning' | 'critical' | 'idle' for a node.
export function nodeToneKey(node, nowSeconds) {
  const s = (node?.status || '').toLowerCase();
  if (s === 'lost') return 'critical';
  if (s === 'self-dropped') return 'idle';
  if (s === 'online') return certExpiringSoon(node, nowSeconds) ? 'warning' : 'online';
  return 'idle';
}

// nodeTone returns the full tone descriptor (color/ring) for a node.
export function nodeTone(node, nowSeconds) {
  return TONES[nodeToneKey(node, nowSeconds)];
}
