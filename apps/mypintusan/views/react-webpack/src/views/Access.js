import { useCallback, useEffect, useState } from 'react'
import { useT, DataTable, Ico } from '@shared'
import * as api from '../lib/access'
import { Modal } from './People'

// The access rules screen: WHO (groups) gets through WHICH doors (grants) WHEN (schedules).
//
// Admin-only, and the nav hides it from everyone else. The asymmetry with the People screen is
// the whole design: issuing a badge is a receptionist's daily, reversible act; a rule on this
// screen changes who may enter every door in a group, at every hour, until somebody notices.
// Every change made here is therefore also published to the notification feed, named after the
// administrator who made it.
export default function Access({ caps = {}, toast }) {
  const t = useT()
  const [groups, setGroups] = useState(null)
  const [schedules, setSchedules] = useState(null)
  const [grants, setGrants] = useState(null)
  const [holidays, setHolidays] = useState(null)
  const [doors, setDoors] = useState([])
  const [error, setError] = useState('')
  // editRules is the catalog's admin-only grant on grants, groups, schedules and holidays. Reading
  // them is separately granted to an operator, which is why this screen is not simply hidden.
  const canEdit = !!caps.editRules

  const load = useCallback(async () => {
    try {
      const [g, s, gr, h, d] = await Promise.all([
        api.listGroups(), api.listSchedules(), api.listGrants(), api.listHolidays(), api.listDoors()
      ])
      setGroups(g && g.items ? g.items : [])
      setSchedules(s && s.items ? s.items : [])
      setGrants(gr && gr.items ? gr.items : [])
      setHolidays(h && h.items ? h.items : [])
      setDoors(Array.isArray(d) ? d : [])
      setError('')
    } catch (e) {
      setError(e && e.message ? e.message : t('common.error'))
    }
  }, [t])

  useEffect(() => { load() }, [load])

  if (error) {
    return (
      <div className="screen">
        <div className="notice notice-error"><p>{error}</p><button type="button" onClick={load}>{t('common.retry')}</button></div>
      </div>
    )
  }
  if (groups === null || schedules === null || grants === null || holidays === null) {
    return <div className="screen"><p>{t('common.loading')}</p></div>
  }

  return (
    <div className="screen">
      <header className="screen-head">
        <div>
          <h1>{t('access.title')}</h1>
          <p className="muted">{t('access.subtitle')}</p>
        </div>
      </header>

      {/* An operator is deliberately granted READ on groups, grants and schedules — "the rules
          they have to work within" — and nothing else here. So the sections render for them and
          every control that would change a rule is withheld: a screen that offers a Revoke button
          the server refuses teaches people that this screen cannot be trusted, on the one screen
          where a mistake changes who may enter a building. `canEdit` comes from the server's own
          matrix, not from a second copy of the policy living in the browser. */}
      {!canEdit ? <p className="muted small">{t('access.readOnly')}</p> : null}

      <GrantsSection canEdit={canEdit} grants={grants} groups={groups} schedules={schedules} doors={doors} onChanged={load} toast={toast} />
      <GroupsSection canEdit={canEdit} groups={groups} onChanged={load} toast={toast} />
      <SchedulesSection canEdit={canEdit} schedules={schedules} onChanged={load} toast={toast} />
      <HolidaysSection canEdit={canEdit} holidays={holidays} onChanged={load} toast={toast} />
    </div>
  )
}

// --- grants -------------------------------------------------------------------------------------

