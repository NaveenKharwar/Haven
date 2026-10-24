import { useCallback, useEffect, useMemo, useRef, useState, type FormEvent, type ReactNode } from 'react'
import { api, appUrl, formatBytes, ledState, statusWord, type AuthStatus, type CatalogApp, type InstalledApp, type Check } from '../api'
import { AppIcon, Led, LogBox, Readout, Spinner, cx, useClock, useJob, usePoll } from '../components'
import { exitTv, focusDefault, focusEl, useSpatialNav } from './spatial'
import { cpuText, diskText, isLive, memText } from '../usage'
import { BackdropLayer, useBackground } from '../bg'
import { StoryBody, changeClass, pct, useWidgets, weatherInfo, weekday } from '../widgets'
import { formatMoney, formatNumber, timeAgo, type NewsItem } from '../api'
import { setSound, soundOn } from './sound'
import { setLite, useLite } from './lite'

// TV mode: a 10-foot launcher driven entirely by a remote's arrows, OK and Back.

export default function TvApp() {
  const [auth, setAuth] = useState<AuthStatus | null>(null)
  const check = useCallback(() => {
    api.status().then(setAuth).catch(() => setTimeout(check, 3000))
  }, [])
  useEffect(check, [check])

  let body: ReactNode
  if (!auth) body = <div className="flex min-h-screen items-center justify-center"><Spinner size={32} /></div>
  else if (auth.setupRequired) body = <TvNotice title="Finish setup first" text="Create the admin account on a phone or computer, then come back to this screen." onRetry={check} />
  else if (!auth.authenticated) body = <TvPair onDone={check} />
  else body = <TvHome onSignedOut={check} />
  const lite = useLite()
  // Light mode never loads the animated background at all.
  const bg = useBackground('tv', lite ? 0 : 60000)
  return (
    <div className={cx('tv', lite && 'tv-lite')}>
      {!lite && <BackdropLayer bg={bg} tv />}
      {body}
    </div>
  )
}

function TvWordmark({ right }: { right?: ReactNode }) {
  return (
    <header className="flex items-center justify-between gap-[1em] border-b border-rule px-[3em] py-[1.1em]">
      <span className="flex items-center gap-[0.6em]">
        <span className="grid h-[1.3em] w-[1.3em] place-items-center rounded-[2px] bg-accent">
          <span className="h-[0.4em] w-[0.4em] rounded-full bg-accent-ink" />
        </span>
        <span className="text-[1.2em] font-bold tracking-[0.3em]">HAVEN</span>
      </span>
      {right}
    </header>
  )
}

function TvNotice({ title, text, onRetry }: { title: string; text: string; onRetry: () => void }) {
  useSpatialNav(() => {})
  return (
    <div className="flex min-h-screen flex-col">
      <TvWordmark />
      <div className="flex flex-1 flex-col items-start justify-center gap-[1em] px-[3em]">
        <h1 className="text-[2.6em] font-semibold tracking-tight">{title}</h1>
        <p className="max-w-[36em] text-[1.1em] text-ink-2">{text}</p>
        <button className="key key-primary mt-[1em]" data-nav data-nav-default onClick={onRetry}>
          Check again
        </button>
      </div>
    </div>
  )
}

// --- Pairing ---------------------------------------------------------------

