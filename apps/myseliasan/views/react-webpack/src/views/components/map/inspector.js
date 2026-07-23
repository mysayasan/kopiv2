import { useEffect, useRef, useState } from 'react';
import PropTypes from 'prop-types';
import { useT, Ico } from '@shared';
import { api } from '../../lib/helpers';
import { nodeToneKey } from '../../lib/fleet_status';

// Inspector is the right pane: a single contextual card for whatever is selected — a building, a
// camera, or an appliance — replacing the old scatter of floating popups for the primary flow. It
// falls back to the current building's summary (when inside one) or a fleet hint (at the world
// level). Live footage still opens as a floating window from the card's actions.

const STATUS_ORDER = ['critical', 'warning', 'online', 'idle'];
const SEV_RANK = { critical: 3, warning: 2, info: 1 };
const KIND_ICON = { camera: 'video', iot: 'cpu' };

function shortAgo(sec) {
  if (!sec) return '';
  const d = Math.max(0, Math.floor(Date.now() / 1000) - sec);
  if (d < 60) return `${d}s`;
  if (d < 3600) return `${Math.floor(d / 60)}m`;
  if (d < 86400) return `${Math.floor(d / 3600)}h`;
  return `${Math.floor(d / 86400)}d`;
}
const hasFootage = (e) => e && e.refType === 'alert_event' && Number(e.refId) > 0;

function Head({ glyph, emoji, title, sub, status, statusLabel }) {
  return (
    <div className="mw-insp-head">
      <div className="mw-insp-ic">{emoji || <Ico n={glyph} sz={20} />}</div>
      <div className="mw-insp-id">
        <div className="mw-insp-t">{title}</div>
        {sub ? <div className="mw-insp-s">{sub}</div> : null}
        {status ? <div><span className={`mw-pill ${status}`}><span className={`mw-dot ${status}`} />{statusLabel}</span></div> : null}
      </div>
    </div>
  );
}
Head.propTypes = { glyph: PropTypes.string, emoji: PropTypes.string, title: PropTypes.string, sub: PropTypes.string, status: PropTypes.string, statusLabel: PropTypes.string };

export function Inspector({
  sel, level, bId, sites = [], nodes = [], nodesById = {}, floorplansByBuilding = {}, allSites = [], editMode = false,
  onPlayCamera, onOpenMedia, onLocate, onAssignBuilding, onOpenNode, onOpenBuilding, onOpenFloor, onEditBuilding, onPlaceNode,
}) {
  const t = useT();
  const nowSec = Math.floor(Date.now() / 1000);

  if (sel && sel.type === 'camera') {
    return <CameraCard key={`${sel.nodeId}::${sel.cameraId}`} sel={sel} onPlayCamera={onPlayCamera} onOpenMedia={onOpenMedia} onLocate={onLocate} editMode={editMode} />;
  }
  if (sel && sel.type === 'node') {
    const n = nodesById[sel.nodeId] || nodes.find((x) => x.nodeId === sel.nodeId);
    if (n) return <NodeCard node={n} allSites={allSites} floorplansByBuilding={floorplansByBuilding} onAssignBuilding={onAssignBuilding} onOpenNode={onOpenNode} onPlaceNode={onPlaceNode} />;
  }
  // building card when a building is selected OR we're inside one
  const bid = (sel && sel.type === 'building') ? sel.id : (level !== 'world' ? bId : null);
  const row = bid ? sites.find((s) => s.site && s.site.id === bid) : null;
  if (row) {
    return <BuildingCard row={row} nodes={nodes} nodesById={nodesById} plans={floorplansByBuilding[bid] || []} nowSec={nowSec} editMode={editMode} onOpenBuilding={onOpenBuilding} onOpenFloor={onOpenFloor} onEditBuilding={onEditBuilding} onSelectNodeFn={onOpenNode} />;
  }
  return (
    <div className="mw-insp-empty">
      <Ico n="map" sz={30} />
      <div><strong>{t('mw.nothingSelected')}</strong><div className="mw-insp-empty-sub">{t('mw.nothingSelectedHint')}</div></div>
    </div>
  );
}

