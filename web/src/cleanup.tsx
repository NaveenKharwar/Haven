import { useState } from 'react'
import { FolderOpen, Sparkles } from 'lucide-react'
import { api, formatBytes, type CleanupItem } from './api'
import { ErrorLine, JobDialog, Spinner, cx, usePoll } from './components'

/** Safe, explained ways to get disk space back. */
export default function CleanupPanel({ onOpenPath }: { onOpenPath: (path: string) => void }) {
  const { data, error, refresh } = usePoll(async () => (await api.cleanupScan()).items, 600000)
  const [picked, setPicked] = useState<Set<string>>(new Set())
  const [job, setJob] = useState<string | null>(null)
  const [err, setErr] = useState('')
  const items: CleanupItem[] = data || []
  const total = items.filter((i) => picked.has(i.id)).reduce((n, i) => n + i.bytes, 0)

  const toggle = (id: string) =>
    setPicked((s) => {
      const n = new Set(s)
      n.has(id) ? n.delete(id) : n.add(id)
      return n
    })
  const run = async () => {
    setErr('')
    try {
      const j = await api.cleanupRun([...picked])
      setJob(j.id)
    } catch (e: any) {
      setErr(e.message)
    }
  }

  return (
    <section className="plate overflow-hidden" aria-label="Cleanup">
      <div className="plate-head">
        <h2 className="etch">Cleanup</h2>
        <button className="key key-sm" onClick={() => refresh()}>Scan again</button>
      </div>
      <div className="space-y-3 p-4 sm:p-5">
        <p className="text-sm text-ink-2">Get disk space back. Haven shows what each item frees before anything runs. Your apps' data and files are never touched here.</p>
        {!data && !error && <div className="flex items-center gap-3 text-sm text-ink-2"><Spinner /> Measuring…</div>}
        {error && <ErrorLine>{error}</ErrorLine>}
        <ul className="divide-y divide-rule-2">
          {items.map((i) => (
            <li key={i.id}>
              <label className="flex cursor-pointer items-start gap-3 py-3">
                <input type="checkbox" className="mt-1 h-4 w-4 shrink-0 accent-[var(--accent)]" checked={picked.has(i.id)} onChange={() => toggle(i.id)} />
                <span className="min-w-0 flex-1">
                  <span className="flex items-baseline justify-between gap-3">
                    <span className="font-medium">{i.title}</span>
                    <span className={cx('shrink-0 font-mono text-xs', i.bytes > 0 || i.hint ? 'text-accent' : 'text-muted')}>{i.hint || (i.bytes > 0 ? `frees ${formatBytes(i.bytes)}` : 'nothing to clean')}</span>
                  </span>
                  <span className="mt-0.5 block text-xs text-ink-2">{i.info}</span>
                  {i.note && <span className="mt-0.5 block text-xs text-warn">{i.note}</span>}
                </span>
              </label>
              {i.path && (
                <button className="key key-flat key-sm mb-3 ml-7" onClick={() => onOpenPath(i.path!)}>
                  <FolderOpen size={13} /> Open <span className="font-mono">{i.path}</span> in Files
                </button>
              )}
            </li>
          ))}
        </ul>
        {err && <ErrorLine>{err}</ErrorLine>}
        <div className="flex items-center justify-between gap-3">
          <span className="text-xs text-muted">{picked.size > 0 ? `${picked.size} selected, about ${formatBytes(total)}` : 'Tick what you want to clean.'}</span>
          <button className="key key-primary" disabled={picked.size === 0} onClick={run}><Sparkles size={14} /> Clean selected</button>
        </div>
      </div>
      <JobDialog jobId={job} title="Cleanup" onClose={() => { setJob(null); setPicked(new Set()); refresh() }} />
    </section>
  )
}
