import { useEffect, useState } from 'react';
import { Ico } from '@shared';
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
export function NodesTab({ onToast, nodes, reloadNodes, managingNodeId, onManage, onBack }) {
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
    if (!window.confirm('Generate a new fleet key? Existing nodes keep working only if you update their key too.')) return;
    setBusy(true);
    const r = await api('/api/nodes/fleet-key', { method: 'POST' });
    setBusy(false);
    if (r.ok) { setFleetKey(r.body); toast('Fleet key generated. Paste it into each node.'); }
    else toast(r.message || 'Failed to generate fleet key.');
  }

  async function copyKey() {
    const key = fleetKey?.fleetKey;
    if (!key) return;
    try {
      await navigator.clipboard.writeText(key);
      setCopied(true);
      toast('Fleet key copied to clipboard.');
      setTimeout(() => setCopied(false), 2000);
    } catch (_) {
      toast('Could not copy automatically — select the key and copy manually.');
    }
  }

  async function scan() {
    if (!fleetKey?.set) { toast('Generate a fleet key first — discovery probes are signed with it.'); return; }
    setScanning(true);
    const r = await api('/api/nodes/scan', { method: 'POST', body: JSON.stringify({ timeoutMs: 4000 }) });
    setScanning(false);
    if (!r.ok) { toast(r.message || 'Scan failed (is a fleet key set?).'); return; }
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
    if (!ip || !port) { toast('Enter the node IP and HTTPS port.'); return; }
    if (!code) { toast('Enter the claim code shown on the node.'); return; }
    setBusy(true);
    const r = await api('/api/nodes/adopt', {
      method: 'POST',
      body: JSON.stringify({ ip, httpsPort: port, claimCode: code, name: adoptForm.name.trim(), description: adoptForm.description.trim(), icon: adoptForm.icon }),
    });
    setBusy(false);
    if (r.ok) { toast('Node adopted.'); setAdoptForm({ ip: '', httpsPort: '', claimCode: '', name: '', description: '', icon: DEFAULT_NODE_ICON }); reloadNodes(); }
    else toast(r.message || 'Adoption failed (check the claim code and that the node is reachable).');
  }

  async function release(node) {
    if (!window.confirm(`Release "${node.name || node.nodeId}"? It will become discoverable again.`)) return;
    setBusy(true);
    const r = await api(`/api/nodes/${encodeURIComponent(node.nodeId)}/release`, { method: 'POST' });
    setBusy(false);
    if (r.ok) { toast('Node released.'); if (managingNodeId === node.nodeId) onBack(); reloadNodes(); }
    else toast(r.message || 'Release failed.');
  }

  const keySet = !!fleetKey?.set;

  if (managing) {
    return (
      <NodeManager
        node={managing}
        onToast={toast}
        onBack={() => { onBack(); reloadNodes(); }}
      />
    );
  }

  return (
    <section className="workspace">
      <FormBusyOverlay busy={busy} />

      <section className="settings-panel span-two">
        <header>
          <h2><span className="btn-icon"><Ico n="key" /> Fleet key</span></h2>
        </header>
        <p className="settings-hint">
          Nodes are discoverable only by a control plane that shares this key. Generate it once, then paste it into
          each mymatasan node&apos;s Settings → Connectivity.
        </p>
        <div className="node-key-row">
          <div className="fleet-key-field">
            <code className="fleet-key-box">{keySet ? fleetKey.fleetKey : 'Not set — generate one to begin.'}</code>
            {keySet ? (
              <button
                type="button"
                className="fleet-key-copy"
                onClick={copyKey}
                title="Copy fleet key"
                aria-label="Copy fleet key"
              >
                <Ico n={copied ? 'check-ok' : 'copy'} sz={15} />
              </button>
            ) : null}
          </div>
          <button type="button" className="quiet" onClick={generateFleetKey} disabled={busy}>
            <span className="btn-icon"><Ico n="refresh" /> Generate / rotate</span>
          </button>
        </div>
      </section>

      <section className="settings-panel span-two">
        <header>
          <h2><span className="btn-icon"><Ico n="search" /> Discover nodes</span></h2>
          <div className="settings-header-actions">
            <button type="button" onClick={scan} disabled={scanning || !keySet}>
              <span className="btn-icon"><Ico n="wifi" /> {scanning ? 'Scanning…' : 'Scan LAN'}</span>
            </button>
          </div>
        </header>
        <p className="settings-hint">
          Scan the local network for unpaired mymatasan nodes. Already-adopted nodes go silent and won&apos;t appear.
        </p>
        {!keySet ? <p className="settings-hint danger-text">Set a fleet key above before scanning.</p> : null}
        {discovered && discovered.length === 0 ? <p className="settings-hint">No unpaired nodes found.</p> : null}
        {discovered && discovered.length > 0 ? (
          <table className="event-table">
            <thead><tr><th>Name</th><th>Node ID</th><th>Address</th><th>Version</th><th></th></tr></thead>
            <tbody>
              {discovered.map((n) => (
                <tr key={n.nodeId}>
                  <td>{n.name || '—'}</td>
                  <td>{(n.nodeId || '').slice(0, 8)}</td>
                  <td>{n.ip}:{n.httpsPort}</td>
                  <td>{n.version || '—'}</td>
                  <td>
                    {n.adopted
                      ? <span className="status-pill online">Adopted</span>
                      : <button type="button" className="quiet" onClick={() => selectDiscovered(n)}>Select</button>}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        ) : null}
      </section>

      <section className="settings-panel span-two">
        <header><h2><span className="btn-icon"><Ico n="plus" /> Adopt a node</span></h2></header>
        <p className="settings-hint">
          Pick a discovered node (or enter an address manually for a different subnet), then enter the claim code shown
          on that node&apos;s Connectivity page. Name defaults to the node&apos;s hostname — rename it if you like.
        </p>
        <div className="settings-field-grid">
          <label>Node IP
            <input value={adoptForm.ip} onChange={(e) => setAdoptForm({ ...adoptForm, ip: e.target.value })} placeholder="192.168.1.40" disabled={busy} />
          </label>
          <label>HTTPS port
            <input value={adoptForm.httpsPort} onChange={(e) => setAdoptForm({ ...adoptForm, httpsPort: e.target.value })} placeholder="3000" disabled={busy} />
          </label>
          <label>Claim code
            <input value={adoptForm.claimCode} onChange={(e) => setAdoptForm({ ...adoptForm, claimCode: e.target.value })} placeholder="claim code" disabled={busy} />
          </label>
          <label>Name <span className="settings-field-opt">(optional)</span>
            <input value={adoptForm.name} onChange={(e) => setAdoptForm({ ...adoptForm, name: e.target.value })} placeholder="defaults to hostname" disabled={busy} />
          </label>
          <label className="field-span-two">Description <span className="settings-field-opt">(optional)</span>
            <textarea value={adoptForm.description} onChange={(e) => setAdoptForm({ ...adoptForm, description: e.target.value })} rows={2} placeholder="Shown as a tooltip in the nav, e.g. “Front gate — 4 cameras”" disabled={busy} />
          </label>
          <label>Icon
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
            <span className="btn-icon"><Ico n="check-ok" /> Adopt</span>
          </button>
        </div>
      </section>

      <section className="settings-panel span-two">
        <header>
          <h2><span className="btn-icon"><Ico n="shield" /> Adopted nodes</span></h2>
          <div className="settings-header-actions">
            <button type="button" className="quiet" onClick={reloadNodes} disabled={busy}>
              <span className="btn-icon"><Ico n="reload" /> Refresh</span>
            </button>
          </div>
        </header>
        {nodeList.length === 0 ? (
          <p className="settings-hint">No adopted nodes yet.</p>
        ) : (
          <table className="event-table">
            <thead><tr><th>Name</th><th>Address</th><th>Status</th><th>Cert expires</th><th>Adopted</th><th></th></tr></thead>
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
                      <span className="btn-icon"><Ico n="sliders" /> Manage</span>
                    </button>
                    <button type="button" className="quiet danger-text" onClick={() => release(n)} disabled={busy}>Release</button>
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
