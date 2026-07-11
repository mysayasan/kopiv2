import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { Ico, useT } from '@shared';
import { api, apiBase, formatTimestamp } from '../lib/helpers';

const PAGE_SIZE = 30;

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

  const nodeNameById = useMemo(() => {
    const m = new Map();
    (nodes || []).forEach((n) => m.set(String(n.nodeId), n.name || n.nodeId));
    return m;
  }, [nodes]);

  const fetchPage = useCallback(
    async (offset, replace) => {
      setLoading(true);
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
          {items.map((notif) => (
            <NotificationRow key={notif.id} notif={notif} nodeNameById={nodeNameById} onAcknowledge={acknowledge} t={t} />
          ))}
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
    </section>
  );
}

// NotificationRow renders one consolidated entry: a severity marker, title/body, the origin
// (the node it came from, or "Control plane" for myseliasan's own events), a timestamp, and
// a one-click Dismiss while unread.
function NotificationRow({ notif, nodeNameById, onAcknowledge, t }) {
  const unread = !notif.isRead;
  const severity = notif.severity || 'info';
  const src = String(notif.source || '');
  const nodeId = src.startsWith('node:') ? src.slice(5) : '';
  const origin = nodeId ? (nodeNameById.get(nodeId) || t('notif.nodeN', { id: nodeId })) : t('notif.controlPlane');
  // AI-detection rows carry an annotated event snapshot on the node; fetch it over the
  // control tunnel (the proxy streams the image — the browser never hits the node).
  const isAiAlert = notif.refType === 'alert_event' && Number(notif.refId) > 0 && !!nodeId;

  return (
    <article className={`notification-row${unread ? '' : ' is-read'}${isAiAlert ? ' notification-row--ai' : ''}`}>
      {isAiAlert ? <NotificationSnap nodeId={nodeId} alertId={notif.refId} t={t} /> : null}
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
// through myseliasan's node proxy (`/api/nodes/{id}/proxy/api/vision/alerts/{id}/snapshot`).
// The proxy streams the image bytes, so the browser loads it same-origin (cookie auth, GET
// needs no CSRF) without ever contacting the node directly. Clicking opens it full-size.
function NotificationSnap({ nodeId, alertId, t }) {
  const [failed, setFailed] = useState(false);
  const url = `${apiBase()}/api/nodes/${encodeURIComponent(nodeId)}/proxy/api/vision/alerts/${alertId}/snapshot?annotated=1`;
  if (failed) {
    return <div className="notification-snap notification-snap--ph"><Ico n="camera" sz={20} /></div>;
  }
  return (
    <a className="notification-snap" href={url} target="_blank" rel="noreferrer" title={t('notif.enlarge')}>
      <img src={url} alt={t('notif.eventSnapshot')} loading="lazy" onError={() => setFailed(true)} />
    </a>
  );
}
