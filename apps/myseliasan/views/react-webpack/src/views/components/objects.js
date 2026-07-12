import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { Ico, Tabs, DataTable, useT } from '@shared';
import { api, apiBase, formatTimestamp } from '../lib/helpers';
import { NodeEmbed, NodeFeatureBanner } from './node_embed';
import { ObjectClassesPanel } from './nodecam/vision';
import { setNodeProxyBase } from './nodecam/lib/helpers';

// ObjectsPage is the control plane's fleet-scale "Objects" surface, mirroring mymatasan's
// combined Objects page but across the whole fleet:
//   - Search  → find detected objects across ALL adopted nodes (or one), by object,
//               camera, date range, and confidence — with click-to-footage over the tunnel.
//   - Classes → manage a chosen node's detection-class registry over the proxy.
// All data flows browser ⇄ myseliasan ⇄ node (proxy / recording-stream); the browser never
// contacts a node directly.
export function ObjectsPage({ nodes = [], onToast }) {
  const t = useT();
  const [tab, setTab] = useState('search');
  return (
    <section className="workspace objects-page">
      <Tabs
        ariaLabel={t('obj.tabsAria')}
        active={tab}
        onChange={setTab}
        tabs={[
          { id: 'search', label: t('obj.tabSearch'), icon: 'eye' },
          { id: 'classes', label: t('obj.tabClasses'), icon: 'list' },
        ]}
      />
      {tab === 'search' ? <FleetObjectSearch nodes={nodes} /> : <NodeClassesEditor nodes={nodes} onToast={onToast} />}
    </section>
  );
}

// ---------------------------------------------------------------------------- search ---

const PER_NODE_LIMIT = 100;
const MAX_RESULTS = 300;
const DEFAULT_MIN_CONF = 60;

function objectCategory(label) {
  const l = String(label || '').toLowerCase();
  if (/(person|people|pedestrian|man|woman|child|human)/.test(l)) return 'person';
  if (/(car|truck|bus|van|motorcycle|motorbike|bicycle|bike|vehicle|train)/.test(l)) return 'vehicle';
  if (/(dog|cat|bird|animal|horse|cow|sheep|deer)/.test(l)) return 'animal';
  if (/(fire|smoke|flame)/.test(l)) return 'fire';
  return 'other';
}
const pad2 = (n) => String(n).padStart(2, '0');
const localDateStr = (d) => `${d.getFullYear()}-${pad2(d.getMonth() + 1)}-${pad2(d.getDate())}`;
const dayStartEpoch = (s) => Math.floor(new Date(`${s}T00:00:00`).getTime() / 1000);
const dayEndEpoch = (s) => Math.floor(new Date(`${s}T23:59:59`).getTime() / 1000);

