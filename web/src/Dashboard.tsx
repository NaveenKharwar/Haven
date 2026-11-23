import { ChevronDown } from 'lucide-react'
import { useState, type ReactNode } from 'react'
import { api, atLeast, formatBytes, formatUptime, splitBytes, timeOf } from './api'
import { ErrorLine, Ladder, Led, Readout, Trace, cx, usePoll } from './components'
import { useHaven } from './context'
import { UsageRanking } from './usage'
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

function OverviewDisclosure({ title, hint, children }: { title: string; hint: string; children: ReactNode }) {
  const [open, setOpen] = useState(false)
  return (
    <details className="overview-disclosure group/disclosure plate min-w-0" onToggle={(event) => setOpen(event.currentTarget.open)}>
      <summary className="plate-head cursor-pointer list-none border-b-0 hover:bg-raised focus-visible:outline-offset-[-2px] group-open/disclosure:border-b">
        <span className="etch">{title}</span>
        <span className="flex items-center gap-2" aria-hidden>
          <span className="hidden text-xs text-muted sm:inline">{hint}</span>
          <ChevronDown size={15} className="text-muted transition-transform group-open/disclosure:rotate-180" />
        </span>
      </summary>
      {open && children}
    </details>
  )
}

const actionLabel: Record<string, string> = {
  install: 'Install', uninstall: 'Remove', update: 'Update', 'update-all': 'Updates', 'update-with-backup': 'Updates',
  start: 'Start', stop: 'Stop', restart: 'Restart', reconfigure: 'Settings', backup: 'Back up', restore: 'Restore',
}

function Activity() {
  const { data, error, refresh } = usePoll(api.jobs, 5000)
  if (!data) {
    return error ? (
      <div className="flex flex-wrap items-center justify-between gap-3 p-4">
        <ErrorLine>{error}</ErrorLine>
        <button className="key key-sm" onClick={refresh}>Try again</button>
      </div>
    ) : <p role="status" className="p-4 text-sm text-muted">Loading activity…</p>
  }

  const jobs = data.slice(0, 9)
  if (jobs.length === 0) {
    return <p className="p-4 text-sm text-muted">Nothing yet. Installs, updates and restarts are logged here.</p>
  }

  return (
    <ol className="divide-y divide-rule-2">
      {jobs.map((j) => (
        <li key={j.id} className="flex items-center gap-3 px-4 py-3 font-mono text-xs">
          <span className="shrink-0 text-muted">{timeOf(j.started)}</span>
          <span className="w-16 shrink-0 truncate uppercase tracking-wider text-ink-2" title={actionLabel[j.action] || j.action}>{actionLabel[j.action] || j.action}</span>
          <span className="min-w-0 flex-1 truncate" title={j.app}>{j.app}</span>
          <Led s={j.status === 'running' ? 'busy' : j.status === 'success' ? 'ok' : 'bad'} label={j.status} />
        </li>
      ))}
    </ol>
  )
}

export default function Overview() {
  const { apps, appsReady, sys, openApp, role } = useHaven()
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

      <div className={cx('grid grid-cols-1 items-start gap-3', manage && 'md:grid-cols-2')}>
        <OverviewDisclosure title="Usage by app" hint="By memory">
          <UsageRanking apps={apps} ready={appsReady} memTotal={sys?.memTotal} onOpen={openApp} />
        </OverviewDisclosure>
        {manage && (
          <OverviewDisclosure title="Activity" hint="Recent jobs">
            <Activity />
          </OverviewDisclosure>
        )}
      </div>
    </div>
  )
}
