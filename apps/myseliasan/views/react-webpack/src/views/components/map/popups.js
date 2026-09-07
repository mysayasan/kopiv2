import { useEffect, useLayoutEffect, useRef, useState } from 'react';
import PropTypes from 'prop-types';
import { useT, Ico, Tabs } from '@shared';
import { api, apiBase } from '../../lib/helpers';
import { nodeTone } from '../../lib/fleet_status';

// The floating cards the map opens over itself: a node's device card (status, cameras, events)
// and the frame that anchors any of them to a pin without letting it clip off the viewport.
//
// Extracted from fleet_map.js unchanged.

// Same-origin control-plane URLs for a node event's annotated snapshot and its recorded clip.
// Both route through myseliasan's node proxy / recording-stream, so the browser never contacts
// the node directly (mirrors the Notifications page).
const eventSnapshotSrc = (nodeId, alertId) =>
  `${apiBase()}/api/nodes/${encodeURIComponent(nodeId)}/proxy/api/vision/alerts/${alertId}/snapshot?annotated=1`;
const recordingStreamSrc = (nodeId, segId) =>
  `${apiBase()}/api/nodes/${encodeURIComponent(nodeId)}/recording-stream/${segId}`;
// A node event carries reviewable footage when it's a mymatasan AI detection (alert_event + refId).
const eventHasFootage = (e) => e && e.refType === 'alert_event' && Number(e.refId) > 0;


const SEV_RANK = { critical: 3, warning: 2, info: 1 };
// Compact relative time ("5m", "3h", "2d") for the event list.
function shortAgo(sec) {
  if (!sec) return '';
  const d = Math.max(0, Math.floor(Date.now() / 1000) - sec);
  if (d < 60) return `${d}s`;
  if (d < 3600) return `${Math.floor(d / 60)}m`;
  if (d < 86400) return `${Math.floor(d / 3600)}h`;
  return `${Math.floor(d / 86400)}d`;
}

