import { useCallback, useEffect, useRef, useState, type ReactNode } from 'react'
import { ArrowUpRight, Copy } from 'lucide-react'
import { api, type Diagnosis, type PowerStatus, type RemoteStatus } from './api'
import { ErrorLine, Led, LogBox, Sections, Spinner, cx, toast, useJob, usePoll } from './components'
import { useHaven } from './context'
import { DiagnosisView, QR, copyText } from './diagnosis'
import NetworkPanel from './network'
import InsightsPanel from './insights'
import AccessLogPanel from './logs'
import FilesPanel from './files'
import CleanupPanel from './cleanup'

function Panel({ title, status, children }: { title: string; status?: ReactNode; children: ReactNode }) {
  return (
    <section className="plate overflow-hidden" aria-label={title}>
      <div className="plate-head">
        <h2 className="etch">{title}</h2>
        {status}
      </div>
      <div className="p-4 sm:p-5">{children}</div>
    </section>
  )
}

function Unsupported({ text }: { text?: string }) {
  return (
    <p className="flex items-start gap-3 text-sm text-ink-2">
      <span className="mt-1.5">
        <Led s="off" />
      </span>
      <span>{text} Install Haven on your Ubuntu server and this panel sets it up with one click.</span>
    </p>
  )
}

function Row({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div className="flex items-baseline justify-between gap-4 py-2 text-sm">
      <span className="etch">{label}</span>
      <span className="min-w-0 truncate text-right font-mono text-xs text-ink">{children}</span>
    </div>
  )
}

function CopyLine({ url }: { url: string }) {
  const [copied, setCopied] = useState(false)
  return (
    <div className="well flex items-center gap-2 py-1 pl-3 pr-1">
      <a href={url} target="_blank" rel="noreferrer" className="min-w-0 flex-1 truncate font-mono text-sm hover:text-accent">
        {url}
      </a>
      <button
        className="key key-flat key-sm key-icon"
        aria-label={`Copy ${url}`}
        onClick={async () => {
          setCopied(await copyText(url))
          setTimeout(() => setCopied(false), 1500)
        }}
      >
        {copied ? <Led s="ok" /> : <Copy size={13} />}
      </button>
    </div>
  )
}

function JobTail({ id }: { id: string }) {
  const job = useJob(id)
  return <LogBox lines={(job?.log || []).slice(-8)} className="mt-3 h-36" />
}

// --- Remote access ----------------------------------------------------------

function RemotePanel() {
  const [fast, setFast] = useState(false)
  const { data: st, refresh } = usePoll(api.remote, fast ? 2500 : 15000, [fast])
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  useEffect(() => {
    setFast(!!st && (!!st.jobId || st.state === 'NeedsLogin' || st.state === 'Starting'))
  }, [st])

  const setup = async () => {
    setBusy(true)
    setError('')
    try {
      await api.remoteSetup()
      setFast(true)
      refresh()
    } catch (e) {
      setError((e as Error).message)
    } finally {
      setBusy(false)
    }
  }
  const act = async (a: 'down' | 'logout') => {
    if (a === 'logout' && !confirm('Sign this server out of Tailscale? You will need to sign in again to use remote access.')) return
    setBusy(true)
    try {
      await api.remoteAction(a)
      refresh()
    } catch (e) {
      toast.error((e as Error).message)
    } finally {
      setBusy(false)
    }
  }

  const status = st && (
    <span className="flex items-center gap-2">
      <Led s={st.state === 'Running' ? 'ok' : st.jobId ? 'busy' : st.state === 'NeedsLogin' ? 'warn' : 'off'} />
      <span className="etch text-[0.625rem]">{remoteWord(st)}</span>
    </span>
  )

  return (
    <Panel title="Remote access" status={status}>
      {!st ? (
        <Spinner />
      ) : !st.supported ? (
        <Unsupported text="Remote access is set up on the server." />
      ) : st.state === 'Running' ? (
        <RemoteOn st={st} busy={busy} onAction={act} />
      ) : (
        <div className="space-y-4">
          <p className="text-sm text-ink-2">
            Open Haven and your apps from anywhere, through <span className="text-ink">Tailscale</span>: a private network between your own devices.
            No router setup, nothing exposed to the internet, and free for personal use.
          </p>
          {st.state === 'NeedsLogin' && st.authUrl ? (
            <div className="flex flex-wrap items-center gap-5">
              <QR text={st.authUrl} />
              <div className="min-w-0 flex-1 space-y-3">
                <p className="font-semibold">Sign in to finish</p>
                <p className="text-sm text-ink-2">Scan the code with your phone, or open the link. Use the Tailscale account you used before. This page updates by itself.</p>
                <a href={st.authUrl} target="_blank" rel="noreferrer" className="key key-primary">
                  Sign in to Tailscale <ArrowUpRight size={14} />
                </a>
              </div>
            </div>
          ) : st.jobId ? (
            <div>
              <p className="flex items-center gap-2 text-sm">
                <Spinner size={14} /> {st.installed ? 'Connecting…' : 'Installing Tailscale. This takes a minute…'}
              </p>
              <JobTail id={st.jobId} />
            </div>
          ) : (
            <button className="key key-primary" onClick={setup} disabled={busy} data-loading={busy}>
              {st.installed ? (st.state === 'Stopped' ? 'Turn remote access on' : 'Connect') : 'Set up remote access'}
            </button>
          )}
          <ErrorLine>{error}</ErrorLine>
        </div>
      )}
    </Panel>
  )
}

