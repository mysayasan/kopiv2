import { useEffect, useState } from 'react';
import './styles/app.css';
import './styles/controlplane.css';
import './styles/rbac-standard.css';
import './styles/node-dashboard.css';
import './styles/live-views.css';
import './styles/objects.css';
import './styles/teach.css';
import './styles/notifications.css';
import './styles/node-settings.css';
import './styles/fleet-rules.css';
import { SideNav } from './components/layout';
import { ToastStack, LangProvider, normalizeLang, useT, LanguageDropdown, AppFooter } from '@shared';
import { FormBusyOverlay, ThemeDropdown } from './components/ui';
import { DashboardTab } from './components/dashboard';
import { NodesTab } from './components/nodes';
import { FleetRulesPage } from './components/fleet_rules';
import { LiveViewsPage } from './components/live_views';
import { ObjectsPage } from './components/objects';
import { TeachPage } from './components/teach';
import { NotificationsPage } from './components/notifications';
import { UsersPage, RolesAccessPage } from './components/rbac_admin';
import { AuditLogPage } from './components/audit_log';
import { LoginScreen, ChangePasswordScreen, PendingClearanceScreen } from './components/auth_screens';
import { api, sessionCanGet, apiBase } from './lib/helpers';
import { enBundle, loadLocaleDict } from './i18n';

