import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { Ico, useT } from '@shared';
import { api, errorMessage, formatAgo, formatTimestamp } from '../lib/helpers';
import { CredentialModal } from './devices';
import { ConfirmModal, CopyButton, EmptyState, Field, FormBusyOverlay, Modal, Panel } from './ui';

// DiscoveryPage is onboarding at scale, and the whole page is shaped by ONE fact:
//
//   a device that is not in the inventory cannot connect to the broker at all.
//
// That is the security model — it is what makes the device table the credential store and
// what makes deleting a device really revoke it. But nobody onboards two hundred door
// contacts by typing two hundred device keys, and a device that does not exist yet cannot
// announce itself. So the door is opened on purpose:
//
//   - TIME-BOXED. The window expires on its own; there is no way to leave it open by
//     forgetting about it, which is how every "temporary" provisioning mode becomes
//     permanent.
//   - SECRET-GATED. It mints a one-time key with real entropy, shown exactly once.
//   - QUARANTINED. This is the property that actually matters, and the page says so out
//     loud: what an enrolling client publishes becomes a CANDIDATE and nothing else. No
//     telemetry is stored for it and no rule is evaluated. Somebody who slips into the
//     window can leave junk for an admin to decline; they cannot forge a reading, move a
//     chart, or trigger or suppress an alert.
//
// Adoption is the deliberate act at the end of it: the admin sees what the thing actually
// sends, is told what it probably is, and mints it a real credential.
//
// Every route behind this page is admin-only server-side. The nav entry is hidden for
// everyone else as a courtesy, not as the enforcement.

// The window lengths on offer. The server clamps anything above an hour: a window measured
// in days is not a window, it is a permanently open door with a nicer name.
const MINUTE_OPTIONS = [5, 10, 15, 30, 60];

// CANDIDATE_POLL is how often the candidate list is re-read WHILE the window is open, so a
// device that has just been powered up appears without anybody pressing refresh. It stops
// the moment the window closes — nothing new can arrive after that.
const CANDIDATE_POLL_MS = 5000;

