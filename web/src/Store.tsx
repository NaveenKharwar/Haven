import { useEffect, useMemo, useRef, useState, type ReactNode } from 'react'
import { ArrowUpRight, Search, X } from 'lucide-react'
import { api, ApiError, type CatalogApp, type Check, type Setting } from './api'
import { AppIcon, ChecksList, Drawer, ErrorLine, Led, Modal, Notice, Spinner, cx, toast } from './components'
import { useHaven } from './context'
import { PasswordInput } from './Auth'

export default function CatalogPage() {
  const { catalog, apps } = useHaven()
  const [q, setQ] = useState('')
  const [cat, setCat] = useState('All')
  const [selected, setSelected] = useState<CatalogApp | null>(null)
  const [custom, setCustom] = useState(false)

  const counts = useMemo(() => {
    const m = new Map<string, number>()
    catalog.forEach((a) => m.set(a.category, (m.get(a.category) || 0) + 1))
    return [...m.entries()].sort((a, b) => a[0].localeCompare(b[0]))
  }, [catalog])
  const installed = useMemo(() => new Set(apps.map((a) => a.catalogId)), [apps])
  const s = q.trim().toLowerCase()
  const list = catalog.filter((a) => (cat === 'All' || a.category === cat) && (!s || `${a.name} ${a.tagline} ${a.category}`.toLowerCase().includes(s)))
  const groups = counts.map(([c]) => [c, list.filter((a) => a.category === c)] as const).filter(([, xs]) => xs.length > 0)

  return (
    <div className="space-y-6">
      <div className="flex flex-wrap items-end justify-between gap-4">
        <div>
          <h1 className="text-3xl font-semibold tracking-tight sm:text-4xl">Catalog</h1>
          <p className="mt-2 text-sm text-ink-2">{catalog.length} apps. Each one is checked against your server before it installs.</p>
        </div>
        <div className="relative w-full sm:w-72">
          <Search size={15} className="pointer-events-none absolute left-3 top-1/2 -translate-y-1/2 text-muted" />
          <input className="field pl-9" placeholder="Search the catalog" value={q} onChange={(e) => setQ(e.target.value)} aria-label="Search the catalog" />
        </div>
      </div>

      <div className="grid grid-cols-1 gap-5 lg:grid-cols-[13rem_minmax(0,1fr)]">
        <nav aria-label="Categories" className="lg:sticky lg:top-20 lg:self-start">
          <div className="well flex gap-0.5 overflow-x-auto p-0.5 lg:flex-col">
            {[['All', catalog.length] as const, ...counts].map(([c, n]) => (
              <button key={c} onClick={() => setCat(c)} aria-pressed={cat === c} className={cx('key key-sm shrink-0 justify-between gap-3 border-transparent lg:w-full', cat !== c && 'key-flat')}>
                <span className="flex items-center gap-2">
                  <span className={cx('h-1.5 w-1.5 rounded-full', cat === c ? 'bg-accent' : 'bg-unlit')} />
                  {c}
                </span>
                <span className="font-mono text-[0.6875rem] text-muted">{String(n).padStart(2, '0')}</span>
              </button>
            ))}
          </div>
        </nav>

        <div className="space-y-5">
          {groups.length === 0 && <p className="plate px-4 py-10 text-center text-sm text-muted">Nothing matches “{q}”. Try a shorter word.</p>}
          {groups.map(([c, xs]) => (
            <section key={c} className="plate overflow-hidden" aria-label={c}>
              <div className="plate-head">
                <h2 className="etch">{c}</h2>
                <span className="etch text-[0.625rem]">{String(xs.length).padStart(2, '0')}</span>
              </div>
              <ul className="divide-y divide-rule-2">
                {xs.map((a) => (
                  <li key={a.id}>
                    <button onClick={() => setSelected(a)} className="group flex w-full items-center gap-4 px-4 py-4 text-left hover:bg-raised">
                      <AppIcon src={a.icon} name={a.name} size={40} />
                      <span className="min-w-0 flex-1">
                        <span className="flex items-center gap-2">
                          <span className="truncate font-semibold">{a.name}</span>
                          {installed.has(a.id) && (
                            <span className="tag tag-ok">
                              <Led s="ok" /> Installed
                            </span>
                          )}
                        </span>
                        <span className="block truncate text-sm text-ink-2">{a.tagline}</span>
                      </span>
                      <span className="hidden shrink-0 text-right font-mono text-[0.6875rem] leading-relaxed text-muted md:block">
                        <span className="block">{defaultPort(a) ? `port ${defaultPort(a)}` : 'host network'}</span>
                        <span className="block">{a.version}</span>
                      </span>
                      <span className="key key-sm shrink-0 group-hover:border-rule-hi">{installed.has(a.id) ? 'Add another' : 'Install'}</span>
                    </button>
                  </li>
                ))}
              </ul>
            </section>
          ))}

          <div className="flex flex-wrap items-center justify-between gap-3 rounded-[3px] border border-dashed border-rule-hi px-4 py-4">
            <span className="min-w-0">
              <span className="block font-semibold">Not in the catalog?</span>
              <span className="block text-sm text-ink-2">Paste any Docker Compose file and Haven runs it like the rest.</span>
            </span>
            <button className="key" onClick={() => setCustom(true)}>
              Add custom app
            </button>
          </div>
        </div>
      </div>

      {selected && <InstallSheet app={selected} onClose={() => setSelected(null)} />}
      {custom && <CustomSheet onClose={() => setCustom(false)} />}
    </div>
  )
}

