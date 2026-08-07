import { useCallback, useEffect, useState } from 'react';
import { useT, DataTable, Ico } from '@shared';
import * as api from './lib/access';

// mypintusan's access log, ported into the control-plane embed. This is the screen that gets
// opened after something has gone wrong, so it defaults to showing EVERYTHING — a refusal is at
// least as interesting as an entry, and a denied unknown card at 03:00 on a perimeter door is
// the most valuable row the system holds.
export function ActivityPage() {
  const t = useT();
  const [rows, setRows] = useState(null);
  const [filter, setFilter] = useState('');
  const [error, setError] = useState('');

  const load = useCallback(async () => {
    try {
      const list = await api.listEvents({ limit: 300, decision: filter || undefined });
      setRows(Array.isArray(list) ? list : []);
      setError('');
    } catch (e) {
      setError(e && e.message ? e.message : t('ndoor.error'));
    }
  }, [filter, t]);

  useEffect(() => { load(); }, [load]);

  // Refresh on a timer. A door log that only updates on reload is one an operator stops trusting.
  useEffect(() => {
    const id = setInterval(load, 10000);
    return () => clearInterval(id);
  }, [load]);

  const columns = [
    { key: 'at', label: t('ndoor.activity.when'), render: (_v, r) => api.formatTime(r.at) },
    {
      key: 'who',
      label: t('ndoor.activity.who'),
      render: (_v, r) => r.holderName || <span className="muted">{t('ndoor.activity.unknownPerson')}</span>,
    },
    { key: 'doorId', label: t('ndoor.activity.door'), render: (_v, r) => r.doorId || '—' },
    {
      key: 'decision',
      label: t('ndoor.activity.result'),
      render: (_v, r) => (
        <span className={`pill pill-${r.decision === 'granted' ? 'ok' : 'danger'}`}>
          {r.decision === 'granted' ? t('ndoor.activity.granted') : t('ndoor.activity.denied')}
        </span>
      ),
    },
    {
      key: 'reason',
      label: t('ndoor.activity.why'),
      render: (_v, r) => (
        <span>
          {t(api.REASON_KEYS[r.reason] || 'ndoor.none')}
          {/* Duress is flagged in the log but never at the door. The person who typed it must not
              be able to tell an alarm was raised, and neither must anyone standing over them. */}
          {r.duress ? <span className="pill pill-danger sm">{t('ndoor.activity.duress')}</span> : null}
          {r.offline ? <span className="pill sm">{t('ndoor.activity.offline')}</span> : null}
        </span>
      ),
    },
    {
      key: 'rawCredential',
      label: t('ndoor.activity.card'),
      // The raw value is shown even — especially — when nothing matched it. It is what turns
      // "access refused" into "this specific card was tried at this door".
      render: (_v, r) => (r.rawCredential ? <code>{r.rawCredential}</code> : '—'),
    },
  ];

  return (
    <div className="screen">
      <header className="screen-head">
        <div>
          <h1>{t('ndoor.activity.title')}</h1>
          <p className="muted">{t('ndoor.activity.subtitle')}</p>
        </div>
        <div className="seg">
          <button type="button" className={filter === '' ? 'on' : ''} onClick={() => setFilter('')}>
            {t('ndoor.activity.all')}
          </button>
          <button type="button" className={filter === 'denied' ? 'on' : ''} onClick={() => setFilter('denied')}>
            {t('ndoor.activity.onlyDenied')}
          </button>
          <button type="button" className={filter === 'granted' ? 'on' : ''} onClick={() => setFilter('granted')}>
            {t('ndoor.activity.onlyGranted')}
          </button>
        </div>
      </header>

      {error ? (
        <div className="notice notice-error">
          <p>{error}</p>
          <button type="button" onClick={load}>{t('ndoor.retry')}</button>
        </div>
      ) : null}

      {rows === null && !error ? <p>{t('ndoor.loading')}</p> : null}
      {rows && rows.length === 0 ? (
        <p className="muted"><Ico n="list" sz={15} /> {t('ndoor.activity.empty')}</p>
      ) : null}
      {rows && rows.length > 0 ? <DataTable columns={columns} rows={rows} /> : null}
    </div>
  );
}
