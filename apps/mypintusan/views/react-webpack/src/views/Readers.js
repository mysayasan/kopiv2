import { useCallback, useEffect, useState } from 'react'
import { useT, DataTable, Ico } from '@shared'
import * as api from '../lib/access'

// The readers screen exists mainly to answer two questions an installer asks: is it talking, and is
// it encrypted. Both are things the operator cannot see from the door itself.
export default function Readers({ caps = {} }) {
  const t = useT()
  const [readers, setReaders] = useState(null)
  const [settings, setSettings] = useState(null)
  // Distinct from `settings === null`: "nobody asked" and "asked and was refused" produce the same
  // empty value and must not produce the same words on screen. See securityFor.
  const [postureUnknown, setPostureUnknown] = useState(false)
  const [error, setError] = useState('')

  const load = useCallback(async () => {
    try {
      // The enrolled readers come from the database; the security posture comes from the settings,
      // where each configured reader reports whether a key is installed WITHOUT revealing it.
      // The security posture lives in the access settings, which are ADMIN-ONLY. Asking anyway
      // and swallowing the 403 is what made this screen lie: `securityFor` then returned null for
      // every reader and the column rendered "Not encrypted" — about readers running encrypted
      // sessions. The answer to "is this reader encrypted?" is the one thing this screen exists to
      // give, and a confident wrong answer about the security of a door is worse than no answer.
      const canSeePosture = !!caps.viewSettings
      const [list, cfg] = await Promise.all([
        api.listReaders(),
        canSeePosture ? api.getSettings().catch(() => null) : Promise.resolve(null)
      ])
      setReaders(Array.isArray(list) ? list : [])
      setSettings(cfg)
      setPostureUnknown(!canSeePosture || !cfg)
      setError('')
    } catch (e) {
      setError(e && e.message ? e.message : t('common.error'))
    }
  }, [t, caps.viewSettings])

  useEffect(() => { load() }, [load])

  // securityFor finds the configured posture for a reader, matched on cable + address.
  const securityFor = reader => {
    if (!settings || !Array.isArray(settings.buses)) return null
    const bus = settings.buses.find(b => b.port === reader.busPort)
    if (!bus) return null
    return (bus.readers || []).find(r => r.address === reader.osdpAddress) || null
  }

  const columns = [
    { key: 'name', label: t('readers.title') },
    { key: 'busPort', label: t('readers.bus'), render: (_v, r) => <code>{r.busPort}</code> },
    { key: 'osdpAddress', label: t('readers.address') },
    { key: 'doorId', label: t('readers.door'), render: (_v, r) => r.doorId || '—' },
    {
      key: 'security',
      label: t('readers.security'),
      render: (_v, r) => {
        // Say "unknown", not "not encrypted", when this account cannot read the posture at all.
        if (postureUnknown) {
          return (
            <span title={t('readers.securityUnknownHint')}>
              <span className="pill">{t('readers.securityUnknown')}</span>
            </span>
          )
        }
        const sec = securityFor(r)
        if (!sec || !sec.hasScbk) {
          return <span className="pill pill-warn">{t('readers.notEncrypted')}</span>
        }
        // A reader still on the key it shipped with is NOT secure — that key is published by the
        // manufacturer. Saying "encrypted" here would be a lie an installer would believe.
        if (sec.usingDefaultKey) {
          return (
            <span title={t('readers.defaultKeyWarn')}>
              <span className="pill pill-danger">{t('readers.defaultKey')}</span>
            </span>
          )
        }
        return <span className="pill pill-ok">{t('readers.encrypted')}</span>
      }
    },
    { key: 'lastSeenAt', label: t('readers.lastSeen'), render: (_v, r) => api.formatTime(r.lastSeenAt) }
  ]

  return (
    <div className="screen">
      <header className="screen-head">
        <div>
          <h1>{t('readers.title')}</h1>
          <p className="muted">{t('readers.subtitle')}</p>
        </div>
      </header>

      {error ? <div className="notice notice-error"><p>{error}</p></div> : null}
      {readers === null && !error ? <p>{t('common.loading')}</p> : null}
      {readers && readers.length === 0 ? (
        <p className="muted"><Ico n="cpu" sz={15} /> {t('readers.empty')}</p>
      ) : null}
      {readers && readers.length > 0 ? (
        <DataTable columns={columns} rows={readers} />
      ) : null}
    </div>
  )
}
