import { useEffect, useMemo, useState } from 'react'
import { ChevronDown, Copy, RefreshCw } from 'lucide-react'
import { api, type AppUpdate, type ImgStatus } from './api'
import { AppIcon, Led, Spinner, cx, toast, usePoll } from './components'
import { useHaven } from './context'
import { copyText } from './diagnosis'
import HavenUpdateCard from './selfupdate'

function ago(iso: string): string {
  if (!iso || iso.startsWith('0001')) return 'never'
  const s = Math.max(0, Math.round((Date.now() - new Date(iso).getTime()) / 1000))
  if (s < 60) return 'just now'
  if (s < 3600) return `${Math.round(s / 60)} min ago`
  if (s < 86400) return `${Math.round(s / 3600)} h ago`
  return `${Math.round(s / 86400)} d ago`
}

const imgWord: Record<ImgStatus['status'], string> = { current: 'Up to date', update: 'New version', restart: 'Restart needed', unknown: 'Can’t check' }
const imgTone: Record<ImgStatus['status'], string> = { current: 'tag-ok', update: 'tag-accent', restart: 'tag-accent', unknown: '' }

function shortRef(ref: string) {
  return ref.replace(/^(docker\.io\/|registry-1\.docker\.io\/)/, '')
}

export default function UpdatesPage() {
  const { openJob, refreshApps, role } = useHaven()
  const [fast, setFast] = useState(false)
  const { data: self, refresh: refreshSelf } = usePoll(async () => (role === 'admin' ? api.selfUpdate() : null), 60000, [role])
  const havenCard = role === 'admin' && self ? <HavenUpdateCard status={self} onRefresh={refreshSelf} /> : null
  const { data, refresh } = usePoll(api.updates, fast ? 2500 : 30000, [fast])
  const [picked, setPicked] = useState<Set<string>>(new Set())
  const [touched, setTouched] = useState(false)
  const [prune, setPrune] = useState(true)
  const [open, setOpen] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)

  useEffect(() => setFast(!!data?.checking), [data?.checking])
  const withUpdates = useMemo(() => (data?.apps || []).filter((a) => a.hasUpdate && !a.busy), [data])
  // Until you choose yourself, everything with an update is selected.
  useEffect(() => {
    if (!touched) setPicked(new Set(withUpdates.map((a) => a.id)))
  }, [withUpdates, touched])

  const toggle = (id: string) => {
    setTouched(true)
    setPicked((p) => {
      const n = new Set(p)
      n.has(id) ? n.delete(id) : n.add(id)
      return n
    })
  }
  const run = async (ids: string[]) => {
    if (ids.length === 0) return
    setBusy(true)
    try {
      const job = await api.applyUpdates(ids, prune)
      openJob(job.id, ids.length === 1 ? (data?.apps.find((a) => a.id === ids[0])?.name ?? 'app') : `${ids.length} apps`)
      setTouched(false)
      refreshApps()
      setTimeout(refresh, 4000)
    } catch (e) {
      toast.error((e as Error).message)
    } finally {
      setBusy(false)
    }
  }
  const check = async () => {
    try {
      await api.checkUpdates()
      setFast(true)
      refresh()
    } catch (e) {
      toast.error((e as Error).message)
    }
  }

  const selected = withUpdates.filter((a) => picked.has(a.id))
  const current = (data?.apps || []).filter((a) => !a.hasUpdate)
  const [showCurrent, setShowCurrent] = useState<boolean | null>(null)
  const currentOpen = showCurrent ?? withUpdates.length === 0
  const sys = data?.system?.supported ? data.system : null
  const sysNeeds = !!sys && (!!sys.dockerNew || sys.upgradable > 0)
  const havenNeeds = !!self?.available
  const total = withUpdates.length + (havenNeeds ? 1 : 0) + (sysNeeds ? 1 : 0)
  const headline = !data ? 'Loading…' : data.checking ? 'Checking for new versions…' : total > 0 ? `${total} thing${total === 1 ? '' : 's'} can be updated.` : data.apps.length ? 'Everything is up to date.' : 'No apps installed yet.'
  const rows = (list: AppUpdate[]) =>
    list.map((a) => <Row key={a.id} a={a} picked={picked.has(a.id)} onPick={() => toggle(a.id)} expanded={open === a.id} onExpand={() => setOpen(open === a.id ? null : a.id)} onUpdate={() => run([a.id])} disabled={busy} />)

  return (
    <div className="space-y-6">
      <div className="flex flex-wrap items-end justify-between gap-3">
        <div>
          <h1 className="text-3xl font-semibold tracking-tight sm:text-4xl">Updates</h1>
          <p className="mt-1.5 text-sm text-ink-2">
            {headline}
            {data && !data.checking && data.checked && !data.checked.startsWith('0001') && <span className="text-muted"> Last checked {ago(data.checked)}.</span>}
          </p>
        </div>
        <button className="key" onClick={check} disabled={!data || data.checking} data-loading={data?.checking}>
          {data?.checking ? <Spinner size={14} /> : <RefreshCw size={14} />} Check now
        </button>
      </div>

      <div className={cx('grid grid-cols-1 gap-3', role === 'admin' ? 'sm:grid-cols-3' : 'sm:grid-cols-2')} aria-label="Summary">
        {role === 'admin' && (
          <Tile
            label="Haven"
            led={!self ? 'off' : self.error ? 'warn' : havenNeeds ? 'warn' : 'ok'}
            value={!self ? '…' : havenNeeds ? `${self.current} → ${self.latest}` : self.current}
            note={!self ? '' : self.error ? 'Can’t check' : !self.supported ? 'Manual updates' : havenNeeds ? 'New version' : 'Up to date'}
          />
        )}
        <Tile
          label="Apps"
          led={!data ? 'off' : withUpdates.length ? 'warn' : 'ok'}
          value={!data ? '…' : `${data.apps.length} installed`}
          note={!data ? '' : withUpdates.length ? `${withUpdates.length} can be updated` : 'All up to date'}
        />
        <Tile
          label="Server software"
          led={!sys ? 'off' : sysNeeds ? 'warn' : 'ok'}
          value={!sys ? '—' : sysNeeds ? [sys.dockerNew ? 'Docker' : '', sys.upgradable ? `${sys.upgradable} packages` : ''].filter(Boolean).join(' + ') : 'Docker and Ubuntu'}
          note={!sys ? 'Not checked here' : sysNeeds ? 'Needs a command' : 'Up to date'}
        />
      </div>

      {havenCard}

      {data?.lastError && (
        <p role="alert" className="plate flex items-center gap-3 border-warn px-4 py-3 text-sm">
          <Led s="warn" /> {data.lastError}
        </p>
      )}

      {withUpdates.length > 0 && (
        <section className="plate overflow-hidden" aria-label="Apps that need an update">
          <div className="plate-head flex-wrap gap-y-2">
            <h2 className="etch">Needs an update · {withUpdates.length}</h2>
            <span className="flex flex-wrap items-center gap-x-4 gap-y-2">
              <label className="flex items-center gap-2 text-xs text-ink-2">
                <input type="checkbox" className="accent-[var(--accent)]" checked={prune} onChange={(e) => setPrune(e.target.checked)} />
                Remove old images afterwards
              </label>
              {withUpdates.length > 1 && (
                <button className="key key-sm" disabled={busy || selected.length === 0} onClick={() => run(selected.map((a) => a.id))}>
                  Update selected ({selected.length})
                </button>
              )}
              <button className="key key-sm key-primary" disabled={busy} onClick={() => run(withUpdates.map((a) => a.id))}>
                Update all ({withUpdates.length})
              </button>
            </span>
          </div>
          <ul className="divide-y divide-rule-2">{rows(data!.apps.filter((a) => a.hasUpdate))}</ul>
        </section>
      )}

      {current.length > 0 && (
        <section className="plate overflow-hidden" aria-label="Apps that are up to date">
          <button className="plate-head w-full text-left" onClick={() => setShowCurrent(!currentOpen)} aria-expanded={currentOpen}>
            <h2 className="etch">Up to date · {current.length}</h2>
            <span className="flex items-center gap-2 text-xs text-muted">
              {currentOpen ? 'Hide' : 'Show'}
              <ChevronDown size={14} className={cx('transition-transform', currentOpen && 'rotate-180')} />
            </span>
          </button>
          {currentOpen && <ul className="divide-y divide-rule-2">{rows(current)}</ul>}
        </section>
      )}

      {sys && <SystemCard sys={sys} />}

      <details className="text-xs text-muted">
        <summary className="cursor-pointer select-none hover:text-ink-2">How updates work</summary>
        <p className="mt-2 max-w-3xl">
          Haven asks each image registry whether the version behind an app's tag has changed, without downloading anything. An update downloads the new images and restarts the app. If it doesn't come back healthy, Haven puts the previous
          version back by itself. Server software (Docker and Ubuntu) changes the server itself, so Haven shows you the command instead of running it.
        </p>
      </details>
    </div>
  )
}

