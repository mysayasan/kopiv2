import { useCallback, useEffect, useState } from 'react';
import { Ico } from '@shared';
import { DiscoveredDevices } from './nodecam/cameras';
import { InfoButton } from './nodecam/ui';
import { normalizeScanDevice, sameCamera } from './nodecam/lib/helpers';

// Human labels for the "N devices found via X" summary. Mirrors mymatasan's own wording.
const PROTOCOL_LABELS = {
  all: 'all methods',
  onvif: 'ONVIF',
  ssdp: 'SSDP/UPnP',
  mdns: 'mDNS',
  sadp: 'SADP',
  portscan: 'port scan',
};

// NodeDiscoverPanel adds cameras to an adopted node by discovering them ON that node.
//
// This has to tunnel — it is not a stylistic choice. Camera discovery is multicast (ONVIF
// WS-Discovery to 239.255.255.250:3702, plus SSDP / mDNS / SADP), and multicast does not
// route off the LAN. The control plane is not on the cameras' broadcast domain, so a scan
// run from here would find precisely nothing. Every call below goes through the node proxy,
// so the probe packets leave the NODE's NICs and the port scan sweeps the NODE's subnets —
// exactly as if an operator were standing at the node's own UI. Even the CIDR box is
// prefilled from the node's interfaces, not the browser's.
//
// The list and the credential dialog are mymatasan's real components (nodecam/cameras.js),
// so a device row looks and behaves identically to the node's own Discover page. Saving is
// gated server-side: the node verifies the login (ONVIF GetStreamURI + RTSP DESCRIBE) and
// refuses to persist a camera that rejects it, which surfaces here as the dialog staying
// open with the error.
//
// Writes through the proxy are already admin-only (the node rejects a non-GET from a
// viewer) and are recorded in the audit log as node.command, so there is nothing extra to
// enforce here.
export function NodeDiscoverPanel({ px, t, onToast }) {
  const [timeoutMs, setTimeoutMs] = useState('3000');
  const [scanProtocol, setScanProtocol] = useState('all');
  const [scanCIDR, setScanCIDR] = useState('');
  const [manualAddress, setManualAddress] = useState('');
  const [discovered, setDiscovered] = useState([]);
  const [saved, setSaved] = useState([]);
  const [busy, setBusy] = useState(false);
  const [note, setNote] = useState('');

  // The node's already-saved cameras, so DiscoveredDevices can split the results into
  // "not saved" and an already-added group instead of offering duplicates.
  const loadSaved = useCallback(async () => {
    const r = await px('/api/cameras?limit=200');
    setSaved(r.ok ? (Array.isArray(r.body) ? r.body : r.body?.items || []) : []);
  }, [px]);

  useEffect(() => { loadSaved(); }, [loadSaved]);

  // Prefill the subnet box from the NODE's interfaces. Leaving it to the operator to guess
  // is the difference between a port scan that finds cameras and one that scans nothing.
  useEffect(() => {
    let live = true;
    px('/api/onvif/local-subnets').then((r) => {
      if (!live || !r.ok) return;
      const subnets = Array.isArray(r.body) ? r.body : [];
      if (subnets.length) setScanCIDR((current) => current || subnets[0]);
    });
    return () => { live = false; };
  }, [px]);

  async function scan() {
    setBusy(true);
    setNote('');
    const cidr = scanCIDR.trim();
    try {
      let devices = [];
      // A command that never reached the node (it's offline -> 404 "node is not connected")
      // must NOT be reported as "0 cameras found". Those mean opposite things to an
      // operator: one says "your LAN has no cameras", the other says "we never looked".
      let reached = false;
      let failure = '';

      if (scanProtocol === 'all') {
        // ONVIF and the multi-protocol sweep answer different cameras, so run both and
        // merge by host with ONVIF winning (it carries the stream/profile metadata).
        const ms = Number(timeoutMs) || 5000;
        const scanBody = { timeoutMs: ms };
        if (cidr) scanBody.cidr = cidr;
        const [onvifRes, scanRes] = await Promise.all([
          px('/api/onvif/discover', { method: 'POST', body: JSON.stringify({ timeoutMs: ms }) }),
          px('/api/onvif/scan', { method: 'POST', body: JSON.stringify(scanBody) }),
        ]);
        // Either engine answering is enough — one may be unsupported while the other works.
        reached = onvifRes.ok || scanRes.ok;
        failure = onvifRes.message || scanRes.message || '';
        const onvifDevices = (onvifRes.ok && Array.isArray(onvifRes.body) ? onvifRes.body : [])
          .map((d) => ({ ...d, _discoveryMethods: ['onvif'] }));
        const scanDevices = (scanRes.ok && Array.isArray(scanRes.body) ? scanRes.body : [])
          .map(normalizeScanDevice);
        const onvifHosts = new Set(onvifDevices.map((d) => d.host));
        devices = [...onvifDevices, ...scanDevices.filter((d) => !onvifHosts.has(d.host))];
      } else if (scanProtocol === 'onvif') {
        const r = await px('/api/onvif/discover', {
          method: 'POST',
          body: JSON.stringify({ timeoutMs: Number(timeoutMs) || 3000 }),
        });
        reached = r.ok;
        failure = r.message || '';
        devices = (r.ok && Array.isArray(r.body) ? r.body : []).map((d) => ({ ...d, _discoveryMethods: ['onvif'] }));
      } else {
        const body = { timeoutMs: Number(timeoutMs) || 5000, methods: [scanProtocol] };
        if (cidr) body.cidr = cidr;
        const r = await px('/api/onvif/scan', { method: 'POST', body: JSON.stringify(body) });
        reached = r.ok;
        failure = r.message || '';
        devices = (r.ok && Array.isArray(r.body) ? r.body : []).map(normalizeScanDevice);
      }

      if (!reached) {
        setDiscovered([]);
        if (onToast) onToast(failure || t('nset.scanFailed'), 'error');
        setNote(t('nset.scanFailed'));
        return;
      }

      setDiscovered(devices);
      const newCount = devices.filter((d) => !saved.some((sv) => sameCamera(d, sv))).length;
      setNote(t('app.devicesFound', {
        n: devices.length,
        label: PROTOCOL_LABELS[scanProtocol] || scanProtocol,
        newCount,
        savedCount: devices.length - newCount,
      }));
    } finally {
      setBusy(false);
    }
  }

  async function probe(event) {
    event.preventDefault();
    const address = manualAddress.trim();
    if (!address) {
      if (onToast) onToast(t('app.manualAddrRequired'), 'error');
      return;
    }
    setBusy(true);
    const r = await px('/api/onvif/probe', { method: 'POST', body: JSON.stringify({ address }) });
    setBusy(false);
    if (!r.ok || !r.body) {
      if (onToast) onToast(r.message || t('nset.probeFailed'), 'error');
      return;
    }
    const device = r.body;
    setDiscovered((current) => [device, ...current.filter((item) => item.xAddr !== device.xAddr)]);
    setNote(t('app.manualProbeDone'));
  }

  // Throws on failure BY DESIGN: AddCameraDialog catches it to keep the credential dialog
  // open with the message, which is what makes a rejected camera login recoverable in place
  // rather than losing everything the operator typed.
  async function save(device, draft = {}) {
    // Frontend-only annotations the node's struct won't accept (DisallowUnknownFields).
    const { _discoveryMethods, _openPorts, ...deviceData } = device;
    setBusy(true);
    try {
      const r = await px('/api/cameras/discovered', {
        method: 'POST',
        body: JSON.stringify({
          ...deviceData,
          name: (draft.name || '').trim(),
          description: (draft.description || '').trim(),
          username: (draft.username || '').trim(),
          password: draft.password || '',
        }),
      });
      if (!r.ok) throw new Error(r.message || t('cam.authFailed'));
      if (onToast) onToast(t('app.cameraSaved'));
      await loadSaved();
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="settings-layout">
      <section className="settings-panel span-two">
        <header>
          <h2><span className="btn-icon"><Ico n="video" /> {t('cam.discoverCameras')}</span></h2>
        </header>
        <p className="settings-hint">{t('nset.discoverHint')}</p>

        <section className="discover-layout">
          <div className="discover-actions">
            <div className="discover-card discover-card--scan">
              <div className="discover-card-head">
                <span className="discover-card-icon"><Ico n="wifi" /></span>
                <div className="discover-card-heading">
                  <h3>{t('cam.scanTitle')}</h3>
                  <p>{t('cam.scanCardHint')}</p>
                </div>
              </div>
              <div className="scan-row">
                <label>
                  {t('cam.scanTimeout')}
                  <input value={timeoutMs} onChange={(e) => setTimeoutMs(e.target.value)} inputMode="numeric" disabled={busy} />
                </label>
                <label className="scan-protocol-label">
                  {t('cam.protocol')}
                  <select
                    value={scanProtocol}
                    onChange={(e) => setScanProtocol(e.target.value)}
                    className="scan-protocol-select"
                    disabled={busy}
                  >
                    <option value="all">{t('cam.allMethods')}</option>
                    <option value="onvif">ONVIF</option>
                    <option value="ssdp">{t('cam.ssdp')}</option>
                    <option value="mdns">{t('cam.mdns')}</option>
                    <option value="sadp">{t('cam.sadp')}</option>
                    <option value="portscan">{t('cam.portScan')}</option>
                  </select>
                </label>
                <label className="scan-protocol-label">
                  <span className="scan-label-row">
                    {t('cam.subnet')}
                    <InfoButton text={t('cam.subnetInfo')} />
                  </span>
                  <input
                    value={scanCIDR}
                    onChange={(e) => setScanCIDR(e.target.value)}
                    placeholder="auto"
                    className="scan-cidr-input"
                    disabled={busy}
                  />
                </label>
                <button type="button" onClick={scan} disabled={busy}>
                  <span className="btn-icon"><Ico n="wifi" /> {busy ? t('cam.scanning') : t('cam.scan')}</span>
                </button>
              </div>
            </div>

            <div className="discover-divider" aria-hidden="true"><span>{t('common.or')}</span></div>

            <div className="discover-card discover-card--manual">
              <div className="discover-card-head">
                <span className="discover-card-icon"><Ico n="plus" /></span>
                <div className="discover-card-heading">
                  <h3>{t('cam.manualTitle')}</h3>
                  <p>{t('cam.manualCardHint')}</p>
                </div>
              </div>
              <form className="probe-row" onSubmit={probe}>
                <label>
                  {t('cam.manualAddress')}
                  <input
                    value={manualAddress}
                    onChange={(e) => setManualAddress(e.target.value)}
                    placeholder="192.168.1.40"
                    disabled={busy}
                  />
                </label>
                <button type="submit" disabled={busy}>
                  <span className="btn-icon"><Ico n="search" /> {t('cam.probe')}</span>
                </button>
              </form>
            </div>
          </div>
        </section>

        {note ? <p className="settings-hint">{note}</p> : null}

        <DiscoveredDevices devices={discovered} saved={saved} busy={busy} onSave={save} />
      </section>
    </div>
  );
}
