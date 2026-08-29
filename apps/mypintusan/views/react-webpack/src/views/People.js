import { useCallback, useEffect, useState } from 'react'
import { useT, DataTable, Ico } from '@shared'
import * as api from '../lib/access'

// People and their badges — the operator's daily surface. Issuing and revoking a badge is routine,
// reversible and fully logged, which is why it sits at operator level while the access RULES
// (groups, schedules, grants) are admin-only.
export default function People({ caps = {}, toast }) {
  const t = useT()
  const [holders, setHolders] = useState(null)
  const [selected, setSelected] = useState(null)
  const [adding, setAdding] = useState(false)
  const [error, setError] = useState('')

  const load = useCallback(async () => {
    try {
      const list = await api.listHolders()
      setHolders(Array.isArray(list) ? list : [])
      setError('')
    } catch (e) {
      setError(e && e.message ? e.message : t('common.error'))
    }
  }, [t])

  useEffect(() => { load() }, [load])

  const columns = [
    { key: 'name', label: t('people.name') },
    { key: 'ref', label: t('people.ref') },
    { key: 'kind', label: t('people.kind'), render: (_v, r) => t(`people.kind.${r.kind}`) },
    {
      key: 'status',
      label: t('people.status'),
      render: (_v, r) => (
        <span className={`pill pill-${r.status === 'active' ? 'ok' : 'warn'}`}>
          {t(`people.status.${r.status}`)}
        </span>
      )
    },
    {
      key: 'badges',
      label: t('people.badges'),
      render: (_v, r) => (
        <button type="button" className="btn btn-quiet" onClick={() => setSelected(r)}>
          {t('badge.title')}
        </button>
      )
    }
  ]

  return (
    <div className="screen">
      <header className="screen-head">
        <div>
          <h1>{t('people.title')}</h1>
          <p className="muted">{t('people.subtitle')}</p>
        </div>
        {/* Enrolling somebody is operator-level; a viewer sees who holds what and changes
            nothing. Offered only if the server would accept it. */}
        {caps.managePeople ? (
          <button type="button" className="btn btn-primary" onClick={() => setAdding(true)}>
            <Ico n="plus" sz={15} /> {t('people.add')}
          </button>
        ) : null}
      </header>

      {error ? <div className="notice notice-error"><p>{error}</p></div> : null}
      {holders === null && !error ? <p>{t('common.loading')}</p> : null}
      {holders && holders.length === 0 ? <p className="muted">{t('people.empty')}</p> : null}
      {holders && holders.length > 0 ? <DataTable columns={columns} rows={holders} /> : null}

      {adding ? (
        <AddPerson
          onClose={() => setAdding(false)}
          onSaved={() => { setAdding(false); load() }}
          toast={toast}
        />
      ) : null}
      {selected ? (
        <Badges holder={selected} canIssue={!!caps.issueBadges} onClose={() => setSelected(null)} toast={toast} />
      ) : null}
    </div>
  )
}

function AddPerson({ onClose, onSaved, toast }) {
  const t = useT()
  const [form, setForm] = useState({ name: '', ref: '', kind: 'staff' })
  const [saving, setSaving] = useState(false)

  const submit = async e => {
    e.preventDefault()
    setSaving(true)
    try {
      await api.createHolder(form)
      onSaved()
    } catch (err) {
      toast(err && err.message ? err.message : t('common.error'), 'error')
    } finally {
      setSaving(false)
    }
  }

  return (
    <Modal title={t('people.addTitle')} onClose={onClose}>
      <form onSubmit={submit} className="form">
        <label>
          <span>{t('people.name')}</span>
          <input required value={form.name} onChange={e => setForm({ ...form, name: e.target.value })} />
        </label>
        <label>
          <span>{t('people.ref')}</span>
          <input required value={form.ref} onChange={e => setForm({ ...form, ref: e.target.value })} />
          <small className="muted">{t('people.refHint')}</small>
        </label>
        <label>
          <span>{t('people.kind')}</span>
          <select value={form.kind} onChange={e => setForm({ ...form, kind: e.target.value })}>
            {['staff', 'contractor', 'visitor', 'service'].map(k => (
              <option key={k} value={k}>{t(`people.kind.${k}`)}</option>
            ))}
          </select>
        </label>
        {/* Said plainly at the point of creation: a new person exists but reaches nothing. That is
            the fail-closed default, and an operator who does not know it will file a bug. */}
        <p className="muted small">{t('people.noAccessYet')}</p>
        <div className="form-actions">
          <button type="button" className="btn btn-quiet" onClick={onClose}>{t('common.cancel')}</button>
          <button type="submit" className="btn btn-primary" disabled={saving}>{t('common.save')}</button>
        </div>
      </form>
    </Modal>
  )
}

