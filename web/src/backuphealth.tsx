import { api, timeAgo } from './api'
import { ErrorLine, Led, Spinner, usePoll } from './components'
import { useHaven } from './context'
import { Widget } from './widgetshell'

export function BackupHealthWidget() {
  const { data, error, refresh } = usePoll(api.backupHealth, 10000)
  const { openApp } = useHaven()
  const failed = data?.filter((app) => app.failed).length || 0
  const unknown = data?.filter((app) => !app.lastSuccess).length || 0
  return (
    <Widget title="Backup health" className="max-h-[24rem]">
      <div className="flex min-h-0 flex-1 flex-col gap-3 p-4">
        {!data && !error && <Spinner />}
        {error && <div><ErrorLine>{error}</ErrorLine><button className="key key-sm mt-2" onClick={refresh}>Try again</button></div>}
        {data && data.length === 0 && <p className="text-sm text-ink-2">Install an app to track its backups.</p>}
        {data && data.length > 0 && <>
          <p className="text-sm text-ink-2">{failed ? `${failed} latest attempt${failed === 1 ? '' : 's'} failed` : 'No recorded failures'} · {unknown} without a recorded success</p>
          <ul className="min-h-0 flex-1 divide-y divide-rule-2 overflow-y-auto" aria-label="Backup results">
            {data.map((app) => <li key={app.id} className="flex items-center gap-2 py-2">
              <Led s={app.failed ? 'bad' : app.lastSuccess ? 'ok' : 'off'} />
              <button className="min-w-0 flex-1 truncate text-left text-sm font-semibold hover:underline focus-visible:underline" onClick={() => openApp(app.id)}>{app.name}</button>
              <span className="text-right text-xs text-muted">{app.failed ? `Failed${app.lastSuccess ? ` · saved ${timeAgo(app.lastSuccess)}` : ''}` : app.lastSuccess ? `Saved ${timeAgo(app.lastSuccess)}` : 'No record'}</span>
            </li>)}
          </ul>
          <p className="mt-auto text-xs text-muted">Records begin with this feature. They do not verify that backup files still exist or can be restored.</p>
        </>}
      </div>
    </Widget>
  )
}
