import { useCallback, useEffect, useMemo, useState } from 'react';
import { DataTable, Ico, useT } from '@shared';
import { api, errorMessage, formatAgo, formatNumber, formatTimestamp } from '../lib/helpers';
import { EmptyState, Field, Modal } from './ui';

// Actuation, seen from the browser.
//
// Everything in this file is written around ONE fact: "sent" is not "done". The POST returning
// 200 means the hub published a message. It does NOT mean the relay closed, the door unlocked or
// the thermostat moved — only the DEVICE reporting the resulting state back means that, and that
// is what `confirmed` is. An operator who believes a door is locked when it is not is precisely
// the failure this whole phase exists to prevent, so nothing here is allowed to imply success it
// has not been told about.
//
// The server owns every gate (read-only by default, admin only, declared commands only,
// server-side bounds, rate limit, no automatic retry). The UI's job is to make that weight
// visible and never to contradict it: when the server refuses, its words are shown verbatim,
// because "200 is outside the safe range 5..30 for this command" tells an operator what to do
// and "Bad request" does not.

// The confirmation poll. The backend's confirm timeout is 30s and its sweeper runs every 10s, so
// an unconfirmed command turns `failed` somewhere between 30s and 40s after it was sent. The
// window here is deliberately longer than that: the unconfirmed outcome is the single most
// important thing this page can tell an operator, and giving up before the server has ruled would
// leave them staring at a spinner instead of at the warning.
const POLL_MS = 2000;
const POLL_WINDOW_MS = 50000;

const KINDS = ['switch', 'setpoint', 'dimmer', 'position', 'cct', 'mode', 'color'];

// parseOptions reads a mode command's Options JSON ([{value,label}]) defensively — a malformed
// declaration yields an empty list, which the UI renders as "unconfigured" rather than crashing.
export function parseOptions(raw) {
  if (!raw) return [];
  try {
    const arr = JSON.parse(raw);
    return Array.isArray(arr) ? arr.filter((o) => o && o.value != null) : [];
  } catch {
    return [];
  }
}

// Colour crosses the wire as one packed integer 0xRRGGBB; the browser's colour input speaks hex.
export function hexToInt(hex) {
  const n = parseInt(String(hex).replace('#', ''), 16);
  return Number.isFinite(n) ? n : 0;
}
export function intToHex(v) {
  const n = Math.max(0, Math.min(0xffffff, Math.round(Number(v) || 0)));
  return `#${n.toString(16).padStart(6, '0')}`;
}

// describeValue renders a value the way its command MEANS it — a switch was turned on, a dimmer set
// to 40%, a colour to #FF8000 — so history, the confirm dialog and the twin all read in human terms.
export function describeValue(value, decl, t) {
  const kind = String(decl?.kind || '').toLowerCase();
  const v = Number(value);
  switch (kind) {
    case 'switch': return v === 1 ? t('cmd.on') : t('cmd.off');
    case 'dimmer':
    case 'position': return `${formatNumber(v)}%`;
    case 'cct': return `${formatNumber(v)} K`;
    case 'color': return intToHex(v);
    case 'mode': {
      const o = parseOptions(decl?.options).find((x) => Number(x.value) === v);
      return o ? o.label : formatNumber(v);
    }
    default: return formatNumber(v);
  }
}

