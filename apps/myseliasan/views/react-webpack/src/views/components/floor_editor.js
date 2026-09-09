import { lazy, Suspense, useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from 'react';
import PropTypes from 'prop-types';
import { useT, Ico, icoSvg } from '@shared';
import { apiBase } from '../lib/helpers';
import { nodeTone, TONES } from '../lib/fleet_status';
import { KIND_BUILDING, KIND_OUTDOOR, normKind } from './site_kinds';
import { PLAN_OBJECTS, boundsOfObject, readModel, writeModel } from './map/plan_objects';
import { PlanFields } from './map/plan_inspector';
import { PlanOutliner, loadOutlinerState, saveOutlinerState } from './map/plan_outliner';
import {
  DEF_SILL, DEF_HEAD, sillOf, headOf, carveSeg,
  IDENTITY_XF, xfPoint, xfLengthAlong, rectCenter, rectSize, rectFrom, rectCorners, pointInRotatedRect, boundsOfPoints,
  HANDLES, resizeFactors, frameOf, frameFromBox, handleWorld, rotateKnobWorld,
} from './plan_geometry';

// three.js loads only when the operator flips to the 3D tab.
const Floor3D = lazy(() => import('./floor_3d'));

// FloorEditor is THE single floor editor: one canvas surface that does everything, with a 2D ⇄ 3D
// toggle in its own header.
//   • 2D tab — Select/Move (place & aim cameras dropped from the palette), Wall (draw walls, grid
//     snap, rubber-band, dbl-click/Enter finish, Esc cancels the run), Room (drag a rectangle →
//     four walls), Round (drag a box → an elliptical room), Door (click a wall → an opening),
//     Stairs (drag a box → a straight flight), Erase. Undo/Redo for walls. A selected camera opens
//     an inspector (aim, field of view, mount height, tilt, remove).
//   • 3D tab — the same walls extruded with the cameras standing on the floor. Flip back to keep
//     editing. Toolbar swaps with the tab.
// Walls autosave (debounced). Camera placement/move/aim persist immediately through the parent, so
// there is no separate "save" step. This replaces the old OpenLayers map + freehand plan tool.
//
// COORDINATES: wall segments are stored image-space (top-left, y-down). Camera placements keep the
// legacy OL convention (bottom-left, y-up) so existing placements and the 3D view stay valid; the
// canvas flips y for camera markers only. Both line up over the plan image.

// Fallback canvas bounds, used until the wrapper has been measured (and if it ever measures to
// nothing). The editor now fits itself to whatever space its host gives it — it lives in a dialog
// whose height depends on the viewport, so a fixed 620 forced a laptop to scroll a plan that would
// otherwise have fit.
const MAX_W = 1100;
const MAX_H = 620;
const MIN_STAGE_H = 260; // never shrink the viewport to a sliver

// The viewport's own palette.
//
// A dark surround with a light SHEET is the convention every drawing tool settles on (Figma,
// Illustrator, a CAD paper-space): the dark frames the work and the sheet is the paper the plan is
// ink on. It is also the low-risk choice — every stroke colour in this file was picked against a
// light sheet and stays valid. The surround is deliberately NOT theme-aware: a viewport is dark in
// both themes, the way a DCC app's is, and these are canvas pixels so they cannot be CSS tokens.
const VIEW_SURROUND = '#1b2027';
const VIEW_SHEET = '#f7f8fa';
const GRID_MINOR = 'rgba(100,116,139,0.16)';
const GRID_MAJOR = 'rgba(100,116,139,0.34)';
const GRID_EDGE = 'rgba(15,23,33,0.35)';
// Snap indicator: what the pointer actually caught, so a snap is visible rather than inferred.
const SNAP_COLOR = '#22c55e';

// Snap modes, in the order the magnet cycles them. 'off' is a mode rather than a separate toggle so
// one control answers "is it snapping, and to what" — and Ctrl inverts whatever is set, which is
// how a drawing tool lets you place something free for one click without changing your setting.
const SNAP_MODES = ['grid', 'vertex', 'edge', 'midpoint', 'off'];
const SNAP_ICO = { grid: 'grid2', vertex: 'map-pin', edge: 'wall', midpoint: 'circle', off: 'x' };

// The toolbar is built from what the place actually IS. Drawing geometry (a run of segments, a
// rectangle, an ellipse) is common to every kind — a wall and a fence are the same line, an
// enclosure is the same box — so those tools are shared and only their LABELS change. What differs
// is the special tools, because they describe things only that kind of place has:
//
//   building — Door, Window, Stairs: a building has storeys and glazing, and you author how sight
//              lines pass through its envelope.
//   outdoor  — Gate, Parking: a park, yard or car park has no storeys and no windows. Its
//              perimeter is broken by gates, and its bays are the thing cameras are aimed at.
//              (Site-plan convention: fences and gates, parking bays, hardstanding — see README.)
//
// A point asset (junction, pole) has no plan at all, so it never reaches this editor.
// placingCursor turns the marker's own icon into the mouse cursor, so what you are carrying is
// visible on the pointer rather than inferred from a banner. Drawn twice: a thick white pass
// underneath as a halo (the plan can be any colour), then the accent-coloured icon on top.
//
// 24px with a centred hotspot - browsers ignore cursor images much bigger than 32px, and the
// hotspot has to be the icon's middle because that is where the marker will land.
// An appliance is a BOARD - a mini PC or a Pi - whatever it happens to manage. Kind used to
// pick the glyph, which made a recorder look like a camera and a door controller look like a
// door: the icon showed what the box WATCHES rather than what it IS, and a camera pin and its
// recorder's pin were then indistinguishable on the same plan. Kind is still on the row, in
// the name, and in the inspector.
const APPLIANCE_ICON = 'board';
function placingCursor(iconName) {
  const inner = icoSvg[iconName] || icoSvg.cpu || '';
  const svg = `<svg xmlns="http://www.w3.org/2000/svg" width="24" height="24" viewBox="0 0 24 24" fill="none" stroke-linecap="round" stroke-linejoin="round">`
    + `<g stroke="#ffffff" stroke-width="5">${inner}</g>`
    + `<g stroke="#2d6cdf" stroke-width="2">${inner}</g></svg>`;
  return `url("data:image/svg+xml,${encodeURIComponent(svg)}") 12 12, crosshair`;
}

// The canvas cannot draw an SVG element, so each marker glyph is rasterised ONCE into an <img>
// from the same shared icon set the tree, the read-only floor view and the placing cursor use.
// Without this the editor drew bare coloured discs while every other surface drew the icon - the
// same pin looking like two different things depending on which screen you were on, and what you
// dropped never looking like what you carried.
//
// Keyed by name+colour and cached for the life of the page; a miss kicks off a load and asks for a
// redraw when it arrives, so the first frame degrades to the plain disc rather than blocking.
const markerIcons = new Map();
function markerIcon(name, color, onReady) {
  const key = `${name}|${color}`;
  const hit = markerIcons.get(key);
  if (hit) return hit.ok ? hit.img : null;
  const inner = icoSvg[name] || '';
  if (!inner) { markerIcons.set(key, { ok: false }); return null; }
  const svg = `<svg xmlns="http://www.w3.org/2000/svg" width="24" height="24" viewBox="0 0 24 24" fill="none" `
    + `stroke="${color}" stroke-width="2.4" stroke-linecap="round" stroke-linejoin="round">${inner}</svg>`;
  const img = new Image();
  const entry = { img, ok: false };
  markerIcons.set(key, entry);
  img.onload = () => { entry.ok = true; if (onReady) onReady(); };
  img.onerror = () => { entry.ok = false; };
  img.src = `data:image/svg+xml,${encodeURIComponent(svg)}`;
  return null;
}

const TOOLSETS = {
  [KIND_BUILDING]: ['select', 'wall', 'room', 'round', 'door', 'window', 'stairs', 'platform', 'setscale', 'erase'],
  [KIND_OUTDOOR]: ['select', 'wall', 'room', 'round', 'door', 'parking', 'road', 'hedge', 'tree', 'ground', 'setscale', 'erase'],
};
// Per-kind icon + label for the tools whose MEANING shifts with the place. The tool id stays the
// same (`wall` draws segments, `door` cuts an opening) — only what the operator is told changes.
const TOOL_FACE = {
  [KIND_BUILDING]: {
    wall: { icon: 'wall', key: 'grid.wall', hint: 'grid.wallHint' },
    room: { icon: 'square', key: 'grid.room', hint: 'grid.roomHint' },
    door: { icon: 'door', key: 'grid.door', hint: 'grid.doorHint' },
  },
  [KIND_OUTDOOR]: {
    wall: { icon: 'fence', key: 'grid.fence', hint: 'grid.fenceHint' },
    room: { icon: 'square', key: 'grid.compound', hint: 'grid.compoundHint' },
    door: { icon: 'gate', key: 'grid.gate', hint: 'grid.gateHint' },
  },
};
const DEFAULT_FACE = {
  select: { icon: 'cursor', key: 'fed.select', hint: 'fed.selectHint' },
  round: { icon: 'circle', key: 'grid.round', hint: 'grid.roundHint' },
  window: { icon: 'window', key: 'grid.window', hint: 'grid.windowHint' },
  stairs: { icon: 'stairs', key: 'grid.stairs', hint: 'grid.stairsHint' },
  parking: { icon: 'parking', key: 'grid.parking', hint: 'grid.parkingHint' },
  platform: { icon: 'platform', key: 'grid.platform', hint: 'grid.platformHint' },
  // The outdoor kit. A road inside a BUILDING is a category error, so these appear only on an
  // outdoor site (the registry's `kinds` says so, and TOOLSETS agrees).
  road: { icon: 'road', key: 'grid.road', hint: 'grid.roadHint' },
  hedge: { icon: 'hedge', key: 'grid.hedge', hint: 'grid.hedgeHint' },
  tree: { icon: 'tree', key: 'grid.tree', hint: 'grid.treeHint' },
  ground: { icon: 'ground', key: 'grid.ground2', hint: 'grid.groundHint' },
  setscale: { icon: 'sliders', key: 'grid.setScale', hint: 'grid.setScaleHint' },
  erase: { icon: 'trash', key: 'grid.erase', hint: 'grid.eraseHint' },
};
const faceOf = (kind, id) => (TOOL_FACE[kind] && TOOL_FACE[kind][id]) || DEFAULT_FACE[id] || { icon: 'cursor', key: id, hint: id };
// Tools that drag out a rectangle, and (a superset) tools that show a snapped cursor dot. Named
// once so adding a tool does not mean hunting down four `a || b || c` chains.
const BOX_TOOLS = new Set(['room', 'round', 'stairs', 'parking', 'platform']);
// Tools that build a RUN of points the way the wall tool does - click corners, double-click or
// Enter to finish. A road and a hedge are lines; ground is the same run, closed into an area.
const POLY_TOOLS = new Set(['road', 'hedge', 'ground']);
const DRAG_TOOLS = new Set(['wall', 'room', 'round', 'stairs', 'parking', 'platform', 'road', 'hedge', 'ground', 'tree']);
const COLS_TARGET = 28;
// How many snap steps sit inside one metre cell. A finer lattice than the cell itself, so an object
// locks onto the grid easily without making the metre cell (and the scale it defines) tiny.
const GRID_SUBDIV = 4;
// Stair step (tread) lines: how many the operator can set, and the default derived from a storey
// height at a nominal ~0.18 m riser. 0 stored means "use the default".
// Raised-floor default rise and its slider bounds, in metres. A low platform by default.
const DEF_RISE = 0.6;
const RISE_MIN = 0.1;
const RISE_MAX = 6;
const STAIR_MIN_STEPS = 2;
// The flight's own climb height (metres). 0 = a full storey; the slider bounds otherwise.
const STAIR_MIN_H = 0.3;
const STAIR_MAX_H = 8;
const STAIR_MAX_STEPS = 40;

function arcRadius(w, h) { return Math.max(50, Math.min(w, h) * 0.16); }
// roundSegs approximates the ellipse inscribed in the drag box as a closed polyline of short wall
// segments. A round room therefore stores, renders and extrudes through the very same segment path
// as any other wall — the 2D canvas and the 3D view need no special case for curves.
function roundSegs(cx, cy, rx, ry, unit) {
  const per = Math.PI * (3 * (rx + ry) - Math.sqrt((3 * rx + ry) * (rx + 3 * ry))); // Ramanujan
  const n = Math.max(16, Math.min(96, Math.round(per / Math.max(6, unit * 0.5))));
  const segs = [];
  for (let i = 0; i < n; i++) {
    const a0 = (i / n) * Math.PI * 2; const a1 = ((i + 1) / n) * Math.PI * 2;
    segs.push({ x1: cx + rx * Math.cos(a0), y1: cy + ry * Math.sin(a0), x2: cx + rx * Math.cos(a1), y2: cy + ry * Math.sin(a1) });
  }
  return segs;
}
function dist2seg(px, py, s) {
  const vx = s.x2 - s.x1; const vy = s.y2 - s.y1;
  const len2 = vx * vx + vy * vy || 1;
  let tt = ((px - s.x1) * vx + (py - s.y1) * vy) / len2;
  tt = Math.max(0, Math.min(1, tt));
  return Math.hypot(px - (s.x1 + tt * vx), py - (s.y1 + tt * vy));
}

// A parking bay row: an image-space rect divided into `bays` stalls across its width. Outdoor
// areas are watched FOR their bays (which stall, which vehicle), so the bays are worth drawing —
// they are what an operator aims a camera at.
function normRect(a, b) { return { x1: Math.min(a.x, b.x), y1: Math.min(a.y, b.y), x2: Math.max(a.x, b.x), y2: Math.max(a.y, b.y) }; }
// Bays divide along the LONGER side — a row of stalls runs the length of the row, not across it.
function baysAcrossX(p) { return (p.x2 - p.x1) >= (p.y2 - p.y1); }

// The two ends of an opening, so it contributes its real extent to the selection box rather than a
// single point.
function openingEnds(d) {
  const ux = Math.cos(d.a || 0) * (d.w / 2); const uy = Math.sin(d.a || 0) * (d.w / 2);
  return [{ x: d.cx - ux, y: d.cy - uy }, { x: d.cx + ux, y: d.cy + uy }];
}

const HANDLE_PX = 7; // on-screen size of a handle square
const ROTATE_ARM_PX = 26; // how far the rotate knob floats above the box
const MIN_SPAN = 4; // below this the box is too thin to derive a ratio from; the axis stays put

const STAIR_DIRS = ['n', 'e', 's', 'w'];
function rotateDir(dir) { return STAIR_DIRS[(STAIR_DIRS.indexOf(dir) + 1) % 4] || 'n'; }
// Normalise a drag box to x1<x2, y1<y2 so downstream code never worries about drag direction.
function normStair(a, b, dir) { return { x1: Math.min(a.x, b.x), y1: Math.min(a.y, b.y), x2: Math.max(a.x, b.x), y2: Math.max(a.y, b.y), dir }; }
// Default ascent runs along the footprint's LONGER side (that is where a flight naturally goes):
// tall box climbs up the screen, wide box climbs to the right. The operator can rotate afterwards.
function defaultStairDir(a, b) { return Math.abs(b.y - a.y) >= Math.abs(b.x - a.x) ? 'n' : 'e'; }

export function FloorEditor({ floor, siteKind = KIND_BUILDING, placements = [], nodesById = {}, placing, onPlace, onClearPlacing, onMove, onAim, onRemove, onSaveModel, onToast, onStatus, busy }) {
  const t = useT();
  const editorRef = useRef(null); // the editor surface; floating panels are positioned within it
  const canvasRef = useRef(null);
  const imgRef = useRef(null);
  const segsRef = useRef([]);
  const stairsRef = useRef([]); // straight-flight stairs: { x1,y1,x2,y2 (image-space footprint), dir:'n'|'e'|'s'|'w' }
  const doorsRef = useRef([]); // openings in walls: { cx,cy (image-space centre on a wall), w (px), a (wall angle rad) }
  const windowsRef = useRef([]); // same shape as a door, plus sill/head in metres — wall remains below and above
  const parkingRef = useRef([]); // outdoor bay rows: { x1,y1,x2,y2 (image-space footprint), bays }
  const platsRef = useRef([]); // raised floors: { x1,y1,x2,y2, a, rise (metres above the floor) }
  // The outdoor kit (model v3). These are drawn entirely from their registry declarations - there
  // is no per-type drawing code for them anywhere in this file.
  const roadsRef = useRef([]);  // { pts:[{x,y}], width (m), surface, markings, kerb }
  const treesRef = useRef([]);  // { x, y, canopy (m), height (m), stem (m), species }
  const hedgesRef = useRef([]); // { pts:[{x,y}], width (m), height (m) }
  const groundRef = useRef([]); // { pts:[{x,y}], surface }
  const histRef = useRef([]);
  const futRef = useRef([]);
  const draftRef = useRef(null); // wall {pts} · room/round/stairs/parking {start,cur}
  const hoverRef = useRef(-1); // wall index under the erase cursor
  const hoverStairRef = useRef(-1); // stair index under the erase cursor
  const hoverDoorRef = useRef(-1); // door index under the erase cursor
  const hoverWinRef = useRef(-1); // window index under the erase cursor
  const hoverParkRef = useRef(-1); // parking-row index under the erase cursor
  const hoverPlatRef = useRef(-1); // raised-floor index under the erase cursor
  // Erase hover for the registry-drawn types, as { ref, idx } - one ref rather than one per type,
  // so a new declaration needs no new state here.
  const hoverExtraRef = useRef({ ref: null, idx: -1 });
  const cursorRef = useRef(null);
  // Where the pointer is, in image space, for the status bar's readout ONLY.
  //
  // Separate from cursorRef on purpose: cursorRef is drawing state - it is the SNAPPED point, it is
  // deliberately nulled by the erase tool, and it is only maintained for the tools that draw. The
  // readout has to work under every tool, including Select, which is the one the editor opens on.
  // Sharing the ref would have meant either a dead readout or changing what the canvas draws.
  const hoverPosRef = useRef(null);
  const panRef = useRef(null); // an in-flight viewport pan: { x, y, tx, ty }
  const spaceRef = useRef(false); // space held = pan with the left button, for a trackpad with no middle click
  const saveTimer = useRef(null);
  const placingRef = useRef(placing); placingRef.current = placing;
  // See the note on `tool`: only the select branch places, so carrying something has to mean
  // select. Done as an effect rather than at the drop, because a pick can also arrive with the
  // editor already open (the palette on the left, or a second drag).
  useEffect(() => {
    if (!placing) return;
    setTool('select');
    draftRef.current = null;
  }, [placing]);

  // What the pointer should look like while carrying it: the marker's own icon. A camera is a
  // camera; anything else is the appliance, which draws by node kind.
  const placingCursorCss = useMemo(() => {
    if (!placing) return undefined;
    return placingCursor(placing.cameraId ? 'video' : APPLIANCE_ICON);
  }, [placing]);
  const [, tick] = useState(0);
  const redraw = useCallback(() => tick((n) => n + 1), []);
  // While space is held (or a pan is in flight) the pointer says so: a grab hand beats a crosshair
  // that no longer does what a crosshair does.
  const panCursor = spaceRef.current ? (panRef.current ? 'grabbing' : 'grab') : placingCursorCss;
  const nowSecRef = useRef(Math.floor(Date.now() / 1000));

  const [mode, setMode] = useState('2d');
  // The tool palette and the properties inspector are both dockable panels. Each can dock to the
  // left or right edge — dropping both on the same side stacks them in one column — or float freely.
  // Drag a panel by its grip; where you drop it (near the left/right edge, or the middle) decides.
  const [dock, setDock] = useState({ toolbar: 'left', outliner: 'right', props: 'right' }); // 'left' | 'right' | 'float'
  const [floatPos, setFloatPos] = useState({ toolbar: null, outliner: null, props: null });
  // ---- outliner view state ----------------------------------------------------------------------
  //
  // Which objects are hidden and which are locked, as sets of selection keys. VIEW state, not model
  // state: hiding the parking rows to get at the walls underneath is one operator's working
  // preference while drawing, not a fact about the building, and writing it into the shared model
  // would push it onto everyone who opens the plan afterwards - the PDF report included.
  //
  // Per floor, in localStorage, and a browser that blocks site data simply shows everything.
  const floorId = floor && floor.id;
  const [hidden, setHidden] = useState(() => new Set(loadOutlinerState(floorId).hidden));
  const [locked, setLocked] = useState(() => new Set(loadOutlinerState(floorId).locked));
  const hiddenRef = useRef(hidden); hiddenRef.current = hidden;
  const lockedRef = useRef(locked); lockedRef.current = locked;
  useEffect(() => {
    const st = loadOutlinerState(floorId);
    setHidden(new Set(st.hidden)); setLocked(new Set(st.locked));
  }, [floorId]);
  useEffect(() => { saveOutlinerState(floorId, hidden, locked); }, [floorId, hidden, locked]);
  // Hiding or locking something that is currently HELD drops it from the selection. Otherwise the
  // inspector would go on editing a row the operator has just put out of reach, and a drag would
  // move something they cannot see.
  useEffect(() => {
    setSelection((prev) => {
      if (!prev.size) return prev;
      const next = new Set([...prev].filter((k) => !hidden.has(k) && !locked.has(k)));
      return next.size === prev.size ? prev : next;
    });
  }, [hidden, locked]);
  // A hidden or locked object cannot be picked, and a hidden one is not drawn. Both are asked by
  // key, so a type added to the registry is covered without an edit.
  const isHidden = useCallback((tag, i) => hiddenRef.current.has(`${tag}:${i}`), []);
  const isPickable = useCallback((tag, i) => !hiddenRef.current.has(`${tag}:${i}`) && !lockedRef.current.has(`${tag}:${i}`), []);

  const [tool, setTool] = useState('select'); // select | wall | room | round | door | window | stairs | parking | erase
  // Arriving with something to place (dragged in from the map's tray) switches to select and
  // holds there. Only the select branch of the pointer handler actually PLACES, so with a drawing
  // tool active the next click would draw a wall and quietly lose the thing you were carrying.
  const toolRef = useRef(tool); toolRef.current = tool;
  // ONE selection holding things of any kind, as keys "<kind>:<index>" — 'seg' | 'door' | 'win' |
  // 'stair' | 'park', plus 'cam:<placementId>' for a camera/node marker. A plan edit is rarely
  // about one object ("move this room and the cameras in it", "delete this whole wing"), so select
  // is a set, shift adds and removes, a marquee sweeps up everything it touches, and a drag or
  // Delete applies to all of it at once. Per-object inspectors then show when exactly one is held.
  const [selection, setSelection] = useState(() => new Set());
  const marqueeRef = useRef(null); // rubber-band box while selecting
  const moveRef = useRef(null); // { mode, sx, sy, moved, xf, orig } while dragging the whole selection
  const povRef = useRef(null); // { id, mode:'aim'|'fov' } while dragging a camera's coverage on the canvas
  const clipRef = useRef(null); // copied plan geometry (markers never go on the clipboard)
  const pasteRunRef = useRef(0); // how many times the current clipboard has been pasted, for the offset ladder

  // What this place IS decides which special tools exist and how an opening is drawn (door vs gate).
  const kind = normKind(siteKind);
  const tools = TOOLSETS[kind] || TOOLSETS[KIND_BUILDING];

  const w = (floor && floor.width) || 1024;
  const h = (floor && floor.height) || 768;
  // The stored model, read through the registry so unrecognised keys are CAPTURED rather than
  // silently dropped on the next autosave (see readModel/writeModel).
  const parsed = readModel(floor && floor.grid);
  // meta AND arrays together, because the seeding below reads `existing.segments`, `existing.walls`
  // and friends. Only `extras` is held apart - it is the one thing nothing here should look at.
  const existing = Object.keys(parsed.meta).length || Object.keys(parsed.extras).length
    || Object.values(parsed.arrays).some((a) => a.length) ? { ...parsed.meta, ...parsed.arrays } : null;
  // Every top-level key this build does not know about, round-tripped verbatim on save. This is
  // what makes a future model version safe: an older editor opening a newer plan preserves the
  // arrays it has never heard of instead of deleting them.
  const extrasRef = useRef(parsed.extras);
  const unit = (existing && existing.unit) || (existing && existing.cellPx) || Math.max(8, Math.round(Math.max(w, h) / COLS_TARGET));
  // Fit the plan to the space the host actually has (minus the wrapper's padding), so the same
  // editor works full-screen and in a dialog on a small laptop.
  const wrapRef = useRef(null);
  const [stage, setStage] = useState({ w: MAX_W, h: MAX_H });
  useEffect(() => {
    const el = wrapRef.current;
    if (!el || typeof ResizeObserver === 'undefined') return undefined;
    const measure = () => {
      const cs = window.getComputedStyle(el);
      const padX = parseFloat(cs.paddingLeft || 0) + parseFloat(cs.paddingRight || 0);
      const padY = parseFloat(cs.paddingTop || 0) + parseFloat(cs.paddingBottom || 0);
      const aw = el.clientWidth - padX;
      const ah = el.clientHeight - padY;
      if (aw > 0 && ah > 0) setStage({ w: aw, h: Math.max(MIN_STAGE_H, ah) });
    };
    measure();
    const ro = new ResizeObserver(measure);
    ro.observe(el);
    return () => ro.disconnect();
    // Re-observe when the canvas remounts (e.g. after switching to 3D and back) so it keeps fitting.
  }, [mode]);

  // ---- the viewport -----------------------------------------------------------------------------
  //
  // The canvas now FILLS the stage and the plan is drawn through a view transform
  // (image → screen: sx = x*scale + tx). It used to be the other way round: the canvas element was
  // sized to the whole plan and the wrapper's SCROLLBARS did the panning. That is why there was no
  // middle-mouse pan, why zoom could only grow about the viewport centre, and why zooming had to
  // fix up scrollLeft/scrollTop afterwards.
  //
  // One transform replaces all of it, and it is what lets the wheel zoom about the CURSOR — the
  // thing that makes a viewport feel like a viewport rather than a scrolling document.
  const MIN_SCALE = 0.05;
  const MAX_SCALE = 12;
  const clampScale = (z) => Math.min(MAX_SCALE, Math.max(MIN_SCALE, z));
  // fitView centres the whole plan in the stage with a small margin — the "frame all" answer, and
  // the view the editor opens on.
  const fitView = useCallback((sw, sh) => {
    const pad = 24;
    const sc = Math.min((sw - pad * 2) / w, (sh - pad * 2) / h, 2);
    const scale = clampScale(sc > 0 ? sc : 1);
    return { scale, tx: (sw - w * scale) / 2, ty: (sh - h * scale) / 2 };
  }, [w, h]);
  const [view, setView] = useState(() => fitView(MAX_W, MAX_H));
  const viewRef = useRef(view); viewRef.current = view;
  const ds = view.scale;
  const cssW = Math.max(1, Math.round(stage.w));
  const cssH = Math.max(1, Math.round(stage.h));

  // Frame the plan whenever the stage is resized before the operator has taken control of the view
  // (opening the editor, switching back from 3D, resizing the window). Once they pan or zoom, their
  // view is theirs and a resize must not yank it back.
  const touchedRef = useRef(false);
  useLayoutEffect(() => {
    if (touchedRef.current) return;
    setView(fitView(stage.w, stage.h));
  }, [stage.w, stage.h, fitView]);

  // zoomAbout keeps one screen point pinned while the scale changes — the cursor for a wheel zoom,
  // the stage centre for the toolbar buttons.
  const zoomAbout = useCallback((factor, px, py) => {
    touchedRef.current = true;
    setView((v) => {
      const next = clampScale(v.scale * factor);
      const k = next / v.scale;
      return { scale: next, tx: px - (px - v.tx) * k, ty: py - (py - v.ty) * k };
    });
  }, []);
  // The wheel zooms about the cursor. Ctrl/⌘+wheel does the same (existing muscle memory), and
  // both suppress the page default — the canvas fills the stage now, so there is nothing to scroll.
  useEffect(() => {
    const el = canvasRef.current;
    if (!el) return undefined;
    const onWheel = (e) => {
      e.preventDefault();
      const r = el.getBoundingClientRect();
      zoomAbout(e.deltaY < 0 ? 1.12 : 1 / 1.12, e.clientX - r.left, e.clientY - r.top);
    };
    el.addEventListener('wheel', onWheel, { passive: false });
    return () => el.removeEventListener('wheel', onWheel);
    // Re-bind when the canvas remounts (e.g. after switching to 3D and back).
  }, [mode, zoomAbout]);

  // frameOn fits the view to an image-space box - the shared answer for "frame all" (the plan) and
  // "frame selected" (whatever is held).
  const frameOn = useCallback((box) => {
    if (!box || !(box.x2 > box.x1) || !(box.y2 > box.y1)) return;
    touchedRef.current = true;
    const pad = 40;
    const sw = Math.max(1, stage.w); const sh = Math.max(1, stage.h);
    const sc = clampScale(Math.min((sw - pad * 2) / (box.x2 - box.x1), (sh - pad * 2) / (box.y2 - box.y1)));
    setView({
      scale: sc,
      tx: sw / 2 - ((box.x1 + box.x2) / 2) * sc,
      ty: sh / 2 - ((box.y1 + box.y2) / 2) * sc,
    });
  }, [stage.w, stage.h]);
  const frameAll = useCallback(() => { touchedRef.current = false; setView(fitView(stage.w, stage.h)); }, [fitView, stage.w, stage.h]);

  const [cellMeters, setCellMeters] = useState(() => { const s = floor && floor.scale > 0 ? floor.scale : 0; return s > 0 ? +(s * unit).toFixed(2) : 0.5; });
  const [wallHeight, setWallHeight] = useState(() => (floor && floor.wallHeight > 0 ? floor.wallHeight : 2.7));
  const scale = cellMeters > 0 ? cellMeters / unit : 0;
  const scaleRef = useRef(scale); scaleRef.current = scale;

  // Is that scale REAL, or the nominal one the editor assumes so its grid means something?
  //
  // FloorPlan.Scale defaults to 0 = UNSET, but cellMeters above falls back to 0.5 m regardless -
  // so `scale` is never zero and every metre readout on an unscaled plan has been a guess printed
  // as a fact. A survey drawing that says "1.50 m" when nobody ever told it how big the plan is, is
  // worse than one that says "129 px", because it looks like an answer.
  //
  // The nominal stays (the grid, the 3D view and coverage all need SOMETHING), but anything shown
  // to an operator asks this first. `setscale`, and typing in the cell-size box, are the two ways
  // it becomes known.
  const [scaleKnown, setScaleKnown] = useState(() => !!(floor && floor.scale > 0));
  const shownScale = scaleKnown ? scale : 0;
  const shownScaleRef = useRef(shownScale); shownScaleRef.current = shownScale;

  // There are five kinds of thing that can be selected; clearing them one at a time at every call
  const clearSel = useCallback(() => setSelection(new Set()), []);
  const selKey = (kindName, i) => `${kindName}:${i}`;
  const isSel = (kindName, i) => selection.has(selKey(kindName, i));
  // Every index currently selected of one kind.
  const selOf = useCallback((kindName) => {
    const out = [];
    selection.forEach((k) => { const c = k.indexOf(':'); if (k.slice(0, c) === kindName) out.push(Number(k.slice(c + 1))); });
    return out;
  }, [selection]);
  // The index of the SOLE selected object when it is of this kind, else -1. Per-object inspectors
  // key off this: with several things held there is no single width or ascent to edit.
  const onlyOne = (kindName) => {
    if (selection.size !== 1) return -1;
    const a = selOf(kindName);
    return a.length === 1 ? a[0] : -1;
  };
  // Views onto the selection that the rest of the editor reads. selSegs is a set because walls are
  // routinely selected in bulk; the rest resolve to a single index only when nothing else is held.
  const selSegs = useMemo(() => new Set(selOf('seg')), [selOf]);
  const selStair = onlyOne('stair');
  const selDoor = onlyOne('door');
  const selWin = onlyOne('win');
  const selPark = onlyOne('park');
  const selPlat = onlyOne('plat');
  const selCam = onlyOne('cam');
  const selId = selCam >= 0 ? selCam : null;

  // A single selected CAMERA (a marker with a coverage wedge) is adjusted on the canvas, not through
  // the transform frame: drag its body to move it, its aim knob to point it, its edge knobs to widen
  // or narrow the field of view. The sliders stay for fine-tuning. Node markers (no wedge) keep the
  // ordinary frame. povCam returns that camera placement, or null.
  const povCam = () => {
    if (tool !== 'select' || selection.size !== 1 || selCam < 0) return null;
    const p = placements.find((x) => x.id === selCam);
    return p && p.cameraId && (p.fov || 0) > 0 ? p : null;
  };
  // The camera's on-canvas handles, in SCREEN pixels: the body, the aim knob at the wedge centre, and
  // the two edge knobs at heading ± fov/2. Heading 0 points up (north) and increases clockwise, the
  // same convention the wedge is drawn with.
  const camHandles = (p) => {
    const sx = p.x * ds; const sy = (h - p.y) * ds;
    const rr = arcRadius(w, h) * ds;
    const at = (deg) => { const a = (deg * Math.PI) / 180; return { x: sx + rr * Math.sin(a), y: sy - rr * Math.cos(a) }; };
    const hd = p.heading || 0; const half = (p.fov || 0) / 2;
    return { sx, sy, aim: at(hd), edgeL: at(hd - half), edgeR: at(hd + half) };
  };
  // headingTo: the compass bearing (0 = up, clockwise) from a camera to an image-space point.
  const headingTo = (p, im) => {
    const dx = im.x - p.x; const dy = im.y - (h - p.y); // image space, y down
    return ((Math.atan2(dx, -dy) * 180) / Math.PI + 360) % 360;
  };

  // Seed walls (segment shape, or legacy cells) and stairs from the floor's model.
  useEffect(() => {
    let segs = [];
    if (existing && Array.isArray(existing.segments)) segs = existing.segments.map((s) => ({ x1: s.x1, y1: s.y1, x2: s.x2, y2: s.y2 }));
    else if (existing && Array.isArray(existing.walls)) { const cell = existing.cellPx || unit; segs = existing.walls.map(([c, r]) => ({ x1: c * cell, y1: (r + 0.5) * cell, x2: (c + 1) * cell, y2: (r + 0.5) * cell })); }
    segsRef.current = segs;
    stairsRef.current = existing && Array.isArray(existing.stairs) ? existing.stairs.map((s) => ({ x1: s.x1, y1: s.y1, x2: s.x2, y2: s.y2, dir: s.dir || 'n', a: s.a || 0, steps: s.steps || 0, height: s.height > 0 ? s.height : 0, down: !!s.down })) : [];
    doorsRef.current = existing && Array.isArray(existing.doors) ? existing.doors.map((d) => ({ cx: d.cx, cy: d.cy, w: d.w, a: d.a || 0, hf: !!d.hf, sf: !!d.sf })) : [];
    windowsRef.current = existing && Array.isArray(existing.windows) ? existing.windows.map((d) => ({ cx: d.cx, cy: d.cy, w: d.w, a: d.a || 0, sill: d.sill, head: d.head })) : [];
    parkingRef.current = existing && Array.isArray(existing.parking) ? existing.parking.map((p) => ({ x1: p.x1, y1: p.y1, x2: p.x2, y2: p.y2, bays: p.bays || 1, a: p.a || 0 })) : [];
    platsRef.current = existing && Array.isArray(existing.platforms) ? existing.platforms.map((p) => ({ x1: p.x1, y1: p.y1, x2: p.x2, y2: p.y2, a: p.a || 0, rise: p.rise > 0 ? p.rise : DEF_RISE })) : [];
    // The outdoor kit is taken as stored: its shapes are declared in the registry, so there is no
    // per-field normalising to do here beyond guarding the point list.
    const takePoly = (list) => (Array.isArray(list) ? list.filter((o) => Array.isArray(o.pts) && o.pts.length).map((o) => ({ ...o, pts: o.pts.map((q) => ({ x: q.x, y: q.y })) })) : []);
    roadsRef.current = takePoly(existing && existing.roads);
    hedgesRef.current = takePoly(existing && existing.hedges);
    groundRef.current = takePoly(existing && existing.ground);
    treesRef.current = existing && Array.isArray(existing.trees) ? existing.trees.map((o) => ({ ...o })) : [];
    histRef.current = []; futRef.current = []; draftRef.current = null; clearSel();
    redraw();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [floor && floor.id]);

  useEffect(() => {
    const img = new Image();
    img.onload = () => { imgRef.current = img; redraw(); };
    img.onerror = () => { imgRef.current = null; redraw(); };
    img.src = `${apiBase()}/api/floors/${floor.id}/image?t=${floor.updatedAt || ''}`;
    return () => { imgRef.current = null; };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [floor && floor.id, floor && floor.updatedAt]);

  // The floor's model as stored in floor.grid. windows/parking are additive: a floor authored
  // before they existed simply has neither key, and every reader defaults them to empty.
  // The model is assembled FROM THE REGISTRY, so a type added there is saved without an edit here -
  // and every unrecognised key read at load is written back untouched.
  const listByRef = () => ({
    segs: segsRef.current, stairs: stairsRef.current, doors: doorsRef.current,
    windows: windowsRef.current, parking: parkingRef.current, platforms: platsRef.current,
    roads: roadsRef.current, trees: treesRef.current, hedges: hedgesRef.current, ground: groundRef.current,
  });
  const modelJSON = () => {
    const lists = listByRef();
    const arrays = {};
    Object.values(PLAN_OBJECTS).forEach((t) => { arrays[t.array] = lists[t.ref] || []; });
    // v3 = the outdoor kit exists. An older build reading this keeps the arrays it does not know
    // (see readModel/writeModel), which is the guarantee P3 landed before this phase for.
    return writeModel({ version: 3, unit, arrays, extras: extrasRef.current });
  };

  // Debounced autosave of the wall model + scale + height.
  //
  // saveTimer is nulled the moment the save fires, so "is there a timer" is an honest answer to
  // "is there unsaved work" — flushSave below depends on that being true.
  const saveNow = useCallback((urgent) => {
    onSaveModel({ grid: JSON.stringify(modelJSON()), scale: scaleRef.current, wallHeight: +wallHeight || 0, elevation: (floor && floor.elevation) || 0 }, { urgent });
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [onSaveModel, unit, wallHeight, floor && floor.elevation]);
  const scheduleSave = useCallback(() => {
    clearTimeout(saveTimer.current);
    saveTimer.current = setTimeout(() => { saveTimer.current = null; saveNow(false); }, 700);
  }, [saveNow]);
  useEffect(() => () => clearTimeout(saveTimer.current), []);

  // A pending autosave must not die with the tab.
  //
  // The editor now owns a whole browser tab, and people close tabs constantly — nobody ever closed
  // the old modal by accident. That turned a harmless 700 ms debounce into a real way to lose the
  // last edit: draw a wall, hit Ctrl+W, and it was never written.
  //
  // Three guards, weakest last. `visibilitychange` is the one that actually works — the page is
  // still alive and a normal request completes. `pagehide` is the backstop for a browser that skips
  // straight there, and takes `urgent` so the request goes out with keepalive and survives the
  // teardown. `beforeunload` only asks the browser to confirm, and only when something really is
  // pending; browsers ignore custom text, so there is none to write.
  const flushSave = useCallback((urgent) => {
    if (!saveTimer.current) return false;
    clearTimeout(saveTimer.current);
    saveTimer.current = null;
    saveNow(urgent);
    return true;
  }, [saveNow]);
  useEffect(() => {
    const onVisibility = () => { if (document.visibilityState === 'hidden') flushSave(true); };
    const onPageHide = () => flushSave(true);
    const onBeforeUnload = (e) => { if (flushSave(true)) { e.preventDefault(); e.returnValue = ''; } };
    document.addEventListener('visibilitychange', onVisibility);
    window.addEventListener('pagehide', onPageHide);
    window.addEventListener('beforeunload', onBeforeUnload);
    return () => {
      document.removeEventListener('visibilitychange', onVisibility);
      window.removeEventListener('pagehide', onPageHide);
      window.removeEventListener('beforeunload', onBeforeUnload);
    };
  }, [flushSave]);

  // History snapshots the WHOLE model, so undo/redo restores it in one step however many lists a
  // single edit touched. Taking a snapshot (rather than a per-list diff) is also why adding a new
  // kind of thing to the plan needs nothing here beyond listing it.
  // History and commit are driven by the registry's `ref` names, so a type added there is undoable
  // and committable without an edit here. The ref names are historic and not derivable, which is
  // exactly why the registry records them.
  const REF_SETTERS = {
    segs: (v) => { segsRef.current = v; },
    stairs: (v) => { stairsRef.current = v; },
    doors: (v) => { doorsRef.current = v; },
    windows: (v) => { windowsRef.current = v; },
    parking: (v) => { parkingRef.current = v; },
    platforms: (v) => { platsRef.current = v; },
    roads: (v) => { roadsRef.current = v; },
    trees: (v) => { treesRef.current = v; },
    hedges: (v) => { hedgesRef.current = v; },
    ground: (v) => { groundRef.current = v; },
  };
  const snapshot = () => {
    const lists = listByRef(); const out = {};
    Object.values(PLAN_OBJECTS).forEach((t) => { out[t.ref] = lists[t.ref] || []; });
    return out;
  };
  const restore = (m) => {
    Object.values(PLAN_OBJECTS).forEach((t) => { const set = REF_SETTERS[t.ref]; if (set) set(m[t.ref] || []); });
  };
  const pushHistory = useCallback(() => { histRef.current.push(snapshot()); if (histRef.current.length > 100) histRef.current.shift(); futRef.current = []; }, []);
  // commit takes a PATCH: name only the lists the edit changes, e.g. commit({ doors: next }).
  const commit = useCallback((patch) => {
    pushHistory();
    const p = patch || {};
    Object.values(PLAN_OBJECTS).forEach((t) => {
      if (p[t.ref] !== undefined) { const set = REF_SETTERS[t.ref]; if (set) set(p[t.ref]); }
    });
    redraw(); scheduleSave();
  }, [pushHistory, redraw, scheduleSave]);
  const undo = useCallback(() => { if (!histRef.current.length) return; futRef.current.push(snapshot()); restore(histRef.current.pop()); draftRef.current = null; redraw(); scheduleSave(); }, [redraw, scheduleSave]);
  const redo = useCallback(() => { if (!futRef.current.length) return; histRef.current.push(snapshot()); restore(futRef.current.pop()); redraw(); scheduleSave(); }, [redraw, scheduleSave]);
  // Re-save when scale/height change (but not on first mount).
  // finishSetScale turns "this run is 4.2 m" into the plan's metres-per-pixel.
  //
  // Scale defaults to UNSET (0), and the model is split on which side of that line each field sits:
  // door and window widths are pixels, sills and stair heights are metres. Every metre field is
  // dead until a plan has a scale, and the only way to give it one was a spinner labelled "each
  // cell" that asked the operator to do this arithmetic in their head.
  //
  // It sets `cellMeters` rather than a scale directly, because the cell is what the rest of the
  // editor is expressed in - so one answer here moves the grid, the readouts and the 3D view
  // together.
  const finishSetScale = useCallback((a, b) => {
    draftRef.current = null;
    const px = Math.hypot(b.x - a.x, b.y - a.y);
    if (px < 2) { redraw(); return; } // a click, not a measurement
    const answer = window.prompt(t('grid.setScalePrompt'), '');
    if (answer === null) { redraw(); return; }
    const metres = parseFloat(String(answer).replace(',', '.'));
    if (!(metres > 0)) {
      if (onToast) onToast(t('grid.setScaleBad'), 'error');
      redraw();
      return;
    }
    const perPx = metres / px;          // metres per image pixel
    const cell = +(perPx * unit).toFixed(3); // …expressed as the metre cell the editor works in
    setCellMeters(cell);
    setScaleKnown(true);
    if (onToast) onToast(t('grid.setScaleDone', { m: metres.toFixed(2), cell: cell.toFixed(2) }), 'success');
    redraw();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [unit, onToast, t, redraw]);

  const firstRef = useRef(true);
  useEffect(() => { if (firstRef.current) { firstRef.current = false; return; } if (segsRef.current.length || stairsRef.current.length) scheduleSave(); /* eslint-disable-next-line */ }, [cellMeters, wallHeight]);

  // The snap grid is a SUBDIVISION of the metre cell: `unit` still defines the cell (and the scale),
  // but drawing and snapping happen at unit/GRID_SUBDIV, so a marker locks onto a much finer lattice
  // without changing what a cell means in metres.
  const snap = unit / GRID_SUBDIV;

  // ---- snapping ---------------------------------------------------------------------------------
  //
  // Snapping used to be invisible and compulsory: always on, always the grid (with a silent
  // endpoint preference), no way to see what it caught and no way to turn it off for one click.
  // Now it is a mode the operator sets, Ctrl inverts it for as long as it is held, and whatever was
  // caught is drawn.
  //
  // The modes are cumulative in usefulness rather than exclusive: 'vertex', 'edge' and 'midpoint'
  // each still fall back to the grid when nothing is in range, because a drawing tool that snaps to
  // nothing when there is no geometry nearby is just a tool that does not snap.
  const [snapMode, setSnapMode] = useState('grid');
  const snapModeRef = useRef(snapMode); snapModeRef.current = snapMode;
  // Ctrl held = invert. From a snapping mode it means "let me place this freely"; from 'off' it
  // means "just this once, snap".
  const snapInvertRef = useRef(false);
  const lastSnapRef = useRef(null); // { x, y, kind } - what the pointer last caught, for the indicator

  const snapPt = useCallback((x, y) => {
    const st = unit / GRID_SUBDIV;
    const inverted = snapInvertRef.current;
    const mode = snapModeRef.current;
    const active = inverted ? (mode === 'off' ? 'grid' : 'off') : mode;
    if (active === 'off') { lastSnapRef.current = null; return { x, y }; }
    const reach = st * 0.9;
    let best = null; let bestD = reach;
    const take = (px, py, kind) => { const d = Math.hypot(px - x, py - y); if (d < bestD) { bestD = d; best = { x: px, y: py, kind }; } };
    if (active === 'vertex' || active === 'grid') {
      // 'grid' keeps the endpoint preference the editor always had - joining a wall to the one you
      // just drew is the single most common thing anyone does here, and losing it would be a
      // regression dressed up as a feature.
      segsRef.current.forEach((sg) => { take(sg.x1, sg.y1, 'vertex'); take(sg.x2, sg.y2, 'vertex'); });
    }
    if (active === 'midpoint') {
      segsRef.current.forEach((sg) => take((sg.x1 + sg.x2) / 2, (sg.y1 + sg.y2) / 2, 'midpoint'));
    }
    if (active === 'edge') {
      segsRef.current.forEach((sg) => {
        const vx = sg.x2 - sg.x1; const vy = sg.y2 - sg.y1; const len2 = vx * vx + vy * vy;
        if (len2 < 1e-6) return;
        let t = ((x - sg.x1) * vx + (y - sg.y1) * vy) / len2;
        t = Math.max(0, Math.min(1, t));
        take(sg.x1 + t * vx, sg.y1 + t * vy, 'edge');
      });
    }
    if (best) { lastSnapRef.current = best; return { x: best.x, y: best.y }; }
    const gx = Math.max(0, Math.min(w, Math.round(x / st) * st));
    const gy = Math.max(0, Math.min(h, Math.round(y / st) * st));
    lastSnapRef.current = { x: gx, y: gy, kind: 'grid' };
    return { x: gx, y: gy };
  }, [unit, w, h]);

  // ---- drawing ----
  const draw = useCallback(() => {
    const cv = canvasRef.current; if (!cv) return;
    const ctx = cv.getContext('2d');
    // The canvas is the VIEWPORT now, not the plan. Paint the surround, then translate into view
    // space so every `x * ds` below lands where the pan puts it. Line widths and handle sizes stay
    // in screen pixels, which is what we want: a handle is 7px whatever the zoom.
    ctx.setTransform(1, 0, 0, 1, 0, 0);
    ctx.clearRect(0, 0, cssW, cssH);
    ctx.fillStyle = VIEW_SURROUND;
    ctx.fillRect(0, 0, cssW, cssH);
    ctx.setTransform(1, 0, 0, 1, Math.round(view.tx), Math.round(view.ty));
    const planW = w * ds; const planH = h * ds;
    // The plan's own sheet, so the drawing area is legible against the surround.
    ctx.fillStyle = VIEW_SHEET;
    ctx.fillRect(0, 0, planW, planH);
    if (imgRef.current) { ctx.globalAlpha = 0.45; ctx.drawImage(imgRef.current, 0, 0, planW, planH); ctx.globalAlpha = 1; }
    // Drafting grid: faint minor lines on the fine snap step, stronger lines on the metre cell —
    // so the lattice reads clearly and an object is easy to line up. Minor lines are dropped once
    // they would be denser than a few pixels apart (zoomed out), to keep the canvas legible.
    const step = unit / GRID_SUBDIV;
    ctx.lineWidth = 1;
    // The lattice belongs to the PLAN, so it stops at the plan's edge rather than running out over
    // the surround. When the canvas was the plan those were the same thing.
    if (step * ds >= 4) {
      ctx.strokeStyle = GRID_MINOR;
      ctx.beginPath();
      for (let x = 0; x <= w; x += step) { ctx.moveTo(Math.round(x * ds) + 0.5, 0); ctx.lineTo(Math.round(x * ds) + 0.5, planH); }
      for (let y = 0; y <= h; y += step) { ctx.moveTo(0, Math.round(y * ds) + 0.5); ctx.lineTo(planW, Math.round(y * ds) + 0.5); }
      ctx.stroke();
    }
    ctx.strokeStyle = GRID_MAJOR;
    ctx.beginPath();
    for (let x = 0; x <= w; x += unit) { ctx.moveTo(Math.round(x * ds) + 0.5, 0); ctx.lineTo(Math.round(x * ds) + 0.5, planH); }
    for (let y = 0; y <= h; y += unit) { ctx.moveTo(0, Math.round(y * ds) + 0.5); ctx.lineTo(planW, Math.round(y * ds) + 0.5); }
    ctx.stroke();
    // A border on the sheet: without it the plan has no edge once it is smaller than the viewport.
    ctx.strokeStyle = GRID_EDGE; ctx.lineWidth = 1;
    ctx.strokeRect(0.5, 0.5, Math.round(planW) - 1, Math.round(planH) - 1);

    // What the outliner has hidden, read once for the whole frame.
    const hid = hiddenRef.current;

    const seg = (s, color, wd) => { ctx.strokeStyle = color; ctx.lineWidth = wd; ctx.lineCap = 'round'; ctx.beginPath(); ctx.moveTo(s.x1 * ds, s.y1 * ds); ctx.lineTo(s.x2 * ds, s.y2 * ds); ctx.stroke(); };
    const dot = (x, y, color, r = 3) => { ctx.fillStyle = color; ctx.beginPath(); ctx.arc(x, y, r, 0, Math.PI * 2); ctx.fill(); };

    // walls (image space) — selected walls are highlighted; a group drag previews the moved position.
    const mv = moveRef.current;
    const pv = mv && mv.moved ? xfObjects(mv.orig, mv.xf) : null;
    // ---- registry-drawn types ------------------------------------------------------------------
    //
    // Every type that declares `draw2d` is painted from its declaration. There is no per-type code
    // for roads, trees, hedges or ground anywhere in this file - that is the whole point of the
    // registry, and the reason the outdoor kit was four declarations rather than sixteen routines.
    //
    // `mpp` is the NOMINAL metres-per-pixel: geometry has to be drawn even on a plan nobody has
    // scaled, so this deliberately uses `scale` and not `shownScale`.
    const mppNow = scaleRef.current > 0 ? scaleRef.current : 0.5 / unit;
    const drawRegistry = (under) => {
      Object.keys(PLAN_OBJECTS).forEach((name) => {
        const spec = PLAN_OBJECTS[name];
        if (spec.builtin || !spec.draw2d) return;
        if (!!spec.under !== under) return;
        const list = listByRef()[spec.ref] || [];
        list.forEach((o, i) => {
          if (hid.has(`${spec.sel}:${i}`)) return;
          const moved = pv && pv.extra && pv.extra[spec.ref] && pv.extra[spec.ref].has(i) ? pv.extra[spec.ref].get(i) : o;
          ctx.save();
          spec.draw2d(ctx, moved, {
            ds, mpp: mppNow,
            sel: selection.has(`${spec.sel}:${i}`),
            hov: toolRef.current === 'erase' && hoverExtraRef.current.ref === spec.ref && hoverExtraRef.current.idx === i,
          });
          ctx.restore();
        });
      });
    };
    drawRegistry(true); // ground first: the surface everything else sits on
    segsRef.current.forEach((s, i) => {
      if (hid.has(`seg:${i}`)) return; // hidden in the outliner
      let ss = s;
      if (pv && pv.segs.has(i)) ss = pv.segs.get(i);
      const hov = toolRef.current === 'erase' && i === hoverRef.current;
      const selected = selSegs.has(i);
      const color = hov ? '#ef4444' : selected ? '#2d6cdf' : '#334155';
      // Draw the wall broken by every opening on it — doors AND windows — so the gaps show in 2D
      // as they do in 3D. The window symbol is then drawn back into its gap below.
      carveSeg(ss, doorsRef.current.concat(windowsRef.current), unit * 0.6).forEach((pc) => seg(pc, color, hov || selected ? 6 : 5));
      dot(ss.x1 * ds, ss.y1 * ds, color, 2.5); dot(ss.x2 * ds, ss.y2 * ds, color, 2.5);
    });

    // doors — an opening in the wall. In a building that is the classic hinge + swing arc + leaf;
    // on an outdoor site the same opening is a GATE, drawn as posts with a barred leaf, because a
    // swing arc through a fence line reads as a door into nothing.
    doorsRef.current.forEach((d, i) => {
      if (hid.has(`door:${i}`)) return; // hidden in the outliner
      const hov = toolRef.current === 'erase' && i === hoverDoorRef.current;
      const sel = isSel('door', i);
      if (pv && pv.doors.has(i)) d = pv.doors.get(i);
      const col = hov ? '#ef4444' : sel ? '#2d6cdf' : '#b45309';
      if (kind === KIND_OUTDOOR) {
        const ux = Math.cos(d.a); const uy = Math.sin(d.a); const nx = -Math.sin(d.a); const ny = Math.cos(d.a);
        const half = d.w / 2;
        const p0 = { x: d.cx - ux * half, y: d.cy - uy * half };
        const p1 = { x: d.cx + ux * half, y: d.cy + uy * half };
        ctx.strokeStyle = col; ctx.lineWidth = sel || hov ? 3 : 2.5; ctx.lineCap = 'round';
        // posts
        ctx.beginPath();
        ctx.moveTo((p0.x - nx * 4) * ds, (p0.y - ny * 4) * ds); ctx.lineTo((p0.x + nx * 4) * ds, (p0.y + ny * 4) * ds);
        ctx.moveTo((p1.x - nx * 4) * ds, (p1.y - ny * 4) * ds); ctx.lineTo((p1.x + nx * 4) * ds, (p1.y + ny * 4) * ds);
        ctx.stroke();
        // barred leaf across the opening
        ctx.lineWidth = sel || hov ? 2 : 1.5;
        ctx.beginPath();
        ctx.moveTo(p0.x * ds, p0.y * ds); ctx.lineTo(p1.x * ds, p1.y * ds);
        for (let k = 1; k < 4; k++) {
          const f = k / 4;
          const bx = p0.x + (p1.x - p0.x) * f; const by = p0.y + (p1.y - p0.y) * f;
          ctx.moveTo((bx - nx * 2.5) * ds, (by - ny * 2.5) * ds); ctx.lineTo((bx + nx * 2.5) * ds, (by + ny * 2.5) * ds);
        }
        ctx.stroke();
        return;
      }
      const ux = Math.cos(d.a); const uy = Math.sin(d.a); const nx = -Math.sin(d.a); const ny = Math.cos(d.a);
      const half = d.w / 2;
      // hf flips which END the hinge sits on (a left-hand vs right-hand door); sf flips which SIDE
      // of the wall the leaf swings to (opens in vs out). Together they give the four real door
      // hands, because a real building has doors hinged every which way.
      const hs = d.hf ? -1 : 1; const ss = d.sf ? -1 : 1;
      const hinge = { x: d.cx - ux * half * hs, y: d.cy - uy * half * hs };
      const latch = { x: d.cx + ux * half * hs, y: d.cy + uy * half * hs };
      const tip = { x: hinge.x + nx * d.w * ss, y: hinge.y + ny * d.w * ss };
      ctx.strokeStyle = col; ctx.lineWidth = sel || hov ? 2.5 : 2; ctx.lineCap = 'round';
      // jambs
      ctx.beginPath(); ctx.moveTo((hinge.x - nx * 3) * ds, (hinge.y - ny * 3) * ds); ctx.lineTo((hinge.x + nx * 3) * ds, (hinge.y + ny * 3) * ds);
      ctx.moveTo((latch.x - nx * 3) * ds, (latch.y - ny * 3) * ds); ctx.lineTo((latch.x + nx * 3) * ds, (latch.y + ny * 3) * ds); ctx.stroke();
      // leaf
      ctx.beginPath(); ctx.moveTo(hinge.x * ds, hinge.y * ds); ctx.lineTo(tip.x * ds, tip.y * ds); ctx.stroke();
      // swing arc from latch (closed) to the open leaf tip, centred on the hinge — always the short
      // 90° sweep, whichever hand the flips put the leaf in.
      const a0 = Math.atan2(latch.y - hinge.y, latch.x - hinge.x); const a1 = Math.atan2(tip.y - hinge.y, tip.x - hinge.x);
      let delta = a1 - a0; while (delta > Math.PI) delta -= 2 * Math.PI; while (delta < -Math.PI) delta += 2 * Math.PI;
      ctx.beginPath(); ctx.setLineDash([4, 3]); ctx.arc(hinge.x * ds, hinge.y * ds, d.w * ds, a0, a1, delta < 0); ctx.stroke(); ctx.setLineDash([]);
    });

    // windows — the standard plan symbol: jambs, then three thin lines across the gap (the two wall
    // faces and the glazing between them). No swing, because a window is an opening you see through
    // rather than pass through — which is exactly why it matters to a camera's sight line.
    windowsRef.current.forEach((d, i) => {
      if (hid.has(`win:${i}`)) return; // hidden in the outliner
      const hov = toolRef.current === 'erase' && i === hoverWinRef.current;
      const sel = isSel('win', i);
      if (pv && pv.wins.has(i)) d = pv.wins.get(i);
      const col = hov ? '#ef4444' : sel ? '#2d6cdf' : '#0369a1';
      const ux = Math.cos(d.a); const uy = Math.sin(d.a); const nx = -Math.sin(d.a); const ny = Math.cos(d.a);
      const half = d.w / 2;
      const p0 = { x: d.cx - ux * half, y: d.cy - uy * half };
      const p1 = { x: d.cx + ux * half, y: d.cy + uy * half };
      // The symbol is drawn in the window's own frame, which keeps the maths to plain offsets and
      // lets the frame be a real filled shape rather than a few hairlines. Three thin parallel
      // strokes were far too faint to find on a busy plan — a window is a hole in the envelope and
      // should read as loudly as the door next to it.
      const depth = Math.max(3.2, 5 / ds); // how far the frame stands off the wall centre line
      ctx.save();
      ctx.translate(d.cx * ds, d.cy * ds);
      ctx.rotate(d.a || 0);
      const hw = half * ds; const hd = depth * ds;
      // Opaque frame body: punches a clean break through the wall behind it.
      ctx.fillStyle = hov ? 'rgba(254,226,226,0.98)' : sel ? 'rgba(219,234,254,0.98)' : 'rgba(255,255,255,0.98)';
      ctx.fillRect(-hw, -hd, hw * 2, hd * 2);
      ctx.strokeStyle = col; ctx.lineJoin = 'round'; ctx.lineCap = 'butt';
      ctx.lineWidth = sel || hov ? 2.4 : 2;
      ctx.strokeRect(-hw, -hd, hw * 2, hd * 2);
      // Glazing: a bold bar down the middle in glass blue, with a highlight above it so the pane
      // reads as glass and not as another wall line.
      ctx.fillStyle = hov ? '#ef4444' : sel ? '#2d6cdf' : '#38bdf8';
      ctx.fillRect(-hw, -hd * 0.34, hw * 2, hd * 0.68);
      ctx.fillStyle = 'rgba(255,255,255,0.55)';
      ctx.fillRect(-hw, -hd * 0.34, hw * 2, hd * 0.22);
      // Jambs: heavy end stops, the cue that the wall genuinely stops here.
      ctx.strokeStyle = col; ctx.lineCap = 'round';
      ctx.lineWidth = sel || hov ? 4 : 3.2;
      ctx.beginPath();
      ctx.moveTo(-hw, -hd * 1.35); ctx.lineTo(-hw, hd * 1.35);
      ctx.moveTo(hw, -hd * 1.35); ctx.lineTo(hw, hd * 1.35);
      ctx.stroke();
      ctx.restore();
    });

    // raised floors — a footprint that sits higher, shown as a hatched slab with its rise labelled,
    // so it reads as a platform you step up onto (and reach by stairs). Drawn before parking/markers.
    platsRef.current.forEach((p, i) => {
      if (hid.has(`plat:${i}`)) return; // hidden in the outliner
      const hov = toolRef.current === 'erase' && i === hoverPlatRef.current;
      const sel = isSel('plat', i);
      if (pv && pv.plats.has(i)) p = pv.plats.get(i);
      const col = hov ? '#ef4444' : sel ? '#2d6cdf' : '#a16207';
      const pc = rectCenter(p);
      ctx.save(); ctx.translate(pc.x * ds, pc.y * ds); ctx.rotate(p.a || 0); ctx.translate(-pc.x * ds, -pc.y * ds);
      const x1 = p.x1 * ds; const y1 = p.y1 * ds; const x2 = p.x2 * ds; const y2 = p.y2 * ds;
      ctx.fillStyle = hov ? 'rgba(239,68,68,0.10)' : sel ? 'rgba(45,108,223,0.12)' : 'rgba(161,98,7,0.12)';
      ctx.fillRect(x1, y1, x2 - x1, y2 - y1);
      // Diagonal hatch marks the raised area apart from the flat floor.
      ctx.save(); ctx.beginPath(); ctx.rect(x1, y1, x2 - x1, y2 - y1); ctx.clip();
      ctx.strokeStyle = hov ? 'rgba(239,68,68,0.35)' : sel ? 'rgba(45,108,223,0.35)' : 'rgba(161,98,7,0.30)'; ctx.lineWidth = 1;
      const gap = 12; ctx.beginPath();
      for (let d2 = -Math.abs(y2 - y1); d2 < (x2 - x1); d2 += gap) { ctx.moveTo(x1 + d2, y1); ctx.lineTo(x1 + d2 + (y2 - y1), y2); }
      ctx.stroke(); ctx.restore();
      ctx.strokeStyle = col; ctx.lineWidth = sel || hov ? 2.5 : 2; ctx.strokeRect(x1, y1, x2 - x1, y2 - y1);
      // Rise label ("+0.6 m") at the centre.
      const txt = `+${(p.rise > 0 ? p.rise : DEF_RISE).toFixed(2)} m`;
      ctx.fillStyle = col; ctx.font = '600 11px system-ui, sans-serif'; ctx.textAlign = 'center'; ctx.textBaseline = 'middle';
      ctx.fillText(txt, (x1 + x2) / 2, (y1 + y2) / 2);
      ctx.textAlign = 'start'; ctx.textBaseline = 'alphabetic';
      ctx.restore();
    });

    // parking — a row of bays: the footprint outline plus the stall divider lines. Drawn under the
    // wall/fence layer's colours but above the plan image, so it reads as ground marking.
    parkingRef.current.forEach((p, i) => {
      if (hid.has(`park:${i}`)) return; // hidden in the outliner
      const hov = toolRef.current === 'erase' && i === hoverParkRef.current;
      const sel = isSel('park', i);
      if (pv && pv.parks.has(i)) p = pv.parks.get(i);
      const col = hov ? '#ef4444' : sel ? '#2d6cdf' : '#475569';
      // Drawn in the footprint's OWN frame so its rotation costs nothing but a save/rotate/restore;
      // every coordinate below stays the plain axis-aligned one it always was.
      const pc = rectCenter(p);
      ctx.save(); ctx.translate(pc.x * ds, pc.y * ds); ctx.rotate(p.a || 0); ctx.translate(-pc.x * ds, -pc.y * ds);
      const x1 = p.x1 * ds; const y1 = p.y1 * ds; const x2 = p.x2 * ds; const y2 = p.y2 * ds;
      ctx.fillStyle = hov ? 'rgba(239,68,68,0.10)' : sel ? 'rgba(45,108,223,0.12)' : 'rgba(71,85,105,0.08)';
      ctx.fillRect(x1, y1, x2 - x1, y2 - y1);
      ctx.strokeStyle = col; ctx.lineWidth = sel || hov ? 2.5 : 2;
      ctx.strokeRect(x1, y1, x2 - x1, y2 - y1);
      const n = Math.max(1, Math.min(60, p.bays || 1));
      const acrossX = baysAcrossX(p);
      ctx.lineWidth = 1.25; ctx.beginPath();
      for (let k = 1; k < n; k++) {
        const f = k / n;
        if (acrossX) { const xx = x1 + (x2 - x1) * f; ctx.moveTo(xx, y1); ctx.lineTo(xx, y2); }
        else { const yy = y1 + (y2 - y1) * f; ctx.moveTo(x1, yy); ctx.lineTo(x2, yy); }
      }
      ctx.stroke();
      // The bay count, so a row reads as "12 bays" without counting the lines.
      ctx.fillStyle = col;
      ctx.font = '600 11px system-ui, sans-serif'; ctx.textAlign = 'center'; ctx.textBaseline = 'middle';
      ctx.fillText(`P·${n}`, (x1 + x2) / 2, (y1 + y2) / 2);
      ctx.textAlign = 'start'; ctx.textBaseline = 'alphabetic';
      ctx.restore();
    });

    // stairs — footprint + a chosen number of step (tread) lines across the run + an ascent arrow.
    stairsRef.current.forEach((s, i) => {
      if (hid.has(`stair:${i}`)) return; // hidden in the outliner
      const hov = toolRef.current === 'erase' && i === hoverStairRef.current;
      const sel = isSel('stair', i);
      if (pv && pv.stairs.has(i)) s = pv.stairs.get(i);
      const color = hov ? '#ef4444' : sel ? '#2d6cdf' : '#0f766e';
      const sc = rectCenter(s);
      ctx.save(); ctx.translate(sc.x * ds, sc.y * ds); ctx.rotate(s.a || 0); ctx.translate(-sc.x * ds, -sc.y * ds);
      const x1 = s.x1 * ds; const y1 = s.y1 * ds; const x2 = s.x2 * ds; const y2 = s.y2 * ds;
      ctx.fillStyle = hov ? 'rgba(239,68,68,0.10)' : sel ? 'rgba(45,108,223,0.12)' : 'rgba(15,118,110,0.12)';
      ctx.fillRect(x1, y1, x2 - x1, y2 - y1);
      ctx.strokeStyle = color; ctx.lineWidth = sel || hov ? 2.5 : 2; ctx.strokeRect(x1, y1, x2 - x1, y2 - y1);
      // Step count: the operator's chosen number of tread lines, or a default from the storey height.
      const steps = stairSteps(s);
      const vertical = s.dir === 'n' || s.dir === 's';
      ctx.lineWidth = 1.25; ctx.beginPath();
      for (let k = 1; k < steps; k++) {
        const f = k / steps;
        if (vertical) { const yy = y1 + (y2 - y1) * f; ctx.moveTo(x1, yy); ctx.lineTo(x2, yy); }
        else { const xx = x1 + (x2 - x1) * f; ctx.moveTo(xx, y1); ctx.lineTo(xx, y2); }
      }
      ctx.stroke();
      // ascent arrow: from the bottom of the flight to the top (the direction you climb)
      const cx = (x1 + x2) / 2; const cy = (y1 + y2) / 2;
      let bx; let by; let tx; let ty;
      if (s.dir === 'n') { bx = cx; by = y2; tx = cx; ty = y1; }
      else if (s.dir === 's') { bx = cx; by = y1; tx = cx; ty = y2; }
      else if (s.dir === 'w') { bx = x2; by = cy; tx = x1; ty = cy; }
      else { bx = x1; by = cy; tx = x2; ty = cy; }
      const ang = Math.atan2(ty - by, tx - bx); const ah = 7;
      ctx.strokeStyle = color; ctx.lineWidth = 2; ctx.beginPath(); ctx.moveTo(bx, by); ctx.lineTo(tx, ty); ctx.stroke();
      ctx.beginPath(); ctx.moveTo(tx, ty);
      ctx.lineTo(tx - ah * Math.cos(ang - 0.4), ty - ah * Math.sin(ang - 0.4));
      ctx.moveTo(tx, ty); ctx.lineTo(tx - ah * Math.cos(ang + 0.4), ty - ah * Math.sin(ang + 0.4));
      ctx.stroke();
      // A short note: the flight's climb, and — when it rests on a raised floor — the base it sits
      // on, so "on +0.60 m" reads next to the platform's own "+0.60 m".
      const baseH = stairBaseH(s);
      const arrow = s.down ? '↧' : '↥'; // ↥ climbs, ↧ descends
      const note = baseH > 0 ? `${arrow} ${t('grid.onBase')} +${baseH.toFixed(2)} m` : `${arrow} ${stairClimbH(s).toFixed(2)} m`;
      ctx.fillStyle = color; ctx.font = '600 10px system-ui, sans-serif'; ctx.textAlign = 'center'; ctx.textBaseline = 'middle';
      ctx.fillText(note, (x1 + x2) / 2, (y1 + y2) / 2);
      ctx.textAlign = 'start'; ctx.textBaseline = 'alphabetic';
      ctx.restore();
    });

    // A single selected camera gets its own POV handles instead of the transform frame: an aim knob
    // at the wedge tip, an edge knob on each side of the field of view. Drawn after the markers below
    // via povCamHandle, so here we just suppress the transform frame for it.
    const cam = povCam();

    // Transform frame: the box around the selection, its eight resize handles, and the rotate knob
    // on a stalk above it. Drawn last of the plan layers so nothing can cover a handle, and hidden
    // mid-gesture for rotate/scale so the operator watches the geometry rather than the frame.
    if (tool === 'select' && selection.size && !cam) {
      const fr = pv ? selFrameFor(pv) : selFrame();
      if (fr) {
        // Draw in the frame's OWN rotation, so a turned object's handles sit on its actual sides.
        ctx.save();
        ctx.translate(fr.cx * ds, fr.cy * ds);
        ctx.rotate(fr.a);
        const hw = fr.hw * ds; const hh = fr.hh * ds;
        ctx.setLineDash([4, 3]); ctx.strokeStyle = 'rgba(45,108,223,0.85)'; ctx.lineWidth = 1;
        ctx.strokeRect(-hw, -hh, hw * 2, hh * 2); ctx.setLineDash([]);
        const knob = (kx, ky, round) => {
          ctx.beginPath();
          if (round) ctx.arc(kx, ky, HANDLE_PX / 2 + 1, 0, Math.PI * 2);
          else ctx.rect(kx - HANDLE_PX / 2, ky - HANDLE_PX / 2, HANDLE_PX, HANDLE_PX);
          ctx.fillStyle = '#ffffff'; ctx.fill();
          ctx.strokeStyle = '#2d6cdf'; ctx.lineWidth = 1.5; ctx.stroke();
        };
        HANDLES.forEach((hd) => knob((hd.fx * 2 - 1) * hw, (hd.fy * 2 - 1) * hh, false));
        // rotate knob on a stalk beyond the top edge, in the frame's up-direction
        ctx.beginPath(); ctx.moveTo(0, -hh); ctx.lineTo(0, -hh - ROTATE_ARM_PX);
        ctx.strokeStyle = 'rgba(45,108,223,0.85)'; ctx.lineWidth = 1; ctx.stroke();
        knob(0, -hh - ROTATE_ARM_PX, true);
        ctx.restore();
      }
    }

    // marquee selection box
    const mq = marqueeRef.current;
    if (mq) {
      const rx = Math.min(mq.x1, mq.x2); const ry = Math.min(mq.y1, mq.y2); const rw = Math.abs(mq.x2 - mq.x1); const rh = Math.abs(mq.y2 - mq.y1);
      ctx.setLineDash([5, 4]); ctx.strokeStyle = '#2d6cdf'; ctx.lineWidth = 1.5; ctx.strokeRect(rx * ds, ry * ds, rw * ds, rh * ds); ctx.setLineDash([]);
      ctx.fillStyle = 'rgba(45,108,223,0.08)'; ctx.fillRect(rx * ds, ry * ds, rw * ds, rh * ds);
    }

    // camera / node markers (OL coords → flip y). FOV wedge for cameras.
    const rad = arcRadius(w, h);
    drawRegistry(false); // roads, hedges and trees over the structure

    placements.forEach((p) => {
      if (hid.has(`cam:${p.id}`)) return; // hidden in the outliner
      // Markers live in OL space (y UP) while the drag delta is image space (y DOWN), so the
      // vertical offset is subtracted here and again when the move is persisted.
      const o = pv && pv.cams.get(p.id);
      const px = o ? o.x : p.x; const py = o ? o.y : p.y;
      // The preview carries a turned HEADING as well as a moved position (a rotated selection turns
      // its cameras with it), so the wedge has to read both from it - taking the position from the
      // preview but the heading from the stored row leaves the cone pointing the old way mid-drag.
      const hdg = o ? (o.heading || 0) : (p.heading || 0);
      const sx = px * ds; const sy = (h - py) * ds;
      const tone = nodeTone(nodesById[p.nodeId], nowSecRef.current) || TONES.idle;
      const isCam = !!p.cameraId;
      if (isCam && (p.fov || 0) > 0) {
        const half = (p.fov || 0) / 2; const N = 22;
        ctx.beginPath(); ctx.moveTo(sx, sy);
        for (let i = 0; i <= N; i++) { const deg = hdg - half + (p.fov || 0) * (i / N); const a = (deg * Math.PI) / 180; ctx.lineTo(sx + rad * ds * Math.sin(a), sy - rad * ds * Math.cos(a)); }
        ctx.closePath(); ctx.fillStyle = `${tone.color}28`; ctx.fill(); ctx.strokeStyle = `${tone.color}88`; ctx.lineWidth = 1; ctx.stroke();
      }
      const selected = isSel('cam', p.id);
      // A touch larger than the old bare disc, so a legible glyph fits inside it.
      const r = isCam ? 9 : 11;
      ctx.beginPath(); ctx.arc(sx, sy, r, 0, Math.PI * 2);
      ctx.fillStyle = tone.color; ctx.fill();
      ctx.lineWidth = selected ? 3 : 2; ctx.strokeStyle = selected ? '#2d6cdf' : '#fff'; ctx.stroke();
      // The same glyph the tray, the cursor and the read-only view use, in white on the tone disc.
      const glyph = isCam ? 'video' : APPLIANCE_ICON;
      const gimg = markerIcon(glyph, '#ffffff', redraw);
      if (gimg) { const gs = r * 1.15; ctx.drawImage(gimg, sx - gs / 2, sy - gs / 2, gs, gs); }
      const label = p.lastKnownName || (isCam ? `Cam ${p.cameraId}` : (nodesById[p.nodeId]?.name || p.nodeId));
      ctx.font = '600 11px system-ui, sans-serif'; ctx.textAlign = 'center';
      const ly = sy - r - 5;
      ctx.lineWidth = 3; ctx.strokeStyle = 'rgba(255,255,255,0.85)'; ctx.strokeText(label, sx, ly);
      ctx.fillStyle = '#1f2937'; ctx.fillText(label, sx, ly); ctx.textAlign = 'left';
    });

    // Camera POV handles: draw over the selected camera's wedge so it can be aimed and widened by
    // dragging. An accent line runs from the body out to each knob; the aim knob (round) points the
    // camera, the two edge knobs set the spread. A live readout shows the current heading/fov.
    if (cam) {
      // Follow the drag preview, exactly as the marker and its wedge do. camHandles reads a
      // placement's stored x/y/heading, so handing it the committed row while a drag is in flight
      // left the aim knob, the edge knobs, the lines out to them and the heading readout sitting at
      // the camera's OLD spot - the marker and its aiming furniture visibly coming apart.
      const co = pv && pv.cams.get(cam.id);
      const H = camHandles(co ? { ...cam, x: co.x, y: co.y, heading: co.heading } : cam);
      ctx.strokeStyle = '#2d6cdf'; ctx.lineWidth = 1;
      [H.aim, H.edgeL, H.edgeR].forEach((k) => { ctx.beginPath(); ctx.moveTo(H.sx, H.sy); ctx.lineTo(k.x, k.y); ctx.stroke(); });
      const knob = (k, r) => { ctx.beginPath(); ctx.arc(k.x, k.y, r, 0, Math.PI * 2); ctx.fillStyle = '#ffffff'; ctx.fill(); ctx.strokeStyle = '#2d6cdf'; ctx.lineWidth = 1.5; ctx.stroke(); };
      knob(H.edgeL, HANDLE_PX / 2 + 1); knob(H.edgeR, HANDLE_PX / 2 + 1);
      knob(H.aim, HANDLE_PX / 2 + 2.5); // aim knob a touch larger so it reads as the primary control
      const txt = `${Math.round(cam.heading || 0)}° · ${Math.round(cam.fov || 0)}°`;
      ctx.font = '600 11px system-ui, sans-serif'; ctx.textAlign = 'center';
      ctx.lineWidth = 3; ctx.strokeStyle = 'rgba(255,255,255,0.9)'; ctx.strokeText(txt, H.aim.x, H.aim.y - 12);
      ctx.fillStyle = '#2d6cdf'; ctx.fillText(txt, H.aim.x, H.aim.y - 12); ctx.textAlign = 'left';
    }

    // wall draft preview + measurement
    const label = (mx, my, text) => { ctx.font = '600 12px system-ui, sans-serif'; const pad = 4; const tw = ctx.measureText(text).width; ctx.fillStyle = 'rgba(15,23,33,0.85)'; ctx.fillRect(mx + 8, my - 20, tw + pad * 2, 18); ctx.fillStyle = '#fff'; ctx.fillText(text, mx + 8 + pad, my - 7); };
    // Measurements shown while drawing use the KNOWN scale, so an unscaled plan measures in pixels
    // rather than in metres nobody has established. See the scaleKnown note.
    const sc = shownScaleRef.current;
    const mUnit = sc > 0 ? 'm' : 'px';
    const mOf = (a, b) => (Math.hypot(b.x - a.x, b.y - a.y) * (sc > 0 ? sc : 1)).toFixed(sc > 0 ? 1 : 0);
    const d = draftRef.current; const cur = cursorRef.current;
    if (d && d.pts) {
      // A ground area previews CLOSED, because that is what it will become - an outline that looks
      // like an open run right up until it is committed would be a preview that lies.
      if (d.poly && PLAN_OBJECTS[Object.keys(PLAN_OBJECTS).find((k) => PLAN_OBJECTS[k].tool && PLAN_OBJECTS[k].tool.id === d.poly)]?.closed && d.pts.length > 2) {
        const f = d.pts[0]; const l2 = d.pts[d.pts.length - 1];
        seg({ x1: l2.x, y1: l2.y, x2: f.x, y2: f.y }, 'rgba(45,108,223,0.45)', 3);
      }
      for (let i = 0; i < d.pts.length - 1; i++) seg({ x1: d.pts[i].x, y1: d.pts[i].y, x2: d.pts[i + 1].x, y2: d.pts[i + 1].y }, '#2d6cdf', 5);
      d.pts.forEach((p) => dot(p.x * ds, p.y * ds, '#2d6cdf'));
      const last = d.pts[d.pts.length - 1];
      if (cur && last) { seg({ x1: last.x, y1: last.y, x2: cur.x, y2: cur.y }, 'rgba(45,108,223,0.6)', 4); label(cur.x * ds, cur.y * ds, `${mOf(last, cur)} ${mUnit}`); }
    } else if (d && d.round && d.start && cur) {
      const cx = (d.start.x + cur.x) / 2; const cy = (d.start.y + cur.y) / 2; const rx = Math.abs(cur.x - d.start.x) / 2; const ry = Math.abs(cur.y - d.start.y) / 2;
      ctx.beginPath(); ctx.ellipse(cx * ds, cy * ds, rx * ds, ry * ds, 0, 0, Math.PI * 2);
      ctx.fillStyle = 'rgba(45,108,223,0.12)'; ctx.fill(); ctx.strokeStyle = '#2d6cdf'; ctx.lineWidth = 4; ctx.stroke();
      label(cur.x * ds, cur.y * ds, `${(rx * 2 * (sc > 0 ? sc : 1)).toFixed(sc > 0 ? 1 : 0)} × ${(ry * 2 * (sc > 0 ? sc : 1)).toFixed(sc > 0 ? 1 : 0)} ${mUnit}`);
    } else if (d && d.start && cur) {
      const rx = Math.min(d.start.x, cur.x); const ry = Math.min(d.start.y, cur.y); const rw = Math.abs(cur.x - d.start.x); const rh = Math.abs(cur.y - d.start.y);
      ctx.strokeStyle = '#2d6cdf'; ctx.lineWidth = 4; ctx.strokeRect(rx * ds, ry * ds, rw * ds, rh * ds); ctx.fillStyle = 'rgba(45,108,223,0.12)'; ctx.fillRect(rx * ds, ry * ds, rw * ds, rh * ds);
      label(cur.x * ds, cur.y * ds, `${(rw * (sc > 0 ? sc : 1)).toFixed(sc > 0 ? 1 : 0)} × ${(rh * (sc > 0 ? sc : 1)).toFixed(sc > 0 ? 1 : 0)} ${mUnit}`);
    } else if (cur && DRAG_TOOLS.has(toolRef.current)) dot(cur.x * ds, cur.y * ds, 'rgba(45,108,223,0.7)', 4);

    // The set-scale rubber band: a plain measured line, with its pixel length, waiting to be told
    // what that length is in metres.
    if (d && d.scaleFrom && cur) {
      ctx.setLineDash([6, 4]);
      seg({ x1: d.scaleFrom.x, y1: d.scaleFrom.y, x2: cur.x, y2: cur.y }, SNAP_COLOR, 2);
      ctx.setLineDash([]);
      dot(d.scaleFrom.x * ds, d.scaleFrom.y * ds, SNAP_COLOR, 4);
      dot(cur.x * ds, cur.y * ds, SNAP_COLOR, 4);
      // The set-scale rubber band always measures in PIXELS: metres are the answer it is about to
      // be given, so showing a metre value here would be showing the guess it exists to replace.
      label(cur.x * ds, cur.y * ds, `${Math.round(Math.hypot(cur.x - d.scaleFrom.x, cur.y - d.scaleFrom.y))} px`);
    }

    // The snap indicator: a ring on what the pointer actually caught. Snapping used to be
    // invisible, so a marker that jumped looked like a bug rather than a feature doing its job.
    const sn = lastSnapRef.current;
    if (sn && cur && (DRAG_TOOLS.has(toolRef.current) || toolRef.current === 'setscale')) {
      const sx = sn.x * ds; const sy = sn.y * ds;
      ctx.strokeStyle = SNAP_COLOR; ctx.lineWidth = 1.5;
      if (sn.kind === 'vertex') { ctx.strokeRect(sx - 4.5, sy - 4.5, 9, 9); }
      else if (sn.kind === 'midpoint') { ctx.beginPath(); ctx.moveTo(sx, sy - 5); ctx.lineTo(sx + 5, sy); ctx.lineTo(sx, sy + 5); ctx.lineTo(sx - 5, sy); ctx.closePath(); ctx.stroke(); }
      else if (sn.kind === 'edge') { ctx.beginPath(); ctx.moveTo(sx - 6, sy); ctx.lineTo(sx + 6, sy); ctx.moveTo(sx, sy - 6); ctx.lineTo(sx, sy + 6); ctx.stroke(); }
      else { ctx.beginPath(); ctx.arc(sx, sy, 4.5, 0, Math.PI * 2); ctx.stroke(); }
    }
  }, [cssW, cssH, w, h, unit, ds, view.tx, view.ty, snapMode, shownScale, placements, selection, wallHeight, kind, t]);
  useEffect(() => { if (mode === '2d') draw(); });

  // ---- geometry helpers on the canvas ----
  // Screen → image, undoing the whole view transform (pan included).
  const evImg = (e) => { const r = canvasRef.current.getBoundingClientRect(); const v = viewRef.current; return { x: ((e.clientX - r.left) - v.tx) / v.scale, y: ((e.clientY - r.top) - v.ty) / v.scale }; };
  const evOL = (e) => { const p = evImg(e); return { x: p.x, y: h - p.y }; }; // OL space (bottom-left) for cameras
  // Every hit test skips what the outliner has hidden or locked. That IS what those toggles mean:
  // hidden things are not there to click, and a locked one is deliberately click-through so the
  // thing underneath it can be reached.
  const hitMarker = (ol) => { let best = 14 / ds; let hit = null; placements.forEach((p) => { if (!isPickable('cam', p.id)) return; const d = Math.hypot(p.x - ol.x, p.y - ol.y); if (d < best) { best = d; hit = p; } }); return hit; };
  const nearestSeg = (im) => { let idx = -1; let best = 8 / ds; segsRef.current.forEach((s, i) => { if (!isPickable('seg', i)) return; const dd = dist2seg(im.x, im.y, s); if (dd < best) { best = dd; idx = i; } }); return idx; };
  // Topmost stair whose footprint contains the point (last drawn wins, matching paint order).
  const hitStair = (im) => { for (let i = stairsRef.current.length - 1; i >= 0; i--) { if (isPickable('stair', i) && pointInRotatedRect(im.x, im.y, stairsRef.current[i])) return i; } return -1; };
  // Openings are small targets sitting on a wall; both are hit the same way.
  // An opening is clicked on the SYMBOL it draws, not just the point it was dropped on. Project the
  // pointer into the opening's own frame (u along the wall, n across it) and test a box that covers
  // what is actually drawn: the full width along the wall, and — for a building door — the leaf and
  // swing arc that reach a whole door-width out on the swing side. A gate/window is a thin symbol,
  // so its box is just the wall depth. margin (in image units) keeps thin openings easy to grab.
  const pointInOpening = (im, d, swings) => {
    const ux = Math.cos(d.a || 0); const uy = Math.sin(d.a || 0);
    const nx = -uy; const ny = ux;
    const lu = (im.x - d.cx) * ux + (im.y - d.cy) * uy; // along the wall
    const ln = (im.x - d.cx) * nx + (im.y - d.cy) * ny; // across the wall
    const m = 7 / ds;
    if (Math.abs(lu) > d.w / 2 + m) return false;
    if (swings) {
      // The leaf + arc live on the swing side; allow a small margin on the near side too.
      const s = ln * (d.sf ? -1 : 1);
      return s >= -(m + 5 / ds) && s <= d.w + m;
    }
    return Math.abs(ln) <= m + 5 / ds;
  };
  const hitOpening = (list, im, swings, tag) => { for (let i = list.length - 1; i >= 0; i--) { if (isPickable(tag, i) && pointInOpening(im, list[i], swings)) return i; } return -1; };
  const hitDoor = (im) => hitOpening(doorsRef.current, im, kind === KIND_BUILDING, 'door');
  const hitWindow = (im) => hitOpening(windowsRef.current, im, false, 'win');
  const hitParking = (im) => { for (let i = parkingRef.current.length - 1; i >= 0; i--) { if (isPickable('park', i) && pointInRotatedRect(im.x, im.y, parkingRef.current[i])) return i; } return -1; };
  const hitPlatform = (im) => { for (let i = platsRef.current.length - 1; i >= 0; i--) { if (isPickable('plat', i) && pointInRotatedRect(im.x, im.y, platsRef.current[i])) return i; } return -1; };

  // ---- hit-testing the registry-drawn types ----------------------------------------------------
  //
  // Driven by `geometry`, so a declaration is clickable the moment it exists: a polyline is hit
  // near its run (widened by its real width), a point within its own radius. Ground is tested LAST
  // and only if nothing else answered - it is a background area, and letting it swallow clicks
  // would make everything drawn on top of it unreachable.
  const mppRef = useRef(0.5); // nominal metres-per-pixel, kept for hit tests outside the draw
  const hitRegistry = (im) => {
    const mpp = scaleRef.current > 0 ? scaleRef.current : 0.5 / unit;
    const names = Object.keys(PLAN_OBJECTS).filter((n) => !PLAN_OBJECTS[n].builtin);
    const order = names.filter((n) => !PLAN_OBJECTS[n].under).concat(names.filter((n) => PLAN_OBJECTS[n].under));
    for (const name of order) {
      const spec = PLAN_OBJECTS[name];
      const list = listByRef()[spec.ref] || [];
      for (let i = list.length - 1; i >= 0; i--) {
        if (!isPickable(spec.sel, i)) continue;
        const o = list[i];
        if (spec.geometry === 'point') {
          const r = Math.max(6 / ds, (o.canopy || 1) / mpp);
          if (Math.hypot(im.x - o.x, im.y - o.y) <= r) return { name, spec, idx: i };
        } else if (spec.geometry === 'polyline') {
          const pts = o.pts || [];
          if (spec.under && pts.length >= 3) {
            // An AREA is hit anywhere inside it (even-odd ray cast), not just near its outline.
            let inside = false;
            for (let a = 0, b = pts.length - 1; a < pts.length; b = a++) {
              if ((pts[a].y > im.y) !== (pts[b].y > im.y)
                && im.x < ((pts[b].x - pts[a].x) * (im.y - pts[a].y)) / (pts[b].y - pts[a].y) + pts[a].x) inside = !inside;
            }
            if (inside) return { name, spec, idx: i };
          } else {
            const half = Math.max(6 / ds, ((o.width || 1) / mpp) / 2);
            for (let k = 0; k + 1 < pts.length; k++) {
              if (dist2seg(im.x, im.y, { x1: pts[k].x, y1: pts[k].y, x2: pts[k + 1].x, y2: pts[k + 1].y }) <= half) return { name, spec, idx: i };
            }
          }
        }
      }
    }
    return null;
  };
  // Nearest point on any wall to place an opening on — the projected centre + the wall's angle.
  const nearestWallHit = (im, reach) => {
    let best = (reach || 28) / ds; let hit = null;
    segsRef.current.forEach((s) => {
      const vx = s.x2 - s.x1; const vy = s.y2 - s.y1; const len2 = vx * vx + vy * vy || 1;
      let tt = ((im.x - s.x1) * vx + (im.y - s.y1) * vy) / len2; tt = Math.max(0, Math.min(1, tt));
      const cx = s.x1 + tt * vx; const cy = s.y1 + tt * vy; const dd = Math.hypot(im.x - cx, im.y - cy);
      if (dd < best) { best = dd; hit = { cx, cy, a: Math.atan2(vy, vx), len: Math.sqrt(len2) }; }
    });
    return hit;
  };
  // Re-seat an opening (door / window) on the wall nearest its centre, taking that wall's angle so
  // it lies flush and actually cuts the opening. A pasted or dragged opening is a free-floating
  // centre until this runs, which is why a copied door would not snap into a wall on its own. Left
  // untouched when no wall is within reach, so it can still be parked off-wall and nudged closer.
  const snapOpeningToWall = (o) => {
    const hit = nearestWallHit({ x: o.cx, y: o.cy }, 34);
    return hit ? { ...o, cx: hit.cx, cy: hit.cy, a: hit.a } : o;
  };

  const finishWall = useCallback(() => {
    const d = draftRef.current;
    if (d && d.pts && d.pts.length >= 2) { const add = []; for (let i = 0; i < d.pts.length - 1; i++) { const a = d.pts[i]; const b = d.pts[i + 1]; if (a.x !== b.x || a.y !== b.y) add.push({ x1: a.x, y1: a.y, x2: b.x, y2: b.y }); } if (add.length) commit({ segs: segsRef.current.concat(add) }); }
    draftRef.current = null; redraw();
  }, [commit, redraw]);

  // finishPoly commits a run of points as ONE object of a registry type. The wall tool turns its run
  // into many segments; a road, a hedge and a ground area are each a single object that OWNS its
  // run, which is why they finish through here instead.
  const finishPoly = useCallback((toolId) => {
    const d = draftRef.current;
    const name = Object.keys(PLAN_OBJECTS).find((k) => PLAN_OBJECTS[k].tool && PLAN_OBJECTS[k].tool.id === toolId);
    const spec = name ? PLAN_OBJECTS[name] : null;
    if (spec && d && d.pts && d.pts.length >= (spec.closed ? 3 : 2)) {
      const obj = { pts: d.pts.map((q) => ({ x: q.x, y: q.y })) };
      spec.fields.forEach((f) => { if (f.default !== undefined) obj[f.key] = f.default; });
      const list = listByRef()[spec.ref] || [];
      const at = list.length;
      commit({ [spec.ref]: list.concat([obj]) });
      setSelection(new Set([selKey(spec.sel, at)]));
    }
    draftRef.current = null; redraw();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [commit, redraw]);

  // xfObjects applies ONE transform to a snapshot of the selection and returns the new geometry,
  // keyed the same way. The drag preview and the commit both call this, so what you see while
  // dragging is by construction what gets saved.
  //
  // Each kind takes the transform in the way that makes sense for it: a wall's two endpoints are
  // mapped; an opening's centre is mapped, its angle picks up the rotation and its width the stretch
  // along its own axis; a footprint keeps its size in its own frame and gains the rotation; a camera
  // marker is mapped through OL's y-up frame and its heading turns with the plan.
  const xfObjects = (orig, xf) => {
    const out = { segs: new Map(), doors: new Map(), wins: new Map(), stairs: new Map(), parks: new Map(), plats: new Map(), cams: new Map(), extra: {} };
    // Registry-drawn types transform by their GEOMETRY: every point of a polyline is mapped, a
    // point's position is mapped. A declaration added later moves, rotates and scales for free.
    Object.keys(orig.extra || {}).forEach((refName) => {
      const name = Object.keys(PLAN_OBJECTS).find((k) => PLAN_OBJECTS[k].ref === refName);
      const spec = name ? PLAN_OBJECTS[name] : null;
      if (!spec) return;
      const m = new Map();
      orig.extra[refName].forEach((o, i) => {
        if (spec.geometry === 'polyline') m.set(i, { ...o, pts: (o.pts || []).map((q) => xfPoint(q, xf)) });
        else if (spec.geometry === 'point') { const c = xfPoint({ x: o.x, y: o.y }, xf); m.set(i, { ...o, x: c.x, y: c.y }); }
        else m.set(i, o);
      });
      out.extra[refName] = m;
    });
    const degs = (xf.ang * 180) / Math.PI;
    orig.segs.forEach((o, i) => {
      const a = xfPoint({ x: o.x1, y: o.y1 }, xf); const b = xfPoint({ x: o.x2, y: o.y2 }, xf);
      out.segs.set(i, { x1: a.x, y1: a.y, x2: b.x, y2: b.y });
    });
    const opening = (o) => {
      const c = xfPoint({ x: o.cx, y: o.cy }, xf);
      return { ...o, cx: c.x, cy: c.y, a: (o.a || 0) + xf.ang, w: Math.max(1, o.w * xfLengthAlong(o.a || 0, xf)) };
    };
    orig.doors.forEach((o, i) => out.doors.set(i, opening(o)));
    orig.wins.forEach((o, i) => out.wins.set(i, opening(o)));
    const footprint = (o) => {
      const c = xfPoint(rectCenter(o), xf);
      const s = rectSize(o);
      // Each side scales by how much the transform stretches THAT side's direction. When the frame
      // is aligned to the object (single-object resize) this is exactly the axis you grabbed; for a
      // group scale it is the correct projection of a screen-axis stretch onto the object's sides.
      const fw = xfLengthAlong(o.a || 0, xf);
      const fh = xfLengthAlong((o.a || 0) + Math.PI / 2, xf);
      const sized = { w: Math.max(1, s.w * fw), h: Math.max(1, s.h * fh) };
      return { ...o, ...rectFrom(c, sized), a: (o.a || 0) + xf.ang };
    };
    orig.stairs.forEach((o, i) => out.stairs.set(i, footprint(o)));
    orig.parks.forEach((o, i) => out.parks.set(i, footprint(o)));
    orig.plats.forEach((o, i) => out.plats.set(i, footprint(o)));
    orig.cams.forEach((o, id) => {
      // Markers are stored y-UP; the transform works in the image's y-DOWN frame.
      const p = xfPoint({ x: o.x, y: h - o.y }, xf);
      out.cams.set(id, { x: p.x, y: h - p.y, heading: (o.heading || 0) + degs });
    });
    return out;
  };

  // beginMove snapshots the CURRENT geometry of everything in `keys`, so the drag can preview and
  // then commit by offsetting the originals — never by accumulating deltas, which would drift.
  const beginMove = (keys, im) => {
    const orig = { segs: new Map(), doors: new Map(), wins: new Map(), stairs: new Map(), parks: new Map(), plats: new Map(), cams: new Map(), extra: {} };
    const byId = {};
    placements.forEach((p) => { byId[p.id] = p; });
    keys.forEach((k) => {
      const c = k.indexOf(':'); const t2 = k.slice(0, c); const i = Number(k.slice(c + 1));
      if (t2 === 'seg' && segsRef.current[i]) orig.segs.set(i, { ...segsRef.current[i] });
      else if (t2 === 'door' && doorsRef.current[i]) orig.doors.set(i, { ...doorsRef.current[i] });
      else if (t2 === 'win' && windowsRef.current[i]) orig.wins.set(i, { ...windowsRef.current[i] });
      else if (t2 === 'stair' && stairsRef.current[i]) orig.stairs.set(i, { ...stairsRef.current[i] });
      else if (t2 === 'park' && parkingRef.current[i]) orig.parks.set(i, { ...parkingRef.current[i] });
      else if (t2 === 'plat' && platsRef.current[i]) orig.plats.set(i, { ...platsRef.current[i] });
      else if (t2 === 'cam' && byId[i]) orig.cams.set(i, { x: byId[i].x, y: byId[i].y, heading: byId[i].heading || 0 });
      else {
        // Registry-drawn types, snapshotted into `extra` keyed by their ref. One bucket rather than
        // one field per type, so a declaration added later moves without an edit here.
        const name = Object.keys(PLAN_OBJECTS).find((k) => PLAN_OBJECTS[k].sel === t2);
        const spec = name ? PLAN_OBJECTS[name] : null;
        if (spec && !spec.builtin) {
          const o = (listByRef()[spec.ref] || [])[i];
          if (o) {
            if (!orig.extra[spec.ref]) orig.extra[spec.ref] = new Map();
            orig.extra[spec.ref].set(i, JSON.parse(JSON.stringify(o)));
          }
        }
      }
    });
    return orig;
  };

  // boundsOfPreview is selectionBounds over the DRAGGED geometry, so the frame travels with the
  // objects during a move instead of sitting where they used to be.
  // A lone marker, or a perfectly horizontal opening, has no extent on one axis. The frame is padded
  // out to something grabbable about its centre — otherwise the handles would stack on top of each
  // other and the thing could never be rotated.
  const padBounds = (b) => {
    if (!b) return null;
    const minSpan = 22 / ds;
    const out = { ...b };
    if (out.x2 - out.x1 < minSpan) { const c = (out.x1 + out.x2) / 2; out.x1 = c - minSpan / 2; out.x2 = c + minSpan / 2; }
    if (out.y2 - out.y1 < minSpan) { const c = (out.y1 + out.y2) / 2; out.y1 = c - minSpan / 2; out.y2 = c + minSpan / 2; }
    return out;
  };

  // ---- the selection frame -----------------------------------------------------------------
  //
  // When exactly one rotated footprint or opening is selected, the frame turns WITH it, so its
  // handles line up with the object's own sides and a "horizontal" pull lengthens the object rather
  // than shearing it. In every other case (a group, a wall, a marker, or an unrotated object) the
  // frame is the plain screen-aligned box. selFrame reads the live model; selFrameFor reads a drag
  // preview, so the frame travels with the objects while they move.
  const framePad = (fr) => {
    if (!fr) return null;
    const minSpan = 11 / ds;
    return frameOf(fr.cx, fr.cy, Math.max(fr.hw, minSpan), Math.max(fr.hh, minSpan), fr.a);
  };
  const rectFrame = (r) => { const c = rectCenter(r); const s = rectSize(r); return frameOf(c.x, c.y, s.w / 2, s.h / 2, r.a || 0); };
  // An opening is a thin thing along its own axis; give it a nominal depth so it stays grabbable.
  const openFrame = (d) => frameOf(d.cx, d.cy, d.w / 2, Math.max(6 / ds, d.w * 0.12), d.a || 0);

  // soleRotated returns the ONE selected geometry object's oriented frame, or null when the
  // selection is anything else — a group, a wall, a marker, or nothing. Walls and markers are
  // excluded on purpose: neither has an independent width and height to confuse, so the plain
  // screen-aligned box is the right frame for them.
  const soleRotated = (doors, wins, stairs, parks, plats, others) => {
    if (doors.length + wins.length + stairs.length + parks.length + plats.length + others !== 1) return null;
    if (stairs[0]) return rectFrame(stairs[0]);
    if (parks[0]) return rectFrame(parks[0]);
    if (plats[0]) return rectFrame(plats[0]);
    if (doors[0]) return openFrame(doors[0]);
    if (wins[0]) return openFrame(wins[0]);
    return null;
  };

  const selFrame = () => {
    const oriented = soleRotated(
      selOf('door').map((i) => doorsRef.current[i]).filter(Boolean),
      selOf('win').map((i) => windowsRef.current[i]).filter(Boolean),
      selOf('stair').map((i) => stairsRef.current[i]).filter(Boolean),
      selOf('park').map((i) => parkingRef.current[i]).filter(Boolean),
      selOf('plat').map((i) => platsRef.current[i]).filter(Boolean),
      selOf('seg').length + selOf('cam').length,
    );
    if (oriented) return framePad(oriented);
    const b = selectionBounds();
    return b ? frameFromBox(b) : null;
  };

  const selFrameFor = (pv) => {
    const oriented = soleRotated([...pv.doors.values()], [...pv.wins.values()], [...pv.stairs.values()], [...pv.parks.values()], [...pv.plats.values()], pv.segs.size + pv.cams.size);
    if (oriented) return framePad(oriented);
    const b = boundsOfPreview(pv);
    return b ? frameFromBox(b) : null;
  };

  const boundsOfPreview = (pv) => {
    const pts = [];
    pv.segs.forEach((s) => pts.push({ x: s.x1, y: s.y1 }, { x: s.x2, y: s.y2 }));
    pv.doors.forEach((d) => pts.push(...openingEnds(d)));
    pv.wins.forEach((d) => pts.push(...openingEnds(d)));
    pv.stairs.forEach((r) => pts.push(...rectCorners(r)));
    pv.parks.forEach((r) => pts.push(...rectCorners(r)));
    pv.plats.forEach((r) => pts.push(...rectCorners(r)));
    pv.cams.forEach((p) => pts.push({ x: p.x, y: h - p.y }));
    return padBounds(boundsOfPoints(pts));
  };

  // hitHandle reports which transform handle is under the pointer, or null, working in the frame's
  // own rotation. Handles are a fixed SCREEN size, so the grab radius is converted back into image
  // units — otherwise they would be impossible to hit when zoomed out.
  // hitCamHandle: which POV handle of the selected camera is under the pointer, or null.
  const hitCamHandle = (im) => {
    const p = povCam();
    if (!p) return null;
    const H = camHandles(p);
    const px = im.x * ds; const py = im.y * ds; const grab = HANDLE_PX + 3;
    if (Math.hypot(px - H.aim.x, py - H.aim.y) <= grab) return 'aim';
    if (Math.hypot(px - H.edgeL.x, py - H.edgeL.y) <= grab) return 'edgeL';
    if (Math.hypot(px - H.edgeR.x, py - H.edgeR.y) <= grab) return 'edgeR';
    return null;
  };

  const hitHandle = (im) => {
    // A single selected camera uses POV handles, not the transform frame.
    if (povCam()) return null;
    if (tool !== 'select' || !selection.size) return null;
    const fr = selFrame();
    if (!fr) return null;
    const grab = (HANDLE_PX + 4) / 2 / ds;
    const rk = rotateKnobWorld(fr, ROTATE_ARM_PX / ds);
    if (Math.hypot(im.x - rk.x, im.y - rk.y) <= grab + 2 / ds) return 'rot';
    for (const hd of HANDLES) {
      const hwp = handleWorld(hd.id, fr);
      if (Math.abs(im.x - hwp.x) <= grab && Math.abs(im.y - hwp.y) <= grab) return hd.id;
    }
    return null;
  };

  // The axis-aligned box around everything selected, in image space. It sees a rotated footprint's
  // real corners, not its stored ones, so a group frame encloses everything it should.
  const selectionBounds = () => {
    const pts = [];
    selOf('seg').forEach((i) => { const s = segsRef.current[i]; if (s) { pts.push({ x: s.x1, y: s.y1 }, { x: s.x2, y: s.y2 }); } });
    selOf('door').forEach((i) => { const d = doorsRef.current[i]; if (d) pts.push(...openingEnds(d)); });
    selOf('win').forEach((i) => { const d = windowsRef.current[i]; if (d) pts.push(...openingEnds(d)); });
    selOf('stair').forEach((i) => { const r = stairsRef.current[i]; if (r) pts.push(...rectCorners(r)); });
    selOf('park').forEach((i) => { const r = parkingRef.current[i]; if (r) pts.push(...rectCorners(r)); });
    selOf('plat').forEach((i) => { const r = platsRef.current[i]; if (r) pts.push(...rectCorners(r)); });
    const byId = {}; placements.forEach((p) => { byId[p.id] = p; });
    selOf('cam').forEach((id) => { const p = byId[id]; if (p) pts.push({ x: p.x, y: h - p.y }); });
    // Registry-drawn types contribute their bounds, computed from `geometry` alone.
    Object.keys(PLAN_OBJECTS).forEach((name) => {
      const spec = PLAN_OBJECTS[name];
      if (spec.builtin) return;
      selOf(spec.sel).forEach((i) => {
        const o = (listByRef()[spec.ref] || [])[i];
        const bb = o && boundsOfObject(name, o);
        if (bb) pts.push({ x: bb.x1, y: bb.y1 }, { x: bb.x2, y: bb.y2 });
      });
    });
    return padBounds(boundsOfPoints(pts));
  };

  // ---- copy / paste ----
  //
  // The clipboard holds plan GEOMETRY only. Camera and node markers are deliberately excluded: a
  // camera sits in exactly one physical place and holds exactly one pin (the exclusive-placement
  // rule), so pasting a copy of one would be asking the server for something it must refuse. Copying
  // a selection that includes markers copies the geometry and says so.
  // A snapshot of the selection's plan geometry (markers excluded — see above), and its count.
  const clipOfSelection = useCallback(() => {
    const clip = {
      segs: selOf('seg').map((i) => segsRef.current[i]).filter(Boolean).map((o) => ({ ...o })),
      doors: selOf('door').map((i) => doorsRef.current[i]).filter(Boolean).map((o) => ({ ...o })),
      windows: selOf('win').map((i) => windowsRef.current[i]).filter(Boolean).map((o) => ({ ...o })),
      stairs: selOf('stair').map((i) => stairsRef.current[i]).filter(Boolean).map((o) => ({ ...o })),
      parking: selOf('park').map((i) => parkingRef.current[i]).filter(Boolean).map((o) => ({ ...o })),
      platforms: selOf('plat').map((i) => platsRef.current[i]).filter(Boolean).map((o) => ({ ...o })),
    };
    clip.n = clip.segs.length + clip.doors.length + clip.windows.length + clip.stairs.length + clip.parking.length + clip.platforms.length;
    return clip;
  }, [selOf]);

  const copySelection = useCallback(() => {
    if (!selection.size) return;
    const clip = clipOfSelection();
    if (!clip.n) { if (onToast) onToast(t('fed.copyMarkersOnly'), 'info'); return; }
    clipRef.current = clip;
    pasteRunRef.current = 0; // the next paste starts its offset ladder afresh
    if (onToast) onToast(t('fed.copied', { n: clip.n }), 'success');
  }, [selection, clipOfSelection, onToast, t]);

  // Cut = copy the geometry, then remove exactly what was copied. Markers are left in place: they
  // cannot go on the clipboard, so cutting one would delete it with no way to paste it back.
  const cutSelection = useCallback(() => {
    if (!selection.size) return;
    const clip = clipOfSelection();
    if (!clip.n) { if (onToast) onToast(t('fed.copyMarkersOnly'), 'info'); return; }
    clipRef.current = clip;
    pasteRunRef.current = 0;
    const segs = new Set(selOf('seg')); const doors = new Set(selOf('door')); const wins = new Set(selOf('win'));
    const stairs = new Set(selOf('stair')); const parks = new Set(selOf('park')); const plats = new Set(selOf('plat'));
    const patch = {};
    // Registry-drawn types delete through their own ref, in the same single commit.
    Object.keys(PLAN_OBJECTS).forEach((name) => {
      const spec = PLAN_OBJECTS[name];
      if (spec.builtin) return;
      const gone = new Set(selOf(spec.sel));
      if (gone.size) patch[spec.ref] = (listByRef()[spec.ref] || []).filter((_, i) => !gone.has(i));
    });
    if (segs.size) patch.segs = segsRef.current.filter((_, i) => !segs.has(i));
    if (doors.size) patch.doors = doorsRef.current.filter((_, i) => !doors.has(i));
    if (wins.size) patch.windows = windowsRef.current.filter((_, i) => !wins.has(i));
    if (stairs.size) patch.stairs = stairsRef.current.filter((_, i) => !stairs.has(i));
    if (parks.size) patch.parking = parkingRef.current.filter((_, i) => !parks.has(i));
    if (plats.size) patch.platforms = platsRef.current.filter((_, i) => !plats.has(i));
    commit(patch);
    setSelection(new Set());
    if (onToast) onToast(t('fed.cut', { n: clip.n }), 'success');
  }, [selection, clipOfSelection, selOf, commit, onToast, t]);

  const pasteClipboard = useCallback(() => {
    const clip = clipRef.current;
    if (!clip) return;
    // Each paste of the same clipboard steps further down-right, so pasting three times gives three
    // visible copies rather than one stack you cannot tell apart.
    pasteRunRef.current += 1;
    const off = unit * pasteRunRef.current;
    const shiftRect = (o) => ({ ...o, x1: o.x1 + off, y1: o.y1 + off, x2: o.x2 + off, y2: o.y2 + off });
    const patch = {};
    const picked = new Set();
    if (clip.segs.length) {
      const base = segsRef.current.length;
      patch.segs = segsRef.current.concat(clip.segs.map((o) => ({ x1: o.x1 + off, y1: o.y1 + off, x2: o.x2 + off, y2: o.y2 + off })));
      clip.segs.forEach((_, k) => picked.add(selKey('seg', base + k)));
    }
    if (clip.doors.length) {
      const base = doorsRef.current.length;
      // Openings re-seat onto a nearby wall on paste, so a copied door drops flush into a wall when
      // one is in reach rather than landing off it (it can still be dragged wall-to-wall after).
      patch.doors = doorsRef.current.concat(clip.doors.map((o) => snapOpeningToWall({ ...o, cx: o.cx + off, cy: o.cy + off })));
      clip.doors.forEach((_, k) => picked.add(selKey('door', base + k)));
    }
    if (clip.windows.length) {
      const base = windowsRef.current.length;
      patch.windows = windowsRef.current.concat(clip.windows.map((o) => snapOpeningToWall({ ...o, cx: o.cx + off, cy: o.cy + off })));
      clip.windows.forEach((_, k) => picked.add(selKey('win', base + k)));
    }
    if (clip.stairs.length) {
      const base = stairsRef.current.length;
      patch.stairs = stairsRef.current.concat(clip.stairs.map(shiftRect));
      clip.stairs.forEach((_, k) => picked.add(selKey('stair', base + k)));
    }
    if (clip.parking.length) {
      const base = parkingRef.current.length;
      patch.parking = parkingRef.current.concat(clip.parking.map(shiftRect));
      clip.parking.forEach((_, k) => picked.add(selKey('park', base + k)));
    }
    if (clip.platforms && clip.platforms.length) {
      const base = platsRef.current.length;
      patch.platforms = platsRef.current.concat(clip.platforms.map(shiftRect));
      clip.platforms.forEach((_, k) => picked.add(selKey('plat', base + k)));
    }
    if (!Object.keys(patch).length) return;
    commit(patch);
    // The pasted copies become the selection, so they can be dragged straight into place.
    setSelection(picked);
  }, [commit, unit]);

  // deleteSelection removes everything held, in ONE history step. Plan geometry goes through a
  // single commit (so undo brings it all back together); markers are the parent's records, removed
  // through its callback.
  const deleteSelection = useCallback(() => {
    if (!selection.size) return;
    const segs = new Set(selOf('seg')); const doors = new Set(selOf('door')); const wins = new Set(selOf('win'));
    const stairs = new Set(selOf('stair')); const parks = new Set(selOf('park')); const plats = new Set(selOf('plat')); const cams = selOf('cam');
    const patch = {};
    if (segs.size) patch.segs = segsRef.current.filter((_, i) => !segs.has(i));
    if (doors.size) patch.doors = doorsRef.current.filter((_, i) => !doors.has(i));
    if (wins.size) patch.windows = windowsRef.current.filter((_, i) => !wins.has(i));
    if (stairs.size) patch.stairs = stairsRef.current.filter((_, i) => !stairs.has(i));
    if (parks.size) patch.parking = parkingRef.current.filter((_, i) => !parks.has(i));
    if (plats.size) patch.platforms = platsRef.current.filter((_, i) => !plats.has(i));
    if (Object.keys(patch).length) commit(patch);
    cams.forEach((id) => onRemove(id));
    setSelection(new Set());
  }, [selection, selOf, commit, onRemove]);

  // commitMove turns a finished transform - a drag, or a modal G/R/S - into the model. Shared, so
  // the keyboard path cannot drift from the mouse path: same preview, same geometry, same undo step.
  const commitMove = (d) => {
    if (!d || !d.moved) { redraw(); return; }
    // Exactly the geometry the preview was drawing - same function, same transform.
    const next = xfObjects(d.orig, d.xf);
    // Plan geometry lands in one commit (one undo step for the whole gesture); markers persist
    // through the parent, one call each.
    const patch = {};
    if (next.segs.size) patch.segs = segsRef.current.map((s, i) => next.segs.get(i) || s);
    // Openings re-seat onto the nearest wall on drop, so a dragged (or pasted-then-dragged) door
    // or window locks flush into a wall instead of floating where it was released.
    if (next.doors.size) patch.doors = doorsRef.current.map((x, i) => { const nd = next.doors.get(i); return nd ? snapOpeningToWall(nd) : x; });
    if (next.wins.size) patch.windows = windowsRef.current.map((x, i) => { const nw = next.wins.get(i); return nw ? snapOpeningToWall(nw) : x; });
    // A dragged stair locks fully onto the raised floor it was dropped on, so it rests on the slab
    // rather than half-overhanging it.
    if (next.stairs.size) patch.stairs = stairsRef.current.map((x, i) => { const ns = next.stairs.get(i); return ns ? snapStairToPlatform(ns) : x; });
    if (next.parks.size) patch.parking = parkingRef.current.map((x, i) => next.parks.get(i) || x);
    if (next.plats.size) patch.platforms = platsRef.current.map((x, i) => next.plats.get(i) || x);
    // Registry-drawn types write back through their own ref, so the same commit covers a road
    // moved with a wall in one selection - one undo step for the whole gesture.
    Object.keys(next.extra || {}).forEach((refName) => {
      const m = next.extra[refName];
      if (m && m.size) patch[refName] = (listByRef()[refName] || []).map((x, i) => m.get(i) || x);
    });
    if (Object.keys(patch).length) commit(patch);
    next.cams.forEach((o, id) => {
      onMove(id, Math.max(0, Math.min(w, o.x)), Math.max(0, Math.min(h, o.y)));
      // A rotation turns what a camera is pointing at, so its heading turns with it.
      if (d.xf.ang) onAim(id, { heading: ((o.heading % 360) + 360) % 360 });
    });
    redraw();
  };

  // ---- modal transforms (G / R / S) --------------------------------------------------------------
  //
  // Press G, move the mouse, the selection follows; type a number for an exact value; X or Y locks
  // an axis; Enter or a click confirms, Esc puts it back. R rotates, S scales.
  //
  // They are ACCELERATORS, never the only way: every one of these is still a drag on a handle, and
  // the status bar says so. That is the whole compromise with Blender's keymap - take the speed,
  // refuse the modality that makes it hostile to someone who has never used it.
  //
  // They reuse moveRef, so the live preview, the selection frame and the commit path are the drag's
  // - there is no second implementation to drift.
  const startModal = useCallback((kind2) => {
    if (!selection.size) return;
    const fr = selFrame();
    if (!fr) return;
    const cur = cursorRef.current || { x: fr.cx, y: fr.cy };
    moveRef.current = {
      mode: kind2, modal: true, handle: 'se', frame: fr,
      anchor: { x: fr.cx, y: fr.cy },
      sx: cur.x, sy: cur.y, moved: false,
      xf: { ...IDENTITY_XF },
      orig: beginMove(selection, cur),
      axis: null, typed: '',
    };
    redraw();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [selection, redraw]);

  // applyModal recomputes the transform from either the typed number (exact) or the pointer.
  const applyModal = useCallback((im) => {
    const d = moveRef.current;
    if (!d || !d.modal) return;
    const typed = parseFloat(d.typed);
    const hasTyped = d.typed !== '' && Number.isFinite(typed);
    if (d.mode === 'move') {
      let tx; let ty;
      if (hasTyped) {
        // A typed distance is in METRES when the plan has a scale - that is the whole point of
        // being able to type it. With no scale it can only mean pixels, and the status bar's
        // readout says which you are getting.
        const px = scaleRef.current > 0 ? typed / scaleRef.current : typed;
        tx = d.axis === 'y' ? 0 : px;
        ty = d.axis === 'x' ? 0 : (d.axis === 'y' ? px : 0);
      } else if (im) {
        tx = Math.round((im.x - d.sx) / snap) * snap;
        ty = Math.round((im.y - d.sy) / snap) * snap;
        if (d.axis === 'x') ty = 0; else if (d.axis === 'y') tx = 0;
      } else return;
      d.xf = { ...IDENTITY_XF, tx, ty };
      d.moved = !!(tx || ty);
    } else if (d.mode === 'rotate') {
      let ang;
      if (hasTyped) ang = (typed * Math.PI) / 180;
      else if (im) ang = Math.atan2(im.y - d.anchor.y, im.x - d.anchor.x) - Math.atan2(d.sy - d.anchor.y, d.sx - d.anchor.x);
      else return;
      d.xf = { ...IDENTITY_XF, px: d.anchor.x, py: d.anchor.y, ang };
      d.moved = !!ang;
    } else {
      let f;
      if (hasTyped) f = typed;
      else if (im) {
        const d0 = Math.hypot(d.sx - d.anchor.x, d.sy - d.anchor.y) || 1;
        f = Math.hypot(im.x - d.anchor.x, im.y - d.anchor.y) / d0;
      } else return;
      if (!(f > 0.01)) f = 0.01;
      d.xf = { ...IDENTITY_XF, px: d.anchor.x, py: d.anchor.y, sx: d.axis === 'y' ? 1 : f, sy: d.axis === 'x' ? 1 : f, fa: d.frame ? d.frame.a || 0 : 0 };
      d.moved = f !== 1;
    }
    redraw();
  }, [snap, redraw]);

  const cancelModal = useCallback(() => { moveRef.current = null; redraw(); }, [redraw]);
  const confirmModal = useCallback(() => { const d = moveRef.current; moveRef.current = null; commitMove(d);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [redraw]);

  const onPointerDown = (e) => {
    // Middle-mouse, or space held, pans the viewport whatever tool is active. This is the gesture
    // the scrollbar model could not offer at all, and it has to work from inside every tool - you
    // pan to see where you are drawing, without putting the pencil down.
    if (e.button === 1 || spaceRef.current) {
      e.preventDefault();
      panRef.current = { x: e.clientX, y: e.clientY, tx: viewRef.current.tx, ty: viewRef.current.ty };
      touchedRef.current = true;
      try { canvasRef.current.setPointerCapture(e.pointerId); } catch (_) { /* ignore */ }
      redraw();
      return;
    }
    // A modal transform is confirmed by a click, the way it is in every tool that has them.
    if (moveRef.current && moveRef.current.modal) { e.preventDefault(); confirmModal(); return; }
    if (e.button !== 0) return; e.preventDefault();
    const im = evImg(e);
    // The set-scale tool measures a run the operator already knows the length of, then asks for it.
    // Everything in metres downstream - every numeric field P3 adds, the 3D view, coverage - is
    // dead until a plan has one, and until now there was no way to give it one but a spinner
    // labelled "each cell".
    if (tool === 'setscale') {
      const p = snapPt(im.x, im.y);
      const d = draftRef.current;
      if (!d || !d.scaleFrom) { draftRef.current = { scaleFrom: p }; cursorRef.current = p; redraw(); return; }
      finishSetScale(d.scaleFrom, p);
      return;
    }
    if (tool === 'erase') {
      const di = hitDoor(im); if (di >= 0) { commit({ doors: doorsRef.current.filter((_, k) => k !== di) }); return; }
      const wi = hitWindow(im); if (wi >= 0) { commit({ windows: windowsRef.current.filter((_, k) => k !== wi) }); return; }
      const i = nearestSeg(im); if (i >= 0) { commit({ segs: segsRef.current.filter((_, k) => k !== i) }); return; }
      const rg = hitRegistry(im);
      if (rg) { const list = listByRef()[rg.spec.ref] || []; commit({ [rg.spec.ref]: list.filter((_, k) => k !== rg.idx) }); return; }
      const si = hitStair(im); if (si >= 0) { commit({ stairs: stairsRef.current.filter((_, k) => k !== si) }); return; }
      const pi = hitParking(im); if (pi >= 0) { commit({ parking: parkingRef.current.filter((_, k) => k !== pi) }); return; }
      const li = hitPlatform(im); if (li >= 0) commit({ platforms: platsRef.current.filter((_, k) => k !== li) }); return;
    }
    // Door/gate and window are both openings cut into a wall: click the wall, get an opening of a
    // sensible real-world width, capped so it can never be wider than the wall it sits in.
    if (tool === 'door' || tool === 'window') {
      const hit = nearestWallHit(im);
      if (!hit) { if (onToast) onToast(t(tool === 'window' ? 'grid.windowNeedsWall' : (kind === KIND_OUTDOOR ? 'grid.gateNeedsFence' : 'grid.doorNeedsWall')), 'info'); return; }
      const defM = tool === 'window' ? 1.2 : 0.9; // a window is a touch wider than a single door leaf
      const defW = scaleRef.current > 0 ? defM / scaleRef.current : unit * (tool === 'window' ? 2 : 1.5);
      const dw = Math.max(unit, Math.min(defW, hit.len * 0.9));
      if (tool === 'window') {
        const nw = windowsRef.current.length;
        commit({ windows: windowsRef.current.concat([{ cx: hit.cx, cy: hit.cy, w: dw, a: hit.a, sill: DEF_SILL, head: DEF_HEAD }]) });
        setSelection(new Set([selKey('win', nw)]));
      } else {
        const nd = doorsRef.current.length;
        commit({ doors: doorsRef.current.concat([{ cx: hit.cx, cy: hit.cy, w: dw, a: hit.a }]) });
        setSelection(new Set([selKey('door', nd)]));
      }
      return;
    }
    // A tree is a single click: a point with the defaults its declaration carries.
    if (tool === 'tree') {
      const p = snapPt(im.x, im.y);
      const spec = PLAN_OBJECTS.tree;
      const obj = { x: p.x, y: p.y };
      spec.fields.forEach((f) => { if (f.default !== undefined) obj[f.key] = f.default; });
      const list = treesRef.current;
      commit({ trees: list.concat([obj]) });
      setSelection(new Set([selKey('tree', list.length)]));
      return;
    }
    // Roads, hedges and ground areas are RUNS, drawn exactly like a wall: click corners, then
    // double-click or Enter. Reusing the gesture means there is no new interaction to learn.
    if (POLY_TOOLS.has(tool)) {
      const p = snapPt(im.x, im.y);
      if (!draftRef.current || !draftRef.current.pts) draftRef.current = { pts: [p], poly: tool };
      else { const pts = draftRef.current.pts; const last = pts[pts.length - 1]; if (last.x !== p.x || last.y !== p.y) pts.push(p); }
      cursorRef.current = p; redraw(); return;
    }
    if (tool === 'wall') { const p = snapPt(im.x, im.y); if (!draftRef.current || !draftRef.current.pts) draftRef.current = { pts: [p] }; else { const pts = draftRef.current.pts; const last = pts[pts.length - 1]; if (last.x !== p.x || last.y !== p.y) pts.push(p); } cursorRef.current = p; redraw(); return; }
    if (BOX_TOOLS.has(tool)) { const p = snapPt(im.x, im.y); draftRef.current = { start: p, cur: p, round: tool === 'round', stairs: tool === 'stairs', parking: tool === 'parking', platform: tool === 'platform' }; cursorRef.current = p; try { canvasRef.current.setPointerCapture(e.pointerId); } catch (_) { /* ignore */ } redraw(); return; }
    // select tool
    const ol = evOL(e);
    if (placingRef.current) { onPlace(placingRef.current, ol.x, ol.y); if (onClearPlacing) onClearPlacing(); return; }
    const cap = () => { try { canvasRef.current.setPointerCapture(e.pointerId); } catch (_) { /* ignore */ } };

    // Camera POV handles come first: aim and field-of-view are adjusted right on the wedge.
    const ch = hitCamHandle(im);
    if (ch) { povRef.current = { id: selCam, mode: ch }; cap(); redraw(); return; }

    // Transform handles sit ON TOP of the selection, so they are tested before anything under them
    // — otherwise the object being resized would swallow its own handle.
    const gh = hitHandle(im);
    if (gh) {
      const fr = selFrame();
      moveRef.current = {
        mode: gh === 'rot' ? 'rotate' : 'scale',
        handle: gh,
        frame: fr,
        anchor: { x: fr.cx, y: fr.cy }, // the rotate pivot; resize derives its own anchor
        sx: im.x, sy: im.y, moved: false,
        xf: { ...IDENTITY_XF },
        orig: beginMove(selection, im),
      };
      cap(); redraw(); return;
    }

    // What did the pointer land on? Small targets first: a marker, then openings (which sit ON a
    // wall), then the wall itself, then filled footprints — so a wall drawn across a stair or a
    // parking row stays reachable.
    const hit = hitMarker(ol);
    let target = null;
    // Registry-drawn types join the same precedence chain. Resolved up front so the built-in
    // branch below can fall through to it rather than duplicating the ordering.
    const rgHit = hit ? null : hitRegistry(im);
    if (hit) target = ['cam', hit.id];
    else {
      const dOnWall = hitDoor(im);
      const wOnWall = dOnWall >= 0 ? -1 : hitWindow(im);
      const segIdx = dOnWall >= 0 || wOnWall >= 0 ? -1 : nearestSeg(im);
      const si = dOnWall >= 0 || wOnWall >= 0 || segIdx >= 0 ? -1 : hitStair(im);
      const pi = dOnWall >= 0 || wOnWall >= 0 || segIdx >= 0 || si >= 0 ? -1 : hitParking(im);
      const li = dOnWall >= 0 || wOnWall >= 0 || segIdx >= 0 || si >= 0 || pi >= 0 ? -1 : hitPlatform(im);
      if (dOnWall >= 0) target = ['door', dOnWall];
      else if (wOnWall >= 0) target = ['win', wOnWall];
      else if (segIdx >= 0) target = ['seg', segIdx];
      else if (si >= 0) target = ['stair', si];
      else if (pi >= 0) target = ['park', pi];
      else if (li >= 0) target = ['plat', li];
      else if (rgHit) target = [rgHit.spec.sel, rgHit.idx];
    }

    if (target) {
      const [tk, ti] = target;
      // Pressing on something ALREADY selected keeps the whole selection and starts moving it —
      // that is what makes "select several, then drag them together" work. Shift toggles instead.
      const already = isSel(tk, ti);
      let next;
      if (e.shiftKey) { next = new Set(selection); if (already) next.delete(selKey(tk, ti)); else next.add(selKey(tk, ti)); }
      else next = already ? selection : new Set([selKey(tk, ti)]);
      setSelection(next);
      if (!e.shiftKey) {
        moveRef.current = { mode: 'move', sx: im.x, sy: im.y, moved: false, xf: { ...IDENTITY_XF }, orig: beginMove(next, im) };
        cap();
      }
      redraw(); return;
    }

    // empty space → rubber-band select (shift keeps what is already held)
    if (!e.shiftKey) setSelection(new Set());
    marqueeRef.current = { x1: im.x, y1: im.y, x2: im.x, y2: im.y, add: e.shiftKey };
    cap(); redraw();
  };
  const onPointerMove = (e) => {
    if (panRef.current) {
      const pn = panRef.current;
      setView((v) => ({ ...v, tx: pn.tx + (e.clientX - pn.x), ty: pn.ty + (e.clientY - pn.y) }));
      return;
    }
    // Ctrl inverts the snap setting for as long as it is held; the readout has to follow, so a
    // redraw is owed whenever it changes under the pointer.
    if (snapInvertRef.current !== (e.ctrlKey || e.metaKey)) snapInvertRef.current = e.ctrlKey || e.metaKey;
    // The readout follows the pointer under EVERY tool, so it is updated before any of the
    // tool-specific branches below get a chance to return early.
    //
    // Under Select nothing else asks for a repaint, so the readout would sit stale without one -
    // but a repaint per mouse move is wasteful. Tick only when the DISPLAYED value would actually
    // change, which is at the precision the status bar prints.
    const prevHover = hoverPosRef.current;
    hoverPosRef.current = evImg(e);
    if (!moveRef.current && !draftRef.current) {
      const q = scaleRef.current > 0 ? 0.01 / scaleRef.current : 1; // one printed step, in image units
      if (!prevHover
        || Math.round(prevHover.x / q) !== Math.round(hoverPosRef.current.x / q)
        || Math.round(prevHover.y / q) !== Math.round(hoverPosRef.current.y / q)) redraw();
    }
    // A modal transform follows the pointer with NO button held - that is what makes it modal.
    if (moveRef.current && moveRef.current.modal) { const p = hoverPosRef.current; cursorRef.current = p; applyModal(p); return; }
    const im = evImg(e);
    if (povRef.current) {
      // Aiming/widening a camera on the canvas. The heading is the bearing from the camera to the
      // pointer; the field of view is twice the angle between the dragged edge and the heading, so
      // the wedge opens symmetrically. Shift snaps the heading to 5°. onAim updates live (the parent
      // re-renders the wedge) and debounces the save, exactly like the sliders.
      const p = placements.find((x) => x.id === povRef.current.id);
      if (p) {
        const deg = headingTo(p, im);
        if (povRef.current.mode === 'aim') {
          const hd = e.shiftKey ? Math.round(deg / 5) * 5 : Math.round(deg);
          onAim(p.id, { heading: (hd + 360) % 360 });
        } else {
          let off = deg - (p.heading || 0);
          off = ((off % 360) + 540) % 360 - 180; // shortest signed angle to the heading
          onAim(p.id, { fov: Math.max(10, Math.min(170, Math.round(2 * Math.abs(off)))) });
        }
      }
      return;
    }
    if (moveRef.current) {
      const d = moveRef.current;
      if (d.mode === 'move') {
        // Translation snaps to the grid, as it always has, so walls stay on it.
        const tx = Math.round((im.x - d.sx) / snap) * snap;
        const ty = Math.round((im.y - d.sy) / snap) * snap;
        d.xf = { ...IDENTITY_XF, tx, ty };
        if (tx || ty) d.moved = true;
      } else if (d.mode === 'rotate') {
        // Angle from the selection centre: where the pointer started vs where it is now. Shift
        // snaps to 15°, the usual way to get a clean right angle without fighting the mouse.
        const a0 = Math.atan2(d.sy - d.anchor.y, d.sx - d.anchor.x);
        const a1 = Math.atan2(im.y - d.anchor.y, im.x - d.anchor.x);
        let ang = a1 - a0;
        if (e.shiftKey) { const step = Math.PI / 12; ang = Math.round(ang / step) * step; }
        d.xf = { ...IDENTITY_XF, px: d.anchor.x, py: d.anchor.y, ang };
        if (ang) d.moved = true;
      } else {
        // Resize IN THE FRAME. A side handle changes its own axis only, a corner changes both, and
        // the opposite edge stays pinned. Because the frame turns with a rotated object, its axes
        // are the object's own — so a "horizontal" pull lengthens the object, never shears it. See
        // resizeFactors, which owns the rule and is tested against it (including the rotated case).
        const f = resizeFactors(d.handle, d.frame, im, e.shiftKey, MIN_SPAN);
        d.xf = { ...IDENTITY_XF, px: f.anchor.x, py: f.anchor.y, sx: f.sx, sy: f.sy, fa: f.fa };
        if (f.sx !== 1 || f.sy !== 1) d.moved = true;
      }
      redraw(); return;
    }
    if (marqueeRef.current) { marqueeRef.current.x2 = im.x; marqueeRef.current.y2 = im.y; redraw(); return; }
    if (tool === 'erase') {
      // Same precedence as the click above, so what highlights is what will actually be erased.
      const di = hitDoor(im); hoverDoorRef.current = di;
      const wi = di >= 0 ? -1 : hitWindow(im); hoverWinRef.current = wi;
      const taken = di >= 0 || wi >= 0;
      hoverRef.current = taken ? -1 : nearestSeg(im);
      const taken2 = taken || hoverRef.current >= 0;
      hoverStairRef.current = taken2 ? -1 : hitStair(im);
      hoverParkRef.current = taken2 || hoverStairRef.current >= 0 ? -1 : hitParking(im);
      hoverPlatRef.current = taken2 || hoverStairRef.current >= 0 || hoverParkRef.current >= 0 ? -1 : hitPlatform(im);
      const anyBuiltin = taken2 || hoverStairRef.current >= 0 || hoverParkRef.current >= 0 || hoverPlatRef.current >= 0;
      const rg = anyBuiltin ? null : hitRegistry(im);
      hoverExtraRef.current = rg ? { ref: rg.spec.ref, idx: rg.idx } : { ref: null, idx: -1 };
      cursorRef.current = null; redraw(); return;
    }
    if (DRAG_TOOLS.has(tool) || tool === 'door' || tool === 'window' || tool === 'setscale') { const p = snapPt(im.x, im.y); cursorRef.current = p; const d = draftRef.current; if (d && d.start) d.cur = p; redraw(); }
  };
  const onPointerUp = (e) => {
    // A modal transform owns the pointer until it is confirmed or cancelled; the button coming back
    // up is not the end of it.
    if (moveRef.current && moveRef.current.modal) return;
    if (panRef.current) { panRef.current = null; try { canvasRef.current.releasePointerCapture(e.pointerId); } catch (_) { /* ignore */ } redraw(); return; }
    if (povRef.current) { povRef.current = null; redraw(); return; } // the save is already debounced by onAim
    if (moveRef.current) {
      const d = moveRef.current; moveRef.current = null;
      commitMove(d); return;
    }
    if (marqueeRef.current) {
      const mq = marqueeRef.current; marqueeRef.current = null;
      const rx = Math.min(mq.x1, mq.x2); const ry = Math.min(mq.y1, mq.y2); const rX = Math.max(mq.x1, mq.x2); const rY = Math.max(mq.y1, mq.y2);
      const inBox = (x, y) => x >= rx && x <= rX && y >= ry && y <= rY;
      // A footprint counts as swept when the band OVERLAPS it, not only when its centre is inside —
      // otherwise dragging a band across a large car park would miss the thing you drew it over.
      const overlaps = (r) => Math.min(r.x1, r.x2) <= rX && Math.max(r.x1, r.x2) >= rx && Math.min(r.y1, r.y2) <= rY && Math.max(r.y1, r.y2) >= ry;
      const found = [];
      // A band must not sweep up what a click could not reach - otherwise hiding or locking a row
      // stops it being clickable and leaves it selectable, which is worse than neither.
      segsRef.current.forEach((s, i) => { if (isPickable('seg', i) && (inBox(s.x1, s.y1) || inBox(s.x2, s.y2) || inBox((s.x1 + s.x2) / 2, (s.y1 + s.y2) / 2))) found.push(selKey('seg', i)); });
      doorsRef.current.forEach((d, i) => { if (isPickable('door', i) && inBox(d.cx, d.cy)) found.push(selKey('door', i)); });
      windowsRef.current.forEach((d, i) => { if (isPickable('win', i) && inBox(d.cx, d.cy)) found.push(selKey('win', i)); });
      stairsRef.current.forEach((s, i) => { if (isPickable('stair', i) && overlaps(s)) found.push(selKey('stair', i)); });
      parkingRef.current.forEach((p, i) => { if (isPickable('park', i) && overlaps(p)) found.push(selKey('park', i)); });
      platsRef.current.forEach((p, i) => { if (isPickable('plat', i) && overlaps(p)) found.push(selKey('plat', i)); });
      // Registry-drawn types are swept by their BOUNDS, which the registry can compute for any
      // geometry - so a type added later joins the band with no edit here.
      Object.keys(PLAN_OBJECTS).forEach((name) => {
        const spec = PLAN_OBJECTS[name];
        if (spec.builtin) return;
        (listByRef()[spec.ref] || []).forEach((o, i) => {
          if (!isPickable(spec.sel, i)) return;
          const bb = boundsOfObject(name, o);
          if (bb && bb.x1 <= rX && bb.x2 >= rx && bb.y1 <= rY && bb.y2 >= ry) found.push(selKey(spec.sel, i));
        });
      });
      // Markers are OL space (y up); the band is image space.
      placements.forEach((p) => { if (isPickable('cam', p.id) && inBox(p.x, h - p.y)) found.push(selKey('cam', p.id)); });
      setSelection((prev) => { const ns = mq.add ? new Set(prev) : new Set(); found.forEach((k) => ns.add(k)); return ns; });
      redraw(); return;
    }
    if (BOX_TOOLS.has(tool)) {
      const d = draftRef.current;
      if (d && d.start) {
        const a = d.start; const b = snapPt(evImg(e).x, evImg(e).y);
        if (d.parking) {
          if (Math.abs(b.x - a.x) > unit && Math.abs(b.y - a.y) > unit) {
            const r = normRect(a, b);
            // Seed the bay count from the row's length at a nominal 2.5 m stall, so a drag over a
            // real car park lands near the truth instead of at 1.
            const lenPx = baysAcrossX(r) ? r.x2 - r.x1 : r.y2 - r.y1;
            const bays = scaleRef.current > 0 ? Math.max(1, Math.min(60, Math.round((lenPx * scaleRef.current) / 2.5))) : 4;
            const newIdx = parkingRef.current.length;
            commit({ parking: parkingRef.current.concat([{ ...r, bays }]) });
            setSelection(new Set([selKey('park', newIdx)]));
          }
        } else if (d.stairs) {
          if (Math.abs(b.x - a.x) > unit && Math.abs(b.y - a.y) > unit) { const st = normStair(a, b, defaultStairDir(a, b)); const newIdx = stairsRef.current.length; commit({ stairs: stairsRef.current.concat([st]) }); setSelection(new Set([selKey('stair', newIdx)])); }
        } else if (d.platform) {
          if (Math.abs(b.x - a.x) > unit && Math.abs(b.y - a.y) > unit) { const r = normRect(a, b); const newIdx = platsRef.current.length; commit({ platforms: platsRef.current.concat([{ ...r, a: 0, rise: DEF_RISE }]) }); setSelection(new Set([selKey('plat', newIdx)])); }
        } else if (d.round) {
          const cx = (a.x + b.x) / 2; const cy = (a.y + b.y) / 2; const rx = Math.abs(b.x - a.x) / 2; const ry = Math.abs(b.y - a.y) / 2;
          if (rx > 1 && ry > 1) commit({ segs: segsRef.current.concat(roundSegs(cx, cy, rx, ry, unit)) });
        } else {
          const x1 = Math.min(a.x, b.x); const y1 = Math.min(a.y, b.y); const x2 = Math.max(a.x, b.x); const y2 = Math.max(a.y, b.y);
          if (x2 - x1 > 1 && y2 - y1 > 1) commit({ segs: segsRef.current.concat([{ x1, y1, x2, y2: y1 }, { x1: x2, y1, x2, y2 }, { x1: x2, y1: y2, x2: x1, y2 }, { x1, y1: y2, x2: x1, y2: y1 }]) });
        }
      }
      draftRef.current = null; redraw();
    }
  };
  const onDoubleClick = (e) => {
    if (tool === 'wall') { e.preventDefault(); finishWall(); }
    else if (POLY_TOOLS.has(tool)) { e.preventDefault(); finishPoly(tool); }
  };
  const onDrop = (e) => { e.preventDefault(); const raw = e.dataTransfer.getData('text/placement'); if (!raw) return; let payload; try { payload = JSON.parse(raw); } catch (_) { return; } const ol = evOL(e); onPlace(payload, ol.x, ol.y); if (onClearPlacing) onClearPlacing(); };

  useEffect(() => {
    const onKey = (e) => {
      if (e.target && (e.target.tagName === 'INPUT' || e.target.tagName === 'TEXTAREA')) return;
      if (mode !== '2d') return;
      // A modal transform swallows the keyboard while it runs: digits build an exact value, X and Y
      // lock an axis, Enter confirms, Esc puts everything back. Handled FIRST so a stray Home or a
      // tool shortcut cannot fire underneath a transform in progress.
      const md = moveRef.current;
      if (md && md.modal) {
        if (e.key === 'Escape') { e.preventDefault(); e.stopImmediatePropagation(); cancelModal(); return; }
        if (e.key === 'Enter' || e.key === 'Tab') { e.preventDefault(); confirmModal(); return; }
        if (e.key === 'x' || e.key === 'X') { e.preventDefault(); md.axis = md.axis === 'x' ? null : 'x'; applyModal(cursorRef.current); return; }
        if (e.key === 'y' || e.key === 'Y') { e.preventDefault(); md.axis = md.axis === 'y' ? null : 'y'; applyModal(cursorRef.current); return; }
        if (e.key === 'Backspace') { e.preventDefault(); md.typed = md.typed.slice(0, -1); applyModal(cursorRef.current); return; }
        if (/^[0-9.-]$/.test(e.key)) { e.preventDefault(); md.typed += e.key; applyModal(cursorRef.current); return; }
        return; // everything else is inert until the transform ends
      }
      // The accelerators themselves. Bare letters, so they never collide with the Ctrl shortcuts
      // below, and only with something selected - otherwise there is nothing to transform.
      if (!e.ctrlKey && !e.metaKey && !e.altKey && selection.size) {
        if (e.key === 'g' || e.key === 'G') { e.preventDefault(); startModal('move'); return; }
        if (e.key === 'r' || e.key === 'R') { e.preventDefault(); startModal('rotate'); return; }
        if (e.key === 's' || e.key === 'S') { e.preventDefault(); startModal('scale'); return; }
      }
      // Viewport navigation. Deliberately the keys every 3D and drawing tool already uses, so
      // nobody has to learn ours: Home frames everything, "." frames what is held, and space is a
      // held modifier that turns the left button into a pan for trackpads with no middle click.
      if (e.key === ' ' || e.code === 'Space') { if (!spaceRef.current) { spaceRef.current = true; redraw(); } e.preventDefault(); return; }
      if (e.key === 'Home') { e.preventDefault(); frameAll(); return; }
      if (e.key === '.') {
        e.preventDefault();
        const b = selectionBounds();
        if (b) frameOn(b); else frameAll();
        return;
      }
      // Cycle the snap magnet. A single key because it is changed mid-draw, and Ctrl-invert covers
      // the one-off case without touching the setting at all.
      if (e.key === 'm' || e.key === 'M') { e.preventDefault(); setSnapMode((cur) => SNAP_MODES[(SNAP_MODES.indexOf(cur) + 1) % SNAP_MODES.length]); return; }
      if ((e.key === 'Control' || e.key === 'Meta') && !snapInvertRef.current) { snapInvertRef.current = true; redraw(); }
      if (e.key === 'Delete' || e.key === 'Backspace') {
        if (selection.size) { e.preventDefault(); deleteSelection(); }
      } else if ((e.ctrlKey || e.metaKey) && (e.key === 'c' || e.key === 'C')) {
        if (selection.size) { e.preventDefault(); copySelection(); }
      } else if ((e.ctrlKey || e.metaKey) && (e.key === 'x' || e.key === 'X')) {
        if (selection.size) { e.preventDefault(); cutSelection(); }
      } else if ((e.ctrlKey || e.metaKey) && (e.key === 'v' || e.key === 'V')) {
        if (clipRef.current) { e.preventDefault(); pasteClipboard(); }
      } else if ((e.ctrlKey || e.metaKey) && (e.key === 'a' || e.key === 'A')) {
        // Select all of this plan's geometry and markers — the natural partner to a bulk delete.
        e.preventDefault();
        const all = new Set();
        segsRef.current.forEach((_, i) => all.add(selKey('seg', i)));
        doorsRef.current.forEach((_, i) => all.add(selKey('door', i)));
        windowsRef.current.forEach((_, i) => all.add(selKey('win', i)));
        stairsRef.current.forEach((_, i) => all.add(selKey('stair', i)));
        parkingRef.current.forEach((_, i) => all.add(selKey('park', i)));
        platsRef.current.forEach((_, i) => all.add(selKey('plat', i)));
        placements.forEach((p) => all.add(selKey('cam', p.id)));
        setSelection(all);
      } else if (e.key === 'Enter' && tool === 'wall') finishWall();
      else if (e.key === 'Enter' && POLY_TOOLS.has(tool)) finishPoly(tool);
      else if (e.key === 'Escape') {
        // Esc backs out one level: cancel an in-progress wall run (drop the uncommitted corners so
        // the chain stops) or clear a selection. Enter or double-click is how you finish and keep a
        // wall. When there IS something to cancel we consume the event here — otherwise the dialog
        // that hosts this editor also listens for Escape and would close the whole editor.
        const hadDraft = !!draftRef.current;
        const hadSel = selection.size > 0;
        if (hadDraft || hadSel) {
          e.preventDefault(); e.stopImmediatePropagation();
          draftRef.current = null; clearSel(); redraw();
        }
        // else: nothing to cancel — let Escape bubble so the host dialog can close.
      }
      else if ((e.ctrlKey || e.metaKey) && (e.key === 'z' || e.key === 'Z')) { e.preventDefault(); if (e.shiftKey) redo(); else undo(); }
      else if ((e.ctrlKey || e.metaKey) && (e.key === 'y' || e.key === 'Y')) { e.preventDefault(); redo(); }
    };
    // Space and Ctrl are held modifiers, so their release matters as much as their press.
    const onKeyUp = (e) => {
      if (e.key === ' ' || e.code === 'Space') { spaceRef.current = false; redraw(); }
      if (e.key === 'Control' || e.key === 'Meta') { snapInvertRef.current = false; redraw(); }
    };
    // Capture phase: this runs before the host's (bubble-phase) window Escape handler, so a
    // consumed Escape never reaches it. Delete/Ctrl+Z are unaffected (they don't stop propagation).
    window.addEventListener('keydown', onKey, true);
    window.addEventListener('keyup', onKeyUp, true);
    // A window that loses focus never delivers the keyup, so a held modifier would stick on: come
    // back to the tab and the left button is still panning for no visible reason.
    const onBlur = () => { spaceRef.current = false; snapInvertRef.current = false; panRef.current = null; redraw(); };
    window.addEventListener('blur', onBlur);
    return () => {
      window.removeEventListener('keydown', onKey, true);
      window.removeEventListener('keyup', onKeyUp, true);
      window.removeEventListener('blur', onBlur);
    };
  }, [mode, tool, selection, placements, deleteSelection, copySelection, cutSelection, pasteClipboard, finishWall, finishPoly, undo, redo, redraw, commit, clearSel, frameAll, frameOn, startModal, applyModal, cancelModal, confirmModal]);

  // ---- the generic object accessors the inspector works through -------------------------------
  //
  // One selected object of ANY registry type, plus the two verbs that change it. Adding a type in
  // P5 gets a working inspector through these without an edit here - which is the reason the
  // registry landed before this phase.
  const LIST_BY_REF = () => listByRef();
  const soleTyped = (() => {
    if (selection.size !== 1) return null;
    const key = [...selection][0];
    const c = key.indexOf(':'); const tag = key.slice(0, c); const idx = Number(key.slice(c + 1));
    const name = Object.keys(PLAN_OBJECTS).find((k) => PLAN_OBJECTS[k].sel === tag);
    if (!name || PLAN_OBJECTS[name].sel === 'seg') return null; // a wall has no fields of its own
    const list = LIST_BY_REF()[PLAN_OBJECTS[name].ref] || [];
    const obj = list[idx];
    return obj ? { name, idx, obj, spec: PLAN_OBJECTS[name] } : null;
  })();

  // patchObject merges fields into one object and commits it - one undo step, whatever the type.
  const patchObject = (name, idx, patch) => {
    const spec = PLAN_OBJECTS[name]; if (!spec) return;
    const list = LIST_BY_REF()[spec.ref] || [];
    commit({ [spec.ref]: list.map((o, i) => (i === idx ? { ...o, ...patch } : o)) });
  };
  // patchObjectLive is the same edit WITHOUT a history entry, for a slider being dragged: one undo
  // step per gesture rather than one per pixel. pushHistory on grab, this while moving.
  const patchObjectLive = (name, idx, patch) => {
    const spec = PLAN_OBJECTS[name]; if (!spec) return;
    const set = REF_SETTERS[spec.ref]; if (!set) return;
    const list = LIST_BY_REF()[spec.ref] || [];
    set(list.map((o, i) => (i === idx ? { ...o, ...patch } : o)));
    redraw(); scheduleSave();
  };
  // transformObject routes a typed X / rotation / width through the SAME affine step a drag or a
  // G/R/S transform uses, so the mouse and the keyboard cannot disagree about what a transform means.
  const transformObject = (name, idx, partial) => {
    const spec = PLAN_OBJECTS[name]; if (!spec) return;
    const xf = { ...IDENTITY_XF, ...partial };
    const orig = beginMove(new Set([selKey(spec.sel, idx)]), { x: 0, y: 0 });
    const next = xfObjects(orig, xf);
    const patch = {};
    if (next.segs.size) patch.segs = segsRef.current.map((o, i) => next.segs.get(i) || o);
    if (next.doors.size) patch.doors = doorsRef.current.map((o, i) => next.doors.get(i) || o);
    if (next.wins.size) patch.windows = windowsRef.current.map((o, i) => next.wins.get(i) || o);
    if (next.stairs.size) patch.stairs = stairsRef.current.map((o, i) => next.stairs.get(i) || o);
    if (next.parks.size) patch.parking = parkingRef.current.map((o, i) => next.parks.get(i) || o);
    if (next.plats.size) patch.platforms = platsRef.current.map((o, i) => next.plats.get(i) || o);
    // Registry-drawn types write back through their own ref, so the same commit covers a road
    // moved with a wall in one selection - one undo step for the whole gesture.
    Object.keys(next.extra || {}).forEach((refName) => {
      const m = next.extra[refName];
      if (m && m.size) patch[refName] = (listByRef()[refName] || []).map((x, i) => m.get(i) || x);
    });
    if (Object.keys(patch).length) commit(patch);
  };

  const sel = selId ? placements.find((p) => p.id === selId) : null;
  const selStairObj = selStair >= 0 ? stairsRef.current[selStair] : null;
  const selDoorObj = selDoor >= 0 ? doorsRef.current[selDoor] : null;
  const selWinObj = selWin >= 0 ? windowsRef.current[selWin] : null;
  const selParkObj = selPark >= 0 ? parkingRef.current[selPark] : null;
  const selPlatObj = selPlat >= 0 ? platsRef.current[selPlat] : null;
  // Flip a stair between going UP and going DOWN (a descent to a lower level / basement).
  // A stair's effective step count: its own setting, or a default from the storey height.
  // The raised floor a stair SITS ON, or null: the one whose footprint contains the stair's CENTRE.
  // Using the centre (not any overlap) means a stair only rests on a platform when it is deliberately
  // placed over it — a sliver of edge overlap no longer flips the whole flight onto the slab.
  const stairPlatform = (s) => {
    const c = rectCenter(s);
    let plat = null; let best = -1;
    platsRef.current.forEach((p) => { if (pointInRotatedRect(c.x, c.y, p)) { const r = p.rise > 0 ? p.rise : DEF_RISE; if (r > best) { best = r; plat = p; } } });
    return plat;
  };
  // A stair on a platform starts at that platform's height; on the ordinary floor it starts at 0.
  const stairBaseH = (s) => { const p = stairPlatform(s); return p ? (p.rise > 0 ? p.rise : DEF_RISE) : 0; };
  // snapStairToPlatform LOCKS a stair fully within the platform it sits on, so no part of the flight
  // overhangs the slab edge and floats. If the stair is larger than the platform it is centred on it.
  // A stair whose centre is off every platform is left where it is (an ordinary floor stair).
  const snapStairToPlatform = (s) => {
    const plat = stairPlatform(s);
    if (!plat) return s;
    const pcx = (plat.x1 + plat.x2) / 2; const pcy = (plat.y1 + plat.y2) / 2;
    const a = plat.a || 0; const cs = Math.cos(-a); const sn = Math.sin(-a);
    const toLocal = (x, y) => { const dx = x - pcx; const dy = y - pcy; return { x: dx * cs - dy * sn, y: dx * sn + dy * cs }; };
    const fromLocal = (x, y) => { const c2 = Math.cos(a); const s2 = Math.sin(a); return { x: pcx + x * c2 - y * s2, y: pcy + x * s2 + y * c2 }; };
    // The stair's extent in the platform's frame, and the platform's half-size.
    const corners = rectCorners(s).map((cn) => toLocal(cn.x, cn.y));
    const hw = (Math.max(...corners.map((p) => p.x)) - Math.min(...corners.map((p) => p.x))) / 2;
    const hh = (Math.max(...corners.map((p) => p.y)) - Math.min(...corners.map((p) => p.y))) / 2;
    const PW = Math.abs(plat.x2 - plat.x1) / 2; const PH = Math.abs(plat.y2 - plat.y1) / 2;
    const c = rectCenter(s); const lc = toLocal(c.x, c.y);
    const clamp = (v, ext, lim) => (ext >= lim ? 0 : Math.max(-(lim - ext), Math.min(lim - ext, v)));
    const world = fromLocal(clamp(lc.x, hw, PW), clamp(lc.y, hh, PH));
    const dx = world.x - c.x; const dy = world.y - c.y;
    if (Math.abs(dx) < 1e-6 && Math.abs(dy) < 1e-6) return s;
    return { ...s, x1: s.x1 + dx, y1: s.y1 + dy, x2: s.x2 + dx, y2: s.y2 + dy };
  };
  const stairClimbH = (s) => (s.height > 0 ? s.height : (+wallHeight || 2.7));
  // Step lines default to the climb height (a stair resting on a high platform is a shorter flight),
  // unless the operator has set a count.
  const stairSteps = (s) => Math.max(STAIR_MIN_STEPS, Math.min(STAIR_MAX_STEPS, s.steps || Math.round(stairClimbH(s) / 0.18)));
  // ---- the status bar ---------------------------------------------------------------------------
  //
  // Reported UP to the host, which owns the strip along the bottom of the window. The editor is the
  // only thing that knows the tool, the selection, where the pointer is and what the magnet caught,
  // and the host is the only thing with somewhere to put it.
  //
  // The cursor position is in METRES when the plan has a scale and pixels when it does not - saying
  // "3.4" with no idea what a pixel is worth would be a fabricated number, and the set-scale tool
  // exists precisely so that stops being the answer.
  const statusRef = useRef('');
  useEffect(() => {
    if (!onStatus) return;
    const cur = hoverPosRef.current;
    const md = moveRef.current;
    const effSnap = snapInvertRef.current ? (snapMode === 'off' ? 'grid' : 'off') : snapMode;
    const next = {
      tool: t(faceOf(kind, tool).key),
      selection: selection.size,
      snap: t(`grid.snap.${effSnap}`),
      snapOn: effSnap !== 'off',
      zoom: Math.round(view.scale * 100),
      pos: cur ? (shownScale > 0
        ? `${(cur.x * shownScale).toFixed(2)}, ${(cur.y * shownScale).toFixed(2)} m`
        : `${Math.round(cur.x)}, ${Math.round(cur.y)} px`) : '',
      // The accelerators are advertised here rather than hidden in a keymap nobody opens - the
      // discoverability half of "G/R/S are accelerators, never the only way". While one is RUNNING
      // the strip becomes its readout instead: what you are doing, what you have typed, and how to
      // get out. A modal state with nothing on screen saying so is the thing that makes a modal
      // keymap hostile, and it is cheap to avoid.
      hint: mode !== '2d' ? '' : (md && md.modal
        ? t('grid.modalHint', {
          op: t(`grid.modal.${md.mode}`),
          value: md.typed !== '' ? md.typed : '…',
          unit: md.mode === 'move' ? (shownScale > 0 ? 'm' : 'px') : (md.mode === 'rotate' ? '°' : '×'),
          axis: md.axis ? md.axis.toUpperCase() : t('grid.modalFree'),
        })
        : t('grid.statusHint')),
      modal: !!(md && md.modal),
    };
    const sig = JSON.stringify(next);
    if (sig === statusRef.current) return;
    statusRef.current = sig;
    onStatus(next);
  });

  const draftFloor = { ...floor, grid: JSON.stringify(modelJSON()), scale, wallHeight: +wallHeight || 0 };
  const toolHint = (id) => t(faceOf(kind, id).hint);
  // Tool buttons are icon-only (label in the tooltip) so the vertical palette stays narrow. Both the
  // icon and the label come from the per-kind face, so the same `wall` tool reads "Wall" inside a
  // building and "Fence" on a park.
  const toolBtn = (id) => {
    const f = faceOf(kind, id);
    // Locked while carrying something: picking a tool would abandon the placement, and it is not
    // obvious that it does. The banner's Cancel is the way out.
    const locked = !!placing && id !== 'select';
    return (
      <button
        key={id}
        type="button"
        className={tool === id ? 'active' : ''}
        disabled={locked}
        onClick={() => { setTool(id); draftRef.current = null; if (id !== 'select') clearSel(); }}
        title={locked ? t('grid.lockedWhilePlacing') : t(f.key)}
        aria-label={t(f.key)}
      >
        <Ico n={f.icon} sz={15} />
      </button>
    );
  };

  // startPanelDrag makes any dockable panel movable by its grip. Grabbing undocks it to a floating
  // panel (at its current spot, so it doesn't jump); where it is dropped decides its new home —
  // near the left edge docks left, near the right edge docks right (stacking onto whatever is there),
  // and anywhere in between leaves it floating. Swapping and stacking both fall out of this.
  const startPanelDrag = (name) => (e) => {
    const panel = e.currentTarget.closest('.fe-panel'); const surf = editorRef.current;
    if (!panel || !surf) return;
    e.preventDefault();
    const sr = surf.getBoundingClientRect(); const pr = panel.getBoundingClientRect();
    const offX = e.clientX - pr.left; const offY = e.clientY - pr.top; const pw = pr.width; const ph = pr.height;
    setDock((d) => ({ ...d, [name]: 'float' }));
    setFloatPos((f) => ({ ...f, [name]: { x: pr.left - sr.left, y: pr.top - sr.top } }));
    const move = (ev) => {
      const x = Math.max(0, Math.min(ev.clientX - sr.left - offX, sr.width - pw));
      const y = Math.max(0, Math.min(ev.clientY - sr.top - offY, sr.height - ph));
      setFloatPos((f) => ({ ...f, [name]: { x, y } }));
    };
    const up = (ev) => {
      window.removeEventListener('pointermove', move); window.removeEventListener('pointerup', up);
      const rel = ev.clientX - sr.left;
      const zone = rel < sr.width * 0.25 ? 'left' : rel > sr.width * 0.75 ? 'right' : 'float';
      if (zone !== 'float') setDock((d) => ({ ...d, [name]: zone }));
    };
    window.addEventListener('pointermove', move); window.addEventListener('pointerup', up);
  };
  const grip = (name) => <button type="button" className="fe-grip" onPointerDown={startPanelDrag(name)} title={t('grid.dragPanel')} aria-label={t('grid.dragPanel')}><Ico n="grid2" sz={12} /></button>;

  // The inspector's body — identical whether the panel is docked or floating.
  const inspectorBody = (
    <>
      {sel && sel.cameraId ? (
        <div className="fed-inspector">
          <div className="fed-inspector-title"><Ico n="video" sz={13} /> {sel.lastKnownName || t('nodes.cameraN', { id: sel.cameraId })}</div>
          <p className="grid-hint">{t('fed.aimHint')}</p>
          <label className="grid-field"><span>{t('grid.aim')}</span><input type="range" min="0" max="359" step="1" value={Math.round(sel.heading || 0)} onChange={(e) => onAim(sel.id, { heading: +e.target.value })} /><em>{Math.round(sel.heading || 0)}°</em></label>
          <label className="grid-field"><span>{t('grid.fov')}</span><input type="range" min="0" max="170" step="1" value={Math.round(sel.fov || 0)} onChange={(e) => onAim(sel.id, { fov: +e.target.value })} /><em>{Math.round(sel.fov || 0)}°</em></label>
          <label className="grid-field"><span>{t('grid.mountHeight')}</span><input type="range" min="0.5" max="6" step="0.1" value={sel.mountHeight > 0 ? sel.mountHeight : 2.5} onChange={(e) => onAim(sel.id, { mountHeight: +e.target.value })} /><em>{(sel.mountHeight > 0 ? sel.mountHeight : 2.5).toFixed(1)} m</em></label>
          <label className="grid-field"><span>{t('grid.pitch')}</span><input type="range" min="0" max="90" step="1" value={Math.round(sel.pitch || 0)} onChange={(e) => onAim(sel.id, { pitch: +e.target.value })} /><em>{Math.round(sel.pitch || 0)}°</em></label>
          <button type="button" className="quiet danger-text fed-remove" onClick={deleteSelection}><span className="btn-icon"><Ico n="trash" sz={13} /> {t('map.removeMarker')}</span></button>
        </div>
      ) : sel ? (
        <div className="fed-inspector">
          <div className="fed-inspector-title"><Ico n="cpu" sz={13} /> {sel.lastKnownName || nodesById[sel.nodeId]?.name || sel.nodeId}</div>
          <button type="button" className="quiet danger-text fed-remove" onClick={deleteSelection}><span className="btn-icon"><Ico n="trash" sz={13} /> {t('map.removeMarker')}</span></button>
        </div>
      ) : selection.size > 1 || selSegs.size ? (
        <div className="fed-inspector">
          {/* Several things held, of whatever mix of kinds — there is no single width or ascent to
              edit, so the panel offers what DOES apply to all of them. */}
          <div className="fed-inspector-title"><Ico n="cursor" sz={13} /> {t('fed.objectsSelected', { n: selection.size })}</div>
          <div className="grid-readout">
            {selSegs.size ? <div><span>{t(kind === KIND_OUTDOOR ? 'grid.fences' : 'grid.walls')}</span><strong>{selSegs.size}</strong></div> : null}
            {selOf('door').length ? <div><span>{t(kind === KIND_OUTDOOR ? 'grid.gates' : 'grid.doors')}</span><strong>{selOf('door').length}</strong></div> : null}
            {selOf('win').length ? <div><span>{t('grid.windows')}</span><strong>{selOf('win').length}</strong></div> : null}
            {selOf('stair').length ? <div><span>{t('grid.stairs')}</span><strong>{selOf('stair').length}</strong></div> : null}
            {selOf('park').length ? <div><span>{t('grid.parking')}</span><strong>{selOf('park').length}</strong></div> : null}
            {selOf('plat').length ? <div><span>{t('grid.platforms')}</span><strong>{selOf('plat').length}</strong></div> : null}
            {selOf('cam').length ? <div><span>{t('map.cameras')}</span><strong>{selOf('cam').length}</strong></div> : null}
          </div>
          <p className="grid-hint">{t('fed.multiHint')}</p>
          <button type="button" className="quiet danger-text fed-remove" onClick={deleteSelection}><span className="btn-icon"><Ico n="trash" sz={13} /> {t('grid.erase')}</span></button>
          <button type="button" className="quiet fed-remove" onClick={clearSel}>{t('fed.deselect')}</button>
        </div>
      ) : soleTyped ? (
        <div className="fed-inspector">
          {/* ONE panel for every registry type. It replaces five hand-written blocks that were
              sliders and nothing else - there was no way to type a number anywhere in this editor,
              which is most of why it read as a toy. A type declares its fields once and gets a
              working inspector; P5's outdoor kit needs no edit here at all. */}
          <div className="fed-inspector-title">
            <Ico n={faceOf(kind, soleTyped.spec.tool ? soleTyped.spec.tool.id : 'select').icon} sz={13} />
            {' '}{t(soleTyped.spec.label)}
          </div>
          {/* Readouts that are DERIVED rather than editable stay: a stair's resting height comes
              from the platform under it, and a bay's width from the row divided by its count. */}
          {soleTyped.name === 'stair' && stairBaseH(soleTyped.obj) > 0 ? (
            <div className="grid-readout"><div><span>{t('grid.restsOn')}</span><strong>+{stairBaseH(soleTyped.obj).toFixed(2)} m</strong></div></div>
          ) : null}
          {soleTyped.name === 'parking' ? (
            <div className="grid-readout"><div><span>{t('grid.bayWidth')}</span><strong>{shownScale > 0 ? `${(((baysAcrossX(soleTyped.obj) ? soleTyped.obj.x2 - soleTyped.obj.x1 : soleTyped.obj.y2 - soleTyped.obj.y1) * shownScale) / Math.max(1, soleTyped.obj.bays || 1)).toFixed(2)} m` : '—'}</strong></div></div>
          ) : null}
          <PlanFields
            typeName={soleTyped.name}
            index={soleTyped.idx}
            obj={soleTyped.obj}
            scale={shownScale}
            unit={unit}
            siteKind={kind}
            onPatch={(patch) => patchObject(soleTyped.name, soleTyped.idx, patch)}
            onPatchLive={(patch) => patchObjectLive(soleTyped.name, soleTyped.idx, patch)}
            onTransform={(xf) => transformObject(soleTyped.name, soleTyped.idx, xf)}
          />
          <button type="button" className="quiet danger-text fed-remove" onClick={deleteSelection}><span className="btn-icon"><Ico n="trash" sz={13} /> {t('grid.erase')}</span></button>
        </div>
      ) : (
        <div className="fed-inspector">
          <label className="grid-field"><span>{t('grid.cellSize')}</span><span className="grid-input-row"><input type="number" min="0.1" step="0.1" value={cellMeters} onChange={(e) => { setCellMeters(Math.max(0, +e.target.value || 0)); setScaleKnown(true); }} /><em>{t('grid.metres')}</em></span></label>
          <label className="grid-field"><span>{t('grid.wallHeight')}</span><span className="grid-input-row"><input type="number" min="0.5" step="0.1" value={wallHeight} onChange={(e) => setWallHeight(Math.max(0, +e.target.value || 0))} /><em>{t('grid.metres')}</em></span></label>
          {/* The tally that used to live here - "Walls 42, Doors 3" - is gone. It was a COUNT where
              a LIST was wanted, and the outliner beside this panel is now that list: the same
              numbers, plus the ability to find, hide or lock any one of them. Two of them would
              just be the same facts twice. */}
          <p className="grid-hint">{t('fed.placeHint')}</p>
        </div>
      )}
    </>
  );

  // Build each dockable panel once, then place it into the left/right dock zone or float it.
  const panelWrap = (name, children) => {
    const floating = dock[name] === 'float';
    const pos = floatPos[name] || { x: 12, y: 12 };
    return (
      <div key={name} className={`fe-panel ${name === 'toolbar' ? 'fe-toolbar' : name === 'outliner' ? 'fe-outliner' : 'fe-inspector'}${floating ? ' fe-float' : ''}`} style={floating ? { left: pos.x, top: pos.y } : undefined}>
        {children}
      </div>
    );
  };
  const toolbarPanel = panelWrap('toolbar', (
    <>
      {grip('toolbar')}
      <div className="floor-editor-modes" role="group" aria-label={t('map.viewMode')}>
        <button type="button" className={mode === '2d' ? 'active' : ''} onClick={() => setMode('2d')} title={t('map.view2d')} aria-label={t('map.view2d')}><Ico n="map" sz={14} /></button>
        <button type="button" className={mode === '3d' ? 'active' : ''} onClick={() => setMode('3d')} title={t('map.view3d')} aria-label={t('map.view3d')}><Ico n="box" sz={14} /></button>
      </div>
      {mode === '2d' ? (
        <>
          <span className="fd-toolbar-sep" />
          {/* The tool list comes from what this place IS — see TOOLSETS. */}
          <div className="grid-tool-group" role="group" aria-label={t('grid.tool')}>
            {tools.map((id) => toolBtn(id))}
          </div>
          <span className="fd-toolbar-sep" />
          <div className="fe-toolbar-row">
            <button type="button" className="fd-tool" onClick={undo} disabled={!histRef.current.length} title={t('grid.undo')} aria-label={t('grid.undo')}><Ico n="undo" sz={14} /></button>
            <button type="button" className="fd-tool" onClick={redo} disabled={!futRef.current.length} title={t('grid.redo')} aria-label={t('grid.redo')}><Ico n="redo" sz={14} /></button>
          </div>
          {draftRef.current && draftRef.current.pts ? <button type="button" className="fd-tool" onClick={finishWall} title={t('grid.finish')} aria-label={t('grid.finish')}><Ico n="check-ok" sz={14} /></button> : null}
          <span className="fd-toolbar-sep" />
          {/* The snap magnet. One control answers "is it snapping, and to what" - and it says so in
              the status bar as well, because a mode you cannot see is the mode this editor had. */}
          <button type="button" className={`fd-tool${snapMode === 'off' ? '' : ' active'}`} onClick={() => setSnapMode((cur) => SNAP_MODES[(SNAP_MODES.indexOf(cur) + 1) % SNAP_MODES.length])} title={t('grid.snapCycle', { mode: t(`grid.snap.${snapMode}`) })} aria-label={t('grid.snapCycle', { mode: t(`grid.snap.${snapMode}`) })}><Ico n={SNAP_ICO[snapMode] || 'grid2'} sz={14} /></button>
          <span className="fd-toolbar-sep" />
          <div className="fe-toolbar-row">
            <button type="button" className="fd-tool" onClick={() => zoomAbout(1 / 1.2, stage.w / 2, stage.h / 2)} disabled={view.scale <= MIN_SCALE + 1e-6} title={t('grid.zoomOut')} aria-label={t('grid.zoomOut')}><Ico n="zoom-out" sz={14} /></button>
            <button type="button" className="fd-tool" onClick={() => zoomAbout(1.2, stage.w / 2, stage.h / 2)} disabled={view.scale >= MAX_SCALE - 1e-6} title={t('grid.zoomIn')} aria-label={t('grid.zoomIn')}><Ico n="zoom-in" sz={14} /></button>
          </div>
          <div className="fe-toolbar-row">
            <button type="button" className="fd-tool" onClick={frameAll} title={t('grid.frameAll')} aria-label={t('grid.frameAll')}><Ico n="grid4" sz={14} /></button>
            <button type="button" className="fd-tool" onClick={() => { const b = selectionBounds(); if (b) frameOn(b); else frameAll(); }} disabled={!selection.size} title={t('grid.frameSel')} aria-label={t('grid.frameSel')}><Ico n="search" sz={14} /></button>
          </div>
          {/* The readout is now the true view scale, and clicking it frames the plan. Zoom is no
              longer a multiple of "fit", so a percentage of fit would mean nothing. */}
          <button type="button" className="fd-tool fe-zoom-readout" onClick={frameAll} title={t('grid.zoomFit')}>{Math.round(view.scale * 100)}%</button>
        </>
      ) : null}
    </>
  ));
  const propsPanel = mode === '2d' ? panelWrap('props', (
    <>
      <div className="fe-float-bar">{grip('props')}<span className="fe-float-hint">{toolHint(tool)}</span></div>
      {inspectorBody}
    </>
  )) : null;

  // The outliner: everything on the plan, as a list. Its own dockable panel, so it can sit beside
  // the inspector, swap sides with it, or float.
  const outlinerPanel = mode === '2d' ? panelWrap('outliner', (
    <>
      <div className="fe-float-bar">{grip('outliner')}<span className="fe-float-hint">{t('ol.title')}</span></div>
      <PlanOutliner
        lists={listByRef()}
        placements={placements}
        nodesById={nodesById}
        selection={selection}
        scale={shownScale}
        siteKind={kind}
        hidden={hidden}
        locked={locked}
        onSelect={(key, add) => {
          // Click selects; shift/ctrl-click adds. The canvas and the list are one selection.
          setSelection((prev) => {
            if (!add) return new Set([key]);
            const ns = new Set(prev);
            if (ns.has(key)) ns.delete(key); else ns.add(key);
            return ns;
          });
        }}
        onToggleHidden={(key) => setHidden((prev) => { const ns = new Set(prev); if (ns.has(key)) ns.delete(key); else ns.add(key); return ns; })}
        onToggleLocked={(key) => setLocked((prev) => { const ns = new Set(prev); if (ns.has(key)) ns.delete(key); else ns.add(key); return ns; })}
        onToggleCollection={(which, keys, on) => {
          const setter = which === 'hidden' ? setHidden : setLocked;
          setter((prev) => { const ns = new Set(prev); keys.forEach((k) => (on ? ns.add(k) : ns.delete(k))); return ns; });
        }}
      />
    </>
  )) : null;

  const items = { toolbar: toolbarPanel, outliner: outlinerPanel, props: propsPanel };
  const names = mode === '2d' ? ['toolbar', 'outliner', 'props'] : ['toolbar'];
  const leftEls = names.filter((n) => dock[n] === 'left').map((n) => items[n]);
  const rightEls = names.filter((n) => dock[n] === 'right').map((n) => items[n]);
  const floatEls = names.filter((n) => dock[n] === 'float').map((n) => items[n]);

  return (
    <div className="floor-editor" ref={editorRef}>
      <div className="floor-editor-main">
        {leftEls.length ? <div className="fe-dock fe-dock-left">{leftEls}</div> : null}
        <div className="floor-editor-stage">
          {mode === '2d' ? (
            // The canvas IS the viewport: it fills the stage and the plan moves inside it. No
            // scrollbars - panning is the middle button (or space-drag), which is why the wrap no
            // longer switches its overflow on zoom.
            <div className="floor-editor-canvas-wrap" ref={wrapRef} onDragOver={(e) => { e.preventDefault(); e.dataTransfer.dropEffect = 'copy'; }} onDrop={onDrop}>
              <canvas ref={canvasRef} width={cssW} height={cssH} className={`grid-canvas tool-${tool}${placing ? ' placing' : ''}`} style={{ width: cssW, height: cssH, touchAction: 'none', cursor: panCursor }}
                onPointerDown={onPointerDown} onPointerMove={onPointerMove} onPointerUp={onPointerUp} onPointerLeave={() => { cursorRef.current = null; hoverPosRef.current = null; hoverRef.current = -1; if (!moveRef.current) redraw(); }} onDoubleClick={onDoubleClick} />
            </div>
          ) : (
            <div className="grid-3d-wrap">
              <Suspense fallback={<div className="floor-view-empty">{t('map.loading3d')}</div>}>
                <Floor3D floors={[{ floor: draftFloor, placements }]} activeIndex={0} nodesById={nodesById} nowSec={nowSecRef.current} />
              </Suspense>
            </div>
          )}
        </div>
        {rightEls.length ? <div className="fe-dock fe-dock-right">{rightEls}</div> : null}
      </div>
      {floatEls}
    </div>
  );
}

FloorEditor.propTypes = {
  floor: PropTypes.object,
  siteKind: PropTypes.string,
  placements: PropTypes.array,
  nodesById: PropTypes.object,
  placing: PropTypes.object,
  onPlace: PropTypes.func,
  onClearPlacing: PropTypes.func,
  onMove: PropTypes.func,
  onAim: PropTypes.func,
  onRemove: PropTypes.func,
  onSaveModel: PropTypes.func,
  onToast: PropTypes.func,
  onStatus: PropTypes.func,
  busy: PropTypes.bool,
};
