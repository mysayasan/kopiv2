import { useEffect, useState } from 'react';
import './styles/app.css';
import './styles/rbac-standard.css';
import './styles/iot.css';
import { SideNav } from './components/layout';
import { ToastStack, LangProvider, normalizeLang, LanguageDropdown, AppFooter } from '@shared';
import { FormBusyOverlay, ThemeDropdown } from './components/ui';
import { DashboardPage, DevicesHome, RulesPage, AlertsPage, NotificationsPage, ProfilesPage, ScenesPage, SchedulesPage, FlowsPage, KbPage, SettingsPage } from './components/pages';
import { FirstRunWizard } from './components/onboarding';
import { LoginScreen, ChangePasswordScreen } from './components/auth_screens';
import { api, apiBase } from './lib/helpers';
import { enBundle, loadLocaleDict } from './i18n';

const THEME_KEY = 'myiotsan_theme';
const NAV_PIN_KEY = 'myiotsan_nav_pinned';

function AppInner({ lang, onLangChange }) {
  const [theme, setTheme] = useState(() => {
    try { return localStorage.getItem(THEME_KEY) || 'light'; } catch (_) { return 'light'; }
  });
  useEffect(() => {
    const root = document.documentElement;
    ['light', 'dark', 'contrast'].forEach((name) => root.classList.remove(`theme-${name}`));
    root.classList.add(`theme-${theme}`);
  }, [theme]);
  function changeTheme(next) {
    setTheme(next);
    try { localStorage.setItem(THEME_KEY, next); } catch (_) {}
  }

  // Side-nav display mode: pinned (default, always in-flow) vs auto-hide (collapses to
  // a slim hover edge and slides in on hover). Persisted like the theme.
  const [navPinned, setNavPinned] = useState(() => {
    try { return localStorage.getItem(NAV_PIN_KEY) !== 'false'; } catch (_) { return true; }
  });
  function toggleNavPinned() {
    setNavPinned((p) => {
      const next = !p;
      try { localStorage.setItem(NAV_PIN_KEY, String(next)); } catch (_) {}
      return next;
    });
  }

  // authState: 'loading' | 'anon' | 'mustchange' | 'ready'
  const [authState, setAuthState] = useState('loading');
  const [session, setSession] = useState(null);
  const [activeTab, setActiveTab] = useState('dashboard');
  // Which sub-tab the Devices page shows: 'inventory' (list + manual add) or 'discover' (scan +
  // enrollment + adopt). Lifted here so the first-run wizard can send the user straight to Discover.
  const [devicesSub, setDevicesSub] = useState('inventory');
  const [toasts, setToasts] = useState([]);

  function pushToast(text, kind = 'info') {
    if (!text) return;
    const id = `${Date.now()}-${Math.random().toString(16).slice(2)}`;
    setToasts((list) => [{ id, text, kind }, ...list].slice(0, 5));
  }

  async function loadSession() {
    const r = await api('/api/auth/session').catch(() => ({ ok: false }));
    if (r.ok && r.body) {
      setSession(r.body);
      setAuthState(r.body.mustChangePassword ? 'mustchange' : 'ready');
    } else {
      setSession(null);
      setAuthState('anon');
    }
  }
  useEffect(() => { loadSession(); /* eslint-disable-next-line */ }, []);

  async function logout() {
    await api('/api/auth/logout', { method: 'POST' }).catch(() => {});
    setSession(null);
    setAuthState('anon');
  }

  // The first-run wizard, and the two honest conditions it turns on:
  //
  //   - the estate is EMPTY. Not "the user is new" — a hub with devices in it does not need
  //     to be told what a device is, whoever is looking at it.
  //   - the viewer is an ADMIN. The path it points at (open an enrollment window) is
  //     admin-only, so offering it to an operator would be walking them to a locked door.
  //
  // It asks the server how many devices exist rather than assuming; an empty answer is the
  // only thing that opens it.
  // Dismissal is a property of the INSTALL, not of a browser, so it lives server-side in
  // the shared `setup.state` row (the same contract the other three apps use). It used to
  // be a localStorage key, which meant the same admin met the wizard again from a second
  // machine — or after clearing site data — on a hub that had been running for months.
  const [wizardDismissed, setWizardDismissed] = useState(true);
  const [estateEmpty, setEstateEmpty] = useState(false);
  useEffect(() => {
    if (authState !== 'ready' || !session?.isAdmin) return undefined;
    let cancelled = false;
    (async () => {
      const [state, devices] = await Promise.all([
        api('/api/setup/state').catch(() => ({ ok: false })),
        api('/api/devices?limit=1').catch(() => ({ ok: false })),
      ]);
      if (cancelled) return;
      // A failed probe leaves the wizard dismissed: an unreachable endpoint must not
      // start throwing a first-run dialog at an established hub.
      setWizardDismissed(!state.ok || !!state.body?.completed);
      if (devices.ok) setEstateEmpty((devices.body?.total || 0) === 0);
    })();
    return () => { cancelled = true; };
  }, [authState, session]);

  async function dismissWizard() {
    setWizardDismissed(true);
    await api('/api/setup/complete', { method: 'POST' }).catch(() => {});
  }
  const showWizard = authState === 'ready' && !!session?.isAdmin && estateEmpty && !wizardDismissed;

  if (authState === 'loading') {
    return <main className="boot-screen"><FormBusyOverlay busy /></main>;
  }
  // The pre-app screens take the language switcher too — it lives in the workspace
  // header, which the user cannot reach until they are signed in.
  if (authState === 'anon') {
    return <LoginScreen onLoggedIn={loadSession} lang={lang} onLangChange={onLangChange} />;
  }
  if (authState === 'mustchange') {
    return <ChangePasswordScreen onDone={loadSession} onToast={pushToast} onLogout={logout} lang={lang} onLangChange={onLangChange} />;
  }

  return (
    <div className={`app-shell${navPinned ? '' : ' nav-autohide'}`}>
      <SideNav
        activeTab={activeTab}
        busy={false}
        onTab={setActiveTab}
        onLogout={logout}
        session={session}
        pinned={navPinned}
        onTogglePinned={toggleNavPinned}
      />
      <main className="main-workspace">
        <div className="shared-lang-bar">
          <LanguageDropdown lang={lang} onLang={onLangChange} />
          <ThemeDropdown theme={theme} onThemeChange={changeTheme} />
        </div>
        <ToastStack toasts={toasts} onDismiss={(id) => setToasts((list) => list.filter((x) => x.id !== id))} />

        {activeTab === 'dashboard' ? <DashboardPage onNavigate={setActiveTab} /> : null}
        {/* The devices page takes the session because its Control tab hides the command
            controls from non-admins. That is UX only — the server 403s a non-admin POST to
            /api/devices/{id}/commands, which is the actual enforcement. */}
        {activeTab === 'devices' ? <DevicesHome onToast={pushToast} session={session} sub={devicesSub} onSub={setDevicesSub} /> : null}
        {activeTab === 'rules' ? <RulesPage onToast={pushToast} /> : null}
        {activeTab === 'alerts' ? <AlertsPage onToast={pushToast} /> : null}
        {activeTab === 'notifications' ? <NotificationsPage onToast={pushToast} /> : null}
        {activeTab === 'profiles' ? <ProfilesPage onToast={pushToast} /> : null}
        {/* The knowledge base is read-only reference content, readable by anyone signed in. */}
        {activeTab === 'help' ? <KbPage onToast={pushToast} /> : null}
        {/* Scenes are readable by anyone; RUNNING one is admin-only (the server 403s a non-admin
            POST to /run, and ScenesPage hides the Run button off session.isAdmin). */}
        {activeTab === 'scenes' ? <ScenesPage onToast={pushToast} session={session} /> : null}
        {/* Schedules readable by anyone; authoring/test-firing/location are admin-only server-side. */}
        {activeTab === 'schedules' ? <SchedulesPage onToast={pushToast} session={session} /> : null}
        {/* Flows are admin-only in full (a flow can run JS and actuate): every /api/flows route is
            administrator-gated server-side; hiding the tab from non-admins is UX on top of that. */}
        {activeTab === 'flows' && session?.isAdmin ? <FlowsPage onToast={pushToast} session={session} /> : null}
        {/* Settings is admin-only too — every /api/settings/*, /api/system and /api/pairing route
            is administrator-gated server-side; hiding the tab is UX on top of that. */}
        {activeTab === 'settings' && session?.isAdmin ? <SettingsPage onToast={pushToast} /> : null}
        <AppFooter appName="MyIotSan" apiBase={apiBase()} />

        {showWizard ? (
          <FirstRunWizard
            onDismiss={dismissWizard}
            onGoDiscovery={() => { dismissWizard(); setDevicesSub('discover'); setActiveTab('devices'); }}
          />
        ) : null}
      </main>
    </div>
  );
}