// DeviceControl is the whole Control tab: what the device can be told, what it was told, and
// what it says is actually true.
//
// The controls that ISSUE a command are admin-only (the server 403s anyone else). The history
// and the twin are NOT: being able to see that somebody unlocked a door is a different power
// from being able to unlock it, and hiding the audit trail from the people who cannot write to
// it would mean only the people who could have done it can see what was done.
export function DeviceControl({ device, session, onToast }) {
  const t = useT();
  const isAdmin = !!session?.isAdmin;

  const [decls, setDecls] = useState([]);
  const [actuationEnabled, setActuationEnabled] = useState(false);
  const [busy, setBusy] = useState(true);
  const [error, setError] = useState('');

  const [history, setHistory] = useState([]);
  const [historyBusy, setHistoryBusy] = useState(true);
  const [historyError, setHistoryError] = useState('');

  const [twin, setTwin] = useState([]);
  const [twinError, setTwinError] = useState('');

  // The command in flight: the row the server gave back, plus whether we are still waiting for
  // the device to have its say.
  const [issued, setIssued] = useState(null);
  const [waiting, setWaiting] = useState(false);
  const [timedOut, setTimedOut] = useState(false);
  // refusal is a 400 from the server — the command was never sent. Its text is the server's,
  // shown exactly as written.
  const [refusal, setRefusal] = useState('');

  const [confirming, setConfirming] = useState(null);
  const [sending, setSending] = useState(false);

  const loadCommands = useCallback(async () => {
    setBusy(true);
    const r = await api(`/api/devices/${device.id}/commands`);
    if (r.ok) {
      setDecls(r.body?.items || []);
      setActuationEnabled(!!r.body?.actuationEnabled);
      setError('');
    } else {
      setError(errorMessage(r, t('cmd.loadFailed')));
    }
    setBusy(false);
  }, [device.id, t]);

  const loadHistory = useCallback(async () => {
    setHistoryBusy(true);
    const r = await api(`/api/devices/${device.id}/commands/history?limit=100`);
    if (r.ok) {
      setHistory(r.body?.items || []);
      setHistoryError('');
    } else {
      setHistoryError(errorMessage(r, t('cmd.historyFailed')));
    }
    setHistoryBusy(false);
  }, [device.id, t]);

  const loadTwin = useCallback(async () => {
    const r = await api(`/api/devices/${device.id}/twin`);
    if (r.ok) {
      setTwin(r.body?.items || []);
      setTwinError('');
    } else {
      setTwinError(errorMessage(r, t('twin.failed')));
    }
  }, [device.id, t]);

  useEffect(() => { loadCommands(); loadHistory(); loadTwin(); }, [loadCommands, loadHistory, loadTwin]);

  // The confirmation poll. It watches the audit trail — the same rows anybody else would see —
  // rather than a private channel, so what the operator is shown is what actually happened.
  useEffect(() => {
    if (!waiting || !issued?.id) return undefined;
    const id = issued.id;
    const deadline = Date.now() + POLL_WINDOW_MS;
    let cancelled = false;
    let timer = 0;

    async function tick() {
      const r = await api(`/api/devices/${device.id}/commands/history?limit=100`);
      if (cancelled) return;
      if (r.ok) {
        const items = r.body?.items || [];
        setHistory(items);
        const row = items.find((x) => x.id === id);
        if (row) {
          setIssued(row);
          if (row.status === 'confirmed' || row.status === 'failed') {
            setWaiting(false);
            loadTwin();
            return;
          }
        }
      }
      if (Date.now() >= deadline) {
        // The server has not ruled yet. We do NOT call this a success, and we do not resend.
        setWaiting(false);
        setTimedOut(true);
        loadTwin();
        return;
      }
      timer = setTimeout(tick, POLL_MS);
    }

    timer = setTimeout(tick, POLL_MS);
    return () => { cancelled = true; clearTimeout(timer); };
  }, [waiting, issued?.id, device.id, loadTwin]);

  async function send(decl, value) {
    setSending(true);
    const r = await api(`/api/devices/${device.id}/commands`, {
      method: 'POST',
      body: JSON.stringify({ name: decl.name, value: Number(value) }),
    });
    setSending(false);
    setConfirming(null);
    if (!r.ok) {
      // A refusal is not a bug to be paraphrased. The server wrote its message to be read by
      // the person who tried — show it, and reload the history, because a refused command is
      // still an audit row ("somebody tried to unlock the front door at 03:00").
      setRefusal(errorMessage(r, t('cmd.issueFailed')));
      setIssued(null);
      setWaiting(false);
      setTimedOut(false);
      loadHistory();
      return;
    }
    setRefusal('');
    setTimedOut(false);
    setIssued(r.body || null);
    setWaiting(true);
    loadHistory();
  }

  const declByName = useMemo(() => {
    const map = {};
    decls.forEach((d) => { map[String(d.name).toLowerCase()] = d; });
    return map;
  }, [decls]);

  const canIssue = isAdmin && actuationEnabled && !!device.enabled;

  return (
    <div className="iot-control">
      <div className="toolbar">
        <div>
          <h3 className="section-subtitle">{t('cmd.title')}</h3>
          <p className="settings-hint">{t('cmd.hint')}</p>
        </div>
        <button type="button" className="quiet" onClick={() => { loadCommands(); loadHistory(); loadTwin(); }} disabled={busy}>
          <span className="btn-icon"><Ico n="refresh" sz={14} /> {t('common.refresh')}</span>
        </button>
      </div>

      {error ? <div className="status-line">{error}</div> : null}

      {/* READ-ONLY BY DEFAULT. A device is not commandable because somebody plugged it in — an
          admin has to say so. When that switch is off, say so, and say where it lives. */}
      {!busy && !actuationEnabled ? (
        <div className="iot-actuation-off">
          <Ico n="lock" sz={18} />
          <div>
            <strong>{t('cmd.offTitle')}</strong>
            <p>{t('cmd.offBody')}</p>
          </div>
        </div>
      ) : null}

      {!busy && actuationEnabled && !device.enabled ? (
        <div className="iot-actuation-off">
          <Ico n="lock" sz={18} />
          <div>
            <strong>{t('cmd.deviceOffTitle')}</strong>
            <p>{t('cmd.deviceOffBody')}</p>
          </div>
        </div>
      ) : null}

      {!busy && !isAdmin ? <p className="settings-hint iot-cmd-viewer-note">{t('cmd.adminOnly')}</p> : null}

      {!busy && decls.length === 0 ? (
        <EmptyState text={device.profileId ? t('cmd.none') : t('cmd.noProfile')} />
      ) : (
        <div className="iot-cmd-grid">
          {decls.map((d) => (
            <CommandControl
              key={d.id || d.name}
              decl={d}
              canIssue={canIssue}
              onAsk={(value) => { setRefusal(''); setConfirming({ decl: d, value }); }}
            />
          ))}
        </div>
      )}

      {refusal ? (
        <Outcome
          kind="refused"
          title={t('cmd.refused')}
          body={refusal}
          onDismiss={() => setRefusal('')}
        />
      ) : null}

      {issued ? (
        <IssuedOutcome
          cmd={issued}
          waiting={waiting}
          timedOut={timedOut}
          decl={declByName[String(issued.name).toLowerCase()]}
          onDismiss={() => { setIssued(null); setWaiting(false); setTimedOut(false); }}
        />
      ) : null}

      <TwinTable rows={twin} error={twinError} onRefresh={loadTwin} />

      <CommandHistory
        rows={history}
        busy={historyBusy}
        error={historyError}
        declByName={declByName}
        session={session}
      />

      {confirming ? (
        <ConfirmCommandModal
          device={device}
          decl={confirming.decl}
          value={confirming.value}
          busy={sending}
          onCancel={() => setConfirming(null)}
          onConfirm={() => send(confirming.decl, confirming.value)}
        />
      ) : null}
    </div>
  );
}