export function DiscoveryPage({ onToast }) {
  const t = useT();
  const [status, setStatus] = useState({ open: false, expiresAt: 0 });
  const [candidates, setCandidates] = useState([]);
  const [profiles, setProfiles] = useState([]);
  const [minutes, setMinutes] = useState(10);
  const [busy, setBusy] = useState(true);
  const [opening, setOpening] = useState(false);
  const [error, setError] = useState('');
  // mintedKey exists in exactly one place in the world — this state, on this screen, right
  // after the POST that created it. The hub kept only a bcrypt hash.
  const [mintedKey, setMintedKey] = useState('');
  // credential is the adopted device's REAL permanent password: same show-once contract.
  const [credential, setCredential] = useState(null);
  const [confirmReject, setConfirmReject] = useState(null);
  const [rejecting, setRejecting] = useState(false);

  const loadStatus = useCallback(async () => {
    const r = await api('/api/discovery/window');
    if (r.ok) {
      setStatus(r.body || { open: false, expiresAt: 0 });
      setError('');
    } else {
      setError(errorMessage(r, t('disc.statusFailed')));
    }
  }, [t]);

  const loadCandidates = useCallback(async () => {
    const r = await api('/api/discovery/candidates');
    if (r.ok) setCandidates(r.body?.items || []);
    else setError(errorMessage(r, t('disc.candidatesFailed')));
  }, [t]);

  const loadProfiles = useCallback(async () => {
    const r = await api('/api/profiles');
    if (r.ok) setProfiles(r.body?.items || []);
  }, []);

  const loadAll = useCallback(async () => {
    setBusy(true);
    // Candidates outlive the window they arrived in — one that was never reviewed is still
    // waiting — so they are loaded whether or not a window is open right now.
    await Promise.all([loadStatus(), loadCandidates(), loadProfiles()]);
    setBusy(false);
  }, [loadStatus, loadCandidates, loadProfiles]);

  useEffect(() => { loadAll(); }, [loadAll]);

  // Poll only while the door is open. A closed window admits nothing, so polling it would be
  // a request per five seconds asking a question whose answer cannot change.
  useEffect(() => {
    if (!status.open) return undefined;
    const id = setInterval(loadCandidates, CANDIDATE_POLL_MS);
    return () => clearInterval(id);
  }, [status.open, loadCandidates]);

  async function openWindow() {
    setOpening(true);
    const r = await api('/api/discovery/window', {
      method: 'POST',
      body: JSON.stringify({ minutes: Number(minutes) || 10 }),
    });
    setOpening(false);
    if (!r.ok) {
      onToast(errorMessage(r, t('disc.openFailed')), 'error');
      return;
    }
    setStatus({ open: !!r.body?.open, expiresAt: r.body?.expiresAt || 0 });
    // The one and only time this value is readable. Straight into the show-once dialog.
    if (r.body?.key) setMintedKey(r.body.key);
    onToast(t('disc.opened'), 'success');
    loadCandidates();
  }

  async function closeWindow() {
    const r = await api('/api/discovery/window', { method: 'DELETE' });
    if (!r.ok) {
      onToast(errorMessage(r, t('disc.closeFailed')), 'error');
      return;
    }
    setStatus(r.body || { open: false, expiresAt: 0 });
    onToast(t('disc.closed'), 'success');
  }

  async function adopt(candidate, form) {
    const r = await api(`/api/discovery/candidates/${candidate.id}/adopt`, {
      method: 'POST',
      body: JSON.stringify({
        profileId: Number(form.profileId) || 0,
        name: form.name,
        tag: form.tag,
        location: form.location,
      }),
    });
    if (!r.ok) return errorMessage(r, t('disc.adoptFailed'));
    const device = r.body?.device;
    onToast(t('disc.adopted', { name: device?.name || candidate.deviceKey }), 'success');
    setCredential({ password: r.body?.password, name: device?.name || candidate.deviceKey, rotated: false });
    loadCandidates();
    return '';
  }

  const [scanning, setScanning] = useState(false);
  async function runScan(req) {
    setScanning(true);
    const r = await api('/api/discovery/scan', { method: 'POST', body: JSON.stringify(req) });
    setScanning(false);
    if (!r.ok) {
      onToast(errorMessage(r, t('disc.scanFailed')), 'error');
      return;
    }
    onToast(t('disc.scanDone', { n: r.body?.found || 0 }), 'success');
    loadCandidates();
  }

  async function reject(candidate) {
    setRejecting(true);
    const r = await api(`/api/discovery/candidates/${candidate.id}`, { method: 'DELETE' });
    setRejecting(false);
    setConfirmReject(null);
    if (!r.ok) {
      onToast(errorMessage(r, t('disc.rejectFailed')), 'error');
      return;
    }
    onToast(t('disc.rejected'), 'success');
    loadCandidates();
  }

  return (
    <section className="workspace">
      <Panel
        icon="search"
        title={t('page.discovery')}
        hint={t('page.discoveryHint')}
        actions={(
          <button type="button" className="quiet" onClick={loadAll} disabled={busy}>
            <span className="btn-icon"><Ico n="refresh" sz={14} /> {t('common.refresh')}</span>
          </button>
        )}
      >
        {error ? <div className="status-line">{error}</div> : null}

        {/* The security model, in the place where somebody is about to act on it. An admin
            opening a window should understand they are opening a door, that it closes
            itself, and that nothing which walks through it is trusted. */}
        <div className="iot-disc-model">
          <h3><span className="btn-icon"><Ico n="shield" sz={15} /> {t('disc.model.title')}</span></h3>
          <p>{t('disc.model.p1')}</p>
          <p>{t('disc.model.p2')}</p>
          <p className="iot-disc-model-quarantine">
            <Ico n="lock" sz={14} /> <span>{t('disc.model.p3')}</span>
          </p>
        </div>

        <WindowCard
          status={status}
          minutes={minutes}
          onMinutes={setMinutes}
          onOpen={openWindow}
          onClose={closeWindow}
          onExpired={loadStatus}
          busy={busy}
          opening={opening}
        />
      </Panel>

      <Panel icon="wifi" title={t('disc.scanTitle')} hint={t('disc.scanHint')}>
        <ScanCard onScan={runScan} scanning={scanning} />
      </Panel>

      <Panel icon="cpu" title={t('disc.candidatesTitle')} hint={t('disc.candidatesHint')}>
        {candidates.length === 0 ? (
          status.open ? (
            <div className="iot-disc-waiting">
              <EmptyState text={t('disc.emptyOpen')} icon="wifi" />
              <p className="settings-hint">{t('disc.emptyOpenSteps', { host: window.location.hostname })}</p>
            </div>
          ) : (
            <EmptyState text={t('disc.emptyClosed')} />
          )
        ) : (
          <ul className="iot-disc-list">
            {candidates.map((c) => (
              <CandidateCard
                key={c.id}
                candidate={c}
                profiles={profiles}
                onAdopt={adopt}
                onReject={() => setConfirmReject(c)}
              />
            ))}
          </ul>
        )}
      </Panel>

      {mintedKey ? <KeyModal value={mintedKey} onClose={() => setMintedKey('')} /> : null}
      {credential ? <CredentialModal credential={credential} onClose={() => setCredential(null)} /> : null}
      {confirmReject ? (
        <ConfirmModal
          title={t('disc.rejectTitle')}
          body={t('disc.rejectBody', { key: confirmReject.deviceKey })}
          confirmLabel={t('disc.reject')}
          busy={rejecting}
          onCancel={() => setConfirmReject(null)}
          onConfirm={() => reject(confirmReject)}
        />
      ) : null}
    </section>
  );
}

