import { useState, useEffect, useCallback, useMemo, useRef } from 'react';
import { Ico } from './icons';
import { FormAlert } from './ui';
import { useT } from '@shared/i18n';
import { HelpButton, useManual } from '@shared/Manual';
import { StatCard } from '@shared/charts';
import { apiBase, formatTimestamp, apiJson } from '../lib/helpers';

// Case files: the investigation, on screen.
//
// The screen is built around one claim the operator has to be able to trust: WHAT IS IN
// THIS CASE IS STILL HERE. So the hold panel is not a status line at the bottom, it is at
// the top beside the status, it says how many clips are only still on disk because the
// case is open, and closing the case restates that number in the dialog. A case screen
// that shows a tidy list of evidence and lets retention quietly delete it underneath is
// worse than no case screen, because it is believed.
//
// It is also the screen an operator arrives at LEAST OFTEN and understands least: a case
// is not a folder, and nothing about a list of clips explains why one should be opened at
// all. So the page teaches as well as works — a four-step guide across the top, a progress
// rail on the open case that says where this investigation actually is, and empty states
// that name the screen the evidence comes FROM rather than apologising for being empty.
// The teaching is dismissible and remembered; it is a guide, not a nag.

// bookmarkPad is how much footage a one-click bookmark takes around the chosen moment.
// A bookmark is a moment to a person and a span to the exporter; padding it here rather
// than storing an instant is what makes "bookmark this" produce something playable.
const BOOKMARK_PAD_SECONDS = 30;

// The JSON fetch lives in lib/helpers as apiJson, shared with the wall screen.
const callApi = apiJson;

// GUIDE_KEY remembers a dismissed guide across visits. Per-browser rather than per-user
// on purpose: it is a reading preference, not a setting worth a server round trip, and an
// operator who has read it once should not meet it again on every visit to the screen.
const GUIDE_KEY = 'mymatasan.cases.guide';

function guideDismissed() {
  try {
    return window.localStorage.getItem(GUIDE_KEY) === 'off';
  } catch (_) {
    // Private-mode browsers throw on localStorage. Showing the guide is the safe answer:
    // the cost of seeing it twice is far below the cost of never seeing it.
    return false;
  }
}

function rememberGuide(open) {
  try {
    window.localStorage.setItem(GUIDE_KEY, open ? 'on' : 'off');
  } catch (_) { /* see guideDismissed */ }
}

// The four steps of a case, in the order they happen. Each one links into the manual
// section that explains it, so the guide is a doorway rather than a summary that will
// drift away from the real documentation.
const GUIDE_STEPS = [
  { n: 1, icon: 'folder', anchor: 'opening', title: 'cases.guide1Title', body: 'cases.guide1Body' },
  { n: 2, icon: 'plus', anchor: 'adding', title: 'cases.guide2Title', body: 'cases.guide2Body' },
  { n: 3, icon: 'shield', anchor: 'holding', title: 'cases.guide3Title', body: 'cases.guide3Body' },
  { n: 4, icon: 'download', anchor: 'exporting', title: 'cases.guide4Title', body: 'cases.guide4Body' },
];

function bytesLabel(bytes) {
  const n = Number(bytes) || 0;
  if (n <= 0) return '0 MB';
  if (n < 1024 * 1024) return `${Math.max(1, Math.round(n / 1024))} KB`;
  if (n < 1024 * 1024 * 1024) return `${Math.round(n / (1024 * 1024))} MB`;
  return `${(n / (1024 * 1024 * 1024)).toFixed(1)} GB`;
}

// millisOf matches formatTimestamp's rule: the API speaks seconds, but a value that is
// already in milliseconds must not be multiplied again.
function millisOf(value) {
  const raw = Number(value || 0);
  return raw > 9999999999 ? raw : raw * 1000;
}

function partsFor(value, opts) {
  try {
    return new Intl.DateTimeFormat(undefined, opts).format(new Date(millisOf(value)));
  } catch (_) {
    return new Date(millisOf(value)).toLocaleString();
  }
}

// shortStamp is the list card's date. Seconds are noise in a list of cases; they matter
// on a piece of evidence, which is why that is a different function.
function shortStamp(value) {
  if (!Number(value)) return '-';
  return partsFor(value, { year: 'numeric', month: 'short', day: '2-digit', hour: '2-digit', minute: '2-digit' });
}

// timeRange is one piece of evidence's span. A minute of footage written as
// "Aug 31, 2025, 06:21:40 – Aug 31, 2025, 06:22:40" makes the reader compare two long
// strings to find the one field that differs. The date is said ONCE when both ends fall on
// the same day — and in full when they do not, because a span crossing midnight is exactly
// the case where the date carries information.
function timeRange(from, to) {
  if (!Number(from)) return '-';
  if (!Number(to) || Number(to) === Number(from)) return formatTimestamp(from);
  const day = { year: 'numeric', month: 'short', day: '2-digit' };
  if (partsFor(from, day) === partsFor(to, day)) {
    return `${formatTimestamp(from)} – ${partsFor(to, { hour: '2-digit', minute: '2-digit', second: '2-digit' })}`;
  }
  return `${formatTimestamp(from)} – ${formatTimestamp(to)}`;
}

// initials is the assignee chip's avatar. A name is more scannable than an id and two
// letters are more scannable than a name in a list of twenty cases.
function initials(name) {
  const parts = String(name || '').trim().split(/\s+/).filter(Boolean);
  if (parts.length === 0) return '?';
  if (parts.length === 1) return parts[0].slice(0, 2).toUpperCase();
  return (parts[0][0] + parts[parts.length - 1][0]).toUpperCase();
}

// CaseThumb renders one item's frame, fetched through the API so an authorization failure
// shows as a placeholder rather than a browser-native broken image.
function CaseThumb({ segmentId, seek, authHeader }) {
  const [url, setUrl] = useState(null);
  const [failed, setFailed] = useState(false);
  useEffect(() => {
    if (!Number(segmentId)) return undefined;
    let cancelled = false;
    let obj = null;
    (async () => {
      try {
        const headers = authHeader ? { Authorization: authHeader } : {};
        const params = new URLSearchParams({ seek: String(Math.max(0, Number(seek) || 0)), w: '200' });
        const r = await fetch(`${apiBase()}/api/recording/segments/${segmentId}/frame?${params}`, {
          credentials: 'include', headers,
        });
        if (!r.ok) throw new Error(String(r.status));
        const blob = await r.blob();
        if (cancelled) return;
        obj = URL.createObjectURL(blob);
        setUrl(obj);
      } catch (_) {
        if (!cancelled) setFailed(true);
      }
    })();
    return () => { cancelled = true; if (obj) URL.revokeObjectURL(obj); };
  }, [segmentId, seek, authHeader]);
  if (failed || !Number(segmentId)) {
    return <span className="case-thumb case-thumb--none" aria-hidden="true"><Ico n="film" sz={18} /></span>;
  }
  return url
    ? <img className="case-thumb" src={url} alt="" />
    : <span className="case-thumb case-thumb--load" aria-hidden="true" />;
}

