import { useState, useEffect } from 'react';
import { useT } from './i18n';
import './styles/footer.css';

// AppFooter is the shared page footer: the app name + version + shared-core
// version + short commit + build date (read from a version endpoint), followed by
// the KopiV2 product tagline. It is SELF-CONTAINED — its own `shared-footer-*`
// classes + styles/footer.css (tokenized with --ui-*), so it follows each app's
// theme and a UI redesign can't leave it unstyled. Version fields render only when
// present, so a missing/unreachable endpoint degrades gracefully to just the name.
//
// Props: appName (display name, e.g. "MyMataSan"); apiBase (optional base prefix
// for the fetch, default ''); versionPath (default '/api/version'); authHeader
// (optional Authorization value for the version probe).
export function AppFooter({ appName, apiBase = '', versionPath = '/api/version', authHeader }) {
  const t = useT();
  const [ver, setVer] = useState(null);
  useEffect(() => {
    let cancelled = false;
    (async () => {
      try {
        const headers = authHeader ? { Authorization: authHeader } : {};
        const resp = await fetch(`${apiBase}${versionPath}`, { credentials: 'include', headers });
        if (!resp.ok) return;
        const payload = await resp.json();
        const body = payload?.data?.result ?? payload?.result ?? payload;
        if (!cancelled && body && typeof body === 'object') setVer(body);
      } catch (_) { /* footer is best-effort */ }
    })();
    return () => { cancelled = true; };
  }, [apiBase, versionPath, authHeader]);
  return (
    <footer className="shared-footer">
      <div className="shared-footer-line">
        <span className="shared-footer-name">{appName}</span>
        {ver?.appVersion ? <span className="shared-footer-ver">v{ver.appVersion}</span> : null}
        {ver?.coreVersion ? <span className="shared-footer-core">{t('foot.core', { v: ver.coreVersion })}</span> : null}
        {ver?.commit ? <span className="shared-footer-commit" title={t('foot.commit')}>#{String(ver.commit).slice(0, 8)}</span> : null}
        {ver?.updatedAt ? <span className="shared-footer-built">{t('foot.built', { date: ver.updatedAt })}</span> : null}
      </div>
      <p className="shared-footer-tagline">{t('foot.tagline')}</p>
    </footer>
  );
}
