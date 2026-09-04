import { useEffect, useState, lazy, Suspense } from 'react';
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
import './styles/fleet-policy.css';
import './styles/failover.css';
import './styles/push.css';
import './styles/fleet-wall.css';
// The site/asset KIND PICKER, shared by the map's AssetWizard and the first-run setup
// wizard. It is here rather than in fleet-map.css because setup never loads the map
// chunk — when it lived there the wizard's three tiles rendered with no CSS at all.
import './styles/asset-kind.css';
import { SideNav, WorkspaceHeader } from './components/layout';
import { ToastStack, LangProvider, normalizeLang, useT, AppFooter, useStickyTab, clearStickyTab } from '@shared';
import { ManualProvider, ManualLibrary } from '@shared/Manual';
import { FormBusyOverlay } from './components/ui';
import { DashboardTab } from './components/dashboard';
// The Map page pulls in OpenLayers (~110KB gz). Lazy-load it so that weight is fetched only
// when an operator actually opens the Map tab, keeping the initial bundle lean.
const MapPage = lazy(() => import('./components/map_page').then((m) => ({ default: m.MapPage })));
import { NodesTab } from './components/nodes';
import { AIInsightPage } from './components/insight';
import { FleetRulesPage } from './components/fleet_rules';
import { FleetPolicyPage } from './components/fleet_policy';
import { FailoverPage } from './components/failover';
import { LiveViewsPage } from './components/live_views';
import { FleetWallPage } from './components/fleet_wall';
import { ObjectsPage } from './components/objects';
import { TeachPage } from './components/teach';
import { NotificationsPage } from './components/notifications';
import { UsersPage, RolesAccessPage } from './components/rbac_admin';
import { AuditLogPage } from './components/audit_log';
import { ReportsPage } from './components/reports';
import { SettingsPage } from './components/settings';
import { LoginScreen, ChangePasswordScreen, PendingClearanceScreen } from './components/auth_screens';
import { SetupWizard } from './components/setup';
import { api, sessionCanGet, apiBase } from './lib/helpers';
import { enBundle, loadLocaleDict } from './i18n';

