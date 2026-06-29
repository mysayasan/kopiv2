import { useEffect, useRef, useState } from 'react';
import { Ico } from './icons';
import { FormBusyOverlay } from './ui';
import { api, apiBase, formatTimestamp } from '../lib/helpers';

const TABS = [
  { id: 'cameras', label: 'Cameras', icon: 'camera' },
  { id: 'events', label: 'Events', icon: 'bell' },
  { id: 'access', label: 'Access', icon: 'shield' },
  { id: 'remote', label: 'Remote', icon: 'send' },
];

// NodeManager is the per-node commander surface: a live event feed, per-role access
// management, and a remote console that drives the node's own API over the control
// tunnel (the node enforces its own authorization on every proxied request).
export function NodeManager({ node, onToast, onBack }) {
  const [tab, setTab] = useState('cameras');
  return (
    <section className="workspace">
      <section className="settings-panel span-two">
        <header>
          <h2><span className="btn-icon"><Ico n="monitor" /> {node.name || node.nodeId}</span></h2>
          <div className="settings-header-actions">
            {node.baseUrl ? (
              <a className="quiet btn-link" href={node.baseUrl} target="_blank" rel="noreferrer" title="Open this node's own UI (full-motion video, all screens)">
                <span className="btn-icon"><Ico n="login" /> Open node UI</span>
              </a>
            ) : null}
            <button type="button" className="quiet" onClick={onBack}>
              <span className="btn-icon"><Ico n="arr-left" /> Back to nodes</span>
            </button>
          </div>
        </header>
        <p className="settings-hint">
          {node.baseUrl} · <span className={`status-pill ${node.status === 'online' ? 'online' : 'offline'}`}>{node.status || 'online'}</span>
        </p>
        <div className="node-manage-tabs">
          {TABS.map((t) => (
            <button key={t.id} type="button" className={`quiet${tab === t.id ? ' active' : ''}`} onClick={() => setTab(t.id)}>
              <span className="btn-icon"><Ico n={t.icon} /> {t.label}</span>
            </button>
          ))}
        </div>
      </section>

      {tab === 'cameras' ? <NodeCameras node={node} onToast={onToast} /> : null}
      {tab === 'events' ? <NodeEvents node={node} /> : null}
      {tab === 'access' ? <NodeAccess node={node} onToast={onToast} /> : null}
      {tab === 'remote' ? <NodeRemote node={node} onToast={onToast} /> : null}
    </section>
  );
}

// NodeCameras shows full-frame-rate live view of the node's cameras over WebRTC: the
// node relays each camera's RTP up its media channel and myseliasan re-broadcasts it
// to the browser (the browser peers only with myseliasan). If WebRTC can't establish,
// each tile falls back to the low-bandwidth snapshot poll over the command tunnel.
function NodeCameras({ node, onToast }) {
  const [cams, setCams] = useState([]);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState('');
  const [iceServers, setIceServers] = useState([]);

  async function load() {
    setLoading(true);
    const r = await api(`/api/nodes/${encodeURIComponent(node.nodeId)}/proxy/api/cameras?limit=100`, { noRedirect: true })
      .catch(() => ({ ok: false }));
    setLoading(false);
    if (r.status === 403) { setError('No access to this node.'); setCams([]); return; }
    if (r.ok) { setError(''); setCams(Array.isArray(r.body) ? r.body : (r.body?.items || [])); }
    else { setError(r.message || 'Failed to load cameras.'); if (onToast) onToast(r.message || 'Failed to load cameras.'); }
  }
  useEffect(() => { load(); /* eslint-disable-next-line */ }, [node.nodeId]);

  // ICE config (STUN/TURN) for cross-network browser↔parent peering; empty on same LAN.
  useEffect(() => {
    api('/api/node-stream/config', { noRedirect: true })
      .then((r) => { if (r.ok && Array.isArray(r.body?.iceServers)) setIceServers(r.body.iceServers); })
      .catch(() => {});
  }, []);

  return (
    <section className="settings-panel span-two">
      <header>
        <h2><span className="btn-icon"><Ico n="camera" /> Cameras</span></h2>
        <div className="settings-header-actions">
          <button type="button" className="quiet" onClick={load} disabled={loading}>
            <span className="btn-icon"><Ico n="reload" /> Refresh</span>
          </button>
        </div>
      </header>
      <p className="settings-hint">
        Full-motion live view relayed over the node&apos;s secure media channel and re-broadcast via WebRTC. Tiles that
        can&apos;t establish WebRTC fall back to ~1.5s snapshots automatically.
      </p>
      {error ? <p className="settings-hint danger-text">{error}</p> : null}
      {cams.length === 0 && !error ? (
        <p className="settings-hint">No cameras configured on this node.</p>
      ) : (
        <div className="node-cam-grid">
          {cams.map((c) => (
            <NodeCameraTile key={c.id} nodeId={node.nodeId} cam={c} iceServers={iceServers} />
          ))}
        </div>
      )}
    </section>
  );
}

