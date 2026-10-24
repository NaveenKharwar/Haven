import { useEffect, useRef, useState, type ReactNode } from 'react'
import { createPortal } from 'react-dom'
import { LoaderCircle, X } from 'lucide-react'
import { api, type Check, type Job } from './api'

/** A group of page sections that lays itself out by how many there are and how much room there is. */
export function Sections({ children, min, className }: { children: ReactNode; min?: string; className?: string }) {
  return (
    <div className={cx('sections', className)} style={min ? ({ '--sec-min': min } as React.CSSProperties) : undefined}>
      {children}
    </div>
  )
}

export function cx(...c: (string | false | null | undefined)[]) {
  return c.filter(Boolean).join(' ')
}

// --- Indicators -----------------------------------------------------------

export function Led({ s, label }: { s: 'ok' | 'warn' | 'bad' | 'busy' | 'off'; label?: string }) {
  return <span className="led" data-s={s === 'off' ? undefined : s} role={label ? 'img' : undefined} aria-label={label} aria-hidden={label ? undefined : true} />
}

/** Instrument readout: pads with leading zeros that stay unlit, like a segment display. */
export function Readout({ value, digits = 3, decimals = 0, unit, className }: { value: number | null | undefined; digits?: number; decimals?: number; unit?: string; className?: string }) {
  if (value == null || Number.isNaN(value)) {
    return (
      <span className={cx('readout', className)}>
        <span className="unlit">{'-'.repeat(digits)}</span>
        {unit && <span className="ml-1 text-[0.45em] font-medium tracking-normal text-muted">{unit}</span>}
      </span>
    )
  }
  const fixed = Math.max(0, value).toFixed(decimals)
  const [int, frac] = fixed.split('.')
  const pad = Math.max(0, digits - int.length)
  return (
    <span className={cx('readout', className)}>
      {pad > 0 && <span className="unlit">{'0'.repeat(pad)}</span>}
      {int}
      {frac != null && <span className="text-ink-2">.{frac}</span>}
      {unit && <span className="ml-1 text-[0.45em] font-medium tracking-normal text-muted">{unit}</span>}
    </span>
  )
}

/** Segmented meter. `plain` skips the amber/red zones, for bars that compare rather than warn. */
export function Ladder({ percent, segs = 24, plain }: { percent: number; segs?: number; plain?: boolean }) {
  const lit = Math.round((Math.min(100, Math.max(0, percent)) / 100) * segs)
  return (
    <div className="ladder" style={{ ['--segs' as string]: segs }} role="meter" aria-valuenow={Math.round(percent)} aria-valuemin={0} aria-valuemax={100}>
      {Array.from({ length: segs }, (_, i) => {
        const pos = ((i + 1) / segs) * 100
        const on = i < lit ? (plain ? '' : pos > 90 ? 'bad' : pos > 75 ? 'warn' : '') : undefined
        return <i key={i} data-on={on} />
      })}
    </div>
  )
}

/** A stepped trace, like an oscilloscope line. The latest sample is lit. */
export function Trace({ values, max, height = 36 }: { values: number[]; max?: number; height?: number }) {
  const w = 200
  const h = height
  if (values.length < 2) return <svg width="100%" height={h} aria-hidden />
  const top = max ?? Math.max(...values, 1)
  const y = (v: number) => h - 3 - (Math.min(v, top) / top) * (h - 6)
  const step = w / (values.length - 1)
  let d = `M0,${y(values[0]).toFixed(1)}`
  values.forEach((v, i) => {
    if (i === 0) return
    d += ` H${(i * step).toFixed(1)} V${y(v).toFixed(1)}`
  })
  const last = values[values.length - 1]
  return (
    <svg width="100%" height={h} viewBox={`0 0 ${w} ${h}`} preserveAspectRatio="none" aria-hidden className="block">
      <line x1="0" x2={w} y1={h - 1} y2={h - 1} stroke="var(--rule-2)" strokeWidth="1" vectorEffect="non-scaling-stroke" />
      <line x1="0" x2={w} y1={h / 2} y2={h / 2} stroke="var(--rule-2)" strokeWidth="1" strokeDasharray="2 4" vectorEffect="non-scaling-stroke" />
      <path d={d} fill="none" stroke="var(--ink-2)" strokeWidth="1.5" vectorEffect="non-scaling-stroke" />
      <circle cx={w - 1.5} cy={y(last)} r="2.5" fill="var(--accent)" />
    </svg>
  )
}

