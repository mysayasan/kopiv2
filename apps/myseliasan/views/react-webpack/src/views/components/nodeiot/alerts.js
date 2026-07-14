import { useCallback, useEffect, useState } from 'react';
import { DataTable, Ico, useT } from '@shared';
import { accessError, api, forbidden, formatNumber, formatTimestamp } from './lib/helpers';
import { AccessDenied, Panel, SeverityPill } from './ui';

// AlertsPage is the node's log: every firing, newest first, with the reading that tripped it.
//
// It has an Acknowledge button and NO delete button, and that is deliberate. Acknowledging
// is an operator power — "I have seen this". Erasing is not: the person who was present at
// an incident must not be the person who can remove the record of it. The node does not
// expose a delete either, so this is not a gap in the UI, it is the design — and the control
// plane does not get to add one just because it is one level up.
export function AlertsPage({ onToast }) {
  const t = useT();
  const [rows, setRows] = useState([]);
  const [total, setTotal] = useState(0);
  const [devices, setDevices] = useState([]);
  const [deviceId, setDeviceId] = useState(0);
  const [query, setQuery] = useState({ offset: 0, limit: 25 });
  const [busy, setBusy] = useState(true);
  const [error, setError] = useState('');
  const [denied, setDenied] = useState(false);
  const [acking, setAcking] = useState(0);

  useEffect(() => {
    api('/api/devices?limit=1000').then((r) => { if (r.ok) setDevices(r.body?.items || []); }).catch(() => {});
  }, []);

  const load = useCallback(async () => {
    setBusy(true);
    const params = new URLSearchParams({ limit: String(query.limit), offset: String(query.offset) });
    if (deviceId) params.set('deviceId', String(deviceId));
    const r = await api(`/api/alerts?${params.toString()}`);
    if (r.ok) {
      setRows(r.body?.items || []);
      setTotal(Number(r.body?.total) || 0);
      setError('');
      setDenied(false);
    } else if (forbidden(r)) {
      setDenied(true);
      setError('');
    } else {
      setDenied(false);
      setError(accessError(r, t, t('alerts.loadFailed')));
    }
    setBusy(false);
  }, [query, deviceId, t]);

  useEffect(() => { load(); }, [load]);

  // Acknowledging is an operator power at the node. A viewer's grant will be refused there,
  // and the refusal is reported as what it is rather than as a mysterious failed click.
  async function ack(alert) {
    setAcking(alert.id);
    const r = await api(`/api/alerts/${alert.id}/ack`, { method: 'POST' });
    setAcking(0);
    if (!r.ok) {
      onToast(forbidden(r) ? t('niot.ackForbidden') : accessError(r, t, t('alerts.ackFailed')), 'error');
      return;
    }
    onToast(t('alerts.acked'), 'success');
    load();
  }

  // The alert API pages and filters by device only — it does not take the grid's column
  // filters or sorters. Rather than render filter/sort controls that would silently do
  // nothing, every column opts out and the one filter the server DOES support is a real
  // control above the table.
  const columns = [
    { key: 'createdAt', label: t('alerts.when'), filterable: false, render: (v) => formatTimestamp(v) },
    { key: 'severity', label: t('rules.severity'), filterable: false, render: (v) => <SeverityPill severity={v} /> },
    { key: 'deviceName', label: t('alerts.device'), filterable: false, render: (v) => v || '—' },
    { key: 'ruleName', label: t('alerts.rule'), filterable: false, render: (v) => v || '—' },
    { key: 'message', label: t('alerts.message'), filterable: false },
    { key: 'value', label: t('alerts.value'), filterable: false, render: (v, row) => (row.key ? `${row.key}: ${formatNumber(v)}` : formatNumber(v)) },
    { key: 'ackedAt', label: t('alerts.ack'), filterable: false, render: (v, row) => (
      v ? (
        <span className="status-pill resolved" title={formatTimestamp(v)}>{t('alerts.ackedAt')}</span>
      ) : (
        <button type="button" className="quiet" disabled={acking === row.id} onClick={() => ack(row)}>
          <span className="btn-icon"><Ico n="acknowledge" sz={14} /> {t('alerts.ackAction')}</span>
        </button>
      )
    ) },
  ];

  return (
    <section className="workspace">
      <Panel
        icon="bell"
        title={t('page.alerts')}
        hint={t('page.alertsHint')}
        actions={denied ? null : (
          <button type="button" className="quiet" onClick={load} disabled={busy}>
            <span className="btn-icon"><Ico n="refresh" sz={14} /> {t('common.refresh')}</span>
          </button>
        )}
      >
        {denied ? <AccessDenied /> : (
          <>
            {error ? <div className="status-line">{error}</div> : null}
            <div className="toolbar">
              <label className="iot-inline-filter">
                <span>{t('alerts.filterDevice')}</span>
                <select
                  value={deviceId}
                  onChange={(e) => { setDeviceId(Number(e.target.value)); setQuery((q) => ({ ...q, offset: 0 })); }}
                >
                  <option value={0}>{t('alerts.allDevices')}</option>
                  {devices.map((d) => <option key={d.id} value={d.id}>{d.name}</option>)}
                </select>
              </label>
              <p className="settings-hint">{t('alerts.noDeleteNote')}</p>
            </div>
            <DataTable
              key={`alerts-${deviceId}`}
              rows={rows}
              columns={columns}
              serverMode
              total={total}
              onQuery={(q) => setQuery((prev) => (
                // Keep the identity stable when nothing actually moved, so the grid's
                // mount-time emit does not fire a second, identical fetch.
                prev.offset === q.offset && prev.limit === q.limit ? prev : { offset: q.offset, limit: q.limit }
              ))}
              busy={busy}
              pageSize={25}
              pageSizeOptions={[25, 50, 100]}
              emptyText={t('alerts.empty')}
            />
          </>
        )}
      </Panel>
    </section>
  );
}
