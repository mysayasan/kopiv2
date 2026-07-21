import { useCallback, useEffect, useRef, useState } from 'react';
import PropTypes from 'prop-types';
import { useT, Ico } from '@shared';
import { api, apiBase, csrfToken } from '../lib/helpers';
import { nodeTone, TONES } from '../lib/fleet_status';
import { FloorDesigner } from './floor_designer';

import Map from 'ol/Map.js';
import View from 'ol/View.js';
import ImageLayer from 'ol/layer/Image.js';
import Static from 'ol/source/ImageStatic.js';
import Projection from 'ol/proj/Projection.js';
import VectorLayer from 'ol/layer/Vector.js';
import VectorSource from 'ol/source/Vector.js';
import Feature from 'ol/Feature.js';
import Point from 'ol/geom/Point.js';
import Polygon from 'ol/geom/Polygon.js';
import Translate from 'ol/interaction/Translate.js';
import Select from 'ol/interaction/Select.js';
import { click } from 'ol/events/condition.js';
import { getCenter } from 'ol/extent.js';
import { Fill, Stroke, Style, Circle as CircleStyle, Text } from 'ol/style.js';
import 'ol/ol.css';
import '../styles/fleet-map.css';

// A placement is "reachable" only when its node is currently online. Anything else — lost,
// self-dropped, or a node no longer in the fleet — means we cannot fetch its live details, so
// the marker renders from the stored snapshot (last-known name) with a muted, dashed style.
// It NEVER vanishes: a camera you placed stays on the plan even when its node is down.
function reachable(node) {
  return !!node && (node.status || '').toLowerCase() === 'online';
}

// hexToRgba turns a #rrggbb tone colour into rgba() so the coverage arc can be translucent.
function hexToRgba(hex, alpha) {
  const h = String(hex || '#94a3b8').replace('#', '');
  const r = parseInt(h.slice(0, 2), 16);
  const g = parseInt(h.slice(2, 4), 16);
  const b = parseInt(h.slice(4, 6), 16);
  return `rgba(${r}, ${g}, ${b}, ${alpha})`;
}

// arcRadius scales the coverage wedge to the plan so it reads as an area, not a fixed blob.
function arcRadius(floor) {
  const w = (floor && floor.width) || 1024;
  const h = (floor && floor.height) || 768;
  return Math.max(50, Math.min(w, h) * 0.16);
}

// arcRing returns the polygon ring (in the floor's pixel coords, OL y-up) for a camera's coverage
// wedge: a fan from the marker spanning `fov` degrees, centred on `heading` (clockwise from up).
function arcRing(p, radius) {
  const half = (p.fov || 0) / 2;
  const N = 26;
  const pts = [[p.x, p.y]];
  for (let i = 0; i <= N; i++) {
    const deg = (p.heading || 0) - half + (p.fov || 0) * (i / N);
    const rad = (deg * Math.PI) / 180;
    pts.push([p.x + radius * Math.sin(rad), p.y + radius * Math.cos(rad)]);
  }
  pts.push([p.x, p.y]);
  return pts;
}

// arcStyle fills a camera's coverage wedge in the node's tone colour, translucent.
function arcStyle(feature, nodesById, nowSec) {
  const p = feature.get('placement');
  const node = nodesById[p.nodeId];
  const tone = node ? nodeTone(node, nowSec) : TONES.idle;
  return new Style({
    fill: new Fill({ color: hexToRgba(tone.color, 0.16) }),
    stroke: new Stroke({ color: hexToRgba(tone.color, 0.45), width: 1 }),
  });
}

function placementStyle(feature, nodesById, nowSec) {
  const p = feature.get('placement');
  const node = nodesById[p.nodeId];
  const online = reachable(node);
  const tone = node ? nodeTone(node, nowSec) : TONES.idle;
  const liveName = p.cameraId ? p.lastKnownName : (node ? node.name || node.nodeId : p.lastKnownName);
  const label = liveName || p.lastKnownName || p.nodeId;
  return new Style({
    image: new CircleStyle({
      radius: p.cameraId ? 7 : 9,
      fill: new Fill({ color: online ? tone.color : 'rgba(148,163,184,0.55)' }),
      stroke: new Stroke({
        color: online ? '#ffffff' : tone.ring,
        width: 2,
        lineDash: online ? undefined : [3, 3],
      }),
    }),
    text: new Text({
      text: label,
      offsetY: -16,
      font: '600 11px system-ui, sans-serif',
      fill: new Fill({ color: online ? '#1f2937' : '#64748b' }),
      stroke: new Stroke({ color: 'rgba(255,255,255,0.85)', width: 3 }),
    }),
  });
}

// Curated building glyphs an operator can tag a site with; the chosen one is drawn on the geo-map
// marker and shown in pickers. Emoji so it needs no image asset and renders in the OL canvas.
export const BUILDING_ICONS = ['🏢', '🏬', '🏭', '🏠', '🏘️', '🏗️', '🏪', '🏫', '🏥', '🏨', '🏦', '🏛️', '🏟️', '⛪', '🅿️', '⛽', '🗼', '🚧'];
export const DEFAULT_BUILDING_ICON = '🏢';