function TvPair({ onDone }: { onDone: () => void }) {
  const [pair, setPair] = useState<{ code: string; id: string; exp: number } | null>(null)
  const [error, setError] = useState('')
  const [now, setNow] = useState(Date.now())
  const [usePassword, setUsePassword] = useState(false)

  const start = useCallback(async () => {
    setError('')
    try {
      const p = await api.tvStart()
      setPair({ code: p.code, id: p.id, exp: Date.now() + p.expiresIn * 1000 })
    } catch (e) {
      setError((e as Error).message)
    }
  }, [])
  useEffect(() => {
    start()
  }, [start])

  useEffect(() => {
    if (!pair) return
    let stop = false
    const poll = async () => {
      try {
        const r = await api.tvPoll(pair.id)
        if (stop) return
        if (r.status === 'approved') return onDone()
        if (r.status === 'expired') return start()
      } catch {
        /* network blip: keep trying */
      }
      if (!stop) setTimeout(poll, 2000)
    }
    const t = setTimeout(poll, 2000)
    const tick = setInterval(() => setNow(Date.now()), 1000)
    return () => {
      stop = true
      clearTimeout(t)
      clearInterval(tick)
    }
  }, [pair, onDone, start])

  useSpatialNav(() => setUsePassword(false))
  const left = pair ? Math.max(0, Math.round((pair.exp - now) / 1000)) : 0
  if (usePassword) return <TvPasswordLogin onDone={onDone} onCancel={() => setUsePassword(false)} />

  return (
    <div className="flex min-h-screen flex-col">
      <TvWordmark right={<span className="etch text-[0.7em]">Pair this TV</span>} />
      <div className="grid flex-1 grid-cols-[minmax(0,1.1fr)_minmax(0,1fr)] items-center gap-[4em] px-[3em] py-[2em]">
        <div>
          <h1 className="text-[2.8em] font-semibold leading-[1.1] tracking-tight">Sign in with your phone.</h1>
          <ol className="mt-[1.4em] space-y-[0.7em] text-[1.05em] text-ink-2">
            <li className="flex gap-[0.8em]">
              <span className="font-mono text-accent">1</span>
              <span>
                On your phone, open <span className="font-mono text-ink">{window.location.host}</span> and sign in.
              </span>
            </li>
            <li className="flex gap-[0.8em]">
              <span className="font-mono text-accent">2</span>
              <span>Go to Settings, then Pair a TV.</span>
            </li>
            <li className="flex gap-[0.8em]">
              <span className="font-mono text-accent">3</span>
              <span>Type the code shown here.</span>
            </li>
          </ol>
          <div className="mt-[2em] flex gap-[0.8em]">
            <button className="key" data-nav data-nav-default onClick={start}>
              New code
            </button>
            <button className="key" data-nav onClick={() => setUsePassword(true)}>
              Use a password instead
            </button>
            <button className="key key-flat" data-nav onClick={exitTv}>
              Leave TV mode
            </button>
          </div>
        </div>
        <div className="plate p-[2em]">
          <div className="etch mb-[1.2em] text-[0.7em]">Pairing code</div>
          {error ? (
            <p className="text-[1.1em] text-bad">{error}</p>
          ) : (
            <div className="flex items-center gap-[0.35em]" aria-live="polite">
              {(pair?.code || '------').split('').map((ch, i) =>
                ch === '-' && i === 3 ? (
                  <span key={i} className="w-[0.6em] text-center text-[2em] text-muted">
                    –
                  </span>
                ) : (
                  <span key={i} className="well readout flex h-[2.3em] w-[1.7em] items-center justify-center text-[2.4em]">
                    {pair ? ch : <span className="unlit">0</span>}
                  </span>
                ),
              )}
            </div>
          )}
          <div className="mt-[1.4em] flex items-center gap-[0.6em] font-mono text-[0.8em] text-muted">
            <Led s="busy" /> Waiting for your phone · new code in {Math.floor(left / 60)}:{String(left % 60).padStart(2, '0')}
          </div>
        </div>
      </div>
    </div>
  )
}

function TvPasswordLogin({ onDone, onCancel }: { onDone: () => void; onCancel: () => void }) {
  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)
  useEffect(() => {
    focusDefault()
  }, [])
  const submit = async (e: FormEvent) => {
    e.preventDefault()
    setBusy(true)
    setError('')
    try {
      await api.login(username, password)
      onDone()
    } catch (err) {
      setError((err as Error).message)
    } finally {
      setBusy(false)
    }
  }
  return (
    <div className="flex min-h-screen flex-col">
      <TvWordmark right={<span className="etch text-[0.7em]">Sign in</span>} />
      <form onSubmit={submit} className="mx-auto flex w-full max-w-[28em] flex-1 flex-col justify-center gap-[1em] px-[2em]">
        <h1 className="text-[2.2em] font-semibold tracking-tight">Sign in</h1>
        <label className="etch text-[0.7em]" htmlFor="tv-user">
          Username
        </label>
        <input id="tv-user" className="field" data-nav data-nav-default value={username} onChange={(e) => setUsername(e.target.value)} autoComplete="username" />
        <label className="etch text-[0.7em]" htmlFor="tv-pw">
          Password
        </label>
        <input id="tv-pw" className="field" data-nav type="password" value={password} onChange={(e) => setPassword(e.target.value)} autoComplete="current-password" />
        {error && <p className="text-bad">{error}</p>}
        <div className="mt-[0.6em] flex gap-[0.8em]">
          <button className="key key-primary" data-nav disabled={busy}>
            Sign in
          </button>
          <button type="button" className="key" data-nav onClick={onCancel}>
            Back to pairing
          </button>
        </div>
      </form>
    </div>
  )
}

