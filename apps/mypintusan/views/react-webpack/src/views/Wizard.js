import { useCallback, useState } from 'react'
import { useT, Ico, BrandLogo } from '@shared'
import * as api from '../lib/access'

// The first-run wizard.
//
// This is the screen that decides whether a facilities manager can install this product without
// calling anybody. It asks for the four things nothing works without — where the site is in time,
// which cable the readers are on, one door, one person with a badge — and it says out loud what a
// door system quietly assumes but a first-time installer cannot know: readers arrive set to
// address 0, so the second one has to be changed before it will answer.
//
// Every step is skippable except the time zone, because a half-configured door system that boots
// is more useful than a wizard somebody abandons.
const STEPS = ['welcome', 'site', 'cable', 'door', 'person', 'done']

export default function Wizard({ onFinished }) {
  const t = useT()
  const [step, setStep] = useState(0)
  const [busy, setBusy] = useState(false)
  // The wizard renders INSTEAD of the app shell, so the shell's ToastStack is not mounted. Without
  // an inline banner every failure here is completely invisible: the button appears to do nothing
  // and the user has no idea why. Errors belong on the step that produced them.
  const [error, setError] = useState('')

  const [site, setSite] = useState({ timezone: '' })
  const [cable, setCable] = useState({ port: '', address: 1, label: '' })
  const [door, setDoor] = useState({ name: '', class: 'interior', unlockSeconds: 5 })
  const [person, setPerson] = useState({ name: '', ref: '', facilityCode: 0, cardNumber: '' })

  const name = STEPS[step]
  const next = () => setStep(s => Math.min(s + 1, STEPS.length - 1))
  const back = () => setStep(s => Math.max(s - 1, 0))

  // Load the current timezone once, so the field starts from what the controller already believes
  // rather than blank — the value in config.json is usually right and re-typing it invites a typo.
  const loadSite = useCallback(async () => {
    try {
      const cfg = await api.getSettings()
      setSite({ timezone: cfg.timezone || '' })
      if (Array.isArray(cfg.buses) && cfg.buses[0]) {
        setCable(c => ({ ...c, port: cfg.buses[0].port || '' }))
      }
    } catch { /* a fresh install may have nothing yet */ }
  }, [])

  const saveSite = async () => {
    setBusy(true)
    setError('')
    try {
      const cfg = await api.getSettings()
      await api.saveSettings({ ...cfg, timezone: site.timezone })
      next()
    } catch (e) {
      setError(e && e.message ? e.message : t('common.error'))
    } finally {
      setBusy(false)
    }
  }

  const saveCable = async () => {
    setBusy(true)
    setError('')
    try {
      const cfg = await api.getSettings()
      const buses = Array.isArray(cfg.buses) ? [...cfg.buses] : []
      const idx = buses.findIndex(b => b.port === cable.port)
      const reader = { address: Number(cable.address) || 0, label: cable.label, requireSecureChannel: false }
      if (idx >= 0) {
        const existing = buses[idx].readers || []
        buses[idx] = {
          ...buses[idx],
          readers: existing.some(r => r.address === reader.address) ? existing : [...existing, reader]
        }
      } else {
        buses.push({ port: cable.port, slotMillis: 50, replyTimeoutMillis: 200, readers: [reader] })
      }
      await api.saveSettings({ ...cfg, buses })
      next()
    } catch (e) {
      // The server's validation text names the actual problem ("two readers at address 1"), so it
      // is shown as-is: the person reading it is the one who has to change a DIP switch.
      setError(e && e.message ? e.message : t('common.error'))
    } finally {
      setBusy(false)
    }
  }

  const saveDoor = async () => {
    setBusy(true)
    setError('')
    try {
      await api.createDoor({
        name: door.name,
        class: door.class,
        unlockSeconds: Number(door.unlockSeconds) || 5,
        busPort: cable.port,
        osdpAddress: Number(cable.address) || 0,
        readerName: cable.label || door.name
      })
      next()
    } catch (e) {
      setError(e && e.message ? e.message : t('common.error'))
    } finally {
      setBusy(false)
    }
  }

  const savePerson = async () => {
    setBusy(true)
    setError('')
    try {
      const holder = await api.createHolder({ name: person.name, ref: person.ref, kind: 'staff' })
      if (person.cardNumber) {
        await api.issueCredential(holder.id, {
          kind: 'card', format: 'wiegand26',
          facilityCode: Number(person.facilityCode) || 0,
          cardNumber: person.cardNumber
        })
      }
      next()
    } catch (e) {
      setError(e && e.message ? e.message : t('common.error'))
    } finally {
      setBusy(false)
    }
  }

  const finish = async () => {
    setBusy(true)
    setError('')
    try {
      await api.completeSetup()
      onFinished()
    } catch (e) {
      setError(e && e.message ? e.message : t('common.error'))
      setBusy(false)
    }
  }

  return (
    <div className="wizard-page">
      <div className="wizard">
        <header className="wizard-head">
          <BrandLogo size={30} />
          <div>
            <strong>{t('app.name')}</strong>
            <span>{t('wizard.subtitle')}</span>
          </div>
        </header>

        <ol className="wizard-steps" aria-label={t('wizard.progress')}>
          {STEPS.slice(0, -1).map((s, i) => (
            <li key={s} className={i === step ? 'on' : i < step ? 'done' : ''}>
              <span>{i + 1}</span>
              {t(`wizard.step.${s}`)}
            </li>
          ))}
        </ol>

        <div className="wizard-body">
          {error ? <div className="notice notice-error"><p>{error}</p></div> : null}
          {name === 'welcome' ? (
            <Step title={t('wizard.welcome.title')} lead={t('wizard.welcome.lead')}>
              <ul className="wizard-list">
                <li>{t('wizard.welcome.p1')}</li>
                <li>{t('wizard.welcome.p2')}</li>
                <li>{t('wizard.welcome.p3')}</li>
              </ul>
              <Nav
                onNext={() => { loadSite(); next() }}
                nextLabel={t('wizard.start')}
                busy={busy}
              />
            </Step>
          ) : null}

          {name === 'site' ? (
            <Step title={t('wizard.site.title')} lead={t('wizard.site.lead')}>
              <label>
                <span>{t('settings.timezone')}</span>
                <input
                  value={site.timezone}
                  placeholder="Asia/Kuala_Lumpur"
                  onChange={e => setSite({ timezone: e.target.value })}
                />
                <small className="muted">{t('wizard.site.hint')}</small>
              </label>
              <Nav onBack={back} onNext={saveSite} busy={busy} disabled={!site.timezone.trim()} />
            </Step>
          ) : null}

          {name === 'cable' ? (
            <Step title={t('wizard.cable.title')} lead={t('wizard.cable.lead')}>
              <label>
                <span>{t('settings.busPort')}</span>
                <input
                  value={cable.port}
                  placeholder="tcp://127.0.0.1:4950"
                  onChange={e => setCable({ ...cable, port: e.target.value })}
                />
                <small className="muted">{t('wizard.cable.portHint')}</small>
              </label>
              <div className="form-row">
                <label>
                  <span>{t('settings.readerLabel')}</span>
                  <input value={cable.label} onChange={e => setCable({ ...cable, label: e.target.value })} />
                </label>
                <label>
                  <span>{t('settings.readerAddress')}</span>
                  <input
                    type="number" min="0" max="127"
                    value={cable.address}
                    onChange={e => setCable({ ...cable, address: e.target.value })}
                  />
                </label>
              </div>
              {/* The single most useful sentence in this wizard. Readers ship set to address 0, so
                  the second one on a cable never answers until somebody changes it — and nothing
                  about the symptom points at the cause. */}
              <p className="wizard-note">
                <Ico n="info" sz={14} /> {t('wizard.cable.addressNote')}
              </p>
              <Nav onBack={back} onNext={saveCable} onSkip={next} busy={busy} disabled={!cable.port.trim()} />
            </Step>
          ) : null}

          {name === 'door' ? (
            <Step title={t('wizard.door.title')} lead={t('wizard.door.lead')}>
              <label>
                <span>{t('doors.name')}</span>
                <input
                  value={door.name}
                  placeholder={t('wizard.door.placeholder')}
                  onChange={e => setDoor({ ...door, name: e.target.value })}
                />
              </label>
              <div className="form-row">
                <label>
                  <span>{t('doors.class')}</span>
                  <select value={door.class} onChange={e => setDoor({ ...door, class: e.target.value })}>
                    {['interior', 'perimeter', 'critical'].map(c => (
                      <option key={c} value={c}>{t(`doors.class.${c}`)}</option>
                    ))}
                  </select>
                  <small className="muted">{t('wizard.door.classHint')}</small>
                </label>
                <label>
                  <span>{t('doors.strikeTime')}</span>
                  <input
                    type="number" min="1"
                    value={door.unlockSeconds}
                    onChange={e => setDoor({ ...door, unlockSeconds: e.target.value })}
                  />
                </label>
              </div>
              <Nav onBack={back} onNext={saveDoor} onSkip={next} busy={busy} disabled={!door.name.trim()} />
            </Step>
          ) : null}

          {name === 'person' ? (
            <Step title={t('wizard.person.title')} lead={t('wizard.person.lead')}>
              <div className="form-row">
                <label>
                  <span>{t('people.name')}</span>
                  <input value={person.name} onChange={e => setPerson({ ...person, name: e.target.value })} />
                </label>
                <label>
                  <span>{t('people.ref')}</span>
                  <input value={person.ref} onChange={e => setPerson({ ...person, ref: e.target.value })} />
                </label>
              </div>
              <div className="form-row">
                <label>
                  <span>{t('badge.facility')}</span>
                  <input
                    type="number"
                    value={person.facilityCode}
                    onChange={e => setPerson({ ...person, facilityCode: e.target.value })}
                  />
                </label>
                <label>
                  <span>{t('badge.number')}</span>
                  <input
                    value={person.cardNumber}
                    onChange={e => setPerson({ ...person, cardNumber: e.target.value })}
                  />
                </label>
              </div>
              <small className="muted">{t('badge.facilityHint')}</small>
              {/* Said before they wonder why the badge does not work: creating a person and a card
                  does not grant access anywhere. That is the fail-closed default. */}
              <p className="wizard-note">
                <Ico n="info" sz={14} /> {t('wizard.person.grantNote')}
              </p>
              <Nav
                onBack={back} onNext={savePerson} onSkip={next}
                busy={busy} disabled={!person.name.trim() || !person.ref.trim()}
              />
            </Step>
          ) : null}

          {name === 'done' ? (
            <Step title={t('wizard.done.title')} lead={t('wizard.done.lead')}>
              <ul className="wizard-list">
                <li>{t('wizard.done.p1')}</li>
                <li>{t('wizard.done.p2')}</li>
              </ul>
              <div className="form-actions">
                <button type="button" className="btn btn-primary" disabled={busy} onClick={finish}>
                  {t('wizard.finish')}
                </button>
              </div>
            </Step>
          ) : null}
        </div>
      </div>
    </div>
  )
}

function Step({ title, lead, children }) {
  return (
    <section className="wizard-step">
      <h1>{title}</h1>
      {lead ? <p className="muted">{lead}</p> : null}
      <div className="form">{children}</div>
    </section>
  )
}

function Nav({ onBack, onNext, onSkip, nextLabel, busy, disabled }) {
  const t = useT()
  return (
    <div className="wizard-nav">
      {onBack ? (
        <button type="button" className="btn btn-quiet" onClick={onBack} disabled={busy}>
          {t('wizard.back')}
        </button>
      ) : <span />}
      <div className="wizard-nav-right">
        {/* Every step but the time zone can be skipped. A half-configured controller that boots is
            more useful than a wizard somebody abandons halfway and never returns to. */}
        {onSkip ? (
          <button type="button" className="btn btn-quiet" onClick={onSkip} disabled={busy}>
            {t('wizard.skip')}
          </button>
        ) : null}
        <button type="button" className="btn btn-primary" onClick={onNext} disabled={busy || disabled}>
          {nextLabel || t('wizard.next')}
        </button>
      </div>
    </div>
  )
}