function Badges({ holder, canIssue, onClose, toast }) {
  const t = useT()
  const [creds, setCreds] = useState(null)
  const [issuing, setIssuing] = useState(false)

  const load = useCallback(async () => {
    try {
      const list = await api.listCredentials(holder.id)
      setCreds(Array.isArray(list) ? list : [])
    } catch {
      setCreds([])
    }
  }, [holder.id])

  useEffect(() => { load() }, [load])

  const revoke = async cred => {
    const status = window.prompt(`${t('badge.revokeTitle')}\nrevoked / lost / stolen / suspended`, 'revoked')
    if (!status) return
    try {
      await api.revokeCredential(holder.id, cred.id, status, '')
      load()
    } catch (e) {
      toast(e && e.message ? e.message : t('common.error'), 'error')
    }
  }

  return (
    <Modal title={`${t('badge.title')} — ${holder.name}`} onClose={onClose} wide>
      {creds === null ? <p>{t('common.loading')}</p> : null}
      {creds && creds.length === 0 ? <p className="muted">{t('badge.empty')}</p> : null}
      {creds && creds.length > 0 ? (
        <table className="mini-table">
          <thead>
            <tr>
              <th>{t('badge.format')}</th>
              <th>{t('badge.facility')}</th>
              <th>{t('badge.number')}</th>
              <th>{t('badge.status')}</th>
              <th />
            </tr>
          </thead>
          <tbody>
            {creds.map(c => (
              <tr key={c.id}>
                <td>{c.format || c.kind}</td>
                <td>{c.facilityCode}</td>
                <td><code>{c.cardNumber}</code></td>
                <td>
                  <span className={`pill pill-${c.status === 'active' ? 'ok' : 'warn'}`}>{c.status}</span>
                </td>
                <td>
                  {c.status === 'active' && canIssue ? (
                    <button type="button" className="btn btn-quiet" onClick={() => revoke(c)}>
                      {t('badge.revoke')}
                    </button>
                  ) : null}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      ) : null}

      {/* Revoking keeps the row rather than deleting it, so the access log stays explicable years
          later — and so a stolen card is still RECOGNISED if somebody tries it. */}
      <p className="muted small">{t('badge.lostHint')}</p>

      {issuing ? (
        <IssueBadge
          holder={holder}
          onClose={() => setIssuing(false)}
          onSaved={() => { setIssuing(false); load() }}
          toast={toast}
        />
      ) : canIssue ? (
        <button type="button" className="btn btn-primary" onClick={() => setIssuing(true)}>
          {t('badge.issue')}
        </button>
      ) : (
        <p className="muted small">{t('badge.noIssuePermission')}</p>
      )}
    </Modal>
  )
}

function IssueBadge({ holder, onClose, onSaved, toast }) {
  const t = useT()
  const [form, setForm] = useState({
    kind: 'card', format: 'wiegand26', facilityCode: 0, cardNumber: '', pin: '', duressPin: ''
  })
  const [saving, setSaving] = useState(false)

  const submit = async e => {
    e.preventDefault()
    setSaving(true)
    try {
      await api.issueCredential(holder.id, {
        ...form,
        facilityCode: Number(form.facilityCode) || 0
      })
      onSaved()
    } catch (err) {
      toast(err && err.message ? err.message : t('common.error'), 'error')
    } finally {
      setSaving(false)
    }
  }

  return (
    <form onSubmit={submit} className="form form-inset">
      <label>
        <span>{t('badge.format')}</span>
        <select value={form.format} onChange={e => setForm({ ...form, format: e.target.value })}>
          <option value="wiegand26">wiegand26</option>
          <option value="wiegand34">wiegand34</option>
          <option value="raw-uid">raw-uid</option>
        </select>
      </label>
      <div className="form-row">
        <label>
          <span>{t('badge.facility')}</span>
          <input
            type="number"
            value={form.facilityCode}
            onChange={e => setForm({ ...form, facilityCode: e.target.value })}
          />
        </label>
        <label>
          <span>{t('badge.number')}</span>
          <input required value={form.cardNumber} onChange={e => setForm({ ...form, cardNumber: e.target.value })} />
        </label>
      </div>
      {/* Both fields are needed, and the hint says why: the same card number exists under many
          facility codes, so matching on the number alone opens the door for the wrong person. */}
      <small className="muted">{t('badge.facilityHint')}</small>

      <div className="form-row">
        <label>
          <span>{t('badge.pin')}</span>
          <input type="password" value={form.pin} onChange={e => setForm({ ...form, pin: e.target.value })} />
        </label>
        <label>
          <span>{t('badge.duressPin')}</span>
          <input
            type="password"
            value={form.duressPin}
            onChange={e => setForm({ ...form, duressPin: e.target.value })}
          />
        </label>
      </div>
      <small className="muted">{t('badge.duressHint')}</small>

      <div className="form-actions">
        <button type="button" className="btn btn-quiet" onClick={onClose}>{t('common.cancel')}</button>
        <button type="submit" className="btn btn-primary" disabled={saving}>{t('common.save')}</button>
      </div>
    </form>
  )
}

export function Modal({ title, children, onClose, wide }) {
  const t = useT()
  return (
    <div className="modal-backdrop" onClick={onClose}>
      <div className={`modal${wide ? ' wide' : ''}`} onClick={e => e.stopPropagation()}>
        <header>
          <h2>{title}</h2>
          <button type="button" className="btn btn-quiet" onClick={onClose} aria-label={t('common.close')}>
            <Ico n="x" sz={16} />
          </button>
        </header>
        <div className="modal-body">{children}</div>
      </div>
    </div>
  )
}