// --- Home ------------------------------------------------------------------

type Overlay = { kind: 'app'; id: string } | { kind: 'install'; app: CatalogApp } | { kind: 'job'; id: string; title: string } | { kind: 'news'; item: NewsItem } | null

function needsInput(app: CatalogApp) {
  return app.settings.some((s) => s.required && !s.generate && !s.default && s.prefill !== 'host')
}

function TvHome({ onSignedOut }: { onSignedOut: () => void }) {
  const lite = useLite()
  const { data, refresh } = usePoll(api.apps, lite ? 10000 : 4000, [lite])
  const { data: widgets, refresh: refreshWidgets } = useWidgets()
  const [sound, setSoundState] = useState(soundOn)
  const { data: sys } = usePoll(api.system, lite ? 15000 : 3000, [lite])
  const [catalog, setCatalog] = useState<CatalogApp[]>([])
  const [overlay, setOverlay] = useState<Overlay>(null)
  const opener = useRef<HTMLElement | null>(null)
  const now = useClock()

  useEffect(() => {
    api.catalog().then(setCatalog).catch(() => {})
  }, [])

  // Once the app list first arrives, start on the first app.
  const placed = useRef(false)
  useEffect(() => {
    if (!data || placed.current) return
    placed.current = true
    setTimeout(focusDefault, 30)
  }, [data])

  const open = (o: Overlay) => {
    opener.current = document.activeElement as HTMLElement
    setOverlay(o)
  }
  const close = useCallback(() => {
    setOverlay(null)
    refresh()
    setTimeout(() => {
      const el = opener.current
      if (el && el.isConnected) focusEl(el)
      else focusDefault()
    }, 30)
  }, [refresh])
  useSpatialNav(() => {
    if (overlay) close()
  })

  const apps = data?.apps || []
  const installed = useMemo(() => new Set(apps.map((a) => a.catalogId)), [apps])
  const available = catalog.filter((c) => !installed.has(c.id))
  const root = sys?.disks?.find((d) => d.mount === '/') || sys?.disks?.[0]
  const running = apps.filter((a) => a.status === 'running').length

  const signOut = async () => {
    await api.logout().catch(() => {})
    onSignedOut()
  }

  return (
    <div className="flex min-h-screen flex-col pb-[3em]">
      <TvWordmark
        right={
          <span className="flex items-baseline gap-[1.2em]">
            <span className="font-mono text-[0.8em] text-muted">{sys?.hostname}</span>
            <span className="readout text-[1.6em]">
              {String(now.getHours()).padStart(2, '0')}:{String(now.getMinutes()).padStart(2, '0')}
            </span>
          </span>
        }
      />

      <section className="mx-[3em] mt-[1.6em] grid grid-cols-5 gap-px overflow-hidden rounded-[3px] border border-rule bg-rule-2" aria-label="Server">
        {[
          ['CPU', <Readout key="c" value={sys?.cpuPercent} digits={3} unit="%" />],
          ['Memory', <Readout key="m" value={sys?.memPercent} digits={3} unit="%" />],
          ['Storage', <Readout key="d" value={root?.percent} digits={3} unit="%" />],
          ['Download', <span key="n" className="readout">{sys ? formatBytes(sys.netRx).replace(' ', ' ') : '—'}<span className="ml-[0.2em] text-[0.45em] text-muted">/s</span></span>],
          ['Apps', <span key="a" className="readout">{running}<span className="text-muted">/{apps.length}</span></span>],
        ].map(([label, value]) => (
          <div key={label as string} className="bg-plate px-[1.2em] py-[1em]">
            <div className="etch text-[0.65em]">{label}</div>
            <div className="mt-[0.4em] text-[2.2em]">{value}</div>
          </div>
        ))}
      </section>

      <TvRow title="Apps" hint={apps.length ? `${running} running` : undefined}>
        {apps.length === 0 ? (
          <div className="plate flex h-[11em] w-[24em] shrink-0 items-center px-[1.4em] text-ink-2">No apps yet. Pick one from the catalog below.</div>
        ) : (
          apps.map((a, i) => <TvAppTile key={a.id} app={a} first={i === 0} onSelect={() => open({ kind: 'app', id: a.id })} />)
        )}
      </TvRow>

      <TvToday lite={lite} widgets={widgets} onStory={(item) => open({ kind: 'news', item })} />

      <TvRow title="Catalog" hint={`${available.length} more to install`}>
        {available.slice(0, lite ? 16 : available.length).map((c) => (
          <button key={c.id} data-nav data-nav-row="catalog" onClick={() => open({ kind: 'install', app: c })} className="plate flex h-[11em] w-[16em] shrink-0 flex-col justify-between bg-raised p-[1.1em] text-left">
            <AppIcon src={c.icon} name={c.name} size={48} />
            <span>
              <span className="block truncate text-[1.15em] font-semibold">{c.name}</span>
              <span className="line-clamp-2 text-[0.8em] leading-snug text-ink-2">{c.tagline}</span>
            </span>
          </button>
        ))}
      </TvRow>

      <TvRow title="Haven">
        {[
          ['Refresh', 'Reload apps, stats and widgets', () => { refresh(); refreshWidgets() }],
          [`Sounds: ${sound ? 'on' : 'off'}`, 'Soft clicks when you move', () => { setSound(!sound); setSoundState(!sound) }],
          [`Performance: ${lite ? 'Light' : 'Full'}`, lite ? 'No animation, easy on weak TVs. Press for Full' : 'Animated background and effects. Press for Light', () => setLite(!lite)],
          ['Leave TV mode', 'Use the full web interface', exitTv],
          ['Sign out', 'This TV will need pairing again', signOut],
        ].map(([label, text, fn]) => (
          <button key={label as string} data-nav data-nav-row="haven" onClick={fn as () => void} className="plate flex h-[6.5em] w-[16em] shrink-0 flex-col justify-center bg-raised px-[1.1em] text-left">
            <span className="text-[1.1em] font-semibold">{label as string}</span>
            <span className="text-[0.8em] text-ink-2">{text as string}</span>
          </button>
        ))}
      </TvRow>

      <p className="etch mx-[3em] mt-[2.4em] text-[0.65em]">Arrows move · OK selects · Back closes</p>

      {overlay?.kind === 'app' && (
        <TvAppSheet app={apps.find((a) => a.id === overlay.id)} onClose={close} onJob={(id, title) => setOverlay({ kind: 'job', id, title })} />
      )}
      {overlay?.kind === 'install' && <TvInstallSheet app={overlay.app} onClose={close} onJob={(id, title) => setOverlay({ kind: 'job', id, title })} />}
      {overlay?.kind === 'news' && (
        <Sheet label="Story">
          <StoryBody it={overlay.item} qrSize={220} />
          <div className="mt-[1.6em] flex gap-[0.8em]">
            <button className="key key-primary" data-nav data-nav-default onClick={close}>
              Close
            </button>
          </div>
        </Sheet>
      )}
      {overlay?.kind === 'job' && <TvJob id={overlay.id} title={overlay.title} onClose={close} />}
    </div>
  )
}

