import { useEffect, useRef, useState } from 'react';
import { Ico, useT } from '@shared';
import { FormBusyOverlay, IconDropdown } from './ui';
import { NodeManager } from './node_manager';
import { NodeSettingsDialog } from './node_settings_dialog';
import { api, formatTimestamp } from '../lib/helpers';

// NodeWipeCountdown mirrors mymatasan's SecureWipeCountdown: a confirmation modal that
// AUTO-PROCEEDS when the countdown hits zero unless cancelled. Cancel is focused so the
// safe action is one keypress away. Used to remotely factory-reset an adopted node.
function NodeWipeCountdown({ node, seconds = 10, onCancel, onProceed }) {
  const t = useT();
  const [remaining, setRemaining] = useState(seconds);
  const proceedRef = useRef(onProceed);
  proceedRef.current = onProceed;
  useEffect(() => {
    const started = Date.now();
    const id = setInterval(() => {
      const left = Math.max(0, seconds - Math.floor((Date.now() - started) / 1000));
      setRemaining(left);
      if (left <= 0) { clearInterval(id); if (proceedRef.current) proceedRef.current(); }
    }, 250);
    return () => clearInterval(id);
  }, [seconds]);
  return (
    <div className="modal-backdrop">
      <div className="modal-card danger-modal" role="alertdialog" aria-modal="true">
        <h2><span className="btn-icon"><Ico n="warning" /> {t('nwipe.title', { name: node.name || node.nodeId })}</span></h2>
        <p>{t('nwipe.warning')}</p>
        <p className="wipe-countdown">{t('nwipe.countdown', { n: remaining })}</p>
        <div className="modal-actions">
          <button type="button" className="quiet" autoFocus onClick={onCancel}>
            <span className="btn-icon"><Ico n="x" /> {t('nwipe.cancel')}</span>
          </button>
          <button type="button" className="danger-solid" onClick={() => proceedRef.current && proceedRef.current()}>
            <span className="btn-icon"><Ico n="warning" /> {t('nwipe.wipeNow')}</span>
          </button>
        </div>
      </div>
    </div>
  );
}

