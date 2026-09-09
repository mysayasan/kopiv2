import { useMemo } from 'react';
import PropTypes from 'prop-types';
import { useT, Ico } from '@shared';
import { nodeToneKey, TONES } from '../../lib/fleet_status';
import { hasDrawablePlan, multiPlan, siteGlyph } from '../site_kinds';

// TwinTree is the fleet map's rail: TWO trees with different jobs, stacked in one scroller.
//
// The shape follows the one fact the old rail could not express: a mymatasan recorder's cameras
// can be in DIFFERENT places. So a node is an OCCUPANT of a place, never a container of one, and
// the containment axis is camera -> area -> place. A camera holds exactly one pin fleet-wide (the
// (NodeId, CameraId) unique key), which is what makes every camera appear in exactly one branch.
//
//   Places          the twin. Sites hold areas, areas hold the cameras pinned in them. The
//                   recorder shows only as a dim tag beside each camera, so one NVR feeding two
//                   buildings reads as what it is instead of forcing a choice of parent.
//   Not placed yet  the tray, and the ONLY place a node is a parent - because "which recorder is
//                   it on" is exactly how an operator finds a camera that still needs placing.
//
// A camera is in the tray or in a place, never both and never neither, so emptying the tray is a
// finishable job. The counter on the Places root is the progress bar for it.

// The drag payload's type. Named for what it carries - a thing to place - because it is a camera
// on a camera row and the appliance itself on an appliance row.
const PICK_MIME = 'text/tray-pick';

const TONE_ORDER = ['critical', 'warning', 'online', 'idle'];
const worseTone = (a, b) => (TONE_ORDER.indexOf(b) < TONE_ORDER.indexOf(a) ? b : a);
// An appliance is a BOARD - a mini PC or a Pi - whatever it happens to manage. Kind used to
// pick the glyph, which made a recorder look like a camera and a door controller look like a
// door: the icon showed what the box WATCHES rather than what it IS, and a camera pin and its
// recorder's pin were then indistinguishable on the same plan. Kind is still on the row, in
// the name, and in the inspector.
const APPLIANCE_ICON = 'board';

// nodeLabel is what an operator calls the appliance; nodeId is the fallback a never-named node has.
const nodeLabel = (n) => (n && (n.name || n.nodeId)) || '';

function Caret({ open, onClick, label }) {
  return (
    <button type="button" className="tt-caret" onClick={onClick} aria-expanded={open} aria-label={label}>
      <Ico n={open ? 'chev-down' : 'chev-right'} sz={12} />
    </button>
  );
}
Caret.propTypes = { open: PropTypes.bool, onClick: PropTypes.func, label: PropTypes.string };

const Dot = ({ tone }) => <span className="tt-dot" style={{ background: TONES[tone].color }} />;
Dot.propTypes = { tone: PropTypes.string };