function GrantsSection({ canEdit, grants, groups, schedules, doors, onChanged, toast }) {
  const t = useT()
  const [adding, setAdding] = useState(false)
  const canAdd = groups.length > 0 && schedules.length > 0 && doors.length > 0

  const revoke = async grant => {
    if (!window.confirm(t('access.grants.revokeConfirm'))) return
    try {
      await api.deleteGrant(grant.id)
      onChanged()
    } catch (e) {
      toast(e && e.message ? e.message : t('common.error'), 'error')
    }
  }

  const columns = [
    { key: 'groupName', label: t('access.grants.group'), render: (_v, r) => r.groupName || r.groupId },
    { key: 'doorName', label: t('access.grants.door'), render: (_v, r) => r.doorName || r.doorId },
    { key: 'scheduleName', label: t('access.grants.schedule'), render: (_v, r) => r.scheduleName || r.scheduleId },
    {
      key: 'actions', label: '',
      render: (_v, r) => (
        canEdit ? <button type="button" className="btn btn-quiet" onClick={() => revoke(r)}>{t('access.grants.revoke')}</button> : null
      )
    }
  ]

  return (
    <section className="access-section">
      <div className="access-section-head">
        <h2 className="section-head">{t('access.grants.title')}</h2>
        {canEdit ? (
          <button type="button" className="btn btn-primary" disabled={!canAdd} onClick={() => setAdding(true)}>
            <Ico n="plus" sz={14} /> {t('access.grants.add')}
          </button>
        ) : null}
      </div>
      <p className="muted small">{t('access.grants.hint')}</p>
      {!canAdd ? <p className="muted small">{t('access.grants.needAll')}</p> : null}
      {grants.length === 0 ? <p className="muted">{t('access.grants.empty')}</p> : <DataTable columns={columns} rows={grants} />}

      {adding ? (
        <AddGrant
          groups={groups} schedules={schedules} doors={doors}
          onClose={() => setAdding(false)}
          onSaved={() => { setAdding(false); onChanged() }}
          toast={toast}
        />
      ) : null}
    </section>
  )
}

function AddGrant({ groups, schedules, doors, onClose, onSaved, toast }) {
  const t = useT()
  const [form, setForm] = useState({
    groupId: groups[0] ? groups[0].id : 0,
    doorId: doors[0] ? doors[0].id : 0,
    scheduleId: schedules[0] ? schedules[0].id : 0
  })
  const [saving, setSaving] = useState(false)

  const submit = async e => {
    e.preventDefault()
    setSaving(true)
    try {
      await api.createGrant({
        groupId: Number(form.groupId), doorId: Number(form.doorId), scheduleId: Number(form.scheduleId)
      })
      onSaved()
    } catch (err) {
      toast(err && err.message ? err.message : t('common.error'), 'error')
    } finally {
      setSaving(false)
    }
  }

  return (
    <Modal title={t('access.grants.addTitle')} onClose={onClose}>
      <form onSubmit={submit} className="form">
        <label>
          <span>{t('access.grants.group')}</span>
          <select value={form.groupId} onChange={e => setForm({ ...form, groupId: e.target.value })}>
            {groups.map(g => <option key={g.id} value={g.id}>{g.name}</option>)}
          </select>
        </label>
        <label>
          <span>{t('access.grants.door')}</span>
          <select value={form.doorId} onChange={e => setForm({ ...form, doorId: e.target.value })}>
            {doors.map(d => <option key={d.id} value={d.id}>{d.name}</option>)}
          </select>
        </label>
        <label>
          <span>{t('access.grants.schedule')}</span>
          <select value={form.scheduleId} onChange={e => setForm({ ...form, scheduleId: e.target.value })}>
            {schedules.map(s => <option key={s.id} value={s.id}>{s.name}{s.always ? ` — ${t('access.schedules.always')}` : ''}</option>)}
          </select>
        </label>
        {/* Said at the point of creation: this takes effect at the next badge, everywhere in the
            group. There is no save-then-apply step to catch a mistake in. */}
        <p className="muted small">{t('access.grants.immediateNote')}</p>
        <div className="form-actions">
          <button type="button" className="btn btn-quiet" onClick={onClose}>{t('common.cancel')}</button>
          <button type="submit" className="btn btn-primary" disabled={saving}>{t('common.save')}</button>
        </div>
      </form>
    </Modal>
  )
}

// --- groups -------------------------------------------------------------------------------------

