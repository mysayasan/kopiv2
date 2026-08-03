import { useEffect, useState, useCallback, useRef } from 'react';
import { FactoryResetSection, Ico, useT, Tabs } from '@shared';
import { api, apiBase } from '../lib/helpers';
import '../styles/settings.css';

// SettingsPage is the in-app editor for the SAFE SUBSET of config.json (localAuth, SSO,
// pairing, security, storage, logging) plus a System tab that restarts the process.
// Superadmin-only (the nav item and the backend both gate it). Because these are infra
// blocks the host reads only at boot, every save is written back to config.json and takes
// effect on the next restart — the page surfaces that with a "restart required" banner.

// Redis database numbers (0–15) as a dropdown, so the field is a pick, not a type-in.
const DB_OPTIONS = Array.from({ length: 16 }, (_, i) => ({ v: i, label: String(i) }));

// --- myidsan SSO bundle import ----------------------------------------------
// myidsan's Apps page exports the client it just registered as a small JSON file.
// Every value in it has to be matched here byte-for-byte (client ID, audience, the
// redirect URL), and retyping them across two consoles is exactly where operators
// slip. Importing fills the form; it does NOT save — the operator still reviews and
// presses Save, because this is the setting that decides who can log in.
const SSO_BUNDLE_KIND = 'myidsan.sso.client';
const SSO_BUNDLE_MAX_VERSION = 1;

// Bundle leaf -> settings-form path. Anything absent from the file is left alone
// rather than blanked, so a partial bundle cannot silently wipe working config.
const SSO_BUNDLE_FIELDS = [
  ['issuer', 'sso.issuer'],
  ['audience', 'sso.audience'],
  ['clientId', 'sso.clientId'],
  ['clientSecret', 'sso.clientSecret'],
  ['providerBaseUrl', 'sso.providerBaseUrl'],
  ['redirectBaseUrl', 'sso.redirectBaseUrl'],
  ['redirectPath', 'sso.redirectPath'],
  ['sessionTtlSeconds', 'sso.sessionTtlSeconds'],
];

