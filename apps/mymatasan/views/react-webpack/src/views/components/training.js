import { useState, useEffect, useCallback, useRef } from 'react';
import { Ico } from './icons';
import { useT } from '@shared/i18n';
import { ConsoleLog } from './console';
import { ObjectClassesPanel } from './vision';
import { apiBase, cameraTitle } from '../lib/helpers';

// api is a thin authed fetch wrapper that unwraps the standard response envelope
// and leaves FormData bodies untouched (so the browser sets the multipart boundary).
async function api(path, authHeader, options = {}) {
  const headers = { ...(options.headers || {}) };
  if (authHeader) {
    headers.Authorization = authHeader;
  }
  if (options.body && !(options.body instanceof FormData)) {
    headers['Content-Type'] = 'application/json';
  }
  const resp = await fetch(`${apiBase()}${path}`, { credentials: 'include', ...options, headers });
  const text = await resp.text();
  let payload = null;
  if (text) {
    try { payload = JSON.parse(text); } catch (_) { payload = { message: text }; }
  }
  if (!resp.ok) {
    throw new Error(payload?.message || payload?.data?.message || `Request failed (${resp.status})`);
  }
  return payload?.data?.result ?? payload?.result ?? payload;
}

// AuthBlobImage loads an image from an authed endpoint as a blob (these endpoints
// need the Authorization header, which a plain <img> cannot send).
function AuthBlobImage({ path, authHeader, alt = '' }) {
  const [url, setUrl] = useState(null);
  useEffect(() => {
    let revoked = false;
    let objectUrl = null;
    const headers = authHeader ? { Authorization: authHeader } : {};
    fetch(`${apiBase()}${path}`, { credentials: 'include', headers })
      .then((r) => (r.ok ? r.blob() : Promise.reject(new Error('load failed'))))
      .then((blob) => {
        if (!revoked) {
          objectUrl = URL.createObjectURL(blob);
          setUrl(objectUrl);
        }
      })
      .catch(() => {});
    return () => {
      revoked = true;
      if (objectUrl) URL.revokeObjectURL(objectUrl);
    };
  }, [path, authHeader]);
  return url ? <img src={url} alt={alt} /> : <div className="training-thumb-empty" />;
}

