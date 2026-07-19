import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import PropTypes from 'prop-types';
import { useT, Ico } from '@shared';
import { api, apiBase } from '../lib/helpers';
import { NodeCameraTile } from './node_manager';
import { PTZRing } from './nodecam/ptz';
import { nodeTone } from '../lib/fleet_status';

// A placement's stored (x, y) is in the floor's OpenLayers pixel projection (origin bottom-
// left, y up). To position a marker over a plain <img> (origin top-left) we flip y.
function markerPos(p, w, h) {
  const left = Math.max(0, Math.min(100, (p.x / w) * 100));
  const top = Math.max(0, Math.min(100, ((h - p.y) / h) * 100));
  return { left: `${left}%`, top: `${top}%` };
}

// fovRadius scales the coverage wedge to the plan (matches the editor's arcRadius).
function fovRadius(w, h) {
  return Math.max(50, Math.min(w, h) * 0.16);
}

// fovPath builds the SVG path (in image-pixel/viewBox coords, y-DOWN) for a camera's coverage
// wedge: a fan from the marker spanning `fov` degrees, centred on `heading` (clockwise from up).
function fovPath(p, w, h) {
  const cx = p.x;
  const cy = h - p.y; // flip y into the SVG's top-left origin
  const r = fovRadius(w, h);
  const half = (p.fov || 0) / 2;
  const N = 24;
  let d = `M ${cx.toFixed(1)} ${cy.toFixed(1)}`;
  for (let i = 0; i <= N; i++) {
    const deg = (p.heading || 0) - half + (p.fov || 0) * (i / N);
    const rad = (deg * Math.PI) / 180;
    d += ` L ${(cx + r * Math.sin(rad)).toFixed(1)} ${(cy - r * Math.cos(rad)).toFixed(1)}`;
  }
  return `${d} Z`;
}

const MINI_W = 340;
const MINI_H = 224;
const MIN_W = 240; // smallest the user can shrink a floating window to
const MIN_H = 150;

// Keep a window's top-left inside the viewport (with an 8px margin), given its size.
function clampToViewport(left, top, w = MINI_W, h = MINI_H) {
  const vw = typeof window !== 'undefined' ? window.innerWidth : 1280;
  const vh = typeof window !== 'undefined' ? window.innerHeight : 800;
  return {
    left: Math.max(8, Math.min(left, vw - w - 8)),
    top: Math.max(8, Math.min(top, vh - h - 8)),
  };
}

// useFloatingWindow drives a floating mini window's position AND size. It seeds a spot near the
// click, exposes `onHeadPointerDown` to DRAG by the title bar, and `onResizePointerDown` to RESIZE
// from the bottom-right grip (clamped to a minimum size and to the viewport). Shared by LiveWindow
// and MediaWindow so both float, drag, resize, and clamp identically.
function useFloatingWindow(x, y, maximized, initW = MINI_W, initH = MINI_H) {
  const [size, setSize] = useState({ w: initW, h: initH });
  const [pos, setPos] = useState(() => clampToViewport((x || 0) + 12, (y || 0) - initH / 2, initW, initH));
  const drag = useRef(null);   // { sx, sy, left, top } while dragging
  const resize = useRef(null); // { sx, sy, w, h, left, top } while resizing

  const onPointerMove = useCallback((e) => {
    if (drag.current) {
      const d = drag.current;
      setPos(clampToViewport(d.left + (e.clientX - d.sx), d.top + (e.clientY - d.sy), d.w, d.h));
      return;
    }
    if (resize.current) {
      const r = resize.current;
      const vw = typeof window !== 'undefined' ? window.innerWidth : 1280;
      const vh = typeof window !== 'undefined' ? window.innerHeight : 800;
      const w = Math.max(MIN_W, Math.min(r.w + (e.clientX - r.sx), vw - r.left - 8));
      const h = Math.max(MIN_H, Math.min(r.h + (e.clientY - r.sy), vh - r.top - 8));
      setSize({ w, h });
    }
  }, []);
  const onPointerUp = useCallback(() => {
    drag.current = null;
    resize.current = null;
    window.removeEventListener('pointermove', onPointerMove);
    window.removeEventListener('pointerup', onPointerUp);
  }, [onPointerMove]);
  const onHeadPointerDown = useCallback((e) => {
    // Don't start a drag from the action buttons, and there's nothing to drag when maximised.
    if (maximized || (e.target.closest && e.target.closest('button'))) return;
    e.preventDefault();
    drag.current = { sx: e.clientX, sy: e.clientY, left: pos.left, top: pos.top, w: size.w, h: size.h };
    window.addEventListener('pointermove', onPointerMove);
    window.addEventListener('pointerup', onPointerUp);
  }, [maximized, pos, size, onPointerMove, onPointerUp]);
  const onResizePointerDown = useCallback((e) => {
    if (maximized) return;
    e.preventDefault();
    e.stopPropagation();
    resize.current = { sx: e.clientX, sy: e.clientY, w: size.w, h: size.h, left: pos.left, top: pos.top };
    window.addEventListener('pointermove', onPointerMove);
    window.addEventListener('pointerup', onPointerUp);
  }, [maximized, size, pos, onPointerMove, onPointerUp]);

  return { pos, size, onHeadPointerDown, onResizePointerDown };
}