// SECTIONS drives both the tab bar and each form. A field's `path` is relative to the
// section payload root (the same shape the API returns/accepts); `k` is its i18n label key
// under settings.field.*. Group rows (type 'group') render a subheading only.
const SECTIONS = [
  {
    id: 'localAuth', icon: 'user', tone: 'blue',
    fields: [
      { path: 'localAuth.enabled', k: 'enabled', type: 'checkbox' },
      { path: 'localAuth.username', k: 'username', type: 'text' },
      { path: 'localAuth.password', k: 'password', type: 'password' },
    ],
  },
  {
    id: 'sso', icon: 'lock', tone: 'violet',
    action: 'importSso',
    fields: [
      { path: 'sso.issuer', k: 'issuer', type: 'text' },
      { path: 'sso.audience', k: 'audience', type: 'text' },
      { path: 'sso.clientId', k: 'clientId', type: 'text' },
      { path: 'sso.clientSecret', k: 'clientSecret', type: 'password' },
      { path: 'sso.providerBaseUrl', k: 'providerBaseUrl', type: 'text' },
      { path: 'sso.redirectBaseUrl', k: 'redirectBaseUrl', type: 'text' },
      { path: 'sso.redirectPath', k: 'redirectPath', type: 'text', suggest: ['/api/auth/callback'] },
      { path: 'sso.caCertPath', k: 'caCertPath', type: 'text', browse: 'file' },
      { path: 'sso.sessionTtlSeconds', k: 'sessionTtl', type: 'number', suggest: [3600, 28800, 86400, 259200, 604800] },
      { path: 'sso.policyCacheTtlSeconds', k: 'policyCacheTtl', type: 'number' },
    ],
  },
  {
    id: 'pairing', icon: 'wifi', tone: 'teal',
    fields: [
      { path: 'pairing.enabled', k: 'enabled', type: 'checkbox' },
      { path: 'pairing.multicastAddr', k: 'multicastAddr', type: 'text', suggest: ['239.255.90.21:49531'] },
      { path: 'pairing.mtlsPort', k: 'mtlsPort', type: 'number', suggest: [39532] },
      { path: 'pairing.controlPort', k: 'controlPort', type: 'number', suggest: [39533] },
      { path: 'pairing.mediaPort', k: 'mediaPort', type: 'number', suggest: [39534] },
      { path: 'pairing.certTtlHours', k: 'certTtlHours', type: 'number' },
      { path: 'pairing.renewBeforeHours', k: 'renewBeforeHours', type: 'number' },
      { path: 'pairing.heartbeatIntervalSeconds', k: 'heartbeatInterval', type: 'number' },
      { path: 'pairing.replayWindowSeconds', k: 'replayWindow', type: 'number' },
      { path: 'pairing.parentBaseUrl', k: 'parentBaseUrl', type: 'text' },
    ],
  },
  {
    id: 'security', icon: 'shield', tone: 'amber',
    fields: [
      { path: 'jwt.secret', k: 'jwtSecret', type: 'password' },
      { path: 'allowOrigins', k: 'allowOrigins', type: 'text' },
      { type: 'group', g: 'tls' },
      { path: 'tls.certPath', k: 'certPath', type: 'text', browse: 'file' },
      { path: 'tls.keyPath', k: 'keyPath', type: 'text', browse: 'file' },
      { type: 'group', g: 'securityHeaders' },
      { path: 'securityHeaders.contentSecurityPolicy', k: 'csp', type: 'textarea' },
      { type: 'group', g: 'rateLimit' },
      { path: 'rateLimit.enabled', k: 'enabled', type: 'checkbox' },
      { path: 'rateLimit.defaultWindowSeconds', k: 'defaultWindow', type: 'number' },
      { type: 'group', g: 'devOnly' },
      { path: 'rateLimit.devOnly.enabled', k: 'enabled', type: 'checkbox' },
      { path: 'rateLimit.devOnly.requests', k: 'requests', type: 'number' },
      { path: 'rateLimit.devOnly.windowSeconds', k: 'windowSeconds', type: 'number' },
      { type: 'group', g: 'authOnly' },
      { path: 'rateLimit.authOnly.enabled', k: 'enabled', type: 'checkbox' },
      { path: 'rateLimit.authOnly.requests', k: 'requests', type: 'number' },
      { path: 'rateLimit.authOnly.windowSeconds', k: 'windowSeconds', type: 'number' },
      { type: 'group', g: 'public' },
      { path: 'rateLimit.public.enabled', k: 'enabled', type: 'checkbox' },
      { path: 'rateLimit.public.requests', k: 'requests', type: 'number' },
      { path: 'rateLimit.public.windowSeconds', k: 'windowSeconds', type: 'number' },
    ],
  },
  {
    id: 'storage', icon: 'server', tone: 'steel',
    fields: [
      { type: 'group', g: 'fileStorage' },
      { path: 'fileStorage.path', k: 'path', type: 'text', browse: 'dir' },
      { path: 'fileStorage.cleanup.enabled', k: 'cleanupEnabled', type: 'checkbox' },
      { path: 'fileStorage.cleanup.frequencySeconds', k: 'frequencySeconds', type: 'number' },
      { path: 'fileStorage.cleanup.batchSize', k: 'batchSize', type: 'number' },
      { type: 'group', g: 'cache' },
      { path: 'cache.provider', k: 'provider', type: 'select', options: [
        { v: 'default', labelKey: 'settings.opt.provider.default' },
        { v: 'redis', labelKey: 'settings.opt.provider.redis' },
      ] },
      { path: 'cache.ttlSeconds', k: 'ttlSeconds', type: 'number' },
      { path: 'cache.keyPrefix', k: 'keyPrefix', type: 'text' },
      // The Redis connection fields only matter when the provider is Redis, so the whole
      // card is hidden otherwise and carries a live connection Test.
      { type: 'group', g: 'redis', when: (f) => getAt(f, 'cache.provider') === 'redis', action: 'testCache' },
      { path: 'cache.redis.address', k: 'address', type: 'text', suggest: ['localhost:6379'] },
      { path: 'cache.redis.password', k: 'password', type: 'password' },
      { path: 'cache.redis.db', k: 'db', type: 'select', numeric: true, options: DB_OPTIONS },
      { path: 'cache.redis.useTls', k: 'useTls', type: 'checkbox' },
      { path: 'cache.redis.connectTimeoutMs', k: 'connectTimeout', type: 'number' },
      { path: 'cache.redis.operationTimeoutMs', k: 'operationTimeout', type: 'number' },
    ],
  },
  {
    id: 'logging', icon: 'list', tone: 'steel',
    fields: [
      { type: 'group', g: 'logging' },
      { path: 'logging.enabled', k: 'enabled', type: 'checkbox' },
      { path: 'logging.path', k: 'path', type: 'text', browse: 'file' },
      { path: 'logging.maxLineBytes', k: 'maxLineBytes', type: 'number' },
      { path: 'logging.maxFileSizeMb', k: 'maxFileSizeMb', type: 'number' },
      { path: 'logging.cleanup.enabled', k: 'cleanupEnabled', type: 'checkbox' },
      { path: 'logging.cleanup.maxRetentionDays', k: 'maxRetentionDays', type: 'number' },
      { path: 'logging.cleanup.frequencyMinutes', k: 'frequencyMinutes', type: 'number' },
      { type: 'group', g: 'apiLog' },
      { path: 'apiLog.cleanup.enabled', k: 'cleanupEnabled', type: 'checkbox' },
      { path: 'apiLog.cleanup.maxRetentionDays', k: 'maxRetentionDays', type: 'number' },
      { path: 'apiLog.cleanup.frequencyMinutes', k: 'frequencyMinutes', type: 'number' },
      { type: 'group', g: 'telemetry' },
      { path: 'telemetry.enabled', k: 'enabled', type: 'checkbox' },
      { path: 'telemetry.prometheus.enabled', k: 'prometheusEnabled', type: 'checkbox' },
      { path: 'telemetry.prometheus.metricsPath', k: 'metricsPath', type: 'text', suggest: ['/metrics'] },
      { path: 'telemetry.prometheus.apiDurationThresholdMs', k: 'apiDurationThreshold', type: 'number' },
    ],
  },
];