// CommandControl is one declared command, rendered as what it IS: a switch is two states, a
// setpoint is a number inside a range. Neither fires anything by itself — both hand the value to
// the confirmation dialog, because there is no such thing as an accidental door unlock here.
function CommandControl({ decl, canIssue, onAsk }) {
  const t = useT();
  const kind = KINDS.includes(String(decl.kind).toLowerCase()) ? String(decl.kind).toLowerCase() : 'switch';
  const min = Number(decl.min) || 0;
  const max = Number(decl.max) || 0;
  const options = useMemo(() => parseOptions(decl.options), [decl.options]);
  // A setpoint/cct with no range, or a mode with no options, is not "anything goes" — the server
  // refuses EVERY value. Saying so here is the difference between a command that mysteriously never
  // works and a device type that needs fixing.
  const noRange = (kind === 'setpoint' || kind === 'cct') && min === 0 && max === 0;
  const noOptions = kind === 'mode' && options.length === 0;
  const unconfigured = noRange || noOptions;

  const [value, setValue] = useState(() => {
    if (kind === 'setpoint' || kind === 'cct') return String(min);
    if (kind === 'dimmer' || kind === 'position') return '50';
    if (kind === 'mode') return options.length ? String(options[0].value) : '0';
    if (kind === 'color') return '#ffffff';
    return '0';
  });

  const blocked = !canIssue || unconfigured;
  const submit = () => onAsk(kind === 'color' ? hexToInt(value) : Number(value));

  let control;
  if (kind === 'switch') {
    control = (
      <div className="iot-cmd-switch">
        <button type="button" disabled={blocked} onClick={() => onAsk(1)}>
          <span className="btn-icon"><Ico n="check-ok" sz={14} /> {t('cmd.switchOn')}</span>
        </button>
        <button type="button" className="quiet" disabled={blocked} onClick={() => onAsk(0)}>
          <span className="btn-icon"><Ico n="stop" sz={14} /> {t('cmd.switchOff')}</span>
        </button>
      </div>
    );
  } else if (kind === 'dimmer' || kind === 'position' || kind === 'cct') {
    // A slider: percent for dimmer/position, Kelvin for cct. min/max are a CONVENIENCE — the
    // server refuses an out-of-range value however it was typed, pasted or scripted.
    const lo = kind === 'cct' ? min : 0;
    const hi = kind === 'cct' ? max : 100;
    const unit = kind === 'cct' ? 'K' : '%';
    control = (
      <div className="iot-cmd-slider">
        <input type="range" min={lo} max={hi} step="1" value={value} disabled={blocked}
          onChange={(e) => setValue(e.target.value)} />
        <span className="iot-cmd-readout">{formatNumber(Number(value))}{unit}</span>
        <button type="button" disabled={blocked} onClick={submit}>
          <span className="btn-icon"><Ico n="send" sz={14} /> {t('cmd.sendShort')}</span>
        </button>
      </div>
    );
  } else if (kind === 'mode') {
    control = (
      <div className="iot-cmd-setpoint">
        <Field label={t('cmd.value')}>
          <select value={value} disabled={blocked} onChange={(e) => setValue(e.target.value)}>
            {options.map((o) => <option key={o.value} value={o.value}>{o.label || o.value}</option>)}
          </select>
        </Field>
        <button type="button" disabled={blocked} onClick={submit}>
          <span className="btn-icon"><Ico n="send" sz={14} /> {t('cmd.sendShort')}</span>
        </button>
      </div>
    );
  } else if (kind === 'color') {
    control = (
      <div className="iot-cmd-slider">
        <input type="color" value={value} disabled={blocked} onChange={(e) => setValue(e.target.value)} />
        <span className="iot-cmd-readout">{intToHex(hexToInt(value))}</span>
        <button type="button" disabled={blocked} onClick={submit}>
          <span className="btn-icon"><Ico n="send" sz={14} /> {t('cmd.sendShort')}</span>
        </button>
      </div>
    );
  } else {
    // setpoint — a bare number inside a range.
    control = (
      <div className="iot-cmd-setpoint">
        <Field label={t('cmd.value')} hint={noRange ? '' : t('cmd.range', { min: formatNumber(min), max: formatNumber(max) })}>
          <input type="number" step="any" min={min} max={max} value={value} disabled={blocked}
            onChange={(e) => setValue(e.target.value)} />
        </Field>
        <button type="button" disabled={blocked || value === ''} onClick={submit}>
          <span className="btn-icon"><Ico n="send" sz={14} /> {t('cmd.sendShort')}</span>
        </button>
      </div>
    );
  }

  return (
    <div className="iot-cmd-card">
      <div className="iot-cmd-head">
        <span className="iot-cmd-label">{decl.label || decl.name}</span>
        <span className="iot-cmd-kind">{t(`cmdKind.${kind}`)}</span>
      </div>

      {control}

      {noRange ? (
        <p className="iot-cmd-warn"><Ico n="warning" sz={13} /> {t('cmd.noSafeRange')}</p>
      ) : null}
      {noOptions ? (
        <p className="iot-cmd-warn"><Ico n="warning" sz={13} /> {t('cmd.noOptions')}</p>
      ) : null}
      {!decl.confirmKey ? (
        <p className="iot-cmd-warn"><Ico n="warning" sz={13} /> {t('cmd.noConfirmKey')}</p>
      ) : null}
    </div>
  );
}

