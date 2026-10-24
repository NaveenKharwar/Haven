import { useCallback, useEffect, useState } from 'react'
import { ArrowUpRight, CircleArrowUp, Play, RotateCw, ScrollText, Square, Trash2, X } from 'lucide-react'
import { api, appUrl, atLeast, containerLed, formatBytes, ledState, statusWord, type Container, type InstalledApp, type Setting } from './api'
import { AppIcon, Drawer, Led, LogBox, Modal, Notice, Spinner, cx, toast } from './components'
import { useHaven } from './context'
import { UsageCells, cpuText, diskText, fmtCpu, memText } from './usage'
import { SettingField } from './Store'
import { DiagnosisView } from './diagnosis'
import type { Diagnosis } from './api'

function useAppAction() {
  const { openJob, refreshApps } = useHaven()
  return async (app: InstalledApp, action: 'start' | 'stop' | 'restart' | 'update') => {
    try {
      const job = await api.action(app.id, action)
      openJob(job.id, app.name)
      refreshApps()
    } catch (e) {
      toast.error((e as Error).message)
    }
  }
}

export default function AppsPage() {
  const { apps, appsReady, containers, dockerError, openApp, navigate } = useHaven()
  const [logsFor, setLogsFor] = useState<Container | null>(null)
  const running = apps.filter((a) => a.status === 'running').length

  return (
    <div className="space-y-6">
      <div className="flex flex-wrap items-end justify-between gap-3">
        <div>
          <h1 className="text-3xl font-semibold tracking-tight sm:text-4xl">Apps</h1>
          <p className="mt-2 text-sm text-ink-2">
            {!appsReady ? 'Reading your apps…' : apps.length === 0 ? 'Nothing installed yet.' : `${running} of ${apps.length} running. Select an app for logs, settings and updates.`}
          </p>
        </div>
        <button className="key" onClick={() => navigate('catalog')}>
          Browse catalog
        </button>
      </div>

      {dockerError && (
        <p role="alert" className="plate flex items-center gap-3 border-bad px-4 py-3 text-sm">
          <Led s="bad" /> Docker isn't responding: {dockerError}
        </p>
      )}

      {!appsReady && <div className="plate h-48 bg-well" aria-hidden />}

      {apps.length > 0 && (
        <section className="plate overflow-hidden" aria-label="Installed apps">
          <div className="plate-head hidden sm:flex">
            <span className="etch w-28">Status</span>
            <span className="etch flex-1">App</span>
            <span className="etch hidden w-16 text-right md:block">CPU</span>
            <span className="etch hidden w-20 text-right md:block">Memory</span>
            <span className="etch hidden w-20 text-right md:block">Disk</span>
            <span className="etch w-28 text-right">Controls</span>
          </div>
          <ul className="divide-y divide-rule-2">
            {apps.map((a) => (
              <AppRow key={a.id} app={a} onOpen={() => openApp(a.id)} />
            ))}
          </ul>
        </section>
      )}

      {containers.length > 0 && (
        <section className="plate overflow-hidden" aria-labelledby="other-h">
          <div className="plate-head">
            <h2 id="other-h" className="etch">
              Other containers
            </h2>
            <span className="hidden text-xs text-muted sm:block">Not installed by Haven. Basic controls only.</span>
          </div>
          <ul className="divide-y divide-rule-2">
            {containers.map((c) => (
              <ContainerRow key={c.id} c={c} onLogs={() => setLogsFor(c)} />
            ))}
          </ul>
        </section>
      )}

      <Modal open={!!logsFor} onClose={() => setLogsFor(null)} wide label="Logs" title={logsFor?.name}>
        {logsFor && <LogsView load={() => api.containerLogs(logsFor.id).then((r) => r.logs)} />}
      </Modal>
    </div>
  )
}

