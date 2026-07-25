import { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from 'react';
import PropTypes from 'prop-types';
import { useT, Ico, Tabs } from '@shared';
import { api, apiBase } from '../lib/helpers';
import { nodeTone, nodeToneKey, TONES } from '../lib/fleet_status';
import { BuildingFloorView, CameraWindow, MediaWindow } from './node_floor_view';
import { AssetWizard, SiteDialog } from './asset_wizard';
import { BuildingEditorDialog } from './building_editor_dialog';
import { KIND_BUILDING, KIND_OUTDOOR, KIND_POINT, KIND_ORDER, normKind, hasPlans, siteGlyph } from './site_kinds';

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
import { getCenter } from 'ol/extent.js';
import { Fill, Stroke, Style, Circle as CircleStyle, RegularShape, Text } from 'ol/style.js';
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
};
// Admin borders: a firmer, near-solid line for country outlines and the old faint dashes for
// internal (state/region) boundaries — so the eye can tell a national border from a state line.
const COUNTRY_BORDER = new Style({ stroke: new Stroke({ color: '#a9a294', width: 1.4 }) });
const REGION_BORDER = new Style({ stroke: new Stroke({ color: '#c3bdb3', width: 1, lineDash: [3, 3] }) });

// Label styles. Each is a reused singleton whose text we set per feature before returning it —
// the standard OpenLayers idiom (the renderer reads the text synchronously), which avoids
// allocating a Style/Text per feature per frame. A white halo keeps every label legible over
// land, water, or roads. `declutter: true` on the layer drops labels that would overlap.
const halo = (w) => new Stroke({ color: 'rgba(255,255,255,0.9)', width: w });
const COUNTRY_LABEL = new Style({ text: new Text({ font: '700 12px system-ui, sans-serif', fill: new Fill({ color: '#3f3d38' }), stroke: halo(3), overflow: true }) });
const REGION_LABEL = new Style({ text: new Text({ font: '600 11px system-ui, sans-serif', fill: new Fill({ color: '#6b6a63' }), stroke: halo(2.5), overflow: true }) });
const CITY_LABEL = new Style({
  image: new CircleStyle({ radius: 2.6, fill: new Fill({ color: '#5b5a54' }), stroke: new Stroke({ color: '#ffffff', width: 1 }) }),
  text: new Text({ font: '500 11px system-ui, sans-serif', fill: new Fill({ color: '#33322e' }), stroke: halo(2.5), offsetY: -10 }),
});
const WATER_LABEL = new Style({ text: new Text({ font: 'italic 400 11px system-ui, sans-serif', fill: new Fill({ color: '#3d6b85' }), stroke: halo(2), overflow: true }) });
const ROAD_LABEL = new Style({ text: new Text({ font: '500 10px system-ui, sans-serif', fill: new Fill({ color: '#5a5852' }), stroke: halo(2.5), placement: 'line', maxAngle: 0.6 }) });

// Web-Mercator resolution → approximate tile zoom, so the style function can gate labels by zoom
// (the style function only receives a resolution, not the view zoom).
const R0 = 156543.03392804097;
const zoomForResolution = (res) => Math.log2(R0 / res);
// Prefer the English name, falling back to the native name the tile carries.
const placeLabel = (f) => f.get('name:en') || f.get('name') || '';