// CaseGuide is the "what is this screen" panel. It sits above the work rather than behind
// a "?" because the operator who needs it does not yet know there is anything to ask.
export function CaseGuide({ onDismiss }) {
  const t = useT();
  const manual = useManual();
  return (
    <div className="case-guide" data-case-guide="1">
      <div className="case-guide-head">
        <span className="case-guide-mark" aria-hidden="true"><Ico n="book" sz={16} /></span>
        <div className="case-guide-titles">
          <strong>{t('cases.guideTitle')}</strong>
          <p>{t('cases.guideLead')}</p>
        </div>
        <button type="button" className="quiet" onClick={onDismiss} aria-label={t('cases.guideHide')}>
          <Ico n="x" sz={14} />
        </button>
      </div>
      <ol className="case-guide-steps">
        {GUIDE_STEPS.map((step) => (
          <li key={step.n}>
            <span className="case-guide-num" aria-hidden="true">{step.n}</span>
            <span className="case-guide-ico" aria-hidden="true"><Ico n={step.icon} sz={18} /></span>
            <strong>{t(step.title)}</strong>
            <p>{t(step.body)}</p>
            <button type="button" className="case-guide-more"
              onClick={() => manual.openHelp('case-files', step.anchor)}>
              {t('cases.guideMore')} <Ico n="chev-right" sz={12} />
            </button>
          </li>
        ))}
      </ol>
    </div>
  );
}

// CaseSteps is the progress rail on an open case: where this investigation actually is,
// stated from what the SERVER knows rather than from what the operator clicked in this
// session. Each step is a button that takes them to the part of the page that advances it,
// which is the difference between a diagram and a control.
function CaseSteps({ detail, onJump }) {
  const t = useT();
  const c = detail?.case || {};
  const items = detail?.items || [];
  const closed = c.status === 'closed';
  const steps = [
    {
      id: 'summary',
      icon: 'folder',
      done: true,
      label: t('cases.stepOpened'),
      note: c.openedName ? t('cases.stepOpenedBy', { who: c.openedName }) : formatTimestamp(c.openedAt),
    },
    {
      id: 'evidence',
      icon: 'film',
      done: items.length > 0,
      label: t('cases.stepEvidence'),
      note: items.length > 0 ? t('cases.itemCount', { n: items.length }) : t('cases.stepEvidenceTodo'),
    },
    {
      id: 'summary',
      icon: 'edit-2',
      done: Boolean(String(c.summary || '').trim()),
      label: t('cases.stepWriteUp'),
      note: String(c.summary || '').trim() ? t('cases.stepWriteUpDone') : t('cases.stepWriteUpTodo'),
    },
    {
      id: 'closing',
      // NOT check-ok: the dot draws the step's own icon until the step is done, and a
      // tick over "Still open" says the opposite of what the row says next to it.
      icon: 'circle',
      done: closed,
      label: t('cases.stepOutcome'),
      note: closed ? t('cases.stepOutcomeDone') : t('cases.stepOutcomeTodo'),
    },
  ];
  // The first step that is not done is where the operator is. Everything after it is
  // simply "not yet" — marking it as a problem would make a young case look neglected.
  const current = steps.findIndex((s) => !s.done);
  return (
    <div className="case-steps" role="group" aria-label={t('cases.stepsTitle')}>
      {steps.map((step, i) => (
        <button key={step.label} type="button"
          className={`case-step${step.done ? ' is-done' : ''}${i === current ? ' is-current' : ''}`}
          onClick={() => onJump?.(step.id)}>
          <span className="case-step-dot" aria-hidden="true">
            <Ico n={step.done ? 'check-ok' : step.icon} sz={14} />
          </span>
          <span className="case-step-text">
            <strong>{step.label}</strong>
            <em>{step.note}</em>
          </span>
        </button>
      ))}
    </div>
  );
}

// EvidencePlayer plays one piece of evidence without leaving the case.
//
// The Play button on an evidence row used to call a callback the Cases screen was never
// given, so it did nothing at all — the row offered playback and the page could not
// deliver it. Playback belongs here: a case is read by scrubbing between its clips, and
// bouncing to the Recording screen loses the case.
function EvidencePlayer({ item, authHeader, onClose }) {
  const t = useT();
  const [url, setUrl] = useState(null);
  const [error, setError] = useState('');
  const videoRef = useRef(null);

  useEffect(() => {
    let cancelled = false;
    let obj = null;
    (async () => {
      try {
        const headers = authHeader ? { Authorization: authHeader } : {};
        const resp = await fetch(`${apiBase()}/api/recording/segments/${item.segmentId}/download`, {
          credentials: 'include', headers,
        });
        if (!resp.ok) throw new Error(String(resp.status));
        const blob = await resp.blob();
        if (cancelled) return;
        obj = URL.createObjectURL(blob);
        setUrl(obj);
      } catch (err) {
        if (!cancelled) setError(err.message);
      }
    })();
    return () => { cancelled = true; if (obj) URL.revokeObjectURL(obj); };
  }, [item, authHeader]);

  useEffect(() => {
    const onKey = (e) => { if (e.key === 'Escape') onClose?.(); };
    document.addEventListener('keydown', onKey);
    return () => document.removeEventListener('keydown', onKey);
  }, [onClose]);

  return (
    <div className="video-overlay" onClick={onClose}>
      <div className="video-dialog" onClick={(e) => e.stopPropagation()} role="dialog"
        aria-label={t('cases.play')}>
        <div className="video-dialog-header">
          <div className="video-dialog-title-group">
            <span className="video-dialog-title">
              {item.cameraName || t('cases.cameraN', { n: item.cameraId })}
            </span>
          </div>
          <button type="button" className="video-dialog-close" onClick={onClose}
            aria-label={t('common.close')}>✕</button>
        </div>
        <div className="video-dialog-body">
          {error ? <FormAlert message={error} /> : null}
          {!url && !error ? <div className="video-loading-msg">{t('cases.playLoading')}</div> : null}
          {url ? (
            // The seek is applied once the browser knows the duration. Setting it before
            // metadata arrives is silently ignored, which would drop the operator at the
            // start of a twenty-minute segment instead of at the moment they marked.
            <video ref={videoRef} className="video-player" controls autoPlay src={url}
              onLoadedMetadata={() => {
                const at = Math.max(0, Number(item.seek) || 0);
                if (videoRef.current && at > 0) videoRef.current.currentTime = at;
              }} />
          ) : null}
        </div>
        <div className="video-dialog-meta">
          {timeRange(item.startedAt, item.endedAt)}
          {item.label ? ` · ${item.label}` : ''}
        </div>
      </div>
    </div>
  );
}

