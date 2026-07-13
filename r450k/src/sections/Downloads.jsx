import { useEffect, useState } from 'react';
import Reveal from '../components/Reveal.jsx';
import SpotlightCard from '../components/SpotlightCard.jsx';
import Icon from '../components/Icon.jsx';
import TallyModal from '../components/TallyModal.jsx';
import { useContent } from '../i18n/index.jsx';

// Tally feedback form shown right after a download starts (see TALLY_FORM below).
// It's shown at most once per visitor — a localStorage flag suppresses it on every
// later download so we never nag repeat downloaders. Both helpers are wrapped in
// try/catch so SSR / private-mode (no localStorage) degrades to "just don't show".
const TALLY_FORM = 'https://tally.so/r/A75Jzl';
const TALLY_SEEN_KEY = 'r450k:downloadFeedbackSeen';

function hasSeenTally() {
  try {
    return typeof localStorage !== 'undefined' && localStorage.getItem(TALLY_SEEN_KEY) === '1';
  } catch (_) {
    return false;
  }
}

function markTallySeen() {
  try {
    if (typeof localStorage !== 'undefined') localStorage.setItem(TALLY_SEEN_KEY, '1');
  } catch (_) {
    /* ignore — worst case the form shows again next time */
  }
}

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
  const [tallyOpen, setTallyOpen] = useState(false);

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
  // The Worker returns one entry per product (mymatasan, myseliasan), each with its own
  // version and asset list — they release independently, off separate tags.
  const products = ((data && data.products) || []).filter((p) => p.assets && p.assets.length > 0);

  function onDownloadClick() {
    if (hasSeenTally()) return;
    markTallySeen();
    setTallyOpen(true);
  }

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
        </Reveal>

        {status === 'loading' ? (
          <p className="downloads__note">{t.loading}</p>
        ) : status === 'error' || products.length === 0 ? (
          <Reveal className="downloads__note">
            <p>{t.unavailable}</p>
          </Reveal>
        ) : (
          products.map((p) => {
            const copy = (t.products && t.products[p.id]) || {};
            const groups = ['windows', 'linux']
              .map((os) => ({ os, items: p.assets.filter((a) => a.os === os) }))
              .filter((g) => g.items.length > 0);
            return (
              <div className="downloads__product" key={p.id}>
                <Reveal className="downloads__producthead">
                  <h3 className="downloads__productname">{copy.name || p.name}</h3>
                  {copy.tagline ? <p className="downloads__producttag">{copy.tagline}</p> : null}
                  {p.version ? (
                    <p className="downloads__version">
                      {t.latest}: <strong>{p.version}</strong>
                    </p>
                  ) : null}
                </Reveal>

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
                                target="_blank" (blank tab that drops the download).
                                The onClick only opens the Tally form as a side effect
                                (and only once per visitor) — it never preventDefaults,
                                so the download is untouched. */}
                            <a className="dllink" href={a.url} onClick={onDownloadClick}>
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

                  {p.docker ? (
                    <SpotlightCard className="dlcard" style={{ '--i': groups.length }}>
                      <div className="dlcard__head">
                        <span className="dlcard__icon">
                          <Icon name="docker" size={22} />
                        </span>
                        <h3 className="dlcard__name">{t.docker}</h3>
                      </div>
                      <p className="dlcard__hint">{copy.dockerHint || t.dockerHint}</p>
                      <code className="dlcard__code">docker pull {p.docker.image}</code>
                    </SpotlightCard>
                  ) : null}
                </Reveal>
              </div>
            );
          })
        )}
      </div>

      <TallyModal
        open={tallyOpen}
        onClose={() => setTallyOpen(false)}
        src={`${TALLY_FORM}?hideTitle=1`}
        title={t.formTitle}
        blurb={t.formBlurb}
        closeLabel={t.formClose}
      />
    </section>
  );
}