export function AppIcon({ src, name, size = 40 }: { src?: string; name: string; size?: number }) {
  const [failed, setFailed] = useState(false)
  const style = { width: size, height: size }
  return (
    <span className="well flex shrink-0 items-center justify-center overflow-hidden" style={style} aria-hidden>
      {!src || failed ? (
        <span className="font-mono font-semibold text-ink-2" style={{ fontSize: size * 0.36 }}>
          {name.replace(/[^a-z0-9]/gi, '').slice(0, 2).toUpperCase()}
        </span>
      ) : (
        <img src={src} alt="" loading="lazy" onError={() => setFailed(true)} style={{ width: size * 0.66, height: size * 0.66, objectFit: 'contain' }} />
      )}
    </span>
  )
}

/** A recessed notice with a warning LED: things to read before acting. */
export function Notice({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div className="well flex items-start gap-3 px-4 py-3">
      <span className="mt-1">
        <Led s="warn" />
      </span>
      <div className="min-w-0">
        <div className="etch mb-1 text-warn">{label}</div>
        <div className="break-words text-sm text-ink-2">{children}</div>
      </div>
    </div>
  )
}

/** An inline error, led by a red LED. */
export function ErrorLine({ children, className }: { children: ReactNode; className?: string }) {
  if (!children) return null
  return (
    <p role="alert" className={cx('flex items-start gap-2 text-sm text-bad', className)}>
      <span className="mt-[0.4em]">
        <Led s="bad" />
      </span>
      <span className="min-w-0 whitespace-pre-wrap break-words">{children}</span>
    </p>
  )
}

export function Spinner({ size = 16 }: { size?: number }) {
  return <LoaderCircle size={size} className="spin text-muted" aria-label="Loading" />
}

// --- Overlays -------------------------------------------------------------