// SiteDialog is the small create/edit-building modal: a name field + a grid of glyphs to pick from.
// Replaces the old name-only window.prompt so a building can carry an icon from the moment it's made.
export function SiteDialog({ mode, initialName, initialIcon, busy, onSave, onCancel }) {
  const t = useT();
  const [name, setName] = useState(initialName || '');
  const [icon, setIcon] = useState(initialIcon || DEFAULT_BUILDING_ICON);
  const canSave = name.trim().length > 0 && !busy;
  return (
    <div className="fd-overlay" role="dialog" aria-label={mode === 'edit' ? t('map.editBuilding') : t('map.addBuilding')}>
      <div className="site-dialog">
        <div className="site-dialog-title"><span className="site-dialog-glyph">{icon}</span> {mode === 'edit' ? t('map.editBuilding') : t('map.addBuilding')}</div>
        <label className="site-dialog-field">
          <span>{t('map.buildingName')}</span>
          <input
            type="text"
            autoFocus
            value={name}
            onChange={(e) => setName(e.target.value)}
            placeholder={t('map.buildingName')}
            onKeyDown={(e) => { if (e.key === 'Enter' && canSave) onSave(name.trim(), icon); }}
          />
        </label>
        <div className="site-dialog-field">
          <span>{t('map.buildingIcon')}</span>
          <div className="site-icon-grid" role="listbox" aria-label={t('map.buildingIcon')}>
            {BUILDING_ICONS.map((g) => (
              <button key={g} type="button" className={`site-icon${icon === g ? ' active' : ''}`} onClick={() => setIcon(g)} aria-selected={icon === g}>{g}</button>
            ))}
          </div>
        </div>
        <div className="site-dialog-actions">
          <button type="button" className="quiet" onClick={onCancel} disabled={busy}>{t('map.cancel')}</button>
          <button type="button" onClick={() => onSave(name.trim(), icon)} disabled={!canSave}>{mode === 'edit' ? t('fd.save') : t('map.addSite')}</button>
        </div>
      </div>
    </div>
  );
}
SiteDialog.propTypes = { mode: PropTypes.string, initialName: PropTypes.string, initialIcon: PropTypes.string, busy: PropTypes.bool, onSave: PropTypes.func, onCancel: PropTypes.func };

