import { useEffect, useState } from 'react';
import './styles/app.css';
import './styles/rbac-standard.css';
import './styles/iot.css';
import { SideNav } from './components/layout';
import { ToastStack, LangProvider, normalizeLang, LanguageDropdown, AppFooter } from '@shared';
import { FormBusyOverlay, ThemeDropdown } from './components/ui';
import { DashboardPage, DevicesPage, AlertsPage, SettingsPage } from './components/pages';
import { LoginScreen, ChangePasswordScreen } from './components/auth_screens';
import { api, apiBase } from './lib/helpers';
import { messages as appMessages } from './i18n';

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

        {activeTab === 'dashboard' ? <DashboardPage /> : null}
        {activeTab === 'devices' ? <DevicesPage /> : null}
        {activeTab === 'alerts' ? <AlertsPage /> : null}
        {activeTab === 'settings' ? <SettingsPage /> : null}
        <AppFooter appName="MyIotSan" apiBase={apiBase()} />
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
  function changeLang(l) {
    setLang(l);
    try { localStorage.setItem(LANG_KEY, l); } catch (_) {}
  }
  return (
    <LangProvider lang={lang} messages={appMessages}>
      <AppInner lang={lang} onLangChange={changeLang} />
    </LangProvider>
  );
}
