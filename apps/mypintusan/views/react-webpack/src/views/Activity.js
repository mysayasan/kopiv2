import { useCallback, useEffect, useState } from 'react'
import { useT, DataTable, Ico } from '@shared'
import * as api from '../lib/access'

// The access log. This is the screen that gets opened after something has gone wrong, so it defaults
// to showing EVERYTHING — a refusal is at least as interesting as an entry, and a denied unknown
// card at 03:00 on a perimeter door is the most valuable row the system holds.
export default function Activity() {
  const t = useT()
  const [rows, setRows] = useState(null)
  const [filter, setFilter] = useState('')
  const [error, setError] = useState('')

  const load = useCallback(async () => {
    try {
      const list = await api.listEvents({ limit: 300, decision: filter || undefined })
      setRows(Array.isArray(list) ? list : [])
      setError('')
    } catch (e) {
      setError(e && e.message ? e.message : t('common.error'))
    }
  }, [filter, t])

  useEffect(() => { load() }, [load])

  // Refresh on a timer. A door log that only updates on reload is one an operator stops trusting.
  useEffect(() => {
    const id = setInterval(load, 10000)
    return () => clearInterval(id)
  }, [load])

  const columns = [
    { key: 'at', label: t('activity.when'), render: (_v, r) => api.formatTime(r.at) },
    {
      key: 'who',
      label: t('activity.who'),
      render: (_v, r) => r.holderName || <span className="muted">{t('activity.unknownPerson')}</span>
    },
    { key: 'doorId', label: t('activity.door'), render: (_v, r) => r.doorId || '—' },
    {
      key: 'decision',
      label: t('activity.result'),
      render: (_v, r) => (
        <span className={`pill pill-${r.decision === 'granted' ? 'ok' : 'danger'}`}>
          {r.decision === 'granted' ? t('activity.granted') : t('activity.denied')}
        </span>
      )
    },
    {
      key: 'reason',
      label: t('activity.why'),
      render: (_v, r) => (
        <span>
          {t(api.REASON_KEYS[r.reason] || 'common.none')}
          {/* Duress is flagged in the log but never at the door. The person who typed it must not
              be able to tell an alarm was raised, and neither must anyone standing over them. */}
          {r.duress ? <span className="pill pill-danger sm">{t('activity.duress')}</span> : null}
          {r.offline ? <span className="pill sm">{t('activity.offline')}</span> : null}
        </span>
      )
    },
    {
      key: 'rawCredential',
      label: t('activity.card'),
      // The raw value is shown even — especially — when nothing matched it. It is what turns
      // "access refused" into "this specific card was tried at this door", which is the difference
      // between a shrug and an investigation.
      render: (_v, r) => r.rawCredential ? <code>{r.rawCredential}</code> : '—'
    }
  ]

  return (
    <div className="screen">
      <header className="screen-head">
        <div>
          <h1>{t('activity.title')}</h1>
          <p className="muted">{t('activity.subtitle')}</p>
        </div>
        <div className="seg">
          <button type="button" className={filter === '' ? 'on' : ''} onClick={() => setFilter('')}>
            {t('activity.all')}
          </button>
          <button type="button" className={filter === 'denied' ? 'on' : ''} onClick={() => setFilter('denied')}>
            {t('activity.onlyDenied')}
          </button>
          <button type="button" className={filter === 'granted' ? 'on' : ''} onClick={() => setFilter('granted')}>
            {t('activity.onlyGranted')}
          </button>
        </div>
      </header>

      {error ? (
        <div className="notice notice-error">
          <p>{error}</p>
          <button type="button" onClick={load}>{t('common.retry')}</button>
        </div>
      ) : null}

      {rows === null && !error ? <p>{t('common.loading')}</p> : null}
      {rows && rows.length === 0 ? (
        <p className="muted"><Ico n="list" sz={15} /> {t('activity.empty')}</p>
      ) : null}
      {rows && rows.length > 0 ? <DataTable columns={columns} rows={rows} /> : null}
    </div>
  )
}