// AlertPreviewModal shows an alert's snapshot and, when a recording segment was
// captured for it, plays that clip — so the user can review context before
// importing the frame as a training sample.
function AlertPreviewModal({ alert, authHeader, busy, onAdd, onClose }) {
  const t = useT();
  const [videoUrl, setVideoUrl] = useState(null);
  const [loadingVideo, setLoadingVideo] = useState(true);
  const [noClip, setNoClip] = useState(false);

  useEffect(() => {
    let revoked = false;
    let objectUrl = null;
    setVideoUrl(null);
    setLoadingVideo(true);
    setNoClip(false);
    const headers = authHeader ? { Authorization: authHeader } : {};
    (async () => {
      try {
        const segResp = await fetch(`${apiBase()}/api/recording/segments?alertId=${alert.id}&limit=1`, { credentials: 'include', headers });
        const segPayload = await segResp.json().catch(() => null);
        const segResult = segPayload?.data?.result ?? segPayload?.result ?? segPayload;
        const seg = Array.isArray(segResult?.items) ? segResult.items[0] : null;
        if (!seg) {
          if (!revoked) { setNoClip(true); setLoadingVideo(false); }
          return;
        }
        const vidResp = await fetch(`${apiBase()}/api/recording/segments/${seg.id}/download`, { credentials: 'include', headers });
        if (!vidResp.ok) throw new Error('clip download failed');
        const blob = await vidResp.blob();
        if (!revoked) {
          objectUrl = URL.createObjectURL(blob);
          setVideoUrl(objectUrl);
          setLoadingVideo(false);
        }
      } catch (_) {
        if (!revoked) { setNoClip(true); setLoadingVideo(false); }
      }
    })();
    return () => {
      revoked = true;
      if (objectUrl) URL.revokeObjectURL(objectUrl);
    };
  }, [alert.id, authHeader]);

  useEffect(() => {
    const onKey = (e) => { if (e.key === 'Escape') onClose(); };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, [onClose]);

  return (
    <div className="video-overlay" onClick={onClose}>
      <div className="video-dialog" onClick={(e) => e.stopPropagation()}>
        <div className="video-dialog-header">
          <span className="video-dialog-title">{alert.label || alert.detectionType} · #{alert.id}</span>
          <button type="button" className="video-dialog-close" onClick={onClose} aria-label={t('common.close')}>✕</button>
        </div>
        <div className="video-dialog-body">
          {videoUrl ? (
            <video className="video-player" controls autoPlay muted src={videoUrl} />
          ) : (
            <AuthBlobImage path={`/api/vision/alerts/${alert.id}/snapshot`} authHeader={authHeader} alt={t('tr.altAlertSnapshot')} />
          )}
        </div>
        <div className="video-dialog-meta">
          {loadingVideo ? t('tr.loadingClip') : noClip ? t('tr.noClip') : t('tr.cameraN', { id: alert.cameraId })}
        </div>
        <div className="action-row">
          <button type="button" onClick={onAdd} disabled={busy}>
            <span className="btn-icon"><Ico n="plus" /> {t('tr.addToDataset')}</span>
          </button>
          <button type="button" className="quiet" onClick={onClose}>{t('common.close')}</button>
        </div>
      </div>
    </div>
  );
}

const emptyDatasetDraft = { name: '', description: '', classes: '' };

function parseAnnotations(value) {
  if (!value) return [];
  try {
    const arr = JSON.parse(value);
    if (!Array.isArray(arr)) return [];
    return arr
      .filter((a) => a && Number(a.w) > 0 && Number(a.h) > 0)
      .map((a) => ({
        className: String(a.className || '').toLowerCase(),
        x: Number(a.x) || 0,
        y: Number(a.y) || 0,
        w: Number(a.w) || 0,
        h: Number(a.h) || 0,
        source: a.source || 'manual',
      }));
  } catch (_) {
    return [];
  }
}

function annotationCount(value) {
  return parseAnnotations(value).length;
}

function modelClasses(model) {
  try {
    const arr = JSON.parse(model?.classes || '[]');
    return Array.isArray(arr) ? arr : [];
  } catch (_) {
    return [];
  }
}

function clamp01(v) { return Math.min(1, Math.max(0, v)); }

// nextBoxName returns the next "boxN" placeholder name for a freshly drawn box,
// continuing past the highest existing boxN so names stay unique.
function nextBoxName(existing) {
  let max = 0;
  (existing || []).forEach((b) => {
    const m = /^box(\d+)$/.exec(b.className || '');
    if (m) max = Math.max(max, Number(m[1]));
  });
  return `box${max + 1}`;
}

// ImageAnnotator is the box-labeling editor: draw boxes over the image, assign a
// class to each, run auto-label, then save. Coordinates are normalized 0..1 and
// the SVG overlay maps them to the displayed image via percentage units.
function ImageAnnotator({ image, dataset, authHeader, busy, onSave, onAutoLabel, onClose }) {
  const t = useT();
  const [boxes, setBoxes] = useState(() => parseAnnotations(image.annotations));
  const [selected, setSelected] = useState(-1);
  const [checked, setChecked] = useState(() => new Set());
  const [classOptions, setClassOptions] = useState([]);
  const [blobUrl, setBlobUrl] = useState(null);
  const [drag, setDrag] = useState(null); // {mode:'draw'|'move', ...}
  const canvasRef = useRef(null);

  // Track unsaved changes so closing without saving warns the user (the editor
  // is local until Save persists it).
  const initialJsonRef = useRef(JSON.stringify(parseAnnotations(image.annotations)));
  const dirty = JSON.stringify(boxes) !== initialJsonRef.current;
  const boxesRef = useRef(boxes);
  boxesRef.current = boxes;
  const requestClose = useCallback(() => {
    const dirtyNow = JSON.stringify(boxesRef.current) !== initialJsonRef.current;
    if (dirtyNow && !window.confirm(t('tr.discardConfirm'))) return;
    onClose();
  }, [onClose]);

  // Seed class options from the dataset's declared classes plus any classes the
  // existing/auto annotations already use.
  useEffect(() => {
    const datasetClasses = parseAnnotations(image.annotations).map((a) => a.className);
    const declared = (() => {
      try { return JSON.parse(dataset?.classes || '[]'); } catch (_) { return []; }
    })();
    const opts = Array.from(new Set([...declared, ...datasetClasses].map((c) => String(c).toLowerCase()).filter(Boolean)));
    setClassOptions(opts);
  }, [image.id, dataset?.id]);

  useEffect(() => {
    let revoked = false;
    let objectUrl = null;
    const headers = authHeader ? { Authorization: authHeader } : {};
    fetch(`${apiBase()}/api/training/images/${image.id}/file`, { credentials: 'include', headers })
      .then((r) => (r.ok ? r.blob() : Promise.reject(new Error('load'))))
      .then((blob) => { if (!revoked) { objectUrl = URL.createObjectURL(blob); setBlobUrl(objectUrl); } })
      .catch(() => {});
    return () => { revoked = true; if (objectUrl) URL.revokeObjectURL(objectUrl); };
  }, [image.id, authHeader]);

  useEffect(() => {
    const onKey = (e) => { if (e.key === 'Escape') requestClose(); };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, [requestClose]);

  function toNorm(e) {
    const rect = canvasRef.current?.getBoundingClientRect();
    if (!rect || rect.width === 0) return [0, 0];
    return [clamp01((e.clientX - rect.left) / rect.width), clamp01((e.clientY - rect.top) / rect.height)];
  }

  function hitBox(px, py) {
    for (let i = boxes.length - 1; i >= 0; i -= 1) {
      const b = boxes[i];
      if (px >= b.x && px <= b.x + b.w && py >= b.y && py <= b.y + b.h) return i;
    }
    return -1;
  }

  function onMouseDown(e) {
    if (busy) return;
    const [px, py] = toNorm(e);
    const idx = hitBox(px, py);
    if (idx >= 0) {
      setSelected(idx);
      setDrag({ mode: 'move', idx, offX: px - boxes[idx].x, offY: py - boxes[idx].y });
    } else {
      setSelected(-1);
      setDrag({ mode: 'draw', startX: px, startY: py, curX: px, curY: py });
    }
  }

  function onMouseMove(e) {
    if (!drag) return;
    const [px, py] = toNorm(e);
    if (drag.mode === 'draw') {
      setDrag({ ...drag, curX: px, curY: py });
    } else if (drag.mode === 'move') {
      setBoxes((prev) => prev.map((b, i) => {
        if (i !== drag.idx) return b;
        return { ...b, x: clamp01(Math.min(px - drag.offX, 1 - b.w)), y: clamp01(Math.min(py - drag.offY, 1 - b.h)) };
      }));
    }
  }

  function onMouseUp() {
    if (!drag) return;
    if (drag.mode === 'draw') {
      const x = Math.min(drag.startX, drag.curX);
      const y = Math.min(drag.startY, drag.curY);
      const w = Math.abs(drag.curX - drag.startX);
      const h = Math.abs(drag.curY - drag.startY);
      if (w > 0.01 && h > 0.01) {
        // Auto-name a freshly drawn box (box1, box2, …); the user renames it in
        // the list. Numbering continues past the current max so it stays unique.
        const next = [...boxes, { className: nextBoxName(boxes), x, y, w, h, source: 'manual' }];
        setBoxes(next);
        setSelected(next.length - 1);
        setChecked(new Set());
      }
    }
    setDrag(null);
  }

  // Live preview of the box being drawn.
  const drawPreview = drag?.mode === 'draw'
    ? { x: Math.min(drag.startX, drag.curX), y: Math.min(drag.startY, drag.curY), w: Math.abs(drag.curX - drag.startX), h: Math.abs(drag.curY - drag.startY) }
    : null;

  function setBoxClassAt(index, value) {
    setBoxes((prev) => prev.map((b, i) => (i === index ? { ...b, className: value } : b)));
  }

  function toggleChecked(index) {
    setChecked((prev) => {
      const next = new Set(prev);
      if (next.has(index)) next.delete(index); else next.add(index);
      return next;
    });
  }

  function toggleAllChecked() {
    setChecked((prev) => (prev.size === boxes.length ? new Set() : new Set(boxes.map((_, i) => i))));
  }

  function deleteChecked() {
    if (checked.size === 0) return;
    setBoxes((prev) => prev.filter((_, i) => !checked.has(i)));
    setChecked(new Set());
    setSelected(-1);
  }

  async function runAutoLabel() {
    const anns = await onAutoLabel(image.id);
    if (Array.isArray(anns)) {
      setBoxes(anns.map((a) => ({ className: String(a.className || '').toLowerCase(), x: a.x, y: a.y, w: a.w, h: a.h, source: a.source || 'auto' })));
      setSelected(-1);
      setChecked(new Set());
      const found = anns.map((a) => String(a.className || '').toLowerCase()).filter(Boolean);
      setClassOptions((prev) => Array.from(new Set([...prev, ...found])));
    }
  }

  const unlabeled = boxes.filter((b) => !b.className).length;
  const allChecked = boxes.length > 0 && checked.size === boxes.length;

  return (
    <div className="video-overlay" onClick={requestClose}>
      <div className="video-dialog annotator-dialog" onClick={(e) => e.stopPropagation()}>
        <div className="video-dialog-header">
          <span className="video-dialog-title">
            {t('tr.labelImage')}
            {dirty ? <span className="annotator-dirty">{t('tr.unsaved')}</span> : null}
          </span>
          <button type="button" className="video-dialog-close" onClick={requestClose} aria-label={t('common.close')}>✕</button>
        </div>

        <datalist id="annotator-label-suggestions">
          {classOptions.map((c) => <option key={c} value={c} />)}
        </datalist>

        <span className="field-hint annotator-intro">
          {t('tr.annotatorIntro')}
        </span>

        <div className="annotator-body">
          {/* Left: the box list with multi-select + bulk delete. */}
          <aside className="annotator-sidebar">
            <div className="annotator-sidebar-head">
              <strong>{t('tr.boxesCount', { n: boxes.length })}</strong>
              <div className="annotator-sidebar-tools">
                <label className="check-row compact">
                  <input type="checkbox" checked={allChecked} onChange={toggleAllChecked} disabled={boxes.length === 0} />
                  {t('common.all')}
                </label>
                <button type="button" className="quiet danger" onClick={deleteChecked} disabled={busy || checked.size === 0}>
                  <span className="btn-icon"><Ico n="trash" /> {t('tr.deleteN', { n: checked.size })}</span>
                </button>
              </div>
            </div>
            <div className="annotator-boxlist">
              {boxes.length === 0 ? (
                <span className="field-hint">{t('tr.noBoxesYet')}</span>
              ) : (
                boxes.map((b, i) => (
                  <div
                    key={i}
                    className={`annotator-box-row${i === selected ? ' is-selected' : ''}`}
                    onMouseEnter={() => setSelected(i)}
                  >
                    <input type="checkbox" checked={checked.has(i)} onChange={() => toggleChecked(i)} title={t('tr.selectForBulk')} />
                    <i className={`anno-swatch ${b.source}`} />
                    <input
                      className="annotator-box-name"
                      list="annotator-label-suggestions"
                      value={b.className}
                      onChange={(e) => setBoxClassAt(i, e.target.value)}
                      placeholder={t('tr.labelPlaceholder')}
                    />
                  </div>
                ))
              )}
            </div>
          </aside>

          {/* Right: the image + drawing controls. */}
          <div className="annotator-main">
            <div className="annotator-legend">
              <div className="annotator-legend-keys">
                <span><i className="anno-swatch manual" /> {t('tr.youDrew')}</span>
                <span><i className="anno-swatch auto" /> {t('tr.autoLabeled')}</span>
                <span><i className="anno-swatch selected" /> {t('tr.selected')}</span>
              </div>
              <button type="button" className="quiet" onClick={runAutoLabel} disabled={busy} title={t('tr.autoLabelTitle')}>
                <span className="btn-icon"><Ico n="wand" /> {t('tr.autoLabel')}</span>
              </button>
            </div>

            <div
              className="annotator-canvas"
              ref={canvasRef}
              onMouseDown={onMouseDown}
              onMouseMove={onMouseMove}
              onMouseUp={onMouseUp}
              onMouseLeave={onMouseUp}
            >
              {blobUrl ? <img src={blobUrl} alt={t('tr.altAnnotate')} draggable="false" /> : <div className="training-thumb-empty" />}
              <svg className="annotator-overlay" viewBox="0 0 100 100" preserveAspectRatio="none">
                {boxes.map((b, i) => (
                  <rect
                    key={i}
                    x={b.x * 100} y={b.y * 100} width={b.w * 100} height={b.h * 100}
                    className={`anno-box ${b.source} ${i === selected ? 'selected' : ''}`}
                    vectorEffect="non-scaling-stroke"
                  />
                ))}
                {drawPreview ? (
                  <rect x={drawPreview.x * 100} y={drawPreview.y * 100} width={drawPreview.w * 100} height={drawPreview.h * 100} className="anno-box drawing" vectorEffect="non-scaling-stroke" />
                ) : null}
              </svg>
              {/* Label captions on each box — above the box, or just inside the top
                  edge when the box is near the top of the image (no room above). */}
              <div className="anno-label-layer">
                {boxes.map((b, i) => {
                  const above = b.y > 0.07;
                  return (
                    <span
                      key={i}
                      className={`anno-label ${b.source}${i === selected ? ' selected' : ''}`}
                      style={{ left: `${b.x * 100}%`, top: `${b.y * 100}%`, transform: above ? 'translateY(-100%)' : 'none' }}
                    >
                      {b.className || '—'}
                    </span>
                  );
                })}
              </div>
            </div>
          </div>
        </div>

        {unlabeled > 0 ? (
          <div className="field-hint annotator-warn">{t('tr.unlabeledWarn', { n: unlabeled })}</div>
        ) : null}

        <div className="action-row">
          <button type="button" onClick={() => onSave(image.id, boxes)} disabled={busy || !dirty}>
            <span className="btn-icon"><Ico n="save" /> {t('tr.saveLabels')}</span>
          </button>
          <button type="button" className="quiet" onClick={requestClose}>{t('common.cancel')}</button>
        </div>
      </div>
    </div>
  );
}

// Detection-type options for the importer filter. Values match the stored
// detectionType (rule mode / legacy object type) and are filtered server-side.
const alertTypeOptions = [
  ['presence', 'det.presence'],
  ['crowd', 'det.crowd'],
  ['intrusion', 'det.intrusion'],
  ['line_crossing', 'det.lineCrossing'],
  ['multi_line_crossing', 'det.multiLineCrossing'],
  ['person', 'det.person'],
  ['vehicle', 'det.vehicle'],
  ['animal', 'det.animal'],
  ['fire', 'det.fire'],
  ['smoke', 'det.smoke'],
];

export function TrainingTab({ authHeader, cameras, onMessage, onModelActivated, classes, labelCatalog, activeModelClasses, onSaveClass, onDeleteClass }) {
  const t = useT();
  const [datasets, setDatasets] = useState([]);
  const [selectedId, setSelectedId] = useState(null);
  const [images, setImages] = useState([]);
  const [importAlerts, setImportAlerts] = useState([]);
  const [importTotal, setImportTotal] = useState(0);
  const [draft, setDraft] = useState(emptyDatasetDraft);
  const [busy, setBusy] = useState(false);
  const [previewAlert, setPreviewAlert] = useState(null);
  const [editingImage, setEditingImage] = useState(null);
  const [filterCamera, setFilterCamera] = useState('');
  const [filterType, setFilterType] = useState('');
  const [alertPage, setAlertPage] = useState(0);
  const [models, setModels] = useState([]);
  const [modelDraft, setModelDraft] = useState({ name: '', classes: '' });
  const [capability, setCapability] = useState(null);
  const [installing, setInstalling] = useState(false);
  const [installLog, setInstallLog] = useState('');
  const [trainDraft, setTrainDraft] = useState({ epochs: 50, imgsz: 640 });
  const [step, setStep] = useState('datasets');
  const fileInputRef = useRef(null);
  const modelFileRef = useRef(null);
  const alertsRef = useRef(null);
  const alertPageSize = 12;

  const notify = useCallback((msg, kind) => { if (onMessage) onMessage(msg, kind); }, [onMessage]);

  const cameraName = useCallback(
    (id) => {
      const cam = (cameras || []).find((c) => Number(c.id) === Number(id));
      return cam ? cameraTitle(cam) : t('tr.cameraLabel', { id });
    },
    [cameras, t],
  );

  const selected = datasets.find((d) => Number(d.id) === Number(selectedId)) || null;

  const loadDatasets = useCallback(async () => {
    try {
      const result = await api('/api/training/datasets', authHeader);
      const items = Array.isArray(result?.items) ? result.items : [];
      setDatasets(items);
      setSelectedId((current) => current || items[0]?.id || null);
    } catch (err) {
      notify(err.message, 'error');
    }
  }, [authHeader, notify]);

  const loadImages = useCallback(async (datasetId) => {
    if (!datasetId) { setImages([]); return; }
    try {
      const result = await api(`/api/training/datasets/${datasetId}/images`, authHeader);
      setImages(Array.isArray(result?.items) ? result.items : []);
    } catch (err) {
      notify(err.message, 'error');
    }
  }, [authHeader, notify]);

  // Importable alerts are paged and filtered ENTIRELY server-side: status=active
  // excludes diagnostics, cameraId/detectionType narrow the set, and limit/offset
  // page it — the client never pulls the whole table down to filter locally.
  const loadImportAlerts = useCallback(async (page, camera, type) => {
    const params = new URLSearchParams({
      status: 'active',
      limit: String(alertPageSize),
      offset: String(page * alertPageSize),
    });
    if (camera) params.set('cameraId', String(camera));
    if (type) params.set('detectionType', type);
    try {
      const result = await api(`/api/vision/alerts?${params.toString()}`, authHeader);
      setImportAlerts(Array.isArray(result?.items) ? result.items : []);
      setImportTotal(typeof result?.total === 'number' ? result.total : 0);
    } catch (err) {
      notify(err.message, 'error');
    }
  }, [authHeader, notify]);

  const loadModels = useCallback(async () => {
    try {
      const result = await api('/api/training/models', authHeader);
      setModels(Array.isArray(result?.items) ? result.items : []);
    } catch (err) {
      notify(err.message, 'error');
    }
  }, [authHeader, notify]);

  const loadCapability = useCallback(async () => {
    try {
      setCapability(await api('/api/training/capability', authHeader));
    } catch (_) { /* capability is best-effort */ }
  }, [authHeader]);

  // Kick off the in-app dependency install. The backend checks the GPU is
  // CUDA-capable first, pauses the detector, and runs the platform setup script;
  // we then poll status and refresh capability when it finishes.
  const installDeps = useCallback(() => {
    run(async () => {
      const st = await api('/api/training/setup-deps', authHeader, { method: 'POST' });
      setInstallLog(st?.log || '');
      setInstalling(true);
    }, t('tr.installingGpu'));
  }, [authHeader]);

  useEffect(() => {
    if (!installing) return undefined;
    const id = window.setInterval(async () => {
      try {
        const st = await api('/api/training/setup-deps', authHeader);
        setInstallLog(st?.log || '');
        if (!st?.running) {
          window.clearInterval(id);
          setInstalling(false);
          await loadCapability();
          notify(st?.status === 'done'
            ? t('tr.gpuInstalled')
            : t('tr.gpuInstallErr'), st?.status === 'done' ? 'success' : 'error');
        }
      } catch (_) { /* keep polling */ }
    }, 3000);
    return () => window.clearInterval(id);
  }, [installing, authHeader, loadCapability, notify]);

  useEffect(() => { loadDatasets(); }, [loadDatasets]);
  useEffect(() => { loadImages(selectedId); }, [selectedId, loadImages]);
  useEffect(() => { loadImportAlerts(alertPage, filterCamera, filterType); }, [alertPage, filterCamera, filterType, loadImportAlerts]);
  useEffect(() => { loadModels(); }, [loadModels]);
  useEffect(() => { loadCapability(); }, [loadCapability]);

  // Poll while any model is training so the progress bar advances live.
  const trainingActive = models.some((m) => m.status === 'pending' || m.status === 'running');
  useEffect(() => {
    if (!trainingActive) return undefined;
    const id = window.setInterval(() => { loadModels(); }, 4000);
    return () => window.clearInterval(id);
  }, [trainingActive, loadModels]);

  function changeFilterCamera(value) { setFilterCamera(value); setAlertPage(0); }
  function changeFilterType(value) { setFilterType(value); setAlertPage(0); }

  async function run(action, okMessage) {
    setBusy(true);
    notify('');
    try {
      await action();
      if (okMessage) notify(okMessage);
    } catch (err) {
      notify(err.message, 'error');
    } finally {
      setBusy(false);
    }
  }

  function createDataset(event) {
    event.preventDefault();
    run(async () => {
      const body = {
        name: draft.name,
        description: draft.description,
        classes: draft.classes.split(',').map((s) => s.trim()).filter(Boolean),
      };
      const saved = await api('/api/training/datasets', authHeader, { method: 'POST', body: JSON.stringify(body) });
      setDraft(emptyDatasetDraft);
      await loadDatasets();
      if (saved?.id) setSelectedId(saved.id);
    }, t('tr.datasetSaved'));
  }

  function deleteDataset(id) {
    run(async () => {
      await api(`/api/training/datasets/${id}`, authHeader, { method: 'DELETE' });
      if (Number(id) === Number(selectedId)) setSelectedId(null);
      await loadDatasets();
    }, t('tr.datasetDeleted'));
  }

  function uploadFiles(fileList) {
    if (!selectedId || !fileList || fileList.length === 0) return;
    run(async () => {
      for (const file of Array.from(fileList)) {
        const form = new FormData();
        form.append('file', file);
        await api(`/api/training/datasets/${selectedId}/images/upload`, authHeader, { method: 'POST', body: form });
      }
      await loadImages(selectedId);
      await loadDatasets();
    }, t('tr.imagesUploaded'));
  }

  function addFromAlert(alertId) {
    if (!selectedId) return;
    run(async () => {
      await api(`/api/training/datasets/${selectedId}/images/from-alert`, authHeader, {
        method: 'POST',
        body: JSON.stringify({ alertId }),
      });
      await loadImages(selectedId);
      await loadDatasets();
      await loadImportAlerts(alertPage, filterCamera, filterType);
    }, t('tr.alertAdded'));
  }

  function deleteImage(id) {
    run(async () => {
      await api(`/api/training/images/${id}`, authHeader, { method: 'DELETE' });
      await loadImages(selectedId);
      await loadDatasets();
    }, t('tr.imageRemoved'));
  }

  function saveAnnotations(imageId, annotations) {
    run(async () => {
      await api(`/api/training/images/${imageId}/annotations`, authHeader, {
        method: 'PUT',
        body: JSON.stringify({ annotations }),
      });
      await loadImages(selectedId);
      await loadDatasets();
      setEditingImage(null);
    }, t('tr.labelsSaved'));
  }

  // autoLabelImage runs the detector server-side and returns the resulting
  // annotations to the open editor (which replaces its boxes with them).
  async function autoLabelImage(imageId) {
    try {
      const updated = await api(`/api/training/images/${imageId}/autolabel`, authHeader, { method: 'POST' });
      await loadImages(selectedId);
      await loadDatasets();
      notify(t('tr.autoLabelDone'));
      return parseAnnotations(updated?.annotations);
    } catch (err) {
      notify(err.message, 'error');
      return null;
    }
  }

  function exportDataset() {
    if (!selectedId) return;
    run(async () => {
      const headers = authHeader ? { Authorization: authHeader } : {};
      const resp = await fetch(`${apiBase()}/api/training/datasets/${selectedId}/export`, { credentials: 'include', headers });
      if (!resp.ok) {
        const text = await resp.text();
        let msg = t('tr.exportFailed', { status: resp.status });
        try { msg = JSON.parse(text)?.message || msg; } catch (_) { /* keep default */ }
        throw new Error(msg);
      }
      const blob = await resp.blob();
      const url = URL.createObjectURL(blob);
      const a = document.createElement('a');
      a.href = url;
      a.download = `dataset-${selectedId}.zip`;
      document.body.appendChild(a);
      a.click();
      a.remove();
      URL.revokeObjectURL(url);
    }, t('tr.datasetExported'));
  }

  function importModel(event) {
    event.preventDefault();
    const file = modelFileRef.current?.files?.[0];
    if (!file) { notify(t('tr.choosePt'), 'error'); return; }
    run(async () => {
      const form = new FormData();
      form.append('file', file);
      form.append('name', modelDraft.name);
      form.append('classes', modelDraft.classes);
      const model = await api('/api/training/models/import', authHeader, { method: 'POST', body: form });
      setModelDraft({ name: '', classes: '' });
      if (modelFileRef.current) modelFileRef.current.value = '';
      await loadModels();
      const cls = modelClasses(model);
      notify(cls.length
        ? t('tr.modelImportedCls', { cls: cls.join(', ') })
        : t('tr.modelImported'));
    });
  }

  function startTraining() {
    if (!selectedId) { notify(t('tr.pickDatasetTrain'), 'error'); return; }
    run(async () => {
      await api('/api/training/models', authHeader, {
        method: 'POST',
        body: JSON.stringify({ datasetId: Number(selectedId), epochs: Number(trainDraft.epochs) || 50, imgsz: Number(trainDraft.imgsz) || 640 }),
      });
      await loadModels();
    }, t('tr.trainingStarted'));
  }

  function activateModel(id) {
    run(async () => {
      const model = await api(`/api/training/models/${id}/activate`, authHeader, { method: 'POST' });
      await loadModels();
      // Refresh the AI page's class registry so the model's classes appear in the
      // rule "Detect" picker (and the Object Classes list) without a manual reload.
      if (onModelActivated) onModelActivated();
      const cls = modelClasses(model);
      notify(cls.length
        ? t('tr.modelActivatedCls', { cls: cls.join(', ') })
        : t('tr.modelActivated'));
    });
  }

  function deactivateModel() {
    run(async () => {
      await api('/api/training/models/deactivate', authHeader, { method: 'POST' });
      await loadModels();
      if (onModelActivated) onModelActivated();
    }, t('tr.modelDeactivated'));
  }

  function deleteModel(id) {
    run(async () => {
      await api(`/api/training/models/${id}`, authHeader, { method: 'DELETE' });
      await loadModels();
    }, t('tr.modelDeleted'));
  }

  // The server returns this page already filtered (status=active + camera/type);
  // the snapshot guard is a belt-and-braces check since real detections carry one.
  const importableAlerts = importAlerts.filter((a) => a?.id && a.snapshotPath);
  const alertPageCount = Math.max(1, Math.ceil(importTotal / alertPageSize));
  const rangeStart = importTotal === 0 ? 0 : alertPage * alertPageSize + 1;
  const rangeEnd = Math.min((alertPage + 1) * alertPageSize, importTotal);

  const steps = [
    ['datasets', t('tr.stepDatasets'), 'folder'],
    ['images', t('tr.stepImages'), 'grid2'],
    ['models', t('tr.stepModels'), 'cpu'],
    ['classes', t('tr.stepClasses'), 'box'],
  ];
  const stepHeaders = {
    datasets: { title: t('tr.stepDatasets'), desc: t('tr.datasetsDesc') },
    images: { title: t('tr.stepImages'), desc: t('tr.imagesDesc') },
    models: { title: t('tr.stepModels'), desc: t('tr.modelsDesc') },
    classes: { title: t('tr.stepClasses'), desc: t('tr.classesDesc') },
  };
  const header = stepHeaders[step] || stepHeaders.datasets;

  return (
    <section className="workspace settings-workspace training-workspace">
      <aside className="settings-side-nav" aria-label={t('tr.trainingAria')}>
        {steps.map(([id, label, icon]) => (
          <button
            key={id}
            type="button"
            className={step === id ? 'active' : 'quiet'}
            onClick={() => setStep(id)}
          >
            <span className="btn-icon"><Ico n={icon} /> {label}</span>
          </button>
        ))}
      </aside>

      <div className="settings-content">
      <header className="training-header">
        <span className="training-eyebrow">{t('tr.eyebrow')}</span>
        <h1>{header.title}</h1>
        <p>{header.desc}</p>
      </header>

      {step !== 'datasets' ? (
        <div className="training-context-bar">
          <label>
            {t('tr.activeDataset')}
            <select value={selectedId || ''} onChange={(e) => setSelectedId(Number(e.target.value) || null)}>
              {datasets.length === 0 ? <option value="">{t('tr.noneCreate')}</option> : null}
              {datasets.map((ds) => <option key={ds.id} value={ds.id}>{ds.name}</option>)}
            </select>
          </label>
          {selected ? (
            <span className="field-hint">{t('tr.imagesLabeled', { n: selected.imageCount || 0, m: selected.labeledCount || 0 })}</span>
          ) : null}
        </div>
      ) : null}

      {step === 'datasets' ? (
        <section className="settings-panel">
          <header>
            <h2>{t('tr.stepDatasets')}</h2>
            <span className="status-pill">{datasets.length}</span>
          </header>
          <ul className="class-registry-list">
            {datasets.map((ds) => (
              <li
                key={ds.id}
                className={`class-registry-row training-dataset-row${Number(ds.id) === Number(selectedId) ? ' is-active' : ''}`}
              >
                <button type="button" className="training-dataset-pick" onClick={() => { setSelectedId(ds.id); setStep('images'); }}>
                  <strong>{ds.name}</strong>
                  <span className="field-hint"> {t('tr.imagesLabeled', { n: ds.imageCount || 0, m: ds.labeledCount || 0 })}</span>
                </button>
                <button type="button" className="quiet danger" onClick={() => deleteDataset(ds.id)} disabled={busy}>{t('common.delete')}</button>
              </li>
            ))}
            {datasets.length === 0 ? <li className="field-hint">{t('tr.noDatasets')}</li> : null}
          </ul>
          <form className="vision-rule-form" onSubmit={createDataset}>
            <header><h3>{t('tr.newDataset')}</h3></header>
            <label>
              {t('common.name')}
              <input value={draft.name} onChange={(e) => setDraft({ ...draft, name: e.target.value })} placeholder={t('tr.phCouriers')} disabled={busy} />
            </label>
            <label>
              {t('tr.classesOptional')}
              <input value={draft.classes} onChange={(e) => setDraft({ ...draft, classes: e.target.value })} placeholder={t('tr.phClassesBlank')} disabled={busy} />
              <span className="field-hint">{t('tr.classesHint')}</span>
            </label>
            <label>
              {t('common.description')}
              <input value={draft.description} onChange={(e) => setDraft({ ...draft, description: e.target.value })} placeholder={t('tr.phDescription')} disabled={busy} />
            </label>
            <div className="action-row">
              <button type="submit" disabled={busy || !draft.name.trim()}>
                <span className="btn-icon"><Ico n="plus" /> {t('tr.createDataset')}</span>
              </button>
            </div>
          </form>
        </section>
      ) : null}

      {step === 'images' ? (
        <section className="settings-panel">
          <header>
            <h2>{selected ? t('tr.imagesNamed', { name: selected.name }) : t('tr.images')}</h2>
            <span className="status-pill">{images.length}</span>
          </header>
          {!selected ? (
            <span className="field-hint">{t('tr.selectDatasetHint')}</span>
          ) : (
            <>
              <div className="action-row">
                <input
                  ref={fileInputRef}
                  type="file"
                  accept="image/jpeg"
                  multiple
                  style={{ display: 'none' }}
                  onChange={(e) => { uploadFiles(e.target.files); e.target.value = ''; }}
                />
                <button type="button" onClick={() => fileInputRef.current?.click()} disabled={busy}>
                  <span className="btn-icon"><Ico n="plus" /> {t('tr.uploadJpegs')}</span>
                </button>
                <span className="field-hint">{t('tr.or')}</span>
                <button type="button" className="quiet" onClick={() => alertsRef.current?.scrollIntoView({ behavior: 'smooth', block: 'start' })}>
                  <span className="btn-icon"><Ico n="arr-down" /> {t('tr.addFromAlerts')}</span>
                </button>
              </div>
              <span className="field-hint">{t('tr.clickImageHint')}</span>

              <div className="training-image-grid">
                {images.map((img) => (
                  <div key={img.id} className="training-image-cell">
                    <button type="button" className="training-image-open" onClick={() => setEditingImage(img)} title={t('tr.clickToLabel')}>
                      <AuthBlobImage path={`/api/training/images/${img.id}/file`} authHeader={authHeader} alt={t('tr.altTrainingSample')} />
                      <span className="training-image-badge">{annotationCount(img.annotations)} ▢</span>
                    </button>
                    <div className="training-image-meta">
                      <span className={`status-pill ${img.status === 'unlabeled' ? '' : 'ok'}`}>{img.status}</span>
                      <button type="button" className="quiet danger" onClick={() => deleteImage(img.id)} disabled={busy}>✕</button>
                    </div>
                  </div>
                ))}
                {images.length === 0 ? <span className="field-hint">{t('tr.noImages')}</span> : null}
              </div>

              <header ref={alertsRef} className="training-divider"><h3>{t('tr.addFromAlerts')}</h3></header>
              <span className="field-hint">{t('tr.alertsHint')}</span>
              <div className="training-filter-row">
                <label>
                  {t('tr.camera')}
                  <select value={filterCamera} onChange={(e) => changeFilterCamera(e.target.value)}>
                    <option value="">{t('tr.allCameras')}</option>
                    {(cameras || []).map((cam) => (
                      <option key={cam.id} value={cam.id}>{cameraName(cam.id)}</option>
                    ))}
                  </select>
                </label>
                <label>
                  {t('tr.detectionType')}
                  <select value={filterType} onChange={(e) => changeFilterType(e.target.value)}>
                    <option value="">{t('tr.allTypes')}</option>
                    {alertTypeOptions.map(([value, label]) => (
                      <option key={value} value={value}>{t(label)}</option>
                    ))}
                  </select>
                </label>
                {(filterCamera || filterType) ? (
                  <button type="button" className="quiet" onClick={() => { setFilterCamera(''); setFilterType(''); setAlertPage(0); }}>{t('common.clear')}</button>
                ) : null}
              </div>
              <div className="training-alert-list">
                {importableAlerts.map((a) => (
                  <div key={a.id} className="training-alert-row">
                    <button type="button" className="training-alert-thumb" onClick={() => setPreviewAlert(a)} title={t('tr.preview')}>
                      <AuthBlobImage path={`/api/vision/alerts/${a.id}/snapshot`} authHeader={authHeader} alt={t('tr.altAlertSnapshot')} />
                    </button>
                    <div className="training-alert-info">
                      <strong>{a.label || a.detectionType}</strong>
                      <span className="field-hint">{cameraName(a.cameraId)} · #{a.id}</span>
                    </div>
                    <div className="training-alert-actions">
                      <button type="button" className="quiet" onClick={() => setPreviewAlert(a)} disabled={busy}>
                        <span className="btn-icon"><Ico n="play" /> {t('tr.preview')}</span>
                      </button>
                      <button type="button" onClick={() => addFromAlert(a.id)} disabled={busy}>{t('common.add')}</button>
                    </div>
                  </div>
                ))}
                {importableAlerts.length === 0 ? (
                  <span className="field-hint">{(filterCamera || filterType) ? t('tr.noMatch') : t('tr.noRecent')}</span>
                ) : null}
              </div>
              {importTotal > alertPageSize ? (
                <div className="training-pager">
                  <button type="button" className="quiet" onClick={() => setAlertPage((p) => Math.max(0, p - 1))} disabled={alertPage === 0}>{t('tr.prev')}</button>
                  <span className="field-hint">{t('tr.range', { a: rangeStart, b: rangeEnd, total: importTotal })}</span>
                  <button type="button" className="quiet" onClick={() => setAlertPage((p) => Math.min(alertPageCount - 1, p + 1))} disabled={alertPage >= alertPageCount - 1}>{t('tr.next')}</button>
                </div>
              ) : null}
            </>
          )}
        </section>
      ) : null}

      {step === 'models' ? (
      <section className="settings-panel">
        <header>
          <h2>{t('tr.stepModels')}</h2>
          <span className="status-pill">{models.length}</span>
        </header>
        <span className="field-hint">
          {t('tr.modelsHint')}
        </span>
        {(() => {
          const active = models.find((m) => m.isActive);
          if (!active) return null;
          const cls = modelClasses(active);
          return (
            <div className="training-active-banner">
              <Ico n="check-ok" /> {t('tr.activeRunning', { name: active.name })}
              {cls.length ? t('tr.activeUseClasses', { cls: cls.join(', ') }) : null}
            </div>
          );
        })()}

        <section className="schedule-panel">
          <header>
            <h3>{t('tr.trainInApp')}</h3>
            {capability ? (
              <span className={`status-pill ${capability.cuda ? 'ok' : ''}`}>
                {capability.cuda ? t('tr.gpuReady') : capability.available ? t('tr.cpuOnly') : t('tr.unavailable')}
              </span>
            ) : null}
          </header>
          {capability && capability.detail ? <span className="field-hint">{capability.detail}</span> : null}
          {capability && capability.canInstall && !capability.cuda ? (
            <div className="install-deps">
              <div className="action-row">
                <button type="button" onClick={installDeps} disabled={busy || installing}>
                  <span className="btn-icon">
                    <Ico n={installing ? 'refresh' : 'download'} /> {installing ? t('tr.installingGpuBtn') : t('tr.installGpuBtn')}
                  </span>
                </button>
                <span className="field-hint">
                  {t('tr.detectedGpu', { gpu: capability.gpu })}
                </span>
              </div>
              {installing ? <span className="field-hint">{t('tr.downloadingInstalling')}</span> : null}
              {installLog ? <ConsoleLog title={t('tr.gpuInstallTitle')} log={installLog} running={installing} /> : null}
            </div>
          ) : null}
          {capability && !capability.hasGpu && !capability.cuda ? (
            <span className="field-hint">{t('tr.noCuda')}</span>
          ) : null}
          <div className="metadata-row">
            <label>
              {t('tr.epochs')}
              <input type="number" min="1" max="1000" value={trainDraft.epochs} onChange={(e) => setTrainDraft({ ...trainDraft, epochs: Number(e.target.value) })} disabled={busy} />
            </label>
            <label>
              {t('tr.imageSize')}
              <input type="number" min="64" step="32" value={trainDraft.imgsz} onChange={(e) => setTrainDraft({ ...trainDraft, imgsz: Number(e.target.value) })} disabled={busy} />
            </label>
          </div>
          <div className="action-row">
            <button type="button" onClick={startTraining} disabled={busy || !selected || trainingActive || (capability ? !capability.available : false)}>
              <span className="btn-icon"><Ico n="cpu" /> {t('tr.train')} {selected ? `“${selected.name}”` : t('tr.dataset')}</span>
            </button>
            <button type="button" className="quiet" onClick={exportDataset} disabled={busy || !selected} title={t('tr.exportInsteadTitle')}>
              <span className="btn-icon"><Ico n="download" /> {t('tr.exportInstead')}</span>
            </button>
          </div>
          {trainingActive ? <span className="field-hint">{t('tr.trainingInProgress')}</span> : null}
          {!selected ? <span className="field-hint">{t('tr.pickDatasetAbove')}</span> : null}
        </section>

        <ul className="class-registry-list">
          {models.map((m) => (
            <li key={m.id} className="class-registry-row">
              <div>
                <strong>{m.name}</strong>
                {m.isActive ? <span className="status-pill ok"> {t('tr.activePill')}</span> : null}
                <span className="field-hint">
                  {' '}{m.status}{m.status === 'running' ? ` · ${m.progress || 0}%` : ''} · {(() => { try { return JSON.parse(m.classes || '[]').join(', '); } catch (_) { return ''; } })() || t('tr.noClasses')}
                  {(() => { try { const mt = JSON.parse(m.metrics || '{}'); return mt.map50 ? ` · mAP50 ${(mt.map50 * 100).toFixed(0)}%` : (mt.error ? ` · ${mt.error}` : ''); } catch (_) { return ''; } })()}
                </span>
                {m.status === 'running' ? (
                  <div className="training-progress"><div className="training-progress-bar" style={{ width: `${m.progress || 0}%` }} /></div>
                ) : null}
              </div>
              <div className="class-registry-actions">
                {!m.isActive && m.status !== 'running' && m.status !== 'pending' ? (
                  <button type="button" onClick={() => activateModel(m.id)} disabled={busy || !m.filePath}>{t('tr.activate')}</button>
                ) : null}
                {m.isActive ? (
                  <button type="button" className="quiet" onClick={deactivateModel} disabled={busy} title={t('tr.deactivateTitle')}>{t('tr.deactivate')}</button>
                ) : null}
                <button type="button" className="quiet danger" onClick={() => deleteModel(m.id)} disabled={busy || m.isActive || m.status === 'running'} title={m.isActive ? t('tr.deactivateFirst') : t('tr.deleteModel')}>{t('common.delete')}</button>
              </div>
            </li>
          ))}
          {models.length === 0 ? <li className="field-hint">{t('tr.noModels')}</li> : null}
        </ul>
        <form className="vision-rule-form" onSubmit={importModel}>
          <header><h3>{t('tr.importTrainedModel')}</h3></header>
          <div className="metadata-row">
            <label>
              {t('common.name')}
              <input value={modelDraft.name} onChange={(e) => setModelDraft({ ...modelDraft, name: e.target.value })} placeholder={t('tr.phCouriersV1')} disabled={busy} />
            </label>
            <label>
              {t('tr.classesOptional')}
              <input value={modelDraft.classes} onChange={(e) => setModelDraft({ ...modelDraft, classes: e.target.value })} placeholder={t('tr.phAutoDetected')} disabled={busy} />
            </label>
          </div>
          <label>
            {t('tr.weightsFile')}
            <input ref={modelFileRef} type="file" accept=".pt" disabled={busy} />
            <span className="field-hint">{t('tr.weightsHint')}</span>
          </label>
          <div className="action-row">
            <button type="submit" disabled={busy || !modelDraft.name.trim()}>
              <span className="btn-icon"><Ico n="save" /> {t('tr.importModel')}</span>
            </button>
          </div>
        </form>
      </section>
      ) : null}

      {step === 'classes' ? (
        <ObjectClassesPanel
          classes={classes}
          labelCatalog={labelCatalog}
          activeModelClasses={activeModelClasses}
          busy={busy}
          onSaveClass={onSaveClass}
          onDeleteClass={onDeleteClass}
        />
      ) : null}
      </div>

      {previewAlert ? (
        <AlertPreviewModal
          alert={previewAlert}
          authHeader={authHeader}
          busy={busy}
          onAdd={() => { addFromAlert(previewAlert.id); setPreviewAlert(null); }}
          onClose={() => setPreviewAlert(null)}
        />
      ) : null}

      {editingImage ? (
        <ImageAnnotator
          image={editingImage}
          dataset={selected}
          authHeader={authHeader}
          busy={busy}
          onSave={saveAnnotations}
          onAutoLabel={autoLabelImage}
          onClose={() => setEditingImage(null)}
        />
      ) : null}
    </section>
  );
}