function TvRow({ title, hint, children }: { title: string; hint?: string; children: ReactNode }) {
  return (
    <section className="mt-[2em]" aria-label={title}>
      <div className="mx-[3em] mb-[0.8em] flex items-baseline gap-[1em]">
        <h2 className="text-[1.35em] font-semibold">{title}</h2>
        {hint && <span className="etch text-[0.65em]">{hint}</span>}
      </div>
      {/* Vertical padding leaves room for the focus bezel and lift. */}
      <div className="flex gap-[1.1em] overflow-x-auto px-[3em] py-[0.8em] [scrollbar-width:none]">{children}</div>
    </section>
  )
}

function TvAppTile({ app, first, onSelect }: { app: InstalledApp; first: boolean; onSelect: () => void }) {
  const led = ledState(app.status, app.busy)
  return (
    <button data-nav data-nav-row="apps" data-nav-default={first ? '' : undefined} onClick={onSelect} className="plate flex h-[11em] w-[16em] shrink-0 flex-col justify-between bg-raised p-[1.1em] text-left">
      <span className="flex w-full items-center justify-between">
        <AppIcon src={app.icon} name={app.name} size={52} />
        <span className="flex items-center gap-[0.45em]">
          <Led s={led} />
          <span className={cx('etch text-[0.6em]', led === 'bad' && 'text-bad')}>{statusWord(app.status, app.busy)}</span>
        </span>
      </span>
      <span>
        <span className="block truncate text-[1.2em] font-semibold">{app.name}</span>
        <span className="block truncate font-mono text-[0.75em] text-muted">
          {app.webPort ? `:${app.webPort}` : app.id}
          {isLive(app) ? ` · ${memText(app)} · ${cpuText(app)}` : ''}
        </span>
      </span>
    </button>
  )
}

