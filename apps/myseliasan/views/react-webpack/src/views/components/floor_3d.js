import { useEffect, useMemo, useRef, useState } from 'react';
import PropTypes from 'prop-types';
import * as THREE from 'three';
import { OrbitControls } from 'three/examples/jsm/controls/OrbitControls.js';
import { useT } from '@shared';
import { apiBase } from '../lib/helpers';
import { nodeTone } from '../lib/fleet_status';

// Floor3D is the 3D building view. It auto-generates a scene from the floor data with no 3D
// knowledge required of the operator: the flat plan image becomes a textured floor slab, the
// painted GRID (from the grid editor) is extruded into walls — or a perimeter box when no grid is
// authored yet — and every camera stands above the slab as a coverage cone coloured by its owning
// node's status. It consumes the same { floor, placements } payload as BuildingFloorView.
//
// It can render ONE floor (default) or STACK a whole building's floors at their elevations. Scale
// is metres throughout: a floor's real-world scale (metres-per-pixel) is used when set, else the
// plan's longer side is assumed ASSUMED_LONG_SIDE_M so proportions still read correctly.

const ASSUMED_LONG_SIDE_M = 20; // fallback real size of a plan's longer side when no scale is set
const DEFAULT_WALL_H = 2.7; // metres — a storey
const DEFAULT_MOUNT_H = 2.5; // metres — a wall-mounted camera
const DEFAULT_PITCH = 15; // degrees of downward tilt when a placement has none stored
const FLOOR_GAP = 0.4; // metres of air between stacked floors

function mppOf(floor) {
  const w = (floor && floor.width) || 1024;
  const h = (floor && floor.height) || 768;
  return floor && floor.scale > 0 ? floor.scale : ASSUMED_LONG_SIDE_M / Math.max(w, h);
}
function wallHOf(floor) {
  return floor && floor.wallHeight > 0 ? floor.wallHeight : DEFAULT_WALL_H;
}

// coverageRangeM mirrors the 2D wedge radius (min(w,h)*0.16, floored) so 2D and 3D coverage read at
// the same scale, returned in metres.
function coverageRangeM(w, h, mpp) {
  return Math.max(50, Math.min(w, h) * 0.16) * mpp;
}

// coneToward builds a translucent view cone: apex at the camera, opening onto a disc at target.
function coneToward(apex, target, fovDeg, color) {
  const dir = new THREE.Vector3().subVectors(target, apex);
  const len = dir.length() || 0.001;
  const baseR = Math.max(0.15, len * Math.tan((Math.min(160, Math.max(10, fovDeg)) * Math.PI) / 360));
  const geo = new THREE.ConeGeometry(baseR, len, 28, 1, true);
  geo.translate(0, -len / 2, 0); // apex to origin
  const mat = new THREE.MeshBasicMaterial({ color, transparent: true, opacity: 0.16, side: THREE.DoubleSide, depthWrite: false });
  const mesh = new THREE.Mesh(geo, mat);
  mesh.quaternion.setFromUnitVectors(new THREE.Vector3(0, -1, 0), dir.clone().normalize());
  mesh.position.copy(apex);
  return mesh;
}

// parseGrid safely reads a floor's model JSON. Accepts the segment shape (new: drawn walls) and the
// legacy cell shape (old: painted cells). Returns null when absent/invalid.
function parseGrid(floor) {
  if (!floor || !floor.grid) return null;
  try {
    const g = typeof floor.grid === 'string' ? JSON.parse(floor.grid) : floor.grid;
    if (!g) return null;
    if (Array.isArray(g.segments) && g.segments.length) return g;
    if (Array.isArray(g.walls) && g.walls.length && (g.cellPx || g.unit)) return g;
    return null;
  } catch (_) { return null; }
}