// NodeCameraTile renders one camera: it negotiates a WebRTC stream against myseliasan
// (which relays the node's RTP) and shows it in a <video>; on any failure it switches
// to snapshot polling over the command tunnel so the tile always shows something.
function NodeCameraTile({ nodeId, cam, iceServers }) {
  const videoRef = useRef(null);
  const [mode, setMode] = useState('connecting'); // connecting | live | snapshot
  const [tick, setTick] = useState(Date.now());

  useEffect(() => {
    let cancelled = false;
    let pc = null;
    if (typeof RTCPeerConnection === 'undefined') { setMode('snapshot'); return () => {}; }

    const fallback = () => { if (!cancelled) setMode('snapshot'); };

    (async () => {
      try {
        pc = new RTCPeerConnection({ iceServers: iceServers || [] });
        pc.addTransceiver('video', { direction: 'recvonly' });
        pc.addTransceiver('audio', { direction: 'recvonly' });
        pc.ontrack = (event) => {
          if (cancelled || !videoRef.current) return;
          const stream = videoRef.current.srcObject instanceof MediaStream
            ? videoRef.current.srcObject : new MediaStream();
          stream.addTrack(event.track);
          videoRef.current.srcObject = stream;
          videoRef.current.play().catch(() => {});
          if (event.track.kind === 'video') setMode('live');
        };
        pc.onconnectionstatechange = () => {
          if (cancelled) return;
          if (pc.connectionState === 'failed' || pc.connectionState === 'closed') fallback();
        };
        const offer = await pc.createOffer();
        await pc.setLocalDescription(offer);
        await waitForIceGathering(pc);
        const r = await api(`/api/nodes/${encodeURIComponent(nodeId)}/cameras/${cam.id}/webrtc/offer`, {
          method: 'POST', noRedirect: true,
          body: JSON.stringify({ type: pc.localDescription.type, sdp: pc.localDescription.sdp }),
        }).catch(() => ({ ok: false }));
        if (cancelled) return;
        if (!r.ok || !r.body || !r.body.sdp) { fallback(); return; }
        await pc.setRemoteDescription({ type: r.body.type || 'answer', sdp: r.body.sdp });
      } catch (_) {
        fallback();
      }
    })();

    return () => {
      cancelled = true;
      try {
        if (videoRef.current && videoRef.current.srcObject) {
          videoRef.current.srcObject.getTracks().forEach((t) => t.stop());
          videoRef.current.srcObject = null;
        }
      } catch (_) { /* ignore */ }
      if (pc) pc.close();
    };
    // eslint-disable-next-line
  }, [nodeId, cam.id]);

  // Snapshot fallback: refresh the still frame on an interval while in snapshot mode.
  useEffect(() => {
    if (mode !== 'snapshot') return undefined;
    const iv = setInterval(() => setTick(Date.now()), 1500);
    return () => clearInterval(iv);
  }, [mode]);

  const label = cam.name || `Camera ${cam.id}`;
  const badge = mode === 'live' ? 'live' : mode === 'snapshot' ? 'snapshot' : 'connecting…';
  const snapUrl = `${apiBase()}/api/nodes/${encodeURIComponent(nodeId)}/proxy/api/vision/cameras/${cam.id}/frame?t=${tick}`;

  return (
    <figure className="node-cam-card">
      {mode === 'snapshot' ? (
        <img
          className="node-cam-img"
          src={snapUrl}
          alt={label}
          onError={(e) => { e.currentTarget.classList.add('node-cam-img--err'); }}
          onLoad={(e) => { e.currentTarget.classList.remove('node-cam-img--err'); }}
        />
      ) : (
        <video ref={videoRef} className="node-cam-img" autoPlay playsInline muted />
      )}
      <figcaption className="node-cam-cap">{label} · {badge}</figcaption>
    </figure>
  );
}

