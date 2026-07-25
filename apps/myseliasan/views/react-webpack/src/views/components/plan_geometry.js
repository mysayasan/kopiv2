// Shared plan geometry for the 2D floor editor and the 3D viewer.
//
// The 2D canvas and the three.js scene have to agree EXACTLY on where a wall is broken by a door or
// a window: a gap the operator draws must be the gap they walk (or see) through in 3D. This used to
// be two copies of the same maths in floor_editor.js and floor_3d.js, each carrying a comment
// saying it had to be kept in sync with the other. It is one module now, so they cannot drift.
//
// Everything here is pure and image-space (top-left origin, y down) — no React, no three.js.

// Window opening defaults in METRES. A sill at 0.9 and a head at 2.1 is the ordinary domestic
// window; they are stored per window so a clerestory or a shopfront can differ.
export const DEF_SILL = 0.9;
export const DEF_HEAD = 2.1;
export const sillOf = (win) => (win && win.sill > 0 ? win.sill : DEF_SILL);
export const headOf = (win) => (win && win.head > sillOf(win) ? win.head : Math.max(sillOf(win) + 0.3, DEF_HEAD));

// openingSpanOnSeg returns the [t0,t1] parameter interval an OPENING cuts along wall segment s, or
// null if it does not sit on this wall. Doors and windows are the same geometry — a centre on the
// wall, a width, and the wall's angle — and differ only in how they are drawn and how much wall is
// left above and below. An opening is "on" s when its centre projects close to the line AND runs
// roughly parallel to it, so an opening in one wall never cuts a wall crossing it.
export function openingSpanOnSeg(s, d, tol) {
  const vx = s.x2 - s.x1; const vy = s.y2 - s.y1; const len2 = vx * vx + vy * vy; if (len2 < 1e-6) return null;
  const len = Math.sqrt(len2);
  const t = ((d.cx - s.x1) * vx + (d.cy - s.y1) * vy) / len2;
  const px = s.x1 + t * vx; const py = s.y1 + t * vy;
  if (Math.hypot(d.cx - px, d.cy - py) > tol) return null;
  let da = Math.abs((Math.atan2(vy, vx) - (d.a || 0)) % Math.PI); da = Math.min(da, Math.PI - da);
  if (da > 0.35) return null;
  const half = (d.w / 2) / len; const t0 = t - half; const t1 = t + half;
  if (t1 <= 0 || t0 >= 1) return null;
  return [Math.max(0, t0), Math.min(1, t1)];
}

// mergedSpans → the openings' intervals on s, sorted and merged so two overlapping openings cut one
// hole rather than two that leave a sliver of wall between them.
function mergedSpans(s, openings, tol) {
  const spans = [];
  openings.forEach((d) => { const iv = openingSpanOnSeg(s, d, tol); if (iv) spans.push(iv); });
  if (!spans.length) return [];
  spans.sort((a, b) => a[0] - b[0]);
  const merged = [];
  spans.forEach((sp) => { const last = merged[merged.length - 1]; if (!last || sp[0] > last[1]) merged.push([sp[0], sp[1]]); else last[1] = Math.max(last[1], sp[1]); });
  return merged;
}

// remainingSpans → the [t0,t1] intervals of s that are still solid wall once every opening is cut.
export function remainingSpans(s, openings, tol) {
  const merged = mergedSpans(s, openings, tol);
  if (!merged.length) return [[0, 1]];
  const walls = []; let cursor = 0;
  merged.forEach(([a, b]) => { if (a > cursor + 1e-4) walls.push([cursor, a]); cursor = Math.max(cursor, b); });
  if (cursor < 1 - 1e-4) walls.push([cursor, 1]);
  return walls;
}

// ---- object transforms -------------------------------------------------------------------
//
// Everything on a plan can be moved, rotated and resized, and a selection of mixed things has to
// transform as one. That is expressed as a single affine step applied about a pivot:
//
//   scale (sx, sy) about (px, py) → rotate `ang` about (px, py) → translate (tx, ty)
//
// One description covers all three gestures — a move is scale 1 with no rotation, a rotate is scale
// 1 with no translation — so the drag preview and the committed result run the SAME code and cannot
// disagree about where an object ended up.

// `fa` is the FRAME angle the scale is applied in. It is what fixes resizing a rotated object: the
// stretch happens along the frame's axes, not the screen's, so a "horizontal" pull on a turned
// stair lengthens the stair rather than shearing it. fa = 0 is the ordinary screen-aligned frame.
export const IDENTITY_XF = { px: 0, py: 0, sx: 1, sy: 1, fa: 0, ang: 0, tx: 0, ty: 0 };

const rot2 = (x, y, a) => ({ x: x * Math.cos(a) - y * Math.sin(a), y: x * Math.sin(a) + y * Math.cos(a) });