// AddToCaseDialog is the "put this in a case" action, shared by every screen that can
// produce evidence — the timeline, the object grid, the alert log. One dialog rather than
// one per screen, because the thing being added is the same shape everywhere.
//
// It takes a LIST, not one item, because the timeline's unit is a moment across several
// cameras: bookmarking 14:07 with four tiles up means four pieces of evidence, and making
// the operator do that four times would be the screen fighting its own design.
export function AddToCaseDialog({ items = [], notificationId = 0, summary = '', authHeader, onClose, onAdded }) {
  const t = useT();
  const [cases, setCases] = useState([]);
  const [caseId, setCaseId] = useState(0);
  const [newTitle, setNewTitle] = useState('');
  const [note, setNote] = useState('');
  const [error, setError] = useState('');
  const [busy, setBusy] = useState(false);

  useEffect(() => {
    let cancelled = false;
    (async () => {
      try {
        const data = await callApi('/api/cases?status=open&limit=100', { authHeader });
        if (cancelled) return;
        const rows = data?.cases || [];
        setCases(rows);
        if (rows.length > 0) setCaseId(Number(rows[0].id));
      } catch (err) {
        if (!cancelled) setError(err.message);
      }
    })();
    return () => { cancelled = true; };
  }, [authHeader]);

  useEffect(() => {
    const onKey = (e) => { if (e.key === 'Escape') onClose?.(); };
    document.addEventListener('keydown', onKey);
    return () => document.removeEventListener('keydown', onKey);
  }, [onClose]);

  const submit = useCallback(async () => {
    setBusy(true);
    setError('');
    try {
      let target = Number(caseId) || 0;
      if (!target) {
        const created = await callApi('/api/cases', {
          method: 'POST', authHeader, body: { title: newTitle },
        });
        target = Number(created?.id) || 0;
      }
      if (!target) throw new Error(t('cases.addFailed'));
      if (notificationId) {
        // A FEED ENTRY IS SENT BY ID, not as a built evidence body. What the entry
        // actually refers to — an alert with its own camera, time and snapshot, or
        // nothing at all — is the server's decision, because a client that made it would
        // make it differently on every screen that has this button.
        await callApi(`/api/cases/${target}/items/from-notification`, {
          method: 'POST', authHeader,
          body: { notificationId: Number(notificationId), note },
        });
      } else {
        // Sequential, and the first failure stops the run with its own message. Adding
        // four clips and reporting a single "failed" after silently landing two is worse
        // than stopping: the operator has to know what is in the case.
        for (const item of items) {
          await callApi(`/api/cases/${target}/items`, {
            method: 'POST', authHeader,
            body: { ...item, note },
          });
        }
      }
      onAdded?.(target);
      onClose?.();
    } catch (err) {
      setError(err.message);
    } finally {
      setBusy(false);
    }
  }, [caseId, newTitle, note, items, notificationId, authHeader, onAdded, onClose, t]);

  const count = notificationId ? 1 : items.length;

  return (
    <div className="video-overlay" onClick={onClose}>
      <div className="video-dialog case-dialog" onClick={(e) => e.stopPropagation()} role="dialog" aria-label={t('cases.addTitle')}>
        <div className="video-dialog-header">
          <strong>{t('cases.addTitle')}</strong>
          <button type="button" className="quiet" onClick={onClose} aria-label={t('common.close')}>
            <Ico n="x" />
          </button>
        </div>
        <p className="case-dialog-lead">{t('cases.addLead', { n: count })}</p>
        <ul className="case-evidence-lines">
          {items.map((item, i) => (
            <li key={`${item.cameraId}-${item.startedAt}-${i}`}>
              <Ico n="film" sz={13} />
              <span>
                {item.cameraName ? `${item.cameraName} · ` : ''}
                {timeRange(item.startedAt, item.endedAt)}
              </span>
            </li>
          ))}
          {/* A feed entry has no span to show — the server works out what it refers to and
              how much footage to keep. Naming it is still the point: a dialog that asks an
              operator to confirm something it will not name is asking them to guess. */}
          {notificationId && summary ? (
            <li data-case-evidence="notification"><Ico n="bell" sz={13} /> <span>{summary}</span></li>
          ) : null}
        </ul>
        {error ? <FormAlert message={error} /> : null}
        <label className="case-field">
          <span>{t('cases.addTo')}</span>
          <select value={caseId} onChange={(e) => setCaseId(Number(e.target.value))} disabled={busy}>
            {cases.map((c) => (
              <option key={c.id} value={c.id}>{c.title}</option>
            ))}
            <option value={0}>{t('cases.addNew')}</option>
          </select>
        </label>
        {!Number(caseId) ? (
          <label className="case-field">
            <span>{t('cases.title')}</span>
            <input value={newTitle} onChange={(e) => setNewTitle(e.target.value)} disabled={busy}
              placeholder={t('cases.titlePlaceholder')} />
          </label>
        ) : null}
        <label className="case-field">
          <span>{t('cases.note')}</span>
          <textarea rows={2} value={note} onChange={(e) => setNote(e.target.value)} disabled={busy}
            placeholder={t('cases.notePlaceholder')} />
          <small className="case-field-hint">{t('cases.noteWhy')}</small>
        </label>
        {/* Said at the moment the evidence is added, because this is when it becomes true
            and the operator is deciding. */}
        <p className="case-hint"><Ico n="shield" sz={14} /> <span>{t('cases.holdHint')}</span></p>
        <div className="case-dialog-actions">
          <button type="button" className="quiet" data-case-act="cancel" onClick={onClose} disabled={busy}>{t('common.cancel')}</button>
          {/* THE GUARD HAS TO KNOW ABOUT BOTH KINDS OF THING BEING ADDED. A feed entry is
              sent by id and carries no `items`, so a guard that only counted items left this
              button permanently disabled on the Notifications screen — the dialog opened, the
              button looked ordinary, and pressing it did nothing at all. Caught by the screen
              check reading the case back off the SERVER; a check that trusted the dialog
              closing would have called it green. */}
          <button type="button" data-case-act="add" onClick={submit}
            disabled={busy || (items.length === 0 && !notificationId) || (!Number(caseId) && !newTitle.trim())}>
            {busy ? t('common.saving') : t('cases.add')}
          </button>
        </div>
      </div>
    </div>
  );
}