function FleetObjectSearch({ nodes }) {
  const t = useT();
  const nodeList = useMemo(() => (Array.isArray(nodes) ? nodes : []), [nodes]);

  const [scope, setScope] = useState('all'); // 'all' | nodeId
  const [labels, setLabels] = useState([]);
  const [objs, setObjs] = useState([]);
  const [from, setFrom] = useState(() => { const d = new Date(); d.setDate(d.getDate() - 6); return localDateStr(d); });
  const [to, setTo] = useState(() => localDateStr(new Date()));
  const [minConf, setMinConf] = useState(DEFAULT_MIN_CONF);
  const [rows, setRows] = useState([]);
  const [camNames, setCamNames] = useState(new Map());
  const [loading, setLoading] = useState(false);
  const [searched, setSearched] = useState(false);
  const [truncated, setTruncated] = useState(false);
  const [playing, setPlaying] = useState(null);
  const [maximizing, setMaximizing] = useState(null);

  const targets = useMemo(
    () => (scope === 'all' ? nodeList : nodeList.filter((n) => String(n.nodeId) === String(scope))),
    [scope, nodeList],
  );

  // Aggregate the object-label list across the current scope so the picker offers what the
  // nodes actually recorded.
  useEffect(() => {
    let cancelled = false;
    (async () => {
      const per = await Promise.all(targets.slice(0, 25).map(async (n) => {
        const r = await api(`/api/nodes/${encodeURIComponent(n.nodeId)}/proxy/api/observations/labels`, { noRedirect: true }).catch(() => ({ ok: false }));
        return r.ok ? (Array.isArray(r.body) ? r.body : (r.body?.items || [])) : [];
      }));
      if (cancelled) return;
      const set = new Set();
      per.flat().forEach((l) => { if (l) set.add(String(l)); });
      setLabels([...set].sort());
    })();
    return () => { cancelled = true; };
  }, [targets]);

  const camName = useCallback((nodeId, cameraId) => camNames.get(`${nodeId}:${cameraId}`) || t('obj.cameraN', { id: cameraId }), [camNames, t]);

  async function loadCamNames(list) {
    const per = await Promise.all(list.map(async (n) => {
      const r = await api(`/api/nodes/${encodeURIComponent(n.nodeId)}/proxy/api/cameras?limit=500`, { noRedirect: true }).catch(() => ({ ok: false }));
      const cams = r.ok ? (Array.isArray(r.body) ? r.body : (r.body?.items || [])) : [];
      return { nodeId: n.nodeId, cams };
    }));
    const m = new Map();
    per.forEach(({ nodeId, cams }) => cams.forEach((c) => m.set(`${nodeId}:${c.id}`, c.name || t('obj.cameraN', { id: c.id }))));
    setCamNames(m);
  }

  async function runSearch(e) {
    if (e && e.preventDefault) e.preventDefault();
    setLoading(true);
    setSearched(true);
    const filters = [];
    if (from) filters.push({ fieldName: 'startedAt', compare: 5, value: dayStartEpoch(from) });
    if (to) filters.push({ fieldName: 'startedAt', compare: 6, value: dayEndEpoch(to) });
    if (objs.length) filters.push({ fieldName: 'label', compare: 7, value: objs });
    if (minConf > 0) filters.push({ fieldName: 'maxConfidence', compare: 5, value: minConf / 100 });
    const sorters = [{ fieldName: 'startedAt', sort: 2 }];
    const per = await Promise.all(targets.map(async (n) => {
      const p = new URLSearchParams({ limit: String(PER_NODE_LIMIT), offset: '0', filters: JSON.stringify(filters), sorters: JSON.stringify(sorters) });
      const r = await api(`/api/nodes/${encodeURIComponent(n.nodeId)}/proxy/api/observations?${p}`, { noRedirect: true }).catch(() => ({ ok: false }));
      const items = r.ok ? (Array.isArray(r.body) ? r.body : (r.body?.items || [])) : [];
      return items.map((it) => ({ ...it, id: `${n.nodeId}:${it.id}`, _nodeId: n.nodeId, _nodeName: n.name || n.nodeId }));
    }));
    const merged = per.flat().sort((a, b) => (b.startedAt || 0) - (a.startedAt || 0));
    setRows(merged.slice(0, MAX_RESULTS));
    setTruncated(merged.length > MAX_RESULTS);
    setLoading(false);
    loadCamNames(targets);
  }

  const columns = [
    { key: '_nodeName', label: t('obj.colNode') },
    { key: 'startedAt', label: t('obj.colTime'), render: (v) => formatTimestamp(v) },
    { key: 'cameraId', label: t('obj.colCamera'), render: (v, r) => camName(r._nodeId, v) },
    {
      key: 'label',
      label: t('obj.colObject'),
      render: (v, r) => (
        <span className={`object-tag object-tag--${objectCategory(v)}`}>{v}{r.maxCount > 1 ? ` ×${r.maxCount}` : ''}</span>
      ),
    },
    { key: 'maxConfidence', label: t('obj.colConfidence'), render: (v) => `${Math.round((v || 0) * 100)}%` },
    {
      key: 'footage',
      label: t('obj.colFootage'),
      filterable: false,
      render: (_v, r) => (r.footagePending || !r.segmentId
        ? <span className="footage-pending">{t('obj.footagePending')}</span>
        : <FootageThumb row={r} onPlay={() => setPlaying(r)} onMaximize={() => setMaximizing(r)} t={t} />),
    },
  ];

  return (
    <div className="workspace">
      <section className="settings-panel span-two">
        <header><h2><span className="btn-icon"><Ico n="eye" /> {t('obj.searchTitle')}</span></h2></header>
        <p className="settings-hint">{t('obj.searchHint')}</p>
        <form className="object-search-filters" onSubmit={runSearch}>
          <label>{t('obj.node')}
            <select value={scope} onChange={(e) => setScope(e.target.value)} disabled={loading}>
              <option value="all">{t('obj.allNodes', { n: nodeList.length })}</option>
              {nodeList.map((n) => <option key={n.nodeId} value={n.nodeId}>{n.name || n.nodeId}</option>)}
            </select>
          </label>
          <label>{t('obj.object')}
            <LabelPicker labels={labels} value={objs} onChange={setObjs} disabled={loading} t={t} />
          </label>
          <label>{t('obj.from')}<input type="date" value={from} max={to} onChange={(e) => setFrom(e.target.value)} disabled={loading} /></label>
          <label>{t('obj.to')}<input type="date" value={to} min={from} onChange={(e) => setTo(e.target.value)} disabled={loading} /></label>
          <label>{t('obj.minConf')}<input type="number" min="0" max="100" value={minConf} onChange={(e) => setMinConf(Math.max(0, Math.min(100, Number(e.target.value) || 0)))} disabled={loading} /></label>
          <button type="submit" disabled={loading}><span className="btn-icon"><Ico n="search" /> {loading ? t('obj.searching') : t('obj.search')}</span></button>
        </form>

        {truncated ? <p className="settings-hint">{t('obj.truncated', { n: MAX_RESULTS })}</p> : null}
        {searched && !loading && rows.length === 0 ? (
          <p className="settings-hint">{t('obj.noResults')}</p>
        ) : (
          <DataTable rows={rows} columns={columns} pageSize={20} pageSizeOptions={[20, 50, 100]} busy={loading} emptyText={t('obj.searchPrompt')} />
        )}
      </section>

      {playing ? <ObjVideoModal row={playing} onClose={() => setPlaying(null)} t={t} /> : null}
      {maximizing ? (
        <ObjSnapModal
          row={maximizing}
          onClose={() => setMaximizing(null)}
          onPlay={() => { const r = maximizing; setMaximizing(null); setPlaying(r); }}
          t={t}
        />
      ) : null}
    </div>
  );
}

