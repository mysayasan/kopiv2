import { lazy, Suspense, useCallback, useEffect, useRef, useState } from 'react';
import PropTypes from 'prop-types';
import { useT, Ico } from '@shared';
import { apiBase } from '../lib/helpers';
import { nodeTone, TONES } from '../lib/fleet_status';

// three.js loads only when the operator flips to the 3D tab.
const Floor3D = lazy(() => import('./floor_3d'));

// FloorEditor is THE single floor editor: one canvas surface that does everything, with a 2D ⇄ 3D
// toggle in its own header.
//   • 2D tab — Select/Move (place & aim cameras dropped from the palette), Wall (draw walls, grid
//     snap, rubber-band, dbl-click/Enter finish), Room (drag a rectangle → four walls), Erase.
//     Undo/Redo for walls. A selected camera opens an inspector (aim, field of view, mount height,
//     tilt, remove).
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
function dist2seg(px, py, s) {
  const vx = s.x2 - s.x1; const vy = s.y2 - s.y1;
  const len2 = vx * vx + vy * vy || 1;
  let tt = ((px - s.x1) * vx + (py - s.y1) * vy) / len2;
  tt = Math.max(0, Math.min(1, tt));
  return Math.hypot(px - (s.x1 + tt * vx), py - (s.y1 + tt * vy));
}