function Sheet({ label, children }: { label: string; children: ReactNode }) {
  useEffect(() => {
    const t = setTimeout(focusDefault, 30)
    return () => clearTimeout(t)
  }, [])
  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-scrim" data-nav-trap role="dialog" aria-modal="true" aria-label={label}>
      <div className="plate rise max-h-[90vh] w-[min(56em,92vw)] overflow-y-auto p-[2em]">{children}</div>
    </div>
  )
}

function TvAppSheet({ app, onClose, onJob }: { app?: InstalledApp; onClose: () => void; onJob: (id: string, title: string) => void }) {
  const [error, setError] = useState('')
  if (!app) return null
  const url = app.status !== 'stopped' ? appUrl(app.web, app.webPort) : null
  const led = ledState(app.status, app.busy)
  const busy = !!app.busy
  const run = async (action: 'start' | 'stop' | 'restart' | 'update') => {
    try {
      const job = await api.action(app.id, action)
      onJob(job.id, app.name)
    } catch (e) {
      setError((e as Error).message)
    }
  }
  return (
    <Sheet label={app.name}>
      <div className="flex items-center gap-[1.2em]">
        <AppIcon src={app.icon} name={app.name} size={84} />
        <div className="min-w-0">
          <h2 className="truncate text-[2.2em] font-semibold tracking-tight">{app.name}</h2>
          <div className="mt-[0.3em] flex items-center gap-[0.6em]">
            <Led s={led} />
            <span className="etch text-[0.7em]">{statusWord(app.status, app.busy)}</span>
            <span className="font-mono text-[0.8em] text-muted">· {app.containers.filter((c) => c.state === 'running').length}/{app.containers.length} containers up</span>
          </div>
        </div>
      </div>
      <div className="mt-[1.4em] grid grid-cols-3 gap-px overflow-hidden rounded-[3px] border border-rule bg-rule-2">
        {[
          ['CPU', cpuText(app)],
          ['Memory', memText(app)],
          ['Disk', diskText(app)],
        ].map(([label, value]) => (
          <div key={label} className="bg-well px-[1em] py-[0.7em]">
            <div className="etch text-[0.6em]">{label}</div>
            <div className="readout mt-[0.3em] text-[1.6em]">{value}</div>
          </div>
        ))}
      </div>
      {app.updateAvailable && <p className="mt-[1em] text-accent">Version {app.updateAvailable} is available.</p>}
      {error && <p className="mt-[1em] text-bad">{error}</p>}
      <div className="mt-[1.8em] flex flex-wrap gap-[0.8em]">
        {url && (
          <button className="key key-primary" data-nav data-nav-default onClick={() => window.location.assign(url)}>
            Open {app.name}
          </button>
        )}
        {app.status === 'stopped' ? (
          <button className="key" data-nav data-nav-default={url ? undefined : ''} disabled={busy} onClick={() => run('start')}>
            Start
          </button>
        ) : (
          <>
            <button className="key" data-nav data-nav-default={url ? undefined : ''} disabled={busy} onClick={() => run('restart')}>
              Restart
            </button>
            <button className="key" data-nav disabled={busy} onClick={() => run('stop')}>
              Stop
            </button>
          </>
        )}
        <button className="key" data-nav disabled={busy} onClick={() => run('update')}>
          {app.updateAvailable ? 'Update' : 'Pull latest'}
        </button>
        <button className="key key-flat" data-nav onClick={onClose}>
          Close
        </button>
      </div>
      {url && <p className="mt-[1.2em] text-[0.8em] text-muted">Opening an app leaves Haven. Press Back on your remote to return.</p>}
    </Sheet>
  )
}