// --- nested value helpers (immutable) ---------------------------------------
function getAt(obj, path) {
  return path.split('.').reduce((o, k) => (o == null ? undefined : o[k]), obj);
}
function setAt(obj, path, value) {
  const parts = path.split('.');
  const root = { ...(obj || {}) };
  let cur = root;
  for (let i = 0; i < parts.length - 1; i += 1) {
    cur[parts[i]] = { ...(cur[parts[i]] || {}) };
    cur = cur[parts[i]];
  }
  cur[parts[parts.length - 1]] = value;
  return root;
}

export function SettingsPage({ session, onToast }) {
  const t = useT();
  const [active, setActive] = useState('localAuth');
  const [values, setValues] = useState({}); // section id -> loaded payload
  const [form, setForm] = useState(null); // active section's working copy
  const [loading, setLoading] = useState(true);
  const [busy, setBusy] = useState(false);
  const [restartNeeded, setRestartNeeded] = useState(false);
  const [restarting, setRestarting] = useState(false);

  const loadAll = useCallback(async () => {
    setLoading(true);
    const r = await api('/api/settings', { noRedirect: true }).catch(() => ({ ok: false }));
    if (r.ok && r.body && r.body.values) setValues(r.body.values);
    setLoading(false);
  }, []);
  useEffect(() => { loadAll(); }, [loadAll]);

  // Re-seed the working copy whenever the active tab or the loaded values change.
  useEffect(() => {
    if (active === 'system') { setForm(null); return; }
    setForm(values[active] ? JSON.parse(JSON.stringify(values[active])) : null);
  }, [active, values]);

  const save = useCallback(async () => {
    if (!form) return;
    setBusy(true);
    const r = await api(`/api/settings/${active}`, { method: 'PUT', body: JSON.stringify(form), noRedirect: true }).catch(() => ({ ok: false }));
    setBusy(false);
    if (!r.ok) {
      onToast && onToast(r.message || t('settings.saveFailed'), 'error');
      return;
    }
    // Refresh the masked view (so secret placeholders reflect the new "set" state).
    await loadAll();
    if (r.body && r.body.needsRestart) setRestartNeeded(true);
    onToast && onToast(t('settings.saved'), 'success');
  }, [active, form, loadAll, onToast, t]);

  const reset = useCallback(async () => {
    if (!window.confirm(t('settings.resetConfirm'))) return;
    setBusy(true);
    const r = await api(`/api/settings/${active}/reset`, { method: 'POST', noRedirect: true }).catch(() => ({ ok: false }));
    setBusy(false);
    if (!r.ok) {
      onToast && onToast(r.message || t('settings.saveFailed'), 'error');
      return;
    }
    await loadAll();
    if (r.body && r.body.needsRestart) setRestartNeeded(true);
    onToast && onToast(t('settings.resetDone'), 'success');
  }, [active, loadAll, onToast, t]);

  // restart POSTs the relaunch, then polls the public health endpoint until the fresh
  // process answers, then reloads the page.
  const restart = useCallback(async () => {
    setRestarting(true);
    onToast && onToast(t('settings.restarting'), 'info');
    await api('/api/system/restart', { method: 'POST', noRedirect: true }).catch(() => {});
    const deadline = Date.now() + 120000;
    const tick = async () => {
      if (Date.now() > deadline) { window.location.reload(); return; }
      try {
        const res = await fetch(`${apiBase()}/api/health`, { credentials: 'same-origin' });
        if (res.ok) { window.location.reload(); return; }
      } catch (_) { /* server still down */ }
      window.setTimeout(tick, 1500);
    };
    window.setTimeout(tick, 2500);
  }, [onToast, t]);

  if (!session?.isSuperadmin) {
    return <div className="settings-panel"><p className="settings-hint">{t('settings.superadminOnly')}</p></div>;
  }

  const tabs = SECTIONS.map((s) => ({ id: s.id, label: t(`settings.sec.${s.id}.title`) }))
    .concat([{ id: 'system', label: t('settings.sec.system.title') }]);
  const activeSection = SECTIONS.find((s) => s.id === active);
  // The Save button is enabled only when the working copy differs from what was loaded.
  // Both are the same masked shape (secrets blank + "<field>Set" flags), so a plain deep
  // compare is accurate: touching any field makes them differ, saving reloads and re-seeds
  // the form so it goes clean again.
  const dirty = form && values[active] ? JSON.stringify(form) !== JSON.stringify(values[active]) : false;

  return (
    <div className="settings-panel">
      <div className="settings-header">
        <h2>{t('settings.title')}</h2>
      </div>
      <p className="settings-hint">{t('settings.hint')}</p>

      {restartNeeded ? (
        <div className="settings-restart-banner" role="alert">
          <span><Ico n="warning" sz={16} /> {t('settings.restartRequired')}</span>
          <button type="button" className="settings-btn settings-btn-primary" onClick={restart} disabled={restarting}>
            <Ico n={restarting ? 'reload' : 'refresh'} sz={15} />
            <span>{restarting ? t('settings.restarting') : t('settings.restartNow')}</span>
          </button>
        </div>
      ) : null}

      <Tabs tabs={tabs} active={active} onChange={setActive} ariaLabel={t('settings.title')} />

      {loading ? (
        <div className="settings-loading"><Ico n="reload" sz={18} /><span>{t('common.loading')}</span></div>
      ) : active === 'system' ? (
        <SystemTab t={t} onRestart={restart} restarting={restarting} onToast={onToast} />
      ) : !form ? (
        <p className="settings-hint settings-hint--error">{t('settings.loadFailed')}</p>
      ) : (
        <div className="settings-body">
          <SectionHero
            section={activeSection}
            t={t}
            action={activeSection.action === 'importSso'
              ? <SsoImportButton form={form} setForm={setForm} t={t} onToast={onToast} />
              : null}
          />
          <div className="settings-cards">
            {toGroups(activeSection.fields, t).filter((g) => !g.when || g.when(form)).map((g, i) => (
              <section key={g.title || `g${i}`} className="settings-card">
                {g.action === 'testCache' ? (
                  <div className="settings-card-head">
                    <h3 className="settings-card-title settings-card-title--flush">{g.title}</h3>
                    <CacheTestButton form={form} t={t} onToast={onToast} />
                  </div>
                ) : g.title ? <h3 className="settings-card-title">{g.title}</h3> : null}
                <div className="settings-grid">
                  {g.fields.map((f) => <Field key={f.path} field={f} form={form} setForm={setForm} t={t} />)}
                </div>
              </section>
            ))}
          </div>
          <div className="settings-actions">
            <button type="button" className="settings-btn settings-btn-primary" onClick={save} disabled={busy || !dirty}>
              <Ico n="save" sz={15} /><span>{t('settings.save')}</span>
            </button>
            <button type="button" className="settings-btn" onClick={reset} disabled={busy}>
              <Ico n="reload" sz={15} /><span>{t('settings.reset')}</span>
            </button>
            {dirty ? <span className="settings-dirty">{t('settings.unsaved')}</span> : null}
          </div>
        </div>
      )}
    </div>
  );
}

