import { Component, useCallback, useEffect, useMemo, useState, useSyncExternalStore } from 'react'
import { Tv } from 'lucide-react'
import { api, atLeast, isTvDevice, type AuthStatus, type CatalogApp, type Role } from './api'
import { getMode, setMode, type DeviceMode } from './mode'
import { defaultLiteForTv } from './tv/lite'
import ModePicker from './modepicker'
import { BackdropLayer, useBackground } from './bg'
import { JobDialog, Led, Readout, Spinner, Toaster, cx, toast, usePoll } from './components'
import { Ctx, type HavenCtx, type Route } from './context'
import { Login, Setup } from './Auth'
import Overview from './Dashboard'
import CatalogPage from './Store'
import AppsPage, { AppDrawer } from './Apps'
import SettingsPage from './Settings'
import UpdatesPage from './Updates'
import NotificationBell from './bell'
import ServerPage from './Server'
import TvApp from './tv/TV'
import { enterTv } from './tv/spatial'
import { go, readPath, subscribePath } from './router'
import ProfileMenu from './profile'
import { Footer } from './about'


const allNav: { id: Route; label: string; key: string; min: Role }[] = [
  { id: 'overview', label: 'Overview', key: '1', min: 'guest' },
  { id: 'apps', label: 'Apps', key: '2', min: 'guest' },
  { id: 'catalog', label: 'Catalog', key: '3', min: 'manager' },
  { id: 'updates', label: 'Updates', key: '4', min: 'manager' },
  { id: 'server', label: 'Server', key: '5', min: 'manager' },
  { id: 'settings', label: 'Settings', key: '6', min: 'guest' },
]
const legacy: Record<string, Route> = { dashboard: 'overview', store: 'catalog' }

function routeFromHash(): Route {
  const h = readPath()
  return (allNav.find((n) => n.id === h)?.id as Route) || legacy[h] || 'overview'
}

export default function App() {
  const path = useSyncExternalStore(subscribePath, readPath)
  useEffect(() => {
    // A device that chose TV mode (or is clearly a TV) opens straight into it.
    if (readPath() === '' && (getMode() === 'tv' || (getMode() === null && isTvDevice()))) enterTv()
  }, [])
  if (path.startsWith('tv')) return <TvApp />
  return <WebApp />
}

/** Sends a device that chose TV mode into it. */
function GoTv() {
  useEffect(() => {
    enterTv()
  }, [])
  return null
}

function WebApp() {
  const [auth, setAuth] = useState<AuthStatus | null>(null)
  const [authError, setAuthError] = useState('')
  const [mode, setModeState] = useState<DeviceMode | null>(getMode)
  const pickMode = (m: DeviceMode | null) => {
    setMode(m)
    setModeState(m)
    if (m === 'tv') defaultLiteForTv()
  }
  const check = useCallback(async () => {
    try {
      setAuth(await api.status())
      setAuthError('')
    } catch (e) {
      setAuthError((e as Error).message)
    }
  }, [])
  useEffect(() => {
    check()
    const onUnauth = () => check()
    window.addEventListener('haven:unauthorized', onUnauth)
    return () => window.removeEventListener('haven:unauthorized', onUnauth)
  }, [check])

  if (!auth) {
    return (
      <div className="flex min-h-screen flex-col items-center justify-center gap-4 text-sm text-ink-2">
        {authError ? (
          <>
            <span className="flex items-center gap-2">
              <Led s="bad" /> Can't reach Haven: {authError}
            </span>
            <button className="key" onClick={check}>
              Try again
            </button>
          </>
        ) : (
          <Spinner />
        )}
      </div>
    )
  }
  if (auth.setupRequired) return <Setup onDone={check} />
  if (!auth.authenticated) {
    if (mode === null) return <ModePicker onPick={pickMode} />
    if (mode === 'tv') return <GoTv />
    return <Login onDone={check} onChangeDevice={() => pickMode(null)} />
  }
  return (
    <>
      <Shell
        user={auth.user || ''}
        role={auth.role || 'guest'}
        version={auth.version}
        repo={auth.repo}
        author={auth.author || 'Naveen Kharwar'}
        authorUrl={auth.authorUrl}
        onLogout={async () => {
          await api.logout().catch(() => {})
          check()
        }}
      />
      <Toaster />
    </>
  )
}

