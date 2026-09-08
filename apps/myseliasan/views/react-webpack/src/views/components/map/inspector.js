import { useEffect, useMemo, useState } from 'react';
import PropTypes from 'prop-types';
import { useT, Ico } from '@shared';
import { api } from '../../lib/helpers';
import { nodeToneKey } from '../../lib/fleet_status';
import { hasDrawablePlan, siteGlyph } from '../site_kinds';
import { nodeKindOf } from '../layout';

// Inspector is the map's right pane: ONE contextual card for whatever the tree or the plan has
// selected. It replaces the scatter of floating popups the map used to answer with - a card that
// covered the thing you clicked, moved when the map moved, and could not be read alongside the
// plan it described.
//
// Live footage is the deliberate exception: it stays a floating window, because watching a camera
// while navigating somewhere else is the entire point of it.

const KIND_ICON = { camera: 'video', iot: 'cpu', door: 'door' };
const SEV_RANK = { critical: 3, warning: 2, info: 1 };
const TONE_ORDER = ['critical', 'warning', 'online', 'idle'];
const hasFootage = (e) => e && e.refType === 'alert_event' && Number(e.refId) > 0;

function shortAgo(sec) {
  if (!sec) return '';
  const d = Math.max(0, Math.floor(Date.now() / 1000) - sec);
  if (d < 60) return `${d}s`;
  if (d < 3600) return `${Math.floor(d / 60)}m`;
  if (d < 86400) return `${Math.floor(d / 3600)}h`;
  return `${Math.floor(d / 86400)}d`;
}

function Head({ glyph, emoji, title, sub, status, statusLabel }) {
  return (
    <div className="mw-insp-head">
      <div className="mw-insp-ic">{emoji || <Ico n={glyph} sz={20} />}</div>
      <div className="mw-insp-id">
        <div className="mw-insp-t" title={title}>{title}</div>
        {sub ? <div className="mw-insp-s">{sub}</div> : null}
        {status ? <div><span className={`mw-pill ${status}`}>{statusLabel}</span></div> : null}
      </div>
    </div>
  );
}
Head.propTypes = { glyph: PropTypes.string, emoji: PropTypes.string, title: PropTypes.string, sub: PropTypes.string, status: PropTypes.string, statusLabel: PropTypes.string };

// ---------------------------------------------------------------------------------------------
// A place: its areas, what is in them, and the way into the editor.
function PlaceCard({ row, plans, nodesById, nowSec, onOpenArea, onEdit }) {
  const t = useT();
  const s = row.site;
  let worst = 'idle';
  (row.nodeIds || []).forEach((nid) => {
    const k = nodeToneKey(nodesById[nid], nowSec);
    if (TONE_ORDER.indexOf(k) < TONE_ORDER.indexOf(worst)) worst = k;
  });
  const areas = (plans && plans.list) || [];
  const drawable = hasDrawablePlan(s.kind);
  return (
    <>
      <Head
        emoji={siteGlyph(s)}
        title={s.name}
        sub={drawable ? t('insp.nAreas', { n: areas.length }) : t(`bld.kind.${s.kind || 'building'}`)}
        status={worst}
        statusLabel={t(`map.legend.${worst}`)}
      />
      <div className="mw-inspscroll">
        <div className="mw-kpis">
          <div className="mw-kpi"><div className="v">{(row.cameraKeys || []).length}</div><div className="l">{t('map.cameras')}</div></div>
          <div className="mw-kpi"><div className="v">{s.mapPlaced ? t('insp.yes') : t('insp.no')}</div><div className="l">{t('insp.onTheMap')}</div></div>
        </div>
        {/* A point asset's single area is implicit and unnamed, so listing it would put a row
            reading "At this point" between the junction and its cameras. */}
        {drawable ? (
          <>
            <div className="mw-seclbl">{t('bld.areas')}</div>
            {areas.length === 0 ? <div className="mw-insp-note">{t('map.noAreasYet')}</div> : areas.map((fp) => (
              <button key={fp.floor.id} type="button" className="mw-lrow" onClick={() => onOpenArea(s, fp.floor.id)}>
                <Ico n="grid2" sz={13} />
                <span className="nm">{fp.floor.name}</span>
                <span className="rt">{(fp.placements || []).filter((p) => p.cameraId).length}</span>
              </button>
            ))}
          </>
        ) : null}
        <div className="mw-act">
          <button type="button" className="mw-btn primary" onClick={() => onEdit(s)}>
            <Ico n="edit-2" sz={13} /> {drawable ? t('bld.editAreas') : t('map.editAsset')}
          </button>
        </div>
      </div>
    </>
  );
}
PlaceCard.propTypes = { row: PropTypes.object, plans: PropTypes.object, nodesById: PropTypes.object, nowSec: PropTypes.number, onOpenArea: PropTypes.func, onEdit: PropTypes.func };