// WindowPTZ overlays the shared pan/tilt ring on a floating live window, routing commands over the
// node's control tunnel (the same proxy path the camera page and video wall use), so PTZ works
// cross-network. Rendered only for cameras that advertise PTZ.
function WindowPTZ({ nodeId, cameraId }) {
  const [busy, setBusy] = useState(false);
  const px = useCallback(
    (path, opts = {}) => api(`/api/nodes/${encodeURIComponent(nodeId)}/proxy${path}`, { noRedirect: true, ...opts }).catch(() => ({ ok: false })),
    [nodeId],
  );
  const move = async (dir) => {
    setBusy(true);
    await px(`/api/cameras/${cameraId}/ptz/move`, { method: 'POST', body: JSON.stringify({ direction: dir, speed: 0.35, durationMs: 0 }) });
    setBusy(false);
  };
  const stop = () => px(`/api/cameras/${cameraId}/ptz/stop`, { method: 'POST' });
  return (
    <div className="ptz-ring-overlay floor-live-ptz">
      <PTZRing busy={busy} size={92} onMove={move} onStop={stop} />
    </div>
  );
}
WindowPTZ.propTypes = { nodeId: PropTypes.string, cameraId: PropTypes.any };

// LiveWindow plays a camera's LIVE footage in a floating on-screen window. It opens SMALL,
// anchored near where the camera was clicked (x, y are viewport coords), can be DRAGGED around
// by its title bar, and MAXIMISES to cover the whole screen. Several can be open at once. The
// shared NodeCameraTile is kept MOUNTED across the small⇄maximised toggle and across drags so
// the WebRTC stream is never torn down and restarted; it tears down only when this window
// unmounts (closed).
export function LiveWindow({ nodeId, cameraId, name, iceServers, ptzSupported, x, y, onClose }) {
  const t = useT();
  const [maximized, setMaximized] = useState(false);
  const { pos, size, onHeadPointerDown, onResizePointerDown } = useFloatingWindow(x, y, maximized);

  const miniStyle = maximized ? undefined : { left: pos.left, top: pos.top, width: size.w, height: size.h };

  return (
    <div className={`floor-live${maximized ? ' maximized' : ' mini'}`} style={miniStyle}>
      <div className="floor-live-head" onPointerDown={onHeadPointerDown}>
        <span className="floor-live-name"><Ico n="video" sz={13} /> {name}</span>
        <span className="floor-live-actions">
          {maximized ? (
            <button type="button" className="icon-button" onClick={() => setMaximized(false)} title={t('map.restore')} aria-label={t('map.restore')}><Ico n="arr-left" sz={14} /></button>
          ) : (
            <button type="button" className="icon-button" onClick={() => setMaximized(true)} title={t('map.maximize')} aria-label={t('map.maximize')}><Ico n="grid2" sz={13} /></button>
          )}
          <button type="button" className="icon-button" onClick={onClose} title={t('nset.close')} aria-label={t('nset.close')}><Ico n="x" sz={14} /></button>
        </span>
      </div>
      <div
        className="floor-live-body"
        onDoubleClick={(e) => { if (e.target.closest && e.target.closest('.floor-live-ptz')) return; setMaximized((m) => !m); }}
      >
        <NodeCameraTile nodeId={nodeId} cam={{ id: cameraId, name }} iceServers={iceServers} />
        {ptzSupported ? <WindowPTZ nodeId={nodeId} cameraId={cameraId} /> : null}
      </div>
      {!maximized ? <div className="floor-live-resize" onPointerDown={onResizePointerDown} title={t('map.resize')} aria-hidden="true" /> : null}
    </div>
  );
}
LiveWindow.propTypes = { nodeId: PropTypes.string, cameraId: PropTypes.any, name: PropTypes.string, iceServers: PropTypes.array, ptzSupported: PropTypes.bool, x: PropTypes.number, y: PropTypes.number, onClose: PropTypes.func };