const THEME_KEY = 'myseliasan_theme';
const NAV_PIN_KEY = 'myseliasan_nav_pinned';

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
  // Side-nav display mode: pinned (default, always in-flow) vs auto-hide (collapses to a
  // slim hover edge and slides in on hover). Persisted like the theme. Standardized with
  // mymatasan's rail — see .app-shell.nav-autohide in rbac-standard.css.
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
  // Fleet state is lifted here so the side-nav tree and the Nodes page stay in sync:
  // the tree lists adopted nodes and `managingNodeId` selects which one the page opens.
  const [nodes, setNodes] = useState([]);
  const [managingNodeId, setManagingNodeId] = useState(null);
  // A focused camera (chosen from the node's camera sub-tree) narrows the node's
  // Cameras view to that single live tile; null shows every camera on the node.
  const [managingCameraId, setManagingCameraId] = useState(null);
  // Consolidated notification feed state: notifUnread drives the side-nav badge; notifVersion
  // bumps on each SSE arrival so the Notifications page live-reloads its top page.
  const [notifUnread, setNotifUnread] = useState(0);
  const [notifVersion, setNotifVersion] = useState(0);

  async function loadNotifUnread() {
    const r = await api('/api/notifications?unread=true&limit=1&offset=0', { noRedirect: true }).catch(() => ({ ok: false }));
    if (r.ok) setNotifUnread(Number(r.body?.total || 0));
  }
  // Refresh the badge on login, and open one SSE stream that bumps the version + badge on
  // every new notification (control-plane or node-pushed). EventSource auto-reconnects.
  useEffect(() => {
    if (authState !== 'ready') { setNotifUnread(0); return undefined; }
    loadNotifUnread();
    if (typeof window.EventSource === 'undefined') return undefined;
    let source = null;
    let closed = false;
    const connect = () => {
      if (closed) return;
      try { source = new EventSource(`${apiBase()}/api/notifications/stream`, { withCredentials: true }); }
      catch (_) { return; }
      source.addEventListener('notification', () => { setNotifVersion((v) => v + 1); loadNotifUnread(); });
      source.onerror = () => {
        if (source && source.readyState === EventSource.CLOSED && !closed) { source.close(); window.setTimeout(connect, 5000); }
      };
    };
    connect();
    return () => { closed = true; if (source) source.close(); };
    // eslint-disable-next-line
  }, [authState]);

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
  // a nodeId jumps straight to that node's manage view. An optional cameraId focuses
  // that node's Cameras tab on a single camera; omitting it shows all cameras.
  function selectNode(nodeId, cameraId = null) {
    setManagingNodeId(nodeId);
    setManagingCameraId(cameraId);
    setActiveTab('nodes');
  }
  // Leaving the Nodes section clears the managed node/camera so returning lands on the list.
  function selectTab(id) {
    if (id !== 'nodes') { setManagingNodeId(null); setManagingCameraId(null); }
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
  // The pre-app screens take the language switcher too — it lives in the workspace
  // header, which the user cannot reach until they are signed in and cleared.
  if (authState === 'anon') {
    return <LoginScreen onLoggedIn={loadSession} lang={lang} onLangChange={onLangChange} />;
  }
  if (authState === 'mustchange') {
    return <ChangePasswordScreen onDone={loadSession} onToast={pushToast} onLogout={logout} lang={lang} onLangChange={onLangChange} />;
  }
  // Authenticated but no role assigned yet — gate the whole control plane behind a
  // clearance screen until a superadmin grants a role.
  if (session?.pending && !session?.isSuperadmin) {
    return <PendingClearanceScreen email={session?.email} onRefresh={loadSession} onLogout={logout} lang={lang} onLangChange={onLangChange} />;
  }

  // Demote to the dashboard if the active tab is no longer permitted (e.g. after a
  // handoff that retired the current stock account, or a role that lost node access).
  const canNodes = sessionCanGet(session, '/api/nodes');
  // Reading correlation rules follows the permission matrix (the API is behind it); WRITING
  // them is superadmin-only and enforced server-side.
  const canFleetRules = sessionCanGet(session, '/api/fleet-rules');
  const adminTabs = ['users', 'roles', 'audit'];
  if (adminTabs.includes(activeTab) && !session?.isSuperadmin) setActiveTab('dashboard');
  if ((activeTab === 'nodes' || activeTab === 'liveviews' || activeTab === 'objects' || activeTab === 'teach') && !canNodes) setActiveTab('dashboard');
  if (activeTab === 'fleetrules' && !canFleetRules) setActiveTab('dashboard');

  return (
    <div className={`app-shell${navPinned ? '' : ' nav-autohide'}`}>
      <SideNav
        activeTab={activeTab}
        busy={false}
        onTab={selectTab}
        onLogout={logout}
        session={session}
        nodes={nodes}
        managingNodeId={managingNodeId}
        managingCameraId={managingCameraId}
        onSelectNode={selectNode}
        notifUnread={notifUnread}
        pinned={navPinned}
        onTogglePinned={toggleNavPinned}
      />
      <main className="main-workspace">
        <div className="shared-lang-bar">
          <LanguageDropdown lang={lang} onLang={onLangChange} />
          <ThemeDropdown theme={theme} onThemeChange={changeTheme} />
        </div>
        {session?.superadminHandoffPending ? (
          <div className="handoff-banner" role="alert">
            <span className="handoff-banner-text">{t('handoff.text')}</span>
            {session?.isSuperadmin ? <button type="button" className="handoff-banner-action" onClick={() => setActiveTab('users')}>{t('handoff.goToUsers')}</button> : null}
          </div>
        ) : null}
        <ToastStack toasts={toasts} onDismiss={(id) => setToasts((list) => list.filter((t) => t.id !== id))} />

        {activeTab === 'dashboard' ? <DashboardTab nodes={nodes} /> : null}
        {activeTab === 'liveviews' && canNodes ? <LiveViewsPage nodes={nodes} /> : null}
        {activeTab === 'objects' && canNodes ? <ObjectsPage nodes={nodes} onToast={pushToast} /> : null}
        {activeTab === 'teach' && canNodes ? <TeachPage nodes={nodes} onToast={pushToast} /> : null}
        {activeTab === 'fleetrules' && canFleetRules ? <FleetRulesPage nodes={nodes} session={session} onToast={pushToast} /> : null}
        {activeTab === 'notifications' ? <NotificationsPage nodes={nodes} refreshSignal={notifVersion} onChanged={loadNotifUnread} /> : null}
        {activeTab === 'nodes' && canNodes ? (
          <NodesTab
            onToast={pushToast}
            nodes={nodes}
            reloadNodes={loadNodes}
            managingNodeId={managingNodeId}
            managingCameraId={managingCameraId}
            onManage={selectNode}
            onClearFocus={() => setManagingCameraId(null)}
            onBack={() => { setManagingNodeId(null); setManagingCameraId(null); }}
          />
        ) : null}
        {activeTab === 'users' && session?.isSuperadmin ? (
          <UsersPage session={session} onToast={pushToast} onSessionChanged={loadSession} />
        ) : null}
        {activeTab === 'roles' && session?.isSuperadmin ? <RolesAccessPage onToast={pushToast} /> : null}
        {activeTab === 'audit' && session?.isSuperadmin ? <AuditLogPage onToast={pushToast} /> : null}
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
  // The app's message bundle. English is always present (it is every key's fallback and must be
  // there on first paint); other locales are fetched on demand — see ./i18n — and accumulated
  // here so a second switch back is instant.
  const [appMessages, setAppMessages] = useState(enBundle);
  // A returning non-English user must not see a flash of English app strings, so gate the first
  // paint until their locale chunk has loaded. English users never wait.
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
    // setState it triggers. The `appMessages[lang]` guard above already handles an already-loaded
    // locale.
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
    // Brief, and only for a returning non-English user on cold load. Kept minimal on purpose.
    return <main className="boot-screen" />;
  }
  return (
    <LangProvider lang={lang} messages={appMessages}>
      <AppInner lang={lang} onLangChange={changeLang} />
    </LangProvider>
  );
}
