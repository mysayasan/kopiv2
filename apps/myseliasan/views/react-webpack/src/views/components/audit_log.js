import { useEffect, useState, useCallback, useMemo } from 'react';
import { Ico, DataTable, useT } from '@shared';
import { api, formatTimestamp } from '../lib/helpers';

// AuditLogPage is the superadmin-only viewer over the append-only audit trail
// (/api/audit). It reads the most recent entries and lets the operator narrow by
// action and target type. The trail itself is immutable — this page is read-only.

// Known action verbs → a translation key + a tone for the pill. Unknown actions fall
// back to their raw string with a neutral tone, so new server-side actions still show.
const ACTIONS = [
  'node.adopt', 'node.release', 'node.command', 'node.self_dropped',
  'fleet.key_rotate', 'rbac.set_role', 'rbac.set_disabled', 'rbac.elevate',
  'node_access.set', 'node_access.revoke',
];

const outcomePill = (outcome) => {
  switch ((outcome || '').toLowerCase()) {
    case 'success': return 'online';
    case 'denied': return 'offline';
    case 'error': return 'offline';
    default: return '';
  }
};

export function AuditLogPage({ onToast }) {
  const t = useT();
  const [rows, setRows] = useState([]);
  const [loading, setLoading] = useState(false);
  const [action, setAction] = useState('');
  const [targetType, setTargetType] = useState('');

  const load = useCallback(async () => {
    setLoading(true);
    const params = new URLSearchParams({ limit: '200' });
    if (action) params.set('action', action);
    if (targetType) params.set('targetType', targetType);
    const r = await api(`/api/audit?${params}`, { noRedirect: true }).catch(() => ({ ok: false }));
    setLoading(false);
    if (r.ok) setRows(Array.isArray(r.body) ? r.body : []);
    else if (onToast) onToast(r.message || t('audit.loadFailed'), 'error');
  }, [action, targetType, onToast, t]);
  useEffect(() => { load(); }, [load]);

  const actionLabel = useCallback((a) => {
    if (!a) return '';
    const key = `audit.action.${a}`;
    const translated = t(key, {});
    return translated === key ? a : translated;
  }, [t]);

  const columns = useMemo(() => [
    { key: 'createdAt', label: t('audit.colTime'), render: (v) => formatTimestamp(v) },
    {
      key: 'actorEmail',
      label: t('audit.colActor'),
      render: (v, row) => <span className="node-name-cell"><Ico n="user" sz={14} /> {v || (row.actorId ? `#${row.actorId}` : t('audit.actorSystem'))}</span>,
    },
    { key: 'action', label: t('audit.colAction'), render: (v) => <span className="audit-action">{actionLabel(v)}</span> },
    {
      key: 'targetId',
      label: t('audit.colTarget'),
      render: (v, row) => (v || row.targetType ? <span className="audit-target">{row.targetType}{v ? `: ${v}` : ''}</span> : '—'),
    },
    { key: 'outcome', label: t('audit.colOutcome'), render: (v) => <span className={`status-pill ${outcomePill(v)}`}>{t(`audit.outcome.${v}`, {}) !== `audit.outcome.${v}` ? t(`audit.outcome.${v}`, {}) : (v || '—')}</span> },
    { key: 'detail', label: t('audit.colDetail') },
  ], [t, actionLabel]);

  return (
    <section className="workspace audit-tab">
      <div className="camera-tab-header">
        <div>
          <h2 className="section-title">{t('nav.audit')}</h2>
          <p className="section-subtitle">{t('audit.subtitle')}</p>
        </div>
        <div className="dashboard-toolbar">
          <select className="filter-select" value={action} onChange={(e) => setAction(e.target.value)} aria-label={t('audit.filterAction')}>
            <option value="">{t('audit.allActions')}</option>
            {ACTIONS.map((a) => <option key={a} value={a}>{actionLabel(a)}</option>)}
          </select>
          <select className="filter-select" value={targetType} onChange={(e) => setTargetType(e.target.value)} aria-label={t('audit.filterTarget')}>
            <option value="">{t('audit.allTargets')}</option>
            <option value="node">node</option>
            <option value="user">user</option>
            <option value="fleet">fleet</option>
            <option value="node-access">node-access</option>
          </select>
          <button type="button" className="quiet" onClick={load} disabled={loading}>
            <span className="btn-icon"><Ico n="reload" /> {t('dash.refresh')}</span>
          </button>
        </div>
      </div>
      <DataTable rows={rows} columns={columns} pageSize={15} emptyText={t('audit.empty')} />
    </section>
  );
}
