import { useState, useEffect, useCallback, useMemo } from 'react';
import { Ico } from './icons';
import { FormAlert } from './ui';
import { useT } from '@shared/i18n';
import { HelpButton } from '@shared/Manual';
import { apiBase, formatTimestamp } from '../lib/helpers';

// Case files: the investigation, on screen.
//
// The screen is built around one claim the operator has to be able to trust: WHAT IS IN
// THIS CASE IS STILL HERE. So the hold panel is not a status line at the bottom, it is at
// the top beside the status, it says how many clips are only still on disk because the
// case is open, and closing the case restates that number in the dialog. A case screen
// that shows a tidy list of evidence and lets retention quietly delete it underneath is
// worse than no case screen, because it is believed.

// bookmarkPad is how much footage a one-click bookmark takes around the chosen moment.
// A bookmark is a moment to a person and a span to the exporter; padding it here rather
// than storing an instant is what makes "bookmark this" produce something playable.
const BOOKMARK_PAD_SECONDS = 30;

async function callApi(path, { method = 'GET', body, authHeader } = {}) {
  const headers = { ...(authHeader ? { Authorization: authHeader } : {}) };
  if (body !== undefined) headers['Content-Type'] = 'application/json';
  const r = await fetch(`${apiBase()}${path}`, {
    method,
    credentials: 'include',
    headers,
    body: body === undefined ? undefined : JSON.stringify(body),
  });
  const payload = await r.json().catch(() => null);
  const data = payload?.data?.result ?? payload?.result ?? payload?.data ?? payload;
  if (!r.ok) {
    // The server's own words, always. "This case is closed — reopen it before adding
    // evidence" is a different fact from "something went wrong", and only one of them
    // tells the operator what to do next.
    const message = payload?.message || payload?.data?.message || `HTTP ${r.status}`;
    throw new Error(message);
  }
  return data;
}