// LabelPicker is a compact checkbox dropdown over the aggregated object labels (empty = any).
function LabelPicker({ labels, value, onChange, disabled, t }) {
  const [open, setOpen] = useState(false);
  const ref = useRef(null);
  useEffect(() => {
    if (!open) return undefined;
    const onDown = (e) => { if (ref.current && !ref.current.contains(e.target)) setOpen(false); };
    document.addEventListener('mousedown', onDown);
    return () => document.removeEventListener('mousedown', onDown);
  }, [open]);
  const toggle = (l) => onChange(value.includes(l) ? value.filter((x) => x !== l) : [...value, l]);
  const summary = value.length === 0 ? t('obj.anyObject') : t('obj.nSelected', { n: value.length });
  return (
    <div className="multi-select" ref={ref}>
      <button type="button" className="multi-select-toggle" onClick={() => setOpen((o) => !o)} disabled={disabled}>
        <span>{summary}</span><Ico n="chev-down" sz={12} />
      </button>
      {open ? (
        <div className="multi-select-menu">
          {labels.length === 0 ? <div className="multi-select-empty">{t('obj.noLabels')}</div> : labels.map((l) => (
            <label key={l} className="multi-select-item">
              <input type="checkbox" checked={value.includes(l)} onChange={() => toggle(l)} />
              <span className={`object-tag object-tag--${objectCategory(l)}`}>{l}</span>
            </label>
          ))}
        </div>
      ) : null}
    </div>
  );
}

// boxStrFromPeak converts an observation's stored peak box (a JSON `{x,y,w,h}` string,
// normalized 0..1) into the "x,y,w,h" the frame endpoint expects — mirroring mymatasan.
// Without this the box param can't be parsed and no bounding box is drawn.
function boxStrFromPeak(peakBox) {
  if (!peakBox) return '';
  try {
    const b = JSON.parse(peakBox);
    if (b && Number(b.w) > 0 && Number(b.h) > 0) return `${b.x},${b.y},${b.w},${b.h}`;
  } catch (_) { /* ignore */ }
  return '';
}

