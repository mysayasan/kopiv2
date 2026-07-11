import { useCallback, useEffect, useRef, useState } from 'react';
import { Ico, useT } from '@shared';
import { NodeCameraTile } from './node_manager';
import { NodeEmbed } from './node_embed';
import { LayoutDropdown } from './nodecam/ui';
import { PTZRing } from './nodecam/ptz';
import { layoutCapacity, layoutColumns, layoutRows } from './nodecam/lib/helpers';
import {
  getLiveViewsState, setLiveViewLayout,
  addLiveView, removeLiveView, moveLiveView, isInLiveViews, subscribeLiveViews,
} from '../lib/live_views_store';
import { api } from '../lib/helpers';

// LiveViewsPage is myseliasan's own cross-node camera wall — the fleet analogue of a node's
// Live Views page, built to mirror mymatasan's Live Views (grid layouts, pagination,
// fullscreen, drag-to-reorder, maximize, PTZ). Each tile is a relay-backed live view of a
// camera on some node, so one wall consolidates footage from every managed node. It renders
// inside NodeEmbed so mymatasan's (scoped) stylesheet gives it the exact node look.
export function LiveViewsPage({ nodes = [] }) {
  const t = useT();
  const [state, setState] = useState(getLiveViewsState);
  const [iceServers, setIceServers] = useState([]);

  useEffect(() => subscribeLiveViews(setState), []);
  useEffect(() => {
    api('/api/node-stream/config', { noRedirect: true })
      .then((r) => { if (r.ok && Array.isArray(r.body?.iceServers)) setIceServers(r.body.iceServers); })
      .catch(() => {});
  }, []);

  return (
    <NodeEmbed className="live-views-embed">
      <NodeViewsWall tiles={state.tiles} layout={state.layout} nodes={nodes} iceServers={iceServers} t={t} />
    </NodeEmbed>
  );
}