function TvInstallSheet({ app, onClose, onJob }: { app: CatalogApp; onClose: () => void; onJob: (id: string, title: string) => void }) {
  const blocked = needsInput(app)
  const values = useMemo(() => {
    const v: Record<string, string> = {}
    for (const s of app.settings) v[s.key] = s.prefill === 'host' && !s.default ? window.location.hostname : s.default || ''
    return v
  }, [app])
  const [checks, setChecks] = useState<Check[] | null>(null)
  const [error, setError] = useState('')
  useEffect(() => {
    if (blocked) return
    api.preflight(app.id, app.id, values).then((r) => setChecks(r.checks)).catch((e) => setError(e.message))
  }, [app.id, blocked, values])
  const fails = checks?.filter((c) => c.level === 'fail') || []
  const install = async () => {
    try {
      const job = await api.install(app.id, app.id, values)
      onJob(job.id, app.name)
    } catch (e) {
      setError((e as Error).message)
    }
  }
  return (
    <Sheet label={`Install ${app.name}`}>
      <div className="flex items-center gap-[1.2em]">
        <AppIcon src={app.icon} name={app.name} size={84} />
        <div className="min-w-0">
          <h2 className="truncate text-[2.2em] font-semibold tracking-tight">{app.name}</h2>
          <p className="text-ink-2">{app.tagline}</p>
        </div>
      </div>
      <p className="mt-[1.2em] max-w-[40em] text-ink-2">{app.description}</p>
      {blocked ? (
        <div className="mt-[1.2em] flex items-start gap-[0.6em] text-warn"><span className="mt-[0.45em]"><Led s="warn" /></span>This app needs a few details, like a password. Install it from your phone or computer.</div>
      ) : (
        <div className="mt-[1.2em] font-mono text-[0.8em]">
          {!checks && !error && (
            <span className="flex items-center gap-[0.6em] text-muted">
              <Spinner /> Testing the server…
            </span>
          )}
          {checks && fails.length === 0 && (
            <span className="flex items-center gap-[0.6em] text-ok">
              <Led s="ok" /> Self-test passed. Installs with default settings.
            </span>
          )}
          {fails.map((c, i) => (
            <div key={i} className="flex items-start gap-[0.6em] text-bad">
              <Led s="bad" /> {c.name}: {c.message}
            </div>
          ))}
        </div>
      )}
      {error && <p className="mt-[1em] text-bad">{error}</p>}
      <div className="mt-[1.8em] flex gap-[0.8em]">
        {!blocked && (
          <button className="key key-primary" data-nav data-nav-default disabled={!checks || fails.length > 0} onClick={install}>
            Install {app.name}
          </button>
        )}
        <button className="key" data-nav data-nav-default={blocked ? '' : undefined} onClick={onClose}>
          {blocked ? 'OK' : 'Cancel'}
        </button>
      </div>
    </Sheet>
  )
}

function TvJob({ id, title, onClose }: { id: string; title: string; onClose: () => void }) {
  const job = useJob(id)
  const running = !job || job.status === 'running'
  const doneRef = useRef<HTMLButtonElement>(null)
  useEffect(() => {
    if (!running) doneRef.current?.focus()
  }, [running])
  return (
    <Sheet label={`${title} console`}>
      <div className="mb-[1em] flex items-center gap-[0.7em]">
        <Led s={running ? 'busy' : job?.status === 'success' ? 'ok' : 'bad'} />
        <h2 className="text-[1.6em] font-semibold">
          {title} · {running ? 'working' : job?.status === 'success' ? 'done' : 'failed'}
        </h2>
      </div>
      {job?.status === 'failed' && job.error && <p className="mb-[1em] text-bad">{job.error}</p>}
      <LogBox lines={(job?.log || []).slice(-60)} className="h-[16em] text-[0.75em]" />
      <div className="mt-[1.4em] flex gap-[0.8em]">
        <button ref={doneRef} className={cx('key', !running && 'key-primary')} data-nav data-nav-default onClick={onClose}>
          {running ? 'Hide, keep running' : 'Done'}
        </button>
      </div>
    </Sheet>
  )
}

// --- Today: weather, markets, currency and news ----------------------------

