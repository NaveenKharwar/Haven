import { useState } from 'react'
import { api } from './api'
import { Led, LogBox, Spinner, cx, usePoll } from './components'

function ago(iso: string): string {
  const s = Math.max(0, Math.round((Date.now() - new Date(iso).getTime()) / 1000))
  if (s < 5) return 'just now'
  if (s < 60) return `${s}s ago`
  if (s < 3600) return `${Math.round(s / 60)} min ago`
  return `${Math.round(s / 3600)} h ago`
}

const clock = (iso: string) => new Date(iso).toLocaleTimeString([], { hour12: false })
const tone = (s: number) => (s >= 500 ? 'text-bad' : s >= 400 ? 'text-warn' : 'text-ok')

/** Everything that reached Haven: who, under which name, and how it went. */
export default function AccessLogPanel() {
  const [live, setLive] = useState(true)
  const { data: log } = usePoll(api.accessLog, live ? 2000 : 3600000, [live])
  const [showAvahi, setShowAvahi] = useState(false)
  const { data: avahi } = usePoll(api.avahiLog, showAvahi ? 5000 : 3600000, [showAvahi])
  const [ipFilter, setIpFilter] = useState('')

  const entries = (log?.entries || []).filter((e) => !ipFilter || e.ip === ipFilter)
  return (
    <section className="plate overflow-hidden" aria-label="Access log">
      <div className="plate-head">
        <h2 className="etch">Access log</h2>
        <span className="flex items-center gap-3">
          <button className="flex items-center gap-2" onClick={() => setLive(!live)} aria-pressed={live}>
            <Led s={live ? 'ok' : 'off'} />
            <span className="etch text-[0.625rem]">{live ? 'Live' : 'Paused'}</span>
          </button>
        </span>
      </div>
      <div className="space-y-5 p-4 sm:p-5">
        <p className="text-sm text-ink-2">
          Every request that reaches Haven, with the name or address your device used. If a page keeps loading and <span className="text-ink">nothing appears here</span>, the request never arrived: the
          device couldn't find the server, or something blocks the network. If it appears slowly or with an error, the problem is on this side.
        </p>

        {!log ? (
          <Spinner />
        ) : (
          <>
            {log.events.length > 0 && (
              <div>
                <h3 className="etch mb-1">Connections that stalled</h3>
                <ul className="well divide-y divide-rule-2">
                  {log.events.slice(0, 6).map((ev) => (
                    <li key={ev.seq} className="flex flex-wrap items-start gap-x-3 gap-y-1 px-3 py-2 text-sm">
                      <Led s="warn" />
                      <span className="font-mono text-xs text-muted">{clock(ev.time)}</span>
                      <span className="font-mono text-xs">
                        {ev.ip} → :{ev.port}
                      </span>
                      <span className="min-w-0 flex-1 basis-48 text-ink-2">
                        {ev.note} <span className="font-mono text-xs text-muted">({Math.round(ev.waitedMs / 1000)}s)</span>
                      </span>
                    </li>
                  ))}
                </ul>
              </div>
            )}

            {log.clients.length > 0 && (
              <div>
                <h3 className="etch mb-1">Devices that reached Haven</h3>
                <ul className="divide-y divide-rule-2">
                  {log.clients.slice(0, 8).map((c) => (
                    <li key={c.ip}>
                      <button className={cx('flex w-full flex-wrap items-center gap-x-3 gap-y-0.5 py-2 text-left text-sm hover:bg-raised', ipFilter === c.ip && 'bg-raised')} onClick={() => setIpFilter(ipFilter === c.ip ? '' : c.ip)}>
                        <span className="w-28 font-mono text-xs">{c.ip}</span>
                        <span className="text-ink-2">{c.agent || 'unknown'}</span>
                        <span className="min-w-0 flex-1 truncate font-mono text-xs text-muted">{c.hosts.join(', ')}</span>
                        <span className="font-mono text-xs text-muted">
                          {c.requests} requests{c.errors ? ` · ${c.errors} errors` : ''} · {ago(c.lastSeen)}
                        </span>
                      </button>
                    </li>
                  ))}
                </ul>
                {ipFilter && <p className="mt-1 text-xs text-muted">Showing only {ipFilter}. Select it again to show everything.</p>}
              </div>
            )}

            <div>
              <h3 className="etch mb-1">Requests</h3>
              {entries.length === 0 ? (
                <p className="well px-3 py-4 text-sm text-muted">Nothing yet. Open Haven from a device and it appears here.</p>
              ) : (
                <div className="well max-h-96 overflow-auto">
                  <table className="w-full min-w-[34rem] border-collapse font-mono text-xs">
                    <thead className="sticky top-0 bg-well text-left text-muted">
                      <tr>
                        {['Time', 'From', 'Name used', 'Request', 'Result', 'Time taken'].map((h) => (
                          <th key={h} className="px-3 py-2 font-medium">
                            {h}
                          </th>
                        ))}
                      </tr>
                    </thead>
                    <tbody>
                      {entries.map((e) => (
                        <tr key={e.seq} className="border-t border-rule-2">
                          <td className="whitespace-nowrap px-3 py-1.5 text-muted">{clock(e.time)}</td>
                          <td className="whitespace-nowrap px-3 py-1.5">{e.ip}</td>
                          <td className="whitespace-nowrap px-3 py-1.5">{e.host}</td>
                          <td className="max-w-[16rem] truncate px-3 py-1.5" title={`${e.method} ${e.path}`}>
                            {e.method} {e.path}
                          </td>
                          <td className={cx('px-3 py-1.5', tone(e.status))}>{e.status}</td>
                          <td className={cx('px-3 py-1.5', e.ms > 1000 ? 'text-warn' : 'text-muted')}>{e.ms >= 1000 ? `${(e.ms / 1000).toFixed(1)} s` : `${e.ms} ms`}</td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                </div>
              )}
              <p className="mt-2 text-xs text-muted">
                {log.hidden.toLocaleString()} background refreshes of Haven's own pages are counted but not listed. The log is kept in memory and starts fresh when Haven restarts.
              </p>
            </div>
          </>
        )}

        <div>
          <button className="key key-sm" onClick={() => setShowAvahi(!showAvahi)} aria-expanded={showAvahi}>
            {showAvahi ? 'Hide' : 'Show'} name service log (Avahi)
          </button>
          {showAvahi && (
            <div className="mt-3">
              {avahi ? (
                <LogBox lines={avahi.log ? avahi.log.split('\n') : ['Not available here. This works on the Linux server.']} className="h-56" />
              ) : (
                <Spinner />
              )}
            </div>
          )}
        </div>
      </div>
    </section>
  )
}