export function Wordmark() {
  return (
    <span className="flex items-center gap-3">
      <span className="grid h-5 w-5 place-items-center rounded-[2px] bg-accent">
        <span className="h-1.5 w-1.5 rounded-full bg-accent-ink" />
      </span>
      <span className="text-[0.95rem] font-bold tracking-[0.28em]">HAVEN</span>
    </span>
  )
}

/** If a page fails while drawing, show a way out instead of leaving a half-drawn screen. */
class PageGuard extends Component<{ children: React.ReactNode }, { failed: boolean }> {
  state = { failed: false }
  static getDerivedStateFromError() {
    return { failed: true }
  }
  render() {
    if (!this.state.failed) return this.props.children
    return (
      <section className="plate p-6 text-sm">
        <p className="font-medium">This page didn't load properly.</p>
        <p className="mt-1 text-muted">It usually fixes itself with a reload, for example right after Haven updated.</p>
        <button className="key key-sm mt-4" onClick={() => window.location.reload()}>
          Reload
        </button>
      </section>
    )
  }
}

function Shell({ user, role, version, repo, author, authorUrl, onLogout }: { user: string; role: Role; version: string; repo?: string; author: string; authorUrl?: string; onLogout: () => void }) {
  const nav = useMemo(() => allNav.filter((n) => atLeast(role, n.min)).map((n) => (n.id === 'server' && role !== 'admin' ? { ...n, label: 'Files' } : n)), [role])
  // The address bar is the only source of truth for the page, so the two can't drift apart.
  const route = useSyncExternalStore(subscribePath, routeFromHash)
  const allowed = nav.some((n) => n.id === route)
  const [catalog, setCatalog] = useState<CatalogApp[]>([])
  const [job, setJob] = useState<{ id: string; title?: string } | null>(null)
  const [openAppId, setOpenAppId] = useState<string | null>(null)
  const { data, refresh } = usePoll(api.apps, 4000)
  const { data: sys } = usePoll(api.system, 3000)
  const { data: upd } = usePoll(async () => (atLeast(role, 'manager') ? api.updates() : null), 120000)
  const background = useBackground('web')

  useEffect(() => {
    if (atLeast(role, 'manager')) api.catalog().then(setCatalog).catch((e) => toast.error(e.message))
  }, [role])

  const navigate = useCallback((r: Route) => {
    go(r)
    window.scrollTo({ top: 0 })
  }, [])

  // After Haven updates itself, this page is still running the old interface against the new
  // server. Notice the new version and reload, even if the Updates page was left in the meantime.
  useEffect(() => {
    if (!version || version.endsWith('-dev')) return
    const t = setInterval(() => {
      api.status().then((st) => {
        if (st.version && st.version !== version) window.location.reload()
      }).catch(() => {})
    }, 5000)
    return () => clearInterval(t)
  }, [version])

  // Number keys switch pages, like a mode selector.
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      const t = e.target as HTMLElement
      if (e.metaKey || e.ctrlKey || e.altKey || t.closest('input, textarea, select, [contenteditable], [role=dialog], aside')) return
      const n = nav.find((x) => x.key === e.key)
      if (n) navigate(n.id)
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [navigate, nav])

  const ctx: HavenCtx = useMemo(
    () => ({
      user,
      role,
      version,
      repo,
      author,
      authorUrl,
      apps: data?.apps || [],
      appsReady: !!data,
      containers: data?.containers || [],
      dockerError: data?.dockerError,
      catalog,
      sys,
      updatesCount: upd ? upd.apps.filter((a) => a.hasUpdate).length : 0,
      refreshApps: refresh,
      openJob: (id, title) => setJob({ id, title }),
      openApp: (id) => setOpenAppId(id),
      navigate,
    }),
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [user, role, version, repo, author, authorUrl, data, catalog, sys, upd, navigate],
  )
  const updates = ctx.updatesCount

  return (
    <Ctx.Provider value={ctx}>
      <BackdropLayer bg={background} />
      <header className="sticky top-0 z-30 border-b border-rule bg-paper">
        <div className="mx-auto flex h-14 max-w-[90rem] items-center gap-4 px-4 lg:px-8">
          <button onClick={() => navigate('overview')} aria-label="Haven overview" className="rounded-[2px]">
            <Wordmark />
          </button>
          <span className="hidden h-6 w-px bg-rule md:block" />
          <span className="hidden min-w-0 items-center gap-2 md:flex">
            <Led s={sys?.docker ? 'ok' : sys ? 'bad' : 'off'} label={sys?.docker ? 'Docker running' : 'Docker not running'} />
            <span className="truncate font-mono text-xs text-ink-2">{sys?.hostname || '…'}</span>
          </span>

          <nav aria-label="Main" className="well mx-auto hidden items-center gap-0.5 p-0.5 md:flex">
            {nav.map((n) => {
              const on = route === n.id
              return (
                <button
                  key={n.id}
                  onClick={() => navigate(n.id)}
                  aria-current={on ? 'page' : undefined}
                  className={cx('key key-sm gap-2 border-transparent', !on && 'key-flat')}
                  title={`${n.label} (press ${n.key})`}
                >
                  <span className={cx('h-1.5 w-1.5 rounded-full', on ? 'bg-accent' : 'bg-unlit')} />
                  {n.label}
                  {n.id === 'updates' && updates > 0 && <span className="tag tag-accent">{updates}</span>}
                </button>
              )
            })}
          </nav>

          <div className="ml-auto flex items-center gap-4 md:ml-0">
            <span className="hidden items-baseline gap-3 font-mono text-xs text-muted lg:flex">
              <span>
                CPU <Readout value={sys?.cpuPercent} digits={3} className="text-ink" />%
              </span>
              <span>
                MEM <Readout value={sys?.memPercent} digits={3} className="text-ink" />%
              </span>
            </span>
            <button className="key key-sm hidden sm:inline-flex" onClick={enterTv} title="Open TV mode">
              <Tv size={14} /> TV
            </button>
            {atLeast(role, 'manager') && <NotificationBell onOpenRoute={navigate} />}
            <ProfileMenu user={user} role={role} onSettings={() => navigate('settings')} onTv={enterTv} onLogout={onLogout} />
          </div>
        </div>
      </header>

      <main className="mx-auto flex min-h-[calc(100dvh-3.75rem)] max-w-[90rem] flex-col px-4 pb-28 pt-6 md:pb-12 lg:px-8 lg:pt-8">
        <PageGuard key={`page-${route}`}>
        <div className="rise w-full flex-1">
          {!allowed && <Overview />}
          {allowed && route === 'overview' && <Overview />}
          {allowed && route === 'apps' && <AppsPage />}
          {allowed && route === 'catalog' && <CatalogPage />}
          {allowed && route === 'updates' && <UpdatesPage />}
          {allowed && route === 'server' && <ServerPage />}
          {allowed && route === 'settings' && <SettingsPage onLogout={onLogout} />}
        </div>
        </PageGuard>
        <Footer key={`footer-${route}`} />
      </main>

      <nav aria-label="Main" className="fixed inset-x-0 bottom-0 z-30 border-t border-rule bg-paper px-2 pb-[max(0.5rem,env(safe-area-inset-bottom))] pt-2 md:hidden">
        <div className="well grid gap-0.5 p-0.5" style={{ gridTemplateColumns: `repeat(${nav.length}, minmax(0, 1fr))` }}>
          {nav.map((n) => {
            const on = route === n.id
            return (
              <button key={n.id} onClick={() => navigate(n.id)} aria-current={on ? 'page' : undefined} className={cx('key key-sm w-full gap-1 border-transparent px-0.5 max-[400px]:text-[0.6875rem]', !on && 'key-flat')}>
                <span className={cx('h-1.5 w-1.5 shrink-0 rounded-full max-[400px]:hidden', on ? 'bg-accent' : 'bg-unlit')} />
                {n.label}
              </button>
            )
          })}
        </div>
      </nav>

      <AppDrawer id={openAppId} onClose={() => setOpenAppId(null)} />
      <JobDialog
        jobId={job?.id || null}
        title={job?.title}
        onClose={() => {
          setJob(null)
          refresh()
        }}
      />
    </Ctx.Provider>
  )
}