// xfPoint maps one image-space point through a transform: scale in frame `fa` about the pivot, then
// rotate `ang` about the pivot, then translate.
export function xfPoint(p, xf) {
  let x = p.x - xf.px; let y = p.y - xf.py;
  if (xf.sx !== 1 || xf.sy !== 1) {
    const fa = xf.fa || 0;
    const l = rot2(x, y, -fa); // into the frame
    const r = rot2(l.x * xf.sx, l.y * xf.sy, fa); // scale, then back out
    x = r.x; y = r.y;
  }
  if (xf.ang) { const r = rot2(x, y, xf.ang); x = r.x; y = r.y; }
  return { x: x + xf.px + xf.tx, y: y + xf.py + xf.ty };
}

// xfLengthAlong returns how much a length lying at world angle `a` is stretched by the transform.
// Measured in the frame `fa`, so an opening keeps pace with the wall it sits in whether that wall is
// axis-aligned or turned. A door 0.9 m wide in a wall stretched along its length widens with it.
export function xfLengthAlong(a, xf) {
  const fa = xf.fa || 0;
  return Math.hypot(Math.cos(a - fa) * xf.sx, Math.sin(a - fa) * xf.sy) || 1;
}

// rectCenter / rectSize read an axis-aligned footprint stored as two corners.
export const rectCenter = (r) => ({ x: (r.x1 + r.x2) / 2, y: (r.y1 + r.y2) / 2 });
export const rectSize = (r) => ({ w: Math.abs(r.x2 - r.x1), h: Math.abs(r.y2 - r.y1) });
// rectFrom rebuilds the stored corner pair from a centre and a size.
export const rectFrom = (c, size) => ({ x1: c.x - size.w / 2, y1: c.y - size.h / 2, x2: c.x + size.w / 2, y2: c.y + size.h / 2 });

// rectCorners returns a footprint's four corners in world space, honouring its own rotation `a`
// (stored as a rotation about its centre). Used for hit-testing, drawing and bounds.
export function rectCorners(r) {
  const c = rectCenter(r); const s = rectSize(r);
  const hw = s.w / 2; const hh = s.h / 2;
  const a = r.a || 0;
  const cos = Math.cos(a); const sin = Math.sin(a);
  return [[-hw, -hh], [hw, -hh], [hw, hh], [-hw, hh]].map(([dx, dy]) => ({
    x: c.x + dx * cos - dy * sin,
    y: c.y + dx * sin + dy * cos,
  }));
}

// pointInRotatedRect inverse-rotates the point into the footprint's own frame, so a rotated stair or
// parking row is still clickable exactly where it is drawn.
export function pointInRotatedRect(px, py, r) {
  const c = rectCenter(r); const s = rectSize(r);
  const a = -(r.a || 0);
  const dx = px - c.x; const dy = py - c.y;
  const lx = dx * Math.cos(a) - dy * Math.sin(a);
  const ly = dx * Math.sin(a) + dy * Math.cos(a);
  return Math.abs(lx) <= s.w / 2 && Math.abs(ly) <= s.h / 2;
}

// polysOverlap: do two convex polygons (given as corner lists) intersect? Separating-axis test —
// if any edge normal of either polygon fully separates them, they do not touch. Used to tell when a
// stair meets a raised floor so the stair can climb to that platform's height instead of the storey.
function projectPoly(poly, ax) {
  let min = Infinity; let max = -Infinity;
  poly.forEach((p) => { const d = p.x * ax.x + p.y * ax.y; if (d < min) min = d; if (d > max) max = d; });
  return [min, max];
}
export function polysOverlap(a, b) {
  const axesOf = (poly) => poly.map((p, i) => { const q = poly[(i + 1) % poly.length]; return { x: -(q.y - p.y), y: q.x - p.x }; });
  const axes = axesOf(a).concat(axesOf(b));
  for (const ax of axes) {
    const [amin, amax] = projectPoly(a, ax);
    const [bmin, bmax] = projectPoly(b, ax);
    if (amax < bmin || bmax < amin) return false;
  }
  return true;
}
// rectsOverlap: the same for two footprints, honouring each one's own rotation.
export const rectsOverlap = (r1, r2) => polysOverlap(rectCorners(r1), rectCorners(r2));

// boundsOfPoints → the axis-aligned box containing every point, or null when given none.
export function boundsOfPoints(points) {
  if (!points.length) return null;
  let x1 = Infinity; let y1 = Infinity; let x2 = -Infinity; let y2 = -Infinity;
  points.forEach((p) => {
    if (p.x < x1) x1 = p.x; if (p.x > x2) x2 = p.x;
    if (p.y < y1) y1 = p.y; if (p.y > y2) y2 = p.y;
  });
  return { x1, y1, x2, y2 };
}

// ---- selection frame + resize handles ----------------------------------------------------
//
// A selection frame is an ORIENTED box: centre, half-extents, and an angle. When a single rotated
// object is selected the frame turns with it (angle = the object's), so its handles line up with the
// object's own sides. For a group, or an unrotated object, the angle is 0 and it is the ordinary
// screen-aligned box. Everything below works in the frame, so the same handle logic serves both.
//
// Eight handles round the box. A SIDE handle resizes on its own axis only — dragging the right edge
// changes width and nothing else — while a CORNER handle resizes both. Whichever handle is grabbed,
// the edge opposite it stays put, so the box grows away from where you are pulling.

