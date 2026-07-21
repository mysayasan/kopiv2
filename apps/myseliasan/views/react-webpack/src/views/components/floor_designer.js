import { useCallback, useEffect, useRef, useState } from 'react';
import PropTypes from 'prop-types';
import { useT, Ico } from '@shared';

// Logical canvas resolution. The rasterised PNG saved as the floor image is this size, so
// placement pixel-coordinates line up with what the operator drew.
const DEFAULT_W = 1280;
const DEFAULT_H = 800;
const GRID = 20; // fine grid cell (px); every 5th line is emphasised
const MAJOR = 100; // emphasised major grid line
const INK = '#1f2937';

// --- geometry helpers (all in logical canvas pixels) ---
function dist2ToSeg(px, py, x1, y1, x2, y2) {
  const dx = x2 - x1;
  const dy = y2 - y1;
  const len2 = dx * dx + dy * dy || 1;
  let t = ((px - x1) * dx + (py - y1) * dy) / len2;
  t = Math.max(0, Math.min(1, t));
  const cx = x1 + t * dx;
  const cy = y1 + t * dy;
  return (px - cx) ** 2 + (py - cy) ** 2;
}

function shapeBBox(s) {
  if (s.type === 'room') return [Math.min(s.x, s.x + s.w), Math.min(s.y, s.y + s.h), Math.abs(s.w), Math.abs(s.h)];
  if (s.type === 'wall') return [Math.min(s.x1, s.x2), Math.min(s.y1, s.y2), Math.abs(s.x2 - s.x1), Math.abs(s.y2 - s.y1)];
  if (s.type === 'text') return [s.x - 4, s.y - 20, (s.text.length * 11) + 8, 28];
  if (s.type === 'pen') {
    const xs = s.points.map((p) => p[0]);
    const ys = s.points.map((p) => p[1]);
    return [Math.min(...xs), Math.min(...ys), Math.max(...xs) - Math.min(...xs), Math.max(...ys) - Math.min(...ys)];
  }
  return [0, 0, 0, 0];
}

// Rotation helpers. A shape may carry a `rot` (radians) applied around its own centre.
function rotatePt(px, py, cx, cy, ang) {
  const c = Math.cos(ang);
  const s = Math.sin(ang);
  const dx = px - cx;
  const dy = py - cy;
  return [cx + dx * c - dy * s, cy + dx * s + dy * c];
}
function shapeCenter(s) {
  if (s.type === 'wall') return [(s.x1 + s.x2) / 2, (s.y1 + s.y2) / 2];
  if (s.type === 'room') return [s.x + s.w / 2, s.y + s.h / 2];
  if (s.type === 'text') return [s.x, s.y - 8];
  const [bx, by, bw, bh] = shapeBBox(s);
  return [bx + bw / 2, by + bh / 2];
}
// applyRot sets the canvas transform for a shape's rotation and returns an undo fn (or null).
function applyRot(ctx, s) {
  if (!s.rot) return null;
  const [cx, cy] = shapeCenter(s);
  ctx.save();
  ctx.translate(cx, cy);
  ctx.rotate(s.rot);
  ctx.translate(-cx, -cy);
  return () => ctx.restore();
}
// reflectShape mirrors a shape across the horizontal ('h', axis x=ax) or vertical ('v', y=ay)
// line — a flip. Rotation sense is inverted so a rotated shape mirrors correctly.
function reflectShape(s, axis, ax, ay) {
  const rx = (x) => (axis === 'h' ? 2 * ax - x : x);
  const ry = (y) => (axis === 'v' ? 2 * ay - y : y);
  const rot = -(s.rot || 0);
  if (s.type === 'wall') return { ...s, x1: rx(s.x1), y1: ry(s.y1), x2: rx(s.x2), y2: ry(s.y2), rot };
  if (s.type === 'room') return { ...s, x: axis === 'h' ? rx(s.x + s.w) : s.x, y: axis === 'v' ? ry(s.y + s.h) : s.y, rot };
  if (s.type === 'pen') return { ...s, points: s.points.map((p) => [rx(p[0]), ry(p[1])]), rot };
  if (s.type === 'text') return { ...s, x: rx(s.x), y: ry(s.y), rot };
  return s;
}
function groupBBox(sel) {
  let x0 = Infinity; let y0 = Infinity; let x1 = -Infinity; let y1 = -Infinity;
  for (const s of sel) { const [bx, by, bw, bh] = shapeBBox(s); x0 = Math.min(x0, bx); y0 = Math.min(y0, by); x1 = Math.max(x1, bx + bw); y1 = Math.max(y1, by + bh); }
  return [x0, y0, x1 - x0, y1 - y0];
}
// rotateShapeAround rotates a shape by `delta` around an arbitrary pivot (its centre orbits the
// pivot; its own rotation increases by delta).
function rotateShapeAround(s, gx, gy, delta) {
  const [cx, cy] = shapeCenter(s);
  const [nx, ny] = rotatePt(cx, cy, gx, gy, delta);
  const moved = translateShape(s, nx - cx, ny - cy);
  return { ...moved, rot: (s.rot || 0) + delta };
}
// selectionHandles returns the on-canvas control positions for a selection: a rotate knob above
// it and flip toggles to its right (H) and below (V).
function selectionHandles(sel, gap = 30) {
  const [gx, gy, gw, gh] = groupBBox(sel);
  const g2 = gap * 0.73;
  return { cx: gx + gw / 2, cy: gy + gh / 2, top: gy, rot: [gx + gw / 2, gy - gap], flipH: [gx + gw + g2, gy + gh / 2], flipV: [gx + gw / 2, gy + gh + g2] };
}
function drawHandle(ctx, x, y, glyph, r = 11) {
  ctx.save();
  ctx.fillStyle = '#ffffff';
  ctx.strokeStyle = '#2d6cdf';
  ctx.lineWidth = 1.5 * (r / 11);
  ctx.beginPath();
  ctx.arc(x, y, r, 0, Math.PI * 2);
  ctx.fill();
  ctx.stroke();
  ctx.fillStyle = '#2d6cdf';
  ctx.font = `700 ${Math.max(8, Math.round(r * 1.15))}px system-ui, sans-serif`;
  ctx.textAlign = 'center';
  ctx.textBaseline = 'middle';
  ctx.fillText(glyph, x, y + r * 0.08);
  ctx.restore();
}

