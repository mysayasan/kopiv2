import { useEffect, useState } from 'react';
import './styles/app.css';
import './styles/controlplane.css';
import { TopBar } from './components/layout';
import { ToastStack } from './components/ui';
import { DashboardTab } from './components/dashboard';
import { NodesTab } from './components/nodes';
import { Ico } from './components/icons';
import { api } from './lib/helpers';

const THEME_KEY = 'myseliasan_theme';

export default function App() {
  const [theme, setTheme] = useState(() => {
    try { return localStorage.getItem(THEME_KEY) || 'light'; } catch (_) { return 'light'; }
  });
  useEffect(() => {
    const root = document.documentElement;
    ['light', 'dark'].forEach((t) => root.classList.remove(`theme-${t}`));
    root.classList.add(`theme-${theme}`);
  }, [theme]);
  function changeTheme(t) {
    setTheme(t);
    try { localStorage.setItem(THEME_KEY, t); } catch (_) {}
  }

  const [session, setSession] = useState(null);
  const [activeTab, setActiveTab] = useState('dashboard');
  const [busy, setBusy] = useState(false);
  const [toasts, setToasts] = useState([]);

  function pushToast(text) {
    const id = `${Date.now()}-${Math.random().toString(16).slice(2)}`;
    setToasts((list) => [{ id, text }, ...list].slice(0, 5));
  }

  async function loadSession() {
    setBusy(true);
    const r = await api('/api/session/me').catch(() => ({ ok: false }));
    setBusy(false);
    if (r.ok) setSession(r.body);
  }
  useEffect(() => { loadSession(); /* eslint-disable-next-line */ }, []);

  async function logout() {
    await api('/api/auth/logout', { method: 'POST' }).catch(() => {});
    window.location.href = '/api/auth/start';
  }

  return (
    <main className="app-shell">
      <TopBar
        activeTab={activeTab}
        busy={busy}
        onTab={setActiveTab}
        onRefresh={loadSession}
        onLogout={logout}
        theme={theme}
        onThemeChange={changeTheme}
        session={session}
      />
      <ToastStack toasts={toasts} onDismiss={(id) => setToasts((list) => list.filter((t) => t.id !== id))} />

      {activeTab === 'dashboard' ? <DashboardTab session={session} /> : null}
      {activeTab === 'nodes' ? <NodesTab onToast={pushToast} /> : null}
      {activeTab === 'authorization' ? (
        <section className="workspace">
          <section className="settings-panel span-two">
            <header><h2><span className="btn-icon"><Ico n="user" /> Authorization</span></h2></header>
            <p className="settings-hint">Federated authorization is managed through myidsan.</p>
          </section>
        </section>
      ) : null}
    </main>
  );
}
