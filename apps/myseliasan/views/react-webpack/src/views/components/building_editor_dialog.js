import { useCallback, useEffect, useRef, useState } from 'react';
import PropTypes from 'prop-types';
import { useT, Ico } from '@shared';
import { api, apiBase, csrfToken } from '../lib/helpers';
import { nodeTone } from '../lib/fleet_status';
import { FloorEditor } from './floor_editor';
import { SiteDialog } from './asset_wizard';
import { multiPlan, normKind, siteGlyph } from './site_kinds';

// BuildingEditorDialog is the authoring surface for ONE building, opened as a modal over the
// geographic map: pick an area (floor) along the top, drag a node or camera off the palette, and
// drop it where it sits on the plan. It owns area management + placement CRUD; FloorEditor owns
// the canvas itself (walls, camera pins, aim, 2D⇄3D).
//
// Placements are myseliasan-owned, so a camera stays on the plan while its node is offline — the
// control plane has no camera inventory of its own, and the live palette list goes empty when the
// node is unreachable. That is why a placement carries a lastKnownName snapshot.

// placedLabel names where a pin sits — "Head Office / Ground floor", falling back to whichever
// part the server could resolve. Shared by the palette tooltip and the refused-placement toast.
function placedLabel(where) {
  if (!where) return '';
  if (where.siteName && where.floorName) return `${where.siteName} / ${where.floorName}`;
  return where.siteName || where.floorName || '';
}

function AreaTab({ floor, active, onOpen, onRename }) {
  const t = useT();
  return (
    <span className={`bld-areatab${active ? ' active' : ''}`}>
      <button type="button" className="bld-areatab-nm" onClick={() => onOpen(floor)} onDoubleClick={() => onRename(floor)} title={t('bld.renameHint')}>
        {floor.name}
      </button>
    </span>
  );
}
AreaTab.propTypes = { floor: PropTypes.object, active: PropTypes.bool, onOpen: PropTypes.func, onRename: PropTypes.func };