function BuildingCard({ row, nodes, nodesById, plans, nowSec, editMode, onOpenBuilding, onOpenFloor, onEditBuilding }) {
  const t = useT();
  const s = row.site;
  let worst = 'idle';
  (row.nodeIds || []).forEach((nid) => { const k = nodeToneKey(nodesById[nid], nowSec); if (STATUS_ORDER.indexOf(k) < STATUS_ORDER.indexOf(worst)) worst = k; });
  const residents = nodes.filter((n) => n.siteId === s.id);
  return (
    <>
      <Head emoji={s.icon || '🏢'} title={s.name} sub={`${plans.length} ${t('mw.floors')}`} status={worst} statusLabel={t(`map.legend.${worst}`)} />
      <div className="mw-inspscroll">
        <div className="mw-kpis">
          <div className="mw-kpi"><div className="v">{row.cameras}</div><div className="l">{t('map.cameras')}</div></div>
          <div className="mw-kpi"><div className="v">{plans.length}</div><div className="l">{t('mw.floors')}</div></div>
        </div>
        <div className="mw-seclbl">{t('mw.floors')}</div>
        {plans.length === 0 ? <div className="mw-insp-note">{t('map.noFloorsInBuilding')}</div> : plans.map((fp) => (
          <button key={fp.floor.id} type="button" className="mw-lrow" onClick={() => onOpenFloor(s.id, fp.floor.id)}>
            <Ico n="grid2" sz={13} /><span className="nm">{fp.floor.name}</span><span className="rt">{(fp.placements || []).filter((p) => p.cameraId).length}</span>
          </button>
        ))}
        {residents.length > 0 ? (
          <>
            <div className="mw-seclbl">{t('mw.appliancesHere')}</div>
            {residents.map((n) => (
              <div key={n.nodeId} className="mw-lrow static">
                <span className={`mw-dot ${nodeToneKey(n, nowSec)}`} /><span className="nm">{n.name || n.nodeId}</span><span className="rt">{n.kind === 'iot' ? 'IoT' : t('map.layerNodes')}</span>
              </div>
            ))}
          </>
        ) : null}
        <div className="mw-act">
          <button type="button" className="mw-btn primary" onClick={() => onOpenBuilding(s)}>{t('mw.openFloorPlan')}</button>
          {editMode ? <button type="button" className="mw-btn" onClick={() => onEditBuilding(s)}><Ico n="edit-2" sz={13} /> {t('map.editBuilding')}</button> : null}
        </div>
      </div>
    </>
  );
}
BuildingCard.propTypes = { row: PropTypes.object, nodes: PropTypes.array, nodesById: PropTypes.object, plans: PropTypes.array, nowSec: PropTypes.number, editMode: PropTypes.bool, onOpenBuilding: PropTypes.func, onOpenFloor: PropTypes.func, onEditBuilding: PropTypes.func };

const EVENTS_PAGE = 40;
const EVENTS_MAX = 400;

function CameraCard({ sel, onPlayCamera, onOpenMedia, onLocate, editMode }) {
  const t = useT();
  // Newest-first, filtered to this camera. We page by GROWING the fetch limit (the endpoint returns
  // the newest N for the node, which we filter by cameraId), so scrolling loads older events.
  const [limit, setLimit] = useState(EVENTS_PAGE);
  const [state, setState] = useState({ loading: true, list: [], atEnd: false });
  const [loadingMore, setLoadingMore] = useState(false);
  const listRef = useRef(null);

  useEffect(() => {
    let live = true;
    if (limit > EVENTS_PAGE) setLoadingMore(true);
    api(`/api/notifications?nodeId=${encodeURIComponent(sel.nodeId)}&limit=${limit}`, { noRedirect: true })
      .then((r) => {
        if (!live) return;
        const rows = Array.isArray(r.body?.items) ? r.body.items : (Array.isArray(r.body) ? r.body : []);
        const mine = rows.filter((e) => String(e.cameraId) === String(sel.cameraId));
        mine.sort((a, b) => (SEV_RANK[(b.severity || '').toLowerCase()] || 0) - (SEV_RANK[(a.severity || '').toLowerCase()] || 0) || (b.createdAt || 0) - (a.createdAt || 0));
        // We've seen everything once the node returned fewer rows than we asked for, or we hit the cap.
        setState({ loading: false, list: mine, atEnd: rows.length < limit || limit >= EVENTS_MAX });
        setLoadingMore(false);
      })
      .catch(() => { if (live) { setState((s) => ({ ...s, loading: false })); setLoadingMore(false); } });
    return () => { live = false; };
  }, [sel.nodeId, sel.cameraId, limit]);

  const loadMore = () => { if (!state.atEnd && !loadingMore) setLimit((l) => Math.min(l + EVENTS_PAGE, EVENTS_MAX)); };
  const onScroll = (e) => { const el = e.currentTarget; if (el.scrollHeight - el.scrollTop - el.clientHeight < 90) loadMore(); };

  return (
    <>
      <Head glyph="video" title={sel.name} sub={`${sel.buildingName || ''}${sel.floorName ? ` · ${sel.floorName}` : ''}`} />
      <div className="mw-act mw-cam-actions">
        <button type="button" className="mw-btn primary" onClick={(e) => onPlayCamera(sel, e.clientX, e.clientY)}><Ico n="play" sz={13} /> {t('mw.openLive')}</button>
        {onLocate ? <button type="button" className="mw-btn" onClick={() => onLocate(sel)}><Ico n="map-pin" sz={13} /> {t('map.locate')}</button> : null}
      </div>
      <div className="mw-seclbl mw-cam-evhdr">{t('map.events')}{state.list.length ? <span className="mw-evcount">{state.list.length}</span> : null}</div>
      {/* The events list is its OWN bounded scroller so a busy camera doesn't stretch the panel; it
          loads older events as you scroll (infinite), with a fallback button. */}
      <div className="mw-inspscroll mw-evlist" ref={listRef} onScroll={onScroll}>
        {state.loading ? <div className="mw-insp-note">{t('common.loading')}</div> : null}
        {!state.loading && state.list.length === 0 ? <div className="mw-insp-note">{t('map.noEvents')}</div> : null}
        {state.list.map((e) => {
          const title = e.title || e.body || t('map.event');
          const footage = hasFootage(e);
          return (
            <div key={e.id} className="mw-erow">
              <span className={`mw-dot sev-${(e.severity || 'info').toLowerCase()}`} />
              {footage ? (
                <button type="button" className="nm link" onClick={(ev) => onOpenMedia({ nodeId: sel.nodeId, alertId: Number(e.refId), name: title }, ev.clientX, ev.clientY)}>{title}</button>
              ) : <span className="nm">{title}</span>}
              <span className="rt">{shortAgo(e.createdAt)}</span>
            </div>
          );
        })}
        {loadingMore ? <div className="mw-insp-note">{t('common.loading')}</div> : null}
        {!state.loading && !state.atEnd && !loadingMore ? <button type="button" className="mw-loadmore" onClick={loadMore}>{t('mw.loadOlder')}</button> : null}
        {editMode ? <div className="mw-insp-note edit">{t('mw.cameraEditNote')}</div> : null}
      </div>
    </>
  );
}
CameraCard.propTypes = { sel: PropTypes.object, onPlayCamera: PropTypes.func, onOpenMedia: PropTypes.func, onLocate: PropTypes.func, editMode: PropTypes.bool };

