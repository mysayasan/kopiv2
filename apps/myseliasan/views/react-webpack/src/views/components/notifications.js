import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { Ico, useT } from '@shared';
import { api, apiBase, formatTimestamp } from '../lib/helpers';

const PAGE_SIZE = 30;

// snapshotUrl / streamUrl build the same-origin control-plane URLs for a node's annotated
// event snapshot and its range-streamed recording clip. Both go through myseliasan's node
// proxy / recording-stream, so the browser never contacts the node directly.
const snapshotUrl = (nodeId, alertId) =>
  `${apiBase()}/api/nodes/${encodeURIComponent(nodeId)}/proxy/api/vision/alerts/${alertId}/snapshot?annotated=1`;
const recordingStreamUrl = (nodeId, segId) =>
  `${apiBase()}/api/nodes/${encodeURIComponent(nodeId)}/recording-stream/${segId}`;

const clipKey = (nodeId, alertId) => `${nodeId}:${alertId}`;

// NotificationsPage is the control plane's consolidated feed: myseliasan's OWN system
// events (node going-offline, control-plane warnings, login/security) AND every event a
// managed mymatasan node pushes up its control channel — all in one place. The backend
// already tags node-sourced rows `source: node:<id>` (see app.go ingestNodeEvent), so this
// page just lists /api/notifications, lets the operator scope to a single node or unread,
// and live-updates from the SSE stream (driven by `refreshSignal` bumped in App).
export function NotificationsPage({ nodes = [], refreshSignal = 0, onChanged }) {
  const t = useT();
  const [view, setView] = useState('unread'); // 'unread' | 'all'
  const [nodeFilter, setNodeFilter] = useState('all'); // 'all' | nodeId
  const [items, setItems] = useState([]);
  const [total, setTotal] = useState(0);
  const [loading, setLoading] = useState(false);
  // Snapshot lightbox (camera button) + recording player (play button), mirroring the
  // mymatasan Notifications page — opened in-place instead of a new browser tab.
  const [zoom, setZoom] = useState(null); // { url } | null
  const [playing, setPlaying] = useState(null); // { url, startedAt } | null
  // clip lookup: `${nodeId}:${alertId}` -> recording segment. Pre-resolved per node so the
  // Play button only shows when a clip actually exists (matching mymatasan).
  const [clips, setClips] = useState(() => new Map());
  const fetchedNodesRef = useRef(new Set());

  const nodeNameById = useMemo(() => {
    const m = new Map();
    (nodes || []).forEach((n) => m.set(String(n.nodeId), n.name || n.nodeId));
    return m;
  }, [nodes]);

  const fetchPage = useCallback(
    async (offset, replace) => {
      setLoading(true);
      if (replace) {
        // Reloading from the top (filter change / Reload / live arrival) — drop the clip
        // cache so segments are re-resolved for the fresh list.
        fetchedNodesRef.current = new Set();
        setClips(new Map());
      }
      const params = new URLSearchParams({ limit: String(PAGE_SIZE), offset: String(offset) });
      if (view === 'unread') params.set('unread', 'true');
      if (nodeFilter !== 'all') params.set('nodeId', nodeFilter);
      const r = await api(`/api/notifications?${params}`, { noRedirect: true }).catch(() => ({ ok: false }));
      setLoading(false);
      if (!r.ok) return;
      const list = Array.isArray(r.body?.items) ? r.body.items : [];
      setItems((prev) => (replace ? list : [...prev, ...list]));
      setTotal(typeof r.body?.total === 'number' ? r.body.total : list.length);
    },
    [view, nodeFilter],
  );

  // Reload from the top whenever the filters change.
  useEffect(() => { fetchPage(0, true); }, [fetchPage]);

  // Live refresh: when the SSE stream reports a new notification (App bumps refreshSignal),
  // reload the top page — but only when the user hasn't paged deeper, so a live arrival
  // doesn't yank a scrolled list. The Reload button covers the deep-scroll case.
  const lastSignalRef = useRef(refreshSignal);
  useEffect(() => {
    if (refreshSignal === lastSignalRef.current) return;
    lastSignalRef.current = refreshSignal;
    if (items.length <= PAGE_SIZE) fetchPage(0, true);
  }, [refreshSignal, fetchPage, items.length]);

  // Resolve recording clips for the AI-detection rows: fetch each involved node's segments
  // ONCE, build a `${nodeId}:${alertId}` → segment map. The Play button then renders only
  // when a clip exists — like mymatasan, which pre-loads segments and hides the button when
  // a detection has no recorded clip.
  useEffect(() => {
    const nodeIds = new Set();
    items.forEach((n) => {
      const src = String(n.source || '');
      if (src.startsWith('node:') && n.refType === 'alert_event' && Number(n.refId) > 0) nodeIds.add(src.slice(5));
    });
    const toFetch = [...nodeIds].filter((id) => !fetchedNodesRef.current.has(id));
    if (toFetch.length === 0) return undefined;
    toFetch.forEach((id) => fetchedNodesRef.current.add(id));
    let cancelled = false;
    (async () => {
      const results = await Promise.all(toFetch.map(async (nodeId) => {
        const r = await api(`/api/nodes/${encodeURIComponent(nodeId)}/proxy/api/recording/segments?limit=500&offset=0`, { noRedirect: true }).catch(() => ({ ok: false }));
        const list = r.ok ? (Array.isArray(r.body) ? r.body : (r.body?.items || [])) : [];
        return { nodeId, list };
      }));
      if (cancelled) return;
      setClips((prev) => {
        const next = new Map(prev);
        results.forEach(({ nodeId, list }) => {
          (list || []).forEach((seg) => {
            const aid = Number(seg.alertId);
            if (aid > 0 && seg.id) {
              const key = clipKey(nodeId, aid);
              if (!next.has(key)) next.set(key, seg);
            }
          });
        });
        return next;
      });
    })();
    return () => { cancelled = true; };
  }, [items]);

  async function acknowledge(notif) {
    // Optimistically clear: drop from the Unread view, else flag read in place.
    setItems((cur) => (view === 'unread'
      ? cur.filter((n) => Number(n.id) !== Number(notif.id))
      : cur.map((n) => (Number(n.id) === Number(notif.id) ? { ...n, isRead: true } : n))));
    // For a node AI detection, propagate the acknowledgement to the source alert on the
    // node (over the proxy) so the node's own review state stays in sync — then clear the
    // notification from the control-plane feed.
    const src = String(notif.source || '');
    const nodeId = src.startsWith('node:') ? src.slice(5) : '';
    if (nodeId && notif.refType === 'alert_event' && Number(notif.refId) > 0) {
      await api(`/api/nodes/${encodeURIComponent(nodeId)}/proxy/api/vision/alerts/${notif.refId}/ack`, { method: 'POST', noRedirect: true }).catch(() => {});
    }
    await api(`/api/notifications/${notif.id}/read`, { method: 'POST', noRedirect: true }).catch(() => {});
    if (onChanged) onChanged();
  }

  // Play a resolved recording clip (segment already known to exist) over the range-capable
  // recording-stream endpoint.
  const playSegment = useCallback((nodeId, seg) => {
    setPlaying({ url: recordingStreamUrl(nodeId, seg.id), startedAt: seg.startedAt });
  }, []);

  const hasMore = items.length < total;

  // Infinite scroll: load the next page when the sentinel nears the viewport. A ref holds
  // the latest loader so the observer (set up once) never calls a stale closure.
  const sentinelRef = useRef(null);
  const inFlightRef = useRef(false);
  const loadMoreRef = useRef(() => {});
  loadMoreRef.current = () => {
    if (inFlightRef.current || loading || !hasMore) return;
    inFlightRef.current = true;
    fetchPage(items.length, false).finally(() => { inFlightRef.current = false; });
  };
  useEffect(() => {
    const el = sentinelRef.current;
    if (!el || typeof IntersectionObserver === 'undefined') return undefined;
    const obs = new IntersectionObserver((entries) => { if (entries[0]?.isIntersecting) loadMoreRef.current(); }, { rootMargin: '300px' });
    obs.observe(el);
    return () => obs.disconnect();
  }, []);

  return (
    <section className="workspace notifications-page">
      <div className="camera-tab-header">
        <div>
          <h2 className="section-title"><span className="btn-icon"><Ico n="bell" /> {t('notif.title')}</span></h2>
          <p className="section-subtitle">{t('notif.desc')}</p>
        </div>
      </div>

      <div className="notifications-toolbar">
        <div className="seg-toggle" role="group" aria-label={t('notif.readFilter')}>
          <button type="button" className={view === 'unread' ? 'active' : 'quiet'} onClick={() => setView('unread')}>{t('notif.unread')}</button>
          <button type="button" className={view === 'all' ? 'active' : 'quiet'} onClick={() => setView('all')}>{t('notif.all')}</button>
        </div>
        <label className="notifications-source">
          <span className="notifications-source-label">{t('notif.source')}</span>
          <select value={nodeFilter} onChange={(e) => setNodeFilter(e.target.value)}>
            <option value="all">{t('notif.allSources')}</option>
            {(nodes || []).map((n) => (
              <option key={n.nodeId} value={n.nodeId}>{n.name || n.nodeId}</option>
            ))}
          </select>
        </label>
        <button type="button" className="quiet" onClick={() => fetchPage(0, true)} disabled={loading}>
          <span className="btn-icon"><Ico n="reload" /> {t('notif.reload')}</span>
        </button>
      </div>

      {items.length === 0 && !loading ? (
        <div className="page-placeholder">
          <h2>{t('notif.emptyTitle')}</h2>
          <p>{view === 'unread' ? t('notif.emptyUnread') : t('notif.emptyAll')}</p>
        </div>
      ) : (
        <div className="notifications-list">
          {items.map((notif) => {
            const src = String(notif.source || '');
            const nodeId = src.startsWith('node:') ? src.slice(5) : '';
            const isAiAlert = !!nodeId && notif.refType === 'alert_event' && Number(notif.refId) > 0;
            const clip = isAiAlert ? clips.get(clipKey(nodeId, notif.refId)) : null;
            return (
              <NotificationRow
                key={notif.id}
                notif={notif}
                nodeId={nodeId}
                isAiAlert={isAiAlert}
                clip={clip}
                nodeNameById={nodeNameById}
                onAcknowledge={acknowledge}
                onZoom={(url) => setZoom({ url })}
                onPlay={playSegment}
                t={t}
              />
            );
          })}
        </div>
      )}

      <div ref={sentinelRef} className="notifications-sentinel" aria-hidden="true" />
      {hasMore ? (
        <div className="notifications-more">
          <button type="button" className="quiet" onClick={() => fetchPage(items.length, false)} disabled={loading}>
            {loading ? t('notif.loading') : t('notif.loadMore', { n: items.length, total })}
          </button>
        </div>
      ) : null}

      {zoom ? <ImageZoomModal url={zoom.url} onClose={() => setZoom(null)} t={t} /> : null}
      {playing ? <VideoModal state={playing} onClose={() => setPlaying(null)} t={t} /> : null}
    </section>
  );
}

