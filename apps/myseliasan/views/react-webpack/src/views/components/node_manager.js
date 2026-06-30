import { useEffect, useRef, useState } from 'react';
import { Ico, useT } from '@shared';
import { FormBusyOverlay } from './ui';
import { api, apiBase, formatTimestamp } from '../lib/helpers';

// Tab ids drive both the icon and the localized label (nm.tab<Id>).
const TABS = [
  { id: 'cameras', icon: 'camera' },
  { id: 'events', icon: 'bell' },
  { id: 'remote', icon: 'send' },
];
const tabKey = (id) => `nm.tab${id[0].toUpperCase()}${id.slice(1)}`;

// NodeManager is the per-node commander surface: a live event feed, per-role access
// management, and a remote console that drives the node's own API over the control
// tunnel (the node enforces its own authorization on every proxied request).
export function NodeManager({ node, onToast, onBack }) {
  const t = useT();
  const [tab, setTab] = useState('cameras');
  return (
    <section className="workspace">
      <section className="settings-panel span-two">
        <header>
          <h2><span className="btn-icon"><Ico n="monitor" /> {node.name || node.nodeId}</span></h2>
          <div className="settings-header-actions">
            {node.baseUrl ? (
              <a className="quiet btn-link" href={node.baseUrl} target="_blank" rel="noreferrer" title={t('nm.openNodeUiTitle')}>
                <span className="btn-icon"><Ico n="login" /> {t('nm.openNodeUi')}</span>
              </a>
            ) : null}
            <button type="button" className="quiet" onClick={onBack}>
              <span className="btn-icon"><Ico n="arr-left" /> {t('nm.back')}</span>
            </button>
          </div>
        </header>
        <p className="settings-hint">
          {node.baseUrl} · <span className={`status-pill ${node.status === 'online' ? 'online' : 'offline'}`}>{node.status || 'online'}</span>
        </p>
        <div className="node-manage-tabs">
          {TABS.map((tb) => (
            <button key={tb.id} type="button" className={`quiet${tab === tb.id ? ' active' : ''}`} onClick={() => setTab(tb.id)}>
              <span className="btn-icon"><Ico n={tb.icon} /> {t(tabKey(tb.id))}</span>
            </button>
          ))}
        </div>
      </section>

      {tab === 'cameras' ? <NodeCameras node={node} onToast={onToast} /> : null}
      {tab === 'events' ? <NodeEvents node={node} /> : null}
      {tab === 'remote' ? <NodeRemote node={node} onToast={onToast} /> : null}
    </section>
  );
}