// WindowCard is the door: shut, with a handle; or open, with the clock that will shut it.
function WindowCard({ status, minutes, onMinutes, onOpen, onClose, onExpired, busy, opening }) {
  const t = useT();
  const remaining = useCountdown(status.open ? status.expiresAt : 0, onExpired);

  if (!status.open) {
    return (
      <div className="iot-disc-window is-closed">
        <div className="iot-disc-window-state">
          <span className="status-pill offline">{t('disc.stateClosed')}</span>
          <p>{t('disc.windowClosed')}</p>
        </div>
        <div className="iot-disc-window-open">
          <Field label={t('disc.minutes')} hint={t('disc.minutesHint')}>
            <select value={minutes} onChange={(e) => onMinutes(Number(e.target.value))} disabled={opening}>
              {MINUTE_OPTIONS.map((m) => (
                <option key={m} value={m}>
                  {m === 60 ? t('disc.minutesHour') : t('disc.minutesOption', { n: m })}
                </option>
              ))}
            </select>
          </Field>
          <button type="button" onClick={onOpen} disabled={busy || opening}>
            <span className="btn-icon">
              <Ico n="key" sz={14} /> {opening ? t('disc.opening') : t('disc.open')}
            </span>
          </button>
        </div>
      </div>
    );
  }

  return (
    <div className="iot-disc-window is-open">
      <div className="iot-disc-window-state">
        <span className="status-pill online">{t('disc.stateOpen')}</span>
        <p>{t('disc.windowOpenState')}</p>
      </div>
      <div className="iot-disc-window-open">
        <div className="iot-disc-countdown" role="timer" aria-live="off">
          <span className="iot-disc-countdown-label">{t('disc.closesIn')}</span>
          <strong>{remaining}</strong>
          <span className="iot-disc-countdown-at">{formatTimestamp(status.expiresAt)}</span>
        </div>
        <button type="button" className="danger-solid" onClick={onClose}>
          <span className="btn-icon"><Ico n="lock" sz={14} /> {t('disc.closeNow')}</span>
        </button>
      </div>
    </div>
  );
}

