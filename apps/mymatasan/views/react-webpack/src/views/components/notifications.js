import { useState, useEffect, useMemo, useCallback, useRef } from 'react';
import { Ico } from './icons';
import { useT } from '@shared/i18n';
import { useSnapshotBlob } from '../hooks';
import {
  apiBase,
  formatTimestamp,
  parseMetadata,
  formatSourceLabel,
  detectionTypeLabel,
  fieldValue,
  notificationKind,
  isVisionAlertNotification,
  cameraTitle,
  segmentFilename,
  segmentDuration,
  formatFileSize,
} from '../lib/helpers';

const PAGE_SIZE = 20;

// Type filter chips → the server-side query that isolates each kind. Machine
// health and login both live under the "system" category, so they are separated
// by source instead.
const TYPE_FILTERS = [
  { id: 'all', labelKey: 'notif.filterAll', query: {} },
  { id: 'ai', labelKey: 'notif.filterAi', query: { category: 'vision.alert' } },
  { id: 'camera', labelKey: 'notif.filterCamera', query: { category: 'health.check' } },
  { id: 'machine', labelKey: 'notif.filterMachine', query: { source: 'machine-health-monitor' } },
  { id: 'login', labelKey: 'notif.filterLogin', query: { source: 'local-auth' } },
];

const SEVERITY_RANK = { critical: 3, warning: 2, info: 1 };