export function TwinTree({
  sites = [], nodes = [], nodesById = {}, nowSec,
  plansBySite = {}, camsByNode = {}, placedKeys,
  expanded, onToggle, query, onQuery, placing,
  onAddSite, onOpenSite, onOpenArea, onOpenCamera, onEditSite, onSelectNode, onPlayCamera,
  onPlaceCamera, onWaiveLocation, dropTarget, onDropTarget,
}) {
  const t = useT();
  const isOpen = (key) => expanded.has(key);

  // Dragging something out of the tray onto a place is the whole verb: "this belongs there". The
  // payload is a PICK - a camera, or an appliance itself (cameraId ''), which is how the box gets
  // pinned and therefore how the node's location is recorded at all. The drop target decides which
  // area it lands on; the editor takes it from there for the exact spot.
  const dragPick = (e, payload) => {
    e.dataTransfer.setData(PICK_MIME, JSON.stringify(payload));
    e.dataTransfer.effectAllowed = 'copy';
  };
  const readPick = (e) => {
    try {
      const raw = e.dataTransfer.getData(PICK_MIME);
      return raw ? JSON.parse(raw) : null;
    } catch (_) { return null; }
  };
  // A row only lights up for a drag it can actually accept, so "nothing happens" is never the
  // answer to a drop the UI appeared to invite.
  const dropProps = (key, onDrop) => ({
    onDragOver: (e) => {
      if (e.dataTransfer.types.indexOf(PICK_MIME) < 0) return;
      e.preventDefault();
      e.dataTransfer.dropEffect = 'copy';
      if (dropTarget !== key) onDropTarget(key);
    },
    onDragLeave: () => { if (dropTarget === key) onDropTarget(null); },
    onDrop: (e) => {
      const payload = readPick(e);
      onDropTarget(null);
      if (!payload) return;
      e.preventDefault();
      onDrop(payload);
    },
  });
  const q = (query || '').trim().toLowerCase();
  const hit = (s) => !q || (s || '').toLowerCase().includes(q);

  // --- the tray: every camera on every node that holds no pin ----------------------------------
  // An UNREACHABLE node is the trap here. Its camera list cannot be fetched, so folding that into
  // "0 unplaced" would quietly tell an operator the job is finished. It reports unknown instead.
  const tray = useMemo(() => nodes.map((n) => {
    const entry = camsByNode[n.nodeId];
    const known = !!(entry && !entry.loading && !entry.error);
    const unplaced = known
      ? (entry.cams || []).filter((c) => !placedKeys.has(`${n.nodeId}::${c.id}`))
      : [];
    return {
      node: n,
      loading: !entry || entry.loading,
      unknown: !!(entry && entry.error),
      known,
      unplaced,
      total: known ? (entry.cams || []).length : 0,
      // A node whose own marker is nowhere is a separate loose end from its cameras: the box has
      // to be SOMEWHERE, and until it is the map cannot show where this recorder physically sits.
      boxUnplaced: !placedKeys.has(`${n.nodeId}::`),
    };
  }), [nodes, camsByNode, placedKeys]);

  const trayRows = tray.filter((r) => r.unplaced.length > 0 || r.unknown || r.loading
    || (r.boxUnplaced && !r.node.noFixedLocation));
  const shownTray = trayRows.filter((r) => !q || hit(nodeLabel(r.node)) || r.unplaced.some((c) => hit(c.name)));
  const unplacedTotal = tray.reduce((a, r) => a + r.unplaced.length, 0);
  // An appliance counts as a loose end only while nobody has DECIDED about it: a waived box is
  // answered, not outstanding.
  const boxesToPin = tray.filter((r) => r.boxUnplaced && !r.node.noFixedLocation).length;
  const anyUnknown = tray.some((r) => r.unknown);

  // --- the Places root counter: the progress bar for the whole authoring job -------------------
  const placedTotal = useMemo(
    () => sites.reduce((a, row) => a + ((row.cameraKeys || []).length), 0),
    [sites],
  );
  const grandTotal = placedTotal + unplacedTotal;

  const siteTone = (row) => {
    let worst = 'idle';
    (row.nodeIds || []).forEach((nid) => { worst = worseTone(worst, nodeToneKey(nodesById[nid], nowSec)); });
    return worst;
  };

  // A site matches the search if IT matches, or anything loaded inside it does - so typing a
  // camera name surfaces the place that holds it rather than emptying the tree.
  const siteMatches = (row) => {
    if (hit(row.site.name)) return true;
    if (!q) return true;
    const plans = (plansBySite[row.site.id] || {}).list || [];
    return plans.some((fp) => hit(fp.floor.name) || (fp.placements || []).some((p) => hit(p.lastKnownName)));
  };
  const shownSites = sites.filter((row) => row.site && siteMatches(row));

  function renderCamera(row, fp, p) {
    const owner = nodesById[p.nodeId];
    const tone = nodeToneKey(owner, nowSec);
    // A placement with no cameraId is the appliance itself, not a camera.
    if (!p.cameraId) {
      return (
        <div key={p.id} className="tt-row tt-leaf">
          <span className="tt-caret-gap" />
          <Dot tone={tone} />
          <Ico n={APPLIANCE_ICON} sz={11} />
          <button type="button" className="tt-name dim" onClick={(e) => onSelectNode(owner || { nodeId: p.nodeId }, e.clientX, e.clientY)}>
            {p.lastKnownName || nodeLabel(owner) || p.nodeId}
          </button>
          <span className="tt-tag" title={t('tree.theBox')}>{t('tree.theBox')}</span>
        </div>
      );
    }
    return (
      <div key={p.id} className="tt-row tt-leaf">
        <span className="tt-caret-gap" />
        <Dot tone={tone} />
        <Ico n="video" sz={11} />
        <button
          type="button"
          className="tt-name"
          onClick={() => onOpenCamera(row.site, fp.floor, p)}
          title={p.lastKnownName || t('tree.showOnPlan')}
        >
          {p.lastKnownName || t('nodes.cameraN', { id: p.cameraId })}
        </button>
        {/* The recorder as a dim tag, never as a parent - this is where the split-node fact
            actually shows up on screen. */}
        <span className="tt-tag" title={nodeLabel(owner) || p.nodeId}>{nodeLabel(owner) || p.nodeId}</span>
        <button
          type="button"
          className="tt-act"
          onClick={(e) => onPlayCamera({ nodeId: p.nodeId, cameraId: p.cameraId, name: p.lastKnownName || p.cameraId }, e.clientX, e.clientY)}
          title={t('tree.openLive')}
          aria-label={t('tree.openLive')}
        >
          <Ico n="play" sz={11} />
        </button>
      </div>
    );
  }

  function renderArea(row, fp) {
    const key = `area:${fp.floor.id}`;
    const open = isOpen(key) || (!!q && (fp.placements || []).some((p) => hit(p.lastKnownName)));
    const cams = (fp.placements || []).filter((p) => !q || hit(p.lastKnownName) || hit(fp.floor.name));
    const camCount = (fp.placements || []).filter((p) => p.cameraId).length;
    return (
      <div key={fp.floor.id} className="tt-branch">
        <div className={`tt-row${dropTarget === key ? ' droptarget' : ''}`} {...dropProps(key, (payload) => onPlaceCamera(row.site, fp.floor.id, payload))}>
          <Caret open={open} onClick={() => onToggle(key)} label={t('tree.expand')} />
          <Ico n="grid2" sz={11} />
          <button type="button" className="tt-name dim" onClick={() => onOpenArea(row.site, fp.floor.id)} title={t('tree.openPlan')}>
            {fp.floor.name}
          </button>
          <span className="tt-chip">{camCount}</span>
        </div>
        {open ? (
          <div className="tt-children">
            {cams.length === 0 ? <div className="tt-empty sm">{t('tree.noCamerasHere')}</div> : null}
            {cams.map((p) => renderCamera(row, fp, p))}
          </div>
        ) : null}
      </div>
    );
  }

  function renderSite(row) {
    const s = row.site;
    const key = `site:${s.id}`;
    const open = isOpen(key) || (!!q && !hit(s.name));
    const plans = plansBySite[s.id];
    const onMap = !!s.mapPlaced;
    const placingThis = placing && placing.kind === 'site' && placing.id === s.id;
    const cams = (row.cameraKeys || []).length;
    return (
      <div key={s.id} className="tt-branch">
        <div
          className={`tt-row${placingThis ? ' placing' : ''}${dropTarget === key ? ' droptarget' : ''}`}
          {...dropProps(key, (payload) => onPlaceCamera(row.site, null, payload))}
        >
          <Caret open={open} onClick={() => onToggle(key)} label={t('tree.expand')} />
          <Dot tone={siteTone(row)} />
          <span className="tt-glyph" aria-hidden="true">{siteGlyph(s)}</span>
          <button
            type="button"
            className="tt-name"
            draggable={!onMap}
            onDragStart={!onMap ? (e) => { e.dataTransfer.setData('text/site-id', String(s.id)); e.dataTransfer.effectAllowed = 'move'; } : undefined}
            onClick={() => onOpenSite(row)}
            title={onMap ? t('map.flyTo') : t('map.placeHint')}
          >
            {s.name}
          </button>
          {cams ? <span className="tt-chip">{cams}</span> : null}
          {/* Not on the map yet is a real, actionable state - say so instead of leaving the row
              looking identical to a placed one. */}
          {!onMap ? <span className="tt-tag warn" title={t('map.notOnMap')}>{t('tree.notOnMap')}</span> : null}
          {multiPlan(s.kind) ? (
            <button type="button" className="tt-act" onClick={() => onEditSite(s)} title={t('bld.addArea')} aria-label={t('bld.addArea')}>
              <Ico n="plus" sz={11} />
            </button>
          ) : null}
          <button type="button" className="tt-act" onClick={() => onEditSite(s)} title={t('map.editAsset')} aria-label={t('map.editAsset')}>
            <Ico n="edit-2" sz={11} />
          </button>
        </div>
        {open ? (
          <div className="tt-children">
            {!plans || plans.loading ? <div className="tt-empty sm">{t('common.loading')}</div> : null}
            {plans && !plans.loading && plans.list.length === 0 ? (
              <div className="tt-empty sm">{hasDrawablePlan(s.kind) ? t('map.noAreasYet') : t('tree.noCamerasHere')}</div>
            ) : null}
            {/* A point asset's single area is implicit and unnamed by the operator, so showing it
                as a level would put a row reading "At this point" between a junction and the two
                cameras on it. Hang its cameras straight off the place instead. */}
            {plans && !plans.loading
              ? (hasDrawablePlan(s.kind)
                ? plans.list.map((fp) => renderArea(row, fp))
                : plans.list.flatMap((fp) => (fp.placements || [])
                  .filter((p) => !q || hit(p.lastKnownName))
                  .map((p) => renderCamera(row, fp, p))))
              : null}
          </div>
        ) : null}
      </div>
    );
  }

  function renderTrayNode(r) {
    const key = `tray:${r.node.nodeId}`;
    const open = isOpen(key) || (!!q && r.unplaced.some((c) => hit(c.name)));
    const tone = nodeToneKey(r.node, nowSec);
    // Only worth dragging while the box has no pin: a placed one would be refused as already
    // placed, and the way to move it is to unpin it on the plan it is on.
    const boxPin = r.boxUnplaced && !r.node.noFixedLocation;
    const boxPayload = { nodeId: r.node.nodeId, cameraId: '', name: nodeLabel(r.node) };
    return (
      <div key={r.node.nodeId} className="tt-branch">
        {/* tt-trayrow: a tray row's TAGS are its content - what is unknown, what is unpinned, what
            has been waived - so they wrap onto a second line rather than truncating to stubs. */}
        {/* An appliance whose own box has no pin is draggable too, onto the place it sits in.
            That pin is what records WHERE THE BOX IS - it is the only writer of the node's site -
            so without this the tray could tell you the location was missing but gave you no way
            to answer it from here. */}
        <div
          className={`tt-row tt-trayrow${boxPin ? ' tt-draggable' : ''}`}
          draggable={boxPin}
          onDragStart={boxPin ? (e) => dragPick(e, boxPayload) : undefined}
          title={boxPin ? t('tree.dragApplianceToPlace') : undefined}
        >
          {r.unplaced.length > 0 ? <Caret open={open} onClick={() => onToggle(key)} label={t('tree.expand')} /> : <span className="tt-caret-gap" />}
          <Dot tone={tone} />
          <Ico n={APPLIANCE_ICON} sz={11} />
          <button type="button" className="tt-name" onClick={(e) => onSelectNode(r.node, e.clientX, e.clientY)}>{nodeLabel(r.node)}</button>
          {r.loading ? <span className="tt-tag" title={t('common.loading')}>{t('common.loading')}</span> : null}
          {/* NEVER "0 unplaced" for a node we could not reach: that reads as done. */}
          {r.unknown ? <span className="tt-tag warn" title={t('tree.camerasUnknown')}>{t('tree.camerasUnknown')}</span> : null}
          {r.unplaced.length > 0 ? <span className="tt-chip todo">{t('tree.nUnplaced', { n: r.unplaced.length })}</span> : null}
          {r.boxUnplaced && !r.node.noFixedLocation ? <span className="tt-tag" title={t('tree.boxNotPlaced')}>{t('tree.boxNotPlaced')}</span> : null}
          {r.node.noFixedLocation ? <span className="tt-tag" title={t('tree.noFixedLocationHint')}>{t('tree.noFixedLocation')}</span> : null}
          {/* The exit from the tray for an appliance that genuinely has no place on any plan - a
              colo recorder, a hosted hub. Without it the tray can never be emptied, and a tray
              that can never be emptied is one operators stop reading. */}
          {/* Dragging is the quick path; this is the discoverable one, and the only one that
              works for anybody who cannot drag. */}
          {boxPin ? (
            <button
              type="button"
              className="tt-act"
              onClick={() => onPlaceCamera(null, null, boxPayload)}
              title={t('tree.placeApplianceOnPlan')}
              aria-label={t('tree.placeApplianceOnPlan')}
            >
              <Ico n="map-pin" sz={11} />
            </button>
          ) : null}
          {r.boxUnplaced ? (
            <button
              type="button"
              className="tt-act"
              onClick={() => onWaiveLocation(r.node, !r.node.noFixedLocation)}
              title={r.node.noFixedLocation ? t('tree.undoNoFixedLocation') : t('tree.markNoFixedLocation')}
              aria-label={r.node.noFixedLocation ? t('tree.undoNoFixedLocation') : t('tree.markNoFixedLocation')}
            >
              <Ico n={r.node.noFixedLocation ? 'undo' : 'check-ok'} sz={11} />
            </button>
          ) : null}
        </div>
        {open ? (
          <div className="tt-children">
            {r.unplaced.map((c) => {
              const payload = { nodeId: r.node.nodeId, cameraId: String(c.id), name: c.name || t('nodes.cameraN', { id: c.id }) };
              return (
                <div
                  key={c.id}
                  className="tt-row tt-leaf tt-draggable"
                  draggable
                  onDragStart={(e) => dragPick(e, payload)}
                  title={t('tree.dragToPlace')}
                >
                  <span className="tt-caret-gap" />
                  <Ico n="video" sz={11} />
                  <span className="tt-name dim">{payload.name}</span>
                  {/* Dragging is the quick path; the button is the discoverable one, and the only
                      one that works for anybody who cannot drag. */}
                  <button
                    type="button"
                    className="tt-act"
                    onClick={() => onPlaceCamera(null, null, payload)}
                    title={t('tree.placeOnPlan')}
                    aria-label={t('tree.placeOnPlan')}
                  >
                    <Ico n="map-pin" sz={11} />
                  </button>
                  <button
                    type="button"
                    className="tt-act"
                    onClick={(e) => onPlayCamera(payload, e.clientX, e.clientY)}
                    title={t('tree.openLive')}
                    aria-label={t('tree.openLive')}
                  >
                    <Ico n="play" sz={11} />
                  </button>
                </div>
              );
            })}
          </div>
        ) : null}
      </div>
    );
  }

  return (
    <aside className="fleet-map-rail tt">
      <div className="tt-search">
        <Ico n="search" sz={13} />
        <input
          value={query}
          onChange={(e) => onQuery(e.target.value)}
          placeholder={t('tree.searchPlaceholder')}
          aria-label={t('tree.searchPlaceholder')}
        />
        {query ? (
          <button type="button" className="tt-search-x" onClick={() => onQuery('')} aria-label={t('tree.clear')}>
            <Ico n="x" sz={11} />
          </button>
        ) : null}
      </div>

      <div className="tt-scroll">
        {/* ---- Places: the twin ---- */}
        <div className="tt-row tt-root">
          <Caret open={isOpen('places')} onClick={() => onToggle('places')} label={t('tree.expand')} />
          <Ico n="globe" sz={13} />
          <span className="tt-name">{t('tree.everywhere')}</span>
          <span className="tt-chip">{sites.length}</span>
          {grandTotal > 0 ? (
            <span className={`tt-chip${unplacedTotal > 0 ? ' warn' : ''}`} title={t('tree.placedOfTotalHint')}>
              {t('tree.placedOfTotal', { placed: placedTotal, total: grandTotal })}{anyUnknown ? '+' : ''}
            </span>
          ) : null}
          <button type="button" className="tt-act" onClick={onAddSite} title={t('map.addAsset')} aria-label={t('map.addAsset')}>
            <Ico n="plus" sz={12} />
          </button>
        </div>
        {isOpen('places') ? (
          <div className="tt-children">
            {shownSites.length === 0 ? (
              <div className="tt-empty">{q ? t('tree.noMatches') : t('map.noAssetsYet')}</div>
            ) : null}
            {shownSites.map((row) => renderSite(row))}
          </div>
        ) : null}

        {/* ---- the tray: what is not in the twin yet ---- */}
        <div className="tt-row tt-root">
          <Caret open={isOpen('tray')} onClick={() => onToggle('tray')} label={t('tree.expand')} />
          <Ico n="map-pin" sz={13} />
          <span className="tt-name">{t('tree.notPlacedYet')}</span>
          {unplacedTotal > 0 ? <span className="tt-chip todo" title={t('tree.camerasToPlace')}>{unplacedTotal} <Ico n="video" sz={9} /></span> : null}
          {/* Two loose ends, counted separately, because they are different jobs: cameras that are
              not on any plan, and appliances whose own box has no pin. */}
          {boxesToPin > 0 ? <span className="tt-chip todo" title={t('tree.boxesToPin')}>{boxesToPin} <Ico n="cpu" sz={9} /></span> : null}
          {anyUnknown ? <span className="tt-tag warn" title={t('tree.someUnreachable')}>{t('tree.someUnreachable')}</span> : null}
        </div>
        {isOpen('tray') ? (
          <div className="tt-children">
            {/* An empty tray is the finish line, so it says so rather than rendering nothing. */}
            {shownTray.length === 0 ? (
              <div className="tt-empty">{q ? t('tree.noMatches') : t('tree.allPlaced')}</div>
            ) : null}
            {shownTray.map((r) => renderTrayNode(r))}
          </div>
        ) : null}
      </div>
    </aside>
  );
}

TwinTree.propTypes = {
  sites: PropTypes.array, nodes: PropTypes.array, nodesById: PropTypes.object, nowSec: PropTypes.number,
  plansBySite: PropTypes.object, camsByNode: PropTypes.object, placedKeys: PropTypes.instanceOf(Set),
  expanded: PropTypes.instanceOf(Set), onToggle: PropTypes.func, query: PropTypes.string, onQuery: PropTypes.func,
  placing: PropTypes.object,
  onAddSite: PropTypes.func, onOpenSite: PropTypes.func, onOpenArea: PropTypes.func, onOpenCamera: PropTypes.func,
  onEditSite: PropTypes.func, onSelectNode: PropTypes.func, onPlayCamera: PropTypes.func,
  onPlaceCamera: PropTypes.func, onWaiveLocation: PropTypes.func,
  dropTarget: PropTypes.string, onDropTarget: PropTypes.func,
};
