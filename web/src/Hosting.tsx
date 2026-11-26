import { useEffect, useState, type FormEvent, type ReactNode } from 'react'
import { BookOpen, Eye, EyeOff } from 'lucide-react'
import { repoUrl } from './about'
import {
  hostingApi, type HostingConnection, type HostingDiscovery, type HostingHealth, type HostingResource,
  type HostingReview, type HostingSite, type HostingState, type HostingTemplate,
} from './api'
import { ErrorLine, Led, Modal, Notice, Sections, Spinner, usePoll } from './components'
import { useHaven } from './context'

function Plate({ title, tools, children }: { title: string; tools?: ReactNode; children: ReactNode }) {
  return (
    <section className="plate min-w-0">
      <div className="plate-head gap-2">
        <h2 className="etch">{title}</h2>
        {tools}
      </div>
      {children}
    </section>
  )
}

function Field({ label, children }: { label: string; children: ReactNode }) {
  return <label className="grid min-w-0 gap-1.5 text-sm"><span>{label}</span>{children}</label>
}

function TokenField({ id, label, value, change }: { id: string; label: string; value: string; change: (value: string) => void }) {
  const [show, setShow] = useState(false)
  return (
    <div className="grid min-w-0 gap-1.5 text-sm">
      <label htmlFor={id}>{label}</label>
      <div className="relative">
        <input id={id} className="field w-full pr-10" type={show ? 'text' : 'password'} required autoComplete="off" value={value} onChange={(e) => change(e.target.value)} />
        <button type="button" className="absolute inset-y-0 right-0 flex w-10 items-center justify-center text-muted hover:text-ink" onClick={() => setShow((visible) => !visible)} aria-label={show ? 'Hide token' : 'Show token'} aria-pressed={show}>
          {show ? <EyeOff size={15} aria-hidden /> : <Eye size={15} aria-hidden />}
        </button>
      </div>
    </div>
  )
}

function PickResource({ label, value, options, change, optional }: {
  label: string; value: string; options?: HostingResource[] | null; change: (value: string) => void; optional?: boolean
}) {
  return (
    <Field label={label}>
      {options?.length ? (
        <select className="field w-full" value={value} required={!optional} onChange={(e) => change(e.target.value)}>
          <option value="">{optional ? 'Use the default' : 'Choose from Coolify'}</option>
          {options.map(({ uuid, name }) => <option key={uuid} value={uuid}>{name || uuid}</option>)}
        </select>
      ) : (
        <input className="field w-full" value={value} required={!optional} onChange={(e) => change(e.target.value)} autoCapitalize="none" spellCheck={false} />
      )}
    </Field>
  )
}

function useHostingAction() {
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const run = async <T,>(action: () => Promise<T>, done?: (value: T) => void) => {
    setBusy(true)
    setError('')
    try {
      const result = await action()
      done?.(result)
    } catch (e) { setError((e as Error).message) } finally { setBusy(false) }
  }
  return { busy, error, run }
}

