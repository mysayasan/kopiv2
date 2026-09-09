// The plan object registry — one declaration per kind of thing that can live on a floor plan.
//
// WHY THIS EXISTS
//
// The floor model (the `grid` JSON on entities.FloorPlan) is consumed by FOUR renderers in TWO
// languages:
//
//   floor_editor.js        the editor canvas          JS / Canvas2D
//   node_floor_view.js     the read-only floor view   JS / SVG
//   floor_3d.js            the 3D scene               JS / three.js
//   services/report_floorgrid.go   the PDF compositor  GO
//
// Every new object type therefore cost four implementations, and history says the Go one is the
// one that silently falls behind — the PDF then quietly omits geometry the operator drew. This
// module is the single place a type is DECLARED, so the three JS consumers agree by construction
// and the Go one is held to it by a test (see plan_objects.test.js and report_floorgrid).
//
// WHAT IS AND IS NOT HERE
//
// This owns the type's IDENTITY: which array it lives in, what shape its geometry is, which site
// kinds offer it, what its tool looks like, what fields it has, and how to measure it. It does NOT
// own the existing hand-written drawing code. Walls, doors and windows in particular are woven
// through plan_geometry.js (an opening CARVES the wall it sits on, in 2D and 3D alike), and
// rewriting that to route through a hook would be a large change to proven code for no gain this
// phase. Those are marked `builtin: true` and keep their drawing where it is.
//
// New types added from here on declare `draw2d` / `svg2d` / `build3d` and need no edit in any
// renderer. That is the promise this module exists to make good on, and it is what makes the
// outdoor kit four declarations rather than sixteen drawing routines.

import { KIND_BUILDING, KIND_OUTDOOR } from '../site_kinds';

// ---- field types --------------------------------------------------------------------------------
//
// A field descriptor drives the numeric inspector, the outliner's secondary label, and the Go
// parity test's expected key list — from ONE declaration. That is the point of the registry: the
// alternative is the same knowledge written out three times and drifting.
//
//   length   a real-world distance in METRES. Scale-dependent: a plan with no scale set cannot
//            show one honestly, so the inspector falls back to pixels and says so rather than
//            printing a fabricated number. See §4 of the plan doc.
//   px       a distance in IMAGE PIXELS. Doors and windows store their width this way, which is
//            a genuine inconsistency in the existing model — recorded here rather than hidden,
//            because the inspector has to know which it is holding.
//   angle    radians in the model, degrees at the surface.
//   count    a whole number.
//   bool     a flag.
//   enum     one of `options`.
export const FIELD_TYPES = ['length', 'px', 'angle', 'count', 'bool', 'enum'];

// ---- geometry shapes ----------------------------------------------------------------------------
//
// What a type's coordinates MEAN, so a consumer can measure, hit-test or transform an object it has
// never heard of:
//
//   segment   { x1, y1, x2, y2 }                a line, both ends meaningful
//   rect      { x1, y1, x2, y2, a? }            an axis-aligned box, optionally rotated by `a`
//   opening   { cx, cy, w, a }                  a centre ON a wall, a width along it, its angle
//   point     { x, y }                          a position with no extent
//   polyline  { pts: [{x,y}, …] }               a run of points
export const GEOMETRIES = ['segment', 'rect', 'opening', 'point', 'polyline'];

