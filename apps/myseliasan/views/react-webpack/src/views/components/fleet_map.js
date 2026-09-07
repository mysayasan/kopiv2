import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import PropTypes from 'prop-types';
import { useT, Ico } from '@shared';
import { api, apiBase } from '../lib/helpers';
import { nodeTone, nodeToneKey, TONES } from '../lib/fleet_status';
import { BuildingFloorView, CameraWindow, MediaWindow } from './node_floor_view';
import { AssetWizard } from './asset_wizard';
import { BuildingEditorDialog } from './building_editor_dialog';
import { normKind, siteGlyph } from './site_kinds';
// The map's own pieces, lifted out of this file so the workspace below is state and composition
// rather than 1,600 lines of cartography, canvas styling and floating cards.
import { basemapStyle } from './map/basemap_style';
import { SEV_COLOR, PULSE_PERIOD, hexToRgba, easeOutCubic, markerShape, pinStyle, siteStyle } from './map/markers';
import { NodeCameraPopup, MapPopupFrame, eventSnapshotSrc, recordingStreamSrc, SEV_RANK } from './map/popups';
import { BasemapDownloadBanner, BasemapSetupDialog } from './map/basemap_ui';
import { TwinTree } from './map/twin_tree';

// OpenLayers, driven directly through refs (no React wrapper - see the note in Phase 0).
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
import { Fill, Stroke, Style, Circle as CircleStyle } from 'ol/style.js';
import { getVectorContext } from 'ol/render.js';
import { PMTilesVectorSource } from 'ol-pmtiles';
import 'ol/ol.css';
import '../styles/fleet-map.css';
import '../styles/twin-tree.css';

