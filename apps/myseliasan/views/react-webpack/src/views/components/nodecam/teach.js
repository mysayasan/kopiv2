import { useState, useEffect, useCallback, useRef } from 'react';
import { Ico } from './icons';
import { Tabs } from '@shared/Tabs';
import { useT } from '@shared/i18n';
import { ZoneDrawingPreview } from './previews';
import { apiBase, cameraTitle } from './lib/helpers';

// TeachTab is the home of the camera-teaching experience, replacing the retired
// manual Training page. T1 shipped the wizard shell (name → kind → where); T2
// adds step 4, the teaching sessions: presence-gated auto-capture into the
// skill's dataset with a live filmstrip and a sample coach. Accuracy check and
// activation land in the following phases and show as locked steps.

// Wizard steps. `step` on the skill row is the resume point; locked steps are
// the roadmap (until their phase ships).
const WIZARD_STEPS = [
  { step: 1, labelKey: 'teach.stepName', icon: 'edit-2' },
  { step: 2, labelKey: 'teach.stepKind', icon: 'grid2' },
  { step: 3, labelKey: 'teach.stepWhere', icon: 'camera' },
  { step: 4, labelKey: 'teach.stepExamples', icon: 'video' },
  { step: 5, labelKey: 'teach.stepAccuracy', icon: 'search' },
  { step: 6, labelKey: 'teach.stepActivate', icon: 'play' },
];

const SKILL_TYPES = [
  { id: 'object', titleKey: 'teach.typeObject', descKey: 'teach.typeObjectDesc', icon: 'box' },
  { id: 'inspect', titleKey: 'teach.typeInspect', descKey: 'teach.typeInspectDesc', icon: 'check-ok' },
  { id: 'anomaly', titleKey: 'teach.typeAnomaly', descKey: 'teach.typeAnomalyDesc', icon: 'bell' },
];

// teachSlug mirrors the backend's class-slug rule: lowercase, single spaces.
function teachSlug(name) {
  return String(name || '').trim().toLowerCase().split(/\s+/).filter(Boolean).join(' ');
}

// downloadBase64 turns an API {filename, dataBase64} payload into a browser
// download without a round-trip through a blob URL server.
function downloadBase64(filename, dataBase64) {
  const bytes = Uint8Array.from(atob(dataBase64), (c) => c.charCodeAt(0));
  const url = URL.createObjectURL(new Blob([bytes], { type: 'application/octet-stream' }));
  const a = document.createElement('a');
  a.href = url;
  a.download = filename;
  document.body.appendChild(a);
  a.click();
  a.remove();
  URL.revokeObjectURL(url);
}

