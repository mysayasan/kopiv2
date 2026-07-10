import { useEffect, useState } from 'react';
import { Ico, useT } from '@shared';
import { FormBusyOverlay, IconDropdown } from './ui';
import { NodeManager } from './node_manager';
import { api, formatTimestamp } from '../lib/helpers';

// NODE_ICONS is the curated set of pre-installed glyphs an operator can assign to a
// node so it's recognisable at a glance in the nav (site type / fixture). Every name
// must exist in icons.js; DEFAULT_NODE_ICON is used when none is chosen.
export const NODE_ICONS = [
  'monitor', 'camera', 'video', 'shield', 'cpu', 'server',
  'home', 'building', 'box', 'map-pin', 'globe', 'truck',
  'door', 'wifi', 'bell', 'folder',
];
export const DEFAULT_NODE_ICON = 'monitor';

// NodesTab is the control plane's mymatasan management surface: fleet-key
// management, LAN discovery, claim-code adoption (incl. manual address for other
// subnets), and the adopted-nodes table with release. Uses the shared settings
// panel / metric-card visual language.
export function NodesTab({ onToast, nodes, reloadNodes, managingNodeId, managingCameraId, onManage, onClearFocus, onBack }) {
  const t = useT();
  const [fleetKey, setFleetKey] = useState(null);
  const [discovered, setDiscovered] = useState(null);
  const [busy, setBusy] = useState(false);
  const [scanning, setScanning] = useState(false);
  const [copied, setCopied] = useState(false);
  const [adoptForm, setAdoptForm] = useState({ ip: '', httpsPort: '', claimCode: '', name: '', description: '', icon: DEFAULT_NODE_ICON });

  const nodeList = Array.isArray(nodes) ? nodes : [];
  // The managed node is selected from the side-nav tree or the table; resolve it from
  // the shared list so the page mirrors whatever the nav picked.
  const managing = managingNodeId ? nodeList.find((n) => n.nodeId === managingNodeId) : null;

  function toast(text) { if (onToast) onToast(text); }

  async function loadFleetKey() {
    const r = await api('/api/nodes/fleet-key').catch(() => ({ ok: false }));
    if (r.ok) setFleetKey(r.body);
  }
  useEffect(() => { loadFleetKey(); /* eslint-disable-next-line */ }, []);

  async function generateFleetKey() {
    if (!window.confirm(t('node.confirmRotate'))) return;
    setBusy(true);
    const r = await api('/api/nodes/fleet-key', { method: 'POST' });
    setBusy(false);
    if (r.ok) { setFleetKey(r.body); toast(t('node.fleetGenerated')); }
    else toast(r.message || t('node.fleetGenFailed'));
  }

  async function copyKey() {
    const key = fleetKey?.fleetKey;
    if (!key) return;
    try {
      await navigator.clipboard.writeText(key);
      setCopied(true);
      toast(t('node.fleetCopied'));
      setTimeout(() => setCopied(false), 2000);
    } catch (_) {
      toast(t('node.copyManual'));
    }
  }

  async function scan() {
    if (!fleetKey?.set) { toast(t('node.genKeyFirst')); return; }
    setScanning(true);
    const r = await api('/api/nodes/scan', { method: 'POST', body: JSON.stringify({ timeoutMs: 4000 }) });
    setScanning(false);
    if (!r.ok) { toast(r.message || t('node.scanFailed')); return; }
    setDiscovered(Array.isArray(r.body) ? r.body : []);
  }

  function selectDiscovered(node) {
    // Pre-fill the name with the node's reported hostname so it's the default the
    // operator can keep or rename.
    setAdoptForm({ ip: node.ip || '', httpsPort: String(node.httpsPort || ''), claimCode: '', name: node.name || '', description: '', icon: DEFAULT_NODE_ICON });
  }

  async function adopt() {
    const ip = adoptForm.ip.trim();
    const port = parseInt(adoptForm.httpsPort, 10);
    const code = adoptForm.claimCode.trim();
    if (!ip || !port) { toast(t('node.enterIpPort')); return; }
    if (!code) { toast(t('node.enterClaim')); return; }
    setBusy(true);
    const r = await api('/api/nodes/adopt', {
      method: 'POST',
      body: JSON.stringify({ ip, httpsPort: port, claimCode: code, name: adoptForm.name.trim(), description: adoptForm.description.trim(), icon: adoptForm.icon }),
    });
    setBusy(false);
    if (r.ok) { toast(t('node.nodeAdopted')); setAdoptForm({ ip: '', httpsPort: '', claimCode: '', name: '', description: '', icon: DEFAULT_NODE_ICON }); reloadNodes(); }
    else toast(r.message || t('node.adoptFailed'));
  }

  async function release(node) {
    if (!window.confirm(t('node.confirmRelease', { name: node.name || node.nodeId }))) return;
    setBusy(true);
    const r = await api(`/api/nodes/${encodeURIComponent(node.nodeId)}/release`, { method: 'POST' });
    setBusy(false);
    if (r.ok) { toast(t('node.nodeReleased')); if (managingNodeId === node.nodeId) onBack(); reloadNodes(); }
    else toast(r.message || t('node.releaseFailed'));
  }

  const keySet = !!fleetKey?.set;

  if (managing) {
    return (
      <NodeManager
        node={managing}
        onToast={toast}
        focusCameraId={managingCameraId}
        onClearFocus={onClearFocus}
        onBack={() => { onBack(); reloadNodes(); }}
      />
    );
  }

  return (
    <section className="workspace">
      <FormBusyOverlay busy={busy} />

      <section className="settings-panel span-two">
        <header>
          <h2><span className="btn-icon"><Ico n="key" /> {t('node.fleetKey')}</span></h2>
        </header>
        <p className="settings-hint">{t('node.fleetKeyHint')}</p>
        <div className="node-key-row">
          <div className="fleet-key-field">
            <code className="fleet-key-box">{keySet ? fleetKey.fleetKey : t('node.notSet')}</code>
            {keySet ? (
              <button
                type="button"
                className="fleet-key-copy"
                onClick={copyKey}
                title={t('node.copyFleetKey')}
                aria-label={t('node.copyFleetKey')}
              >
                <Ico n={copied ? 'check-ok' : 'copy'} sz={15} />
              </button>
            ) : null}
          </div>
          <button type="button" className="quiet" onClick={generateFleetKey} disabled={busy}>
            <span className="btn-icon"><Ico n="refresh" /> {t('node.generateRotate')}</span>
          </button>
        </div>
      </section>

      <section className="settings-panel span-two">
        <header>
          <h2><span className="btn-icon"><Ico n="search" /> {t('node.discover')}</span></h2>
          <div className="settings-header-actions">
            <button type="button" onClick={scan} disabled={scanning || !keySet}>
              <span className="btn-icon"><Ico n="wifi" /> {scanning ? t('node.scanning') : t('node.scanLan')}</span>
            </button>
          </div>
        </header>
        <p className="settings-hint">{t('node.discoverHint')}</p>
        {!keySet ? <p className="settings-hint danger-text">{t('node.setKeyFirst')}</p> : null}
        {discovered && discovered.length === 0 ? <p className="settings-hint">{t('node.noUnpaired')}</p> : null}
        {discovered && discovered.length > 0 ? (
          <table className="event-table">
            <thead><tr><th>{t('node.colName')}</th><th>{t('node.colNodeId')}</th><th>{t('node.colAddress')}</th><th>{t('node.colVersion')}</th><th></th></tr></thead>
            <tbody>
              {discovered.map((n) => (
                <tr key={n.nodeId}>
                  <td>{n.name || '—'}</td>
                  <td>{(n.nodeId || '').slice(0, 8)}</td>
                  <td>{n.ip}:{n.httpsPort}</td>
                  <td>{n.version || '—'}</td>
                  <td>
                    {n.adopted
                      ? <span className="status-pill online">{t('node.adopted')}</span>
                      : <button type="button" className="quiet" onClick={() => selectDiscovered(n)}>{t('node.select')}</button>}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        ) : null}
      </section>

      <section className="settings-panel span-two">
        <header><h2><span className="btn-icon"><Ico n="plus" /> {t('node.adoptTitle')}</span></h2></header>
        <p className="settings-hint">{t('node.adoptHint')}</p>
        <div className="settings-field-grid">
          <label>{t('node.nodeIp')}
            <input value={adoptForm.ip} onChange={(e) => setAdoptForm({ ...adoptForm, ip: e.target.value })} placeholder="192.168.1.40" disabled={busy} />
          </label>
          <label>{t('node.httpsPort')}
            <input value={adoptForm.httpsPort} onChange={(e) => setAdoptForm({ ...adoptForm, httpsPort: e.target.value })} placeholder="3000" disabled={busy} />
          </label>
          <label>{t('node.claimCode')}
            <input value={adoptForm.claimCode} onChange={(e) => setAdoptForm({ ...adoptForm, claimCode: e.target.value })} placeholder={t('node.claimPlaceholder')} disabled={busy} />
          </label>
          <label>{t('node.name')} <span className="settings-field-opt">{t('node.optional')}</span>
            <input value={adoptForm.name} onChange={(e) => setAdoptForm({ ...adoptForm, name: e.target.value })} placeholder={t('node.defaultsHostname')} disabled={busy} />
          </label>
          <label className="field-span-two">{t('node.description')} <span className="settings-field-opt">{t('node.optional')}</span>
            <textarea value={adoptForm.description} onChange={(e) => setAdoptForm({ ...adoptForm, description: e.target.value })} rows={2} placeholder={t('node.descPlaceholder')} disabled={busy} />
          </label>
          <label>{t('node.icon')}
            <IconDropdown
              value={adoptForm.icon}
              options={NODE_ICONS}
              onChange={(name) => setAdoptForm({ ...adoptForm, icon: name })}
              disabled={busy}
            />
          </label>
        </div>
        <div className="settings-actions">
          <button type="button" onClick={adopt} disabled={busy}>
            <span className="btn-icon"><Ico n="check-ok" /> {t('node.adopt')}</span>
          </button>
        </div>
      </section>

      <section className="settings-panel span-two">
        <header>
          <h2><span className="btn-icon"><Ico n="shield" /> {t('node.adoptedNodes')}</span></h2>
          <div className="settings-header-actions">
            <button type="button" className="quiet" onClick={reloadNodes} disabled={busy}>
              <span className="btn-icon"><Ico n="reload" /> {t('node.refresh')}</span>
            </button>
          </div>
        </header>
        {nodeList.length === 0 ? (
          <p className="settings-hint">{t('node.noAdopted')}</p>
        ) : (
          <table className="event-table">
            <thead><tr><th>{t('node.colName')}</th><th>{t('node.colAddress')}</th><th>{t('node.colStatus')}</th><th>{t('node.colCertExpires')}</th><th>{t('node.colAdopted')}</th><th></th></tr></thead>
            <tbody>
              {nodeList.map((n) => (
                <tr key={n.nodeId}>
                  <td><span className="node-name-cell"><Ico n={n.icon || DEFAULT_NODE_ICON} sz={15} /> {n.name || n.nodeId}</span></td>
                  <td>{n.baseUrl}</td>
                  <td><span className={`status-pill ${n.status === 'online' ? 'online' : 'offline'}`}>{n.status || 'online'}</span></td>
                  <td>{n.certExpiresAt ? formatTimestamp(n.certExpiresAt) : '—'}</td>
                  <td>{n.adoptedAt ? formatTimestamp(n.adoptedAt) : '—'}</td>
                  <td className="node-row-actions">
                    <button type="button" className="quiet" onClick={() => onManage(n.nodeId)}>
                      <span className="btn-icon"><Ico n="sliders" /> {t('node.manage')}</span>
                    </button>
                    <button type="button" className="quiet danger-text" onClick={() => release(n)} disabled={busy}>{t('node.release')}</button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </section>
    </section>
  );
}