function defaultPort(a: CatalogApp): string | undefined {
  if (!a.web) return undefined
  if (/^\d+$/.test(a.web.port)) return a.web.port
  return a.settings.find((s) => s.key === a.web?.port)?.default
}

function defaultValues(app: CatalogApp) {
  const v: Record<string, string> = {}
  for (const s of app.settings) v[s.key] = s.prefill === 'host' && !s.default ? window.location.hostname : s.default || ''
  return v
}

function SheetFrame({ label, onClose, children, footer }: { label: string; onClose: () => void; children: ReactNode; footer: ReactNode }) {
  return (
    <Drawer open onClose={onClose} label={label}>
      <div className="plate-head shrink-0 border-rule">
        <span className="etch">{label}</span>
        <button className="key key-flat key-sm key-icon" onClick={onClose} aria-label="Close">
          <X size={16} />
        </button>
      </div>
      <div className="min-h-0 flex-1 overflow-y-auto p-5">{children}</div>
      <div className="flex shrink-0 flex-wrap items-center justify-end gap-2 border-t border-rule bg-plate px-5 py-3">{footer}</div>
    </Drawer>
  )
}

function InstallSheet({ app, onClose }: { app: CatalogApp; onClose: () => void }) {
  const { apps, openJob, refreshApps } = useHaven()
  const taken = apps.some((a) => a.id === app.id)
  const [id, setId] = useState(taken ? `${app.id}-2` : app.id)
  const [values, setValues] = useState<Record<string, string>>(() => defaultValues(app))
  const [checks, setChecks] = useState<Check[] | null>(null)
  const [checking, setChecking] = useState(false)
  const [busy, setBusy] = useState(false)
  const [advanced, setAdvanced] = useState(taken)

  // Re-run the self-test shortly after typing stops.
  useEffect(() => {
    let stop = false
    setChecking(true)
    const t = setTimeout(async () => {
      try {
        const r = await api.preflight(app.id, id, values)
        if (!stop) setChecks(r.checks)
      } catch (e) {
        if (!stop) setChecks([{ name: 'Self-test', level: 'fail', message: (e as Error).message }])
      } finally {
        if (!stop) setChecking(false)
      }
    }, 450)
    return () => {
      stop = true
      clearTimeout(t)
    }
  }, [app.id, id, values])

  const fails = checks?.filter((c) => c.level === 'fail').length || 0
  const passes = checks?.filter((c) => c.level === 'ok').length || 0
  const install = async () => {
    setBusy(true)
    try {
      const job = await api.install(app.id, id, values)
      onClose()
      openJob(job.id, app.name)
      refreshApps()
    } catch (e) {
      if (e instanceof ApiError && e.checks) setChecks(e.checks)
      toast.error((e as Error).message)
    } finally {
      setBusy(false)
    }
  }

  return (
    <SheetFrame
      label="Install"
      onClose={onClose}
      footer={
        <>
          <span className="mr-auto flex items-center gap-2 font-mono text-xs text-muted">
            {checking ? <Spinner size={13} /> : <Led s={fails ? 'bad' : checks ? 'ok' : 'off'} />}
            {checking ? 'Testing…' : fails ? `${fails} to fix` : checks ? 'Ready' : ''}
          </span>
          <button className="key" onClick={onClose}>
            Cancel
          </button>
          <button className="key key-primary" onClick={install} disabled={busy || checking || fails > 0} data-loading={busy}>
            Install {app.name}
          </button>
        </>
      }
    >
      <div className="space-y-6">
        <div className="flex items-start gap-4">
          <AppIcon src={app.icon} name={app.name} size={56} />
          <div className="min-w-0">
            <h2 className="text-2xl font-semibold tracking-tight">{app.name}</h2>
            <p className="font-mono text-xs text-muted">
              {app.category} · {app.version} · {app.archs.join(' / ')}
              {app.minRamMb ? ` · ${app.minRamMb} MB RAM` : ''}
            </p>
          </div>
        </div>
        <div className="space-y-2">
          <p className="text-ink-2">{app.description}</p>
          {app.website && (
            <a className="inline-flex items-center gap-1 text-sm text-accent hover:underline" href={app.website} target="_blank" rel="noreferrer">
              Project website <ArrowUpRight size={13} />
            </a>
          )}
        </div>

        {app.screenshots && app.screenshots.length > 0 && <Gallery urls={app.screenshots} name={app.name} />}

        {app.notes && (
          <Notice label="Read first">{app.notes}</Notice>
        )}

        {app.settings.length > 0 && (
          <section className="space-y-3" aria-label="Settings">
            <h3 className="etch">Settings</h3>
            <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
              {app.settings.map((s) => (
                <SettingField key={s.key} setting={s} value={values[s.key] ?? ''} onChange={(v) => setValues((x) => ({ ...x, [s.key]: v }))} />
              ))}
            </div>
          </section>
        )}

        <section className="plate overflow-hidden" aria-live="polite">
          <div className="plate-head">
            <h3 className="etch">Self-test</h3>
            <span className="font-mono text-[0.6875rem] text-muted">{checks ? `${passes} pass · ${fails} fail` : '…'}</span>
          </div>
          <div className="px-4 py-1">{checks ? <ChecksList checks={checks} /> : <div className="py-3"><Spinner /></div>}</div>
        </section>

        <div>
          <button className="key key-flat key-sm -ml-2" onClick={() => setAdvanced(!advanced)} aria-expanded={advanced}>
            {advanced ? 'Hide advanced' : 'Advanced'}
          </button>
          {advanced && (
            <div className="mt-3 space-y-4">
              <div className="space-y-2 sm:w-1/2">
                <label htmlFor="app-id" className="etch block">
                  App ID
                </label>
                <input id="app-id" className="field font-mono" value={id} onChange={(e) => setId(e.target.value.toLowerCase())} />
                <p className="text-xs text-muted">Change it to run a second copy.</p>
              </div>
              <div className="space-y-2">
                <span className="etch block">Compose file</span>
                <pre className="well tape max-h-72 overflow-auto p-3">{app.compose}</pre>
              </div>
            </div>
          )}
        </div>
      </div>
    </SheetFrame>
  )
}

