import { useCallback, useEffect, useMemo, useState } from 'react'
import {
  SideNav, BrandLogo, ToastStack, LangProvider, useT, normalizeLang, LanguageDropdown, AppFooter,
  useStickyTab, clearStickyTab
} from '@shared'
import { enBundle, loadLocaleDict } from './i18n'
import * as api from '../lib/access'
import Login from './Login'
import Doors from './Doors'
import People from './People'
import Activity from './Activity'
import Readers from './Readers'
import Access from './Access'
import Trail from './Trail'
import Settings from './Settings'
import Wizard from './Wizard'

// Each section names the CAPABILITY that makes it worth showing, and those capabilities come from
// the server's permission matrix (GET /api/auth/capabilities) — the same matrix that decides every
// request.
//
// This used to be `admin: true/false`. Two things were wrong with that, and they pulled in
// opposite directions. Editing a grant IS admin-only, and the catalog says so — but an operator is
// deliberately granted READ on groups, grants and schedules, "the rules they have to work within",
// and `admin: true` hid the whole section from them. Meanwhile every other section was shown to
// everybody regardless of what the server would answer. A rail driven by the matrix cannot make
// either mistake, and it follows the catalog automatically when the catalog changes.
const TABS = [
  { id: 'doors', icon: 'door', label: 'nav.doors', needs: 'viewDoors' },
  { id: 'people', icon: 'user', label: 'nav.people', needs: 'viewPeople' },
  { id: 'activity', icon: 'list', label: 'nav.activity', needs: 'viewActivity' },
  { id: 'readers', icon: 'cpu', label: 'nav.readers', needs: 'viewReaders' },
  { id: 'access', icon: 'lock', label: 'nav.access', needs: 'viewRules' },
  // The administrative trail sits next to Access rules, not next to Activity, on purpose: it is
  // read by whoever is asking what was CHANGED, and that question starts on the rules screen.
  { id: 'trail', icon: 'shield', label: 'nav.trail', needs: 'viewAudit' },
  { id: 'settings', icon: 'sliders', label: 'nav.settings', needs: 'viewSettings' }
]

// While the capabilities are still loading nothing is offered. Showing a control and taking it
// away half a second later is worse than a beat of nothing — on this screen the control opens a
// door.
const NO_CAPS = {}

// Names this app's remembered section (see @shared/stickyTab). The prefix keeps the five apps
// from reading each other's value when they are served from the same host.
const TAB_KEY = 'mypintusan_active_tab'

const LANG_KEY = 'mypintusan.lang'

export default function App() {
  const [lang, setLang] = useState(() => normalizeLang(localStorage.getItem(LANG_KEY) || 'en'))
  const [messages, setMessages] = useState(enBundle)

  // Locale dictionaries load on demand; English is already in the bundle as the fallback.
  useEffect(() => {
    let cancelled = false
    loadLocaleDict(lang).then(dict => {
      if (!cancelled && dict) setMessages(prev => ({ ...prev, [lang]: dict }))
    })
    return () => { cancelled = true }
  }, [lang])

  const changeLang = useCallback(next => {
    const v = normalizeLang(next)
    localStorage.setItem(LANG_KEY, v)
    setLang(v)
  }, [])

  return (
    <LangProvider lang={lang} messages={messages}>
      <Shell lang={lang} onLang={changeLang} />
    </LangProvider>
  )
}

