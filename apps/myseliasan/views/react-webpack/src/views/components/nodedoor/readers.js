import { useCallback, useEffect, useState } from 'react';
import { useT, DataTable, Ico } from '@shared';
import * as api from './lib/access';

// mypintusan's readers screen, ported into the control-plane embed. It answers the two questions
// an installer asks: is it talking, and is it encrypted — both invisible from the door itself,
// and now visible from the control plane without a site visit.
export function ReadersPage() {
  const t = useT();
  const [readers, setReaders] = useState(null);
  const [settings, setSettings] = useState(null);
  const [error, setError] = useState('');

  const load = useCallback(async () => {
    try {
      // The enrolled readers come from the node's database; the security posture comes from its
      // settings, where each configured reader reports whether a key is installed WITHOUT
      // revealing it — so nothing secret ever crosses the tunnel.
      const [list, cfg] = await Promise.all([
        api.listReaders(),
        api.getSettings().catch(() => null),
      ]);
      setReaders(Array.isArray(list) ? list : []);
      setSettings(cfg);
      setError('');
    } catch (e) {
      setError(e && e.message ? e.message : t('ndoor.error'));
    }
  }, [t]);

  useEffect(() => { load(); }, [load]);

  // securityFor finds the configured posture for a reader, matched on cable + address.
  const securityFor = (reader) => {
    if (!settings || !Array.isArray(settings.buses)) return null;
    const bus = settings.buses.find((b) => b.port === reader.busPort);
    if (!bus) return null;
    return (bus.readers || []).find((r) => r.address === reader.osdpAddress) || null;
  };

  const columns = [
    { key: 'name', label: t('ndoor.readers.title') },
    { key: 'busPort', label: t('ndoor.readers.bus'), render: (_v, r) => <code>{r.busPort}</code> },
    { key: 'osdpAddress', label: t('ndoor.readers.address') },
    { key: 'doorId', label: t('ndoor.readers.door'), render: (_v, r) => r.doorId || '—' },
    {
      key: 'security',
      label: t('ndoor.readers.security'),
      render: (_v, r) => {
        const sec = securityFor(r);
        if (!sec || !sec.hasScbk) {
          return <span className="pill pill-warn">{t('ndoor.readers.notEncrypted')}</span>;
        }
        // A reader still on the key it shipped with is NOT secure — that key is published by the
        // manufacturer. Saying "encrypted" here would be a lie an installer would believe.
        if (sec.usingDefaultKey) {
          return (
            <span title={t('ndoor.readers.defaultKeyWarn')}>
              <span className="pill pill-danger">{t('ndoor.readers.defaultKey')}</span>
            </span>
          );
        }
        return <span className="pill pill-ok">{t('ndoor.readers.encrypted')}</span>;
      },
    },
    { key: 'lastSeenAt', label: t('ndoor.readers.lastSeen'), render: (_v, r) => api.formatTime(r.lastSeenAt) },
  ];

  return (
    <div className="screen">
      <header className="screen-head">
        <div>
          <h1>{t('ndoor.readers.title')}</h1>
          <p className="muted">{t('ndoor.readers.subtitle')}</p>
        </div>
      </header>

      {error ? <div className="notice notice-error"><p>{error}</p></div> : null}
      {readers === null && !error ? <p>{t('ndoor.loading')}</p> : null}
      {readers && readers.length === 0 ? (
        <p className="muted"><Ico n="cpu" sz={15} /> {t('ndoor.readers.empty')}</p>
      ) : null}
      {readers && readers.length > 0 ? (
        <DataTable columns={columns} rows={readers} />
      ) : null}
    </div>
  );
}