// waitForIceGathering resolves when the peer has gathered all ICE candidates (the
// answer/offer is then complete and self-contained), or after a short cap so a slow
// network doesn't stall the tile forever.
function waitForIceGathering(pc) {
  return new Promise((resolve) => {
    if (pc.iceGatheringState === 'complete') { resolve(); return; }
    const done = () => {
      if (pc.iceGatheringState === 'complete') {
        pc.removeEventListener('icegatheringstatechange', done);
        resolve();
      }
    };
    pc.addEventListener('icegatheringstatechange', done);
    setTimeout(resolve, 3000);
  });
}

// NodeEvents shows the node's slice of the control plane's unified feed (history via
// the list endpoint, live via the SSE stream filtered to this node).
function NodeEvents({ node }) {
  const [items, setItems] = useState([]);
  const [loading, setLoading] = useState(false);

  async function load() {
    setLoading(true);
    const r = await api(`/api/notifications?nodeId=${encodeURIComponent(node.nodeId)}&limit=100`).catch(() => ({ ok: false }));
    setLoading(false);
    if (r.ok && r.body) setItems(Array.isArray(r.body.items) ? r.body.items : []);
  }

  useEffect(() => {
    load();
    let es;
    try {
      es = new EventSource(`${apiBase()}/api/notifications/stream`, { withCredentials: true });
      es.addEventListener('notification', (ev) => {
        try {
          const n = JSON.parse(ev.data);
          const nid = n && n.data && n.data.nodeId;
          if (nid === node.nodeId) setItems((cur) => [n, ...cur].slice(0, 100));
        } catch (_) { /* ignore malformed frame */ }
      });
    } catch (_) { /* SSE unavailable — manual refresh still works */ }
    return () => { if (es) es.close(); };
    // eslint-disable-next-line
  }, [node.nodeId]);

  return (
    <section className="settings-panel span-two">
      <header>
        <h2><span className="btn-icon"><Ico n="bell" /> Events</span></h2>
        <div className="settings-header-actions">
          <button type="button" className="quiet" onClick={load} disabled={loading}>
            <span className="btn-icon"><Ico n="reload" /> Refresh</span>
          </button>
        </div>
      </header>
      <p className="settings-hint">Alerts, health checks, and system events this node pushed to the control plane (live).</p>
      {items.length === 0 ? (
        <p className="settings-hint">No events from this node yet.</p>
      ) : (
        <table className="event-table">
          <thead><tr><th>Time</th><th>Severity</th><th>Category</th><th>Event</th></tr></thead>
          <tbody>
            {items.map((n, i) => (
              <tr key={n.id || i}>
                <td>{formatTimestamp(n.createdAt)}</td>
                <td><span className={`status-pill ${severityClass(n.severity)}`}>{n.severity || 'info'}</span></td>
                <td>{n.category || '—'}</td>
                <td><strong>{n.title}</strong>{n.body ? <><br /><span className="settings-hint">{n.body}</span></> : null}</td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </section>
  );
}

function severityClass(sev) {
  if (sev === 'critical') return 'offline';
  if (sev === 'warning') return 'warn';
  return 'online';
}

// NodeAccess manages the per-(role, node) read/write grants. Only the node's owning
// role may view/change them; the API returns 403 otherwise, surfaced here in place.
function NodeAccess({ node, onToast }) {
  const [grants, setGrants] = useState([]);
  const [denied, setDenied] = useState(false);
  const [busy, setBusy] = useState(false);
  const [form, setForm] = useState({ roleId: '', canRead: true, canWrite: false });

  function toast(t) { if (onToast) onToast(t); }

  async function load() {
    const r = await api(`/api/nodes/access?nodeId=${encodeURIComponent(node.nodeId)}`, { noRedirect: true }).catch(() => ({ ok: false }));
    if (r.status === 403) { setDenied(true); setGrants([]); return; }
    setDenied(false);
    if (r.ok) setGrants(Array.isArray(r.body) ? r.body : []);
  }
  useEffect(() => { load(); /* eslint-disable-next-line */ }, [node.nodeId]);

  async function save() {
    const roleId = parseInt(form.roleId, 10);
    if (!roleId || roleId <= 0) { toast('Enter a valid role id.'); return; }
    setBusy(true);
    const r = await api('/api/nodes/access', {
      method: 'POST',
      noRedirect: true,
      body: JSON.stringify({ roleId, nodeId: node.nodeId, canRead: form.canRead, canWrite: form.canWrite }),
    });
    setBusy(false);
    if (r.status === 403) { toast('Only the node owner can manage access.'); return; }
    if (r.ok) { toast('Access grant saved.'); setForm({ roleId: '', canRead: true, canWrite: false }); load(); }
    else toast(r.message || 'Failed to save grant.');
  }

  async function remove(grant) {
    if (!window.confirm(`Remove access for role ${grant.roleId}?`)) return;
    setBusy(true);
    const r = await api(`/api/nodes/access/${grant.id}`, { method: 'DELETE', noRedirect: true });
    setBusy(false);
    if (r.status === 403) { toast('Only the node owner can manage access.'); return; }
    if (r.ok) { toast('Grant removed.'); load(); }
    else toast(r.message || 'Failed to remove grant.');
  }

  if (denied) {
    return (
      <section className="settings-panel span-two">
        <header><h2><span className="btn-icon"><Ico n="shield" /> Access</span></h2></header>
        <p className="settings-hint danger-text">Only the role that adopted this node can manage its access grants.</p>
      </section>
    );
  }

  return (
    <section className="settings-panel span-two">
      <FormBusyOverlay busy={busy} />
      <header><h2><span className="btn-icon"><Ico n="shield" /> Access</span></h2></header>
      <p className="settings-hint">
        Grant other myseliasan roles access to this node. Read-only acts as a viewer; read+write acts as admin (write
        implies read). The owning role always has full access.
      </p>
      <div className="node-access-form">
        <label className="node-access-role">Role id
          <input value={form.roleId} onChange={(e) => setForm({ ...form, roleId: e.target.value })} placeholder="e.g. 3" disabled={busy} />
        </label>
        <div className="node-access-checks">
          <label className="node-access-check">
            <input type="checkbox" checked={form.canRead} onChange={(e) => setForm({ ...form, canRead: e.target.checked, canWrite: e.target.checked ? form.canWrite : false })} disabled={busy} /> Read
          </label>
          <label className="node-access-check">
            <input type="checkbox" checked={form.canWrite} onChange={(e) => setForm({ ...form, canWrite: e.target.checked, canRead: e.target.checked ? true : form.canRead })} disabled={busy} /> Write
          </label>
        </div>
        <button type="button" className="node-access-save" onClick={save} disabled={busy}>
          <span className="btn-icon"><Ico n="save" /> Save grant</span>
        </button>
      </div>
      {grants.length === 0 ? (
        <p className="settings-hint">No extra grants — only the owning role can access this node.</p>
      ) : (
        <table className="event-table">
          <thead><tr><th>Role id</th><th>Access</th><th></th></tr></thead>
          <tbody>
            {grants.map((g) => (
              <tr key={g.id}>
                <td>{g.roleId}</td>
                <td>{g.canWrite ? 'Read + Write (admin)' : g.canRead ? 'Read (viewer)' : '—'}</td>
                <td><button type="button" className="quiet danger-text" onClick={() => remove(g)} disabled={busy}>Remove</button></td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </section>
  );
}

const QUICK = [
  { label: 'Version', method: 'GET', path: '/api/version' },
  { label: 'Runtime settings', method: 'GET', path: '/api/settings/runtime' },
  { label: 'AI rules', method: 'GET', path: '/api/vision/rules' },
  { label: 'Notifications', method: 'GET', path: '/api/notifications' },
];

// NodeRemote drives the node's own API over the tunnel. The node authorizes each
// request as if it were local (the operator's grant decides viewer vs admin), so a
// read-only operator's writes come back 403 here.
function NodeRemote({ node, onToast }) {
  const [method, setMethod] = useState('GET');
  const [path, setPath] = useState('/api/settings/runtime');
  const [body, setBody] = useState('');
  const [resp, setResp] = useState(null);
  const [busy, setBusy] = useState(false);
  const respRef = useRef(null);

  async function send() {
    const p = path.trim();
    if (!p.startsWith('/')) { if (onToast) onToast('Path must start with /'); return; }
    setBusy(true);
    const opts = { method, noRedirect: true };
    if (method !== 'GET' && body.trim()) opts.body = body;
    const r = await api(`/api/nodes/${encodeURIComponent(node.nodeId)}/proxy${p}`, opts).catch((e) => ({ ok: false, message: String(e) }));
    setBusy(false);
    setResp(r);
    if (respRef.current) respRef.current.scrollIntoView({ behavior: 'smooth', block: 'nearest' });
  }

  return (
    <section className="settings-panel span-two">
      <FormBusyOverlay busy={busy} />
      <header><h2><span className="btn-icon"><Ico n="send" /> Remote console</span></h2></header>
      <p className="settings-hint">
        Call this node&apos;s API over the secure tunnel. The node enforces its own authorization — read-only access
        rejects writes. Streaming endpoints (live video, SSE) are not tunnelable.
      </p>
      <div className="node-remote-quick">
        {QUICK.map((q) => (
          <button key={q.label} type="button" className="quiet" onClick={() => { setMethod(q.method); setPath(q.path); setBody(''); }}>
            {q.label}
          </button>
        ))}
      </div>
      <div className="node-remote-bar">
        <select value={method} onChange={(e) => setMethod(e.target.value)} disabled={busy}>
          {['GET', 'POST', 'PUT', 'DELETE'].map((m) => <option key={m} value={m}>{m}</option>)}
        </select>
        <input value={path} onChange={(e) => setPath(e.target.value)} placeholder="/api/settings/runtime" disabled={busy} />
        <button type="button" onClick={send} disabled={busy}><span className="btn-icon"><Ico n="send" /> Send</span></button>
      </div>
      {method !== 'GET' ? (
        <label className="node-remote-body">Request body (JSON)
          <textarea value={body} onChange={(e) => setBody(e.target.value)} rows={5} placeholder='{ "key": "value" }' disabled={busy} />
        </label>
      ) : null}
      {resp ? (
        <div className="node-remote-resp" ref={respRef}>
          <div className={`status-pill ${resp.ok ? 'online' : 'offline'}`}>HTTP {resp.status || '—'}</div>
          <pre className="node-remote-pre">{formatResp(resp)}</pre>
        </div>
      ) : null}
    </section>
  );
}

function formatResp(r) {
  if (r.body !== undefined && r.body !== null) {
    try { return JSON.stringify(r.body, null, 2); } catch (_) { return String(r.body); }
  }
  return r.message || '(empty response)';
}
