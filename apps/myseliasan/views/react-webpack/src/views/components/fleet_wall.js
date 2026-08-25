import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { Ico, useT } from '@shared';
import { NodeCameraTile } from './node_manager';
import { NodeEmbed } from './node_embed';
import { api } from '../lib/helpers';

// Fleet video walls (W3-3d): a saved camera arrangement that SPANS APPLIANCES.
//
// The Live Views page is already a cross-node wall — but it is a scratchpad: per browser,
// unnamed, gone when somebody clears their site data, and rebuilt by hand at the start of
// every shift. This is the version a control room can actually run on: named, shared, saved on
// the control plane, cycling on its own, and — the part no appliance can do — pulling up the
// camera that is raising the alarm in a building nobody was looking at.

const DEFAULT_GRID = '2x2';

function gridDims(grid) {
  const [c, r] = String(grid || DEFAULT_GRID).split('x').map((n) => Number(n) || 0);
  return { columns: c || 2, rows: r || 2, capacity: (c || 2) * (r || 2) };
}

const tileKey = (tile) => `${tile.nodeId}::${tile.cameraId}`;

export function FleetWallPage({ session, refreshSignal = 0, onToast }) {
  const t = useT();
  const [walls, setWalls] = useState([]);
  const [grids, setGrids] = useState([]);
  const [activeId, setActiveId] = useState(0);
  const [editing, setEditing] = useState(null);
  const [iceServers, setIceServers] = useState([]);
  const [page, setPage] = useState(0);
  // The tiles an alert has pulled up, keyed by tile, each with the time it may go away again.
  const [popped, setPopped] = useState({});
  const canWrite = !!session?.isSuperadmin;

  const load = useCallback(async () => {
    const [list, gridList] = await Promise.all([
      api('/api/fleet-walls', { noRedirect: true }).catch(() => ({ ok: false })),
      api('/api/fleet-walls/grids', { noRedirect: true }).catch(() => ({ ok: false })),
    ]);
    if (gridList.ok && Array.isArray(gridList.body?.grids)) setGrids(gridList.body.grids);
    if (!list.ok) return;
    const items = Array.isArray(list.body?.items) ? list.body.items : [];
    setWalls(items);
    setActiveId((prev) => {
      if (prev && items.some((w) => w.id === prev)) return prev;
      const fallback = items.find((w) => w.isDefault) || items[0];
      return fallback ? fallback.id : 0;
    });
  }, []);
  useEffect(() => { load(); }, [load]);

  useEffect(() => {
    api('/api/node-stream/config', { noRedirect: true })
      .then((r) => { if (r.ok && Array.isArray(r.body?.iceServers)) setIceServers(r.body.iceServers); })
      .catch(() => {});
  }, []);

  const wall = useMemo(() => walls.find((w) => w.id === activeId) || null, [walls, activeId]);
  const tiles = useMemo(() => (wall?.tileList || []), [wall]);
  const dims = gridDims(wall?.grid);
  const pages = Math.max(1, Math.ceil(tiles.length / dims.capacity));

  useEffect(() => { setPage(0); }, [activeId]);

  // The rotation. Stopped whenever an alert has pulled something up: a wall that rotates away
  // from the thing it just pulled up has wasted the one job it had.
  const hasPopped = Object.keys(popped).length > 0;
  useEffect(() => {
    const seconds = Number(wall?.cycleSeconds || 0);
    if (!seconds || pages <= 1 || hasPopped) return undefined;
    const timer = window.setInterval(() => setPage((p) => (p + 1) % pages), seconds * 1000);
    return () => window.clearInterval(timer);
  }, [wall?.cycleSeconds, pages, hasPopped]);

  // THE THING NO APPLIANCE CAN DO. The control plane sees every node's alerts in one feed, so
  // a wall here can pull up the camera raising the alarm in a building the operator was not
  // watching — on a different machine from every other tile on the screen.
  const seenRef = useRef(0);
  useEffect(() => {
    const seconds = Number(wall?.autoPopSeconds || 0);
    if (!seconds || tiles.length === 0) return;
    let cancelled = false;
    (async () => {
      const r = await api('/api/notifications?limit=20', { noRedirect: true }).catch(() => ({ ok: false }));
      if (cancelled || !r.ok) return;
      const rows = Array.isArray(r.body?.items) ? r.body.items : [];
      const onWall = new Set(tiles.map(tileKey));
      const now = Date.now();
      const next = {};
      let newest = seenRef.current;
      for (const row of rows) {
        const at = Number(row.createdAt || 0);
        if (at > newest) newest = at;
        // Only what arrived since the last look. Without this the wall re-pops the same
        // alert on every refresh and never rotates again.
        if (at <= seenRef.current) continue;
        const source = String(row.source || '');
        if (!source.startsWith('node:')) continue;
        const key = `${source.slice(5)}::${Number(row.cameraId || 0)}`;
        if (!onWall.has(key)) continue;
        next[key] = now + seconds * 1000;
      }
      seenRef.current = newest;
      if (Object.keys(next).length) setPopped((prev) => ({ ...prev, ...next }));
    })();
    return () => { cancelled = true; };
  }, [refreshSignal, wall?.autoPopSeconds, tiles]);

  // Let a pop expire. One timer for the whole set rather than one per tile.
  useEffect(() => {
    if (!hasPopped) return undefined;
    const timer = window.setInterval(() => {
      const now = Date.now();
      setPopped((prev) => {
        const next = {};
        let changed = false;
        for (const [key, until] of Object.entries(prev)) {
          if (until > now) next[key] = until; else changed = true;
        }
        return changed ? next : prev;
      });
    }, 1000);
    return () => window.clearInterval(timer);
  }, [hasPopped]);

  // What is actually on screen. A pop REPLACES the page rather than being squeezed in beside
  // it: the operator asked for a grid of four, and four is what a person can take in.
  const visible = useMemo(() => {
    const alarmed = tiles.filter((tile) => popped[tileKey(tile)]);
    if (alarmed.length) return alarmed.slice(0, dims.capacity);
    return tiles.slice(page * dims.capacity, page * dims.capacity + dims.capacity);
  }, [tiles, popped, page, dims.capacity]);

  const remove = useCallback(async (target) => {
    if (!window.confirm(t('fw.deleteConfirm', { name: target.name }))) return;
    const r = await api(`/api/fleet-walls/${target.id}`, { method: 'DELETE', noRedirect: true })
      .catch(() => ({ ok: false }));
    if (!r.ok) { onToast && onToast(r.message || t('fw.deleteFailed'), 'error'); return; }
    await load();
    onToast && onToast(t('fw.deleted'), 'success');
  }, [load, onToast, t]);

  if (editing) {
    return (
      <WallEditor
        initial={editing}
        grids={grids.length ? grids : [DEFAULT_GRID]}
        onCancel={() => setEditing(null)}
        onSaved={async () => { setEditing(null); await load(); onToast && onToast(t('fw.saved'), 'success'); }}
        onToast={onToast}
      />
    );
  }

  return (
    <section className="workspace fleet-wall-page" data-fw="page">
      <section className="settings-panel span-two">
        <header>
          <h2><span className="btn-icon"><Ico n="grid4" /> {t('fw.title')}</span></h2>
          <div className="settings-header-actions">
            {walls.length > 1 ? (
              <select
                className="fw-picker" data-fw="picker" value={activeId}
                onChange={(e) => setActiveId(Number(e.target.value))}
                aria-label={t('fw.choose')}
              >
                {walls.map((w) => <option key={w.id} value={w.id}>{w.name}</option>)}
              </select>
            ) : null}
            {canWrite ? (
              <button type="button" className="quiet" data-fw-act="new"
                onClick={() => setEditing({ id: 0, name: '', grid: grids[0] || DEFAULT_GRID, tiles: [], cycleSeconds: 0, autoPopSeconds: 0, isDefault: walls.length === 0 })}>
                <span className="btn-icon"><Ico n="plus" /> {t('fw.newWall')}</span>
              </button>
            ) : null}
          </div>
        </header>
        <p className="fw-lead">{t('fw.lead')}</p>
        {/* What this is NOT. Said on the screen, because somebody choosing between this and
            the Live Views page is choosing between a saved arrangement and a scratchpad. */}
        <p className="fw-limit">{t('fw.limitRelay')}</p>
      </section>

      {!wall ? (
        <section className="settings-panel span-two">
          <p className="settings-hint" data-fw="empty">{canWrite ? t('fw.empty') : t('fw.emptyReadonly')}</p>
        </section>
      ) : (
        <>
          <section className="settings-panel span-two">
            <header>
              <h2>{wall.name}</h2>
              <div className="settings-header-actions">
                {/* The two counts a person needs before trusting what they are looking at. */}
                {wall.offlineTiles > 0 ? (
                  <span className="fw-warn" data-fw="offline">{t('fw.offlineTiles', { n: wall.offlineTiles })}</span>
                ) : null}
                {wall.unknownTiles > 0 ? (
                  <span className="fw-warn fw-warn--gone" data-fw="unknown">{t('fw.unknownTiles', { n: wall.unknownTiles })}</span>
                ) : null}
                {pages > 1 ? (
                  <span className="settings-hint" data-fw="page-of">{t('fw.pageOf', { n: page + 1, total: pages })}</span>
                ) : null}
                {hasPopped ? (
                  <span className="fw-alarm" data-fw="alarmed"><Ico n="warning" sz={14} /> {t('fw.alarmed')}</span>
                ) : null}
                {canWrite ? (
                  <>
                    <button type="button" className="quiet" data-fw-act="edit"
                      onClick={() => setEditing({
                        id: wall.id, name: wall.name, grid: wall.grid,
                        tiles: (wall.tileList || []).map((x) => ({ nodeId: x.nodeId, cameraId: x.cameraId })),
                        cycleSeconds: wall.cycleSeconds, autoPopSeconds: wall.autoPopSeconds,
                        isDefault: wall.isDefault,
                      })}>
                      <span className="btn-icon"><Ico n="edit-2" sz={13} /> {t('fw.edit')}</span>
                    </button>
                    <button type="button" className="quiet danger-text" data-fw-act="delete" onClick={() => remove(wall)}>
                      <span className="btn-icon"><Ico n="trash" sz={13} /> {t('fw.delete')}</span>
                    </button>
                  </>
                ) : null}
              </div>
            </header>

            <NodeEmbed className="live-views-embed">
              <div
                className="fw-grid"
                data-fw="grid"
                data-fw-grid={wall.grid}
                style={{ gridTemplateColumns: `repeat(${dims.columns}, minmax(0, 1fr))` }}
              >
                {visible.map((tile) => (
                  <WallTile
                    key={tileKey(tile)} tile={tile} iceServers={iceServers}
                    alarmed={!!popped[tileKey(tile)]} t={t}
                  />
                ))}
              </div>
            </NodeEmbed>
            <p className="settings-hint" data-fw="meta">
              {t('fw.meta', {
                tiles: tiles.length,
                cycle: wall.cycleSeconds ? t('fw.cycleEvery', { n: wall.cycleSeconds }) : t('fw.cycleOff'),
                pop: wall.autoPopSeconds ? t('fw.popFor', { n: wall.autoPopSeconds }) : t('fw.popOff'),
              })}
            </p>
          </section>
        </>
      )}
    </section>
  );
}

