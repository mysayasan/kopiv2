import { useEffect, useState } from 'react';
import './styles/app.css';
import './styles/controlplane.css';
import './styles/rbac-standard.css';
import { SideNav } from './components/layout';
import { ToastStack, FormBusyOverlay } from './components/ui';
import { DashboardTab } from './components/dashboard';
import { NodesTab } from './components/nodes';
import { UsersPage, RolesPage, RbacPage } from './components/rbac_admin';
import { LoginScreen, ChangePasswordScreen } from './components/auth_screens';
import { api, sessionCanGet } from './lib/helpers';

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

  // authState: 'loading' | 'anon' | 'mustchange' | 'ready'
  const [authState, setAuthState] = useState('loading');
  const [session, setSession] = useState(null);
  const [activeTab, setActiveTab] = useState('dashboard');
  const [toasts, setToasts] = useState([]);

  function pushToast(text) {
    const id = `${Date.now()}-${Math.random().toString(16).slice(2)}`;
    setToasts((list) => [{ id, text }, ...list].slice(0, 5));
  }

  async function loadSession() {
    const r = await api('/api/session/me', { noRedirect: true }).catch(() => ({ ok: false }));
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
    await api('/api/auth/logout', { method: 'POST', noRedirect: true }).catch(() => {});
    setSession(null);
    setAuthState('anon');
  }

  if (authState === 'loading') {
    return <main className="boot-screen"><FormBusyOverlay busy /></main>;
  }
  if (authState === 'anon') {
    return <LoginScreen onLoggedIn={loadSession} />;
  }
  if (authState === 'mustchange') {
    return <ChangePasswordScreen onDone={loadSession} onToast={pushToast} onLogout={logout} />;
  }

  // Demote to the dashboard if the active tab is no longer permitted (e.g. after a
  // handoff that retired the current stock account, or a role that lost node access).
  const canNodes = sessionCanGet(session, '/api/nodes');
  const adminTabs = ['users', 'roles', 'rbac'];
  if (adminTabs.includes(activeTab) && !session?.isSuperadmin) setActiveTab('dashboard');
  if (activeTab === 'nodes' && !canNodes) setActiveTab('dashboard');

  return (
    <div className="app-shell">
      <SideNav
        activeTab={activeTab}
        busy={false}
        onTab={setActiveTab}
        onLogout={logout}
        theme={theme}
        onThemeChange={changeTheme}
        session={session}
      />
      <main className="main-workspace">
        {session?.superadminHandoffPending ? (
          <div className="handoff-banner" role="alert">
            <span className="handoff-banner-text">
              A default <strong>stock superadmin</strong> account is still active. Open <strong>Users</strong> and disable it to finish securing the control plane.
            </span>
            {session?.isSuperadmin ? <button type="button" className="handoff-banner-action" onClick={() => setActiveTab('users')}>Go to Users</button> : null}
          </div>
        ) : null}
        <ToastStack toasts={toasts} onDismiss={(id) => setToasts((list) => list.filter((t) => t.id !== id))} />

        {activeTab === 'dashboard' ? <DashboardTab session={session} /> : null}
        {activeTab === 'nodes' && canNodes ? <NodesTab onToast={pushToast} /> : null}
        {activeTab === 'users' && session?.isSuperadmin ? (
          <UsersPage session={session} onToast={pushToast} onSessionChanged={loadSession} />
        ) : null}
        {activeTab === 'roles' && session?.isSuperadmin ? <RolesPage onToast={pushToast} /> : null}
        {activeTab === 'rbac' && session?.isSuperadmin ? <RbacPage onToast={pushToast} /> : null}
      </main>
    </div>
  );
}
