import { useEffect, useMemo, useState } from 'react';
import PropTypes from 'prop-types';
import { useT, Ico } from '@shared';
import { PLAN_OBJECTS, PLAN_TYPES } from './plan_objects';

// The outliner — a list of everything on the plan.
//
// Until now the only way to find something on a plan was to look at the plan. The inspector's
// resting state was a TALLY ("Walls 42, Doors 3"): a count, not a list. Nothing could be found,
// hidden, locked, or stepped through, and a wall buried under a car park was unreachable without
// moving the car park.
//
// Rows are grouped by the registry's `collection`, so a type added in P5 appears here with no edit.
//
// VISIBILITY AND LOCK ARE VIEW STATE, NOT MODEL STATE. They are one operator's working preference
// while they draw — hiding the parking rows to get at the walls underneath is not a fact about the
// building, and writing it into the shared model would push one person's working view onto everyone
// who opens that plan afterwards, including the PDF report. So they live in localStorage, keyed by
// floor, and a browser that blocks storage simply shows everything visible and unlocked.

const STORE_PREFIX = 'myseliasan_outliner_';

// The order collections appear in. Devices last: they are what the structure is FOR, so they read
// as the payload rather than the container.
const COLLECTION_ORDER = ['structure', 'outdoor', 'devices'];
const COLLECTION_LABEL = {
  structure: 'ol.structure',
  outdoor: 'ol.outdoor',
  devices: 'ol.devices',
};

export function loadOutlinerState(floorId) {
  const empty = { hidden: [], locked: [] };
  if (!floorId) return empty;
  try {
    const raw = localStorage.getItem(STORE_PREFIX + floorId);
    if (!raw) return empty;
    const j = JSON.parse(raw);
    return { hidden: Array.isArray(j.hidden) ? j.hidden : [], locked: Array.isArray(j.locked) ? j.locked : [] };
  } catch (_) { return empty; } // a private window, or site data blocked: show everything
}
export function saveOutlinerState(floorId, hidden, locked) {
  if (!floorId) return;
  try {
    localStorage.setItem(STORE_PREFIX + floorId, JSON.stringify({ hidden: [...hidden], locked: [...locked] }));
  } catch (_) { /* nothing to do; the view simply will not be remembered */ }
}

// rowLabel names one object. The registry gives the type's name; the index disambiguates within it,
// and a short secondary reads off whichever field is most identifying.
function secondary(typeName, o, scale, t) {
  const spec = PLAN_OBJECTS[typeName];
  if (!spec || !o) return '';
  const f = spec.fields[0];
  if (!f) return '';
  const raw = o[f.key] !== undefined && o[f.key] !== null ? o[f.key] : f.default;
  if (raw === undefined || raw === null) return '';
  if (f.type === 'bool') return raw ? t(f.label) : '';
  if (f.type === 'enum') return String(raw).toUpperCase();
  if (f.type === 'px') return scale > 0 ? `${(raw * scale).toFixed(2)} m` : `${Math.round(raw)} px`;
  if (f.type === 'length') return `${Number(raw).toFixed(2)} m`;
  return String(Math.round(raw));
}

