import { useEffect, useRef, useState } from 'react';
import { Ico } from './icons';
import { useT } from './i18n';
import './styles/toast.css';

// Toast / ToastStack: the shared transient-notification system for the RBAC apps.
// `kind` drives the accent — 'success' (green), 'error' (red), or 'info' (neutral).
// Newest on top; auto-dismisses with a leave animation; hover pauses the timer.
// Each app keeps its own `toasts` state + `pushToast(text, kind)` and renders one
// <ToastStack/> at the app root.

function Toast({ id, text, kind = 'info', duration = 3500, onDismiss }) {
  const t = useT();
  const [leaving, setLeaving] = useState(false);
  const timer = useRef(null);
  const start = () => {
    clearTimeout(timer.current);
    timer.current = window.setTimeout(() => setLeaving(true), duration);
  };
  useEffect(() => {
    start();
    return () => clearTimeout(timer.current);
    // eslint-disable-next-line
  }, []);
  return (
    <div
      className={`toast toast-${kind}${leaving ? ' toast--leaving' : ''}`}
      role="status"
      aria-live="polite"
      onMouseEnter={() => clearTimeout(timer.current)}
      onMouseLeave={start}
      onTransitionEnd={() => { if (leaving) onDismiss(id); }}
    >
      <span className="toast-text">{text}</span>
      <button type="button" className="toast-close" aria-label={t('common.dismiss')} onClick={() => setLeaving(true)}>
        <Ico n="x" sz={13} />
      </button>
    </div>
  );
}

export function ToastStack({ toasts, onDismiss }) {
  if (!toasts || toasts.length === 0) return null;
  return (
    <div className="toast-stack" aria-live="polite">
      {toasts.map((t) => (
        <Toast key={t.id} id={t.id} text={t.text} kind={t.kind} onDismiss={onDismiss} />
      ))}
    </div>
  );
}
