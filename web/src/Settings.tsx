import PeoplePanel from './people'
import AboutPanel from './about'
import { lazy, Suspense, useState, type FormEvent, type ReactNode } from 'react'
import { LogOut, Tv } from 'lucide-react'
import { api } from './api'
import { ErrorLine, Led, Sections, cx, toast } from './components'
import { useHaven } from './context'
import { PasswordInput } from './Auth'
import { enterTv } from './tv/spatial'
import { Avatar } from './profile'
import { WidgetsPanel } from './widgeteditors'

// The background picker pulls in the shader library, so load it on demand.
const BackgroundPanel = lazy(() => import('./bgpanel'))

type Theme = 'graphite' | 'bone'

export function applyTheme(t: Theme) {
  if (t === 'bone') document.documentElement.dataset.theme = 'bone'
  else delete document.documentElement.dataset.theme
  try {
    localStorage.setItem('haven-theme', t)
  } catch {
    /* storage blocked */
  }
}

function Panel({ title, children, aside }: { title: string; children: ReactNode; aside?: ReactNode }) {
  return (
    <section className="plate overflow-hidden" aria-label={title}>
      <div className="plate-head">
        <h2 className="etch">{title}</h2>
        {aside}
      </div>
      <div className="p-4 sm:p-5">{children}</div>
    </section>
  )
}

function PairTv() {
  const [code, setCode] = useState('')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const [done, setDone] = useState(false)
  const submit = async (e: FormEvent) => {
    e.preventDefault()
    setBusy(true)
    setError('')
    try {
      await api.tvApprove(code)
      setDone(true)
      setCode('')
      toast.ok('TV paired. It will sign in within a few seconds.')
    } catch (err) {
      setError((err as Error).message)
    } finally {
      setBusy(false)
    }
  }
  return (
    <Panel title="Pair a TV" aside={done ? <span className="tag tag-ok"><Led s="ok" /> Paired</span> : undefined}>
      <p className="mb-4 text-sm text-ink-2">
        On the TV, open <span className="font-mono text-ink">{window.location.host}/tv</span>. It shows a six-letter code. Type it here and the TV signs in, with no password typed on the remote.
      </p>
      <form onSubmit={submit} className="flex flex-wrap items-start gap-2">
        <input
          className="field w-44 font-mono text-lg uppercase tracking-[0.3em]"
          value={code}
          onChange={(e) => {
            setCode(e.target.value.toUpperCase().slice(0, 8))
            setDone(false)
          }}
          placeholder="ABC-123"
          aria-label="TV code"
          aria-invalid={!!error}
          autoComplete="off"
          autoCapitalize="characters"
        />
        <button className="key key-primary h-10" disabled={busy || code.replace(/[^A-Z0-9]/g, '').length < 6} data-loading={busy}>
          Pair TV
        </button>
      </form>
      <ErrorLine className="mt-3">{error}</ErrorLine>
    </Panel>
  )
}

const ROLE_WORD: Record<string, string> = { admin: 'Administrator', manager: 'Manager', member: 'Member', guest: 'Guest' }

const ALL_TABS = [
  ['look', 'Appearance'],
  ['widgets', 'Widgets'],
  ['people', 'People'],
  ['tv', 'TV'],
  ['account', 'Account'],
  ['about', 'About'],
] as const
type TabId = (typeof ALL_TABS)[number][0]
// Only the admin changes how Haven looks or who has access. Everyone can pair a TV and change their password.
const ADMIN_ONLY: TabId[] = ['look', 'widgets', 'people']

