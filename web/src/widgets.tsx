import { useContext, useEffect, useRef, useState } from 'react'
import { Check, ChevronLeft, ChevronRight, Eye, EyeOff, GripVertical, LayoutGrid, RotateCcw, Cloud, CloudDrizzle, CloudFog, CloudHail, CloudLightning, CloudMoon, CloudRain, CloudSnow, CloudSun, Moon, Sun, type LucideIcon } from 'lucide-react'
import { announceUIChange, api, atLeast, formatMoney, formatNumber, timeAgo, type CurrencyData, type NetTest, type News, type NewsItem, type Quote, type Weather, type WidgetData } from './api'
import { Led, Modal, Spinner, Trace, cx, usePoll } from './components'
import { QR } from './diagnosis'
import { Ctx } from './context'
import { Widget } from './widgetshell'
import { ClockWidget, LinksWidget, NotesWidget, StatusWidget } from './extrawidgets'
import { AirQualityWidget } from './airquality'
import { EditCta, EditPencil } from './widgeteditors'
import { PRIVATE_KEYS, W_DEFAULT, W_NAMES, type WKey, type WidgetWidth } from './widgetmeta'
import { useWidgetLayoutMotion, widgetOrder, type WidgetLayout } from './widgetlayout'

// Widgets for the Overview (and their data helpers, shared with TV mode).

export function useWidgets() {
  const poll = usePoll(api.widgets, 5 * 60 * 1000)
  useEffect(() => {
    const on = () => poll.refresh()
    window.addEventListener('haven:ui', on)
    return () => window.removeEventListener('haven:ui', on)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])
  return poll
}

// --- weather codes (WMO) ------------------------------------------------------

export function weatherInfo(code: number, isDay = true): { label: string; Icon: LucideIcon } {
  if (code === 0) return { label: 'Clear', Icon: isDay ? Sun : Moon }
  if (code === 1 || code === 2) return { label: code === 1 ? 'Mostly clear' : 'Partly cloudy', Icon: isDay ? CloudSun : CloudMoon }
  if (code === 3) return { label: 'Overcast', Icon: Cloud }
  if (code === 45 || code === 48) return { label: 'Fog', Icon: CloudFog }
  if (code >= 51 && code <= 57) return { label: 'Drizzle', Icon: CloudDrizzle }
  if ((code >= 61 && code <= 67) || (code >= 80 && code <= 82)) return { label: code >= 80 ? 'Showers' : 'Rain', Icon: CloudRain }
  if ((code >= 71 && code <= 77) || code === 85 || code === 86) return { label: 'Snow', Icon: CloudSnow }
  if (code === 95) return { label: 'Thunderstorm', Icon: CloudLightning }
  if (code === 96 || code === 99) return { label: 'Storm with hail', Icon: CloudHail }
  return { label: 'Unknown', Icon: Cloud }
}

export function weekday(date: string, i: number): string {
  if (i === 0) return 'Today'
  return new Date(`${date}T12:00:00`).toLocaleDateString(undefined, { weekday: 'short' })
}

export function changeClass(n: number) {
  return n > 0.005 ? 'text-ok' : n < -0.005 ? 'text-bad' : 'text-muted'
}

export function pct(n: number) {
  return `${n > 0 ? '+' : ''}${n.toFixed(2)}%`
}

// --- shells ---------------------------------------------------------------------

