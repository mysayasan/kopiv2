import { lazy, Suspense, useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from 'react';
import PropTypes from 'prop-types';
import { useT, Ico } from '@shared';
import { apiBase } from '../lib/helpers';
import { nodeTone, TONES } from '../lib/fleet_status';
import { KIND_BUILDING, KIND_OUTDOOR, normKind } from './site_kinds';
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
const MIN_STAGE_H = 260; // never shrink the plan to a sliver, scroll instead

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
const TOOLSETS = {
  [KIND_BUILDING]: ['select', 'wall', 'room', 'round', 'door', 'window', 'stairs', 'platform', 'erase'],
  [KIND_OUTDOOR]: ['select', 'wall', 'room', 'round', 'door', 'parking', 'erase'],
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
  erase: { icon: 'trash', key: 'grid.erase', hint: 'grid.eraseHint' },
};
const faceOf = (kind, id) => (TOOL_FACE[kind] && TOOL_FACE[kind][id]) || DEFAULT_FACE[id] || { icon: 'cursor', key: id, hint: id };
// Tools that drag out a rectangle, and (a superset) tools that show a snapped cursor dot. Named
// once so adding a tool does not mean hunting down four `a || b || c` chains.
const BOX_TOOLS = new Set(['room', 'round', 'stairs', 'parking', 'platform']);
const DRAG_TOOLS = new Set(['wall', 'room', 'round', 'stairs', 'parking', 'platform']);
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

export function FloorEditor({ floor, siteKind = KIND_BUILDING, placements = [], nodesById = {}, placing, onPlace, onClearPlacing, onMove, onAim, onRemove, onSaveModel, onToast, busy }) {
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
  const histRef = useRef([]);
  const futRef = useRef([]);
  const draftRef = useRef(null); // wall {pts} · room/round/stairs/parking {start,cur}
  const hoverRef = useRef(-1); // wall index under the erase cursor
  const hoverStairRef = useRef(-1); // stair index under the erase cursor
  const hoverDoorRef = useRef(-1); // door index under the erase cursor
  const hoverWinRef = useRef(-1); // window index under the erase cursor
  const hoverParkRef = useRef(-1); // parking-row index under the erase cursor
  const hoverPlatRef = useRef(-1); // raised-floor index under the erase cursor
  const cursorRef = useRef(null);
  const saveTimer = useRef(null);
  const placingRef = useRef(placing); placingRef.current = placing;
  const [, tick] = useState(0);
  const redraw = useCallback(() => tick((n) => n + 1), []);
  const nowSecRef = useRef(Math.floor(Date.now() / 1000));

  const [mode, setMode] = useState('2d');
  // The tool palette and the properties inspector are both dockable panels. Each can dock to the
  // left or right edge — dropping both on the same side stacks them in one column — or float freely.
  // Drag a panel by its grip; where you drop it (near the left/right edge, or the middle) decides.
  const [dock, setDock] = useState({ toolbar: 'left', props: 'right' }); // 'left' | 'right' | 'float'
  const [floatPos, setFloatPos] = useState({ toolbar: null, props: null });
  const [tool, setTool] = useState('select'); // select | wall | room | round | door | window | stairs | parking | erase
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
  const existing = (() => { try { return floor && floor.grid ? JSON.parse(floor.grid) : null; } catch (_) { return null; } })();
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

  // Display scale = the auto-fit scale (plan → available space) times a user zoom multiplier. Zoom 1
  // is "fit"; above 1 the canvas grows past the viewport and the wrap shows scrollbars.
  const [zoom, setZoom] = useState(1);
  const clampZoom = (z) => Math.min(6, Math.max(0.25, z));
  const fitScale = Math.min(stage.w / w, stage.h / h, 2);
  const ds = fitScale * zoom;
  const cssW = Math.round(w * ds);
  const cssH = Math.round(h * ds);

  // Ctrl/⌘ + wheel zooms (a native non-passive listener so it can suppress the page/scroll default);
  // a plain wheel is left alone so it scrolls the overflowing canvas normally.
  useEffect(() => {
    const el = canvasRef.current;
    if (!el) return undefined;
    const onWheel = (e) => { if (!(e.ctrlKey || e.metaKey)) return; e.preventDefault(); setZoom((z) => clampZoom(z * (e.deltaY < 0 ? 1.1 : 1 / 1.1))); };
    el.addEventListener('wheel', onWheel, { passive: false });
    return () => el.removeEventListener('wheel', onWheel);
    // Re-bind when the canvas remounts (e.g. after switching to 3D and back).
  }, [mode]);
  // Keep the viewport centre fixed across a zoom change, so zooming grows/shrinks around the middle
  // of what you are looking at rather than the top-left corner.
  const prevZoomRef = useRef(zoom);
  useLayoutEffect(() => {
    const el = wrapRef.current;
    if (el && prevZoomRef.current && prevZoomRef.current !== zoom) {
      const r = zoom / prevZoomRef.current;
      el.scrollLeft = (el.scrollLeft + el.clientWidth / 2) * r - el.clientWidth / 2;
      el.scrollTop = (el.scrollTop + el.clientHeight / 2) * r - el.clientHeight / 2;
    }
    prevZoomRef.current = zoom;
  }, [zoom]);

  const [cellMeters, setCellMeters] = useState(() => { const s = floor && floor.scale > 0 ? floor.scale : 0; return s > 0 ? +(s * unit).toFixed(2) : 0.5; });
  const [wallHeight, setWallHeight] = useState(() => (floor && floor.wallHeight > 0 ? floor.wallHeight : 2.7));
  const scale = cellMeters > 0 ? cellMeters / unit : 0;
  const scaleRef = useRef(scale); scaleRef.current = scale;

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
  const modelJSON = () => ({
    version: 2, unit, segments: segsRef.current, stairs: stairsRef.current,
    doors: doorsRef.current, windows: windowsRef.current, parking: parkingRef.current, platforms: platsRef.current,
  });

  // Debounced autosave of the wall model + scale + height.
  const scheduleSave = useCallback(() => {
    clearTimeout(saveTimer.current);
    saveTimer.current = setTimeout(() => {
      onSaveModel({ grid: JSON.stringify(modelJSON()), scale: scaleRef.current, wallHeight: +wallHeight || 0, elevation: (floor && floor.elevation) || 0 });
    }, 700);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [onSaveModel, unit, wallHeight, floor && floor.elevation]);
  useEffect(() => () => clearTimeout(saveTimer.current), []);

  // History snapshots the WHOLE model, so undo/redo restores it in one step however many lists a
  // single edit touched. Taking a snapshot (rather than a per-list diff) is also why adding a new
  // kind of thing to the plan needs nothing here beyond listing it.
  const snapshot = () => ({ segs: segsRef.current, stairs: stairsRef.current, doors: doorsRef.current, windows: windowsRef.current, parking: parkingRef.current, plats: platsRef.current });
  const restore = (m) => { segsRef.current = m.segs || []; stairsRef.current = m.stairs || []; doorsRef.current = m.doors || []; windowsRef.current = m.windows || []; parkingRef.current = m.parking || []; platsRef.current = m.plats || []; };
  const pushHistory = useCallback(() => { histRef.current.push(snapshot()); if (histRef.current.length > 100) histRef.current.shift(); futRef.current = []; }, []);
  // commit takes a PATCH: name only the lists the edit changes, e.g. commit({ doors: next }).
  const commit = useCallback((patch) => {
    pushHistory();
    const p = patch || {};
    if (p.segs !== undefined) segsRef.current = p.segs;
    if (p.stairs !== undefined) stairsRef.current = p.stairs;
    if (p.doors !== undefined) doorsRef.current = p.doors;
    if (p.windows !== undefined) windowsRef.current = p.windows;
    if (p.parking !== undefined) parkingRef.current = p.parking;
    if (p.platforms !== undefined) platsRef.current = p.platforms;
    redraw(); scheduleSave();
  }, [pushHistory, redraw, scheduleSave]);
  const undo = useCallback(() => { if (!histRef.current.length) return; futRef.current.push(snapshot()); restore(histRef.current.pop()); draftRef.current = null; redraw(); scheduleSave(); }, [redraw, scheduleSave]);
  const redo = useCallback(() => { if (!futRef.current.length) return; histRef.current.push(snapshot()); restore(futRef.current.pop()); redraw(); scheduleSave(); }, [redraw, scheduleSave]);
  // Re-save when scale/height change (but not on first mount).
  const firstRef = useRef(true);
  useEffect(() => { if (firstRef.current) { firstRef.current = false; return; } if (segsRef.current.length || stairsRef.current.length) scheduleSave(); /* eslint-disable-next-line */ }, [cellMeters, wallHeight]);

  // The snap grid is a SUBDIVISION of the metre cell: `unit` still defines the cell (and the scale),
  // but drawing and snapping happen at unit/GRID_SUBDIV, so a marker locks onto a much finer lattice
  // without changing what a cell means in metres.
  const snap = unit / GRID_SUBDIV;
  const snapPt = useCallback((x, y) => {
    const st = unit / GRID_SUBDIV;
    const mr = st * 0.75; let best = null; let bestD = mr;
    segsRef.current.forEach((s) => { [[s.x1, s.y1], [s.x2, s.y2]].forEach(([ex, ey]) => { const d = Math.hypot(ex - x, ey - y); if (d < bestD) { bestD = d; best = { x: ex, y: ey }; } }); });
    if (best) return best;
    return { x: Math.max(0, Math.min(w, Math.round(x / st) * st)), y: Math.max(0, Math.min(h, Math.round(y / st) * st)) };
  }, [unit, w, h]);

  // ---- drawing ----
  const draw = useCallback(() => {
    const cv = canvasRef.current; if (!cv) return;
    const ctx = cv.getContext('2d');
    ctx.clearRect(0, 0, cssW, cssH);
    if (imgRef.current) { ctx.globalAlpha = 0.45; ctx.drawImage(imgRef.current, 0, 0, cssW, cssH); ctx.globalAlpha = 1; }
    else { ctx.fillStyle = '#f1f5f9'; ctx.fillRect(0, 0, cssW, cssH); }
    // Drafting grid: faint minor lines on the fine snap step, stronger lines on the metre cell —
    // so the lattice reads clearly and an object is easy to line up. Minor lines are dropped once
    // they would be denser than a few pixels apart (zoomed out), to keep the canvas legible.
    const step = unit / GRID_SUBDIV;
    ctx.lineWidth = 1;
    if (step * ds >= 4) {
      ctx.strokeStyle = 'rgba(100,116,139,0.16)';
      ctx.beginPath();
      for (let x = 0; x <= w; x += step) { ctx.moveTo(Math.round(x * ds) + 0.5, 0); ctx.lineTo(Math.round(x * ds) + 0.5, cssH); }
      for (let y = 0; y <= h; y += step) { ctx.moveTo(0, Math.round(y * ds) + 0.5); ctx.lineTo(cssW, Math.round(y * ds) + 0.5); }
      ctx.stroke();
    }
    ctx.strokeStyle = 'rgba(100,116,139,0.34)';
    ctx.beginPath();
    for (let x = 0; x <= w; x += unit) { ctx.moveTo(Math.round(x * ds) + 0.5, 0); ctx.lineTo(Math.round(x * ds) + 0.5, cssH); }
    for (let y = 0; y <= h; y += unit) { ctx.moveTo(0, Math.round(y * ds) + 0.5); ctx.lineTo(cssW, Math.round(y * ds) + 0.5); }
    ctx.stroke();

    const seg = (s, color, wd) => { ctx.strokeStyle = color; ctx.lineWidth = wd; ctx.lineCap = 'round'; ctx.beginPath(); ctx.moveTo(s.x1 * ds, s.y1 * ds); ctx.lineTo(s.x2 * ds, s.y2 * ds); ctx.stroke(); };
    const dot = (x, y, color, r = 3) => { ctx.fillStyle = color; ctx.beginPath(); ctx.arc(x, y, r, 0, Math.PI * 2); ctx.fill(); };

    // walls (image space) — selected walls are highlighted; a group drag previews the moved position.
    const mv = moveRef.current;
    const pv = mv && mv.moved ? xfObjects(mv.orig, mv.xf) : null;
    segsRef.current.forEach((s, i) => {
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
    placements.forEach((p) => {
      // Markers live in OL space (y UP) while the drag delta is image space (y DOWN), so the
      // vertical offset is subtracted here and again when the move is persisted.
      const o = pv && pv.cams.get(p.id);
      const px = o ? o.x : p.x; const py = o ? o.y : p.y;
      const sx = px * ds; const sy = (h - py) * ds;
      const tone = nodeTone(nodesById[p.nodeId], nowSecRef.current) || TONES.idle;
      const isCam = !!p.cameraId;
      if (isCam && (p.fov || 0) > 0) {
        const half = (p.fov || 0) / 2; const N = 22;
        ctx.beginPath(); ctx.moveTo(sx, sy);
        for (let i = 0; i <= N; i++) { const deg = (p.heading || 0) - half + (p.fov || 0) * (i / N); const a = (deg * Math.PI) / 180; ctx.lineTo(sx + rad * ds * Math.sin(a), sy - rad * ds * Math.cos(a)); }
        ctx.closePath(); ctx.fillStyle = `${tone.color}28`; ctx.fill(); ctx.strokeStyle = `${tone.color}88`; ctx.lineWidth = 1; ctx.stroke();
      }
      const selected = isSel('cam', p.id);
      ctx.beginPath(); ctx.arc(sx, sy, isCam ? 7 : 9, 0, Math.PI * 2);
      ctx.fillStyle = tone.color; ctx.fill();
      ctx.lineWidth = selected ? 3 : 2; ctx.strokeStyle = selected ? '#2d6cdf' : '#fff'; ctx.stroke();
      const label = p.lastKnownName || (isCam ? `Cam ${p.cameraId}` : (nodesById[p.nodeId]?.name || p.nodeId));
      ctx.font = '600 11px system-ui, sans-serif'; ctx.textAlign = 'center';
      ctx.lineWidth = 3; ctx.strokeStyle = 'rgba(255,255,255,0.85)'; ctx.strokeText(label, sx, sy - 13);
      ctx.fillStyle = '#1f2937'; ctx.fillText(label, sx, sy - 13); ctx.textAlign = 'left';
    });

    // Camera POV handles: draw over the selected camera's wedge so it can be aimed and widened by
    // dragging. An accent line runs from the body out to each knob; the aim knob (round) points the
    // camera, the two edge knobs set the spread. A live readout shows the current heading/fov.
    if (cam) {
      const H = camHandles(cam);
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
    const mOf = (a, b) => (Math.hypot(b.x - a.x, b.y - a.y) * scaleRef.current).toFixed(1);
    const d = draftRef.current; const cur = cursorRef.current;
    if (d && d.pts) {
      for (let i = 0; i < d.pts.length - 1; i++) seg({ x1: d.pts[i].x, y1: d.pts[i].y, x2: d.pts[i + 1].x, y2: d.pts[i + 1].y }, '#2d6cdf', 5);
      d.pts.forEach((p) => dot(p.x * ds, p.y * ds, '#2d6cdf'));
      const last = d.pts[d.pts.length - 1];
      if (cur && last) { seg({ x1: last.x, y1: last.y, x2: cur.x, y2: cur.y }, 'rgba(45,108,223,0.6)', 4); label(cur.x * ds, cur.y * ds, `${mOf(last, cur)} m`); }
    } else if (d && d.round && d.start && cur) {
      const cx = (d.start.x + cur.x) / 2; const cy = (d.start.y + cur.y) / 2; const rx = Math.abs(cur.x - d.start.x) / 2; const ry = Math.abs(cur.y - d.start.y) / 2;
      ctx.beginPath(); ctx.ellipse(cx * ds, cy * ds, rx * ds, ry * ds, 0, 0, Math.PI * 2);
      ctx.fillStyle = 'rgba(45,108,223,0.12)'; ctx.fill(); ctx.strokeStyle = '#2d6cdf'; ctx.lineWidth = 4; ctx.stroke();
      label(cur.x * ds, cur.y * ds, `${(rx * 2 * scaleRef.current).toFixed(1)} × ${(ry * 2 * scaleRef.current).toFixed(1)} m`);
    } else if (d && d.start && cur) {
      const rx = Math.min(d.start.x, cur.x); const ry = Math.min(d.start.y, cur.y); const rw = Math.abs(cur.x - d.start.x); const rh = Math.abs(cur.y - d.start.y);
      ctx.strokeStyle = '#2d6cdf'; ctx.lineWidth = 4; ctx.strokeRect(rx * ds, ry * ds, rw * ds, rh * ds); ctx.fillStyle = 'rgba(45,108,223,0.12)'; ctx.fillRect(rx * ds, ry * ds, rw * ds, rh * ds);
      label(cur.x * ds, cur.y * ds, `${(rw * scaleRef.current).toFixed(1)} × ${(rh * scaleRef.current).toFixed(1)} m`);
    } else if (cur && DRAG_TOOLS.has(toolRef.current)) dot(cur.x * ds, cur.y * ds, 'rgba(45,108,223,0.7)', 4);
  }, [cssW, cssH, w, h, unit, ds, placements, selection, wallHeight, kind, t]);
  useEffect(() => { if (mode === '2d') draw(); });

  // ---- geometry helpers on the canvas ----
  const evImg = (e) => { const r = canvasRef.current.getBoundingClientRect(); return { x: (e.clientX - r.left) / ds, y: (e.clientY - r.top) / ds }; }; // image space (top-left)
  const evOL = (e) => { const p = evImg(e); return { x: p.x, y: h - p.y }; }; // OL space (bottom-left) for cameras
  const hitMarker = (ol) => { let best = 14 / ds; let hit = null; placements.forEach((p) => { const d = Math.hypot(p.x - ol.x, p.y - ol.y); if (d < best) { best = d; hit = p; } }); return hit; };
  const nearestSeg = (im) => { let idx = -1; let best = 8 / ds; segsRef.current.forEach((s, i) => { const dd = dist2seg(im.x, im.y, s); if (dd < best) { best = dd; idx = i; } }); return idx; };
  // Topmost stair whose footprint contains the point (last drawn wins, matching paint order).
  const hitStair = (im) => { for (let i = stairsRef.current.length - 1; i >= 0; i--) { if (pointInRotatedRect(im.x, im.y, stairsRef.current[i])) return i; } return -1; };
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
  const hitOpening = (list, im, swings) => { for (let i = list.length - 1; i >= 0; i--) { if (pointInOpening(im, list[i], swings)) return i; } return -1; };
  const hitDoor = (im) => hitOpening(doorsRef.current, im, kind === KIND_BUILDING);
  const hitWindow = (im) => hitOpening(windowsRef.current, im, false);
  const hitParking = (im) => { for (let i = parkingRef.current.length - 1; i >= 0; i--) { if (pointInRotatedRect(im.x, im.y, parkingRef.current[i])) return i; } return -1; };
  const hitPlatform = (im) => { for (let i = platsRef.current.length - 1; i >= 0; i--) { if (pointInRotatedRect(im.x, im.y, platsRef.current[i])) return i; } return -1; };
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

  // xfObjects applies ONE transform to a snapshot of the selection and returns the new geometry,
  // keyed the same way. The drag preview and the commit both call this, so what you see while
  // dragging is by construction what gets saved.
  //
  // Each kind takes the transform in the way that makes sense for it: a wall's two endpoints are
  // mapped; an opening's centre is mapped, its angle picks up the rotation and its width the stretch
  // along its own axis; a footprint keeps its size in its own frame and gains the rotation; a camera
  // marker is mapped through OL's y-up frame and its heading turns with the plan.
  const xfObjects = (orig, xf) => {
    const out = { segs: new Map(), doors: new Map(), wins: new Map(), stairs: new Map(), parks: new Map(), plats: new Map(), cams: new Map() };
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
    const orig = { segs: new Map(), doors: new Map(), wins: new Map(), stairs: new Map(), parks: new Map(), plats: new Map(), cams: new Map() };
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

  const onPointerDown = (e) => {
    if (e.button !== 0) return; e.preventDefault();
    const im = evImg(e);
    if (tool === 'erase') {
      const di = hitDoor(im); if (di >= 0) { commit({ doors: doorsRef.current.filter((_, k) => k !== di) }); return; }
      const wi = hitWindow(im); if (wi >= 0) { commit({ windows: windowsRef.current.filter((_, k) => k !== wi) }); return; }
      const i = nearestSeg(im); if (i >= 0) { commit({ segs: segsRef.current.filter((_, k) => k !== i) }); return; }
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
      cursorRef.current = null; redraw(); return;
    }
    if (DRAG_TOOLS.has(tool) || tool === 'door' || tool === 'window') { const p = snapPt(im.x, im.y); cursorRef.current = p; const d = draftRef.current; if (d && d.start) d.cur = p; redraw(); }
  };
  const onPointerUp = (e) => {
    if (povRef.current) { povRef.current = null; redraw(); return; } // the save is already debounced by onAim
    if (moveRef.current) {
      const d = moveRef.current; moveRef.current = null;
      if (!d.moved) { redraw(); return; }
      // Exactly the geometry the preview was drawing — same function, same transform.
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
      if (Object.keys(patch).length) commit(patch);
      next.cams.forEach((o, id) => {
        onMove(id, Math.max(0, Math.min(w, o.x)), Math.max(0, Math.min(h, o.y)));
        // A rotation turns what a camera is pointing at, so its heading turns with it.
        if (d.xf.ang) onAim(id, { heading: ((o.heading % 360) + 360) % 360 });
      });
      redraw(); return;
    }
    if (marqueeRef.current) {
      const mq = marqueeRef.current; marqueeRef.current = null;
      const rx = Math.min(mq.x1, mq.x2); const ry = Math.min(mq.y1, mq.y2); const rX = Math.max(mq.x1, mq.x2); const rY = Math.max(mq.y1, mq.y2);
      const inBox = (x, y) => x >= rx && x <= rX && y >= ry && y <= rY;
      // A footprint counts as swept when the band OVERLAPS it, not only when its centre is inside —
      // otherwise dragging a band across a large car park would miss the thing you drew it over.
      const overlaps = (r) => Math.min(r.x1, r.x2) <= rX && Math.max(r.x1, r.x2) >= rx && Math.min(r.y1, r.y2) <= rY && Math.max(r.y1, r.y2) >= ry;
      const found = [];
      segsRef.current.forEach((s, i) => { if (inBox(s.x1, s.y1) || inBox(s.x2, s.y2) || inBox((s.x1 + s.x2) / 2, (s.y1 + s.y2) / 2)) found.push(selKey('seg', i)); });
      doorsRef.current.forEach((d, i) => { if (inBox(d.cx, d.cy)) found.push(selKey('door', i)); });
      windowsRef.current.forEach((d, i) => { if (inBox(d.cx, d.cy)) found.push(selKey('win', i)); });
      stairsRef.current.forEach((s, i) => { if (overlaps(s)) found.push(selKey('stair', i)); });
      parkingRef.current.forEach((p, i) => { if (overlaps(p)) found.push(selKey('park', i)); });
      platsRef.current.forEach((p, i) => { if (overlaps(p)) found.push(selKey('plat', i)); });
      // Markers are OL space (y up); the band is image space.
      placements.forEach((p) => { if (inBox(p.x, h - p.y)) found.push(selKey('cam', p.id)); });
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
  const onDoubleClick = (e) => { if (tool === 'wall') { e.preventDefault(); finishWall(); } };
  const onDrop = (e) => { e.preventDefault(); const raw = e.dataTransfer.getData('text/placement'); if (!raw) return; let payload; try { payload = JSON.parse(raw); } catch (_) { return; } const ol = evOL(e); onPlace(payload, ol.x, ol.y); if (onClearPlacing) onClearPlacing(); };

  useEffect(() => {
    const onKey = (e) => {
      if (e.target && (e.target.tagName === 'INPUT' || e.target.tagName === 'TEXTAREA')) return;
      if (mode !== '2d') return;
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
    // Capture phase: this runs before the host dialog's (bubble-phase) window Escape handler, so a
    // consumed Escape never reaches it. Delete/Ctrl+Z are unaffected (they don't stop propagation).
    window.addEventListener('keydown', onKey, true);
    return () => window.removeEventListener('keydown', onKey, true);
  }, [mode, tool, selection, placements, deleteSelection, copySelection, cutSelection, pasteClipboard, finishWall, undo, redo, redraw, commit, clearSel]);

  const sel = selId ? placements.find((p) => p.id === selId) : null;
  const selStairObj = selStair >= 0 ? stairsRef.current[selStair] : null;
  const selDoorObj = selDoor >= 0 ? doorsRef.current[selDoor] : null;
  const selWinObj = selWin >= 0 ? windowsRef.current[selWin] : null;
  const selParkObj = selPark >= 0 ? parkingRef.current[selPark] : null;
  const selPlatObj = selPlat >= 0 ? platsRef.current[selPlat] : null;
  const rotateStair = () => { if (selStair < 0) return; const next = stairsRef.current.map((s, i) => (i === selStair ? { ...s, dir: rotateDir(s.dir) } : s)); commit({ stairs: next }); };
  const setSteps = (n) => { stairsRef.current = stairsRef.current.map((s, i) => (i === selStair ? { ...s, steps: n } : s)); redraw(); scheduleSave(); };
  const setStairH = (m) => { stairsRef.current = stairsRef.current.map((s, i) => (i === selStair ? { ...s, height: m } : s)); redraw(); scheduleSave(); };
  // Flip a stair between going UP and going DOWN (a descent to a lower level / basement).
  const toggleStairDown = () => { if (selStair < 0) return; commit({ stairs: stairsRef.current.map((s, i) => (i === selStair ? { ...s, down: !s.down } : s)) }); };
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
  // Slider drags update in place (no per-tick history); pushHistory once on grab keeps it undoable.
  const setDoorWidth = (px) => { doorsRef.current = doorsRef.current.map((d, i) => (i === selDoor ? { ...d, w: px } : d)); redraw(); scheduleSave(); };
  // Toggle a door's hinge-side (hf) or swing-side (sf) mirror. One history step per flip, so it undoes.
  const toggleDoor = (key) => { if (selDoor < 0) return; commit({ doors: doorsRef.current.map((d, i) => (i === selDoor ? { ...d, [key]: !d[key] } : d)) }); };
  const patchWindow = (patch) => { windowsRef.current = windowsRef.current.map((d, i) => (i === selWin ? { ...d, ...patch } : d)); redraw(); scheduleSave(); };
  const setBays = (n) => { parkingRef.current = parkingRef.current.map((p, i) => (i === selPark ? { ...p, bays: n } : p)); redraw(); scheduleSave(); };
  const setRise = (m) => { platsRef.current = platsRef.current.map((p, i) => (i === selPlat ? { ...p, rise: m } : p)); redraw(); scheduleSave(); };
  const draftFloor = { ...floor, grid: JSON.stringify(modelJSON()), scale, wallHeight: +wallHeight || 0 };
  const toolHint = (id) => t(faceOf(kind, id).hint);
  // Tool buttons are icon-only (label in the tooltip) so the vertical palette stays narrow. Both the
  // icon and the label come from the per-kind face, so the same `wall` tool reads "Wall" inside a
  // building and "Fence" on a park.
  const toolBtn = (id) => {
    const f = faceOf(kind, id);
    return (
      <button
        key={id}
        type="button"
        className={tool === id ? 'active' : ''}
        onClick={() => { setTool(id); draftRef.current = null; if (id !== 'select') clearSel(); }}
        title={t(f.key)}
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
      ) : selStairObj ? (
        <div className="fed-inspector">
          <div className="fed-inspector-title"><Ico n="stairs" sz={13} /> {t('grid.stairs')}</div>
          <div className="grid-readout">
            <div><span>{t('grid.ascent')}</span><strong>{{ n: '↑', e: '→', s: '↓', w: '←' }[selStairObj.dir] || '↑'}</strong></div>
            {stairBaseH(selStairObj) > 0 ? <div><span>{t('grid.restsOn')}</span><strong>+{stairBaseH(selStairObj).toFixed(2)} m</strong></div> : null}
          </div>
          {/* Height: the flight's OWN climb, kept whether it sits on the floor or on a raised floor,
              so a stair on a platform is the same flight, just lifted. Steps: its tread-line count. */}
          <label className="grid-field"><span>{t('grid.height')}</span><input type="range" min={STAIR_MIN_H} max={STAIR_MAX_H} step="0.05" value={stairClimbH(selStairObj)} onPointerDown={pushHistory} onChange={(e) => setStairH(+e.target.value)} /><em>{stairClimbH(selStairObj).toFixed(2)} m</em></label>
          <label className="grid-field"><span>{t('grid.steps')}</span><input type="range" min={STAIR_MIN_STEPS} max={STAIR_MAX_STEPS} step="1" value={stairSteps(selStairObj)} onPointerDown={pushHistory} onChange={(e) => setSteps(+e.target.value)} /><em>{stairSteps(selStairObj)}</em></label>
          <button type="button" className={`quiet fed-remove${selStairObj.down ? ' active' : ''}`} onClick={toggleStairDown}><span className="btn-icon"><Ico n={selStairObj.down ? 'arr-down' : 'arr-up'} sz={13} /> {selStairObj.down ? t('grid.goesDown') : t('grid.goesUp')}</span></button>
          <button type="button" className="quiet fed-remove" onClick={rotateStair}><span className="btn-icon"><Ico n="rotate-cw" sz={13} /> {t('grid.rotateAscent')}</span></button>
          <button type="button" className="quiet danger-text fed-remove" onClick={deleteSelection}><span className="btn-icon"><Ico n="trash" sz={13} /> {t('grid.erase')}</span></button>
        </div>
      ) : selDoorObj ? (
        <div className="fed-inspector">
          <div className="fed-inspector-title"><Ico n={faceOf(kind, 'door').icon} sz={13} /> {t(faceOf(kind, 'door').key)}</div>
          <label className="grid-field"><span>{t('grid.doorWidth')}</span><input type="range" min={Math.round(unit * 0.5)} max={Math.round(unit * 6)} step="1" value={Math.round(selDoorObj.w)} onPointerDown={pushHistory} onChange={(e) => setDoorWidth(+e.target.value)} /><em>{scale > 0 ? `${(selDoorObj.w * scale).toFixed(2)} m` : `${Math.round(selDoorObj.w)} px`}</em></label>
          {/* A door's hand: which end the hinge is on, and which way it swings. Only a building door
              draws a leaf and swing, so the flips are shown there — a gate is a symmetric barred
              opening with no hand to set. */}
          {kind === KIND_BUILDING ? (
            <div className="fe-btn-row">
              <button type="button" className={`quiet fed-remove${selDoorObj.hf ? ' active' : ''}`} onClick={() => toggleDoor('hf')}><span className="btn-icon"><Ico n="flip-h" sz={13} /> {t('grid.flipHinge')}</span></button>
              <button type="button" className={`quiet fed-remove${selDoorObj.sf ? ' active' : ''}`} onClick={() => toggleDoor('sf')}><span className="btn-icon"><Ico n="flip-v" sz={13} /> {t('grid.flipSwing')}</span></button>
            </div>
          ) : null}
          <button type="button" className="quiet danger-text fed-remove" onClick={deleteSelection}><span className="btn-icon"><Ico n="trash" sz={13} /> {t('grid.erase')}</span></button>
        </div>
      ) : selWinObj ? (
        <div className="fed-inspector">
          <div className="fed-inspector-title"><Ico n="window" sz={13} /> {t('grid.window')}</div>
          <label className="grid-field"><span>{t('grid.doorWidth')}</span><input type="range" min={Math.round(unit * 0.5)} max={Math.round(unit * 8)} step="1" value={Math.round(selWinObj.w)} onPointerDown={pushHistory} onChange={(e) => patchWindow({ w: +e.target.value })} /><em>{scale > 0 ? `${(selWinObj.w * scale).toFixed(2)} m` : `${Math.round(selWinObj.w)} px`}</em></label>
          {/* Sill and head are what make a window a window rather than a door: wall remains below
              and above, which is exactly what decides whether a camera can see through it. */}
          <label className="grid-field"><span>{t('grid.sill')}</span><input type="range" min="0" max="2.5" step="0.05" value={sillOf(selWinObj)} onPointerDown={pushHistory} onChange={(e) => patchWindow({ sill: +e.target.value })} /><em>{sillOf(selWinObj).toFixed(2)} m</em></label>
          <label className="grid-field"><span>{t('grid.head')}</span><input type="range" min="0.5" max="4" step="0.05" value={headOf(selWinObj)} onPointerDown={pushHistory} onChange={(e) => patchWindow({ head: +e.target.value })} /><em>{headOf(selWinObj).toFixed(2)} m</em></label>
          <button type="button" className="quiet danger-text fed-remove" onClick={deleteSelection}><span className="btn-icon"><Ico n="trash" sz={13} /> {t('grid.erase')}</span></button>
        </div>
      ) : selParkObj ? (
        <div className="fed-inspector">
          <div className="fed-inspector-title"><Ico n="parking" sz={13} /> {t('grid.parking')}</div>
          <label className="grid-field"><span>{t('grid.bays')}</span><input type="range" min="1" max="60" step="1" value={Math.max(1, selParkObj.bays || 1)} onPointerDown={pushHistory} onChange={(e) => setBays(+e.target.value)} /><em>{Math.max(1, selParkObj.bays || 1)}</em></label>
          <div className="grid-readout"><div><span>{t('grid.bayWidth')}</span><strong>{scale > 0 ? `${(((baysAcrossX(selParkObj) ? selParkObj.x2 - selParkObj.x1 : selParkObj.y2 - selParkObj.y1) * scale) / Math.max(1, selParkObj.bays || 1)).toFixed(2)} m` : '—'}</strong></div></div>
          <button type="button" className="quiet danger-text fed-remove" onClick={deleteSelection}><span className="btn-icon"><Ico n="trash" sz={13} /> {t('grid.erase')}</span></button>
        </div>
      ) : selPlatObj ? (
        <div className="fed-inspector">
          <div className="fed-inspector-title"><Ico n="platform" sz={13} /> {t('grid.platform')}</div>
          {/* Rise: how far this floor sits above the surrounding floor. Combine with a stair to reach it. */}
          <label className="grid-field"><span>{t('grid.rise')}</span><input type="range" min={RISE_MIN} max={RISE_MAX} step="0.05" value={selPlatObj.rise > 0 ? selPlatObj.rise : DEF_RISE} onPointerDown={pushHistory} onChange={(e) => setRise(+e.target.value)} /><em>{(selPlatObj.rise > 0 ? selPlatObj.rise : DEF_RISE).toFixed(2)} m</em></label>
          <button type="button" className="quiet danger-text fed-remove" onClick={deleteSelection}><span className="btn-icon"><Ico n="trash" sz={13} /> {t('grid.erase')}</span></button>
        </div>
      ) : (
        <div className="fed-inspector">
          <label className="grid-field"><span>{t('grid.cellSize')}</span><span className="grid-input-row"><input type="number" min="0.1" step="0.1" value={cellMeters} onChange={(e) => setCellMeters(Math.max(0, +e.target.value || 0))} /><em>{t('grid.metres')}</em></span></label>
          <label className="grid-field"><span>{t('grid.wallHeight')}</span><span className="grid-input-row"><input type="number" min="0.5" step="0.1" value={wallHeight} onChange={(e) => setWallHeight(Math.max(0, +e.target.value || 0))} /><em>{t('grid.metres')}</em></span></label>
          <div className="grid-readout">
            <div><span>{t(kind === KIND_OUTDOOR ? 'grid.fences' : 'grid.walls')}</span><strong>{segsRef.current.length}</strong></div>
            {doorsRef.current.length ? <div><span>{t(kind === KIND_OUTDOOR ? 'grid.gates' : 'grid.doors')}</span><strong>{doorsRef.current.length}</strong></div> : null}
            {windowsRef.current.length ? <div><span>{t('grid.windows')}</span><strong>{windowsRef.current.length}</strong></div> : null}
            {stairsRef.current.length ? <div><span>{t('grid.stairs')}</span><strong>{stairsRef.current.length}</strong></div> : null}
            {parkingRef.current.length ? <div><span>{t('grid.bays')}</span><strong>{parkingRef.current.reduce((n, p) => n + Math.max(1, p.bays || 1), 0)}</strong></div> : null}
            {platsRef.current.length ? <div><span>{t('grid.platforms')}</span><strong>{platsRef.current.length}</strong></div> : null}
            <div><span>{t('map.cameras')}</span><strong>{placements.filter((p) => p.cameraId).length}</strong></div>
          </div>
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
      <div key={name} className={`fe-panel ${name === 'toolbar' ? 'fe-toolbar' : 'fe-inspector'}${floating ? ' fe-float' : ''}`} style={floating ? { left: pos.x, top: pos.y } : undefined}>
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
          <div className="fe-toolbar-row">
            <button type="button" className="fd-tool" onClick={() => setZoom((z) => clampZoom(z / 1.2))} disabled={zoom <= 0.25} title={t('grid.zoomOut')} aria-label={t('grid.zoomOut')}><Ico n="zoom-out" sz={14} /></button>
            <button type="button" className="fd-tool" onClick={() => setZoom((z) => clampZoom(z * 1.2))} disabled={zoom >= 6} title={t('grid.zoomIn')} aria-label={t('grid.zoomIn')}><Ico n="zoom-in" sz={14} /></button>
          </div>
          <button type="button" className="fd-tool fe-zoom-readout" onClick={() => setZoom(1)} title={t('grid.zoomFit')}>{Math.round(zoom * 100)}%</button>
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

  const items = { toolbar: toolbarPanel, props: propsPanel };
  const names = mode === '2d' ? ['toolbar', 'props'] : ['toolbar'];
  const leftEls = names.filter((n) => dock[n] === 'left').map((n) => items[n]);
  const rightEls = names.filter((n) => dock[n] === 'right').map((n) => items[n]);
  const floatEls = names.filter((n) => dock[n] === 'float').map((n) => items[n]);

  return (
    <div className="floor-editor" ref={editorRef}>
      <div className="floor-editor-main">
        {leftEls.length ? <div className="fe-dock fe-dock-left">{leftEls}</div> : null}
        <div className="floor-editor-stage">
          {mode === '2d' ? (
            <div className="floor-editor-canvas-wrap" ref={wrapRef} style={{ overflow: zoom > 1 ? 'auto' : 'hidden' }} onDragOver={(e) => { e.preventDefault(); e.dataTransfer.dropEffect = 'copy'; }} onDrop={onDrop}>
              <canvas ref={canvasRef} width={cssW} height={cssH} className={`grid-canvas tool-${tool}${placing ? ' placing' : ''}`} style={{ width: cssW, height: cssH, touchAction: 'none' }}
                onPointerDown={onPointerDown} onPointerMove={onPointerMove} onPointerUp={onPointerUp} onPointerLeave={() => { cursorRef.current = null; hoverRef.current = -1; if (!moveRef.current) redraw(); }} onDoubleClick={onDoubleClick} />
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
  busy: PropTypes.bool,
};
