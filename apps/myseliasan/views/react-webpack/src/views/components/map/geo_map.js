import { useEffect, useRef } from 'react';
import PropTypes from 'prop-types';
import { useT } from '@shared';
import { api, apiBase } from '../../lib/helpers';
import { nodeTone, nodeToneKey, TONES } from '../../lib/fleet_status';

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
import { Fill, Stroke, Style, Circle as CircleStyle, Text } from 'ol/style.js';
import { getVectorContext } from 'ol/render.js';
import { PMTilesVectorSource } from 'ol-pmtiles';
import 'ol/ol.css';

// GeoMap is the WORLD level of the map workspace: an offline vector basemap with two marker
// layers — buildings (where cameras physically live) and building-less appliances (standalone /
// IoT / off-site nodes). It is presentational: selection, drilling and placement are reported up
// through callbacks; the workspace owns all state. Extracted from the old monolithic FleetMap so
// the same map can live inside the unified three-pane shell.

const DEFAULT_CENTER = [109.45, 4.15];
const DEFAULT_ZOOM = 6;
export const DEFAULT_BUILDING_GLYPH = '🏢';
const GLYPH_FONT = '15px "Segoe UI Emoji", "Noto Color Emoji", "Apple Color Emoji", system-ui, sans-serif';

// --- basemap cartography (place labels + fills), keyed on the Protomaps layer name ---
const BASE_STYLES = {
  earth: new Style({ fill: new Fill({ color: '#f3f1ec' }) }),
  landcover: new Style({ fill: new Fill({ color: '#e6ece1' }) }),
  landuse: new Style({ fill: new Fill({ color: '#e9ece4' }) }),
  water: new Style({ fill: new Fill({ color: '#b9d7e6' }) }),
  buildings: new Style({ fill: new Fill({ color: '#e2ded6' }) }),
  roads: new Style({ stroke: new Stroke({ color: '#ffffff', width: 1.2 }) }),
};
const COUNTRY_BORDER = new Style({ stroke: new Stroke({ color: '#a9a294', width: 1.4 }) });
const REGION_BORDER = new Style({ stroke: new Stroke({ color: '#c3bdb3', width: 1, lineDash: [3, 3] }) });
const halo = (w) => new Stroke({ color: 'rgba(255,255,255,0.9)', width: w });
const COUNTRY_LABEL = new Style({ text: new Text({ font: '700 12px system-ui, sans-serif', fill: new Fill({ color: '#3f3d38' }), stroke: halo(3), overflow: true }) });
const REGION_LABEL = new Style({ text: new Text({ font: '600 11px system-ui, sans-serif', fill: new Fill({ color: '#6b6a63' }), stroke: halo(2.5), overflow: true }) });
const CITY_LABEL = new Style({
  image: new CircleStyle({ radius: 2.6, fill: new Fill({ color: '#5b5a54' }), stroke: new Stroke({ color: '#ffffff', width: 1 }) }),
  text: new Text({ font: '500 11px system-ui, sans-serif', fill: new Fill({ color: '#33322e' }), stroke: halo(2.5), offsetY: -10 }),
});
const WATER_LABEL = new Style({ text: new Text({ font: 'italic 400 11px system-ui, sans-serif', fill: new Fill({ color: '#3d6b85' }), stroke: halo(2), overflow: true }) });
const R0 = 156543.03392804097;
const zoomForResolution = (res) => Math.log2(R0 / res);
const placeLabel = (f) => f.get('name:en') || f.get('name') || '';