// useCountdown ticks the time left on the window down to zero, then tells the caller so the
// page can re-read the real status from the server rather than deciding for itself that the
// door is shut. (The server enforces the expiry on every connect; this is only the clock the
// human reads.)
function useCountdown(expiresAt, onExpired) {
  const [now, setNow] = useState(() => Math.floor(Date.now() / 1000));
  const firedRef = useRef(false);

  useEffect(() => {
    firedRef.current = false;
    if (!expiresAt) return undefined;
    setNow(Math.floor(Date.now() / 1000));
    const id = setInterval(() => setNow(Math.floor(Date.now() / 1000)), 1000);
    return () => clearInterval(id);
  }, [expiresAt]);

  const left = expiresAt ? Math.max(0, expiresAt - now) : 0;

  useEffect(() => {
    if (!expiresAt || left > 0 || firedRef.current) return;
    firedRef.current = true;
    if (onExpired) onExpired();
  }, [expiresAt, left, onExpired]);

  return useMemo(() => {
    const mm = Math.floor(left / 60);
    const ss = left % 60;
    return `${String(mm).padStart(2, '0')}:${String(ss).padStart(2, '0')}`;
  }, [left]);
}

// KeyModal is the enrollment key, once. Same contract as a device credential — the hub kept
// only a bcrypt hash — so the dialog says so and makes copying it the obvious next act.
function KeyModal({ value, onClose }) {
  const t = useT();
  const [copied, setCopied] = useState(false);
  return (
    <Modal title={t('disc.keyTitle')} onClose={onClose}>
      <div className="iot-cred-warning">
        <Ico n="warning" sz={18} />
        <p>{t('disc.keyWarning')}</p>
      </div>
      <p className="settings-hint">{t('disc.keyFor')}</p>
      <div className="iot-cred-value">
        <code>{value}</code>
      </div>
      <div className="modal-actions">
        <CopyButton value={value} label={t('disc.keyCopy')} onCopied={() => setCopied(true)} />
        <button type="button" onClick={onClose} disabled={!copied} title={copied ? '' : t('disc.keyMustCopy')}>
          {t('disc.keyAck')}
        </button>
      </div>
      {!copied ? <p className="field-hint iot-cred-gate">{t('disc.keyMustCopy')}</p> : null}
    </Modal>
  );
}

