import { useEffect, useState } from 'react'
import { type CheckResult } from './api'
import { Led, Spinner, cx } from './components'
import { EditCta, EditPencil, offsetOf, zoneLabel } from './widgeteditors'
import { Widget } from './widgetshell'

// The Clock, Notes, Quick links and Status widgets. Their settings open from
// the pencil, like every other widget.

// --- clock ------------------------------------------------------------------------

function dayWord(now: Date, zone: string) {
  const f = (d: Date, tz?: string) => new Intl.DateTimeFormat('en-CA', { timeZone: tz }).format(d)
  const here = f(now)
  const there = f(now, zone)
  if (here === there) return 'Today'
  return there > here ? 'Tomorrow' : 'Yesterday'
}

export function ClockWidget({ zones, hour12, admin }: { zones: string[]; hour12: boolean; admin: boolean }) {
  const [now, setNow] = useState(() => new Date())
  useEffect(() => {
    const t = setInterval(() => setNow(new Date()), 1000)
    return () => clearInterval(t)
  }, [])
  const local = Intl.DateTimeFormat().resolvedOptions().timeZone
  const parts = (tz?: string) =>
    new Intl.DateTimeFormat('en-GB', { timeZone: tz, hour: '2-digit', minute: '2-digit', second: '2-digit', hour12, hourCycle: hour12 ? 'h12' : 'h23' }).formatToParts(now)
  const pick = (ps: Intl.DateTimeFormatPart[], t: string) => ps.find((p) => p.type === t)?.value || ''
  const time = (tz?: string) => {
    const ps = parts(tz)
    return `${pick(ps, 'hour')}:${pick(ps, 'minute')}${hour12 ? ` ${pick(ps, 'dayPeriod').toUpperCase()}` : ''}`
  }
  const localParts = parts()
  return (
    <Widget title="Clock" aside={<EditPencil kind="clock" />}>
      <div className="px-4 pb-3 pt-4">
        <div className="readout text-5xl tabular-nums" aria-label="Local time">
          {pick(localParts, 'hour')}:{pick(localParts, 'minute')}
          <span className="text-2xl text-muted">:{pick(localParts, 'second')}</span>
          {hour12 && <span className="ml-2 text-2xl text-muted">{pick(localParts, 'dayPeriod').toUpperCase()}</span>}
        </div>
        <div className="mt-1 text-sm text-ink-2">{new Intl.DateTimeFormat(undefined, { weekday: 'long', day: 'numeric', month: 'long' }).format(now)}</div>
        <div className="font-mono text-[0.6875rem] text-muted">{local}</div>
      </div>
      {zones.length > 0 ? (
        <ul className="mt-auto divide-y divide-rule-2 border-t border-rule-2">
          {zones.map((z) => (
            <li key={z} className="flex items-baseline justify-between gap-3 px-4 py-2" title={`${z} · ${offsetOf(z)}`}>
              <span className="min-w-0">
                <span className="block truncate text-sm">{zoneLabel(z)}</span>
                <span className="block text-[0.6875rem] text-muted">{dayWord(now, z)}</span>
              </span>
              <span className="font-mono text-lg tabular-nums">{time(z)}</span>
            </li>
          ))}
        </ul>
      ) : admin ? (
        <p className="mt-auto border-t border-rule-2 px-4 py-3 text-xs text-muted">Use the pencil to add other cities, for family or friends abroad.</p>
      ) : null}
    </Widget>
  )
}

// --- notes --------------------------------------------------------------------------

export function NotesWidget({ text }: { text: string; admin: boolean }) {
  return (
    <Widget title="Notes" className="max-h-[24rem]" aside={<EditPencil kind="notes" />}>
      {text.trim() ? (
        <p className="min-h-0 flex-1 overflow-y-auto whitespace-pre-wrap break-words px-4 py-3 text-sm">{text}</p>
      ) : (
        <div className="flex flex-1 flex-col items-start justify-center gap-3 p-4">
          <p className="text-sm text-ink-2">A shared note for the household: Wi-Fi password, chores, reminders.</p>
          <EditCta kind="notes">Write a note</EditCta>
        </div>
      )}
    </Widget>
  )
}

// --- quick links ---------------------------------------------------------------------

type Link = { name: string; url: string }

function hostOf(u: string) {
  try {
    return new URL(u).host
  } catch {
    return u
  }
}

