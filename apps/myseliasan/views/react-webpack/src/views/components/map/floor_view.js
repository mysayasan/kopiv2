import { useEffect, useMemo, useState } from 'react';
import PropTypes from 'prop-types';
import { useT, Ico } from '@shared';
import { api, apiBase } from '../../lib/helpers';
import { nodeTone } from '../../lib/fleet_status';

// FloorPlanView renders ONE floor of a building — its plan image, every camera's coverage wedge,
// and the camera markers — with NO chrome of its own (the workspace supplies the breadcrumb and
// floor selector). Each marker/wedge takes its owning node's status colour (a building's cameras
// can span several nodes). Clicking a camera selects it into the inspector rather than opening a
// window, so the plan stays the focus. This is the monitor-mode floor surface.

function markerPos(p, w, h) {
  return { left: `${Math.max(0, Math.min(100, (p.x / w) * 100))}%`, top: `${Math.max(0, Math.min(100, ((h - p.y) / h) * 100))}%` };
}
function fovRadius(w, h) { return Math.max(50, Math.min(w, h) * 0.16); }
function fovPath(p, w, h) {
  const cx = p.x; const cy = h - p.y; const r = fovRadius(w, h); const half = (p.fov || 0) / 2; const N = 24;
  let d = `M ${cx.toFixed(1)} ${cy.toFixed(1)}`;
  for (let i = 0; i <= N; i++) {
    const deg = (p.heading || 0) - half + (p.fov || 0) * (i / N);
    const rad = (deg * Math.PI) / 180;
    d += ` L ${(cx + r * Math.sin(rad)).toFixed(1)} ${(cy - r * Math.cos(rad)).toFixed(1)}`;
  }
  return `${d} Z`;
}

export function FloorPlanView({ site, plan, nodesById = {}, focusCameraId, selCameraKey, onSelectCamera }) {
  const t = useT();
  const floor = plan && plan.floor;
  const placements = useMemo(() => (plan && plan.placements) || [], [plan]);
  const w = (floor && floor.width) || 1024;
  const h = (floor && floor.height) || 768;

  // Resolve PTZ per (node,camera) across the floor's owning nodes, so opening live shows the ring.
  const [ptzByKey, setPtzByKey] = useState({});
  useEffect(() => {
    let live = true; setPtzByKey({});
    const nodeIds = Array.from(new Set(placements.filter((p) => p.cameraId).map((p) => p.nodeId)));
    if (!nodeIds.length) return undefined;
    Promise.all(nodeIds.map((nid) => api(`/api/nodes/${encodeURIComponent(nid)}/proxy/api/cameras?limit=200`, { noRedirect: true })
      .then((r) => ({ nid, list: r.ok ? (Array.isArray(r.body) ? r.body : (r.body?.items || [])) : [] })).catch(() => ({ nid, list: [] }))))
      .then((res) => { if (!live) return; const m = {}; res.forEach(({ nid, list }) => list.forEach((c) => { m[`${nid}::${c.id}`] = !!c.ptzSupported; })); setPtzByKey(m); });
    return () => { live = false; };
  }, [placements]);

  if (!floor) return <div className="mw-floor-empty">{t('map.noFloorsInBuilding')}</div>;

  return (
    <div className="mw-floorstage">
      <div className="floor-plan-wrap" style={{ aspectRatio: `${w} / ${h}` }}>
        <img className="floor-plan-img" src={`${apiBase()}/api/floors/${floor.id}/image`} alt={floor.name} draggable={false} />
        <svg className="floor-fov" viewBox={`0 0 ${w} ${h}`} preserveAspectRatio="none" aria-hidden="true">
          {placements.filter((p) => p.cameraId && (p.fov || 0) > 0).map((p) => {
            const c = nodeTone(nodesById[p.nodeId]).color;
            return <path key={`fov-${p.id}`} d={fovPath(p, w, h)} fill={c} fillOpacity="0.16" stroke={c} strokeOpacity="0.45" strokeWidth="1" vectorEffect="non-scaling-stroke" />;
          })}
        </svg>
        {placements.map((p) => {
          const isCam = !!p.cameraId;
          const node = nodesById[p.nodeId];
          const label = p.lastKnownName || (isCam ? t('nodes.cameraN', { id: p.cameraId }) : (node ? node.name || p.nodeId : p.nodeId));
          const tone = nodeTone(node);
          const focused = (isCam && focusCameraId && String(p.cameraId) === String(focusCameraId)) || (isCam && selCameraKey === `camera:${p.nodeId}:${p.cameraId}`);
          return (
            <button
              key={p.id}
              type="button"
              className={`floor-marker${isCam ? ' cam' : ' node'}${focused ? ' focus' : ''}`}
              style={{ ...markerPos(p, w, h), borderColor: focused ? undefined : tone.ring }}
              onClick={(e) => { if (isCam && onSelectCamera) onSelectCamera({ nodeId: p.nodeId, cameraId: p.cameraId, name: label, buildingId: site.id, buildingName: site.name, floorId: floor.id, floorName: floor.name, ptzSupported: ptzByKey[`${p.nodeId}::${p.cameraId}`] ?? !!p.ptzSupported }, e.clientX, e.clientY); }}
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
  );
}

FloorPlanView.propTypes = {
  site: PropTypes.object, plan: PropTypes.object, nodesById: PropTypes.object,
  focusCameraId: PropTypes.any, selCameraKey: PropTypes.string, onSelectCamera: PropTypes.func,
};