function Shell({ lang, onLang }) {
  const t = useT()
  const [user, setUser] = useState(null)
  const [caps, setCaps] = useState(NO_CAPS)
  const [booting, setBooting] = useState(true)
  // The section survives a refresh (see @shared/stickyTab). The permitted set is derived from
  // the SAME capability matrix that builds the rail below, so a restored section can never be
  // one the rail would refuse to offer — which matters here more than anywhere else in the
  // suite, because these screens open doors.
  const allowedTabs = useMemo(() => TABS.filter(item => !!caps[item.needs]).map(item => item.id), [caps])
  // Land on the first section this operator actually has rather than always on Doors: somebody
  // granted only the trail has no business being dropped on a door list that will 403.
  const fallbackTab = allowedTabs[0] || 'doors'
  // While the capabilities are still loading nothing is decided — passing the (empty) allowed
  // set there would bounce every operator off their restored section on every single reload,
  // because at first paint nobody has any capability yet.
  // Guard only while somebody is actually signed in. With no user the capability set is empty,
  // which the hook reads as "may reach nothing" and answers by writing the fallback back into
  // the key sign-out had just cleared.
  const [tab, setTab] = useStickyTab(TAB_KEY, fallbackTab, (booting || !user) ? null : allowedTabs)
  const [needsSetup, setNeedsSetup] = useState(false)
  const [toasts, setToasts] = useState([])

  // ToastStack's item shape is {id, text, kind} — not {message, tone}. Getting it wrong renders an
  // empty, unstyled toast, which looks like the action silently did nothing.
  const toast = useCallback((text, kind = 'info') => {
    const id = Math.random().toString(36).slice(2)
    setToasts(list => [...list, { id, text, kind }])
    setTimeout(() => setToasts(list => list.filter(x => x.id !== id)), 5000)
  }, [])

  // Probe the session once on boot. A 401 simply means "show the sign-in card" — it is the normal
  // first-visit path, not an error worth surfacing.
  //
  // The setup flag is a property of the INSTALL, held server-side, not of this browser. An admin
  // signing in from a second machine on a controller that has been running for months must not be
  // met by the first-run wizard again.
  useEffect(() => {
    let cancelled = false
    api.session()
      .then(async u => {
        if (cancelled) return
        setUser(u)
        // Capabilities are fetched with the session, not lazily per screen: the rail is rendered
        // from them, and a rail that appears one section at a time is a rail nobody trusts.
        try {
          const c = await api.capabilities()
          if (!cancelled) setCaps(c || NO_CAPS)
        } catch { /* no capabilities means no offers — fail closed, never open */ }
        try {
          const st = await api.setupState()
          if (!cancelled) setNeedsSetup(!(st && st.completed))
        } catch { /* an older install without the flag is treated as already set up */ }
      })
      .catch(() => { if (!cancelled) setUser(null) })
      .finally(() => { if (!cancelled) setBooting(false) })
    return () => { cancelled = true }
  }, [])

  // Signing in has to load the capabilities too. The boot probe above runs once, on mount, and by
  // then nobody is signed in — so without this the rail after a fresh sign-in is rendered from an
  // empty capability set and the app looks like it has no screens at all.
  const signIn = useCallback(async u => {
    setUser(u)
    try {
      setCaps((await api.capabilities()) || NO_CAPS)
    } catch { /* fail closed: no capabilities, no offers */ }
    try {
      const st = await api.setupState()
      setNeedsSetup(!(st && st.completed))
    } catch { /* an older install without the flag is treated as already set up */ }
  }, [])

  const signOut = useCallback(async () => {
    try { await api.logout() } catch { /* signing out locally is enough */ }
    // Forget the section too, so the next person to sign in on this terminal starts at the
    // top of the rail rather than inside the last operator's work. On a door controller the
    // previous operator's whereabouts is itself worth not leaving on the screen.
    clearStickyTab(TAB_KEY)
    setUser(null)
    setCaps(NO_CAPS)
  }, [])

  const groups = useMemo(() => [{
    label: '',
    items: TABS
      // Offer a section only if the server would serve it. Hiding what the matrix refuses keeps
      // the rail honest rather than offering a door that leads to a 403; showing what the matrix
      // allows keeps it from hiding something somebody was deliberately granted.
      .filter(item => !!caps[item.needs])
      .map(item => ({
        id: item.id,
        icon: item.icon,
        label: t(item.label),
        active: tab === item.id,
        onClick: () => setTab(item.id)
      }))
  }], [tab, t, caps])

  if (booting) return <div className="boot-screen">{t('common.loading')}</div>
  if (!user) return <Login onSignedIn={signIn} lang={lang} onLang={onLang} />
  // The wizard writes settings and door hardware, so it is offered only to somebody the matrix
  // would let do both. An operator signing into an unconfigured controller sees the ordinary
  // (empty) screens rather than a wizard that would 403 on every step.
  if (needsSetup && caps.editSettings && caps.createDoors) {
    return <Wizard onFinished={() => setNeedsSetup(false)} />
  }

  const Screen = { doors: Doors, people: People, activity: Activity, readers: Readers, access: Access, trail: Trail, settings: Settings }[tab] || Doors

  return (
    <div className="app-shell">
      <SideNav
        brand={
          <div className="brand">
            <BrandLogo size={26} />
            <div className="brand-text">
              <strong>{t('app.name')}</strong>
              <span>{t('app.tagline')}</span>
            </div>
          </div>
        }
        groups={groups}
        footer={
          <div className="nav-footer">
            <LanguageDropdown lang={lang} onLang={onLang} />
            <button type="button" className="nav-item tone-steel" onClick={signOut}>
              <span className="nav-label">{t('nav.signOut')}</span>
            </button>
          </div>
        }
      />
      <main className="app-main">
        <Screen user={user} caps={caps} toast={toast} />
        <AppFooter />
      </main>
      <ToastStack toasts={toasts} onDismiss={id => setToasts(l => l.filter(x => x.id !== id))} />
    </div>
  )
}