function CoolifySetup({ state, refresh }: { state: HostingState; refresh: () => void }) {
  const { openJob } = useHaven()
  const [connection, setConnection] = useState<HostingConnection>({ ...state.coolify, url: state.coolify.url || 'http://127.0.0.1:8000' })
  const [token, setToken] = useState('')
  const [found, setFound] = useState<HostingDiscovery>()
  const [checks, setChecks] = useState<Awaited<ReturnType<typeof hostingApi.preflight>>>()
  const [install, setInstall] = useState(false)
  const [recover, setRecover] = useState(false)
  const [edit, setEdit] = useState(false)
  const { busy, error, run } = useHostingAction()
  const set = (key: keyof HostingConnection, value: string) => setConnection((prev) => ({ ...prev, [key]: value }))
  const discover = () => run(() => hostingApi.discover(connection, token), (result) => {
    setFound(result)
    setConnection((prev) => ({
      ...prev,
      server: prev.server || (result.servers.length === 1 ? result.servers[0].uuid : ''),
      project: prev.project || (result.projects.length === 1 ? result.projects[0].uuid : ''),
      environment: prev.environment || (result.environments?.length === 1 ? result.environments[0].uuid : ''),
      destination: prev.destination || (result.destinations?.length === 1 ? result.destinations[0].uuid : ''),
    }))
  })
  const resourcesReady = Boolean(found && found.environments !== null)
  const submit = (e: FormEvent) => {
    e.preventDefault()
    if (!resourcesReady) {
      discover()
      return
    }
    void run(() => hostingApi.connect(connection, token), () => {
      setToken('')
      setFound(undefined)
      setEdit(false)
      refresh()
    })
  }
  const closeConnection = () => {
    setToken('')
    setFound(undefined)
    setEdit(false)
  }

  return (
    <Plate title="Coolify" tools={edit
      ? <button className="key key-sm" type="button" onClick={closeConnection}>{state.coolify.url ? 'Close' : 'Back'}</button>
      : state.coolify.url && <button className="key key-sm" type="button" onClick={() => setEdit(true)}>Connection settings</button>}>
      <div className="space-y-4 p-4 sm:p-5">
        {state.coolify.url && <>
          <p className="flex min-w-0 items-center gap-2 text-sm"><Led s="ok" /> <span className="break-all">{state.coolify.url}</span></p>
          {new URL(state.coolify.url).hostname === '127.0.0.1' && (
            <details className="text-sm">
              <summary className="cursor-pointer text-ink-2">Open the private Coolify dashboard</summary>
              <p className="mt-2">From another device, forward the dashboard port over SSH before opening the dashboard link.</p>
              <code className="mt-2 block break-all text-xs">ssh -N -L {new URL(state.coolify.url).port || '80'}:127.0.0.1:{new URL(state.coolify.url).port || '80'} YOUR_SSH_USER@{window.location.hostname}</code>
            </details>
          )}
        </>}
        {edit && (
          <form className="space-y-4" onSubmit={submit}>
            {!state.coolify.url && <div className="max-w-2xl space-y-1">
              <h3 className="text-base font-semibold">Connect Coolify</h3>
              <p className="text-sm text-ink-2">Use a team token with Read, Write and Deploy permissions.</p>
            </div>}
            <Field label="Dashboard URL"><input className="field w-full" type="url" required value={connection.url} onChange={(e) => { set('url', e.target.value); setFound(undefined) }} /></Field>
            <TokenField id="coolify-api-token" label="Coolify API token" value={token} change={setToken} />
            {found && <>
              <div className="grid gap-3 sm:grid-cols-2">
                <PickResource label="Server UUID" value={connection.server} options={found.servers} change={(v) => {
                  setConnection((prev) => ({ ...prev, server: v, destination: '' }))
                  setFound((prev) => prev ? { ...prev, destinations: null } : prev)
                }} />
                <PickResource label="Project UUID" value={connection.project} options={found.projects} change={(v) => {
                  setConnection((prev) => ({ ...prev, project: v, environment: '' }))
                  setFound((prev) => prev ? { ...prev, environments: null } : prev)
                }} />
                {found.environments !== null && <PickResource label="Environment UUID" value={connection.environment} options={found.environments} change={(v) => set('environment', v)} />}
                {found.destinations !== null && <PickResource label="Destination UUID (optional)" value={connection.destination || ''} options={found.destinations} change={(v) => set('destination', v)} optional />}
              </div>
              <p className="text-sm text-muted">{resourcesReady
                ? `Coolify ${found.version} resources are ready to save.`
                : `Coolify ${found.version} responded. Choose a server and project, then reload.`}</p>
            </>}
            <div className="flex flex-wrap gap-2">
              <button className={found ? 'key' : 'key key-primary'} type="button" disabled={busy || !connection.url || !token} data-loading={busy && !resourcesReady} onClick={discover}>{found ? 'Reload resources' : 'Load resources'}</button>
              {resourcesReady && <button className="key key-primary" disabled={busy} data-loading={busy}>Save connection</button>}
            </div>
          </form>
        )}
        {!state.coolify.url && !edit && (
          <div className="space-y-5">
            <div className="max-w-2xl space-y-1">
              <p className="flex items-center gap-2 text-sm"><Led s="off" /> Coolify is not connected</p>
              <h3 className="text-lg font-semibold">Choose a Coolify setup</h3>
            </div>
            <div className="divide-y divide-rule border-y border-rule sm:grid sm:grid-cols-2 sm:divide-x sm:divide-y-0">
              <div className="flex h-full flex-col gap-3 py-4 sm:pr-5">
                <div className="space-y-1">
                  <h4 className="font-semibold">Install on this server</h4>
                  <p className="text-sm text-ink-2">Check this server, then install the supported release.</p>
                </div>
                <div className="mt-auto pt-1"><button className="key key-primary" type="button" disabled={busy} data-loading={busy} onClick={() => void run(hostingApi.preflight, setChecks)}>{checks ? 'Run checks again' : 'Run install checks'}</button></div>
              </div>
              <div className="flex h-full flex-col gap-3 py-4 sm:pl-5">
                <div className="space-y-1">
                  <h4 className="font-semibold">Connect an existing installation</h4>
                  <p className="text-sm text-ink-2">Connect with its dashboard URL and API token.</p>
                </div>
                <div className="mt-auto pt-1"><button className="key" type="button" disabled={busy} onClick={() => setEdit(true)}>Connect existing Coolify</button></div>
              </div>
            </div>
            {checks && (
              <div className="space-y-3">
                <ul className="h-80 divide-y divide-rule overflow-y-auto overscroll-contain pr-2" tabIndex={0} aria-label="Coolify installation checks">
                  {[...checks].sort((a, b) => {
                    const priority = (c: typeof a) => !c.ok && !c.warning && !c.recoverable ? 0 : c.warning || c.recoverable ? 1 : 2
                    return priority(a) - priority(b)
                  }).map((c) => {
                    const state = c.warning || c.recoverable ? 'warn' : c.ok ? 'ok' : 'bad'
                    const label = c.warning ? 'Warning' : c.recoverable ? 'Can resume' : c.ok ? 'Passed' : 'Blocked'
                    return <li key={c.name} className="flex items-start gap-2 py-2 text-sm"><span className="mt-1"><Led s={state} label={label} /></span><div><strong>{c.name}</strong><p className="text-muted">{c.info}</p></div></li>
                  })}
                </ul>
                {checks.every((c) => c.ok) && <button className="key" onClick={() => { setRecover(false); setInstall(true) }}>Review Coolify installation</button>}
                {checks.some((c) => c.recoverable) && checks.every((c) => c.ok || c.recoverable) && <button className="key" onClick={() => { setRecover(true); setInstall(true) }}>Review installation recovery</button>}
              </div>
            )}
          </div>
        )}
        <ErrorLine>{error}</ErrorLine>
      </div>
      <Modal open={install} onClose={() => !busy && setInstall(false)} title="Install Coolify" footer={
        <button className="key key-primary" disabled={busy} onClick={() => void run(() => hostingApi.setup(recover), ({ jobId }) => { setInstall(false); setEdit(true); openJob(jobId, recover ? 'Resume Coolify installation' : 'Install Coolify') })}>{recover ? 'Resume installation' : `Install Coolify ${state.version}`}</button>
      }>
        <p className="text-sm">{recover ? 'Resume the supported installer without deleting existing Coolify data.' : 'Install the supported Coolify release and configure local deployments.'}</p>
        <ErrorLine>{error}</ErrorLine>
      </Modal>
    </Plate>
  )
}