function GroupsSection({ canEdit, groups, onChanged, toast }) {
  const t = useT()
  const [adding, setAdding] = useState(false)
  const [name, setName] = useState('')
  const [membersOf, setMembersOf] = useState(null)

  const create = async e => {
    e.preventDefault()
    try {
      await api.createGroup({ name })
      setName('')
      setAdding(false)
      onChanged()
    } catch (err) {
      toast(err && err.message ? err.message : t('common.error'), 'error')
    }
  }

  const remove = async group => {
    if (!window.confirm(t('access.groups.deleteConfirm'))) return
    try {
      await api.deleteGroup(group.id)
      onChanged()
    } catch (e) {
      // "grants still reference it" comes back in plain words — show it verbatim.
      toast(e && e.message ? e.message : t('common.error'), 'error')
    }
  }

  const columns = [
    { key: 'name', label: t('access.groups.name') },
    { key: 'description', label: t('access.groups.description'), render: (_v, r) => r.description || '—' },
    {
      key: 'actions', label: '',
      render: (_v, r) => (
        <span className="row-actions">
          <button type="button" className="btn btn-quiet" onClick={() => setMembersOf(r)}>{t('access.groups.members')}</button>
          {canEdit ? <button type="button" className="btn btn-quiet" onClick={() => remove(r)}>{t('common.delete')}</button> : null}
        </span>
      )
    }
  ]

  return (
    <section className="access-section">
      <div className="access-section-head">
        <h2 className="section-head">{t('access.groups.title')}</h2>
        {canEdit ? (
          <button type="button" className="btn btn-quiet" onClick={() => setAdding(a => !a)}>
            <Ico n="plus" sz={14} /> {t('access.groups.add')}
          </button>
        ) : null}
      </div>
      {adding ? (
        <form onSubmit={create} className="form form-row-inline">
          <input required placeholder={t('access.groups.namePlaceholder')} value={name} onChange={e => setName(e.target.value)} />
          <button type="submit" className="btn btn-primary">{t('common.save')}</button>
        </form>
      ) : null}
      {groups.length === 0 ? <p className="muted">{t('access.groups.empty')}</p> : <DataTable columns={columns} rows={groups} />}

      {membersOf ? <MembersModal canEdit={canEdit} group={membersOf} onClose={() => setMembersOf(null)} toast={toast} /> : null}
    </section>
  )
}

