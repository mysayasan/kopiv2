import { lazy, Suspense, useCallback, useEffect, useLayoutEffect, useRef, useState } from 'react';
import PropTypes from 'prop-types';
import { useT, Ico } from '@shared';
import { apiBase } from '../lib/helpers';
import { nodeTone, TONES } from '../lib/fleet_status';

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
const COLS_TARGET = 28;

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

// doorSpanOnSeg returns the [t0,t1] parameter interval of a door's opening along wall segment s
// (or null if the door does not sit on this wall). A door is "on" s when its centre projects close
// to the line AND runs roughly parallel to it, so a door on one wall never cuts a wall crossing it.
function doorSpanOnSeg(s, d, tol) {
  const vx = s.x2 - s.x1; const vy = s.y2 - s.y1; const len2 = vx * vx + vy * vy; if (len2 < 1e-6) return null;
  const len = Math.sqrt(len2);
  const t = ((d.cx - s.x1) * vx + (d.cy - s.y1) * vy) / len2;
  const px = s.x1 + t * vx; const py = s.y1 + t * vy;
  if (Math.hypot(d.cx - px, d.cy - py) > tol) return null;
  let da = Math.abs((Math.atan2(vy, vx) - d.a) % Math.PI); da = Math.min(da, Math.PI - da);
  if (da > 0.35) return null;
  const half = (d.w / 2) / len; const t0 = t - half; const t1 = t + half;
  if (t1 <= 0 || t0 >= 1) return null;
  return [Math.max(0, t0), Math.min(1, t1)];
}
// carveSeg splits wall segment s into the sub-segments that remain once every door opening on it is
// removed — the pieces of wall actually drawn/extruded, leaving a gap you can pass through.
function carveSeg(s, doors, tol) {
  const spans = [];
  doors.forEach((d) => { const iv = doorSpanOnSeg(s, d, tol); if (iv) spans.push(iv); });
  if (!spans.length) return [s];
  spans.sort((a, b) => a[0] - b[0]);
  const merged = [];
  spans.forEach((sp) => { const last = merged[merged.length - 1]; if (!last || sp[0] > last[1]) merged.push([sp[0], sp[1]]); else last[1] = Math.max(last[1], sp[1]); });
  const at = (tt) => ({ x: s.x1 + (s.x2 - s.x1) * tt, y: s.y1 + (s.y2 - s.y1) * tt });
  const pieces = []; let cursor = 0;
  merged.forEach(([a, b]) => { if (a > cursor + 1e-4) { const p = at(cursor); const q = at(a); pieces.push({ x1: p.x, y1: p.y, x2: q.x, y2: q.y }); } cursor = Math.max(cursor, b); });
  if (cursor < 1 - 1e-4) { const p = at(cursor); const q = at(1); pieces.push({ x1: p.x, y1: p.y, x2: q.x, y2: q.y }); }
  return pieces;
}

const STAIR_DIRS = ['n', 'e', 's', 'w'];
function rotateDir(dir) { return STAIR_DIRS[(STAIR_DIRS.indexOf(dir) + 1) % 4] || 'n'; }
// Normalise a drag box to x1<x2, y1<y2 so downstream code never worries about drag direction.
function normStair(a, b, dir) { return { x1: Math.min(a.x, b.x), y1: Math.min(a.y, b.y), x2: Math.max(a.x, b.x), y2: Math.max(a.y, b.y), dir }; }
// Default ascent runs along the footprint's LONGER side (that is where a flight naturally goes):
// tall box climbs up the screen, wide box climbs to the right. The operator can rotate afterwards.
function defaultStairDir(a, b) { return Math.abs(b.y - a.y) >= Math.abs(b.x - a.x) ? 'n' : 'e'; }
function pointInStair(px, py, s) { return px >= s.x1 && px <= s.x2 && py >= s.y1 && py <= s.y2; }