function bytesLabel(bytes) {
  const n = Number(bytes) || 0;
  if (n <= 0) return '0 MB';
  if (n < 1024 * 1024) return `${Math.max(1, Math.round(n / 1024))} KB`;
  if (n < 1024 * 1024 * 1024) return `${Math.round(n / (1024 * 1024))} MB`;
  return `${(n / (1024 * 1024 * 1024)).toFixed(1)} GB`;
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

// AddToCaseDialog is the "put this in a case" action, shared by every screen that can
// produce evidence — the timeline, the object grid, the alert log. One dialog rather than
// one per screen, because the thing being added is the same shape everywhere.
//
// It takes a LIST, not one item, because the timeline's unit is a moment across several
// cameras: bookmarking 14:07 with four tiles up means four pieces of evidence, and making
// the operator do that four times would be the screen fighting its own design.
export function AddToCaseDialog({ items = [], authHeader, onClose, onAdded }) {
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
      // Sequential, and the first failure stops the run with its own message. Adding
      // four clips and reporting a single "failed" after silently landing two is worse
      // than stopping: the operator has to know what is in the case.
      for (const item of items) {
        await callApi(`/api/cases/${target}/items`, {
          method: 'POST', authHeader,
          body: { ...item, note },
        });
      }
      onAdded?.(target);
      onClose?.();
    } catch (err) {
      setError(err.message);
    } finally {
      setBusy(false);
    }
  }, [caseId, newTitle, note, items, authHeader, onAdded, onClose, t]);

  return (
    <div className="video-overlay" onClick={onClose}>
      <div className="video-dialog case-dialog" onClick={(e) => e.stopPropagation()} role="dialog" aria-label={t('cases.addTitle')}>
        <div className="video-dialog-header">
          <strong>{t('cases.addTitle')}</strong>
          <button type="button" className="quiet" onClick={onClose} aria-label={t('common.close')}>
            <Ico n="x" />
          </button>
        </div>
        <ul className="case-evidence-lines">
          {items.map((item, i) => (
            <li key={`${item.cameraId}-${item.startedAt}-${i}`}>
              {item.cameraName ? `${item.cameraName} · ` : ''}
              {formatTimestamp(item.startedAt)} – {formatTimestamp(item.endedAt)}
            </li>
          ))}
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
        </label>
        {/* Said at the moment the evidence is added, because this is when it becomes true
            and the operator is deciding. */}
        <p className="case-hint"><Ico n="info" sz={14} /> <span>{t('cases.holdHint')}</span></p>
        <div className="case-dialog-actions">
          <button type="button" className="quiet" onClick={onClose} disabled={busy}>{t('common.cancel')}</button>
          <button type="button" onClick={submit}
            disabled={busy || items.length === 0 || (!Number(caseId) && !newTitle.trim())}>
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
      <h4>{t('cases.exportTitle')}</h4>
      {error ? <FormAlert message={error} /> : null}
      {!job ? (
        <>
          <label className="case-field">
            <span>{t('cases.exportReason')}</span>
            <input value={reason} onChange={(e) => setReason(e.target.value)} disabled={busy}
              placeholder={t('cases.exportReasonPlaceholder')} />
          </label>
          <p className="case-hint">{t('cases.exportHint')}</p>
          <button type="button" onClick={start} disabled={busy || !reason.trim()}>
            <span className="btn-icon"><Ico n="download" /> {busy ? t('common.saving') : t('cases.exportStart')}</span>
          </button>
        </>
      ) : null}
      {job && job.status !== 'ready' && job.status !== 'failed' ? (
        <p className="case-export-status">{t('cases.exportBuilding')}</p>
      ) : null}
      {job?.status === 'failed' ? <FormAlert message={job.error || t('cases.exportFailed')} /> : null}
      {job?.status === 'ready' ? (
        <div className="case-export-ready">
          <p>{t('cases.exportReady', { n: Number(job?.caseManifest?.totals?.clipsWritten) || 0 })}</p>
          {/* An incomplete bundle says so HERE as well as inside the zip. The person who
              hands it over is the one who needs to know, and they may never open it. */}
          {missing > 0 ? <FormAlert message={t('cases.exportMissing', { n: missing })} /> : null}
          <a className="btn-link" href={`${apiBase()}/api/cases/exports/${job.id}/download`}
            onClick={() => onMessage?.(t('cases.exportDownloading'))}>
            <span className="btn-icon"><Ico n="download" /> {t('cases.exportDownload')}</span>
          </a>
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
  const [note, setNote] = useState(item.note || '');
  const isNote = item.kind === 'note';

  return (
    <li className={`case-item${isNote ? ' case-item--note' : ''}`}>
      {!isNote ? <CaseThumb segmentId={item.segmentId} seek={item.seek} authHeader={authHeader} /> : (
        <span className="case-thumb case-thumb--note" aria-hidden="true"><Ico n="edit-2" sz={18} /></span>
      )}
      <div className="case-item-body">
        <div className="case-item-head">
          {!isNote ? (
            <span className="case-item-where">
              {item.cameraName || t('cases.cameraN', { n: item.cameraId })}
              {' · '}
              {formatTimestamp(item.startedAt)} – {formatTimestamp(item.endedAt)}
            </span>
          ) : (
            <span className="case-item-where">{t('cases.noteAt', { time: formatTimestamp(item.startedAt) })}</span>
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
            <textarea rows={2} value={note} onChange={(e) => setNote(e.target.value)} />
            <button type="button" onClick={() => { onNote?.(item, note); setEditing(false); }}>{t('common.save')}</button>
            <button type="button" className="quiet" onClick={() => { setNote(item.note || ''); setEditing(false); }}>
              {t('common.cancel')}
            </button>
          </div>
        ) : (
          <p className="case-item-note">{item.note || <em>{t('cases.noNote')}</em>}</p>
        )}
        <div className="case-item-actions">
          {!isNote && Number(item.segmentId) > 0 ? (
            <button type="button" className="quiet" onClick={() => onPlay?.(item)}>
              <span className="btn-icon"><Ico n="play" sz={14} /> {t('cases.play')}</span>
            </button>
          ) : null}
          {editable ? (
            <>
              <button type="button" className="quiet" onClick={() => setEditing(true)}>
                <span className="btn-icon"><Ico n="edit-2" sz={14} /> {t('cases.annotate')}</span>
              </button>
              <button type="button" className="quiet danger" onClick={() => onRemove?.(item)}>
                <span className="btn-icon"><Ico n="trash" sz={14} /> {t('cases.remove')}</span>
              </button>
            </>
          ) : null}
        </div>
      </div>
    </li>
  );
}

// CasesTab is the whole screen: the list on the left, the open case on the right.
export function CasesTab({ authHeader, onPlaySegment, onMessage }) {
  const t = useT();
  const [status, setStatus] = useState('open');
  const [rows, setRows] = useState([]);
  const [selectedId, setSelectedId] = useState(0);
  const [detail, setDetail] = useState(null);
  const [error, setError] = useState('');
  const [busy, setBusy] = useState(false);
  const [creating, setCreating] = useState(false);
  const [newTitle, setNewTitle] = useState('');
  const [closing, setClosing] = useState(false);
  const [noteDraft, setNoteDraft] = useState('');
  const [users, setUsers] = useState([]);

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
      const query = status === 'all' ? '' : `?status=${status}`;
      const data = await callApi(`/api/cases${query}`, { authHeader });
      setRows(data?.cases || []);
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

  const open = detail?.case;
  const editable = open && open.status === 'open';
  const hold = detail?.hold || {};

  const assignedOptions = useMemo(() => ([{ id: 0, name: t('cases.unassigned') }, ...users]), [users, t]);

  return (
    <section className="panel cases-panel">
      <div className="panel-head">
        <h3>{t('cases.heading')}</h3>
        <HelpButton slug="case-files" />
      </div>
      {error ? <FormAlert message={error} /> : null}

      <div className="cases-layout">
        <div className="cases-list">
          <div className="cases-list-head">
            <select value={status} onChange={(e) => setStatus(e.target.value)} aria-label={t('cases.filterStatus')}>
              <option value="open">{t('cases.filterOpen')}</option>
              <option value="closed">{t('cases.filterClosed')}</option>
              <option value="all">{t('cases.filterAll')}</option>
            </select>
            <button type="button" onClick={() => setCreating(true)}>
              <span className="btn-icon"><Ico n="plus" sz={14} /> {t('cases.new')}</span>
            </button>
          </div>
          {creating ? (
            <div className="cases-new">
              <input value={newTitle} onChange={(e) => setNewTitle(e.target.value)}
                placeholder={t('cases.titlePlaceholder')} aria-label={t('cases.title')} />
              <button type="button" onClick={createCase} disabled={busy || !newTitle.trim()}>{t('common.save')}</button>
              <button type="button" className="quiet" onClick={() => { setCreating(false); setNewTitle(''); }}>
                {t('common.cancel')}
              </button>
            </div>
          ) : null}
          {rows.length === 0 ? <p className="muted">{t('cases.none')}</p> : null}
          <ul>
            {rows.map((row) => (
              <li key={row.id}>
                <button type="button"
                  className={`cases-list-item${row.id === selectedId ? ' active' : ''}`}
                  onClick={() => setSelectedId(row.id)}>
                  <span className="cases-list-title">{row.title}</span>
                  <span className="cases-list-meta">
                    <span className={`case-status case-status--${row.status}`}>{t(`cases.status.${row.status}`)}</span>
                    {t('cases.itemCount', { n: row.itemCount })}
                    {row.assignedName ? ` · ${row.assignedName}` : ''}
                  </span>
                </button>
              </li>
            ))}
          </ul>
        </div>

        <div className="cases-detail">
          {!open ? <p className="muted">{t('cases.pick')}</p> : (
            <>
              <div className="cases-detail-head">
                <div>
                  <h4>{open.title}</h4>
                  <p className="muted">
                    {t('cases.openedBy', { who: open.openedName, when: formatTimestamp(open.openedAt) })}
                  </p>
                </div>
                <span className={`case-status case-status--${open.status}`}>{t(`cases.status.${open.status}`)}</span>
              </div>

              {/* The hold, stated where the case is read rather than buried. */}
              <div className="case-hold-panel">
                <Ico n="shield" sz={16} />
                <div>
                  <strong>{t('cases.holdTitle', { n: hold.segments || 0, size: bytesLabel(hold.bytes) })}</strong>
                  {Number(hold.beyondRetention) > 0 ? (
                    <p className="case-hold-beyond">{t('cases.holdBeyond', { n: hold.beyondRetention })}</p>
                  ) : null}
                  {Number(hold.missing) > 0 ? (
                    <p className="case-hold-missing">{t('cases.holdMissing', { n: hold.missing })}</p>
                  ) : null}
                  {open.status === 'closed' ? <p className="muted">{t('cases.holdReleased')}</p> : null}
                </div>
              </div>

              <label className="case-field">
                <span>{t('cases.summary')}</span>
                <textarea rows={3} defaultValue={open.summary} disabled={!editable || busy}
                  onBlur={(e) => {
                    if (!editable || e.target.value === (open.summary || '')) return;
                    act(() => callApi(`/api/cases/${open.id}`, {
                      method: 'POST', authHeader, body: { summary: e.target.value },
                    }));
                  }} />
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
              </label>

              {open.status === 'closed' ? (
                <div className="case-outcome">
                  <strong>{t('cases.outcome')}</strong>
                  <p>{open.outcome}</p>
                  <p className="muted">{t('cases.closedBy', { who: open.closedName, when: formatTimestamp(open.closedAt) })}</p>
                </div>
              ) : null}

              <h4 className="case-evidence-heading">{t('cases.evidence', { n: (detail.items || []).length })}</h4>
              <ul className="case-items">
                {(detail.items || []).map((item) => (
                  <CaseItemRow key={item.id} item={item} authHeader={authHeader} editable={editable}
                    onPlay={(it) => onPlaySegment?.(it)}
                    onNote={(it, note) => act(() => callApi(`/api/cases/${open.id}/items/${it.id}`, {
                      method: 'POST', authHeader, body: { note },
                    }))}
                    onRemove={(it) => act(() => callApi(`/api/cases/${open.id}/items/${it.id}/remove`, {
                      method: 'POST', authHeader,
                    }))} />
                ))}
              </ul>
              {(detail.items || []).length === 0 ? <p className="muted">{t('cases.noEvidence')}</p> : null}

              {editable ? (
                <div className="case-add-note">
                  <textarea rows={2} value={noteDraft} onChange={(e) => setNoteDraft(e.target.value)}
                    placeholder={t('cases.notePlaceholder')} aria-label={t('cases.note')} />
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

              <ExportPanel caseId={open.id} authHeader={authHeader} onMessage={onMessage} />

              <div className="case-detail-actions">
                {editable ? (
                  <button type="button" onClick={() => setClosing(true)} disabled={busy}>
                    <span className="btn-icon"><Ico n="check-ok" sz={14} /> {t('cases.close')}</span>
                  </button>
                ) : (
                  <button type="button" onClick={() => act(() => callApi(`/api/cases/${open.id}/reopen`, {
                    method: 'POST', authHeader,
                  }))} disabled={busy}>
                    <span className="btn-icon"><Ico n="refresh" sz={14} /> {t('cases.reopen')}</span>
                  </button>
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
    </section>
  );
}