// toGroups folds a flat field list into cards, opening a new card at each 'group' marker.
// Fields before the first marker land in an untitled lead card.
function toGroups(fields, t) {
  const groups = [];
  let cur = { title: null, fields: [] };
  for (const f of fields) {
    if (f.type === 'group') {
      if (cur.fields.length || cur.title) groups.push(cur);
      cur = { title: t(`settings.group.${f.g}`), when: f.when, action: f.action, fields: [] };
    } else {
      cur.fields.push(f);
    }
  }
  if (cur.fields.length || cur.title) groups.push(cur);
  return groups;
}

// SsoImportButton loads a myidsan-exported bundle into the SSO form. It deliberately
// stops at filling the fields: the operator sees exactly what changed, can correct it,
// and presses Save themselves. Nothing is sent to the server here, so the import needs
// no new endpoint and no new permission — it rides the existing settings save.
function SsoImportButton({ form, setForm, t, onToast }) {
  const inputRef = useRef(null);

  const apply = useCallback(async (file) => {
    if (!file) return;
    let bundle;
    try {
      bundle = JSON.parse(await file.text());
    } catch (_) {
      onToast && onToast(t('settings.sso.importBadJson'), 'error');
      return;
    }
    if (!bundle || bundle.kind !== SSO_BUNDLE_KIND) {
      onToast && onToast(t('settings.sso.importWrongKind'), 'error');
      return;
    }
    // A newer major bundle may carry fields this build cannot honour; refuse rather
    // than import a subset and let the operator believe it is complete.
    if (Number(bundle.version) > SSO_BUNDLE_MAX_VERSION) {
      onToast && onToast(t('settings.sso.importTooNew'), 'error');
      return;
    }
    const sso = bundle.sso || {};
    let next = form;
    const applied = [];
    for (const [leaf, path] of SSO_BUNDLE_FIELDS) {
      const value = sso[leaf];
      if (value === undefined || value === null || value === '') continue;
      if (String(getAt(next, path) ?? '') === String(value)) continue;
      next = setAt(next, path, value);
      applied.push(leaf);
    }
    if (!applied.length) {
      onToast && onToast(t('settings.sso.importNoChange'), 'info');
      return;
    }
    setForm(next);
    // The secret is the one field myidsan can only hand over at generation time, so
    // say plainly when the bundle did not carry one and the old value still stands.
    const withSecret = applied.includes('clientSecret');
    onToast && onToast(
      withSecret
        ? t('settings.sso.importedWithSecret', { n: applied.length })
        : t('settings.sso.importedNoSecret', { n: applied.length }),
      'success',
    );
  }, [form, setForm, onToast, t]);

  return (
    <>
      <input
        ref={inputRef}
        type="file"
        accept="application/json,.json"
        style={{ display: 'none' }}
        onChange={(e) => {
          const file = e.target.files && e.target.files[0];
          e.target.value = ''; // let the same file be picked again after a correction
          apply(file);
        }}
      />
      <button type="button" className="settings-btn" onClick={() => inputRef.current && inputRef.current.click()} title={t('settings.sso.importTip')}>
        <Ico n="upload" sz={15} /><span>{t('settings.sso.import')}</span>
      </button>
    </>
  );
}