// WallTile is one camera on one appliance.
//
// A TILE THAT CANNOT SHOW A PICTURE SAYS SO IN WORDS. Rendering an offline appliance as a
// black rectangle makes it indistinguishable from a dark room — which is the failure that
// matters, because a wall is watched by somebody who is not going to investigate every dark
// square.
function WallTile({ tile, iceServers, alarmed, t }) {
  const known = tile.nodeKnown !== false;
  const online = known && (!tile.nodeStatus || tile.nodeStatus === 'online');
  return (
    <div
      className={`fw-tile${alarmed ? ' fw-tile--alarmed' : ''}${online ? '' : ' fw-tile--dark'}`}
      data-fw="tile" data-fw-tile={tileKey(tile)} data-fw-alarmed={alarmed ? '1' : '0'}
      data-fw-state={known ? (online ? 'online' : 'offline') : 'unknown'}
    >
      <div className="fw-tile-head">
        <span className="fw-tile-node">{tile.nodeName || tile.nodeId}</span>
        <span className="fw-tile-cam">{t('fw.cameraN', { id: tile.cameraId })}</span>
      </div>
      {online ? (
        <NodeCameraTile nodeId={tile.nodeId} cam={{ id: tile.cameraId, name: '' }} iceServers={iceServers} />
      ) : (
        <p className="fw-tile-dark" data-fw="tile-dark">
          <Ico n="warning" sz={15} />
          <span>{known ? t('fw.tileOffline') : t('fw.tileUnknown')}</span>
        </p>
      )}
    </div>
  );
}

