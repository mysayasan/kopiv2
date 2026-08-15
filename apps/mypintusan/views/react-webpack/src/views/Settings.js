import { useCallback, useEffect, useState } from 'react'
import { useT, Ico, DeploymentPanel } from '@shared'
import * as api from '../lib/access'
import { apiRequest } from '../lib/api'

// The shared panel speaks the fetch contract — a resolved {ok, body} rather than a throw —
// while this app's helpers unwrap and rethrow. Only the read is ever used here: mypintusan
// is an appliance, so the panel renders a reason and offers nothing to save.
async function deploymentApi(path, options) {
  try {
    const res = await apiRequest(path, options)
    const body = (res && res.data && res.data.result !== undefined)
      ? res.data.result
      : (res && res.result !== undefined ? res.result : res)
    return { ok: true, body }
  } catch (err) {
    return { ok: false, message: err.message }
  }
}

// The settings screen is the reason config.json only seeds the first boot: after that, everything
// here is editable by a facilities manager rather than by somebody with SSH and a text editor.
//
// SECURITY KEYS ARE NEVER LOADED INTO THIS SCREEN. The server redacts them and reports
// hasScbk/usingDefaultKey instead, and a save sends them back absent — the server carries the
// stored key forward. That means this page cannot leak a site key, and equally cannot destroy one.
export default function Settings({ toast }) {
  const t = useT()
  const [form, setForm] = useState(null)
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState('')

  const load = useCallback(async () => {
    try {
      const cfg = await api.getSettings()
      setForm(cfg)
      setError('')
    } catch (e) {
      setError(e && e.message ? e.message : t('common.error'))
    }
  }, [t])

  useEffect(() => { load() }, [load])

  const save = async e => {
    e.preventDefault()
    setSaving(true)
    try {
      const saved = await api.saveSettings(form)
      setForm(saved)
      toast(t('settings.saved'), 'ok')
    } catch (err) {
      // The server's validation messages are written for this reader — "two readers at address 1"
      // — so they are shown verbatim rather than replaced with a generic failure.
      toast(err && err.message ? err.message : t('settings.saveFailed'), 'error')
    } finally {
      setSaving(false)
    }
  }

  const reset = async () => {
    if (!window.confirm(t('settings.resetConfirm'))) return
    try {
      setForm(await api.resetSettings())
      toast(t('settings.saved'), 'ok')
    } catch (err) {
      toast(err && err.message ? err.message : t('common.error'), 'error')
    }
  }

  if (error) return <div className="screen"><div className="notice notice-error"><p>{error}</p></div></div>
  if (!form) return <div className="screen"><p>{t('common.loading')}</p></div>

  const setBus = (i, patch) => {
    const buses = form.buses.map((b, idx) => idx === i ? { ...b, ...patch } : b)
    setForm({ ...form, buses })
  }
  const setReader = (bi, ri, patch) => {
    const buses = form.buses.map((b, idx) => idx !== bi ? b : {
      ...b, readers: b.readers.map((r, j) => j === ri ? { ...r, ...patch } : r)
    })
    setForm({ ...form, buses })
  }

  return (
    <div className="screen">
      <header className="screen-head">
        <div>
          <h1>{t('settings.title')}</h1>
          <p className="muted">{t('settings.subtitle')}</p>
        </div>
        <button type="button" className="btn btn-quiet" onClick={reset}>{t('settings.reset')}</button>
      </header>

      <form onSubmit={save} className="form">
        <label>
          <span>{t('settings.timezone')}</span>
          <input value={form.timezone || ''} onChange={e => setForm({ ...form, timezone: e.target.value })} />
          <small className="muted">{t('settings.timezoneHint')}</small>
        </label>

        <div className="form-row">
          <label>
            <span>{t('settings.tick')}</span>
            <input
              type="number" min="1" max="60"
              value={form.tickSeconds || 1}
              onChange={e => setForm({ ...form, tickSeconds: Number(e.target.value) })}
            />
            <small className="muted">{t('settings.tickHint')}</small>
          </label>
          <label>
            <span>{t('settings.pinWindow')}</span>
            <input
              type="number" min="1"
              value={form.pinWindowSeconds || 15}
              onChange={e => setForm({ ...form, pinWindowSeconds: Number(e.target.value) })}
            />
            <small className="muted">{t('settings.pinWindowHint')}</small>
          </label>
        </div>

        <h2 className="section-head">{t('settings.buses')}</h2>
        <p className="muted small">{t('settings.restartNote')}</p>

        {(form.buses || []).map((bus, bi) => (
          <fieldset className="bus-box" key={bi}>
            <legend>
              <input
                className="bus-port"
                value={bus.port || ''}
                placeholder="tcp://host:port"
                onChange={e => setBus(bi, { port: e.target.value })}
              />
            </legend>

            {(bus.readers || []).map((rd, ri) => (
              <div className="reader-row" key={ri}>
                <label>
                  <span>{t('settings.readerLabel')}</span>
                  <input value={rd.label || ''} onChange={e => setReader(bi, ri, { label: e.target.value })} />
                </label>
                <label>
                  <span>{t('settings.readerAddress')}</span>
                  <input
                    type="number" min="0" max="127"
                    value={rd.address ?? 0}
                    onChange={e => setReader(bi, ri, { address: Number(e.target.value) })}
                  />
                </label>
                <label className="check">
                  <input
                    type="checkbox"
                    checked={!!rd.requireSecureChannel}
                    onChange={e => setReader(bi, ri, { requireSecureChannel: e.target.checked })}
                  />
                  <span>{t('settings.requireEncryption')}</span>
                </label>
                <span className="key-state">
                  {rd.usingDefaultKey ? (
                    <span className="pill pill-danger" title={t('readers.defaultKeyWarn')}>
                      <Ico n="shield" sz={12} /> {t('readers.defaultKey')}
                    </span>
                  ) : rd.hasScbk ? (
                    <span className="pill pill-ok">{t('settings.keyInstalled')}</span>
                  ) : (
                    <span className="pill pill-warn">{t('settings.noKey')}</span>
                  )}
                </span>
              </div>
            ))}

            {/* The address-0 collision is the commonest onboarding mistake — readers ship set to 0,
                so a second one silently never answers. Said here, before it happens. */}
            <p className="muted small">{t('settings.addressHint')}</p>

            <button
              type="button"
              className="btn btn-quiet"
              onClick={() => setBus(bi, {
                readers: [...(bus.readers || []), { address: 0, requireSecureChannel: false, label: '' }]
              })}
            >
              <Ico n="plus" sz={14} /> {t('settings.addReader')}
            </button>
          </fieldset>
        ))}

        <button
          type="button"
          className="btn btn-quiet"
          onClick={() => setForm({
            ...form,
            buses: [...(form.buses || []), { port: '', slotMillis: 50, replyTimeoutMillis: 200, readers: [] }]
          })}
        >
          <Ico n="plus" sz={14} /> {t('settings.addBus')}
        </button>

        <div className="form-actions">
          <button type="submit" className="btn btn-primary" disabled={saving}>{t('common.save')}</button>
        </div>
      </form>

      <FleetSection toast={toast} />
    </div>
  )
}