function Tile({ label, value, note, led }: { label: string; value: string; note: string; led: 'ok' | 'warn' | 'bad' | 'off' }) {
  return (
    <div className="plate px-4 py-3">
      <div className="flex items-center justify-between gap-2">
        <span className="etch text-[0.625rem]">{label}</span>
        <Led s={led} />
      </div>
      <div className="mt-2 truncate font-mono text-lg font-semibold">{value}</div>
      <div className={cx('mt-0.5 text-xs', led === 'warn' ? 'text-warn' : 'text-muted')}>{note}</div>
    </div>
  )
}

function Row({ a, picked, onPick, expanded, onExpand, onUpdate, disabled }: { a: AppUpdate; picked: boolean; onPick: () => void; expanded: boolean; onExpand: () => void; onUpdate: () => void; disabled: boolean }) {
  const busy = !!a.busy
  const cannot = a.images.length > 0 && a.images.every((i) => i.status === 'unknown')
  return (
    <li>
      <div className="flex items-center gap-3 px-3 py-3 sm:px-4">
        {a.hasUpdate ? (
          <input type="checkbox" className="accent-[var(--accent)]" checked={picked} onChange={onPick} disabled={busy} aria-label={`Select ${a.name}`} />
        ) : (
          <span className="w-[13px]" />
        )}
        <AppIcon src={a.icon} name={a.name} size={34} />
        <button className="min-w-0 flex-1 text-left" onClick={onExpand} aria-expanded={expanded}>
          <span className="flex items-center gap-2">
            <span className="truncate font-semibold">{a.name}</span>
            {a.hasUpdate ? <span className="tag tag-accent">{a.catalogVersion ? `Version ${a.catalogVersion}` : 'Update'}</span> : cannot ? <span className="tag">Can’t check</span> : <span className="tag tag-ok">Up to date</span>}
          </span>
          <span className="block truncate font-mono text-xs text-muted">
            {a.images.length} image{a.images.length === 1 ? '' : 's'}
            {a.version && a.version !== 'latest' ? ` · ${a.version}` : ''} · checked {ago(a.checked)}
          </span>
        </button>
        <button className="key key-sm key-icon" onClick={onExpand} aria-label="Details" aria-expanded={expanded}>
          <ChevronDown size={14} className={cx('transition-transform', expanded && 'rotate-180')} />
        </button>
        {a.hasUpdate && (
          <button className="key key-sm key-primary" onClick={onUpdate} disabled={busy || disabled}>
            {busy ? `${a.busy}…` : 'Update'}
          </button>
        )}
      </div>
      {expanded && (
        <ul className="divide-y divide-rule-2 border-t border-rule-2 bg-well px-4">
          {a.catalogVersion && (
            <li className="py-2 text-sm text-ink-2">
              The catalog has version <span className="font-mono text-ink">{a.catalogVersion}</span>. Updating also refreshes this app's settings file.
            </li>
          )}
          {a.images.map((i) => (
            <li key={i.ref} className="flex flex-wrap items-center gap-x-3 gap-y-1 py-2">
              <span className="min-w-0 flex-1 truncate font-mono text-xs" title={i.ref}>
                {shortRef(i.ref)}
              </span>
              <span className={cx('tag', imgTone[i.status])}>{imgWord[i.status]}</span>
              {i.detail && <span className="basis-full text-xs text-muted">{i.detail}</span>}
            </li>
          ))}
        </ul>
      )}
    </li>
  )
}