// WallEditor builds the arrangement. Cameras are read PER APPLIANCE over the tunnel, because
// the appliance is the only thing that knows what cameras it has.
function WallEditor({ initial, grids, onCancel, onSaved, onToast }) {
  const t = useT();
  const [form, setForm] = useState(initial);
  const [nodes, setNodes] = useState([]);
  const [openNode, setOpenNode] = useState('');
  const [byNode, setByNode] = useState({});
  const [error, setError] = useState('');
  const [busy, setBusy] = useState(false);

  useEffect(() => {
    api('/api/nodes', { noRedirect: true })
      .then((r) => { if (r.ok) setNodes(Array.isArray(r.body) ? r.body : (r.body?.items || [])); })
      .catch(() => {});
  }, []);

  const loadCameras = useCallback(async (nodeId) => {
    setByNode((m) => ({ ...m, [nodeId]: { status: 'loading', cameras: [] } }));
    const r = await api(`/api/nodes/${encodeURIComponent(nodeId)}/proxy/api/cameras?limit=200`, { noRedirect: true })
      .catch(() => ({ ok: false }));
    // The camera list answers {data:{result:[…]}}, not {result:{items:[…]}} — reading the
    // wrong key returns an empty list for an appliance full of cameras.
    const list = Array.isArray(r.body) ? r.body : (r.body?.items || r.body?.result || null);
    setByNode((m) => ({ ...m, [nodeId]: { status: list ? 'ready' : 'error', cameras: list || [] } }));
  }, []);

  const has = (nodeId, cameraId) => (form.tiles || []).some((x) => x.nodeId === nodeId && Number(x.cameraId) === Number(cameraId));
  const toggle = (nodeId, cameraId) => {
    setForm((f) => {
      const tiles = f.tiles || [];
      return has(nodeId, cameraId)
        ? { ...f, tiles: tiles.filter((x) => !(x.nodeId === nodeId && Number(x.cameraId) === Number(cameraId))) }
        : { ...f, tiles: [...tiles, { nodeId, cameraId: Number(cameraId) }] };
    });
  };

  const save = async () => {
    setBusy(true);
    setError('');
    const r = await api('/api/fleet-walls', {
      method: 'POST', body: JSON.stringify(form), noRedirect: true,
    }).catch(() => ({ ok: false }));
    setBusy(false);
    if (!r.ok) {
      // The server's refusals are written to be read. Show them as they are.
      setError(r.message || t('fw.saveFailed'));
      return;
    }
    onSaved && onSaved();
  };

  return (
    <form className="workspace" onSubmit={(e) => { e.preventDefault(); save(); }} data-fw="editor">
      <section className="settings-panel span-two">
        <header>
          <h2><span className="btn-icon"><Ico n="grid4" /> {form.id ? t('fw.editTitle') : t('fw.newTitle')}</span></h2>
        </header>
        {error ? <p className="fw-error" role="alert" data-fw="error"><Ico n="warning" sz={14} /> {error}</p> : null}
        <div className="settings-field-grid">
          <label>
            {t('fw.name')}
            <input data-fw-input="name" value={form.name} required disabled={busy}
              onChange={(e) => setForm((f) => ({ ...f, name: e.target.value }))} />
          </label>
          <label>
            {t('fw.grid')}
            <select data-fw-input="grid" value={form.grid} disabled={busy}
              onChange={(e) => setForm((f) => ({ ...f, grid: e.target.value }))}>
              {grids.map((g) => <option key={g} value={g}>{g}</option>)}
            </select>
          </label>
          <label>
            {t('fw.cycle')}
            <input type="number" min={0} data-fw-input="cycle" value={form.cycleSeconds} disabled={busy}
              onChange={(e) => setForm((f) => ({ ...f, cycleSeconds: Number(e.target.value) || 0 }))} />
          </label>
          <label>
            {t('fw.pop')}
            <input type="number" min={0} data-fw-input="pop" value={form.autoPopSeconds} disabled={busy}
              onChange={(e) => setForm((f) => ({ ...f, autoPopSeconds: Number(e.target.value) || 0 }))} />
          </label>
        </div>
        <p className="settings-hint">{t('fw.popHint')}</p>
        <label className="fw-default">
          <input type="checkbox" data-fw-input="default" checked={!!form.isDefault} disabled={busy}
            onChange={(e) => setForm((f) => ({ ...f, isDefault: e.target.checked }))} />
          <span>{t('fw.isDefault')}</span>
        </label>
      </section>

      <section className="settings-panel span-two">
        <header>
          <h2><span className="btn-icon"><Ico n="camera" /> {t('fw.cameras', { n: (form.tiles || []).length })}</span></h2>
        </header>
        <div className="fw-nodes">
          {nodes.map((node) => (
            <div key={node.nodeId} className="fw-node">
              <button type="button" className="quiet fw-node-head" data-fw-node={node.nodeId}
                onClick={() => {
                  const next = openNode === node.nodeId ? '' : node.nodeId;
                  setOpenNode(next);
                  if (next && !byNode[next]) loadCameras(next);
                }}>
                <Ico n={openNode === node.nodeId ? 'chev-down' : 'chev-right'} sz={14} />
                <span>{node.name || node.nodeId}</span>
                <span className={`status-pill ${node.status === 'online' ? 'online' : 'offline'}`}>{node.status}</span>
              </button>
              {openNode === node.nodeId ? (
                <div className="fw-cams">
                  {(byNode[node.nodeId]?.cameras || []).map((cam) => (
                    <label key={cam.id} className="fw-cam">
                      <input type="checkbox" data-fw-cam={`${node.nodeId}::${cam.id}`}
                        checked={has(node.nodeId, cam.id)} disabled={busy}
                        onChange={() => toggle(node.nodeId, cam.id)} />
                      <span>{cam.name || t('fw.cameraN', { id: cam.id })}</span>
                    </label>
                  ))}
                  {byNode[node.nodeId]?.status === 'ready' && (byNode[node.nodeId]?.cameras || []).length === 0
                    ? <p className="settings-hint">{t('fw.noCameras')}</p> : null}
                  {byNode[node.nodeId]?.status === 'error'
                    ? <p className="settings-hint settings-hint--error">{t('fw.camerasFailed')}</p> : null}
                </div>
              ) : null}
            </div>
          ))}
        </div>
        <div className="modal-actions">
          <button type="button" className="quiet" data-fw-act="cancel" onClick={onCancel} disabled={busy}>{t('fw.cancel')}</button>
          <button type="submit" data-fw-act="save" disabled={busy}>
            <span className="btn-icon"><Ico n="save" sz={15} /> {busy ? t('fw.saving') : t('fw.save')}</span>
          </button>
        </div>
      </section>
    </form>
  );
}