export function BuildingEditorDialog({ site, nodes = [], onToast, onClose, onChanged }) {
  const t = useT();
  const changedRef = useRef(onChanged); changedRef.current = onChanged;
  const notifyChanged = () => { if (changedRef.current) changedRef.current(); };

  const [building, setBuilding] = useState(site);
  const [floors, setFloors] = useState([]);
  const [activeFloor, setActiveFloor] = useState(null);
  const [placements, setPlacements] = useState([]);
  const [camsByNode, setCamsByNode] = useState({});
  const [expanded, setExpanded] = useState({});
  const [busy, setBusy] = useState(false);
  const [loaded, setLoaded] = useState(false);
  const [placing, setPlacing] = useState(null); // { nodeId, cameraId, name }
  const [placedIndex, setPlacedIndex] = useState({}); // "nodeId::cameraId" -> { siteName, floorName, floorId, … }
  const [editSite, setEditSite] = useState(false);
  const fileInputRef = useRef(null);
  const dragGhostRef = useRef(null);
  const dragGhostLabelRef = useRef(null);
  const aimTimerRef = useRef(null);
  const nowSec = Math.floor(Date.now() / 1000);
  // Multi-plan affordances are a building's alone (see site_kinds).
  const canAddAreas = multiPlan(building.kind);

  const nodesById = {};
  for (const n of nodes) nodesById[n.nodeId] = n;

  const loadFloors = useCallback(async (selectId) => {
    try {
      const res = await api(`/api/sites/${building.id}/floors`);
      const list = res.ok ? res.body || [] : [];
      list.sort((a, b) => (a.ordinal - b.ordinal) || (a.id - b.id));
      setFloors(list);
      setActiveFloor((cur) => {
        const want = selectId || (cur && cur.id);
        return list.find((f) => f.id === want) || list[0] || null;
      });
      setLoaded(true);
      notifyChanged();
      return list;
    } catch (_) { setFloors([]); setActiveFloor(null); setLoaded(true); return []; }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [building.id]);

  const loadPlacements = useCallback(async (floorId) => {
    if (!floorId) { setPlacements([]); return; }
    try { const res = await api(`/api/floors/${floorId}/placements`); setPlacements(res.ok ? res.body || [] : []); notifyChanged(); }
    catch (_) { setPlacements([]); }
  }, []);

  // Placement is exclusive fleet-wide, so the palette has to know about pins in OTHER buildings —
  // the per-floor list above can only ever see the area being edited. Keyed "nodeId::cameraId".
  const loadPlacedIndex = useCallback(async () => {
    try {
      const res = await api('/api/placements', { noRedirect: true });
      const rows = res.ok && Array.isArray(res.body) ? res.body : [];
      const m = {};
      rows.forEach((p) => { m[`${p.nodeId}::${p.cameraId || ''}`] = p; });
      setPlacedIndex(m);
    } catch (_) { /* leave the last good index; the server still refuses a double placement */ }
  }, []);

  useEffect(() => { loadFloors(); }, [loadFloors]);
  useEffect(() => { loadPlacedIndex(); }, [loadPlacedIndex]);
  useEffect(() => { loadPlacements(activeFloor && activeFloor.id); }, [activeFloor && activeFloor.id, loadPlacements]);
  useEffect(() => () => clearTimeout(aimTimerRef.current), []);

  // Esc closes the dialog — but only when nothing inside is mid-flow (placing a marker, or the
  // rename dialog open), so one keypress never discards more than the operator expects.
  useEffect(() => {
    const onKey = (e) => {
      if (e.key !== 'Escape') return;
      if (editSite) return;
      if (placing) { setPlacing(null); return; }
      onClose();
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, [placing, editSite, onClose]);

  // --- camera palette (live, over the node tunnel) ---
  const loadCams = useCallback(async (nodeId) => {
    setCamsByNode((m) => ({ ...m, [nodeId]: { loading: true, cams: m[nodeId]?.cams || [] } }));
    const r = await api(`/api/nodes/${encodeURIComponent(nodeId)}/proxy/api/cameras?limit=200`, { noRedirect: true }).catch(() => ({ ok: false }));
    const cams = r.ok ? (Array.isArray(r.body) ? r.body : (r.body?.items || [])) : [];
    setCamsByNode((m) => ({ ...m, [nodeId]: { loading: false, cams, error: !r.ok } }));
  }, []);
  function toggleNode(nodeId) { setExpanded((e) => { const next = !e[nodeId]; if (next && !camsByNode[nodeId]) loadCams(nodeId); return { ...e, [nodeId]: next }; }); }

  // --- placement CRUD (FloorEditor calls these) ---
  const placeAt = useCallback(async (payload, x, y) => {
    if (!payload || !activeFloor) return;
    setBusy(true);
    try {
      const res = await api(`/api/floors/${activeFloor.id}/placements`, { method: 'POST', body: JSON.stringify({ nodeId: payload.nodeId, cameraId: payload.cameraId || '', lastKnownName: payload.name, x, y }) });
      if (!res.ok) {
        // 409: the camera already holds a pin. The palette normally prevents this, but a stale
        // index or a second operator can still get here — so say WHERE it is rather than "failed",
        // and refresh the index so the palette catches up.
        const d = res.body && res.body.details;
        const taken = Array.isArray(d) ? d.find((x2) => x2 && x2.reason === 'alreadyPlaced') : (d && d.reason === 'alreadyPlaced' ? d : null);
        if (taken) {
          if (onToast) onToast(t('bld.unplaceFirst', { where: placedLabel(taken) }), 'error');
          loadPlacedIndex();
          return;
        }
        throw new Error();
      }
      await loadPlacements(activeFloor.id);
      loadPlacedIndex();
    } catch (_) { if (onToast) onToast(t('map.placeFailed'), 'error'); } finally { setBusy(false); }
  }, [activeFloor, loadPlacements, loadPlacedIndex, onToast, t]);

  const persistMove = useCallback(async (id, x, y) => {
    setPlacements((list) => list.map((p) => (p.id === id ? { ...p, x, y } : p)));
    try { const res = await api(`/api/placements/${id}`, { method: 'PUT', body: JSON.stringify({ x, y }) }); if (!res.ok) throw new Error(); }
    catch (_) { if (onToast) onToast(t('map.saveFailed'), 'error'); loadPlacements(activeFloor && activeFloor.id); }
  }, [activeFloor, loadPlacements, onToast, t]);

  // setAim applies a heading/fov/mount/pitch patch locally at once (so the wedge/cone move live) and
  // persists on a short debounce so a slider drag isn't a burst of writes.
  const setAim = useCallback((id, patch) => {
    setPlacements((list) => list.map((p) => (p.id === id ? { ...p, ...patch } : p)));
    clearTimeout(aimTimerRef.current);
    aimTimerRef.current = setTimeout(() => { api(`/api/placements/${id}`, { method: 'PUT', body: JSON.stringify(patch) }).catch(() => { if (onToast) onToast(t('map.saveFailed'), 'error'); }); }, 250);
  }, [onToast, t]);

  const deletePlacement = useCallback(async (id) => {
    setBusy(true);
    // Unplacing is what frees a camera to be placed elsewhere, so the palette index has to be
    // refreshed here or the camera would stay greyed out until the dialog is reopened.
    try { await api(`/api/placements/${id}`, { method: 'DELETE' }); await loadPlacements(activeFloor && activeFloor.id); loadPlacedIndex(); }
    catch (_) { if (onToast) onToast(t('map.error'), 'error'); } finally { setBusy(false); }
  }, [activeFloor, loadPlacements, loadPlacedIndex, onToast, t]);

  // saveModel persists the drawn walls + scale + wall height (autosaved by FloorEditor). It patches
  // local floor state (same id) so the editor never re-seeds or refetches the image.
  const saveModel = useCallback(async (model) => {
    const fid = activeFloor && activeFloor.id;
    if (!fid) return;
    try {
      const res = await api(`/api/floors/${fid}/model`, { method: 'PUT', body: JSON.stringify(model) });
      if (!res.ok) throw new Error();
      setFloors((list) => list.map((f) => (f.id === fid ? { ...f, ...model } : f)));
      setActiveFloor((f) => (f && f.id === fid ? { ...f, ...model } : f));
      notifyChanged();
    } catch (_) { if (onToast) onToast(t('map.error'), 'error'); }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [activeFloor && activeFloor.id, onToast, t]);

  // --- area management ---
  async function addArea() {
    const name = window.prompt(t('bld.areaNamePrompt'), t('bld.areaNew'));
    if (name === null) return;
    const trimmed = (name || '').trim() || t('bld.areaNew');
    setBusy(true);
    try {
      const res = await api(`/api/sites/${building.id}/areas`, { method: 'POST', body: JSON.stringify({ name: trimmed, ordinal: floors.length }) });
      if (!res.ok) throw new Error();
      await loadFloors(res.body && res.body.id);
      if (onToast) onToast(t('bld.areaAdded'), 'success');
    } catch (_) { if (onToast) onToast(t('bld.areaAddFailed'), 'error'); } finally { setBusy(false); }
  }

  async function renameArea(floor) {
    const name = window.prompt(t('bld.areaNamePrompt'), floor.name);
    if (name === null) return;
    const trimmed = (name || '').trim();
    if (!trimmed || trimmed === floor.name) return;
    setBusy(true);
    try {
      const res = await api(`/api/floors/${floor.id}`, { method: 'PUT', body: JSON.stringify({ name: trimmed, ordinal: floor.ordinal || 0 }) });
      if (!res.ok) throw new Error();
      await loadFloors(floor.id);
    } catch (_) { if (onToast) onToast(t('map.error'), 'error'); } finally { setBusy(false); }
  }

  async function deleteArea(floor) {
    if (!window.confirm(t('map.floorDeleteConfirm', { name: floor.name }))) return;
    setBusy(true);
    try { await api(`/api/floors/${floor.id}`, { method: 'DELETE' }); await loadFloors(); if (onToast) onToast(t('map.floorDeleted'), 'success'); }
    catch (_) { if (onToast) onToast(t('map.error'), 'error'); } finally { setBusy(false); }
  }

  // Replace the active area's blank canvas with a real uploaded plan (a scan or CAD export). The
  // drawn walls and every placement survive — only the underlying image changes.
  async function uploadPlan(file) {
    if (!file || !activeFloor) return;
    if (!/^image\//.test(file.type || '')) { if (onToast) onToast(t('map.floorNotImage'), 'error'); return; }
    setBusy(true);
    try {
      const fd = new FormData();
      fd.append('file', file);
      fd.append('name', activeFloor.name);
      const res = await fetch(`${apiBase()}/api/floors/${activeFloor.id}/image`, { method: 'POST', credentials: 'include', headers: { 'X-CSRF-Token': csrfToken() }, body: fd });
      if (!res.ok) throw new Error();
      await loadFloors(activeFloor.id);
      if (onToast) onToast(t('map.floorUploaded'), 'success');
    } catch (_) { if (onToast) onToast(t('map.floorUploadFailed'), 'error'); } finally { setBusy(false); }
  }

  // Remove the active area's plan picture, restoring the blank canvas it started as — the inverse
  // of uploadPlan. The drawn walls and every placement survive; deleting the area is the
  // destructive option. Only reachable when the floor actually carries an uploaded plan
  // (floor.hasPlanImage) — a blank area has nothing to remove.
  async function removePlan() {
    if (!activeFloor) return;
    if (!window.confirm(t('bld.removePlanConfirm', { name: activeFloor.name }))) return;
    setBusy(true);
    try {
      const res = await api(`/api/floors/${activeFloor.id}/image`, { method: 'DELETE' });
      if (!res.ok) throw new Error();
      await loadFloors(activeFloor.id);
      if (onToast) onToast(t('bld.planRemoved'), 'success');
    } catch (_) { if (onToast) onToast(t('bld.removePlanFailed'), 'error'); } finally { setBusy(false); }
  }

  async function saveSite(name, icon) {
    setBusy(true);
    try {
      // kind is echoed back unchanged — an asset's kind is fixed once its plans hang off it, and
      // omitting it here would silently normalise a park back into a building.
      const res = await api(`/api/sites/${building.id}`, { method: 'PUT', body: JSON.stringify({ name, description: building.description || '', icon, kind: normKind(building.kind), ordinal: building.ordinal || 0 }) });
      if (!res.ok) throw new Error();
      setBuilding((b) => ({ ...b, name, icon }));
      setEditSite(false);
      notifyChanged();
      if (onToast) onToast(t('map.siteUpdated'), 'success');
    } catch (_) { if (onToast) onToast(t('map.error'), 'error'); } finally { setBusy(false); }
  }

  // Delete the whole asset — its floor plans, their images, and every camera placement on them go
  // with it. Guarded by a confirm, then the editor closes back to the map.
  async function deleteSite() {
    if (!window.confirm(t('map.deleteAssetConfirm', { name: building.name }))) return;
    setBusy(true);
    try {
      const res = await api(`/api/sites/${building.id}`, { method: 'DELETE' });
      if (!res.ok) throw new Error();
      if (onToast) onToast(t('map.assetDeleted', { name: building.name }), 'success');
      if (onClose) onClose(); // parent reloads the overview on close, so the marker vanishes
    } catch (_) { if (onToast) onToast(t('map.error'), 'error'); setBusy(false); }
  }

  // --- palette drag/pick ---
  function dragPayload(e, payload) {
    e.dataTransfer.setData('text/placement', JSON.stringify(payload));
    e.dataTransfer.effectAllowed = 'copy';
    if (dragGhostLabelRef.current) dragGhostLabelRef.current.textContent = payload.name || '';
    if (dragGhostRef.current) { dragGhostRef.current.classList.toggle('is-cam', !!payload.cameraId); try { e.dataTransfer.setDragImage(dragGhostRef.current, 16, 16); } catch (_) { /* older browsers */ } }
  }
  function pick(payload) { setPlacing((cur) => (cur && cur.nodeId === payload.nodeId && cur.cameraId === payload.cameraId ? null : payload)); }
  const isPicked = (nodeId, cameraId) => placing && placing.nodeId === nodeId && (placing.cameraId || '') === (cameraId || '');

  // Where this camera/node is already pinned, or undefined when it is free to place. A camera holds
  // one pin fleet-wide, so this is what disables it in the palette.
  const placedAt = (nodeId, cameraId) => placedIndex[`${nodeId}::${cameraId || ''}`];
  // The tooltip on an already-placed entry: "this area" when the pin is on the floor being edited
  // (it is visible right there), otherwise the building and area to go and unplace it from.
  const placedTitle = (where) => (
    where.floorId === (activeFloor && activeFloor.id)
      ? t('bld.placedHere')
      : t('bld.placedIn', { where: placedLabel(where) })
  );

  return (
    <div className="bld-overlay" role="dialog" aria-modal="true" aria-label={t('bld.editorLabel', { name: building.name })}>
      <div className="bld-dialog">
        <header className="bld-head">
          <span className="bld-head-glyph" aria-hidden="true">{siteGlyph(building)}</span>
          <h2 className="bld-head-name">{building.name}</h2>
          <button type="button" className="quiet bld-head-edit" onClick={() => setEditSite(true)} disabled={busy}>
            <span className="btn-icon"><Ico n="edit-2" sz={13} /> {t('map.editAsset')}</span>
          </button>
          <button type="button" className="quiet danger-text bld-head-edit" onClick={deleteSite} disabled={busy}>
            <span className="btn-icon"><Ico n="trash" sz={13} /> {t('map.deleteAsset')}</span>
          </button>
          <span className="bld-head-spacer" />
          {activeFloor ? (
            <>
              <button type="button" className="quiet" onClick={() => fileInputRef.current && fileInputRef.current.click()} disabled={busy} title={t('bld.uploadPlanHint')}>
                <span className="btn-icon"><Ico n="download" sz={13} /> {t('bld.uploadPlan')}</span>
              </button>
              <input ref={fileInputRef} type="file" accept="image/png,image/jpeg,image/gif" style={{ display: 'none' }} onChange={(e) => { const f = e.target.files && e.target.files[0]; e.target.value = ''; uploadPlan(f); }} />
              {/* Only offered when there is actually a plan picture to remove. An area that is
                  still the blank canvas has nothing to restore it to, so the button would be a
                  no-op dressed up as a destructive action. */}
              {activeFloor.hasPlanImage ? (
                <button type="button" className="quiet" onClick={removePlan} disabled={busy} title={t('bld.removePlanHint')}>
                  <span className="btn-icon"><Ico n="trash" sz={13} /> {t('bld.removePlan')}</span>
                </button>
              ) : null}
            </>
          ) : null}
          <button type="button" className="icon-button bld-head-close" onClick={onClose} aria-label={t('bld.done')} title={t('bld.done')}><Ico n="x" sz={15} /></button>
        </header>

        <div className="bld-areabar" role="tablist" aria-label={t('bld.areas')}>
          {floors.map((f) => (
            <AreaTab key={f.id} floor={f} active={activeFloor && activeFloor.id === f.id} onOpen={setActiveFloor} onRename={renameArea} />
          ))}
          {/* Only a building has more than one plan. An outdoor area is a single ground surface,
              so it is offered neither "add area" nor a delete that would leave it with none. */}
          {canAddAreas ? <button type="button" className="bld-areaadd" onClick={addArea} disabled={busy}><Ico n="plus" sz={12} /> {t('bld.addArea')}</button> : null}
          <span className="bld-areabar-spacer" />
          {activeFloor ? (
            <>
              <button type="button" className="quiet bld-areaact" onClick={() => renameArea(activeFloor)} disabled={busy}><Ico n="edit-2" sz={12} /> {t('bld.renameArea')}</button>
              {canAddAreas ? <button type="button" className="quiet danger-text bld-areaact" onClick={() => deleteArea(activeFloor)} disabled={busy}><Ico n="trash" sz={12} /> {t('bld.deleteArea')}</button> : null}
            </>
          ) : null}
        </div>

        <div className="bld-body">
          <aside className="bld-palette">
            <div className="palette-head">{t('map.placeThings')}</div>
            <div className="palette-hint">{t('bld.paletteHint')}</div>
            <ul className="palette-list">
              {nodes.length === 0 ? <li className="palette-cam muted">{t('bld.noNodes')}</li> : null}
              {nodes.map((n) => {
                const isCamera = (n.kind || 'camera') !== 'iot';
                const cams = camsByNode[n.nodeId];
                const nodeWhere = placedAt(n.nodeId, '');
                return (
                  <li key={n.nodeId} className="palette-node">
                    <div className="palette-node-row">
                      {isCamera ? (
                        <button type="button" className="palette-expand" onClick={() => toggleNode(n.nodeId)} aria-label={t('map.showCameras')}><Ico n={expanded[n.nodeId] ? 'chev-down' : 'chev-right'} sz={12} /></button>
                      ) : <span className="palette-expand-spacer" />}
                      {/* Already pinned somewhere → not draggable, not pickable. It has one physical
                          home, so the way to move it is to unplace it there first. */}
                      <button
                        type="button"
                        className={`palette-item${isPicked(n.nodeId, '') ? ' active' : ''}${nodeWhere ? ' placed' : ''}`}
                        draggable={!nodeWhere}
                        disabled={!!nodeWhere}
                        onDragStart={nodeWhere ? undefined : (e) => { dragPayload(e, { nodeId: n.nodeId, cameraId: '', name: n.name || n.nodeId }); setPlacing(null); }}
                        onClick={() => pick({ nodeId: n.nodeId, cameraId: '', name: n.name || n.nodeId })}
                        title={nodeWhere ? placedTitle(nodeWhere) : t('bld.paletteHint')}
                      >
                        <span className="rail-dot" style={{ background: nodeTone(n, nowSec).color }} />
                        <span className="rail-name">{n.name || n.nodeId}</span>
                        {nodeWhere ? <Ico n="map-pin" sz={12} /> : (isPicked(n.nodeId, '') ? <Ico n="map-pin" sz={13} /> : null)}
                      </button>
                    </div>
                    {isCamera && expanded[n.nodeId] ? (
                      <ul className="palette-cams">
                        {cams?.loading ? <li className="palette-cam muted">{t('common.loading')}</li> : null}
                        {cams?.error ? <li className="palette-cam muted">{t('map.camsOffline')}</li> : null}
                        {cams?.cams?.map((c) => {
                          const camWhere = placedAt(n.nodeId, String(c.id));
                          const camName = c.name || t('nodes.cameraN', { id: c.id });
                          return (
                            <li key={c.id}>
                              <button
                                type="button"
                                className={`palette-cam${isPicked(n.nodeId, String(c.id)) ? ' active' : ''}${camWhere ? ' placed' : ''}`}
                                draggable={!camWhere}
                                disabled={!!camWhere}
                                onDragStart={camWhere ? undefined : (e) => { dragPayload(e, { nodeId: n.nodeId, cameraId: String(c.id), name: camName }); setPlacing(null); }}
                                onClick={() => pick({ nodeId: n.nodeId, cameraId: String(c.id), name: camName })}
                                title={camWhere ? placedTitle(camWhere) : t('bld.paletteHint')}
                              >
                                <Ico n="video" sz={12} /> <span className="rail-name">{camName}</span>
                                {camWhere ? <Ico n="map-pin" sz={12} /> : (isPicked(n.nodeId, String(c.id)) ? <Ico n="map-pin" sz={12} /> : null)}
                              </button>
                            </li>
                          );
                        })}
                        {cams && !cams.loading && !cams.error && cams.cams.length === 0 ? <li className="palette-cam muted">{t('map.noCams')}</li> : null}
                      </ul>
                    ) : null}
                  </li>
                );
              })}
            </ul>
          </aside>

          <div className="bld-stage">
            {placing ? (
              <div className="fleet-map-placing" role="status">
                <span><Ico n="map-pin" sz={14} /> {t('map.placingBanner', { name: placing.name })}</span>
                <button type="button" className="linklike" onClick={() => setPlacing(null)}>{t('map.cancel')}</button>
              </div>
            ) : null}
            {!loaded ? (
              <div className="bld-empty">{t('common.loading')}</div>
            ) : !activeFloor ? (
              // A building can reach zero areas (all deleted, or one created before areas existed).
              // Offer the way back rather than a dead canvas.
              <div className="bld-empty">
                <Ico n="grid2" sz={30} />
                <div className="bld-empty-t">{t('bld.noAreas')}</div>
                <div className="bld-empty-s">{t('bld.noAreasHint')}</div>
                <button type="button" onClick={addArea} disabled={busy}><span className="btn-icon"><Ico n="plus" sz={14} /> {t('bld.addArea')}</span></button>
              </div>
            ) : (
              <FloorEditor
                floor={activeFloor}
                siteKind={normKind(building.kind)}
                placements={placements}
                nodesById={nodesById}
                placing={placing}
                onPlace={placeAt}
                onClearPlacing={() => setPlacing(null)}
                onMove={persistMove}
                onAim={setAim}
                onRemove={deletePlacement}
                onSaveModel={saveModel}
                onToast={onToast}
                busy={busy}
              />
            )}
          </div>
        </div>

        <footer className="bld-foot">
          <span className="bld-foot-hint">{t('bld.autosaveHint')}</span>
          <button type="button" onClick={onClose}>{t('bld.done')}</button>
        </footer>
      </div>

      <div ref={dragGhostRef} className="indoor-drag-ghost" aria-hidden="true">
        <span className="indoor-drag-ghost-mark" />
        <span className="indoor-drag-ghost-label" ref={dragGhostLabelRef} />
      </div>

      {editSite ? (
        <SiteDialog initialName={building.name} initialIcon={building.icon} kind={building.kind} busy={busy} onSave={saveSite} onCancel={() => setEditSite(false)} />
      ) : null}
    </div>
  );
}

BuildingEditorDialog.propTypes = {
  site: PropTypes.object,
  nodes: PropTypes.array,
  onToast: PropTypes.func,
  onClose: PropTypes.func,
  onChanged: PropTypes.func,
};
