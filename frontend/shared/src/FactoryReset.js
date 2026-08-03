import { useCallback, useEffect, useRef, useState } from 'react';
import { Ico } from './icons';
import { useT } from './i18n';
import './styles/factory-reset.css';

// The shared factory-reset UI for myseliasan, myidsan and myiotsan: a Danger Zone
// panel, a typed confirmation dialog, and a blocking progress overlay.
//
// The confirmation is GitHub-style — the operator types the app's own name before the
// button enables. That is deliberately different from mymatasan's older reset, which
// auto-proceeds when a ten second countdown runs out: a destructive action that
// completes by inaction can fire because somebody was called away mid-dialog, and it
// cannot tell "meant it" from "walked away". Typing the name is a positive act, and
// using the app's OWN name means muscle memory from one console cannot carry a
// reflexive confirmation into another app's dialog.
//
// The phrase comes from the server (GET /system/reset/state) rather than being written
// in here, so the text the operator is asked for and the text the server checks can
// never drift apart.
//
// Each app passes its own `api` function, since their request helpers differ.

// SENTINEL marks where the phrase sits inside the translated instruction, so it can be
// rendered as a real <code> element without building HTML from a string, and without
// concatenating fragments (which would lose the word order of ms/zh/ar).
const SENTINEL = '@@PHRASE@@';

// FactoryResetSection is the Danger Zone. It renders nothing until the server says the
// reset is available (bootstrap.allowReset), so an install that has not opted in never
// shows an operator a button they cannot use.
export function FactoryResetSection({ api, appLabel, onToast }) {
  const t = useT();
  const [state, setState] = useState(null); // { allowed, confirmPhrase }
  const [dialogOpen, setDialogOpen] = useState(false);
  const [progress, setProgress] = useState(null);

  useEffect(() => {
    let alive = true;
    api('/api/system/reset/state')
      .then((r) => { if (alive && r && r.ok) setState(r.body || null); })
      .catch(() => { /* unavailable: the section stays hidden */ });
    return () => { alive = false; };
  }, [api]);

  const start = useCallback(async (confirm) => {
    const r = await api('/api/system/reset', {
      method: 'POST',
      body: JSON.stringify({ confirm }),
    }).catch(() => ({ ok: false }));
    if (!r || !r.ok) {
      onToast && onToast((r && r.message) || t('reset.startFailed'), 'error');
      return false;
    }
    setDialogOpen(false);
    setProgress(r.body || { running: true, percent: 0 });
    return true;
  }, [api, onToast, t]);

  if (!state || !state.allowed) return null;

  return (
    <>
      <section className="fr-zone">
        <div className="fr-zone-head">
          <h3><span className="btn-icon"><Ico n="warning" sz={15} /> {t('reset.zoneTitle')}</span></h3>
        </div>
        <p className="fr-zone-body">{t('reset.zoneBody', { app: appLabel })}</p>
        <button type="button" className="fr-danger-btn" onClick={() => setDialogOpen(true)}>
          <span className="btn-icon"><Ico n="trash" sz={14} /> {t('reset.button')}</span>
        </button>
      </section>

      {dialogOpen ? (
        <FactoryResetDialog
          phrase={state.confirmPhrase}
          appLabel={appLabel}
          onCancel={() => setDialogOpen(false)}
          onConfirm={start}
        />
      ) : null}

      {progress ? <FactoryResetOverlay api={api} initial={progress} /> : null}
    </>
  );
}

