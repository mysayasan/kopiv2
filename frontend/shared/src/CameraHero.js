import { Fragment } from 'react';
import { Ico } from './icons';
import { useT } from './i18n';
import './styles/camera-hero.css';

// statusTone maps a backend status string onto one of the hero's colour tones.
// `pendingValue` names the status (if any) that means "in progress, no verdict yet" and so
// earns the neutral accent tone instead of a misleading green or red — mymatasan reports
// rtspStatus as online / offline / resolved, where "resolved" means the stream URL was
// resolved but the stream itself has not been verified.
export function statusTone(status, pendingValue) {
  const s = (status || '').toLowerCase();
  if (s === 'online') return 'online';
  if (s === 'offline') return 'offline';
  if (pendingValue && s === pendingValue.toLowerCase()) return 'pending';
  return 'unknown';
}

// CameraHero is THE camera-page header across the suite: mymatasan's own camera page and
// myseliasan's Nodes → Camera page render this same component, so the two apps cannot
// drift apart. It carries a breadcrumb trail (so a camera is never a dead-end you have to
// navigate out of via the rail), the camera's identity (status-tinted tile + name +
// description), and its status chips.
//
// Props:
//   crumbs       array of { label, onClick? } — the trail, outermost first. The LAST entry
//                is rendered as the current page (plain text) regardless of onClick.
//   crumbsLabel  accessible name for the breadcrumb <nav>; defaults to the shared
//                common.breadcrumb string, so callers normally omit it.
//   name         camera display name (required)
//   description  optional one-line description, shown under the name
//   tone         'online' | 'offline' | 'unknown' — tints the camera tile + its dot
//   chips        array of { key, label, tone, icon?, capitalize? } status chips
export function CameraHero({ crumbs, crumbsLabel, name, description, tone = 'unknown', chips }) {
  const t = useT();
  const trail = (crumbs || []).filter(Boolean);
  const list = (chips || []).filter(Boolean);
  return (
    <header className="cam-hero">
      {trail.length > 0 ? (
        <nav className="cam-hero-crumbs" aria-label={crumbsLabel || t('common.breadcrumb')}>
          {trail.map((crumb, i) => {
            const isLast = i === trail.length - 1;
            return (
              <Fragment key={`${crumb.label}-${i}`}>
                {i > 0 ? (
                  <span className="cam-hero-crumb-sep" aria-hidden="true">
                    <Ico n="chev-right" sz={12} />
                  </span>
                ) : null}
                {isLast || !crumb.onClick ? (
                  <span className="cam-hero-crumb is-current" aria-current={isLast ? 'page' : undefined}>
                    {crumb.label}
                  </span>
                ) : (
                  <button type="button" className="cam-hero-crumb" onClick={crumb.onClick}>
                    {crumb.label}
                  </button>
                )}
              </Fragment>
            );
          })}
        </nav>
      ) : null}

      <div className="cam-hero-main">
        <span className={`cam-hero-avatar is-${tone}`} aria-hidden="true">
          <Ico n="camera" sz={24} />
        </span>
        <div className="cam-hero-text">
          <h2 className="cam-hero-name">{name}</h2>
          {description ? <p className="cam-hero-desc">{description}</p> : null}
        </div>
        {list.length > 0 ? (
          <div className="cam-hero-chips">
            {list.map((chip) => (
              <span key={chip.key || chip.label} className={`cam-hero-chip is-${chip.tone || 'unknown'}`}>
                {chip.icon ? <Ico n={chip.icon} sz={13} /> : <span className="cam-hero-chip-dot" aria-hidden="true" />}
                <span className={chip.capitalize ? 'cam-hero-chip-cap' : undefined}>{chip.label}</span>
              </span>
            ))}
          </div>
        ) : null}
      </div>
    </header>
  );
}
