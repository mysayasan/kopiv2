import { Fill, Stroke, Style, Circle as CircleStyle, RegularShape, Text } from 'ol/style.js';
import { TONES } from '../../lib/fleet_status';
import { KIND_OUTDOOR, KIND_POINT, normKind } from '../site_kinds';

// The marks the fleet map draws on top of the basemap: site markers (shape by kind, tone by the
// worst owning-node status) and node pins, plus the small colour/easing helpers the critical
// "beacon" animation needs. Canvas styling, so colours are concrete hex rather than CSS tokens.
//
// Extracted from fleet_map.js unchanged. The beacon RING itself is not drawn here: it is animated
// every frame in the layer's prerender handler so it stays smooth, and that lives with the map.

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

export {
  SEV_COLOR, HOVER, PULSE_PERIOD, BUILDING_GLYPH_FONT,
  hexToRgba, easeOutCubic, markerShape, pinStyle, siteStyle,
};