function TvTile({ children, wide, onSelect }: { children: ReactNode; wide?: boolean; onSelect?: () => void }) {
  return (
    <button data-nav data-nav-row="today" onClick={onSelect} className={cx('plate flex h-[11em] shrink-0 flex-col justify-between bg-raised p-[1.1em] text-left', wide ? 'w-[24em]' : 'w-[16em]')}>
      {children}
    </button>
  )
}

function TvToday({ widgets, onStory, lite }: { widgets: ReturnType<typeof useWidgets>['data']; onStory: (it: NewsItem) => void; lite: boolean }) {
  if (!widgets) return null
  const cities = widgets.weather.off ? [] : widgets.weather.data || []
  const quotes = widgets.markets.off ? [] : widgets.markets.data || []
  const fx = widgets.currency.off ? undefined : widgets.currency.data
  const newsList = widgets.news.off ? [] : widgets.news.data || []
  const news = newsList[0]
  const showWeather = cities.length > 0
  if (!showWeather && quotes.length === 0 && !fx && !news) return null
  return (
    <>
      {(showWeather || quotes.length > 0 || fx) && (
        <TvRow title="Today">
          {cities.map((w) => (
            <TvTile key={w.place} wide>
              {(() => {
                const { label, Icon } = weatherInfo(w.code, w.isDay)
                return (
                  <>
                    <span className="flex w-full items-start justify-between">
                      <span>
                        <span className="readout text-[3em]">{Math.round(w.temp)}°</span>
                        <span className="block text-[0.9em] text-ink-2">
                          {label} · {w.place}
                        </span>
                      </span>
                      <Icon className="h-[3em] w-[3em] text-accent" strokeWidth={1.3} aria-hidden />
                    </span>
                    <span className="grid w-full gap-[0.3em] font-mono text-[0.62em]" style={{ gridTemplateColumns: `repeat(${w.days.length || 1}, minmax(0, 1fr))` }}>
                      {w.days.map((d, i) => (
                        <span key={d.date} className="text-center">
                          <span className="block text-muted">{weekday(d.date, i)}</span>
                          {Math.round(d.max)}° <span className="block text-muted">{Math.round(d.min)}°</span>
                        </span>
                      ))}
                    </span>
                  </>
                )
              })()}
            </TvTile>
          ))}
          {quotes.map((q) => (
            <TvTile key={q.symbol}>
              <span className="etch text-[0.65em]">{q.name}</span>
              <span>
                <span className="readout block text-[2em]">{q.price ? formatNumber(q.price) : '—'}</span>
                <span className={cx('font-mono text-[0.85em]', changeClass(q.changePct))}>{q.price ? pct(q.changePct) : q.error || ''}</span>
                {q.local && (
                  <span className="block truncate font-mono text-[0.7em] text-muted">
                    {formatMoney(q.local.price, q.local.currency, 0)} / {q.local.unit}
                  </span>
                )}
              </span>
            </TvTile>
          ))}
          {fx && fx.rates.length > 0 && (
            <TvTile>
              <span className="etch text-[0.65em]">Currency · {fx.home}</span>
              <span className="space-y-[0.2em] font-mono text-[0.9em]">
                {fx.rates.slice(0, 4).map((r) => (
                  <span key={r.code} className="flex justify-between gap-[0.6em]">
                    <span>1 {r.code}</span>
                    <span>{formatMoney(r.rate, fx.home)}</span>
                  </span>
                ))}
              </span>
            </TvTile>
          )}
        </TvRow>
      )}
      {newsList.slice(0, lite ? 1 : 3).map((n) => (
        <TvRow key={n.label} title="News" hint={n.label}>
          {n.items.slice(0, lite ? 6 : 10).map((it) => (
            <button
              key={it.link}
              data-nav
              data-nav-row={`news-${n.label}`}
              onClick={() => onStory(it)}
              className="plate flex h-[11em] w-[20em] shrink-0 flex-col justify-between bg-raised p-[1.1em] text-left"
            >
              <span className="line-clamp-4 text-[1em] font-medium leading-snug">{it.title}</span>
              <span className="truncate font-mono text-[0.7em] text-muted">
                {[it.source, it.published && !it.published.startsWith('0001') ? timeAgo(it.published) : ''].filter(Boolean).join(' · ')}
              </span>
            </button>
          ))}
        </TvRow>
      ))}
    </>
  )
}
