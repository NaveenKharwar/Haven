import { useEffect, useRef, useState } from 'react'
import { Bell, Check, Trash2 } from 'lucide-react'
import { api, type Notification } from './api'
import { JobDialog, Led, cx, usePoll } from './components'
import type { Route } from './context'

function ago(iso: string): string {
  const s = Math.max(0, Math.round((Date.now() - new Date(iso).getTime()) / 1000))
  if (s < 60) return 'just now'
  if (s < 3600) return `${Math.round(s / 60)} min ago`
  if (s < 86400) return `${Math.round(s / 3600)} h ago`
  return `${Math.round(s / 86400)} d ago`
}

const led: Record<Notification['level'], 'ok' | 'warn' | 'bad' | 'off'> = { ok: 'ok', warn: 'warn', bad: 'bad', info: 'off' }

/** The notification bell: unread count, and a dropdown with the list. */
export default function NotificationBell({ onOpenRoute }: { onOpenRoute: (r: Route) => void }) {
  const [open, setOpen] = useState(false)
  const [jobId, setJobId] = useState<string | null>(null)
  const { data, refresh } = usePoll(api.notifications, open ? 8000 : 20000, [open])
  const box = useRef<HTMLDivElement>(null)
  useEffect(() => {
    if (!open) return
    const away = (e: MouseEvent) => !box.current?.contains(e.target as Node) && setOpen(false)
    const esc = (e: KeyboardEvent) => e.key === 'Escape' && setOpen(false)
    document.addEventListener('mousedown', away)
    window.addEventListener('keydown', esc)
    return () => {
      document.removeEventListener('mousedown', away)
      window.removeEventListener('keydown', esc)
    }
  }, [open])

  const items = data?.items || []
  const unread = data?.unread || 0
  const act = async (fn: () => Promise<unknown>) => {
    try {
      await fn()
    } finally {
      refresh()
    }
  }
  return (
    <div ref={box} className="relative">
      <button className="key key-sm key-icon relative h-9 w-9" onClick={() => setOpen(!open)} aria-haspopup="dialog" aria-expanded={open} aria-label={unread ? `Notifications, ${unread} unread` : 'Notifications'}>
        <Bell size={16} />
        {unread > 0 && (
          <span className="absolute -right-1 -top-1 grid h-[1.1rem] min-w-[1.1rem] place-items-center rounded-full bg-accent px-1 font-mono text-[0.625rem] font-bold leading-none text-accent-ink">{unread > 9 ? '9+' : unread}</span>
        )}
      </button>
      {open && (
        <div role="dialog" aria-label="Notifications" className="plate rise fixed inset-x-3 top-16 z-40 flex max-h-[min(34rem,calc(100vh-5rem))] flex-col overflow-hidden shadow-xl sm:absolute sm:inset-x-auto sm:right-0 sm:top-full sm:mt-2 sm:w-[24rem]">
          <div className="plate-head shrink-0">
            <h2 className="etch">Notifications</h2>
            <span className="flex gap-1">
              <button className="key key-flat key-sm h-7 px-2 text-[0.6875rem]" disabled={unread === 0} onClick={() => act(() => api.readNotifications('all'))}>
                <Check size={12} /> Mark all read
              </button>
              <button className="key key-flat key-sm key-icon h-7 w-7" disabled={items.length === 0} onClick={() => act(() => api.clearNotifications(true))} aria-label="Clear all" title="Clear all">
                <Trash2 size={13} />
              </button>
            </span>
          </div>
          {items.length === 0 ? (
            <p className="px-4 py-10 text-center text-sm text-muted">Nothing to report. Haven will tell you about failing apps, updates and server problems here.</p>
          ) : (
            <ul className="min-h-0 flex-1 divide-y divide-rule-2 overflow-y-auto">
              {items.map((n) => (
                <li key={n.id}>
                  <button
                    className={cx('flex w-full items-start gap-3 px-4 py-3 text-left hover:bg-raised', !n.read && 'bg-accent-wash')}
                    onClick={() => {
                      if (!n.read) api.readNotifications([n.id]).finally(refresh)
                      if (n.job) {
                        setOpen(false)
                        setJobId(n.job)
                      } else if (n.route) {
                        setOpen(false)
                        onOpenRoute(n.route as Route)
                      }
                    }}
                  >
                    <span className="mt-1.5">
                      <Led s={led[n.level]} />
                    </span>
                    <span className="min-w-0 flex-1">
                      <span className={cx('block text-sm', !n.read && 'font-semibold')}>{n.title}</span>
                      {n.job && <span className="mt-0.5 block text-xs text-accent">See what happened</span>}
                      {n.body && <span className="mt-0.5 line-clamp-3 block text-xs text-ink-2">{n.body}</span>}
                      <span className="mt-1 block font-mono text-[0.6875rem] text-muted">{ago(n.time)}</span>
                    </span>
                  </button>
                </li>
              ))}
            </ul>
          )}
        </div>
      )}
      <JobDialog jobId={jobId} title="" onClose={() => setJobId(null)} />
    </div>
  )
}