/** Screenshots from the project itself. A picture that fails to load is hidden. */
function Gallery({ urls, name }: { urls: string[]; name: string }) {
  const [broken, setBroken] = useState<Set<string>>(new Set())
  const [open, setOpen] = useState<number | null>(null)
  const strip = useRef<HTMLDivElement>(null)
  const ok = urls.filter((u) => !broken.has(u))
  useEffect(() => {
    const el = strip.current
    if (!el) return
    // A normal mouse wheel scrolls the strip sideways.
    const wheel = (e: WheelEvent) => {
      if (el.scrollWidth <= el.clientWidth || Math.abs(e.deltaX) > Math.abs(e.deltaY)) return
      e.preventDefault()
      el.scrollLeft += e.deltaY
    }
    el.addEventListener('wheel', wheel, { passive: false })
    return () => el.removeEventListener('wheel', wheel)
  }, [ok.length])
  useEffect(() => {
    if (open === null) return
    const key = (e: KeyboardEvent) => {
      if (e.key === 'ArrowRight') setOpen((i) => (i === null ? i : (i + 1) % ok.length))
      if (e.key === 'ArrowLeft') setOpen((i) => (i === null ? i : (i - 1 + ok.length) % ok.length))
    }
    window.addEventListener('keydown', key)
    return () => window.removeEventListener('keydown', key)
  }, [open, ok.length])
  if (ok.length === 0) return null
  const hide = (u: string) => setBroken((b) => new Set(b).add(u))
  return (
    <section aria-label={`${name} screenshots`} className="space-y-2">
      <h3 className="etch">Screenshots</h3>
      <div ref={strip} className="-mx-1 flex gap-2 overflow-x-auto px-1 pb-2">
        {ok.map((u, i) => (
          <button key={u} onClick={() => setOpen(i)} className="well shrink-0 overflow-hidden hover:border-rule-hi" aria-label={`Enlarge screenshot ${i + 1} of ${ok.length}`}>
            <img src={u} alt={`${name} screenshot ${i + 1}`} loading="lazy" referrerPolicy="no-referrer" onError={() => hide(u)} className="block h-32 w-auto max-w-[16rem] object-cover" />
          </button>
        ))}
      </div>
      <p className="text-xs text-muted">Pictures come from the {name} project and load from its own website.</p>
      {open !== null && ok[open] && (
        <Modal open onClose={() => setOpen(null)} wide label="Screenshot" title={`${name} · ${open + 1} of ${ok.length}`}
          footer={
            ok.length > 1 && (
              <>
                <button className="key" onClick={() => setOpen((open - 1 + ok.length) % ok.length)}>
                  Previous
                </button>
                <button className="key" onClick={() => setOpen((open + 1) % ok.length)}>
                  Next
                </button>
              </>
            )
          }
        >
          <img src={ok[open]} alt={`${name} screenshot ${open + 1}`} referrerPolicy="no-referrer" onError={() => hide(ok[open])} className="mx-auto block max-h-[70vh] w-auto max-w-full rounded-[2px] border border-rule" />
        </Modal>
      )}
    </section>
  )
}

