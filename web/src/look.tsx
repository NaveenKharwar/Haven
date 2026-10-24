import { useEffect, useRef, useState, type ReactNode } from 'react'
import { X } from 'lucide-react'
import { announceUIChange, api, type Place, type UISettings } from './api'
import { ErrorLine, Led, Spinner, cx, toast } from './components'
import { Suggest } from './suggest'

// Pieces shared by the widget editors and the background settings.

// Currencies the European Central Bank publishes (the source for rates).
export const CURRENCIES = ['AUD', 'BGN', 'BRL', 'CAD', 'CHF', 'CNY', 'CZK', 'DKK', 'EUR', 'GBP', 'HKD', 'HUF', 'IDR', 'ILS', 'INR', 'ISK', 'JPY', 'KRW', 'MXN', 'MYR', 'NOK', 'NZD', 'PHP', 'PLN', 'RON', 'SEK', 'SGD', 'THB', 'TRY', 'USD', 'ZAR']

export const MARKET_PICKS: [string, string][] = [
  ['GC=F', 'Gold'], ['SI=F', 'Silver'], ['PL=F', 'Platinum'], ['CL=F', 'Crude oil'],
  ['^NSEI', 'Nifty 50'], ['^BSESN', 'Sensex'], ['^GSPC', 'S&P 500'], ['^IXIC', 'Nasdaq'], ['^DJI', 'Dow Jones'],
  ['BTC-USD', 'Bitcoin'], ['ETH-USD', 'Ethereum'],
  ['RELIANCE.NS', 'Reliance'], ['TCS.NS', 'TCS'], ['INFY.NS', 'Infosys'], ['HDFCBANK.NS', 'HDFC Bank'],
  ['AAPL', 'Apple'], ['MSFT', 'Microsoft'], ['NVDA', 'Nvidia'], ['TSLA', 'Tesla'],
]
// Publisher feeds that were checked to work. Anything else can be pasted in.
export const NEWS_PRESETS: { name: string; url: string; group: string }[] = [
  { group: 'India', name: 'The Hindu', url: 'https://www.thehindu.com/news/national/feeder/default.rss' },
  { group: 'India', name: 'Times of India', url: 'https://timesofindia.indiatimes.com/rssfeedstopstories.cms' },
  { group: 'India', name: 'NDTV', url: 'https://feeds.feedburner.com/ndtvnews-top-stories' },
  { group: 'India', name: 'Indian Express', url: 'https://indianexpress.com/feed/' },
  { group: 'India', name: 'Hindustan Times', url: 'https://www.hindustantimes.com/feeds/rss/india-news/rssfeed.xml' },
  { group: 'India', name: 'Mint', url: 'https://www.livemint.com/rss/news' },
  { group: 'India', name: 'Business Standard', url: 'https://www.business-standard.com/rss/home_page_top_stories.rss' },
  { group: 'World', name: 'BBC World', url: 'https://feeds.bbci.co.uk/news/world/rss.xml' },
  { group: 'World', name: 'Al Jazeera', url: 'https://www.aljazeera.com/xml/rss/all.xml' },
  { group: 'World', name: 'The Guardian', url: 'https://www.theguardian.com/world/rss' },
  { group: 'Tech', name: 'TechCrunch', url: 'https://techcrunch.com/feed/' },
  { group: 'Tech', name: 'Hacker News', url: 'https://hnrss.org/frontpage' },
]

export const pickName = (sym: string) => MARKET_PICKS.find(([s]) => s === sym)?.[1] || sym

/** Loads UI settings and saves changes shortly after the last edit. */
export function useUISettings() {
  const [ui, setUI] = useState<UISettings | null>(null)
  const [saved, setSaved] = useState<'idle' | 'saving' | 'saved'>('idle')
  const timer = useRef<ReturnType<typeof setTimeout>>(undefined)
  useEffect(() => {
    api.ui().then(setUI).catch((e) => toast.error(e.message))
  }, [])
  const update = (fn: (u: UISettings) => UISettings) => {
    setUI((cur) => {
      if (!cur) return cur
      const next = fn(cur)
      // Only the sections changed here are saved; the rest is re-read from
      // the server, so two panels editing different sections can't clash.
      const changed = (Object.keys(next) as (keyof UISettings)[]).filter((k) => JSON.stringify(next[k]) !== JSON.stringify(cur[k]))
      clearTimeout(timer.current)
      setSaved('saving')
      timer.current = setTimeout(async () => {
        try {
          const latest = await api.ui()
          const merged = { ...latest } as UISettings
          for (const k of changed) (merged as Record<string, unknown>)[k] = next[k]
          setUI(await api.saveUI(merged))
          setSaved('saved')
          announceUIChange()
        } catch (e) {
          setSaved('idle')
          toast.error((e as Error).message)
        }
      }, 450)
      return next
    })
  }
  return { ui, update, saved }
}

export function SavedMark({ state }: { state: 'idle' | 'saving' | 'saved' }) {
  if (state === 'idle') return null
  return (
    <span className="flex items-center gap-2">
      {state === 'saving' ? <Spinner size={12} /> : <Led s="ok" />}
      <span className="etch text-[0.625rem]">{state === 'saving' ? 'Saving' : 'Saved'}</span>
    </span>
  )
}

export function Field({ label, children, hint }: { label: string; children: ReactNode; hint?: ReactNode }) {
  return (
    <div className="space-y-2">
      <div className="etch">{label}</div>
      {children}
      {hint && <p className="text-xs text-muted">{hint}</p>}
    </div>
  )
}

