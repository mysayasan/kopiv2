import { useEffect, useState } from 'react';
import Reveal from '../components/Reveal.jsx';
import SpotlightCard from '../components/SpotlightCard.jsx';
import Icon from '../components/Icon.jsx';
import { useContent } from '../i18n/index.jsx';

function formatSize(bytes) {
  if (!bytes) return '';
  const mb = bytes / (1024 * 1024);
  return mb >= 1 ? `${mb.toFixed(1)} MB` : `${Math.max(1, Math.round(bytes / 1024))} KB`;
}

// osMeta drives the per-platform card header (icon + which download-kinds it groups).
const OS_META = {
  windows: { icon: 'windows', key: 'windows' },
  linux: { icon: 'terminal', key: 'linux' },
};

export default function Downloads() {
  const { downloads: t } = useContent();
  const [state, setState] = useState({ status: 'loading', data: null });

  useEffect(() => {
    let alive = true;
    fetch('/api/downloads')
      .then((r) => r.json())
      .then((data) => alive && setState({ status: data.ok ? 'ready' : 'error', data }))
      .catch(() => alive && setState({ status: 'error', data: null }));
    return () => {
      alive = false;
    };
  }, []);

  const { status, data } = state;
  const assets = (data && data.assets) || [];
  const groups = ['windows', 'linux']
    .map((os) => ({ os, items: assets.filter((a) => a.os === os) }))
    .filter((g) => g.items.length > 0);

  return (
    <section className="section" id="download">
      <div className="container">
        <Reveal className="section__head">
          <p className="kicker">{t.kicker}</p>
          <h2 className="section__title">{t.title}</h2>
          <p className="section__lead">{t.subtitle}</p>
          {t.license ? (
            <p className="downloads__license">
              <Icon name="shield" size={15} /> {t.license}
            </p>
          ) : null}
          {status === 'ready' && data.version ? (
            <p className="downloads__version">
              {t.latest}: <strong>{data.version}</strong>
            </p>
          ) : null}
        </Reveal>

        {status === 'loading' ? (
          <p className="downloads__note">{t.loading}</p>
        ) : status === 'error' || groups.length === 0 ? (
          <Reveal className="downloads__note">
            <p>{t.unavailable}</p>
          </Reveal>
        ) : (
          <Reveal className="grid grid--downloads" stagger>
            {groups.map((g, gi) => (
              <SpotlightCard className="dlcard" key={g.os} style={{ '--i': gi }}>
                <div className="dlcard__head">
                  <span className="dlcard__icon">
                    <Icon name={OS_META[g.os].icon} size={22} />
                  </span>
                  <h3 className="dlcard__name">{t[OS_META[g.os].key]}</h3>
                </div>
                <ul className="dlcard__list">
                  {g.items.map((a) => (
                    <li key={a.name}>
                      {/* Same-origin endpoint that streams the bytes with
                          Content-Disposition: attachment (the Worker proxies
                          GitHub rather than redirecting), so a plain same-tab
                          click downloads in place without leaving the page. No
                          target="_blank" (blank tab that drops the download). */}
                      <a className="dllink" href={a.url}>
                        <span className="dllink__label">
                          <Icon name="download" size={16} />
                          {a.label}
                        </span>
                        {a.size ? <span className="dllink__size">{formatSize(a.size)}</span> : null}
                      </a>
                    </li>
                  ))}
                </ul>
              </SpotlightCard>
            ))}

            {data.docker ? (
              <SpotlightCard className="dlcard" style={{ '--i': groups.length }}>
                <div className="dlcard__head">
                  <span className="dlcard__icon">
                    <Icon name="docker" size={22} />
                  </span>
                  <h3 className="dlcard__name">{t.docker}</h3>
                </div>
                <p className="dlcard__hint">{t.dockerHint}</p>
                <code className="dlcard__code">docker pull {data.docker.image}</code>
              </SpotlightCard>
            ) : null}
          </Reveal>
        )}
      </div>
    </section>
  );
}