// FootageThumb is the footage cell, matching mymatasan's Object Search exactly: a boxed
// snapshot of the sighting moment (proxied frame, box + label drawn server-side) with
// translucent overlay buttons — Play (opens the covering segment) and Maximize (opens the
// snapshot large). Falls back to a plain play button if the frame fails to load.
function FootageThumb({ row, onPlay, onMaximize, t }) {
  const [failed, setFailed] = useState(false);
  const boxStr = boxStrFromPeak(row.peakBox);
  const pct = Math.round((row.maxConfidence || 0) * 100);
  const params = new URLSearchParams({ seek: String(row.seekSeconds || 0), w: '200' });
  if (boxStr) {
    params.set('box', boxStr);
    // Label the box with the object + its confidence, so the drawn frame reads e.g.
    // "person 87%".
    params.set('label', `${row.label || ''} ${pct}%`.trim());
  }
  const url = `${apiBase()}/api/nodes/${encodeURIComponent(row._nodeId)}/proxy/api/recording/segments/${row.segmentId}/frame?${params}`;
  if (failed) {
    return (
      <button type="button" className="obs-thumb obs-thumb--empty" onClick={onPlay} title={t('obj.play')} aria-label={t('obj.play')}>
        <Ico n="play" sz={16} />
      </button>
    );
  }
  return (
    <div className="obs-thumb">
      <img src={url} alt="" loading="lazy" onError={() => setFailed(true)} />
      <div className="obs-thumb-actions">
        <button type="button" className="obs-thumb-btn" onClick={onPlay} title={t('obj.play')} aria-label={t('obj.play')}>
          <Ico n="play" sz={16} />
        </button>
        <button type="button" className="obs-thumb-btn" onClick={onMaximize} title={t('obj.maximize')} aria-label={t('obj.maximize')}>
          <Ico n="camera" sz={16} />
        </button>
      </div>
    </div>
  );
}

