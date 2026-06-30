import { useEffect, useState } from 'react';
import './styles/app.css';
import './styles/controlplane.css';
import './styles/rbac-standard.css';
import { SideNav } from './components/layout';
import { ToastStack, LangProvider, normalizeLang, useT, LanguageDropdown, AppFooter } from '@shared';
import { FormBusyOverlay } from './components/ui';
import { DashboardTab } from './components/dashboard';
import { NodesTab } from './components/nodes';
import { UsersPage, RolesPage, RbacPage } from './components/rbac_admin';
import { LoginScreen, ChangePasswordScreen, PendingClearanceScreen } from './components/auth_screens';
import { api, sessionCanGet, apiBase } from './lib/helpers';
import { messages as appMessages } from './i18n';

const THEME_KEY = 'myseliasan_theme';

function AppInner({ lang, onLangChange }) {
  const t = useT();
  const [theme, setTheme] = useState(() => {
    try { return localStorage.getItem(THEME_KEY) || 'light'; } catch (_) { return 'light'; }
  });
  useEffect(() => {
    const root = document.documentElement;
    ['light', 'dark', 'contrast'].forEach((t) => root.classList.remove(`theme-${t}`));
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
  // Fleet state is lifted here so the side-nav tree and the Nodes page stay in sync:
  // the tree lists adopted nodes and `managingNodeId` selects which one the page opens.
  const [nodes, setNodes] = useState([]);
  const [managingNodeId, setManagingNodeId] = useState(null);

  async function loadNodes() {
    if (!sessionCanGet(session, '/api/nodes')) { setNodes([]); return; }
    const r = await api('/api/nodes').catch(() => ({ ok: false }));
    if (r.ok) setNodes(Array.isArray(r.body) ? r.body : []);
  }
  useEffect(() => {
    if (authState === 'ready') loadNodes();
    // eslint-disable-next-line
  }, [authState, session]);

  // selectNode drives both nav surfaces: null opens the fleet list/management page,
  // a nodeId jumps straight to that node's manage view.
  function selectNode(nodeId) {
    setManagingNodeId(nodeId);
    setActiveTab('nodes');
  }
  // Leaving the Nodes section clears the managed node so returning lands on the list.
  function selectTab(id) {
    if (id !== 'nodes') setManagingNodeId(null);
    setActiveTab(id);
  }

  function pushToast(text, kind = 'info') {
    if (!text) return;
    const id = `${Date.now()}-${Math.random().toString(16).slice(2)}`;
    setToasts((list) => [{ id, text, kind }, ...list].slice(0, 5));
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
  // Authenticated but no role assigned yet — gate the whole control plane behind a
  // clearance screen until a superadmin grants a role.
  if (session?.pending && !session?.isSuperadmin) {
    return <PendingClearanceScreen email={session?.email} onRefresh={loadSession} onLogout={logout} />;
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
        onTab={selectTab}
        onLogout={logout}
        theme={theme}
        onThemeChange={changeTheme}
        session={session}
        nodes={nodes}
        managingNodeId={managingNodeId}
        onSelectNode={selectNode}
      />
      <main className="main-workspace">
        <div className="shared-lang-bar"><LanguageDropdown lang={lang} onLang={onLangChange} /></div>
        {session?.superadminHandoffPending ? (
          <div className="handoff-banner" role="alert">
            <span className="handoff-banner-text">{t('handoff.text')}</span>
            {session?.isSuperadmin ? <button type="button" className="handoff-banner-action" onClick={() => setActiveTab('users')}>{t('handoff.goToUsers')}</button> : null}
          </div>
        ) : null}
        <ToastStack toasts={toasts} onDismiss={(id) => setToasts((list) => list.filter((t) => t.id !== id))} />

        {activeTab === 'dashboard' ? <DashboardTab session={session} /> : null}
        {activeTab === 'nodes' && canNodes ? (
          <NodesTab
            onToast={pushToast}
            nodes={nodes}
            reloadNodes={loadNodes}
            managingNodeId={managingNodeId}
            onManage={selectNode}
            onBack={() => setManagingNodeId(null)}
          />
        ) : null}
        {activeTab === 'users' && session?.isSuperadmin ? (
          <UsersPage session={session} onToast={pushToast} onSessionChanged={loadSession} />
        ) : null}
        {activeTab === 'roles' && session?.isSuperadmin ? <RolesPage onToast={pushToast} /> : null}
        {activeTab === 'rbac' && session?.isSuperadmin ? <RbacPage onToast={pushToast} /> : null}
        <AppFooter appName="MySeliaSan" apiBase={apiBase()} />
      </main>
    </div>
  );
}

const LANG_KEY = 'myseliasan_lang';

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