/** The site's own icon, fetched by Haven's server. A letter stands in until it loads, or when there is none. */
function LinkIcon({ index, link }: { index: number; link: Link }) {
  const [failed, setFailed] = useState(false)
  const letter = (link.name[0] || '?').toUpperCase()
  return (
    <span className="relative grid h-8 w-8 shrink-0 place-items-center overflow-hidden rounded-[3px] border border-rule bg-accent-wash font-mono text-sm font-semibold text-accent" aria-hidden>
      {letter}
      {!failed && <img src={`/api/widgets/favicon?i=${index}&k=${encodeURIComponent(link.url)}`} alt="" loading="lazy" onError={() => setFailed(true)} className="absolute inset-0 h-full w-full bg-well object-contain p-1" />}
    </span>
  )
}

export function LinksWidget({ links }: { links: Link[]; admin: boolean }) {
  return (
    <Widget title="Quick links" className="max-h-[24rem]" aside={<EditPencil kind="links" />}>
      {links.length > 0 ? (
        <ul className="grid min-h-0 flex-1 auto-rows-min grid-cols-1 gap-px overflow-y-auto bg-rule-2 sm:grid-cols-2 md:grid-cols-1 2xl:grid-cols-2">
          {links.map((l, i) => (
            <li key={l.url + l.name} className="bg-plate">
              <a href={l.url} target="_blank" rel="noreferrer noopener" className="flex items-center gap-3 px-3 py-2.5 hover:bg-raised">
                <LinkIcon index={i} link={l} />
                <span className="min-w-0">
                  <span className="block truncate text-sm font-medium">{l.name}</span>
                  <span className="block truncate font-mono text-[0.6875rem] text-muted">{hostOf(l.url)}</span>
                </span>
              </a>
            </li>
          ))}
        </ul>
      ) : (
        <div className="flex flex-1 flex-col items-start justify-center gap-3 p-4">
          <p className="text-sm text-ink-2">Links to your router, NAS or favourite sites, one tap away.</p>
          <EditCta kind="links">Add links</EditCta>
        </div>
      )}
    </Widget>
  )
}

// --- status ---------------------------------------------------------------------------

function downFor(since?: string) {
  if (!since || since.startsWith('0001')) return ''
  const s = Math.max(0, Math.round((Date.now() - new Date(since).getTime()) / 1000))
  if (s < 90) return 'for a minute'
  if (s < 3600) return `for ${Math.round(s / 60)} min`
  if (s < 86400) return `for ${Math.round(s / 3600)} h`
  return `for ${Math.round(s / 86400)} d`
}

export function StatusWidget({ results }: { results: CheckResult[]; checks: { name: string; target: string }[]; admin: boolean }) {
  const upCount = results.filter((r) => r.up).length
  return (
    <Widget
      title="Status"
      className="max-h-[24rem]"
      aside={
        <span className="flex items-center gap-2">
          {results.length > 0 && (
            <span className={cx('etch text-[0.5625rem]', upCount < results.length && !results.some((r) => r.pending) && 'text-bad')}>
              {upCount} of {results.length} up
            </span>
          )}
          <EditPencil kind="status" />
        </span>
      }
    >
      {results.length > 0 ? (
        <ul className="min-h-0 flex-1 divide-y divide-rule-2 overflow-y-auto">
          {results.map((r) => (
            <li key={r.target} className="flex items-center gap-3 px-4 py-2.5">
              <Led s={r.pending ? 'off' : r.up ? 'ok' : 'bad'} label={r.pending ? 'Checking' : r.up ? 'Up' : 'Down'} />
              <span className="min-w-0 flex-1">
                <span className="block truncate text-sm font-medium">{r.name}</span>
                <span className="block truncate font-mono text-[0.6875rem] text-muted" title={r.target}>
                  {r.pending ? 'checking…' : r.up ? r.target : `${r.error || 'down'} ${downFor(r.since)}`}
                </span>
              </span>
              {r.up && !r.pending && r.ms !== undefined && <span className="shrink-0 font-mono text-xs text-muted">{r.ms} ms</span>}
              {r.pending && <Spinner size={12} />}
            </li>
          ))}
        </ul>
      ) : (
        <div className="flex flex-1 flex-col items-start justify-center gap-3 p-4">
          <p className="text-sm text-ink-2">Green and red lights for your router, a website or an app, and a bell alert if one goes down.</p>
          <EditCta kind="status">Add checks</EditCta>
        </div>
      )}
    </Widget>
  )
}