// ConfirmCommandModal is the gate every command goes through. It names the device, the command
// and the value, and it says plainly what will physically happen. There is deliberately no
// "don't ask again": the day the dialog becomes a formality is the day somebody unlocks the
// wrong door with a stray click.
function ConfirmCommandModal({ device, decl, value, busy, onCancel, onConfirm }) {
  const t = useT();
  const kind = String(decl.kind).toLowerCase();
  const label = decl.label || decl.name;
  const question = kind === 'switch'
    ? t('cmd.confirmSwitch', {
      command: label,
      device: device.name,
      state: Number(value) === 1 ? t('cmd.on') : t('cmd.off'),
    })
    : t('cmd.confirmSetpoint', { command: label, device: device.name, value: describeValue(value, decl, t) });

  return (
    <Modal title={t('cmd.confirmTitle')} onClose={busy ? undefined : onCancel} danger>
      <p className="iot-cmd-question">{question}</p>
      <div className="iot-cmd-consequence">
        <Ico n="warning" sz={18} />
        <p>{t('cmd.confirmPhysical')}</p>
      </div>
      {!decl.confirmKey ? <p className="field-hint">{t('cmd.confirmNoKey')}</p> : null}
      <div className="modal-actions">
        <button type="button" className="quiet" onClick={onCancel} disabled={busy}>{t('common.cancel')}</button>
        <button type="button" className="danger-solid" onClick={onConfirm} disabled={busy}>
          {busy ? t('cmd.sending') : t('cmd.send')}
        </button>
      </div>
    </Modal>
  );
}