// NodeCameraPopup summarises a placed node as a compact, tabbed status card: a status-coloured
// header (identity + state, glanceable at a glance), a meta strip with at-a-glance counts and the
// Open-node action, then Events / Cameras as TABS — so only one clean, single-scroll list shows at
// a time instead of two competing stacked lists. Events are worst-first; cameras are searchable
// once there are many. Everything is fetched live and the card never grows past the viewport.
function NodeCameraPopup({ node, nowSec, onOpenNode, onPlay, onOpenMedia, onLocate, onAck, onClose }) {
  const t = useT();
  const [cams, setCams] = useState({ loading: true, list: [], reachable: true });
  const [events, setEvents] = useState({ loading: true, list: [] });
  const [camQuery, setCamQuery] = useState('');
  const [tab, setTab] = useState('events');

  // Acknowledge an event straight from the popup: mark it read (optimistically), propagate the
  // ack to the source AI alert on the node when it's a detection, and let the map refresh its pin
  // badges. Mirrors the Notifications page's acknowledge, minus the list re-fetch.
  const ackEvent = useCallback((e) => {
    setEvents((cur) => ({ ...cur, list: cur.list.map((n) => (n.id === e.id ? { ...n, isRead: true } : n)) }));
    if (eventHasFootage(e)) {
      api(`/api/nodes/${encodeURIComponent(node.nodeId)}/proxy/api/vision/alerts/${e.refId}/ack`, { method: 'POST', noRedirect: true }).catch(() => {});
    }
    api(`/api/notifications/${e.id}/read`, { method: 'POST', noRedirect: true }).catch(() => {});
    if (onAck) onAck();
  }, [node.nodeId, onAck]);

  useEffect(() => {
    // Only a camera node serves /api/cameras. Asking a sensor hub or a door controller was a
    // guaranteed 404 down the tunnel on every popup open — wasted round-trip, noisy node log.
    if (nodeKindOf(node) !== 'camera') {
      setCams({ loading: false, list: [], reachable: true });
      return undefined;
    }
    let live = true;
    setCams({ loading: true, list: [], reachable: true });
    api(`/api/nodes/${encodeURIComponent(node.nodeId)}/proxy/api/cameras?limit=200`, { noRedirect: true })
      .then((r) => {
        if (!live) return;
        const list = r.ok ? (Array.isArray(r.body) ? r.body : r.body?.items || []) : [];
        setCams({ loading: false, list, reachable: r.ok });
      })
      .catch(() => { if (live) setCams({ loading: false, list: [], reachable: false }); });
    return () => { live = false; };
    // eslint-disable-next-line
  }, [node.nodeId]);

  // The node's recent events, ordered MOST CRITICAL FIRST (then newest).
  useEffect(() => {
    let live = true;
    setEvents({ loading: true, list: [] });
    api(`/api/notifications?nodeId=${encodeURIComponent(node.nodeId)}&limit=30`, { noRedirect: true })
      .then((r) => {
        if (!live) return;
        const rows = Array.isArray(r.body?.items) ? r.body.items : (Array.isArray(r.body) ? r.body : []);
        rows.sort((a, b) => {
          const s = (SEV_RANK[(b.severity || '').toLowerCase()] || 0) - (SEV_RANK[(a.severity || '').toLowerCase()] || 0);
          return s !== 0 ? s : (b.createdAt || 0) - (a.createdAt || 0);
        });
        setEvents({ loading: false, list: rows });
      })
      .catch(() => { if (live) setEvents({ loading: false, list: [] }); });
    return () => { live = false; };
  }, [node.nodeId]);

  const toneKey = nodeToneKey(node, nowSec);
  const tone = nodeTone(node, nowSec);
  const pillClass = toneKey === 'online' ? 'online' : toneKey === 'critical' ? 'offline' : toneKey === 'warning' ? 'warn' : '';
  const isCamera = nodeKindOf(node) === 'camera';
  const unread = events.list.filter((e) => !e.isRead).length;
  const onlineCams = cams.list.filter((c) => (c.healthStatus || '').toLowerCase() === 'online').length;

  const tabs = [{ id: 'events', label: (<>{t('map.tabEvents')}{unread > 0 ? <span className="mp-tab-count danger">{unread}</span> : null}</>), icon: 'bell' }];
  if (isCamera) tabs.push({ id: 'cameras', label: (<>{t('map.cameras')}{cams.list.length > 0 ? <span className="mp-tab-count">{cams.list.length}</span> : null}</>), icon: 'video' });
  const activeTab = (tab === 'cameras' && !isCamera) ? 'events' : tab;

  const renderEvents = () => {
    if (events.loading) return <div className="mp-empty">{t('common.loading')}</div>;
    if (events.list.length === 0) return <div className="mp-empty"><Ico n="bell" sz={22} /><span>{t('map.noEvents')}</span></div>;
    return events.list.map((e) => {
      const title = e.title || e.body || t('map.event');
      const footage = eventHasFootage(e);
      const main = (
        <>
          <span className={`sev-dot sev-${(e.severity || 'info').toLowerCase()}`} />
          <span className="mp-event-title" title={e.body || e.title}>{title}</span>
          {footage ? <span className="mp-event-cam" aria-hidden="true"><Ico n="camera" sz={12} /></span> : null}
        </>
      );
      return (
        <div key={e.id} className={`mp-event${e.isRead ? ' read' : ''}`}>
          {footage ? (
            <button
              type="button"
              className="mp-event-main has-footage"
              title={t('map.openFootage')}
              onClick={(ev) => onOpenMedia && onOpenMedia({ nodeId: node.nodeId, alertId: Number(e.refId), name: title }, ev.clientX, ev.clientY)}
            >
              {main}
            </button>
          ) : (
            <span className="mp-event-main">{main}</span>
          )}
          <span className="mp-event-time">{shortAgo(e.createdAt)}</span>
          {onLocate && Number(e.cameraId) > 0 ? (
            <button type="button" className="mp-event-locate" title={t('map.locateOnPlan')} aria-label={t('map.locateOnPlan')} onClick={() => onLocate({ nodeId: node.nodeId, cameraId: e.cameraId, name: title })}>
              <Ico n="map-pin" sz={13} />
            </button>
          ) : null}
          {!e.isRead ? (
            <button type="button" className="mp-event-ack" title={t('map.ack')} aria-label={t('map.ack')} onClick={() => ackEvent(e)}>
              <Ico n="acknowledge" sz={13} />
            </button>
          ) : null}
        </div>
      );
    });
  };

  const renderCameras = () => {
    if (cams.loading) return <div className="mp-empty">{t('common.loading')}</div>;
    if (!cams.reachable) return <div className="mp-empty"><Ico n="video" sz={22} /><span>{t('map.camsOffline')}</span></div>;
    if (cams.list.length === 0) return <div className="mp-empty"><Ico n="video" sz={22} /><span>{t('map.noCams')}</span></div>;
    const q = camQuery.trim().toLowerCase();
    const shown = q ? cams.list.filter((c) => (c.name || `${c.id}`).toLowerCase().includes(q)) : cams.list;
    return (
      <>
        {/* Sticky search once there are many cameras, so hundreds stay navigable — it stays put
            while the list below scrolls. */}
        {cams.list.length > 8 ? (
          <div className="mp-camsearch">
            <Ico n="search" sz={13} />
            <input
              type="text"
              value={camQuery}
              onChange={(e) => setCamQuery(e.target.value)}
              placeholder={t('map.searchCams')}
              aria-label={t('map.searchCams')}
            />
          </div>
        ) : null}
        {shown.length === 0 ? <div className="mp-empty"><span>{t('map.noCamMatch')}</span></div> : shown.map((c) => (
          <button key={c.id} type="button" className="mp-cam" onClick={(e) => onPlay && onPlay({ nodeId: node.nodeId, cameraId: c.id, name: c.name || t('nodes.cameraN', { id: c.id }), ptzSupported: !!c.ptzSupported }, e.clientX, e.clientY)}>
            <span className={`cam-dot ${((c.healthStatus || '').toLowerCase() === 'online') ? 'on' : 'off'}`} />
            <span className="mp-cam-name">{c.name || t('nodes.cameraN', { id: c.id })}</span>
            <Ico n="play" sz={12} />
          </button>
        ))}
      </>
    );
  };

  return (
    <div className="mp-card" role="dialog" aria-label={node.name || node.nodeId}>
      {/* The whole identity block is the "open node" affordance — clicking the node's name/icon
          opens its management pages (a chevron hints at it on hover). No separate action icon. */}
      <div className="mp-head">
        <button
          type="button"
          className="mp-id"
          onClick={() => onOpenNode && onOpenNode(node.nodeId)}
          disabled={!onOpenNode}
          title={onOpenNode ? t('map.openNode') : undefined}
        >
          <span className="mp-avatar" style={{ background: tone.color }}><Ico n={isCamera ? 'video' : 'cpu'} sz={15} /></span>
          <span className="mp-title" title={node.name || node.nodeId}>{node.name || node.nodeId}</span>
          {onOpenNode ? <span className="mp-id-go" aria-hidden="true"><Ico n="chev-right" sz={15} /></span> : null}
        </button>
        <button type="button" className="icon-button mp-close" onClick={onClose} aria-label={t('nset.close')}><Ico n="x" sz={14} /></button>
      </div>

      <div className="mp-meta">
        <span className={`status-pill ${pillClass}`}>{t(`map.legend.${toneKey}`)}</span>
        <span className="mp-meta-spacer" />
        {isCamera && cams.list.length > 0 ? (
          <span className="mp-stat" title={t('map.cameras')}><Ico n="video" sz={12} /> {onlineCams}/{cams.list.length}</span>
        ) : null}
        {unread > 0 ? <span className="mp-stat danger" title={t('map.events')}><Ico n="bell" sz={12} /> {unread}</span> : null}
      </div>

      {tabs.length > 1 ? (
        <Tabs tabs={tabs} active={activeTab} onChange={setTab} ariaLabel={t('map.sections')} className="mp-tabs" />
      ) : (
        <div className="mp-solo-head"><Ico n="bell" sz={13} /> {t('map.events')} {unread > 0 ? <span className="mp-tab-count danger">{unread}</span> : null}</div>
      )}

      <div className="mp-body">
        {activeTab === 'cameras' ? renderCameras() : renderEvents()}
      </div>
    </div>
  );
}
NodeCameraPopup.propTypes = { node: PropTypes.object, nowSec: PropTypes.number, onOpenNode: PropTypes.func, onPlay: PropTypes.func, onOpenMedia: PropTypes.func, onLocate: PropTypes.func, onAck: PropTypes.func, onClose: PropTypes.func };