// caseEvidenceFromMoment builds the evidence payload for a bookmarked moment.
export function caseEvidenceFromMoment(cameraId, at, cameraName, label) {
  const centre = Math.round(Number(at) || 0);
  return {
    kind: 'footage',
    cameraId: Number(cameraId) || 0,
    startedAt: Math.max(0, centre - BOOKMARK_PAD_SECONDS),
    endedAt: centre + BOOKMARK_PAD_SECONDS,
    label: label || '',
    cameraName,
  };
}

// ExportPanel starts a case bundle and follows it to the download.
function ExportPanel({ caseId, authHeader, onMessage }) {
  const t = useT();
  const [reason, setReason] = useState('');
  const [job, setJob] = useState(null);
  const [error, setError] = useState('');
  const [busy, setBusy] = useState(false);

  // A new case selection must not inherit the previous case's bundle. Without this the
  // panel would offer a download of case A while sitting under the heading of case B.
  useEffect(() => { setJob(null); setReason(''); setError(''); }, [caseId]);

  const start = useCallback(async () => {
    setBusy(true);
    setError('');
    try {
      const created = await callApi(`/api/cases/${caseId}/export`, {
        method: 'POST', authHeader, body: { reason },
      });
      setJob(created);
    } catch (err) {
      setError(err.message);
    } finally {
      setBusy(false);
    }
  }, [caseId, reason, authHeader]);

  // Poll while the bundle builds. A case of eight clips is minutes of decrypting and
  // joining, so there is nothing to show but progress until it is done.
  useEffect(() => {
    if (!job || job.status === 'ready' || job.status === 'failed') return undefined;
    let cancelled = false;
    const timer = setInterval(async () => {
      try {
        const got = await callApi(`/api/cases/exports/${job.id}`, { authHeader });
        if (!cancelled) setJob(got);
      } catch (err) {
        if (!cancelled) { setError(err.message); setJob(null); }
      }
    }, 1500);
    return () => { cancelled = true; clearInterval(timer); };
  }, [job, authHeader]);

  const missing = Number(job?.caseManifest?.totals?.clipsMissing) || 0;

  return (
    <div className="case-export">
      {error ? <FormAlert message={error} /> : null}
      {!job ? (
        <>
          <ul className="case-export-contents">
            <li><Ico n="film" sz={13} /> <span>{t('cases.exportPart1')}</span></li>
            <li><Ico n="list" sz={13} /> <span>{t('cases.exportPart2')}</span></li>
            <li><Ico n="shield-check" sz={13} /> <span>{t('cases.exportPart3')}</span></li>
          </ul>
          <label className="case-field">
            <span>{t('cases.exportReason')}</span>
            <input value={reason} onChange={(e) => setReason(e.target.value)} disabled={busy}
              placeholder={t('cases.exportReasonPlaceholder')} />
            <small className="case-field-hint">{t('cases.exportReasonWhy')}</small>
          </label>
          <button type="button" onClick={start} disabled={busy || !reason.trim()}>
            <span className="btn-icon"><Ico n="download" /> {busy ? t('common.saving') : t('cases.exportStart')}</span>
          </button>
        </>
      ) : null}
      {job && job.status !== 'ready' && job.status !== 'failed' ? (
        <p className="case-export-status"><span className="case-spinner" aria-hidden="true" /> {t('cases.exportBuilding')}</p>
      ) : null}
      {job?.status === 'failed' ? <FormAlert message={job.error || t('cases.exportFailed')} /> : null}
      {job?.status === 'ready' ? (
        <div className="case-export-ready">
          <p className="case-export-done">
            <Ico n="check-ok" sz={15} /> {t('cases.exportReady', { n: Number(job?.caseManifest?.totals?.clipsWritten) || 0 })}
          </p>
          {/* An incomplete bundle says so HERE as well as inside the zip. The person who
              hands it over is the one who needs to know, and they may never open it. */}
          {missing > 0 ? <FormAlert message={t('cases.exportMissing', { n: missing })} /> : null}
          <div className="case-export-actions">
            <a className="btn-link" href={`${apiBase()}/api/cases/exports/${job.id}/download`}
              onClick={() => onMessage?.(t('cases.exportDownloading'))}>
              <span className="btn-icon"><Ico n="download" /> {t('cases.exportDownload')}</span>
            </a>
            <button type="button" className="quiet" onClick={() => { setJob(null); setReason(''); }}>
              {t('cases.exportAgain')}
            </button>
          </div>
        </div>
      ) : null}
    </div>
  );
}

// CloseCaseDialog makes closing a decision rather than a click, by restating what the
// closure releases.
function CloseCaseDialog({ detail, authHeader, onClose, onClosed }) {
  const t = useT();
  const [outcome, setOutcome] = useState('');
  const [error, setError] = useState('');
  const [busy, setBusy] = useState(false);
  const held = Number(detail?.hold?.segments) || 0;
  const beyond = Number(detail?.hold?.beyondRetention) || 0;

  useEffect(() => {
    const onKey = (e) => { if (e.key === 'Escape') onClose?.(); };
    document.addEventListener('keydown', onKey);
    return () => document.removeEventListener('keydown', onKey);
  }, [onClose]);

  const submit = useCallback(async () => {
    setBusy(true);
    setError('');
    try {
      await callApi(`/api/cases/${detail.case.id}/close`, {
        method: 'POST', authHeader, body: { outcome },
      });
      onClosed?.();
      onClose?.();
    } catch (err) {
      setError(err.message);
    } finally {
      setBusy(false);
    }
  }, [detail, outcome, authHeader, onClose, onClosed]);

  return (
    <div className="video-overlay" onClick={onClose}>
      <div className="video-dialog case-dialog" onClick={(e) => e.stopPropagation()} role="dialog" aria-label={t('cases.closeTitle')}>
        <div className="video-dialog-header">
          <strong>{t('cases.closeTitle')}</strong>
          <button type="button" className="quiet" onClick={onClose} aria-label={t('common.close')}>
            <Ico n="x" />
          </button>
        </div>
        {error ? <FormAlert message={error} /> : null}
        <label className="case-field">
          <span>{t('cases.outcome')}</span>
          <textarea rows={3} value={outcome} onChange={(e) => setOutcome(e.target.value)} disabled={busy}
            placeholder={t('cases.outcomePlaceholder')} />
          <small className="case-field-hint">{t('cases.outcomeWhy')}</small>
        </label>
        {held > 0 ? (
          <FormAlert message={beyond > 0
            ? t('cases.closeReleasesExpired', { n: held, expired: beyond })
            : t('cases.closeReleases', { n: held })} />
        ) : null}
        <div className="case-dialog-actions">
          <button type="button" className="quiet" onClick={onClose} disabled={busy}>{t('common.cancel')}</button>
          <button type="button" onClick={submit} disabled={busy || !outcome.trim()}>
            {busy ? t('common.saving') : t('cases.close')}
          </button>
        </div>
      </div>
    </div>
  );
}

