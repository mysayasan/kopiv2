import { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from 'react';
import PropTypes from 'prop-types';
import { useT, Ico, Tabs } from '@shared';
import { api, apiBase } from '../lib/helpers';
import { nodeTone, nodeToneKey, TONES } from '../lib/fleet_status';
import { NodeFloorView, CameraWindow, MediaWindow } from './node_floor_view';

// OpenLayers, driven directly through refs (no React wrapper — see the note in Phase 0).
import Map from 'ol/Map.js';
import View from 'ol/View.js';
import VectorTileLayer from 'ol/layer/VectorTile.js';
import VectorLayer from 'ol/layer/Vector.js';
import VectorSource from 'ol/source/Vector.js';
import Cluster from 'ol/source/Cluster.js';
import Feature from 'ol/Feature.js';
import Point from 'ol/geom/Point.js';
import Translate from 'ol/interaction/Translate.js';
import { fromLonLat, toLonLat } from 'ol/proj.js';
import { Fill, Stroke, Style, Circle as CircleStyle, Text } from 'ol/style.js';
import { getVectorContext } from 'ol/render.js';
import { PMTilesVectorSource } from 'ol-pmtiles';
import 'ol/ol.css';
import '../styles/fleet-map.css';

const DEFAULT_CENTER = [109.45, 4.15];
const DEFAULT_ZOOM = 6;

// Same-origin control-plane URLs for a node event's annotated snapshot and its recorded clip.
// Both route through myseliasan's node proxy / recording-stream, so the browser never contacts
// the node directly (mirrors the Notifications page).
const eventSnapshotSrc = (nodeId, alertId) =>
  `${apiBase()}/api/nodes/${encodeURIComponent(nodeId)}/proxy/api/vision/alerts/${alertId}/snapshot?annotated=1`;
const recordingStreamSrc = (nodeId, segId) =>
  `${apiBase()}/api/nodes/${encodeURIComponent(nodeId)}/recording-stream/${segId}`;
// A node event carries reviewable footage when it's a mymatasan AI detection (alert_event + refId).
const eventHasFootage = (e) => e && e.refType === 'alert_event' && Number(e.refId) > 0;

// Basemap cartography — plain OL styles keyed on the Protomaps layer name (see Phase 0).
const BASE_STYLES = {
  earth: new Style({ fill: new Fill({ color: '#f3f1ec' }) }),
  landcover: new Style({ fill: new Fill({ color: '#e6ece1' }) }),
  landuse: new Style({ fill: new Fill({ color: '#e9ece4' }) }),
  water: new Style({ fill: new Fill({ color: '#b9d7e6' }) }),
  buildings: new Style({ fill: new Fill({ color: '#e2ded6' }) }),
  roads: new Style({ stroke: new Stroke({ color: '#ffffff', width: 1.2 }) }),
  boundaries: new Style({ stroke: new Stroke({ color: '#c3bdb3', width: 1, lineDash: [3, 3] }) }),
};
function basemapStyle(feature) {
  return BASE_STYLES[feature.get('layer')] || null;
}

// Severity → badge colour (canvas, so concrete hex).
const SEV_COLOR = { critical: '#ef4444', warning: '#f59e0b', info: '#3b82f6' };

// hexToRgba turns a #rrggbb tone colour into an rgba() string at the given alpha, so the beacon
// ring can fade out as it expands.
function hexToRgba(hex, alpha) {
  const h = hex.replace('#', '');
  const r = parseInt(h.slice(0, 2), 16);
  const g = parseInt(h.slice(2, 4), 16);
  const b = parseInt(h.slice(4, 6), 16);
  return `rgba(${r}, ${g}, ${b}, ${alpha})`;
}

// easeOutCubic makes each ring shoot out quickly then ease as it fades — a more natural, modern
// pulse than a linear expansion.
const easeOutCubic = (t) => 1 - Math.pow(1 - t, 3);

// Beacon timing: one ring is born, expands, and fades every PULSE_PERIOD ms; a second ring runs
// half a period behind so a wave is always in flight (continuous, not a blip).
const PULSE_PERIOD = 1800;

// pinStyle renders a placed node. A single node shows a tone-coloured pin and an optional
// notification count badge (top-right). The critical "beacon" ring is NOT drawn here — it is
// animated every frame in the layer's prerender handler (see boot) so it stays perfectly smooth.
// A cluster of several shows a neutral disc with the count and the worst tone.
function pinStyle(clusterFeature) {
  const members = clusterFeature.get('features') || [];
  if (members.length === 1) {
    const f = members[0];
    const tone = f.get('tone') || TONES.idle;
    const notif = f.get('notif'); // { count, sev } | undefined
    const styles = [];
    // Main pin.
    styles.push(new Style({ image: new CircleStyle({ radius: 8, fill: new Fill({ color: tone.color }), stroke: new Stroke({ color: '#ffffff', width: 2 }) }) }));
    // Notification count badge (top-right; OL displacement y is up-positive).
    if (notif && notif.count > 0) {
      styles.push(new Style({ image: new CircleStyle({ radius: 7, fill: new Fill({ color: SEV_COLOR[notif.sev] || '#ef4444' }), stroke: new Stroke({ color: '#ffffff', width: 1.5 }), displacement: [10, 10] }) }));
      styles.push(new Style({ text: new Text({ text: notif.count > 99 ? '99+' : String(notif.count), font: '700 9px system-ui, sans-serif', fill: new Fill({ color: '#ffffff' }), offsetX: 10, offsetY: -10 }) }));
    }
    return styles;
  }
  const order = ['critical', 'warning', 'online', 'idle'];
  let worst = 'idle';
  let totalNotif = 0;
  for (const m of members) {
    const k = m.get('toneKey') || 'idle';
    if (order.indexOf(k) < order.indexOf(worst)) worst = k;
    const n = m.get('notif');
    if (n) totalNotif += n.count;
  }
  const tone = TONES[worst];
  const styles = [new Style({
    image: new CircleStyle({ radius: 13, fill: new Fill({ color: 'rgba(255,255,255,0.92)' }), stroke: new Stroke({ color: tone.ring, width: 3 }) }),
    text: new Text({ text: String(members.length), font: '600 12px system-ui, sans-serif', fill: new Fill({ color: '#334155' }) }),
  })];
  if (totalNotif > 0) {
    styles.push(new Style({ image: new CircleStyle({ radius: 7, fill: new Fill({ color: '#ef4444' }), stroke: new Stroke({ color: '#ffffff', width: 1.5 }), displacement: [13, 13] }) }));
    styles.push(new Style({ text: new Text({ text: totalNotif > 99 ? '99+' : String(totalNotif), font: '700 9px system-ui, sans-serif', fill: new Fill({ color: '#ffffff' }), offsetX: 13, offsetY: -13 }) }));
  }
  return styles;
}

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
  const isCamera = (node.kind || 'camera') !== 'iot';
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

// FleetMap is the geographic fleet view. Nodes appear as status pins over the offline basemap.
// PLACING a node is click-first (the discoverable path): pick a node in the side list, then
// click its spot on the map. Dragging a node from the list, and dragging an existing pin to
// move it, both still work for power users. Clicking a placed pin opens its cameras.
export function FleetMap({ nodes = [], reloadNodes, onToast, onOpenNode }) {
  const t = useT();
  const containerRef = useRef(null);
  const mapRef = useRef(null);
  const pointSourceRef = useRef(null);
  const [mapReady, setMapReady] = useState(false);
  const [state, setState] = useState('loading'); // loading | ready | nobasemap
  const [attribution, setAttribution] = useState('');
  // The node the operator picked to place (click-to-place mode), mirrored to a ref so the OL
  // click handler — set up once — always reads the live value.
  const [placing, setPlacing] = useState(null);
  const placingRef = useRef(null);
  placingRef.current = placing;
  // Camera popup: the node whose pin was clicked + where to anchor the card.
  const [popup, setPopup] = useState(null); // { node, x, y }
  // Drill-down: the node whose floor plan is open (with its cameras), or null for the map.
  const [drill, setDrill] = useState(null); // { node, floorplans }
  // Live footage windows: several can be open at once (each its own floating window). Opened
  // from a camera click in EITHER the quick popup or the floor plan — a camera never opens the
  // full camera page.
  const [liveWindows, setLiveWindows] = useState([]); // [{ key, nodeId, cameraId, name, x, y }]
  // Recorded-footage windows (event snapshot + optional clip), same floating/draggable model.
  const [mediaWindows, setMediaWindows] = useState([]); // [{ key, name, snapshotSrc, clipSrc, x, y }]
  const [iceServers, setIceServers] = useState([]);
  // Unread notification tally per node: { [nodeId]: { count, sev } }. Drives the pin badges +
  // blink. Refreshed on a timer so a new alert lights up the map without a page reload.
  const [notifByNode, setNotifByNode] = useState({});
  const pinLayerRef = useRef(null);
  const notifReloadRef = useRef(null); // the notif-tally loader, so an ack can refresh pin badges now
  const nowSec = Math.floor(Date.now() / 1000);

  useEffect(() => {
    let live = true;
    api('/api/node-stream/config', { noRedirect: true })
      .then((r) => { if (live && r.ok && Array.isArray(r.body?.iceServers)) setIceServers(r.body.iceServers); })
      .catch(() => {});
    return () => { live = false; };
  }, []);

  // Per-node unread-notification tally, refreshed every 20s.
  useEffect(() => {
    let live = true;
    const worse = (a, b) => {
      const rank = { critical: 3, warning: 2, info: 1 };
      return (rank[b] || 0) > (rank[a] || 0) ? b : a;
    };
    const load = () => api('/api/notifications?unread=true&limit=500', { noRedirect: true })
      .then((r) => {
        if (!live || !r.ok) return;
        const rows = Array.isArray(r.body?.items) ? r.body.items : (Array.isArray(r.body) ? r.body : []);
        const map = {};
        rows.forEach((n) => {
          const src = n.source || '';
          if (src.indexOf('node:') !== 0) return;
          const nid = src.slice(5);
          const cur = map[nid] || { count: 0, sev: 'info' };
          cur.count += 1;
          cur.sev = worse(cur.sev, (n.severity || 'info').toLowerCase());
          map[nid] = cur;
        });
        setNotifByNode(map);
      })
      .catch(() => {});
    notifReloadRef.current = load;
    load();
    const iv = setInterval(load, 20000);
    return () => { live = false; clearInterval(iv); };
  }, []);


  // Open a live footage window for a camera near the click (x, y = viewport coords). Adds a new
  // floating window; a camera already open is left as-is (not duplicated). The popup stays open
  // (it only closes on the map click / its own close button) so several cameras can be opened.
  const playCamera = useCallback((payload, x, y) => {
    const key = `${payload.nodeId}::${payload.cameraId}`;
    setLiveWindows((cur) => {
      if (cur.some((w) => w.key === key)) return cur; // already open — don't duplicate
      const offset = cur.length * 28; // cascade so a new window doesn't land exactly on another
      return [...cur, { key, ...payload, x: (x || 0) + offset, y: (y || 0) + offset }];
    });
  }, []);
  const closeLive = useCallback((key) => setLiveWindows((cur) => cur.filter((w) => w.key !== key)), []);

  // Open a recorded-footage window for an event (snapshot + clip if one exists) near the click.
  // The clip is resolved on demand: fetch the node's recording segments and match the one whose
  // alertId is this event's refId (same resolution the Notifications page uses). Snapshot always
  // shows; the clip toggle appears only when a segment was found.
  const openMedia = useCallback(async (payload, x, y) => {
    const key = `media:${payload.nodeId}::${payload.alertId}`;
    let clipSrc = '';
    try {
      const r = await api(`/api/nodes/${encodeURIComponent(payload.nodeId)}/proxy/api/recording/segments?limit=500&offset=0`, { noRedirect: true });
      const list = r.ok ? (Array.isArray(r.body) ? r.body : (r.body?.items || [])) : [];
      const seg = list.find((s) => Number(s.alertId) === Number(payload.alertId) && s.id);
      if (seg) clipSrc = recordingStreamSrc(payload.nodeId, seg.id);
    } catch (_) { /* snapshot-only if segments can't be resolved */ }
    setMediaWindows((cur) => {
      if (cur.some((w) => w.key === key)) return cur; // already open — don't duplicate
      const offset = cur.length * 28;
      return [...cur, {
        key,
        name: payload.name,
        snapshotSrc: eventSnapshotSrc(payload.nodeId, payload.alertId),
        clipSrc,
        x: (x || 0) + offset,
        y: (y || 0) + offset,
      }];
    });
  }, []);
  const closeMedia = useCallback((key) => setMediaWindows((cur) => cur.filter((w) => w.key !== key)), []);

  // Locate a camera physically: from an event, open the floor plan the camera is placed on with
  // its marker highlighted — the "where did this happen" jump. If the camera isn't on any plan,
  // say so rather than opening an empty view.
  const locateOnPlan = useCallback(async (payload) => {
    try {
      const res = await api(`/api/node-floorplan/${encodeURIComponent(payload.nodeId)}`);
      const plans = res.ok && Array.isArray(res.body) ? res.body.filter((p) => p && p.floor) : [];
      const cid = String(payload.cameraId);
      const onPlan = plans.some((p) => (p.placements || []).some((pl) => String(pl.cameraId) === cid));
      if (!onPlan) { if (onToast) onToast(t('map.notOnPlan'), 'info'); return; }
      const node = nodes.find((n) => n.nodeId === payload.nodeId) || { nodeId: payload.nodeId, name: payload.name };
      setPopup(null);
      setLiveWindows([]); // clear floating windows so the highlighted marker on the plan is unobstructed
      setMediaWindows([]);
      setDrill({ node, floorplans: plans, focusCameraId: payload.cameraId });
    } catch (_) { if (onToast) onToast(t('map.error'), 'error'); }
  }, [nodes, onToast, t]);

  // Clicking a node opens its floor plan directly in the map panel (with a Back button) when it
  // has one — the cameras-on-a-plan view is the richer surface. When the node has NO floor plan,
  // fall back to the quick summary popup (status + events + cameras). Mirrored to a ref for the
  // once-bound OL click handler.
  const openNode = useCallback(async (node, px) => {
    let plans = [];
    try {
      const res = await api(`/api/node-floorplan/${encodeURIComponent(node.nodeId)}`);
      plans = res.ok && Array.isArray(res.body) ? res.body.filter((p) => p && p.floor) : [];
    } catch (_) { /* treat as no plan */ }
    if (plans.length > 0) {
      setPopup(null);
      setDrill({ node, floorplans: plans });
    } else {
      // px is relative to the map container; convert to viewport coords so the popup frame can
      // clamp against the window and never clip off the top/edges.
      const rect = containerRef.current ? containerRef.current.getBoundingClientRect() : { left: 0, top: 0 };
      setPopup({ node, x: Math.round(rect.left + px[0]), y: Math.round(rect.top + px[1]), floorplans: [] });
    }
  }, []);
  const openNodeRef = useRef(openNode);
  openNodeRef.current = openNode;

  const placed = useMemo(() => nodes.filter((n) => n && n.mapPlaced), [nodes]);
  const unplaced = useMemo(() => nodes.filter((n) => n && !n.mapPlaced), [nodes]);

  const persistPosition = useCallback(async (nodeId, lon, lat) => {
    try {
      const res = await api(`/api/nodes/${nodeId}/position`, { method: 'PUT', body: JSON.stringify({ lat, lon, placed: true }) });
      if (!res.ok) throw new Error(res.message || 'save failed');
      if (reloadNodes) reloadNodes();
    } catch (e) {
      if (onToast) onToast(t('map.saveFailed'), 'error');
      if (reloadNodes) reloadNodes();
    }
  }, [reloadNodes, onToast, t]);

  // Build the map once.
  useEffect(() => {
    let cancelled = false;
    let resizeObs = null;
    let fitTimers = [];
    let onWinResize = null;
    async function boot() {
      let info = null;
      try { const res = await api('/api/basemap/info'); info = res.ok ? res.body : null; } catch (_) { info = null; }
      if (cancelled || !containerRef.current) return;

      const layers = [];
      if (info?.available) {
        setAttribution(info.attribution || '');
        layers.push(new VectorTileLayer({
          declutter: true,
          source: new PMTilesVectorSource({ url: `${apiBase()}/api/basemap/tiles.pmtiles`, attributions: info.attribution || undefined }),
          style: basemapStyle,
        }));
      }
      const pointSource = new VectorSource();
      pointSourceRef.current = pointSource;
      const clusterSource = new Cluster({ distance: 36, minDistance: 18, source: pointSource });
      const pinLayer = new VectorLayer({ source: clusterSource, style: pinStyle });
      pinLayerRef.current = pinLayer;
      layers.push(pinLayer);

      // Beacon: draw the critical pins' pulse BEHIND the dots, redrawn every frame so it is
      // perfectly smooth. Using prerender + getVectorContext (rather than an animated style
      // function, which OL caches and won't re-evaluate per frame) is the reliable OL way to
      // animate. The handler re-requests a render only while a critical pin is on screen, so an
      // all-clear map settles to static and burns no frames.
      pinLayer.on('prerender', (evt) => {
        const vctx = getVectorContext(evt);
        const time = evt.frameState ? evt.frameState.time : 0;
        const base = (time % PULSE_PERIOD) / PULSE_PERIOD;
        let animating = false;
        for (const cf of clusterSource.getFeatures()) {
          const members = cf.get('features') || [];
          if (members.length !== 1) continue;
          if (!members[0].get('critical')) continue;
          animating = true;
          const color = (members[0].get('tone') || TONES.idle).ring;
          const geom = cf.getGeometry();
          for (const offset of [0, 0.5]) {
            const p = (base + offset) % 1;
            const grown = easeOutCubic(p);
            const alpha = 0.5 * (1 - p);
            if (alpha <= 0.01) continue;
            vctx.setStyle(new Style({ image: new CircleStyle({ radius: 9 + grown * 15, stroke: new Stroke({ color: hexToRgba(color, alpha), width: 2.5 - grown }) }) }));
            vctx.drawGeometry(geom);
          }
        }
        if (animating && mapRef.current) mapRef.current.render();
      });

      const map = new Map({
        target: containerRef.current,
        layers,
        view: new View({ center: fromLonLat(DEFAULT_CENTER), zoom: DEFAULT_ZOOM }),
        controls: [],
      });
      mapRef.current = map;

      // Fill the panel. OpenLayers measures the container ONCE at creation, but the flex panel
      // often isn't at its final size yet (and the panel can resize later, or be laid out after
      // the tab is shown) — so without re-measuring the map renders blank until a zoom forces it.
      // Re-fit aggressively: next frame, a few short delays (late layout / fonts / tab reveal), on
      // container resize (ResizeObserver), and on window resize — so the map fills like any GIS.
      const fit = () => { if (mapRef.current) mapRef.current.updateSize(); };
      fit();
      requestAnimationFrame(fit);
      fitTimers = [setTimeout(fit, 150), setTimeout(fit, 400), setTimeout(fit, 900)];
      onWinResize = fit;
      window.addEventListener('resize', onWinResize);
      if (typeof ResizeObserver !== 'undefined' && containerRef.current) {
        resizeObs = new ResizeObserver(fit);
        resizeObs.observe(containerRef.current);
      }

      // Drag an existing pin to reposition.
      const translate = new Translate({ layers: [pinLayer], hitTolerance: 5 });
      translate.on('translateend', (evt) => {
        const clusterFeature = evt.features.item(0);
        if (!clusterFeature) return;
        const members = clusterFeature.get('features') || [];
        if (members.length !== 1) { if (onToast) onToast(t('map.zoomToMove'), 'info'); clusterSource.refresh(); return; }
        const node = members[0].get('node');
        const coord = clusterFeature.getGeometry().getCoordinates();
        members[0].getGeometry().setCoordinates(coord);
        const [lon, lat] = toLonLat(coord);
        persistPosition(node.nodeId, lon, lat);
      });
      map.addInteraction(translate);

      // Single click does one of two things depending on mode:
      //  - placing mode: drop the picked node at the clicked spot.
      //  - normal: if a single-node pin was hit, open its camera popup; else close any popup.
      map.on('singleclick', (evt) => {
        const target = placingRef.current;
        if (target) {
          const [lon, lat] = toLonLat(evt.coordinate);
          persistPosition(target.nodeId, lon, lat);
          setPlacing(null);
          if (onToast) onToast(t('map.placed', { name: target.name || target.nodeId }), 'success');
          return;
        }
        const feat = map.forEachFeatureAtPixel(evt.pixel, (f) => f, { hitTolerance: 5 });
        const members = feat ? (feat.get('features') || []) : [];
        if (members.length === 1) {
          const node = members[0].get('node');
          const px = map.getPixelFromCoordinate(evt.coordinate);
          openNodeRef.current(node, px);
        } else {
          setPopup(null);
        }
      });
      // Close the popup when the map moves (it would otherwise float away from its pin).
      map.on('movestart', () => setPopup(null));

      setState(info?.available ? 'ready' : 'nobasemap');
      setMapReady(true);
    }
    boot();
    return () => {
      cancelled = true;
      fitTimers.forEach(clearTimeout);
      if (onWinResize) window.removeEventListener('resize', onWinResize);
      if (resizeObs) { resizeObs.disconnect(); resizeObs = null; }
      if (mapRef.current) { mapRef.current.setTarget(undefined); mapRef.current = null; }
      pointSourceRef.current = null;
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  // Re-populate pins whenever the placed set changes.
  useEffect(() => {
    if (!mapReady) return;
    const src = pointSourceRef.current;
    if (!src) return;
    src.clear();
    src.addFeatures(placed.map((n) => {
      const f = new Feature({ geometry: new Point(fromLonLat([n.lon, n.lat])) });
      const toneKey = nodeToneKey(n, nowSec);
      const notif = notifByNode[n.nodeId];
      f.set('node', n);
      f.set('tone', nodeTone(n, nowSec));
      f.set('toneKey', toneKey);
      f.set('notif', notif);
      // Blink when the node itself is critical (lost) or it has an unread critical event.
      f.set('critical', toneKey === 'critical' || (notif && notif.sev === 'critical'));
      return f;
    }));
  }, [placed, nowSec, mapReady, notifByNode]);

  // Escape cancels placing mode.
  useEffect(() => {
    if (!placing) return undefined;
    const onKey = (e) => { if (e.key === 'Escape') setPlacing(null); };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, [placing]);

  // Drop a node dragged from the rail onto the map (power-user shortcut alongside click-place).
  function onDrop(e) {
    e.preventDefault();
    const nodeId = e.dataTransfer.getData('text/node-id');
    const map = mapRef.current;
    if (!nodeId || !map) return;
    const coord = map.getEventCoordinate(e.nativeEvent || e);
    const [lon, lat] = toLonLat(coord);
    persistPosition(nodeId, lon, lat);
    setPlacing(null);
  }

  return (
    <section className="settings-panel span-two fleet-map-panel">
      <header>
        <h2><span className="btn-icon"><Ico n="map" /> {t('map.title')}</span></h2>
        <div className="fleet-map-legend" aria-hidden="true">
          {['online', 'warning', 'critical', 'idle'].map((k) => (
            <span key={k} className="legend-item">
              <span className="legend-dot" style={{ background: TONES[k].color }} />
              {t(`map.legend.${k}`)}
            </span>
          ))}
        </div>
      </header>
      <p className="settings-hint">{t('map.geoHint')}</p>
      {state === 'nobasemap' ? <p className="settings-hint">{t('map.noBasemap')}</p> : null}

      <div className="fleet-map-body">
        <aside className="fleet-map-rail">
          <div className="fleet-map-rail-head">{t('map.toPlace')} <span className="count-badge">{unplaced.length}</span></div>
          {unplaced.length === 0 ? (
            <div className="fleet-map-rail-empty"><Ico n="check-ok" sz={22} /><span>{t('map.allPlaced')}</span></div>
          ) : (
            <>
              <div className="fleet-map-rail-hint">{t('map.placeHint')}</div>
              <ul className="fleet-map-rail-list">
                {unplaced.map((n) => {
                  const active = placing && placing.nodeId === n.nodeId;
                  return (
                    <li key={n.nodeId}>
                      <button
                        type="button"
                        className={`fleet-map-rail-node${active ? ' active' : ''}`}
                        draggable
                        onDragStart={(e) => { e.dataTransfer.setData('text/node-id', n.nodeId); e.dataTransfer.effectAllowed = 'move'; setPlacing(null); }}
                        onClick={() => setPlacing(active ? null : n)}
                        title={t('map.placeHint')}
                      >
                        <span className="rail-dot" style={{ background: nodeTone(n, nowSec).color }} />
                        <span className="rail-name">{n.name || n.nodeId}</span>
                        {active ? <Ico n="map-pin" sz={14} /> : null}
                      </button>
                    </li>
                  );
                })}
              </ul>
            </>
          )}
        </aside>

        <div className="fleet-map-stage">
          {placing ? (
            <div className="fleet-map-placing" role="status">
              <span><Ico n="map-pin" sz={14} /> {t('map.placingBanner', { name: placing.name || placing.nodeId })}</span>
              <button type="button" className="linklike" onClick={() => setPlacing(null)}>{t('map.cancel')}</button>
            </div>
          ) : null}
          <div
            className={`fleet-map-canvas${placing ? ' placing' : ''}`}
            ref={containerRef}
            role="application"
            aria-label={t('map.title')}
            onDragOver={(e) => { e.preventDefault(); e.dataTransfer.dropEffect = 'move'; }}
            onDrop={onDrop}
          />
          {popup ? (
            <MapPopupFrame x={popup.x} y={popup.y}>
              <NodeCameraPopup
                node={popup.node}
                nowSec={nowSec}
                onOpenNode={onOpenNode}
                onPlay={playCamera}
                onOpenMedia={openMedia}
                onLocate={locateOnPlan}
                onAck={() => { if (notifReloadRef.current) notifReloadRef.current(); }}
                onClose={() => setPopup(null)}
              />
            </MapPopupFrame>
          ) : null}
          {drill ? (
            <div className="fleet-map-drill">
              <NodeFloorView node={drill.node} floorplans={drill.floorplans} focusCameraId={drill.focusCameraId} onBack={() => setDrill(null)} onPlay={playCamera} />
            </div>
          ) : null}
        </div>
      </div>
      {attribution ? <div className="fleet-map-attribution">{attribution}</div> : null}

      {/* Floating live windows are position:fixed (viewport-anchored), so they live at the
          top of the tree, outside the map stage. */}
      {liveWindows.map((w) => (
        <CameraWindow
          key={w.key}
          nodeId={w.nodeId}
          cameraId={w.cameraId}
          name={w.name}
          iceServers={iceServers}
          ptzSupported={w.ptzSupported}
          x={w.x}
          y={w.y}
          onOpenMedia={openMedia}
          onLocate={locateOnPlan}
          onClose={() => closeLive(w.key)}
        />
      ))}
      {mediaWindows.map((w) => (
        <MediaWindow
          key={w.key}
          name={w.name}
          snapshotSrc={w.snapshotSrc}
          clipSrc={w.clipSrc}
          x={w.x}
          y={w.y}
          onClose={() => closeMedia(w.key)}
        />
      ))}
    </section>
  );
}

FleetMap.propTypes = {
  nodes: PropTypes.array,
  reloadNodes: PropTypes.func,
  onToast: PropTypes.func,
  onOpenNode: PropTypes.func,
};