// ---------------------------------------------------------------------------------------------
// A camera: what it has seen lately, and the way to watch it.
const EVENTS_PAGE = 40;
const EVENTS_MAX = 400;

function CameraCard({ sel, onPlay, onOpenMedia, onLocate }) {
  const t = useT();
  // Page by GROWING the fetch limit: the endpoint returns the newest N for the whole node, which
  // we then filter to this camera, so scrolling asks for more of the node's feed.
  const [limit, setLimit] = useState(EVENTS_PAGE);
  const [state, setState] = useState({ loading: true, list: [], atEnd: false });
  const [loadingMore, setLoadingMore] = useState(false);

  useEffect(() => {
    let live = true;
    if (limit > EVENTS_PAGE) setLoadingMore(true);
    api(`/api/notifications?nodeId=${encodeURIComponent(sel.nodeId)}&limit=${limit}`, { noRedirect: true })
      .then((r) => {
        if (!live) return;
        const rows = Array.isArray(r.body?.items) ? r.body.items : (Array.isArray(r.body) ? r.body : []);
        const mine = rows.filter((e) => String(e.cameraId) === String(sel.cameraId));
        mine.sort((a, b) => (SEV_RANK[(b.severity || '').toLowerCase()] || 0) - (SEV_RANK[(a.severity || '').toLowerCase()] || 0) || (b.createdAt || 0) - (a.createdAt || 0));
        setState({ loading: false, list: mine, atEnd: rows.length < limit || limit >= EVENTS_MAX });
        setLoadingMore(false);
      })
      .catch(() => { if (live) { setState((x) => ({ ...x, loading: false })); setLoadingMore(false); } });
    return () => { live = false; };
  }, [sel.nodeId, sel.cameraId, limit]);

  const loadMore = () => { if (!state.atEnd && !loadingMore) setLimit((l) => Math.min(l + EVENTS_PAGE, EVENTS_MAX)); };
  const onScroll = (e) => { const el = e.currentTarget; if (el.scrollHeight - el.scrollTop - el.clientHeight < 90) loadMore(); };

  return (
    <>
      <Head glyph="video" title={sel.name} sub={[sel.siteName, sel.floorName].filter(Boolean).join(' · ')} />
      <div className="mw-act mw-cam-actions">
        <button type="button" className="mw-btn primary" onClick={(e) => onPlay({ nodeId: sel.nodeId, cameraId: sel.cameraId, name: sel.name }, e.clientX, e.clientY)}>
          <Ico n="play" sz={13} /> {t('tree.openLive')}
        </button>
        {onLocate ? (
          <button type="button" className="mw-btn" onClick={() => onLocate(sel)}><Ico n="map-pin" sz={13} /> {t('map.locate')}</button>
        ) : null}
      </div>
      <div className="mw-seclbl mw-cam-evhdr">
        {t('map.events')}{state.list.length ? <span className="mw-evcount">{state.list.length}</span> : null}
      </div>
      {/* Its own bounded scroller, so a busy camera does not stretch the pane past the plan. */}
      <div className="mw-inspscroll mw-evlist" onScroll={onScroll}>
        {state.loading ? <div className="mw-insp-note">{t('common.loading')}</div> : null}
        {!state.loading && state.list.length === 0 ? <div className="mw-insp-note">{t('map.noEvents')}</div> : null}
        {state.list.map((e) => {
          const title = e.title || e.body || t('map.event');
          return (
            <div key={e.id} className="mw-erow">
              <span className={`mw-dot sev-${(e.severity || 'info').toLowerCase()}`} />
              {hasFootage(e) ? (
                <button type="button" className="nm link" onClick={(ev) => onOpenMedia({ nodeId: sel.nodeId, alertId: Number(e.refId), name: title }, ev.clientX, ev.clientY)}>{title}</button>
              ) : <span className="nm">{title}</span>}
              <span className="rt">{shortAgo(e.createdAt)}</span>
            </div>
          );
        })}
        {loadingMore ? <div className="mw-insp-note">{t('common.loading')}</div> : null}
        {!state.loading && !state.atEnd && !loadingMore ? (
          <button type="button" className="mw-loadmore" onClick={loadMore}>{t('insp.loadOlder')}</button>
        ) : null}
      </div>
    </>
  );
}
CameraCard.propTypes = { sel: PropTypes.object, onPlay: PropTypes.func, onOpenMedia: PropTypes.func, onLocate: PropTypes.func };