function basemapStyle(feature, resolution) {
  const layer = feature.get('layer');
  const zoom = zoomForResolution(resolution);
  if (layer === 'boundaries') return feature.get('kind') === 'country' ? COUNTRY_BORDER : REGION_BORDER;
  if (layer === 'places') {
    const label = placeLabel(feature);
    if (!label) return null;
    const minZoom = feature.get('min_zoom');
    if (typeof minZoom === 'number' && zoom + 0.4 < minZoom) return null;
    const kind = feature.get('kind');
    if (kind === 'country') { COUNTRY_LABEL.getText().setText(label.toUpperCase()); return COUNTRY_LABEL; }
    if (kind === 'region') { REGION_LABEL.getText().setText(label.toUpperCase()); return REGION_LABEL; }
    if (kind === 'locality') {
      const big = (feature.get('population_rank') || 0) >= 11;
      const txt = CITY_LABEL.getText();
      txt.setFont(big ? '600 12px system-ui, sans-serif' : '500 11px system-ui, sans-serif');
      txt.setText(label);
      return CITY_LABEL;
    }
    return null;
  }
  if (layer === 'water') {
    const fill = BASE_STYLES.water;
    const label = placeLabel(feature);
    if (label && zoom >= 5) { WATER_LABEL.getText().setText(label); return [fill, WATER_LABEL]; }
    return fill;
  }
  if (layer === 'roads') {
    const base = BASE_STYLES.roads;
    return base;
  }
  return BASE_STYLES[layer] || null;
}

// --- marker styles ---
function hexToRgba(hex, alpha) {
  const h = hex.replace('#', '');
  return `rgba(${parseInt(h.slice(0, 2), 16)}, ${parseInt(h.slice(2, 4), 16)}, ${parseInt(h.slice(4, 6), 16)}, ${alpha})`;
}
const easeOutCubic = (t) => 1 - Math.pow(1 - t, 3);
const PULSE_PERIOD = 1800;
const SEV_RANK = { critical: 3, warning: 2, info: 1 };
const worseSev = (a, b) => ((SEV_RANK[b] || 0) > (SEV_RANK[a] || 0) ? b : a);
const SEV_COLOR = { critical: '#ef4444', warning: '#f59e0b', info: '#3b82f6' };
const HOVER = '#2f6bd6';

function buildingStyle(feature) {
  const tone = feature.get('tone') || TONES.idle;
  const cams = feature.get('cameras') || 0;
  const icon = feature.get('icon') || DEFAULT_BUILDING_GLYPH;
  const selected = feature.get('selected');
  const hover = feature.get('hover');
  const notif = feature.get('notif'); // { count, sev } | null
  const styles = [];
  // Hover / selected halo — a clear "this is clickable" affordance for the canvas marker.
  if (hover || selected) {
    styles.push(new Style({ image: new CircleStyle({ radius: 19, fill: new Fill({ color: hexToRgba(HOVER, hover ? 0.16 : 0.1) }) }) }));
  }
  styles.push(new Style({ image: new CircleStyle({ radius: hover ? 15 : 14, fill: new Fill({ color: '#ffffff' }), stroke: new Stroke({ color: (selected || hover) ? HOVER : tone.ring, width: (selected || hover) ? 4 : 3 }) }) }));
  styles.push(new Style({ text: new Text({ text: icon, font: GLYPH_FONT }) }));
  styles.push(new Style({ text: new Text({ text: feature.get('name') || '', offsetY: 24, font: '600 11px system-ui, sans-serif', fill: new Fill({ color: '#1f2937' }), stroke: halo(3) }) }));
  // Camera-count badge (bottom-right).
  if (cams > 0) {
    styles.push(new Style({ image: new CircleStyle({ radius: 8, fill: new Fill({ color: tone.color }), stroke: new Stroke({ color: '#ffffff', width: 1.5 }), displacement: [12, -12] }) }));
    styles.push(new Style({ text: new Text({ text: cams > 99 ? '99+' : String(cams), font: '700 9px system-ui, sans-serif', fill: new Fill({ color: '#ffffff' }), offsetX: 12, offsetY: 12 }) }));
  }
  // Unread-notification badge (top-right, severity coloured). The blink itself is the beacon.
  if (notif && notif.count > 0) {
    styles.push(new Style({ image: new CircleStyle({ radius: 8, fill: new Fill({ color: SEV_COLOR[notif.sev] || '#ef4444' }), stroke: new Stroke({ color: '#ffffff', width: 1.5 }), displacement: [12, 12] }) }));
    styles.push(new Style({ text: new Text({ text: notif.count > 99 ? '99+' : String(notif.count), font: '700 9px system-ui, sans-serif', fill: new Fill({ color: '#ffffff' }), offsetX: 12, offsetY: -12 }) }));
  }
  return styles;
}