// NotificationRow renders one consolidated entry: the AI snapshot (with an enlarge button,
// plus a play button when the detection has a recorded clip), the severity marker,
// title/body, the origin (the node it came from, or "Control plane"), a timestamp, and a
// one-click Dismiss while unread.
function NotificationRow({ notif, nodeId, isAiAlert, clip, nodeNameById, onAcknowledge, onZoom, onPlay, t }) {
  const unread = !notif.isRead;
  const severity = notif.severity || 'info';
  const origin = nodeId ? (nodeNameById.get(nodeId) || t('notif.nodeN', { id: nodeId })) : t('notif.controlPlane');

  return (
    <article className={`notification-row${unread ? '' : ' is-read'}${isAiAlert ? ' notification-row--ai' : ''}`}>
      {isAiAlert ? (
        <NotificationSnap
          url={snapshotUrl(nodeId, notif.refId)}
          clip={clip}
          onZoom={onZoom}
          onPlay={() => onPlay(nodeId, clip)}
          t={t}
        />
      ) : null}
      <div className="notification-body">
        <div className="notification-title-row">
          <span className={`notif-sev notif-sev--${severity}`} aria-hidden="true" />
          <strong>{notif.title || t('notif.event')}</strong>
          <span className="notification-kind">{origin}</span>
        </div>
        {notif.body ? <div className="notification-sub">{notif.body}</div> : null}
        <div className="notification-meta">{formatTimestamp(notif.createdAt)}</div>
      </div>
      <div className="notification-actions">
        {unread ? (
          <button type="button" onClick={() => onAcknowledge(notif)}>
            <span className="btn-icon"><Ico n="acknowledge" /> {t('notif.acknowledge')}</span>
          </button>
        ) : null}
      </div>
    </article>
  );
}

