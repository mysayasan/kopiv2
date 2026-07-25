import { useEffect, useMemo, useRef, useState } from 'react';
import PropTypes from 'prop-types';
import * as THREE from 'three';
import { sillOf, headOf, openingSpanOnSeg, remainingSpans, pointInRotatedRect, rectCorners } from './plan_geometry';
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
// parseList reads one of the additive model arrays (doors, windows, parking). A floor authored
// before a given array existed simply has no such key, which reads as empty.
function parseList(floor, key) {
  if (!floor || !floor.grid) return [];
  try {
    const g = typeof floor.grid === 'string' ? JSON.parse(floor.grid) : floor.grid;
    return g && Array.isArray(g[key]) ? g[key] : [];
  } catch (_) { return []; }
}
const parseDoors = (floor) => parseList(floor, 'doors');
const parseWindows = (floor) => parseList(floor, 'windows');
const parseParking = (floor) => parseList(floor, 'parking');
const parsePlatforms = (floor) => parseList(floor, 'platforms');


// Severity colours for the unread-notification badge, matching the 2D plan markers.
const SEV_BADGE = { critical: '#ef4444', warning: '#f59e0b', info: '#2d6cdf' };
const PULSE_MS = 1600;

// badgeSprite draws the unread count on a canvas and returns it as a camera-facing sprite. Sprites
// always billboard, so the number stays readable from any orbit angle; depthTest off keeps it
// visible when the camera it belongs to is behind a wall.
function badgeSprite(count, color) {
  const size = 128;
  const cv = document.createElement('canvas');
  cv.width = size; cv.height = size;
  const c2 = cv.getContext('2d');
  c2.beginPath(); c2.arc(size / 2, size / 2, size * 0.4, 0, Math.PI * 2);
  c2.fillStyle = color; c2.fill();
  c2.lineWidth = size * 0.08; c2.strokeStyle = '#ffffff'; c2.stroke();
  const label = count > 99 ? '99+' : String(count);
  c2.fillStyle = '#ffffff';
  c2.font = `bold ${label.length > 2 ? size * 0.34 : size * 0.46}px system-ui, sans-serif`;
  c2.textAlign = 'center'; c2.textBaseline = 'middle';
  c2.fillText(label, size / 2, size / 2 + size * 0.03);
  const tex = new THREE.CanvasTexture(cv);
  tex.colorSpace = THREE.SRGBColorSpace;
  const sprite = new THREE.Sprite(new THREE.SpriteMaterial({ map: tex, transparent: true, depthTest: false, depthWrite: false }));
  sprite.renderOrder = 10;
  return sprite;
}