// IssuedOutcome is the life of the command that was just sent — and the three states it can end
// in are rendered as three visibly different things, because they mean three different things.
function IssuedOutcome({ cmd, waiting, timedOut, decl, onDismiss }) {
  const t = useT();
  const shown = valueText(cmd.value, decl, t);

  if (waiting || cmd.status === 'pending' || (cmd.status === 'sent' && !timedOut)) {
    return (
      <Outcome
        kind="sent"
        icon="reload"
        spin
        title={t('cmd.sentWaiting')}
        body={t('cmd.sentWaitingBody')}
        meta={t('cmd.outcomeMeta', { command: decl?.label || cmd.name, value: shown })}
      />
    );
  }
  if (cmd.status === 'confirmed') {
    return (
      <Outcome
        kind="confirmed"
        icon="check-ok"
        title={t('cmd.confirmedTitle')}
        body={t('cmd.confirmedBody', { time: formatTimestamp(cmd.confirmedAt) })}
        meta={t('cmd.outcomeMeta', { command: decl?.label || cmd.name, value: shown })}
        onDismiss={onDismiss}
      />
    );
  }
  if (cmd.status === 'failed') {
    // The error is the server's, and it is shown WHOLE. The timeout one — "the device never
    // reported the new state — it may or may not have acted. Not retried automatically:
    // re-sending could act twice." — is the entire safety design in one sentence. Truncating it
    // would throw away the only thing that tells an operator not to just click it again.
    return (
      <Outcome
        kind="failed"
        icon="warning"
        title={t('cmd.failedTitle')}
        body={cmd.error || t('cmd.failedNoReason')}
        meta={t('cmd.outcomeMeta', { command: decl?.label || cmd.name, value: shown })}
        onDismiss={onDismiss}
      />
    );
  }
  // Still 'sent' after the whole window: the server has not ruled, so neither do we.
  return (
    <Outcome
      kind="unconfirmed"
      icon="warning"
      title={t('cmd.notConfirmedTitle')}
      body={t('cmd.notConfirmedBody')}
      meta={t('cmd.outcomeMeta', { command: decl?.label || cmd.name, value: shown })}
      onDismiss={onDismiss}
    />
  );
}