// parseStairs reads the straight-flight stairs authored in the 2D editor. Independent of parseGrid
// so a floor can carry stairs with no walls. Each entry is an image-space footprint + ascent dir.
function parseStairs(floor) {
  if (!floor || !floor.grid) return [];
  try {
    const g = typeof floor.grid === 'string' ? JSON.parse(floor.grid) : floor.grid;
    return g && Array.isArray(g.stairs) ? g.stairs : [];
  } catch (_) { return []; }
}
function parseDoors(floor) {
  if (!floor || !floor.grid) return [];
  try {
    const g = typeof floor.grid === 'string' ? JSON.parse(floor.grid) : floor.grid;
    return g && Array.isArray(g.doors) ? g.doors : [];
  } catch (_) { return []; }
}
// doorSpanOnSeg mirrors the editor's version: the [t0,t1] opening a door cuts in wall segment s
// (image space), or null. Kept in sync with floor_editor.js so 2D gaps and 3D gaps line up.
function doorSpanOnSeg(s, d, tol) {
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
// carveSpans → the wall pieces [t-intervals] that remain once door openings are removed, plus the
// merged door intervals themselves (used to place a lintel over each opening).
function carveSpans(s, doors, tol) {
  const spans = [];
  doors.forEach((d) => { const iv = doorSpanOnSeg(s, d, tol); if (iv) spans.push(iv); });
  if (!spans.length) return { walls: [[0, 1]], doors: [] };
  spans.sort((a, b) => a[0] - b[0]);
  const merged = [];
  spans.forEach((sp) => { const last = merged[merged.length - 1]; if (!last || sp[0] > last[1]) merged.push([sp[0], sp[1]]); else last[1] = Math.max(last[1], sp[1]); });
  const walls = []; let cursor = 0;
  merged.forEach(([a, b]) => { if (a > cursor + 1e-4) walls.push([cursor, a]); cursor = Math.max(cursor, b); });
  if (cursor < 1 - 1e-4) walls.push([cursor, 1]);
  return { walls, doors: merged };
}

export default function Floor3D({ floors = [], activeIndex = 0, stacked = false, nodesById = {}, focusCameraId, nowSec, onPlay }) {
  const t = useT();
  const mountRef = useRef(null);
  const disposeRef = useRef(null);
  const [hover, setHover] = useState(null); // { name, x, y } overlay label
  const [texError, setTexError] = useState(false);

  // Rebuild only when the geometry-affecting inputs change.
  const sig = useMemo(() => JSON.stringify(floors.map((e) => {
    const f = e.floor || {};
    return {
      id: f.id, w: f.width, h: f.height, s: f.scale, wh: f.wallHeight, el: f.elevation, g: f.grid,
      p: (e.placements || []).map((p) => `${p.id}:${p.x},${p.y},${p.heading},${p.fov},${p.mountHeight || ''},${p.pitch || ''},${p.cameraId || ''}`),
    };
  })) + `|${activeIndex}|${stacked}|${focusCameraId}|${nowSec}`, [floors, activeIndex, stacked, focusCameraId, nowSec]);

  useEffect(() => {
    const host = mountRef.current;
    if (!host || floors.length === 0) return undefined;
    setTexError(false);

    // Which floors to draw, and each one's base elevation (metres).
    const shown = stacked ? floors.map((e, i) => ({ e, i })) : [{ e: floors[activeIndex] || floors[0], i: activeIndex }];
    let cum = 0;
    const elevations = shown.map(({ e }, idx) => {
      const f = e.floor || {};
      const y = f.elevation > 0 ? f.elevation : cum;
      cum = (f.elevation > 0 ? f.elevation : cum) + wallHOf(f) + FLOOR_GAP;
      return stacked ? y : 0; // single-floor view sits on the ground
    });

    const active = floors[activeIndex] || floors[0];
    const aFloor = active.floor || {};
    const aMpp = mppOf(aFloor);
    const worldW = ((aFloor.width || 1024)) * aMpp;
    const worldH = ((aFloor.height || 768)) * aMpp;
    const span = Math.max(worldW, worldH, cum || wallHOf(aFloor));

    const dark = typeof document !== 'undefined' && document.documentElement.getAttribute('data-theme') === 'dark';
    const scene = new THREE.Scene();
    scene.background = new THREE.Color(dark ? 0x0f172a : 0xeef2f7);

    const camera = new THREE.PerspectiveCamera(45, 1, 0.1, span * 60);
    camera.position.set(span * 0.9, span * 1.15, span * 1.35);

    const renderer = new THREE.WebGLRenderer({ antialias: true });
    renderer.setPixelRatio(Math.min(2, typeof window !== 'undefined' ? window.devicePixelRatio : 1));
    host.appendChild(renderer.domElement);

    scene.add(new THREE.AmbientLight(0xffffff, dark ? 0.8 : 0.95));
    const keyLight = new THREE.DirectionalLight(0xffffff, 0.7);
    keyLight.position.set(span, span * 2, span * 0.6);
    scene.add(keyLight);

    const clickable = [];
    const disposables = [];
    let pending = 0; // outstanding texture loads

    // buildFloor lays down one floor: slab + walls (grid or perimeter) + camera/node markers.
    const buildFloor = ({ e, i }, baseY, isActive) => {
      const f = e.floor || {};
      const placements = e.placements || [];
      const w = f.width || 1024;
      const h = f.height || 768;
      const mpp = mppOf(f);
      const fw = w * mpp;
      const fh = h * mpp;
      const wallH = wallHOf(f);
      const rangeM = coverageRangeM(w, h, mpp);
      const dim = stacked && !isActive;
      const toWorld = (x, y) => new THREE.Vector3((x / w - 0.5) * fw, baseY, (0.5 - y / h) * fh);

      // Floor slab, textured with the plan image (cookie-authed, same-origin).
      const slabMat = new THREE.MeshLambertMaterial({ color: dark ? 0x334155 : 0xf8fafc, transparent: dim, opacity: dim ? 0.5 : 1 });
      const slab = new THREE.Mesh(new THREE.PlaneGeometry(fw, fh), slabMat);
      slab.rotation.x = -Math.PI / 2;
      slab.position.y = baseY;
      scene.add(slab);
      pending += 1;
      new THREE.TextureLoader().load(
        `${apiBase()}/api/floors/${f.id}/image`,
        (tex) => {
          tex.colorSpace = THREE.SRGBColorSpace;
          tex.anisotropy = renderer.capabilities.getMaxAnisotropy();
          slabMat.map = tex;
          slabMat.color.set(0xffffff);
          slabMat.needsUpdate = true;
          pending -= 1;
        },
        undefined,
        () => { pending -= 1; setTexError(true); },
      );

      const grid = parseGrid(f);
      const wallMat = new THREE.MeshLambertMaterial({ color: dark ? 0x94a3b8 : 0xcbd5e1, transparent: dim, opacity: dim ? 0.3 : 1, side: THREE.DoubleSide });
      disposables.push(wallMat);
      // Segment endpoints are image-space (top-left, y-down): worldZ = (y/h - 0.5)*fh — the same
      // frame as the texture (image top at -Z) and placement markers, so walls line up with both.
      if (grid && Array.isArray(grid.segments) && grid.segments.length) {
        const thick = Math.max(0.08, (grid.unit || 20) * mpp * 0.4);
        const doors = parseDoors(f);
        const doorTol = (grid.unit || 20) * 0.6;
        const doorH = Math.min(2.1, wallH * 0.85); // metres of clear opening under the lintel
        // Build one wall box spanning parameters [t0,t1] of segment s, at the given height/offset.
        const addPiece = (s, t0, t1, height, yOff) => {
          const ax = ((s.x1 + (s.x2 - s.x1) * t0) / w - 0.5) * fw; const az = ((s.y1 + (s.y2 - s.y1) * t0) / h - 0.5) * fh;
          const bx = ((s.x1 + (s.x2 - s.x1) * t1) / w - 0.5) * fw; const bz = ((s.y1 + (s.y2 - s.y1) * t1) / h - 0.5) * fh;
          const dx = bx - ax; const dz = bz - az; const len = Math.hypot(dx, dz);
          if (len < 1e-4) return;
          const mesh = new THREE.Mesh(new THREE.BoxGeometry(len + thick, height, thick), wallMat);
          mesh.position.set((ax + bx) / 2, baseY + yOff + height / 2, (az + bz) / 2);
          mesh.rotation.y = Math.atan2(-dz, dx);
          scene.add(mesh);
        };
        grid.segments.forEach((s) => {
          const { walls, doors: openings } = carveSpans(s, doors, doorTol);
          walls.forEach(([t0, t1]) => addPiece(s, t0, t1, wallH, 0)); // full-height wall between doors
          openings.forEach(([t0, t1]) => { if (wallH - doorH > 0.02) addPiece(s, t0, t1, wallH - doorH, doorH); }); // lintel above the opening
        });
      } else if (grid && Array.isArray(grid.walls) && grid.walls.length) {
        // Legacy painted-cell grid: extrude each cell into a box (instanced).
        const cell = grid.cellPx || grid.unit;
        const cw = cell * mpp;
        const inst = new THREE.InstancedMesh(new THREE.BoxGeometry(cw, wallH, cw), wallMat, grid.walls.length);
        const m = new THREE.Matrix4();
        grid.walls.forEach(([c, r], k) => {
          m.makeTranslation(((c + 0.5) * cell / w - 0.5) * fw, baseY + wallH / 2, ((r + 0.5) * cell / h - 0.5) * fh);
          inst.setMatrixAt(k, m);
        });
        inst.instanceMatrix.needsUpdate = true;
        scene.add(inst);
      } else {
        // No grid authored: a perimeter box so the slab reads as a room.
        const th = span * 0.01;
        const mkWall = (bw, bh, bd, x, z) => { const mesh = new THREE.Mesh(new THREE.BoxGeometry(bw, bh, bd), wallMat); mesh.position.set(x, baseY + bh / 2, z); scene.add(mesh); };
        mkWall(fw, wallH, th, 0, -fh / 2);
        mkWall(fw, wallH, th, 0, fh / 2);
        mkWall(th, wallH, fh, -fw / 2, 0);
        mkWall(th, wallH, fh, fw / 2, 0);
      }

      // Straight-flight stairs: a solid run of rising steps filling the footprint from the slab up
      // to storey height, climbing in the authored ascent direction. Same image→world frame as the
      // walls, so they sit correctly over the plan and under any camera placed above them.
      const stairs = parseStairs(f);
      if (stairs.length) {
        const stairMat = new THREE.MeshLambertMaterial({ color: dark ? 0x64748b : 0x94a3b8, transparent: dim, opacity: dim ? 0.35 : 1, side: THREE.DoubleSide });
        disposables.push(stairMat);
        stairs.forEach((s) => {
          const wx1 = (Math.min(s.x1, s.x2) / w - 0.5) * fw; const wx2 = (Math.max(s.x1, s.x2) / w - 0.5) * fw;
          const wz1 = (Math.min(s.y1, s.y2) / h - 0.5) * fh; const wz2 = (Math.max(s.y1, s.y2) / h - 0.5) * fh;
          const dx = wx2 - wx1; const dz = wz2 - wz1;
          if (dx < 1e-3 || dz < 1e-3) return;
          const dir = s.dir || 'n';
          const vertical = dir === 'n' || dir === 's'; // ascent runs along z (image y)
          const N = Math.max(3, Math.min(30, Math.round(wallH / 0.18)));
          const stepH = wallH / N;
          for (let k = 0; k < N; k++) {
            const topH = stepH * (k + 1); // each tread stacks a little taller toward the top of the run
            let bw; let bd; let cx; let cz;
            if (vertical) {
              bw = dx; bd = dz / N; cx = (wx1 + wx2) / 2;
              cz = dir === 'n' ? wz2 - (k + 0.5) * bd : wz1 + (k + 0.5) * bd;
            } else {
              bd = dz; bw = dx / N; cz = (wz1 + wz2) / 2;
              cx = dir === 'w' ? wx2 - (k + 0.5) * bw : wx1 + (k + 0.5) * bw;
            }
            const box = new THREE.Mesh(new THREE.BoxGeometry(bw, topH, bd), stairMat);
            box.position.set(cx, baseY + topH / 2, cz);
            scene.add(box);
          }
        });
      }

      // Markers.
      const markerR = Math.max(0.18, span * 0.02);
      placements.forEach((p) => {
        const tone = nodeTone(nodesById[p.nodeId], nowSec);
        const color = new THREE.Color(tone.color);
        const isCam = !!p.cameraId;
        const mountH = isCam ? (p.mountHeight > 0 ? p.mountHeight : DEFAULT_MOUNT_H) : Math.max(0.4, span * 0.03);
        const base = toWorld(p.x, p.y);
        const apex = base.clone().setY(baseY + mountH);
        const focused = isCam && focusCameraId && String(p.cameraId) === String(focusCameraId);

        const bodyGeo = isCam ? new THREE.BoxGeometry(markerR * 1.6, markerR * 1.1, markerR * 2.2) : new THREE.CylinderGeometry(markerR, markerR, markerR * 1.2, 16);
        const bodyMat = new THREE.MeshStandardMaterial({ color, emissive: focused ? color : 0x000000, emissiveIntensity: focused ? 0.6 : 0, metalness: 0.1, roughness: 0.6, transparent: dim, opacity: dim ? 0.55 : 1 });
        const body = new THREE.Mesh(bodyGeo, bodyMat);
        body.position.copy(apex);
        body.userData = { placement: p, name: p.lastKnownName || (isCam ? `Camera ${p.cameraId}` : (nodesById[p.nodeId]?.name || p.nodeId)), isCam };
        scene.add(body);
        if (isCam) clickable.push(body);

        if (isCam && (p.fov || 0) > 0) {
          const headRad = ((p.heading || 0) * Math.PI) / 180;
          const pitchRad = ((p.pitch > 0 ? p.pitch : DEFAULT_PITCH) * Math.PI) / 180;
          const horiz = Math.cos(pitchRad) * rangeM;
          const target = new THREE.Vector3(
            apex.x + Math.sin(headRad) * horiz,
            Math.max(baseY, apex.y - Math.sin(pitchRad) * rangeM),
            apex.z - Math.cos(headRad) * horiz,
          );
          scene.add(coneToward(apex, target, p.fov, color));
        }
        const line = new THREE.Line(
          new THREE.BufferGeometry().setFromPoints([apex, base]),
          new THREE.LineBasicMaterial({ color, transparent: true, opacity: dim ? 0.25 : 0.5 }),
        );
        scene.add(line);
      });
    };

    shown.forEach((item, idx) => buildFloor(item, elevations[idx], item.i === activeIndex));

    const controls = new OrbitControls(camera, renderer.domElement);
    controls.enableDamping = true;
    controls.dampingFactor = 0.08;
    controls.maxPolarAngle = Math.PI / 2 - 0.02;
    controls.minDistance = span * 0.35;
    controls.maxDistance = span * 8;
    controls.target.set(0, (cum || wallHOf(aFloor)) * 0.35, 0);

    const resize = () => {
      const rect = host.getBoundingClientRect();
      const cw = Math.max(1, rect.width);
      const ch = Math.max(1, rect.height);
      renderer.setSize(cw, ch, false);
      camera.aspect = cw / ch;
      camera.updateProjectionMatrix();
    };
    resize();
    const ro = typeof ResizeObserver !== 'undefined' ? new ResizeObserver(resize) : null;
    if (ro) ro.observe(host); else if (typeof window !== 'undefined') window.addEventListener('resize', resize);

    const raycaster = new THREE.Raycaster();
    const ndc = new THREE.Vector2();
    let down = null;
    const relXY = (ev) => { const r = renderer.domElement.getBoundingClientRect(); return { x: ev.clientX - r.left, y: ev.clientY - r.top, cw: r.width, ch: r.height }; };
    const pick = (ev) => {
      const { x, y, cw, ch } = relXY(ev);
      ndc.set((x / cw) * 2 - 1, -(y / ch) * 2 + 1);
      raycaster.setFromCamera(ndc, camera);
      return raycaster.intersectObjects(clickable, false)[0];
    };
    const onDown = (ev) => { down = relXY(ev); };
    const onUp = (ev) => {
      if (!down) return;
      const cur = relXY(ev);
      const moved = Math.hypot(cur.x - down.x, cur.y - down.y);
      down = null;
      if (moved > 5) return;
      const hit = pick(ev);
      if (hit && onPlay) {
        const p = hit.object.userData.placement;
        onPlay({ nodeId: p.nodeId, cameraId: p.cameraId, name: hit.object.userData.name, ptzSupported: !!p.ptzSupported }, ev.clientX, ev.clientY);
      }
    };
    const onMove = (ev) => {
      const hit = pick(ev);
      renderer.domElement.style.cursor = hit ? 'pointer' : 'grab';
      if (hit) { const r = relXY(ev); setHover({ name: hit.object.userData.name, x: r.x, y: r.y }); }
      else setHover(null);
    };
    renderer.domElement.addEventListener('pointerdown', onDown);
    renderer.domElement.addEventListener('pointerup', onUp);
    renderer.domElement.addEventListener('pointermove', onMove);
    renderer.domElement.addEventListener('pointerleave', () => setHover(null));

    let raf = 0;
    const tick = () => { controls.update(); renderer.render(scene, camera); raf = requestAnimationFrame(tick); };
    tick();

    disposeRef.current = () => {
      cancelAnimationFrame(raf);
      if (ro) ro.disconnect(); else if (typeof window !== 'undefined') window.removeEventListener('resize', resize);
      renderer.domElement.removeEventListener('pointerdown', onDown);
      renderer.domElement.removeEventListener('pointerup', onUp);
      renderer.domElement.removeEventListener('pointermove', onMove);
      controls.dispose();
      scene.traverse((o) => {
        if (o.geometry) o.geometry.dispose();
        if (o.material) { const mats = Array.isArray(o.material) ? o.material : [o.material]; mats.forEach((mm) => { if (mm.map) mm.map.dispose(); mm.dispose(); }); }
      });
      disposables.forEach((d) => d.dispose && d.dispose());
      renderer.dispose();
      if (renderer.domElement.parentNode === host) host.removeChild(renderer.domElement);
      void pending;
    };
    return () => { if (disposeRef.current) disposeRef.current(); disposeRef.current = null; };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [sig]);

  return (
    <div className="floor3d" ref={mountRef}>
      {texError ? <div className="floor3d-note">{t('map.plan3dNoImage')}</div> : null}
      {hover ? <div className="floor3d-label" style={{ left: hover.x + 12, top: hover.y + 12 }}>{hover.name}</div> : null}
    </div>
  );
}

Floor3D.propTypes = {
  floors: PropTypes.array, // [{ floor, placements }]
  activeIndex: PropTypes.number,
  stacked: PropTypes.bool,
  nodesById: PropTypes.object,
  focusCameraId: PropTypes.any,
  nowSec: PropTypes.number,
  onPlay: PropTypes.func,
};