// CandidateCard is one thing that knocked on the door: what it calls itself, where it
// published, what fields it sent, and — the honest part — what the hub thinks it is, or a
// plain admission that it does not know.
function CandidateCard({ candidate, profiles, onAdopt, onReject }) {
  const t = useT();
  const [showPayload, setShowPayload] = useState(false);
  const [adopting, setAdopting] = useState(false);
  const [error, setError] = useState('');
  const suggested = profiles.find((p) => p.id === candidate.suggestedProfileId) || null;
  // A scan-sourced candidate (Modbus/mDNS/SSDP/…) was found on the network, not announced over the
  // broker, so it shows how it was found + where it lives rather than a topic + observed fields.
  const isScan = !!candidate.source && candidate.source !== 'mqtt';

  const [form, setForm] = useState(() => ({
    name: candidate.deviceKey,
    // 0 is a real, meaningful value here: with no suggestion it means "adopt it without a
    // type for now"; the server reads 0 as "use the suggestion", which is 0 too.
    profileId: candidate.suggestedProfileId || 0,
    tag: '',
    location: '',
  }));
  const set = (patch) => setForm((f) => ({ ...f, ...patch }));

  const keys = (candidate.observedKeys || '').split(',').map((k) => k.trim()).filter(Boolean);

  async function submit(e) {
    e.preventDefault();
    setAdopting(true);
    setError('');
    const msg = await onAdopt(candidate, form);
    setAdopting(false);
    if (msg) setError(msg);
  }

  return (
    <li className="iot-disc-card">
      <FormBusyOverlay busy={adopting} />
      <header className="iot-disc-card-head">
        <div className="iot-disc-card-id">
          <code>{candidate.deviceKey}</code>
          <span className="iot-disc-quarantine">
            <Ico n="lock" sz={11} /> {t('disc.quarantined')}
          </span>
        </div>
        <div className="iot-disc-card-meta">
          <span>{t('disc.messages', { n: candidate.messageCount })}</span>
          <span title={formatTimestamp(candidate.firstSeenAt)}>
            {t('disc.firstSeen')}: {formatAgo(candidate.firstSeenAt, t)}
          </span>
          <span title={formatTimestamp(candidate.lastSeenAt)}>
            {t('disc.lastSeen')}: {formatAgo(candidate.lastSeenAt, t)}
          </span>
        </div>
      </header>

      <div className="iot-disc-card-facts">
        {isScan ? (
          <>
            <div>
              <span>{t('disc.foundVia')}</span>
              <code>{t(`disc.scanType.${candidate.source}`)}</code>
            </div>
            <div>
              <span>{t('disc.address')}</span>
              <code>{candidate.address || candidate.endpoint || '—'}</code>
            </div>
            <div>
              <span>{t('disc.identifiedAs')}</span>
              <code>{candidate.observedKeys || '—'}</code>
            </div>
          </>
        ) : (
          <>
            <div>
              <span>{t('disc.topic')}</span>
              <code>{candidate.topic || '—'}</code>
            </div>
            <div>
              <span>{t('disc.observed')}</span>
              {keys.length > 0 ? (
                <div className="iot-disc-keys">
                  {keys.map((k) => <code key={k} className="iot-disc-key">{k}</code>)}
                </div>
              ) : (
                <p className="field-hint">{t('disc.noObserved')}</p>
              )}
            </div>
          </>
        )}
      </div>

      <button type="button" className="quiet iot-disc-payload-toggle" onClick={() => setShowPayload((v) => !v)}>
        <span className="btn-icon">
          <Ico n={showPayload ? 'chev-up' : 'chev-down'} sz={12} />
          {showPayload ? t('disc.hidePayload') : t('disc.showPayload')}
        </span>
      </button>
      {showPayload ? <pre className="iot-disc-payload">{candidate.payload || '—'}</pre> : null}

      {/* The suggestion, or the absence of one. The backend deliberately says nothing rather
          than guess wrong — a type an installer accepts without thinking silently mis-decodes
          every reading that device will ever send — so the UI does not invent a guess here
          either. */}
      {suggested ? (
        <p className="iot-disc-suggest">
          <Ico n="wand" sz={14} />
          <span>
            {t('disc.suggested')} <strong>{suggested.name}</strong> — {t('disc.suggestionHint')}
          </span>
        </p>
      ) : (
        <p className="iot-disc-suggest is-none">
          <Ico n="info" sz={14} />
          <span>
            <strong>{t('disc.noSuggestion')}</strong> {t('disc.noSuggestionHint')}
          </span>
        </p>
      )}

      <form className="iot-disc-adopt" onSubmit={submit}>
        {error ? <div className="status-line">{error}</div> : null}
        <div className="settings-field-grid">
          <Field label={t('devices.name')} hint={t('disc.nameHint')}>
            <input value={form.name} onChange={(e) => set({ name: e.target.value })} />
          </Field>
          <Field label={t('devices.profile')} hint={suggested ? t('disc.profilePreselected') : t('disc.noProfileHint')}>
            <select value={form.profileId} onChange={(e) => set({ profileId: Number(e.target.value) })}>
              <option value={0}>{t('disc.noProfile')}</option>
              {profiles.map((p) => (
                <option key={p.id} value={p.id}>
                  {p.id === candidate.suggestedProfileId ? t('disc.profileSuggestedOption', { name: p.name }) : p.name}
                </option>
              ))}
            </select>
          </Field>
          <Field label={t('devices.tag')} hint={t('devices.tagHint')}>
            <input value={form.tag} onChange={(e) => set({ tag: e.target.value })} />
          </Field>
          <Field label={t('devices.location')}>
            <input value={form.location} onChange={(e) => set({ location: e.target.value })} />
          </Field>
        </div>
        <p className="field-hint">{t('disc.adoptNote')}</p>
        <div className="action-row iot-actions-end">
          <button type="button" className="quiet danger-text" onClick={onReject} disabled={adopting}>
            {t('disc.reject')}
          </button>
          <button type="submit" disabled={adopting}>
            <span className="btn-icon">
              <Ico n="check-ok" sz={14} /> {adopting ? t('disc.adopting') : t('disc.adopt')}
            </span>
          </button>
        </div>
      </form>
    </li>
  );
}