// NodeCameras shows full-frame-rate live view of the node's cameras over WebRTC: the
// node relays each camera's RTP up its media channel and myseliasan re-broadcasts it
// to the browser (the browser peers only with myseliasan). If WebRTC can't establish,
// each tile falls back to the low-bandwidth snapshot poll over the command tunnel.
function NodeCameras({ node, onToast }) {
  const t = useT();
  const [cams, setCams] = useState([]);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState('');
  const [iceServers, setIceServers] = useState([]);

  async function load() {
    setLoading(true);
    const r = await api(`/api/nodes/${encodeURIComponent(node.nodeId)}/proxy/api/cameras?limit=100`, { noRedirect: true })
      .catch(() => ({ ok: false }));
    setLoading(false);
    if (r.status === 403) { setError(t('nm.noAccess')); setCams([]); return; }
    if (r.ok) { setError(''); setCams(Array.isArray(r.body) ? r.body : (r.body?.items || [])); }
    else { setError(r.message || t('nm.failedCameras')); if (onToast) onToast(r.message || t('nm.failedCameras')); }
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
        <h2><span className="btn-icon"><Ico n="camera" /> {t('nm.tabCameras')}</span></h2>
        <div className="settings-header-actions">
          <button type="button" className="quiet" onClick={load} disabled={loading}>
            <span className="btn-icon"><Ico n="reload" /> {t('nm.refresh')}</span>
          </button>
        </div>
      </header>
      <p className="settings-hint">{t('nm.camerasHint')}</p>
      {error ? <p className="settings-hint danger-text">{error}</p> : null}
      {cams.length === 0 && !error ? (
        <p className="settings-hint">{t('nm.noCameras')}</p>
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
  const t = useT();
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
          videoRef.current.srcObject.getTracks().forEach((trk) => trk.stop());
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

  const label = cam.name || t('nm.cameraN', { id: cam.id });
  const badge = mode === 'live' ? t('nm.badgeLive') : mode === 'snapshot' ? t('nm.badgeSnapshot') : t('nm.badgeConnecting');
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
  const t = useT();
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
        <h2><span className="btn-icon"><Ico n="bell" /> {t('nm.tabEvents')}</span></h2>
        <div className="settings-header-actions">
          <button type="button" className="quiet" onClick={load} disabled={loading}>
            <span className="btn-icon"><Ico n="reload" /> {t('nm.refresh')}</span>
          </button>
        </div>
      </header>
      <p className="settings-hint">{t('nm.eventsHint')}</p>
      {items.length === 0 ? (
        <p className="settings-hint">{t('nm.noEvents')}</p>
      ) : (
        <table className="event-table">
          <thead><tr><th>{t('nm.colTime')}</th><th>{t('nm.colSeverity')}</th><th>{t('nm.colCategory')}</th><th>{t('nm.colEvent')}</th></tr></thead>
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

// Quick-action presets for the remote console; label localized via tkey.
const QUICK = [
  { tkey: 'nm.quickVersion', method: 'GET', path: '/api/version' },
  { tkey: 'nm.quickRuntime', method: 'GET', path: '/api/settings/runtime' },
  { tkey: 'nm.quickAiRules', method: 'GET', path: '/api/vision/rules' },
  { tkey: 'nm.quickNotifications', method: 'GET', path: '/api/notifications' },
];

// NodeRemote drives the node's own API over the tunnel. The node authorizes each
// request as if it were local (the operator's grant decides viewer vs admin), so a
// read-only operator's writes come back 403 here.
function NodeRemote({ node, onToast }) {
  const t = useT();
  const [method, setMethod] = useState('GET');
  const [path, setPath] = useState('/api/settings/runtime');
  const [body, setBody] = useState('');
  const [resp, setResp] = useState(null);
  const [busy, setBusy] = useState(false);
  const respRef = useRef(null);

  async function send() {
    const p = path.trim();
    if (!p.startsWith('/')) { if (onToast) onToast(t('nm.pathStartSlash')); return; }
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
      <header><h2><span className="btn-icon"><Ico n="send" /> {t('nm.remoteConsole')}</span></h2></header>
      <p className="settings-hint">{t('nm.remoteHint')}</p>
      <div className="node-remote-quick">
        {QUICK.map((q) => (
          <button key={q.tkey} type="button" className="quiet" onClick={() => { setMethod(q.method); setPath(q.path); setBody(''); }}>
            {t(q.tkey)}
          </button>
        ))}
      </div>
      <div className="node-remote-bar">
        <select value={method} onChange={(e) => setMethod(e.target.value)} disabled={busy}>
          {['GET', 'POST', 'PUT', 'DELETE'].map((m) => <option key={m} value={m}>{m}</option>)}
        </select>
        <input value={path} onChange={(e) => setPath(e.target.value)} placeholder="/api/settings/runtime" disabled={busy} />
        <button type="button" onClick={send} disabled={busy}><span className="btn-icon"><Ico n="send" /> {t('nm.send')}</span></button>
      </div>
      {method !== 'GET' ? (
        <label className="node-remote-body">{t('nm.requestBody')}
          <textarea value={body} onChange={(e) => setBody(e.target.value)} rows={5} placeholder='{ "key": "value" }' disabled={busy} />
        </label>
      ) : null}
      {resp ? (
        <div className="node-remote-resp" ref={respRef}>
          <div className={`status-pill ${resp.ok ? 'online' : 'offline'}`}>HTTP {resp.status || '—'}</div>
          <pre className="node-remote-pre">{formatResp(resp, t)}</pre>
        </div>
      ) : null}
    </section>
  );
}

function formatResp(r, t) {
  if (r.body !== undefined && r.body !== null) {
    try { return JSON.stringify(r.body, null, 2); } catch (_) { return String(r.body); }
  }
  return r.message || t('nm.emptyResp');
}