// MediaWindow shows the RECORDED footage of a past event — the annotated detection snapshot,
// and (when the event has a recorded clip) its video — in the same floating, draggable,
// maximisable window as LiveWindow. Opened by clicking an event with footage in the node popup,
// so reviewing an alert never leaves the map. Both the snapshot and clip are streamed
// same-origin through myseliasan's node proxy / recording-stream, so plain <img>/<video> work.
export function MediaWindow({ name, snapshotSrc, clipSrc, x, y, onClose }) {
  const t = useT();
  const [maximized, setMaximized] = useState(false);
  const [mode, setMode] = useState('image'); // 'image' | 'video'
  const [imgFailed, setImgFailed] = useState(false);
  const { pos, size, onHeadPointerDown, onResizePointerDown } = useFloatingWindow(x, y, maximized);

  const miniStyle = maximized ? undefined : { left: pos.left, top: pos.top, width: size.w, height: size.h };
  const showVideo = mode === 'video' && !!clipSrc;

  return (
    <div className={`floor-live media${maximized ? ' maximized' : ' mini'}`} style={miniStyle}>
      <div className="floor-live-head" onPointerDown={onHeadPointerDown}>
        <span className="floor-live-name"><Ico n={showVideo ? 'play' : 'camera'} sz={13} /> {name}</span>
        <span className="floor-live-actions">
          {clipSrc ? (
            showVideo ? (
              <button type="button" className="icon-button" onClick={() => setMode('image')} title={t('map.showSnapshot')} aria-label={t('map.showSnapshot')}><Ico n="camera" sz={13} /></button>
            ) : (
              <button type="button" className="icon-button" onClick={() => setMode('video')} title={t('map.playClip')} aria-label={t('map.playClip')}><Ico n="play" sz={13} /></button>
            )
          ) : null}
          {maximized ? (
            <button type="button" className="icon-button" onClick={() => setMaximized(false)} title={t('map.restore')} aria-label={t('map.restore')}><Ico n="arr-left" sz={14} /></button>
          ) : (
            <button type="button" className="icon-button" onClick={() => setMaximized(true)} title={t('map.maximize')} aria-label={t('map.maximize')}><Ico n="grid2" sz={13} /></button>
          )}
          <button type="button" className="icon-button" onClick={onClose} title={t('nset.close')} aria-label={t('nset.close')}><Ico n="x" sz={14} /></button>
        </span>
      </div>
      <div className="floor-live-body media" onDoubleClick={() => setMaximized((m) => !m)}>
        {showVideo ? (
          <video className="floor-live-video" src={clipSrc} controls autoPlay />
        ) : imgFailed ? (
          <div className="floor-live-media-ph"><Ico n="camera" sz={22} /><span>{t('map.snapshotUnavailable')}</span></div>
        ) : (
          <img className="floor-live-image" src={snapshotSrc} alt={name} draggable={false} onError={() => setImgFailed(true)} />
        )}
      </div>
      {!maximized ? <div className="floor-live-resize" onPointerDown={onResizePointerDown} title={t('map.resize')} aria-hidden="true" /> : null}
    </div>
  );
}
MediaWindow.propTypes = { name: PropTypes.string, snapshotSrc: PropTypes.string, clipSrc: PropTypes.string, x: PropTypes.number, y: PropTypes.number, onClose: PropTypes.func };

