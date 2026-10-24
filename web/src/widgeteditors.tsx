import { useContext, useEffect, useState, type ReactNode } from 'react'
import { Pencil, Plus, X } from 'lucide-react'
import { announceUIChange, api, type WidgetSettings } from './api'
import { ErrorLine, Modal, Spinner, cx } from './components'
import { Ctx } from './context'
import { CURRENCIES, CitySearch, Chip, Field, MARKET_PICKS, NewsSources, Segmented, pickName } from './look'
import { Suggest, type Suggestion } from './suggest'
import { W_NAMES, type WKey } from './widgetmeta'

// One editor dialog per widget. Every widget has the same pencil, and the
// Settings → Widgets list opens the very same dialogs.

/** Saves only the keys that were edited, on top of what is stored now, so two editors can't undo each other. */
export async function saveWidgets(patch: Partial<WidgetSettings>) {
  const ui = await api.ui()
  await api.saveUI({ ...ui, widgets: { ...ui.widgets, ...patch } })
  announceUIChange()
}

// --- time zones (also used by the Clock widget) -----------------------------------------

export function zoneLabel(z: string) {
  return z.split('/').pop()!.replace(/_/g, ' ')
}

// Names people search for that the system list spells another way.
const ZONE_EXTRAS = ['Asia/Kolkata', 'Asia/Ho_Chi_Minh', 'Asia/Kathmandu', 'Asia/Yangon', 'Europe/Kyiv', 'America/Argentina/Buenos_Aires', 'Pacific/Auckland']

let zoneList: string[] | null = null
function allZones(): string[] {
  if (zoneList) return zoneList
  let z: string[] = []
  try {
    z = (Intl as any).supportedValuesOf('timeZone') as string[]
  } catch {
    /* older browser: the extras still work */
  }
  zoneList = [...new Set([...z, ...ZONE_EXTRAS, 'UTC'])]
  return zoneList
}

export function offsetOf(zone: string): string {
  try {
    return new Intl.DateTimeFormat('en', { timeZone: zone, timeZoneName: 'shortOffset' }).formatToParts(new Date()).find((p) => p.type === 'timeZoneName')?.value.replace('GMT', 'UTC') || ''
  } catch {
    return ''
  }
}

export function zoneSuggestions(query: string, taken: string[]): Suggestion[] | null {
  const q = query.trim().toLowerCase().replace(/\s+/g, '_')
  if (q.length < 2) return null
  const starts: string[] = []
  const contains: string[] = []
  for (const z of allZones()) {
    if (taken.includes(z)) continue
    const city = z.split('/').pop()!.toLowerCase()
    if (city.startsWith(q)) starts.push(z)
    else if (z.toLowerCase().includes(q)) contains.push(z)
  }
  return [...starts, ...contains].slice(0, 8).map((z) => ({ key: z, label: zoneLabel(z), hint: `${z.split('/')[0].replace(/_/g, ' ')} · ${offsetOf(z)}` }))
}

