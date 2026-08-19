import { useEffect, useRef, useState } from 'react';
import { Ico } from './icons';
import { useT } from '@shared/i18n';
import { THEMES, THEME_ICONS, liveViewLayouts } from '../lib/constants';
import { parseTracks } from '../lib/helpers';

export function ThemeDropdown({ theme, onThemeChange }) {
  const tr = useT();
  const [open, setOpen] = useState(false);
  const wrapRef = useRef(null);
  useEffect(() => {
    if (!open) return;
    function onDown(e) {
      if (wrapRef.current && !wrapRef.current.contains(e.target)) setOpen(false);
    }
    document.addEventListener('mousedown', onDown);
    return () => document.removeEventListener('mousedown', onDown);
  }, [open]);
  return (
    <div className="theme-drop-wrap" ref={wrapRef}>
      <button
        type="button"
        className={`quiet theme-toggle${open ? ' active' : ''}`}
        onClick={() => setOpen((o) => !o)}
        aria-haspopup="listbox"
        aria-expanded={open}
      >
        <span className="btn-icon">
          <Ico n={THEME_ICONS[theme]} sz={13} />
          {tr('theme.label')}
          <Ico n="chev-down" sz={11} />
        </span>
      </button>
      {open && (
        <div className="theme-menu" role="listbox" aria-label={tr('theme.select')}>
          {THEMES.map((t) => (
            <button
              key={t}
              type="button"
              role="option"
              aria-selected={t === theme}
              className={`theme-menu-item${t === theme ? ' active' : ''}`}
              onClick={() => { onThemeChange(t); setOpen(false); }}
            >
              <Ico n={THEME_ICONS[t]} sz={14} /> {tr(`theme.${t}`)}
            </button>
          ))}
        </div>
      )}
    </div>
  );
}

export function LayoutDropdown({ layout, onLayout }) {
  const t = useT();
  const [open, setOpen] = useState(false);
  const wrapRef = useRef(null);
  useEffect(() => {
    if (!open) return;
    function onDown(e) {
      if (wrapRef.current && !wrapRef.current.contains(e.target)) setOpen(false);
    }
    document.addEventListener('mousedown', onDown);
    return () => document.removeEventListener('mousedown', onDown);
  }, [open]);
  const current = liveViewLayouts.find((o) => o.id === layout) || liveViewLayouts[0];
  const iconFor = (cols) => (cols <= 2 ? 'grid2' : 'grid4');
  return (
    <div className="layout-drop-wrap" ref={wrapRef}>
      <button
        type="button"
        className={`quiet layout-toggle${open ? ' active' : ''}`}
        onClick={() => setOpen((o) => !o)}
        aria-haspopup="listbox"
        aria-expanded={open}
      >
        <span className="btn-icon">
          <Ico n={iconFor(current.cols)} sz={13} />
          {current.label}
          <Ico n="chev-down" sz={11} />
        </span>
      </button>
      {open && (
        <div className="layout-menu" role="listbox" aria-label={t('layout.select')}>
          {liveViewLayouts.map((o) => (
            <button
              key={o.id}
              type="button"
              role="option"
              aria-selected={o.id === layout}
              className={`layout-menu-item${o.id === layout ? ' active' : ''}`}
              onClick={() => { onLayout(o.id); setOpen(false); }}
            >
              <Ico n={iconFor(o.cols)} sz={14} /> {o.label}
            </button>
          ))}
        </div>
      )}
    </div>
  );
}

// AccordionList + AccordionItem are the shared inline-accordion standard used by
// list editors (notification destinations, AI detection rules, …): a compact,
// always-visible summary row per item that expands to its full body when opened.
// The caller owns the open state (one-open-at-a-time, multi-open — its choice)
// and supplies the summary content and any trailing actions, so the same shell
// renders any kind of item. Keep new list editors on this component so they stay
// visually and behaviourally consistent.
export function AccordionList({ children, className }) {
  return <ul className={`accordion-list${className ? ` ${className}` : ''}`}>{children}</ul>;
}

// AccordionItem is one row. `summary` is the clickable header content (the
// chevron is added automatically); `actions` are controls shown at the row's
// trailing edge that stay accessible while collapsed (e.g. an Enabled toggle or
// Remove). `children` is the expanded body, rendered only when `open`.
export function AccordionItem({ open, onToggle, summary, actions, children, disabled }) {
  return (
    <li className={`accordion-item${open ? ' open' : ''}`}>
      <div className="accordion-summary">
        <button
          type="button"
          className="accordion-summary-main"
          onClick={onToggle}
          aria-expanded={open}
          disabled={disabled}
        >
          <span className="accordion-chevron"><Ico n={open ? 'chev-down' : 'arr-right'} /></span>
          {summary}
        </button>
        {actions ? <div className="accordion-actions">{actions}</div> : null}
      </div>
      {open ? <div className="accordion-body">{children}</div> : null}
    </li>
  );
}

export function FormBusyOverlay({ busy }) {
  if (!busy) return null;
  return (
    <div className="form-busy-overlay" aria-live="polite" aria-label="Loading">
      <div className="form-busy-spinner" />
    </div>
  );
}

// FormAlert is the in-form failure banner. Inline errors used to render as
// `<p class="field-hint danger-text">`, which read as another neutral hint — a
// rejected camera login looked identical to the informational line above it, so
// a wrong password was easy to miss. This states the failure explicitly (icon +
// red panel + title) and announces it to screen readers.
export function FormAlert({ title, message }) {
  if (!message && !title) return null;
  return (
    <div className="form-alert" role="alert">
      <span className="form-alert-icon"><Ico n="warning" sz={16} /></span>
      <span className="form-alert-body">
        {title ? <strong className="form-alert-title">{title}</strong> : null}
        {message ? <span className="form-alert-msg">{message}</span> : null}
      </span>
    </div>
  );
}

export function InfoButton({ text }) {
  return (
    <button type="button" className="info-button" title={text} aria-label={text}>
      i
    </button>
  );
}

export function FieldTitle({ children, info }) {
  return (
    <span className="label-row">
      <span>{children}</span>
      <InfoButton text={info} />
    </span>
  );
}

export function Tracks({ value }) {
  const tracks = parseTracks(value);
  if (!tracks.length) {
    return <span>-</span>;
  }
  return (
    <ul className="track-list">
      {tracks.map((track, idx) => (
        <li key={`${track.codec || 'track'}-${idx}`}>
          {track.mediaType || 'media'} / {track.codec || 'codec'} {track.clockRate ? `@ ${track.clockRate}` : ''}
        </li>
      ))}
    </ul>
  );
}

export function Message({ value, floating = false }) {
  if (!value) {
    return null;
  }
  return <div className={`status-line${floating ? ' status-line--floating' : ''}`}>{value}</div>;
}

// Toast/ToastStack now come from the shared UI module (frontend/shared/src/Toast.js)
// so mymatasan's transient notifications match the RBAC apps (myidsan, myseliasan).
// The shared component imports its own tokenized toast.css; mymatasan maps the
// required --ui-* tokens in app.css. Re-exported here so existing `./ui` import
// sites keep working unchanged. Each toast may carry a `kind` (success|error|info)
// for a colored accent; mymatasan's status messages default to neutral 'info'.
export { ToastStack } from '@shared/Toast';