function useEscape(open: boolean, onClose: () => void) {
  useEffect(() => {
    if (!open) return
    const onKey = (e: KeyboardEvent) => e.key === 'Escape' && onClose()
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [open, onClose])
}

export function Modal({ open, onClose, label, title, children, wide, footer }: {
  open: boolean
  onClose: () => void
  label?: string
  title: ReactNode
  children: ReactNode
  wide?: boolean
  footer?: ReactNode
}) {
  useEscape(open, onClose)
  if (!open) return null
  // Portalled to <body> so an animated (transformed) page can't trap it.
  return createPortal(
    <div className="fixed inset-0 z-50 flex items-end justify-center bg-scrim sm:items-center sm:p-6" onMouseDown={onClose}>
      <div
        role="dialog"
        aria-modal="true"
        className={cx('plate rise flex max-h-[92vh] w-full flex-col overflow-hidden rounded-b-none sm:rounded-[3px]', wide ? 'sm:max-w-3xl' : 'sm:max-w-lg')}
        onMouseDown={(e) => e.stopPropagation()}
      >
        <div className="plate-head">
          <div className="flex min-w-0 items-center gap-3">
            {label && <span className="etch">{label}</span>}
            <span className="min-w-0 truncate text-sm font-semibold">{title}</span>
          </div>
          <button className="key key-flat key-sm key-icon" onClick={onClose} aria-label="Close">
            <X size={15} />
          </button>
        </div>
        <div className="overflow-y-auto p-4 sm:p-5">{children}</div>
        {footer && <div className="flex flex-wrap items-center justify-end gap-2 border-t border-rule-2 px-4 py-3">{footer}</div>}
      </div>
    </div>,
    document.body,
  )
}

export function Drawer({ open, onClose, children, label }: { open: boolean; onClose: () => void; children: ReactNode; label: string }) {
  useEscape(open, onClose)
  if (!open) return null
  return createPortal(
    <div className="fixed inset-0 z-40 flex justify-end bg-scrim" onMouseDown={onClose}>
      <aside aria-label={label} className="slide flex h-full w-full max-w-[44rem] flex-col border-l border-rule bg-paper" onMouseDown={(e) => e.stopPropagation()}>
        {children}
      </aside>
    </div>,
    document.body,
  )
}

// --- Self-test list (pre-install checks) ----------------------------------

const checkTag = { ok: ['tag-ok', 'Pass'], warn: ['tag-warn', 'Note'], fail: ['tag-bad', 'Fail'] } as const

export function ChecksList({ checks }: { checks: Check[] }) {
  return (
    <ul className="divide-y divide-rule-2">
      {checks.map((c, i) => (
        <li key={i} className="flex items-start gap-3 py-2 text-sm">
          <span className={cx('tag mt-0.5 w-12 justify-center', checkTag[c.level][0])}>{checkTag[c.level][1]}</span>
          <span className="min-w-0">
            <span className="font-medium">{c.name}</span>
            <span className="text-ink-2"> · {c.message}</span>
          </span>
        </li>
      ))}
    </ul>
  )
}

// --- Console tape ---------------------------------------------------------

export function LogBox({ lines, className }: { lines: string[]; className?: string }) {
  const ref = useRef<HTMLDivElement>(null)
  const stick = useRef(true)
  useEffect(() => {
    const el = ref.current
    if (el && stick.current) el.scrollTop = el.scrollHeight
  }, [lines])
  return (
    <div
      ref={ref}
      onScroll={(e) => {
        const el = e.currentTarget
        stick.current = el.scrollHeight - el.scrollTop - el.clientHeight < 40
      }}
      className={cx('well tape overflow-auto p-3', className)}
      role="log"
    >
      {lines.length === 0 ? (
        <div className="text-muted">Waiting for output…</div>
      ) : (
        lines.map((l, i) => (
          <div key={i} className={cx('whitespace-pre-wrap break-all', l.startsWith('»') && 'text-console-cmd', l.startsWith('✓') && 'font-semibold text-console-ok', l.startsWith('✗') && 'font-semibold text-console-bad')}>
            {l}
          </div>
        ))
      )}
    </div>
  )
}

const actionWord: Record<string, string> = {
  install: 'Install', uninstall: 'Uninstall', update: 'Update', start: 'Start', stop: 'Stop', restart: 'Restart', reconfigure: 'Apply settings', cleanup: 'Cleanup', 'haven-update': 'Update',
}

export function useJob(jobId: string | null) {
  const [job, setJob] = useState<Job | null>(null)
  useEffect(() => {
    setJob(null)
    if (!jobId) return
    let stop = false
    const tick = async () => {
      try {
        const j = await api.job(jobId)
        if (stop) return
        setJob(j)
        if (j.status === 'running') setTimeout(tick, 900)
      } catch {
        if (!stop) setTimeout(tick, 2000)
      }
    }
    tick()
    return () => {
      stop = true
    }
  }, [jobId])
  return job
}

function copyText(t: string) {
  if (navigator.clipboard?.writeText) return navigator.clipboard.writeText(t)
  const ta = document.createElement('textarea')
  ta.value = t
  document.body.appendChild(ta)
  ta.select()
  document.execCommand('copy')
  ta.remove()
  return Promise.resolve()
}

function HelpBox({ help, error }: { help: NonNullable<Job['help']>; error?: string }) {
  const [copied, setCopied] = useState(false)
  return (
    <div className="well mb-3 space-y-2 p-3 text-sm" role="alert">
      <div className="font-semibold text-bad">{help.title}</div>
      <p className="text-ink-2">{help.why}</p>
      <p>
        <span className="etch mr-2">How to fix</span>
        {help.fix}
      </p>
      {help.command && (
        <div className="flex items-center gap-2">
          <code className="tape min-w-0 flex-1 break-all rounded px-2 py-1.5">{help.command}</code>
          <button className="key key-sm" onClick={() => copyText(help.command!).then(() => { setCopied(true); setTimeout(() => setCopied(false), 1500) })}>
            {copied ? 'Copied' : 'Copy'}
          </button>
        </div>
      )}
      {error && <p className="break-words text-xs text-muted">Haven says: {error}</p>}
    </div>
  )
}

function CopyLog({ job }: { job: Job }) {
  const [copied, setCopied] = useState(false)
  return (
    <div className="mt-2 flex items-center justify-between gap-3 text-xs text-muted">
      <span>Sending this to someone for help? Copy the log.</span>
      <button
        className="key key-flat key-sm"
        onClick={() =>
          copyText(`Haven ${job.action} ${job.app} (${job.status})\n` + (job.log || []).join('\n')).then(() => {
            setCopied(true)
            setTimeout(() => setCopied(false), 1500)
          })
        }
      >
        {copied ? 'Copied' : 'Copy log'}
      </button>
    </div>
  )
}

export function JobDialog({ jobId, title, onClose, onFinished }: { jobId: string | null; title?: string; onClose: (job: Job | null) => void; onFinished?: (job: Job) => void }) {
  const job = useJob(jobId)
  // Tell the caller the moment the job ends, even if the dialog is still open.
  const finished = job && job.status !== 'running' ? job.id : null
  useEffect(() => {
    if (finished && job) onFinished?.(job)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [finished])
  const running = !job || job.status === 'running'
  return (
    <Modal
      open={!!jobId}
      onClose={() => onClose(job)}
      wide
      label="Console"
      title={
        <span className="flex items-center gap-3">
          <Led s={running ? 'busy' : job?.status === 'success' ? 'ok' : 'bad'} />
          {actionWord[job?.action || ''] || 'Working'} · {title || job?.app}
        </span>
      }
      footer={
        <button className={cx('key', !running && 'key-primary')} onClick={() => onClose(job)} autoFocus>
          {running ? 'Hide, keep running' : 'Done'}
        </button>
      }
    >
      {job?.status === 'failed' && job.help && <HelpBox help={job.help} error={job.error} />}
      <LogBox lines={job?.log || []} className={job?.help ? 'h-[14rem]' : 'h-[22rem]'} />
      {job && (job.log?.length || 0) > 0 && <CopyLog job={job} />}
    </Modal>
  )
}

// --- Toasts ---------------------------------------------------------------

type Toast = { id: number; text: string; kind: 'ok' | 'bad' }
let pushToast: (t: Omit<Toast, 'id'>) => void = () => {}
export const toast = {
  ok: (text: string) => pushToast({ text, kind: 'ok' }),
  error: (text: string) => pushToast({ text, kind: 'bad' }),
}

export function Toaster() {
  const [items, setItems] = useState<Toast[]>([])
  useEffect(() => {
    let n = 0
    pushToast = (t) => {
      const id = ++n
      setItems((xs) => [...xs.slice(-3), { ...t, id }])
      setTimeout(() => setItems((xs) => xs.filter((x) => x.id !== id)), t.kind === 'bad' ? 7000 : 4000)
    }
  }, [])
  return (
    <div className="pointer-events-none fixed bottom-20 left-4 right-4 z-[60] flex flex-col gap-2 sm:bottom-6 sm:left-6 sm:right-auto sm:w-[24rem]" aria-live="polite">
      {items.map((t) => (
        <div key={t.id} className="plate rise pointer-events-auto flex items-start gap-3 px-4 py-3 text-sm">
          <span className="mt-2">
            <Led s={t.kind === 'ok' ? 'ok' : 'bad'} />
          </span>
          <span className="min-w-0 break-words">{t.text}</span>
        </div>
      ))}
    </div>
  )
}

// --- Data ----------------------------------------------------------------

export function usePoll<T>(fn: () => Promise<T>, ms: number, deps: unknown[] = []) {
  const [data, setData] = useState<T | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [tick, setTick] = useState(0)
  useEffect(() => {
    let stop = false
    let timer: ReturnType<typeof setTimeout>
    const run = async () => {
      try {
        const d = await fn()
        if (!stop) {
          setData(d)
          setError(null)
        }
      } catch (e) {
        if (!stop) setError((e as Error).message)
      }
      if (!stop) timer = setTimeout(run, document.hidden ? ms * 4 : ms)
    }
    run()
    return () => {
      stop = true
      clearTimeout(timer)
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [...deps, tick])
  return { data, error, refresh: () => setTick((t) => t + 1) }
}

export function useClock() {
  const [now, setNow] = useState(() => new Date())
  useEffect(() => {
    const t = setInterval(() => setNow(new Date()), 15000)
    return () => clearInterval(t)
  }, [])
  return now
}
