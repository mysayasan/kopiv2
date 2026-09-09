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

// Surface palettes, shared by the 2D canvas, the SVG view and the 3D scene so a road is the same
// grey everywhere. They live beside the declarations that read them rather than in a renderer.
const ROAD_SURFACE = { asphalt: '#4b5563', concrete: '#9ca3af', gravel: '#a8a29e', paved: '#78716c' };
const ROAD_3D = { asphalt: 0x4b5563, concrete: 0x9ca3af, gravel: 0xa8a29e, paved: 0x78716c };
const GROUND_FILL = { grass: 'rgba(132,204,22,0.22)', water: 'rgba(56,189,248,0.28)', gravel: 'rgba(168,162,158,0.30)', hardstanding: 'rgba(120,113,108,0.26)' };
const GROUND_3D = { grass: 0x84cc16, water: 0x38bdf8, gravel: 0xa8a29e, hardstanding: 0x78716c };

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

  // ---- the outdoor kit -------------------------------------------------------------------------
  //
  // These four are the first types declared ENTIRELY here: their drawing lives in `draw2d` (the
  // editor canvas), `svg2d` (the read-only floor view) and `build3d` (the three.js scene), so none
  // of the three renderers needed an edit to gain them. That is the promise the registry was built
  // to make good on — four declarations rather than sixteen drawing routines.
  //
  // `view` carries what a renderer needs and a declaration should not have to know: `ds` (image px
  // to screen px), `mpp` (metres per image pixel — always the NOMINAL scale, because geometry has
  // to be drawn even on a plan nobody has scaled yet), and the selection/hover flags.

  // A road is a POLYLINE CENTRELINE WITH A WIDTH, rendered as a ribbon.
  //
  // It must never live in `segments`: those extrude into WALLS, so a road stored there would become
  // a six-metre wall down the middle of the site and destroy every camera view on the plan.
  //
  // One shape covers road, driveway, footpath and cycle lane — the difference is width and surface,
  // which is why there is no separate type for each.
  road: {
    array: 'roads', sel: 'road', ref: 'roads',
    geometry: 'polyline', collection: 'outdoor',
    kinds: [KIND_OUTDOOR],
    label: 'grid.roads',
    tool: { id: 'road' },
    fields: [
      { key: 'width', type: 'length', min: 1, max: 30, step: 0.1, default: 6, label: 'grid.roadWidth' },
      { key: 'surface', type: 'enum', options: ['asphalt', 'concrete', 'gravel', 'paved'], default: 'asphalt', label: 'grid.surface' },
      { key: 'markings', type: 'enum', options: ['none', 'centre', 'lanes'], default: 'centre', label: 'grid.markings' },
      { key: 'kerb', type: 'bool', default: true, label: 'grid.kerb' },
    ],
    draw2d(ctx, o, view) {
      const pts = (o.pts || []).map((p) => ({ x: p.x * view.ds, y: p.y * view.ds }));
      if (pts.length < 2) return;
      const wpx = Math.max(2, ((o.width || 6) / view.mpp) * view.ds);
      const path = () => { ctx.beginPath(); pts.forEach((p, i) => (i ? ctx.lineTo(p.x, p.y) : ctx.moveTo(p.x, p.y))); };
      ctx.lineCap = 'round'; ctx.lineJoin = 'round';
      if (o.kerb !== false) {
        ctx.strokeStyle = view.sel ? '#2d6cdf' : 'rgba(71,85,105,0.55)';
        ctx.lineWidth = wpx + 3;
        path(); ctx.stroke();
      }
      ctx.strokeStyle = view.hov ? '#fca5a5' : (ROAD_SURFACE[o.surface] || ROAD_SURFACE.asphalt);
      ctx.lineWidth = wpx;
      path(); ctx.stroke();
      // Markings make it read as a road rather than a grey band, and only when there is room.
      if (o.markings && o.markings !== 'none' && wpx > 8) {
        ctx.strokeStyle = 'rgba(255,255,255,0.85)';
        ctx.lineWidth = Math.max(1, wpx * 0.045);
        ctx.setLineDash(o.markings === 'lanes' ? [wpx * 0.5, wpx * 0.4] : [wpx * 0.9, wpx * 0.7]);
        path(); ctx.stroke();
        ctx.setLineDash([]);
      }
      if (view.sel) {
        ctx.strokeStyle = '#2d6cdf'; ctx.lineWidth = 1.5; ctx.setLineDash([4, 3]);
        path(); ctx.stroke();
        ctx.setLineDash([]);
      }
    },
    svg2d(o, view, key) {
      const pts = o.pts || [];
      if (pts.length < 2) return null;
      const d = pts.map((p, i) => (i ? 'L' : 'M') + p.x + ' ' + p.y).join(' ');
      const wpx = Math.max(2, (o.width || 6) / view.mpp);
      return { key, el: 'path', props: { d, stroke: ROAD_SURFACE[o.surface] || ROAD_SURFACE.asphalt, strokeWidth: wpx, fill: 'none', strokeLinecap: 'round', strokeLinejoin: 'round' } };
    },
    build3d(o) {
      // A flat ribbon a few centimetres above the ground slab — NOT a wall.
      const pts = o.pts || [];
      if (pts.length < 2) return [];
      return [{ kind: 'ribbon', pts, width: o.width || 6, height: o.kerb === false ? 0.02 : 0.12, color: ROAD_3D[o.surface] || ROAD_3D.asphalt }];
    },
  },

  // A TREE is a point with a canopy radius, a total height and a CLEAR STEM.
  //
  // The clear stem is the field that earns the type. A camera mounted at 2.5 m may see UNDER a
  // canopy whose stem is clear to 3 m, and that is precisely the argument installers have on site.
  // Species drives the glyph only: no catalogue, no seasons, no growth model.
  tree: {
    array: 'trees', sel: 'tree', ref: 'trees',
    geometry: 'point', collection: 'outdoor',
    kinds: [KIND_OUTDOOR],
    label: 'grid.trees',
    tool: { id: 'tree' },
    fields: [
      { key: 'canopy', type: 'length', min: 0.5, max: 20, step: 0.1, default: 4.5, label: 'grid.canopy' },
      { key: 'height', type: 'length', min: 1, max: 40, step: 0.5, default: 8, label: 'grid.treeHeight' },
      { key: 'stem', type: 'length', min: 0, max: 12, step: 0.1, default: 2.2, label: 'grid.clearStem' },
      { key: 'species', type: 'enum', options: ['broadleaf', 'conifer', 'palm'], default: 'broadleaf', label: 'grid.species' },
    ],
    draw2d(ctx, o, view) {
      const x = o.x * view.ds; const y = o.y * view.ds;
      const r = Math.max(4, ((o.canopy || 4.5) / view.mpp) * view.ds);
      ctx.beginPath(); ctx.arc(x, y, r, 0, Math.PI * 2);
      ctx.fillStyle = view.hov ? 'rgba(239,68,68,0.18)' : view.sel ? 'rgba(45,108,223,0.18)' : 'rgba(22,163,74,0.16)';
      ctx.fill();
      ctx.strokeStyle = view.hov ? '#ef4444' : view.sel ? '#2d6cdf' : 'rgba(22,163,74,0.78)';
      ctx.lineWidth = view.sel || view.hov ? 2 : 1.5;
      ctx.setLineDash(o.species === 'conifer' ? [4, 3] : []);
      ctx.stroke();
      ctx.setLineDash([]);
      // The trunk: a filled dot, so the tree has a POSITION and not only an extent.
      ctx.beginPath(); ctx.arc(x, y, Math.max(2, r * 0.1), 0, Math.PI * 2);
      ctx.fillStyle = view.sel ? '#2d6cdf' : '#4d7c0f'; ctx.fill();
    },
    svg2d(o, view, key) {
      const r = Math.max(3, (o.canopy || 4.5) / view.mpp);
      return { key, el: 'circle', props: { cx: o.x, cy: o.y, r, fill: 'rgba(22,163,74,0.16)', stroke: 'rgba(22,163,74,0.78)', strokeWidth: Math.max(1, r * 0.06) } };
    },
    build3d(o) {
      return [{ kind: 'tree', x: o.x, y: o.y, canopy: o.canopy || 4.5, height: o.height || 8, stem: o.stem || 2.2, species: o.species || 'broadleaf' }];
    },
  },

  // A HEDGE is a polyline with a width and a height — the low SOLID occluder that actually blocks a
  // fence-line camera. Its own type rather than a wall with a height override, because the outliner
  // has to group it as planting and "erase hedge" must not mean "erase wall".
  hedge: {
    array: 'hedges', sel: 'hedge', ref: 'hedges',
    geometry: 'polyline', collection: 'outdoor',
    kinds: [KIND_OUTDOOR],
    label: 'grid.hedges',
    tool: { id: 'hedge' },
    fields: [
      { key: 'width', type: 'length', min: 0.2, max: 5, step: 0.1, default: 0.8, label: 'grid.hedgeWidth' },
      { key: 'height', type: 'length', min: 0.2, max: 6, step: 0.1, default: 1.6, label: 'grid.hedgeHeight' },
    ],
    draw2d(ctx, o, view) {
      const pts = (o.pts || []).map((p) => ({ x: p.x * view.ds, y: p.y * view.ds }));
      if (pts.length < 2) return;
      ctx.lineCap = 'round'; ctx.lineJoin = 'round';
      ctx.strokeStyle = view.hov ? '#ef4444' : view.sel ? '#2d6cdf' : '#15803d';
      ctx.lineWidth = Math.max(3, ((o.width || 0.8) / view.mpp) * view.ds);
      ctx.beginPath(); pts.forEach((p, i) => (i ? ctx.lineTo(p.x, p.y) : ctx.moveTo(p.x, p.y))); ctx.stroke();
    },
    svg2d(o, view, key) {
      const pts = o.pts || [];
      if (pts.length < 2) return null;
      const d = pts.map((p, i) => (i ? 'L' : 'M') + p.x + ' ' + p.y).join(' ');
      return { key, el: 'path', props: { d, stroke: '#15803d', strokeWidth: Math.max(2, (o.width || 0.8) / view.mpp), fill: 'none', strokeLinecap: 'round', strokeLinejoin: 'round' } };
    },
    build3d(o) {
      const pts = o.pts || [];
      if (pts.length < 2) return [];
      return [{ kind: 'ribbon', pts, width: o.width || 0.8, height: o.height || 1.6, color: 0x2f7a34, solid: true }];
    },
  },

  // GROUND is an area with a surface. Purely cosmetic and painted beneath everything else — it is
  // what makes a site plan legible AS a site plan rather than a diagram of fences.
  ground: {
    array: 'ground', sel: 'gnd', ref: 'ground',
    geometry: 'polyline', collection: 'outdoor',
    kinds: [KIND_OUTDOOR],
    label: 'grid.ground',
    tool: { id: 'ground' },
    under: true, // painted before every other object
    closed: true, // the run closes back to its first point: this is an area, not a line
    fields: [
      { key: 'surface', type: 'enum', options: ['grass', 'water', 'gravel', 'hardstanding'], default: 'grass', label: 'grid.surface' },
    ],
    draw2d(ctx, o, view) {
      const pts = (o.pts || []).map((p) => ({ x: p.x * view.ds, y: p.y * view.ds }));
      if (pts.length < 3) return;
      ctx.beginPath(); pts.forEach((p, i) => (i ? ctx.lineTo(p.x, p.y) : ctx.moveTo(p.x, p.y))); ctx.closePath();
      ctx.fillStyle = GROUND_FILL[o.surface] || GROUND_FILL.grass;
      ctx.fill();
      ctx.strokeStyle = view.hov ? '#ef4444' : view.sel ? '#2d6cdf' : 'rgba(71,85,105,0.38)';
      ctx.lineWidth = view.sel || view.hov ? 2 : 1;
      ctx.stroke();
    },
    svg2d(o, view, key) {
      const pts = o.pts || [];
      if (pts.length < 3) return null;
      return { key, el: 'polygon', props: { points: pts.map((p) => p.x + ',' + p.y).join(' '), fill: GROUND_FILL[o.surface] || GROUND_FILL.grass, stroke: 'rgba(71,85,105,0.38)', strokeWidth: 1 } };
    },
    build3d(o) {
      const pts = o.pts || [];
      if (pts.length < 3) return [];
      return [{ kind: 'area', pts, color: GROUND_3D[o.surface] || GROUND_3D.grass }];
    },
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