// NotificationSnap shows the annotated event screenshot for a node AI detection, pulled
// through myseliasan's node proxy. The image itself isn't clickable; overlay buttons sit on
// top — Play (only when a recording clip exists) opens the clip in a dialog, and the camera
// button enlarges the snapshot in a zoom lightbox.
function NotificationSnap({ url, clip, onZoom, onPlay, t }) {
  const [failed, setFailed] = useState(false);
  if (failed) {
    return <div className="notification-snap notification-snap--ph"><Ico n="camera" sz={20} /></div>;
  }
  return (
    <div className="notification-snap">
      <img src={url} alt={t('notif.eventSnapshot')} loading="lazy" onError={() => setFailed(true)} />
      <div className="obs-thumb-actions">
        {clip ? (
          <button type="button" className="obs-thumb-btn" onClick={onPlay} title={t('notif.viewClip')} aria-label={t('notif.viewClip')}>
            <Ico n="play" sz={16} />
          </button>
        ) : null}
        <button type="button" className="obs-thumb-btn" onClick={() => onZoom(url)} title={t('notif.enlarge')} aria-label={t('notif.enlarge')}>
          <Ico n="camera" sz={16} />
        </button>
      </div>
    </div>
  );
}

// ImageZoomModal shows an event snapshot full-size in a lightbox with zoom (wheel + buttons)
// and click-drag panning once zoomed in. The image is streamed same-origin through the node
// proxy, so a plain <img src> works (no auth header / blob needed).
function ImageZoomModal({ url, onClose, t }) {
  const [scale, setScale] = useState(1);
  const [pos, setPos] = useState({ x: 0, y: 0 });
  const dragRef = useRef(null);
  const bodyRef = useRef(null);

  const clampScale = (s) => Math.min(5, Math.max(1, Math.round(s * 100) / 100));
  const reset = () => { setScale(1); setPos({ x: 0, y: 0 }); };
  const zoomBy = (delta) => setScale((s) => {
    const next = clampScale(s + delta);
    if (next === 1) setPos({ x: 0, y: 0 });
    return next;
  });

  useEffect(() => {
    const onKey = (e) => { if (e.key === 'Escape') onClose(); };
    document.addEventListener('keydown', onKey);
    return () => document.removeEventListener('keydown', onKey);
  }, [onClose]);

  useEffect(() => {
    const el = bodyRef.current;
    if (!el) return undefined;
    const onWheel = (e) => { e.preventDefault(); zoomBy(e.deltaY < 0 ? 0.25 : -0.25); };
    el.addEventListener('wheel', onWheel, { passive: false });
    return () => el.removeEventListener('wheel', onWheel);
  }, []);

  function onPointerDown(e) {
    if (scale <= 1) return;
    dragRef.current = { startX: e.clientX, startY: e.clientY, baseX: pos.x, baseY: pos.y };
  }
  function onPointerMove(e) {
    if (!dragRef.current) return;
    setPos({ x: dragRef.current.baseX + (e.clientX - dragRef.current.startX), y: dragRef.current.baseY + (e.clientY - dragRef.current.startY) });
  }
  function onPointerUp() { dragRef.current = null; }

  return (
    <div className="image-zoom-overlay" onClick={onClose}>
      <div className="image-zoom-dialog" onClick={(e) => e.stopPropagation()}>
        <div className="image-zoom-toolbar">
          <button type="button" className="quiet" onClick={() => zoomBy(-0.25)} disabled={scale <= 1} aria-label={t('notif.zoomOut')}>−</button>
          <span className="image-zoom-level">{Math.round(scale * 100)}%</span>
          <button type="button" className="quiet" onClick={() => zoomBy(0.25)} disabled={scale >= 5} aria-label={t('notif.zoomIn')}>+</button>
          <button type="button" className="quiet" onClick={reset} disabled={scale === 1 && pos.x === 0 && pos.y === 0}>{t('notif.reset')}</button>
          <span className="image-zoom-spacer" />
          <button type="button" className="image-zoom-close" onClick={onClose} aria-label={t('notif.close')}>✕</button>
        </div>
        <div
          ref={bodyRef}
          className="image-zoom-body"
          onDoubleClick={() => (scale > 1 ? reset() : zoomBy(1))}
          onPointerDown={onPointerDown}
          onPointerMove={onPointerMove}
          onPointerUp={onPointerUp}
          onPointerLeave={onPointerUp}
          style={{ cursor: scale > 1 ? (dragRef.current ? 'grabbing' : 'grab') : 'zoom-in' }}
        >
          <img src={url} alt={t('notif.eventSnapshot')} draggable={false} style={{ transform: `translate(${pos.x}px, ${pos.y}px) scale(${scale})` }} />
        </div>
      </div>
    </div>
  );
}

// VideoModal plays the recording clip linked to a detection, streamed over the control
// tunnel via /recording-stream.
function VideoModal({ state, onClose, t }) {
  useEffect(() => {
    const onKey = (e) => { if (e.key === 'Escape') onClose(); };
    document.addEventListener('keydown', onKey);
    return () => document.removeEventListener('keydown', onKey);
  }, [onClose]);

  return (
    <div className="video-overlay" onClick={onClose}>
      <div className="video-dialog" onClick={(e) => e.stopPropagation()}>
        <div className="video-dialog-header">
          <div className="video-dialog-title-group">
            <span className="video-dialog-title">{t('notif.eventClip')}</span>
          </div>
          <button type="button" className="video-dialog-close" onClick={onClose} aria-label={t('notif.close')}>✕</button>
        </div>
        <div className="video-dialog-body">
          <video className="video-player" controls autoPlay src={state.url} />
        </div>
        {state.startedAt ? <div className="video-dialog-meta">{formatTimestamp(state.startedAt)}</div> : null}
      </div>
    </div>
  );
}