// FactoryResetDialog asks the operator to type the confirmation phrase. The button stays
// disabled until it matches exactly, and Cancel is focused so the safe action is one
// keypress away.
export function FactoryResetDialog({ phrase, appLabel, onCancel, onConfirm }) {
  const t = useT();
  const [typed, setTyped] = useState('');
  const [busy, setBusy] = useState(false);
  const matches = typed.trim() === phrase;

  const submit = async (event) => {
    event.preventDefault();
    if (!matches || busy) return;
    setBusy(true);
    const ok = await onConfirm(typed.trim());
    if (!ok) setBusy(false); // stay open so the operator can read the failure
  };

  const instruction = t('reset.typeToConfirm', { phrase: SENTINEL }).split(SENTINEL);

  return (
    <div className="fr-backdrop">
      <div className="fr-card" role="alertdialog" aria-modal="true" aria-labelledby="fr-title">
        <h2 id="fr-title"><span className="btn-icon"><Ico n="warning" sz={16} /> {t('reset.dialogTitle')}</span></h2>
        <p>{t('reset.dialogBody', { app: appLabel })}</p>
        <ul className="fr-losses">
          <li>{t('reset.lossData')}</li>
          <li>{t('reset.lossAccounts')}</li>
          <li>{t('reset.lossSecrets')}</li>
        </ul>
        <p className="fr-irreversible">{t('reset.irreversible')}</p>
        <form onSubmit={submit}>
          <label className="fr-field">
            <span>{instruction[0]}<code>{phrase}</code>{instruction[1] || ''}</span>
            <input
              value={typed}
              onChange={(e) => setTyped(e.target.value)}
              autoComplete="off"
              autoCapitalize="off"
              autoCorrect="off"
              spellCheck={false}
              disabled={busy}
              aria-label={t('reset.typeAria', { phrase })}
            />
          </label>
          <div className="fr-actions">
            <button type="button" className="fr-quiet" autoFocus onClick={onCancel} disabled={busy}>
              {t('reset.cancel')}
            </button>
            <button type="submit" className="fr-danger-btn" disabled={!matches || busy}>
              {busy ? t('reset.starting') : t('reset.confirmButton')}
            </button>
          </div>
        </form>
      </div>
    </div>
  );
}

// FactoryResetOverlay blocks the page while the wipe runs and reports progress.
//
// Polling has to survive the server going away underneath it: the reset closes the
// database pool partway through and then restarts the process. So a failed poll is not
// an error — it means the wipe reached the part where this process stops answering — and
// the overlay switches to waiting for /health, reloading once the fresh process replies.
export function FactoryResetOverlay({ api, initial }) {
  const t = useT();
  const [progress, setProgress] = useState(initial || null);
  const [finalizing, setFinalizing] = useState(false);
  const finalizingRef = useRef(false);

  useEffect(() => {
    let alive = true;
    let timer = null;

    const enterFinalizing = () => {
      if (!finalizingRef.current) {
        finalizingRef.current = true;
        setFinalizing(true);
      }
    };

    const waitForHealth = async () => {
      if (!alive) return;
      const r = await api('/api/health', { noRedirect: true }).catch(() => ({ ok: false }));
      if (!alive) return;
      if (r && r.ok) {
        // The rebuilt install is a first-run one, so a full reload is the honest
        // landing: the old session went with the database that held it.
        window.location.reload();
        return;
      }
      timer = window.setTimeout(waitForHealth, 2000);
    };

    const poll = async () => {
      if (!alive) return;
      const r = await api('/api/system/reset/progress', { noRedirect: true }).catch(() => ({ ok: false }));
      if (!alive) return;
      if (r && r.ok && r.body) {
        setProgress(r.body);
        if (r.body.stage === 'restarting' || (!r.body.running && Number(r.body.percent) >= 100)) {
          enterFinalizing();
          timer = window.setTimeout(waitForHealth, 2000);
          return;
        }
        timer = window.setTimeout(poll, 1000);
        return;
      }
      // Unreachable: the pool is closed or the process is relaunching.
      enterFinalizing();
      timer = window.setTimeout(waitForHealth, 2000);
    };

    timer = window.setTimeout(poll, 800);
    return () => { alive = false; if (timer) window.clearTimeout(timer); };
  }, [api]);

  const pct = Math.max(0, Math.min(100, Number(progress && progress.percent) || 0));
  const failed = (progress && progress.stage === 'failed') || !!(progress && progress.error);

  return (
    <div className="fr-overlay">
      <div className="fr-overlay-card">
        {failed ? (
          <>
            <h2><span className="btn-icon"><Ico n="warning" sz={16} /> {t('reset.failedTitle')}</span></h2>
            <p className="fr-error">{(progress && progress.error) || t('reset.unknownError')}</p>
          </>
        ) : (
          <>
            <h2>{t('reset.overlayTitle')}</h2>
            <div className="fr-bar"><div className="fr-bar-fill" style={{ width: `${pct}%` }} /></div>
            <p className="fr-pct">{pct}%</p>
            <p className="fr-msg">
              {finalizing ? t('reset.finalizing') : ((progress && progress.message) || t('reset.working'))}
            </p>
            {progress && progress.warning ? (
              <p className="fr-warning"><span className="btn-icon"><Ico n="warning" sz={13} /></span> {progress.warning}</p>
            ) : null}
            <p className="fr-hint">{t('reset.doNotClose')}</p>
          </>
        )}
      </div>
    </div>
  );
}