function AppRow({ app, onOpen }: { app: InstalledApp; onOpen: () => void }) {
  const run = useAppAction()
  const { role } = useHaven()
  const manage = atLeast(role, 'manager')
  const canRestart = atLeast(role, 'member')
  const url = app.status !== 'stopped' ? appUrl(app.web, app.webPort) : null
  const led = ledState(app.status, app.busy)
  const busy = !!app.busy
  const up = app.containers.filter((c) => c.state === 'running').length
  return (
    <li className="flex items-center gap-3 px-3 py-3 hover:bg-raised sm:px-4">
      <span className="flex shrink-0 items-center gap-2 sm:w-28">
        <Led s={led} label={statusWord(app.status, app.busy)} />
        <span className={cx('etch hidden sm:inline', led === 'bad' && 'text-bad', led === 'busy' && 'text-accent')}>{statusWord(app.status, app.busy)}</span>
      </span>
      <button onClick={onOpen} className="flex min-w-0 flex-1 items-center gap-3 rounded-[2px] text-left">
        <AppIcon src={app.icon} name={app.name} size={34} />
        <span className="min-w-0">
          <span className="flex items-center gap-2">
            <span className="truncate font-semibold">{app.name}</span>
            {app.updateAvailable && <span className="tag tag-accent">Update</span>}
          </span>
          <span className="block truncate font-mono text-xs text-muted">
            {app.id} · {up}/{app.containers.length} up{app.webPort ? ` · :${app.webPort}` : ''}
            {app.version && !app.custom ? ` · ${app.version}` : ''}
          </span>
        </span>
      </button>
      <span className="hidden w-16 shrink-0 text-right font-mono text-xs md:block">{cpuText(app)}</span>
      <span className="hidden w-20 shrink-0 text-right font-mono text-xs md:block">{memText(app)}</span>
      <span className="hidden w-20 shrink-0 text-right font-mono text-xs text-ink-2 md:block" title="Images plus data folder">
        {diskText(app)}
      </span>
      <span className="flex shrink-0 items-center justify-end gap-1 sm:w-28">
        {url && (
          <a href={url} target="_blank" rel="noreferrer" className="key key-sm key-icon" aria-label={`Open ${app.name}`} title="Open">
            <ArrowUpRight size={14} />
          </a>
        )}
        {app.status === 'stopped' ? (
          manage && (
            <button className="key key-sm key-icon" disabled={busy} onClick={() => run(app, 'start')} aria-label={`Start ${app.name}`} title="Start">
              <Play size={14} />
            </button>
          )
        ) : (
          <>
            {canRestart && (
              <button className="key key-sm key-icon hidden sm:inline-flex" disabled={busy} onClick={() => run(app, 'restart')} aria-label={`Restart ${app.name}`} title="Restart">
                <RotateCw size={14} />
              </button>
            )}
            {manage && (
              <button className="key key-sm key-icon hidden sm:inline-flex" disabled={busy} onClick={() => run(app, 'stop')} aria-label={`Stop ${app.name}`} title="Stop">
                <Square size={13} />
              </button>
            )}
          </>
        )}
      </span>
    </li>
  )
}

function ContainerRow({ c, onLogs }: { c: Container; onLogs: () => void }) {
  const [busy, setBusy] = useState(false)
  const { refreshApps } = useHaven()
  const act = async (a: 'start' | 'stop' | 'restart') => {
    setBusy(true)
    try {
      await api.containerAction(c.id, a)
      refreshApps()
    } catch (e) {
      toast.error((e as Error).message)
    } finally {
      setBusy(false)
    }
  }
  const running = c.state === 'running'
  return (
    <li className="flex items-center gap-3 px-3 py-3 sm:px-4">
      <Led s={containerLed(c)} label={c.status} />
      <span className="min-w-0 flex-1">
        <span className="block truncate font-mono text-sm">{c.name}</span>
        <span className="block truncate font-mono text-xs text-muted">
          {c.image} · {c.status}
          {c.mem ? ` · ${formatBytes(c.mem)} · ${fmtCpu(c.cpu ?? 0)} cpu` : ''}
        </span>
      </span>
      <span className="flex shrink-0 gap-1">
        {busy ? (
          <Spinner />
        ) : running ? (
          <>
            <button className="key key-sm key-icon" onClick={() => act('restart')} aria-label={`Restart ${c.name}`} title="Restart">
              <RotateCw size={14} />
            </button>
            <button className="key key-sm key-icon" onClick={() => act('stop')} aria-label={`Stop ${c.name}`} title="Stop">
              <Square size={13} />
            </button>
          </>
        ) : (
          <button className="key key-sm key-icon" onClick={() => act('start')} aria-label={`Start ${c.name}`} title="Start">
            <Play size={14} />
          </button>
        )}
        <button className="key key-sm key-icon" onClick={onLogs} aria-label={`Logs for ${c.name}`} title="Logs">
          <ScrollText size={14} />
        </button>
      </span>
    </li>
  )
}

