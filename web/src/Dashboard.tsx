import { ArrowUpRight, Plus } from 'lucide-react'
import type { ReactNode } from 'react'
import { api, appUrl, atLeast, formatBytes, formatUptime, ledState, splitBytes, statusWord, timeOf, type InstalledApp } from './api'
import { AppIcon, Ladder, Led, Readout, Trace, cx, usePoll } from './components'
import { useHaven } from './context'
import { UsageRanking, UsageStrip } from './usage'
import { WidgetBoard, useWidgets } from './widgets'

function Cell({ label, spec, children, foot }: { label: string; spec?: ReactNode; children: ReactNode; foot?: ReactNode }) {
  return (
    <div className="flex min-w-0 flex-col gap-3 bg-plate p-4 sm:p-5">
      <div className="flex items-center justify-between gap-2">
        <span className="etch">{label}</span>
        {spec && <span className="etch hidden min-w-0 truncate text-[0.625rem] sm:inline">{spec}</span>}
      </div>
      {children}
      {foot && <div className="mt-auto truncate font-mono text-[0.6875rem] text-muted">{foot}</div>}
    </div>
  )
}

export function AppModule({ app, onOpen }: { app: InstalledApp; onOpen: () => void }) {
  const url = app.status !== 'stopped' ? appUrl(app.web, app.webPort) : null
  const led = ledState(app.status, app.busy)
  return (
    <div className="plate group relative flex min-w-0 flex-col bg-raised transition-[border-color] hover:border-rule-hi">
      <button onClick={onOpen} className="absolute inset-0 z-0 rounded-[3px]" aria-label={`Manage ${app.name}`} />
      <div className="pointer-events-none relative flex items-center justify-between border-b border-rule-2 px-3 py-2">
        <span className="flex items-center gap-2">
          <Led s={led} />
          <span className={cx('etch', led === 'bad' && 'text-bad', led === 'busy' && 'text-accent')}>{statusWord(app.status, app.busy)}</span>
        </span>
        <span className="vent" aria-hidden>
          <i />
          <i />
          <i />
        </span>
      </div>
      <div className="pointer-events-none relative flex items-center gap-3 px-3 py-4">
        <AppIcon src={app.icon} name={app.name} size={40} />
        <span className="min-w-0">
          <span className="block truncate font-semibold">{app.name}</span>
          <span className="block truncate font-mono text-xs text-muted">{app.webPort ? `:${app.webPort}` : app.id}</span>
        </span>
      </div>
      <UsageStrip app={app} className="pointer-events-none relative mx-3 mb-3" />
      <div className="relative z-10 mt-auto flex gap-2 px-3 pb-3">
        {url ? (
          <a href={url} target="_blank" rel="noreferrer" className="key key-sm flex-1">
            Open <ArrowUpRight size={13} />
          </a>
        ) : (
          <span className="key key-sm flex-1" aria-disabled="true">
            Not running
          </span>
        )}
        {app.updateAvailable && <span className="tag tag-accent self-center">Update</span>}
      </div>
    </div>
  )
}

const actionLabel: Record<string, string> = { install: 'Install', uninstall: 'Remove', update: 'Update', start: 'Start', stop: 'Stop', restart: 'Restart', reconfigure: 'Settings' }

function Activity() {
  const { data } = usePoll(api.jobs, 5000)
  const jobs = (data || []).slice(0, 9)
  return (
    <section className="plate flex flex-col" aria-labelledby="activity-h">
      <div className="plate-head">
        <h2 id="activity-h" className="etch">
          Activity
        </h2>
        <span className="etch text-[0.625rem]">Since Haven started</span>
      </div>
      {jobs.length === 0 ? (
        <p className="p-4 text-sm text-muted">Nothing yet. Installs, updates and restarts are logged here.</p>
      ) : (
        <ol className="divide-y divide-rule-2">
          {jobs.map((j) => (
            <li key={j.id} className="flex items-center gap-3 px-4 py-3 font-mono text-xs">
              <span className="text-muted">{timeOf(j.started)}</span>
              <span className="w-16 shrink-0 uppercase tracking-wider text-ink-2">{actionLabel[j.action] || j.action}</span>
              <span className="min-w-0 flex-1 truncate">{j.app}</span>
              <Led s={j.status === 'running' ? 'busy' : j.status === 'success' ? 'ok' : 'bad'} label={j.status} />
            </li>
          ))}
        </ol>
      )}
    </section>
  )
}