function CloudflareSetup({ state, refresh }: { state: HostingState; refresh: () => void }) {
  const [domain, setDomain] = useState(state.cloudflare.name)
  const [token, setToken] = useState('')
  const [checked, setChecked] = useState<Awaited<ReturnType<typeof hostingApi.checkCloudflare>>>()
  const [edit, setEdit] = useState(false)
  const { busy, error, run } = useHostingAction()
  const submit = (e: FormEvent) => {
    e.preventDefault()
    if (!checked) return
    void run(() => hostingApi.cloudflare(domain, token), () => { setToken(''); setChecked(undefined); setEdit(false); refresh() })
  }
  const closeConnection = () => {
    setDomain(state.cloudflare.name)
    setToken('')
    setChecked(undefined)
    setEdit(false)
  }
  return (
    <Plate title="Cloudflare" tools={edit
      ? <button className="key key-sm" type="button" onClick={closeConnection}>{state.cloudflare.name ? 'Close' : 'Back'}</button>
      : state.cloudflare.name && <button className="key key-sm" type="button" onClick={() => setEdit(true)}>Connection settings</button>}>
      <div className="space-y-4 p-4 sm:p-5">
        {state.cloudflare.name ? <p className="flex items-center gap-2 text-sm"><Led s="ok" /> <span className="break-all">{state.cloudflare.name}</span></p>
          : !edit && <div className="space-y-3">
            <p className="text-sm text-ink-2">Connect Cloudflare for Tunnel publishing or managed DNS.</p>
            <button className="key" type="button" onClick={() => setEdit(true)}>Connect Cloudflare</button>
          </div>}
        {edit && (
          <form className="space-y-3" onSubmit={submit}>
            <p className="text-sm text-ink-2">Haven checks the active zone, DNS and Tunnel access before saving. Token permissions: Tunnel Edit, Zone Read and DNS Edit.</p>
            <Field label="Root domain"><input className="field w-full" required placeholder="example.com" value={domain} onChange={(e) => { setDomain(e.target.value); setChecked(undefined) }} autoCapitalize="none" spellCheck={false} /></Field>
            <TokenField id="cloudflare-api-token" label="Cloudflare API token" value={token} change={(value) => { setToken(value); setChecked(undefined) }} />
            {checked && <p className="flex items-center gap-2 text-sm"><Led s="ok" /><span>Verified <strong>{checked.name}</strong>: active zone, DNS and Tunnel access.</span></p>}
            {checked
              ? <button className="key key-primary" disabled={busy} data-loading={busy}>Save Cloudflare connection</button>
              : <button className="key key-primary" type="button" disabled={busy || !domain || !token} data-loading={busy} onClick={() => void run(() => hostingApi.checkCloudflare(domain, token), setChecked)}>Check Cloudflare access</button>}
            <ErrorLine>{error}</ErrorLine>
          </form>
        )}
      </div>
    </Plate>
  )
}