const CW_SEV_RANK = { critical: 3, warning: 2, info: 1 };
const CAM_MINI_W = 380;
const CAM_MINI_H = 420;
function cwShortAgo(sec) {
  if (!sec) return '';
  const d = Math.max(0, Math.floor(Date.now() / 1000) - sec);
  if (d < 60) return `${d}s`;
  if (d < 3600) return `${Math.floor(d / 60)}m`;
  if (d < 86400) return `${Math.floor(d / 3600)}h`;
  return `${Math.floor(d / 86400)}d`;
}
const cwHasFootage = (e) => e && e.refType === 'alert_event' && Number(e.refId) > 0;

// CameraWindow is the floor-plan camera view: LIVE footage on top (with PTZ when supported) and,
// below it, that camera's own recent events scrolling in a bounded strip. It floats, drags,
// resizes and maximises like the other windows. Footage events open a MediaWindow. This is what a
// camera marker on the plan opens — live first, its notifications underneath.
export function CameraWindow({ nodeId, cameraId, name, iceServers, ptzSupported, x, y, onOpenMedia, onLocate, onClose }) {
  const t = useT();
  const [maximized, setMaximized] = useState(false);
  const { pos, size, onHeadPointerDown, onResizePointerDown } = useFloatingWindow(x, y, maximized, CAM_MINI_W, CAM_MINI_H);
  const [events, setEvents] = useState({ loading: true, list: [] });

  // This camera's events (worst-first, then newest), filtered from the node's feed by cameraId.
  useEffect(() => {
    let live = true;
    const load = () => api(`/api/notifications?nodeId=${encodeURIComponent(nodeId)}&limit=60`, { noRedirect: true })
      .then((r) => {
        if (!live) return;
        const rows = Array.isArray(r.body?.items) ? r.body.items : (Array.isArray(r.body) ? r.body : []);
        const mine = rows.filter((e) => String(e.cameraId) === String(cameraId));
        mine.sort((a, b) => {
          const s = (CW_SEV_RANK[(b.severity || '').toLowerCase()] || 0) - (CW_SEV_RANK[(a.severity || '').toLowerCase()] || 0);
          return s !== 0 ? s : (b.createdAt || 0) - (a.createdAt || 0);
        });
        setEvents({ loading: false, list: mine });
      })
      .catch(() => { if (live) setEvents({ loading: false, list: [] }); });
    load();
    const iv = setInterval(load, 30000);
    return () => { live = false; clearInterval(iv); };
  }, [nodeId, cameraId]);

  const miniStyle = maximized ? undefined : { left: pos.left, top: pos.top, width: size.w, height: size.h };
  const unread = events.list.filter((e) => !e.isRead).length;

  return (
    <div className={`floor-live camwin${maximized ? ' maximized' : ' mini'}`} style={miniStyle}>
      <div className="floor-live-head" onPointerDown={onHeadPointerDown}>
        <span className="floor-live-name"><Ico n="video" sz={13} /> {name}</span>
        <span className="floor-live-actions">
          {maximized ? (
            <button type="button" className="icon-button" onClick={() => setMaximized(false)} title={t('map.restore')} aria-label={t('map.restore')}><Ico n="arr-left" sz={14} /></button>
          ) : (
            <button type="button" className="icon-button" onClick={() => setMaximized(true)} title={t('map.maximize')} aria-label={t('map.maximize')}><Ico n="grid2" sz={13} /></button>
          )}
          <button type="button" className="icon-button" onClick={onClose} title={t('nset.close')} aria-label={t('nset.close')}><Ico n="x" sz={14} /></button>
        </span>
      </div>
      <div className="camwin-live" onDoubleClick={(e) => { if (e.target.closest && e.target.closest('.floor-live-ptz')) return; setMaximized((m) => !m); }}>
        <NodeCameraTile nodeId={nodeId} cam={{ id: cameraId, name }} iceServers={iceServers} />
        {ptzSupported ? <WindowPTZ nodeId={nodeId} cameraId={cameraId} /> : null}
      </div>
      <div className="camwin-events">
        <div className="camwin-events-head">
          <Ico n="bell" sz={12} /> {t('map.events')}{unread > 0 ? <span className="mp-tab-count danger">{unread}</span> : null}
          <span className="camwin-events-spacer" />
          {onLocate ? (
            <button type="button" className="camwin-locate" onClick={() => onLocate({ nodeId, cameraId, name })} title={t('map.locateOnPlan')}>
              <Ico n="map-pin" sz={12} /> {t('map.locate')}
            </button>
          ) : null}
        </div>
        <div className="camwin-events-list">
          {events.loading ? <div className="mp-empty">{t('common.loading')}</div> : null}
          {!events.loading && events.list.length === 0 ? <div className="mp-empty"><Ico n="bell" sz={20} /><span>{t('map.noEvents')}</span></div> : null}
          {events.list.map((e) => {
            const title = e.title || e.body || t('map.event');
            const footage = cwHasFootage(e);
            const main = (
              <>
                <span className={`sev-dot sev-${(e.severity || 'info').toLowerCase()}`} />
                <span className="mp-event-title" title={e.body || e.title}>{title}</span>
                {footage ? <span className="mp-event-cam" aria-hidden="true"><Ico n="camera" sz={12} /></span> : null}
              </>
            );
            return (
              <div key={e.id} className={`mp-event${e.isRead ? ' read' : ''}`}>
                {footage ? (
                  <button type="button" className="mp-event-main has-footage" title={t('map.openFootage')} onClick={(ev) => onOpenMedia && onOpenMedia({ nodeId, alertId: Number(e.refId), name: title }, ev.clientX, ev.clientY)}>{main}</button>
                ) : (
                  <span className="mp-event-main">{main}</span>
                )}
                <span className="mp-event-time">{cwShortAgo(e.createdAt)}</span>
              </div>
            );
          })}
        </div>
      </div>
      {!maximized ? <div className="floor-live-resize" onPointerDown={onResizePointerDown} title={t('map.resize')} aria-hidden="true" /> : null}
    </div>
  );
}
CameraWindow.propTypes = { nodeId: PropTypes.string, cameraId: PropTypes.any, name: PropTypes.string, iceServers: PropTypes.array, ptzSupported: PropTypes.bool, x: PropTypes.number, y: PropTypes.number, onOpenMedia: PropTypes.func, onLocate: PropTypes.func, onClose: PropTypes.func };