export function PlanOutliner({
  lists, placements, nodesById, selection, scale, siteKind,
  hidden, locked, onToggleHidden, onToggleLocked, onSelect, onToggleCollection,
}) {
  const t = useT();

  // Build the tree from the registry, so a new type needs no edit here.
  const groups = useMemo(() => {
    const byCollection = {};
    PLAN_TYPES.forEach((name) => {
      const spec = PLAN_OBJECTS[name];
      if (!spec.kinds.includes(siteKind)) return;
      const list = lists[spec.ref] || [];
      if (!list.length) return;
      (byCollection[spec.collection] = byCollection[spec.collection] || []).push({ name, spec, list });
    });
    // Cameras and appliances are not registry types - they are the parent's records, not plan
    // geometry - but they are the things an operator is actually looking for, so they get a group.
    const devices = placements || [];
    if (devices.length) byCollection.devices = [{ name: 'device', spec: null, list: devices }];
    return byCollection;
  }, [lists, placements, siteKind]);

  const keyOf = (spec, i, o) => (spec ? `${spec.sel}:${i}` : `cam:${o.id}`);

  const collectionKeys = (entries) => {
    const out = [];
    entries.forEach(({ spec, list }) => list.forEach((o, i) => out.push(keyOf(spec, i, o))));
    return out;
  };

  const present = COLLECTION_ORDER.filter((c) => groups[c] && groups[c].length);
  if (!present.length) {
    return <div className="ol-empty">{t('ol.empty')}</div>;
  }

  return (
    <div className="ol-tree" role="tree" aria-label={t('ol.title')}>
      {present.map((collection) => {
        const entries = groups[collection];
        const keys = collectionKeys(entries);
        const allHidden = keys.length > 0 && keys.every((k) => hidden.has(k));
        const allLocked = keys.length > 0 && keys.every((k) => locked.has(k));
        const count = keys.length;
        return (
          <div key={collection} className="ol-group">
            <div className="ol-group-head">
              <span className="ol-group-name">{t(COLLECTION_LABEL[collection])}</span>
              <span className="ol-count">{count}</span>
              <button
                type="button"
                className={`ol-eye${allHidden ? ' off' : ''}`}
                title={allHidden ? t('ol.show') : t('ol.hide')}
                aria-label={allHidden ? t('ol.show') : t('ol.hide')}
                onClick={() => onToggleCollection('hidden', keys, !allHidden)}
              ><Ico n="eye" sz={12} /></button>
              <button
                type="button"
                className={`ol-lock${allLocked ? ' on' : ''}`}
                title={allLocked ? t('ol.unlock') : t('ol.lock')}
                aria-label={allLocked ? t('ol.unlock') : t('ol.lock')}
                onClick={() => onToggleCollection('locked', keys, !allLocked)}
              ><Ico n="lock" sz={12} /></button>
            </div>
            {entries.map(({ name, spec, list }) => (
              <div key={name} className="ol-type">
                <div className="ol-type-name">
                  {spec ? t(spec.label) : t('ol.devices')} <em>{list.length}</em>
                </div>
                {list.map((o, i) => {
                  const k = keyOf(spec, i, o);
                  const isHidden = hidden.has(k);
                  const isLocked = locked.has(k);
                  const label = spec
                    ? `${t(spec.label)} ${i + 1}`
                    : (o.lastKnownName || (o.cameraId ? t('nodes.cameraN', { id: o.cameraId }) : ((nodesById && nodesById[o.nodeId] && nodesById[o.nodeId].name) || o.nodeId)));
                  return (
                    <div
                      key={k}
                      className={`ol-row${selection.has(k) ? ' sel' : ''}${isHidden ? ' hidden' : ''}${isLocked ? ' locked' : ''}`}
                      role="treeitem"
                      aria-selected={selection.has(k)}
                    >
                      <button type="button" className="ol-pick" onClick={(e) => onSelect(k, e.shiftKey || e.ctrlKey || e.metaKey)} title={label}>
                        <Ico n={spec ? (spec.tool ? spec.tool.id : 'square') : (o.cameraId ? 'video' : 'board')} sz={11} />
                        <span className="ol-name">{label}</span>
                        {spec ? <span className="ol-sub">{secondary(name, o, scale, t)}</span> : null}
                      </button>
                      <button
                        type="button"
                        className={`ol-eye${isHidden ? ' off' : ''}`}
                        title={isHidden ? t('ol.show') : t('ol.hide')}
                        aria-label={isHidden ? t('ol.show') : t('ol.hide')}
                        onClick={() => onToggleHidden(k)}
                      ><Ico n="eye" sz={11} /></button>
                      <button
                        type="button"
                        className={`ol-lock${isLocked ? ' on' : ''}`}
                        title={isLocked ? t('ol.unlock') : t('ol.lock')}
                        aria-label={isLocked ? t('ol.unlock') : t('ol.lock')}
                        onClick={() => onToggleLocked(k)}
                      ><Ico n="lock" sz={11} /></button>
                    </div>
                  );
                })}
              </div>
            ))}
          </div>
        );
      })}
    </div>
  );
}

PlanOutliner.propTypes = {
  lists: PropTypes.object,
  placements: PropTypes.array,
  nodesById: PropTypes.object,
  selection: PropTypes.object,
  scale: PropTypes.number,
  siteKind: PropTypes.string,
  hidden: PropTypes.object,
  locked: PropTypes.object,
  onToggleHidden: PropTypes.func,
  onToggleLocked: PropTypes.func,
  onToggleCollection: PropTypes.func,
  onSelect: PropTypes.func,
};