function CoolifyLink({ dashboard }: { dashboard: string }) {
  return <a className="key key-sm" href={dashboard} target="_blank" rel="noreferrer">Open Coolify</a>
}

function HostingCatalog({ dashboard, refresh }: { dashboard: string; refresh: () => void }) {
  const { openJob } = useHaven()
  const [catalog, setCatalog] = useState<Awaited<ReturnType<typeof hostingApi.catalog>>>()
  const [query, setQuery] = useState('')
  const [upstream, setUpstream] = useState(false)
  const [selected, setSelected] = useState<HostingTemplate>()
  const [name, setName] = useState('')
  const [staticHtml, setStaticHtml] = useState('<!doctype html>\n<html lang="en">\n<meta charset="utf-8">\n<meta name="viewport" content="width=device-width">\n<title>My site</title>\n<h1>My site is online</h1>\n<p>Deployed with Haven and Coolify.</p>\n</html>')
  const { busy, error, run } = useHostingAction()
  useEffect(() => { void run(hostingApi.catalog, setCatalog) }, []) // Load the pinned catalog on entry.
  const visible = catalog?.entries.filter((entry) => (entry.guided || upstream) &&
    [entry.name, entry.description, entry.id].some((text) => text.toLowerCase().includes(query.toLowerCase().trim())))
  const submit = (e: FormEvent) => {
    e.preventDefault()
    if (!selected) return
    void run(() => hostingApi.install(selected.id, name, selected.id === 'static-html' ? staticHtml : ''), ({ jobId }) => {
      setSelected(undefined)
      refresh()
      openJob(jobId, 'Install private website')
    })
  }
  return (
    <Plate title="Sites and apps">
      <div className="space-y-3 p-4 sm:p-5">
        <div className="well p-3 text-sm">
          <p className="font-medium">Deploy modern frameworks through Coolify</p>
          <p className="mt-1 text-ink-2">Open Coolify for Git, Docker image or Compose deployments.</p>
          <div className="mt-3 flex flex-wrap gap-2">
            <CoolifyLink dashboard={dashboard} />
            <span className="tag">Git repository</span><span className="tag">Docker image</span><span className="tag">Docker Compose</span>
          </div>
        </div>
        <Field label="Search platforms"><input className="field w-full" type="search" value={query} onChange={(e) => setQuery(e.target.value)} /></Field>
        <label className="flex items-center gap-2 text-sm"><input type="checkbox" checked={upstream} onChange={(e) => setUpstream(e.target.checked)} /> Include Coolify’s upstream templates</label>
        {catalog && !catalog.complete && <p className="text-sm text-muted">The upstream catalog is unavailable. Guided templates are still available.</p>}
        {busy && !catalog && <Spinner />}
        {!selected && <ErrorLine>{error}</ErrorLine>}
      </div>
      {catalog && (
        <ul className="max-h-[32rem] divide-y divide-rule overflow-auto border-t border-rule" aria-label="Website platforms">
          {visible?.map((entry) => (
            <li key={entry.id} className="flex flex-wrap items-center justify-between gap-3 px-4 py-3 sm:px-5">
              <div className="min-w-0 flex-1 basis-44">
                <h3 className="break-words font-medium">{entry.name}</h3>
                <p className="mt-1 text-sm text-muted">{entry.description}</p>
              </div>
              {entry.guided ? <button className="key key-sm" onClick={() => { setName(''); setSelected(entry) }}>Install privately</button> : <CoolifyLink dashboard={dashboard} />}
            </li>
          ))}
          {!visible?.length && <li className="px-4 py-5 text-sm text-muted">No platforms match your search.</li>}
        </ul>
      )}
      <Modal open={!!selected} onClose={() => !busy && setSelected(undefined)} title={selected ? 'Install ' + selected.name : 'Install website'} footer={
        <button className="key key-primary" form="private-hosting-install" disabled={busy || !name.trim()} data-loading={busy}>Confirm private install</button>
      }>
        <form id="private-hosting-install" className="space-y-4" onSubmit={submit}>
          <p className="text-sm">Install privately now. Publishing remains a separate, reviewed action.</p>
          <Field label="Website name"><input className="field w-full" required maxLength={80} value={name} onChange={(e) => setName(e.target.value)} /></Field>
          {selected?.id === 'static-html' && <Field label="Homepage HTML"><textarea className="field min-h-64 w-full font-mono text-xs" required maxLength={262144} value={staticHtml} onChange={(e) => setStaticHtml(e.target.value)} spellCheck={false} /></Field>}
          <ErrorLine>{error}</ErrorLine>
        </form>
      </Modal>
    </Plate>
  )
}

