import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { useT } from '@shared';
import { apiBase, api } from '../lib/helpers';
import { NodeFeatureBanner } from './node_embed';
import { CameraRecordingsPanel } from './nodecam/recording';
import { setNodeProxyBase } from './nodecam/lib/helpers';

// NodeRecordingsTab hosts the real mymatasan recorded-footage panel against a managed
// node. The panel self-loads its segment list and plays clips by downloading each as a
// full blob (resp.blob() → object URL) rather than range-streaming — so it works over the
// buffering commander proxy without a media relay. This wrapper points the panel's fetches
// at the proxy and supplies the delete/purge/ack handlers over the CSRF-safe api().
export function NodeRecordingsTab({ node, camera, onToast }) {
  const t = useT();
  const nodeId = node.nodeId;
  setNodeProxyBase(`${apiBase()}/api/nodes/${encodeURIComponent(nodeId)}/proxy`);

  const [alerts, setAlerts] = useState([]);
  const [unavailable, setUnavailable] = useState(false);

  const onToastRef = useRef(onToast);
  useEffect(() => { onToastRef.current = onToast; }, [onToast]);
  const toast = useCallback((m) => { if (onToastRef.current) onToastRef.current(m); }, []);

  const px = useCallback(
    (path, opts = {}) => api(`/api/nodes/${encodeURIComponent(nodeId)}/proxy${path}`, { noRedirect: true, ...opts }).catch(() => ({ ok: false })),
    [nodeId],
  );

  // Alerts let the panel link a clip to its triggering detection; best-effort.
  const loadAlerts = useCallback(async () => {
    const r = await px('/api/vision/alerts?status=detections&limit=100&offset=0');
    setAlerts(r.ok ? (Array.isArray(r.body) ? r.body : (r.body?.items || [])) : []);
  }, [px]);
  useEffect(() => { loadAlerts(); }, [loadAlerts]);

  // Probe the recording API so an older node (that predates it) shows a clear notice
  // instead of an empty panel.
  useEffect(() => {
    px('/api/recording/config').then((r) => { if (r.status === 404) setUnavailable(true); });
  }, [px]);

  const unacknowledgedAlertIds = useMemo(
    () => new Set(alerts.filter((a) => a && !a.isAcknowledged).map((a) => Number(a.id))),
    [alerts],
  );

  const onDeleteSegment = useCallback(async (id) => {
    const r = await px(`/api/recording/segments/${id}`, { method: 'DELETE' });
    if (!r.ok) toast(t('rec.deleteClipFailed'));
  }, [px, toast, t]);

  const onPurgeExpired = useCallback(async () => {
    const r = await px('/api/recording/segments/purge', { method: 'POST' });
    if (r.ok) { const n = Number(r.body?.deleted) || 0; toast(n > 0 ? t('rec.purgedClips', { n }) : t('rec.noExpiredClips')); }
  }, [px, toast, t]);

  const onAcknowledgeAlert = useCallback(async (id) => {
    setAlerts((cur) => cur.map((a) => (Number(a.id) === Number(id) ? { ...a, isAcknowledged: true } : a)));
    await px(`/api/vision/alerts/${id}/ack`, { method: 'POST' });
    loadAlerts();
  }, [px, loadAlerts]);

  if (!camera) return null;
  if (unavailable) return <NodeFeatureBanner version={node.version} />;
  return (
    <CameraRecordingsPanel
      camera={camera}
      canManage
      busy={false}
      authHeader={undefined}
      onDeleteSegment={onDeleteSegment}
      onPurgeExpired={onPurgeExpired}
      onReload={loadAlerts}
      unacknowledgedAlertIds={unacknowledgedAlertIds}
      onAcknowledgeAlert={onAcknowledgeAlert}
      alerts={alerts}
    />
  );
}