// hitShape returns the topmost shape under (px, py), or null. The click is rotated into each
// shape's local frame first, so rotated shapes hit-test correctly.
function hitShape(shapes, px, py) {
  const TOL2 = 100; // 10px
  for (let i = shapes.length - 1; i >= 0; i--) {
    const s = shapes[i];
    let qx = px; let qy = py;
    if (s.rot) { const [cx, cy] = shapeCenter(s); [qx, qy] = rotatePt(px, py, cx, cy, -s.rot); }
    if (s.type === 'wall' && dist2ToSeg(qx, qy, s.x1, s.y1, s.x2, s.y2) <= TOL2) return s;
    if (s.type === 'pen') {
      for (let k = 1; k < s.points.length; k++) {
        if (dist2ToSeg(qx, qy, s.points[k - 1][0], s.points[k - 1][1], s.points[k][0], s.points[k][1]) <= TOL2) return s;
      }
    }
    if (s.type === 'room' || s.type === 'text') {
      const [bx, by, bw, bh] = shapeBBox(s);
      if (qx >= bx && qx <= bx + bw && qy >= by && qy <= by + bh) return s;
    }
  }
  return null;
}

function translateShape(s, dx, dy) {
  if (s.type === 'room' || s.type === 'text') return { ...s, x: s.x + dx, y: s.y + dy };
  if (s.type === 'wall') return { ...s, x1: s.x1 + dx, y1: s.y1 + dy, x2: s.x2 + dx, y2: s.y2 + dy };
  if (s.type === 'pen') return { ...s, points: s.points.map((p) => [p[0] + dx, p[1] + dy]) };
  return s;
}

// paintShapes draws every shape onto ctx. Used both for the live canvas and the clean export.
function paintShapes(ctx, shapes) {
  ctx.lineJoin = 'round';
  ctx.lineCap = 'round';
  for (const s of shapes) {
    const undo = applyRot(ctx, s);
    if (s.type === 'room') {
      ctx.fillStyle = 'rgba(37,99,235,0.06)';
      ctx.strokeStyle = INK;
      ctx.lineWidth = 2;
      ctx.beginPath();
      ctx.rect(s.x, s.y, s.w, s.h);
      ctx.fill();
      ctx.stroke();
    } else if (s.type === 'wall') {
      ctx.strokeStyle = INK;
      ctx.lineWidth = 4;
      ctx.beginPath();
      ctx.moveTo(s.x1, s.y1);
      ctx.lineTo(s.x2, s.y2);
      ctx.stroke();
    } else if (s.type === 'pen') {
      ctx.strokeStyle = INK;
      ctx.lineWidth = 3;
      ctx.beginPath();
      s.points.forEach((p, i) => (i ? ctx.lineTo(p[0], p[1]) : ctx.moveTo(p[0], p[1])));
      ctx.stroke();
    } else if (s.type === 'text') {
      ctx.font = '600 20px system-ui, sans-serif';
      ctx.textBaseline = 'alphabetic';
      ctx.lineWidth = 4;
      ctx.strokeStyle = 'rgba(255,255,255,0.9)';
      ctx.strokeText(s.text, s.x, s.y);
      ctx.fillStyle = INK;
      ctx.fillText(s.text, s.x, s.y);
    }
    if (undo) undo();
  }
}