export function FloorEditor({ floor, placements = [], nodesById = {}, placing, onPlace, onClearPlacing, onMove, onAim, onRemove, onSaveModel, busy }) {
  const t = useT();
  const canvasRef = useRef(null);
  const imgRef = useRef(null);
  const segsRef = useRef([]);
  const histRef = useRef([]);
  const futRef = useRef([]);
  const draftRef = useRef(null); // wall {pts} · room {start,cur}
  const hoverRef = useRef(-1);
  const cursorRef = useRef(null);
  const dragRef = useRef(null); // { id, x, y } while dragging a marker (OL coords)
  const saveTimer = useRef(null);
  const placingRef = useRef(placing); placingRef.current = placing;
  const [, tick] = useState(0);
  const redraw = useCallback(() => tick((n) => n + 1), []);
  const nowSecRef = useRef(Math.floor(Date.now() / 1000));

  const [mode, setMode] = useState('2d');
  const [tool, setTool] = useState('select'); // select | wall | room | erase
  const toolRef = useRef(tool); toolRef.current = tool;
  const [selId, setSelId] = useState(null); // selected camera/node placement
  const [selSegs, setSelSegs] = useState(() => new Set()); // selected wall-segment indices (multi-select)
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
  }, []);

  const ds = Math.min(stage.w / w, stage.h / h, 2);
  const cssW = Math.round(w * ds);
  const cssH = Math.round(h * ds);

  const [cellMeters, setCellMeters] = useState(() => { const s = floor && floor.scale > 0 ? floor.scale : 0; return s > 0 ? +(s * unit).toFixed(2) : 0.5; });
  const [wallHeight, setWallHeight] = useState(() => (floor && floor.wallHeight > 0 ? floor.wallHeight : 2.7));
  const scale = cellMeters > 0 ? cellMeters / unit : 0;
  const scaleRef = useRef(scale); scaleRef.current = scale;

  // Seed walls from the floor's model (segment shape, or legacy cells).
  useEffect(() => {
    let segs = [];
    if (existing && Array.isArray(existing.segments)) segs = existing.segments.map((s) => ({ x1: s.x1, y1: s.y1, x2: s.x2, y2: s.y2 }));
    else if (existing && Array.isArray(existing.walls)) { const cell = existing.cellPx || unit; segs = existing.walls.map(([c, r]) => ({ x1: c * cell, y1: (r + 0.5) * cell, x2: (c + 1) * cell, y2: (r + 0.5) * cell })); }
    segsRef.current = segs; histRef.current = []; futRef.current = []; draftRef.current = null; setSelId(null); setSelSegs(new Set());
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
      onSaveModel({ grid: JSON.stringify({ version: 2, unit, segments: segsRef.current }), scale: scaleRef.current, wallHeight: +wallHeight || 0, elevation: (floor && floor.elevation) || 0 });
    }, 700);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [onSaveModel, unit, wallHeight, floor && floor.elevation]);
  useEffect(() => () => clearTimeout(saveTimer.current), []);

  const commit = useCallback((next) => {
    histRef.current.push(segsRef.current);
    if (histRef.current.length > 100) histRef.current.shift();
    futRef.current = []; segsRef.current = next; redraw(); scheduleSave();
  }, [redraw, scheduleSave]);
  const undo = useCallback(() => { if (!histRef.current.length) return; futRef.current.push(segsRef.current); segsRef.current = histRef.current.pop(); draftRef.current = null; redraw(); scheduleSave(); }, [redraw, scheduleSave]);
  const redo = useCallback(() => { if (!futRef.current.length) return; histRef.current.push(segsRef.current); segsRef.current = futRef.current.pop(); redraw(); scheduleSave(); }, [redraw, scheduleSave]);
  // Re-save when scale/height change (but not on first mount).
  const firstRef = useRef(true);
  useEffect(() => { if (firstRef.current) { firstRef.current = false; return; } if (segsRef.current.length) scheduleSave(); /* eslint-disable-next-line */ }, [cellMeters, wallHeight]);

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
      seg(ss, hov ? '#ef4444' : selected ? '#2d6cdf' : '#334155', hov || selected ? 6 : 5);
      const dc = selected ? '#2d6cdf' : '#334155';
      dot(ss.x1 * ds, ss.y1 * ds, dc, 2.5); dot(ss.x2 * ds, ss.y2 * ds, dc, 2.5);
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
    } else if (d && d.start && cur) {
      const rx = Math.min(d.start.x, cur.x); const ry = Math.min(d.start.y, cur.y); const rw = Math.abs(cur.x - d.start.x); const rh = Math.abs(cur.y - d.start.y);
      ctx.strokeStyle = '#2d6cdf'; ctx.lineWidth = 4; ctx.strokeRect(rx * ds, ry * ds, rw * ds, rh * ds); ctx.fillStyle = 'rgba(45,108,223,0.12)'; ctx.fillRect(rx * ds, ry * ds, rw * ds, rh * ds);
      label(cur.x * ds, cur.y * ds, `${(rw * scaleRef.current).toFixed(1)} × ${(rh * scaleRef.current).toFixed(1)} m`);
    } else if (cur && (toolRef.current === 'wall' || toolRef.current === 'room')) dot(cur.x * ds, cur.y * ds, 'rgba(45,108,223,0.7)', 4);
  }, [cssW, cssH, w, h, unit, ds, placements, selId, selSegs]);
  useEffect(() => { if (mode === '2d') draw(); });

  // ---- geometry helpers on the canvas ----
  const evImg = (e) => { const r = canvasRef.current.getBoundingClientRect(); return { x: (e.clientX - r.left) / ds, y: (e.clientY - r.top) / ds }; }; // image space (top-left)
  const evOL = (e) => { const p = evImg(e); return { x: p.x, y: h - p.y }; }; // OL space (bottom-left) for cameras
  const hitMarker = (ol) => { let best = 14 / ds; let hit = null; placements.forEach((p) => { const d = Math.hypot(p.x - ol.x, p.y - ol.y); if (d < best) { best = d; hit = p; } }); return hit; };
  const nearestSeg = (im) => { let idx = -1; let best = 8 / ds; segsRef.current.forEach((s, i) => { const dd = dist2seg(im.x, im.y, s); if (dd < best) { best = dd; idx = i; } }); return idx; };

  const finishWall = useCallback(() => {
    const d = draftRef.current;
    if (d && d.pts && d.pts.length >= 2) { const add = []; for (let i = 0; i < d.pts.length - 1; i++) { const a = d.pts[i]; const b = d.pts[i + 1]; if (a.x !== b.x || a.y !== b.y) add.push({ x1: a.x, y1: a.y, x2: b.x, y2: b.y }); } if (add.length) commit(segsRef.current.concat(add)); }
    draftRef.current = null; redraw();
  }, [commit, redraw]);

  const onPointerDown = (e) => {
    if (e.button !== 0) return; e.preventDefault();
    const im = evImg(e);
    if (tool === 'erase') { const i = nearestSeg(im); if (i >= 0) commit(segsRef.current.filter((_, k) => k !== i)); return; }
    if (tool === 'wall') { const p = snapPt(im.x, im.y); if (!draftRef.current || !draftRef.current.pts) draftRef.current = { pts: [p] }; else { const pts = draftRef.current.pts; const last = pts[pts.length - 1]; if (last.x !== p.x || last.y !== p.y) pts.push(p); } cursorRef.current = p; redraw(); return; }
    if (tool === 'room') { const p = snapPt(im.x, im.y); draftRef.current = { start: p, cur: p }; cursorRef.current = p; try { canvasRef.current.setPointerCapture(e.pointerId); } catch (_) { /* ignore */ } redraw(); return; }
    // select tool
    const ol = evOL(e);
    if (placingRef.current) { onPlace(placingRef.current, ol.x, ol.y); if (onClearPlacing) onClearPlacing(); return; }
    const hit = hitMarker(ol);
    const cap = () => { try { canvasRef.current.setPointerCapture(e.pointerId); } catch (_) { /* ignore */ } };
    if (hit) { setSelId(hit.id); setSelSegs(new Set()); dragRef.current = { id: hit.id, x: hit.x, y: hit.y, moved: false }; cap(); return; }
    const idx = nearestSeg(im);
    if (idx >= 0) {
      setSelId(null);
      let ns;
      if (e.shiftKey) { ns = new Set(selSegs); if (ns.has(idx)) ns.delete(idx); else ns.add(idx); }
      else ns = selSegs.has(idx) ? selSegs : new Set([idx]);
      setSelSegs(ns);
      const orig = new Map(); ns.forEach((k) => { const s = segsRef.current[k]; if (s) orig.set(k, { ...s }); });
      dragSegsRef.current = { sx: im.x, sy: im.y, dx: 0, dy: 0, moved: false, orig };
      cap(); redraw(); return;
    }
    // empty space → rubber-band select
    if (!e.shiftKey) { setSelSegs(new Set()); setSelId(null); }
    marqueeRef.current = { x1: im.x, y1: im.y, x2: im.x, y2: im.y };
    cap(); redraw();
  };
  const onPointerMove = (e) => {
    const im = evImg(e);
    if (dragRef.current) { const ol = evOL(e); dragRef.current = { ...dragRef.current, x: Math.max(0, Math.min(w, ol.x)), y: Math.max(0, Math.min(h, ol.y)), moved: true }; redraw(); return; }
    if (dragSegsRef.current) { const d = dragSegsRef.current; d.dx = Math.round((im.x - d.sx) / unit) * unit; d.dy = Math.round((im.y - d.sy) / unit) * unit; if (d.dx || d.dy) d.moved = true; redraw(); return; }
    if (marqueeRef.current) { marqueeRef.current.x2 = im.x; marqueeRef.current.y2 = im.y; redraw(); return; }
    if (tool === 'erase') { hoverRef.current = nearestSeg(im); cursorRef.current = null; redraw(); return; }
    if (tool === 'wall' || tool === 'room') { const p = snapPt(im.x, im.y); cursorRef.current = p; const d = draftRef.current; if (d && d.start) d.cur = p; redraw(); }
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
    if (tool === 'room') { const d = draftRef.current; if (d && d.start) { const a = d.start; const b = snapPt(evImg(e).x, evImg(e).y); const x1 = Math.min(a.x, b.x); const y1 = Math.min(a.y, b.y); const x2 = Math.max(a.x, b.x); const y2 = Math.max(a.y, b.y); if (x2 - x1 > 1 && y2 - y1 > 1) commit(segsRef.current.concat([{ x1, y1, x2, y2: y1 }, { x1: x2, y1, x2, y2 }, { x1: x2, y1: y2, x2: x1, y2 }, { x1, y1: y2, x2: x1, y2: y1 }])); } draftRef.current = null; redraw(); }
  };
  const onDoubleClick = (e) => { if (tool === 'wall') { e.preventDefault(); finishWall(); } };
  const onDrop = (e) => { e.preventDefault(); const raw = e.dataTransfer.getData('text/placement'); if (!raw) return; let payload; try { payload = JSON.parse(raw); } catch (_) { return; } const ol = evOL(e); onPlace(payload, ol.x, ol.y); if (onClearPlacing) onClearPlacing(); };

  useEffect(() => {
    const onKey = (e) => {
      if (e.target && (e.target.tagName === 'INPUT' || e.target.tagName === 'TEXTAREA')) return;
      if (mode !== '2d') return;
      if (e.key === 'Delete' || e.key === 'Backspace') {
        if (selSegs.size) { e.preventDefault(); commit(segsRef.current.filter((_, i) => !selSegs.has(i))); setSelSegs(new Set()); }
        else if (selId) { e.preventDefault(); onRemove(selId); setSelId(null); }
      } else if (e.key === 'Enter' && tool === 'wall') finishWall();
      else if (e.key === 'Escape') { draftRef.current = null; setSelId(null); setSelSegs(new Set()); redraw(); }
      else if ((e.ctrlKey || e.metaKey) && (e.key === 'z' || e.key === 'Z')) { e.preventDefault(); if (e.shiftKey) redo(); else undo(); }
      else if ((e.ctrlKey || e.metaKey) && (e.key === 'y' || e.key === 'Y')) { e.preventDefault(); redo(); }
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, [mode, tool, selId, selSegs, finishWall, undo, redo, redraw, onRemove, commit]);

  const sel = selId ? placements.find((p) => p.id === selId) : null;
  const draftFloor = { ...floor, grid: JSON.stringify({ version: 2, unit, segments: segsRef.current }), scale, wallHeight: +wallHeight || 0 };
  const TOOL_HINT = { select: t('fed.selectHint'), wall: t('grid.wallHint'), room: t('grid.roomHint'), erase: t('grid.eraseHint') };
  const toolBtn = (id, icon, labelKey) => (
    <button type="button" className={tool === id ? 'active' : ''} onClick={() => { setTool(id); draftRef.current = null; if (id !== 'select') { setSelId(null); setSelSegs(new Set()); } }} title={t(labelKey)}><Ico n={icon} sz={13} /> {t(labelKey)}</button>
  );

  return (
    <div className="floor-editor">
      <div className="floor-editor-head">
        <div className="floor-editor-modes" role="group" aria-label={t('map.viewMode')}>
          <button type="button" className={mode === '2d' ? 'active' : ''} onClick={() => setMode('2d')}><Ico n="map" sz={13} /> {t('map.view2d')}</button>
          <button type="button" className={mode === '3d' ? 'active' : ''} onClick={() => setMode('3d')}><Ico n="box" sz={13} /> {t('map.view3d')}</button>
        </div>
        {mode === '2d' ? (
          <div className="floor-editor-tools">
            <div className="grid-tool-group" role="group" aria-label={t('grid.tool')}>
              {toolBtn('select', 'map-pin', 'fed.select')}
              {toolBtn('wall', 'edit-2', 'grid.wall')}
              {toolBtn('room', 'grid2', 'grid.room')}
              {toolBtn('erase', 'trash', 'grid.erase')}
            </div>
            <span className="fd-toolbar-sep" />
            <button type="button" className="fd-tool" onClick={undo} disabled={!histRef.current.length} title={t('grid.undo')}><Ico n="undo" sz={14} /></button>
            <button type="button" className="fd-tool" onClick={redo} disabled={!futRef.current.length} title={t('grid.redo')}><Ico n="redo" sz={14} /></button>
            {draftRef.current && draftRef.current.pts ? <button type="button" className="fd-tool" onClick={finishWall}><Ico n="check-ok" sz={14} /> {t('grid.finish')}</button> : null}
            <span className="grid-toolbar-hint">{TOOL_HINT[tool]}</span>
          </div>
        ) : <span className="grid-toolbar-hint floor-editor-tools">{t('grid.previewHint')}</span>}
      </div>

      <div className="floor-editor-body">
        {mode === '2d' ? (
          <>
            <div className="floor-editor-canvas-wrap" ref={wrapRef} onDragOver={(e) => { e.preventDefault(); e.dataTransfer.dropEffect = 'copy'; }} onDrop={onDrop}>
              <canvas ref={canvasRef} width={cssW} height={cssH} className={`grid-canvas tool-${tool}${placing ? ' placing' : ''}`} style={{ width: cssW, height: cssH, touchAction: 'none' }}
                onPointerDown={onPointerDown} onPointerMove={onPointerMove} onPointerUp={onPointerUp} onPointerLeave={() => { cursorRef.current = null; hoverRef.current = -1; if (!dragRef.current) redraw(); }} onDoubleClick={onDoubleClick} />
            </div>
            <aside className="grid-editor-side">
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
              ) : (
                <>
                  <label className="grid-field"><span>{t('grid.cellSize')}</span><span className="grid-input-row"><input type="number" min="0.1" step="0.1" value={cellMeters} onChange={(e) => setCellMeters(Math.max(0, +e.target.value || 0))} /><em>{t('grid.metres')}</em></span></label>
                  <label className="grid-field"><span>{t('grid.wallHeight')}</span><span className="grid-input-row"><input type="number" min="0.5" step="0.1" value={wallHeight} onChange={(e) => setWallHeight(Math.max(0, +e.target.value || 0))} /><em>{t('grid.metres')}</em></span></label>
                  <div className="grid-readout">
                    <div><span>{t('grid.walls')}</span><strong>{segsRef.current.length}</strong></div>
                    <div><span>{t('map.cameras')}</span><strong>{placements.filter((p) => p.cameraId).length}</strong></div>
                  </div>
                  <p className="grid-hint">{t('fed.placeHint')}</p>
                </>
              )}
            </aside>
          </>
        ) : (
          <div className="grid-3d-wrap">
            <Suspense fallback={<div className="floor-view-empty">{t('map.loading3d')}</div>}>
              <Floor3D floors={[{ floor: draftFloor, placements }]} activeIndex={0} nodesById={nodesById} nowSec={nowSecRef.current} />
            </Suspense>
          </div>
        )}
      </div>
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
  busy: PropTypes.bool,
};