// NodeViewsWall reproduces mymatasan's ViewsTab (toolbar + paginated grid + tile chrome)
// but each tile is a cross-node relay tile keyed by (nodeId, cameraId) instead of a local
// camera. Layout and tile order are persisted client-side via the live-views store.
function NodeViewsWall({ tiles: viewTiles, layout, nodes, iceServers, t }) {
  const tileCount = layoutCapacity(layout);
  const columns = layoutColumns(layout);
  const rows = layoutRows(layout);

  // The grid size is a per-page size, not a cap: every added camera is kept and shown
  // `tileCount` at a time, so changing the grid re-paginates instead of dropping cameras.
  const pageCount = Math.max(1, Math.ceil(viewTiles.length / tileCount));
  const [page, setPage] = useState(0);
  useEffect(() => {
    if (page > pageCount - 1) setPage(Math.max(0, pageCount - 1));
  }, [page, pageCount]);
  // Left/Right arrows page through cameras — the only pager while fullscreen (toolbar hidden).
  useEffect(() => {
    function onKey(event) {
      const tag = event.target?.tagName;
      if (tag === 'INPUT' || tag === 'TEXTAREA' || tag === 'SELECT') return;
      if (event.key === 'ArrowRight') setPage((p) => Math.min(pageCount - 1, p + 1));
      else if (event.key === 'ArrowLeft') setPage((p) => Math.max(0, p - 1));
    }
    document.addEventListener('keydown', onKey);
    return () => document.removeEventListener('keydown', onKey);
  }, [pageCount]);
  const safePage = Math.min(page, pageCount - 1);
  const pageStart = safePage * tileCount;
  const tiles = [...viewTiles.slice(pageStart, pageStart + tileCount)];
  while (tiles.length < tileCount) tiles.push(null);

  const [draggedKey, setDraggedKey] = useState(null);

  // Fullscreen: enter on the wall via the Fullscreen API. While fullscreen all chrome is
  // hidden (see .views-fullscreen CSS) and the grid is sized rows × cols to fill the screen.
  const workspaceRef = useRef(null);
  const [isFullscreen, setIsFullscreen] = useState(false);
  useEffect(() => {
    const onChange = () => {
      const fsEl = document.fullscreenElement || document.webkitFullscreenElement;
      setIsFullscreen(fsEl === workspaceRef.current);
    };
    document.addEventListener('fullscreenchange', onChange);
    document.addEventListener('webkitfullscreenchange', onChange);
    return () => {
      document.removeEventListener('fullscreenchange', onChange);
      document.removeEventListener('webkitfullscreenchange', onChange);
    };
  }, []);
  function toggleFullscreen() {
    const fsEl = document.fullscreenElement || document.webkitFullscreenElement;
    if (fsEl) {
      (document.exitFullscreen || document.webkitExitFullscreen)?.call(document);
    } else {
      const el = workspaceRef.current;
      if (el) (el.requestFullscreen || el.webkitRequestFullscreen)?.call(el);
    }
  }

  // Maximize a single tile to fill the whole grid (and the screen when fullscreen).
  const [maximizedKey, setMaximizedKey] = useState(null);
  const keyOf = (tile) => `${tile.nodeId}::${tile.cameraId}`;
  const maximizedId = viewTiles.some((tile) => keyOf(tile) === maximizedKey) ? maximizedKey : null;
  function toggleMaximize(key) { setMaximizedKey((cur) => (cur === key ? null : key)); }

  return (
    <section className={`workspace${isFullscreen ? ' views-fullscreen' : ''}`} ref={workspaceRef}>
      <div className="toolbar">
        <div className="segmented">
          <LayoutDropdown layout={layout} onLayout={setLiveViewLayout} />
          {pageCount > 1 ? (
            <div className="view-pager" role="group" aria-label={t('cam.liveViewPages')}>
              <button type="button" className="quiet" onClick={() => setPage(safePage - 1)} disabled={safePage <= 0} aria-label={t('cam.prevPage')}>
                <Ico n="arr-left" sz={14} />
              </button>
              <span className="view-pager-label">{safePage + 1} / {pageCount}</span>
              <button type="button" className="quiet" onClick={() => setPage(safePage + 1)} disabled={safePage >= pageCount - 1} aria-label={t('cam.nextPage')}>
                <Ico n="arr-right" sz={14} />
              </button>
            </div>
          ) : null}
          <button type="button" className="quiet" onClick={toggleFullscreen} title={t('cam.fullscreenTitle')}>
            <span className="btn-icon"><Ico n="maximize" /> {t('cam.fullscreen')}</span>
          </button>
        </div>
        <AddCameraMenu nodes={nodes} viewTiles={viewTiles} t={t} />
      </div>

      {viewTiles.length === 0 ? (
        <div className="page-placeholder">
          <h2>{t('lv.emptyTitle')}</h2>
          <p>{t('lv.emptyHint')}</p>
        </div>
      ) : (
        <div className={`view-grid${maximizedId ? ' has-maximized' : ''}`} style={{ '--view-cols': columns, '--view-rows': rows }}>
          {tiles.map((tile, idx) => {
            const key = tile ? keyOf(tile) : `empty-${idx}`;
            return (
              <article
                className={[
                  'view-tile',
                  tile && draggedKey === key ? 'dragging' : '',
                  tile && maximizedId === key ? 'maximized' : '',
                ].filter(Boolean).join(' ')}
                key={key}
                onDoubleClick={() => tile && toggleMaximize(key)}
                draggable={Boolean(tile)}
                onDragStart={(event) => {
                  if (!tile) return;
                  event.dataTransfer.effectAllowed = 'move';
                  event.dataTransfer.setData('text/plain', String(pageStart + idx));
                  setDraggedKey(key);
                }}
                onDragEnd={() => setDraggedKey(null)}
                onDragOver={(event) => {
                  if (tile) { event.preventDefault(); event.dataTransfer.dropEffect = 'move'; }
                }}
                onDrop={(event) => {
                  if (!tile) return;
                  event.preventDefault();
                  const from = Number(event.dataTransfer.getData('text/plain'));
                  moveLiveView(from, pageStart + idx);
                  setDraggedKey(null);
                }}
              >
                {tile ? (
                  <>
                    <div className="tile-header">
                      <span className="drag-handle" title={t('cam.dragReorder')} aria-label={t('cam.dragReorder')}>::</span>
                      <strong>{tile.name || t('nm.cameraN', { id: tile.cameraId })}</strong>
                      {tile.nodeName ? <span className="tile-node-name">{tile.nodeName}</span> : null}
                      <button
                        type="button"
                        className="icon-button"
                        onClick={() => toggleMaximize(key)}
                        aria-label={maximizedId === key ? t('cam.restoreTile') : t('cam.maximizeTile')}
                        title={maximizedId === key ? t('cam.restore') : t('cam.maximize')}
                      >
                        <Ico n={maximizedId === key ? 'minimize' : 'maximize'} sz={12} />
                      </button>
                      <button type="button" className="icon-button" onClick={() => removeLiveView(tile.nodeId, tile.cameraId)} aria-label={t('cam.removeLiveView')}>
                        <Ico n="x" sz={12} />
                      </button>
                    </div>
                    <NodeCameraTile nodeId={tile.nodeId} cam={{ id: tile.cameraId, name: tile.name }} iceServers={iceServers} />
                    {tile.ptzSupported ? <TilePTZ nodeId={tile.nodeId} cameraId={tile.cameraId} /> : null}
                  </>
                ) : (
                  <div className="empty-tile">{t('cam.empty')}</div>
                )}
              </article>
            );
          })}
        </div>
      )}
    </section>
  );
}