// ---------------------------------------------------------------------------------------------
// An appliance. THE card this whole rework exists to make possible: where its cameras actually
// are, which is a list, not a place - and separately, where its own box sits.
function ApplianceCard({ node, placements, camsByNode, nowSec, onOpenNode, onOpenArea, onWaive }) {
  const t = useT();
  const tone = nodeToneKey(node, nowSec);
  const mine = useMemo(() => placements.filter((p) => p.nodeId === node.nodeId), [placements, node.nodeId]);
  const boxPin = mine.find((p) => !p.cameraId);
  const camPins = mine.filter((p) => p.cameraId);

  // Cameras per place. A recorder feeding three buildings has three rows here, and that is the
  // fact no single "which site is this node in?" field could ever carry.
  const fanOut = useMemo(() => {
    const by = new Map();
    camPins.forEach((p) => {
      const key = p.siteId || 0;
      const cur = by.get(key) || { siteId: p.siteId, siteName: p.siteName, floorId: p.floorId, n: 0 };
      cur.n += 1;
      by.set(key, cur);
    });
    return Array.from(by.values()).sort((a, b) => b.n - a.n);
  }, [camPins]);

  const entry = camsByNode[node.nodeId];
  const reachable = !!(entry && !entry.loading && !entry.error);
  const total = reachable ? (entry.cams || []).length : null;
  const unplaced = reachable ? Math.max(0, total - camPins.length) : null;

  return (
    <>
      <Head
        glyph={KIND_ICON[nodeKindOf(node)] || 'cpu'}
        title={node.name || node.nodeId}
        sub={node.nodeId}
        status={tone}
        statusLabel={t(`map.legend.${tone}`)}
      />
      <div className="mw-inspscroll">
        <div className="mw-kpis">
          <div className="mw-kpi"><div className="v">{camPins.length}</div><div className="l">{t('insp.camerasPlaced')}</div></div>
          {/* Never render "0 unplaced" for a node we could not reach - that reads as finished. */}
          <div className="mw-kpi"><div className="v">{unplaced === null ? '?' : unplaced}</div><div className="l">{t('insp.camerasUnplaced')}</div></div>
        </div>

        <div className="mw-seclbl">{t('insp.whereItsCamerasAre')}</div>
        {fanOut.length === 0 ? (
          <div className="mw-insp-note">{t('insp.noCamerasPlaced')}</div>
        ) : fanOut.map((f) => (
          <button key={f.siteId} type="button" className="mw-lrow" onClick={() => onOpenArea({ id: f.siteId, name: f.siteName }, f.floorId)}>
            <Ico n="building" sz={13} />
            <span className="nm">{f.siteName || t('insp.unknownPlace')}</span>
            <span className="rt">{f.n}</span>
          </button>
        ))}
        {!reachable ? <div className="mw-insp-note">{t('insp.camerasUnknownNote')}</div> : null}

        <div className="mw-seclbl">{t('insp.whereItsBoxIs')}</div>
        {boxPin ? (
          <button type="button" className="mw-lrow" onClick={() => onOpenArea({ id: boxPin.siteId, name: boxPin.siteName }, boxPin.floorId)}>
            <Ico n="map-pin" sz={13} />
            <span className="nm">{[boxPin.siteName, boxPin.floorName].filter(Boolean).join(' · ')}</span>
          </button>
        ) : (
          <div className="mw-insp-note">
            {node.noFixedLocation ? t('insp.boxNoFixedLocation') : t('insp.boxNotPinned')}
          </div>
        )}

        <div className="mw-act">
          {onOpenNode ? <button type="button" className="mw-btn primary" onClick={() => onOpenNode(node.nodeId)}>{t('map.openNode')}</button> : null}
          {!boxPin ? (
            <button type="button" className="mw-btn" onClick={() => onWaive(node, !node.noFixedLocation)}>
              <Ico n={node.noFixedLocation ? 'undo' : 'check-ok'} sz={13} />
              {node.noFixedLocation ? t('tree.undoNoFixedLocation') : t('tree.markNoFixedLocation')}
            </button>
          ) : null}
        </div>
      </div>
    </>
  );
}
ApplianceCard.propTypes = {
  node: PropTypes.object, placements: PropTypes.array,
  camsByNode: PropTypes.object, nowSec: PropTypes.number,
  onOpenNode: PropTypes.func, onOpenArea: PropTypes.func, onWaive: PropTypes.func,
};