// CacheTestButton pings Redis with the values currently in the form (a blank password
// falls back to the stored one, resolved server-side). It shows an inline result pill and
// toasts, so an operator can verify connectivity before saving.
function CacheTestButton({ form, t, onToast }) {
  const [state, setState] = useState({ status: 'idle', msg: '' });
  const run = useCallback(async () => {
    setState({ status: 'testing', msg: '' });
    const redis = getAt(form, 'cache.redis') || {};
    const r = await api('/api/settings/cache/test', { method: 'POST', body: JSON.stringify(redis), noRedirect: true })
      .catch(() => ({ ok: false }));
    if (r.ok) {
      setState({ status: 'ok', msg: '' });
      onToast && onToast(t('settings.cacheTest.okToast'), 'success');
    } else {
      const msg = r.message || t('settings.cacheTest.fail');
      setState({ status: 'fail', msg });
      onToast && onToast(t('settings.cacheTest.failToast', { msg }), 'error');
    }
  }, [form, onToast, t]);

  return (
    <div className="settings-cache-test">
      {state.status === 'ok' ? (
        <span className="settings-status-pill ok"><span className="settings-status-dot" />{t('settings.cacheTest.ok')}</span>
      ) : null}
      {state.status === 'fail' ? (
        <span className="settings-status-pill bad" title={state.msg}><span className="settings-status-dot" />{t('settings.cacheTest.fail')}</span>
      ) : null}
      <button type="button" className="settings-btn settings-btn-quiet" onClick={run} disabled={state.status === 'testing'}>
        <Ico n={state.status === 'testing' ? 'reload' : 'zap'} sz={14} />
        <span>{state.status === 'testing' ? t('settings.cacheTest.testing') : t('settings.cacheTest.test')}</span>
      </button>
    </div>
  );
}

// SectionHero is the tinted banner atop each section: an icon chip in the section's tone,
// the section title, and its one-line description.
function SectionHero({ section, t, action }) {
  return (
    <div className={`settings-hero tone-${section.tone}`}>
      <span className="settings-hero-ico"><Ico n={section.icon} sz={22} /></span>
      <div className="settings-hero-text">
        <h3>{t(`settings.sec.${section.id}.title`)}</h3>
        <p>{t(`settings.sec.${section.id}.desc`)}</p>
      </div>
      {action ? <div className="settings-hero-action">{action}</div> : null}
    </div>
  );
}

