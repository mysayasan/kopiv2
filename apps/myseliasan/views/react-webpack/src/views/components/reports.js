import { useEffect, useState, useCallback, useRef } from 'react';
import { Ico, useT } from '@shared';
import { apiBase, api } from '../lib/helpers';
import '../styles/reports.css';

// ReportsPage is the on-demand PDF report hub. Each card gathers its parameters and
// generates a server-rendered PDF from /api/reports/* (rendered pure-Go on the control
// plane — no headless browser, myseliasan runs air-gapped).
//
// The report is shown in a PREVIEW modal first — the browser's built-in PDF viewer
// renders the blob inline, and the operator prints or downloads from there — rather than
// downloading blindly. The Security & Access report is superadmin-gated on the server;
// its card is hidden for non-superadmins so they are not offered a button that 403s.

const RANGE_OPTIONS = [7, 30, 90];

export function ReportsPage({ session, onToast }) {
  const t = useT();
  const [sites, setSites] = useState([]);
  const [busy, setBusy] = useState('');
  const [fleetRange, setFleetRange] = useState(30);
  const [securityRange, setSecurityRange] = useState(30);
  const [incidentRange, setIncidentRange] = useState(30);
  const [inventorySite, setInventorySite] = useState(0);
  // preview = { url, filename, title } for the currently-open PDF, or null.
  const [preview, setPreview] = useState(null);

  useEffect(() => {
    let alive = true;
    api('/api/sites', { noRedirect: true })
      .then((r) => { if (alive && r.ok && Array.isArray(r.body)) setSites(r.body); })
      .catch(() => {});
    return () => { alive = false; };
  }, []);

  // Revoke the object URL when the preview closes or is replaced, so blobs don't leak.
  const closePreview = useCallback(() => {
    setPreview((p) => {
      if (p) { try { URL.revokeObjectURL(p.url); } catch (_) {} }
      return null;
    });
  }, []);
  useEffect(() => () => { if (preview) { try { URL.revokeObjectURL(preview.url); } catch (_) {} } }, [preview]);

  // generate streams a PDF via fetch (the shared api() helper unwraps JSON envelopes and
  // cannot carry binary), then opens it in the preview modal. GET reports need no CSRF
  // header (only writes do).
  const generate = useCallback(async (key, path, title) => {
    setBusy(key);
    try {
      const res = await fetch(`${apiBase()}${path}`, { credentials: 'include' });
      if (!res.ok) {
        onToast && onToast(res.status === 403 ? t('reports.forbidden') : t('reports.failed'), 'error');
        return;
      }
      const blob = await res.blob();
      const cd = res.headers.get('Content-Disposition') || '';
      const m = /filename="?([^"]+)"?/.exec(cd);
      const filename = (m && m[1]) || 'report.pdf';
      const url = URL.createObjectURL(blob);
      setPreview((prev) => {
        if (prev) { try { URL.revokeObjectURL(prev.url); } catch (_) {} }
        return { url, filename, title };
      });
    } catch (_) {
      onToast && onToast(t('reports.failed'), 'error');
    } finally {
      setBusy('');
    }
  }, [onToast, t]);

  const rangeSelect = (value, onChange) => (
    <label className="reports-field">
      <span>{t('reports.period')}</span>
      <select value={value} onChange={(e) => onChange(Number(e.target.value))}>
        {RANGE_OPTIONS.map((d) => (
          <option key={d} value={d}>{t('reports.lastNDays', { n: d })}</option>
        ))}
      </select>
    </label>
  );

  return (
    <div className="settings-panel reports-panel">
      <div className="settings-header">
        <h2>{t('reports.title')}</h2>
      </div>
      <p className="settings-hint">{t('reports.hint')}</p>

      <div className="reports-grid">
        <ReportCard
          icon="activity"
          tone="steel"
          title={t('reports.fleet.title')}
          desc={t('reports.fleet.desc')}
          busy={busy === 'fleet'}
          t={t}
          onGenerate={() => generate('fleet', `/api/reports/fleet-health.pdf?range=${fleetRange}`, t('reports.fleet.title'))}
        >
          {rangeSelect(fleetRange, setFleetRange)}
        </ReportCard>

        <ReportCard
          icon="building"
          tone="teal"
          title={t('reports.inventory.title')}
          desc={t('reports.inventory.desc')}
          busy={busy === 'inventory'}
          t={t}
          onGenerate={() => generate('inventory', `/api/reports/inventory.pdf${inventorySite ? `?siteId=${inventorySite}` : ''}`, t('reports.inventory.title'))}
        >
          <label className="reports-field">
            <span>{t('reports.site')}</span>
            <select value={inventorySite} onChange={(e) => setInventorySite(Number(e.target.value))}>
              <option value={0}>{t('reports.allSites')}</option>
              {sites.map((s) => (
                <option key={s.id} value={s.id}>{s.name}</option>
              ))}
            </select>
          </label>
        </ReportCard>

        <ReportCard
          icon="warning"
          tone="amber"
          title={t('reports.incident.title')}
          desc={t('reports.incident.desc')}
          busy={busy === 'incident'}
          t={t}
          onGenerate={() => generate('incident', `/api/reports/incident.pdf?range=${incidentRange}`, t('reports.incident.title'))}
        >
          {rangeSelect(incidentRange, setIncidentRange)}
        </ReportCard>

        {session?.isSuperadmin ? (
          <ReportCard
            icon="shield"
            tone="violet"
            title={t('reports.security.title')}
            desc={t('reports.security.desc')}
            busy={busy === 'security'}
            t={t}
            onGenerate={() => generate('security', `/api/reports/security.pdf?range=${securityRange}`, t('reports.security.title'))}
          >
            {rangeSelect(securityRange, setSecurityRange)}
          </ReportCard>
        ) : null}
      </div>

      {preview ? <ReportPreview preview={preview} t={t} onClose={closePreview} onToast={onToast} /> : null}
    </div>
  );
}