function Outcome({ kind, icon = 'warning', spin = false, title, body, meta, onDismiss }) {
  const t = useT();
  return (
    <div className={`iot-outcome is-${kind}`} role="status" aria-live="polite">
      <span className={`iot-outcome-icon${spin ? ' is-spinning' : ''}`}><Ico n={icon} sz={18} /></span>
      <div className="iot-outcome-body">
        <strong>{title}</strong>
        <p>{body}</p>
        {meta ? <span className="iot-outcome-meta">{meta}</span> : null}
      </div>
      {onDismiss ? (
        <button type="button" className="quiet" onClick={onDismiss}>{t('cmd.dismiss')}</button>
      ) : null}
    </div>
  );
}

// TwinTable is desired vs reported: what was asked for, and what the device says is actually
// true. The disagreements are the point.
//
// An EXPIRED disagreement is shown, not hidden. "What you asked for never took effect and will
// not now be applied" is information an operator needs — a door that was told to unlock and never
// did is not a tidy row to sweep away.
function TwinTable({ rows, error, onRefresh }) {
  const t = useT();
  const now = Math.floor(Date.now() / 1000);

  return (
    <div className="iot-twin">
      <div className="toolbar">
        <div>
          <h3 className="section-subtitle">{t('twin.title')}</h3>
          <p className="settings-hint">{t('twin.hint')}</p>
        </div>
        <button type="button" className="quiet" onClick={onRefresh}>
          <span className="btn-icon"><Ico n="refresh" sz={14} /> {t('common.refresh')}</span>
        </button>
      </div>

      {error ? <div className="status-line">{error}</div> : null}

      {rows.length === 0 ? (
        <EmptyState text={t('twin.empty')} />
      ) : (
        <table className="iot-twin-table">
          <thead>
            <tr>
              <th>{t('twin.key')}</th>
              <th>{t('twin.desired')}</th>
              <th>{t('twin.reported')}</th>
              <th>{t('twin.state')}</th>
            </tr>
          </thead>
          <tbody>
            {rows.map((r) => {
              const state = twinState(r, now);
              return (
                <tr key={`${r.deviceId}-${r.key}`}>
                  <td><code>{r.key}</code></td>
                  <td>
                    {r.hasDesired ? (
                      <span title={formatTimestamp(r.desiredAt)}>{formatNumber(r.desired)}</span>
                    ) : <span className="iot-twin-none">—</span>}
                  </td>
                  <td>
                    {r.hasReported ? (
                      <span title={formatTimestamp(r.reportedAt)}>
                        {formatNumber(r.reported)} <em>{formatAgo(r.reportedAt, t)}</em>
                      </span>
                    ) : <span className="iot-twin-none">{t('twin.neverReported')}</span>}
                  </td>
                  <td>
                    <span className={`iot-twin-pill is-${state}`}>{t(`twin.${state}`)}</span>
                    {state === 'expired' ? <p className="iot-twin-note">{t('twin.expiredHint')}</p> : null}
                    {state === 'pending' ? <p className="iot-twin-note">{t('twin.pendingHint')}</p> : null}
                  </td>
                </tr>
              );
            })}
          </tbody>
        </table>
      )}
    </div>
  );
}