// TilePTZ drives pan/tilt for a wall tile, routing the commands over the node's control
// tunnel (the same proxy path the camera page uses), so PTZ works cross-network.
function TilePTZ({ nodeId, cameraId }) {
  const [busy, setBusy] = useState(false);
  const px = useCallback(
    (path, opts = {}) => api(`/api/nodes/${encodeURIComponent(nodeId)}/proxy${path}`, { noRedirect: true, ...opts }).catch(() => ({ ok: false })),
    [nodeId],
  );
  async function move(dir) {
    setBusy(true);
    await px(`/api/cameras/${cameraId}/ptz/move`, { method: 'POST', body: JSON.stringify({ direction: dir, speed: 0.35, durationMs: 0 }) });
    setBusy(false);
  }
  function stop() { px(`/api/cameras/${cameraId}/ptz/stop`, { method: 'POST' }); }
  return (
    <div className="ptz-ring-overlay">
      <PTZRing busy={busy} size={100} onMove={move} onStop={stop} />
    </div>
  );
}

// AddCameraMenu is the wall's consolidation control: pick a node, then a camera on it, to
// add that camera to the wall. Cameras are fetched lazily per node over the control tunnel
// (the browser never talks to a node directly). Cameras already on the wall are hidden.
function AddCameraMenu({ nodes, viewTiles, t }) {
  const [open, setOpen] = useState(false);
  const [openNode, setOpenNode] = useState(null);
  // Per-node camera fetch state: { status: 'loading'|'ready'|'error', cameras: [] }.
  const [byNode, setByNode] = useState({});
  const wrapRef = useRef(null);

  useEffect(() => {
    if (!open) return undefined;
    function onDown(e) { if (wrapRef.current && !wrapRef.current.contains(e.target)) { setOpen(false); setOpenNode(null); } }
    document.addEventListener('mousedown', onDown);
    return () => document.removeEventListener('mousedown', onDown);
  }, [open]);

  async function loadNode(nodeId) {
    setByNode((m) => ({ ...m, [nodeId]: { status: 'loading', cameras: [] } }));
    const r = await api(`/api/nodes/${encodeURIComponent(nodeId)}/proxy/api/cameras?limit=200`, { noRedirect: true }).catch(() => ({ ok: false }));
    const list = r.ok ? (Array.isArray(r.body) ? r.body : (r.body?.items || [])) : null;
    setByNode((m) => ({ ...m, [nodeId]: { status: list ? 'ready' : 'error', cameras: list || [] } }));
  }

  function toggleNode(node) {
    const nodeId = node.nodeId;
    if (openNode === nodeId) { setOpenNode(null); return; }
    setOpenNode(nodeId);
    if (!byNode[nodeId] || byNode[nodeId].status === 'error') loadNode(nodeId);
  }

  function add(node, cam) {
    addLiveView({
      nodeId: node.nodeId,
      cameraId: cam.id,
      name: cam.name || cam.model || cam.host || String(cam.id),
      nodeName: node.name || node.nodeId,
      ptzSupported: cam.ptzSupported,
    });
  }

  return (
    <div className="add-camera-wrap" ref={wrapRef}>
      <button type="button" className={`quiet add-camera-toggle${open ? ' active' : ''}`} onClick={() => setOpen((o) => !o)} aria-haspopup="true" aria-expanded={open}>
        <span className="btn-icon"><Ico n="plus" /> {t('lv.addCamera')} <Ico n="chev-down" sz={11} /></span>
      </button>
      {open ? (
        <div className="add-camera-menu" role="menu">
          {nodes.length === 0 ? <p className="add-camera-empty">{t('lv.noNodes')}</p> : null}
          {nodes.map((node) => {
            const nodeId = node.nodeId;
            const info = byNode[nodeId];
            const expanded = openNode === nodeId;
            const avail = (info?.cameras || []).filter((c) => !isInLiveViews(nodeId, c.id));
            return (
              <div key={nodeId} className="add-camera-node">
                <button type="button" className={`add-camera-node-btn${expanded ? ' active' : ''}`} onClick={() => toggleNode(node)} aria-expanded={expanded}>
                  <span className="btn-icon"><Ico n="video" sz={13} /> {node.name || nodeId}</span>
                  <Ico n={expanded ? 'chev-down' : 'arr-right'} sz={11} />
                </button>
                {expanded ? (
                  <div className="add-camera-cams">
                    {info?.status === 'loading' ? <p className="add-camera-empty">{t('lv.loadingCameras')}</p> : null}
                    {info?.status === 'error' ? <p className="add-camera-empty">{t('nodes.nodeOffline')}</p> : null}
                    {info?.status === 'ready' && avail.length === 0 ? <p className="add-camera-empty">{t('lv.allAdded')}</p> : null}
                    {info?.status === 'ready' && avail.map((cam) => (
                      <button key={cam.id} type="button" className="add-camera-cam-btn" onClick={() => add(node, cam)}>
                        <span className="btn-icon"><Ico n="plus" sz={12} /> {cam.name || cam.model || cam.host || t('nm.cameraN', { id: cam.id })}</span>
                      </button>
                    ))}
                  </div>
                ) : null}
              </div>
            );
          })}
        </div>
      ) : null}
    </div>
  );
}