// CaseItemRow is one piece of evidence.
function CaseItemRow({ item, authHeader, editable, onPlay, onNote, onRemove }) {
  const t = useT();
  const [editing, setEditing] = useState(false);
  const [confirmRemove, setConfirmRemove] = useState(false);
  const [note, setNote] = useState(item.note || '');
  const isNote = item.kind === 'note';

  return (
    <li className={`case-item${isNote ? ' case-item--note' : ''}${item.footageMissing ? ' case-item--gone' : ''}`}>
      {!isNote ? (
        Number(item.segmentId) > 0 ? (
          <button type="button" className="case-thumb-btn" onClick={() => onPlay?.(item)}
            aria-label={t('cases.play')} title={t('cases.play')}>
            <CaseThumb segmentId={item.segmentId} seek={item.seek} authHeader={authHeader} />
            <span className="case-thumb-play" aria-hidden="true"><Ico n="play" sz={16} /></span>
          </button>
        ) : <CaseThumb segmentId={item.segmentId} seek={item.seek} authHeader={authHeader} />
      ) : (
        <span className="case-thumb case-thumb--note" aria-hidden="true"><Ico n="edit-2" sz={18} /></span>
      )}
      <div className="case-item-body">
        <div className="case-item-head">
          {!isNote ? (
            <span className="case-item-where">
              <Ico n="camera" sz={13} />
              {item.cameraName || t('cases.cameraN', { n: item.cameraId })}
              {' · '}
              {timeRange(item.startedAt, item.endedAt)}
            </span>
          ) : (
            <span className="case-item-where">
              <Ico n="edit-2" sz={13} />
              {t('cases.noteAt', { time: formatTimestamp(item.startedAt) })}
            </span>
          )}
          {item.label ? <span className="case-item-label">{item.label}</span> : null}
          {/* Footage that is gone is stated on the row. An item that simply refuses to
              play reads as a broken player; this is a fact about the evidence. */}
          {item.footageMissing ? (
            <span className="case-item-missing" title={t('cases.missingHint')}>
              <Ico n="warning" sz={14} /> {t('cases.missing')}
            </span>
          ) : null}
          {/* Partly recorded is neither "here" nor "gone", and saying nothing would let an
              operator believe the clip begins where they marked it. */}
          {!item.footageMissing && item.footageStartsAt ? (
            <span className="case-item-partial">
              {t('cases.footageFrom', { time: formatTimestamp(item.footageStartsAt) })}
            </span>
          ) : null}
        </div>
        {editing ? (
          <div className="case-item-edit">
            <textarea rows={2} value={note} onChange={(e) => setNote(e.target.value)}
              placeholder={t('cases.notePlaceholder')} autoFocus />
            <button type="button" onClick={() => { onNote?.(item, note); setEditing(false); }}>{t('common.save')}</button>
            <button type="button" className="quiet" onClick={() => { setNote(item.note || ''); setEditing(false); }}>
              {t('common.cancel')}
            </button>
          </div>
        ) : (
          <p className="case-item-note">
            {item.note || <em>{editable ? t('cases.noNoteYet') : t('cases.noNote')}</em>}
          </p>
        )}
        <div className="case-item-actions">
          {!isNote && Number(item.segmentId) > 0 ? (
            <button type="button" className="quiet" onClick={() => onPlay?.(item)}>
              <span className="btn-icon"><Ico n="play" sz={14} /> {t('cases.play')}</span>
            </button>
          ) : null}
          {editable && !editing ? (
            <button type="button" className="quiet" onClick={() => setEditing(true)}>
              <span className="btn-icon"><Ico n="edit-2" sz={14} /> {item.note ? t('cases.annotate') : t('cases.addTheNote')}</span>
            </button>
          ) : null}
          {/* Removing evidence releases its footage back to retention, so it asks once
              rather than acting on the first click. The confirmation is inline: a modal
              over a modal-free page would be more ceremony than the action deserves. */}
          {editable && !confirmRemove ? (
            <button type="button" className="quiet danger" onClick={() => setConfirmRemove(true)}>
              <span className="btn-icon"><Ico n="trash" sz={14} /> {t('cases.remove')}</span>
            </button>
          ) : null}
          {editable && confirmRemove ? (
            <span className="case-item-confirm">
              <span>{t('cases.removeConfirm')}</span>
              <button type="button" className="quiet danger" onClick={() => { setConfirmRemove(false); onRemove?.(item); }}>
                {t('cases.removeYes')}
              </button>
              <button type="button" className="quiet" onClick={() => setConfirmRemove(false)}>{t('common.cancel')}</button>
            </span>
          ) : null}
        </div>
      </div>
    </li>
  );
}

// CasesWelcome is what the detail pane shows before a case is chosen. An empty half-screen
// saying "pick a case" teaches nothing; this is the same four steps as the guide, sized
// for the space the screen has anyway, with the one action that starts everything.
function CasesWelcome({ onNew, withSteps }) {
  const t = useT();
  return (
    <div className="cases-welcome">
      <span className="cases-welcome-mark" aria-hidden="true"><Ico n="folder" sz={34} /></span>
      <h4>{t('cases.welcomeTitle')}</h4>
      <p>{t('cases.welcomeBody')}</p>
      {/* Only when the guide above is not already saying it. Two copies of the same four
          steps, one under the other, reads as a page that does not know what it has
          already told you. */}
      {withSteps ? (
        <ul className="cases-welcome-steps">
          {GUIDE_STEPS.map((step) => (
            <li key={step.n}>
              <span aria-hidden="true"><Ico n={step.icon} sz={15} /></span>
              <strong>{t(step.title)}</strong>
              <span className="cases-welcome-body">{t(step.body)}</span>
            </li>
          ))}
        </ul>
      ) : null}
      <button type="button" onClick={onNew}>
        <span className="btn-icon"><Ico n="plus" sz={14} /> {t('cases.new')}</span>
      </button>
    </div>
  );
}