function ReportCard({ icon, tone, title, desc, children, busy, onGenerate, t }) {
  return (
    <div className={`reports-card tone-${tone}`}>
      <div className="reports-card-head">
        <span className="reports-card-ico"><Ico n={icon} sz={20} /></span>
        <h3>{title}</h3>
      </div>
      <p className="reports-card-desc">{desc}</p>
      <div className="reports-card-controls">{children}</div>
      <button type="button" className="reports-card-btn" onClick={onGenerate} disabled={busy}>
        <Ico n={busy ? 'reload' : 'eye'} sz={15} />
        <span>{busy ? t('reports.generating') : t('reports.preview')}</span>
      </button>
    </div>
  );
}

// ReportPreview is a full-screen modal showing the generated PDF in an iframe (the
// browser's native PDF viewer). Print drives the iframe's own print dialog; Download
// saves the blob with its server-suggested filename.
function ReportPreview({ preview, t, onClose, onToast }) {
  const frameRef = useRef(null);

  useEffect(() => {
    const onKey = (e) => { if (e.key === 'Escape') onClose(); };
    document.addEventListener('keydown', onKey);
    return () => document.removeEventListener('keydown', onKey);
  }, [onClose]);

  const doPrint = () => {
    try {
      const win = frameRef.current && frameRef.current.contentWindow;
      if (win) { win.focus(); win.print(); }
      else throw new Error('no frame');
    } catch (_) {
      onToast && onToast(t('reports.printBlocked'), 'error');
    }
  };

  return (
    <div className="reports-preview-overlay" role="dialog" aria-modal="true" onClick={onClose}>
      <div className="reports-preview-modal" onClick={(e) => e.stopPropagation()}>
        <div className="reports-preview-head">
          <h3 title={preview.filename}>{preview.title}</h3>
          <div className="reports-preview-actions">
            <button type="button" className="reports-preview-btn" onClick={doPrint}>
              <Ico n="display" sz={15} /><span>{t('reports.print')}</span>
            </button>
            <a className="reports-preview-btn" href={preview.url} download={preview.filename}>
              <Ico n="download" sz={15} /><span>{t('reports.download')}</span>
            </a>
            <button type="button" className="reports-preview-btn reports-preview-close" onClick={onClose} aria-label={t('reports.close')}>
              <Ico n="x" sz={16} />
            </button>
          </div>
        </div>
        <iframe ref={frameRef} className="reports-preview-frame" title={preview.title} src={preview.url} />
      </div>
    </div>
  );
}