export default function SettingsPage({ onLogout }: { onLogout: () => void }) {
  const { user, version, role } = useHaven()
  const TABS = ALL_TABS.filter(([id]) => role === 'admin' || !ADMIN_ONLY.includes(id))
  const [tab, setTab] = useState<TabId>(() => {
    try {
      const t = sessionStorage.getItem('haven-settings-tab')
      return TABS.some(([id]) => id === t) ? (t as TabId) : TABS[0][0]
    } catch {
      return TABS[0][0]
    }
  })
  const pickTab = (id: TabId) => {
    setTab(id)
    try {
      sessionStorage.setItem('haven-settings-tab', id)
    } catch {
      /* storage blocked */
    }
  }
  const [theme, setTheme] = useState<Theme>(() => (document.documentElement.dataset.theme === 'bone' ? 'bone' : 'graphite'))
  const [current, setCurrent] = useState('')
  const [next, setNext] = useState('')
  const [confirm, setConfirm] = useState('')
  const [busy, setBusy] = useState(false)

  const change = async (e: FormEvent) => {
    e.preventDefault()
    if (next !== confirm) return toast.error('The two new passwords are different.')
    setBusy(true)
    try {
      await api.changePassword(current, next)
      toast.ok('Password changed. Other devices and TVs were signed out.')
      setCurrent('')
      setNext('')
      setConfirm('')
    } catch (err) {
      toast.error((err as Error).message)
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="space-y-6">
      <div className="flex flex-wrap items-end justify-between gap-3">
        <h1 className="text-3xl font-semibold tracking-tight sm:text-4xl">Settings</h1>
        <div className="well inline-flex max-w-full gap-0.5 overflow-x-auto p-0.5" role="tablist" aria-label="Settings sections">
          {TABS.map(([id, label]) => (
            <button key={id} role="tab" aria-selected={tab === id} onClick={() => pickTab(id)} className={cx('key key-sm shrink-0 gap-2 border-transparent', tab !== id && 'key-flat')}>
              <span className={cx('h-1.5 w-1.5 rounded-full', tab === id ? 'bg-accent' : 'bg-unlit')} />
              {label}
            </button>
          ))}
        </div>
      </div>

      {tab === 'look' && (
        <div className="space-y-5">
          <Panel title="Faceplate" aside={<span className="etch text-[0.625rem]">Colours</span>}>
            <div className="well inline-flex gap-0.5 p-0.5" role="radiogroup" aria-label="Theme">
              {(['graphite', 'bone'] as Theme[]).map((t) => (
                <button
                  key={t}
                  role="radio"
                  aria-checked={theme === t}
                  onClick={() => {
                    applyTheme(t)
                    setTheme(t)
                  }}
                  className={cx('key key-sm gap-2 border-transparent capitalize', theme !== t && 'key-flat')}
                >
                  <span className={cx('h-1.5 w-1.5 rounded-full', theme === t ? 'bg-accent' : 'bg-unlit')} />
                  {t === 'graphite' ? 'Graphite (dark)' : 'Bone (light)'}
                </button>
              ))}
            </div>
            <p className="mt-3 text-xs text-muted">This is remembered on this device only.</p>
          </Panel>
          <Suspense fallback={<section className="plate p-5 text-sm text-muted">Loading backgrounds…</section>}>
            <BackgroundPanel />
          </Suspense>
        </div>
      )}

      {tab === 'widgets' && <WidgetsPanel />}

      {tab === 'people' && <PeoplePanel />}

      {tab === 'about' && <AboutPanel />}

      {tab === 'tv' && (
        <Sections>
          <PairTv />
          <Panel title="TV mode">
            <p className="mb-4 text-sm text-ink-2">Open TV mode on this device to try it, or visit it on the TV's browser. Background and sounds for TV mode are set under Appearance and in the TV's Haven row.</p>
            <button className="key" onClick={enterTv}>
              <Tv size={14} /> Switch to TV mode
            </button>
          </Panel>
        </Sections>
      )}

      {tab === 'account' && (
        <Sections>
          <Panel title="Password">
            <form onSubmit={change} className="grid max-w-sm gap-4">
              <div className="space-y-2">
                <label htmlFor="cur" className="etch block">
                  Current password
                </label>
                <PasswordInput id="cur" value={current} onChange={setCurrent} autoComplete="current-password" />
              </div>
              <div className="space-y-2">
                <label htmlFor="new" className="etch block">
                  New password
                </label>
                <PasswordInput id="new" value={next} onChange={setNext} autoComplete="new-password" placeholder="8 characters or more" />
              </div>
              <div className="space-y-2">
                <label htmlFor="new2" className="etch block">
                  Repeat new password
                </label>
                <PasswordInput id="new2" value={confirm} onChange={setConfirm} autoComplete="new-password" />
              </div>
              <div>
                <button className="key key-primary" disabled={busy} data-loading={busy}>
                  Change password
                </button>
              </div>
            </form>
          </Panel>
          <Panel title="Profile">
            <div className="flex items-center gap-4">
              <Avatar name={user} size={56} />
              <div className="min-w-0">
                <div className="truncate text-xl font-semibold">{user}</div>
                <div className="font-mono text-xs text-muted">{ROLE_WORD[role] || role} · signed in on this device</div>
              </div>
            </div>
            <p className="mt-4 font-mono text-xs text-muted">Haven {version} · apps live in /opt/haven/apps</p>
            <div className="mt-4">
              <button className="key key-danger" onClick={onLogout}>
                <LogOut size={14} /> Sign out
              </button>
            </div>
          </Panel>
        </Sections>
      )}
    </div>
  )
}