function pinStyle(clusterFeature) {
  const members = clusterFeature.get('features') || [];
  const hover = clusterFeature.get('hover');
  if (members.length === 1) {
    const f = members[0];
    const tone = f.get('tone') || TONES.idle;
    const selected = f.get('selected');
    const notif = f.get('notif');
    const styles = [];
    if (hover || selected) styles.push(new Style({ image: new CircleStyle({ radius: 15, fill: new Fill({ color: hexToRgba(HOVER, hover ? 0.16 : 0.1) }) }) }));
    styles.push(new Style({
      image: new CircleStyle({ radius: hover ? 10 : 9, fill: new Fill({ color: tone.color }), stroke: new Stroke({ color: (selected || hover) ? HOVER : '#ffffff', width: (selected || hover) ? 3 : 2 }) }),
      text: new Text({ text: f.get('name') || '', offsetY: 18, font: '600 11px system-ui, sans-serif', fill: new Fill({ color: '#1f2937' }), stroke: halo(3) }),
    }));
    if (notif && notif.count > 0) {
      styles.push(new Style({ image: new CircleStyle({ radius: 7, fill: new Fill({ color: SEV_COLOR[notif.sev] || '#ef4444' }), stroke: new Stroke({ color: '#ffffff', width: 1.5 }), displacement: [10, 10] }) }));
      styles.push(new Style({ text: new Text({ text: notif.count > 99 ? '99+' : String(notif.count), font: '700 9px system-ui, sans-serif', fill: new Fill({ color: '#ffffff' }), offsetX: 10, offsetY: -10 }) }));
    }
    return styles;
  }
  return [new Style({
    image: new CircleStyle({ radius: hover ? 14 : 13, fill: new Fill({ color: 'rgba(255,255,255,0.92)' }), stroke: new Stroke({ color: hover ? HOVER : TONES.idle.ring, width: 3 }) }),
    text: new Text({ text: String(members.length), font: '600 12px system-ui, sans-serif', fill: new Fill({ color: '#334155' }) }),
  })];
}