export const HANDLES = [
  { id: 'nw', fx: 0, fy: 0 }, { id: 'n', fx: 0.5, fy: 0 }, { id: 'ne', fx: 1, fy: 0 },
  { id: 'e', fx: 1, fy: 0.5 }, { id: 'se', fx: 1, fy: 1 }, { id: 's', fx: 0.5, fy: 1 },
  { id: 'sw', fx: 0, fy: 1 }, { id: 'w', fx: 0, fy: 0.5 },
];

// frameOf builds an oriented frame; frameFromBox wraps an axis-aligned box as one (angle 0).
export const frameOf = (cx, cy, hw, hh, a = 0) => ({ cx, cy, hw, hh, a });
export const frameFromBox = (b) => frameOf((b.x1 + b.x2) / 2, (b.y1 + b.y2) / 2, Math.abs(b.x2 - b.x1) / 2, Math.abs(b.y2 - b.y1) / 2, 0);

// A local point (frame coordinates, origin at centre) ↔ a world point.
export const frameToWorld = (fr, lx, ly) => { const r = rot2(lx, ly, fr.a); return { x: fr.cx + r.x, y: fr.cy + r.y }; };
export const worldToFrame = (fr, p) => rot2(p.x - fr.cx, p.y - fr.cy, -fr.a);

// Which axes a handle is allowed to change. This is the rule, stated once.
export const handleAxes = (id) => ({
  x: id.includes('e') || id.includes('w'),
  y: id.includes('n') || id.includes('s'),
});

// A handle's position, and the point a drag of it scales AWAY from, both in the frame's LOCAL
// coordinates. The anchor is the opposite side; on an axis a side handle does not own it sits at the
// centre (0), so scaling by 1 about it is a provable no-op for that axis.
const handleLocal = (id, fr) => ({ x: id.includes('w') ? -fr.hw : id.includes('e') ? fr.hw : 0, y: id.includes('n') ? -fr.hh : id.includes('s') ? fr.hh : 0 });
const anchorLocal = (id, fr) => ({ x: id.includes('w') ? fr.hw : id.includes('e') ? -fr.hw : 0, y: id.includes('n') ? fr.hh : id.includes('s') ? -fr.hh : 0 });
export const handleWorld = (id, fr) => { const l = handleLocal(id, fr); return frameToWorld(fr, l.x, l.y); };
// The rotate knob floats on a stalk beyond the top edge, in the frame's own "up".
export const rotateKnobWorld = (fr, armLocal) => frameToWorld(fr, 0, -fr.hh - armLocal);

// resizeFactors → the (sx, sy) a drag of `id` to world pointer `p` should apply, IN THE FRAME. An
// axis the handle does not own comes back as exactly 1. `lockAspect` (Shift) ties the two together,
// but only for a corner — locking a single-axis drag would silently change the other axis, the very
// thing a side handle exists to avoid. `anchor`/`fa` come back so the transform can be built.
export function resizeFactors(id, fr, p, lockAspect, minSpan = 4) {
  const axes = handleAxes(id);
  const hs = handleLocal(id, fr); // where the handle starts, in the frame
  const an = anchorLocal(id, fr); // the pinned point, in the frame
  const pl = worldToFrame(fr, p); // the pointer, in the frame
  let sx = 1; let sy = 1;
  if (axes.x) { const d0 = hs.x - an.x; if (Math.abs(d0) > minSpan) sx = (pl.x - an.x) / d0; }
  if (axes.y) { const d0 = hs.y - an.y; if (Math.abs(d0) > minSpan) sy = (pl.y - an.y) / d0; }
  // Never invert or collapse. A factor goes negative once the pointer crosses the anchor — the box
  // turning inside out — so it clamps to almost-nothing and stops. Taking the magnitude would be
  // worse: dragging a handle past the far edge would silently mirror the selection.
  const clamp = (f) => (f < 0.02 ? 0.02 : f);
  sx = clamp(sx); sy = clamp(sy);
  if (lockAspect && axes.x && axes.y) { const f = Math.max(sx, sy); sx = f; sy = f; }
  const aw = frameToWorld(fr, an.x, an.y);
  return { sx, sy, fa: fr.a, anchor: aw };
}

// carveSeg is remainingSpans expressed as drawable sub-segments — what the 2D canvas strokes.
export function carveSeg(s, openings, tol) {
  const spans = remainingSpans(s, openings, tol);
  if (spans.length === 1 && spans[0][0] === 0 && spans[0][1] === 1) return [s];
  const at = (tt) => ({ x: s.x1 + (s.x2 - s.x1) * tt, y: s.y1 + (s.y2 - s.y1) * tt });
  return spans.map(([a, b]) => { const p = at(a); const q = at(b); return { x1: p.x, y1: p.y, x2: q.x, y2: q.y }; });
}