export function FloorEditor({ floor, placements = [], nodesById = {}, placing, onPlace, onClearPlacing, onMove, onAim, onRemove, onSaveModel, onToast, busy }) {
  const t = useT();
  const editorRef = useRef(null); // the editor surface; floating panels are positioned within it
  const canvasRef = useRef(null);
  const imgRef = useRef(null);
  const segsRef = useRef([]);
  const stairsRef = useRef([]); // straight-flight stairs: { x1,y1,x2,y2 (image-space footprint), dir:'n'|'e'|'s'|'w' }
  const doorsRef = useRef([]); // openings in walls: { cx,cy (image-space centre on a wall), w (px), a (wall angle rad) }
  const histRef = useRef([]);
  const futRef = useRef([]);
  const draftRef = useRef(null); // wall {pts} · room/round/stairs {start,cur}
  const hoverRef = useRef(-1); // wall index under the erase cursor
  const hoverStairRef = useRef(-1); // stair index under the erase cursor
  const hoverDoorRef = useRef(-1); // door index under the erase cursor
  const cursorRef = useRef(null);
  const dragRef = useRef(null); // { id, x, y } while dragging a marker (OL coords)
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
  const [tool, setTool] = useState('select'); // select | wall | room | round | door | stairs | erase
  const toolRef = useRef(tool); toolRef.current = tool;
  const [selId, setSelId] = useState(null); // selected camera/node placement
  const [selSegs, setSelSegs] = useState(() => new Set()); // selected wall-segment indices (multi-select)
  const [selStair, setSelStair] = useState(-1); // selected stairs index (-1 = none)
  const [selDoor, setSelDoor] = useState(-1); // selected door index (-1 = none)
  const marqueeRef = useRef(null); // rubber-band box while selecting walls
  const dragSegsRef = useRef(null); // { sx, sy, dx, dy, moved, orig:Map } while moving selected walls

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

  // Seed walls (segment shape, or legacy cells) and stairs from the floor's model.
  useEffect(() => {
    let segs = [];
    if (existing && Array.isArray(existing.segments)) segs = existing.segments.map((s) => ({ x1: s.x1, y1: s.y1, x2: s.x2, y2: s.y2 }));
    else if (existing && Array.isArray(existing.walls)) { const cell = existing.cellPx || unit; segs = existing.walls.map(([c, r]) => ({ x1: c * cell, y1: (r + 0.5) * cell, x2: (c + 1) * cell, y2: (r + 0.5) * cell })); }
    segsRef.current = segs;
    stairsRef.current = existing && Array.isArray(existing.stairs) ? existing.stairs.map((s) => ({ x1: s.x1, y1: s.y1, x2: s.x2, y2: s.y2, dir: s.dir || 'n' })) : [];
    doorsRef.current = existing && Array.isArray(existing.doors) ? existing.doors.map((d) => ({ cx: d.cx, cy: d.cy, w: d.w, a: d.a || 0 })) : [];
    histRef.current = []; futRef.current = []; draftRef.current = null; setSelId(null); setSelSegs(new Set()); setSelStair(-1); setSelDoor(-1);
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

  // Debounced autosave of the wall model + scale + height.
  const scheduleSave = useCallback(() => {
    clearTimeout(saveTimer.current);
    saveTimer.current = setTimeout(() => {
      onSaveModel({ grid: JSON.stringify({ version: 2, unit, segments: segsRef.current, stairs: stairsRef.current, doors: doorsRef.current }), scale: scaleRef.current, wallHeight: +wallHeight || 0, elevation: (floor && floor.elevation) || 0 });
    }, 700);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [onSaveModel, unit, wallHeight, floor && floor.elevation]);
  useEffect(() => () => clearTimeout(saveTimer.current), []);

  // History snapshots walls, stairs AND doors so undo/redo restores the whole model in one step.
  const pushHistory = useCallback(() => { histRef.current.push({ segs: segsRef.current, stairs: stairsRef.current, doors: doorsRef.current }); if (histRef.current.length > 100) histRef.current.shift(); futRef.current = []; }, []);
  // Pass undefined for a list you are not changing (e.g. adding a stair leaves segsRef untouched).
  const commit = useCallback((nextSegs, nextStairs, nextDoors) => {
    pushHistory();
    if (nextSegs !== undefined) segsRef.current = nextSegs;
    if (nextStairs !== undefined) stairsRef.current = nextStairs;
    if (nextDoors !== undefined) doorsRef.current = nextDoors;
    redraw(); scheduleSave();
  }, [pushHistory, redraw, scheduleSave]);
  const undo = useCallback(() => { if (!histRef.current.length) return; futRef.current.push({ segs: segsRef.current, stairs: stairsRef.current, doors: doorsRef.current }); const p = histRef.current.pop(); segsRef.current = p.segs; stairsRef.current = p.stairs; doorsRef.current = p.doors || []; draftRef.current = null; redraw(); scheduleSave(); }, [redraw, scheduleSave]);
  const redo = useCallback(() => { if (!futRef.current.length) return; histRef.current.push({ segs: segsRef.current, stairs: stairsRef.current, doors: doorsRef.current }); const n = futRef.current.pop(); segsRef.current = n.segs; stairsRef.current = n.stairs; doorsRef.current = n.doors || []; redraw(); scheduleSave(); }, [redraw, scheduleSave]);
  // Re-save when scale/height change (but not on first mount).
  const firstRef = useRef(true);
  useEffect(() => { if (firstRef.current) { firstRef.current = false; return; } if (segsRef.current.length || stairsRef.current.length) scheduleSave(); /* eslint-disable-next-line */ }, [cellMeters, wallHeight]);

  const snapPt = useCallback((x, y) => {
    const mr = unit * 0.55; let best = null; let bestD = mr;
    segsRef.current.forEach((s) => { [[s.x1, s.y1], [s.x2, s.y2]].forEach(([ex, ey]) => { const d = Math.hypot(ex - x, ey - y); if (d < bestD) { bestD = d; best = { x: ex, y: ey }; } }); });
    if (best) return best;
    return { x: Math.max(0, Math.min(w, Math.round(x / unit) * unit)), y: Math.max(0, Math.min(h, Math.round(y / unit) * unit)) };
  }, [unit, w, h]);

  // ---- drawing ----
  const draw = useCallback(() => {
    const cv = canvasRef.current; if (!cv) return;
    const ctx = cv.getContext('2d');
    ctx.clearRect(0, 0, cssW, cssH);
    if (imgRef.current) { ctx.globalAlpha = 0.45; ctx.drawImage(imgRef.current, 0, 0, cssW, cssH); ctx.globalAlpha = 1; }
    else { ctx.fillStyle = '#f1f5f9'; ctx.fillRect(0, 0, cssW, cssH); }
    // grid dots
    ctx.fillStyle = 'rgba(100,116,139,0.3)';
    for (let x = 0; x <= w; x += unit) for (let y = 0; y <= h; y += unit) ctx.fillRect(x * ds - 0.5, y * ds - 0.5, 1, 1);

    const seg = (s, color, wd) => { ctx.strokeStyle = color; ctx.lineWidth = wd; ctx.lineCap = 'round'; ctx.beginPath(); ctx.moveTo(s.x1 * ds, s.y1 * ds); ctx.lineTo(s.x2 * ds, s.y2 * ds); ctx.stroke(); };
    const dot = (x, y, color, r = 3) => { ctx.fillStyle = color; ctx.beginPath(); ctx.arc(x, y, r, 0, Math.PI * 2); ctx.fill(); };

    // walls (image space) — selected walls are highlighted; a group drag previews the moved position.
    const dseg = dragSegsRef.current;
    segsRef.current.forEach((s, i) => {
      let ss = s;
      if (dseg && dseg.orig.has(i)) { const o = dseg.orig.get(i); ss = { x1: o.x1 + dseg.dx, y1: o.y1 + dseg.dy, x2: o.x2 + dseg.dx, y2: o.y2 + dseg.dy }; }
      const hov = toolRef.current === 'erase' && i === hoverRef.current;
      const selected = selSegs.has(i);
      const color = hov ? '#ef4444' : selected ? '#2d6cdf' : '#334155';
      // Draw the wall broken by any door openings on it, so the gap shows in 2D as it does in 3D.
      carveSeg(ss, doorsRef.current, unit * 0.6).forEach((pc) => seg(pc, color, hov || selected ? 6 : 5));
      dot(ss.x1 * ds, ss.y1 * ds, color, 2.5); dot(ss.x2 * ds, ss.y2 * ds, color, 2.5);
    });

    // doors — an opening in the wall drawn as the classic hinge + swing arc + leaf.
    doorsRef.current.forEach((d, i) => {
      const hov = toolRef.current === 'erase' && i === hoverDoorRef.current;
      const sel = i === selDoor;
      const col = hov ? '#ef4444' : sel ? '#2d6cdf' : '#b45309';
      const ux = Math.cos(d.a); const uy = Math.sin(d.a); const nx = -Math.sin(d.a); const ny = Math.cos(d.a);
      const half = d.w / 2;
      const hinge = { x: d.cx - ux * half, y: d.cy - uy * half };
      const latch = { x: d.cx + ux * half, y: d.cy + uy * half };
      const tip = { x: hinge.x + nx * d.w, y: hinge.y + ny * d.w };
      ctx.strokeStyle = col; ctx.lineWidth = sel || hov ? 2.5 : 2; ctx.lineCap = 'round';
      // jambs
      ctx.beginPath(); ctx.moveTo((hinge.x - nx * 3) * ds, (hinge.y - ny * 3) * ds); ctx.lineTo((hinge.x + nx * 3) * ds, (hinge.y + ny * 3) * ds);
      ctx.moveTo((latch.x - nx * 3) * ds, (latch.y - ny * 3) * ds); ctx.lineTo((latch.x + nx * 3) * ds, (latch.y + ny * 3) * ds); ctx.stroke();
      // leaf
      ctx.beginPath(); ctx.moveTo(hinge.x * ds, hinge.y * ds); ctx.lineTo(tip.x * ds, tip.y * ds); ctx.stroke();
      // swing arc from latch to the open leaf tip, centred on the hinge
      const a0 = Math.atan2(latch.y - hinge.y, latch.x - hinge.x); const a1 = Math.atan2(tip.y - hinge.y, tip.x - hinge.x);
      ctx.beginPath(); ctx.setLineDash([4, 3]); ctx.arc(hinge.x * ds, hinge.y * ds, d.w * ds, a0, a1, false); ctx.stroke(); ctx.setLineDash([]);
    });

    // stairs — footprint + step treads (perpendicular to ascent) + an arrow pointing up the flight.
    const nSteps = Math.max(3, Math.min(30, Math.round((+wallHeight || 2.7) / 0.18)));
    stairsRef.current.forEach((s, i) => {
      const hov = toolRef.current === 'erase' && i === hoverStairRef.current;
      const sel = i === selStair;
      const color = hov ? '#ef4444' : sel ? '#2d6cdf' : '#0f766e';
      const x1 = s.x1 * ds; const y1 = s.y1 * ds; const x2 = s.x2 * ds; const y2 = s.y2 * ds;
      ctx.fillStyle = hov ? 'rgba(239,68,68,0.10)' : sel ? 'rgba(45,108,223,0.12)' : 'rgba(15,118,110,0.12)';
      ctx.fillRect(x1, y1, x2 - x1, y2 - y1);
      ctx.strokeStyle = color; ctx.lineWidth = sel || hov ? 2.5 : 2; ctx.strokeRect(x1, y1, x2 - x1, y2 - y1);
      // treads run across the flight, spaced along the ascent axis
      const vertical = s.dir === 'n' || s.dir === 's';
      ctx.lineWidth = 1.25; ctx.beginPath();
      for (let k = 1; k < nSteps; k++) {
        const f = k / nSteps;
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
    });

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
      const dr = dragRef.current && dragRef.current.id === p.id ? dragRef.current : null;
      const px = dr ? dr.x : p.x; const py = dr ? dr.y : p.y;
      const sx = px * ds; const sy = (h - py) * ds;
      const tone = nodeTone(nodesById[p.nodeId], nowSecRef.current) || TONES.idle;
      const isCam = !!p.cameraId;
      if (isCam && (p.fov || 0) > 0) {
        const half = (p.fov || 0) / 2; const N = 22;
        ctx.beginPath(); ctx.moveTo(sx, sy);
        for (let i = 0; i <= N; i++) { const deg = (p.heading || 0) - half + (p.fov || 0) * (i / N); const a = (deg * Math.PI) / 180; ctx.lineTo(sx + rad * ds * Math.sin(a), sy - rad * ds * Math.cos(a)); }
        ctx.closePath(); ctx.fillStyle = `${tone.color}28`; ctx.fill(); ctx.strokeStyle = `${tone.color}88`; ctx.lineWidth = 1; ctx.stroke();
      }
      const selected = p.id === selId;
      ctx.beginPath(); ctx.arc(sx, sy, isCam ? 7 : 9, 0, Math.PI * 2);
      ctx.fillStyle = tone.color; ctx.fill();
      ctx.lineWidth = selected ? 3 : 2; ctx.strokeStyle = selected ? '#2d6cdf' : '#fff'; ctx.stroke();
      const label = p.lastKnownName || (isCam ? `Cam ${p.cameraId}` : (nodesById[p.nodeId]?.name || p.nodeId));
      ctx.font = '600 11px system-ui, sans-serif'; ctx.textAlign = 'center';
      ctx.lineWidth = 3; ctx.strokeStyle = 'rgba(255,255,255,0.85)'; ctx.strokeText(label, sx, sy - 13);
      ctx.fillStyle = '#1f2937'; ctx.fillText(label, sx, sy - 13); ctx.textAlign = 'left';
    });

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
    } else if (cur && (toolRef.current === 'wall' || toolRef.current === 'room' || toolRef.current === 'round' || toolRef.current === 'stairs')) dot(cur.x * ds, cur.y * ds, 'rgba(45,108,223,0.7)', 4);
  }, [cssW, cssH, w, h, unit, ds, placements, selId, selSegs, selStair, selDoor, wallHeight]);
  useEffect(() => { if (mode === '2d') draw(); });

  // ---- geometry helpers on the canvas ----
  const evImg = (e) => { const r = canvasRef.current.getBoundingClientRect(); return { x: (e.clientX - r.left) / ds, y: (e.clientY - r.top) / ds }; }; // image space (top-left)
  const evOL = (e) => { const p = evImg(e); return { x: p.x, y: h - p.y }; }; // OL space (bottom-left) for cameras
  const hitMarker = (ol) => { let best = 14 / ds; let hit = null; placements.forEach((p) => { const d = Math.hypot(p.x - ol.x, p.y - ol.y); if (d < best) { best = d; hit = p; } }); return hit; };
  const nearestSeg = (im) => { let idx = -1; let best = 8 / ds; segsRef.current.forEach((s, i) => { const dd = dist2seg(im.x, im.y, s); if (dd < best) { best = dd; idx = i; } }); return idx; };
  // Topmost stair whose footprint contains the point (last drawn wins, matching paint order).
  const hitStair = (im) => { for (let i = stairsRef.current.length - 1; i >= 0; i--) { if (pointInStair(im.x, im.y, stairsRef.current[i])) return i; } return -1; };
  const hitDoor = (im) => { for (let i = doorsRef.current.length - 1; i >= 0; i--) { const d = doorsRef.current[i]; if (Math.hypot(im.x - d.cx, im.y - d.cy) <= Math.max(d.w / 2, 10 / ds)) return i; } return -1; };
  // Nearest point on any wall to place a door on — returns the projected centre + the wall's angle.
  const nearestWallHit = (im) => {
    let best = 28 / ds; let hit = null;
    segsRef.current.forEach((s) => {
      const vx = s.x2 - s.x1; const vy = s.y2 - s.y1; const len2 = vx * vx + vy * vy || 1;
      let tt = ((im.x - s.x1) * vx + (im.y - s.y1) * vy) / len2; tt = Math.max(0, Math.min(1, tt));
      const cx = s.x1 + tt * vx; const cy = s.y1 + tt * vy; const dd = Math.hypot(im.x - cx, im.y - cy);
      if (dd < best) { best = dd; hit = { cx, cy, a: Math.atan2(vy, vx), len: Math.sqrt(len2) }; }
    });
    return hit;
  };

  const finishWall = useCallback(() => {
    const d = draftRef.current;
    if (d && d.pts && d.pts.length >= 2) { const add = []; for (let i = 0; i < d.pts.length - 1; i++) { const a = d.pts[i]; const b = d.pts[i + 1]; if (a.x !== b.x || a.y !== b.y) add.push({ x1: a.x, y1: a.y, x2: b.x, y2: b.y }); } if (add.length) commit(segsRef.current.concat(add)); }
    draftRef.current = null; redraw();
  }, [commit, redraw]);

  const onPointerDown = (e) => {
    if (e.button !== 0) return; e.preventDefault();
    const im = evImg(e);
    if (tool === 'erase') {
      const di = hitDoor(im); if (di >= 0) { commit(undefined, undefined, doorsRef.current.filter((_, k) => k !== di)); return; }
      const i = nearestSeg(im); if (i >= 0) { commit(segsRef.current.filter((_, k) => k !== i)); return; }
      const si = hitStair(im); if (si >= 0) commit(undefined, stairsRef.current.filter((_, k) => k !== si)); return;
    }
    if (tool === 'door') {
      const hit = nearestWallHit(im);
      if (hit) { const defW = scaleRef.current > 0 ? 0.9 / scaleRef.current : unit * 1.5; const dw = Math.max(unit, Math.min(defW, hit.len * 0.9)); const nd = doorsRef.current.length; commit(undefined, undefined, doorsRef.current.concat([{ cx: hit.cx, cy: hit.cy, w: dw, a: hit.a }])); setSelDoor(nd); }
      else if (onToast) onToast(t('grid.doorNeedsWall'), 'info'); // a door is an opening in a wall — there is nothing to open here
      return;
    }
    if (tool === 'wall') { const p = snapPt(im.x, im.y); if (!draftRef.current || !draftRef.current.pts) draftRef.current = { pts: [p] }; else { const pts = draftRef.current.pts; const last = pts[pts.length - 1]; if (last.x !== p.x || last.y !== p.y) pts.push(p); } cursorRef.current = p; redraw(); return; }
    if (tool === 'room' || tool === 'round' || tool === 'stairs') { const p = snapPt(im.x, im.y); draftRef.current = { start: p, cur: p, round: tool === 'round', stairs: tool === 'stairs' }; cursorRef.current = p; try { canvasRef.current.setPointerCapture(e.pointerId); } catch (_) { /* ignore */ } redraw(); return; }
    // select tool
    const ol = evOL(e);
    if (placingRef.current) { onPlace(placingRef.current, ol.x, ol.y); if (onClearPlacing) onClearPlacing(); return; }
    const hit = hitMarker(ol);
    const cap = () => { try { canvasRef.current.setPointerCapture(e.pointerId); } catch (_) { /* ignore */ } };
    if (hit) { setSelId(hit.id); setSelSegs(new Set()); setSelStair(-1); setSelDoor(-1); dragRef.current = { id: hit.id, x: hit.x, y: hit.y, moved: false }; cap(); return; }
    // a door is a small target sitting on a wall, so it is checked before the wall it lives in.
    const dOnWall = hitDoor(im);
    if (dOnWall >= 0) { setSelDoor(dOnWall); setSelId(null); setSelSegs(new Set()); setSelStair(-1); redraw(); return; }
    const idx = nearestSeg(im);
    if (idx >= 0) {
      setSelId(null); setSelStair(-1); setSelDoor(-1);
      let ns;
      if (e.shiftKey) { ns = new Set(selSegs); if (ns.has(idx)) ns.delete(idx); else ns.add(idx); }
      else ns = selSegs.has(idx) ? selSegs : new Set([idx]);
      setSelSegs(ns);
      const orig = new Map(); ns.forEach((k) => { const s = segsRef.current[k]; if (s) orig.set(k, { ...s }); });
      dragSegsRef.current = { sx: im.x, sy: im.y, dx: 0, dy: 0, moved: false, orig };
      cap(); redraw(); return;
    }
    // a stair footprint is a filled area, checked after thin walls so a wall drawn across it stays reachable
    const si = hitStair(im);
    if (si >= 0) { setSelStair(si); setSelId(null); setSelSegs(new Set()); setSelDoor(-1); redraw(); return; }
    // empty space → rubber-band select
    if (!e.shiftKey) { setSelSegs(new Set()); setSelId(null); }
    setSelStair(-1); setSelDoor(-1);
    marqueeRef.current = { x1: im.x, y1: im.y, x2: im.x, y2: im.y };
    cap(); redraw();
  };
  const onPointerMove = (e) => {
    const im = evImg(e);
    if (dragRef.current) { const ol = evOL(e); dragRef.current = { ...dragRef.current, x: Math.max(0, Math.min(w, ol.x)), y: Math.max(0, Math.min(h, ol.y)), moved: true }; redraw(); return; }
    if (dragSegsRef.current) { const d = dragSegsRef.current; d.dx = Math.round((im.x - d.sx) / unit) * unit; d.dy = Math.round((im.y - d.sy) / unit) * unit; if (d.dx || d.dy) d.moved = true; redraw(); return; }
    if (marqueeRef.current) { marqueeRef.current.x2 = im.x; marqueeRef.current.y2 = im.y; redraw(); return; }
    if (tool === 'erase') { const di = hitDoor(im); hoverDoorRef.current = di; hoverRef.current = di >= 0 ? -1 : nearestSeg(im); hoverStairRef.current = di >= 0 || hoverRef.current >= 0 ? -1 : hitStair(im); cursorRef.current = null; redraw(); return; }
    if (tool === 'wall' || tool === 'room' || tool === 'round' || tool === 'stairs' || tool === 'door') { const p = snapPt(im.x, im.y); cursorRef.current = p; const d = draftRef.current; if (d && d.start) d.cur = p; redraw(); }
  };
  const onPointerUp = (e) => {
    if (dragRef.current) { const dr = dragRef.current; dragRef.current = null; if (dr.moved) onMove(dr.id, dr.x, dr.y); redraw(); return; }
    if (dragSegsRef.current) {
      const d = dragSegsRef.current; dragSegsRef.current = null;
      if (d.moved) commit(segsRef.current.map((s, i) => (d.orig.has(i) ? { x1: d.orig.get(i).x1 + d.dx, y1: d.orig.get(i).y1 + d.dy, x2: d.orig.get(i).x2 + d.dx, y2: d.orig.get(i).y2 + d.dy } : s)));
      else redraw();
      return;
    }
    if (marqueeRef.current) {
      const mq = marqueeRef.current; marqueeRef.current = null;
      const rx = Math.min(mq.x1, mq.x2); const ry = Math.min(mq.y1, mq.y2); const rX = Math.max(mq.x1, mq.x2); const rY = Math.max(mq.y1, mq.y2);
      const inBox = (x, y) => x >= rx && x <= rX && y >= ry && y <= rY;
      const found = [];
      segsRef.current.forEach((s, i) => { if (inBox(s.x1, s.y1) || inBox(s.x2, s.y2) || inBox((s.x1 + s.x2) / 2, (s.y1 + s.y2) / 2)) found.push(i); });
      setSelSegs((prev) => { const ns = e.shiftKey ? new Set(prev) : new Set(); found.forEach((i) => ns.add(i)); return ns; });
      redraw(); return;
    }
    if (tool === 'room' || tool === 'round' || tool === 'stairs') {
      const d = draftRef.current;
      if (d && d.start) {
        const a = d.start; const b = snapPt(evImg(e).x, evImg(e).y);
        if (d.stairs) {
          if (Math.abs(b.x - a.x) > unit && Math.abs(b.y - a.y) > unit) { const st = normStair(a, b, defaultStairDir(a, b)); const newIdx = stairsRef.current.length; commit(undefined, stairsRef.current.concat([st])); setSelStair(newIdx); }
        } else if (d.round) {
          const cx = (a.x + b.x) / 2; const cy = (a.y + b.y) / 2; const rx = Math.abs(b.x - a.x) / 2; const ry = Math.abs(b.y - a.y) / 2;
          if (rx > 1 && ry > 1) commit(segsRef.current.concat(roundSegs(cx, cy, rx, ry, unit)));
        } else {
          const x1 = Math.min(a.x, b.x); const y1 = Math.min(a.y, b.y); const x2 = Math.max(a.x, b.x); const y2 = Math.max(a.y, b.y);
          if (x2 - x1 > 1 && y2 - y1 > 1) commit(segsRef.current.concat([{ x1, y1, x2, y2: y1 }, { x1: x2, y1, x2, y2 }, { x1: x2, y1: y2, x2: x1, y2 }, { x1, y1: y2, x2: x1, y2: y1 }]));
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
        if (selSegs.size) { e.preventDefault(); commit(segsRef.current.filter((_, i) => !selSegs.has(i))); setSelSegs(new Set()); }
        else if (selStair >= 0) { e.preventDefault(); commit(undefined, stairsRef.current.filter((_, i) => i !== selStair)); setSelStair(-1); }
        else if (selDoor >= 0) { e.preventDefault(); commit(undefined, undefined, doorsRef.current.filter((_, i) => i !== selDoor)); setSelDoor(-1); }
        else if (selId) { e.preventDefault(); onRemove(selId); setSelId(null); }
      } else if (e.key === 'Enter' && tool === 'wall') finishWall();
      else if (e.key === 'Escape') {
        // Esc backs out one level: cancel an in-progress wall run (drop the uncommitted corners so
        // the chain stops) or clear a selection. Enter or double-click is how you finish and keep a
        // wall. When there IS something to cancel we consume the event here — otherwise the dialog
        // that hosts this editor also listens for Escape and would close the whole editor.
        const hadDraft = !!draftRef.current;
        const hadSel = !!selId || selSegs.size > 0 || selStair >= 0 || selDoor >= 0;
        if (hadDraft || hadSel) {
          e.preventDefault(); e.stopImmediatePropagation();
          draftRef.current = null; setSelId(null); setSelSegs(new Set()); setSelStair(-1); setSelDoor(-1); redraw();
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
  }, [mode, tool, selId, selSegs, selStair, selDoor, finishWall, undo, redo, redraw, onRemove, commit]);

  const sel = selId ? placements.find((p) => p.id === selId) : null;
  const selStairObj = selStair >= 0 ? stairsRef.current[selStair] : null;
  const selDoorObj = selDoor >= 0 ? doorsRef.current[selDoor] : null;
  const rotateStair = () => { if (selStair < 0) return; const next = stairsRef.current.map((s, i) => (i === selStair ? { ...s, dir: rotateDir(s.dir) } : s)); commit(undefined, next); };
  // Door width drags update in place (no per-tick history); pushHistory once on grab keeps it undoable.
  const setDoorWidth = (px) => { doorsRef.current = doorsRef.current.map((d, i) => (i === selDoor ? { ...d, w: px } : d)); redraw(); scheduleSave(); };
  const draftFloor = { ...floor, grid: JSON.stringify({ version: 2, unit, segments: segsRef.current, stairs: stairsRef.current, doors: doorsRef.current }), scale, wallHeight: +wallHeight || 0 };
  const TOOL_HINT = { select: t('fed.selectHint'), wall: t('grid.wallHint'), room: t('grid.roomHint'), round: t('grid.roundHint'), door: t('grid.doorHint'), stairs: t('grid.stairsHint'), erase: t('grid.eraseHint') };
  // Tool buttons are icon-only (label in the tooltip) so the vertical palette stays narrow.
  const toolBtn = (id, icon, labelKey) => (
    <button type="button" className={tool === id ? 'active' : ''} onClick={() => { setTool(id); draftRef.current = null; if (id !== 'select') { setSelId(null); setSelSegs(new Set()); setSelStair(-1); setSelDoor(-1); } }} title={t(labelKey)} aria-label={t(labelKey)}><Ico n={icon} sz={15} /></button>
  );

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
          <label className="grid-field"><span>{t('grid.aim')}</span><input type="range" min="0" max="359" step="1" value={Math.round(sel.heading || 0)} onChange={(e) => onAim(sel.id, { heading: +e.target.value })} /><em>{Math.round(sel.heading || 0)}°</em></label>
          <label className="grid-field"><span>{t('grid.fov')}</span><input type="range" min="0" max="170" step="1" value={Math.round(sel.fov || 0)} onChange={(e) => onAim(sel.id, { fov: +e.target.value })} /><em>{Math.round(sel.fov || 0)}°</em></label>
          <label className="grid-field"><span>{t('grid.mountHeight')}</span><input type="range" min="0.5" max="6" step="0.1" value={sel.mountHeight > 0 ? sel.mountHeight : 2.5} onChange={(e) => onAim(sel.id, { mountHeight: +e.target.value })} /><em>{(sel.mountHeight > 0 ? sel.mountHeight : 2.5).toFixed(1)} m</em></label>
          <label className="grid-field"><span>{t('grid.pitch')}</span><input type="range" min="0" max="90" step="1" value={Math.round(sel.pitch || 0)} onChange={(e) => onAim(sel.id, { pitch: +e.target.value })} /><em>{Math.round(sel.pitch || 0)}°</em></label>
          <button type="button" className="quiet danger-text fed-remove" onClick={() => { onRemove(sel.id); setSelId(null); }}><span className="btn-icon"><Ico n="trash" sz={13} /> {t('map.removeMarker')}</span></button>
        </div>
      ) : sel ? (
        <div className="fed-inspector">
          <div className="fed-inspector-title"><Ico n="cpu" sz={13} /> {sel.lastKnownName || nodesById[sel.nodeId]?.name || sel.nodeId}</div>
          <button type="button" className="quiet danger-text fed-remove" onClick={() => { onRemove(sel.id); setSelId(null); }}><span className="btn-icon"><Ico n="trash" sz={13} /> {t('map.removeMarker')}</span></button>
        </div>
      ) : selSegs.size ? (
        <div className="fed-inspector">
          <div className="fed-inspector-title"><Ico n="edit-2" sz={13} /> {t('fed.wallsSelected', { n: selSegs.size })}</div>
          <button type="button" className="quiet danger-text fed-remove" onClick={() => { commit(segsRef.current.filter((_, i) => !selSegs.has(i))); setSelSegs(new Set()); }}><span className="btn-icon"><Ico n="trash" sz={13} /> {t('grid.erase')}</span></button>
          <button type="button" className="quiet fed-remove" onClick={() => setSelSegs(new Set())}>{t('fed.deselect')}</button>
        </div>
      ) : selStairObj ? (
        <div className="fed-inspector">
          <div className="fed-inspector-title"><Ico n="stairs" sz={13} /> {t('grid.stairs')}</div>
          <div className="grid-readout"><div><span>{t('grid.ascent')}</span><strong>{{ n: '↑', e: '→', s: '↓', w: '←' }[selStairObj.dir] || '↑'}</strong></div></div>
          <button type="button" className="quiet fed-remove" onClick={rotateStair}><span className="btn-icon"><Ico n="rotate-cw" sz={13} /> {t('grid.rotateAscent')}</span></button>
          <button type="button" className="quiet danger-text fed-remove" onClick={() => { commit(undefined, stairsRef.current.filter((_, i) => i !== selStair)); setSelStair(-1); }}><span className="btn-icon"><Ico n="trash" sz={13} /> {t('grid.erase')}</span></button>
        </div>
      ) : selDoorObj ? (
        <div className="fed-inspector">
          <div className="fed-inspector-title"><Ico n="door" sz={13} /> {t('grid.door')}</div>
          <label className="grid-field"><span>{t('grid.doorWidth')}</span><input type="range" min={Math.round(unit * 0.5)} max={Math.round(unit * 6)} step="1" value={Math.round(selDoorObj.w)} onPointerDown={pushHistory} onChange={(e) => setDoorWidth(+e.target.value)} /><em>{scale > 0 ? `${(selDoorObj.w * scale).toFixed(2)} m` : `${Math.round(selDoorObj.w)} px`}</em></label>
          <button type="button" className="quiet danger-text fed-remove" onClick={() => { commit(undefined, undefined, doorsRef.current.filter((_, i) => i !== selDoor)); setSelDoor(-1); }}><span className="btn-icon"><Ico n="trash" sz={13} /> {t('grid.erase')}</span></button>
        </div>
      ) : (
        <div className="fed-inspector">
          <label className="grid-field"><span>{t('grid.cellSize')}</span><span className="grid-input-row"><input type="number" min="0.1" step="0.1" value={cellMeters} onChange={(e) => setCellMeters(Math.max(0, +e.target.value || 0))} /><em>{t('grid.metres')}</em></span></label>
          <label className="grid-field"><span>{t('grid.wallHeight')}</span><span className="grid-input-row"><input type="number" min="0.5" step="0.1" value={wallHeight} onChange={(e) => setWallHeight(Math.max(0, +e.target.value || 0))} /><em>{t('grid.metres')}</em></span></label>
          <div className="grid-readout">
            <div><span>{t('grid.walls')}</span><strong>{segsRef.current.length}</strong></div>
            {doorsRef.current.length ? <div><span>{t('grid.doors')}</span><strong>{doorsRef.current.length}</strong></div> : null}
            {stairsRef.current.length ? <div><span>{t('grid.stairs')}</span><strong>{stairsRef.current.length}</strong></div> : null}
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
          <div className="grid-tool-group" role="group" aria-label={t('grid.tool')}>
            {toolBtn('select', 'cursor', 'fed.select')}
            {toolBtn('wall', 'edit-2', 'grid.wall')}
            {toolBtn('room', 'grid2', 'grid.room')}
            {toolBtn('round', 'circle', 'grid.round')}
            {toolBtn('door', 'door', 'grid.door')}
            {toolBtn('stairs', 'stairs', 'grid.stairs')}
            {toolBtn('erase', 'trash', 'grid.erase')}
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
      <div className="fe-float-bar">{grip('props')}<span className="fe-float-hint">{TOOL_HINT[tool]}</span></div>
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
                onPointerDown={onPointerDown} onPointerMove={onPointerMove} onPointerUp={onPointerUp} onPointerLeave={() => { cursorRef.current = null; hoverRef.current = -1; if (!dragRef.current) redraw(); }} onDoubleClick={onDoubleClick} />
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