function SystemCard({ sys }: { sys: { supported: boolean; dockerCurrent?: string; dockerNew?: string; upgradable: number; command?: string } }) {
  const [copied, setCopied] = useState(false)
  if (!sys.supported) return null
  const nothing = !sys.dockerNew && sys.upgradable === 0
  if (nothing) {
    return (
      <section className="plate flex flex-wrap items-center gap-x-4 gap-y-2 px-4 py-3" aria-label="Server software">
        <Led s="ok" />
        <span className="text-sm">Docker and Ubuntu packages are up to date</span>
      </section>
    )
  }
  return (
    <section className="plate overflow-hidden" aria-label="Server software">
      <div className="plate-head">
        <h2 className="etch">Server software</h2>
        <span className="flex items-center gap-2">
          <Led s={nothing ? 'ok' : 'warn'} />
          <span className="etch text-[0.625rem]">{nothing ? 'Up to date' : 'Updates available'}</span>
        </span>
      </div>
      <div className="space-y-3 p-4 sm:p-5">
        {sys.dockerNew && (
          <p className="text-sm">
            <span className="font-medium">Docker</span> has a newer version: <span className="font-mono text-xs text-muted">{sys.dockerCurrent}</span> → <span className="font-mono text-xs">{sys.dockerNew}</span>
          </p>
        )}
        {sys.upgradable > 0 && (
          <p className="text-sm">
            <span className="font-semibold">{sys.upgradable}</span> Ubuntu package{sys.upgradable === 1 ? '' : 's'} can be updated.
          </p>
        )}
        {nothing && <p className="text-sm text-ink-2">Docker and the system packages are up to date.</p>}
        {!nothing && sys.command && (
          <>
            <p className="text-sm text-ink-2">System updates change the server itself, so Haven doesn't run them for you. Run this on the server:</p>
            <div className="well flex items-center gap-2 py-1 pl-3 pr-1">
              <code className="min-w-0 flex-1 overflow-x-auto whitespace-nowrap font-mono text-xs">{sys.command}</code>
              <button
                className="key key-flat key-sm key-icon"
                aria-label="Copy command"
                onClick={async () => {
                  setCopied(await copyText(sys.command!))
                  setTimeout(() => setCopied(false), 1500)
                }}
              >
                {copied ? <Led s="ok" /> : <Copy size={13} />}
              </button>
            </div>
            {sys.dockerNew && <p className="text-xs text-muted">Updating Docker restarts your apps for a moment. Do it when nobody is watching something.</p>}
          </>
        )}
      </div>
    </section>
  )
}
