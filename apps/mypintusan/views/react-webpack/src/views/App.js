import { useCallback, useEffect, useMemo, useState } from 'react'
import {
  SideNav, BrandLogo, ToastStack, LangProvider, useT, normalizeLang, LanguageDropdown, AppFooter
} from '@shared'
import { enBundle, loadLocaleDict } from './i18n'
import * as api from '../lib/access'
import Login from './Login'
import Doors from './Doors'
import People from './People'
import Activity from './Activity'
import Readers from './Readers'
import Access from './Access'
import Settings from './Settings'
import Wizard from './Wizard'

const TABS = [
  { id: 'doors', icon: 'door', label: 'nav.doors', admin: false },
  { id: 'people', icon: 'user', label: 'nav.people', admin: false },
  { id: 'activity', icon: 'list', label: 'nav.activity', admin: false },
  { id: 'readers', icon: 'cpu', label: 'nav.readers', admin: false },
  // Access rules are admin-only for the reason the RBAC catalog spells out: a grant edit changes
  // who may enter every door in a group, silently and indefinitely.
  { id: 'access', icon: 'lock', label: 'nav.access', admin: true },
  { id: 'settings', icon: 'sliders', label: 'nav.settings', admin: true }
]

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
  const [booting, setBooting] = useState(true)
  const [tab, setTab] = useState('doors')
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
        try {
          const st = await api.setupState()
          if (!cancelled) setNeedsSetup(!(st && st.completed))
        } catch { /* an older install without the flag is treated as already set up */ }
      })
      .catch(() => { if (!cancelled) setUser(null) })
      .finally(() => { if (!cancelled) setBooting(false) })
    return () => { cancelled = true }
  }, [])

  const signOut = useCallback(async () => {
    try { await api.logout() } catch { /* signing out locally is enough */ }
    setUser(null)
  }, [])

  const groups = useMemo(() => [{
    label: '',
    items: TABS
      // Settings is admin-only in the permission matrix, so hiding it for everyone else keeps the
      // nav honest rather than offering a door that leads to a 403.
      .filter(item => !item.admin || (user && user.isAdmin))
      .map(item => ({
        id: item.id,
        icon: item.icon,
        label: t(item.label),
        active: tab === item.id,
        onClick: () => setTab(item.id)
      }))
  }], [tab, t, user])

  if (booting) return <div className="boot-screen">{t('common.loading')}</div>
  if (!user) return <Login onSignedIn={setUser} lang={lang} onLang={onLang} />
  // Only an admin is offered the wizard: it writes settings and door hardware, which the matrix
  // reserves for admins anyway. An operator signing into an unconfigured controller sees the
  // ordinary (empty) screens rather than a wizard that would 403 on every step.
  if (needsSetup && user.isAdmin) {
    return <Wizard onFinished={() => setNeedsSetup(false)} />
  }

  const Screen = { doors: Doors, people: People, activity: Activity, readers: Readers, access: Access, settings: Settings }[tab] || Doors

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
        <Screen user={user} toast={toast} />
        <AppFooter />
      </main>
      <ToastStack toasts={toasts} onDismiss={id => setToasts(l => l.filter(x => x.id !== id))} />
    </div>
  )
}