export function SettingField({ setting: s, value, onChange }: { setting: Setting; value: string; onChange: (v: string) => void }) {
  const id = `set-${s.key}`
  return (
    <div className="space-y-2">
      <label htmlFor={id} className="etch block">
        {s.label}
        {s.required && !s.generate && <span className="text-accent"> *</span>}
      </label>
      {s.type === 'select' ? (
        <select id={id} className="field" value={value} onChange={(e) => onChange(e.target.value)}>
          {s.options?.map((o) => (
            <option key={o} value={o}>
              {o === 'true' ? 'Yes' : o === 'false' ? 'No' : o}
            </option>
          ))}
        </select>
      ) : s.type === 'password' ? (
        <PasswordInput id={id} value={value} onChange={onChange} autoComplete="new-password" placeholder={s.generate ? 'Leave empty to generate' : ''} />
      ) : (
        <input
          id={id}
          className={cx('field', (s.type === 'path' || s.type === 'port' || s.type === 'number') && 'font-mono')}
          value={value}
          inputMode={s.type === 'port' || s.type === 'number' ? 'numeric' : undefined}
          onChange={(e) => onChange(e.target.value)}
        />
      )}
      {s.description && <p className="text-xs text-muted">{s.description}</p>}
    </div>
  )
}

const composeExample = `services:
  whoami:
    image: traefik/whoami:v1.10
    restart: unless-stopped
    ports:
      - "8090:80"
`

function CustomSheet({ onClose }: { onClose: () => void }) {
  const { openJob, refreshApps } = useHaven()
  const [id, setId] = useState('')
  const [name, setName] = useState('')
  const [port, setPort] = useState('')
  const [compose, setCompose] = useState(composeExample)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')

  const submit = async () => {
    setBusy(true)
    setError('')
    try {
      const job = await api.installCustom(id, name, compose, port)
      onClose()
      openJob(job.id, name || id)
      refreshApps()
    } catch (e) {
      setError((e as Error).message)
    } finally {
      setBusy(false)
    }
  }

  return (
    <SheetFrame
      label="Custom app"
      onClose={onClose}
      footer={
        <>
          <button className="key" onClick={onClose}>
            Cancel
          </button>
          <button className="key key-primary" onClick={submit} disabled={busy || !id || !compose.trim()} data-loading={busy}>
            Install custom app
          </button>
        </>
      }
    >
      <div className="space-y-5">
        <div>
          <h2 className="text-2xl font-semibold tracking-tight">Bring your own Compose file</h2>
          <p className="mt-2 text-sm text-ink-2">
            Haven fills in <code className="font-mono text-xs text-ink">{'${APP_DATA}'}</code>, <code className="font-mono text-xs text-ink">{'${PUID}'}</code>,{' '}
            <code className="font-mono text-xs text-ink">{'${PGID}'}</code> and <code className="font-mono text-xs text-ink">{'${TZ}'}</code> for you.
          </p>
        </div>
        <div className="grid grid-cols-1 gap-4 sm:grid-cols-3">
          <div className="space-y-2">
            <label htmlFor="c-id" className="etch block">
              App ID <span className="text-accent">*</span>
            </label>
            <input id="c-id" className="field font-mono" value={id} onChange={(e) => setId(e.target.value.toLowerCase().replace(/[^a-z0-9-]/g, ''))} placeholder="my-app" />
          </div>
          <div className="space-y-2">
            <label htmlFor="c-name" className="etch block">
              Name
            </label>
            <input id="c-name" className="field" value={name} onChange={(e) => setName(e.target.value)} placeholder="My App" />
          </div>
          <div className="space-y-2">
            <label htmlFor="c-port" className="etch block">
              Web port
            </label>
            <input id="c-port" className="field font-mono" inputMode="numeric" value={port} onChange={(e) => setPort(e.target.value.replace(/\D/g, ''))} placeholder="8090" />
          </div>
        </div>
        <div className="space-y-2">
          <label htmlFor="c-compose" className="etch block">
            compose.yml
          </label>
          <textarea id="c-compose" className="field tape min-h-72" spellCheck={false} value={compose} onChange={(e) => setCompose(e.target.value)} />
        </div>
        <ErrorLine>{error}</ErrorLine>
      </div>
    </SheetFrame>
  )
}