// AdoptDialog is the pop-up adoption form, opened from a discovered node (pre-filled with
// its address + hostname) or the "Adopt manually" button (blank, for a node on another
// subnet). Address + claim code are required; name/description/icon are operator-facing and
// can be edited later from the node's Manage → Details tab. Closes itself on success.
function AdoptDialog({ initial, busy, onClose, onAdopt }) {
  const t = useT();
  const [form, setForm] = useState(initial);
  const set = (patch) => setForm((f) => ({ ...f, ...patch }));
  async function submit(e) {
    e.preventDefault();
    const ok = await onAdopt(form);
    if (ok) onClose();
  }
  return (
    <div className="modal-backdrop" onClick={onClose}>
      <form className="modal-card node-adopt-dialog" role="dialog" aria-modal="true" onClick={(e) => e.stopPropagation()} onSubmit={submit}>
        <header className="node-settings-head">
          <h2><span className="btn-icon"><Ico n="plus" /> {t('node.adoptTitle')}</span></h2>
          <button type="button" className="icon-button" onClick={onClose} aria-label={t('nset.close')}><Ico n="x" sz={16} /></button>
        </header>
        <div className="node-adopt-body">
          <p className="settings-hint">{t('node.adoptHint')}</p>
          <div className="settings-field-grid">
            <label>{t('node.nodeIp')}<input value={form.ip} onChange={(e) => set({ ip: e.target.value })} placeholder="192.168.1.40" disabled={busy} autoFocus /></label>
            <label>{t('node.httpsPort')}<input value={form.httpsPort} onChange={(e) => set({ httpsPort: e.target.value })} placeholder="3000" disabled={busy} /></label>
            <label className="field-span-two">{t('node.claimCode')}<input value={form.claimCode} onChange={(e) => set({ claimCode: e.target.value })} placeholder={t('node.claimPlaceholder')} disabled={busy} /></label>
            <label>{t('node.name')} <span className="settings-field-opt">{t('node.optional')}</span><input value={form.name} onChange={(e) => set({ name: e.target.value })} placeholder={t('node.defaultsHostname')} disabled={busy} /></label>
            <label>{t('node.icon')}<IconDropdown value={form.icon} options={NODE_ICONS} onChange={(name) => set({ icon: name })} disabled={busy} /></label>
            <label className="field-span-two">{t('node.description')} <span className="settings-field-opt">{t('node.optional')}</span><textarea value={form.description} onChange={(e) => set({ description: e.target.value })} rows={2} placeholder={t('node.descPlaceholder')} disabled={busy} /></label>
          </div>
        </div>
        <div className="modal-actions">
          <button type="button" className="quiet" onClick={onClose} disabled={busy}>{t('nset.close')}</button>
          <button type="submit" disabled={busy}><span className="btn-icon"><Ico n="check-ok" /> {t('node.adopt')}</span></button>
        </div>
      </form>
    </div>
  );
}

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
  // Adopt dialog: null = closed; an object = open, pre-filled (from a discovered node or
  // blank for manual address entry).
  const [adoptInitial, setAdoptInitial] = useState(null);
  // The node awaiting the wipe-countdown confirmation, or null when the modal is closed.
  const [wipeNode, setWipeNode] = useState(null);
  // The node whose Settings dialog (Camera Health / Users / Backup / Version & Health) is open.
  const [settingsNode, setSettingsNode] = useState(null);

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

  // Clicking a discovered node opens the Adopt dialog pre-filled with its address + hostname.
  function selectDiscovered(node) {
    setAdoptInitial({ ip: node.ip || '', httpsPort: String(node.httpsPort || ''), claimCode: '', name: node.name || '', description: '', icon: DEFAULT_NODE_ICON });
  }
  // Manual entry (node on another subnet / not discovered): open a blank Adopt dialog.
  function openManualAdopt() {
    setAdoptInitial({ ip: '', httpsPort: '', claimCode: '', name: '', description: '', icon: DEFAULT_NODE_ICON });
  }

  // doAdopt performs the adoption from the dialog's form; returns true so the dialog can
  // close itself on success.
  async function doAdopt(form) {
    const ip = form.ip.trim();
    const port = parseInt(form.httpsPort, 10);
    const code = form.claimCode.trim();
    if (!ip || !port) { toast(t('node.enterIpPort')); return false; }
    if (!code) { toast(t('node.enterClaim')); return false; }
    setBusy(true);
    const r = await api('/api/nodes/adopt', {
      method: 'POST',
      body: JSON.stringify({ ip, httpsPort: port, claimCode: code, name: form.name.trim(), description: form.description.trim(), icon: form.icon }),
    });
    setBusy(false);
    if (r.ok) { toast(t('node.nodeAdopted')); reloadNodes(); return true; }
    toast(r.message || t('node.adoptFailed'));
    return false;
  }

  async function release(node) {
    if (!window.confirm(t('node.confirmRelease', { name: node.name || node.nodeId }))) return;
    setBusy(true);
    const r = await api(`/api/nodes/${encodeURIComponent(node.nodeId)}/release`, { method: 'POST' });
    setBusy(false);
    if (r.ok) { toast(t('node.nodeReleased')); if (managingNodeId === node.nodeId) onBack(); reloadNodes(); }
    else toast(r.message || t('node.releaseFailed'));
  }

  // Wipe = remotely factory-reset the node (erases its recordings/config) over the control
  // tunnel, the same secure-wipe mymatasan runs locally. Gate on the node's reset-allowed
  // flag (bootstrap.allowReset) before showing the countdown so we fail fast when disabled.
  async function requestWipe(node) {
    setBusy(true);
    const st = await api(`/api/nodes/${encodeURIComponent(node.nodeId)}/proxy/api/system/reset/state`, { noRedirect: true }).catch(() => ({ ok: false }));
    setBusy(false);
    if (!st.ok) { toast(t('nwipe.offlineOrUnsupported')); return; }
    if (!st.body?.allowed) { toast(t('nwipe.notAllowed')); return; }
    setWipeNode(node);
  }

  async function confirmWipe() {
    const node = wipeNode;
    setWipeNode(null);
    if (!node) return;
    setBusy(true);
    // The node starts the reset asynchronously and returns the initial progress before it
    // wipes its DB and restarts — so this POST returns, then the node drops offline.
    const r = await api(`/api/nodes/${encodeURIComponent(node.nodeId)}/proxy/api/system/reset`, { method: 'POST', noRedirect: true }).catch(() => ({ ok: false }));
    setBusy(false);
    if (r.ok) { toast(t('nwipe.initiated', { name: node.name || node.nodeId })); reloadNodes(); }
    else toast(r.message || t('nwipe.failed'));
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
      {wipeNode ? <NodeWipeCountdown node={wipeNode} onCancel={() => setWipeNode(null)} onProceed={confirmWipe} /> : null}
      {settingsNode ? (
        <NodeSettingsDialog
          node={settingsNode}
          onClose={() => setSettingsNode(null)}
          onToast={toast}
          onWipe={(n) => { setSettingsNode(null); requestWipe(n); }}
          onNodeUpdated={(updated) => { reloadNodes(); setSettingsNode((s) => (s ? { ...s, ...updated } : s)); }}
        />
      ) : null}

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
            <button type="button" className="quiet" onClick={openManualAdopt}>
              <span className="btn-icon"><Ico n="plus" /> {t('node.adoptManual')}</span>
            </button>
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
                      : <button type="button" className="quiet" onClick={() => selectDiscovered(n)}><span className="btn-icon"><Ico n="plus" /> {t('node.adopt')}</span></button>}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        ) : null}
      </section>

      {adoptInitial ? (
        <AdoptDialog initial={adoptInitial} busy={busy} onClose={() => setAdoptInitial(null)} onAdopt={doAdopt} />
      ) : null}

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
                    <button type="button" className="quiet" onClick={() => setSettingsNode(n)}>
                      <span className="btn-icon"><Ico n="sliders" /> {t('node.manage')}</span>
                    </button>
                    <button type="button" className="quiet node-wipe-btn" onClick={() => requestWipe(n)} disabled={busy}>
                      <span className="btn-icon"><Ico n="warning" /> {t('node.wipe')}</span>
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
