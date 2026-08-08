import { useCallback, useEffect, useState } from 'react';
import { useT, Ico } from '@shared';
import * as api from './lib/access';

// mypintusan's doors screen, ported into the control-plane embed: every door, its state, and a
// button that opens it — driven over the node's command tunnel. Unlike the node's own page there
// is no `user.isAdmin` gate on the lockdown control: the embed does not guess the operator's
// per-node grant, it asks — the node evaluates its own permission matrix and a refusal comes
// back in plain words (the nodeiot philosophy).
export function DoorsPage({ onToast }) {
  const t = useT();
  const [doors, setDoors] = useState(null);
  const [error, setError] = useState('');
  const [busy, setBusy] = useState(0);
  const [lockdown, setLockdownState] = useState(false);

  const load = useCallback(async () => {
    try {
      const [list, ld] = await Promise.all([api.listDoors(), api.getLockdown()]);
      setDoors(Array.isArray(list) ? list : []);
      setLockdownState(!!(ld && ld.lockdown));
      setError('');
    } catch (e) {
      setError(e && e.message ? e.message : t('ndoor.error'));
    }
  }, [t]);

  useEffect(() => { load(); }, [load]);

  // Poll for lockdown state. A door screen that shows stale lockdown is worse than one that
  // shows nothing: an operator would believe the site is open when it is sealed.
  useEffect(() => {
    const id = setInterval(() => {
      api.getLockdown().then((ld) => setLockdownState(!!(ld && ld.lockdown))).catch(() => {});
    }, 5000);
    return () => clearInterval(id);
  }, []);

  const unlock = useCallback(async (door) => {
    setBusy(door.id);
    try {
      await api.unlockDoor(door.id);
      onToast(t('ndoor.doors.unlocked'), 'success');
    } catch (e) {
      onToast(e && e.message ? e.message : t('ndoor.doors.unlockFailed'), 'error');
    } finally {
      setBusy(0);
    }
  }, [t, onToast]);

  const toggleLockdown = useCallback(async () => {
    const next = !lockdown;
    if (next && !window.confirm(t('ndoor.lockdown.confirmEngage'))) return;
    try {
      const res = await api.setLockdown(next);
      setLockdownState(!!(res && res.lockdown));
    } catch (e) {
      onToast(e && e.message ? e.message : t('ndoor.error'), 'error');
    }
  }, [lockdown, t, onToast]);

  if (doors === null && !error) return <div className="screen"><p>{t('ndoor.loading')}</p></div>;

  return (
    <div className="screen">
      <header className="screen-head">
        <div>
          <h1>{t('ndoor.doors.title')}</h1>
          <p className="muted">{t('ndoor.doors.subtitle')}</p>
        </div>
        <LockdownControl active={lockdown} onToggle={toggleLockdown} />
      </header>

      {error ? (
        <div className="notice notice-error">
          <p>{error}</p>
          <button type="button" onClick={load}>{t('ndoor.retry')}</button>
        </div>
      ) : null}

      {doors && doors.length === 0 ? <p className="muted">{t('ndoor.doors.empty')}</p> : null}

      <div className="card-grid">
        {(doors || []).map((door) => (
          <DoorCard
            key={door.id}
            door={door}
            busy={busy === door.id}
            lockdown={lockdown}
            onUnlock={() => unlock(door)}
          />
        ))}
      </div>
    </div>
  );
}

function LockdownControl({ active, onToggle }) {
  const t = useT();
  return (
    <div className={`lockdown-box${active ? ' active' : ''}`}>
      <div>
        <strong>{active ? t('ndoor.lockdown.active') : t('ndoor.lockdown.inactive')}</strong>
        <p className="muted small">{t('ndoor.lockdown.explain')}</p>
        {/* Stated on the control itself, not buried in a manual. Somebody reaching for this in an
            emergency must not believe it can trap people inside. */}
        <p className="muted small">{t('ndoor.lockdown.egressNote')}</p>
      </div>
      <button type="button" className={active ? 'btn btn-ok' : 'btn btn-danger'} onClick={onToggle}>
        {active ? t('ndoor.lockdown.release') : t('ndoor.lockdown.engage')}
      </button>
    </div>
  );
}

function DoorCard({ door, busy, lockdown, onUnlock }) {
  const t = useT();
  const outOfService = !door.enabled;
  const blocked = outOfService || lockdown;

  return (
    <article className={`door-card${blocked ? ' blocked' : ''}`}>
      <header>
        <span className="door-ico"><Ico n="door" sz={20} /></span>
        <div>
          <h2>{door.name}</h2>
          <span className={`pill pill-${door.class}`}>{t(`ndoor.doors.class.${door.class}`)}</span>
        </div>
      </header>

      <dl className="door-meta">
        <div>
          <dt>{t('ndoor.doors.state')}</dt>
          <dd>{outOfService ? t('ndoor.doors.disabled') : t('ndoor.doors.enabled')}</dd>
        </div>
        <div>
          <dt>{t('ndoor.doors.strikeTime')}</dt>
          <dd>{t('ndoor.doors.seconds', { n: door.unlockSeconds || 5 })}</dd>
        </div>
      </dl>

      {door.requireSecureChannel ? (
        <p className="muted small"><Ico n="shield" sz={13} /> {t('ndoor.doors.encrypted')}</p>
      ) : null}

      <button type="button" className="btn btn-primary" disabled={busy || blocked} onClick={onUnlock}>
        {busy ? t('ndoor.doors.unlocking') : t('ndoor.doors.unlock')}
      </button>
    </article>
  );
}