function remoteWord(st: RemoteStatus): string {
  if (st.jobId) return 'Working…'
  return ({ Running: 'On', NeedsLogin: 'Sign-in needed', Stopped: 'Off', NotInstalled: 'Not set up', NotRunning: 'Service stopped', Starting: 'Starting' } as Record<string, string>)[st.state] || st.state
}

function RemoteOn({ st, busy, onAction }: { st: RemoteStatus; busy: boolean; onAction: (a: 'down' | 'logout') => void }) {
  const urls = st.urls || []
  const expiry = st.keyExpiry ? new Date(st.keyExpiry) : null
  const soon = expiry && expiry.getTime() - Date.now() < 30 * 864e5
  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-start gap-5">
        {urls[0] && <QR text={urls[0]} size={128} />}
        <div className="min-w-0 flex-1 space-y-2">
          <p className="text-sm text-ink-2">Away from home, open any of these on a device signed in to the same Tailscale account:</p>
          {urls.map((u) => (
            <CopyLine key={u} url={u} />
          ))}
        </div>
      </div>
      <div className="divide-y divide-rule-2">
        <Row label="Name">{st.dnsName}</Row>
        <Row label="Your devices">
          {st.peersOnline} of {st.peers} online
        </Row>
        <Row label="Sign-in expires">{expiry ? expiry.toLocaleDateString() : 'never'}</Row>
      </div>
      {soon && (
        <p className="text-sm text-warn">
          The sign-in expires soon. In the Tailscale admin console, open this machine and choose Disable key expiry.
        </p>
      )}
      <p className="text-xs text-muted">On your phone: install the Tailscale app and sign in with the same account. App buttons in Haven then work from anywhere.</p>
      <div className="flex flex-wrap gap-2">
        <a className="key key-sm" href="https://login.tailscale.com/admin/machines" target="_blank" rel="noreferrer">
          Tailscale admin <ArrowUpRight size={13} />
        </a>
        <button className="key key-sm" disabled={busy} onClick={() => onAction('down')}>
          Turn off
        </button>
        <button className="key key-sm key-danger" disabled={busy} onClick={() => onAction('logout')}>
          Sign out
        </button>
      </div>
    </div>
  )
}

// --- Local address ------------------------------------------------------------