// Names this app's remembered section (see @shared/stickyTab). The prefix keeps the five
// apps from reading each other's value when they are served from the same host.
const TAB_KEY = 'myseliasan_active_tab';

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
  // The section survives a refresh (see @shared/stickyTab). No `allowed` list is passed: the
  // permission demotions further down are per-API-grant rather than a flat set, and they are
  // reached only once the session has loaded — so they, not a list up here, decide whether a
  // restored section is one this operator may have.
  const [activeTab, setActiveTab] = useStickyTab(TAB_KEY, 'dashboard');
  const [toasts, setToasts] = useState([]);
  // Fleet state is lifted here so the side-nav tree and the Nodes page stay in sync:
  // the tree lists adopted nodes and `managingNodeId` selects which one the page opens.
  const [nodes, setNodes] = useState([]);
  // 'idle'|'loading'|'ok'|'error' — so the nodes page can tell an empty fleet from a failed load.
  const [nodesLoad, setNodesLoad] = useState('idle');
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
    if (!sessionCanGet(session, '/api/nodes')) { setNodes([]); setNodesLoad('ok'); return; }
    setNodesLoad('loading');
    const r = await api('/api/nodes').catch(() => ({ ok: false }));
    if (r.ok) {
      setNodes(Array.isArray(r.body) ? r.body : []);
      setNodesLoad('ok');
    } else {
      // The request FAILED — an expired session, a network blip, a control plane that just
      // restarted. Do NOT blank the fleet: an empty list here is indistinguishable from "you
      // have no nodes", and an operator whose session lapsed would think their whole fleet
      // vanished. Keep whatever we last knew and flag the error so the page can say so.
      setNodesLoad('error');
    }
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

  // Agent-suggested fleet rule hand-off: the Insight page proposes a draft, the
  // Fleet Rules page opens its editor pre-filled. Nothing saves without the operator.
  const [rulePrefill, setRulePrefill] = useState(null);
  function suggestRule(prefill) {
    setRulePrefill(prefill);
    setActiveTab('fleetrules');
  }

  function pushToast(text, kind = 'info') {
    if (!text) return;
    const id = `${Date.now()}-${Math.random().toString(16).slice(2)}`;
    setToasts((list) => [{ id, text, kind }, ...list].slice(0, 5));
  }

  const toastStack = (
    <ToastStack toasts={toasts} onDismiss={(id) => setToasts((list) => list.filter((t) => t.id !== id))} />
  );

  // withToasts wraps a PRE-APP screen so its toasts are actually rendered.
  //
  // The stack lives in the app shell below, which those screens return before ever
  // reaching — so the first-run wizard and the must-change screen were pushing toasts
  // into a list nothing displayed. Every success message in the whole wizard was
  // invisible: the site it created, the fleet key it generated, the Redis it just
  // reached. A button that works and says nothing is indistinguishable from a broken one,
  // which is exactly how it was reported.
  const withToasts = (screen) => (<>{screen}{toastStack}</>);

  // First-run wizard gate: 'unknown' | 'needed' | 'done'. The flag is SERVER-side, so a
  // dismissal sticks per install rather than per browser. Only fetched for a superadmin
  // — they are the only role that can complete it, and nobody else should be held at a
  // loading screen waiting for the answer.
  const [setupState, setSetupState] = useState('unknown');
  useEffect(() => {
    if (authState !== 'ready' || !session?.isSuperadmin) { setSetupState('done'); return undefined; }
    let alive = true;
    api('/api/setup/state', { noRedirect: true })
      .then((r) => { if (alive) setSetupState(r.ok && r.body && r.body.completed ? 'done' : 'needed'); })
      // A failed probe must never lock an operator out of their own control plane, so
      // an unreachable/erroring endpoint falls through to the app rather than the wizard.
      .catch(() => { if (alive) setSetupState('done'); });
    return () => { alive = false; };
  }, [authState, session]);

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
    // Forget the section too, so the next person to sign in on this tab starts at the
    // dashboard rather than inside the last operator's work.
    // Order matters: setActiveTab WRITES through to storage, so clearing first would leave
    // "dashboard" sitting in the very key the clear was meant to empty.
    setActiveTab('dashboard');
    clearStickyTab(TAB_KEY);
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
    return withToasts(<ChangePasswordScreen onDone={loadSession} onToast={pushToast} onLogout={logout} lang={lang} onLangChange={onLangChange} />);
  }
  // Authenticated but no role assigned yet — gate the whole control plane behind a
  // clearance screen until a superadmin grants a role.
  if (session?.pending && !session?.isSuperadmin) {
    return <PendingClearanceScreen email={session?.email} onRefresh={loadSession} onLogout={logout} lang={lang} onLangChange={onLangChange} />;
  }
  // Cleared to use the app, but this install has never been set up. Runs LAST of the
  // pre-app gates so the wizard is only ever reached by a signed-in, password-changed,
  // role-bearing superadmin.
  if (setupState === 'unknown') {
    return <main className="boot-screen"><FormBusyOverlay busy /></main>;
  }
  if (setupState === 'needed') {
    return withToasts(
      <SetupWizard
        session={session}
        lang={lang}
        onLangChange={onLangChange}
        onToast={pushToast}
        onDone={() => setSetupState('done')}
      />,
    );
  }

  // Demote to the dashboard if the active tab is no longer permitted (e.g. after a
  // handoff that retired the current stock account, or a role that lost node access).
  const canNodes = sessionCanGet(session, '/api/nodes');
  // Reading correlation rules follows the permission matrix (the API is behind it); WRITING
  // them is superadmin-only and enforced server-side.
  const canFleetRules = sessionCanGet(session, '/api/fleet-rules');
  // Reading the compliance report follows the matrix; WRITING a policy is superadmin-only
  // and enforced server-side. The report is a health view — hiding it helps nobody.
  const canFleetPolicy = sessionCanGet(session, '/api/fleet-policies');
  // Failover: READING which sites are covered follows the matrix — "is this recorder
  // covered" is a health question and hiding it from the people who would notice a gap
  // helps nobody. Every write, including the takeover, is superadmin-only server-side.
  const canFailover = sessionCanGet(session, '/api/failover-plans');
  // The AI Insight page (digest + ask-the-fleet) follows the matrix on /api/agent:
  // GET shows the digest; POST (generate/chat) is checked inside the page itself.
  const canAgent = sessionCanGet(session, '/api/agent');
  const adminTabs = ['users', 'roles', 'audit', 'settings'];
  if (adminTabs.includes(activeTab) && !session?.isSuperadmin) setActiveTab('dashboard');
  if ((activeTab === 'nodes' || activeTab === 'liveviews' || activeTab === 'objects' || activeTab === 'teach') && !canNodes) setActiveTab('dashboard');
  if (activeTab === 'fleetrules' && !canFleetRules) setActiveTab('dashboard');
  if (activeTab === 'fleetpolicy' && !canFleetPolicy) setActiveTab('dashboard');
  if (activeTab === 'failover' && !canFailover) setActiveTab('dashboard');
  if (activeTab === 'insight' && !canAgent) setActiveTab('dashboard');

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
        <WorkspaceHeader
          lang={lang}
          onLangChange={onLangChange}
          theme={theme}
          onThemeChange={changeTheme}
          activeTab={activeTab}
        />
        {session?.superadminHandoffPending ? (
          <div className="handoff-banner" role="alert">
            <span className="handoff-banner-text">{t('handoff.text')}</span>
            {session?.isSuperadmin ? <button type="button" className="handoff-banner-action" onClick={() => setActiveTab('users')}>{t('handoff.goToUsers')}</button> : null}
          </div>
        ) : null}
        {toastStack}

        {activeTab === 'dashboard' ? <DashboardTab nodes={nodes} /> : null}
        {activeTab === 'insight' && canAgent ? <AIInsightPage session={session} onToast={pushToast} onSuggestRule={session?.isSuperadmin ? suggestRule : null} /> : null}
        {activeTab === 'map' ? (
          <Suspense fallback={<div className="map-loading">{t('common.loading')}</div>}>
            <MapPage nodes={nodes} reloadNodes={loadNodes} onToast={pushToast} onOpenNode={selectNode} />
          </Suspense>
        ) : null}
        {activeTab === 'liveviews' && canNodes ? <LiveViewsPage nodes={nodes} /> : null}
        {activeTab === 'fleetwall' ? <FleetWallPage session={session} refreshSignal={notifVersion} onToast={pushToast} /> : null}
        {activeTab === 'objects' && canNodes ? <ObjectsPage nodes={nodes} onToast={pushToast} /> : null}
        {activeTab === 'teach' && canNodes ? <TeachPage nodes={nodes} onToast={pushToast} /> : null}
        {activeTab === 'fleetrules' && canFleetRules ? <FleetRulesPage nodes={nodes} session={session} onToast={pushToast} prefill={rulePrefill} onPrefillConsumed={() => setRulePrefill(null)} /> : null}
        {activeTab === 'fleetpolicy' && canFleetPolicy ? <FleetPolicyPage nodes={nodes} session={session} onToast={pushToast} /> : null}
        {activeTab === 'failover' && canFailover ? <FailoverPage nodes={nodes} session={session} onToast={pushToast} /> : null}
        {activeTab === 'notifications' ? <NotificationsPage nodes={nodes} refreshSignal={notifVersion} onChanged={loadNotifUnread} session={session} onToast={pushToast} /> : null}
        {activeTab === 'nodes' && canNodes ? (
          <NodesTab
            onToast={pushToast}
            nodes={nodes}
            reloadNodes={loadNodes}
            nodesLoad={nodesLoad}
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
        {activeTab === 'reports' ? <ReportsPage session={session} onToast={pushToast} /> : null}
        {activeTab === 'settings' && session?.isSuperadmin ? <SettingsPage session={session} onToast={pushToast} /> : null}
        {activeTab === 'manual' ? (
          <section className="workspace manual-workspace">
            <ManualLibrary />
          </section>
        ) : null}
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
      {/* Outside AppInner deliberately: the manual has to be readable from the sign-in screen
          and the first-run wizard, both of which render instead of the workspace. */}
      <ManualProvider apiBase={apiBase()} lang={lang} appName="MySeliaSan">
        <AppInner lang={lang} onLangChange={changeLang} />
      </ManualProvider>
    </LangProvider>
  );
}
