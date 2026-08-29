import { useCallback, useEffect, useState } from 'react'
import { useT, Ico } from '@shared'
import * as api from '../lib/access'

// The doors screen is the one an operator leaves open all day: every door, its state, and a button
// that opens it. Everything else in this app exists to make this screen trustworthy.
export default function Doors({ caps = {}, toast }) {
  const t = useT()
  const [doors, setDoors] = useState(null)
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(0)
  const [lockdown, setLockdownState] = useState(false)

  // The two loads settle INDEPENDENTLY. They were one Promise.all, so a refusal on either rejected
  // both — and the lockdown state was admin-only, which meant this screen rendered "you do not have
  // permission for this action" and no doors at all for every viewer and every operator. The door
  // list is what this screen is for; nothing else failing may be allowed to take it down with it.
  const load = useCallback(async () => {
    try {
      const list = await api.listDoors()
      setDoors(Array.isArray(list) ? list : [])
      setError('')
    } catch (e) {
      setError(e && e.message ? e.message : t('common.error'))
    }
    try {
      const ld = await api.getLockdown()
      setLockdownState(!!(ld && ld.lockdown))
    } catch { /* the doors still render; the lockdown pill simply says nothing */ }
  }, [t])

  useEffect(() => { load() }, [load])

  // Poll for lockdown state. A door screen that shows stale lockdown is worse than one that shows
  // nothing: an operator would believe the site is open when it is sealed.
  useEffect(() => {
    const id = setInterval(() => {
      api.getLockdown().then(ld => setLockdownState(!!(ld && ld.lockdown))).catch(() => {})
    }, 5000)
    return () => clearInterval(id)
  }, [])

  const unlock = useCallback(async door => {
    setBusy(door.id)
    try {
      await api.unlockDoor(door.id)
      toast(t('doors.unlocked'), 'ok')
    } catch (e) {
      toast(e && e.message ? e.message : t('doors.unlockFailed'), 'error')
    } finally {
      setBusy(0)
    }
  }, [t, toast])

  const toggleLockdown = useCallback(async () => {
    const next = !lockdown
    if (next && !window.confirm(t('lockdown.confirmEngage'))) return
    try {
      const res = await api.setLockdown(next)
      setLockdownState(!!(res && res.lockdown))
    } catch (e) {
      toast(e && e.message ? e.message : t('common.error'), 'error')
    }
  }, [lockdown, t, toast])

  if (doors === null && !error) return <div className="screen"><p>{t('common.loading')}</p></div>

  return (
    <div className="screen">
      <header className="screen-head">
        <div>
          <h1>{t('doors.title')}</h1>
          <p className="muted">{t('doors.subtitle')}</p>
        </div>
        {/* The lockdown control is offered only to somebody the server would let use it. Anyone
            else still has to SEE that the site is sealed — a door screen that hides lockdown from
            an operator is a screen that cannot explain why nothing is opening. */}
        {caps.lockdown ? (
          <LockdownControl active={lockdown} onToggle={toggleLockdown} />
        ) : lockdown ? (
          <span className="pill pill-danger">{t('lockdown.active')}</span>
        ) : null}
      </header>

      {error ? (
        <div className="notice notice-error">
          <p>{error}</p>
          <button type="button" onClick={load}>{t('common.retry')}</button>
        </div>
      ) : null}

      {doors && doors.length === 0 ? <p className="muted">{t('doors.empty')}</p> : null}

      <div className="card-grid">
        {(doors || []).map(door => (
          <DoorCard
            key={door.id}
            door={door}
            busy={busy === door.id}
            lockdown={lockdown}
            canUnlock={!!caps.unlockDoor}
            onUnlock={() => unlock(door)}
          />
        ))}
      </div>
    </div>
  )
}

function LockdownControl({ active, onToggle }) {
  const t = useT()
  return (
    <div className={`lockdown-box${active ? ' active' : ''}`}>
      <div>
        <strong>{active ? t('lockdown.active') : t('lockdown.inactive')}</strong>
        <p className="muted small">{t('lockdown.explain')}</p>
        {/* Stated on the control itself, not buried in a manual. Somebody reaching for this in an
            emergency must not believe it can trap people inside. */}
        <p className="muted small">{t('lockdown.egressNote')}</p>
      </div>
      <button type="button" className={active ? 'btn btn-ok' : 'btn btn-danger'} onClick={onToggle}>
        {active ? t('lockdown.release') : t('lockdown.engage')}
      </button>
    </div>
  )
}

function DoorCard({ door, busy, lockdown, canUnlock, onUnlock }) {
  const t = useT()
  const outOfService = !door.enabled
  const blocked = outOfService || lockdown

  return (
    <article className={`door-card${blocked ? ' blocked' : ''}`}>
      <header>
        <span className="door-ico"><Ico n="door" sz={20} /></span>
        <div>
          <h2>{door.name}</h2>
          <span className={`pill pill-${door.class}`}>{t(`doors.class.${door.class}`)}</span>
        </div>
      </header>

      <dl className="door-meta">
        <div>
          <dt>{t('doors.state')}</dt>
          <dd>{outOfService ? t('doors.disabled') : t('doors.enabled')}</dd>
        </div>
        <div>
          <dt>{t('doors.strikeTime')}</dt>
          <dd>{t('doors.seconds', { n: door.unlockSeconds || 5 })}</dd>
        </div>
      </dl>

      {door.requireSecureChannel ? (
        <p className="muted small"><Ico n="shield" sz={13} /> {t('doors.encrypted')}</p>
      ) : null}

      {/* No Unlock button at all for a role the server will refuse. A disabled one would be a
          different lie — it reads as "not right now", when the truth is "not by you, ever" — and
          an enabled one produces a bare error at the moment somebody is standing at the door.
          Which roles may open a door is services/rbac.go's decision, asked, not guessed. */}
      {canUnlock ? (
        <button type="button" className="btn btn-primary" disabled={busy || blocked} onClick={onUnlock}>
          {busy ? t('doors.unlocking') : t('doors.unlock')}
        </button>
      ) : (
        <p className="muted small">{t('doors.noUnlockPermission')}</p>
      )}
    </article>
  )
}
