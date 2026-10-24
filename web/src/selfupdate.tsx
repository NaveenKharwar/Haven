import { useEffect, useRef, useState } from 'react'
import { Download, RefreshCw } from 'lucide-react'
import { api, type SelfStatus } from './api'
import { ErrorLine, JobDialog, Led, Spinner, toast } from './components'
import { Markdown } from './markdown'

function ago(iso?: string): string {
  if (!iso || iso.startsWith('0001')) return 'never'
  const s = Math.max(0, Math.round((Date.now() - new Date(iso).getTime()) / 1000))
  if (s < 60) return 'just now'
  if (s < 3600) return `${Math.round(s / 60)} min ago`
  if (s < 86400) return `${Math.round(s / 3600)} h ago`
  return `${Math.round(s / 86400)} d ago`
}

/** Haven's own updates, shown to the admin on the Updates page. */
export default function HavenUpdateCard({ status, onRefresh }: { status: SelfStatus | null; onRefresh: () => void | Promise<unknown> }) {
  const [checking, setChecking] = useState(false)
  const [job, setJob] = useState<string | null>(null)
  const [restarting, setRestarting] = useState(false)
  const [err, setErr] = useState('')
  const s = status

  const check = async () => {
    setChecking(true)
    setErr('')
    try {
      await api.selfUpdateCheck()
      await onRefresh()
    } catch (e: any) {
      setErr(e.message)
    } finally {
      setChecking(false)
    }
  }
  const apply = async () => {
    setErr('')
    try {
      from.current = s?.current || ''
      setJob((await api.selfUpdateApply()).id)
    } catch (e: any) {
      setErr(e.message)
    }
  }
  // The server restarts itself after installing. Wait until the new version answers, then reload.
  // Watching for the new version (not for the moment Haven goes down) means a quick restart,
  // or pressing Done late, can't leave this page waiting for ever.
  const from = useRef('')
  const timer = useRef<ReturnType<typeof setInterval> | null>(null)
  const stop = () => {
    if (timer.current) clearInterval(timer.current)
    timer.current = null
  }
  useEffect(() => stop, [])
  const waitForNewVersion = () => {
    if (timer.current) return // already waiting
    setRestarting(true)
    const startedAt = Date.now()
    let sawDown = false
    timer.current = setInterval(async () => {
      try {
        const st = await api.status()
        if (from.current && st.version !== from.current) {
          stop()
          location.reload()
        } else if (sawDown) {
          // It came back, still on the old version: the update rolled back.
          stop()
          setRestarting(false)
          toast.error(`Haven restarted but is still on version ${st.version}. The update didn't take, so Haven went back to it. Check the console for why.`)
          onRefresh()
        }
      } catch {
        sawDown = true
      }
      if (timer.current && Date.now() - startedAt > 120000) {
        stop()
        setRestarting(false)
        toast.error('Haven is taking longer than usual to come back. Press Reload now, or check the server with: sudo journalctl -u haven -n 50')
      }
    }, 1500)
  }

  if (!s) return null
  // Nothing to install and nothing wrong: one slim row, not a whole card.
  if (!s.available && !s.error && !err && s.supported && !job && !restarting) {
    return (
      <section className="plate flex flex-wrap items-center gap-x-4 gap-y-2 px-4 py-3" aria-label="Haven">
        <Led s="ok" />
        <span className="text-sm">
          Haven <span className="font-mono text-xs">{s.current}</span> is up to date
        </span>
        <span className="text-xs text-muted">Checked {ago(s.checked)}</span>
        <button className="key key-sm ml-auto" onClick={check} disabled={checking}>
          <RefreshCw size={13} className={checking ? 'animate-spin' : ''} /> Check Haven
        </button>
      </section>
    )
  }
  return (
    <section className="plate overflow-hidden" aria-label="Haven">
      <div className="plate-head">
        <h2 className="etch">Haven</h2>
        <span className="flex items-center gap-2">
          <Led s={!s.supported ? 'off' : s.available || s.error ? 'warn' : 'ok'} />
          <span className="etch text-[0.625rem]">{!s.supported ? 'Manual updates' : s.available ? 'Update available' : s.error ? 'Can’t check' : 'Up to date'}</span>
        </span>
      </div>
      <div className="space-y-3 p-4 sm:p-5">
        <p className="text-sm">
          Running version <span className="font-mono text-xs">{s.current}</span>
          {s.available && (
            <>
              {' '}
              → new version <span className="font-mono text-xs">{s.latest}</span>
              {s.versions && s.versions.length > 1 ? <span className="text-muted"> ({s.versions.length + (s.more ?? 0)} releases ahead)</span> : null}
            </>
          )}
        </p>
        {!s.supported && <p className="text-sm text-ink-2">{s.reason} Update by running the installer from a new release.</p>}
        {s.available && s.versions && s.versions.length > 0 && (
          <div className="space-y-2">
            <p className="text-sm">
              {s.versions.length > 1 || (s.more ?? 0) > 0
                ? `This update installs ${s.versions.length + (s.more ?? 0)} releases at once, so you get everything they changed:`
                : 'What is new in this release:'}
            </p>
            <ol className="well max-h-72 divide-y divide-rule-2 overflow-auto" aria-label="Release notes">
              {s.versions.map((v, i) => (
                <li key={v.version} className="space-y-1.5 p-3">
                  <div className="flex flex-wrap items-baseline gap-x-3">
                    <span className="font-mono text-sm font-semibold">{v.version}</span>
                    {i === 0 && <span className="tag tag-accent">Newest</span>}
                    {v.published && !v.published.startsWith('0001') && <span className="text-xs text-muted">{ago(v.published)}</span>}
                  </div>
                  {v.notes ? <Markdown text={v.notes} className="space-y-2 text-sm" /> : <p className="text-sm text-muted">No notes were written for this release.</p>}
                </li>
              ))}
              {(s.more ?? 0) > 0 && <li className="p-3 text-xs text-muted">…and {s.more} older release{s.more === 1 ? '' : 's'}. See them on GitHub.</li>}
            </ol>
          </div>
        )}
        {s.available && (
          <p className="text-xs text-muted">
            Haven downloads the release, checks its signature and checksum, installs it and restarts, which takes a few seconds. Your apps keep running. If the new version doesn't start, Haven goes back to this one by itself.
          </p>
        )}
        {(s.error || err) && <ErrorLine>{err || s.error}</ErrorLine>}
        {restarting && (
          <div className="flex flex-wrap items-center gap-3 text-sm text-ink-2">
            <Spinner /> Haven is restarting. This page reloads by itself.
            <button className="key key-sm" onClick={() => location.reload()}>
              Reload now
            </button>
          </div>
        )}
        {s.supported && (
          <div className="flex flex-wrap items-center gap-2">
            <button className="key key-sm" onClick={check} disabled={checking || restarting}>
              <RefreshCw size={13} className={checking ? 'animate-spin' : ''} /> Check now
            </button>
            {s.available && (
              <button className="key key-sm key-primary" onClick={apply} disabled={restarting}>
                <Download size={13} /> Update to {s.latest}
              </button>
            )}
            <span className="text-xs text-muted">Checked {ago(s.checked)}</span>
          </div>
        )}
      </div>
      <JobDialog
        jobId={job}
        title="Haven"
        onFinished={(j) => j.status === 'success' && waitForNewVersion()}
        onClose={(j) => {
          setJob(null)
          if (j?.status === 'success') waitForNewVersion()
        }}
      />
    </section>
  )
}