function PublishDialog({ site, cloudflare, onClose, refresh }: { site: HostingSite; cloudflare: boolean; onClose: () => void; refresh: () => void }) {
  const { openJob } = useHaven()
  const existing = site.publication
  const [host, setHost] = useState(existing?.host || '')
  const [mode, setMode] = useState<HostingReview['plan']['mode']>(existing?.mode || (cloudflare ? 'tunnel' : 'direct'))
  const [dns, setDNS] = useState<HostingReview['plan']['dns']>(existing?.dns || (cloudflare ? 'cloudflare' : 'manual'))
  const [wan, setWan] = useState('')
  const [proxySafe, setProxySafe] = useState(false)
  const [review, setReview] = useState<HostingReview>()
  const { busy, error, run } = useHostingAction()
  const plan = review?.plan
  const submit = (e: FormEvent) => {
    e.preventDefault()
    if (!review) {
      void run(() => hostingApi.preview(site.id, { host, mode, dns, wan, proxySafe }), setReview)
    } else {
      void run(() => hostingApi.publish(site.id, review.id), ({ jobId }) => {
        refresh()
        onClose()
        openJob(jobId, 'Publish website')
      })
    }
  }
  return (
    <Modal open onClose={() => !busy && onClose()} title={plan ? 'Confirm public access' : 'Plan public access'} footer={
      <>
        {plan && <button className="key" disabled={busy} onClick={() => setReview(undefined)}>Back</button>}
        <button form="hosting-publish" className="key key-primary" disabled={busy} data-loading={busy}>{plan ? 'Confirm publication' : 'Review publication'}</button>
      </>
    }>
      <form id="hosting-publish" className="space-y-4 text-sm" onSubmit={submit}>
        {plan ? (
          <>
            <p>Publish <strong>{site.name}</strong> at <strong className="break-all">https://{plan.host}</strong> using {plan.mode === 'tunnel' ? 'Cloudflare Tunnel' : 'the Coolify proxy'}.</p>
            <div className="well space-y-2 p-3">
              <p>{plan.mode === 'tunnel' ? 'Creates one exact Tunnel route and proxied DNS record.' :
                (plan.dns === 'cloudflare' ? 'Creates a DNS-only A record at ' : 'Create an A record pointing to ') + plan.address + '.'}</p>
              <p>This changes the site’s canonical URL. Back up established content first.</p>
            </div>
          </>
        ) : (
          <>
            <Field label="Public hostname"><input className="field w-full" required placeholder="blog.example.com" value={host} readOnly={!!existing} onChange={(e) => setHost(e.target.value)} autoCapitalize="none" spellCheck={false} /></Field>
            <Field label="Access method">
              <select className="field w-full" disabled={!!existing} value={mode} onChange={(e) => {
                const value = e.target.value as typeof mode
                setMode(value)
                if (value === 'tunnel') setDNS('cloudflare')
              }}>
                <option value="tunnel" disabled={!cloudflare}>Cloudflare Tunnel</option>
                <option value="direct">Direct through Coolify</option>
              </select>
            </Field>
            {!cloudflare && <p className="text-muted">Connect Cloudflare to use a tunnel.</p>}
            {mode === 'direct' && (
              <>
                <Field label="DNS provider">
                  <select className="field w-full" disabled={!!existing} value={dns} onChange={(e) => setDNS(e.target.value as typeof dns)}>
                    <option value="manual">Manual DNS</option>
                    <option value="cloudflare" disabled={!cloudflare}>Cloudflare-managed DNS</option>
                  </select>
                </Field>
                {!existing && <>
                  <Field label="Router WAN IPv4 (optional)"><input className="field w-full" value={wan} onChange={(e) => setWan(e.target.value)} inputMode="decimal" /></Field>
                  <Notice label="Router and proxy">
                    TCP 80 and 443 expose every route on Coolify’s proxy. Keep dashboards, private apps and databases off those ports.
                  </Notice>
                  <label className="flex items-start gap-2"><input className="mt-1" type="checkbox" required checked={proxySafe} onChange={(e) => setProxySafe(e.target.checked)} /> I reviewed the proxy routes and only intended public websites use these ports.</label>
                </>}
              </>
            )}
          </>
        )}
        <ErrorLine>{error}</ErrorLine>
      </form>
    </Modal>
  )
}