function LocalPanel() {
  const { openJob } = useHaven()
  const { data: st, refresh } = usePoll(api.localName, 10000)
  const [name, setName] = useState('')
  const [port80, setPort80] = useState(false)
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)
  const filled = useRef(false)
  useEffect(() => {
    // Fill in the suggestion once; after that the box belongs to the user.
    if (st && !filled.current) {
      filled.current = true
      setName(st.suggestName)
      setPort80(st.port80 || st.port80Free)
    }
  }, [st])

  const ready = !!st && st.running && st.resolves
  const url = st ? `http://${st.advertised}${st.port80 ? '' : `:${st.havenPort}`}` : ''
  const apply = async () => {
    setBusy(true)
    setError('')
    try {
      const job = await api.setLocalName(name, port80)
      openJob(job.id, `local address ${name}.local`)
      setTimeout(refresh, 3000)
    } catch (e) {
      setError((e as Error).message)
    } finally {
      setBusy(false)
    }
  }
  const togglePort80 = async (on: boolean) => {
    try {
      await api.setPort80(on)
      refresh()
    } catch (e) {
      toast.error((e as Error).message)
    }
  }

  return (
    <Panel
      title="Local address"
      status={
        st && (
          <span className="flex items-center gap-2">
            <Led s={ready ? 'ok' : st.installed ? 'warn' : 'off'} />
            <span className="etch text-[0.625rem]">{ready ? 'On' : st.installed ? 'Check' : 'Not set up'}</span>
          </span>
        )
      }
    >
      {!st ? (
        <Spinner />
      ) : (
        <div className="space-y-4">
          <p className="text-sm text-ink-2">Open Haven by name instead of an IP address. The name keeps working when the server's IP changes.</p>
          {ready && <CopyLine url={url} />}
          {!st.supported ? (
            <Unsupported text={st.reason} />
          ) : (
            <>
              <div className="flex flex-wrap items-end gap-2">
                <div className="min-w-0 flex-1 space-y-1.5">
                  <label htmlFor="local-name" className="etch block">
                    Name
                  </label>
                  <div className="flex items-center">
                    <span className="font-mono text-sm text-muted">http://</span>
                    <input
                      id="local-name"
                      className="field mx-1 font-mono"
                      value={name}
                      onChange={(e) => setName(e.target.value.toLowerCase().replace(/[^a-z0-9-]/g, ''))}
                      maxLength={32}
                      aria-invalid={!!error}
                    />
                    <span className="font-mono text-sm text-muted">.local</span>
                  </div>
                </div>
                <button className="key key-primary h-10" onClick={apply} disabled={busy || !name || !!st.jobId} data-loading={busy}>
                  {st.installed ? 'Apply' : 'Set up local address'}
                </button>
              </div>
              <label className={cx('flex items-start gap-3 text-sm', !st.port80Free && 'opacity-60')}>
                <input
                  type="checkbox"
                  className="mt-1 accent-[var(--accent)]"
                  checked={st.installed ? st.port80 : port80}
                  disabled={!st.port80Free}
                  onChange={(e) => (st.installed ? togglePort80(e.target.checked) : setPort80(e.target.checked))}
                />
                <span>
                  Also answer on port 80, so you can leave off :{st.havenPort}
                  {!st.port80Free && <span className="block text-xs text-muted">Port 80 is used by another program.</span>}
                </span>
              </label>
              <ErrorLine>{error}</ErrorLine>
              <div className="divide-y divide-rule-2">
                <Row label="Current IP">{(st.addresses || []).join(', ') || '—'}</Row>
                <Row label="Name service">{st.running ? 'running' : st.installed ? 'stopped' : 'not installed'}</Row>
              </div>
              <NameTest />
              <p className="text-xs text-muted">
                Works on Macs, iPhones, Windows and most Android phones. Some smart TVs can't use .local names; on the TV use the IP above, or the remote-access
                address.
              </p>
            </>
          )}
        </div>
      )}
    </Panel>
  )
}

// --- Lid & power ----------------------------------------------------------------

const lidWord: Record<string, string> = { ignore: 'keeps running', lock: 'keeps running (locks)', suspend: 'goes to sleep', hibernate: 'hibernates', poweroff: 'shuts down', 'suspend-then-hibernate': 'goes to sleep', unknown: 'unknown' }