const LANG_KEY = 'myiotsan_lang';

// App owns the active locale and wraps the tree in the shared LangProvider so every
// shared component (SideNav, DataTable, ToastStack) translates. The locale persists in
// localStorage, mirroring the theme; default is the browser language → English.
export default function App() {
  const [lang, setLang] = useState(() => {
    try { return normalizeLang(localStorage.getItem(LANG_KEY) || navigator.language); } catch (_) { return 'en'; }
  });
  // English is always present (it is every key's fallback and must be there on first paint); other
  // locales are fetched on demand — see ./i18n — and accumulated here so a switch back is instant.
  const [appMessages, setAppMessages] = useState(enBundle);
  // A returning non-English user must not flash English app strings, so gate the first paint until
  // their locale chunk has loaded. English users never wait.
  const [langReady, setLangReady] = useState(lang === 'en');

  useEffect(() => {
    let alive = true;
    if (lang === 'en' || appMessages[lang]) { setLangReady(true); return undefined; }
    loadLocaleDict(lang).then((dict) => {
      if (!alive) return;
      if (dict) setAppMessages((prev) => ({ ...prev, [lang]: dict }));
      setLangReady(true);
    });
    return () => { alive = false; };
    // appMessages intentionally omitted: including it would re-run the effect after the very
    // setState it triggers. The `appMessages[lang]` guard above already handles a loaded locale.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [lang]);

  async function changeLang(l) {
    try { localStorage.setItem(LANG_KEY, l); } catch (_) {}
    // Load the locale's chunk BEFORE switching, so the UI never flashes English on the way to the
    // new language. For English or an already-loaded locale this resolves immediately.
    if (l !== 'en' && !appMessages[l]) {
      const dict = await loadLocaleDict(l);
      if (dict) setAppMessages((prev) => ({ ...prev, [l]: dict }));
    }
    setLang(l);
  }

  if (!langReady) {
    // Brief, and only for a returning non-English user on cold load.
    return <main className="boot-screen" />;
  }
  return (
    <LangProvider lang={lang} messages={appMessages}>
      <AppInner lang={lang} onLangChange={changeLang} />
    </LangProvider>
  );
}
