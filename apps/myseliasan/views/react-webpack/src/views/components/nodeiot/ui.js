import { useEffect, useState } from 'react';
import { Ico, useT } from '@shared';

// myiotsan's UI primitives, ported verbatim into the embed so the node's pages look exactly
// like the node's pages. The theme dropdown is deliberately NOT here: the embed lives inside
// myseliasan's shell and follows myseliasan's theme (the wrapper mirrors it onto the scope).

export function FormBusyOverlay({ busy }) {
  const tr = useT();
  if (!busy) return null;
  return (
    <div className="form-busy-overlay" aria-live="polite" aria-label={tr('ui.loading')}>
      <div className="form-busy-spinner" />
    </div>
  );
}

export function Message({ value, floating = false }) {
  if (!value) return null;
  return <div className={`status-line${floating ? ' status-line--floating' : ''}`}>{value}</div>;
}

// Panel is the standard workspace surface: a titled card with an optional hint line
// and header actions. Every page in the app is built out of these, so the section
// chrome (heading size, hint colour, action row) is defined once.
export function Panel({ icon, title, hint, actions, children, className = '', wide = true }) {
  return (
    <section className={`settings-panel${wide ? ' span-two' : ''}${className ? ` ${className}` : ''}`}>
      {(title || actions) ? (
        <header>
          <h2><span className="btn-icon">{icon ? <Ico n={icon} /> : null} {title}</span></h2>
          {actions ? <div className="settings-header-actions">{actions}</div> : null}
        </header>
      ) : null}
      {hint ? <p className="settings-hint">{hint}</p> : null}
      {children}
    </section>
  );
}

// EmptyState is the honest "there is nothing here" body. The app NEVER draws sample
// data to fill a panel — an empty estate has to look empty.
export function EmptyState({ text, icon = 'info' }) {
  return (
    <div className="phase-empty">
      <Ico n={icon} sz={18} />
      <p>{text}</p>
    </div>
  );
}

// AccessDenied is what a 403 from the node looks like, and it exists because the alternative
// is worse.
//
// A myseliasan user's grant on this node (viewer / operator / admin) is resolved at the node,
// against the node's own permission matrix — the browser is never told which one it got. So
// when a panel is refused, the embed does not blank itself out, draw an empty table, or
// pretend the estate has nothing in it: it says, in the operator's own language, that THEIR
// access to THIS node does not stretch this far, and what would fix it. An empty widget would
// have them chasing a broken hub; this has them asking for the grant they need.
export function AccessDenied({ text, hint }) {
  const t = useT();
  return (
    <div className="iot-actuation-off" role="status">
      <Ico n="lock" sz={18} />
      <div>
        <strong>{text || t('niot.forbidden')}</strong>
        <p>{hint || t('niot.forbiddenHint')}</p>
      </div>
    </div>
  );
}

// Modal is the shared dialog shell (backdrop + card). `danger` tints it for the
// destructive confirmations.
export function Modal({ title, onClose, children, danger = false, wide = false }) {
  useEffect(() => {
    function onKey(e) { if (e.key === 'Escape' && onClose) onClose(); }
    document.addEventListener('keydown', onKey);
    return () => document.removeEventListener('keydown', onKey);
  }, [onClose]);
  return (
    <div className="modal-backdrop" role="dialog" aria-modal="true" aria-label={title}>
      <div className={`modal-card${danger ? ' danger-modal' : ''}${wide ? ' iot-modal-wide' : ''}`}>
        {title ? <h2>{title}</h2> : null}
        {children}
      </div>
    </div>
  );
}

// ConfirmModal asks before a destructive act (delete a device, rotate a credential).
export function ConfirmModal({ title, body, confirmLabel, onConfirm, onCancel, busy, danger = true }) {
  const t = useT();
  return (
    <Modal title={title} onClose={onCancel} danger={danger}>
      <p>{body}</p>
      <div className="modal-actions">
        <button type="button" className="quiet" onClick={onCancel} disabled={busy}>{t('common.cancel')}</button>
        <button type="button" className={danger ? 'danger-solid' : ''} onClick={onConfirm} disabled={busy}>
          {busy ? t('common.saving') : confirmLabel}
        </button>
      </div>
    </Modal>
  );
}

// Field is one labelled form control. `hint` is where the app explains what a setting
// MEANS (deadband, debounce, cooldown) rather than leaving an operator to guess.
export function Field({ label, hint, children, span = false, required = false }) {
  return (
    <label className={`field-label${span ? ' field-span-two' : ''}`}>
      <span>{label}{required ? ' *' : ''}</span>
      {children}
      {hint ? <span className="field-hint">{hint}</span> : null}
    </label>
  );
}

// CheckRow is a checkbox with its label to the right (a toggle, not a form field).
export function CheckRow({ checked, onChange, label, hint, disabled }) {
  return (
    <div className="iot-check-row">
      <label>
        <input type="checkbox" checked={!!checked} disabled={disabled} onChange={(e) => onChange(e.target.checked)} />
        <span>{label}</span>
      </label>
      {hint ? <p className="field-hint">{hint}</p> : null}
    </div>
  );
}

// HealthPill renders a device's reachability. `unknown` is a real state — a device that
// has never reported is not "offline", it has never been heard from at all.
export function HealthPill({ health }) {
  const t = useT();
  const code = health === 'online' || health === 'offline' ? health : 'unknown';
  return <span className={`status-pill ${code}`}>{t(`health.${code}`)}</span>;
}

// SeverityPill colours an alert by urgency, using the same three levels the rules use.
export function SeverityPill({ severity }) {
  const t = useT();
  const code = ['critical', 'warning', 'info'].includes(severity) ? severity : 'info';
  return <span className={`iot-sev iot-sev--${code}`}>{t(`severity.${code}`)}</span>;
}

// CopyButton copies a value to the clipboard. It falls back to a hidden textarea +
// execCommand because the async Clipboard API is unavailable on plain-HTTP origins —
// which is exactly how this air-gapped suite is often served on a LAN.
export function CopyButton({ value, label, onCopied }) {
  const t = useT();
  const [done, setDone] = useState(false);
  async function copy() {
    let ok = false;
    try {
      if (navigator.clipboard && window.isSecureContext) {
        await navigator.clipboard.writeText(value);
        ok = true;
      }
    } catch (_) { ok = false; }
    if (!ok) {
      try {
        const ta = document.createElement('textarea');
        ta.value = value;
        ta.setAttribute('readonly', '');
        ta.style.position = 'fixed';
        ta.style.opacity = '0';
        document.body.appendChild(ta);
        ta.select();
        ok = document.execCommand('copy');
        document.body.removeChild(ta);
      } catch (_) { ok = false; }
    }
    if (ok) {
      setDone(true);
      setTimeout(() => setDone(false), 2000);
      if (onCopied) onCopied();
    }
  }
  return (
    <button type="button" className="quiet" onClick={copy}>
      <span className="btn-icon">
        <Ico n={done ? 'check-ok' : 'copy'} sz={14} />
        {done ? t('common.copied') : (label || t('common.copy'))}
      </span>
    </button>
  );
}