function MembersModal({ canEdit, group, onClose, toast }) {
  const t = useT()
  const [members, setMembers] = useState(null)
  const [holders, setHolders] = useState([])
  const [holderId, setHolderId] = useState('')

  const load = useCallback(async () => {
    try {
      const [m, h] = await Promise.all([api.listGroupMembers(group.id), api.listHolders()])
      setMembers(m && m.items ? m.items : [])
      setHolders(Array.isArray(h) ? h : [])
    } catch {
      setMembers([])
    }
  }, [group.id])

  useEffect(() => { load() }, [load])

  const add = async e => {
    e.preventDefault()
    if (!holderId) return
    try {
      await api.addGroupMember(group.id, Number(holderId))
      setHolderId('')
      load()
    } catch (err) {
      toast(err && err.message ? err.message : t('common.error'), 'error')
    }
  }

  const remove = async member => {
    try {
      await api.removeGroupMember(group.id, member.id)
      load()
    } catch (e) {
      toast(e && e.message ? e.message : t('common.error'), 'error')
    }
  }

  const inGroup = new Set((members || []).map(m => m.holderId))
  const addable = holders.filter(h => !inGroup.has(h.id))

  return (
    <Modal title={`${t('access.groups.membersTitle')} — ${group.name}`} onClose={onClose}>
      {members === null ? <p>{t('common.loading')}</p> : null}
      {members && members.length === 0 ? <p className="muted">{t('access.groups.noMembers')}</p> : null}
      {members && members.length > 0 ? (
        <table className="mini-table">
          <tbody>
            {members.map(m => (
              <tr key={m.id}>
                <td>{m.holderName || m.holderId}</td>
                <td>
                  {canEdit ? <button type="button" className="btn btn-quiet" onClick={() => remove(m)}>{t('common.remove')}</button> : null}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      ) : null}

      {canEdit ? (
      <form onSubmit={add} className="form form-inset">
        <label>
          <span>{t('access.groups.addMember')}</span>
          <select value={holderId} onChange={e => setHolderId(e.target.value)}>
            <option value="">{t('access.groups.pickPerson')}</option>
            {addable.map(h => <option key={h.id} value={h.id}>{h.name}</option>)}
          </select>
        </label>
        <div className="form-actions">
          <button type="submit" className="btn btn-primary" disabled={!holderId}>{t('access.groups.addMemberBtn')}</button>
        </div>
      </form>
      ) : null}
    </Modal>
  )
}

// --- schedules ----------------------------------------------------------------------------------

const WEEKDAY_KEYS = ['access.day.sun', 'access.day.mon', 'access.day.tue', 'access.day.wed', 'access.day.thu', 'access.day.fri', 'access.day.sat']

const toTime = min => `${String(Math.floor(min / 60)).padStart(2, '0')}:${String(min % 60).padStart(2, '0')}`
const toMin = hhmm => {
  const [h, m] = String(hhmm || '0:0').split(':').map(Number)
  return (h || 0) * 60 + (m || 0)
}

function SchedulesSection({ canEdit, schedules, onChanged, toast }) {
  const t = useT()
  const [adding, setAdding] = useState(false)

  const remove = async sched => {
    if (!window.confirm(t('access.schedules.deleteConfirm'))) return
    try {
      await api.deleteSchedule(sched.id)
      onChanged()
    } catch (e) {
      toast(e && e.message ? e.message : t('common.error'), 'error')
    }
  }

  const columns = [
    { key: 'name', label: t('access.schedules.name') },
    {
      key: 'windows', label: t('access.schedules.when'),
      render: (_v, r) => r.always
        ? <span className="pill pill-ok">{t('access.schedules.always')}</span>
        : (r.windows || []).map(w => (
          <span key={w.id} className="pill sm">
            {t(WEEKDAY_KEYS[w.weekday] || 'common.none')} {toTime(w.startMin)}–{toTime(w.endMin)}
            {/* A window ending before it starts wraps past midnight — the night shift. */}
            {w.endMin < w.startMin ? ` ${t('access.schedules.overnight')}` : ''}
          </span>
        ))
    },
    {
      key: 'actions', label: '',
      render: (_v, r) => (
        canEdit ? <button type="button" className="btn btn-quiet" onClick={() => remove(r)}>{t('common.delete')}</button> : null
      )
    }
  ]

  return (
    <section className="access-section">
      <div className="access-section-head">
        <h2 className="section-head">{t('access.schedules.title')}</h2>
        {canEdit ? (
          <button type="button" className="btn btn-quiet" onClick={() => setAdding(true)}>
            <Ico n="plus" sz={14} /> {t('access.schedules.add')}
          </button>
        ) : null}
      </div>
      {schedules.length === 0 ? <p className="muted">{t('access.schedules.empty')}</p> : <DataTable columns={columns} rows={schedules} />}

      {adding ? (
        <AddSchedule
          onClose={() => setAdding(false)}
          onSaved={() => { setAdding(false); onChanged() }}
          toast={toast}
        />
      ) : null}
    </section>
  )
}

function AddSchedule({ onClose, onSaved, toast }) {
  const t = useT()
  const [form, setForm] = useState({ name: '', always: true })
  const [windows, setWindows] = useState([])
  const [saving, setSaving] = useState(false)

  const setWindow = (i, patch) => setWindows(ws => ws.map((w, j) => j === i ? { ...w, ...patch } : w))

  const submit = async e => {
    e.preventDefault()
    setSaving(true)
    try {
      await api.createSchedule({
        name: form.name,
        always: form.always,
        windows: form.always ? [] : windows.map(w => ({
          weekday: Number(w.weekday), startMin: toMin(w.start), endMin: toMin(w.end)
        }))
      })
      onSaved()
    } catch (err) {
      toast(err && err.message ? err.message : t('common.error'), 'error')
    } finally {
      setSaving(false)
    }
  }

  return (
    <Modal title={t('access.schedules.addTitle')} onClose={onClose} wide>
      <form onSubmit={submit} className="form">
        <label>
          <span>{t('access.schedules.name')}</span>
          <input required value={form.name} onChange={e => setForm({ ...form, name: e.target.value })} />
        </label>
        <label className="check">
          <input type="checkbox" checked={form.always} onChange={e => setForm({ ...form, always: e.target.checked })} />
          <span>{t('access.schedules.alwaysLabel')}</span>
        </label>

        {!form.always ? (
          <>
            {windows.map((w, i) => (
              <div className="form-row" key={i}>
                <label>
                  <span>{t('access.schedules.day')}</span>
                  <select value={w.weekday} onChange={e => setWindow(i, { weekday: e.target.value })}>
                    {WEEKDAY_KEYS.map((k, d) => <option key={k} value={d}>{t(k)}</option>)}
                  </select>
                </label>
                <label>
                  <span>{t('access.schedules.from')}</span>
                  <input type="time" value={w.start} onChange={e => setWindow(i, { start: e.target.value })} />
                </label>
                <label>
                  <span>{t('access.schedules.to')}</span>
                  <input type="time" value={w.end} onChange={e => setWindow(i, { end: e.target.value })} />
                </label>
                <button type="button" className="btn btn-quiet" onClick={() => setWindows(ws => ws.filter((_x, j) => j !== i))}>
                  <Ico n="x" sz={14} />
                </button>
              </div>
            ))}
            {/* Overnight shifts are entered exactly as spoken: 22:00 to 06:00. The end being
                before the start IS the night shift, not a mistake. */}
            <p className="muted small">{t('access.schedules.overnightHint')}</p>
            <button
              type="button" className="btn btn-quiet"
              onClick={() => setWindows(ws => [...ws, { weekday: 1, start: '09:00', end: '17:00' }])}
            >
              <Ico n="plus" sz={14} /> {t('access.schedules.addWindow')}
            </button>
          </>
        ) : null}

        <div className="form-actions">
          <button type="button" className="btn btn-quiet" onClick={onClose}>{t('common.cancel')}</button>
          <button type="submit" className="btn btn-primary" disabled={saving}>{t('common.save')}</button>
        </div>
      </form>
    </Modal>
  )
}

// --- holidays -----------------------------------------------------------------------------------

// The holiday calendar had an API, a decision gate, a denial reason translated into four languages
// and an API client in lib/access.js — and no screen. Nothing in this app ever called listHolidays,
// createHoliday or deleteHoliday, so on every install the calendar was empty and the branch of
// `scheduleAllows` that reads it could not run.
//
// It belongs on THIS screen rather than on Settings because it is a rule about who may enter, not a
// property of the appliance: one row here closes every door on the site for a day, INCLUDING doors
// on a 24/7 schedule. That is why it is admin-only, audited like a grant, and why removing a row is
// confirmed — deleting a holiday REOPENS a building on a day it was meant to be shut.
const BEHAVIOURS = ['deny', 'follow-sunday', 'ignore']
const BEHAVIOUR_KEYS = {
  deny: 'access.holidays.deny',
  'follow-sunday': 'access.holidays.followSunday',
  ignore: 'access.holidays.ignore'
}

function HolidaysSection({ canEdit, holidays, onChanged, toast }) {
  const t = useT()
  const [adding, setAdding] = useState(false)

  const remove = async h => {
    if (!window.confirm(t('access.holidays.deleteConfirm'))) return
    try {
      await api.deleteHoliday(h.id)
      onChanged()
    } catch (e) {
      toast(e && e.message ? e.message : t('common.error'), 'error')
    }
  }

  // Soonest first. A calendar is read forwards: the row anyone is looking for is the next one.
  const rows = [...holidays].sort((a, b) => String(a.date).localeCompare(String(b.date)))

  const columns = [
    { key: 'date', label: t('access.holidays.date') },
    { key: 'name', label: t('access.holidays.name') },
    {
      key: 'behaviour',
      label: t('access.holidays.behaviour'),
      render: (_v, r) => (
        <span className={r.behaviour === 'deny' ? 'pill sm pill-warn' : 'pill sm'}>
          {t(BEHAVIOUR_KEYS[r.behaviour] || 'common.none')}
        </span>
      )
    },
    {
      key: 'actions',
      label: '',
      render: (_v, r) => (
        canEdit ? <button type="button" className="btn btn-quiet" onClick={() => remove(r)}>{t('common.delete')}</button> : null
      )
    }
  ]

  return (
    <section className="access-section">
      <div className="access-section-head">
        <h2 className="section-head">{t('access.holidays.title')}</h2>
        {canEdit ? (
          <button type="button" className="btn btn-quiet" onClick={() => setAdding(true)}>
            <Ico n="plus" sz={14} /> {t('access.holidays.add')}
          </button>
        ) : null}
      </div>
      <p className="muted small">{t('access.holidays.lead')}</p>
      {rows.length === 0
        ? <p className="muted">{t('access.holidays.empty')}</p>
        : <DataTable columns={columns} rows={rows} />}

      {adding ? (
        <AddHoliday
          onClose={() => setAdding(false)}
          onSaved={() => { setAdding(false); onChanged() }}
          toast={toast}
        />
      ) : null}
    </section>
  )
}

function AddHoliday({ onClose, onSaved, toast }) {
  const t = useT()
  // `deny` is the default because it is what a public holiday means, and because it is the
  // fail-closed answer: a day entered by mistake shuts a door, which somebody reports within
  // minutes, rather than opening one nobody is watching.
  const [form, setForm] = useState({ name: '', date: '', behaviour: 'deny' })
  const [saving, setSaving] = useState(false)

  const submit = async e => {
    e.preventDefault()
    setSaving(true)
    try {
      await api.createHoliday({ name: form.name, date: form.date, behaviour: form.behaviour })
      onSaved()
    } catch (err) {
      toast(err && err.message ? err.message : t('common.error'), 'error')
    } finally {
      setSaving(false)
    }
  }

  return (
    <Modal title={t('access.holidays.addTitle')} onClose={onClose}>
      <form onSubmit={submit} className="form">
        <label>
          <span>{t('access.holidays.name')}</span>
          <input required value={form.name} onChange={e => setForm({ ...form, name: e.target.value })} />
        </label>
        <label>
          <span>{t('access.holidays.date')}</span>
          {/* A date, not a timestamp: a public holiday is a calendar day in the SITE's timezone and
              must not shift with the server's UTC offset. */}
          <input type="date" required value={form.date} onChange={e => setForm({ ...form, date: e.target.value })} />
          <small className="muted">{t('access.holidays.dateHint')}</small>
        </label>
        <label>
          <span>{t('access.holidays.behaviour')}</span>
          <select value={form.behaviour} onChange={e => setForm({ ...form, behaviour: e.target.value })}>
            {BEHAVIOURS.map(b => <option key={b} value={b}>{t(BEHAVIOUR_KEYS[b])}</option>)}
          </select>
          <small className="muted">{t('access.holidays.behaviourHint')}</small>
        </label>

        <div className="form-actions">
          <button type="button" className="btn btn-quiet" onClick={onClose}>{t('common.cancel')}</button>
          <button type="submit" className="btn btn-primary" disabled={saving}>{t('common.save')}</button>
        </div>
      </form>
    </Modal>
  )
}