// MapPopupFrame positions the node popup relative to the pin's VIEWPORT coordinates (x, y) and
// keeps it fully on screen: centred over the pin and floated above it, but dropped below when the
// header would clip the top edge, and clamped horizontally + vertically so it never spills out —
// however tall the popup grows. It re-clamps whenever the popup's own size changes (its camera /
// event lists load in asynchronously), so a node near any edge always shows its full header.
function MapPopupFrame({ x, y, children }) {
  const ref = useRef(null);
  const [style, setStyle] = useState({ left: 0, top: 0, visibility: 'hidden' });

  useLayoutEffect(() => {
    const el = ref.current;
    if (!el) return undefined;
    const place = () => {
      const r = el.getBoundingClientRect();
      const M = 10; // viewport margin
      const vw = window.innerWidth;
      const vh = window.innerHeight;
      const w = r.width;
      const h = r.height;
      const left = Math.max(M, Math.min(x - w / 2, vw - w - M));
      let top = y - h - 14; // preferred: above the pin
      if (top < M) top = y + 16; // not enough room above → drop below the pin
      if (top + h > vh - M) top = Math.max(M, vh - h - M); // still overflowing → clamp
      setStyle({ left, top, visibility: 'visible' });
    };
    place();
    let ro;
    if (typeof ResizeObserver !== 'undefined') { ro = new ResizeObserver(place); ro.observe(el); }
    return () => { if (ro) ro.disconnect(); };
  }, [x, y]);

  return <div className="map-popup-anchor" ref={ref} style={style}>{children}</div>;
}
MapPopupFrame.propTypes = { x: PropTypes.number, y: PropTypes.number, children: PropTypes.node };

export {
  NodeCameraPopup, MapPopupFrame,
  eventSnapshotSrc, recordingStreamSrc, eventHasFootage, shortAgo, SEV_RANK,
};
