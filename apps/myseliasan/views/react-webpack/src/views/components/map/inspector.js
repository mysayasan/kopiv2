import { useEffect, useMemo, useState } from 'react';
import PropTypes from 'prop-types';
import { useT, Ico } from '@shared';
import { api } from '../../lib/helpers';
import { nodeToneKey } from '../../lib/fleet_status';
import { hasDrawablePlan, siteGlyph } from '../site_kinds';
import { planHref } from '../../lib/plan_route';

// Inspector is the map's right pane: ONE contextual card for whatever the tree or the plan has
// selected. It replaces the scatter of floating popups the map used to answer with - a card that
// covered the thing you clicked, moved when the map moved, and could not be read alongside the
// plan it described.
//
// Live footage is the deliberate exception: it stays a floating window, because watching a camera
// while navigating somewhere else is the entire point of it.

// An appliance is a BOARD - a mini PC or a Pi - whatever it happens to manage. Kind used to
// pick the glyph, which made a recorder look like a camera and a door controller look like a
// door: the icon showed what the box WATCHES rather than what it IS, and a camera pin and its
// recorder's pin were then indistinguishable on the same plan. Kind is still on the row, in
// the name, and in the inspector.
const APPLIANCE_ICON = 'board';
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
function PlaceCard({ row, plans, nodesById, nowSec, onOpenArea }) {
  const t = useT();
  const s = row.site;
  let worst = 'idle';
  (row.nodeIds || []).forEach((nid) => {
    const k = nodeToneKey(nodesById[nid], nowSec);
    if (TONE_ORDER.indexOf(k) < TONE_ORDER.indexOf(worst)) worst = k;
  });
  // "Not asked yet" is not "none". This card reads its areas out of a lazily-filled cache, and
  // treating a missing entry as an empty list told the operator a three-floor building had no
  // areas - and offered to create the ones that already existed. Until the fetch answers, the
  // card says nothing about areas rather than something false.
  const loaded = !!(plans && !plans.loading);
  const areas = (plans && plans.list) || [];
  const drawable = hasDrawablePlan(s.kind);
  return (
    <>
      <Head
        emoji={siteGlyph(s)}
        title={s.name}
        sub={drawable ? (loaded ? t('insp.nAreas', { n: areas.length }) : t('common.loading')) : t(`bld.kind.${s.kind || 'building'}`)}
        status={worst}
        statusLabel={t(`map.legend.${worst}`)}
      />
      <div className="mw-inspscroll">
        {/* One line of facts, not two 60px tiles. The tiles gave "7" and the word "Yes" the visual
            weight of a headline each and pushed everything that is actually actionable - the areas,
            the way into the editor - below the fold of a 300px pane. */}
        <div className="mw-facts">
          <span className="mw-fact"><b>{(row.cameraKeys || []).length}</b> {t('map.cameras')}</span>
          {drawable && loaded ? <span className="mw-fact"><b>{areas.length}</b> {t('bld.areas')}</span> : null}
          <span className={`mw-fact${s.mapPlaced ? '' : ' warn'}`}>
            <Ico n="map-pin" sz={11} /> {s.mapPlaced ? t('insp.onTheMap') : t('insp.notOnTheMap')}
          </span>
        </div>
        {/* A point asset's single area is implicit and unnamed, so listing it would put a row
            reading "At this point" between the junction and its cameras. */}
        {drawable ? (
          <>
            <div className="mw-seclbl">{t('bld.areas')}</div>
            {!loaded ? <div className="mw-insp-note">{t('common.loading')}</div>
              : areas.length === 0 ? <div className="mw-insp-note">{t('map.noAreasYet')}</div> : areas.map((fp) => (
              <button key={fp.floor.id} type="button" className="mw-lrow" onClick={() => onOpenArea(s, fp.floor.id)}>
                <Ico n="grid2" sz={13} />
                <span className="nm">{fp.floor.name}</span>
                <span className="rt">{(fp.placements || []).filter((p) => p.cameraId).length}</span>
              </button>
            ))}
          </>
        ) : null}
        <div className="mw-act">
          {/* A real link, not a button with an onClick: the plan opens in its own tab, and an
              anchor is what makes ctrl-click, middle-click, "open in new window" and "copy link
              address" work. A scripted window.open gives up every one of them. */}
          <a className="mw-btn primary" href={planHref(s.id)} target="_blank" rel="noopener noreferrer">
            <Ico n="edit-2" sz={13} /> {drawable ? t('bld.editAreas') : t('map.editAsset')}
          </a>
        </div>
      </div>
    </>
  );
}
PlaceCard.propTypes = { row: PropTypes.object, plans: PropTypes.object, nodesById: PropTypes.object, nowSec: PropTypes.number, onOpenArea: PropTypes.func };

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
        glyph={APPLIANCE_ICON}
        title={node.name || node.nodeId}
        sub={node.nodeId}
        status={tone}
        statusLabel={t(`map.legend.${tone}`)}
      />
      <div className="mw-inspscroll">
        <div className="mw-facts">
          <span className="mw-fact"><b>{camPins.length}</b> {t('insp.camerasPlaced')}</span>
          {/* Never render "0 unplaced" for a node we could not reach - that reads as finished. */}
          <span className={`mw-fact${unplaced ? ' warn' : ''}`}>
            <b>{unplaced === null ? '?' : unplaced}</b> {t('insp.camerasUnplaced')}
          </span>
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
// Nothing selected: a roll-up of the whole fleet rather than an empty pane.
function FleetSummary({ sites, nodesById, placements, nowSec }) {
  const t = useT();
  const nodes = Object.values(nodesById || {});
  const byTone = { online: 0, warning: 0, critical: 0, idle: 0 };
  nodes.forEach((n) => { byTone[nodeToneKey(n, nowSec)] += 1; });
  const placedCams = placements.filter((p) => p.cameraId).length;
  const onMap = sites.filter((r) => r.site && r.site.mapPlaced).length;

  return (
    <>
      <div className="mw-insp-prompt">
        <Ico n="map" sz={15} />
        <span>{t('insp.nothingSelectedHint')}</span>
      </div>
      <div className="mw-inspscroll">
        <div className="mw-seclbl">{t('insp.fleetAtAGlance')}</div>
        <div className="mw-lrow static"><Ico n="building" sz={13} /><span className="nm">{t('bar.places')}</span><span className="rt">{sites.length}</span></div>
        <div className="mw-lrow static"><Ico n="map-pin" sz={13} /><span className="nm">{t('insp.onTheMap')}</span><span className="rt">{onMap}</span></div>
        <div className="mw-lrow static"><Ico n="video" sz={13} /><span className="nm">{t('insp.camerasPlaced')}</span><span className="rt">{placedCams}</span></div>

        <div className="mw-seclbl">{t('insp.appliances')}</div>
        {/* A status with nothing in it is not news - only the tones actually present get a row,
            so "3 lost" is never buried under three zeroes. */}
        {['critical', 'warning', 'online', 'idle'].filter((k) => byTone[k] > 0).map((k) => (
          <div key={k} className="mw-lrow static">
            <span className={`mw-dot ${k}`} />
            <span className="nm">{t(`map.legend.${k}`)}</span>
            <span className="rt">{byTone[k]}</span>
          </div>
        ))}
      </div>
    </>
  );
}
FleetSummary.propTypes = { sites: PropTypes.array, nodesById: PropTypes.object, placements: PropTypes.array, nowSec: PropTypes.number };