// FleetSection joins this controller to a myseliasan control plane. It renders nothing when the
// pairing API is unreachable — fleet support disabled in config, or a signed-in user without the
// admin role — because a panel full of buttons that all fail is worse than no panel.
function FleetSection({ toast }) {
  const t = useT()
  const [status, setStatus] = useState(null)
  const [hidden, setHidden] = useState(false)
  const [fleetKey, setFleetKey] = useState('')
  const [claim, setClaim] = useState(null)

  const load = useCallback(async () => {
    try {
      setStatus(await api.pairingStatus())
    } catch {
      setHidden(true)
    }
  }, [])

  useEffect(() => { load() }, [load])

  if (hidden || !status) return null

  const saveKey = async e => {
    e.preventDefault()
    try {
      await api.saveFleetKey(fleetKey)
      setFleetKey('')
      toast(t('settings.fleetKeySaved'), 'ok')
      load()
    } catch (err) {
      toast(err && err.message ? err.message : t('common.error'), 'error')
    }
  }

  const genClaim = async () => {
    try {
      setClaim(await api.generateClaimCode())
      load()
    } catch (err) {
      toast(err && err.message ? err.message : t('common.error'), 'error')
    }
  }

  const unpair = async () => {
    if (!window.confirm(t('settings.unpairConfirm'))) return
    try {
      await api.unpairFleet()
      setClaim(null)
      toast(t('settings.unpaired'), 'ok')
      load()
    } catch (err) {
      toast(err && err.message ? err.message : t('common.error'), 'error')
    }
  }

  return (
    <section className="form fleet-section">
      {/* Deployment answer. Fixed for this app — the OSDP bus opens its serial port once
          and holds it for the life of the bus, and a serial port belongs to exactly one
          process on one machine — so the panel renders a reason rather than a choice.
          Shown because an operator who does not know that keeps looking for the setting. */}
      <DeploymentPanel api={deploymentApi} appLabel="MyPintuSan" onToast={toast} />

      <h2 className="section-head">{t('settings.fleet')}</h2>
      <p className="muted small">{t('settings.fleetHint')}</p>

      <div className="fleet-status">
        <span className={status.paired ? 'pill pill-ok' : 'pill pill-warn'}>
          {status.paired ? t('settings.fleetPaired') : t('settings.fleetNotPaired')}
        </span>
        {status.paired && (
          <span className="muted">{t('settings.fleetParent')}: {status.parentName || status.parentId}</span>
        )}
        {!status.paired && (
          <span className="muted">
            {status.fleetKeySet ? t('settings.fleetKeySet') : t('settings.fleetKeyNotSet')}
          </span>
        )}
      </div>

      {!status.paired ? (
        <>
          <form onSubmit={saveKey}>
            <label>
              <span>{t('settings.fleetKey')}</span>
              <input
                type="password" autoComplete="off"
                value={fleetKey}
                onChange={e => setFleetKey(e.target.value)}
              />
              <small className="muted">{t('settings.fleetKeyHint')}</small>
            </label>
            <div className="form-actions">
              <button type="submit" className="btn btn-quiet" disabled={!fleetKey.trim()}>
                {t('settings.saveFleetKey')}
              </button>
            </div>
          </form>

          <label>
            <span>{t('settings.claimCode')}</span>
            {claim && claim.code ? <code className="claim-code">{claim.code}</code> : null}
            <small className="muted">{t('settings.claimHint')}</small>
          </label>
          <div className="form-actions">
            <button type="button" className="btn btn-quiet" onClick={genClaim} disabled={!status.fleetKeySet}>
              {t('settings.genClaim')}
            </button>
          </div>
        </>
      ) : (
        <div className="form-actions">
          <button type="button" className="btn btn-danger" onClick={unpair}>
            {t('settings.unpair')}
          </button>
        </div>
      )}
    </section>
  )
}
