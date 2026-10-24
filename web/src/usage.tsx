import { formatBytes, type AppUsage, type InstalledApp } from './api'
import { Ladder, cx } from './components'

// Per-app resource use: CPU and memory are live; disk is the app's images
// plus its data folder, measured every few minutes.

export function fmtCpu(n?: number): string {
  if (n == null) return '—'
  if (n > 0 && n < 0.1) return '<0.1%'
  return n < 10 ? `${n.toFixed(1)}%` : `${Math.round(n)}%`
}

export function measured(u?: AppUsage): boolean {
  return !!u?.dataAt && !u.dataAt.startsWith('0001')
}

export function diskBytes(u?: AppUsage): number {
  return u ? u.images + u.data : 0
}

/** Live numbers only mean something while the app is up. */
export function isLive(app: InstalledApp): boolean {
  return !!app.usage?.live && app.status !== 'stopped'
}

export function agoText(iso?: string): string {
  if (!iso || iso.startsWith('0001')) return 'not measured yet'
  const s = Math.max(0, Math.round((Date.now() - new Date(iso).getTime()) / 1000))
  if (s < 60) return 'measured just now'
  if (s < 3600) return `measured ${Math.round(s / 60)} min ago`
  return `measured ${Math.round(s / 3600)} h ago`
}

export function cpuText(app: InstalledApp) {
  return isLive(app) ? fmtCpu(app.usage?.cpu) : '—'
}
export function memText(app: InstalledApp) {
  return isLive(app) ? formatBytes(app.usage?.mem || 0) : '—'
}
export function diskText(app: InstalledApp) {
  const u = app.usage
  if (!u) return '—'
  return measured(u) ? formatBytes(diskBytes(u)) : `${formatBytes(u.images)}+`
}

/** Three tiny readouts for an app module: CPU · Memory · Disk. */
export function UsageStrip({ app, className }: { app: InstalledApp; className?: string }) {
  const cells: [string, string, string][] = [
    ['CPU', cpuText(app), 'Share of the whole server’s CPU'],
    ['Mem', memText(app), 'Memory in use right now'],
    ['Disk', diskText(app), 'Images plus the app’s data folder'],
  ]
  return (
    <div className={cx('grid grid-cols-3 gap-px overflow-hidden rounded-[2px] bg-rule-2', className)}>
      {cells.map(([label, value, hint]) => (
        <div key={label} className="min-w-0 bg-well px-2 py-1" title={hint}>
          <div className="etch text-[0.5625rem]">{label}</div>
          <div className="truncate font-mono text-xs text-ink">{value}</div>
        </div>
      ))}
    </div>
  )
}

/** Detailed resource panel for one app. */
export function UsageCells({ app }: { app: InstalledApp }) {
  const u = app.usage
  const live = isLive(app)
  const cells: [string, string, string][] = [
    ['CPU', live ? fmtCpu(u?.cpu) : '—', live ? 'of the whole server' : 'not running'],
    ['Memory', live ? formatBytes(u?.mem || 0) : '—', live ? `${(u?.memPercent || 0).toFixed(1)}% of RAM` : 'not running'],
    ['Images', u ? formatBytes(u.images) : '—', 'the app’s programs'],
    ['Data', u && measured(u) ? formatBytes(u.data) : '…', agoText(u?.dataAt)],
  ]
  return (
    <section className="plate grid grid-cols-2 gap-px overflow-hidden bg-rule-2 sm:grid-cols-4" aria-label="Resource use">
      {cells.map(([label, value, foot]) => (
        <div key={label} className="min-w-0 bg-plate px-4 py-3">
          <div className="etch">{label}</div>
          <div className="readout mt-2 truncate text-2xl">{value}</div>
          <div className="mt-1 truncate font-mono text-[0.6875rem] text-muted">{foot}</div>
        </div>
      ))}
    </section>
  )
}

/** Overview panel ranking apps by memory, with CPU and disk alongside. */
export function UsageRanking({ apps, memTotal, onOpen }: { apps: InstalledApp[]; memTotal?: number; onOpen: (id: string) => void }) {
  const ranked = [...apps].sort((a, b) => (isLive(b) ? b.usage?.mem || 0 : 0) - (isLive(a) ? a.usage?.mem || 0 : 0))
  const top = Math.max(1, ...ranked.map((a) => (isLive(a) ? a.usage?.mem || 0 : 0)))
  const totalMem = ranked.reduce((s, a) => s + (isLive(a) ? a.usage?.mem || 0 : 0), 0)
  const totalDisk = ranked.reduce((s, a) => s + diskBytes(a.usage), 0)
  return (
    <section className="plate overflow-hidden" aria-labelledby="usage-h">
      <div className="plate-head">
        <h2 id="usage-h" className="etch">
          Usage by app
        </h2>
        <span className="etch text-[0.625rem]">by memory</span>
      </div>
      {ranked.length === 0 ? (
        <p className="p-4 text-sm text-muted">Install an app to see what it uses.</p>
      ) : (
        <>
          <ul className="divide-y divide-rule-2">
            {ranked.slice(0, 8).map((a) => {
              const live = isLive(a)
              return (
                <li key={a.id}>
                  <button onClick={() => onOpen(a.id)} className="grid w-full grid-cols-[minmax(0,1fr)_auto] items-center gap-x-3 gap-y-2 px-4 py-3 text-left hover:bg-raised">
                    <span className="truncate text-sm font-medium">{a.name}</span>
                    <span className="font-mono text-xs text-ink">{memText(a)}</span>
                    <Ladder percent={live ? ((a.usage?.mem || 0) / top) * 100 : 0} segs={20} plain />
                    <span className="font-mono text-[0.6875rem] text-muted">
                      {cpuText(a)} cpu · {diskText(a)}
                    </span>
                  </button>
                </li>
              )
            })}
          </ul>
          <div className="border-t border-rule-2 px-4 py-3 font-mono text-[0.6875rem] text-muted">
            Apps use {formatBytes(totalMem)}
            {memTotal ? ` of ${formatBytes(memTotal, 0)} RAM` : ' of RAM'} · {formatBytes(totalDisk)} on disk
          </div>
        </>
      )}
    </section>
  )
}