function LogsView({ load }: { load: () => Promise<string> }) {
  const [lines, setLines] = useState<string[] | null>(null)
  const [error, setError] = useState('')
  const refresh = useCallback(async () => {
    try {
      const text = await load()
      setLines(text.split('\n').filter(Boolean))
      setError('')
    } catch (e) {
      setError((e as Error).message)
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])
  useEffect(() => {
    refresh()
    const t = setInterval(refresh, 4000)
    return () => clearInterval(t)
  }, [refresh])
  if (error) return <p className="text-sm text-bad">{error}</p>
  if (!lines) return <Spinner />
  return <LogBox lines={lines} className="h-[60vh]" />
}

type Tab = 'overview' | 'logs' | 'diagnose' | 'settings' | 'compose'

export function AppDrawer({ id, onClose }: { id: string | null; onClose: () => void }) {
  const { apps, catalog, role } = useHaven()
  const manage = atLeast(role, 'manager')
  const canRestart = atLeast(role, 'member')
  const run = useAppAction()
  const app = apps.find((a) => a.id === id)
  const [tab, setTab] = useState<Tab>('overview')
  const [detail, setDetail] = useState<{ compose: string; env: Record<string, string>; settings?: Setting[] } | null>(null)
  const [confirmRemove, setConfirmRemove] = useState(false)

  useEffect(() => {
    setTab('overview')
    setDetail(null)
    if (id && manage) api.app(id).then(setDetail).catch(() => {})
  }, [id, manage])

  if (!id) return null
  if (!app) {
    return (
      <Drawer open onClose={onClose} label="App">
        <div className="flex h-full items-center justify-center">
          <Spinner />
        </div>
      </Drawer>
    )
  }

  const url = app.status !== 'stopped' ? appUrl(app.web, app.webPort) : null
  const notes = app.notes || catalog.find((c) => c.id === app.catalogId)?.notes
  const led = ledState(app.status, app.busy)
  const busy = !!app.busy
  const tabs: { id: Tab; label: string; show: boolean }[] = [
    { id: 'overview', label: 'Overview', show: true },
    { id: 'logs', label: 'Logs', show: manage },
    { id: 'settings', label: 'Settings', show: manage && !app.custom && !!detail?.settings?.length },
    { id: 'diagnose', label: 'Diagnose', show: manage },
    { id: 'compose', label: 'Compose', show: manage },
  ]

  return (
    <Drawer open onClose={onClose} label={app.name}>
      <div className="plate-head shrink-0 border-rule">
        <span className="flex items-center gap-2">
          <Led s={led} />
          <span className={cx('etch', led === 'bad' && 'text-bad', led === 'busy' && 'text-accent')}>{statusWord(app.status, app.busy)}</span>
        </span>
        <button className="key key-flat key-sm key-icon" onClick={onClose} aria-label="Close">
          <X size={16} />
        </button>
      </div>

      <div className="shrink-0 border-b border-rule px-5 pb-5 pt-5">
        <div className="flex items-center gap-4">
          <AppIcon src={app.icon} name={app.name} size={56} />
          <div className="min-w-0">
            <h2 className="truncate text-2xl font-semibold tracking-tight">{app.name}</h2>
            <p className="font-mono text-xs text-muted">
              {app.custom ? 'custom compose app' : `version ${app.version}`} · {app.id}
            </p>
          </div>
        </div>
        <div className="mt-5 flex flex-wrap gap-2">
          {url && (
            <a className="key key-primary" href={url} target="_blank" rel="noreferrer">
              Open {app.name} <ArrowUpRight size={14} />
            </a>
          )}
          {app.status === 'stopped' ? (
            manage && (
              <button className="key" disabled={busy} onClick={() => run(app, 'start')}>
                <Play size={14} /> Start
              </button>
            )
          ) : (
            <>
              {canRestart && (
                <button className="key" disabled={busy} onClick={() => run(app, 'restart')}>
                  <RotateCw size={14} /> Restart
                </button>
              )}
              {manage && (
                <button className="key" disabled={busy} onClick={() => run(app, 'stop')}>
                  <Square size={13} /> Stop
                </button>
              )}
            </>
          )}
          {manage && (
            <>
              <button className={cx('key', app.updateAvailable && 'text-accent')} disabled={busy} onClick={() => run(app, 'update')}>
                <CircleArrowUp size={14} /> {app.updateAvailable ? `Update to ${app.updateAvailable}` : 'Pull latest'}
              </button>
              <button className="key key-danger sm:ml-auto" disabled={busy} onClick={() => setConfirmRemove(true)}>
                <Trash2 size={14} /> Uninstall
              </button>
            </>
          )}
        </div>
      </div>

      <div className="shrink-0 px-5 pt-4">
        <div className="well inline-flex max-w-full gap-0.5 overflow-x-auto p-0.5" role="tablist">
          {tabs
            .filter((t) => t.show)
            .map((t) => (
              <button key={t.id} role="tab" aria-selected={tab === t.id} onClick={() => setTab(t.id)} className={cx('key key-sm gap-2 border-transparent', tab !== t.id && 'key-flat')}>
                <span className={cx('h-1.5 w-1.5 rounded-full', tab === t.id ? 'bg-accent' : 'bg-unlit')} />
                {t.label}
              </button>
            ))}
        </div>
      </div>

      <div className="min-h-0 flex-1 overflow-y-auto p-5">
        {tab === 'overview' && (
          <div className="space-y-5">
            {(app.status === 'error' || app.status === 'partial') && (
              <div className="plate flex flex-wrap items-center gap-3 border-bad px-4 py-3">
                <Led s="bad" />
                <span className="min-w-0 flex-1 text-sm">Something isn't working. Haven can look for the cause.</span>
                <button className="key key-sm key-primary" onClick={() => setTab('diagnose')}>
                  Diagnose
                </button>
              </div>
            )}
            {notes && (
              <Notice label="Notice">{notes}</Notice>
            )}
            <UsageCells app={app} />
            <section className="plate overflow-hidden">
              <div className="plate-head">
                <span className="etch">Containers</span>
                <span className="etch text-[0.625rem]">{app.containers.length}</span>
              </div>
              {app.containers.length === 0 ? (
                <p className="p-4 text-sm text-muted">No containers yet. Press Start to create them.</p>
              ) : (
                <ul className="divide-y divide-rule-2">
                  {app.containers.map((c) => (
                    <li key={c.id} className="flex items-center gap-3 px-4 py-3">
                      <Led s={containerLed(c)} />
                      <span className="min-w-0 flex-1">
                        <span className="block truncate text-sm font-medium">{c.service || c.name}</span>
                        <span className="block truncate font-mono text-xs text-muted">{c.image}</span>
                      </span>
                      <span className="shrink-0 text-right font-mono text-xs text-ink-2">
                        {c.mem ? `${formatBytes(c.mem)} · ${fmtCpu(c.cpu ?? 0)} · ` : ''}
                        {c.status}
                      </span>
                    </li>
                  ))}
                </ul>
              )}
            </section>
            <dl className="plate grid grid-cols-1 gap-px overflow-hidden bg-rule-2 sm:grid-cols-2">
              {[
                ['Address', url ? url.replace(/^https?:\/\//, '') : '—'],
                ['Installed', new Date(app.installedAt).toLocaleDateString()],
                // Data lives in <haven>/appdata/<id>; the compose files sit beside it in <haven>/apps/<id>.
                ['Config folder', detail?.env.APP_DATA ? detail.env.APP_DATA.replace(/\/appdata\/([^/]+)$/, '/apps/$1') : '—'],
                ['Data folder', detail?.env.APP_DATA || '—'],
              ].map(([k, v]) => (
                <div key={k} className="min-w-0 bg-plate px-4 py-3">
                  <dt className="etch">{k}</dt>
                  <dd className="mt-1 truncate font-mono text-xs text-ink">{v}</dd>
                </div>
              ))}
            </dl>
          </div>
        )}
        {tab === 'logs' && <LogsView key={app.id} load={() => api.logs(app.id).then((r) => r.logs)} />}
        {tab === 'diagnose' && <DiagnoseTab key={app.id} id={app.id} />}
        {tab === 'settings' && detail?.settings && <SettingsTab app={app} settings={detail.settings} env={detail.env} onSaved={() => api.app(app.id).then(setDetail)} />}
        {tab === 'compose' && (
          <div className="space-y-3">
            <p className="text-sm text-ink-2">
              A standard Compose file. You can always manage this app by hand with <code className="font-mono text-xs">docker compose</code> in its config folder.
            </p>
            <pre className="well tape overflow-auto p-3">{detail?.compose ?? 'Loading…'}</pre>
          </div>
        )}
      </div>

      <UninstallDialog app={confirmRemove ? app : null} onClose={() => setConfirmRemove(false)} onDone={onClose} />
    </Drawer>
  )
}

function SettingsTab({ app, settings, env, onSaved }: { app: InstalledApp; settings: Setting[]; env: Record<string, string>; onSaved: () => void }) {
  const { openJob, refreshApps } = useHaven()
  const initial = () => Object.fromEntries(settings.map((s) => [s.key, env[s.key] ?? '']))
  const [values, setValues] = useState<Record<string, string>>(initial)
  const [busy, setBusy] = useState(false)
  const changed = settings.some((s) => (values[s.key] ?? '') !== (env[s.key] ?? ''))
  const save = async () => {
    setBusy(true)
    try {
      const diff = Object.fromEntries(settings.filter((s) => values[s.key] !== env[s.key]).map((s) => [s.key, values[s.key]]))
      const job = await api.reconfigure(app.id, diff)
      openJob(job.id, app.name)
      refreshApps()
      onSaved()
    } catch (e) {
      toast.error((e as Error).message)
    } finally {
      setBusy(false)
    }
  }
  return (
    <div className="space-y-5">
      <p className="text-sm text-ink-2">Saving restarts the app. If it doesn't come back healthy, Haven puts the old settings back.</p>
      <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
        {settings.map((s) => (
          <SettingField key={s.key} setting={s} value={values[s.key] ?? ''} onChange={(v) => setValues((x) => ({ ...x, [s.key]: v }))} />
        ))}
      </div>
      <div className="flex justify-end gap-2">
        <button className="key" disabled={!changed || busy} onClick={() => setValues(initial())}>
          Reset
        </button>
        <button className="key key-primary" disabled={!changed || busy} data-loading={busy} onClick={save}>
          Save and restart
        </button>
      </div>
    </div>
  )
}

function UninstallDialog({ app, onClose, onDone }: { app: InstalledApp | null; onClose: () => void; onDone: () => void }) {
  const { openJob, refreshApps } = useHaven()
  const [removeData, setRemoveData] = useState(false)
  const [typed, setTyped] = useState('')
  useEffect(() => {
    setRemoveData(false)
    setTyped('')
  }, [app?.id])
  if (!app) return null
  const go = async () => {
    try {
      const job = await api.uninstall(app.id, removeData)
      onClose()
      onDone()
      openJob(job.id, app.name)
      refreshApps()
    } catch (e) {
      toast.error((e as Error).message)
    }
  }
  return (
    <Modal
      open
      onClose={onClose}
      label="Uninstall"
      title={app.name}
      footer={
        <>
          <button className="key" onClick={onClose}>
            Keep it
          </button>
          <button className="key key-danger" onClick={go} disabled={removeData && typed !== app.id}>
            <Trash2 size={14} /> {removeData ? 'Uninstall and delete data' : 'Uninstall'}
          </button>
        </>
      }
    >
      <div className="space-y-4 text-sm">
        <p className="text-ink-2">Stops {app.name} and removes its containers.</p>
        <label className="well flex cursor-pointer items-start gap-3 p-3">
          <input type="checkbox" className="mt-1 accent-[var(--bad)]" checked={removeData} onChange={(e) => setRemoveData(e.target.checked)} />
          <span>
            <span className="font-medium">Also delete its data</span>
            <span className="block text-ink-2">Settings, databases and files in its data folder. Media folders you picked are never touched.</span>
          </span>
        </label>
        {removeData ? (
          <div className="space-y-2">
            <label htmlFor="confirm-id" className="etch block">
              Type {app.id} to confirm
            </label>
            <input id="confirm-id" className="field font-mono" value={typed} onChange={(e) => setTyped(e.target.value)} autoComplete="off" />
          </div>
        ) : (
          <p className="text-muted">Data stays on disk. Reinstall with the same app ID to pick it up again.</p>
        )}
      </div>
    </Modal>
  )
}

function DiagnoseTab({ id }: { id: string }) {
  const [d, setD] = useState<Diagnosis | null>(null)
  const [running, setRunning] = useState(false)
  const [error, setError] = useState('')
  const run = useCallback(async () => {
    setRunning(true)
    setError('')
    try {
      setD(await api.diagnoseApp(id))
    } catch (e) {
      setError((e as Error).message)
    } finally {
      setRunning(false)
    }
  }, [id])
  useEffect(() => {
    run()
  }, [run])
  if (error) return <p className="text-sm text-bad">{error}</p>
  return <DiagnosisView d={running && !d ? null : d} running={running} onRun={run} />
}