/** "plex.local:32400" becomes http://plex.local:32400; a bare website name gets https://. */
export function withScheme(raw: string) {
  const u = raw.trim()
  if (!u || /^[a-z][a-z0-9+.-]*:\/\//i.test(u)) return u
  const local = /^(localhost|\d{1,3}(\.\d{1,3}){3}|[^/]*\.local|[^/]*:\d+)([/:?#]|$)/i.test(u)
  return (local ? 'http://' : 'https://') + u
}

// --- what each editor changes ------------------------------------------------------------

function pick(kind: WKey, w: WidgetSettings): Partial<WidgetSettings> {
  switch (kind) {
    case 'weather':
      return { places: w.places, units: w.units }
    case 'news':
      return { newsScope: w.newsScope, sources: w.sources }
    case 'currency':
      return { home: w.home, currencies: w.currencies }
    case 'markets':
      return { markets: w.markets }
    case 'clock':
      return { clocks: w.clocks || [], clockFormat: w.clockFormat === '12' ? '12' : '24' }
    case 'notes':
      return { notes: w.notes || '' }
    case 'links':
      return { links: (w.links || []).filter((l) => l.name.trim() || l.url.trim()).map((l) => ({ name: l.name.trim(), url: withScheme(l.url) })) }
    case 'status':
      return { checks: (w.checks || []).filter((c) => c.name.trim() || c.target.trim()).map((c) => ({ name: c.name.trim(), target: c.target.trim() })) }
  }
}

const WIDE: Record<WKey, boolean> = { weather: false, news: true, currency: false, markets: true, clock: false, notes: false, links: true, status: true }

type Edit = { w: WidgetSettings; set: (patch: Partial<WidgetSettings>) => void }

function WeatherBody({ w, set }: Edit) {
  return (
    <div className="space-y-5">
      <p className="text-sm text-ink-2">Pick up to 5 cities. Weather and Google News follow them.</p>
      <Field label="Cities" hint="The first one is the main city; remove it to promote the next.">
        <CitySearch places={w.places} onAdd={(p) => !w.places.some((x) => x.lat === p.lat && x.lon === p.lon) && set({ places: [...w.places, p] })} onRemove={(i) => set({ places: w.places.filter((_, j) => j !== i) })} />
      </Field>
      <Field label="Units">
        <Segmented label="Units" value={w.units} options={[['metric', '°C, km/h'], ['imperial', '°F, mph']]} onChange={(v) => set({ units: v })} />
      </Field>
    </div>
  )
}

function NewsBody({ w, set }: Edit) {
  return (
    <div className="space-y-5">
      <p className="text-sm text-ink-2">Mix Google News for your cities with the publishers you read.</p>
      <Field label="Google News">
        <Segmented label="Google News" value={w.newsScope} options={[['local', 'My cities'], ['top', 'Top stories'], ['off', 'Off']]} onChange={(v) => set({ newsScope: v })} />
      </Field>
      <Field label="Publishers">
        <NewsSources sources={w.sources || []} onChange={(sources) => set({ sources })} />
      </Field>
    </div>
  )
}

function CurrencyBody({ w, set }: Edit) {
  return (
    <div className="space-y-5">
      <p className="text-sm text-ink-2">Rates of other currencies in your home currency.</p>
      <Field label="Home currency">
        <select className="field h-9 w-28 font-mono" value={w.home} onChange={(e) => set({ home: e.target.value, currencies: w.currencies.filter((c) => c !== e.target.value) })} aria-label="Home currency">
          {CURRENCIES.map((c) => (
            <option key={c}>{c}</option>
          ))}
        </select>
      </Field>
      <Field label="Compare with">
        <div className="flex flex-wrap items-center gap-2">
          {w.currencies.map((c) => (
            <Chip key={c} label={c} onRemove={() => set({ currencies: w.currencies.filter((x) => x !== c) })} />
          ))}
          {w.currencies.length < 8 && (
            <select className="field h-7 w-24 font-mono text-xs" value="" onChange={(e) => e.target.value && set({ currencies: [...w.currencies, e.target.value] })} aria-label="Add currency">
              <option value="">+ Add</option>
              {CURRENCIES.filter((c) => c !== w.home && !w.currencies.includes(c)).map((c) => (
                <option key={c}>{c}</option>
              ))}
            </select>
          )}
        </div>
      </Field>
    </div>
  )
}

function MarketsBody({ w, set }: Edit) {
  const [symbol, setSymbol] = useState('')
  const add = (s: string) => {
    s = s.trim().toUpperCase()
    if (!s || w.markets.includes(s) || w.markets.length >= 10) return
    set({ markets: [...w.markets, s] })
  }
  return (
    <div className="space-y-5">
      <p className="text-sm text-ink-2">Gold, silver, indices and shares. Prices can be delayed.</p>
      <Field label="Showing">
        <div className="flex flex-wrap gap-2">
          {w.markets.map((s) => (
            <Chip key={s} label={pickName(s)} onRemove={() => set({ markets: w.markets.filter((x) => x !== s) })} />
          ))}
          {w.markets.length === 0 && <span className="text-sm text-muted">Nothing selected.</span>}
        </div>
      </Field>
      <Field label="Add">
        <div className="flex flex-wrap gap-1.5">
          {MARKET_PICKS.filter(([s]) => !w.markets.includes(s)).map(([s, name]) => (
            <button key={s} className="key key-sm h-7 px-2 text-[0.6875rem]" onClick={() => add(s)} disabled={w.markets.length >= 10}>
              + {name}
            </button>
          ))}
        </div>
      </Field>
      <Field
        label="Other symbol"
        hint={
          <>
            Yahoo Finance symbols. For Indian shares add <span className="font-mono">.NS</span> (NSE) or <span className="font-mono">.BO</span> (BSE), for example <span className="font-mono">TATAMOTORS.NS</span>.
          </>
        }
      >
        <form
          className="flex gap-2"
          onSubmit={(e) => {
            e.preventDefault()
            add(symbol)
            setSymbol('')
          }}
        >
          <input className="field h-9 font-mono uppercase" placeholder="Symbol" value={symbol} onChange={(e) => setSymbol(e.target.value)} maxLength={20} aria-label="Market symbol" />
          <button className="key h-9" disabled={!symbol.trim()}>
            Add
          </button>
        </form>
      </Field>
    </div>
  )
}

function ClockBody({ w, set }: Edit) {
  const [q, setQ] = useState('')
  const zones = w.clocks || []
  return (
    <div className="space-y-3">
      <Segmented label="Time format" value={w.clockFormat === '12' ? '12' : '24'} options={[['24', '24-hour'], ['12', '12-hour']]} onChange={(v) => set({ clockFormat: v })} />
      <p className="text-sm text-ink-2">Show the time in up to 3 other cities, next to your own.</p>
      {zones.length > 0 && (
        <ul className="well divide-y divide-rule-2">
          {zones.map((z) => (
            <li key={z} className="flex items-center justify-between gap-2 px-3 py-2 text-sm">
              <span>
                {zoneLabel(z)} <span className="text-muted">· {z.split('/')[0].replace(/_/g, ' ')} · {offsetOf(z)}</span>
              </span>
              <button className="key key-flat key-sm key-icon" onClick={() => set({ clocks: zones.filter((x) => x !== z) })} aria-label={`Remove ${zoneLabel(z)}`}>
                <X size={13} />
              </button>
            </li>
          ))}
        </ul>
      )}
      {zones.length < 3 ? (
        <Suggest label="City or time zone" placeholder="Search a city, e.g. London or Tokyo" value={q} onChange={setQ} items={zoneSuggestions(q, zones)} empty="No city matches. Try a nearby big city." onPick={(s) => set({ clocks: [...zones, s.key] })} />
      ) : (
        <p className="text-xs text-muted">That's the most: 3 extra clocks. Remove one to add another.</p>
      )}
    </div>
  )
}

function NotesBody({ w, set }: Edit) {
  const text = w.notes || ''
  return (
    <div>
      <textarea className="field h-64 w-full resize-y text-sm" autoFocus maxLength={4000} value={text} onChange={(e) => set({ notes: e.target.value })} placeholder="Write something the household should see…" aria-label="Note" />
      <div className="mt-1 text-right font-mono text-[0.6875rem] text-muted">{text.length} / 4000</div>
    </div>
  )
}

function Rows<T extends { name: string }>({ rows, onChange, blank, max, nameLabel, valueKey, valueLabel, placeholder, addLabel, empty }: {
  rows: T[]
  onChange: (r: T[]) => void
  blank: T
  max: number
  nameLabel: string
  valueKey: keyof T & string
  valueLabel: string
  placeholder: string
  addLabel: string
  empty: string
}) {
  const edit = (i: number, patch: Partial<T>) => onChange(rows.map((x, k) => (k === i ? { ...x, ...patch } : x)))
  return (
    <div className="space-y-3">
      {rows.length === 0 && <p className="text-sm text-muted">{empty}</p>}
      {rows.map((r, i) => (
        <div key={i} className="grid grid-cols-[minmax(0,1fr)_auto] items-start gap-2 sm:grid-cols-[minmax(0,1fr)_minmax(0,2fr)_auto]">
          <input className="field" placeholder="Name, e.g. Router" maxLength={30} value={r.name} onChange={(e) => edit(i, { name: e.target.value } as Partial<T>)} aria-label={`${nameLabel} ${i + 1}`} />
          <input className="field order-3 col-span-2 font-mono text-xs sm:order-none sm:col-span-1" placeholder={placeholder} value={String(r[valueKey] ?? '')} onChange={(e) => edit(i, { [valueKey]: e.target.value } as Partial<T>)} aria-label={`${valueLabel} ${i + 1}`} inputMode="url" />
          <button className="key key-flat key-icon" onClick={() => onChange(rows.filter((_, k) => k !== i))} aria-label={`Remove ${nameLabel.toLowerCase()} ${i + 1}`}>
            <X size={14} />
          </button>
        </div>
      ))}
      {rows.length < max && (
        <button className="key key-sm" onClick={() => onChange([...rows, blank])}>
          <Plus size={13} /> {addLabel}
        </button>
      )}
    </div>
  )
}

function LinksBody({ w, set }: Edit) {
  return (
    <div className="space-y-3">
      <p className="text-sm text-ink-2">Each link opens in a new tab. Haven fetches the site's icon for you. You can leave off the https:// part.</p>
      <Rows rows={w.links || []} onChange={(links) => set({ links })} blank={{ name: '', url: '' }} max={24} nameLabel="Name of link" valueKey="url" valueLabel="Address of link" placeholder="Address, e.g. 192.168.1.1 or example.com" addLabel="Add a link" empty="No links yet." />
    </div>
  )
}

function StatusBody({ w, set }: Edit) {
  return (
    <div className="space-y-3">
      <p className="text-sm text-ink-2">Use a web address (https://…) or host:port, for example 192.168.1.1:80. Haven checks every 30 seconds and tells you in the bell if something goes down.</p>
      <Rows rows={w.checks || []} onChange={(checks) => set({ checks })} blank={{ name: '', target: '' }} max={12} nameLabel="Name of check" valueKey="target" valueLabel="Address of check" placeholder="192.168.1.1:80 or https://example.com" addLabel="Add a check" empty="Nothing is being watched yet." />
    </div>
  )
}

const BODIES: Record<WKey, (e: Edit) => ReactNode> = {
  weather: (e) => <WeatherBody {...e} />,
  news: (e) => <NewsBody {...e} />,
  currency: (e) => <CurrencyBody {...e} />,
  markets: (e) => <MarketsBody {...e} />,
  clock: (e) => <ClockBody {...e} />,
  notes: (e) => <NotesBody {...e} />,
  links: (e) => <LinksBody {...e} />,
  status: (e) => <StatusBody {...e} />,
}

/** The settings dialog of one widget. Save stores them; Cancel throws the changes away. */
export function WidgetEditor({ kind, onClose }: { kind: WKey; onClose: () => void }) {
  const [w, setW] = useState<WidgetSettings | null>(null)
  const [busy, setBusy] = useState(false)
  const [err, setErr] = useState('')
  useEffect(() => {
    api.ui().then((u) => setW(u.widgets)).catch((e) => setErr(e.message))
  }, [])
  const set = (patch: Partial<WidgetSettings>) => setW((cur) => (cur ? { ...cur, ...patch } : cur))
  const save = async () => {
    if (!w) return
    setBusy(true)
    setErr('')
    try {
      await saveWidgets(pick(kind, w))
      onClose()
    } catch (e: any) {
      setErr(e.message)
      setBusy(false)
    }
  }
  return (
    <Modal
      open
      onClose={onClose}
      wide={WIDE[kind]}
      title={`${W_NAMES[kind]} settings`}
      label="Widget settings"
      footer={
        <>
          <button className="key" onClick={onClose} disabled={busy}>
            Cancel
          </button>
          <button className="key key-primary" onClick={save} disabled={busy || !w}>
            Save
          </button>
        </>
      }
    >
      {w ? BODIES[kind]({ w, set }) : err ? null : <Spinner />}
      {err && <ErrorLine className="mt-3">{err}</ErrorLine>}
    </Modal>
  )
}

// --- the pencil, and a call-to-action button, for every widget ----------------------------

function useAdmin() {
  return useContext(Ctx)?.role === 'admin'
}

/** The same pencil on every widget. Only the admin sees it. */
export function EditPencil({ kind }: { kind: WKey }) {
  const admin = useAdmin()
  const [open, setOpen] = useState(false)
  if (!admin) return null
  return (
    <>
      <button
        // Shown when you point at the widget (or tab to it); always shown on touch screens, which can't hover.
        className="key key-flat key-sm key-icon transition-opacity focus-visible:opacity-100 [@media(hover:hover)]:opacity-0 [@media(hover:hover)]:group-focus-within/w:opacity-100 [@media(hover:hover)]:group-hover/w:opacity-100"
        onClick={() => setOpen(true)}
        aria-label={`Edit ${W_NAMES[kind]}`}
        title={`Edit ${W_NAMES[kind]}`}
      >
        <Pencil size={13} />
      </button>
      {open && <WidgetEditor kind={kind} onClose={() => setOpen(false)} />}
    </>
  )
}

/** A button for an empty widget ("Write a note"). Only the admin sees it. */
export function EditCta({ kind, children }: { kind: WKey; children: ReactNode }) {
  const admin = useAdmin()
  const [open, setOpen] = useState(false)
  if (!admin) return null
  return (
    <>
      <button className="key key-sm key-primary" onClick={() => setOpen(true)}>
        {children}
      </button>
      {open && <WidgetEditor kind={kind} onClose={() => setOpen(false)} />}
    </>
  )
}

// --- Settings → Widgets: a list with an Edit button for each ---------------------------------

function summary(k: WKey, w: WidgetSettings): string {
  switch (k) {
    case 'weather':
      return w.places.length ? `${w.places.map((p) => p.name).join(', ')} · ${w.units === 'imperial' ? '°F' : '°C'}` : 'No city chosen yet'
    case 'news':
      return `${w.newsScope === 'off' ? 'Google News off' : w.newsScope === 'top' ? 'Top stories' : 'Your cities'} · ${(w.sources || []).length} publisher${(w.sources || []).length === 1 ? '' : 's'}`
    case 'currency':
      return `${w.home} against ${w.currencies.join(', ') || 'nothing yet'}`
    case 'markets':
      return w.markets.length ? w.markets.map(pickName).join(', ') : 'Nothing selected'
    case 'clock':
      return (w.clocks || []).length ? (w.clocks || []).map(zoneLabel).join(', ') : 'Local time only'
    case 'notes':
      return (w.notes || '').trim() ? (w.notes || '').trim().slice(0, 60) : 'Empty'
    case 'links':
      return `${(w.links || []).length} link${(w.links || []).length === 1 ? '' : 's'}`
    case 'status':
      return `${(w.checks || []).length} check${(w.checks || []).length === 1 ? '' : 's'}`
  }
}

export function WidgetsPanel() {
  const [w, setW] = useState<WidgetSettings | null>(null)
  const [editing, setEditing] = useState<WKey | null>(null)
  const [err, setErr] = useState('')
  useEffect(() => {
    const load = () => api.ui().then((u) => setW(u.widgets)).catch((e) => setErr(e.message))
    load()
    window.addEventListener('haven:ui', load)
    return () => window.removeEventListener('haven:ui', load)
  }, [])
  if (!w) {
    return (
      <section className="plate p-5">
        {err ? <ErrorLine>{err}</ErrorLine> : <Spinner />}
      </section>
    )
  }
  const hidden = w.hidden || []
  const order = ((w.order || []) as WKey[]).filter((k) => k in W_NAMES)
  const toggle = async (k: WKey) => {
    setErr('')
    try {
      await saveWidgets({ hidden: hidden.includes(k) ? hidden.filter((h) => h !== k) : [...hidden, k] })
    } catch (e: any) {
      setErr((e as Error).message)
    }
  }
  return (
    <section className="plate overflow-hidden" aria-label="Widgets">
      <div className="plate-head">
        <h2 className="etch">Widgets</h2>
        <span className="etch text-[0.625rem]">Show · Edit</span>
      </div>
      <ul className="divide-y divide-rule-2">
        {order.map((k) => (
          <li key={k} className="flex flex-wrap items-center gap-x-4 gap-y-2 px-4 py-3 sm:px-5">
            <label className="flex w-full items-center gap-3 sm:w-auto sm:flex-1">
              <input type="checkbox" className="h-4 w-4 accent-[var(--accent)]" checked={!hidden.includes(k)} onChange={() => toggle(k)} aria-label={`Show ${W_NAMES[k]}`} />
              <span className="min-w-0">
                <span className="block text-sm font-semibold">{W_NAMES[k]}</span>
                <span className={cx('block truncate text-xs', hidden.includes(k) ? 'text-muted' : 'text-ink-2')}>{summary(k, w)}</span>
              </span>
            </label>
            <button className="key key-sm" onClick={() => setEditing(k)} aria-label={`Edit ${W_NAMES[k]} settings`}>
              <Pencil size={13} /> Edit
            </button>
          </li>
        ))}
      </ul>
      {err && <div className="border-t border-rule-2 p-4"><ErrorLine>{err}</ErrorLine></div>}
      <p className="border-t border-rule-2 px-4 py-3 text-xs text-muted sm:px-5">
        To rearrange the widgets, open the Overview and choose Edit layout. Data comes from Open-Meteo, Google News and your chosen publishers, the European Central Bank and Yahoo Finance. Your server fetches and caches it.
      </p>
      {editing && <WidgetEditor kind={editing} onClose={() => setEditing(null)} />}
    </section>
  )
}
