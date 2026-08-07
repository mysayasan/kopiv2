import { useMemo, useState } from 'react';
import PropTypes from 'prop-types';
import { useT, Ico } from '@shared';
import { nodeToneKey } from '../../lib/fleet_status';

// AssetBrowser is the left pane: search + fleet status counts + a Building → Floor → Camera tree,
// plus a group for building-less appliances. It is the primary way to jump anywhere in the fleet
// without hunting on the map. Camera/floor detail comes from the floorplans the workspace has
// loaded for an expanded building; a building's own status is the worst of its owning nodes.

const STATUS_ORDER = ['critical', 'warning', 'online', 'idle'];
const KIND_ICON = { camera: 'video', iot: 'cpu', door: 'door' };

export function AssetBrowser({
  sites = [], nodes = [], nodesById = {}, floorplansByBuilding = {},
  expanded, selectedKey, onToggleExpand, onOpenBuilding, onOpenFloor, onSelectCamera, onSelectNode,
}) {
  const t = useT();
  const [query, setQuery] = useState('');
  const [filter, setFilter] = useState(null); // status filter or null
  const nowSec = Math.floor(Date.now() / 1000);

  const buildingTone = (row) => {
    let worst = 'idle';
    (row.nodeIds || []).forEach((nid) => { const k = nodeToneKey(nodesById[nid], nowSec); if (STATUS_ORDER.indexOf(k) < STATUS_ORDER.indexOf(worst)) worst = k; });
    return worst;
  };

  // Fleet health counts by appliance status — the reliable, always-available signal.
  const counts = useMemo(() => {
    const c = { online: 0, warning: 0, critical: 0 };
    nodes.forEach((n) => { const k = nodeToneKey(n, nowSec); if (k in c) c[k] += 1; });
    return c;
  }, [nodes, nowSec]);

  const q = query.trim().toLowerCase();
  const matchName = (s) => !q || (s || '').toLowerCase().includes(q);

  const shownBuildings = sites.filter((row) => row.site
    && (!filter || buildingTone(row) === filter)
    && (matchName(row.site.name) || (floorplansByBuilding[row.site.id] || []).some((fp) => (fp.placements || []).some((p) => matchName(p.lastKnownName)))));
  const unassigned = nodes.filter((n) => !n.siteId && (!filter || nodeToneKey(n, nowSec) === filter) && matchName(n.name || n.nodeId));

  return (
    <aside className="mw-rail">
      <div className="mw-search">
        <Ico n="search" sz={14} />
        <input value={query} onChange={(e) => setQuery(e.target.value)} placeholder={t('mw.searchPlaceholder')} aria-label={t('mw.searchPlaceholder')} />
        {query ? <button type="button" className="mw-search-x" onClick={() => setQuery('')} aria-label={t('mw.clear')}><Ico n="x" sz={12} /></button> : null}
      </div>
      <div className="mw-statusbar">
        {['online', 'warning', 'critical'].map((k) => (
          <button key={k} type="button" className={`mw-schip${filter === k ? ' active' : ''}`} onClick={() => setFilter(filter === k ? null : k)}>
            <span className="mw-schip-n"><span className={`mw-dot ${k}`} />{counts[k]}</span>
            <span className="mw-schip-k">{t(`map.legend.${k}`)}</span>
          </button>
        ))}
      </div>

      <div className="mw-treescroll">
        <div className="mw-grouphdr"><Ico n="building" sz={12} /> {t('map.buildings')} <span className="mw-cnt">{shownBuildings.length}</span></div>
        {shownBuildings.length === 0 ? <div className="mw-tree-empty">{t('mw.noBuildings')}</div> : null}
        {shownBuildings.map((row) => {
          const s = row.site;
          const isOpen = expanded.has(s.id);
          const plans = floorplansByBuilding[s.id] || [];
          const tone = buildingTone(row);
          return (
            <div key={s.id}>
              <div className={`mw-row${selectedKey === `building:${s.id}` ? ' sel' : ''}`}>
                <button type="button" className="mw-tw" onClick={() => onToggleExpand(s.id)} aria-label={t('mw.expand')}><Ico n={isOpen ? 'chev-down' : 'chev-right'} sz={12} /></button>
                <span className={`mw-dot ${tone}`} />
                <span className="mw-glyph">{s.icon || '🏢'}</span>
                <button type="button" className="mw-nm-btn" onClick={() => onOpenBuilding(s)}>{s.name}</button>
                <span className="mw-meta">{row.cameras}</span>
              </div>
              {isOpen ? (
                <div className="mw-children">
                  {plans.length === 0 ? <div className="mw-tree-empty sm">{t('mw.noFloors')}</div> : null}
                  {plans.map((fp) => {
                    const floorSel = selectedKey === `floor:${s.id}:${fp.floor.id}`;
                    return (
                      <div key={fp.floor.id}>
                        <div className={`mw-row floor${floorSel ? ' sel' : ''}`}>
                          <span className="mw-tw"><Ico n="grid2" sz={11} /></span>
                          <button type="button" className="mw-nm-btn" onClick={() => onOpenFloor(s.id, fp.floor.id)}>{fp.floor.name}</button>
                          <span className="mw-meta">{(fp.placements || []).filter((p) => p.cameraId).length}</span>
                        </div>
                        <div className="mw-children">
                          {(fp.placements || []).filter((p) => p.cameraId).map((p) => (
                            <div key={p.id} className={`mw-row cam${selectedKey === `camera:${p.nodeId}:${p.cameraId}` ? ' sel' : ''}`}>
                              <span className="mw-tw"><Ico n="video" sz={11} /></span>
                              <button type="button" className="mw-nm-btn dim" onClick={() => onSelectCamera(s.id, fp.floor.id, p)}>{p.lastKnownName || t('nodes.cameraN', { id: p.cameraId })}</button>
                            </div>
                          ))}
                        </div>
                      </div>
                    );
                  })}
                </div>
              ) : null}
            </div>
          );
        })}

        <div className="mw-grouphdr"><Ico n="cpu" sz={12} /> {t('mw.unassignedAppliances')} <span className="mw-cnt">{unassigned.length}</span></div>
        {unassigned.length === 0 ? <div className="mw-tree-empty">{t('mw.allAssigned')}</div> : null}
        {unassigned.map((n) => (
          <div key={n.nodeId} className={`mw-row${selectedKey === `node:${n.nodeId}` ? ' sel' : ''}`}>
            <span className="mw-tw"><Ico n={KIND_ICON[n.kind] || 'cpu'} sz={11} /></span>
            <span className={`mw-dot ${nodeToneKey(n, nowSec)}`} />
            <button type="button" className="mw-nm-btn" onClick={() => onSelectNode(n)}>{n.name || n.nodeId}</button>
          </div>
        ))}
      </div>
    </aside>
  );
}

AssetBrowser.propTypes = {
  sites: PropTypes.array, nodes: PropTypes.array, nodesById: PropTypes.object, floorplansByBuilding: PropTypes.object,
  expanded: PropTypes.instanceOf(Set), selectedKey: PropTypes.string,
  onToggleExpand: PropTypes.func, onOpenBuilding: PropTypes.func, onOpenFloor: PropTypes.func,
  onSelectCamera: PropTypes.func, onSelectNode: PropTypes.func,
};
