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

const DEFAULT_MIN_CONF = 60;
const RESULT_LIMIT = 300;

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

// adaptHit maps one federated sighting onto the row shape the footage cell and the two
// modals already read (they are shared with mymatasan's own Object Search and are left
// untouched), while keeping the fleet-only fields the results table shows.
function adaptHit(hit) {
  return {
    ...hit,
    id: `${hit.nodeId}:${hit.kind}:${hit.id}`,
    _nodeId: hit.nodeId,
    _nodeName: hit.nodeName || hit.nodeId,
    _cameraLabel: hit.cameraName || '',
    maxConfidence: hit.confidence,
    maxCount: hit.count,
  };
}

// FleetObjectSearch searches every node the signed-in role can reach, THROUGH the control
// plane rather than from the browser.
//
// The fan-out used to live here: one proxied request per node, per search, plus one more
// per node for its camera names, with failures swallowed by a .catch that returned an empty
// list. That shape could not tell an operator that three of nine recorders never answered —
// the page simply showed fewer rows. Federation now happens server-side and returns a
// COVERAGE block, which this renders above the results: an incomplete search says so.
function FleetObjectSearch({ nodes }) {
  const t = useT();
  const nodeList = useMemo(() => (Array.isArray(nodes) ? nodes : []), [nodes]);

  const [scope, setScope] = useState('all'); // 'all' | nodeId
  const [siteId, setSiteId] = useState(0);
  const [sites, setSites] = useState([]);
  const [labels, setLabels] = useState([]);
  const [objs, setObjs] = useState([]);
  const [text, setText] = useState('');
  const [from, setFrom] = useState(() => { const d = new Date(); d.setDate(d.getDate() - 6); return localDateStr(d); });
  const [to, setTo] = useState(() => localDateStr(new Date()));
  const [minConf, setMinConf] = useState(DEFAULT_MIN_CONF);
  const [rows, setRows] = useState([]);
  const [coverage, setCoverage] = useState(null);
  const [truncated, setTruncated] = useState(false);
  const [loading, setLoading] = useState(false);
  const [searched, setSearched] = useState(false);
  const [playing, setPlaying] = useState(null);
  const [maximizing, setMaximizing] = useState(null);

  // Sites populate the site scope — the query term F-10 asked for and the browser fan-out
  // never had. A failure here hides the selector rather than blocking the search.
  useEffect(() => {
    let cancelled = false;
    api('/api/sites', { noRedirect: true })
      .then((r) => { if (!cancelled && r.ok) setSites(Array.isArray(r.body) ? r.body : (r.body?.items || [])); })
      .catch(() => {});
    return () => { cancelled = true; };
  }, []);

  const scopeParams = useCallback(() => {
    const p = new URLSearchParams();
    if (scope !== 'all') p.set('nodeId', scope);
    if (siteId > 0) p.set('siteId', String(siteId));
    return p;
  }, [scope, siteId]);

  // The label picker is populated from the fleet-wide union, computed server-side. The old
  // browser version capped its fan-out at 25 nodes, so a larger estate silently offered a
  // filter list missing whatever the remaining nodes had seen.
  useEffect(() => {
    let cancelled = false;
    const p = scopeParams();
    api(`/api/nodes/search/labels?${p}`, { noRedirect: true })
      .then((r) => {
        if (cancelled || !r.ok) return;
        setLabels(Array.isArray(r.body?.labels) ? r.body.labels : []);
      })
      .catch(() => {});
    return () => { cancelled = true; };
  }, [scopeParams]);

  async function runSearch(e) {
    if (e && e.preventDefault) e.preventDefault();
    setLoading(true);
    setSearched(true);
    const p = scopeParams();
    if (from) p.set('from', String(dayStartEpoch(from)));
    if (to) p.set('to', String(dayEndEpoch(to)));
    if (objs.length) p.set('labels', objs.join(','));
    if (text.trim()) p.set('text', text.trim());
    if (minConf > 0) p.set('minConfidence', String(minConf / 100));
    p.set('limit', String(RESULT_LIMIT));
    const r = await api(`/api/nodes/search?${p}`, { noRedirect: true }).catch(() => ({ ok: false }));
    if (!r.ok) {
      setRows([]);
      // A failed search is not an empty one. Leaving the previous coverage on screen would
      // attach it to results that were never fetched.
      setCoverage({ failed: true, message: r.message || '' });
      setTruncated(false);
      setLoading(false);
      return;
    }
    setRows((Array.isArray(r.body?.items) ? r.body.items : []).map(adaptHit));
    setCoverage(r.body?.coverage || null);
    setTruncated(Boolean(r.body?.truncated));
    setLoading(false);
  }

  const columns = [
    { key: '_nodeName', label: t('obj.colNode') },
    { key: 'siteName', label: t('obj.colSite') },
    { key: 'startedAt', label: t('obj.colTime'), render: (v) => formatTimestamp(v) },
    { key: '_cameraLabel', label: t('obj.colCamera'), render: (v, r) => v || t('obj.cameraN', { id: r.cameraId }) },
    {
      key: 'label',
      label: t('obj.colObject'),
      render: (v, r) => (r.kind === 'identity' ? (
        <span className="object-tag object-tag--identity" title={v}>
          {r.identity}
          <span className="object-tag-kind">{r.identityKind === 'plate' ? t('obj.kindPlate') : t('obj.kindFace')}</span>
        </span>
      ) : (
        <span className={`object-tag object-tag--${objectCategory(v)}`}>{v}{r.maxCount > 1 ? ` ×${r.maxCount}` : ''}</span>
      )),
    },
    { key: 'maxConfidence', label: t('obj.colConfidence'), render: (v) => `${Math.round((v || 0) * 100)}%` },
    {
      key: 'footage',
      label: t('obj.colFootage'),
      filterable: false,
      render: (_v, r) => {
        // An identity hit is an ALERT, not a presence interval: its evidence is the stored
        // alert snapshot, and it has no covering segment to seek into. Falling through to
        // the object branch would have labelled every plate the fleet ever recognized
        // "No footage" while its picture sat on the node, unoffered.
        if (r.kind === 'identity') {
          return r.hasSnapshot
            ? <IdentityThumb row={r} onOpen={() => setMaximizing(r)} t={t} />
            : <span className="footage-pending">{t('obj.noSnapshot')}</span>;
        }
        if (r.footagePending || !r.segmentId) {
          return <span className="footage-pending">{r.footagePending ? t('obj.footagePending') : t('obj.noFootage')}</span>;
        }
        return <FootageThumb row={r} onPlay={() => setPlaying(r)} onMaximize={() => setMaximizing(r)} t={t} />;
      },
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
          {sites.length > 1 ? (
            <label>{t('obj.site')}
              <select value={siteId} onChange={(e) => setSiteId(Number(e.target.value) || 0)} disabled={loading}>
                <option value="0">{t('obj.allSites')}</option>
                {sites.map((s) => <option key={s.id} value={s.id}>{s.name}</option>)}
              </select>
            </label>
          ) : null}
          <label>{t('obj.object')}
            <LabelPicker labels={labels} value={objs} onChange={setObjs} disabled={loading} t={t} />
          </label>
          <label>{t('obj.identity')}
            <input type="search" value={text} placeholder={t('obj.identityPlaceholder')}
              onChange={(e) => setText(e.target.value)} disabled={loading} />
          </label>
          <label>{t('obj.from')}<input type="date" value={from} max={to} onChange={(e) => setFrom(e.target.value)} disabled={loading} /></label>
          <label>{t('obj.to')}<input type="date" value={to} min={from} onChange={(e) => setTo(e.target.value)} disabled={loading} /></label>
          <label>{t('obj.minConf')}<input type="number" min="0" max="100" value={minConf} onChange={(e) => setMinConf(Math.max(0, Math.min(100, Number(e.target.value) || 0)))} disabled={loading} /></label>
          <button type="submit" disabled={loading}><span className="btn-icon"><Ico n="search" /> {loading ? t('obj.searching') : t('obj.search')}</span></button>
        </form>

        {searched && !loading ? <SearchCoverage coverage={coverage} truncated={truncated} t={t} /> : null}
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

// COVERAGE_STATUS_KEY maps a node's coverage status onto its translated label. Written as an
// explicit map rather than a built key so the i18n drift guard can see every string that
// ships, and so an unknown status from a newer control plane renders as itself rather than
// as a missing-key placeholder.
const COVERAGE_STATUS_KEY = {
  offline: 'obj.statusOffline',
  timeout: 'obj.statusTimeout',
  denied: 'obj.statusDenied',
  unsupported: 'obj.statusUnsupported',
  error: 'obj.statusError',
};

// SearchCoverage says what the search actually reached.
//
// It is not decoration. An empty result set means one of two completely different things —
// "the fleet never saw this" or "the recorders that would have seen it were not asked" —
// and only this block can tell them apart. So it renders on EVERY completed search: a
// reassuring line when coverage was total, a warning naming each unanswered node otherwise.
function SearchCoverage({ coverage, truncated, t }) {
  if (!coverage) return null;
  if (coverage.failed) {
    return <p className="search-coverage search-coverage--bad">{coverage.message || t('obj.coverageFailed')}</p>;
  }
  const searched = coverage.searched || 0;
  const answered = coverage.answered || 0;
  const problems = (coverage.nodes || []).filter((n) => n.status && n.status !== 'ok');
  const statusText = (status) => (COVERAGE_STATUS_KEY[status] ? t(COVERAGE_STATUS_KEY[status]) : status);

  if (searched === 0) {
    return <p className="search-coverage search-coverage--bad">{t('obj.coverageNone')}</p>;
  }
  const complete = Boolean(coverage.complete) && !truncated;
  return (
    <div className={`search-coverage ${complete ? 'search-coverage--ok' : 'search-coverage--warn'}`}>
      <p className="search-coverage-headline">
        {complete ? t('obj.coverageComplete', { n: searched }) : t('obj.coveragePartial', { answered, searched })}
        {coverage.skippedKind > 0 ? ` ${t('obj.coverageSkipped', { n: coverage.skippedKind })}` : ''}
      </p>
      {!complete && coverage.completeThrough > 0 ? (
        <p className="search-coverage-horizon">{t('obj.coverageHorizon', { time: formatTimestamp(coverage.completeThrough) })}</p>
      ) : null}
      {truncated ? <p className="search-coverage-horizon">{t('obj.truncated', { n: RESULT_LIMIT })}</p> : null}
      {problems.length > 0 ? (
        <ul className="search-coverage-nodes">
          {problems.map((n) => (
            <li key={n.nodeId}>
              <strong>{n.nodeName || n.nodeId}</strong>
              <span className={`coverage-status coverage-status--${n.status}`}>{statusText(n.status)}</span>
              {n.reason ? <span className="coverage-reason">{n.reason}</span> : null}
            </li>
          ))}
        </ul>
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

// snapshotUrl builds the image for one sighting, at the requested width.
//
// The two kinds of hit keep their evidence in different places, and this is the only spot
// that needs to know: an object sighting is a moment inside recorded footage, so the frame
// is rendered from its covering segment with the peak box drawn on; an identity hit is an
// alert, whose snapshot the node already stored when the rule fired. Both go over the
// node's proxy — the browser never contacts an appliance directly.
function snapshotUrl(row, width) {
  const proxy = `${apiBase()}/api/nodes/${encodeURIComponent(row._nodeId)}/proxy`;
  if (row.kind === 'identity') {
    return `${proxy}/api/vision/alerts/${row.id}/snapshot`;
  }
  const pct = Math.round((row.maxConfidence || 0) * 100);
  const params = new URLSearchParams({ seek: String(row.seekSeconds || 0), w: String(width) });
  const boxStr = boxStrFromPeak(row.peakBox);
  if (boxStr) {
    params.set('box', boxStr);
    params.set('label', `${row.label || ''} ${pct}%`.trim());
  }
  return `${proxy}/api/recording/segments/${row.segmentId}/frame?${params}`;
}

// IdentityThumb is the footage cell for a recognized plate or face: the alert snapshot the
// node kept, click to enlarge. There is no play action — an alert is a moment, and the
// clip that may or may not surround it is a different question from "is this the car".
function IdentityThumb({ row, onOpen, t }) {
  const [failed, setFailed] = useState(false);
  if (failed) {
    return <span className="footage-pending">{t('obj.noSnapshot')}</span>;
  }
  return (
    <div className="obs-thumb">
      <img src={snapshotUrl(row, 200)} alt="" loading="lazy" onError={() => setFailed(true)} />
      <div className="obs-thumb-actions">
        <button type="button" className="obs-thumb-btn" onClick={onOpen} title={t('obj.maximize')} aria-label={t('obj.maximize')}>
          <Ico n="camera" sz={16} />
        </button>
      </div>
    </div>
  );
}

// FootageThumb is the footage cell, matching mymatasan's Object Search exactly: a boxed
// snapshot of the sighting moment (proxied frame, box + label drawn server-side) with
// translucent overlay buttons — Play (opens the covering segment) and Maximize (opens the
// snapshot large). Falls back to a plain play button if the frame fails to load.
function FootageThumb({ row, onPlay, onMaximize, t }) {
  const [failed, setFailed] = useState(false);
  // The box + label are drawn server-side, so the cell reads e.g. "person 87%".
  const url = snapshotUrl(row, 200);
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
            <span className="video-dialog-title">{row._nodeName} · {row._cameraLabel || t('obj.cameraN', { id: row.cameraId })}</span>
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
  const pct = Math.round((row.maxConfidence || 0) * 100);
  const url = snapshotUrl(row, 1280);
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
            <span className="video-dialog-title">{row._nodeName} · {row._cameraLabel || t('obj.cameraN', { id: row.cameraId })}</span>
            <span className={`object-tag object-tag--${objectCategory(row.label)}`}>{row.label}{row.maxCount > 1 ? ` ×${row.maxCount}` : ''}</span>
          </div>
          <button type="button" className="video-dialog-close" onClick={onClose} aria-label={t('obj.close')}>✕</button>
        </div>
        <div className="snap-body"><img className="snap-image" src={url} alt="" /></div>
        <div className="video-dialog-meta">
          {formatTimestamp(row.startedAt)} · {pct}%
          {row.segmentId ? (
            <button type="button" className="quiet snap-play" onClick={onPlay}>
              <span className="btn-icon"><Ico n="play" /> {t('obj.play')}</span>
            </button>
          ) : null}
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