// ---------------------------------------------------------------------------------------------
export function Inspector({
  sel, sites = [], nodesById = {}, plansBySite = {}, placements = [], camsByNode = {}, nowSec,
  onPlay, onOpenMedia, onLocate, onOpenArea, onOpenNode, onWaive,
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
    return <PlaceCard key={row.site.id} row={row} plans={plansBySite[row.site.id]} nodesById={nodesById} nowSec={nowSec} onOpenArea={onOpenArea} />;
  }
  // Nothing selected. This used to be an icon and two lines of grey text floating in the middle
  // of a 660px-tall empty pane - a third of the page's width spent saying "you have not clicked
  // anything yet". The prompt is still here, but it sits at the top and the rest of the pane
  // answers the question an operator actually has on arrival: what does the fleet look like?
  return <FleetSummary sites={sites} nodesById={nodesById} placements={placements} nowSec={nowSec} />;
}

Inspector.propTypes = {
  sel: PropTypes.object, sites: PropTypes.array, nodesById: PropTypes.object,
  plansBySite: PropTypes.object, placements: PropTypes.array, camsByNode: PropTypes.object, nowSec: PropTypes.number,
  onPlay: PropTypes.func, onOpenMedia: PropTypes.func, onLocate: PropTypes.func,
  onOpenArea: PropTypes.func, onOpenNode: PropTypes.func, onWaive: PropTypes.func,
};
