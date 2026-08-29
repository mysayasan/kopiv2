import { useCallback, useEffect, useMemo, useState } from 'react'
import { useT, DataTable, Ico } from '@shared'
import * as api from '../lib/access'

// The administrative trail: who changed the rules about who gets in.
//
// This is the companion to Activity, and the pair only makes sense together. Activity answers "who
// went through which door, when, and was it allowed". This answers the question that log cannot:
// WHO DECIDED THEY COULD. A grant edited at 23:40 produces nothing in the access log — it produces
// ordinary green badge events three weeks later, on a door somebody was never meant to reach.
//
// So the two screens are deliberately separate rather than tabs on one. They are read by different
// people for different reasons, they are governed by different catalog rules (the access log is
// granted to every role; this one is not), and merging them would bury a dozen rule changes a year
// under a hundred thousand badge events.
const PAGE_SIZE = 300

export default function Trail() {
  const t = useT()
  const [rows, setRows] = useState(null)
  const [total, setTotal] = useState(0)
  const [group, setGroup] = useState('')
  const [error, setError] = useState('')

  const load = useCallback(async () => {
    try {
      const res = await api.listAudit({ limit: PAGE_SIZE })
      // The listing answers {items, total, ...}; a bare array would mean the envelope changed under
      // us, and rendering nothing would look exactly like an appliance where nothing ever happened.
      const items = Array.isArray(res) ? res : (res && Array.isArray(res.items) ? res.items : [])
      setRows(items)
      setTotal(res && typeof res.total === 'number' ? res.total : items.length)
      setError('')
    } catch (e) {
      setError(e && e.message ? e.message : t('common.error'))
    }
  }, [t])

  useEffect(() => { load() }, [load])

  // The narrowing happens here, over the page already fetched — see AUDIT_FILTERS in lib/access.js
  // for why, and note the empty state below, which says so rather than implying nothing happened.
  const shown = useMemo(() => {
    if (!rows) return null
    if (!group) return rows
    const wanted = (api.AUDIT_FILTERS.find(f => f.id === group) || {}).targetTypes || []
    return rows.filter(r => wanted.includes(r.targetType))
  }, [rows, group])

  const columns = [
    { key: 'createdAt', label: t('trail.when'), render: (_v, r) => api.formatTime(r.createdAt) },
    {
      key: 'actorEmail',
      label: t('trail.who'),
      // An entry with no actor is still an entry — the server writes one rather than dropping the
      // record — so it renders as "the appliance itself" instead of an empty cell that reads as a
      // rendering bug.
      render: (_v, r) => r.actorEmail || <span className="muted">{t('trail.system')}</span>
    },
    {
      key: 'action',
      label: t('trail.what'),
      render: (_v, r) => {
        const key = api.AUDIT_ACTION_KEYS[r.action]
        return <span className="pill">{key ? t(key) : r.action}</span>
      }
    },
    {
      key: 'detail',
      label: t('trail.detail'),
      // The DETAIL is the row. Every entry the server writes carries a whole sentence — "group
      // \"Night cleaners\" granted door \"Loading bay\" on schedule \"Out of hours\"" — because a
      // trail of ids is one nobody can read without the database beside them.
      //
      // WRAPPED IN <bdi dir="ltr"> BECAUSE THE SENTENCE IS NOT TRANSLATED, AND MUST NOT BE. It is
      // composed server-side, in English, at the moment the change happened, and stored verbatim —
      // an audit record says what it said; re-rendering it in the reader's language would make the
      // trail's text depend on who is looking at it. The labels around it ARE translated, so an
      // Arabic reader gets a localised screen with English evidence in it.
      //
      // Without the isolation the RTL paragraph direction reorders that English sentence: the
      // quotes and the trailing punctuation jump to the wrong end, so `card issued to "Screen Bench
      // Person"` renders as `"card issued to "Screen Bench Person`. Nothing asserts on quote
      // placement, and every check passed while the Arabic screenshot showed it — which is why the
      // rule here is to open the PNG.
      render: (_v, r) => r.detail ? <bdi dir="ltr">{r.detail}</bdi> : '—'
    },
    {
      key: 'outcome',
      label: t('trail.result'),
      render: (_v, r) => (
        <span className={`pill pill-${r.outcome === 'success' ? 'ok' : 'danger'}`}>
          {r.outcome === 'success' ? t('trail.done') : t('trail.refused')}
        </span>
      )
    },
    {
      key: 'clientIp',
      label: t('trail.from'),
      render: (_v, r) => r.clientIp ? <code>{r.clientIp}</code> : '—'
    }
  ]

  return (
    <div className="screen">
      <header className="screen-head">
        <div>
          <h1>{t('trail.title')}</h1>
          <p className="muted">{t('trail.subtitle')}</p>
        </div>
        <div className="head-actions">
          <div className="seg">
            <button type="button" className={group === '' ? 'on' : ''} onClick={() => setGroup('')}>
              {t('trail.filter.all')}
            </button>
            {api.AUDIT_FILTERS.map(f => (
              <button
                key={f.id}
                type="button"
                className={group === f.id ? 'on' : ''}
                onClick={() => setGroup(f.id)}
              >
                {t(f.label)}
              </button>
            ))}
          </div>
          {/* A real link, not a button that fetches: the export is a download the server names and
              stamps, and it carries whatever the trail holds rather than what this page happened to
              load. */}
          <a className="btn" href={api.auditCsvUrl({ limit: 10000 })} download>
            <Ico n="download" sz={15} /> {t('trail.export')}
          </a>
        </div>
      </header>

      {error ? (
        <div className="notice notice-error">
          <p>{error}</p>
          <button type="button" onClick={load}>{t('common.retry')}</button>
        </div>
      ) : null}

      {shown === null && !error ? <p>{t('common.loading')}</p> : null}

      {shown && shown.length === 0 ? (
        <p className="muted">
          <Ico n="list" sz={15} />{' '}
          {group
            // Says WHAT WAS SEARCHED. "No entries" after a filter would be read as "this never
            // happened", and the filter only ever saw the most recent page.
            ? t('trail.emptyFiltered').replace('{n}', String(rows.length))
            : t('trail.empty')}
        </p>
      ) : null}

      {shown && shown.length > 0 ? (
        <>
          <DataTable columns={columns} rows={shown} />
          {total > rows.length ? (
            <p className="muted small">{t('trail.truncated').replace('{n}', String(rows.length)).replace('{total}', String(total))}</p>
          ) : null}
        </>
      ) : null}
    </div>
  )
}