export function Problem({ text }: { text?: string }) {
  const admin = useContext(Ctx)?.role === 'admin'
  const [testing, setTesting] = useState(false)
  if (!text) return null
  // "can't reach host: reason" gets a step-by-step test of that host.
  const host = text.match(/can't reach ([a-z0-9.-]+)/i)?.[1]
  return (
    <div className="flex flex-wrap items-center gap-x-3 gap-y-1 px-4 py-2 font-mono text-[0.6875rem] text-warn">
      <span className="flex min-w-0 items-center gap-2">
        <Led s="warn" /> <span className="break-words">{text}</span>
      </span>
      {admin && host && (
        <button className="key key-sm ml-auto shrink-0 font-sans" onClick={() => setTesting(true)}>
          Test connection
        </button>
      )}
      {testing && host && <ConnectionTest host={host} onClose={() => setTesting(false)} />}
    </div>
  )
}

function ConnectionTest({ host, onClose }: { host: string; onClose: () => void }) {
  const [res, setRes] = useState<NetTest | null>(null)
  const [err, setErr] = useState('')
  const [run, setRun] = useState(0)
  useEffect(() => {
    setRes(null)
    setErr('')
    api.widgetTest(host).then(setRes).catch((e) => setErr(e.message))
  }, [host, run])
  return (
    <Modal
      open
      onClose={onClose}
      title={`Can this server reach ${host}?`}
      label="Connection test"
      footer={
        <>
          <button className="key" onClick={() => setRun(run + 1)} disabled={!res && !err}>
            Test again
          </button>
          <button className="key key-primary" onClick={onClose}>
            Done
          </button>
        </>
      }
    >
      <div className="font-sans text-sm text-ink">
        {!res && !err && (
          <div className="flex items-center gap-3 text-ink-2">
            <Spinner /> Testing step by step. This can take up to half a minute.
          </div>
        )}
        {err && <p className="text-bad">{err}</p>}
        {res && (
          <div className="space-y-4">
            <ol className="well divide-y divide-rule-2">
              {res.steps.map((s) => (
                <li key={s.name} className="flex items-center gap-3 px-3 py-2">
                  <Led s={s.ok ? 'ok' : 'bad'} />
                  <span className="min-w-0 flex-1">
                    <span className="block">{s.name}</span>
                    <span className="block text-xs text-ink-2">{s.detail}</span>
                  </span>
                  {s.ms !== undefined && <span className="font-mono text-xs text-muted">{s.ms} ms</span>}
                </li>
              ))}
            </ol>
            <p className={cx('font-semibold', res.works ? 'text-ok' : 'text-bad')}>{res.verdict}</p>
            {res.fix && <p className="text-ink-2">{res.fix}</p>}
          </div>
        )}
      </div>
    </Modal>
  )
}

// --- weather --------------------------------------------------------------------

/** Tabs that scroll sideways: with the mouse wheel, with arrows, or by touch. */
export function CityTabs({ names, value, onChange }: { names: string[]; value: number; onChange: (i: number) => void }) {
  const box = useRef<HTMLDivElement>(null)
  const [edge, setEdge] = useState({ left: false, right: false })
  const measure = () => {
    const el = box.current
    if (el) setEdge({ left: el.scrollLeft > 2, right: el.scrollLeft + el.clientWidth < el.scrollWidth - 2 })
  }
  useEffect(() => {
    const el = box.current
    if (!el) return
    // A normal mouse wheel scrolls up and down; turn it into sideways movement over the tabs.
    const wheel = (e: WheelEvent) => {
      if (el.scrollWidth <= el.clientWidth || Math.abs(e.deltaX) > Math.abs(e.deltaY)) return
      e.preventDefault()
      el.scrollLeft += e.deltaY
    }
    el.addEventListener('wheel', wheel, { passive: false })
    el.addEventListener('scroll', measure, { passive: true })
    const ro = new ResizeObserver(measure)
    ro.observe(el)
    measure()
    return () => {
      el.removeEventListener('wheel', wheel)
      el.removeEventListener('scroll', measure)
      ro.disconnect()
    }
  }, [names.length])
  // Keep the chosen tab in view.
  useEffect(() => {
    box.current?.querySelector<HTMLElement>('[aria-selected=true]')?.scrollIntoView({ block: 'nearest', inline: 'nearest' })
  }, [value])
  if (names.length < 2) return null
  const nudge = (d: number) => box.current?.scrollBy({ left: d * 160, behavior: 'smooth' })
  return (
    <div className="relative flex items-center border-b border-rule-2">
      {edge.left && (
        <button className="key key-flat key-sm key-icon z-10 h-7 w-7 shrink-0" onClick={() => nudge(-1)} aria-label="Scroll left">
          <ChevronLeft size={14} />
        </button>
      )}
      <div ref={box} className="flex min-w-0 flex-1 gap-1 overflow-x-auto px-2 py-1.5 [scrollbar-width:none]" role="tablist">
        {names.map((n, i) => (
          <button key={n + i} role="tab" aria-selected={value === i} title={n} onClick={() => onChange(i)} className={cx('key key-sm h-7 max-w-full shrink-0 border-transparent px-2 text-[0.6875rem]', value !== i && 'key-flat')}>
            <span className="min-w-0 truncate">{n}</span>
          </button>
        ))}
      </div>
      {edge.right && (
        <button className="key key-flat key-sm key-icon z-10 h-7 w-7 shrink-0" onClick={() => nudge(1)} aria-label="Scroll right">
          <ChevronRight size={14} />
        </button>
      )}
    </div>
  )
}

function WeatherWidget({ list, error }: { list?: Weather[]; error?: string }) {
  const [i, setI] = useState(0)
  if (!list || list.length === 0) {
    return (
      <Widget title="Weather" aside={<EditPencil kind="weather" />}>
        {error ? (
          <Problem text={error} />
        ) : (
          <div className="flex flex-1 flex-col items-start justify-center gap-3 p-4">
            <p className="text-sm text-ink-2">Choose your city to see the weather and local news.</p>
            <EditCta kind="weather">Choose city</EditCta>
          </div>
        )}
      </Widget>
    )
  }
  const w = list[Math.min(i, list.length - 1)]
  const days = w.days || []
  const { label, Icon } = weatherInfo(w.code, w.isDay)
  return (
    <Widget title={list.length > 1 ? 'Weather' : `Weather · ${w.place}`} aside={<EditPencil kind="weather" />}>
      <CityTabs names={list.map((x) => x.place)} value={Math.min(i, list.length - 1)} onChange={setI} />
      <Problem text={error} />
      <div className="widget-weather flex-1">
        <div className="min-w-0">
          <div className="flex items-center gap-3 px-4 pb-2 pt-4">
            <Icon size={40} strokeWidth={1.4} className="shrink-0 text-accent" aria-hidden />
            <div className="min-w-0">
              <div className="widget-temperature">
                <span className="readout">{Math.round(w.temp)}</span>
                <span className="readout text-2xl text-muted">°{w.units}</span>
              </div>
              <div className="text-sm text-ink-2">{label}</div>
            </div>
          </div>
          <p className="px-4 pb-4 font-mono text-[0.6875rem] text-muted">
            feels {Math.round(w.feels)}° · humidity {w.humidity}% · wind {Math.round(w.wind)} {w.windUnits}
          </p>
        </div>
        {days.length > 0 && <ol className="widget-forecast grid gap-px bg-rule-2" aria-label="Daily forecast" style={{ gridTemplateColumns: `repeat(${days.length}, minmax(0, 1fr))` }}>
          {days.map((d, k) => {
            const day = weatherInfo(d.code)
            return (
              <li key={d.date} className="flex min-w-0 flex-col items-center gap-1 bg-plate px-0.5 py-2" title={`${day.label}, ${d.rain}% chance of rain`}>
                <span className="widget-forecast-day etch max-w-full truncate text-[0.5625rem]">{weekday(d.date, k)}</span>
                <day.Icon size={16} className="shrink-0 text-ink-2" aria-label={day.label} />
                <span className="text-center font-mono text-xs leading-tight">
                  {Math.round(d.max)}°<span className="block text-muted">{Math.round(d.min)}°</span>
                </span>
                <span className="widget-more text-center text-[0.625rem] text-muted">{d.rain}% rain</span>
              </li>
            )
          })}
        </ol>}
      </div>
    </Widget>
  )
}

// --- markets --------------------------------------------------------------------

function MarketsWidget({ quotes, error }: { quotes?: Quote[]; error?: string }) {
  return (
    <Widget title="Markets" aside={<span className="flex items-center gap-2"><span className="etch text-[0.5625rem]">day change</span><EditPencil kind="markets" /></span>}>
      <Problem text={error} />
      {!quotes ? (
        error ? null : <div className="p-4"><Spinner /></div>
      ) : quotes.length === 0 ? (
        <p className="p-4 text-sm text-muted">Add gold, silver or stocks in Settings.</p>
      ) : (
        <ul className="widget-paired-list flex-1">
          {quotes.map((q) => (
            <li key={q.symbol} className="widget-quote-row items-center px-4 py-2.5" data-history={(q.history?.length || 0) > 1 || undefined}>
              <div className="min-w-0">
                <div className="widget-wrap line-clamp-2 text-sm font-medium" title={q.name}>{q.name}</div>
                <div className="widget-wrap font-mono text-[0.6875rem] text-muted">
                  {q.error ? q.error : q.local ? `${formatMoney(q.local.price, q.local.currency, 0)} / ${q.local.unit}` : q.symbol}
                </div>
              </div>
              <div className="widget-quote-chart min-w-0"><Trace values={q.history || []} height={22} /></div>
              <div className="widget-wrap text-right">
                <div className="font-mono text-sm">{q.price ? formatNumber(q.price) : '—'}</div>
                {q.price !== 0 && q.currency && <span className="widget-more font-mono text-[0.625rem] text-muted">{q.currency}</span>}
                <div className={cx('font-mono text-[0.6875rem]', changeClass(q.changePct))}>{q.price ? pct(q.changePct) : ''}</div>
              </div>
            </li>
          ))}
        </ul>
      )}
      {quotes?.some((q) => q.local) && <p className="mt-auto px-4 py-2 text-[0.6875rem] text-muted">Gold and silver in your currency are international prices, before local taxes.</p>}
    </Widget>
  )
}

// --- currency -------------------------------------------------------------------

function CurrencyWidget({ c, error }: { c?: CurrencyData; error?: string }) {
  const rates = c?.rates || []
  return (
    <Widget title={c ? `Currency · in ${c.home}` : 'Currency'} aside={<EditPencil kind="currency" />}>
      <Problem text={error} />
      {!c ? (
        error ? null : <div className="p-4"><Spinner /></div>
      ) : rates.length === 0 ? (
        <p className="p-4 text-sm text-muted">No exchange rates are available.</p>
      ) : (
        <ul className="widget-paired-list flex-1">
          {rates.map((r) => (
            <li key={r.code} className="widget-quote-row items-center px-4 py-2.5" data-history={(r.history?.length || 0) > 1 || undefined}>
              <span className="font-mono text-sm font-semibold">1 {r.code}</span>
              <div className="widget-quote-chart min-w-0"><Trace values={r.history || []} height={22} /></div>
              <span className="widget-wrap text-right">
                <span className="block font-mono text-sm">{formatMoney(r.rate, c.home)}</span>
                <span className={cx('block font-mono text-[0.6875rem]', changeClass(r.change))}>{pct(r.change)} · 2 wk</span>
              </span>
            </li>
          ))}
        </ul>
      )}
      <p className="mt-auto px-4 py-2 text-[0.6875rem] text-muted">
        {c?.date && <time dateTime={c.date} className="mr-2 font-mono">{c.date}</time>}
        Reference rates from the European Central Bank, updated on weekdays.
      </p>
    </Widget>
  )
}

// --- news ---------------------------------------------------------------------------

/** A story: headline, source, summary, and a QR code to read the rest on a phone. */
export function StoryBody({ it, qrSize = 160 }: { it: NewsItem; qrSize?: number }) {
  const when = it.published && !it.published.startsWith('0001') ? timeAgo(it.published) : ''
  return (
    <div className="flex flex-col gap-5 sm:flex-row sm:items-start">
      <div className="min-w-0 flex-1 space-y-3">
        <h3 className="text-lg font-semibold leading-snug">{it.title}</h3>
        <p className="font-mono text-xs text-muted">{[it.source, when].filter(Boolean).join(' · ')}</p>
        <p className="text-ink-2">{it.summary && it.summary !== it.title ? it.summary : 'This source didn’t send a summary. Scan the code to read the story.'}</p>
      </div>
      <div className="flex shrink-0 flex-col items-center gap-2 self-center sm:self-start">
        <QR text={it.link} size={qrSize} />
        <span className="max-w-[10rem] text-center text-xs text-muted">Scan to read the full story on your phone</span>
      </div>
    </div>
  )
}

function NewsWidget({ list, error }: { list?: News[]; error?: string }) {
  const [i, setI] = useState(0)
  const [story, setStory] = useState<NewsItem | null>(null)
  const idx = Math.min(i, Math.max(0, (list?.length || 1) - 1))
  const n = list && list.length ? list[idx] : undefined
  const shown = n?.items?.slice(0, 20) || []
  return (
    <Widget title={n && (list?.length || 0) < 2 ? `News · ${n.label}` : 'News'} className="max-h-[24rem]" aside={<EditPencil kind="news" />}>
      {list && list.length > 1 && (
        <CityTabs
          names={list.map((x) => x.label)}
          value={idx}
          onChange={(k) => setI(k)}
        />
      )}
      <Problem text={error} />
      {!n ? (
        <div className="p-4">{error ? null : <Spinner />}</div>
      ) : shown.length === 0 ? (
        <p className="p-4 text-sm text-muted">No headlines are available.</p>
      ) : (
        <>
          <ul className="widget-paired-list min-h-0 flex-1 overflow-y-auto">
            {shown.map((it) => (
              <li key={it.link}>
                <button className="block h-full w-full px-4 py-2.5 text-left hover:bg-raised" onClick={() => setStory(it)}>
                  <span className="widget-wrap line-clamp-2 text-sm" title={it.title}>{it.title}</span>
                  {it.summary && it.summary !== it.title && (
                    <span className="widget-more mt-1 text-xs text-ink-2"><span className="widget-wrap line-clamp-2">{it.summary}</span></span>
                  )}
                  <span className="mt-0.5 block truncate font-mono text-[0.6875rem] text-muted">
                    {[it.source, it.published && !it.published.startsWith('0001') ? timeAgo(it.published) : ''].filter(Boolean).join(' · ')}
                  </span>
                </button>
              </li>
            ))}
          </ul>
          <Modal
            open={!!story}
            onClose={() => setStory(null)}
            wide
            label="News"
            title={story?.source || n.label}
            footer={
              story && (
                <a className="key" href={story.link} target="_blank" rel="noreferrer">
                  Open on this device
                </a>
              )
            }
          >
            {story && <StoryBody it={story} />}
          </Modal>
        </>
      )}
    </Widget>
  )
}

const widgetWidths = [['', 'Auto'], ['compact', 'Compact'], ['half', 'Half'], ['full', 'Full']] as const

/** The Overview widgets and the admin's shared layout editor. */
export function WidgetBoard({ data }: { data: WidgetData | null }) {
  const role = useContext(Ctx)?.role
  const admin = role === 'admin'
  const manager = atLeast(role, 'manager')
  const [draft, setDraft] = useState<WidgetLayout | null>(null)
  const [committed, setCommitted] = useState<{ settings: WidgetData['settings']; layout: WidgetLayout } | null>(null)
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState('')
  const [announcement, setAnnouncement] = useState('')
  const editButton = useRef<HTMLButtonElement>(null)
  const saveButton = useRef<HTMLButtonElement>(null)
  const motion = useWidgetLayoutMotion(move)
  const editing = admin && draft !== null

  useEffect(() => {
    if (editing) saveButton.current?.focus({ preventScroll: true })
  }, [editing])

  function move(key: WKey, target: WKey, after: boolean) {
    if (!draft || saving || target === key) return
    const order = draft.order.filter((item) => item !== key)
    const to = order.indexOf(target) + (after ? 1 : 0)
    if (to === draft.order.indexOf(key)) return
    order.splice(to, 0, key)
    motion.measure()
    setDraft({ ...draft, order })
    setAnnouncement(`${W_NAMES[key]} moved to position ${to + 1} of ${order.length}.`)
  }

  function step(key: WKey, delta: number) {
    if (!draft || saving) return
    const target = draft.order[draft.order.indexOf(key) + delta]
    if (!target) return
    const focused = document.activeElement
    move(key, target, delta > 0)
    requestAnimationFrame(() => {
      if (focused instanceof HTMLElement && focused.isConnected && document.activeElement === document.body) {
        focused.focus({ preventScroll: true })
      }
    })
  }

  if (!data) {
    let count = manager ? W_DEFAULT.length : 1
    try {
      count = Math.max(1, Math.min(W_DEFAULT.length, parseInt(localStorage.getItem('haven-widget-count') || '', 10) || count))
    } catch {
      /* Storage blocked. Use the role's default. */
    }
    return (
      <div className="board" aria-hidden>
        {Array.from({ length: count }, (_, i) => <div key={i} className="plate h-[20rem] bg-well" />)}
      </div>
    )
  }

  const settings = data.settings
  const saved = committed?.settings === settings ? committed.layout : {
    order: widgetOrder(settings.order),
    hidden: (settings.hidden || []) as WKey[],
    sizes: settings.sizes || {},
  }
  const layout = editing ? draft : saved

  function closeEditor() {
    motion.cancel()
    setDraft(null)
    setAnnouncement('')
    requestAnimationFrame(() => editButton.current?.focus({ preventScroll: true }))
  }

  async function save() {
    if (!draft || saving) return
    setSaving(true)
    setError('')
    try {
      const ui = await api.ui()
      const next = await api.saveUI({ ...ui, widgets: { ...ui.widgets, order: draft.order, hidden: draft.hidden, sizes: draft.sizes } })
      setCommitted({ settings, layout: { ...draft, sizes: next.widgets.sizes || {} } })
      closeEditor()
      announceUIChange()
    } catch (e) {
      setError((e as Error).message)
    } finally {
      setSaving(false)
    }
  }

  function begin() {
    setError('')
    setDraft(saved)
  }

  function resize(key: WKey, width: WidgetWidth | '') {
    if (!draft || (draft.sizes[key]?.width || '') === width) return
    const sizes = { ...draft.sizes }
    if (width) sizes[key] = { width }
    else delete sizes[key]
    motion.measure()
    setDraft({ ...draft, sizes })
  }

  const isOff = (key: WKey) => {
    if (key === 'clock' || key === 'notes' || key === 'links') return false
    return !!data[key]?.off && !settings.hidden?.includes(key)
  }
  const emptyForViewer = (key: WKey) => (key === 'notes' && !settings.notes?.trim()) || (key === 'links' && !settings.links?.length) || (key === 'status' && !settings.checks?.length)
  const visible = layout.order.filter((key) => !layout.hidden.includes(key) && !isOff(key) && !(PRIVATE_KEYS.includes(key) && !manager) && !(!admin && emptyForViewer(key)))
  if (!editing) {
    try {
      localStorage.setItem('haven-widget-count', String(visible.length))
    } catch {
      /* Storage blocked. The layout is still saved on the server. */
    }
  }

  const body = (k: WKey) => {
    switch (k) {
      case 'weather':
        return <WeatherWidget list={data.weather.data} error={data.weather.error} />
      case 'aqi':
        return <AirQualityWidget list={data.aqi?.data} error={data.aqi?.error} />
      case 'markets':
        return <MarketsWidget quotes={data.markets.data} error={data.markets.error} />
      case 'currency':
        return <CurrencyWidget c={data.currency.data} error={data.currency.error} />
      case 'news':
        return <NewsWidget list={data.news.data} error={data.news.error} />
      case 'clock':
        return <ClockWidget zones={data.settings.clocks || []} hour12={data.settings.clockFormat === '12'} admin={admin} />
      case 'notes':
        return <NotesWidget text={data.settings.notes || ''} admin={admin} />
      case 'links':
        return <LinksWidget links={data.settings.links || []} admin={admin} />
      case 'status':
        return <StatusWidget results={data.status?.data || []} checks={data.settings.checks || []} admin={admin} />
    }
  }


  return (
    <section
      aria-label="Overview widgets"
      className="widget-layout space-y-3"
      onKeyDown={(event) => {
        if (!editing || event.key !== 'Escape') return
        if (motion.dragging) motion.cancel()
        else if (!saving) closeEditor()
      }}
    >
      {admin && (editing ? (
        <div className="widget-layout-bar plate flex flex-wrap items-center gap-3 px-3 py-2">
          <div className="mr-auto min-w-0">
            <h2 className="text-sm font-semibold">Edit widget layout</h2>
            <p className="mt-0.5 text-xs text-muted">
              Drag a grip to move a widget. This layout is shared.
              <span className="widget-width-note"> Width choices apply on wider screens.</span>
            </p>
          </div>
          <div className="flex flex-wrap gap-2">
            <button className="key key-sm key-flat" disabled={saving} onClick={() => {
              motion.cancel()
              motion.measure()
              setDraft({ order: W_DEFAULT, hidden: [], sizes: {} })
              setAnnouncement('Default layout restored in the preview.')
            }}><RotateCcw size={13} /> Reset</button>
            <button className="key key-sm" disabled={saving} onClick={closeEditor}>Cancel</button>
            <button ref={saveButton} className="key key-sm key-primary" disabled={saving} onClick={save}><Check size={13} /> {saving ? 'Saving…' : 'Save layout'}</button>
          </div>
          {error && <p role="alert" className="basis-full text-xs text-bad">{error}</p>}
          <span className="sr-only" role="status">{announcement}</span>
        </div>
      ) : (
        <div className="flex justify-end">
          <button ref={editButton} className="key key-sm" onClick={begin}><LayoutGrid size={13} /> Edit layout</button>
        </div>
      ))}
      {(editing || visible.length > 0) && (
        <div ref={motion.board} className="board" data-reordering={motion.dragging ? true : undefined}>
          {(editing ? layout.order : visible).map((key) => {
            const hidden = layout.hidden.includes(key)
            const name = W_NAMES[key]
            const size = layout.sizes[key]
            return (
              <div
                key={key}
                className={cx('widget-slot', editing && 'widget-slot-editing')}
                data-widget-key={key}
                data-width={size?.width}
                data-dragging={motion.dragging === key || undefined}
                data-drop={motion.drop?.key === key ? motion.drop.side : undefined}
              >
                {editing && (
                  <div className="widget-tools" role="group" aria-label={`${name} layout`}>
                    <button
                      className="key key-flat key-sm key-icon widget-grip"
                      aria-label={`Drag ${name} or use arrow keys to move it`}
                      title={`Drag ${name}`}
                      disabled={saving}
                      onPointerDown={(event) => motion.start(event, key)}
                      onPointerMove={motion.track}
                      onPointerUp={motion.finish}
                      onPointerCancel={motion.cancel}
                      onLostPointerCapture={motion.cancel}
                      onKeyDown={(event) => {
                        if (!['ArrowUp', 'ArrowLeft', 'ArrowDown', 'ArrowRight'].includes(event.key)) return
                        event.preventDefault()
                        step(key, event.key === 'ArrowUp' || event.key === 'ArrowLeft' ? -1 : 1)
                      }}
                    ><GripVertical size={15} /></button>
                    <div className="widget-widths well" role="radiogroup" aria-label={`${name} width`}>
                      {widgetWidths.map(([width, label]) => (
                        <label key={width}>
                          <input
                            className="sr-only"
                            type="radio"
                            name={`widget-width-${key}`}
                            value={width}
                            checked={(size?.width || '') === width}
                            disabled={saving}
                            onChange={() => resize(key, width)}
                          />
                          <span>{label}</span>
                        </label>
                      ))}
                    </div>
                    <button
                      className="key key-flat key-sm key-icon widget-visibility"
                      aria-label={`${hidden ? 'Show' : 'Hide'} ${name}`}
                      title={hidden ? 'Show widget' : 'Hide widget'}
                      disabled={saving}
                      onClick={() => setDraft({ ...draft, hidden: hidden ? draft.hidden.filter((item) => item !== key) : [...draft.hidden, key] })}
                    >{hidden ? <Eye size={15} /> : <EyeOff size={15} />}</button>
                  </div>
                )}
                <div className="widget-preview">
                  <div className={cx('widget-content', editing && hidden && 'invisible')} inert={editing || undefined}>{body(key)}</div>
                  {editing && (hidden || isOff(key)) && (
                    <div className="widget-placeholder">
                      <EyeOff size={18} aria-hidden />
                      <span>{name} {hidden ? 'is hidden' : 'is off in Settings'}</span>
                    </div>
                  )}
                </div>
              </div>
            )
          })}
        </div>
      )}
    </section>
  )
}