export function GeoMap({
  sites = [], nodes = [], nodesById = {}, notifByNode = {}, notifByCam = {}, showLayers = { buildings: true, nodes: true },
  editMode = false, selectedKey = null, placing = null,
  onEnterBuilding, onSelectNode, onPlace, onMoveBuilding, onMoveNode, onBackground, onAssignNodeToBuilding,
}) {
  const t = useT();
  const containerRef = useRef(null);
  const mapRef = useRef(null);
  const buildingSrcRef = useRef(null);
  const nodeSrcRef = useRef(null);
  const buildingLayerRef = useRef(null);
  const nodeLayerRef = useRef(null);
  const bTranslateRef = useRef(null);
  const nTranslateRef = useRef(null);
  const nowSec = Math.floor(Date.now() / 1000);

  // Live-value refs so the once-bound OL handlers always read current props.
  const placingRef = useRef(placing); placingRef.current = placing;
  const cbRef = useRef({}); cbRef.current = { onEnterBuilding, onSelectNode, onPlace, onMoveBuilding, onMoveNode, onBackground, onAssignNodeToBuilding };

  // Build the map once.
  useEffect(() => {
    let cancelled = false;
    let ro = null;
    const timers = [];
    let onResize = null;
    (async () => {
      let info = null;
      try { const res = await api('/api/basemap/info'); info = res.ok ? res.body : null; } catch (_) { info = null; }
      if (cancelled || !containerRef.current) return;
      const layers = [];
      if (info && info.available) {
        layers.push(new VectorTileLayer({
          declutter: true,
          source: new PMTilesVectorSource({ url: `${apiBase()}/api/basemap/tiles.pmtiles`, attributions: info.attribution || undefined }),
          style: basemapStyle,
        }));
      }
      const buildingSrc = new VectorSource();
      buildingSrcRef.current = buildingSrc;
      const buildingLayer = new VectorLayer({ source: buildingSrc, style: buildingStyle });
      buildingLayerRef.current = buildingLayer;
      layers.push(buildingLayer);

      const nodeSrc = new VectorSource();
      nodeSrcRef.current = nodeSrc;
      const clusterSrc = new Cluster({ distance: 36, minDistance: 18, source: nodeSrc });
      const nodeLayer = new VectorLayer({ source: clusterSrc, style: pinStyle });
      nodeLayerRef.current = nodeLayer;
      layers.push(nodeLayer);

      // Critical-building beacon (behind the marker), redrawn each frame while any is on screen.
      buildingLayer.on('prerender', (evt) => {
        const vctx = getVectorContext(evt);
        const time = evt.frameState ? evt.frameState.time : 0;
        const base = (time % PULSE_PERIOD) / PULSE_PERIOD;
        let animating = false;
        for (const f of buildingSrc.getFeatures()) {
          if (!f.get('critical')) continue;
          animating = true;
          const color = f.get('beaconColor') || (f.get('tone') || TONES.idle).ring;
          const geom = f.getGeometry();
          for (const offset of [0, 0.5]) {
            const p = (base + offset) % 1;
            const grown = easeOutCubic(p);
            const alpha = 0.5 * (1 - p);
            if (alpha <= 0.01) continue;
            vctx.setStyle(new Style({ image: new CircleStyle({ radius: 14 + grown * 16, stroke: new Stroke({ color: hexToRgba(color, alpha), width: 2.5 - grown }) }) }));
            vctx.drawGeometry(geom);
          }
        }
        if (animating && mapRef.current) mapRef.current.render();
      });

      // The offline basemap is a regional extract, so constrain the view to its bounds — panning
      // outside would only show blank tiles. Without bounds (no archive) leave the view free.
      let view;
      const b = info && Array.isArray(info.bounds) && info.bounds.length === 4 ? info.bounds : null;
      if (b) {
        const extent = [...fromLonLat([b[0], b[1]]), ...fromLonLat([b[2], b[3]])];
        view = new View({ center: getCenter(extent), zoom: DEFAULT_ZOOM, minZoom: 5, extent, showFullExtent: true });
      } else {
        view = new View({ center: fromLonLat(DEFAULT_CENTER), zoom: DEFAULT_ZOOM });
      }
      const map = new Map({ target: containerRef.current, layers, view, controls: [] });
      mapRef.current = map;

      const fit = () => { if (mapRef.current) mapRef.current.updateSize(); };
      fit();
      requestAnimationFrame(fit);
      [150, 400, 900].forEach((d) => timers.push(setTimeout(fit, d)));
      onResize = fit; window.addEventListener('resize', onResize);
      if (typeof ResizeObserver !== 'undefined') { ro = new ResizeObserver(fit); ro.observe(containerRef.current); }

      // Drag interactions (active only in edit mode — see the editMode effect).
      const bTranslate = new Translate({ layers: [buildingLayer], hitTolerance: 5 });
      bTranslate.on('translateend', (evt) => {
        const f = evt.features.item(0); if (!f) return;
        const [lon, lat] = toLonLat(f.getGeometry().getCoordinates());
        if (cbRef.current.onMoveBuilding) cbRef.current.onMoveBuilding(f.get('site').id, lon, lat);
      });
      bTranslate.setActive(false); map.addInteraction(bTranslate); bTranslateRef.current = bTranslate;

      // buildingAt returns the building feature under a pixel (or null) — for drop-to-assign.
      const buildingAt = (pixel) => {
        let b = null;
        map.forEachFeatureAtPixel(pixel, (f, layer) => { if (layer === buildingLayerRef.current && !b) b = f; }, { hitTolerance: 10 });
        return b;
      };

      const nTranslate = new Translate({ layers: [nodeLayer], hitTolerance: 5 });
      nTranslate.on('translateend', (evt) => {
        const cf = evt.features.item(0); if (!cf) return;
        const members = cf.get('features') || [];
        if (members.length !== 1) { clusterSrc.refresh(); return; }
        const nodeId = members[0].get('node').nodeId;
        const coord = cf.getGeometry().getCoordinates();
        // Dropped onto a building? → the appliance now RESIDES there (no standalone pin), like
        // dropping a camera onto a floor. Otherwise it's just a reposition.
        const overB = buildingAt(map.getPixelFromCoordinate(coord));
        if (overB && cbRef.current.onAssignNodeToBuilding) { cbRef.current.onAssignNodeToBuilding(nodeId, overB.get('site').id); clusterSrc.refresh(); return; }
        members[0].getGeometry().setCoordinates(coord);
        const [lon, lat] = toLonLat(coord);
        if (cbRef.current.onMoveNode) cbRef.current.onMoveNode(nodeId, lon, lat);
      });
      nTranslate.setActive(false); map.addInteraction(nTranslate); nTranslateRef.current = nTranslate;

      map.on('singleclick', (evt) => {
        const target = placingRef.current;
        if (target) {
          // Placing an appliance onto a building assigns it there instead of dropping a pin.
          if (target.kind === 'node') {
            const overB = buildingAt(evt.pixel);
            if (overB && cbRef.current.onAssignNodeToBuilding) { cbRef.current.onAssignNodeToBuilding(target.id, overB.get('site').id); return; }
          }
          const [lon, lat] = toLonLat(evt.coordinate);
          if (cbRef.current.onPlace) cbRef.current.onPlace(target, lon, lat);
          return;
        }
        let hitB = null; let hitN = null;
        map.forEachFeatureAtPixel(evt.pixel, (f, layer) => {
          if (layer === buildingLayerRef.current && !hitB) hitB = f;
          else if (layer === nodeLayerRef.current && !hitN) hitN = f;
        }, { hitTolerance: 5 });
        if (hitB) { if (cbRef.current.onEnterBuilding) cbRef.current.onEnterBuilding(hitB.get('site')); return; }
        const members = hitN ? (hitN.get('features') || []) : [];
        if (members.length === 1) { if (cbRef.current.onSelectNode) cbRef.current.onSelectNode(members[0].get('node')); return; }
        if (cbRef.current.onBackground) cbRef.current.onBackground();
      });

      // Hover feedback: pointer cursor + a highlight halo, so a canvas marker reads as clickable.
      let hoverFeat = null;
      map.on('pointermove', (evt) => {
        if (evt.dragging) return;
        let hit = null;
        map.forEachFeatureAtPixel(evt.pixel, (f, layer) => { if ((layer === buildingLayerRef.current || layer === nodeLayerRef.current) && !hit) hit = f; }, { hitTolerance: 5 });
        const el = map.getTargetElement();
        if (el) el.style.cursor = (hit || placingRef.current) ? 'pointer' : '';
        if (hit !== hoverFeat) {
          if (hoverFeat) hoverFeat.set('hover', false);
          if (hit) hit.set('hover', true);
          hoverFeat = hit;
          if (buildingLayerRef.current) buildingLayerRef.current.changed();
          if (nodeLayerRef.current) nodeLayerRef.current.changed();
        }
      });

      // Beacon for a critical building-less node pin (mirrors the building beacon).
      nodeLayer.on('prerender', (evt) => {
        const vctx = getVectorContext(evt);
        const time = evt.frameState ? evt.frameState.time : 0;
        const base = (time % PULSE_PERIOD) / PULSE_PERIOD;
        let animating = false;
        for (const cf of clusterSrc.getFeatures()) {
          const members = cf.get('features') || [];
          if (members.length !== 1 || !members[0].get('critical')) continue;
          animating = true;
          const color = members[0].get('beaconColor') || (members[0].get('tone') || TONES.idle).ring;
          const geom = cf.getGeometry();
          for (const offset of [0, 0.5]) {
            const p = (base + offset) % 1;
            const alpha = 0.5 * (1 - p);
            if (alpha <= 0.01) continue;
            vctx.setStyle(new Style({ image: new CircleStyle({ radius: 9 + easeOutCubic(p) * 14, stroke: new Stroke({ color: hexToRgba(color, alpha), width: 2.5 - easeOutCubic(p) }) }) }));
            vctx.drawGeometry(geom);
          }
        }
        if (animating && mapRef.current) mapRef.current.render();
      });
    })();
    return () => {
      cancelled = true;
      timers.forEach(clearTimeout);
      if (onResize) window.removeEventListener('resize', onResize);
      if (ro) ro.disconnect();
      if (mapRef.current) { mapRef.current.setTarget(undefined); mapRef.current = null; }
      buildingSrcRef.current = null; nodeSrcRef.current = null;
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  // Populate building markers.
  useEffect(() => {
    const src = buildingSrcRef.current; if (!src) return;
    src.clear();
    const order = ['critical', 'warning', 'online', 'idle'];
    src.addFeatures(sites.filter((s) => s.site && s.site.mapPlaced).map((row) => {
      const s = row.site;
      let worst = 'idle';
      (row.nodeIds || []).forEach((nid) => { const k = nodeToneKey(nodesById[nid], nowSec); if (order.indexOf(k) < order.indexOf(worst)) worst = k; });
      // Unread notifications from the cameras PHYSICALLY IN this building (per-camera, not the whole
      // node's tally — a node may record cameras in several buildings).
      let count = 0; let sev = 'info';
      (row.cameraKeys || []).forEach((key) => { const c = notifByCam[key]; if (c && c.count) { count += c.count; sev = worseSev(sev, c.sev); } });
      const notif = count > 0 ? { count, sev } : null;
      const f = new Feature({ geometry: new Point(fromLonLat([s.lon, s.lat])) });
      f.set('site', s); f.set('tone', TONES[worst]); f.set('cameras', row.cameras || 0);
      f.set('name', s.name); f.set('icon', s.icon || DEFAULT_BUILDING_GLYPH); f.set('notif', notif);
      // Blink when the building is lost OR it has an unread warning/critical notification.
      f.set('critical', worst === 'critical' || (notif && notif.sev !== 'info'));
      f.set('beaconColor', worst === 'critical' ? TONES.critical.ring : (notif && notif.sev !== 'info' ? SEV_COLOR[notif.sev] : TONES[worst].ring));
      f.set('selected', selectedKey === `building:${s.id}`);
      return f;
    }));
    if (buildingLayerRef.current) buildingLayerRef.current.setVisible(showLayers.buildings);
  }, [sites, nodesById, notifByCam, nowSec, showLayers.buildings, selectedKey]);

  // Populate building-less node pins.
  useEffect(() => {
    const src = nodeSrcRef.current; if (!src) return;
    src.clear();
    src.addFeatures(nodes.filter((n) => n && n.mapPlaced && !n.siteId).map((n) => {
      const notif = notifByNode[n.nodeId] || null;
      const f = new Feature({ geometry: new Point(fromLonLat([n.lon, n.lat])) });
      const tk = nodeToneKey(n, nowSec);
      f.set('node', n); f.set('tone', nodeTone(n, nowSec)); f.set('name', n.name || n.nodeId); f.set('notif', notif);
      f.set('critical', tk === 'critical' || (notif && notif.sev !== 'info'));
      f.set('beaconColor', tk === 'critical' ? TONES.critical.ring : (notif && notif.sev !== 'info' ? SEV_COLOR[notif.sev] : TONES[tk].ring));
      f.set('selected', selectedKey === `node:${n.nodeId}`);
      return f;
    }));
    if (nodeLayerRef.current) nodeLayerRef.current.setVisible(showLayers.nodes);
  }, [nodes, notifByNode, nowSec, showLayers.nodes, selectedKey]);

  // Enable dragging only in edit mode.
  useEffect(() => {
    if (bTranslateRef.current) bTranslateRef.current.setActive(editMode);
    if (nTranslateRef.current) nTranslateRef.current.setActive(editMode);
  }, [editMode]);

  return <div className={`mw-geo${placing ? ' placing' : ''}`} ref={containerRef} role="application" aria-label={t('nav.map')} />;
}

GeoMap.propTypes = {
  sites: PropTypes.array, nodes: PropTypes.array, nodesById: PropTypes.object, notifByNode: PropTypes.object, notifByCam: PropTypes.object, showLayers: PropTypes.object,
  editMode: PropTypes.bool, selectedKey: PropTypes.string, placing: PropTypes.object,
  onEnterBuilding: PropTypes.func, onSelectNode: PropTypes.func, onPlace: PropTypes.func,
  onMoveBuilding: PropTypes.func, onMoveNode: PropTypes.func, onBackground: PropTypes.func, onAssignNodeToBuilding: PropTypes.func,
};
