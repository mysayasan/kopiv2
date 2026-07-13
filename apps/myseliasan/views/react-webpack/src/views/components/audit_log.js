import { useEffect, useState, useCallback, useMemo } from 'react';
import { Ico, DataTable, useT } from '@shared';
import { FormBusyOverlay } from './ui';
import { api, formatTimestamp } from '../lib/helpers';

// AuditLogPage is the superadmin-only viewer over the append-only audit trail
// (/api/audit). It follows the standard admin-page shape (settings-panel + header +
// settings-hint, like Users / Roles) and leans on the shared DataTable's native
// per-column filtering and sorting instead of bespoke controls. The trail itself is
// immutable — this page is read-only.

const outcomePill = (outcome) => {
  switch ((outcome || '').toLowerCase()) {
    case 'success': return 'online';
    case 'denied':
    case 'error': return 'offline';
    default: return '';
  }
};

export function AuditLogPage({ onToast }) {
  const t = useT();
  const [rows, setRows] = useState([]);
  const [loading, setLoading] = useState(false);

  const load = useCallback(async () => {
    setLoading(true);
    const r = await api('/api/audit?limit=500', { noRedirect: true }).catch(() => ({ ok: false }));
    setLoading(false);
    if (r.ok) setRows(Array.isArray(r.body) ? r.body : []);
    else if (onToast) onToast(r.message || t('audit.loadFailed'), 'error');
  }, [onToast, t]);
  useEffect(() => { load(); }, [load]);

  // Friendly labels for the enum-ish columns; unknown values fall back to the raw
  // string so new server-side actions/outcomes still render.
  const labelFor = useCallback((prefix, v) => {
    if (!v) return '';
    const key = `${prefix}.${v}`;
    const translated = t(key, {});
    return translated === key ? v : translated;
  }, [t]);

  const columns = useMemo(() => [
    { key: 'createdAt', label: t('audit.colTime'), filterType: 'daterange', render: (v) => formatTimestamp(v) },
    {
      key: 'actorEmail',
      label: t('audit.colActor'),
      render: (v, row) => <span className="node-name-cell"><Ico n="user" sz={14} /> {v || (row.actorId ? `#${row.actorId}` : t('audit.actorSystem'))}</span>,
    },
    { key: 'action', label: t('audit.colAction'), render: (v) => labelFor('audit.action', v) },
    {
      key: 'targetType',
      label: t('audit.colTarget'),
      render: (v, row) => (v || row.targetId ? `${v || ''}${row.targetId ? `: ${row.targetId}` : ''}` : '—'),
    },
    { key: 'outcome', label: t('audit.colOutcome'), render: (v) => <span className={`status-pill ${outcomePill(v)}`}>{labelFor('audit.outcome', v) || '—'}</span> },
    { key: 'detail', label: t('audit.colDetail'), filterable: false },
  ], [t, labelFor]);

  return (
    <section className="workspace">
      <FormBusyOverlay busy={loading} />
      <section className="settings-panel span-two">
        <header>
          <h2><span className="btn-icon"><Ico n="list" /> {t('nav.audit')}</span></h2>
          <div className="settings-header-actions">
            <button type="button" className="quiet" onClick={load} disabled={loading}>
              <span className="btn-icon"><Ico n="reload" /> {t('rb.refresh')}</span>
            </button>
          </div>
        </header>
        <p className="settings-hint">{t('audit.subtitle')}</p>
        <DataTable rows={rows} columns={columns} pageSize={15} busy={loading} emptyText={t('audit.empty')} />
      </section>
    </section>
  );
}
