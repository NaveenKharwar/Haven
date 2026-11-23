import { useEffect, useRef, useState } from 'react'
import { ArrowUpRight, Bell, BellOff, Check, Circle, Terminal, Trash2, X } from 'lucide-react'
import { api, ApiError, type Notification, type NotificationFeed } from './api'
import { ErrorLine, JobDialog, Led, Spinner, cx, usePoll } from './components'
import type { Route } from './context'

function ago(iso: string): string {
  const seconds = Math.max(0, Math.floor((Date.now() - new Date(iso).getTime()) / 1000))
  if (seconds < 60) return 'Just now'
  if (seconds < 3600) return `${Math.floor(seconds / 60)} min ago`
  if (seconds < 86400) return `${Math.floor(seconds / 3600)} hr ago`
  return `${Math.floor(seconds / 86400)} d ago`
}

const levels = {
  ok: { light: 'ok', label: 'Success' },
  warn: { light: 'warn', label: 'Warning' },
  bad: { light: 'bad', label: 'Error' },
  info: { light: 'off', label: 'Info' },
} as const

export default function NotificationBell({ destinations, onOpenRoute }: {
  destinations: { id: Route; label: string }[]
  onOpenRoute: (r: Route) => void
}) {
  const [open, setOpen] = useState(false)
  const [unreadOnly, setUnreadOnly] = useState(false)
  const [jobId, setJobId] = useState<string | null>(null)
  const [actionError, setActionError] = useState('')
  const [busy, setBusy] = useState(false)
  const { data, error, setData, refresh } = usePoll(api.notifications, open ? 8000 : 20000, [open])
  const box = useRef<HTMLDivElement>(null)
  const trigger = useRef<HTMLButtonElement>(null)
  const allTab = useRef<HTMLButtonElement>(null)
  const close = () => {
    setOpen(false)
    trigger.current?.focus()
  }

  useEffect(() => {
    if (!open) return
    allTab.current?.focus()
    const away = (e: PointerEvent) => !box.current?.contains(e.target as Node) && setOpen(false)
    const esc = (e: KeyboardEvent) => {
      if (e.key === 'Escape') close()
    }
    document.addEventListener('pointerdown', away)
    document.addEventListener('keydown', esc)
    return () => {
      document.removeEventListener('pointerdown', away)
      document.removeEventListener('keydown', esc)
    }
  }, [open])

  const items = data?.items ?? []
  const unread = data?.unread ?? 0
  const visible = unreadOnly ? items.filter((n) => !n.read) : items

  const act = async (fn: () => Promise<NotificationFeed | undefined>, restoreFocus = false) => {
    setActionError('')
    setBusy(true)
    try {
      const updated = await fn()
      if (updated) {
        setData(updated)
        refresh()
      }
    } catch (e) {
      setActionError((e as Error).message)
    } finally {
      setBusy(false)
      if (restoreFocus && (document.activeElement === document.body || box.current?.contains(document.activeElement))) {
        allTab.current?.focus()
      }
    }
  }

  const viewLog = (n: Notification, id: string) => act(async () => {
    try {
      await api.job(id)
    } catch (e) {
      if (e instanceof ApiError && e.status === 404) throw new Error('This job log has expired.')
      throw e
    }
    const updated = n.read ? undefined : await api.readNotifications([n.id])
    close()
    setJobId(id)
    return updated
  })

  return (
    <div ref={box} className="relative">
      <button ref={trigger} className="key key-sm key-icon relative h-9 w-9" onClick={() => { setActionError(''); setOpen(!open) }} aria-haspopup="dialog" aria-expanded={open} aria-label={unread ? `Notifications, ${unread} unread` : 'Notifications'}>
        <Bell size={16} />
        {unread > 0 && (
          <span className="absolute -right-1 -top-1 grid h-[1.1rem] min-w-[1.1rem] place-items-center rounded-full bg-accent px-1 font-mono text-[0.625rem] font-bold leading-none text-accent-ink">{unread > 9 ? '9+' : unread}</span>
        )}
      </button>
      {open && (
        <div role="dialog" aria-label="Notifications" className="plate rise fixed inset-x-3 top-16 z-40 flex max-h-[min(36rem,calc(100dvh-5rem))] flex-col overflow-hidden shadow-xl sm:absolute sm:inset-x-auto sm:right-0 sm:top-full sm:mt-2 sm:w-[26rem]">
          <div className="plate-head shrink-0">
            <h2 className="text-base font-semibold">Notifications</h2>
            <button className="key key-flat key-sm key-icon" onClick={close} aria-label="Close notifications"><X size={16} /></button>
          </div>
          <div className="flex flex-wrap items-center justify-between gap-x-2 gap-y-2 border-b border-rule px-3 py-2">
            <div className="well inline-flex gap-0.5 p-0.5" role="group" aria-label="Filter notifications">
              <button ref={allTab} className={cx('key key-sm', unreadOnly && 'key-flat')} aria-pressed={!unreadOnly} onClick={() => setUnreadOnly(false)}>All</button>
              <button className={cx('key key-sm', !unreadOnly && 'key-flat')} aria-pressed={unreadOnly} onClick={() => setUnreadOnly(true)}>Unread <span className="font-mono">{unread}</span></button>
            </div>
            <div className="flex flex-wrap gap-1">
              <button className="key key-flat key-sm" disabled={busy || unread === 0} onClick={() => act(() => api.readNotifications('all'), true)}>Mark all read</button>
              <button className="key key-flat key-sm" disabled={busy || items.length === unread} onClick={() => act(() => api.dismissNotifications(items.filter((item) => item.read).map((item) => item.id)), true)}>Clear read</button>
            </div>
          </div>
          {(actionError || error) && (
            <div className="px-3 py-2">
              <ErrorLine>{actionError || error}</ErrorLine>
              {error && <button className="key key-sm mt-2" onClick={refresh}>Retry</button>}
            </div>
          )}
          {visible.length > 0 ? (
            <ul className="min-h-0 flex-1 divide-y divide-rule-2 overflow-y-auto overscroll-contain">
              {visible.map((n) => {
                const destination = destinations.find((route) => n.route === route.id)
                const level = levels[n.level]
                return (
                  <li key={n.id} className={cx('grid grid-cols-[auto_minmax(0,1fr)] gap-x-3 px-4 py-3', !n.read && 'notification-unread')}>
                    <span className="mt-1.5" aria-hidden="true"><Led s={level.light} /></span>
                    <div className="min-w-0">
                      <div className="flex items-start justify-between gap-1">
                        <div className="min-w-0">
                          <h3 className={cx('break-words text-sm leading-snug', n.read ? 'font-medium' : 'font-semibold')}>{n.title}</h3>
                          <p className="mt-1 flex flex-wrap items-center gap-x-2 gap-y-1 text-xs text-muted">
                            <span>{level.label}</span>
                            <time dateTime={n.time} title={new Date(n.time).toLocaleString()}>{ago(n.time)}</time>
                            {!n.read && <span className="text-accent">Unread</span>}
                          </p>
                        </div>
                        <div className="flex shrink-0 gap-0.5">
                          <button className="key key-sm key-flat key-icon max-sm:min-h-9 max-sm:min-w-9" disabled={busy} aria-label={`${n.read ? 'Mark unread' : 'Mark read'}: ${n.title}`} title={n.read ? 'Mark unread' : 'Mark read'} onClick={() => act(() => api.readNotifications([n.id], !n.read), unreadOnly)}>
                            {n.read ? <Circle size={14} /> : <Check size={15} />}
                          </button>
                          <button className="key key-sm key-flat key-icon max-sm:min-h-9 max-sm:min-w-9" disabled={busy} aria-label={`Dismiss: ${n.title}`} title="Dismiss" onClick={() => act(() => api.dismissNotifications([n.id]), true)}>
                            <Trash2 size={14} />
                          </button>
                        </div>
                      </div>
                      {n.body && <p className="mt-2 whitespace-pre-wrap break-words text-sm leading-snug text-ink-2">{n.body}</p>}
                      {(n.job || destination) && (
                        <div className="mt-2 flex flex-wrap gap-2">
                          {n.job && <button className="key key-sm" disabled={busy} onClick={() => viewLog(n, n.job!)}><Terminal size={14} /> View log</button>}
                          {destination && (
                            <button className="key key-sm key-flat" disabled={busy} onClick={() => act(async () => {
                              const updated = n.read ? undefined : await api.readNotifications([n.id])
                              close()
                              onOpenRoute(destination.id)
                              return updated
                            })}>
                              Open {destination.label} <ArrowUpRight size={13} />
                            </button>
                          )}
                        </div>
                      )}
                    </div>
                  </li>
                )
              })}
            </ul>
          ) : data ? (
            <div className="grid justify-items-center gap-2 px-4 py-9 text-center">
              <BellOff size={20} className="text-muted" />
              <p className="text-sm font-medium">{unreadOnly ? 'No unread notifications' : 'No notifications yet'}</p>
              {unreadOnly && items.length > 0 && <button className="key key-flat key-sm" onClick={() => setUnreadOnly(false)}>Show all</button>}
            </div>
          ) : !error && (
            <div role="status" className="grid place-items-center p-9"><Spinner /><span className="sr-only">Loading notifications</span></div>
          )}
        </div>
      )}
      <JobDialog jobId={jobId} onClose={() => { setJobId(null); trigger.current?.focus() }} />
    </div>
  )
}