// ScanCard is the ACTIVE-discovery counterpart to the enrollment window: instead of waiting for a
// device to announce itself, it sweeps the LAN. It offers one toggle per protocol family, a network
// range for the Modbus sweep, and is blunt about the fact that a scan is a deliberate, admin-only,
// read-only probe — never a write.
const SCAN_TYPES = ['modbus', 'mdns', 'ssdp', 'ethernetip', 'bacnet'];

function ScanCard({ onScan, scanning }) {
  const t = useT();
  const [types, setTypes] = useState({ modbus: true, mdns: true, ssdp: true, ethernetip: false, bacnet: false });
  const [cidr, setCidr] = useState('');
  const [transport, setTransport] = useState('tcp');
  const [units, setUnits] = useState('1');
  const toggle = (k) => setTypes((s) => ({ ...s, [k]: !s[k] }));
  const selected = SCAN_TYPES.filter((k) => types[k]);

  function submit(e) {
    e.preventDefault();
    onScan({
      types: selected,
      cidr,
      modbusTransport: transport,
      units: units.split(',').map((u) => Number(u.trim())).filter((n) => n > 0),
    });
  }

  return (
    <form className="iot-scan" onSubmit={submit}>
      <p className="settings-hint">{t('disc.scanIntro')}</p>
      <div className="iot-scan-types">
        {SCAN_TYPES.map((k) => (
          <label key={k} className="check-row">
            <input type="checkbox" checked={!!types[k]} onChange={() => toggle(k)} />
            <span>{t(`disc.scanType.${k}`)}</span>
          </label>
        ))}
      </div>
      {types.modbus ? (
        <div className="settings-field-grid">
          <Field label={t('disc.scanCidr')} hint={t('disc.scanCidrHint')} required>
            <input value={cidr} onChange={(e) => setCidr(e.target.value)} placeholder="192.168.1.0/24" />
          </Field>
          <Field label={t('devices.transport')} hint={t('disc.scanTransportHint')}>
            <select value={transport} onChange={(e) => setTransport(e.target.value)}>
              <option value="tcp">{t('transport.tcp')}</option>
              <option value="rtutcp">{t('transport.rtutcp')}</option>
            </select>
          </Field>
          <Field label={t('disc.scanUnits')} hint={t('disc.scanUnitsHint')}>
            <input value={units} onChange={(e) => setUnits(e.target.value)} placeholder="1,2,3" />
          </Field>
        </div>
      ) : null}
      <div className="iot-scan-warn">
        <Ico n="warning" sz={14} />
        <span>{t('disc.scanWarn')}</span>
      </div>
      <div className="action-row iot-actions-end">
        <button type="submit" disabled={scanning || selected.length === 0}>
          <span className="btn-icon">
            <Ico n="search" sz={14} /> {scanning ? t('disc.scanning') : t('disc.scanRun')}
          </span>
        </button>
      </div>
    </form>
  );
}
