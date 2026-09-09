import { Fill, Stroke, Style, Circle as CircleStyle, Text } from 'ol/style.js';

// How the offline Protomaps basemap is painted. Every rule keys off the vector tile's `layer`
// name, so this is the whole cartography of the fleet map's ground: land, water, roads, borders
// and the place labels that make it read as a map rather than a field of shapes.
//
// Extracted from fleet_map.js unchanged. It is pure OpenLayers styling with no React and no app
// state, which is exactly why it does not belong in a component file.

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

export { basemapStyle };