export default function Overview() {
  const { apps, appsReady, sys, navigate, openApp, role } = useHaven()
  const manage = atLeast(role, 'manager')
  const { data: widgets } = useWidgets()
  const hist = sys?.history || []
  const disks = sys?.disks || []
  const root = disks.find((d) => d.mount === '/') || disks[0]
  const running = apps.filter((a) => a.status === 'running').length
  const faults = apps.filter((a) => a.status === 'error').length
  const [rxVal, rxUnit] = splitBytes(sys?.netRx || 0)
  const [txVal, txUnit] = splitBytes(sys?.netTx || 0)

  return (
    <div className="space-y-5">
      <div className="flex flex-wrap items-end justify-between gap-x-6 gap-y-3">
        <div className="min-w-0">
          <h1 className="text-3xl font-semibold tracking-tight sm:text-4xl">{sys?.hostname || 'Your server'}</h1>
          <p className="mt-2 font-mono text-xs uppercase tracking-wider text-muted">
            {sys ? [sys.os || 'Linux', sys.arch, sys.kernel && `kernel ${sys.kernel}`, `up ${formatUptime(sys.uptime)}`].filter(Boolean).join(' · ') : 'Reading the server…'}
          </p>
        </div>
        <div className="flex items-center gap-2">
          <span className="tag">
            <Led s={running > 0 ? 'ok' : 'off'} /> {appsReady ? `${running}/${apps.length} running` : '… running'}
          </span>
          {faults > 0 && (
            <span className="tag tag-bad">
              <Led s="bad" /> {faults} fault{faults > 1 ? 's' : ''}
            </span>
          )}
        </div>
      </div>

      <section className="plate grid grid-cols-2 gap-px overflow-hidden bg-rule-2 lg:grid-cols-4" aria-label="Server meters">
        <Cell label="CPU" spec={sys ? `${sys.cpuCores} cores` : undefined} foot={sys ? `load ${sys.load.map((l) => l.toFixed(2)).join('  ')}` : undefined}>
          <span className="text-[2.25rem] sm:text-5xl">
            <Readout value={sys?.cpuPercent} digits={3} unit="%" />
          </span>
          <Trace values={hist.map((p) => p.cpu)} max={100} />
        </Cell>
        <Cell label="Memory" spec={sys ? formatBytes(sys.memTotal, 0) : undefined} foot={sys ? `${formatBytes(sys.memUsed)} used · swap ${sys.swapTotal ? formatBytes(sys.swapUsed) : 'off'}` : undefined}>
          <span className="text-[2.25rem] sm:text-5xl">
            <Readout value={sys?.memPercent} digits={3} unit="%" />
          </span>
          <Ladder percent={sys?.memPercent || 0} />
        </Cell>
        <Cell label="Storage" spec={root ? `${formatBytes(root.free, 0)} free` : undefined}>
          <span className="text-[2.25rem] sm:text-5xl">
            <Readout value={root?.percent} digits={3} unit="%" />
          </span>
          <div className="space-y-2">
            {disks.slice(0, 3).map((d) => (
              <div key={d.mount} className="space-y-1">
                <div className="flex justify-between gap-2 font-mono text-[0.6875rem] text-muted">
                  <span className="truncate">{d.mount}</span>
                  <span className="shrink-0">
                    {formatBytes(d.used, 0)} / {formatBytes(d.total, 0)}
                  </span>
                </div>
                <Ladder percent={d.percent} />
              </div>
            ))}
          </div>
        </Cell>
        <Cell label="Download" spec="physical ports" foot={sys ? `upload ${txVal} ${txUnit}/s` : undefined}>
          <span className="text-[2.25rem] sm:text-5xl">
            <Readout value={sys ? Number(rxVal) : null} digits={1} decimals={rxVal.includes('.') ? 1 : 0} unit={`${rxUnit}/s`} />
          </span>
          <Trace values={hist.map((p) => p.rx + p.tx)} />
        </Cell>
      </section>

      <div className="well flex flex-wrap items-center gap-x-5 gap-y-2 px-4 py-3 font-mono text-xs text-ink-2">
        <span>
          <span className="text-muted">TEMP</span> {sys?.tempC != null ? `${sys.tempC.toFixed(0)}°C` : 'n/a'}
        </span>
        <span className="hidden h-3 w-px bg-rule sm:block" />
        <span>
          <span className="text-muted">DOCKER</span> {sys ? sys.docker || <span className="text-bad">offline</span> : '…'}
        </span>
        <span className="hidden h-3 w-px bg-rule sm:block" />
        <span>
          <span className="text-muted">CPU</span> {sys?.cpuModel || '…'}
        </span>
        <span className="hidden h-3 w-px bg-rule sm:block" />
        <span>
          <span className="text-muted">HAVEN</span> {sys?.version || '…'}
        </span>
      </div>

      <WidgetBoard data={widgets} />

      <div className="grid grid-cols-1 gap-5 xl:grid-cols-[minmax(0,2fr)_minmax(0,1fr)]">
        <section aria-labelledby="apps-h">
          <div className="mb-3 flex min-h-10 items-center justify-between gap-3 border-b border-rule pb-2">
            <h2 id="apps-h" className="etch">
              Apps · {appsReady ? apps.length : '…'}
            </h2>
            {manage && (
              <button className="key key-sm" onClick={() => navigate('catalog')}>
                <Plus size={13} /> Add app
              </button>
            )}
          </div>
          {!appsReady ? (
            <div className="grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-3" aria-hidden>
              {[0, 1, 2].map((i) => (
                <div key={i} className="plate h-[9.5rem] bg-well" />
              ))}
            </div>
          ) : apps.length === 0 && !manage ? (
            <p className="rounded-[3px] border border-dashed border-rule-hi px-6 py-12 text-center text-sm text-muted">No apps have been shared with you yet. Ask the admin to give you access.</p>
          ) : apps.length === 0 ? (
            <button onClick={() => navigate('catalog')} className="flex w-full flex-col items-center gap-2 rounded-[3px] border border-dashed border-rule-hi px-6 py-12 text-center hover:border-accent">
              <span className="etch">Empty slot</span>
              <span className="font-semibold">Install your first app</span>
              <span className="max-w-sm text-sm text-muted">Pick something from the catalog. Haven checks the server first, then sets it up.</span>
            </button>
          ) : (
            <div className="grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-3">
              {apps.map((a) => (
                <AppModule key={a.id} app={a} onOpen={() => openApp(a.id)} />
              ))}
            </div>
          )}
        </section>
        <div className="space-y-5">
          <UsageRanking apps={apps} memTotal={sys?.memTotal} onOpen={openApp} />
          {manage && <Activity />}
        </div>
      </div>
    </div>
  )
}
