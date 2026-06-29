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

// NodeCameras shows a live view of the node's cameras by polling single-frame JPEG
// snapshots over the command tunnel. Continuous streams (MJPEG/WebRTC) cannot be
// tunneled, so each tile refreshes a still frame on an interval — a low-bandwidth
// "live" view that works through the same secure channel as everything else.
function NodeCameras({ node, onToast }) {
  const [cams, setCams] = useState([]);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState('');
  const [tick, setTick] = useState(Date.now());
  const [paused, setPaused] = useState(false);

  async function load() {
    setLoading(true);
    const r = await api(`/api/nodes/${encodeURIComponent(node.nodeId)}/proxy/api/cameras?limit=100`, { noRedirect: true })
      .catch(() => ({ ok: false }));
    setLoading(false);
    if (r.status === 403) { setError('No access to this node.'); setCams([]); return; }
    if (r.ok) { setError(''); setCams(Array.isArray(r.body) ? r.body : []); }
    else { setError(r.message || 'Failed to load cameras.'); if (onToast) onToast(r.message || 'Failed to load cameras.'); }
  }
  useEffect(() => { load(); /* eslint-disable-next-line */ }, [node.nodeId]);

  // Refresh every frame tile on an interval (cache-busted), unless paused.
  useEffect(() => {
    if (paused) return undefined;
    const iv = setInterval(() => setTick(Date.now()), 1500);
    return () => clearInterval(iv);
  }, [paused]);

  function frameUrl(camId) {
    return `${apiBase()}/api/nodes/${encodeURIComponent(node.nodeId)}/proxy/api/vision/cameras/${camId}/frame?t=${tick}`;
  }

  return (
    <section className="settings-panel span-two">
      <header>
        <h2><span className="btn-icon"><Ico n="camera" /> Cameras</span></h2>
        <div className="settings-header-actions">
          <button type="button" className="quiet" onClick={() => setPaused((p) => !p)}>
            <span className="btn-icon"><Ico n={paused ? 'play' : 'stop'} /> {paused ? 'Resume' : 'Pause'}</span>
          </button>
          <button type="button" className="quiet" onClick={load} disabled={loading}>
            <span className="btn-icon"><Ico n="reload" /> Refresh</span>
          </button>
        </div>
      </header>
      <p className="settings-hint">
        Live snapshots streamed over the secure tunnel (a still frame refreshed every ~1.5s). Continuous video isn&apos;t
        tunneled — open the node directly for full-motion playback.
      </p>
      {error ? <p className="settings-hint danger-text">{error}</p> : null}
      {cams.length === 0 && !error ? (
        <p className="settings-hint">No cameras configured on this node.</p>
      ) : (
        <div className="node-cam-grid">
          {cams.map((c) => (
            <figure key={c.id} className="node-cam-card">
              <img
                className="node-cam-img"
                src={frameUrl(c.id)}
                alt={c.name || `Camera ${c.id}`}
                onError={(e) => { e.currentTarget.classList.add('node-cam-img--err'); }}
                onLoad={(e) => { e.currentTarget.classList.remove('node-cam-img--err'); }}
              />
              <figcaption className="node-cam-cap">{c.name || `Camera ${c.id}`}</figcaption>
            </figure>
          ))}
        </div>
      )}
    </section>
  );
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
      <div className="settings-field-grid">
        <label>Role id
          <input value={form.roleId} onChange={(e) => setForm({ ...form, roleId: e.target.value })} placeholder="e.g. 3" disabled={busy} />
        </label>
        <label className="node-access-check">
          <input type="checkbox" checked={form.canRead} onChange={(e) => setForm({ ...form, canRead: e.target.checked, canWrite: e.target.checked ? form.canWrite : false })} disabled={busy} /> Read
        </label>
        <label className="node-access-check">
          <input type="checkbox" checked={form.canWrite} onChange={(e) => setForm({ ...form, canWrite: e.target.checked, canRead: e.target.checked ? true : form.canRead })} disabled={busy} /> Write
        </label>
      </div>
      <div className="settings-actions">
        <button type="button" onClick={save} disabled={busy}><span className="btn-icon"><Ico n="save" /> Save grant</span></button>
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