// IndoorMap renders uploaded floor plans in an OL pixel projection (image pixels ARE map
// coordinates) and lets operators drop nodes/cameras onto them. Placements are myseliasan-
// owned, so a placed camera survives its node going offline — the defining behaviour of this
// view, since the control plane otherwise has no camera inventory of its own.
export function IndoorMap({ nodes = [], reloadNodes, onToast, onOpenNode, lockedSiteId = null, onFloorplansChanged, onActiveSiteChange }) {
  const t = useT();
  const activeSiteChangeRef = useRef(onActiveSiteChange);
  activeSiteChangeRef.current = onActiveSiteChange;
  const changedRef = useRef(onFloorplansChanged);
  changedRef.current = onFloorplansChanged;
  const notifyChanged = () => { if (changedRef.current) changedRef.current(); };
  const [sites, setSites] = useState([]);
  const [activeSite, setActiveSite] = useState(null);
  const [floors, setFloors] = useState([]);
  const [activeFloor, setActiveFloor] = useState(null);
  const [placements, setPlacements] = useState([]);
  const [camsByNode, setCamsByNode] = useState({}); // nodeId -> {loading, cams, error}
  const [expanded, setExpanded] = useState({});
  const [busy, setBusy] = useState(false);
  // mapEpoch bumps each time the OL map is rebuilt, so the marker-populate effect re-runs
  // against the fresh (empty) source. selectedId tracks the clicked marker for removal.
  const [mapEpoch, setMapEpoch] = useState(0);
  const [selectedId, setSelectedId] = useState(null);
  // Click-to-place: the palette item the operator picked, mirrored to a ref so the OL click
  // handler (set up once per floor) reads the live value.
  const [placing, setPlacing] = useState(null); // { nodeId, cameraId, name }
  const [designer, setDesigner] = useState(null); // null | { floor } — draw a new plan or edit an existing drawn one
  const [siteDialog, setSiteDialog] = useState(null); // null | { mode:'create'|'edit', id?, name, icon }
  const [dropActive, setDropActive] = useState(false); // desktop image-file drag over the dropzone
  const placingRef = useRef(null);
  placingRef.current = placing;
  const containerRef = useRef(null);
  const mapRef = useRef(null);
  const markerSourceRef = useRef(null);
  const arcSourceRef = useRef(null);
  const handleSourceRef = useRef(null); // rotate handle for a selected camera (drag to aim its FOV)
  const rotatingRef = useRef(false); // true while dragging the rotate handle (don't re-populate it)
  const fileInputRef = useRef(null);
  const dragGhostRef = useRef(null); // custom drag image shown while dragging a palette item
  const dragGhostLabelRef = useRef(null);
  const [canvasDragOver, setCanvasDragOver] = useState(false); // desktop/palette drag over the plan
  const aimTimerRef = useRef(null); // debounces persisting heading/fov while a slider is dragged
  const nowSec = Math.floor(Date.now() / 1000);

  const nodesById = {};
  for (const n of nodes) nodesById[n.nodeId] = n;

  async function loadSites(selectId) {
    try {
      const res = await api('/api/sites');
      const list = res.ok ? res.body || [] : [];
      setSites(list);
      // When embedded (locked to a building) always select that one; otherwise honour selectId/first.
      const want = selectId || lockedSiteId;
      const pick = want ? list.find((s) => s.id === want) : list[0];
      setActiveSite(pick || null);
      return pick || null;
    } catch (_) { setSites([]); return null; }
  }
  async function loadFloors(siteId, selectId) {
    if (!siteId) { setFloors([]); setActiveFloor(null); return; }
    try {
      const res = await api(`/api/sites/${siteId}/floors`);
      const list = res.ok ? res.body || [] : [];
      list.sort((a, b) => (a.ordinal - b.ordinal) || (a.id - b.id));
      setFloors(list);
      const pick = selectId ? list.find((f) => f.id === selectId) : list[0];
      setActiveFloor(pick || null);
      notifyChanged();
    } catch (_) { setFloors([]); setActiveFloor(null); }
  }
  const loadPlacements = useCallback(async (floorId) => {
    if (!floorId) { setPlacements([]); return; }
    try {
      const res = await api(`/api/floors/${floorId}/placements`);
      setPlacements(res.ok ? res.body || [] : []);
      notifyChanged();
    } catch (_) { setPlacements([]); }
  }, []);

  useEffect(() => { loadSites(); }, []);
  // Embedded in the workspace: whenever the locked building changes, switch to it.
  useEffect(() => { if (lockedSiteId && sites.length) { const s = sites.find((x) => x.id === lockedSiteId); if (s && (!activeSite || activeSite.id !== s.id)) setActiveSite(s); } }, [lockedSiteId, sites]);
  useEffect(() => { if (activeSite) loadFloors(activeSite.id); }, [activeSite && activeSite.id]);
  // Report the viewed building up so the geo map can zoom to it when the operator switches back.
  useEffect(() => { if (activeSiteChangeRef.current) activeSiteChangeRef.current(activeSite ? activeSite.id : null); }, [activeSite && activeSite.id]);
  useEffect(() => { loadPlacements(activeFloor && activeFloor.id); }, [activeFloor && activeFloor.id, loadPlacements]);

  // Live cameras for a node, fetched over the tunnel (same path the node tree uses). Returns
  // nothing when the node is offline — which is exactly why placements store a name snapshot.
  const loadCams = useCallback(async (nodeId) => {
    setCamsByNode((m) => ({ ...m, [nodeId]: { loading: true, cams: m[nodeId]?.cams || [] } }));
    const r = await api(`/api/nodes/${encodeURIComponent(nodeId)}/proxy/api/cameras?limit=200`, { noRedirect: true }).catch(() => ({ ok: false }));
    const cams = r.ok ? (Array.isArray(r.body) ? r.body : (r.body?.items || [])) : [];
    setCamsByNode((m) => ({ ...m, [nodeId]: { loading: false, cams, error: !r.ok } }));
  }, []);
  function toggleNode(nodeId) {
    setExpanded((e) => {
      const next = !e[nodeId];
      if (next && !camsByNode[nodeId]) loadCams(nodeId);
      return { ...e, [nodeId]: next };
    });
  }

  // Build the OL map for the active floor (pixel projection + marker layer + interactions).
  useEffect(() => {
    if (mapRef.current) { mapRef.current.setTarget(undefined); mapRef.current = null; }
    markerSourceRef.current = null;
    if (!activeFloor || !containerRef.current) return;
    const w = activeFloor.width || 1024;
    const h = activeFloor.height || 768;
    const extent = [0, 0, w, h];
    const projection = new Projection({ code: `floor-${activeFloor.id}`, units: 'pixels', extent });
    const imageLayer = new ImageLayer({
      source: new Static({ url: `${apiBase()}/api/floors/${activeFloor.id}/image`, projection, imageExtent: extent }),
    });
    // Coverage arcs live in their own layer BELOW the markers (and take no interactions, so a
    // drag/click always lands on the marker, never the wedge).
    const arcSource = new VectorSource();
    arcSourceRef.current = arcSource;
    const arcLayer = new VectorLayer({ source: arcSource, style: (f) => arcStyle(f, nodesById, nowSec) });
    const markerSource = new VectorSource();
    markerSourceRef.current = markerSource;
    const markerLayer = new VectorLayer({
      source: markerSource,
      style: (f) => placementStyle(f, nodesById, nowSec),
    });
    // On-marker handles for a selected camera: an AIM knob at the arc's centre (drag to point the
    // camera) and two EDGE knobs at the wedge sides (drag to widen/narrow the field of view). No
    // dialog — everything is adjusted directly on the marker.
    const handleSource = new VectorSource();
    handleSourceRef.current = handleSource;
    const aimStyle = new Style({ image: new CircleStyle({ radius: 7, fill: new Fill({ color: '#2d6cdf' }), stroke: new Stroke({ color: '#ffffff', width: 2 }) }) });
    const edgeStyle = new Style({ image: new CircleStyle({ radius: 5, fill: new Fill({ color: '#ffffff' }), stroke: new Stroke({ color: '#2d6cdf', width: 2 }) }) });
    const handleLayer = new VectorLayer({ source: handleSource, style: (f) => (f.get('kind') === 'aim' ? aimStyle : edgeStyle) });
    const map = new Map({
      target: containerRef.current,
      layers: [imageLayer, arcLayer, markerLayer, handleLayer],
      view: new View({ projection, center: getCenter(extent), zoom: 1, maxZoom: 8 }),
    });
    map.getView().fit(extent, { padding: [20, 20, 20, 20] });
    mapRef.current = map;

    // Drag a marker to reposition; persist on drop.
    const translate = new Translate({ layers: [markerLayer], hitTolerance: 5 });
    translate.on('translateend', (evt) => {
      const f = evt.features.item(0);
      if (!f) return;
      const p = f.get('placement');
      const [x, y] = f.getGeometry().getCoordinates();
      persistMove(p.id, x, y);
    });
    map.addInteraction(translate);

    // Drag a handle: the AIM knob sets heading; an EDGE knob sets the field-of-view spread. Angles
    // are measured clockwise from up (+y), matching the arc geometry.
    const rotateHandle = new Translate({ layers: [handleLayer], hitTolerance: 8 });
    const handleDrag = (evt) => {
      const f = evt.features.item(0);
      if (!f) return;
      const p = f.get('placement');
      const kind = f.get('kind');
      const r = arcRadius(activeFloor);
      const [hx, hy] = f.getGeometry().getCoordinates();
      let ang = (Math.atan2(hx - p.x, hy - p.y) * 180) / Math.PI;
      if (ang < 0) ang += 360;
      if (kind === 'aim') {
        const heading = ang;
        const rad = (heading * Math.PI) / 180;
        f.getGeometry().setCoordinates([p.x + r * Math.sin(rad), p.y + r * Math.cos(rad)]);
        // Move the edge knobs so the whole wedge rotates with the aim.
        handleSource.getFeatures().forEach((ef) => {
          const k = ef.get('kind');
          if (k === 'edgeL' || k === 'edgeR') {
            const ea = ((heading + (k === 'edgeR' ? (p.fov || 0) / 2 : -(p.fov || 0) / 2)) * Math.PI) / 180;
            ef.getGeometry().setCoordinates([p.x + r * Math.sin(ea), p.y + r * Math.cos(ea)]);
          }
        });
        if (setAimRef.current) setAimRef.current(p.id, { heading });
      } else {
        let d = ang - (p.heading || 0);
        d = ((d + 540) % 360) - 180; // signed offset from the aim direction
        const fov = Math.min(170, Math.max(0, Math.round(Math.abs(d) * 2)));
        const edgeAng = ((p.heading || 0) + (kind === 'edgeR' ? fov / 2 : -fov / 2)) * (Math.PI / 180);
        f.getGeometry().setCoordinates([p.x + r * Math.sin(edgeAng), p.y + r * Math.cos(edgeAng)]);
        if (setAimRef.current) setAimRef.current(p.id, { fov });
      }
    };
    rotateHandle.on('translatestart', () => { rotatingRef.current = true; });
    rotateHandle.on('translating', handleDrag);
    rotateHandle.on('translateend', (evt) => { handleDrag(evt); rotatingRef.current = false; });
    map.addInteraction(rotateHandle);

    // Click a marker to select it (enables the Remove button).
    const select = new Select({ condition: click, layers: [markerLayer], hitTolerance: 5 });
    select.on('select', (e) => {
      const f = e.selected[0];
      setSelectedId(f ? f.get('placement').id : null);
    });
    map.addInteraction(select);

    // Single click on empty plan in placing mode drops the picked node/camera there.
    map.on('singleclick', (evt) => {
      const target = placingRef.current;
      if (!target) return;
      const hit = map.forEachFeatureAtPixel(evt.pixel, (f) => f, { hitTolerance: 5 });
      if (hit) return; // clicked an existing marker — let Select handle it
      const [x, y] = evt.coordinate;
      placeAtRef.current(target, x, y);
      setPlacing(null);
    });

    // Double-click a marker to open its node (single click selects it for removal).
    map.on('dblclick', (evt) => {
      const feat = map.forEachFeatureAtPixel(evt.pixel, (f) => f, { hitTolerance: 5 });
      if (!feat) return;
      const p = feat.get('placement');
      if (p && onOpenNode) { evt.stopPropagation(); onOpenNode(p.nodeId); }
    });

    setMapEpoch((n) => n + 1);
    return () => { if (mapRef.current) { mapRef.current.setTarget(undefined); mapRef.current = null; } markerSourceRef.current = null; arcSourceRef.current = null; handleSourceRef.current = null; };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [activeFloor && activeFloor.id]);

  // Position the aim + edge handles at the selected camera's arc (drag to aim / widen). Skipped
  // while a drag is in progress so replacing features never detaches the OL Translate mid-drag.
  useEffect(() => {
    const hs = handleSourceRef.current;
    if (!hs || rotatingRef.current) return;
    hs.clear();
    const sel = placements.find((p) => p.id === selectedId);
    if (!sel || !sel.cameraId) return;
    const r = arcRadius(activeFloor);
    const heading = sel.heading || 0;
    const fov = sel.fov || 0;
    const mk = (kind, ang) => {
      const rad = (ang * Math.PI) / 180;
      const f = new Feature({ geometry: new Point([sel.x + r * Math.sin(rad), sel.y + r * Math.cos(rad)]) });
      f.set('placement', sel);
      f.set('kind', kind);
      return f;
    };
    hs.addFeatures([mk('aim', heading), mk('edgeL', heading - fov / 2), mk('edgeR', heading + fov / 2)]);
  }, [selectedId, placements, mapEpoch, activeFloor]);

  // Populate/refresh markers (and camera coverage arcs) when placements, the fleet, or the map
  // change. Arcs re-render live as heading/fov change because they key off the same placements.
  useEffect(() => {
    const src = markerSourceRef.current;
    const arcs = arcSourceRef.current;
    if (!src || !arcs) return;
    src.clear();
    arcs.clear();
    const radius = arcRadius(activeFloor);
    arcs.addFeatures(placements
      .filter((p) => p.cameraId && (p.fov || 0) > 0)
      .map((p) => { const f = new Feature({ geometry: new Polygon([arcRing(p, radius)]) }); f.set('placement', p); return f; }));
    src.addFeatures(placements.map((p) => {
      const f = new Feature({ geometry: new Point([p.x, p.y]) });
      f.set('placement', p);
      return f;
    }));
  }, [placements, mapEpoch, nowSec, activeFloor]);

  async function persistMove(id, x, y) {
    try {
      const res = await api(`/api/placements/${id}`, { method: 'PUT', body: JSON.stringify({ x, y }) });
      if (!res.ok) throw new Error();
      setPlacements((list) => list.map((p) => (p.id === id ? { ...p, x, y } : p)));
    } catch (_) {
      if (onToast) onToast(t('map.saveFailed'), 'error');
      loadPlacements(activeFloor && activeFloor.id);
    }
  }

  // setAim updates a camera placement's heading/fov: it applies locally at once (so the arc moves
  // live) and persists on a short debounce so a slider drag isn't a burst of writes.
  const setAim = useCallback((id, patch) => {
    setPlacements((list) => list.map((p) => (p.id === id ? { ...p, ...patch } : p)));
    clearTimeout(aimTimerRef.current);
    aimTimerRef.current = setTimeout(() => {
      api(`/api/placements/${id}`, { method: 'PUT', body: JSON.stringify(patch) }).catch(() => {
        if (onToast) onToast(t('map.saveFailed'), 'error');
      });
    }, 250);
  }, [onToast, t]);
  // Mirror setAim to a ref so the once-bound OL rotate-handle interaction always calls the current one.
  const setAimRef = useRef(setAim);
  setAimRef.current = setAim;

  // placeAt creates a placement at plan-pixel (x, y). Shared by click-to-place and drag-drop.
  const placeAt = useCallback(async (payload, x, y) => {
    if (!payload || !activeFloor) return;
    setBusy(true);
    try {
      const res = await api(`/api/floors/${activeFloor.id}/placements`, {
        method: 'POST',
        body: JSON.stringify({ nodeId: payload.nodeId, cameraId: payload.cameraId || '', lastKnownName: payload.name, x, y }),
      });
      if (!res.ok) throw new Error();
      await loadPlacements(activeFloor.id);
    } catch (_) {
      if (onToast) onToast(t('map.placeFailed'), 'error');
    } finally { setBusy(false); }
  }, [activeFloor, loadPlacements, onToast, t]);
  // Mirror to a ref so the OL singleclick handler (bound once) calls the current placeAt.
  const placeAtRef = useRef(placeAt);
  placeAtRef.current = placeAt;

  // Drop handler for the plan canvas: an image file from the desktop uploads as a new floor
  // (Google-Drive style); otherwise a node/camera dragged from the palette is placed.
  function onDrop(e) {
    if (handleImageFileDrop(e)) return;
    e.preventDefault();
    const raw = e.dataTransfer.getData('text/placement');
    const map = mapRef.current;
    if (!raw || !map || !activeFloor) return;
    let payload;
    try { payload = JSON.parse(raw); } catch (_) { return; }
    const [x, y] = map.getEventCoordinate(e.nativeEvent || e);
    placeAt(payload, x, y);
    setPlacing(null);
  }

  async function removeSelected() {
    if (!selectedId) return;
    setBusy(true);
    try {
      await api(`/api/placements/${selectedId}`, { method: 'DELETE' });
      setSelectedId(null);
      await loadPlacements(activeFloor && activeFloor.id);
    } catch (_) {
      if (onToast) onToast(t('map.error'), 'error');
    } finally { setBusy(false); }
  }

  // Zoom + fit controls for the plan toolbar (mirrors the drawing canvas toolbar).
  function zoomStep(delta) {
    const v = mapRef.current && mapRef.current.getView();
    if (!v) return;
    v.animate({ zoom: (v.getZoom() || 1) + delta, duration: 150 });
  }
  function fitPlan() {
    const m = mapRef.current;
    if (!m || !activeFloor) return;
    const w = activeFloor.width || 1024;
    const h = activeFloor.height || 768;
    m.getView().fit([0, 0, w, h], { padding: [20, 20, 20, 20], duration: 150 });
  }

  // Delete / Backspace removes the selected marker; Escape clears the selection — so a camera can
  // be taken off the plan straight from the keyboard, mirroring the drawing canvas.
  useEffect(() => {
    if (!selectedId) return undefined;
    const onKey = (e) => {
      const el = e.target;
      if (el && (el.tagName === 'INPUT' || el.tagName === 'TEXTAREA' || el.isContentEditable)) return;
      if (e.key === 'Delete' || e.key === 'Backspace') { e.preventDefault(); removeSelected(); }
      else if (e.key === 'Escape') { setSelectedId(null); }
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [selectedId]);

  // --- site/floor management ---
  // Building create/edit go through SiteDialog (name + icon). createSite/editSite just open it;
  // saveSiteDialog persists whichever mode is active.
  function createSite() { setSiteDialog({ mode: 'create', name: '', icon: DEFAULT_BUILDING_ICON }); }
  function editSite() {
    if (!activeSite) return;
    setSiteDialog({ mode: 'edit', id: activeSite.id, name: activeSite.name, icon: activeSite.icon || DEFAULT_BUILDING_ICON });
  }
  async function saveSiteDialog(name, icon) {
    const dlg = siteDialog;
    if (!dlg || !name.trim()) return;
    setBusy(true);
    try {
      if (dlg.mode === 'edit') {
        const res = await api(`/api/sites/${dlg.id}`, { method: 'PUT', body: JSON.stringify({ name: name.trim(), description: activeSite?.description || '', icon, ordinal: activeSite?.ordinal || 0 }) });
        if (!res.ok) throw new Error();
        await loadSites(dlg.id);
        if (onToast) onToast(t('map.siteUpdated'), 'success');
      } else {
        const res = await api('/api/sites', { method: 'POST', body: JSON.stringify({ name: name.trim(), icon }) });
        if (!res.ok) throw new Error();
        const created = await loadSites(res.body && res.body.id);
        if (created) loadFloors(created.id);
        if (onToast) onToast(t('map.siteCreated'), 'success');
      }
      setSiteDialog(null);
    } catch (_) {
      if (onToast) onToast(dlg.mode === 'edit' ? t('map.error') : t('map.siteCreateFailed'), 'error');
    } finally { setBusy(false); }
  }
  async function deleteSite() {
    if (!activeSite) return;
    if (!window.confirm(t('map.siteDeleteConfirm', { name: activeSite.name }))) return;
    setBusy(true);
    try { await api(`/api/sites/${activeSite.id}`, { method: 'DELETE' }); await loadSites(); if (onToast) onToast(t('map.siteDeleted'), 'success'); }
    catch (_) { if (onToast) onToast(t('map.error'), 'error'); } finally { setBusy(false); }
  }
  function pickFile() {
    if (!activeSite) { if (onToast) onToast(t('map.pickSiteFirst'), 'info'); return; }
    if (fileInputRef.current) fileInputRef.current.click();
  }
  // uploadFloorImage sends one image file (from the file picker, a desktop drag-drop, or the
  // drawing designer's rasterized PNG) to the site as a new floor. defaultName seeds the prompt;
  // pass skipPrompt to use it verbatim (the designer already collected a name).
  async function uploadFloorImage(file, defaultName, skipPrompt, design) {
    if (!file || !activeSite) return;
    if (!skipPrompt && !/^image\//.test(file.type || '')) { if (onToast) onToast(t('map.floorNotImage'), 'error'); return; }
    let name = defaultName || (file.name || 'floor').replace(/\.[^.]+$/, '');
    if (!skipPrompt) {
      const entered = window.prompt(t('map.floorNamePrompt'), name);
      if (entered === null) return;
      name = (entered || '').trim() || name;
    }
    setBusy(true);
    try {
      const fd = new FormData();
      fd.append('file', file);
      fd.append('name', name);
      if (design) fd.append('design', design);
      const res = await fetch(`${apiBase()}/api/sites/${activeSite.id}/floors`, { method: 'POST', credentials: 'include', headers: { 'X-CSRF-Token': csrfToken() }, body: fd });
      const body = await res.json().catch(() => ({}));
      if (!res.ok) throw new Error();
      const floor = (body && body.data && body.data.result) || (body && body.result) || null;
      await loadFloors(activeSite.id, floor && floor.id);
      if (onToast) onToast(t('map.floorUploaded'), 'success');
      return floor;
    } catch (_) { if (onToast) onToast(t('map.floorUploadFailed'), 'error'); } finally { setBusy(false); }
    return undefined;
  }

  async function onFileChosen(e) {
    const file = e.target.files && e.target.files[0];
    e.target.value = '';
    await uploadFloorImage(file);
  }

  // saveDesign is called by the drawing designer with the rasterised PNG + the vector JSON. A NEW
  // plan is created; an EDITED one (designer.floor set) replaces that floor's image + design.
  async function saveDesign(blob, planName, design) {
    const editing = designer && designer.floor;
    const safe = (planName || t('map.untitledPlan')).replace(/[^\w.-]+/g, '_');
    const file = new File([blob], `${safe}.png`, { type: 'image/png' });
    if (editing) {
      setBusy(true);
      try {
        const fd = new FormData();
        fd.append('file', file);
        fd.append('name', (planName || editing.name || '').trim());
        fd.append('design', design || '');
        const res = await fetch(`${apiBase()}/api/floors/${editing.id}/image`, { method: 'POST', credentials: 'include', headers: { 'X-CSRF-Token': csrfToken() }, body: fd });
        if (!res.ok) throw new Error();
        await loadFloors(activeSite.id, editing.id);
        setMapEpoch((n) => n + 1); // force the plan image to re-render with the new bytes
        if (onToast) onToast(t('map.floorUpdated'), 'success');
      } catch (_) { if (onToast) onToast(t('map.floorUploadFailed'), 'error'); } finally { setBusy(false); }
    } else {
      await uploadFloorImage(file, planName || t('map.untitledPlan'), true, design);
    }
    setDesigner(null);
  }

  // Drop an image file from the desktop onto the plan area to upload it as a floor (Google-Drive
  // style). Returns true when it handled a file, so the placement-drop path can bail out.
  function handleImageFileDrop(e) {
    const files = e.dataTransfer && e.dataTransfer.files;
    const file = files && files.length ? files[0] : null;
    if (!file) return false;
    e.preventDefault();
    if (!activeSite) { if (onToast) onToast(t('map.pickSiteFirst'), 'info'); return true; }
    uploadFloorImage(file);
    return true;
  }
  async function deleteFloor() {
    if (!activeFloor) return;
    if (!window.confirm(t('map.floorDeleteConfirm', { name: activeFloor.name }))) return;
    setBusy(true);
    try { await api(`/api/floors/${activeFloor.id}`, { method: 'DELETE' }); await loadFloors(activeSite.id); if (onToast) onToast(t('map.floorDeleted'), 'success'); }
    catch (_) { if (onToast) onToast(t('map.error'), 'error'); } finally { setBusy(false); }
  }

  function dragPayload(e, payload) {
    e.dataTransfer.setData('text/placement', JSON.stringify(payload));
    e.dataTransfer.effectAllowed = 'copy';
    // Replace the browser's default text ghost with a bullseye + camera/node chip that reads as
    // "drop a marker here". The ghost element lives off-screen and is reused for every drag.
    if (dragGhostLabelRef.current) dragGhostLabelRef.current.textContent = payload.name || '';
    if (dragGhostRef.current) {
      dragGhostRef.current.classList.toggle('is-cam', !!payload.cameraId);
      try { e.dataTransfer.setDragImage(dragGhostRef.current, 16, 16); } catch (_) { /* older browsers */ }
    }
  }

  // pick sets (or clears) the click-to-place target.
  function pick(payload) {
    setPlacing((cur) => (cur && cur.nodeId === payload.nodeId && cur.cameraId === payload.cameraId ? null : payload));
  }
  const isPicked = (nodeId, cameraId) => placing && placing.nodeId === nodeId && (placing.cameraId || '') === (cameraId || '');

  return (
    <section className="settings-panel span-two indoor-panel">
      <header>
        <h2><span className="btn-icon"><Ico n="building" /> {t('map.viewIndoor')}</span></h2>
        <div className="settings-header-actions">
          {!lockedSiteId ? (
            <>
              <label className="indoor-field">
                <span>{t('map.site')}</span>
                <select value={activeSite ? activeSite.id : ''} onChange={(e) => setActiveSite(sites.find((s) => String(s.id) === e.target.value) || null)} disabled={sites.length === 0}>
                  {sites.length === 0 ? <option value="">{t('map.noSites')}</option> : null}
                  {sites.map((s) => <option key={s.id} value={s.id}>{s.icon ? `${s.icon} ${s.name}` : s.name}</option>)}
                </select>
              </label>
              <button type="button" className="quiet" onClick={createSite} disabled={busy}><span className="btn-icon"><Ico n="plus" sz={14} /> {t('map.addSite')}</span></button>
              {activeSite ? <button type="button" className="quiet" onClick={editSite} disabled={busy} title={t('map.editBuilding')}><span className="btn-icon"><Ico n="edit-2" sz={14} /> {t('map.editBuilding')}</span></button> : null}
              {activeSite ? <button type="button" className="quiet danger-text" onClick={deleteSite} disabled={busy}><span className="btn-icon"><Ico n="trash" sz={14} /> {t('map.deleteSite')}</span></button> : null}
            </>
          ) : null}
          <button type="button" onClick={pickFile} disabled={busy || !activeSite}><span className="btn-icon"><Ico n="download" sz={14} /> {t('map.uploadFloor')}</span></button>
          <button type="button" className="quiet" onClick={() => { if (!activeSite) { if (onToast) onToast(t('map.pickSiteFirst'), 'info'); return; } setDesigner({ floor: null }); }} disabled={busy || !activeSite}><span className="btn-icon"><Ico n="edit-2" sz={14} /> {t('map.createPlan')}</span></button>
          <input ref={fileInputRef} type="file" accept="image/png,image/jpeg,image/gif" style={{ display: 'none' }} onChange={onFileChosen} />
        </div>
      </header>
      <p className="settings-hint">{t('map.indoorHint')}</p>

      {activeSite && floors.length > 0 ? (
        <div className="indoor-floor-tabs">
          {floors.map((f) => (
            <button key={f.id} type="button" className={`indoor-floor-tab${activeFloor && activeFloor.id === f.id ? ' active' : ''}`} onClick={() => setActiveFloor(f)}>{f.name}</button>
          ))}
          {activeFloor ? <button type="button" className="quiet" onClick={() => setDesigner({ floor: activeFloor })} disabled={busy} title={t('map.editPlan')}><span className="btn-icon"><Ico n="edit-2" sz={13} /> {t('map.editPlan')}</span></button> : null}
          {activeFloor ? <button type="button" className="quiet danger-text" onClick={deleteFloor} disabled={busy} title={t('map.deleteFloor')}><Ico n="trash" sz={13} /></button> : null}
        </div>
      ) : null}

      <div className="indoor-body">
        {activeFloor ? (
          <aside className="indoor-palette">
            <div className="palette-head">{t('map.placeThings')}</div>
            <div className="palette-hint">{t('map.placeHint')}</div>
            <ul className="palette-list">
              {nodes.map((n) => {
                const isCamera = (n.kind || 'camera') !== 'iot';
                const cams = camsByNode[n.nodeId];
                return (
                  <li key={n.nodeId} className="palette-node">
                    <div className="palette-node-row">
                      {isCamera ? (
                        <button type="button" className="palette-expand" onClick={() => toggleNode(n.nodeId)} aria-label={t('map.showCameras')}>
                          <Ico n={expanded[n.nodeId] ? 'chev-down' : 'chev-right'} sz={12} />
                        </button>
                      ) : <span className="palette-expand-spacer" />}
                      <button
                        type="button"
                        className={`palette-item${isPicked(n.nodeId, '') ? ' active' : ''}`}
                        draggable
                        onDragStart={(e) => { dragPayload(e, { nodeId: n.nodeId, cameraId: '', name: n.name || n.nodeId }); setPlacing(null); }}
                        onClick={() => pick({ nodeId: n.nodeId, cameraId: '', name: n.name || n.nodeId })}
                        title={t('map.placeHint')}
                      >
                        <span className="rail-dot" style={{ background: nodeTone(n, nowSec).color }} />
                        <span className="rail-name">{n.name || n.nodeId}</span>
                        {isPicked(n.nodeId, '') ? <Ico n="map-pin" sz={13} /> : null}
                      </button>
                    </div>
                    {isCamera && expanded[n.nodeId] ? (
                      <ul className="palette-cams">
                        {cams?.loading ? <li className="palette-cam muted">{t('common.loading')}</li> : null}
                        {cams?.error ? <li className="palette-cam muted">{t('map.camsOffline')}</li> : null}
                        {cams?.cams?.map((c) => (
                          <li key={c.id}>
                            <button
                              type="button"
                              className={`palette-cam${isPicked(n.nodeId, String(c.id)) ? ' active' : ''}`}
                              draggable
                              onDragStart={(e) => { dragPayload(e, { nodeId: n.nodeId, cameraId: String(c.id), name: c.name || t('nodes.cameraN', { id: c.id }) }); setPlacing(null); }}
                              onClick={() => pick({ nodeId: n.nodeId, cameraId: String(c.id), name: c.name || t('nodes.cameraN', { id: c.id }) })}
                              title={t('map.placeHint')}
                            >
                              <Ico n="video" sz={12} /> {c.name || t('nodes.cameraN', { id: c.id })}
                              {isPicked(n.nodeId, String(c.id)) ? <Ico n="map-pin" sz={12} /> : null}
                            </button>
                          </li>
                        ))}
                        {cams && !cams.loading && !cams.error && cams.cams.length === 0 ? <li className="palette-cam muted">{t('map.noCams')}</li> : null}
                      </ul>
                    ) : null}
                  </li>
                );
              })}
            </ul>
          </aside>
        ) : null}

        <div className="indoor-stage">
          {!activeSite ? (
            <div className="indoor-empty">{t('map.indoorEmptyNoSite')}</div>
          ) : floors.length === 0 ? (
            <div
              className={`indoor-dropzone${dropActive ? ' active' : ''}`}
              onDragOver={(e) => { if (Array.from(e.dataTransfer.types || []).includes('Files')) { e.preventDefault(); e.dataTransfer.dropEffect = 'copy'; setDropActive(true); } }}
              onDragLeave={() => setDropActive(false)}
              onDrop={(e) => { setDropActive(false); handleImageFileDrop(e); }}
            >
              <Ico n="download" sz={30} />
              <div className="indoor-dropzone-title">{t('map.dropOrCreate')}</div>
              <div className="indoor-dropzone-sub">{t('map.dropHint')}</div>
              <div className="indoor-dropzone-actions">
                <button type="button" onClick={pickFile} disabled={busy}><span className="btn-icon"><Ico n="download" sz={14} /> {t('map.uploadFloor')}</span></button>
                <button type="button" className="quiet" onClick={() => setDesigner({ floor: null })} disabled={busy}><span className="btn-icon"><Ico n="edit-2" sz={14} /> {t('map.createPlan')}</span></button>
              </div>
            </div>
          ) : (
            <>
              {placing ? (
                <div className="fleet-map-placing" role="status">
                  <span><Ico n="map-pin" sz={14} /> {t('map.placingBanner', { name: placing.name })}</span>
                  <button type="button" className="linklike" onClick={() => setPlacing(null)}>{t('map.cancel')}</button>
                </div>
              ) : null}
              <div className="indoor-plan">
                {(() => {
                  const sel = selectedId ? placements.find((p) => p.id === selectedId) : null;
                  const selName = sel ? (sel.lastKnownName || sel.nodeId) : '';
                  return (
                    <div className="fd-toolbar indoor-toolbar">
                      <span className="indoor-tool-info">
                        {sel ? (
                          <><Ico n={sel.cameraId ? 'video' : 'cpu'} sz={14} /> <strong>{selName}</strong></>
                        ) : (
                          <span className="muted">{t('map.toolbarSelectHint')}</span>
                        )}
                      </span>
                      <button type="button" className="fd-tool danger-text" onClick={removeSelected} disabled={!selectedId || busy} title={t('map.removeMarker')}>
                        <Ico n="trash" sz={15} /> <span>{t('map.removeMarker')}</span>
                      </button>
                      <span className="fd-toolbar-sep" />
                      <button type="button" className="fd-tool" onClick={() => zoomStep(-1)} title={t('fd.zoomOut')}><Ico n="minimize" sz={15} /></button>
                      <button type="button" className="fd-tool" onClick={() => zoomStep(1)} title={t('fd.zoomIn')}><Ico n="plus" sz={15} /></button>
                      <button type="button" className="fd-tool" onClick={fitPlan} title={t('fd.fit')}><Ico n="maximize" sz={15} /> <span>{t('fd.fitLabel')}</span></button>
                    </div>
                  );
                })()}
                <div className={`indoor-canvas${placing ? ' placing' : ''}${canvasDragOver ? ' drag-over' : ''}`} ref={containerRef} role="application" aria-label={t('map.viewIndoor')}
                  onDragOver={(e) => { e.preventDefault(); e.dataTransfer.dropEffect = 'copy'; setCanvasDragOver(true); }}
                  onDragLeave={(e) => { if (!containerRef.current || !containerRef.current.contains(e.relatedTarget)) setCanvasDragOver(false); }}
                  onDrop={(e) => { setCanvasDragOver(false); onDrop(e); }} />
              </div>
            </>
          )}
        </div>
      </div>

      {/* Off-screen custom drag image: a bullseye + label shown while dragging a palette item. */}
      <div ref={dragGhostRef} className="indoor-drag-ghost" aria-hidden="true">
        <span className="indoor-drag-ghost-mark" />
        <span className="indoor-drag-ghost-label" ref={dragGhostLabelRef} />
      </div>

      {designer ? (
        <FloorDesigner
          width={designer.floor ? designer.floor.width : undefined}
          height={designer.floor ? designer.floor.height : undefined}
          backgroundSrc={designer.floor
            ? (designer.floor.design
              ? `${apiBase()}/api/floors/${designer.floor.id}/background?t=${Date.now()}` // drawn/annotated: pristine bg (white, or the original photo)
              : `${apiBase()}/api/floors/${designer.floor.id}/image?t=${Date.now()}`) // plain uploaded: the image IS the background
            : undefined}
          initialShapes={(() => { try { return designer.floor && designer.floor.design ? JSON.parse(designer.floor.design) : []; } catch (_) { return []; } })()}
          initialName={designer.floor ? designer.floor.name : ''}
          onCancel={() => setDesigner(null)}
          onSave={saveDesign}
        />
      ) : null}

      {siteDialog ? (
        <SiteDialog
          mode={siteDialog.mode}
          initialName={siteDialog.name}
          initialIcon={siteDialog.icon}
          busy={busy}
          onSave={saveSiteDialog}
          onCancel={() => setSiteDialog(null)}
        />
      ) : null}
    </section>
  );
}

IndoorMap.propTypes = {
  nodes: PropTypes.array,
  reloadNodes: PropTypes.func,
  onToast: PropTypes.func,
  onOpenNode: PropTypes.func,
  lockedSiteId: PropTypes.number,
  onFloorplansChanged: PropTypes.func,
  onActiveSiteChange: PropTypes.func,
};