const DEFAULT_CENTER = [109.45, 4.15];
const DEFAULT_ZOOM = 6;


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
  // nodeId -> { loading, error, cams }. `error` is load-bearing: an unreachable node's cameras are
  // UNKNOWN, and the tree must never render that as "nothing left to place".
  const [camsByNode, setCamsByNode] = useState({});
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
  // The tree's open branches, as one set of keys ("places", "tray", "site:3", "area:11") rather
  // than a map per level - a tree has one expansion state, whatever the depth of the row.
  const [expanded, setExpanded] = useState(() => new Set(['places', 'tray']));
  // Per-site areas WITH their placements, so expanding a place yields its areas and the cameras
  // pinned in them in one request rather than a second round-trip per area.
  const [plansBySite, setPlansBySite] = useState({});
  // Fleet-wide "what already holds a pin", so the tray can subtract it from each node's live
  // camera list. It is the placement index the editor palette already uses.
  const [placedKeys, setPlacedKeys] = useState(() => new Set());
  const [treeQuery, setTreeQuery] = useState('');
  // Adding a building is a three-beat flow owned here: the wizard collects name/glyph/areas, the
  // map takes the drop point, then the editor opens on the building just created. editorSite is
  // also the re-entry point for an EXISTING building (from the rail or the drill-down).
  const [wizardOpen, setWizardOpen] = useState(false);
  const [editorSite, setEditorSite] = useState(null);
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

  // Every adopted node's live camera list, over the tunnel. It answers two questions at once:
  //   marker health - "online / total" on a place's badge, so a camera that drops while its NODE
  //                   is still up is not invisible (a place's tone only tracks node status);
  //   the tray      - which of a recorder's cameras hold no pin yet.
  // It covers the WHOLE fleet rather than only placed sites, because the tray's whole job is the
  // cameras that are nowhere yet - and those live on nodes that may sit at no place at all.
  //
  // A node that cannot be reached is recorded as `error`, never as an empty list. "We could not
  // ask" and "there is nothing left to place" are different answers, and collapsing them would
  // quietly tell an operator the job is finished.
  useEffect(() => {
    let live = true;
    const ids = nodes.map((n) => n && n.nodeId).filter(Boolean);
    if (ids.length === 0) { setCamHealth({}); setCamsByNode({}); return undefined; }
    setCamsByNode((m) => {
      const next = { ...m };
      ids.forEach((nid) => { if (!next[nid]) next[nid] = { loading: true, cams: [] }; });
      return next;
    });
    Promise.all(ids.map((nid) => api(`/api/nodes/${encodeURIComponent(nid)}/proxy/api/cameras?limit=200`, { noRedirect: true })
      .then((r) => ({ nid, ok: !!r.ok, list: r.ok ? (Array.isArray(r.body) ? r.body : (r.body?.items || [])) : [] }))
      .catch(() => ({ nid, ok: false, list: [] }))))
      .then((results) => {
        if (!live) return;
        const health = {};
        const byNode = {};
        results.forEach(({ nid, ok, list }) => {
          byNode[nid] = ok ? { loading: false, cams: list } : { loading: false, error: true, cams: [] };
          if (ok) list.forEach((c) => { health[`${nid}::${c.id}`] = (c.healthStatus || '').toLowerCase(); });
        });
        setCamHealth(health);
        setCamsByNode(byNode);
      });
    return () => { live = false; };
  }, [nodes]);

  // Fleet-wide placement index: every camera (and appliance) that already holds a pin. The tray
  // subtracts it from each node's camera list. Refreshed alongside the site overview so placing
  // something in the editor removes it from the tray without a reload.
  const reloadPlaced = useCallback(() => api('/api/placements', { noRedirect: true })
    .then((r) => {
      const rows = r.ok && Array.isArray(r.body) ? r.body : [];
      setPlacedKeys(new Set(rows.map((p) => `${p.nodeId}::${p.cameraId || ''}`)));
    })
    .catch(() => {}), []);
  useEffect(() => { reloadPlaced(); }, [reloadPlaced, sites]);

  // A site's cameras are the ones PLACED there — for every kind, including a point asset, which
  // now owns an implicit area to pin them to.
  //
  // This used to fall back, for a point asset, to "every camera on every appliance assigned here".
  // That is wrong whenever one recorder feeds more than one place, which is the normal case: an
  // NVR with cameras in two buildings, assigned to a junction, made the junction claim all of
  // them. A camera holds exactly one pin fleet-wide, so a placement is the only honest answer to
  // "what is here".
  const resolvedCamKeys = useCallback((row) => row.cameraKeys || [], []);


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

  // Clicking a site opens its plans with EVERY camera on them (multi-node) — the digital-twin
  // drill, shown even with no plans yet. Every kind takes this path now, a point asset included:
  // it owns one implicit area, so the cameras on that junction or gate are pinned things you can
  // see and click, not an inferred list borrowed from whichever appliance was assigned to it.
  // focusFloorId opens the drill-down on a SPECIFIC area (clicked in the rail), rather than the
  // default first plan — otherwise clicking one area could land you on the other.
  const openBuilding = useCallback(async (site, px, focusFloorId, focusCameraId) => {
    let plans = [];
    try {
      const res = await api(`/api/sites/${site.id}/floorplans`);
      plans = res.ok && Array.isArray(res.body) ? res.body.filter((p) => p && p.floor) : [];
    } catch (_) { /* open empty */ }
    setPopup(null);
    setDrill({ kind: 'site', site, floorplans: plans, focusFloorId, focusCameraId });
  }, []);
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
      setPlacing({ kind: 'site', id: created.id, name: created.name, siteKind: normKind(siteKind), thenEdit: true });
      if (onToast) onToast(t('bld.createdPlaceIt', { name: created.name }), 'success');
    } catch (_) {
      if (onToast) onToast(t('map.siteCreateFailed'), 'error');
    } finally { setBusy(false); }
  }, [onToast, t]);

  // Open the authoring dialog for an asset. Takes the site row (id/name/icon/kind) from wherever the
  // operator asked — rail row, drill-down header, or the drop that just finished. A point asset
  // opens the same editor as everything else: it has no walls to draw, but it has an area to drop
  // its cameras onto and aim them, which is the only way its cameras get placed at all.
  const openEditor = useCallback((site) => {
    setPopup(null);
    setDrill(null);
    setEditorSite(site);
  }, []);
  const openEditorRef = useRef(openEditor);
  openEditorRef.current = openEditor;

  // --- what the tree's rows do -----------------------------------------------------------------
  // Deliberately the SAME verbs the flat rail had, so this phase changes the shape of the rail and
  // nothing about what clicking things does. A place that is on the map flies to it; one that is
  // not enters placing mode, because "drop me somewhere" is the only thing left to do with it.
  const openSiteFromTree = useCallback((row) => {
    const s = row.site;
    if (s.mapPlaced) { flyToSite(s); return; }
    setPlacing((cur) => (cur && cur.kind === 'site' && cur.id === s.id
      ? null
      : { kind: 'site', id: s.id, name: s.name, siteKind: normKind(s.kind) }));
  }, [flyToSite]);

  // A camera leaf opens the plan it is pinned to, with that camera highlighted - the tree found
  // it, so the map should land on it rather than on the area in general.
  const openCameraFromTree = useCallback((site, floor, placement) => {
    openBuilding(site, null, floor.id, placement.cameraId);
  }, [openBuilding]);

  // An appliance row opens its device card. The popup anchors to viewport coordinates, so the
  // click's own position is what keeps the card next to the row it came from.
  const selectNodeFromTree = useCallback((node, x, y) => {
    if (!node || !node.nodeId) return;
    setDrill(null);
    setPopup({ node, x: Math.round(x || 0), y: Math.round(y || 0) });
  }, []);
  // The OL click handler is bound once, so it reads the live site list through a ref to resolve
  // the id it just placed into the full row the editor needs.
  const sitesRef = useRef(sites);
  sitesRef.current = sites;

  // Lazily fetch a place's areas WITH their placements the first time its branch opens. One call
  // (/floorplans, not /floors) because the tree's leaves are the cameras pinned in each area, so
  // fetching areas alone would only mean a second request per area a moment later.
  const loadSitePlans = useCallback(async (siteId) => {
    setPlansBySite((m) => ({ ...m, [siteId]: { ...(m[siteId] || { list: [] }), loading: true } }));
    try {
      const res = await api(`/api/sites/${siteId}/floorplans`, { noRedirect: true });
      const list = res.ok && Array.isArray(res.body)
        ? res.body.filter((p) => p && p.floor).sort((a, b) => (a.floor.ordinal || 0) - (b.floor.ordinal || 0))
        : [];
      setPlansBySite((m) => ({ ...m, [siteId]: { loading: false, list } }));
    } catch (_) {
      setPlansBySite((m) => ({ ...m, [siteId]: { loading: false, error: true, list: [] } }));
    }
  }, []);
  const toggleBranch = useCallback((key) => {
    setExpanded((cur) => {
      const next = new Set(cur);
      if (next.has(key)) next.delete(key); else next.add(key);
      return next;
    });
  }, []);
  // Load the areas of any open place we don't have yet. Driven off state rather than the click, so
  // an editor change that drops the cache re-loads the branch that is still open.
  useEffect(() => {
    expanded.forEach((key) => {
      if (key.indexOf('site:') !== 0) return;
      const id = Number(key.slice(5));
      if (id && !plansBySite[id]) loadSitePlans(id);
    });
  }, [expanded, plansBySite, loadSitePlans]);

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
        }
      });
      // Close the popups when the map moves (they would otherwise float away from their marker).
      map.on('movestart', () => setPopup(null));

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
        <TwinTree
          sites={sites}
          nodes={nodes}
          nodesById={nodesById}
          nowSec={nowSec}
          plansBySite={plansBySite}
          camsByNode={camsByNode}
          placedKeys={placedKeys}
          expanded={expanded}
          onToggle={toggleBranch}
          query={treeQuery}
          onQuery={setTreeQuery}
          placing={placing}
          onAddSite={() => setWizardOpen(true)}
          onOpenSite={openSiteFromTree}
          onOpenArea={(site, floorId) => openBuilding(site, null, floorId)}
          onOpenCamera={openCameraFromTree}
          onEditSite={openEditor}
          onSelectNode={selectNodeFromTree}
          onPlayCamera={playCamera}
        />

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
            <BasemapDownloadBanner
              canDownload={canDownload}
              downloading={downloading}
              envManaged={bmConfig.envManaged}
              onDownload={downloadRegion}
              onSetUp={() => setSetupOpen(true)}
            />
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
          {drill ? (
            <div className="fleet-map-drill">
              <BuildingFloorView site={drill.site} floorplans={drill.floorplans} nodesById={nodesById} notifByCam={notifByCam} focusCameraId={drill.focusCameraId} focusFloorId={drill.focusFloorId} onBack={() => { const s = drill.site; setDrill(null); flyToSite(s); }} onPlay={playCamera} onRemovePlacements={removeGhostPlacements} onEdit={openEditor} />
            </div>
          ) : null}
        </div>
      </div>
      {attribution ? <div className="fleet-map-attribution">{attribution}</div> : null}

      {setupOpen ? (
        <BasemapSetupDialog config={bmConfig} onSave={saveSource} onCancel={() => setSetupOpen(false)} />
      ) : null}

      {wizardOpen ? (
        <AssetWizard busy={busy} onCreate={createBuilding} onCancel={() => setWizardOpen(false)} />
      ) : null}

      {editorSite ? (
        <BuildingEditorDialog
          site={editorSite}
          nodes={nodes}
          onToast={onToast}
          onClose={() => { const id = editorSite.id; setEditorSite(null); setPlansBySite((m) => { const c = { ...m }; delete c[id]; return c; }); if (siteReloadRef.current) siteReloadRef.current(); }}
          onChanged={() => { setPlansBySite((m) => { const c = { ...m }; delete c[editorSite.id]; return c; }); if (siteReloadRef.current) siteReloadRef.current(); }}
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