// InfoTip is an (i) affordance whose hint appears on hover/focus. It keeps the field's
// helper text OUT of the layout flow, so every input is the same height and rows align.
function InfoTip({ text }) {
  return (
    <span className="settings-info">
      <button type="button" className="settings-info-btn" aria-label={text}>
        <Ico n="info" sz={13} />
      </button>
      <span className="settings-info-bubble" role="tooltip">{text}</span>
    </span>
  );
}

function FieldLabel({ text, info }) {
  return (
    <span className="settings-field-label">
      <span className="settings-field-label-text">{text}</span>
      {info ? <InfoTip text={info} /> : null}
    </span>
  );
}

function Field({ field, form, setForm, t }) {
  const val = getAt(form, field.path);
  const label = t(`settings.field.${field.k}`);
  // Per-field help lives at settings.info.<path>. t() echoes the key when absent, so an
  // unmatched key means "no help". Secret fields also carry the keep-blank note.
  const helpKey = `settings.info.${field.path}`;
  let help = t(helpKey);
  if (help === helpKey) help = '';
  const info = field.type === 'password'
    ? [help, t('settings.secretHint')].filter(Boolean).join(' ')
    : help;
  const onChange = (v) => setForm(setAt(form, field.path, v));

  if (field.type === 'checkbox') {
    return (
      <label className={`settings-toggle${val ? ' on' : ''}`}>
        <input type="checkbox" checked={!!val} onChange={(e) => onChange(e.target.checked)} />
        <span className="settings-toggle-track" aria-hidden="true"><span className="settings-toggle-thumb" /></span>
        <span className="settings-toggle-label">{label}</span>
        {info ? <InfoTip text={info} /> : null}
      </label>
    );
  }
  if (field.type === 'password') {
    const isSet = !!getAt(form, `${field.path}Set`);
    return (
      <div className="settings-field">
        <FieldLabel text={label} info={info} />
        <input
          type="password"
          value={val || ''}
          placeholder={isSet ? '••••••••' : t('settings.notSet')}
          autoComplete="new-password"
          onChange={(e) => onChange(e.target.value)}
        />
      </div>
    );
  }
  if (field.type === 'select') {
    return (
      <div className="settings-field">
        <FieldLabel text={label} info={info} />
        <select value={val == null ? '' : String(val)} onChange={(e) => onChange(field.numeric ? Number(e.target.value) : e.target.value)}>
          {(field.options || []).map((o) => <option key={o.v} value={o.v}>{o.label != null ? o.label : t(o.labelKey)}</option>)}
        </select>
      </div>
    );
  }
  if (field.browse) {
    return <BrowseField field={field} value={val} onChange={onChange} label={label} info={info} t={t} />;
  }
  if (field.type === 'textarea') {
    return (
      <div className="settings-field settings-field-wide">
        <FieldLabel text={label} info={info} />
        <textarea value={val || ''} rows={3} onChange={(e) => onChange(e.target.value)} />
      </div>
    );
  }
  const listId = field.suggest ? `sug-${field.path}` : undefined;
  const datalist = field.suggest ? (
    <datalist id={listId}>{field.suggest.map((s) => <option key={s} value={s} />)}</datalist>
  ) : null;
  if (field.type === 'number') {
    return (
      <div className="settings-field">
        <FieldLabel text={label} info={info} />
        <input
          type="number"
          list={listId}
          value={val === undefined || val === null ? '' : val}
          onChange={(e) => onChange(e.target.value === '' ? 0 : Number(e.target.value))}
        />
        {datalist}
      </div>
    );
  }
  return (
    <div className="settings-field">
      <FieldLabel text={label} info={info} />
      <input type="text" list={listId} value={val || ''} onChange={(e) => onChange(e.target.value)} />
      {datalist}
    </div>
  );
}

// BrowseField is a path input paired with a Browse button that opens the server-side
// file/folder picker (field.browse is 'file' or 'dir'), so an operator selects a path
// instead of typing it. The text input stays editable for paths that don't exist yet.
function BrowseField({ field, value, onChange, label, info, t }) {
  const [open, setOpen] = useState(false);
  return (
    <div className="settings-field">
      <FieldLabel text={label} info={info} />
      <div className="settings-browse">
        <input type="text" value={value || ''} onChange={(e) => onChange(e.target.value)} />
        <button type="button" className="settings-btn settings-btn-quiet" onClick={() => setOpen(true)}>
          <Ico n="folder" sz={14} /><span>{t('settings.browse')}</span>
        </button>
      </div>
      {open ? (
        <FileBrowserModal
          mode={field.browse}
          initialPath={value}
          t={t}
          onSelect={(p) => { onChange(p); setOpen(false); }}
          onClose={() => setOpen(false)}
        />
      ) : null}
    </div>
  );
}

