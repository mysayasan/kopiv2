import { useCallback, useEffect, useRef, useState } from 'react';
import { useT } from '@shared';
import { api, apiBase } from '../lib/helpers';
import { SavedCameraRow } from './nodecam/cameras';
import { setNodeProxyBase } from './nodecam/lib/helpers';
import { addLiveView } from '../lib/live_views_store';

// NodeSettingsTab renders the node app's FULL per-camera settings (mymatasan SavedCameraRow:
// Details, Access, Stream, Recording, ONVIF capabilities/date-time/network, Maintenance
// [reboot + soft/hard factory reset], Users, and the remove-camera danger zone). The
// component self-fetches its ONVIF sections; those + the handlers below all tunnel to the
// node (proxy + CSRF). Sections appear only for capabilities the camera actually reports.
export function NodeSettingsTab({ node, camera, onToast }) {
  const t = useT();
  const nodeId = node.nodeId;
  setNodeProxyBase(`${apiBase()}/api/nodes/${encodeURIComponent(nodeId)}/proxy`);

  const [busy, setBusy] = useState(false);
  const [detailDraft, setDetailDraft] = useState({ name: '', description: '' });
  const [credentials, setCredentials] = useState({ username: '', password: '' });
  const [streamOptions, setStreamOptions] = useState(undefined);
  const [configs, setConfigs] = useState([]);

  const onToastRef = useRef(onToast);
  useEffect(() => { onToastRef.current = onToast; }, [onToast]);
  const toast = useCallback((m) => { if (onToastRef.current) onToastRef.current(m); }, []);

  const px = useCallback(
    (path, opts = {}) => api(`/api/nodes/${encodeURIComponent(nodeId)}/proxy${path}`, { noRedirect: true, ...opts }).catch(() => ({ ok: false })),
    [nodeId],
  );

  const loadConfigs = useCallback(async () => {
    const r = await px('/api/recording/config');
    setConfigs(r.ok ? (Array.isArray(r.body) ? r.body : (r.body?.items || [])) : []);
  }, [px]);
  useEffect(() => { loadConfigs(); }, [loadConfigs]);

  // Reset editable drafts whenever the selected camera changes.
  useEffect(() => {
    setDetailDraft({ name: camera?.name || '', description: camera?.description || '' });
    setCredentials({ username: camera?.username || '', password: '' });
    setStreamOptions(undefined);
  }, [camera?.id]); // eslint-disable-line react-hooks/exhaustive-deps

  async function onSaveDetails(device) {
    setBusy(true);
    const r = await px(`/api/cameras/${device.id}`, { method: 'PUT', body: JSON.stringify({ name: detailDraft.name, description: detailDraft.description }) });
    setBusy(false);
    toast(r.ok ? t('cam.detailsSaved') : t('cam.saveFailed'));
  }
  function onDiscardDetails() { setDetailDraft({ name: camera?.name || '', description: camera?.description || '' }); }

  async function onSaveCredentials(device) {
    setBusy(true);
    const r = await px(`/api/cameras/${device.id}/credentials`, { method: 'POST', body: JSON.stringify({ username: credentials.username, password: credentials.password }) });
    setBusy(false);
    if (r.ok) { toast(t('app.camCredsSaved')); setCredentials((c) => ({ ...c, password: '' })); }
    else toast(t('cam.saveFailed'));
  }

  async function onResolve(device) {
    setBusy(true);
    const r = await px(`/api/cameras/${device.id}/stream-options`, { method: 'POST', body: JSON.stringify({ username: credentials.username, password: credentials.password }) });
    setBusy(false);
    if (r.ok) setStreamOptions(r.body); else toast(t('cam.saveFailed'));
  }

  async function onTest(device, option) {
    setBusy(true);
    const r = await px(`/api/cameras/${device.id}/rtsp-test`, { method: 'POST', ...(option?.rtspUrl ? { body: JSON.stringify({ rtspUrl: option.rtspUrl }) } : {}) });
    setBusy(false);
    toast(r.ok ? t('cam.rtspTestOk') : t('cam.rtspTestFail'));
  }

  async function onSaveRecordingConfig(cfg) {
    setBusy(true);
    const r = await px('/api/recording/config', { method: 'PUT', body: JSON.stringify(cfg) });
    setBusy(false);
    if (r.ok) {
      const saved = r.body?.config || r.body;
      setConfigs((cur) => {
        const next = cur.filter((c) => Number(c.cameraId) !== Number(cfg.cameraId));
        return saved ? [...next, saved] : next;
      });
      toast(t('app.recordingConfigSaved'));
    } else toast(t('cam.saveFailed'));
  }

  async function onRemove(id) {
    setBusy(true);
    const r = await px(`/api/cameras/${id}`, { method: 'DELETE' });
    setBusy(false);
    toast(r.ok ? t('app.cameraRemoved') : t('cam.saveFailed'));
  }

  function onAdd(device) {
    addLiveView({ nodeId, cameraId: device.id, name: device.name || device.model || device.host || t('nm.cameraN', { id: device.id }), nodeName: node.name || nodeId });
    toast(t('cam.addToLiveViews'));
  }

  if (!camera) return null;
  return (
    <section className="camera-settings-panel">
      <div className="toolbar">
        <div>
          <h2 className="section-title">{t('cam.settingsTitle')}</h2>
          <p className="section-subtitle">{t('cam.settingsSub')}</p>
        </div>
      </div>
      <SavedCameraRow
        device={camera}
        onMessage={toast}
        busy={busy}
        detailDraft={detailDraft}
        credentials={credentials}
        streamOptions={streamOptions}
        onDetailDraft={(id, draft) => setDetailDraft(draft)}
        onSaveDetails={onSaveDetails}
        onDiscardDetails={onDiscardDetails}
        onCredential={(id, cred) => setCredentials(cred)}
        onSaveCredentials={onSaveCredentials}
        onResolve={onResolve}
        onTest={onTest}
        onPreview={() => {}}
        onAdd={onAdd}
        onRemove={onRemove}
        recordingConfigs={configs}
        onSaveRecordingConfig={onSaveRecordingConfig}
        authHeader={undefined}
        canManage
      />
    </section>
  );
}
