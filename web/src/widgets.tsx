import { useContext, useEffect, useRef, useState } from 'react'
import { Check, ChevronLeft, ChevronRight, Eye, EyeOff, GripVertical, LayoutGrid, RotateCcw, Cloud, CloudDrizzle, CloudFog, CloudHail, CloudLightning, CloudMoon, CloudRain, CloudSnow, CloudSun, Moon, Sun, type LucideIcon } from 'lucide-react'
import { announceUIChange, api, atLeast, formatMoney, formatNumber, timeAgo, type CurrencyData, type NetTest, type News, type NewsItem, type Quote, type Weather, type WidgetData } from './api'
import { Led, Modal, Spinner, Trace, cx, usePoll } from './components'
import { QR } from './diagnosis'
import { Ctx } from './context'
import { Widget } from './widgetshell'
import { ClockWidget, LinksWidget, NotesWidget, StatusWidget } from './extrawidgets'
import { EditCta, EditPencil } from './widgeteditors'
import { PRIVATE_KEYS, W_DEFAULT, W_NAMES, type WKey } from './widgetmeta'

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

function Problem({ text }: { text?: string }) {
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
function CityTabs({ names, value, onChange }: { names: string[]; value: number; onChange: (i: number) => void }) {
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
          <button key={n + i} role="tab" aria-selected={value === i} onClick={() => onChange(i)} className={cx('key key-sm h-7 shrink-0 border-transparent px-2 text-[0.6875rem]', value !== i && 'key-flat')}>
            {n}
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
  const { label, Icon } = weatherInfo(w.code, w.isDay)
  return (
    <Widget title={list.length > 1 ? 'Weather' : `Weather · ${w.place}`} aside={<EditPencil kind="weather" />}>
      <CityTabs names={list.map((x) => x.place)} value={Math.min(i, list.length - 1)} onChange={setI} />
      <Problem text={error} />
      <div className="flex items-center gap-4 px-4 pb-2 pt-4">
        <Icon size={44} strokeWidth={1.4} className="shrink-0 text-accent" aria-hidden />
        <div className="min-w-0">
          <div className="text-5xl">
            <span className="readout">{Math.round(w.temp)}</span>
            <span className="readout text-2xl text-muted">°{w.units}</span>
          </div>
          <div className="truncate text-sm text-ink-2">{label}</div>
        </div>
      </div>
      <p className="px-4 pb-4 font-mono text-[0.6875rem] text-muted">
        feels {Math.round(w.feels)}° · humidity {w.humidity}% · wind {Math.round(w.wind)} {w.windUnits}
      </p>
      <ol className="mt-auto grid gap-px border-t border-rule-2 bg-rule-2" style={{ gridTemplateColumns: `repeat(${w.days.length || 1}, minmax(0, 1fr))` }}>
        {w.days.map((d, k) => {
          const day = weatherInfo(d.code)
          return (
            <li key={d.date} className="flex flex-col items-center gap-1 bg-plate px-1 py-2" title={`${day.label}, ${d.rain}% chance of rain`}>
              <span className="etch text-[0.5625rem]">{weekday(d.date, k)}</span>
              <day.Icon size={16} className="text-ink-2" aria-label={day.label} />
              <span className="text-center font-mono text-xs leading-tight">
                {Math.round(d.max)}°<span className="block text-muted">{Math.round(d.min)}°</span>
              </span>
            </li>
          )
        })}
      </ol>
    </Widget>
  )
}

// --- markets --------------------------------------------------------------------

function MarketsWidget({ quotes, error }: { quotes?: Quote[]; error?: string }) {
  return (
    <Widget title="Markets" aside={<span className="flex items-center gap-2"><span className="etch text-[0.5625rem]">day change</span><EditPencil kind="markets" /></span>}>
      <Problem text={error} />
      {!quotes ? (
        <div className="p-4">
          <Spinner />
        </div>
      ) : quotes.length === 0 ? (
        <p className="p-4 text-sm text-muted">Add gold, silver or stocks in Settings.</p>
      ) : (
        <ul className="flex flex-1 flex-col divide-y divide-rule-2">
          {quotes.map((q) => (
            <li key={q.symbol} className="grid flex-1 grid-cols-[minmax(0,1fr)_4.5rem_auto] items-center gap-3 px-4 py-2.5">
              <div className="min-w-0">
                <div className="truncate text-sm font-medium">{q.name}</div>
                <div className="truncate font-mono text-[0.6875rem] text-muted">
                  {q.error ? q.error : q.local ? `${formatMoney(q.local.price, q.local.currency, 0)} / ${q.local.unit}` : q.symbol}
                </div>
              </div>
              <Trace values={q.history || []} height={22} />
              <div className="text-right">
                <div className="font-mono text-sm">{q.price ? formatNumber(q.price) : '—'}</div>
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
  return (
    <Widget title={c ? `Currency · in ${c.home}` : 'Currency'} aside={<span className="flex items-center gap-2">{c?.date && <span className="etch text-[0.5625rem]">{c.date}</span>}<EditPencil kind="currency" /></span>}>
      <Problem text={error} />
      {!c ? (
        <div className="p-4">
          <Spinner />
        </div>
      ) : (
        <ul className="flex flex-1 flex-col divide-y divide-rule-2">
          {c.rates.map((r) => (
            <li key={r.code} className="grid flex-1 grid-cols-[3.5rem_minmax(0,1fr)_auto] items-center gap-3 px-4 py-2.5">
              <span className="font-mono text-sm font-semibold">1 {r.code}</span>
              <Trace values={r.history || []} height={22} />
              <span className="text-right">
                <span className="block font-mono text-sm">{formatMoney(r.rate, c.home)}</span>
                <span className={cx('block font-mono text-[0.6875rem]', changeClass(r.change))}>{pct(r.change)} · 2 wk</span>
              </span>
            </li>
          ))}
        </ul>
      )}
      <p className="mt-auto px-4 py-2 text-[0.6875rem] text-muted">Reference rates from the European Central Bank, updated on weekdays.</p>
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
  const shown = n ? n.items.slice(0, 20) : []
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
      ) : (
        <>
          <ul className="min-h-0 flex-1 divide-y divide-rule-2 overflow-y-auto">
            {shown.map((it) => (
              <li key={it.link}>
                <button className="block w-full px-4 py-2.5 text-left hover:bg-raised" onClick={() => setStory(it)}>
                  <span className="line-clamp-2 text-sm">{it.title}</span>
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

/** The widget row on the Overview. */
/** The Overview widgets. The admin can rearrange and hide them with Edit layout. */
export function WidgetBoard({ data }: { data: WidgetData | null }) {
  const role = useContext(Ctx)?.role
  const admin = role === 'admin'
  const manager = atLeast(role, 'manager')
  const [editing, setEditing] = useState(false)
  const [order, setOrder] = useState<WKey[]>(W_DEFAULT)
  const [hiddenKeys, setHiddenKeys] = useState<WKey[]>([])
  const [drag, setDrag] = useState<WKey | null>(null)
  const [over, setOver] = useState<WKey | null>(null)
  const [saving, setSaving] = useState(false)
  const [err, setErr] = useState('')
  if (!data) {
    // Hold the place of the cards that are coming, so nothing below jumps down when they arrive.
    // Until Haven has seen the real number, guess from the role: managers and the admin get all eight.
    let n = manager ? W_DEFAULT.length : 1
    try {
      n = Math.max(1, Math.min(8, parseInt(localStorage.getItem('haven-widget-count') || '', 10) || n))
    } catch {
      /* storage blocked: assume one row */
    }
    return (
      <div className="board" aria-hidden>
        {Array.from({ length: n }, (_, i) => (
          <div key={i} className="plate h-[20rem] bg-well" />
        ))}
      </div>
    )
  }

  const savedOrder = ((data.settings?.order || W_DEFAULT) as WKey[]).filter((k) => W_DEFAULT.includes(k))
  const savedHidden = (data.settings?.hidden || []) as WKey[]
  const begin = () => {
    setOrder(savedOrder.length === W_DEFAULT.length ? savedOrder : W_DEFAULT)
    setHiddenKeys(savedHidden)
    setErr('')
    setEditing(true)
  }
  const save = async () => {
    setSaving(true)
    setErr('')
    try {
      const ui = await api.ui()
      await api.saveUI({ ...ui, widgets: { ...ui.widgets, order, hidden: hiddenKeys } })
      announceUIChange()
      setEditing(false)
    } catch (e: any) {
      setErr(e.message)
    } finally {
      setSaving(false)
    }
  }
  const move = (k: WKey, to: number) =>
    setOrder((o) => {
      const rest = o.filter((x) => x !== k)
      rest.splice(Math.max(0, Math.min(rest.length, to)), 0, k)
      return rest
    })
  const toggleHidden = (k: WKey) => setHiddenKeys((h) => (h.includes(k) ? h.filter((x) => x !== k) : [...h, k]))

  const body = (k: WKey) => {
    switch (k) {
      case 'weather':
        return <WeatherWidget list={data.weather.data} error={data.weather.error} />
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

  const keys = editing ? order : savedOrder.length === W_DEFAULT.length ? savedOrder : W_DEFAULT
  // A widget is off when it was hidden, or (for the older four) turned off in Settings.
  const isOff = (k: WKey) => (k === 'weather' || k === 'markets' || k === 'currency' || k === 'news' ? !!data[k].off : k === 'status' ? !!data.status?.off || savedHidden.includes(k) : savedHidden.includes(k))
  // Someone who can't edit has nothing to see in an empty note, link list or check list.
  const emptyForViewer = (k: WKey) => (k === 'notes' && !(data.settings.notes || '').trim()) || (k === 'links' && !(data.settings.links || []).length) || (k === 'status' && !(data.settings.checks || []).length)
  const viewerHides = (k: WKey) => (PRIVATE_KEYS.includes(k) && !manager) || (!admin && emptyForViewer(k))
  const shown = (k: WKey) => (editing ? !hiddenKeys.includes(k) : !isOff(k) && !viewerHides(k))
  const visible = keys.filter(shown)
  if (!editing) {
    try {
      localStorage.setItem('haven-widget-count', String(visible.length))
    } catch {
      /* storage blocked */
    }
  }
  if (!editing && visible.length === 0) {
    return admin ? (
      <div className="flex justify-end">
        <button className="key key-sm" onClick={begin}>
          <LayoutGrid size={13} /> Edit layout
        </button>
      </div>
    ) : null
  }

  return (
    <div className="space-y-3">
      {admin && (
        <div className="flex flex-wrap items-center justify-end gap-2">
          {editing ? (
            <>
              <span className="mr-auto text-xs text-ink-2">Click and hold a widget, then drag it where you want. Everyone sees this layout.</span>
              {err && <span className="text-xs text-bad">{err}</span>}
              <button
                className="key key-sm"
                onClick={() => {
                  setOrder(W_DEFAULT)
                  setHiddenKeys([])
                }}
              >
                <RotateCcw size={13} /> Reset
              </button>
              <button className="key key-sm" onClick={() => setEditing(false)} disabled={saving}>
                Cancel
              </button>
              <button className="key key-sm key-primary" onClick={save} disabled={saving}>
                <Check size={13} /> Done
              </button>
            </>
          ) : (
            <button className="key key-sm" onClick={begin}>
              <LayoutGrid size={13} /> Edit layout
            </button>
          )}
        </div>
      )}
      <div className="board">
        {(editing ? keys : visible).map((k, i) => {
          const hide = editing && hiddenKeys.includes(k)
          if (!editing) return <div key={k} className="flex min-w-0">{body(k)}</div>
          return (
            <div
              key={k}
              draggable
              onDragStart={(e) => {
                setDrag(k)
                e.dataTransfer.effectAllowed = 'move'
                e.dataTransfer.setData('text/plain', k)
              }}
              onDragOver={(e) => {
                e.preventDefault()
                if (over !== k) setOver(k)
              }}
              onDragLeave={() => setOver((o) => (o === k ? null : o))}
              onDrop={(e) => {
                e.preventDefault()
                if (drag && drag !== k) move(drag, keys.indexOf(k))
                setDrag(null)
                setOver(null)
              }}
              onDragEnd={() => {
                setDrag(null)
                setOver(null)
              }}
              className={cx('relative flex cursor-grab flex-col rounded-[3px] ring-1 ring-rule transition-shadow active:cursor-grabbing', over === k && drag !== k && 'ring-2 ring-[var(--accent)]', drag === k && 'opacity-50')}
              aria-label={`${W_NAMES[k]} widget, position ${i + 1} of ${keys.length}`}
            >
              <div className="absolute inset-x-0 top-0 z-20 flex items-center gap-1 rounded-t-[3px] border-b border-rule bg-paper px-2 py-1.5">
                <span className="text-muted" title="Click and hold to move" aria-hidden>
                  <GripVertical size={16} />
                </span>
                <span className="etch mr-auto text-[0.625rem]">{W_NAMES[k]}</span>
                <button className="key key-flat key-sm key-icon" onClick={() => toggleHidden(k)} aria-label={hide ? `Show ${W_NAMES[k]}` : `Hide ${W_NAMES[k]}`} title={hide ? 'Show' : 'Hide'}>
                  {hide ? <Eye size={14} /> : <EyeOff size={14} />}
                </button>
              </div>
              <div className="pointer-events-none relative flex flex-1 select-none pt-10">
                {/* A hidden widget keeps its place: its content stays (unseen) so the row doesn't change height. */}
                {!isOff(k) && <div className={cx('flex w-full flex-1', hide && 'invisible')}>{body(k)}</div>}
                {(hide || isOff(k)) && (
                  <div className={cx('flex min-h-[10rem] w-full flex-col items-center justify-center gap-2 rounded-[3px] border border-dashed border-rule-hi bg-paper p-6 text-center', isOff(k) ? 'flex-1' : 'absolute inset-x-0 bottom-0 top-10')}>
                    <EyeOff size={22} className="text-muted" aria-hidden />
                    <span className="text-sm font-semibold">{W_NAMES[k]} is hidden</span>
                    <span className="text-xs text-muted">{hide ? 'It won’t show on the Overview. Click the eye above to bring it back.' : 'It is switched off in Settings → Widgets.'}</span>
                  </div>
                )}
              </div>
            </div>
          )
        })}
      </div>
    </div>
  )
}