function SiteRow({ site, state, refresh }: { site: HostingSite; state: HostingState; refresh: () => void }) {
  const { openJob } = useHaven()
  const [health, setHealth] = useState<HostingHealth>()
  const [logs, setLogs] = useState<string>()
  const [publish, setPublish] = useState(false)
  const [confirm, setConfirm] = useState<'unpublish' | 'external'>()
  const { busy, error, run } = useHostingAction()
  const p = site.publication
  useEffect(() => { setHealth(undefined) }, [p?.phase, p?.host])
  const stateLabel = site.deployment === 'failed' ? 'Deployment failed' : site.deployment === 'deploying' ? 'Deploying' : site.deployment === 'queued' ? 'Queued' : site.phase === 'installing' ? 'Install pending' : p?.phase === 'removing' ? 'Removal pending' :
    p?.phase === 'publishing' ? 'Publication pending' : health?.verified ? 'HTTPS verified' : p ? 'Public route configured' : 'Private installation'
  const healthLabels = [['Deployment', health?.deployment], ['Local container', health?.local], ['Connector', health?.connector],
    ['Route', health?.route], ['DNS', health?.dns], ['HTTPS', health?.https], ['Certificate', health?.certificate]]
  const privateURL = 'http://127.0.0.1:' + site.privatePort
  const localHost = window.location.hostname
  return (
    <li className="space-y-3 p-4 sm:p-5">
      <div className="flex flex-wrap items-start justify-between gap-2">
        <div className="min-w-0">
          <h3 className="break-words text-lg font-semibold">{site.name}</h3>
          {p && <a href={'https://' + p.host} className="break-all text-sm text-ink-2 underline underline-offset-2" target="_blank" rel="noreferrer">https://{p.host}</a>}
        </div>
        <span className="tag"><Led s={health?.verified ? 'ok' : p || site.phase === 'installing' ? 'warn' : 'off'} /> {stateLabel}</span>
      </div>
      {!p && site.phase === 'installed' && (
        <details className="text-sm">
          <summary className="cursor-pointer text-ink-2">Open privately with SSH</summary>
          <div className="well mt-2 space-y-2 p-3">
            <p>Run from your computer, replacing the SSH user and server address:</p>
            <code className="block break-all text-xs">ssh -N -L {site.privatePort}:127.0.0.1:{site.privatePort} YOUR_SSH_USER@{localHost}</code>
            <a className="underline underline-offset-2" href={privateURL} target="_blank" rel="noreferrer">{privateURL}</a>
          </div>
        </details>
      )}
      {p?.mode === 'direct' && p.phase === 'configured' && (
        <div className="well space-y-2 p-3 text-sm">
          {p.dns === 'manual' && <p>Create a DNS-only A record for <strong className="break-all">{p.host}</strong> pointing to <strong>{p.address}</strong>. Remove that record yourself if you unpublish.</p>}
          <p>Forward TCP 80 and 443 to Coolify, then test from another network. Check Coolify’s proxy logs if HTTPS fails.</p>
          {p.external ? <p className="text-ink-2">External access checked by an admin.</p> : <button className="key key-sm" onClick={() => setConfirm('external')}>Confirm external access</button>}
          {p.ipError && <ErrorLine>{p.ipError}</ErrorLine>}
        </div>
      )}
      {health && (
        <dl className="well divide-y divide-rule px-3 text-sm">
          {healthLabels.map(([label, value]) => value && <div key={label} className="grid gap-1 py-2 sm:grid-cols-[7rem_minmax(0,1fr)]"><dt className="font-medium">{label}</dt><dd className="break-words text-ink-2">{value}</dd></div>)}
        </dl>
      )}
      {site.lastError && <ErrorLine>{site.lastError}</ErrorLine>}
      <div className="flex flex-wrap gap-2">
        {site.phase === 'installing' && state.enabled && <button className="key key-sm" disabled={busy} onClick={() => void run(() => hostingApi.retry(site.id), ({ jobId }) => { refresh(); openJob(jobId, 'Retry private install') })}>Retry install</button>}
        {site.serviceId && state.enabled && <>
          <button className="key key-sm" disabled={busy} onClick={() => void run(() => hostingApi.health(site.id), setHealth)}>Check website</button>
          <button className="key key-sm" disabled={busy} onClick={() => void run(() => hostingApi.logs(site.id), ({ logs }) => setLogs(logs))}>View logs</button>
          <a className="key key-sm" href={'/api/hosting/sites/' + encodeURIComponent(site.id) + '/recovery'} download>Download recovery metadata</a>
        </>}
        {state.enabled && site.phase === 'installed' && (!p || p.phase === 'publishing') && <button className="key key-sm key-primary" onClick={() => setPublish(true)}>{p ? 'Review and retry' : 'Publish website'}</button>}
        {p && <button className="key key-sm" onClick={() => setConfirm('unpublish')}>{p.phase === 'removing' ? 'Retry unpublish' : 'Unpublish'}</button>}
      </div>
      {!confirm && <ErrorLine>{error}</ErrorLine>}
      {publish && <PublishDialog site={site} cloudflare={!!state.cloudflare.name} onClose={() => setPublish(false)} refresh={refresh} />}
      <Modal open={logs !== undefined} onClose={() => setLogs(undefined)} title={site.name + ' logs'} wide>
        <pre className="well max-h-96 overflow-auto whitespace-pre-wrap break-words p-3 text-xs">{logs || 'Coolify returned no log lines.'}</pre>
      </Modal>
      <Modal open={!!confirm} onClose={() => !busy && setConfirm(undefined)} title={confirm === 'external' ? 'Confirm external access' : 'Unpublish website'} footer={
        <button className={confirm === 'unpublish' ? 'key key-danger' : 'key key-primary'} disabled={busy} onClick={() => {
          if (confirm === 'external') void run(() => hostingApi.external(site.id), () => { setConfirm(undefined); refresh() })
          if (confirm === 'unpublish') void run(() => hostingApi.unpublish(site.id), ({ jobId }) => { setConfirm(undefined); refresh(); openJob(jobId, 'Unpublish website') })
        }}>{confirm === 'external' ? 'I reached this site externally' : 'Confirm unpublish'}</button>
      }>
        <p className="break-words text-sm">{confirm === 'external' ? 'Confirm that ' + p?.host + ' opened over HTTPS from a separate internet connection with Tailscale off.' :
          'Remove public access for ' + p?.host + '. Coolify keeps the website and its data.'}</p>
        <ErrorLine>{error}</ErrorLine>
      </Modal>
    </li>
  )
}

export default function HostingPage() {
  const { repo } = useHaven()
  const { data: state, error, refresh } = usePoll(hostingApi.status, 5000)
  const guide = repoUrl(repo, '/blob/main/docs/WEBSITE_HOSTING_GUIDE.md') || 'https://github.com/NaveenKharwar/Haven/blob/main/docs/WEBSITE_HOSTING_GUIDE.md'
  return (
    <div className="space-y-6">
      <header className="flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between">
        <div>
          <div className="flex flex-wrap items-center gap-2">
            <h1 className="text-2xl font-semibold tracking-tight">Website and App Hosting</h1>
            <span className="tag tag-warn">Experimental</span>
          </div>
          <p className="mt-1 max-w-3xl text-sm text-muted">Install private sites with Coolify and publish them when ready.</p>
        </div>
        <a className="key shrink-0 gap-2" href={guide} target="_blank" rel="noreferrer noopener"><BookOpen size={15} aria-hidden /> Beginner guide</a>
      </header>
      <ErrorLine>{error}</ErrorLine>
      {!state && !error && <Spinner />}
      {state && (
        <>
          {!state.enabled && <Notice label="Hosting is off">Run <code className="break-all">sudo haven experimental</code> on the server and enable Website and App Hosting.</Notice>}
          {state.enabled && (
            <Sections>
              <CoolifySetup state={state} refresh={refresh} />
              <CloudflareSetup state={state} refresh={refresh} />
            </Sections>
          )}
          {state.coolify.url && (
            <Sections>
              <Plate title="Your websites" tools={<CoolifyLink dashboard={state.coolify.url} />}>
                {!state.sites.length && <p className="p-4 text-sm text-muted sm:p-5">Choose a platform to install your first private website.</p>}
                <ul className="divide-y divide-rule">{state.sites.map((site) => <SiteRow key={site.id} site={site} state={state} refresh={refresh} />)}</ul>
                {!!state.sites.length && <div className="border-t border-rule p-4 text-sm text-muted sm:p-5">Manage database and storage backups in Coolify. Restore both privately before publishing again.</div>}
              </Plate>
              {state.enabled && <HostingCatalog dashboard={state.coolify.url} refresh={refresh} />}
            </Sections>
          )}
        </>
      )}
    </div>
  )
}