// ObjVideoModal plays the covering segment over the range-streamed recording tunnel and
// reliably seeks to the exact moment the object was seen. Setting currentTime on
// loadedmetadata alone is unreliable on range-streamed footage (the browser can reset it
// once real data arrives), so — mirroring mymatasan — the seek is re-asserted on both
// loadedmetadata and canplay until it takes, clamped to the clip duration. The peak box is
// drawn over the video around the sighting moment.
function ObjVideoModal({ row, onClose, t }) {
  const ref = useRef(null);
  const seekedRef = useRef(false);
  const seekSeconds = Number(row.seekSeconds) || 0;
  const [boxVisible, setBoxVisible] = useState(true);
  const url = `${apiBase()}/api/nodes/${encodeURIComponent(row._nodeId)}/recording-stream/${row.segmentId}`;
  const pct = Math.round((row.maxConfidence || 0) * 100);
  const peakBox = useMemo(() => {
    if (!row.peakBox) return null;
    try { const b = JSON.parse(row.peakBox); if (b && Number(b.w) > 0 && Number(b.h) > 0) return b; } catch (_) { /* ignore */ }
    return null;
  }, [row.peakBox]);
  useEffect(() => {
    const k = (e) => { if (e.key === 'Escape') onClose(); };
    document.addEventListener('keydown', k);
    return () => document.removeEventListener('keydown', k);
  }, [onClose]);
  const applySeek = (v) => {
    if (!v || seekSeconds <= 0 || seekedRef.current) return;
    const dur = v.duration;
    const target = Number.isFinite(dur) && dur > 0 ? Math.min(seekSeconds, dur - 0.5) : seekSeconds;
    if (target <= 0) return;
    if (Math.abs(v.currentTime - target) < 0.75) { seekedRef.current = true; return; }
    try { v.currentTime = target; } catch (_) { /* ignore */ }
  };
  const fitAspect = (v) => { if (v && v.videoWidth > 0 && v.videoHeight > 0) v.style.aspectRatio = `${v.videoWidth} / ${v.videoHeight}`; };
  const onTime = (e) => {
    if (!peakBox) return;
    const near = Math.abs(e.currentTarget.currentTime - Math.max(seekSeconds, 0)) < 2.5;
    setBoxVisible((prev) => (prev === near ? prev : near));
  };
  return (
    <div className="video-overlay" onClick={onClose}>
      <div className="video-dialog" onClick={(e) => e.stopPropagation()}>
        <div className="video-dialog-header">
          <div className="video-dialog-title-group">
            <span className="video-dialog-title">{row._nodeName} · {t('obj.cameraN', { id: row.cameraId })}</span>
            <span className={`object-tag object-tag--${objectCategory(row.label)}`}>{row.label}{row.maxCount > 1 ? ` ×${row.maxCount}` : ''}</span>
          </div>
          <button type="button" className="video-dialog-close" onClick={onClose} aria-label={t('obj.close')}>✕</button>
        </div>
        <div className="video-dialog-body">
          <div className="video-stage">
            <video ref={ref} className="video-player" controls autoPlay src={url}
              onLoadedMetadata={(e) => { applySeek(e.currentTarget); fitAspect(e.currentTarget); }}
              onCanPlay={(e) => applySeek(e.currentTarget)}
              onTimeUpdate={onTime} />
            {peakBox && (
              <div
                className={`detect-box${boxVisible ? ' show' : ''}`}
                style={{
                  left: `${Math.max(0, Math.min(100, Number(peakBox.x) * 100))}%`,
                  top: `${Math.max(0, Math.min(100, Number(peakBox.y) * 100))}%`,
                  width: `${Math.max(0, Math.min(100, Number(peakBox.w) * 100))}%`,
                  height: `${Math.max(0, Math.min(100, Number(peakBox.h) * 100))}%`,
                }}
              >
                <span className="detect-box-label">{row.label} · {pct}%</span>
              </div>
            )}
          </div>
        </div>
        <div className="video-dialog-meta">
          {formatTimestamp(row.startedAt)} · {pct}%
          {seekSeconds > 0 ? ` · ${t('obj.jumpedTo', { time: `${Math.floor(seekSeconds / 60)}:${String(Math.floor(seekSeconds % 60)).padStart(2, '0')}` })}` : ''}
        </div>
      </div>
    </div>
  );
}

// ObjSnapModal opens the sighting snapshot large (higher-res render of the same boxed frame),
// with a Play action to switch to footage — mirroring mymatasan's maximize dialog.
function ObjSnapModal({ row, onClose, onPlay, t }) {
  const boxStr = boxStrFromPeak(row.peakBox);
  const pct = Math.round((row.maxConfidence || 0) * 100);
  const params = new URLSearchParams({ seek: String(row.seekSeconds || 0), w: '1280' });
  if (boxStr) {
    params.set('box', boxStr);
    params.set('label', `${row.label || ''} ${pct}%`.trim());
  }
  const url = `${apiBase()}/api/nodes/${encodeURIComponent(row._nodeId)}/proxy/api/recording/segments/${row.segmentId}/frame?${params}`;
  useEffect(() => {
    const k = (e) => { if (e.key === 'Escape') onClose(); };
    document.addEventListener('keydown', k);
    return () => document.removeEventListener('keydown', k);
  }, [onClose]);
  return (
    <div className="video-overlay" onClick={onClose}>
      <div className="snap-dialog" onClick={(e) => e.stopPropagation()}>
        <div className="video-dialog-header">
          <div className="video-dialog-title-group">
            <span className="video-dialog-title">{row._nodeName} · {t('obj.cameraN', { id: row.cameraId })}</span>
            <span className={`object-tag object-tag--${objectCategory(row.label)}`}>{row.label}{row.maxCount > 1 ? ` ×${row.maxCount}` : ''}</span>
          </div>
          <button type="button" className="video-dialog-close" onClick={onClose} aria-label={t('obj.close')}>✕</button>
        </div>
        <div className="snap-body"><img className="snap-image" src={url} alt="" /></div>
        <div className="video-dialog-meta">
          {formatTimestamp(row.startedAt)} · {pct}%
          <button type="button" className="quiet snap-play" onClick={onPlay}>
            <span className="btn-icon"><Ico n="play" /> {t('obj.play')}</span>
          </button>
        </div>
      </div>
    </div>
  );
}