// ---------------------------------------------------------------------------------------------
export function Inspector({
  sel, sites = [], nodesById = {}, plansBySite = {}, placements = [], camsByNode = {}, nowSec,
  onPlay, onOpenMedia, onLocate, onOpenArea, onEdit, onOpenNode, onWaive,
}) {
  const t = useT();
  const sitesById = useMemo(() => {
    const m = {};
    sites.forEach((r) => { if (r.site) m[r.site.id] = r; });
    return m;
  }, [sites]);

  if (sel && sel.type === 'camera') {
    return <CameraCard key={`${sel.nodeId}::${sel.cameraId}`} sel={sel} onPlay={onPlay} onOpenMedia={onOpenMedia} onLocate={onLocate} />;
  }
  if (sel && sel.type === 'node') {
    const node = nodesById[sel.nodeId];
    if (node) {
      return (
        <ApplianceCard
          key={sel.nodeId}
          node={node}
          placements={placements}
          camsByNode={camsByNode}
          nowSec={nowSec}
          onOpenNode={onOpenNode}
          onOpenArea={onOpenArea}
          onWaive={onWaive}
        />
      );
    }
  }
  const siteId = sel && (sel.type === 'site' ? sel.id : sel.siteId);
  const row = siteId ? sitesById[siteId] : null;
  if (row) {
    return <PlaceCard key={row.site.id} row={row} plans={plansBySite[row.site.id]} nodesById={nodesById} nowSec={nowSec} onOpenArea={onOpenArea} onEdit={onEdit} />;
  }
  return (
    <div className="mw-insp-empty">
      <Ico n="map" sz={30} />
      <div>
        <strong>{t('insp.nothingSelected')}</strong>
        <div className="mw-insp-empty-sub">{t('insp.nothingSelectedHint')}</div>
      </div>
    </div>
  );
}

Inspector.propTypes = {
  sel: PropTypes.object, sites: PropTypes.array, nodesById: PropTypes.object,
  plansBySite: PropTypes.object, placements: PropTypes.array, camsByNode: PropTypes.object, nowSec: PropTypes.number,
  onPlay: PropTypes.func, onOpenMedia: PropTypes.func, onLocate: PropTypes.func,
  onOpenArea: PropTypes.func, onEdit: PropTypes.func, onOpenNode: PropTypes.func, onWaive: PropTypes.func,
};