// NotificationsTab is the dedicated, full-history notification page. It owns its
// own paged/filtered fetch from the shared /api/notifications store. AI detection
// rows are rich (event screenshot + confidence/source/type) and require an
// explicit Acknowledge; every other kind is dismissed with a single click and
// offers a deep-link to where it came from (health settings, the targeted user).
export function NotificationsTab({
  authHeader,
  isAdmin,
  saved,
  visionAlerts,
  visionRules,
  clipByAlertId,
  focusId,
  refreshSignal,
  onAcknowledgeAlert,
  onMarkRead,
  onMessage,
  onOpenSettingsSection,
  onOpenUser,
}) {
  const t = useT();
  const [view, setView] = useState('unread'); // 'unread' | 'all'
  const [typeFilter, setTypeFilter] = useState('all');
  const [items, setItems] = useState([]);
  const [total, setTotal] = useState(0);
  const [loading, setLoading] = useState(false);
  const focusedRowRef = useRef(null);
  const scrolledFocusRef = useRef(null);
  // In-page clip playback (no redirect to the Recording page).
  const [playingSegment, setPlayingSegment] = useState(null);
  const [videoUrl, setVideoUrl] = useState(null);
  const [loadingVideo, setLoadingVideo] = useState(false);
  // Alert id whose snapshot is open in the zoomable lightbox (0 = closed).
  const [zoomAlertId, setZoomAlertId] = useState(0);

  const closeVideoModal = useCallback(() => {
    setVideoUrl((prev) => {
      if (prev) URL.revokeObjectURL(prev);
      return null;
    });
    setPlayingSegment(null);
    setLoadingVideo(false);
  }, []);

  const playClip = useCallback(
    async (segment) => {
      if (!segment) return;
      setPlayingSegment(segment);
      setVideoUrl(null);
      setLoadingVideo(true);
      try {
        const headers = authHeader ? { Authorization: authHeader } : {};
        const resp = await fetch(`${apiBase()}/api/recording/segments/${segment.id}/download`, { credentials: 'include', headers });
        if (!resp.ok) throw new Error(`${resp.status}`);
        const blob = await resp.blob();
        setVideoUrl(URL.createObjectURL(blob));
      } catch (_) {
        setPlayingSegment(null);
      } finally {
        setLoadingVideo(false);
      }
    },
    [authHeader],
  );

  useEffect(() => {
    if (!playingSegment) return undefined;
    const onKey = (e) => { if (e.key === 'Escape') closeVideoModal(); };
    document.addEventListener('keydown', onKey);
    return () => document.removeEventListener('keydown', onKey);
  }, [playingSegment, closeVideoModal]);

  const alertById = useMemo(
    () => new Map((visionAlerts || []).map((a) => [Number(a.id), a])),
    [visionAlerts],
  );
  const ruleById = useMemo(
    () => new Map((visionRules || []).map((r) => [Number(r.id), r])),
    [visionRules],
  );

  const fetchPage = useCallback(
    async (offset, replace) => {
      setLoading(true);
      try {
        const params = new URLSearchParams({ limit: String(PAGE_SIZE), offset: String(offset) });
        if (view === 'unread') params.set('unread', 'true');
        const typeQuery = (TYPE_FILTERS.find((t) => t.id === typeFilter) || TYPE_FILTERS[0]).query;
        if (typeQuery.category) params.set('category', typeQuery.category);
        if (typeQuery.source) params.set('source', typeQuery.source);
        const headers = authHeader ? { Authorization: authHeader } : {};
        const resp = await fetch(`${apiBase()}/api/notifications?${params}`, { credentials: 'include', headers, cache: 'no-store' });
        if (!resp.ok) throw new Error(`${resp.status}`);
        const payload = await resp.json();
        const result = payload?.data?.result ?? payload?.result ?? payload;
        const list = Array.isArray(result?.items) ? result.items : [];
        setItems((prev) => (replace ? list : [...prev, ...list]));
        setTotal(typeof result?.total === 'number' ? result.total : list.length);
      } catch (e) {
        if (onMessage) onMessage(t('notif.failedLoad'), 'error');
      } finally {
        setLoading(false);
      }
    },
    [authHeader, view, typeFilter, onMessage],
  );

  // Reload from the top whenever the filters change.
  useEffect(() => {
    fetchPage(0, true);
  }, [fetchPage]);

  // Auto-refresh when the bell feed changes (new arrival, or something read/acked
  // elsewhere). Skip the initial mount and skip when the user has paged deeper, so
  // a live update doesn't yank a scrolled list — the Reload button covers that.
  const lastSignalRef = useRef(refreshSignal);
  useEffect(() => {
    if (refreshSignal === lastSignalRef.current) {
      return;
    }
    lastSignalRef.current = refreshSignal;
    if (items.length <= PAGE_SIZE) {
      fetchPage(0, true);
    }
  }, [refreshSignal, fetchPage, items.length]);

  // Scroll to the notification a dropdown click deep-linked to — once per focus,
  // so later list updates (acks/dismisses) don't keep yanking the view back.
  useEffect(() => {
    if (!focusId) {
      scrolledFocusRef.current = null;
      return;
    }
    if (scrolledFocusRef.current === Number(focusId)) {
      return;
    }
    if (focusedRowRef.current) {
      focusedRowRef.current.scrollIntoView({ behavior: 'smooth', block: 'center' });
      scrolledFocusRef.current = Number(focusId);
    }
  }, [focusId, items]);

  // Optimistically reflect an action: drop the row in the Unread view, otherwise
  // flag it read in place. The parent handlers persist + sync the bell feed.
  function applyLocal(id) {
    setItems((cur) =>
      view === 'unread'
        ? cur.filter((n) => Number(n.id) !== Number(id))
        : cur.map((n) => (Number(n.id) === Number(id) ? { ...n, isRead: true } : n)),
    );
  }

  function dismiss(notif) {
    applyLocal(notif.id);
    onMarkRead(notif.id);
  }

  function acknowledge(notif) {
    applyLocal(notif.id);
    onAcknowledgeAlert(Number(notif.refId));
  }

  const hasMore = items.length < total;

  // Infinite scroll: auto-load the next page when the sentinel near the end of
  // the list scrolls into view. A ref holds the latest loader so the observer
  // (set up once) never calls a stale closure; an in-flight guard prevents
  // double-firing before `loading` state settles.
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
    const obs = new IntersectionObserver(
      (entries) => { if (entries[0]?.isIntersecting) loadMoreRef.current(); },
      { rootMargin: '300px' },
    );
    obs.observe(el);
    return () => obs.disconnect();
  }, []);

  return (
    <section className="panel notifications-page">
      <header className="notifications-head">
        <div className="notifications-headings">
          <h2><span className="btn-icon"><Ico n="bell" /> {t('notif.title')}</span></h2>
          <p className="notifications-desc">{t('notif.desc')}</p>
        </div>
        <div className="notifications-toolbar">
          <div className="seg-toggle" role="group" aria-label={t('notif.readFilter')}>
            <button type="button" className={view === 'unread' ? 'active' : 'quiet'} onClick={() => setView('unread')}>{t('notif.unread')}</button>
            <button type="button" className={view === 'all' ? 'active' : 'quiet'} onClick={() => setView('all')}>{t('notif.all')}</button>
          </div>
          <button type="button" className="quiet" onClick={() => fetchPage(0, true)} disabled={loading}>
            <span className="btn-icon"><Ico n="refresh" /> {t('notif.reload')}</span>
          </button>
        </div>
      </header>

      <div className="notifications-filters">
        {TYPE_FILTERS.map((f) => (
          <button
            key={f.id}
            type="button"
            className={`chip${typeFilter === f.id ? ' chip--active' : ''}`}
            onClick={() => setTypeFilter(f.id)}
          >
            {t(f.labelKey)}
          </button>
        ))}
      </div>

      <div className="notifications-list">
        {items.map((notif) => (
          <NotificationRow
            key={notif.id}
            notif={notif}
            alert={isVisionAlertNotification(notif) ? alertById.get(Number(notif.refId)) : null}
            rule={ruleById.get(Number(notif.refId))}
            clip={isVisionAlertNotification(notif) && clipByAlertId ? clipByAlertId.get(Number(notif.refId)) : null}
            saved={saved}
            authHeader={authHeader}
            isAdmin={isAdmin}
            focused={focusId && Number(focusId) === Number(notif.id)}
            focusRef={focusedRowRef}
            onAcknowledge={acknowledge}
            onDismiss={dismiss}
            onPlayClip={playClip}
            onZoomImage={setZoomAlertId}
            onOpenSettingsSection={onOpenSettingsSection}
            onOpenUser={onOpenUser}
          />
        ))}
      </div>

      <div ref={sentinelRef} className="notifications-sentinel" aria-hidden="true" />

      {hasMore ? (
        <div className="notifications-more">
          <button type="button" className="quiet" onClick={() => fetchPage(items.length, false)} disabled={loading}>
            {loading ? t('notif.loading') : t('notif.loadMore', { n: items.length, total })}
          </button>
        </div>
      ) : null}

      {playingSegment ? (
        <div className="video-overlay" onClick={closeVideoModal}>
          <div className="video-dialog" onClick={(e) => e.stopPropagation()}>
            <div className="video-dialog-header">
              <div className="video-dialog-title-group">
                <span className="video-dialog-title">{segmentFilename(playingSegment)}</span>
              </div>
              <button type="button" className="video-dialog-close" onClick={closeVideoModal} aria-label={t('notif.close')}>✕</button>
            </div>
            <div className="video-dialog-body">
              {loadingVideo ? <div className="video-loading-msg">{t('notif.loadingVideo')}</div> : null}
              {videoUrl ? <video className="video-player" controls autoPlay src={videoUrl} /> : null}
            </div>
            <div className="video-dialog-meta">
              {formatTimestamp(playingSegment.startedAt)} · {segmentDuration(playingSegment)} · {formatFileSize(playingSegment.fileSize)}
              {playingSegment.alertId ? ` · ${t('notif.alertNum', { id: playingSegment.alertId })}` : ''}
            </div>
          </div>
        </div>
      ) : null}

      {zoomAlertId ? (
        <ImageZoomModal alertId={zoomAlertId} authHeader={authHeader} onClose={() => setZoomAlertId(0)} />
      ) : null}
    </section>
  );
}

