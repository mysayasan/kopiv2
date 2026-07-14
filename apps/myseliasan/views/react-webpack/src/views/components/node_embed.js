import { useEffect, useState } from 'react';
import { Ico, useT } from '@shared';
import { ensureScopedMymatasanCss, NODECAM_SCOPE_CLASS } from './nodecam/scoped_css';
import { ensureScopedMyiotsanCss, NODEIOT_SCOPE_CLASS } from './nodeiot/scoped_css';

// NodeFeatureBanner is the per-section "unavailable on this node" notice. When a panel's
// endpoint 404s (the node is an older version that predates the feature) we show this
// instead of a silent blank — so the operator knows WHY, and that updating the node fixes
// it. Version is the node's reported build (from the registry).
export function NodeFeatureBanner({ version }) {
  const t = useT();
  return (
    <div className="node-feature-banner" role="status">
      <Ico n="info" sz={16} />
      <span>{version ? t('nm.featureUnavailableV', { version }) : t('nm.featureUnavailable')}</span>
    </div>
  );
}

// currentTheme reads myseliasan's active theme class off <html> so the embedded page's
// scoped theme tokens (mymatasan's .theme-* variants, rewritten onto the wrapper) match.
function currentTheme() {
  if (typeof document === 'undefined') return 'theme-light';
  const m = document.documentElement.className.match(/theme-[\w-]+/);
  return m ? m[0] : 'theme-light';
}

// NodeEmbed wraps an embedded mymatasan page so mymatasan's (scoped) stylesheet styles it
// exactly like the node's own app, isolated under `.nodecam-embed` — without a Shadow DOM,
// so React events/inputs behave normally. It injects the scoped stylesheet once and mirrors
// myseliasan's light/dark theme onto the wrapper.
export function NodeEmbed({ children, className }) {
  const [theme, setTheme] = useState(currentTheme);

  useEffect(() => { ensureScopedMymatasanCss(); }, []);

  useEffect(() => {
    const obs = new MutationObserver(() => setTheme(currentTheme()));
    obs.observe(document.documentElement, { attributes: true, attributeFilter: ['class'] });
    return () => obs.disconnect();
  }, []);

  return (
    <div className={`${NODECAM_SCOPE_CLASS} ${theme}${className ? ` ${className}` : ''}`}>
      {children}
    </div>
  );
}

// NodeIotEmbed is the same contract for a SENSOR HUB: myiotsan's own stylesheet, scoped to
// `.nodeiot-embed`, so an embedded hub page looks exactly like the hub's own app while it is
// living inside myseliasan's shell. Two node apps, two stylesheets, two scopes — neither can
// reach the other, and neither can reach the shell.
export function NodeIotEmbed({ children, className }) {
  const [theme, setTheme] = useState(currentTheme);

  useEffect(() => { ensureScopedMyiotsanCss(); }, []);

  useEffect(() => {
    const obs = new MutationObserver(() => setTheme(currentTheme()));
    obs.observe(document.documentElement, { attributes: true, attributeFilter: ['class'] });
    return () => obs.disconnect();
  }, []);

  return (
    <div className={`${NODEIOT_SCOPE_CLASS} ${theme}${className ? ` ${className}` : ''}`}>
      {children}
    </div>
  );
}
