import { useCallback, useEffect, useRef, useState } from 'react';
import { Ico, useT } from '@shared';
import { FormBusyOverlay } from './ui';
import { NodeDashboard } from './node_dashboard';
import { NodeEmbed } from './node_embed';
import { NodeDetectionTab } from './node_detection';
import { NodeSettingsTab } from './node_settings';
import { NodeRecordingsTab } from './node_recordings';
import { PTZRing, onvifLocation } from './nodecam/ptz';
import { installProxyCsrf } from './nodecam/lib/helpers';
import { addLiveView, removeLiveView, isInLiveViews, subscribeLiveViews } from '../lib/live_views_store';
import { api, apiBase, csrfToken, formatTimestamp } from '../lib/helpers';

// The copied camera-page components issue their own raw fetch() writes; teach window.fetch
// to attach the CSRF token on proxy writes so those pass myseliasan's auth gate. Idempotent.
installProxyCsrf(csrfToken);

// NodeManager mirrors the node's own app inside the control plane: navigation lives
// entirely in the nav tree (no tabs/back button here), so selecting the node shows its
// dashboard and selecting a camera shows that camera's page — the same surfaces the
// operator sees on the node itself, driven over the control tunnel.
export function NodeManager({ node, onToast, focusCameraId, onClearFocus }) {
  if (focusCameraId != null) {
    return <NodeCameraPage node={node} cameraId={focusCameraId} onToast={onToast} />;
  }
  return <NodeDashboard node={node} onToast={onToast} />;
}

// fieldValue mirrors the node app's helper: shows a dash for empty values (but keeps 0).
const fieldValue = (v) => (v === 0 || v === '0' ? '0' : (v || '—'));
const healthLabelKey = (status) => {
  switch ((status || '').toLowerCase()) {
    case 'online': return 'cam.online';
    case 'offline': return 'cam.offline';
    default: return 'cam.unknown';
  }
};