// boundsOf returns the image-space box a single object occupies, from its geometry alone. This is
// what lets "frame the selection" and the outliner work for a type they know nothing else about.
export function boundsOf(geometry, o) {
  if (!o) return null;
  switch (geometry) {
    case 'segment':
      return { x1: Math.min(o.x1, o.x2), y1: Math.min(o.y1, o.y2), x2: Math.max(o.x1, o.x2), y2: Math.max(o.y1, o.y2) };
    case 'rect': {
      // A rotated rect's box is the box of its four turned corners, not of its stored extents.
      const cx = (o.x1 + o.x2) / 2; const cy = (o.y1 + o.y2) / 2;
      const hw = Math.abs(o.x2 - o.x1) / 2; const hh = Math.abs(o.y2 - o.y1) / 2;
      const a = o.a || 0; const cs = Math.cos(a); const sn = Math.sin(a);
      const xs = []; const ys = [];
      [[-hw, -hh], [hw, -hh], [hw, hh], [-hw, hh]].forEach(([dx, dy]) => {
        xs.push(cx + dx * cs - dy * sn); ys.push(cy + dx * sn + dy * cs);
      });
      return { x1: Math.min(...xs), y1: Math.min(...ys), x2: Math.max(...xs), y2: Math.max(...ys) };
    }
    case 'opening': {
      const ux = Math.cos(o.a || 0) * (o.w / 2); const uy = Math.sin(o.a || 0) * (o.w / 2);
      return { x1: Math.min(o.cx - ux, o.cx + ux), y1: Math.min(o.cy - uy, o.cy + uy), x2: Math.max(o.cx - ux, o.cx + ux), y2: Math.max(o.cy - uy, o.cy + uy) };
    }
    case 'point':
      return { x1: o.x, y1: o.y, x2: o.x, y2: o.y };
    case 'polyline': {
      const pts = o.pts || [];
      if (!pts.length) return null;
      return {
        x1: Math.min(...pts.map((p) => p.x)), y1: Math.min(...pts.map((p) => p.y)),
        x2: Math.max(...pts.map((p) => p.x)), y2: Math.max(...pts.map((p) => p.y)),
      };
    }
    default:
      return null;
  }
}

// ---- the registry -------------------------------------------------------------------------------
//
// `array`      the key this type occupies in the grid JSON. THE identifier — the Go parity test,
//              the model reader and the history snapshot all key off it.
// `sel`        the short tag the editor's selection keys use ("seg:3"). Historic and not derivable.
// `ref`        the key this type's list takes in a commit() patch, which is also historic.
// `geometry`   see GEOMETRIES.
// `collection` how the outliner groups it (P4).
// `kinds`      which site kinds offer the tool. A road inside a building is a category error.
// `tool`       null when the type is not drawn by a tool of its own (a window is placed on a wall).
// `fields`     see FIELD_TYPES. Order is the order the inspector shows them in.
// `builtin`    true = this type's drawing lives in the renderers, not here. See the note at the top.
export const PLAN_OBJECTS = {
  wall: {
    array: 'segments', sel: 'seg', ref: 'segs',
    geometry: 'segment', collection: 'structure',
    kinds: [KIND_BUILDING, KIND_OUTDOOR],
    label: 'grid.walls',
    tool: { id: 'wall' },
    fields: [],
    builtin: true,
  },
  door: {
    array: 'doors', sel: 'door', ref: 'doors',
    geometry: 'opening', collection: 'structure',
    kinds: [KIND_BUILDING, KIND_OUTDOOR],
    label: 'grid.doors',
    tool: { id: 'door' },
    // Width is PIXELS, not metres — see the `px` note above. The inspector converts for display
    // when the plan has a scale, and says "px" when it does not.
    fields: [
      { key: 'w', type: 'px', min: 0.5, max: 6, step: 1, label: 'grid.doorWidth', ofUnit: true },
      { key: 'hf', type: 'bool', default: false, label: 'grid.flipHinge', buildingOnly: true },
      { key: 'sf', type: 'bool', default: false, label: 'grid.flipSwing', buildingOnly: true },
    ],
    builtin: true,
  },
  window: {
    array: 'windows', sel: 'win', ref: 'windows',
    geometry: 'opening', collection: 'structure',
    kinds: [KIND_BUILDING],
    label: 'grid.windows',
    tool: { id: 'window' },
    // Sill and head are what make a window a window rather than a door: wall remains below and
    // above, which is exactly what decides whether a camera can see through it.
    fields: [
      { key: 'w', type: 'px', min: 0.5, max: 8, step: 1, label: 'grid.doorWidth', ofUnit: true },
      { key: 'sill', type: 'length', min: 0, max: 2.5, step: 0.05, default: 0.9, label: 'grid.sill' },
      { key: 'head', type: 'length', min: 0.5, max: 4, step: 0.05, default: 2.1, label: 'grid.head' },
    ],
    builtin: true,
  },
  stair: {
    array: 'stairs', sel: 'stair', ref: 'stairs',
    geometry: 'rect', collection: 'structure',
    kinds: [KIND_BUILDING],
    label: 'grid.stairs',
    tool: { id: 'stairs' },
    fields: [
      { key: 'height', type: 'length', min: 0.3, max: 8, step: 0.05, label: 'grid.height' },
      { key: 'steps', type: 'count', min: 2, max: 40, step: 1, label: 'grid.steps' },
      { key: 'down', type: 'bool', default: false, label: 'grid.goesDown' },
      { key: 'dir', type: 'enum', options: ['n', 'e', 's', 'w'], default: 'n', label: 'grid.ascent' },
    ],
    builtin: true,
  },
  parking: {
    array: 'parking', sel: 'park', ref: 'parking',
    geometry: 'rect', collection: 'outdoor',
    kinds: [KIND_OUTDOOR],
    label: 'grid.parking',
    tool: { id: 'parking' },
    fields: [
      { key: 'bays', type: 'count', min: 1, max: 60, step: 1, default: 4, label: 'grid.bays' },
    ],
    builtin: true,
  },
  platform: {
    array: 'platforms', sel: 'plat', ref: 'platforms',
    geometry: 'rect', collection: 'structure',
    kinds: [KIND_BUILDING],
    label: 'grid.platforms',
    tool: { id: 'platform' },
    fields: [
      { key: 'rise', type: 'length', min: 0.1, max: 6, step: 0.05, default: 0.6, label: 'grid.rise' },
    ],
    builtin: true,
  },
};