// basemapStyle paints one basemap vector-tile feature. Fills/lines come from the layer name;
// the `places`, `water`, and `roads` layers additionally carry names, which we render as text so
// the map reads like a real map (countries, states, cities) instead of blank shapes.
function basemapStyle(feature, resolution) {
  const layer = feature.get('layer');
  const zoom = zoomForResolution(resolution);

  if (layer === 'boundaries') {
    return feature.get('kind') === 'country' ? COUNTRY_BORDER : REGION_BORDER;
  }

  if (layer === 'places') {
    const label = placeLabel(feature);
    if (!label) return null;
    // Each place point carries the zoom at which it should first appear; honour it so we don't
    // splatter every village across a zoomed-out view (declutter then thins whatever remains).
    const minZoom = feature.get('min_zoom');
    if (typeof minZoom === 'number' && zoom + 0.4 < minZoom) return null;
    const kind = feature.get('kind');
    if (kind === 'country') { COUNTRY_LABEL.getText().setText(label.toUpperCase()); return COUNTRY_LABEL; }
    if (kind === 'region') { REGION_LABEL.getText().setText(label.toUpperCase()); return REGION_LABEL; }
    if (kind === 'locality') {
      // Bump the biggest cities up a size so a capital reads before a small town.
      const big = (feature.get('population_rank') || 0) >= 11;
      const txt = CITY_LABEL.getText();
      txt.setFont(big ? '600 12px system-ui, sans-serif' : '500 11px system-ui, sans-serif');
      txt.setText(label);
      return CITY_LABEL;
    }
    return null; // neighbourhoods, etc. — left off to keep the map calm
  }

  if (layer === 'water') {
    const fill = BASE_STYLES.water;
    const label = placeLabel(feature);
    if (label && zoom >= 5) { WATER_LABEL.getText().setText(label); return [fill, WATER_LABEL]; }
    return fill;
  }

  if (layer === 'roads') {
    const base = BASE_STYLES.roads;
    const kind = feature.get('kind');
    const label = feature.get('name:en') || feature.get('name') || feature.get('ref');
    if (label && zoom >= 11 && (kind === 'highway' || kind === 'major_road')) {
      ROAD_LABEL.getText().setText(label);
      return [base, ROAD_LABEL];
    }
    return base;
  }

  return BASE_STYLES[layer] || null;
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
  const hover = clusterFeature.get('hover');
  if (members.length === 1) {
    const f = members[0];
    const tone = f.get('tone') || TONES.idle;
    const notif = f.get('notif'); // { count, sev } | undefined
    const styles = [];
    if (hover) styles.push(new Style({ image: new CircleStyle({ radius: 15, fill: new Fill({ color: hexToRgba(HOVER, 0.16) }) }) }));
    // Main pin.
    styles.push(new Style({ image: new CircleStyle({ radius: hover ? 9 : 8, fill: new Fill({ color: tone.color }), stroke: new Stroke({ color: hover ? HOVER : '#ffffff', width: hover ? 3 : 2 }) }) }));
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
  const styles = [];
  if (hover) styles.push(new Style({ image: new CircleStyle({ radius: 18, fill: new Fill({ color: hexToRgba(HOVER, 0.16) }) }) }));
  styles.push(new Style({
    image: new CircleStyle({ radius: hover ? 14 : 13, fill: new Fill({ color: 'rgba(255,255,255,0.92)' }), stroke: new Stroke({ color: hover ? HOVER : tone.ring, width: 3 }) }),
    text: new Text({ text: String(members.length), font: '600 12px system-ui, sans-serif', fill: new Fill({ color: '#334155' }) }),
  }));
  if (totalNotif > 0) {
    styles.push(new Style({ image: new CircleStyle({ radius: 7, fill: new Fill({ color: '#ef4444' }), stroke: new Stroke({ color: '#ffffff', width: 1.5 }), displacement: [13, 13] }) }));
    styles.push(new Style({ text: new Text({ text: totalNotif > 99 ? '99+' : String(totalNotif), font: '700 9px system-ui, sans-serif', fill: new Fill({ color: '#ffffff' }), offsetX: 13, offsetY: -13 }) }));
  }
  return styles;
}

// BUILDING_GLYPH_FONT prefers the platform colour-emoji font so a chosen asset icon renders in
// colour on the OL canvas (Windows/Chrome/macOS all ship one).
const BUILDING_GLYPH_FONT = '15px "Segoe UI Emoji", "Noto Color Emoji", "Apple Color Emoji", system-ui, sans-serif';
const HOVER = '#2f6bd6'; // accent used for the hover/selection highlight ring + halo

// markerShape draws a site marker in the SHAPE of its kind: a disc for a building, a square for an
// outdoor area, a diamond for a point asset. Zoomed out past the point where the name label is
// legible, the silhouette is the only thing left to tell a park from an office block — so the shape
// carries the kind, and the glyph inside carries the specifics.
//
// The radii are tuned so the three read as the same visual weight: a square of radius r covers more
// area than a disc of radius r, and a diamond covers less, hence the multipliers.
function markerShape(kind, radius, fill, stroke) {
  const opts = { fill, stroke };
  if (kind === KIND_OUTDOOR) return new RegularShape({ ...opts, points: 4, angle: Math.PI / 4, radius: radius * 1.06 });
  if (kind === KIND_POINT) return new RegularShape({ ...opts, points: 4, angle: 0, radius: radius * 1.3 });
  return new CircleStyle({ ...opts, radius });
}

// siteStyle renders a placed site: the operator's chosen glyph on a white marker ringed in the worst
// owning-node tone (so status still reads at a glance), shaped by the site's kind, the name below,
// and a camera-count badge.
function siteStyle(feature) {
  const tone = feature.get('tone') || TONES.idle;
  const cams = feature.get('cameras') || 0;
  const kind = normKind(feature.get('kind'));
  const icon = feature.get('icon');
  const hover = feature.get('hover');
  const styles = [];
  // Hover halo + accent ring — a clear "this is clickable" affordance for the canvas marker.
  if (hover) styles.push(new Style({ image: markerShape(kind, 19, new Fill({ color: hexToRgba(HOVER, 0.16) }), undefined) }));
  styles.push(new Style({ image: markerShape(kind, hover ? 15 : 14, new Fill({ color: '#ffffff' }), new Stroke({ color: hover ? HOVER : tone.ring, width: hover ? 4 : 3 })) }));
  styles.push(new Style({ text: new Text({ text: icon, font: BUILDING_GLYPH_FONT }) }));
  styles.push(new Style({ text: new Text({ text: feature.get('name') || '', offsetY: 24, font: '600 11px system-ui, sans-serif', fill: new Fill({ color: '#1f2937' }), stroke: new Stroke({ color: 'rgba(255,255,255,0.85)', width: 3 }) }) }));
  // Camera badge (bottom-right): "online/total" when live health shows some are down (amber/red),
  // otherwise just the total. Surfaces a camera that dropped while its node stayed up.
  const total = feature.get('camTotal') != null ? feature.get('camTotal') : cams;
  if (total > 0) {
    const known = feature.get('camKnown') || 0;
    const online = feature.get('camOnline') || 0;
    const down = known > 0 && online < total; // we have readings and some aren't online
    const badgeColor = !down ? tone.color : (online === 0 ? '#ef4444' : '#f59e0b');
    const label = down ? `${online}/${total}` : (total > 99 ? '99+' : String(total));
    styles.push(new Style({ image: new CircleStyle({ radius: down ? 9 : 8, fill: new Fill({ color: badgeColor }), stroke: new Stroke({ color: '#ffffff', width: 1.5 }), displacement: [12, -12] }) }));
    styles.push(new Style({ text: new Text({ text: label, font: '700 9px system-ui, sans-serif', fill: new Fill({ color: '#ffffff' }), offsetX: 12, offsetY: 12 }) }));
  }
  // Unread-notification badge (top-right, severity coloured) — sum of THIS building's camera alerts.
  const notif = feature.get('notif');
  if (notif && notif.count > 0) {
    styles.push(new Style({ image: new CircleStyle({ radius: 8, fill: new Fill({ color: SEV_COLOR[notif.sev] || '#ef4444' }), stroke: new Stroke({ color: '#ffffff', width: 1.5 }), displacement: [12, 12] }) }));
    styles.push(new Style({ text: new Text({ text: notif.count > 99 ? '99+' : String(notif.count), font: '700 9px system-ui, sans-serif', fill: new Fill({ color: '#ffffff' }), offsetX: 12, offsetY: -12 }) }));
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

// SiteAssetPopup is what a POINT asset (a junction, a gate, a pole) opens: it has no plan to drill
// into, so the marker answers "what is mounted here?" instead. With exactly one appliance the map
// skips this and opens that appliance's device card directly — this card is for the none and
// several cases, where jumping to "the" appliance would be a guess.
function SiteAssetPopup({ site, nodes, nowSec, onOpenNode, onClose }) {
  const t = useT();
  return (
    <div className="mp-card" role="dialog" aria-label={site.name}>
      <div className="mp-head">
        <span className="mp-id as-static">
          <span className="mp-avatar as-glyph" aria-hidden="true">{siteGlyph(site)}</span>
          <span className="mp-title" title={site.name}>{site.name}</span>
        </span>
        <button type="button" className="icon-button mp-close" onClick={onClose} aria-label={t('nset.close')}><Ico n="x" sz={14} /></button>
      </div>
      <div className="mp-solo-head"><Ico n="cpu" sz={13} /> {t('map.appliancesHere')} {nodes.length > 0 ? <span className="mp-tab-count">{nodes.length}</span> : null}</div>
      <div className="mp-body">
        {nodes.length === 0 ? (
          <div className="mp-empty"><Ico n="cpu" sz={22} /><span>{t('map.noAppliancesHere')}</span></div>
        ) : nodes.map((n) => (
          <button key={n.nodeId} type="button" className="mp-cam" onClick={() => onOpenNode(n)}>
            <span className="cam-dot" style={{ background: nodeTone(n, nowSec).color }} />
            <span className="mp-cam-name">{n.name || n.nodeId}</span>
            <Ico n="chev-right" sz={12} />
          </button>
        ))}
      </div>
    </div>
  );
}
SiteAssetPopup.propTypes = { site: PropTypes.object, nodes: PropTypes.array, nowSec: PropTypes.number, onOpenNode: PropTypes.func, onClose: PropTypes.func };

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
  // Multi-region basemap: one PMTiles layer per downloaded region. `outside` is true when the view
  // has been panned beyond every region's coverage; with `canDownload` that offers a download.
  const [canDownload, setCanDownload] = useState(false);
  const [outside, setOutside] = useState(false);
  const [downloading, setDownloading] = useState(false);
  const [bmConfig, setBmConfig] = useState({ hasTool: false, envManaged: false, source: '' }); // download setup state
  const [setupOpen, setSetupOpen] = useState(false);
  const sourceInputRef = useRef(null);
  const basemapLayersRef = useRef({}); // region name -> VectorTileLayer
  const regionsRef = useRef([]); // live [{ name, bounds }] for the once-bound moveend handler

  // makeRegionLayer builds a basemap layer for one region archive served at /tiles/{name}.
  const makeRegionLayer = useCallback((name, attr) => new VectorTileLayer({
    declutter: true,
    source: new PMTilesVectorSource({ url: `${apiBase()}/api/basemap/tiles/${encodeURIComponent(name)}`, attributions: attr || undefined }),
    style: basemapStyle,
  }), []);
  // addRegionLayers inserts a layer (at the bottom) for any region we don't already have — used
  // after a download so the new tiles appear without rebuilding the map.
  const addRegionLayers = useCallback((regs, attr) => {
    const map = mapRef.current; if (!map) return;
    regs.forEach((rg) => {
      if (basemapLayersRef.current[rg.name]) return;
      const lyr = makeRegionLayer(rg.name, attr);
      basemapLayersRef.current[rg.name] = lyr;
      map.getLayers().insertAt(0, lyr);
    });
  }, [makeRegionLayer]);

  // Download the map data for the area currently in view (extract its bbox from the configured
  // remote source, server-side). Refreshes the region list + adds the new layer on success.
  const downloadRegion = useCallback(async () => {
    const map = mapRef.current; const size = map && map.getSize();
    if (!map || !size || downloading) return;
    const v = map.getView();
    const ext = v.calculateExtent(size);
    const [minLon, minLat] = toLonLat([ext[0], ext[1]]);
    const [maxLon, maxLat] = toLonLat([ext[2], ext[3]]);
    if ((maxLon - minLon) > 20 || (maxLat - minLat) > 20) { if (onToast) onToast(t('map.regionTooBig'), 'info'); return; }
    const maxZoom = Math.min(14, Math.max(8, Math.round(v.getZoom() || 10) + 1));
    setDownloading(true);
    try {
      const res = await api('/api/basemap/download', { method: 'POST', body: JSON.stringify({ minLon, minLat, maxLon, maxLat, maxZoom }) });
      if (!res.ok) throw new Error(res.message || 'failed');
      const inf = await api('/api/basemap/info');
      const regs = (inf.ok && Array.isArray(inf.body?.regions)) ? inf.body.regions : [];
      regionsRef.current = regs;
      addRegionLayers(regs, inf.body && inf.body.attribution);
      setOutside(false);
      if (onToast) onToast(t('map.regionDownloaded'), 'success');
    } catch (e) {
      if (onToast) onToast(t('map.regionFailed', { msg: (e && e.message) ? e.message : '' }), 'error');
    } finally { setDownloading(false); }
  }, [downloading, addRegionLayers, onToast, t]);

  // Load the download setup state (source URL + whether the pmtiles tool is installed).
  useEffect(() => {
    let live = true;
    api('/api/basemap/config', { noRedirect: true }).then((r) => { if (live && r.ok && r.body) setBmConfig(r.body); }).catch(() => {});
    return () => { live = false; };
  }, []);

  // Save the remote source URL (runtime, no restart), then refresh availability + region layers.
  const saveSource = useCallback(async (url) => {
    try {
      const res = await api('/api/basemap/config', { method: 'PUT', body: JSON.stringify({ source: url }) });
      if (!res.ok) throw new Error(res.message || 'failed');
      if (res.body) setBmConfig(res.body);
      const inf = await api('/api/basemap/info');
      if (inf.ok && inf.body) {
        setCanDownload(!!inf.body.canDownload);
        const regs = Array.isArray(inf.body.regions) ? inf.body.regions : [];
        regionsRef.current = regs;
        addRegionLayers(regs, inf.body.attribution);
      }
      setSetupOpen(false);
      if (onToast) onToast(t('map.sourceSaved'), 'success');
    } catch (e) {
      if (onToast) onToast(t('map.regionFailed', { msg: (e && e.message) ? e.message : '' }), 'error');
    }
  }, [addRegionLayers, onToast, t]);
  // The node the operator picked to place (click-to-place mode), mirrored to a ref so the OL
  // click handler — set up once — always reads the live value.
  const [placing, setPlacing] = useState(null);
  const placingRef = useRef(null);
  placingRef.current = placing;
  // Camera popup: the node whose pin was clicked + where to anchor the card.
  const [popup, setPopup] = useState(null); // { node, x, y }
  // A point asset's appliance chooser: shown when a junction/pole has none or several appliances,
  // since there is no single device card to jump straight to.
  const [sitePopup, setSitePopup] = useState(null); // { site, nodes, x, y }
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
  const [notifByCam, setNotifByCam] = useState({}); // "nodeId::cameraId" -> { count, sev } — building attribution
  const [camHealth, setCamHealth] = useState({}); // "nodeId::cameraId" -> health string ('online'|'offline'|…)
  const [camsByNode, setCamsByNode] = useState({}); // nodeId -> ["nodeId::cameraId", …] — a point asset's cameras
  const pinLayerRef = useRef(null);
  const notifReloadRef = useRef(null); // the notif-tally loader, so an ack can refresh pin badges now
  // Buildings (sites) are the OTHER thing on the map — a building is where cameras physically live,
  // independent of which node records them. sites holds the overview rows { site, nodeIds, cameras,
  // floors }; showLayers toggles the two marker layers so an operator can focus on either.
  const [sites, setSites] = useState([]);
  // Buildings-centric map: buildings are the only markers; node pins stay off.
  const [showLayers] = useState({ buildings: true, nodes: false });
  // Which building rows in the rail are expanded, and the lazily-loaded floors/areas inside each
  // (id -> { loading, error, list }). A building's children are its floor plans (1st floor, kitchen…).
  const [expandedSites, setExpandedSites] = useState({});
  const [floorsBySite, setFloorsBySite] = useState({});
  // Adding a building is a three-beat flow owned here: the wizard collects name/glyph/areas, the
  // map takes the drop point, then the editor opens on the building just created. editorSite is
  // also the re-entry point for an EXISTING building (from the rail or the drill-down).
  const [wizardOpen, setWizardOpen] = useState(false);
  const [editorSite, setEditorSite] = useState(null);
  const [renameSite, setRenameSite] = useState(null); // a point asset being renamed (it has no editor)
  const [busy, setBusy] = useState(false); // a building create/save is in flight
  const buildingSourceRef = useRef(null);
  const buildingLayerRef = useRef(null);
  const siteReloadRef = useRef(null);
  const nowSec = Math.floor(Date.now() / 1000);

  // nodesById resolves a placement's / building's owning node for its status tone.
  const nodesById = useMemo(() => { const m = {}; nodes.forEach((n) => { m[n.nodeId] = n; }); return m; }, [nodes]);

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
    // Server-side aggregate: compact (source, cameraId) groups, so we never page the whole unread
    // feed to the browser (which used to cap at 500 + bucket client-side).
    const load = () => api('/api/notifications/tally?unread=true', { noRedirect: true })
      .then((r) => {
        if (!live || !r.ok) return;
        const rows = Array.isArray(r.body) ? r.body : (Array.isArray(r.body?.items) ? r.body.items : []);
        const byNode = {};
        const byCam = {};
        rows.forEach((row) => {
          const src = row.source || '';
          if (src.indexOf('node:') !== 0) return;
          const nid = src.slice(5);
          const sev = (row.severity || 'info').toLowerCase();
          const n = Number(row.count) || 0;
          const cur = byNode[nid] || { count: 0, sev: 'info' };
          cur.count += n; cur.sev = worse(cur.sev, sev); byNode[nid] = cur;
          // Each (source, cameraId) is already one group → per-camera tally is a direct assign.
          if (row.cameraId) byCam[`${nid}::${row.cameraId}`] = { count: n, sev };
        });
        setNotifByNode(byNode);
        setNotifByCam(byCam);
      })
      .catch(() => {});
    notifReloadRef.current = load;
    load();
    const iv = setInterval(load, 20000);
    return () => { live = false; clearInterval(iv); };
  }, []);

  // Building overview (geo-located sites + their health rollup), refreshed on a slow timer so a
  // camera added/removed in the indoor tab, or a node going down, updates the building markers.
  useEffect(() => {
    let live = true;
    const load = () => api('/api/sites/overview', { noRedirect: true })
      .then((r) => { if (live && r.ok && Array.isArray(r.body)) setSites(r.body); })
      .catch(() => {});
    siteReloadRef.current = load;
    load();
    const iv = setInterval(load, 30000);
    return () => { live = false; clearInterval(iv); };
  }, []);

  // Live camera health for the cameras in PLACED buildings, so a building marker can show
  // "online / total" — a camera that drops while its NODE is still up is otherwise invisible on
  // the map (the building tone only tracks node status). Refetched whenever the overview changes
  // (~30s); one proxy call per owning node, skipped when there are no placed buildings.
  useEffect(() => {
    let live = true;
    const nodeIds = Array.from(new Set(sites.filter((s) => s.site && s.site.mapPlaced).flatMap((s) => resolvedNodeIds(s))));
    if (nodeIds.length === 0) { setCamHealth({}); setCamsByNode({}); return undefined; }
    Promise.all(nodeIds.map((nid) => api(`/api/nodes/${encodeURIComponent(nid)}/proxy/api/cameras?limit=200`, { noRedirect: true })
      .then((r) => ({ nid, list: r.ok ? (Array.isArray(r.body) ? r.body : (r.body?.items || [])) : [] }))
      .catch(() => ({ nid, list: [] }))))
      .then((results) => {
        if (!live) return;
        const m = {};
        const byNode = {};
        results.forEach(({ nid, list }) => {
          byNode[nid] = list.map((c) => `${nid}::${c.id}`);
          list.forEach((c) => { m[`${nid}::${c.id}`] = (c.healthStatus || '').toLowerCase(); });
        });
        setCamHealth(m);
        setCamsByNode(byNode);
      });
    return () => { live = false; };
  }, [sites, resolvedNodeIds]);

  // A site's cameras. A building/outdoor area knows them from its plan placements; a point asset has
  // no plan, so its cameras are simply every camera on the appliance assigned to it.
  const resolvedCamKeys = useCallback((row) => {
    const keys = row.cameraKeys || [];
    if (keys.length > 0 || !row.site || hasPlans(row.site.kind)) return keys;
    return resolvedNodeIds(row).flatMap((nid) => camsByNode[nid] || []);
  }, [resolvedNodeIds, camsByNode]);


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

  // Locate a camera physically: open the BUILDING the camera lives in, with its marker highlighted
  // — the "where did this happen" jump. A camera's physical home is its site/floor (not its node),
  // so we resolve the floor that holds it, then open that building's plan focused on the camera.
  const locateOnPlan = useCallback(async (payload) => {
    try {
      const res = await api(`/api/node-floorplan/${encodeURIComponent(payload.nodeId)}`);
      const plans = res.ok && Array.isArray(res.body) ? res.body.filter((p) => p && p.floor) : [];
      const cid = String(payload.cameraId);
      const holder = plans.find((p) => (p.placements || []).some((pl) => String(pl.cameraId) === cid));
      if (!holder) { if (onToast) onToast(t('map.notOnPlan'), 'info'); return; }
      const siteId = holder.floor.siteId;
      const site = (sites.find((s) => s.site && s.site.id === siteId) || {}).site || { id: siteId, name: t('map.viewIndoor') };
      const sres = await api(`/api/sites/${siteId}/floorplans`);
      const sitePlans = sres.ok && Array.isArray(sres.body) ? sres.body.filter((p) => p && p.floor) : plans;
      setPopup(null);
      setLiveWindows([]); // clear floating windows so the highlighted marker on the plan is unobstructed
      setMediaWindows([]);
      setDrill({ kind: 'site', site, floorplans: sitePlans, focusCameraId: payload.cameraId });
    } catch (_) { if (onToast) onToast(t('map.error'), 'error'); }
  }, [sites, onToast, t]);

  // Smoothly centre + zoom the map on a placed building (used when returning to the map from its
  // floor plan, so the eye lands on the building you were just inside).
  const flyToSite = useCallback((s) => {
    const v = mapRef.current && mapRef.current.getView();
    if (!v || !s || !s.mapPlaced || typeof s.lon !== 'number' || typeof s.lat !== 'number') return;
    v.animate({ center: fromLonLat([s.lon, s.lat]), zoom: Math.max(13, v.getZoom() || DEFAULT_ZOOM), duration: 650 });
  }, []);

  // Clicking a NODE opens its device card (status + cameras + events) — a node is an appliance, not
  // a building, so it never opens a floor plan. Mirrored to a ref for the once-bound OL handler.
  const openNode = useCallback((node, px) => {
    // px is relative to the map container; convert to viewport coords so the popup frame can clamp
    // against the window and never clip off the top/edges.
    const rect = containerRef.current ? containerRef.current.getBoundingClientRect() : { left: 0, top: 0 };
    setDrill(null);
    setPopup({ node, x: Math.round(rect.left + px[0]), y: Math.round(rect.top + px[1]) });
  }, []);
  const openNodeRef = useRef(openNode);
  openNodeRef.current = openNode;

  // Clicking a site opens the right thing for what it IS. A building or outdoor area opens its
  // plans with EVERY camera on them (multi-node) — the digital-twin drill, shown even with no plans
  // yet. A point asset has no plan to open, so it opens the device card of the appliance mounted
  // there instead; with several appliances it first offers the choice, with none it says so.
  // focusFloorId opens the drill-down on a SPECIFIC area (clicked in the rail), rather than the
  // default first plan — otherwise clicking one area could land you on the other.
  const openBuilding = useCallback(async (site, px, focusFloorId) => {
    if (!hasPlans(site.kind)) {
      const rect = containerRef.current ? containerRef.current.getBoundingClientRect() : { left: 0, top: 0 };
      const x = Math.round(rect.left + (px ? px[0] : 0));
      const y = Math.round(rect.top + (px ? px[1] : 0));
      const mine = nodesBySiteId[site.id] || [];
      setDrill(null);
      if (mine.length === 1) { setSitePopup(null); setPopup({ node: mine[0], x, y }); return; }
      setPopup(null);
      setSitePopup({ site, nodes: mine, x, y });
      return;
    }
    let plans = [];
    try {
      const res = await api(`/api/sites/${site.id}/floorplans`);
      plans = res.ok && Array.isArray(res.body) ? res.body.filter((p) => p && p.floor) : [];
    } catch (_) { /* open empty */ }
    setPopup(null);
    setSitePopup(null);
    setDrill({ kind: 'site', site, floorplans: plans, focusFloorId });
  }, [nodesBySiteId]);
  const openBuildingRef = useRef(openBuilding);
  openBuildingRef.current = openBuilding;

  // Delete stale (ghost) camera placements whose camera no longer exists on its node, then refresh
  // the open building's plans (markers vanish) and the building overview (camera counts drop).
  const removeGhostPlacements = useCallback(async (ids) => {
    if (!ids || !ids.length) return;
    await Promise.all(ids.map((id) => api(`/api/placements/${id}`, { method: 'DELETE' }).catch(() => {})));
    if (drill && drill.site) {
      try {
        const res = await api(`/api/sites/${drill.site.id}/floorplans`);
        const plans = res.ok && Array.isArray(res.body) ? res.body.filter((p) => p && p.floor) : [];
        setDrill((d) => (d ? { ...d, floorplans: plans } : d));
      } catch (_) { /* ignore */ }
    }
    if (siteReloadRef.current) siteReloadRef.current();
    if (onToast) onToast(t('map.ghostsRemoved', { n: ids.length }), 'success');
  }, [drill, onToast, t]);

  // Buildings-centric map: a node never gets its own pin — every node lives in a site and is
  // reached by drilling into that site. Nothing is placed standalone, so there are no node pins.
  const placed = useMemo(() => [], []);
  // Nodes that still need a home: not yet assigned to any site — the rail nudges you to assign.
  const nodesToPlace = useMemo(() => nodes.filter((n) => n && !n.siteId), [nodes]);
  // All sites (placed or not) are valid homes to assign a node to.
  const allSites = useMemo(() => sites.map((row) => row.site).filter(Boolean), [sites]);
  // Nodes assigned to each site, by site id. The server's overview derives a site's nodes from its
  // camera PLACEMENTS, which a point asset has none of — a junction's cameras reach it only through
  // this assignment. So every consumer resolves nodes as "placed on a plan here" ∪ "assigned here".
  const nodesBySiteId = useMemo(() => {
    const m = {};
    nodes.forEach((n) => { if (n && n.siteId) (m[n.siteId] = m[n.siteId] || []).push(n); });
    return m;
  }, [nodes]);
  const resolvedNodeIds = useCallback((row) => {
    const s = row.site || {};
    const ids = (row.nodeIds || []).slice();
    (nodesBySiteId[s.id] || []).forEach((n) => { if (ids.indexOf(n.nodeId) < 0) ids.push(n.nodeId); });
    return ids;
  }, [nodesBySiteId]);
  // A site's rail status = the worst status among the nodes that answer for it.
  const siteToneKey = (row) => {
    const order = ['critical', 'warning', 'online', 'idle'];
    let worst = 'idle';
    resolvedNodeIds(row).forEach((nid) => { const k = nodeToneKey(nodesById[nid], nowSec); if (order.indexOf(k) < order.indexOf(worst)) worst = k; });
    return worst;
  };
  // Sites split by kind for the rail, each group in the wizard's order so the headings are stable.
  const sitesByKind = useMemo(() => {
    const g = { [KIND_BUILDING]: [], [KIND_OUTDOOR]: [], [KIND_POINT]: [] };
    sites.forEach((row) => { if (row.site) g[normKind(row.site.kind)].push(row); });
    return g;
  }, [sites]);

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

  // Persist a building's dragged/placed geographic position, then refresh the building markers.
  const persistSitePosition = useCallback(async (siteId, lon, lat) => {
    try {
      const res = await api(`/api/sites/${siteId}/position`, { method: 'PUT', body: JSON.stringify({ lat, lon, placed: true }) });
      if (!res.ok) throw new Error('save failed');
      if (siteReloadRef.current) siteReloadRef.current();
    } catch (_) {
      if (onToast) onToast(t('map.saveFailed'), 'error');
      if (siteReloadRef.current) siteReloadRef.current();
    }
  }, [onToast, t]);
  const persistSitePosRef = useRef(persistSitePosition);
  persistSitePosRef.current = persistSitePosition;

  // Create the asset and its plans, then hand straight to placement mode. The site exists from this
  // moment whether or not the operator ever clicks the map, so nothing is lost if they walk away
  // mid-flow — it simply shows up in the rail as still-to-place.
  const createBuilding = useCallback(async (name, icon, siteKind, areaNames) => {
    setBusy(true);
    try {
      const res = await api('/api/sites', { method: 'POST', body: JSON.stringify({ name, icon, kind: siteKind }) });
      if (!res.ok || !res.body || !res.body.id) throw new Error();
      const created = res.body;
      // Areas are created in order so their ordinal matches what the operator typed. A point asset
      // passes none — it has no surface to author.
      for (let i = 0; i < areaNames.length; i++) {
        // eslint-disable-next-line no-await-in-loop
        const ar = await api(`/api/sites/${created.id}/areas`, { method: 'POST', body: JSON.stringify({ name: areaNames[i], ordinal: i }) });
        if (!ar.ok) throw new Error();
      }
      setWizardOpen(false);
      if (siteReloadRef.current) siteReloadRef.current();
      // thenEdit: the map click that drops the marker also opens the editor, so "add an asset" ends
      // on the plan surface rather than back at a map with an unexplained new marker. A point asset
      // has no editor, so it simply lands on the map and waits for an appliance to be assigned.
      setPlacing({ kind: 'site', id: created.id, name: created.name, siteKind: normKind(siteKind), thenEdit: hasPlans(siteKind) });
      if (onToast) onToast(t('bld.createdPlaceIt', { name: created.name }), 'success');
    } catch (_) {
      if (onToast) onToast(t('map.siteCreateFailed'), 'error');
    } finally { setBusy(false); }
  }, [onToast, t]);

  // Rename / re-glyph an asset in place. A building or outdoor area gets this inside its editor;
  // a point asset has no editor, so the rail's pencil opens this dialog directly.
  const saveSiteMeta = useCallback(async (site, name, icon) => {
    setBusy(true);
    try {
      const res = await api(`/api/sites/${site.id}`, { method: 'PUT', body: JSON.stringify({ name, description: site.description || '', icon, kind: normKind(site.kind), ordinal: site.ordinal || 0 }) });
      if (!res.ok) throw new Error();
      setRenameSite(null);
      if (siteReloadRef.current) siteReloadRef.current();
      if (onToast) onToast(t('map.siteUpdated'), 'success');
    } catch (_) {
      if (onToast) onToast(t('map.error'), 'error');
    } finally { setBusy(false); }
  }, [onToast, t]);

  // Delete an asset from the rename dialog (used for point assets, which have no editor). Takes its
  // floor plans and camera placements with it; guarded by a confirm.
  const deleteSiteMeta = useCallback(async (site) => {
    if (!window.confirm(t('map.deleteAssetConfirm', { name: site.name }))) return;
    setBusy(true);
    try {
      const res = await api(`/api/sites/${site.id}`, { method: 'DELETE' });
      if (!res.ok) throw new Error();
      setRenameSite(null);
      if (siteReloadRef.current) siteReloadRef.current();
      if (onToast) onToast(t('map.assetDeleted', { name: site.name }), 'success');
    } catch (_) {
      if (onToast) onToast(t('map.error'), 'error');
    } finally { setBusy(false); }
  }, [onToast, t]);

  // Open the authoring dialog for an asset. Takes the site row (id/name/icon/kind) from wherever the
  // operator asked — rail row, drill-down header, or the drop that just finished. A point asset has
  // no plan surface to author, so for it "edit" means renaming/re-glyphing the marker.
  const openEditor = useCallback((site) => {
    setPopup(null);
    setSitePopup(null);
    setDrill(null);
    if (!hasPlans(site.kind)) { setRenameSite(site); return; }
    setEditorSite(site);
  }, []);
  const openEditorRef = useRef(openEditor);
  openEditorRef.current = openEditor;
  // The OL click handler is bound once, so it reads the live site list through a ref to resolve
  // the id it just placed into the full row the editor needs.
  const sitesRef = useRef(sites);
  sitesRef.current = sites;

  // Assign a node to the building it resides in (siteId), or clear it (siteId 0). Assigning takes
  // the node off the map (a building-resident node has no own pin); clearing returns it to the
  // "needs a home" list. The building's own marker then represents the node.
  const assignBuilding = useCallback(async (nodeId, siteId) => {
    try {
      const res = await api(`/api/nodes/${nodeId}/building`, { method: 'PUT', body: JSON.stringify({ siteId }) });
      if (!res.ok) throw new Error('save failed');
      setPopup(null);
      if (reloadNodes) reloadNodes();
    } catch (_) {
      if (onToast) onToast(t('map.saveFailed'), 'error');
      if (reloadNodes) reloadNodes();
    }
  }, [reloadNodes, onToast, t]);

  // Lazily fetch a building's floors/areas the first time its rail row is expanded.
  const loadSiteFloors = useCallback(async (siteId) => {
    setFloorsBySite((m) => ({ ...m, [siteId]: { ...(m[siteId] || {}), loading: true } }));
    try {
      const res = await api(`/api/sites/${siteId}/floors`, { noRedirect: true });
      const list = res.ok && Array.isArray(res.body) ? res.body.slice().sort((a, b) => (a.ordinal || 0) - (b.ordinal || 0)) : [];
      setFloorsBySite((m) => ({ ...m, [siteId]: { loading: false, list } }));
    } catch (_) {
      setFloorsBySite((m) => ({ ...m, [siteId]: { loading: false, error: true, list: [] } }));
    }
  }, []);
  const toggleSite = useCallback((siteId) => {
    setExpandedSites((e) => ({ ...e, [siteId]: !e[siteId] }));
  }, []);
  // Fetch floors for any expanded building we don't have them for. Driven off state (rather than the
  // click) so an editor change that drops the cache re-loads the row that is still open.
  useEffect(() => {
    Object.keys(expandedSites).forEach((id) => {
      if (expandedSites[id] && !floorsBySite[id]) loadSiteFloors(Number(id));
    });
  }, [expandedSites, floorsBySite, loadSiteFloors]);

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
      setAttribution(info?.attribution || '');
      setCanDownload(!!(info && info.canDownload));
      const regs = (info && Array.isArray(info.regions)) ? info.regions : [];
      regionsRef.current = regs;
      basemapLayersRef.current = {};
      regs.forEach((rg) => {
        const lyr = makeRegionLayer(rg.name, info && info.attribution);
        basemapLayersRef.current[rg.name] = lyr;
        layers.push(lyr);
      });
      // Buildings layer sits UNDER the node pins (buildings are the larger, fewer markers).
      const buildingSource = new VectorSource();
      buildingSourceRef.current = buildingSource;
      const buildingLayer = new VectorLayer({ source: buildingSource, style: siteStyle });
      buildingLayerRef.current = buildingLayer;
      buildingLayer.setVisible(showLayers.buildings);
      layers.push(buildingLayer);

      // Beacon: pulse behind a building that is lost OR has an unread warning/critical alert, in the
      // colour of whatever triggered it. Redrawn each frame while any is on screen; settles static.
      buildingLayer.on('prerender', (evt) => {
        const vctx = getVectorContext(evt);
        const time = evt.frameState ? evt.frameState.time : 0;
        const base = (time % PULSE_PERIOD) / PULSE_PERIOD;
        let animating = false;
        for (const f of buildingSource.getFeatures()) {
          if (!f.get('critical')) continue;
          animating = true;
          const color = f.get('beaconColor') || (f.get('tone') || TONES.idle).ring;
          const kind = normKind(f.get('kind'));
          const geom = f.getGeometry();
          for (const offset of [0, 0.5]) {
            const p = (base + offset) % 1;
            const grown = easeOutCubic(p);
            const alpha = 0.5 * (1 - p);
            if (alpha <= 0.01) continue;
            // The ring echoes the marker's silhouette, so a junction pulses as a diamond.
            vctx.setStyle(new Style({ image: markerShape(kind, 14 + grown * 16, undefined, new Stroke({ color: hexToRgba(color, alpha), width: 2.5 - grown })) }));
            vctx.drawGeometry(geom);
          }
        }
        if (animating && mapRef.current) mapRef.current.render();
      });

      const pointSource = new VectorSource();
      pointSourceRef.current = pointSource;
      const clusterSource = new Cluster({ distance: 36, minDistance: 18, source: pointSource });
      const pinLayer = new VectorLayer({ source: clusterSource, style: pinStyle });
      pinLayerRef.current = pinLayer;
      pinLayer.setVisible(showLayers.nodes);
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

      // Centre on the basemap's region if we know it, but DON'T lock the view there — the operator
      // is free to pan anywhere (a region with no data just shows the sea ground, with an option to
      // download it). Panning is unconstrained by design.
      const bnd = info && Array.isArray(info.bounds) && info.bounds.length === 4 ? info.bounds : null;
      const center = bnd ? getCenter([...fromLonLat([bnd[0], bnd[1]]), ...fromLonLat([bnd[2], bnd[3]])]) : fromLonLat(DEFAULT_CENTER);
      const view = new View({ center, zoom: DEFAULT_ZOOM });
      const map = new Map({
        target: containerRef.current,
        layers,
        view,
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

      // Drag an existing node pin to reposition.
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

      // Drag a building marker to reposition it.
      const bTranslate = new Translate({ layers: [buildingLayer], hitTolerance: 5 });
      bTranslate.on('translateend', (evt) => {
        const f = evt.features.item(0);
        if (!f) return;
        const site = f.get('site');
        const [lon, lat] = toLonLat(f.getGeometry().getCoordinates());
        persistSitePosRef.current(site.id, lon, lat);
      });
      map.addInteraction(bTranslate);

      // Single click does one of three things:
      //  - placing mode: drop the picked building/node at the clicked spot.
      //  - a building marker hit: drill into its floor plans (every camera inside).
      //  - a single node pin hit: open its device card; else close any popup.
      map.on('singleclick', (evt) => {
        const target = placingRef.current;
        if (target) {
          const [lon, lat] = toLonLat(evt.coordinate);
          if (target.kind === 'site') persistSitePosRef.current(target.id, lon, lat);
          else persistPosition(target.id, lon, lat);
          setPlacing(null);
          if (onToast) onToast(t('map.placed', { name: target.name }), 'success');
          // A building placed as the tail of "add building" continues into its editor. Resolve the
          // freshest row we have, falling back to what the wizard told us if the overview reload
          // hasn't landed yet.
          if (target.thenEdit && target.kind === 'site') {
            const row = sitesRef.current.find((s) => s.site && s.site.id === target.id);
            openEditorRef.current((row && row.site) || { id: target.id, name: target.name });
          }
          return;
        }
        let hitBuilding = null;
        let hitNode = null;
        map.forEachFeatureAtPixel(evt.pixel, (f, layer) => {
          if (layer === buildingLayerRef.current && !hitBuilding) hitBuilding = f;
          else if (layer === pinLayerRef.current && !hitNode) hitNode = f;
        }, { hitTolerance: 5 });
        if (hitBuilding) { openBuildingRef.current(hitBuilding.get('site'), map.getPixelFromCoordinate(evt.coordinate)); return; }
        const members = hitNode ? (hitNode.get('features') || []) : [];
        if (members.length === 1) {
          const node = members[0].get('node');
          const px = map.getPixelFromCoordinate(evt.coordinate);
          openNodeRef.current(node, px);
        } else {
          setPopup(null);
          setSitePopup(null);
        }
      });
      // Close the popups when the map moves (they would otherwise float away from their marker).
      map.on('movestart', () => { setPopup(null); setSitePopup(null); });

      // Track whether the current view is beyond every downloaded region's coverage (→ offer a
      // download of the area you're looking at).
      const updateOutside = () => {
        const c = toLonLat(map.getView().getCenter());
        const inside = regionsRef.current.some((rg) => rg.bounds && rg.bounds.length === 4 && c[0] >= rg.bounds[0] && c[0] <= rg.bounds[2] && c[1] >= rg.bounds[1] && c[1] <= rg.bounds[3]);
        setOutside(!inside);
      };
      map.on('moveend', updateOutside);
      updateOutside();

      // Hover feedback: a pointer cursor + a highlight halo, so a canvas marker reads as clickable.
      let hoverFeat = null;
      map.on('pointermove', (evt) => {
        if (evt.dragging) return;
        let hit = null;
        map.forEachFeatureAtPixel(evt.pixel, (f, layer) => { if ((layer === buildingLayer || layer === pinLayer) && !hit) hit = f; }, { hitTolerance: 5 });
        const el = map.getTargetElement();
        if (el) el.style.cursor = (hit || placingRef.current) ? 'pointer' : '';
        if (hit !== hoverFeat) {
          if (hoverFeat) hoverFeat.set('hover', false);
          if (hit) hit.set('hover', true);
          hoverFeat = hit;
          buildingLayer.changed();
          pinLayer.changed();
        }
      });

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
      buildingSourceRef.current = null;
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

  // Re-populate building markers when sites (or the nodes that give them their health) change.
  // A building's tone is the WORST status among the nodes that own cameras inside it.
  useEffect(() => {
    if (!mapReady) return;
    const src = buildingSourceRef.current;
    if (!src) return;
    src.clear();
    const order = ['critical', 'warning', 'online', 'idle'];
    src.addFeatures(sites.filter((s) => s.site && s.site.mapPlaced).map((row) => {
      const s = row.site;
      let worst = 'idle';
      resolvedNodeIds(row).forEach((nid) => {
        const k = nodeToneKey(nodesById[nid], nowSec);
        if (order.indexOf(k) < order.indexOf(worst)) worst = k;
      });
      // Unread notifications from the cameras PHYSICALLY AT this site (per-camera, not the whole
      // node's tally — a node may record cameras at several sites).
      const keys = resolvedCamKeys(row);
      let count = 0; let sev = 'info';
      keys.forEach((key) => { const c = notifByCam[key]; if (c && c.count) { count += c.count; sev = ((SEV_RANK[c.sev] || 0) > (SEV_RANK[sev] || 0) ? c.sev : sev); } });
      const notif = count > 0 ? { count, sev } : null;
      // Camera online/total from live health (a camera down while its node is up shows here).
      let online = 0; let known = 0;
      keys.forEach((k) => { const hs = camHealth[k]; if (hs !== undefined) { known += 1; if (hs === 'online') online += 1; } });
      const f = new Feature({ geometry: new Point(fromLonLat([s.lon, s.lat])) });
      f.set('site', s);
      f.set('kind', normKind(s.kind));
      f.set('tone', TONES[worst]);
      f.set('cameras', row.cameras || keys.length);
      f.set('camTotal', keys.length);
      f.set('camOnline', online);
      f.set('camKnown', known); // how many cameras we have a live health reading for
      f.set('name', s.name);
      f.set('icon', siteGlyph(s));
      f.set('notif', notif);
      // Blink when the site is lost OR it has an unread warning/critical notification.
      f.set('critical', worst === 'critical' || (notif && notif.sev !== 'info'));
      f.set('beaconColor', worst === 'critical' ? TONES.critical.ring : (notif && notif.sev !== 'info' ? SEV_COLOR[notif.sev] : TONES[worst].ring));
      return f;
    }));
  }, [sites, nodesById, notifByCam, camHealth, nowSec, mapReady, resolvedNodeIds, resolvedCamKeys]);

  // Toggle marker layers on/off.
  useEffect(() => {
    if (!mapReady) return;
    if (buildingLayerRef.current) buildingLayerRef.current.setVisible(showLayers.buildings);
    if (pinLayerRef.current) pinLayerRef.current.setVisible(showLayers.nodes);
  }, [showLayers, mapReady]);

  // Escape cancels placing mode.
  useEffect(() => {
    if (!placing) return undefined;
    const onKey = (e) => { if (e.key === 'Escape') setPlacing(null); };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, [placing]);

  // Drop a node or building dragged from the rail onto the map (power-user shortcut).
  function onDrop(e) {
    e.preventDefault();
    const map = mapRef.current;
    if (!map) return;
    const siteId = e.dataTransfer.getData('text/site-id');
    const nodeId = e.dataTransfer.getData('text/node-id');
    if (!siteId && !nodeId) return;
    const coord = map.getEventCoordinate(e.nativeEvent || e);
    const [lon, lat] = toLonLat(coord);
    if (siteId) persistSitePosition(Number(siteId), lon, lat);
    else persistPosition(nodeId, lon, lat);
    // Dragging is the power-user shortcut past click-to-place; honour the same "then edit" tail so
    // both routes through the wizard end in the editor.
    const target = placingRef.current;
    if (target && target.thenEdit && target.kind === 'site' && Number(siteId) === target.id) {
      const row = sitesRef.current.find((s) => s.site && s.site.id === target.id);
      openEditorRef.current((row && row.site) || { id: target.id, name: target.name });
    }
    setPlacing(null);
  }

  // A node rail row: status + name + a building selector. On a buildings-centric map a node is only
  // ever assigned to a building (which then represents it on the map) — there is no standalone place.
  const renderNodeRow = (n) => (
    <li key={n.nodeId} className="fleet-map-rail-noderow">
      <span className="rail-dot" style={{ background: nodeTone(n, nowSec).color }} />
      <span className="rail-name" title={n.name || n.nodeId}>{n.name || n.nodeId}</span>
      <select
        className="rail-building-select"
        value={n.siteId ? String(n.siteId) : ''}
        onChange={(e) => assignBuilding(n.nodeId, Number(e.target.value) || 0)}
        title={t('map.residesIn')}
        aria-label={t('map.residesIn')}
      >
        <option value="">{t('map.noBuilding')}</option>
        {allSites.map((s) => <option key={s.id} value={s.id}>{`${siteGlyph(s)} ${s.name}`}</option>)}
      </select>
    </li>
  );

  // One rail row per site. A building or outdoor area expands to the plans inside it; a point asset
  // has none, so its caret is replaced by a spacer and its pencil renames the marker instead of
  // opening an editor.
  const renderSiteRow = (row) => {
    const s = row.site;
    const kind = normKind(s.kind);
    const onMap = !!s.mapPlaced;
    const worst = siteToneKey(row);
    const expandable = hasPlans(kind);
    const isOpen = expandable && !!expandedSites[s.id];
    const fl = floorsBySite[s.id];
    const placingThis = placing && placing.kind === 'site' && placing.id === s.id;
    const camCount = row.cameras || resolvedCamKeys(row).length;
    return (
      <li key={`site-${s.id}`} className="fleet-map-rail-bldgwrap">
        <div className="fleet-map-rail-siterow">
          {expandable ? (
            <button type="button" className="rail-expand" onClick={() => toggleSite(s.id)} aria-label={t('map.showFloors')} aria-expanded={isOpen}><Ico n={isOpen ? 'chev-down' : 'chev-right'} sz={12} /></button>
          ) : <span className="rail-expand-spacer" />}
          <button
            type="button"
            className={`fleet-map-rail-node${placingThis ? ' active' : ''}`}
            draggable={!onMap}
            onDragStart={!onMap ? (e) => { e.dataTransfer.setData('text/site-id', String(s.id)); e.dataTransfer.effectAllowed = 'move'; setPlacing(null); } : undefined}
            onClick={() => (onMap ? flyToSite(s) : setPlacing(placingThis ? null : { kind: 'site', id: s.id, name: s.name, siteKind: kind }))}
            title={onMap ? t('map.flyTo') : t('map.placeHint')}
          >
            <span className="rail-dot" style={{ background: TONES[worst].color }} />
            <span className="rail-emoji" aria-hidden="true">{siteGlyph(s)}</span>
            <span className="rail-name">{s.name}</span>
            {camCount ? <span className="rail-count" title={t('map.cameras')}>{camCount}<Ico n="video" sz={11} /></span> : null}
            {placingThis ? <Ico n="map-pin" sz={14} /> : (!onMap ? <span className="rail-toplace" title={t('map.notOnMap')} aria-label={t('map.notOnMap')}><Ico n="map-pin" sz={12} /></span> : null)}
          </button>
          <button type="button" className="rail-edit-btn" onClick={() => openEditor(s)} title={expandable ? t('bld.editAreas') : t('map.editAsset')} aria-label={expandable ? t('bld.editAreas') : t('map.editAsset')}>
            <Ico n="edit-2" sz={13} />
          </button>
        </div>
        {isOpen ? (
          <ul className="fleet-map-rail-sublist">
            {!fl || fl.loading ? (
              <li className="rail-subfloor muted">{t('common.loading')}</li>
            ) : !fl.list.length ? (
              <li className="rail-subfloor muted">{t('map.noAreasYet')}</li>
            ) : fl.list.map((f) => (
              <li key={f.id} className="rail-subfloor">
                <button type="button" className="rail-floor-btn" onClick={() => openBuilding(s, null, f.id)} title={f.name}>
                  <Ico n="layers" sz={11} />
                  <span className="rail-name">{f.name}</span>
                </button>
              </li>
            ))}
          </ul>
        ) : null}
      </li>
    );
  };

  return (
    <section className="settings-panel span-two fleet-map-panel">
      <header>
        <h2><span className="btn-icon"><Ico n="map" /> {t('map.title')}</span></h2>
        <div className="fleet-map-header-right">
          <div className="fleet-map-legend" aria-hidden="true">
            {['online', 'warning', 'critical', 'idle'].map((k) => (
              <span key={k} className="legend-item">
                <span className="legend-dot" style={{ background: TONES[k].color }} />
                {t(`map.legend.${k}`)}
              </span>
            ))}
          </div>
        </div>
      </header>
      <p className="settings-hint">{t('map.geoHint')}</p>
      {state === 'nobasemap' ? <p className="settings-hint">{t('map.noBasemap')}</p> : null}

      <div className="fleet-map-body">
        <aside className="fleet-map-rail">
          <div className="fleet-map-rail-head">
            <span>{t('map.assets')} <span className="count-badge">{sites.length}</span></span>
            <button type="button" className="rail-addbuilding" onClick={() => setWizardOpen(true)} disabled={busy} title={t('map.addAsset')}>
              <Ico n="plus" sz={13} /> {t('map.addAsset')}
            </button>
          </div>
          {/* One scroll region for the whole rail body — see .fleet-map-rail-scroll. */}
          <div className="fleet-map-rail-scroll">
          {sites.length === 0 && nodesToPlace.length === 0 ? (
            <div className="fleet-map-rail-empty"><Ico n="building" sz={22} /><span>{t('map.noAssetsYet')}</span></div>
          ) : (
            <>
              {/* Assets grouped by what they ARE — buildings, outdoor areas, point assets — so a
                  junction doesn't sit in a list headed "Buildings". A group with nothing in it is
                  simply absent. Placed assets fly-to on click; unplaced ones enter placing mode. */}
              {KIND_ORDER.map((k) => (sitesByKind[k].length === 0 ? null : (
                <div key={`grp-${k}`} className="fleet-map-rail-kindgroup">
                  <div className="fleet-map-rail-group">
                    <Ico n={k === KIND_BUILDING ? 'building' : (k === KIND_OUTDOOR ? 'grid2' : 'map-pin')} sz={12} />
                    {t(`bld.kindPlural.${k}`)} <span className="count-badge">{sitesByKind[k].length}</span>
                  </div>
                  <ul className="fleet-map-rail-list">
                    {sitesByKind[k].map((row) => renderSiteRow(row))}
                  </ul>
                </div>
              )))}
              {/* Appliances not yet at any asset — assign each to one. */}
              {nodesToPlace.length > 0 ? (
                <>
                  <div className="fleet-map-rail-group"><Ico n="cpu" sz={12} /> {t('map.nodesToPlace')} <span className="count-badge">{nodesToPlace.length}</span></div>
                  <div className="fleet-map-rail-hint">{t('map.assignHint')}</div>
                  <ul className="fleet-map-rail-list">
                    {nodesToPlace.map((n) => renderNodeRow(n))}
                  </ul>
                </>
              ) : null}
            </>
          )}
          </div>
        </aside>

        <div className="fleet-map-stage">
          {placing ? (
            <div className="fleet-map-placing" role="status">
              <span><Ico n="map-pin" sz={14} /> {t('map.placingBanner', { name: placing.name })}</span>
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
          {outside || downloading ? (
            <div className="fleet-map-download">
              <span><Ico n="globe" sz={14} /> {t('map.noDataHere')}</span>
              {canDownload ? (
                <button type="button" onClick={downloadRegion} disabled={downloading}>
                  {downloading ? <><Ico n="reload" sz={13} /> {t('map.downloading')}</> : <><Ico n="download" sz={13} /> {t('map.downloadRegion')}</>}
                </button>
              ) : (
                <>
                  <span className="fleet-map-download-note">{t('map.downloadNotConfigured')}</span>
                  {!bmConfig.envManaged ? <button type="button" onClick={() => setSetupOpen(true)}><Ico n="sliders" sz={13} /> {t('map.setUp')}</button> : null}
                </>
              )}
            </div>
          ) : null}
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
          {sitePopup ? (
            <MapPopupFrame x={sitePopup.x} y={sitePopup.y}>
              <SiteAssetPopup
                site={sitePopup.site}
                nodes={sitePopup.nodes}
                nowSec={nowSec}
                onOpenNode={(n) => { const { x, y } = sitePopup; setSitePopup(null); setPopup({ node: n, x, y }); }}
                onClose={() => setSitePopup(null)}
              />
            </MapPopupFrame>
          ) : null}
          {drill ? (
            <div className="fleet-map-drill">
              <BuildingFloorView site={drill.site} floorplans={drill.floorplans} nodesById={nodesById} notifByCam={notifByCam} focusCameraId={drill.focusCameraId} focusFloorId={drill.focusFloorId} onBack={() => { const s = drill.site; setDrill(null); flyToSite(s); }} onPlay={playCamera} onRemovePlacements={removeGhostPlacements} onEdit={openEditor} />
            </div>
          ) : null}
        </div>
      </div>
      {attribution ? <div className="fleet-map-attribution">{attribution}</div> : null}

      {setupOpen ? (
        <div className="fd-overlay" role="dialog" aria-label={t('map.basemapSetup')}>
          <div className="site-dialog">
            <div className="site-dialog-title"><Ico n="globe" sz={16} /> {t('map.basemapSetup')}</div>
            <p className="settings-hint" style={{ margin: 0 }}>{t('map.basemapSetupHint')}</p>
            <label className="site-dialog-field">
              <span>{t('map.sourceUrl')}</span>
              <input ref={sourceInputRef} type="text" defaultValue={bmConfig.source || ''} placeholder="https://build.protomaps.com/20260719.pmtiles" />
            </label>
            <div className={`bm-tool-status ${bmConfig.hasTool ? 'ok' : 'bad'}`}>
              <Ico n={bmConfig.hasTool ? 'check-ok' : 'warning'} sz={13} /> {bmConfig.hasTool ? t('map.toolInstalled') : t('map.toolMissing')}
            </div>
            <div className="site-dialog-actions">
              <button type="button" className="quiet" onClick={() => setSetupOpen(false)}>{t('map.cancel')}</button>
              <button type="button" onClick={() => saveSource((sourceInputRef.current && sourceInputRef.current.value.trim()) || '')}>{t('fd.save')}</button>
            </div>
          </div>
        </div>
      ) : null}

      {wizardOpen ? (
        <AssetWizard busy={busy} onCreate={createBuilding} onCancel={() => setWizardOpen(false)} />
      ) : null}

      {renameSite ? (
        <SiteDialog
          initialName={renameSite.name}
          initialIcon={renameSite.icon}
          kind={renameSite.kind}
          busy={busy}
          onSave={(name, icon) => saveSiteMeta(renameSite, name, icon)}
          onDelete={() => deleteSiteMeta(renameSite)}
          onCancel={() => setRenameSite(null)}
        />
      ) : null}

      {editorSite ? (
        <BuildingEditorDialog
          site={editorSite}
          nodes={nodes}
          onToast={onToast}
          onClose={() => { const id = editorSite.id; setEditorSite(null); setFloorsBySite((m) => { const c = { ...m }; delete c[id]; return c; }); if (siteReloadRef.current) siteReloadRef.current(); }}
          onChanged={() => { setFloorsBySite((m) => { const c = { ...m }; delete c[editorSite.id]; return c; }); if (siteReloadRef.current) siteReloadRef.current(); }}
        />
      ) : null}

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