// twinState is the honest reading of one twin row.
//
//   inSync        the device reports what was asked (or nothing was ever asked and it just reports)
//   pending       asked, not yet reported, and the request is still live
//   expired       asked, never reported, and the request has run out — it will NOT be applied now
//   neverReported the device has never said anything about this key
function twinState(r, now) {
  if (!r.hasDesired) return r.hasReported ? 'reportedOnly' : 'neverReported';
  if (r.hasReported && r.reported === r.desired) return 'inSync';
  const expiresAt = Number(r.desiredExpiresAt) || 0;
  if (expiresAt && expiresAt < now) return 'expired';
  return 'pending';
}

// CommandHistory is the audit trail: who told this device to do what, when, and whether it
// actually happened. Visible to every role — see the note at the top of this file.
function CommandHistory({ rows, busy, error, declByName, session }) {
  const t = useT();

  const columns = [
    { key: 'requestedAt', label: t('cmd.when'), filterType: 'number', render: (v) => (
      <span title={formatTimestamp(v)}>{formatAgo(v, t)}</span>
    ) },
    { key: 'name', label: t('cmd.command'), render: (v) => {
      const decl = declByName[String(v).toLowerCase()];
      return decl?.label || v;
    } },
    { key: 'value', label: t('cmd.valueCol'), filterType: 'number', render: (v, row) => (
      valueText(v, declByName[String(row.name).toLowerCase()], t)
    ) },
    { key: 'status', label: t('cmd.status'), render: (v) => <CommandStatusPill status={v} /> },
    { key: 'requestedBy', label: t('cmd.by'), filterType: 'number', render: (v) => actorText(v, session, t) },
    { key: 'error', label: t('cmd.detail'), filterable: false, render: (v, row) => {
      if (v) return <span className="iot-cmd-error">{v}</span>;
      if (row.status === 'confirmed') {
        return <span className="iot-cmd-ok">{t('cmd.confirmedAt', { ago: formatAgo(row.confirmedAt, t) })}</span>;
      }
      return <span className="iot-twin-none">—</span>;
    } },
  ];

  return (
    <div className="iot-cmd-history">
      <div className="toolbar">
        <div>
          <h3 className="section-subtitle">{t('cmd.historyTitle')}</h3>
          <p className="settings-hint">{t('cmd.historyHint')}</p>
        </div>
      </div>
      {error ? <div className="status-line">{error}</div> : null}
      <DataTable
        rows={rows}
        columns={columns}
        busy={busy}
        pageSize={10}
        pageSizeOptions={[10, 25, 50]}
        emptyText={t('cmd.historyEmpty')}
      />
    </div>
  );
}

// CommandStatusPill renders the four statuses as four DIFFERENT things. `sent` is amber, not
// green: it is the status that means "we do not know yet", and colouring it as a success would
// be the exact lie this page exists to avoid.
export function CommandStatusPill({ status }) {
  const t = useT();
  const code = ['pending', 'sent', 'confirmed', 'failed'].includes(status) ? status : 'pending';
  return <span className={`iot-cmd-pill is-${code}`}>{t(`cmdStatus.${code}`)}</span>;
}

// valueText renders what was asked for the way the command MEANS it: a switch was turned on, it
// was not set to the number 1.
function valueText(value, decl, t) {
  return describeValue(value, decl, t);
}

// actorText names the person. An audit row with no name attached to it is not an audit trail —
// so an unattributed command says so rather than rendering a bare zero.
function actorText(id, session, t) {
  const n = Number(id) || 0;
  if (!n) return t('cmd.actorSystem');
  if (session?.id === n) return session.username || t('cmd.actorYou');
  return t('cmd.actorId', { id: n });
}