// CasesTab is the whole screen: the list on the left, the open case on the right.
export function CasesTab({ authHeader, onPlaySegment, onMessage }) {
  const t = useT();
  const [status, setStatus] = useState('open');
  const [query, setQuery] = useState('');
  const [rows, setRows] = useState([]);
  const [total, setTotal] = useState(0);
  const [selectedId, setSelectedId] = useState(0);
  const [detail, setDetail] = useState(null);
  const [error, setError] = useState('');
  const [busy, setBusy] = useState(false);
  const [creating, setCreating] = useState(false);
  const [newTitle, setNewTitle] = useState('');
  const [closing, setClosing] = useState(false);
  const [noteDraft, setNoteDraft] = useState('');
  const [users, setUsers] = useState([]);
  const [guide, setGuide] = useState(() => !guideDismissed());
  const [playing, setPlaying] = useState(null);
  // flash is the section the progress rail just jumped to; it is cleared on a timer so the
  // highlight reads as an answer to the click rather than a new permanent state.
  const [flash, setFlash] = useState('');
  const sections = useRef({});

  // Names only, from the cases API rather than the administrator-only user routes — an
  // operator has to be able to hand a case to a colleague.
  useEffect(() => {
    let cancelled = false;
    (async () => {
      try {
        const data = await callApi('/api/cases/assignees', { authHeader });
        if (!cancelled) setUsers(data?.assignees || []);
      } catch (_) {
        // A case that cannot list colleagues is still a usable case: it just cannot be
        // reassigned. Not worth an error banner over the whole screen.
      }
    })();
    return () => { cancelled = true; };
  }, [authHeader]);

  const loadList = useCallback(async () => {
    try {
      const query2 = status === 'all' ? '' : `?status=${status}`;
      const data = await callApi(`/api/cases${query2}`, { authHeader });
      setRows(data?.cases || []);
      setTotal(Number(data?.total) || (data?.cases || []).length);
      setError('');
    } catch (err) {
      setError(err.message);
    }
  }, [status, authHeader]);

  const loadDetail = useCallback(async (id) => {
    if (!id) { setDetail(null); return; }
    try {
      const data = await callApi(`/api/cases/${id}`, { authHeader });
      setDetail(data);
      setError('');
    } catch (err) {
      setError(err.message);
      setDetail(null);
    }
  }, [authHeader]);

  useEffect(() => { loadList(); }, [loadList]);
  useEffect(() => { loadDetail(selectedId); }, [selectedId, loadDetail]);

  useEffect(() => {
    if (!flash) return undefined;
    const timer = setTimeout(() => setFlash(''), 1400);
    return () => clearTimeout(timer);
  }, [flash]);

  const refresh = useCallback(async () => {
    await loadList();
    await loadDetail(selectedId);
  }, [loadList, loadDetail, selectedId]);

  const act = useCallback(async (fn) => {
    setBusy(true);
    try {
      await fn();
      await refresh();
      setError('');
    } catch (err) {
      setError(err.message);
    } finally {
      setBusy(false);
    }
  }, [refresh]);

  const createCase = useCallback(() => act(async () => {
    const created = await callApi('/api/cases', { method: 'POST', authHeader, body: { title: newTitle } });
    setNewTitle('');
    setCreating(false);
    setSelectedId(Number(created?.id) || 0);
  }), [act, authHeader, newTitle]);

  const toggleGuide = useCallback(() => {
    setGuide((prev) => { rememberGuide(!prev); return !prev; });
  }, []);

  // The progress rail's jump. `closing` has no section of its own — the step that closes a
  // case IS the dialog, so pressing it opens the dialog on an open case and scrolls to the
  // recorded outcome on a closed one.
  const jump = useCallback((id) => {
    if (id === 'closing' && detail?.case?.status === 'open') { setClosing(true); return; }
    const target = sections.current[id === 'closing' ? 'outcome' : id];
    if (!target) return;
    target.scrollIntoView({ behavior: 'smooth', block: 'nearest' });
    setFlash(id === 'closing' ? 'outcome' : id);
  }, [detail]);

  const play = useCallback((item) => {
    // A parent that owns a player keeps owning it; otherwise the case plays its own
    // evidence rather than offering a button that does nothing.
    if (onPlaySegment) { onPlaySegment(item); return; }
    setPlaying(item);
  }, [onPlaySegment]);

  const open = detail?.case;
  const editable = open && open.status === 'open';
  const hold = detail?.hold || {};
  const items = detail?.items || [];

  const assignedOptions = useMemo(() => ([{ id: 0, name: t('cases.unassigned') }, ...users]), [users, t]);

  // The search filters the page the server returned, so it says so when there is more than
  // one page. A box that silently searched 100 of 400 cases would be worse than no box.
  const visible = useMemo(() => {
    const q = query.trim().toLowerCase();
    if (!q) return rows;
    return rows.filter((row) => [row.title, row.assignedName, row.openedName, row.outcome]
      .some((v) => String(v || '').toLowerCase().includes(q)));
  }, [rows, query]);

  const truncated = total > rows.length;

  return (
    <section className="workspace cases-page">
      <header className="cases-head">
        <div className="cases-head-titles">
          <h3>{t('cases.heading')}</h3>
          <p>{t('cases.intro')}</p>
        </div>
        <div className="cases-head-actions">
          <button type="button" className="quiet" onClick={toggleGuide} aria-expanded={guide}
            data-case-act="guide">
            <span className="btn-icon"><Ico n="book" sz={14} /> {guide ? t('cases.guideHide') : t('cases.guideShow')}</span>
          </button>
          <HelpButton slug="case-files" />
        </div>
      </header>

      {guide ? <CaseGuide onDismiss={toggleGuide} /> : null}
      {error ? <FormAlert message={error} /> : null}

      <div className="cases-layout">
        <div className="cases-list">
          <div className="cases-list-head">
            <div className="cases-search">
              <Ico n="search" sz={14} />
              <input value={query} onChange={(e) => setQuery(e.target.value)}
                placeholder={t('cases.searchPlaceholder')} aria-label={t('cases.search')} />
              {query ? (
                <button type="button" className="quiet" onClick={() => setQuery('')}
                  aria-label={t('cases.searchClear')}><Ico n="x" sz={13} /></button>
              ) : null}
            </div>
            <button type="button" onClick={() => setCreating(true)} data-case-act="new">
              <span className="btn-icon"><Ico n="plus" sz={14} /> {t('cases.new')}</span>
            </button>
          </div>

          {/* A segmented control rather than a dropdown: three choices that are switched
              between constantly should cost one click, not two, and the one in force
              should be readable without opening anything. */}
          <div className="cases-filter" role="group" aria-label={t('cases.filterStatus')}>
            {['open', 'closed', 'all'].map((value) => (
              <button key={value} type="button"
                className={`cases-filter-btn${status === value ? ' active' : ''}`}
                aria-pressed={status === value}
                onClick={() => setStatus(value)}>
                {t(`cases.filter${value.charAt(0).toUpperCase()}${value.slice(1)}`)}
                {status === value ? <span className="cases-filter-count">{total}</span> : null}
              </button>
            ))}
          </div>

          {creating ? (
            <div className="cases-new">
              <label className="case-field">
                <span>{t('cases.title')}</span>
                <input value={newTitle} onChange={(e) => setNewTitle(e.target.value)}
                  onKeyDown={(e) => { if (e.key === 'Enter' && newTitle.trim()) createCase(); }}
                  placeholder={t('cases.titlePlaceholder')} aria-label={t('cases.title')} autoFocus />
              </label>
              <p className="case-hint"><Ico n="info" sz={13} /> <span>{t('cases.newHint')}</span></p>
              <div className="cases-new-actions">
                <button type="button" className="quiet" onClick={() => { setCreating(false); setNewTitle(''); }}>
                  {t('common.cancel')}
                </button>
                <button type="button" onClick={createCase} disabled={busy || !newTitle.trim()}>{t('cases.newCreate')}</button>
              </div>
            </div>
          ) : null}

          {rows.length === 0 ? (
            <div className="cases-list-empty">
              <Ico n="folder" sz={22} />
              <p>{status === 'closed' ? t('cases.noneClosed') : t('cases.none')}</p>
              {status !== 'closed' ? <button type="button" className="quiet" onClick={() => setCreating(true)}>{t('cases.new')}</button> : null}
            </div>
          ) : null}
          {rows.length > 0 && visible.length === 0 ? (
            <p className="muted cases-search-none">{t('cases.searchNone', { q: query.trim() })}</p>
          ) : null}

          <ul>
            {visible.map((row) => (
              <li key={row.id}>
                <button type="button"
                  className={`cases-list-item${row.id === selectedId ? ' active' : ''}`}
                  aria-current={row.id === selectedId ? 'true' : undefined}
                  onClick={() => setSelectedId(row.id)}>
                  <span className="cases-list-top">
                    <span className="cases-list-title">{row.title}</span>
                    <span className={`case-status case-status--${row.status}`}>{t(`cases.status.${row.status}`)}</span>
                  </span>
                  <span className="cases-list-meta">
                    <span className="cases-list-chip"><Ico n="film" sz={12} /> {row.itemCount}</span>
                    {Number(row.noteCount) > 0 ? (
                      <span className="cases-list-chip"><Ico n="edit-2" sz={12} /> {row.noteCount}</span>
                    ) : null}
                    <span className="cases-list-when">{shortStamp(row.openedAt)}</span>
                  </span>
                  {row.assignedName ? (
                    <span className="cases-list-who">
                      <span className="case-avatar" aria-hidden="true">{initials(row.assignedName)}</span>
                      {row.assignedName}
                    </span>
                  ) : (
                    <span className="cases-list-who cases-list-who--none">
                      <span className="case-avatar case-avatar--none" aria-hidden="true"><Ico n="user" sz={11} /></span>
                      {t('cases.unassigned')}
                    </span>
                  )}
                </button>
              </li>
            ))}
          </ul>
          {truncated ? <p className="muted cases-list-truncated">{t('cases.listTruncated', { n: rows.length, total })}</p> : null}
        </div>

        <div className="cases-detail">
          {!open ? <CasesWelcome onNew={() => setCreating(true)} withSteps={!guide} /> : (
            <>
              <div className="cases-detail-head">
                <div className="cases-detail-titles">
                  <h4>{open.title}</h4>
                  <p className="muted">
                    {t('cases.openedBy', { who: open.openedName, when: formatTimestamp(open.openedAt) })}
                  </p>
                </div>
                <span className={`case-status case-status--${open.status}`}>{t(`cases.status.${open.status}`)}</span>
              </div>

              <CaseSteps detail={detail} onJump={jump} />

              {/* The numbers the case rests on, read at a glance. "Only here for this case"
                  is the one that changes what an operator does next, so it is a tile of its
                  own and it turns when it is not zero. */}
              <div className="case-stats">
                <StatCard label={t('cases.statEvidence')} value={items.length}
                  icon={<Ico n="film" sz={15} />} hint={t('cases.statEvidenceHint')} />
                <StatCard label={t('cases.statClips')} value={Number(hold.segments) || 0}
                  icon={<Ico n="shield" sz={15} />} hint={bytesLabel(hold.bytes)}
                  tone={open.status === 'open' && Number(hold.segments) > 0 ? 'success' : 'default'} />
                <StatCard label={t('cases.statAtRisk')} value={Number(hold.beyondRetention) || 0}
                  icon={<Ico n="warning" sz={15} />} hint={t('cases.statAtRiskHint')}
                  tone={Number(hold.beyondRetention) > 0 ? 'warning' : 'default'} />
                <StatCard label={t('cases.statGone')} value={Number(hold.missing) || 0}
                  icon={<Ico n="eye-off" sz={15} />} hint={t('cases.statGoneHint')}
                  tone={Number(hold.missing) > 0 ? 'danger' : 'default'} />
              </div>

              {/* The hold, stated where the case is read rather than buried. */}
              {/* Green is the colour of "your evidence is safe". A case that is holding
                  NOTHING has not earned it, and a reassuring panel over zero clips is the
                  exact misreading this panel exists to prevent. */}
              <div className={`case-hold-panel${open.status === 'closed' ? ' is-released' : ''}${open.status === 'open' && !Number(hold.segments) ? ' is-empty' : ''}`}>
                <Ico n={open.status === 'closed' ? 'lock' : 'shield-check'} sz={18} />
                <div>
                  <strong>{t('cases.holdTitle', { n: hold.segments || 0, size: bytesLabel(hold.bytes) })}</strong>
                  {open.status === 'open' && !Number(hold.segments) ? (
                    <p className="muted">{t('cases.holdNothingYet')}</p>
                  ) : null}
                  {Number(hold.beyondRetention) > 0 ? (
                    <p className="case-hold-beyond">{t('cases.holdBeyond', { n: hold.beyondRetention })}</p>
                  ) : null}
                  {Number(hold.missing) > 0 ? (
                    <p className="case-hold-missing">{t('cases.holdMissing', { n: hold.missing })}</p>
                  ) : null}
                  {open.status === 'closed' ? <p className="muted">{t('cases.holdReleased')}</p> : null}
                </div>
              </div>

              <section className={`case-section${flash === 'summary' ? ' is-flash' : ''}`}
                ref={(el) => { sections.current.summary = el; }}>
                <div className="case-section-head">
                  <h5><Ico n="edit-2" sz={14} /> {t('cases.sectionCase')}</h5>
                  <p>{t('cases.sectionCaseHint')}</p>
                </div>
                <label className="case-field">
                  <span>{t('cases.summary')}</span>
                  <textarea rows={3} defaultValue={open.summary} disabled={!editable || busy}
                    key={`summary-${open.id}`}
                    placeholder={editable ? t('cases.summaryPlaceholder') : ''}
                    onBlur={(e) => {
                      if (!editable || e.target.value === (open.summary || '')) return;
                      act(() => callApi(`/api/cases/${open.id}`, {
                        method: 'POST', authHeader, body: { summary: e.target.value },
                      }));
                    }} />
                  {editable ? <small className="case-field-hint">{t('cases.autosaveHint')}</small> : null}
                </label>
                <label className="case-field">
                  <span>{t('cases.assignee')}</span>
                  <select value={open.assignedTo || 0} disabled={!editable || busy}
                    onChange={(e) => act(() => callApi(`/api/cases/${open.id}`, {
                      method: 'POST', authHeader, body: { assignedTo: Number(e.target.value) },
                    }))}>
                    {assignedOptions.map((u) => (
                      <option key={u.id} value={u.id}>{u.name || u.id}</option>
                    ))}
                  </select>
                  <small className="case-field-hint">{t('cases.assigneeHint')}</small>
                </label>
              </section>

              {open.status === 'closed' ? (
                <section className={`case-section case-outcome${flash === 'outcome' ? ' is-flash' : ''}`}
                  ref={(el) => { sections.current.outcome = el; }}>
                  <div className="case-section-head">
                    <h5><Ico n="check-ok" sz={14} /> {t('cases.outcome')}</h5>
                  </div>
                  <p>{open.outcome}</p>
                  <p className="muted">{t('cases.closedBy', { who: open.closedName, when: formatTimestamp(open.closedAt) })}</p>
                </section>
              ) : null}

              <section className={`case-section${flash === 'evidence' ? ' is-flash' : ''}`}
                ref={(el) => { sections.current.evidence = el; }}>
                <div className="case-section-head">
                  <h5><Ico n="film" sz={14} /> {t('cases.evidence', { n: items.length })}</h5>
                  <p>{t('cases.sectionEvidenceHint')}</p>
                </div>

                {items.length === 0 ? (
                  // The empty state names the screens the evidence comes FROM. "Nothing
                  // here yet" would be true and useless: the reason a case is empty is
                  // almost always that the operator does not know where the button is.
                  <div className="case-evidence-empty">
                    <Ico n="film" sz={22} />
                    <strong>{t('cases.noEvidenceTitle')}</strong>
                    <ul>
                      <li><Ico n="activity" sz={13} /> <span>{t('cases.fromTimeline')}</span></li>
                      <li><Ico n="search" sz={13} /> <span>{t('cases.fromObjects')}</span></li>
                      <li><Ico n="bell" sz={13} /> <span>{t('cases.fromNotifications')}</span></li>
                      <li><Ico n="edit-2" sz={13} /> <span>{t('cases.fromNote')}</span></li>
                    </ul>
                  </div>
                ) : (
                  <ul className="case-items">
                    {items.map((item) => (
                      <CaseItemRow key={item.id} item={item} authHeader={authHeader} editable={editable}
                        onPlay={play}
                        onNote={(it, note) => act(() => callApi(`/api/cases/${open.id}/items/${it.id}`, {
                          method: 'POST', authHeader, body: { note },
                        }))}
                        onRemove={(it) => act(() => callApi(`/api/cases/${open.id}/items/${it.id}/remove`, {
                          method: 'POST', authHeader,
                        }))} />
                    ))}
                  </ul>
                )}

                {editable ? (
                  <div className="case-add-note">
                    <textarea rows={2} value={noteDraft} onChange={(e) => setNoteDraft(e.target.value)}
                      placeholder={t('cases.addNotePlaceholder')} aria-label={t('cases.note')} />
                    <button type="button" disabled={busy || !noteDraft.trim()}
                      onClick={() => act(async () => {
                        await callApi(`/api/cases/${open.id}/items`, {
                          method: 'POST', authHeader, body: { kind: 'note', note: noteDraft },
                        });
                        setNoteDraft('');
                      })}>
                      <span className="btn-icon"><Ico n="plus" sz={14} /> {t('cases.addNote')}</span>
                    </button>
                  </div>
                ) : null}
              </section>

              <section className="case-section" ref={(el) => { sections.current.export = el; }}>
                <div className="case-section-head">
                  <h5><Ico n="download" sz={14} /> {t('cases.exportTitle')}</h5>
                  <p>{t('cases.exportHint')}</p>
                </div>
                <ExportPanel caseId={open.id} authHeader={authHeader} onMessage={onMessage} />
              </section>

              <div className="case-detail-actions">
                {editable ? (
                  <>
                    <p className="case-detail-actions-hint">{t('cases.closeWhy')}</p>
                    <button type="button" onClick={() => setClosing(true)} disabled={busy}>
                      <span className="btn-icon"><Ico n="check-ok" sz={14} /> {t('cases.close')}</span>
                    </button>
                  </>
                ) : (
                  <>
                    <p className="case-detail-actions-hint">{t('cases.reopenWhy')}</p>
                    <button type="button" onClick={() => act(() => callApi(`/api/cases/${open.id}/reopen`, {
                      method: 'POST', authHeader,
                    }))} disabled={busy}>
                      <span className="btn-icon"><Ico n="refresh" sz={14} /> {t('cases.reopen')}</span>
                    </button>
                  </>
                )}
              </div>
            </>
          )}
        </div>
      </div>

      {closing && detail ? (
        <CloseCaseDialog detail={detail} authHeader={authHeader}
          onClose={() => setClosing(false)}
          onClosed={() => { setClosing(false); refresh(); }} />
      ) : null}

      {playing ? (
        <EvidencePlayer item={playing} authHeader={authHeader} onClose={() => setPlaying(null)} />
      ) : null}
    </section>
  );
}