// paintGlow draws an accent halo behind a selected shape so the selection reads as highlighting
// the drawing itself (not just a box around it).
function paintGlow(ctx, s) {
  const undo = applyRot(ctx, s);
  ctx.save();
  ctx.lineJoin = 'round';
  ctx.lineCap = 'round';
  ctx.strokeStyle = 'rgba(45,108,223,0.35)';
  if (s.type === 'wall') { ctx.lineWidth = 11; ctx.beginPath(); ctx.moveTo(s.x1, s.y1); ctx.lineTo(s.x2, s.y2); ctx.stroke(); } else if (s.type === 'room') { ctx.lineWidth = 9; ctx.strokeRect(s.x, s.y, s.w, s.h); } else if (s.type === 'pen') { ctx.lineWidth = 9; ctx.beginPath(); s.points.forEach((p, i) => (i ? ctx.lineTo(p[0], p[1]) : ctx.moveTo(p[0], p[1]))); ctx.stroke(); } else if (s.type === 'text') { const [bx, by, bw, bh] = shapeBBox(s); ctx.fillStyle = 'rgba(45,108,223,0.22)'; ctx.fillRect(bx - 2, by - 2, bw + 4, bh + 4); }
  ctx.restore();
  if (undo) undo();
}

// FloorDesigner is a from-scratch plan editor: draw walls, rooms, freehand lines and text labels
// on a blank canvas, then Save rasterises it to a PNG that becomes the floor image (so placement /
// viewing work unchanged). Opened when the operator has no image to upload.
export function FloorDesigner({ onCancel, onSave, initialShapes, initialName, width, height, backgroundSrc }) {
  const t = useT();
  const W = width && width > 0 ? width : DEFAULT_W;
  const H = height && height > 0 ? height : DEFAULT_H;
  const canvasRef = useRef(null);
  const bgRef = useRef(null); // loaded background <img> (an uploaded plan being annotated)
  const [name, setName] = useState(initialName || '');
  const [tool, setTool] = useState('wall');
  const [grid, setGrid] = useState(true);
  const [snap, setSnap] = useState(true); // grid lock: snap wall/room/label points to the grid
  const [shapes, setShapes] = useState(() => (Array.isArray(initialShapes) ? initialShapes : []));
  const [selectedIds, setSelectedIds] = useState([]); // multi-select
  const historyRef = useRef([]); // undo snapshots
  const futureRef = useRef([]); // redo snapshots
  const [histLen, setHistLen] = useState(0); // mirror ref lengths into state so the buttons react
  const [futLen, setFutLen] = useState(0);
  const draftRef = useRef(null); // in-progress shape while dragging
  const moveRef = useRef(null); // { ids, lastX, lastY, moved, snapshot } while moving a selection
  const marqueeRef = useRef(null); // { x0, y0, x1, y1, base } while rubber-band selecting
  const rotateRef = useRef(null); // { gx, gy, startAng, snapshot, moved } while dragging the rotate knob
  const idRef = useRef((Array.isArray(initialShapes) && initialShapes.length ? Math.max(...initialShapes.map((s) => s.id || 0)) : 0) + 1);
  const viewRef = useRef({ scale: 1, tx: 0, ty: 0 }); // logical→canvas-pixel transform (zoom + pan)
  const fitScaleRef = useRef(1); // the "fit whole plan" scale, used as the zoom-out floor
  const panRef = useRef(null); // { sx, sy, tx, ty } while panning with the middle mouse button

  // Screen (client) point → logical plan coordinate, inverting the current zoom/pan.
  const toLogical = useCallback((e) => {
    const c = canvasRef.current;
    const r = c.getBoundingClientRect();
    const v = viewRef.current;
    const px = (e.clientX - r.left) * (c.width / r.width);
    const py = (e.clientY - r.top) * (c.height / r.height);
    return [(px - v.tx) / v.scale, (py - v.ty) / v.scale];
  }, []);

  // Grid lock: snap a coordinate to the nearest grid line (structural tools only; freehand keeps
  // its raw points).
  const snapV = useCallback((v) => (snap ? Math.round(v / GRID) * GRID : v), [snap]);

  const syncHist = useCallback(() => { setHistLen(historyRef.current.length); setFutLen(futureRef.current.length); }, []);
  // Record the pre-change state for undo. Any new change also invalidates the redo stack.
  const pushHistory = useCallback((prev) => {
    historyRef.current.push(prev);
    if (historyRef.current.length > 100) historyRef.current.shift();
    futureRef.current = [];
    setHistLen(historyRef.current.length);
    setFutLen(0);
  }, []);

  // Redraw the visible canvas under the current zoom/pan: gray backdrop, the white plan plane,
  // background image, grid, shapes, and the (screen-constant) selection UI.
  const render = useCallback(() => {
    const c = canvasRef.current;
    if (!c) return;
    const ctx = c.getContext('2d');
    const v = viewRef.current;
    const sc = v.scale || 1;
    // Backdrop (identity transform) so panning past the plan shows a neutral surface, not artefacts.
    ctx.setTransform(1, 0, 0, 1, 0, 0);
    ctx.clearRect(0, 0, c.width, c.height);
    ctx.fillStyle = '#e9edf2';
    ctx.fillRect(0, 0, c.width, c.height);
    // Everything below is drawn in LOGICAL plan coordinates under the zoom/pan transform.
    ctx.setTransform(sc, 0, 0, sc, v.tx, v.ty);
    ctx.fillStyle = '#ffffff';
    ctx.fillRect(0, 0, W, H);
    if (bgRef.current) { try { ctx.drawImage(bgRef.current, 0, 0, W, H); } catch (_) { /* not ready */ } }
    if (grid) {
      ctx.strokeStyle = '#f1f4f8';
      ctx.lineWidth = 1 / sc;
      ctx.beginPath();
      for (let x = GRID; x < W; x += GRID) { if (x % MAJOR) { ctx.moveTo(x, 0); ctx.lineTo(x, H); } }
      for (let y = GRID; y < H; y += GRID) { if (y % MAJOR) { ctx.moveTo(0, y); ctx.lineTo(W, y); } }
      ctx.stroke();
      ctx.strokeStyle = '#e2e8f0';
      ctx.lineWidth = 1.2 / sc;
      ctx.beginPath();
      for (let x = MAJOR; x < W; x += MAJOR) { ctx.moveTo(x, 0); ctx.lineTo(x, H); }
      for (let y = MAJOR; y < H; y += MAJOR) { ctx.moveTo(0, y); ctx.lineTo(W, y); }
      ctx.stroke();
    }
    const selSet = new Set(selectedIds);
    if (selSet.size) { for (const s of shapes) if (selSet.has(s.id)) paintGlow(ctx, s); }
    const draft = draftRef.current;
    paintShapes(ctx, draft ? [...shapes, draft] : shapes);
    if (selSet.size) {
      const d = 1 / sc; // keep selection chrome a constant size on screen regardless of zoom
      ctx.strokeStyle = '#2d6cdf';
      ctx.setLineDash([6 * d, 4 * d]);
      ctx.lineWidth = 1.5 * d;
      for (const s of shapes) if (selSet.has(s.id)) { const [bx, by, bw, bh] = shapeBBox(s); const u = applyRot(ctx, s); ctx.strokeRect(bx - 4 * d, by - 4 * d, bw + 8 * d, bh + 8 * d); if (u) u(); }
      ctx.setLineDash([]);
      const sel = shapes.filter((s) => selSet.has(s.id));
      const HN = selectionHandles(sel, 30 * d);
      ctx.strokeStyle = '#2d6cdf';
      ctx.lineWidth = 1.5 * d;
      ctx.beginPath();
      ctx.moveTo(HN.cx, HN.top - 4 * d);
      ctx.lineTo(HN.rot[0], HN.rot[1]);
      ctx.stroke();
      drawHandle(ctx, HN.rot[0], HN.rot[1], '↻', 11 * d);
      drawHandle(ctx, HN.flipH[0], HN.flipH[1], '⇋', 11 * d);
      drawHandle(ctx, HN.flipV[0], HN.flipV[1], '⇳', 11 * d);
    }
    const mq = marqueeRef.current;
    if (mq) {
      const d = 1 / sc;
      const rx = Math.min(mq.x0, mq.x1);
      const ry = Math.min(mq.y0, mq.y1);
      const rw = Math.abs(mq.x1 - mq.x0);
      const rh = Math.abs(mq.y1 - mq.y0);
      ctx.fillStyle = 'rgba(45,108,223,0.08)';
      ctx.fillRect(rx, ry, rw, rh);
      ctx.strokeStyle = '#2d6cdf';
      ctx.setLineDash([5 * d, 3 * d]);
      ctx.lineWidth = 1 * d;
      ctx.strokeRect(rx, ry, rw, rh);
      ctx.setLineDash([]);
    }
  }, [shapes, grid, selectedIds, W, H]);

  useEffect(() => { render(); }, [render]);

  // Fit the whole plan into the canvas, centred — the default zoom, and the zoom-out floor.
  const fitView = useCallback(() => {
    const c = canvasRef.current;
    if (!c || !c.width || !c.height) return;
    const s = Math.min(c.width / W, c.height / H) * 0.94;
    fitScaleRef.current = s;
    viewRef.current = { scale: s, tx: (c.width - W * s) / 2, ty: (c.height - H * s) / 2 };
    render();
  }, [W, H, render]);

  // Zoom by a factor about the canvas centre (used by the toolbar buttons).
  const zoomBy = useCallback((factor) => {
    const c = canvasRef.current;
    if (!c) return;
    const v = viewRef.current;
    const cx = c.width / 2;
    const cy = c.height / 2;
    const lx = (cx - v.tx) / v.scale;
    const ly = (cy - v.ty) / v.scale;
    const ns = Math.max(fitScaleRef.current * 0.5, Math.min(10, v.scale * factor));
    viewRef.current = { scale: ns, tx: cx - lx * ns, ty: cy - ly * ns };
    render();
  }, [render]);

  // Keep the canvas bitmap sized to its box (crisp on hi-dpi) and re-fit when the plan/size changes.
  useEffect(() => {
    const c = canvasRef.current;
    if (!c) return undefined;
    const resize = () => {
      const rect = c.getBoundingClientRect();
      const dpr = window.devicePixelRatio || 1;
      const bw = Math.max(1, Math.round(rect.width * dpr));
      const bh = Math.max(1, Math.round(rect.height * dpr));
      if (c.width !== bw || c.height !== bh) { c.width = bw; c.height = bh; }
      fitView();
    };
    resize();
    let ro;
    if (typeof ResizeObserver !== 'undefined') { ro = new ResizeObserver(resize); ro.observe(c); }
    return () => { if (ro) ro.disconnect(); };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [W, H]);

  // Wheel to zoom, centred on the cursor. Non-passive so we can stop the page from scrolling.
  useEffect(() => {
    const c = canvasRef.current;
    if (!c) return undefined;
    const onWheel = (e) => {
      e.preventDefault();
      const rect = c.getBoundingClientRect();
      const cx = (e.clientX - rect.left) * (c.width / rect.width);
      const cy = (e.clientY - rect.top) * (c.height / rect.height);
      const v = viewRef.current;
      const lx = (cx - v.tx) / v.scale;
      const ly = (cy - v.ty) / v.scale;
      const factor = e.deltaY < 0 ? 1.12 : 1 / 1.12;
      const ns = Math.max(fitScaleRef.current * 0.5, Math.min(10, v.scale * factor));
      viewRef.current = { scale: ns, tx: cx - lx * ns, ty: cy - ly * ns };
      render();
    };
    c.addEventListener('wheel', onWheel, { passive: false });
    return () => c.removeEventListener('wheel', onWheel);
  }, [render]);

  // Load the background image (an uploaded plan being annotated) and redraw once it's ready.
  useEffect(() => {
    if (!backgroundSrc) { bgRef.current = null; return undefined; }
    let live = true;
    const im = new Image();
    im.onload = () => { if (live) { bgRef.current = im; render(); } };
    im.onerror = () => { if (live) { bgRef.current = null; } };
    im.src = backgroundSrc;
    return () => { live = false; };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [backgroundSrc]);

  const onPointerDown = (e) => {
    // Middle mouse button pans the canvas, whatever the active tool.
    if (e.button === 1) {
      e.preventDefault();
      canvasRef.current.setPointerCapture?.(e.pointerId);
      panRef.current = { sx: e.clientX, sy: e.clientY, tx: viewRef.current.tx, ty: viewRef.current.ty };
      return;
    }
    if (e.button !== 0) return; // only the left button draws / selects
    e.preventDefault();
    canvasRef.current.setPointerCapture?.(e.pointerId);
    const [x, y] = toLogical(e);
    if (tool === 'select') {
      // On-canvas controllers take priority when a selection exists (hit radii are screen-constant).
      if (selectedIds.length) {
        const sc = viewRef.current.scale || 1;
        const sel = shapes.filter((sh) => selectedIds.includes(sh.id));
        const H = selectionHandles(sel, 30 / sc);
        const near = (h, r = 13 / sc) => (x - h[0]) ** 2 + (y - h[1]) ** 2 <= r * r;
        if (near(H.rot)) { rotateRef.current = { gx: H.cx, gy: H.cy, startAng: Math.atan2(y - H.cy, x - H.cx), snapshot: shapes, moved: false }; return; }
        if (near(H.flipH)) { flipSel('h'); return; }
        if (near(H.flipV)) { flipSel('v'); return; }
      }
      const hit = hitShape(shapes, x, y);
      const additive = e.shiftKey || e.ctrlKey || e.metaKey;
      if (hit) {
        let ids;
        if (additive) ids = selectedIds.includes(hit.id) ? selectedIds.filter((i) => i !== hit.id) : [...selectedIds, hit.id];
        else ids = selectedIds.includes(hit.id) ? selectedIds : [hit.id]; // keep the group if the shape is already in it (so you can drag it)
        setSelectedIds(ids);
        moveRef.current = { ids, lastX: x, lastY: y, moved: false, snapshot: shapes };
      } else {
        if (!additive) setSelectedIds([]);
        marqueeRef.current = { x0: x, y0: y, x1: x, y1: y, base: additive ? selectedIds : [] };
      }
      render();
      return;
    }
    if (tool === 'erase') {
      const hit = hitShape(shapes, x, y);
      if (hit) { pushHistory(shapes); setShapes((list) => list.filter((s) => s.id !== hit.id)); }
      return;
    }
    if (tool === 'text') {
      const text = window.prompt(t('fd.textPrompt'));
      if (text && text.trim()) { pushHistory(shapes); setShapes((list) => [...list, { id: idRef.current++, type: 'text', x: snapV(x), y: snapV(y), text: text.trim() }]); }
      return;
    }
    if (tool === 'wall') draftRef.current = { id: 0, type: 'wall', x1: snapV(x), y1: snapV(y), x2: snapV(x), y2: snapV(y) };
    else if (tool === 'room') draftRef.current = { id: 0, type: 'room', x: snapV(x), y: snapV(y), w: 0, h: 0 };
    else if (tool === 'pen') draftRef.current = { id: 0, type: 'pen', points: [[x, y]] };
    render();
  };

  const onPointerMove = (e) => {
    if (panRef.current) {
      const c = canvasRef.current;
      const rect = c.getBoundingClientRect();
      viewRef.current = { ...viewRef.current, tx: panRef.current.tx + (e.clientX - panRef.current.sx) * (c.width / rect.width), ty: panRef.current.ty + (e.clientY - panRef.current.sy) * (c.height / rect.height) };
      render();
      return;
    }
    const [x, y] = toLogical(e);
    if (rotateRef.current) {
      const rr = rotateRef.current;
      let delta = Math.atan2(y - rr.gy, x - rr.gx) - rr.startAng;
      if (snap) delta = Math.round(delta / (Math.PI / 12)) * (Math.PI / 12); // 15° steps under grid lock
      rr.moved = true;
      setShapes(rr.snapshot.map((s) => (selectedIds.includes(s.id) ? rotateShapeAround(s, rr.gx, rr.gy, delta) : s)));
      render();
      return;
    }
    if (moveRef.current) {
      const m = moveRef.current;
      const dx = x - m.lastX;
      const dy = y - m.lastY;
      m.lastX = x; m.lastY = y; m.moved = true;
      const idset = new Set(m.ids);
      setShapes((list) => list.map((s) => (idset.has(s.id) ? translateShape(s, dx, dy) : s)));
      return;
    }
    if (marqueeRef.current) {
      const mq = marqueeRef.current;
      mq.x1 = x; mq.y1 = y;
      const rx = Math.min(mq.x0, mq.x1);
      const ry = Math.min(mq.y0, mq.y1);
      const rw = Math.abs(mq.x1 - mq.x0);
      const rh = Math.abs(mq.y1 - mq.y0);
      const inside = shapes.filter((s) => { const [bx, by, bw, bh] = shapeBBox(s); return bx < rx + rw && bx + bw > rx && by < ry + rh && by + bh > ry; }).map((s) => s.id);
      setSelectedIds([...new Set([...(mq.base || []), ...inside])]);
      render();
      return;
    }
    const d = draftRef.current;
    if (!d) return;
    if (d.type === 'wall') { d.x2 = snapV(x); d.y2 = snapV(y); }
    else if (d.type === 'room') { d.w = snapV(x) - d.x; d.h = snapV(y) - d.y; }
    else if (d.type === 'pen') { d.points.push([x, y]); }
    render();
  };

  const onPointerUp = () => {
    if (panRef.current) { panRef.current = null; return; }
    if (rotateRef.current) { const rr = rotateRef.current; rotateRef.current = null; if (rr.moved) pushHistory(rr.snapshot); return; }
    if (moveRef.current) {
      const m = moveRef.current;
      moveRef.current = null;
      if (m.moved) pushHistory(m.snapshot); // snapshot is the pre-move state → clean undo of the move
      return;
    }
    if (marqueeRef.current) { marqueeRef.current = null; render(); return; }
    const d = draftRef.current;
    draftRef.current = null;
    if (!d) { render(); return; }
    let ok = false;
    if (d.type === 'wall') ok = Math.hypot(d.x2 - d.x1, d.y2 - d.y1) > 6;
    else if (d.type === 'room') ok = Math.abs(d.w) > 6 && Math.abs(d.h) > 6;
    else if (d.type === 'pen') ok = d.points.length > 2;
    if (ok) { pushHistory(shapes); setShapes((list) => [...list, { ...d, id: idRef.current++ }]); }
    else render();
  };

  const undo = () => {
    if (!historyRef.current.length) return;
    futureRef.current.push(shapes);
    setShapes(historyRef.current.pop());
    setSelectedIds([]);
    syncHist();
  };
  const redo = () => {
    if (!futureRef.current.length) return;
    historyRef.current.push(shapes);
    setShapes(futureRef.current.pop());
    setSelectedIds([]);
    syncHist();
  };
  const clearAll = () => { if (shapes.length && window.confirm(t('fd.clearConfirm'))) { pushHistory(shapes); setShapes([]); setSelectedIds([]); } };

  // Rotate the selection by `delta` radians around the selection's centre (each shape also spins
  // about its own centre), so a single shape rotates in place and a group rotates as one.
  const rotateSel = (delta) => {
    if (!selectedIds.length) return;
    pushHistory(shapes);
    const sel = shapes.filter((s) => selectedIds.includes(s.id));
    const [gx, gy, gw, gh] = groupBBox(sel);
    const gcx = gx + gw / 2;
    const gcy = gy + gh / 2;
    setShapes((list) => list.map((s) => {
      if (!selectedIds.includes(s.id)) return s;
      const [cx, cy] = shapeCenter(s);
      const [nx, ny] = rotatePt(cx, cy, gcx, gcy, delta);
      const moved = translateShape(s, nx - cx, ny - cy);
      return { ...moved, rot: (moved.rot || 0) + delta };
    }));
  };
  // Flip the selection across its own vertical ('h') or horizontal ('v') mid-line.
  const flipSel = (axis) => {
    if (!selectedIds.length) return;
    pushHistory(shapes);
    const sel = shapes.filter((s) => selectedIds.includes(s.id));
    const [gx, gy, gw, gh] = groupBBox(sel);
    const ax = gx + gw / 2;
    const ay = gy + gh / 2;
    setShapes((list) => list.map((s) => (selectedIds.includes(s.id) ? reflectShape(s, axis, ax, ay) : s)));
  };

  const save = () => {
    // Rasterise a CLEAN copy (white bg + shapes, no grid / selection) at logical size.
    const off = document.createElement('canvas');
    off.width = W; off.height = H;
    const ctx = off.getContext('2d');
    ctx.fillStyle = '#ffffff';
    ctx.fillRect(0, 0, W, H);
    if (bgRef.current) { try { ctx.drawImage(bgRef.current, 0, 0, W, H); } catch (_) { /* skip */ } }
    paintShapes(ctx, shapes);
    off.toBlob((blob) => { if (blob && onSave) onSave(blob, name.trim(), JSON.stringify(shapes)); }, 'image/png');
  };

  useEffect(() => {
    const onKey = (e) => {
      // Don't hijack keys while typing in a field (e.g. renaming the plan) — Backspace there must
      // edit text, not delete a shape, and Escape must not close the dialog mid-edit.
      const el = e.target;
      if (el && (el.tagName === 'INPUT' || el.tagName === 'TEXTAREA' || el.isContentEditable)) return;
      if (e.key === 'Escape') onCancel();
      else if ((e.ctrlKey || e.metaKey) && !e.shiftKey && e.key.toLowerCase() === 'z') { e.preventDefault(); undo(); }
      else if ((e.ctrlKey || e.metaKey) && (e.key.toLowerCase() === 'y' || (e.shiftKey && e.key.toLowerCase() === 'z'))) { e.preventDefault(); redo(); }
      else if ((e.key === 'Delete' || e.key === 'Backspace') && selectedIds.length) { pushHistory(shapes); setShapes((l) => l.filter((s) => !selectedIds.includes(s.id))); setSelectedIds([]); }
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [shapes, selectedIds]);

  const TOOLS = [
    ['select', 'cursor', t('fd.select')],
    ['wall', 'flip-h', t('fd.wall')],
    ['room', 'stop', t('fd.room')],
    ['pen', 'edit-2', t('fd.pen')],
    ['text', 'plus', t('fd.text')],
    ['erase', 'eraser', t('fd.erase')],
  ];

  return (
    <div className="fd-overlay" role="dialog" aria-label={t('fd.title')}>
      <div className="fd-dialog">
        <div className="fd-head">
          <span className="fd-title"><Ico n="edit-2" sz={15} /> {t('fd.title')}</span>
          <input className="fd-name" type="text" value={name} onChange={(e) => setName(e.target.value)} placeholder={t('fd.namePlaceholder')} aria-label={t('fd.name')} />
          <span className="fd-head-spacer" />
          <button type="button" className="quiet" onClick={onCancel}>{t('fd.cancel')}</button>
          {/* Save is available when editing an existing plan (a background is present) OR once
              something has been drawn — so a name-only change on an uploaded plan can be saved. */}
          <button type="button" onClick={save} disabled={shapes.length === 0 && !backgroundSrc}><span className="btn-icon"><Ico n="save" sz={14} /> {t('fd.save')}</span></button>
        </div>
        <div className="fd-toolbar">
          {TOOLS.map(([id, icon, label]) => (
            <button key={id} type="button" className={`fd-tool${tool === id ? ' active' : ''}`} onClick={() => { setTool(id); if (id !== 'select') setSelectedIds([]); }} title={label}>
              <Ico n={icon} sz={15} /> <span>{label}</span>
            </button>
          ))}
          <span className="fd-toolbar-sep" />
          <button type="button" className="fd-tool" onClick={() => rotateSel(-Math.PI / 12)} disabled={!selectedIds.length} title={t('fd.rotateLeft')}><span className="fd-mirror"><Ico n="rotate-cw" sz={15} /></span></button>
          <button type="button" className="fd-tool" onClick={() => rotateSel(Math.PI / 12)} disabled={!selectedIds.length} title={t('fd.rotateRight')}><Ico n="rotate-cw" sz={15} /></button>
          <button type="button" className="fd-tool" onClick={() => flipSel('h')} disabled={!selectedIds.length} title={t('fd.flipH')}><Ico n="flip-h" sz={15} /></button>
          <button type="button" className="fd-tool" onClick={() => flipSel('v')} disabled={!selectedIds.length} title={t('fd.flipV')}><Ico n="flip-v" sz={15} /></button>
          <span className="fd-toolbar-sep" />
          <button type="button" className="fd-tool" onClick={undo} disabled={histLen === 0} title={t('fd.undo')}><Ico n="undo" sz={15} /></button>
          <button type="button" className="fd-tool" onClick={redo} disabled={futLen === 0} title={t('fd.redo')}><Ico n="redo" sz={15} /></button>
          <button type="button" className={`fd-tool${snap ? ' active' : ''}`} onClick={() => setSnap((v) => !v)} title={t('fd.snap')}><Ico n="pin" sz={15} /> <span>{t('fd.snapLabel')}</span></button>
          <button type="button" className={`fd-tool${grid ? ' active' : ''}`} onClick={() => setGrid((g) => !g)} title={t('fd.grid')}><Ico n="grid4" sz={15} /></button>
          <button type="button" className="fd-tool danger-text" onClick={clearAll} title={t('fd.clear')}><Ico n="trash" sz={15} /></button>
          <span className="fd-toolbar-sep" />
          <button type="button" className="fd-tool" onClick={() => zoomBy(1 / 1.25)} title={t('fd.zoomOut')}><Ico n="minimize" sz={15} /></button>
          <button type="button" className="fd-tool" onClick={() => zoomBy(1.25)} title={t('fd.zoomIn')}><Ico n="plus" sz={15} /></button>
          <button type="button" className="fd-tool" onClick={fitView} title={t('fd.fit')}><Ico n="maximize" sz={15} /> <span>{t('fd.fitLabel')}</span></button>
        </div>
        <div className="fd-stage">
          <canvas
            ref={canvasRef}
            width={W}
            height={H}
            className={`fd-canvas fd-tool-${tool}`}
            onPointerDown={onPointerDown}
            onPointerMove={onPointerMove}
            onPointerUp={onPointerUp}
            onPointerLeave={onPointerUp}
            onAuxClick={(e) => e.preventDefault()}
          />
        </div>
        <div className="fd-hint">{t('fd.hint')}</div>
      </div>
    </div>
  );
}

FloorDesigner.propTypes = { onCancel: PropTypes.func, onSave: PropTypes.func, initialShapes: PropTypes.array, initialName: PropTypes.string, width: PropTypes.number, height: PropTypes.number, backgroundSrc: PropTypes.string };