function NodeCard({ node, allSites, floorplansByBuilding, onAssignBuilding, onOpenNode, onPlaceNode }) {
  const t = useT();
  const streamed = Object.values(floorplansByBuilding).flat().reduce((acc, fp) => acc + (fp.placements || []).filter((p) => p.nodeId === node.nodeId && p.cameraId).length, 0);
  return (
    <>
      <Head glyph={KIND_ICON[node.kind] || 'cpu'} title={node.name || node.nodeId} sub={node.kind === 'iot' ? t('mw.iotHub') : t('mw.recorder')} status={nodeToneKey(node)} statusLabel={t(`map.legend.${nodeToneKey(node)}`)} />
      <div className="mw-inspscroll">
        <div className="mw-kpis">
          <div className="mw-kpi"><div className="v">{streamed}</div><div className="l">{t('mw.camerasStreamed')}</div></div>
          <div className="mw-kpi"><div className="v">{node.kind === 'iot' ? '—' : '✓'}</div><div className="l">{t('mw.cert')}</div></div>
        </div>
        <div className="mw-field">
          <label>{t('map.residesIn')}</label>
          <select value={node.siteId ? String(node.siteId) : ''} onChange={(e) => onAssignBuilding(node.nodeId, Number(e.target.value) || 0)}>
            <option value="">{t('map.noBuilding')}</option>
            {allSites.map((s) => <option key={s.id} value={s.id}>{s.icon ? `${s.icon} ${s.name}` : s.name}</option>)}
          </select>
        </div>
        <div className="mw-insp-note">{t('mw.residesNote')}</div>
        <div className="mw-act">
          {onOpenNode ? <button type="button" className="mw-btn primary" onClick={() => onOpenNode(node.nodeId)}>{t('map.openNode')}</button> : null}
          {!node.siteId && onPlaceNode ? <button type="button" className="mw-btn" onClick={() => onPlaceNode(node)}><Ico n="map-pin" sz={13} /> {t('map.placeStandalone')}</button> : null}
        </div>
      </div>
    </>
  );
}
NodeCard.propTypes = { node: PropTypes.object, allSites: PropTypes.array, floorplansByBuilding: PropTypes.object, onAssignBuilding: PropTypes.func, onOpenNode: PropTypes.func, onPlaceNode: PropTypes.func };

Inspector.propTypes = {
  sel: PropTypes.object, level: PropTypes.string, bId: PropTypes.number, sites: PropTypes.array, nodes: PropTypes.array,
  nodesById: PropTypes.object, floorplansByBuilding: PropTypes.object, allSites: PropTypes.array, editMode: PropTypes.bool,
  onPlayCamera: PropTypes.func, onOpenMedia: PropTypes.func, onLocate: PropTypes.func, onAssignBuilding: PropTypes.func,
  onOpenNode: PropTypes.func, onOpenBuilding: PropTypes.func, onOpenFloor: PropTypes.func, onEditBuilding: PropTypes.func, onPlaceNode: PropTypes.func,
};