// ImageZoomModal shows an event snapshot full-size in a lightbox with zoom
// (mouse wheel + buttons) and click-drag panning once zoomed in.
function ImageZoomModal({ alertId, authHeader, onClose }) {
  const t = useT();
  const { url, loading, error } = useSnapshotBlob(alertId, authHeader, true);
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

  // Native, non-passive wheel listener so we can preventDefault the page scroll.
  useEffect(() => {
    const el = bodyRef.current;
    if (!el) return undefined;
    const onWheel = (e) => {
      e.preventDefault();
      zoomBy(e.deltaY < 0 ? 0.25 : -0.25);
    };
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
          {loading ? <div className="video-loading-msg">{t('notif.loadingImage')}</div> : null}
          {!loading && url ? (
            <img
              src={url}
              alt={t('notif.eventSnapshot')}
              draggable={false}
              style={{ transform: `translate(${pos.x}px, ${pos.y}px) scale(${scale})` }}
            />
          ) : null}
          {!loading && !url && error ? <div className="video-loading-msg">{t('notif.imageUnavailable')}</div> : null}
        </div>
      </div>
    </div>
  );
}

// NotificationRow renders one feed entry. AI detections get the screenshot +
// detection fields + Acknowledge (and View clip when a recording exists); other
// kinds get a one-click Dismiss plus a deep-link to their origin.
function NotificationRow({
  notif,
  alert,
  rule,
  clip,
  saved,
  authHeader,
  isAdmin,
  focused,
  focusRef,
  onAcknowledge,
  onDismiss,
  onPlayClip,
  onZoomImage,
  onOpenSettingsSection,
  onOpenUser,
}) {
  const t = useT();
  const isAi = isVisionAlertNotification(notif);
  // The hook is always called (rules of hooks); a null id means no fetch.
  const { url: snapUrl, loading: snapLoading, error: snapError } = useSnapshotBlob(
    isAi ? Number(notif.refId) : null,
    authHeader,
    true,
  );

  const data = parseMetadata(notif.metadata); // the stored notification Data
  const alertMeta = alert ? parseMetadata(alert.metadata) : {};
  const unread = !notif.isRead;
  const kind = notificationKind(notif);
  const severity = notif.severity || 'info';

  const rowRef = focused ? focusRef : null;
  const baseClass = `notification-row${focused ? ' focused' : ''}${unread ? '' : ' is-read'}`;

  if (isAi) {
    const acknowledged = alert ? alert.isAcknowledged : notif.isRead;
    // Flag a missing clip only after a short grace period — a fresh detection's
    // clip may still be flushing (pre/post-roll), so we don't cry "No clip" early.
    const ageSec = Date.now() / 1000 - Number(notif.createdAt || 0);
    const noClip = !clip && ageSec >= 120;
    const confidence = Number(alert?.confidence ?? data.confidence ?? 0);
    const objectLabel = alertMeta.objectLabel || alert?.label || data.label || '';
    const detectionType = alert?.detectionType || data.detectionType || '';
    const source = alert ? formatSourceLabel(alertMeta.source) : '';
    const camera =
      data.cameraName ||
      cameraTitle((saved || []).find((d) => Number(d.id) === Number(notif.cameraId))) ||
      (notif.cameraId ? t('notif.cameraN', { id: notif.cameraId }) : '');
    const fields = [
      objectLabel ? { k: 'Object', v: objectLabel, cap: true } : null,
      confidence > 0 ? { k: 'Confidence', v: `${(confidence * 100).toFixed(0)}%` } : null,
      source ? { k: 'Source', v: source } : null,
      detectionType ? { k: 'Type', v: detectionTypeLabel(detectionType) } : null,
    ].filter(Boolean);

    return (
      <article ref={rowRef} className={`${baseClass} notification-row--ai`}>
        <button
          type="button"
          className="notification-snap"
          onClick={() => snapUrl && onZoomImage(Number(notif.refId))}
          disabled={!snapUrl}
          title={snapUrl ? t('notif.clickEnlarge') : undefined}
          aria-label={snapUrl ? t('notif.enlargeSnapshot') : undefined}
        >
          {snapLoading ? <span className="notification-snap-ph">…</span> : null}
          {!snapLoading && snapUrl ? <img src={snapUrl} alt={t('notif.eventSnapshot')} /> : null}
          {!snapLoading && !snapUrl ? <span className="notification-snap-ph"><Ico n="camera" sz={20} /></span> : null}
          {snapError ? null : null}
        </button>
        <div className="notification-body">
          <div className="notification-title-row">
            <span className={`notif-sev notif-sev--${severity}`} aria-hidden="true" />
            <strong>{notif.title || rule?.name || t('notif.detection')}</strong>
            <span className="notification-kind">{t('notif.aiDetection')}</span>
            {!acknowledged ? <span className="segment-unreviewed">{t('notif.unreviewed')}</span> : null}
          </div>
          {camera ? <div className="notification-sub">{camera}</div> : null}
          <dl className="notification-fields">
            {fields.map((f) => (
              <div key={f.k}>
                <dt>{t('notif.f' + f.k)}</dt>
                <dd className={f.cap ? 'cap' : ''}>{fieldValue(f.v)}</dd>
              </div>
            ))}
          </dl>
          <div className="notification-meta">{formatTimestamp(notif.createdAt)}</div>
        </div>
        <div className="notification-actions">
          {unread && !acknowledged ? (
            <button type="button" onClick={() => onAcknowledge(notif)}>
              <span className="btn-icon"><Ico n="acknowledge" /> {t('notif.acknowledge')}</span>
            </button>
          ) : unread ? (
            // Unread but already acknowledged elsewhere — still let the leftover
            // notification be cleared so it can't get stuck in the feed/dropdown.
            <button type="button" className="quiet" onClick={() => onDismiss(notif)}>
              <span className="btn-icon"><Ico n="check-ok" /> {t('notif.dismiss')}</span>
            </button>
          ) : null}
          {clip ? (
            <button type="button" className="quiet" onClick={() => onPlayClip(clip)}>
              <span className="btn-icon"><Ico n="play" /> {t('notif.viewClip')}</span>
            </button>
          ) : noClip ? (
            <span className="notification-noclip"><Ico n="film" sz={13} /> {t('notif.noClip')}</span>
          ) : null}
        </div>
      </article>
    );
  }

  // Non-AI: camera/machine health, login security, settings, other system events.
  // Deep-links target admin-only settings, so they are offered to admins only;
  // viewers still get a one-click Dismiss.
  let deepLink = null;
  if (!isAdmin) {
    deepLink = null;
  } else if (notif.source === 'camera-health-monitor' || notif.category === 'health.check') {
    deepLink = { label: t('notif.cameraHealthSettings'), icon: 'wifi', run: () => onOpenSettingsSection('health') };
  } else if (notif.source === 'machine-health-monitor') {
    deepLink = { label: t('notif.machineHealthSettings'), icon: 'cpu', run: () => onOpenSettingsSection('machine') };
  } else if (notif.source === 'local-auth') {
    const username = (data.username || '').trim();
    deepLink = { label: username ? t('notif.viewUser', { username }) : t('notif.viewUsers'), icon: 'user', run: () => onOpenUser(username) };
  } else if (notif.source === 'settings') {
    deepLink = { label: t('notif.notifSettings'), icon: 'sliders', run: () => onOpenSettingsSection('notifications') };
  }

  return (
    <article ref={rowRef} className={baseClass}>
      <div className="notification-body">
        <div className="notification-title-row">
          <span className={`notif-sev notif-sev--${severity}`} aria-hidden="true" />
          <strong>{notif.title || kind}</strong>
          <span className="notification-kind">{kind}</span>
        </div>
        {notif.body ? <div className="notification-sub notification-sub--body">{notif.body}</div> : null}
        <div className="notification-meta">{formatTimestamp(notif.createdAt)}</div>
      </div>
      <div className="notification-actions">
        {deepLink ? (
          <button type="button" className="quiet" onClick={() => { if (unread) onDismiss(notif); deepLink.run(); }}>
            <span className="btn-icon"><Ico n={deepLink.icon} /> {deepLink.label}</span>
          </button>
        ) : null}
        {unread ? (
          <button type="button" className="quiet" onClick={() => onDismiss(notif)}>
            <span className="btn-icon"><Ico n="check-ok" /> {t('notif.dismiss')}</span>
          </button>
        ) : null}
      </div>
    </article>
  );
}