// FileBrowserModal is a server-side directory picker (GET /api/settings/fs/browse). It
// drills into folders; in 'file' mode clicking a file selects it, and in 'dir' mode a
// "Choose this folder" action selects the current directory. Confined server-side to the
// whitelisted roots.
function FileBrowserModal({ mode, initialPath, onSelect, onClose, t }) {
  const [listing, setListing] = useState(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');

  const browse = useCallback(async (path) => {
    setLoading(true);
    setError('');
    const r = await api(`/api/settings/fs/browse?path=${encodeURIComponent(path || '')}`, { noRedirect: true }).catch(() => ({ ok: false }));
    if (r.ok && r.body) setListing(r.body);
    else setError(r.message || t('settings.fb.error'));
    setLoading(false);
  }, [t]);
  useEffect(() => { browse(initialPath || ''); }, [browse, initialPath]);

  const entries = listing?.entries || [];
  return (
    <div className="settings-fb-backdrop" role="dialog" aria-modal="true" onClick={onClose}>
      <div className="settings-fb" onClick={(e) => e.stopPropagation()}>
        <div className="settings-fb-head">
          <h3><Ico n="folder" sz={16} /> {t(mode === 'dir' ? 'settings.fb.titleDir' : 'settings.fb.titleFile')}</h3>
          <button type="button" className="settings-fb-close" aria-label={t('settings.fb.close')} onClick={onClose}><Ico n="x" sz={16} /></button>
        </div>
        <div className="settings-fb-path" title={listing?.path || ''}>{listing?.path || t('settings.fb.roots')}</div>
        <div className="settings-fb-list">
          {loading ? (
            <div className="settings-loading"><Ico n="reload" sz={16} /><span>{t('common.loading')}</span></div>
          ) : error ? (
            <p className="settings-field-note">{error}</p>
          ) : (
            <ul>
              {listing && listing.parent !== undefined ? (
                <li>
                  <button type="button" className="settings-fb-row" onClick={() => browse(listing.parent)} disabled={!listing.path}>
                    <Ico n="undo" sz={15} /><span>..</span>
                  </button>
                </li>
              ) : null}
              {entries.map((it) => (
                <li key={it.path}>
                  <button type="button" className={`settings-fb-row${it.dir ? ' dir' : ''}`} onClick={() => (it.dir ? browse(it.path) : onSelect(it.path))}>
                    <Ico n={it.dir ? 'folder' : 'copy'} sz={15} /><span>{it.name}</span>
                  </button>
                </li>
              ))}
              {entries.length === 0 ? <li className="settings-fb-empty">{t('settings.fb.empty')}</li> : null}
            </ul>
          )}
        </div>
        <div className="settings-fb-foot">
          {mode === 'dir' ? (
            <button type="button" className="settings-btn settings-btn-primary" disabled={!listing?.path} onClick={() => onSelect(listing.path)}>
              <Ico n="folder" sz={14} /><span>{t('settings.fb.chooseFolder')}</span>
            </button>
          ) : null}
          <button type="button" className="settings-btn" onClick={onClose}>{t('settings.fb.cancel')}</button>
        </div>
      </div>
    </div>
  );
}

// SystemTab shows the running build and live service health by polling the public
// version/health/liveness/readiness endpoints (mirrors mymatasan's System panel), plus the
// process-restart control. Read-only except for Restart.
function SystemTab({ t, onRestart, restarting, onToast }) {
  const [ver, setVer] = useState(null);       // /api/version body, or null
  const [api200, setApi200] = useState(null); // /api/health reachable
  const [live, setLive] = useState(null);     // /health liveness
  const [ready, setReady] = useState(null);   // /ready body {ok, db, cache, …}
  const [busy, setBusy] = useState(false);
  const [checkedAt, setCheckedAt] = useState(null);

  const load = useCallback(async () => {
    setBusy(true);
    const probe = (p) => api(p, { noRedirect: true }).catch(() => ({ ok: false, body: null }));
    const [v, h, l, r] = await Promise.all([
      probe('/api/version'), probe('/api/health'), probe('/health'), probe('/ready'),
    ]);
    setVer(v.ok && v.body && typeof v.body === 'object' ? v.body : null);
    setApi200(!!h.ok);
    setLive(!!l.ok);
    setReady(r.ok && r.body && typeof r.body === 'object' ? r.body : null);
    setCheckedAt(new Date());
    setBusy(false);
  }, []);
  useEffect(() => { load(); }, [load]);

  // Advisory readiness keys beyond the ok verdict (db, cache, …) so operators see detail.
  const readyExtras = ready ? Object.entries(ready).filter(([k]) => k !== 'ok') : [];

  return (
    <div className="settings-body">
      <div className="settings-hero tone-steel">
        <span className="settings-hero-ico"><Ico n="monitor" sz={22} /></span>
        <div className="settings-hero-text">
          <h3>{t('settings.sec.system.title')}</h3>
          <p>{t('settings.sec.system.desc')}</p>
        </div>
      </div>

      <div className="settings-cards">
        {/* Software version */}
        <section className="settings-card">
          <div className="settings-card-head">
            <h3 className="settings-card-title settings-card-title--flush">{t('settings.system.software')}</h3>
            <button type="button" className="settings-btn settings-btn-quiet" onClick={load} disabled={busy}>
              <Ico n="reload" sz={14} /><span>{t('settings.system.refresh')}</span>
            </button>
          </div>
          {ver ? (
            <div className="settings-metrics">
              <MetricTile label={t('settings.system.application')} value={ver.app || '—'} sub={ver.appVersion ? `v${ver.appVersion}` : null} />
              <MetricTile label={t('settings.system.core')} value={ver.coreVersion ? `v${ver.coreVersion}` : '—'} />
              {ver.commit ? <MetricTile label={t('settings.system.commit')} value={String(ver.commit).slice(0, 12)} mono /> : null}
              {ver.updatedAt ? <MetricTile label={t('settings.system.built')} value={String(ver.updatedAt)} /> : null}
            </div>
          ) : (
            <p className="settings-field-note">{t('settings.system.versionUnavailable')}</p>
          )}
          {checkedAt ? <div className="settings-checked-at">{t('settings.system.checkedAt', { time: checkedAt.toLocaleTimeString() })}</div> : null}
        </section>

        {/* Service health */}
        <section className="settings-card">
          <h3 className="settings-card-title">{t('settings.system.health')}</h3>
          <div className="settings-health">
            <HealthRow label={t('settings.system.api')} ok={api200} t={t} />
            <HealthRow label={t('settings.system.liveness')} ok={live} t={t} />
            <HealthRow label={t('settings.system.readiness')} ok={!!ready?.ok} t={t} />
            {readyExtras.map(([k, v]) => (
              <HealthRow key={k} label={k} ok={v === true || v === 'ok' || v === 'up'} note={typeof v === 'string' ? v : null} t={t} />
            ))}
          </div>
        </section>

        {/* Restart */}
        <section className="settings-card settings-system-row">
          <div className="settings-system-meta">
            <span className="settings-system-ico"><Ico n="refresh" sz={18} /></span>
            <div>
              <div className="settings-system-label">{t('settings.system.restart')}</div>
              <div className="settings-field-note">{t('settings.system.restartDesc')}</div>
            </div>
          </div>
          <button type="button" className="settings-btn settings-btn-primary" onClick={onRestart} disabled={restarting}>
            <Ico n={restarting ? 'reload' : 'refresh'} sz={15} />
            <span>{restarting ? t('settings.restarting') : t('settings.restartNow')}</span>
          </button>
        </section>

        {/* Factory reset. Renders nothing unless bootstrap.allowReset is on, which
            myseliasan ships false, so an install that never opted in shows no button. */}
        <FactoryResetSection api={api} appLabel="MySeliaSan" onToast={onToast} />
      </div>
    </div>
  );
}

function MetricTile({ label, value, sub, mono }) {
  return (
    <div className="settings-metric">
      <div className="settings-metric-label">{label}</div>
      <div className={`settings-metric-value${mono ? ' mono' : ''}`}>{value}</div>
      {sub ? <div className="settings-metric-sub">{sub}</div> : null}
    </div>
  );
}

function HealthRow({ label, ok, note, t }) {
  return (
    <div className="settings-health-row">
      <span className="settings-health-label">{label}</span>
      <span className={`settings-status-pill ${ok ? 'ok' : 'bad'}`}>
        <span className="settings-status-dot" />
        {note || (ok ? t('settings.system.online') : t('settings.system.offline'))}
      </span>
    </div>
  );
}
