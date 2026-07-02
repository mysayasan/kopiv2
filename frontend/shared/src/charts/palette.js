// Shared chart palette + tiny geometry helpers for the hand-rolled SVG charts.
// Colors lean on the --ui-* design tokens where a semantic mapping exists
// (severity) and fall back to a fixed categorical ramp otherwise, so charts
// follow each app's theme without every series needing a token.

// CATEGORICAL is the ordered ramp used for arbitrary series (categories,
// sources, objects) that have no inherent semantic color. Picked for contrast
// in both light and dark themes.
export const CATEGORICAL = [
  '#3b82f6', // blue
  '#10b981', // emerald
  '#f59e0b', // amber
  '#8b5cf6', // violet
  '#ef4444', // red
  '#14b8a6', // teal
  '#ec4899', // pink
  '#f97316', // orange
  '#6366f1', // indigo
  '#84cc16', // lime
];

// SEVERITY_COLORS maps notification severities to their conventional colors.
export const SEVERITY_COLORS = {
  critical: '#ef4444',
  warning: '#f59e0b',
  info: '#3b82f6',
};

// CATEGORY_COLORS gives the well-known notification categories a stable color
// so the same kind is the same hue across every chart on the dashboard.
export const CATEGORY_COLORS = {
  'vision.alert': '#8b5cf6',
  'health.check': '#10b981',
  system: '#f59e0b',
  other: '#94a3b8',
};

// colorFor returns a stable color for a series key: a semantic color when known,
// otherwise a categorical ramp color chosen by the key's position in the list.
export function colorFor(key, index = 0, kind = 'category') {
  if (kind === 'severity' && SEVERITY_COLORS[key]) return SEVERITY_COLORS[key];
  if (kind === 'category' && CATEGORY_COLORS[key]) return CATEGORY_COLORS[key];
  return CATEGORICAL[index % CATEGORICAL.length];
}

// niceMax rounds a raw maximum up to a clean axis bound (1/2/5 × 10ⁿ) so the
// y-axis grid lands on readable numbers.
export function niceMax(raw) {
  if (raw <= 0) return 1;
  const mag = Math.pow(10, Math.floor(Math.log10(raw)));
  const norm = raw / mag;
  let step;
  if (norm <= 1) step = 1;
  else if (norm <= 2) step = 2;
  else if (norm <= 5) step = 5;
  else step = 10;
  return step * mag;
}

// polarToXY converts a circle angle (radians, 0 = 12 o'clock, clockwise) to an
// x/y point on a circle of radius r centered at (cx, cy). Used by the donut arcs.
export function polarToXY(cx, cy, r, angle) {
  return { x: cx + r * Math.sin(angle), y: cy - r * Math.cos(angle) };
}

// arcPath builds an SVG donut-segment path between two angles (radians) for the
// ring between innerR and outerR.
export function arcPath(cx, cy, innerR, outerR, startAngle, endAngle) {
  const largeArc = endAngle - startAngle > Math.PI ? 1 : 0;
  const p1 = polarToXY(cx, cy, outerR, startAngle);
  const p2 = polarToXY(cx, cy, outerR, endAngle);
  const p3 = polarToXY(cx, cy, innerR, endAngle);
  const p4 = polarToXY(cx, cy, innerR, startAngle);
  return [
    `M ${p1.x} ${p1.y}`,
    `A ${outerR} ${outerR} 0 ${largeArc} 1 ${p2.x} ${p2.y}`,
    `L ${p3.x} ${p3.y}`,
    `A ${innerR} ${innerR} 0 ${largeArc} 0 ${p4.x} ${p4.y}`,
    'Z',
  ].join(' ');
}