function PowerPanel() {
  const { data: polled, refresh } = usePoll(api.power, 15000)
  const [st, setSt] = useState<PowerStatus | null>(null)
  const [busy, setBusy] = useState(false)
  const [message, setMessage] = useState('')
  const [error, setError] = useState('')
  useEffect(() => {
    if (polled) setSt(polled)
  }, [polled])

  const set = async (body: { keepAwake?: boolean; screenOff?: boolean }) => {
    setBusy(true)
    setError('')
    setMessage('')
    try {
      const r = await api.setPower(body)
      setSt(r)
      if (r.message) setMessage(r.message)
      refresh()
    } catch (e) {
      setError((e as Error).message)
    } finally {
      setBusy(false)
    }
  }

  return (
    <Panel
      title="Lid & power"
      status={
        st?.supported && (
          <span className="flex items-center gap-2">
            <Led s={st.effective ? 'ok' : 'warn'} />
            <span className="etch text-[0.625rem]">{st.effective ? 'Lid-safe' : 'Will sleep'}</span>
          </span>
        )
      }
    >
      {!st ? (
        <Spinner />
      ) : !st.supported ? (
        <Unsupported text="Lid and sleep settings are managed on the server." />
      ) : (
        <div className="space-y-4">
          <p className="text-sm text-ink-2">
            {st.effective
              ? 'You can close the lid: the server keeps running and never goes to sleep.'
              : 'Right now, closing the lid (or leaving it idle) can put the server to sleep, and your apps go offline.'}
          </p>
          <button className={cx('key', !st.effective && 'key-primary')} onClick={() => set({ keepAwake: !st.keepAwake })} disabled={busy} data-loading={busy}>
            {busy ? <Spinner size={14} /> : null}
            {st.keepAwake ? 'Let the laptop sleep again' : 'Keep running with the lid closed'}
          </button>
          {message && <p className="text-sm text-ok">{message}</p>}
          <ErrorLine>{error}</ErrorLine>
          {st.screenSupported && (
            <label className="flex items-start gap-3 text-sm">
              <input type="checkbox" className="mt-1 accent-[var(--accent)]" checked={st.screenOff} onChange={(e) => set({ screenOff: e.target.checked })} disabled={busy} />
              <span>
                Turn the built-in screen off
                <span className="block text-xs text-muted">Saves power and heat while the lid is closed. Plugging in a monitor still works.</span>
              </span>
            </label>
          )}
          <div className="divide-y divide-rule-2">
            {st.lidState && <Row label="Lid">{st.lidState}</Row>}
            <Row label="Lid closed, on battery">{lidWord[st.lidOnBattery] || st.lidOnBattery}</Row>
            <Row label="Lid closed, plugged in">{lidWord[st.lidOnPower] || st.lidOnPower}</Row>
            <Row label="Sleep">{st.sleepBlocked ? 'blocked' : 'allowed'}</Row>
            {st.battery && (
              <Row label="Battery">
                {st.battery.percent}% · {st.battery.acOnline ? 'plugged in' : 'on battery'}
                {st.battery.status && st.battery.status !== 'Unknown' ? ` · ${st.battery.status.toLowerCase()}` : ''}
              </Row>
            )}
          </div>
          {st.desktop && <p className="text-xs text-muted">A desktop session is open. If a change doesn't show above, restart the laptop once.</p>}
        </div>
      )}
    </Panel>
  )
}

// --- Health check ---------------------------------------------------------------

function HealthPanel() {
  const [d, setD] = useState<Diagnosis | null>(null)
  const [running, setRunning] = useState(false)
  const [started, setStarted] = useState(false)
  const run = useCallback(async () => {
    setStarted(true)
    setRunning(true)
    try {
      setD(await api.diagnoseServer())
    } catch (e) {
      toast.error((e as Error).message)
    } finally {
      setRunning(false)
    }
  }, [])
  return (
    <Panel title="Health check">
      {!started ? (
        <div className="flex flex-wrap items-center justify-between gap-3">
          <p className="text-sm text-ink-2">Checks Docker, disks, memory, temperature, internet, the clock, power and remote access, and explains anything wrong.</p>
          <button className="key key-primary" onClick={run}>
            Run health check
          </button>
        </div>
      ) : (
        <DiagnosisView d={running && !d ? null : d} running={running} onRun={run} />
      )}
    </Panel>
  )
}

const SERVER_TABS = [
  ['connect', 'Connect', 'Remote access and the local name'],
  ['network', 'Network', 'Wi-Fi and IP address'],
  ['power', 'Power', 'Lid and sleep'],
  ['insights', 'Insights', 'This server in detail, and the devices around it'],
  ['health', 'Health & logs', 'Checks and who reached Haven'],
  ['files', 'Files', 'Browse and manage files on the server'],
  ['cleanup', 'Cleanup', 'Get disk space back safely'],
] as const
type ServerTab = (typeof SERVER_TABS)[number][0]

export default function ServerPage() {
  const { role } = useHaven()
  const admin = role === 'admin'
  const tabsShown = admin ? SERVER_TABS : SERVER_TABS.filter(([id]) => id === 'files')
  const [tab, setTab] = useState<ServerTab>(() => {
    if (!admin) return 'files'
    try {
      const t = sessionStorage.getItem('haven-server-tab')
      return SERVER_TABS.some(([id]) => id === t) ? (t as ServerTab) : 'connect'
    } catch {
      return 'connect'
    }
  })
  const pick = (id: ServerTab) => {
    setTab(id)
    try {
      sessionStorage.setItem('haven-server-tab', id)
    } catch {
      /* storage blocked */
    }
  }
  const [filesPath, setFilesPath] = useState<string | undefined>()
  const current = SERVER_TABS.find(([id]) => id === tab)!
  return (
    <div className="space-y-6">
      <div className="flex flex-wrap items-end justify-between gap-3">
        <div>
          <h1 className="text-3xl font-semibold tracking-tight sm:text-4xl">{admin ? 'Server' : 'Files'}</h1>
          <p className="mt-2 text-sm text-ink-2">{current[2]}.</p>
        </div>
        <div className={cx("well inline-flex max-w-full gap-0.5 overflow-x-auto p-0.5", !admin && "hidden")} role="tablist" aria-label="Server sections">
          {tabsShown.map(([id, label]) => (
            <button key={id} role="tab" aria-selected={tab === id} onClick={() => pick(id)} className={cx('key key-sm shrink-0 gap-2 border-transparent', tab !== id && 'key-flat')}>
              <span className={cx('h-1.5 w-1.5 rounded-full', tab === id ? 'bg-accent' : 'bg-unlit')} />
              {label}
            </button>
          ))}
        </div>
      </div>

      {tab === 'connect' && (
        <Sections>
          <RemotePanel />
          <LocalPanel />
        </Sections>
      )}
      {tab === 'network' && (
        <Sections>
          <NetworkPanel />
        </Sections>
      )}
      {tab === 'power' && (
        <Sections>
          <PowerPanel />
        </Sections>
      )}
      {tab === 'insights' && <InsightsPanel />}
      {tab === 'files' && <FilesPanel initialPath={filesPath} admin={admin} />}
      {tab === 'cleanup' && <CleanupPanel onOpenPath={(p) => { setFilesPath(p); pick('files') }} />}
      {tab === 'health' && (
        <div className="space-y-5">
          <HealthPanel />
          <AccessLogPanel />
        </div>
      )}
    </div>
  )
}

function NameTest() {
  const [d, setD] = useState<Diagnosis | null>(null)
  const [running, setRunning] = useState(false)
  const [msg, setMsg] = useState('')
  const [error, setError] = useState('')
  const run = async () => {
    setRunning(true)
    setError('')
    try {
      setD(await api.testName())
    } catch (e) {
      setError((e as Error).message)
    } finally {
      setRunning(false)
    }
  }
  const fix = async () => {
    setError('')
    try {
      const r = await api.lanOnly()
      setMsg(`Done. The name is now announced only on ${r.interfaces.join(', ')}. Run the test again.`)
      setD(null)
    } catch (e) {
      setError((e as Error).message)
    }
  }
  return (
    <div className="space-y-3 border-t border-rule-2 pt-4">
      <div className="flex flex-wrap items-center gap-2">
        <button className="key key-sm" onClick={run} disabled={running} data-loading={running}>
          {running ? <Spinner size={13} /> : null} Test the local address
        </button>
        <button className="key key-sm" onClick={fix} title="Stops the name pointing at Docker or VPN addresses">
          Announce only on my network
        </button>
      </div>
      {msg && <p className="text-sm text-ok">{msg}</p>}
      <ErrorLine>{error}</ErrorLine>
      {d && <DiagnosisView d={d} running={running} onRun={run} />}
    </div>
  )
}
