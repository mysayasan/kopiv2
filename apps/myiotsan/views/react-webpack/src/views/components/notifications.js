import { useCallback, useEffect, useState } from 'react';
import { DataTable, Ico, useT } from '@shared';
import { api, errorMessage, formatTimestamp } from '../lib/helpers';
import { Panel, SeverityPill } from './ui';

// NotificationsPage is the unified feed every event source funnels into: rule alerts,
// device health, and the app's own security events. The Alerts log is the evidentiary
// record of a rule firing; this is the operator's inbox over all of it.
export function NotificationsPage({ onToast }) {
  const t = useT();
  const [rows, setRows] = useState([]);
  const [total, setTotal] = useState(0);
  const [unreadOnly, setUnreadOnly] = useState(false);
  const [query, setQuery] = useState({ offset: 0, limit: 25 });
  const [busy, setBusy] = useState(true);
  const [error, setError] = useState('');
  const [marking, setMarking] = useState(0);

  const load = useCallback(async () => {
    setBusy(true);
    const params = new URLSearchParams({ limit: String(query.limit), offset: String(query.offset) });
    if (unreadOnly) params.set('unread', 'true');
    const r = await api(`/api/notifications?${params.toString()}`);
    if (r.ok) {
      setRows(r.body?.items || []);
      setTotal(Number(r.body?.total) || 0);
      setError('');
    } else {
      setError(errorMessage(r, t('notif.loadFailed')));
    }
    setBusy(false);
  }, [query, unreadOnly, t]);

  useEffect(() => { load(); }, [load]);

  async function markRead(row) {
    setMarking(row.id);
    const r = await api(`/api/notifications/${row.id}/read`, { method: 'POST' });
    setMarking(0);
    if (!r.ok) {
      onToast(errorMessage(r, t('notif.markFailed')), 'error');
      return;
    }
    load();
  }

  // Like the alert log, the feed pages server-side and takes no column filters, so the
  // grid does not offer any it cannot honour.
  const columns = [
    { key: 'createdAt', label: t('alerts.when'), filterable: false, render: (v) => formatTimestamp(v) },
    { key: 'severity', label: t('rules.severity'), filterable: false, render: (v) => <SeverityPill severity={v} /> },
    { key: 'category', label: t('notif.category'), filterable: false },
    { key: 'title', label: t('notif.title'), filterable: false, render: (v, row) => (
      <div className="iot-notif-cell">
        <strong className={row.isRead ? '' : 'is-unread'}>{v}</strong>
        <span>{row.body}</span>
      </div>
    ) },
    { key: 'isRead', label: '', filterable: false, render: (v, row) => (
      v ? (
        <span className="status-pill" title={formatTimestamp(row.readAt)}>{t('notif.read')}</span>
      ) : (
        <button type="button" className="quiet" disabled={marking === row.id} onClick={() => markRead(row)}>
          {t('notif.markRead')}
        </button>
      )
    ) },
  ];

  return (
    <section className="workspace">
      <Panel
        icon="send"
        title={t('page.notifications')}
        hint={t('page.notificationsHint')}
        actions={(
          <button type="button" className="quiet" onClick={load} disabled={busy}>
            <span className="btn-icon"><Ico n="refresh" sz={14} /> {t('common.refresh')}</span>
          </button>
        )}
      >
        {error ? <div className="status-line">{error}</div> : null}
        <div className="toolbar">
          <div className="segmented">
            <button type="button" className={unreadOnly ? 'quiet' : 'active'} onClick={() => { setUnreadOnly(false); setQuery((q) => ({ ...q, offset: 0 })); }}>
              {t('common.all')}
            </button>
            <button type="button" className={unreadOnly ? 'active' : 'quiet'} onClick={() => { setUnreadOnly(true); setQuery((q) => ({ ...q, offset: 0 })); }}>
              {t('notif.unread')}
            </button>
          </div>
          <span className="settings-hint"><Ico n="info" sz={12} /> {t('notif.hint')}</span>
        </div>
        <DataTable
          key={`notif-${unreadOnly}`}
          rows={rows}
          columns={columns}
          serverMode
          total={total}
          onQuery={(q) => setQuery((prev) => (
            prev.offset === q.offset && prev.limit === q.limit ? prev : { offset: q.offset, limit: q.limit }
          ))}
          busy={busy}
          pageSize={25}
          pageSizeOptions={[25, 50, 100]}
          emptyText={t('notif.empty')}
        />
      </Panel>
    </section>
  );
}