// NodeCameraPage mirrors the mymatasan camera node page for a managed node: a persistent
// live stage (over myseliasan's media relay) plus the camera's details, health, and ONVIF
// info — read from and written to the node over the control tunnel. Stage 1 of the port;
// Detection/Settings/Recordings tabs follow.
function NodeCameraPage({ node, cameraId, onToast }) {
  const t = useT();
  const nodeId = node.nodeId;
  const [cam, setCam] = useState(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');
  const [iceServers, setIceServers] = useState([]);
  const [draft, setDraft] = useState({ name: '', description: '' });
  const [saving, setSaving] = useState(false);
  const [camTab, setCamTab] = useState('liveview');

  const px = useCallback(
    (path, opts = {}) => api(`/api/nodes/${encodeURIComponent(nodeId)}/proxy${path}`, { noRedirect: true, ...opts }).catch(() => ({ ok: false })),
    [nodeId],
  );

  const load = useCallback(async () => {
    setLoading(true);
    setError('');
    const r = await px('/api/cameras?limit=200');
    setLoading(false);
    if (!r.ok) { setError(r.status === 404 ? t('nodes.nodeOffline') : t('nm.failedCameras')); return; }
    const list = Array.isArray(r.body) ? r.body : (r.body?.items || []);
    const found = list.find((c) => String(c.id) === String(cameraId)) || null;
    setCam(found);
    if (found) setDraft({ name: found.name || '', description: found.description || '' });
  }, [px, cameraId, t]);
  useEffect(() => { load(); }, [load]);

  // ICE config for the relay-backed live tile (empty on same LAN).
  useEffect(() => {
    api('/api/node-stream/config', { noRedirect: true })
      .then((r) => { if (r.ok && Array.isArray(r.body?.iceServers)) setIceServers(r.body.iceServers); })
      .catch(() => {});
  }, []);

  async function saveDetails(e) {
    e.preventDefault();
    if (!cam) return;
    setSaving(true);
    const r = await px(`/api/cameras/${cam.id}`, { method: 'PUT', body: JSON.stringify({ name: draft.name, description: draft.description }) });
    setSaving(false);
    if (r.ok) { if (onToast) onToast(t('cam.detailsSaved')); load(); }
    else if (onToast) onToast(t('cam.saveFailed'));
  }

  const title = cam ? (cam.name || cam.model || cam.host || t('nm.cameraN', { id: cam.id })) : t('nm.cameraN', { id: cameraId });
  const detailsChanged = cam && (draft.name !== (cam.name || '') || draft.description !== (cam.description || ''));
  const isOnvif = !!cam?.xAddr;

  return (
    <section className="workspace">
      {loading ? <FormBusyOverlay busy /> : null}
      <div className="settings-boxes">
        <div className="device-title-row">
          <div>
            <h3>{title}</h3>
            <p>{cam?.xAddr || cam?.host || ''}</p>
          </div>
          {cam ? (
            <div className="device-pill-group">
              <strong className={`status-pill ${(cam.healthStatus || 'unknown').toLowerCase()}`}>{t(healthLabelKey(cam.healthStatus))}</strong>
              <strong className={`status-pill ${cam.rtspStatus || 'unknown'}`}>{cam.rtspStatus || t('cam.notReady')}</strong>
            </div>
          ) : null}
        </div>

        {error ? <p className="settings-hint danger-text">{error}</p> : null}
        {!cam && !loading && !error ? <p className="settings-hint">{t('nm.noCameras')}</p> : null}

        {cam ? (
          <NodeEmbed className="saved-detail">
            <nav className="settings-tabs camera-detail-tabs" role="tablist" aria-label={t('cam.detailTabsAria')}>
              <button type="button" role="tab" aria-selected={camTab === 'liveview'} className={`settings-tab${camTab === 'liveview' ? ' active' : ''}`} onClick={() => setCamTab('liveview')}>
                <Ico n="video" sz={16} /> <span className="settings-tab-label">{t('cam.detailLive')}</span>
              </button>
              <button type="button" role="tab" aria-selected={camTab === 'detection'} className={`settings-tab${camTab === 'detection' ? ' active' : ''}`} onClick={() => setCamTab('detection')}>
                <Ico n="cpu" sz={16} /> <span className="settings-tab-label">{t('cam.detailDetection')}</span>
              </button>
              <button type="button" role="tab" aria-selected={camTab === 'recordings'} className={`settings-tab${camTab === 'recordings' ? ' active' : ''}`} onClick={() => setCamTab('recordings')}>
                <Ico n="film" sz={16} /> <span className="settings-tab-label">{t('tab.recording')}</span>
              </button>
              <button type="button" role="tab" aria-selected={camTab === 'settings'} className={`settings-tab${camTab === 'settings' ? ' active' : ''}`} onClick={() => setCamTab('settings')}>
                <Ico n="sliders" sz={16} /> <span className="settings-tab-label">{t('cam.detailSettings')}</span>
              </button>
            </nav>

            {camTab === 'detection' ? <NodeDetectionTab node={node} camera={cam} onToast={onToast} /> : null}
            {camTab === 'settings' ? <NodeSettingsTab node={node} camera={cam} onToast={onToast} /> : null}
            {camTab === 'recordings' ? <NodeRecordingsTab node={node} camera={cam} onToast={onToast} /> : null}

            {camTab === 'liveview' ? (
              <NodeLiveView node={node} cam={cam} iceServers={iceServers} onToast={onToast} />
            ) : null}
          </NodeEmbed>
        ) : null}
      </div>
    </section>
  );
}

// NodeLiveView reproduces the node app's Live View tab (mymatasan CameraLiveView markup:
// centered stage, PTZ ring overlay, name/status bar, add-to-live-views, info chips) but
// with the relay-backed video tile and PTZ routed over the control tunnel. "Add to Live
// Views" targets myseliasan's OWN cross-node wall (not the node's), per the fleet model.
function NodeLiveView({ node, cam, iceServers, onToast }) {
  const t = useT();
  const nodeId = node.nodeId;
  const [ptzBusy, setPtzBusy] = useState(false);
  const [inViews, setInViews] = useState(() => isInLiveViews(nodeId, cam.id));

  useEffect(() => subscribeLiveViews(() => setInViews(isInLiveViews(nodeId, cam.id))), [nodeId, cam.id]);

  const px = useCallback(
    (path, opts = {}) => api(`/api/nodes/${encodeURIComponent(nodeId)}/proxy${path}`, { noRedirect: true, ...opts }).catch(() => ({ ok: false })),
    [nodeId],
  );

  async function ptzMove(dir) {
    setPtzBusy(true);
    await px(`/api/cameras/${cam.id}/ptz/move`, { method: 'POST', body: JSON.stringify({ direction: dir, speed: 0.35, durationMs: 0 }) });
    setPtzBusy(false);
  }
  function ptzStop() { px(`/api/cameras/${cam.id}/ptz/stop`, { method: 'POST' }); }

  const title = cam.name || cam.model || cam.host || t('nm.cameraN', { id: cam.id });
  function addToViews() {
    addLiveView({ nodeId, cameraId: cam.id, name: title, nodeName: node.name || nodeId });
    if (onToast) onToast(t('cam.addToLiveViews'));
  }
  function removeFromViews() { removeLiveView(nodeId, cam.id); }

  const status = (cam.healthStatus || '').toLowerCase();
  const dotState = status === 'online' ? 'online' : status === 'offline' ? 'offline' : 'unknown';
  const location = onvifLocation(cam.scopes);
  const info = [
    [t('cam.manufacturer'), fieldValue(cam.manufacturer)],
    [t('cam.model'), fieldValue(cam.model)],
    [t('cam.firmware'), fieldValue(cam.firmwareVersion)],
    [t('cam.serial'), fieldValue(cam.serialNumber)],
    ...(location ? [[t('cam.location'), location]] : []),
    [t('cam.host'), fieldValue(cam.host)],
    [t('cam.port'), fieldValue(cam.port)],
    ...(cam.xAddr ? [[t('cam.onvifUri'), cam.xAddr]] : []),
    [t('cam.lastCheckedLabel'), cam.lastHealthCheckAt ? formatTimestamp(cam.lastHealthCheckAt) : '-'],
  ];

  return (
    <section className="camera-live-panel">
      <div className="camera-live-stage">
        <div className="camera-live-view">
          <NodeCameraTile nodeId={nodeId} cam={cam} iceServers={iceServers} />
          {cam.ptzSupported ? (
            <div className="ptz-ring-overlay">
              <PTZRing busy={ptzBusy} size={120} onMove={ptzMove} onStop={ptzStop} />
            </div>
          ) : null}
        </div>
        <div className="camera-live-bar">
          <span className="camera-live-name">
            <span className={`live-dot ${dotState}`} aria-hidden="true" />
            <span className="camera-live-name-text">{title}</span>
          </span>
          {inViews ? (
            <button type="button" className="camera-live-add is-remove" onClick={removeFromViews}>
              <span className="btn-icon"><Ico n="trash" /> {t('cam.removeFromLiveViews')}</span>
            </button>
          ) : (
            <button type="button" className="camera-live-add" onClick={addToViews}>
              <span className="btn-icon"><Ico n="plus" /> {t('cam.addToLiveViews')}</span>
            </button>
          )}
        </div>
      </div>
      {cam.description ? <p className="camera-live-desc">{cam.description}</p> : null}
      <dl className="camera-live-info">
        {info.map(([lbl, value]) => (
          <div key={lbl} className="live-info-chip"><dt>{lbl}</dt><dd>{value}</dd></div>
        ))}
      </dl>
    </section>
  );
}

// NodeCameraTile renders one camera: it negotiates a WebRTC stream against myseliasan
// (which relays the node's RTP) and shows it in a <video>; on any failure it switches
// to snapshot polling over the command tunnel so the tile always shows something.
export function NodeCameraTile({ nodeId, cam, iceServers }) {
  const t = useT();
  const videoRef = useRef(null);
  const audioRef = useRef(null);
  const [mode, setMode] = useState('connecting'); // connecting | live | snapshot
  const [tick, setTick] = useState(Date.now());
  // Audio rides a dedicated <audio> element (video stays muted for autoplay); a mute
  // button controls it, matching mymatasan's live view.
  const [hasAudio, setHasAudio] = useState(false);
  const [muted, setMuted] = useState(true);

  useEffect(() => {
    let cancelled = false;
    let pc = null;
    if (typeof RTCPeerConnection === 'undefined') { setMode('snapshot'); return () => {}; }

    const fallback = () => { if (!cancelled) setMode('snapshot'); };
    setHasAudio(false);
    setMuted(true);

    (async () => {
      try {
        pc = new RTCPeerConnection({ iceServers: iceServers || [] });
        pc.addTransceiver('video', { direction: 'recvonly' });
        pc.addTransceiver('audio', { direction: 'recvonly' });
        pc.ontrack = (event) => {
          if (cancelled) return;
          if (event.track.kind === 'audio') {
            // Route audio to its own element so the video can stay muted (autoplay) while
            // audio is user-toggled independently.
            if (audioRef.current) {
              audioRef.current.srcObject = new MediaStream([event.track]);
              audioRef.current.muted = true; // starts muted; the button unmutes + plays
            }
            setHasAudio(true);
            return;
          }
          if (!videoRef.current) return;
          const stream = videoRef.current.srcObject instanceof MediaStream
            ? videoRef.current.srcObject : new MediaStream();
          stream.addTrack(event.track);
          videoRef.current.srcObject = stream;
          videoRef.current.play().catch(() => {});
          setMode('live');
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
      try {
        if (audioRef.current && audioRef.current.srcObject) {
          audioRef.current.srcObject.getTracks().forEach((trk) => trk.stop());
          audioRef.current.srcObject = null;
        }
      } catch (_) { /* ignore */ }
      if (pc) pc.close();
    };
    // eslint-disable-next-line
  }, [nodeId, cam.id]);

  // Mute button: unmuting starts audio playback (a user gesture, so autoplay policies
  // allow it); muting pauses it.
  function toggleMute() {
    setMuted((prev) => {
      const next = !prev;
      if (audioRef.current) {
        audioRef.current.muted = next;
        if (next) audioRef.current.pause();
        else audioRef.current.play().catch(() => {});
      }
      return next;
    });
  }

  // Snapshot fallback: refresh the still frame on an interval while in snapshot mode.
  useEffect(() => {
    if (mode !== 'snapshot') return undefined;
    const iv = setInterval(() => setTick(Date.now()), 1500);
    return () => clearInterval(iv);
  }, [mode]);

  const label = cam.name || t('nm.cameraN', { id: cam.id });
  const badge = mode === 'live' ? t('nm.badgeLive') : mode === 'snapshot' ? t('nm.badgeSnapshot') : t('nm.badgeConnecting');
  const snapUrl = `${apiBase()}/api/nodes/${encodeURIComponent(nodeId)}/proxy/api/vision/cameras/${cam.id}/frame?t=${tick}`;

  // Mirror mymatasan's LiveViewport markup (`.live-frame` > bare <video>/<img>) so the
  // scoped `.camera-live-view img/video` rules center and size the footage identically.
  return (
    <div className="live-frame">
      {mode === 'snapshot' ? (
        <img src={snapUrl} alt={label} onError={(e) => { e.currentTarget.style.visibility = 'hidden'; }} onLoad={(e) => { e.currentTarget.style.visibility = 'visible'; }} />
      ) : (
        <video ref={videoRef} autoPlay playsInline muted aria-label={label} />
      )}
      <audio ref={audioRef} playsInline style={{ display: 'none' }} />
      {hasAudio ? (
        <button
          type="button"
          className="audio-mute-btn"
          onClick={toggleMute}
          aria-label={muted ? t('prev.unmute') : t('prev.mute')}
          title={muted ? t('prev.unmute') : t('prev.mute')}
        >
          <Ico n={muted ? 'volume-x' : 'volume-2'} sz={14} />
        </button>
      ) : null}
      <span className="node-live-badge">{badge}</span>
    </div>
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