// ---- derived views over the registry -------------------------------------------------------------

export const PLAN_TYPES = Object.keys(PLAN_OBJECTS);
// Every array name the model can carry. The model reader, the history snapshot and the Go parity
// test all read this rather than repeating the list.
export const MODEL_ARRAYS = PLAN_TYPES.map((k) => PLAN_OBJECTS[k].array);
// Keys the model carries that are NOT object arrays. Named so the extras passthrough can tell a
// key it simply does not know from one that is legitimately model metadata.
export const MODEL_META_KEYS = ['version', 'unit', 'cellPx', 'cols', 'rows', 'walls', 'floors'];

export const typeByArray = (name) => PLAN_TYPES.find((k) => PLAN_OBJECTS[k].array === name) || null;
export const typeBySel = (tag) => PLAN_TYPES.find((k) => PLAN_OBJECTS[k].sel === tag) || null;
export const typesForKind = (kind) => PLAN_TYPES.filter((k) => PLAN_OBJECTS[k].kinds.includes(kind));

// boundsOfObject measures one object of a named type — the generic path the outliner and
// frame-selected use.
export function boundsOfObject(typeName, o) {
  const t = PLAN_OBJECTS[typeName];
  return t ? boundsOf(t.geometry, o) : null;
}

// ---- reading a stored model ----------------------------------------------------------------------

// readModel parses a floor's `grid` JSON into { arrays, meta, extras }.
//
// `extras` is the load-bearing part. modelJSON() used to rebuild the saved object from the editor's
// known refs, so ANY key it did not recognise was dropped on the next autosave. That is a silent
// data-loss bug waiting for the first version bump: an older build opening a newer plan would load
// it, ignore the arrays it had never heard of, and delete them 700ms after the operator nudged a
// wall. Round-tripping the unknown keys makes every future bump safe by construction.
export function readModel(grid) {
  const out = { arrays: {}, meta: {}, extras: {} };
  let g = null;
  try { g = typeof grid === 'string' ? JSON.parse(grid) : grid; } catch (_) { g = null; }
  if (!g || typeof g !== 'object') {
    MODEL_ARRAYS.forEach((a) => { out.arrays[a] = []; });
    return out;
  }
  MODEL_ARRAYS.forEach((a) => { out.arrays[a] = Array.isArray(g[a]) ? g[a] : []; });
  MODEL_META_KEYS.forEach((k) => { if (g[k] !== undefined) out.meta[k] = g[k]; });
  Object.keys(g).forEach((k) => {
    if (MODEL_ARRAYS.includes(k) || MODEL_META_KEYS.includes(k)) return;
    out.extras[k] = g[k];
  });
  return out;
}

// writeModel rebuilds the JSON payload: the metadata, every known array, and every unrecognised key
// exactly as it was read. Extras go FIRST so a known key can never be shadowed by a stale one.
export function writeModel({ version = 2, unit, arrays = {}, extras = {} }) {
  const out = { ...extras, version, unit };
  MODEL_ARRAYS.forEach((a) => { out[a] = arrays[a] || []; });
  return out;
}