// ExportSkillDialog collects a passphrase (+ optional samples) and downloads a
// .mmskill package for the given skill.
function ExportSkillDialog({ api, skill, busy, setBusy, notify, onClose }) {
  const t = useT();
  const [passphrase, setPassphrase] = useState('');
  const [includeImages, setIncludeImages] = useState(true);

  async function doExport() {
    if (passphrase.trim().length < 8) { notify(t('teach.pkgPassTooShort'), 'error'); return; }
    setBusy(true);
    try {
      const result = await api(`/api/teach/skills/${skill.id}/export`, {
        method: 'POST',
        body: JSON.stringify({ passphrase, includeImages }),
      });
      if (result?.dataBase64) {
        downloadBase64(result.filename || 'skill.mmskill', result.dataBase64);
        notify(t('teach.pkgExported'));
        onClose();
      }
    } catch (err) {
      notify(err.message, 'error');
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="video-overlay" onClick={onClose}>
      <div className="video-dialog teach-dialog" onClick={(e) => e.stopPropagation()}>
        <div className="video-dialog-header">
          <span className="video-dialog-title">{t('teach.exportTitle', { name: skill.name })}</span>
          <button type="button" className="video-dialog-close" onClick={onClose} aria-label={t('common.close')}>✕</button>
        </div>
        <div className="vision-rule-form">
          <p className="settings-hint">{t('teach.exportLead')}</p>
          <label>
            {t('teach.pkgPassphrase')}
            <input type="password" value={passphrase} onChange={(e) => setPassphrase(e.target.value)} placeholder={t('teach.pkgPassphrasePh')} disabled={busy} autoFocus />
            <span className="field-hint">{t('teach.pkgPassphraseHint')}</span>
          </label>
          <label className="check-row">
            <input type="checkbox" checked={includeImages} onChange={(e) => setIncludeImages(e.target.checked)} disabled={busy} />
            {t('teach.pkgIncludeImages')}
          </label>
          <span className="field-hint">{t('teach.pkgIncludeImagesHint')}</span>
          <div className="action-row">
            <button type="button" className="quiet" onClick={onClose} disabled={busy}>{t('common.cancel')}</button>
            <button type="button" onClick={doExport} disabled={busy || passphrase.trim().length < 8}>
              <span className="btn-icon"><Ico n="download" /> {t('teach.exportDownload')}</span>
            </button>
          </div>
        </div>
      </div>
    </div>
  );
}

// ImportSkillCard uploads a .mmskill, previews its manifest, then imports it as
// a ready-to-activate skill.
function ImportSkillCard({ api, busy, setBusy, notify, onImported }) {
  const t = useT();
  const [dataBase64, setDataBase64] = useState('');
  const [filename, setFilename] = useState('');
  const [passphrase, setPassphrase] = useState('');
  const [manifest, setManifest] = useState(null);
  const fileRef = useRef(null);

  function pickFile(file) {
    if (!file) return;
    const reader = new FileReader();
    reader.onload = () => {
      const result = String(reader.result || '');
      const comma = result.indexOf(',');
      setDataBase64(comma >= 0 ? result.slice(comma + 1) : result);
      setFilename(file.name);
      setManifest(null);
    };
    reader.readAsDataURL(file);
  }

  async function preview() {
    if (!dataBase64) { notify(t('teach.pkgChooseFile'), 'error'); return; }
    if (passphrase.trim().length < 8) { notify(t('teach.pkgPassTooShort'), 'error'); return; }
    setBusy(true);
    try {
      setManifest(await api('/api/teach/import/preview', { method: 'POST', body: JSON.stringify({ dataBase64, passphrase }) }));
    } catch (err) {
      notify(err.message, 'error');
    } finally {
      setBusy(false);
    }
  }

  async function doImport() {
    setBusy(true);
    try {
      const skill = await api('/api/teach/import', { method: 'POST', body: JSON.stringify({ dataBase64, passphrase }) });
      notify(t('teach.pkgImported', { name: skill?.name || '' }));
      setDataBase64(''); setFilename(''); setPassphrase(''); setManifest(null);
      if (fileRef.current) fileRef.current.value = '';
      if (onImported) onImported(skill);
    } catch (err) {
      notify(err.message, 'error');
    } finally {
      setBusy(false);
    }
  }

  return (
    <section className="settings-panel">
      <header><h2>{t('teach.importTitle')}</h2></header>
      <p className="settings-hint">{t('teach.importLead')}</p>
      <div className="action-row">
        <input ref={fileRef} type="file" accept=".mmskill" style={{ display: 'none' }} onChange={(e) => { pickFile(e.target.files?.[0]); e.target.value = ''; }} />
        <button type="button" className="quiet" onClick={() => fileRef.current?.click()} disabled={busy}>
          <span className="btn-icon"><Ico n="arr-up" /> {t('teach.pkgChooseFile')}</span>
        </button>
        {filename ? <span className="field-hint">{filename}</span> : null}
      </div>
      {dataBase64 ? (
        <>
          <label>
            {t('teach.pkgPassphrase')}
            <input type="password" value={passphrase} onChange={(e) => setPassphrase(e.target.value)} placeholder={t('teach.pkgPassphrasePh')} disabled={busy} />
          </label>
          {!manifest ? (
            <div className="action-row">
              <button type="button" onClick={preview} disabled={busy || passphrase.trim().length < 8}>
                <span className="btn-icon"><Ico n="search" /> {t('teach.pkgOpen')}</span>
              </button>
            </div>
          ) : (
            <>
              <div className="training-active-banner">
                <Ico n="check-ok" /> {t('teach.pkgPreview', {
                  name: manifest.name,
                  acc: Math.round((manifest.accuracy || 0) * 100),
                  n: manifest.imageCount || 0,
                })}
              </div>
              <div className="action-row">
                <button type="button" className="quiet" onClick={() => setManifest(null)} disabled={busy}>{t('common.cancel')}</button>
                <button type="button" onClick={doImport} disabled={busy}>
                  <span className="btn-icon"><Ico n="download" /> {t('teach.pkgImport')}</span>
                </button>
              </div>
            </>
          )}
        </>
      ) : null}
    </section>
  );
}

// AuthBlobImage loads an image from an authed endpoint as a blob (these
// endpoints require the Authorization header, which plain <img> can't send).
function AuthBlobImage({ path, authHeader, alt = '' }) {
  const [src, setSrc] = useState('');
  useEffect(() => {
    let cancelled = false;
    let objectUrl = '';
    (async () => {
      try {
        const headers = authHeader ? { Authorization: authHeader } : {};
        const resp = await fetch(`${apiBase()}${path}`, { credentials: 'include', headers });
        if (!resp.ok) return;
        const blob = await resp.blob();
        if (cancelled) return;
        objectUrl = URL.createObjectURL(blob);
        setSrc(objectUrl);
      } catch (_) { /* thumbnail is best-effort */ }
    })();
    return () => {
      cancelled = true;
      if (objectUrl) URL.revokeObjectURL(objectUrl);
    };
  }, [path, authHeader]);
  if (!src) return <div className="teach-thumb-loading" />;
  return <img src={src} alt={alt} />;
}

// CollectStep is wizard step 4: the teaching sessions. It polls the session
// endpoint while visible so the counter, coach, and filmstrip stay live.
function CollectStep({ api, authHeader, wizard, busy, setBusy, notify, onBack, onCheckAccuracy }) {
  const t = useT();
  const [info, setInfo] = useState(null);
  const [images, setImages] = useState([]);

  const skillId = wizard.id;

  const refresh = useCallback(async () => {
    try {
      const next = await api(`/api/teach/skills/${skillId}/session`);
      setInfo(next);
      if (next?.datasetId > 0) {
        const result = await api(`/api/training/datasets/${next.datasetId}/images`);
        const items = Array.isArray(result?.items) ? result.items : [];
        setImages(items.slice(0, 18));
      }
    } catch (err) {
      notify(err.message, 'error');
    }
  }, [api, skillId, notify]);

  useEffect(() => {
    refresh();
    const id = window.setInterval(refresh, 2000);
    return () => window.clearInterval(id);
  }, [refresh]);

  // friendlyClass maps a raw class label to what the user should read: the
  // "not <slug>" contrast class reads as "Normal / good" (or "Normal
  // operation" for anomaly skills), the target class as the skill's own name.
  const friendlyClass = useCallback(
    (label) => (String(label || '').startsWith('not ')
      ? t(wizard.skillType === 'anomaly' ? 'teach.classNormal' : 'teach.classGood')
      : wizard.name),
    [t, wizard.name, wizard.skillType],
  );

  async function run(action) {
    setBusy(true);
    try {
      await action();
      await refresh();
    } catch (err) {
      notify(err.message, 'error');
    } finally {
      setBusy(false);
    }
  }

  const startSession = (classLabel) => run(() => api(`/api/teach/skills/${skillId}/session/start`, {
    method: 'POST',
    body: JSON.stringify({ classLabel }),
  }));
  const stopSession = () => run(() => api(`/api/teach/skills/${skillId}/session/stop`, { method: 'POST' }));
  const deleteImage = (id) => run(() => api(`/api/training/images/${id}`, { method: 'DELETE' }));

  const coachLine = (hint) => {
    switch (hint.code) {
      case 'needMore':
        return t('teach.coachNeedMore', { name: friendlyClass(hint.class), have: hint.have, need: hint.need });
      case 'imbalance':
        return t('teach.coachImbalance', { name: friendlyClass(hint.class) });
      case 'lowDiversity':
        return t('teach.coachDiversity');
      case 'ready':
        return t('teach.coachReady');
      default:
        return '';
    }
  };

  const classes = info?.classes || [];
  const counts = info?.counts || {};

  return (
    <section className="settings-panel">
      <header><h2>{t('teach.collectTitle')}</h2></header>
      <p className="settings-hint">{t('teach.collectLead')}</p>

      <div className="teach-type-cards">
        {classes.map((cls) => {
          const activeHere = info?.active && info.classLabel === cls;
          const isContrast = String(cls).startsWith('not ');
          return (
            <div key={cls} className={`teach-type-card teach-class-card${activeHere ? ' selected' : ''}`}>
              <span className="teach-type-icon"><Ico n={isContrast ? 'check-ok' : 'search'} sz={20} /></span>
              <strong>{friendlyClass(cls)}</strong>
              <span className="field-hint">
                {t(isContrast
                  ? (wizard.skillType === 'anomaly' ? 'teach.cardNormalHint' : 'teach.cardGoodHint')
                  : 'teach.cardTargetHint')}
              </span>
              <span className="status-pill">{t('teach.samplesLabel', { n: counts[cls] || 0 })}</span>
              {activeHere ? (
                <>
                  <span className="teach-live-row">
                    <span className="teach-live-dot" /> {t('teach.watching')}
                  </span>
                  <span className="field-hint">{t('teach.capturedN', { n: info.captured })}</span>
                  {info.lastError ? (
                    <span className="field-hint danger-text">{t('teach.captureError', { msg: info.lastError })}</span>
                  ) : null}
                  <button type="button" className="quiet" onClick={stopSession} disabled={busy}>
                    <span className="btn-icon"><Ico n="stop" /> {t('teach.stopSession')}</span>
                  </button>
                </>
              ) : (
                <button type="button" onClick={() => startSession(cls)} disabled={busy || Boolean(info?.active)}>
                  <span className="btn-icon"><Ico n="play" /> {t('teach.startShowing')}</span>
                </button>
              )}
            </div>
          );
        })}
      </div>

      {(info?.coach || []).length ? (
        <div className="teach-coach">
          {(info.coach || []).map((hint, index) => (
            <div key={`${hint.code}-${hint.class || index}`} className={`teach-coach-line${hint.code === 'ready' ? ' ok' : ''}`}>
              <Ico n={hint.code === 'ready' ? 'check-ok' : 'bell'} sz={14} /> {coachLine(hint)}
            </div>
          ))}
        </div>
      ) : null}

      <header className="training-divider"><h3>{t('teach.filmstrip')}</h3></header>
      <div className="teach-filmstrip">
        {images.map((img) => (
          <div key={img.id} className="teach-thumb">
            <AuthBlobImage path={`/api/training/images/${img.id}/file`} authHeader={authHeader} alt={t('teach.filmstrip')} />
            <button
              type="button"
              className="teach-thumb-delete"
              onClick={() => deleteImage(img.id)}
              disabled={busy}
              aria-label={t('common.delete')}
            >
              ✕
            </button>
          </div>
        ))}
        {images.length === 0 ? <span className="field-hint">{t('teach.noCaptures')}</span> : null}
      </div>

      <div className="action-row">
        <button type="button" className="quiet" onClick={onBack} disabled={busy || Boolean(info?.active)}>
          {t('teach.backToSkills')}
        </button>
        <button type="button" onClick={onCheckAccuracy} disabled={busy || Boolean(info?.active)}>
          <span className="btn-icon"><Ico n="arr-right" /> {t('teach.checkAccuracy')}</span>
        </button>
        {info?.active ? <span className="field-hint">{t('teach.stopBeforeLeaving')}</span> : null}
      </div>
    </section>
  );
}

// EvalStep is wizard step 5: the accuracy check ("pop quiz"). It quick-trains a
// model on the skill's samples server-side, grades it on the held-back split,
// and renders the result in plain language with a gallery of the misses.
function EvalStep({ api, authHeader, wizard, busy, setBusy, notify, onMoreExamples, onActivateStep }) {
  const t = useT();
  const [state, setState] = useState(null);

  const refresh = useCallback(async () => {
    try {
      setState(await api(`/api/teach/skills/${wizard.id}/evaluation`));
    } catch (err) {
      notify(err.message, 'error');
    }
  }, [api, wizard.id, notify]);

  useEffect(() => {
    refresh();
    const id = window.setInterval(refresh, 2500);
    return () => window.clearInterval(id);
  }, [refresh]);

  async function start() {
    setBusy(true);
    try {
      setState(await api(`/api/teach/skills/${wizard.id}/evaluate`, { method: 'POST' }));
    } catch (err) {
      notify(err.message, 'error');
    } finally {
      setBusy(false);
    }
  }

  const isAnomaly = wizard.skillType === 'anomaly';
  const friendlyClass = useCallback(
    (label) => (String(label || '').startsWith('not ')
      ? t(isAnomaly ? 'teach.classNormal' : 'teach.classGood')
      : wizard.name),
    [t, wizard.name, isAnomaly],
  );

  const phase = state?.phase || 'none';
  const report = state?.report;
  const accuracyPct = report ? Math.round(report.accuracy * 100) : 0;
  const gradeKey = !report ? '' : report.accuracy >= 0.95 ? 'teach.gradeExcellent'
    : report.accuracy >= 0.85 ? 'teach.gradeGood'
    : report.accuracy >= 0.7 ? 'teach.gradeFair'
    : 'teach.gradePoor';

  return (
    <section className="settings-panel">
      <header><h2>{t('teach.stepAccuracy')}</h2></header>

      {phase === 'none' ? (
        <>
          <p className="settings-hint">{t(isAnomaly ? 'teach.evalIntroAnomaly' : 'teach.evalIntro')}</p>
          <div className="action-row">
            <button type="button" onClick={start} disabled={busy}>
              <span className="btn-icon"><Ico n="search" /> {t('teach.checkAccuracy')}</span>
            </button>
          </div>
        </>
      ) : null}

      {phase === 'training' ? (
        <>
          <span className="teach-live-row"><span className="teach-live-dot" /> {t('teach.evalTraining', { p: state?.progress || 0 })}</span>
          <div className="training-progress"><div className="training-progress-bar" style={{ width: `${state?.progress || 0}%` }} /></div>
        </>
      ) : null}

      {phase === 'evaluating' ? (
        <span className="teach-live-row"><span className="teach-live-dot" /> {t('teach.evalTesting')}</span>
      ) : null}

      {phase === 'failed' ? (
        <>
          <span className="field-hint danger-text">{t('teach.evalFailed', { msg: state?.error || '' })}</span>
          <div className="action-row">
            <button type="button" onClick={start} disabled={busy}>
              <span className="btn-icon"><Ico n="refresh" /> {t('teach.tryAgain')}</span>
            </button>
          </div>
        </>
      ) : null}

      {phase === 'done' && report ? (
        <>
          <div className="teach-report-headline">
            <strong>{t(isAnomaly ? 'teach.reportHeadlineAnomaly' : 'teach.reportHeadline', { total: report.total, correct: report.correct })}</strong>
            <span className={`status-pill${report.accuracy >= 0.85 ? ' ok' : ''}`}>{t('teach.reportAccuracy', { p: accuracyPct })}</span>
          </div>
          <p className="settings-hint">{t(gradeKey)}</p>
          <ul className="teach-report-classes">
            {(report.perClass || []).map((entry) => (
              <li key={entry.class} className="field-hint">
                {t('teach.perClassLine', { name: friendlyClass(entry.class), correct: entry.correct, total: entry.total })}
              </li>
            ))}
          </ul>
          {(report.misses || []).length ? (
            <>
              <header className="training-divider"><h3>{t('teach.missesTitle')}</h3></header>
              <div className="teach-filmstrip">
                {(report.misses || []).slice(0, 12).map((miss) => (
                  <div key={miss.imageId} className="teach-thumb teach-miss">
                    <AuthBlobImage path={`/api/training/images/${miss.imageId}/file`} authHeader={authHeader} alt={t('teach.missesTitle')} />
                    <span className="teach-miss-caption">
                      {t('teach.missWas', { name: friendlyClass(miss.expected) })}
                      {' · '}
                      {miss.got ? t('teach.missSaid', { name: friendlyClass(miss.got) }) : t('teach.missNothing')}
                    </span>
                  </div>
                ))}
              </div>
            </>
          ) : null}
          <div className="action-row">
            <button type="button" className="quiet" onClick={onMoreExamples} disabled={busy}>
              <span className="btn-icon"><Ico n="video" /> {t('teach.moreExamples')}</span>
            </button>
            <button type="button" className="quiet" onClick={start} disabled={busy}>
              <span className="btn-icon"><Ico n="refresh" /> {t('teach.checkAgain')}</span>
            </button>
            <button type="button" onClick={onActivateStep} disabled={busy}>
              <span className="btn-icon"><Ico n="arr-right" /> {t('teach.toActivate')}</span>
            </button>
          </div>
        </>
      ) : null}
    </section>
  );
}

// KeepTeaching is the correction loop shown on an ACTIVE skill: it lists recent
// alerts the skill produced and lets the operator confirm (✓, reinforces the
// class) or correct (✗, teaches it not to fire). Verdicts flow into the skill's
// dataset so a later re-check improves it on real-world hard cases.
function KeepTeaching({ api, authHeader, wizard, busy, setBusy, notify }) {
  const t = useT();
  const [alerts, setAlerts] = useState([]);
  const [corrections, setCorrections] = useState(0);

  const refresh = useCallback(async () => {
    try {
      const result = await api(`/api/teach/skills/${wizard.id}/feedback`);
      setAlerts(Array.isArray(result?.items) ? result.items : []);
    } catch (err) {
      notify(err.message, 'error');
    }
  }, [api, wizard.id, notify]);

  useEffect(() => {
    refresh();
    const id = window.setInterval(refresh, 5000);
    return () => window.clearInterval(id);
  }, [refresh]);

  const verdict = (alertId, v) => {
    setBusy(true);
    api(`/api/teach/skills/${wizard.id}/feedback`, { method: 'POST', body: JSON.stringify({ alertId, verdict: v }) })
      .then(() => { setCorrections((n) => n + 1); return refresh(); })
      .catch((err) => notify(err.message, 'error'))
      .finally(() => setBusy(false));
  };

  return (
    <>
      <header className="training-divider"><h3>{t('teach.keepTeachingTitle')}</h3></header>
      <p className="settings-hint">{t('teach.keepTeachingLead')}</p>
      {corrections > 0 ? (
        <div className="teach-coach-line"><Ico n="bell" sz={14} /> {t('teach.correctionsNudge', { n: corrections })}</div>
      ) : null}
      <div className="teach-feedback-list">
        {alerts.map((a) => (
          <div key={a.alertId} className="teach-feedback-row">
            <AuthBlobImage path={`/api/vision/alerts/${a.alertId}/snapshot?annotated=1`} authHeader={authHeader} alt={t('teach.keepTeachingTitle')} />
            <div className="teach-feedback-meta">
              <strong>{a.label || wizard.name}</strong>
              <span className="field-hint">{Math.round((a.confidence || 0) * 100)}%</span>
            </div>
            <div className="teach-feedback-actions">
              <button type="button" className="quiet" onClick={() => verdict(a.alertId, 'correct')} disabled={busy} title={t('teach.verdictCorrect')}>
                ✓ {t('teach.verdictCorrect')}
              </button>
              <button type="button" className="quiet danger" onClick={() => verdict(a.alertId, 'wrong')} disabled={busy} title={t('teach.verdictWrong')}>
                ✗ {t('teach.verdictWrong')}
              </button>
            </div>
          </div>
        ))}
        {alerts.length === 0 ? <span className="field-hint">{t('teach.noAlertsYet')}</span> : null}
      </div>
    </>
  );
}

// ActivateStep is wizard step 6: test-drive the candidate model on the live
// camera, then flip the skill on (model hot-swap + auto-created alert rule) or
// off again. Nothing touches live detection until "Turn it on".
function ActivateStep({ api, authHeader, wizard, busy, setBusy, notify, onStatus, onCheckFirst, onDone }) {
  const t = useT();
  const [evalState, setEvalState] = useState(null);
  const [driving, setDriving] = useState(false);
  const [drive, setDrive] = useState(null);

  useEffect(() => {
    (async () => {
      try { setEvalState(await api(`/api/teach/skills/${wizard.id}/evaluation`)); } catch (_) { /* summary is best-effort */ }
    })();
  }, [api, wizard.id]);

  // Poll the test-drive frame while driving; stop the worker when leaving.
  useEffect(() => {
    if (!driving) return undefined;
    let cancelled = false;
    const poll = async () => {
      try {
        const frame = await api(`/api/teach/skills/${wizard.id}/testdrive`);
        if (!cancelled) setDrive(frame);
      } catch (err) {
        if (!cancelled) {
          setDriving(false);
          notify(err.message, 'error');
        }
      }
    };
    poll();
    const id = window.setInterval(poll, 1500);
    return () => {
      cancelled = true;
      window.clearInterval(id);
    };
  }, [driving, api, wizard.id, notify]);
  useEffect(() => () => {
    // Leaving the step shuts the candidate worker down (best-effort; the
    // server-side idle reaper covers the rest).
    fetch(`${apiBase()}/api/teach/skills/${wizard.id}/testdrive/stop`, { method: 'POST', credentials: 'include' }).catch(() => {});
  }, [wizard.id]);

  async function run(action, okMessage) {
    setBusy(true);
    try {
      const out = await action();
      if (okMessage) notify(okMessage);
      return out;
    } catch (err) {
      notify(err.message, 'error');
      return null;
    } finally {
      setBusy(false);
    }
  }

  const startDrive = () => run(async () => {
    await api(`/api/teach/skills/${wizard.id}/testdrive/start`, { method: 'POST' });
    setDrive(null);
    setDriving(true);
  });
  const stopDrive = () => run(async () => {
    await api(`/api/teach/skills/${wizard.id}/testdrive/stop`, { method: 'POST' });
    setDriving(false);
    setDrive(null);
  });
  const activate = () => run(async () => {
    setDriving(false);
    await api(`/api/teach/skills/${wizard.id}/activate`, { method: 'POST' });
    onStatus('active');
  }, t('teach.activated'));
  const deactivate = () => run(async () => {
    await api(`/api/teach/skills/${wizard.id}/deactivate`, { method: 'POST' });
    onStatus('trained');
  }, t('teach.deactivated'));

  const report = evalState?.report;
  const isActive = wizard.status === 'active';
  const seeing = (drive?.detections || [])
    .map((d) => `${d.label} ${Math.round((d.confidence || 0) * 100)}%`)
    .join(', ');

  return (
    <section className="settings-panel">
      <header><h2>{t('teach.activateTitle')}</h2></header>

      {isActive ? (
        <>
          <div className="training-active-banner">
            <Ico n="check-ok" /> {t('teach.activeBanner', { name: wizard.name })}
          </div>
          <p className="settings-hint">{t('teach.activeRuleNote')}</p>
          <div className="action-row">
            <button type="button" className="quiet danger" onClick={deactivate} disabled={busy}>
              {t('teach.turnOff')}
            </button>
            <button type="button" className="quiet" onClick={onDone} disabled={busy}>
              {t('teach.backToSkills')}
            </button>
          </div>
          <KeepTeaching api={api} authHeader={authHeader} wizard={wizard} busy={busy} setBusy={setBusy} notify={notify} />
        </>
      ) : !report ? (
        <>
          <p className="settings-hint">{t('teach.noReportYet')}</p>
          <div className="action-row">
            <button type="button" className="quiet" onClick={onCheckFirst} disabled={busy}>
              <span className="btn-icon"><Ico n="search" /> {t('teach.checkAccuracy')}</span>
            </button>
          </div>
        </>
      ) : (
        <>
          <p className="settings-hint">{t('teach.activateLead', { name: wizard.name })}</p>
          <span className="field-hint">{t('teach.accuracySummary', { p: Math.round(report.accuracy * 100) })}</span>
          {report.accuracy < 0.7 ? (
            <span className="field-hint danger-text">{t('teach.lowAccuracyWarn')}</span>
          ) : null}

          {/* Test-drive is disabled in the control-plane (fleet) build: it polls full JPEG
              frames every 1.5s, which is too heavy to stream through the node tunnel. Verify
              the activated skill via the node's Live View / Detection instead. */}

          <div className="action-row">
            <button type="button" onClick={activate} disabled={busy}>
              <span className="btn-icon"><Ico n="play" /> {t('teach.turnOn')}</span>
            </button>
          </div>
        </>
      )}
    </section>
  );
}

export function TeachTab({ authHeader, onMessage, cameras, streamConfig, labelCatalog, onOpenModels }) {
  const t = useT();
  const [skills, setSkills] = useState([]);
  const [capability, setCapability] = useState(null);
  const [busy, setBusy] = useState(false);
  // wizard === null → skills home. Otherwise the in-progress draft:
  // { id, name, skillType, cameraId, roiPolygon, step }.
  const [wizard, setWizard] = useState(null);
  // Skill currently being exported (shows the passphrase dialog); null = none.
  const [exporting, setExporting] = useState(null);

  const api = useCallback(async (path, options = {}) => {
    const headers = { ...(options.headers || {}) };
    if (authHeader) headers.Authorization = authHeader;
    if (options.body) headers['Content-Type'] = 'application/json';
    const resp = await fetch(`${apiBase()}${path}`, { credentials: 'include', ...options, headers });
    const text = await resp.text();
    let payload = null;
    if (text) { try { payload = JSON.parse(text); } catch (_) { payload = { message: text }; } }
    if (!resp.ok) throw new Error(payload?.message || payload?.data?.message || `Request failed (${resp.status})`);
    return payload?.data?.result ?? payload?.result ?? payload;
  }, [authHeader]);

  const notify = useCallback((msg, kind) => { if (onMessage) onMessage(msg, kind); }, [onMessage]);

  const loadSkills = useCallback(async () => {
    try {
      const result = await api('/api/teach/skills');
      setSkills(Array.isArray(result?.items) ? result.items : []);
    } catch (err) {
      notify(err.message, 'error');
    }
  }, [api, notify]);

  useEffect(() => { loadSkills(); }, [loadSkills]);
  useEffect(() => {
    (async () => {
      try { setCapability(await api('/api/training/capability')); } catch (_) { /* best effort */ }
    })();
  }, [api]);

  async function run(action, okMessage) {
    setBusy(true);
    try {
      const out = await action();
      if (okMessage) notify(okMessage);
      return out;
    } catch (err) {
      notify(err.message, 'error');
      return null;
    } finally {
      setBusy(false);
    }
  }

  function startNewSkill() {
    setWizard({ id: 0, name: '', skillType: '', cameraId: 0, roiPolygon: '', step: 1 });
  }

  function resumeSkill(skill) {
    setWizard({
      id: skill.id,
      name: skill.name || '',
      skillType: skill.skillType || '',
      cameraId: skill.cameraId || 0,
      roiPolygon: skill.roiPolygon || '',
      status: skill.status || 'draft',
      step: Math.min(Math.max(skill.step || 1, 1), 6),
    });
  }

  function deleteSkill(id) {
    run(async () => {
      await api(`/api/teach/skills/${id}`, { method: 'DELETE' });
      await loadSkills();
    }, t('teach.skillDeleted'));
  }

  // persist saves the wizard draft (create on first advance) and moves to
  // nextStep. The skill row's `step` records the furthest step reached, so
  // Continue resumes there.
  async function persist(fields, nextStep) {
    const saved = await run(async () => {
      if (!wizard.id) {
        return api('/api/teach/skills', { method: 'POST', body: JSON.stringify({ ...fields, step: nextStep }) });
      }
      return api(`/api/teach/skills/${wizard.id}`, { method: 'PUT', body: JSON.stringify({ ...fields, step: nextStep }) });
    });
    if (saved) {
      setWizard((w) => ({ ...w, ...fields, id: saved.id || w.id, step: Math.min(nextStep, 6) }));
      await loadSkills();
    }
  }

  async function exitWizard() {
    setWizard(null);
    await loadSkills();
  }

  // Client-side preview of the backend's stock-label collision rule so the user
  // sees the conflict while typing, not on submit.
  const nameSlug = wizard ? teachSlug(wizard.name) : '';
  const nameConflict = Boolean(
    nameSlug &&
    (labelCatalog || []).some((entry) => entry && entry.source === 'stock' && entry.label === nameSlug),
  );

  const selectedCamera = wizard ? (cameras || []).find((c) => Number(c.id) === Number(wizard.cameraId)) || null : null;
  const capabilityMissing = capability && !capability.available;

  const statusLabel = (skill) => {
    if (skill.status === 'active') return t('teach.statusActive');
    if (skill.status === 'collecting') return t('teach.statusCollecting');
    if (skill.status === 'trained') return t('teach.statusReady');
    if (skill.status !== 'draft') return skill.status;
    return (skill.step || 1) > 3 ? t('teach.statusReady') : t('teach.statusDraft');
  };

  if (!wizard) {
    return (
      <section className="workspace teach-tab">
        <div className="settings-content">
          <header className="training-header">
            <span className="training-eyebrow">{t('tab.teach')}</span>
            <h1>{t('teach.title')}</h1>
            <p>{t('teach.lead')}</p>
          </header>

          {capabilityMissing ? (
            <section className="settings-panel">
              <header><h2>{t('teach.capTitle')}</h2></header>
              <p className="settings-hint">{t('teach.capMissing')}</p>
              <div className="action-row">
                <button type="button" className="quiet" onClick={onOpenModels}>
                  <span className="btn-icon"><Ico n="cpu" /> {t('teach.capOpenSettings')}</span>
                </button>
              </div>
            </section>
          ) : null}

          <section className="settings-panel">
            <header>
              <h2>{t('teach.skillsTitle')}</h2>
              <span className="status-pill">{skills.length}</span>
            </header>
            <div className="action-row">
              <button type="button" onClick={startNewSkill} disabled={busy}>
                <span className="btn-icon"><Ico n="plus" /> {t('teach.newSkill')}</span>
              </button>
            </div>
            <ul className="class-registry-list">
              {skills.map((skill) => (
                <li key={skill.id} className="class-registry-row">
                  <div>
                    <strong>{skill.name}</strong>
                    <span className={`status-pill${skill.status === 'collecting' || skill.status === 'active' ? ' ok' : ''}`}> {statusLabel(skill)}</span>
                    <span className="field-hint">
                      {' '}
                      {skill.cameraId
                        ? cameraTitle((cameras || []).find((c) => Number(c.id) === Number(skill.cameraId)))
                        : t('teach.noCameraYet')}
                      {skill.skillType ? ` · ${t(skill.skillType === 'inspect' ? 'teach.typeInspect' : 'teach.typeObject')}` : ''}
                    </span>
                  </div>
                  <div className="class-registry-actions">
                    <button type="button" onClick={() => resumeSkill(skill)} disabled={busy}>
                      {t('teach.continue')}
                    </button>
                    {skill.modelId ? (
                      <button type="button" className="quiet" onClick={() => setExporting(skill)} disabled={busy} title={t('teach.exportShare')}>
                        <span className="btn-icon"><Ico n="download" /> {t('teach.exportShare')}</span>
                      </button>
                    ) : null}
                    <button type="button" className="quiet danger" onClick={() => deleteSkill(skill.id)} disabled={busy}>
                      {t('common.delete')}
                    </button>
                  </div>
                </li>
              ))}
              {skills.length === 0 ? <li className="field-hint">{t('teach.noSkills')}</li> : null}
            </ul>
            <span className="field-hint">{t('teach.advancedHint')}</span>
          </section>

          <ImportSkillCard api={api} busy={busy} setBusy={setBusy} notify={notify} onImported={() => loadSkills()} />
        </div>

        {exporting ? (
          <ExportSkillDialog
            api={api}
            skill={exporting}
            busy={busy}
            setBusy={setBusy}
            notify={notify}
            onClose={() => setExporting(null)}
          />
        ) : null}
      </section>
    );
  }

  return (
    <section className="workspace teach-tab">
      <div className="settings-content teach-wizard-body">
        <header className="training-header">
          <span className="training-eyebrow">{t('tab.teach')}</span>
          <h1>{wizard.name ? t('teach.wizardTitleNamed', { name: wizard.name }) : t('teach.wizardTitle')}</h1>
        </header>

        <Tabs
          ariaLabel={t('teach.wizardAria')}
          active={wizard.step}
          onChange={(step) => setWizard((w) => ({ ...w, step }))}
          tabs={WIZARD_STEPS.map((s) => {
            const reachable = !s.locked && s.step <= wizard.step && wizard.id;
            return {
              id: s.step,
              label: `${t(s.labelKey)}${s.locked ? ` · ${t('teach.stepSoonTag')}` : ''}`,
              icon: s.icon,
              disabled: s.locked || (!reachable && s.step !== wizard.step),
              title: s.locked ? t('teach.stepSoon') : undefined,
            };
          })}
        />

        {wizard.step === 1 ? (
          <section className="settings-panel">
            <header><h2>{t('teach.nameTitle')}</h2></header>
            <p className="settings-hint">{t('teach.nameLead')}</p>
            <label>
              {t('common.name')}
              <input
                value={wizard.name}
                onChange={(e) => setWizard((w) => ({ ...w, name: e.target.value }))}
                placeholder={t('teach.namePh')}
                disabled={busy}
                autoFocus
              />
            </label>
            {nameConflict ? (
              <span className="field-hint danger-text">{t('teach.nameConflict', { name: nameSlug })}</span>
            ) : null}
            <div className="action-row">
              <button type="button" className="quiet" onClick={exitWizard} disabled={busy}>{t('common.cancel')}</button>
              <button
                type="button"
                onClick={() => persist({ name: wizard.name }, 2)}
                disabled={busy || !wizard.name.trim() || nameConflict}
              >
                <span className="btn-icon"><Ico n="arr-right" /> {t('teach.next')}</span>
              </button>
            </div>
          </section>
        ) : null}

        {wizard.step === 2 ? (
          <section className="settings-panel">
            <header><h2>{t('teach.kindTitle')}</h2></header>
            <p className="settings-hint">{t('teach.kindLead')}</p>
            <div className="teach-type-cards">
              {SKILL_TYPES.map((type) => (
                <button
                  key={type.id}
                  type="button"
                  className={`teach-type-card${wizard.skillType === type.id ? ' selected' : ''}`}
                  disabled={busy || type.disabled}
                  onClick={() => setWizard((w) => ({ ...w, skillType: type.id }))}
                >
                  <span className="teach-type-icon"><Ico n={type.icon} sz={22} /></span>
                  <strong>{t(type.titleKey)}{type.disabled ? ` · ${t('teach.stepSoonTag')}` : ''}</strong>
                  <span className="field-hint">{t(type.descKey)}</span>
                </button>
              ))}
            </div>
            <div className="action-row">
              <button type="button" className="quiet" onClick={() => setWizard((w) => ({ ...w, step: 1 }))} disabled={busy}>
                {t('teach.back')}
              </button>
              <button
                type="button"
                onClick={() => persist({ skillType: wizard.skillType }, 3)}
                disabled={busy || !wizard.skillType}
              >
                <span className="btn-icon"><Ico n="arr-right" /> {t('teach.next')}</span>
              </button>
            </div>
          </section>
        ) : null}

        {wizard.step === 3 ? (
          <section className="settings-panel">
            <header><h2>{t('teach.whereTitle')}</h2></header>
            <p className="settings-hint">{t('teach.whereLead')}</p>
            <label>
              {t('teach.cameraLabel')}
              <select
                value={wizard.cameraId || ''}
                onChange={(e) => setWizard((w) => ({ ...w, cameraId: Number(e.target.value) || 0, roiPolygon: '' }))}
                disabled={busy}
              >
                <option value="">{t('teach.pickCamera')}</option>
                {(cameras || []).map((cam) => (
                  <option key={cam.id} value={cam.id}>{cameraTitle(cam)}</option>
                ))}
              </select>
            </label>
            {selectedCamera ? (
              <>
                <span className="field-hint">{t('teach.roiHint')}</span>
                <ZoneDrawingPreview
                  camera={selectedCamera}
                  polygonValue={wizard.roiPolygon}
                  authHeader={authHeader}
                  streamConfig={streamConfig}
                  disabled={busy}
                  onPolygon={(roiPolygon) => setWizard((w) => ({ ...w, roiPolygon }))}
                />
              </>
            ) : null}
            <div className="action-row">
              <button type="button" className="quiet" onClick={() => setWizard((w) => ({ ...w, step: 2 }))} disabled={busy}>
                {t('teach.back')}
              </button>
              <button
                type="button"
                onClick={() => persist({ cameraId: wizard.cameraId, roiPolygon: wizard.roiPolygon }, 4)}
                disabled={busy || !wizard.cameraId}
              >
                <span className="btn-icon"><Ico n="arr-right" /> {t('teach.toExamples')}</span>
              </button>
            </div>
          </section>
        ) : null}

        {wizard.step === 4 ? (
          <CollectStep
            api={api}
            authHeader={authHeader}
            wizard={wizard}
            busy={busy}
            setBusy={setBusy}
            notify={notify}
            onBack={exitWizard}
            onCheckAccuracy={() => setWizard((w) => ({ ...w, step: 5 }))}
          />
        ) : null}

        {wizard.step === 5 ? (
          <EvalStep
            api={api}
            authHeader={authHeader}
            wizard={wizard}
            busy={busy}
            setBusy={setBusy}
            notify={notify}
            onMoreExamples={() => setWizard((w) => ({ ...w, step: 4 }))}
            onActivateStep={() => setWizard((w) => ({ ...w, step: 6 }))}
          />
        ) : null}

        {wizard.step === 6 ? (
          <ActivateStep
            api={api}
            authHeader={authHeader}
            wizard={wizard}
            busy={busy}
            setBusy={setBusy}
            notify={notify}
            onStatus={(status) => { setWizard((w) => ({ ...w, status })); loadSkills(); }}
            onCheckFirst={() => setWizard((w) => ({ ...w, step: 5 }))}
            onDone={exitWizard}
          />
        ) : null}
      </div>
    </section>
  );
}