// ---------------------------------------------------------------------------- classes ---

// NodeClassesEditor manages ONE node's detection-class registry over the proxy. It reuses
// the real mymatasan ObjectClassesPanel (copied under nodecam/) inside a scoped-CSS embed,
// wiring its save/delete through the CSRF-safe tunnel — the per-node model the user chose
// for the fleet Classes tab.
function NodeClassesEditor({ nodes, onToast }) {
  const t = useT();
  const nodeList = useMemo(() => (Array.isArray(nodes) ? nodes : []), [nodes]);
  const [nodeId, setNodeId] = useState(nodeList[0]?.nodeId || '');
  const [classes, setClasses] = useState([]);
  const [labelCatalog, setLabelCatalog] = useState([]);
  const [activeModelClasses, setActiveModelClasses] = useState([]);
  const [busy, setBusy] = useState(false);
  const [unavailable, setUnavailable] = useState(false);

  const px = useCallback(
    (path, opts = {}) => api(`/api/nodes/${encodeURIComponent(nodeId)}/proxy${path}`, { noRedirect: true, ...opts }).catch(() => ({ ok: false })),
    [nodeId],
  );
  const listOf = (r) => (r.ok ? (Array.isArray(r.body) ? r.body : (r.body?.items || [])) : []);

  const load = useCallback(async () => {
    if (!nodeId) return;
    // Point the copied panel's internal apiBase() at this node's proxy (label picker, etc.).
    setNodeProxyBase(`${apiBase()}/api/nodes/${encodeURIComponent(nodeId)}/proxy`);
    const [c, l] = await Promise.all([px('/api/vision/classes'), px('/api/vision/labels')]);
    setUnavailable(c.status === 404);
    setClasses(listOf(c));
    setLabelCatalog(listOf(l));
    px('/api/training/models').then((m) => {
      if (!m.ok) return;
      const items = Array.isArray(m.body) ? m.body : (m.body?.items || []);
      const active = items.find((x) => x.isActive);
      let cls = [];
      if (active) { try { cls = JSON.parse(active.classes || '[]'); } catch (_) { cls = []; } }
      setActiveModelClasses(cls.map((x) => String(x).toLowerCase()));
    });
  }, [nodeId, px]);
  useEffect(() => { load(); }, [load]);

  async function saveClass(payload) {
    setBusy(true);
    const r = await px('/api/vision/classes', { method: 'POST', body: JSON.stringify(payload) });
    setBusy(false);
    if (r.ok) { load(); if (onToast) onToast(t('obj.classSaved')); }
    else if (onToast) onToast(r.message || t('obj.classSaveFailed'));
  }
  async function deleteClass(id) {
    setBusy(true);
    const r = await px(`/api/vision/classes/${id}`, { method: 'DELETE' });
    setBusy(false);
    if (r.ok) { load(); if (onToast) onToast(t('obj.classDeleted')); }
  }

  return (
    <div className="workspace">
      <section className="settings-panel span-two">
        <header><h2><span className="btn-icon"><Ico n="list" /> {t('obj.classesTitle')}</span></h2></header>
        <p className="settings-hint">{t('obj.classesHint')}</p>
        <label className="obj-node-pick">{t('obj.node')}
          <select value={nodeId} onChange={(e) => setNodeId(e.target.value)}>
            {nodeList.length === 0 ? <option value="">{t('obj.noNodes')}</option> : null}
            {nodeList.map((n) => <option key={n.nodeId} value={n.nodeId}>{n.name || n.nodeId}</option>)}
          </select>
        </label>
      </section>

      {!nodeId ? (
        <p className="settings-hint">{t('obj.pickNode')}</p>
      ) : unavailable ? (
        <NodeFeatureBanner version={nodeList.find((n) => n.nodeId === nodeId)?.version} />
      ) : (
        <NodeEmbed className="objects-classes-embed">
          <ObjectClassesPanel
            classes={classes}
            labelCatalog={labelCatalog}
            activeModelClasses={activeModelClasses}
            busy={busy}
            onSaveClass={saveClass}
            onDeleteClass={deleteClass}
          />
        </NodeEmbed>
      )}
    </div>
  );
}
