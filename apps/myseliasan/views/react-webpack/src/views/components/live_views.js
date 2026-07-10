import { useEffect, useState } from 'react';
import { Ico, useT } from '@shared';
import { NodeCameraTile } from './node_manager';
import { getLiveViews, removeLiveView, subscribeLiveViews } from '../lib/live_views_store';
import { api } from '../lib/helpers';

// LiveViewsPage is myseliasan's own cross-node camera wall — the fleet analogue of a
// node's Live Views page. Each tile is a relay-backed live view of a camera on some node
// (added from that camera's Live View tab). Persisted client-side via the live-views store.
export function LiveViewsPage() {
  const t = useT();
  const [tiles, setTiles] = useState(getLiveViews);
  const [iceServers, setIceServers] = useState([]);

  useEffect(() => subscribeLiveViews(setTiles), []);
  useEffect(() => {
    api('/api/node-stream/config', { noRedirect: true })
      .then((r) => { if (r.ok && Array.isArray(r.body?.iceServers)) setIceServers(r.body.iceServers); })
      .catch(() => {});
  }, []);

  return (
    <section className="workspace">
      <div className="camera-tab-header">
        <div>
          <h2 className="section-title">{t('nav.liveViews')}</h2>
          <p className="section-subtitle">{t('lv.subtitle')}</p>
        </div>
      </div>

      {tiles.length === 0 ? (
        <div className="page-placeholder">
          <h2>{t('lv.emptyTitle')}</h2>
          <p>{t('lv.emptyHint')}</p>
        </div>
      ) : (
        <div className="live-views-grid">
          {tiles.map((tile) => (
            <figure key={`${tile.nodeId}::${tile.cameraId}`} className="live-views-tile">
              <div className="camera-live-view">
                <NodeCameraTile nodeId={tile.nodeId} cam={{ id: tile.cameraId, name: tile.name }} iceServers={iceServers} />
              </div>
              <figcaption className="live-views-cap">
                <span className="live-views-meta">
                  <span className="live-views-name">{tile.name || t('nm.cameraN', { id: tile.cameraId })}</span>
                  {tile.nodeName ? <span className="live-views-node">{tile.nodeName}</span> : null}
                </span>
                <button
                  type="button"
                  className="live-views-remove"
                  onClick={() => removeLiveView(tile.nodeId, tile.cameraId)}
                  title={t('cam.removeFromLiveViews')}
                  aria-label={t('cam.removeFromLiveViews')}
                >
                  <Ico n="trash" sz={14} />
                </button>
              </figcaption>
            </figure>
          ))}
        </div>
      )}
    </section>
  );
}