export default function Floor3D({ floors = [], activeIndex = 0, stacked = false, nodesById = {}, notifByCam = {}, focusCameraId, nowSec, onPlay }) {
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
      // The unread count/severity is part of the signature: a new alert must rebuild the scene so
      // the badge and its pulse appear (or clear) without waiting for some other input to change.
      p: (e.placements || []).map((p) => {
        const nt = p.cameraId ? notifByCam[`${p.nodeId}::${p.cameraId}`] : null;
        return `${p.id}:${p.x},${p.y},${p.heading},${p.fov},${p.mountHeight || ''},${p.pitch || ''},${p.cameraId || ''},${nt ? `${nt.count}${nt.sev}` : ''}`;
      }),
    };
  })) + `|${activeIndex}|${stacked}|${focusCameraId}|${nowSec}`, [floors, activeIndex, stacked, focusCameraId, nowSec, notifByCam]);

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
    const pulses = []; // notification beacons animated in the render loop
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

      // Floor slab, textured with the plan image (cookie-authed, same-origin). A DOWN stair descends
      // below the slab, so the slab is cut around each one — otherwise the opaque floor covers the
      // descent and it reads as being buried in concrete. The stairwell openings are the down-stair
      // footprints (image-space AABBs); the slab is split into the rectangles left around them, each
      // a textured plane with UVs taken from its window of the plan image so the picture still lines
      // up. No down stairs → the ordinary single plane.
      const slabMat = new THREE.MeshLambertMaterial({ color: dark ? 0x334155 : 0xf8fafc, transparent: dim, opacity: dim ? 0.5 : 1, side: THREE.DoubleSide });
      const downHoles = parseStairs(f).filter((s) => s.down).map((s) => {
        const cn = rectCorners(s);
        return { x1: Math.min(...cn.map((c) => c.x)), y1: Math.min(...cn.map((c) => c.y)), x2: Math.max(...cn.map((c) => c.x)), y2: Math.max(...cn.map((c) => c.y)) };
      });
      if (downHoles.length) {
        const rectMinus = (r, hole) => {
          const ix1 = Math.max(r.x1, hole.x1); const iy1 = Math.max(r.y1, hole.y1);
          const ix2 = Math.min(r.x2, hole.x2); const iy2 = Math.min(r.y2, hole.y2);
          if (ix1 >= ix2 || iy1 >= iy2) return [r];
          const out = [];
          if (r.y1 < iy1) out.push({ x1: r.x1, y1: r.y1, x2: r.x2, y2: iy1 });
          if (iy2 < r.y2) out.push({ x1: r.x1, y1: iy2, x2: r.x2, y2: r.y2 });
          if (r.x1 < ix1) out.push({ x1: r.x1, y1: iy1, x2: ix1, y2: iy2 });
          if (ix2 < r.x2) out.push({ x1: ix2, y1: iy1, x2: r.x2, y2: iy2 });
          return out;
        };
        let pieces = [{ x1: 0, y1: 0, x2: w, y2: h }];
        downHoles.forEach((hole) => { pieces = pieces.flatMap((r) => rectMinus(r, hole)); });
        pieces.forEach((r) => {
          const worldW = ((r.x2 - r.x1) / w) * fw; const worldD = ((r.y2 - r.y1) / h) * fh;
          if (worldW < 1e-3 || worldD < 1e-3) return;
          const geo = new THREE.PlaneGeometry(worldW, worldD);
          // Vertex order for a 1×1 PlaneGeometry: top-left, top-right, bottom-left, bottom-right.
          const u1 = r.x1 / w; const u2 = r.x2 / w; const v1 = 1 - r.y1 / h; const v2 = 1 - r.y2 / h;
          const uv = geo.attributes.uv; uv.setXY(0, u1, v1); uv.setXY(1, u2, v1); uv.setXY(2, u1, v2); uv.setXY(3, u2, v2); uv.needsUpdate = true;
          const mesh = new THREE.Mesh(geo, slabMat);
          mesh.rotation.x = -Math.PI / 2;
          const cix = (r.x1 + r.x2) / 2; const ciy = (r.y1 + r.y2) / 2;
          mesh.position.set((cix / w - 0.5) * fw, baseY, (0.5 - ciy / h) * fh);
          scene.add(mesh);
        });
      } else {
        const slab = new THREE.Mesh(new THREE.PlaneGeometry(fw, fh), slabMat);
        slab.rotation.x = -Math.PI / 2;
        slab.position.y = baseY;
        scene.add(slab);
      }
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
        const windows = parseWindows(f);
        const doorTol = (grid.unit || 20) * 0.6;
        const doorH = Math.min(2.1, wallH * 0.85); // metres of clear opening under the lintel
        // Build one wall box spanning parameters [t0,t1] of segment s, at the given height/offset.
        // Tinted glass for window openings — thinner than the wall so the reveal still reads.
        const glassMat = new THREE.MeshLambertMaterial({ color: 0x7dd3fc, transparent: true, opacity: dim ? 0.14 : 0.34, side: THREE.DoubleSide, depthWrite: false });
        disposables.push(glassMat);
        const addPiece = (s, t0, t1, height, yOff, mat, thickScale) => {
          const ax = ((s.x1 + (s.x2 - s.x1) * t0) / w - 0.5) * fw; const az = ((s.y1 + (s.y2 - s.y1) * t0) / h - 0.5) * fh;
          const bx = ((s.x1 + (s.x2 - s.x1) * t1) / w - 0.5) * fw; const bz = ((s.y1 + (s.y2 - s.y1) * t1) / h - 0.5) * fh;
          const dx = bx - ax; const dz = bz - az; const len = Math.hypot(dx, dz);
          if (len < 1e-4) return;
          const th = thick * (thickScale || 1);
          const mesh = new THREE.Mesh(new THREE.BoxGeometry(len + (thickScale ? 0 : thick), height, th), mat || wallMat);
          mesh.position.set((ax + bx) / 2, baseY + yOff + height / 2, (az + bz) / 2);
          mesh.rotation.y = Math.atan2(-dz, dx);
          scene.add(mesh);
        };
        grid.segments.forEach((s) => {
          const walls = remainingSpans(s, doors.concat(windows), doorTol);
          walls.forEach(([t0, t1]) => addPiece(s, t0, t1, wallH, 0)); // full-height wall between openings
          // A door leaves a lintel above it and nothing below — you walk through.
          doors.forEach((d) => {
            const iv = openingSpanOnSeg(s, d, doorTol);
            if (iv && wallH - doorH > 0.02) addPiece(s, iv[0], iv[1], wallH - doorH, doorH);
          });
          // A window leaves wall BELOW (up to the sill) and ABOVE (from the head), which is the
          // whole reason it differs from a door: a camera's sight line passes only through the gap.
          windows.forEach((d) => {
            const iv = openingSpanOnSeg(s, d, doorTol);
            if (!iv) return;
            const sill = Math.min(sillOf(d), wallH);
            const head = Math.min(headOf(d), wallH);
            if (sill > 0.02) addPiece(s, iv[0], iv[1], sill, 0);
            if (wallH - head > 0.02) addPiece(s, iv[0], iv[1], wallH - head, head);
            // Glazing fills what is left. Without it a window is just a hole and reads exactly like
            // a doorway with an odd lintel; a tinted pane says "you can see through here but not
            // walk through", which is the distinction the whole feature exists to make.
            if (head - sill > 0.05) addPiece(s, iv[0], iv[1], head - sill, sill, glassMat, 0.35);
          });
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
      }
      // No fallback perimeter: a floor shows exactly the walls that were drawn. An outer wall is
      // something the operator authors, not something the viewer invents.

      // Parking bays (outdoor areas): ground markings, not solids — a painted bay has no height, so
      // it is drawn as thin strips lying on the slab. Without this the 3D view would show a car park
      // as bare ground while the 2D plan showed its rows.
      const parking = parseParking(f);
      if (parking.length) {
        const markMat = new THREE.MeshBasicMaterial({ color: dark ? 0x94a3b8 : 0x64748b, transparent: true, opacity: dim ? 0.25 : 0.7 });
        disposables.push(markMat);
        const STRIPE = Math.max(0.06, 0.12 * mpp * 20); // a painted line, in metres
        parking.forEach((p) => {
          const wx1 = (Math.min(p.x1, p.x2) / w - 0.5) * fw; const wx2 = (Math.max(p.x1, p.x2) / w - 0.5) * fw;
          const wz1 = (Math.min(p.y1, p.y2) / h - 0.5) * fh; const wz2 = (Math.max(p.y1, p.y2) / h - 0.5) * fh;
          const dx = wx2 - wx1; const dz = wz2 - wz1;
          if (dx < 1e-3 || dz < 1e-3) return;
          const n = Math.max(1, Math.min(60, p.bays || 1));
          const acrossX = dx >= dz; // bays divide along the longer side, mirroring the 2D editor
          // The row is built flat and then turned as a whole: image-space angles run clockwise with
          // y down, three.js y-rotation runs the other way, hence the negation.
          const group = new THREE.Group();
          group.position.set((wx1 + wx2) / 2, baseY + 0.006, (wz1 + wz2) / 2);
          group.rotation.y = -(p.a || 0);
          const strip = (cx, cz, sw, sd) => {
            const mesh = new THREE.Mesh(new THREE.BoxGeometry(sw, 0.01, sd), markMat);
            mesh.position.set(cx - (wx1 + wx2) / 2, 0, cz - (wz1 + wz2) / 2); // local to the group
            group.add(mesh);
          };
          // outline
          strip((wx1 + wx2) / 2, wz1, dx, STRIPE); strip((wx1 + wx2) / 2, wz2, dx, STRIPE);
          strip(wx1, (wz1 + wz2) / 2, STRIPE, dz); strip(wx2, (wz1 + wz2) / 2, STRIPE, dz);
          // bay dividers
          for (let k = 1; k < n; k++) {
            const fr = k / n;
            if (acrossX) strip(wx1 + dx * fr, (wz1 + wz2) / 2, STRIPE, dz);
            else strip((wx1 + wx2) / 2, wz1 + dz * fr, dx, STRIPE);
          }
          scene.add(group);
        });
      }

      // Raised floors: a solid slab lifted `rise` metres above the storey floor — a platform you
      // reach by stairs. Built axis-aligned then turned as a whole (image angles are clockwise,
      // three.js is not), so a rotated platform keeps its footprint. A stair that overlaps a platform
      // sits ON TOP of it (its base is lifted to the slab, see the stair block below), so the slab is
      // a plain full box here.
      const platforms = parsePlatforms(f);
      if (platforms.length) {
        const platMat = new THREE.MeshLambertMaterial({ color: dark ? 0x92826a : 0xd6c39a, transparent: dim, opacity: dim ? 0.35 : 1, side: THREE.DoubleSide });
        disposables.push(platMat);
        platforms.forEach((p) => {
          const wx1 = (Math.min(p.x1, p.x2) / w - 0.5) * fw; const wx2 = (Math.max(p.x1, p.x2) / w - 0.5) * fw;
          const wz1 = (Math.min(p.y1, p.y2) / h - 0.5) * fh; const wz2 = (Math.max(p.y1, p.y2) / h - 0.5) * fh;
          const dxp = wx2 - wx1; const dzp = wz2 - wz1;
          if (dxp < 1e-3 || dzp < 1e-3) return;
          const rise = p.rise > 0 ? p.rise : 0.6;
          const box = new THREE.Mesh(new THREE.BoxGeometry(dxp, rise, dzp), platMat);
          box.position.set(0, rise / 2, 0);
          const group = new THREE.Group();
          group.position.set((wx1 + wx2) / 2, baseY, (wz1 + wz2) / 2);
          group.rotation.y = -(p.a || 0);
          group.add(box);
          scene.add(group);
        });
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
          // A stair whose CENTRE sits on a raised floor rests ON IT: its base is lifted to the
          // platform height and it climbs from there. (Centre, not any overlap, matches the 2D lock —
          // the drop snaps the stair fully onto the slab, so nothing floats.) Else the base is 0.
          let base = 0; let best = -1;
          const scx = (s.x1 + s.x2) / 2; const scy = (s.y1 + s.y2) / 2;
          platforms.forEach((pl) => { if (pointInRotatedRect(scx, scy, pl)) { const r = pl.rise > 0 ? pl.rise : 0.6; if (r > best) best = r; } });
          if (best > 0) base = best;
          // The flight's OWN climb height (a full storey by default), kept regardless of the base it
          // rests on — so a stair on a raised floor is the same flight, just lifted, not flattened.
          const climbH = s.height > 0 ? s.height : wallH;
          // Step count matches the 2D plan: the stair's own setting, or a default from the climb.
          const N = Math.max(2, Math.min(40, s.steps || Math.round(climbH / 0.18)));
          const stepH = climbH / N;
          // The flight is built axis-aligned then turned as a whole, so a rotated stair keeps its
          // step spacing and its ascent direction (image angles are clockwise, three.js is not).
          const gx = (wx1 + wx2) / 2; const gz = (wz1 + wz2) / 2;
          const group = new THREE.Group();
          group.position.set(gx, baseY, gz);
          group.rotation.y = -(s.a || 0);
          for (let k = 0; k < N; k++) {
            // Going up, each slice is solid from the base up to its tread (near end short, far end
            // full). Going down, the slices descend below the base to the level below — the same
            // profile, mirrored, so a down stair reads as a descent to a lower floor / basement.
            const boxH = s.down ? Math.max(0.01, climbH - stepH * k) : stepH * (k + 1);
            const cy = s.down ? base - (stepH * k + climbH) / 2 : base + boxH / 2;
            let bw; let bd; let cx; let cz;
            if (vertical) {
              bw = dx; bd = dz / N; cx = (wx1 + wx2) / 2;
              cz = dir === 'n' ? wz2 - (k + 0.5) * bd : wz1 + (k + 0.5) * bd;
            } else {
              bd = dz; bw = dx / N; cz = (wz1 + wz2) / 2;
              cx = dir === 'w' ? wx2 - (k + 0.5) * bw : wx1 + (k + 0.5) * bw;
            }
            const box = new THREE.Mesh(new THREE.BoxGeometry(bw, boxH, bd), stairMat);
            box.position.set(cx - gx, cy, cz - gz);
            group.add(box);
          }
          scene.add(group);
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

        // A camera with unread notifications gets the same treatment as on the 2D plan: a count
        // badge, plus a pulse on the floor beneath it so it catches the eye while orbiting.
        const notif = isCam ? notifByCam[`${p.nodeId}::${p.cameraId}`] : null;
        if (notif && notif.count > 0) {
          const sevColor = SEV_BADGE[notif.sev] || SEV_BADGE.info;
          // The badge shows on every floor — an alert one storey down should not be invisible in
          // the stacked view — but only the floor you are looking at pulses, to bound the motion.
          const badge = badgeSprite(notif.count, sevColor);
          const bs = markerR * 2.4;
          badge.scale.set(bs, bs, 1);
          badge.position.copy(apex).setY(apex.y + markerR * 2.2);
          scene.add(badge);
          if (!dim) {
            // Two offset waves, so one is always mid-flight — matches the map marker's beacon.
            const maxR = Math.max(markerR * 4, span * 0.05);
            [0, 0.5].forEach((offset) => {
              const ringMat = new THREE.MeshBasicMaterial({ color: new THREE.Color(sevColor), transparent: true, opacity: 0.5, side: THREE.DoubleSide, depthWrite: false });
              const ring = new THREE.Mesh(new THREE.RingGeometry(0.86, 1, 40), ringMat);
              ring.rotation.x = -Math.PI / 2;
              ring.position.set(base.x, baseY + 0.03, base.z);
              scene.add(ring);
              pulses.push({ mesh: ring, mat: ringMat, maxR, offset });
            });
          }
        }
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
    const tick = () => {
      controls.update();
      if (pulses.length) {
        const now = typeof performance !== 'undefined' ? performance.now() : 0;
        pulses.forEach((pl) => {
          const ph = ((now / PULSE_MS) + pl.offset) % 1;
          pl.mesh.scale.setScalar(Math.max(0.001, pl.maxR * (0.25 + 0.75 * ph)));
          pl.mat.opacity = 0.5 * (1 - ph);
        });
      }
      renderer.render(scene, camera);
      raf = requestAnimationFrame(tick);
    };
    tick();

    disposeRef.current = () => {
      cancelAnimationFrame(raf);
      if (ro) ro.disconnect(); else if (typeof window !== 'undefined') window.removeEventListener('resize', resize);
      renderer.domElement.removeEventListener('pointerdown', onDown);
      renderer.domElement.removeEventListener('pointerup', onUp);
      renderer.domElement.removeEventListener('pointermove', onMove);
      controls.dispose();
      scene.traverse((o) => {
        // Sprites share ONE module-level geometry inside three.js — disposing it here would pull it
        // out from under every sprite created afterwards, so only their material/texture is freed.
        if (o.geometry && !o.isSprite) o.geometry.dispose();
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
  notifByCam: PropTypes.object, // "nodeId::cameraId" -> { count, sev }
  focusCameraId: PropTypes.any,
  nowSec: PropTypes.number,
  onPlay: PropTypes.func,
};