// NodeFloorView is the geo-map drill-down: it shows a node's floor plan with the cameras the
// operator placed on it, and plays a camera's LIVE footage in an on-plan window (small, then
// maximised to fill the view) — never leaving the map for a camera page. The live player is
// the shared NodeCameraTile, kept MOUNTED across the small⇄maximised toggle so the WebRTC
// stream is not torn down and restarted.
export function NodeFloorView({ node, floorplans, focusCameraId, onBack, onPlay }) {
  const t = useT();
  // When we arrive from a "locate on plan" jump, open the floor that holds the focus camera.
  const focusIdx = useMemo(() => {
    if (!focusCameraId) return 0;
    const i = floorplans.findIndex((fp) => (fp.placements || []).some((p) => String(p.cameraId) === String(focusCameraId)));
    return i >= 0 ? i : 0;
  }, [floorplans, focusCameraId]);
  const [floorIdx, setFloorIdx] = useState(focusIdx);
  useEffect(() => { setFloorIdx(focusIdx); }, [focusIdx]);
  const nowSec = Math.floor(Date.now() / 1000);

  const current = floorplans[floorIdx] || floorplans[0];
  const floor = current && current.floor;
  const placements = (current && current.placements) || [];
  const w = (floor && floor.width) || 1024;
  const h = (floor && floor.height) || 768;
  const tone = useMemo(() => nodeTone(node, nowSec), [node, nowSec]);

  if (!floor) return null;

  return (
    <div className="floor-view">
      <div className="floor-view-bar">
        <button type="button" className="quiet" onClick={onBack}>
          <span className="btn-icon"><Ico n="arr-left" sz={14} /> {t('map.backToMap')}</span>
        </button>
        <span className="floor-view-title">
          <span className="rail-dot" style={{ background: tone.color }} />
          {node.name || node.nodeId} · {floor.name}
        </span>
        {floorplans.length > 1 ? (
          <div className="floor-view-switch">
            {floorplans.map((fp, i) => (
              <button key={fp.floor.id} type="button" className={`indoor-floor-tab${i === floorIdx ? ' active' : ''}`} onClick={() => setFloorIdx(i)}>
                {fp.floor.name}
              </button>
            ))}
          </div>
        ) : null}
      </div>

      <div className="floor-view-stage">
        <div className="floor-plan-wrap" style={{ aspectRatio: `${w} / ${h}` }}>
          <img className="floor-plan-img" src={`${apiBase()}/api/floors/${floor.id}/image`} alt={floor.name} draggable={false} />
          {/* Camera coverage wedges, drawn in image-pixel space and stretched to the plan. */}
          <svg className="floor-fov" viewBox={`0 0 ${w} ${h}`} preserveAspectRatio="none" aria-hidden="true">
            {placements.filter((p) => p.cameraId && (p.fov || 0) > 0).map((p) => (
              <path key={`fov-${p.id}`} d={fovPath(p, w, h)} fill={tone.color} fillOpacity="0.16" stroke={tone.color} strokeOpacity="0.45" strokeWidth="1" vectorEffect="non-scaling-stroke" />
            ))}
          </svg>
          {placements.map((p) => {
            const isCam = !!p.cameraId;
            const label = p.lastKnownName || (isCam ? t('nodes.cameraN', { id: p.cameraId }) : node.name);
            const focused = isCam && focusCameraId && String(p.cameraId) === String(focusCameraId);
            return (
              <button
                key={p.id}
                type="button"
                className={`floor-marker${isCam ? ' cam' : ' node'}${focused ? ' focus' : ''}`}
                style={{ ...markerPos(p, w, h), borderColor: focused ? undefined : tone.ring }}
                onClick={(e) => { if (isCam && onPlay) onPlay({ nodeId: node.nodeId, cameraId: p.cameraId, name: label, ptzSupported: !!p.ptzSupported }, e.clientX, e.clientY); }}
                title={label}
              >
                <Ico n={isCam ? 'video' : 'cpu'} sz={13} />
                <span className="floor-marker-label">{label}</span>
              </button>
            );
          })}
          {placements.length === 0 ? <div className="floor-view-empty">{t('map.noCamsOnPlan')}</div> : null}
        </div>
      </div>
    </div>
  );
}

NodeFloorView.propTypes = {
  node: PropTypes.object,
  floorplans: PropTypes.array,
  focusCameraId: PropTypes.any,
  onBack: PropTypes.func,
  onPlay: PropTypes.func,
};