export function Chip({ label, onRemove }: { label: string; onRemove: () => void }) {
  return (
    <span className="tag h-7 gap-1 pr-1 text-[0.6875rem] normal-case tracking-normal">
      {label}
      <button className="grid h-5 w-5 place-items-center rounded-[2px] hover:bg-raised" onClick={onRemove} aria-label={`Remove ${label}`}>
        <X size={12} />
      </button>
    </span>
  )
}

export function NewsSources({ sources, onChange }: { sources: { name: string; url: string }[]; onChange: (s: { name: string; url: string }[]) => void }) {
  const [url, setUrl] = useState('')
  const [name, setName] = useState('')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const has = (u: string) => sources.some((s) => s.url === u)
  const full = sources.length >= 8
  const toggle = (p: { name: string; url: string }) => onChange(has(p.url) ? sources.filter((s) => s.url !== p.url) : full ? sources : [...sources, { name: p.name, url: p.url }])
  const custom = sources.filter((s) => !NEWS_PRESETS.some((p) => p.url === s.url))
  const add = async () => {
    setBusy(true)
    setError('')
    try {
      const r = await api.checkFeed(url.trim())
      onChange([...sources, { name: name.trim() || r.title || new URL(url).hostname, url: url.trim() }])
      setUrl('')
      setName('')
      toast.ok(`Added: ${r.count} stories found`)
    } catch (e) {
      setError((e as Error).message)
    } finally {
      setBusy(false)
    }
  }
  return (
    <div className="space-y-3">
      {['India', 'World', 'Tech'].map((g) => (
        <div key={g} className="flex flex-wrap items-center gap-1.5">
          <span className="etch w-14 text-[0.625rem]">{g}</span>
          {NEWS_PRESETS.filter((p) => p.group === g).map((p) => (
            <button key={p.url} aria-pressed={has(p.url)} disabled={!has(p.url) && full} onClick={() => toggle(p)} className={cx('key key-sm h-7 px-2 text-[0.6875rem]', has(p.url) && 'border-accent text-accent')}>
              {has(p.url) ? '✓ ' : '+ '}
              {p.name}
            </button>
          ))}
        </div>
      ))}
      {custom.length > 0 && (
        <div className="flex flex-wrap gap-2">
          {custom.map((s) => (
            <Chip key={s.url} label={s.name} onRemove={() => onChange(sources.filter((x) => x.url !== s.url))} />
          ))}
        </div>
      )}
      <form
        className="grid grid-cols-1 gap-2 sm:grid-cols-[minmax(0,2fr)_minmax(0,1fr)_auto]"
        onSubmit={(e) => {
          e.preventDefault()
          if (url.trim() && !full) add()
        }}
      >
        <input className="field h-9" placeholder="Custom feed address (https://…/rss)" value={url} onChange={(e) => setUrl(e.target.value)} aria-label="Custom feed address" inputMode="url" />
        <input className="field h-9" placeholder="Name (optional)" value={name} onChange={(e) => setName(e.target.value)} maxLength={40} aria-label="Feed name" />
        <button className="key h-9" disabled={busy || !url.trim() || full} data-loading={busy}>
          {busy ? <Spinner size={13} /> : null} Add
        </button>
      </form>
      <ErrorLine>{error}</ErrorLine>
      <p className="text-xs text-muted">Up to 8 sources. Any site's RSS or Atom link works. Haven checks it before adding.</p>
    </div>
  )
}

export function Segmented<T extends string>({ value, options, onChange, label }: { value: T; options: [T, string][]; onChange: (v: T) => void; label: string }) {
  return (
    <div className="well inline-flex gap-0.5 p-0.5" role="radiogroup" aria-label={label}>
      {options.map(([v, text]) => (
        <button key={v} role="radio" aria-checked={value === v} onClick={() => onChange(v)} className={cx('key key-sm gap-2 border-transparent', value !== v && 'key-flat')}>
          <span className={cx('h-1.5 w-1.5 rounded-full', value === v ? 'bg-accent' : 'bg-unlit')} />
          {text}
        </button>
      ))}
    </div>
  )
}

export function CitySearch({ places, onAdd, onRemove }: { places: Place[]; onAdd: (p: Place) => void; onRemove: (i: number) => void }) {
  const [q, setQ] = useState('')
  const [results, setResults] = useState<Place[] | null>(null)
  const [busy, setBusy] = useState(false)
  useEffect(() => {
    if (q.trim().length < 2) {
      setResults(null)
      return
    }
    setBusy(true)
    const t = setTimeout(() => {
      api
        .places(q)
        .then(setResults)
        .catch((e) => toast.error(e.message))
        .finally(() => setBusy(false))
    }, 400)
    return () => clearTimeout(t)
  }, [q])
  return (
    <div className="space-y-2">
      {places.length > 0 && (
        <div className="flex flex-wrap gap-2">
          {places.map((p, i) => (
            <Chip key={`${p.lat},${p.lon}`} label={`${p.name}${i === 0 ? ' (main)' : ''}`} onRemove={() => onRemove(i)} />
          ))}
        </div>
      )}
      <Suggest
        label="City"
        placeholder={places.length >= 5 ? 'Up to 5 cities' : places.length ? 'Add another city' : 'Search for your city'}
        disabled={places.length >= 5}
        value={q}
        onChange={setQ}
        busy={busy}
        empty="No places found."
        items={results ? results.map((r) => ({ key: `${r.lat},${r.lon}`, label: r.name, hint: [r.region, r.country].filter(Boolean).join(', ') })) : null}
        onPick={(s) => {
          const r = results?.find((x) => `${x.lat},${x.lon}` === s.key)
          if (r) onAdd(r)
          setResults(null)
        }}
      />
    </div>
  )
}
