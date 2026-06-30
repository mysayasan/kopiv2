import { useEffect, useRef, useState } from 'react';
import { Ico } from './icons';
import { useT } from '@shared/i18n';

// SecureWipeCountdown is the confirmation modal for a factory reset. Per the chosen
// UX it auto-proceeds when the countdown reaches zero unless the user cancels;
// Cancel is focused by default so the safe action is one keypress away.
export function SecureWipeCountdown({ seconds = 10, onCancel, onProceed }) {
  const t = useT();
  const [remaining, setRemaining] = useState(seconds);
  const proceedRef = useRef(onProceed);
  proceedRef.current = onProceed;

  useEffect(() => {
    const started = Date.now();
    const id = setInterval(() => {
      const left = Math.max(0, seconds - Math.floor((Date.now() - started) / 1000));
      setRemaining(left);
      if (left <= 0) {
        clearInterval(id);
        if (proceedRef.current) proceedRef.current();
      }
    }, 250);
    return () => clearInterval(id);
  }, [seconds]);

  return (
    <div className="modal-backdrop">
      <div className="modal-card danger-modal" role="alertdialog" aria-modal="true">
        <h2><span className="btn-icon"><Ico n="warning" /> {t('sw.title')}</span></h2>
        <p>{t('sw.warning')}</p>
        <p className="wipe-countdown">{t('sw.countdown', { n: remaining })}</p>
        <div className="modal-actions">
          <button type="button" className="quiet" autoFocus onClick={onCancel}>
            <span className="btn-icon"><Ico n="x" /> {t('sw.cancel')}</span>
          </button>
          <button type="button" className="danger-solid" onClick={() => proceedRef.current && proceedRef.current()}>
            <span className="btn-icon"><Ico n="warning" /> {t('sw.wipeNow')}</span>
          </button>
        </div>
      </div>
    </div>
  );
}

// ResetProgressOverlay is the full-screen blocking layer shown while a reset runs.
// It reports the estimated completion percentage, then sits on "Restarting…" until
// the server comes back (the parent reloads the page once health returns).
export function ResetProgressOverlay({ progress, onDismiss }) {
  const t = useT();
  const stage = progress?.stage;
  const failed = stage === 'failed';
  const pct = Math.max(0, Math.min(100, Number(progress?.percent) || 0));
  return (
    <div className="reset-overlay">
      <div className="reset-overlay-card">
        {failed ? (
          <>
            <h2><span className="btn-icon"><Ico n="warning" /> {t('sw.resetFailed')}</span></h2>
            <p className="reset-error">{progress?.error || t('sw.unknownError')}</p>
            <div className="modal-actions">
              <button type="button" className="quiet" onClick={onDismiss}>
                <span className="btn-icon"><Ico n="x" /> {t('sw.dismiss')}</span>
              </button>
            </div>
          </>
        ) : (
          <>
            <h2><span className="btn-icon"><Ico n="warning" /> {t('sw.title')}</span></h2>
            <div className="reset-progress-bar"><div className="reset-progress-fill" style={{ width: `${pct}%` }} /></div>
            <p className="reset-progress-pct">{pct}%</p>
            <p className="reset-progress-msg">{progress?.message || t('sw.working')}</p>
            {progress?.warning ? (
              <p className="reset-warning"><span className="btn-icon"><Ico n="warning" /></span> {progress.warning}</p>
            ) : null}
            <p className="field-hint">{t('sw.doNotClose')}</p>
          </>
        )}
      </div>
    </div>
  );
}
