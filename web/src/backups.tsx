import { useEffect, useState } from 'react'
import { api, type BackupManifest, type BackupPlan, type InstalledApp } from './api'
import { ErrorLine, Modal, Notice, Spinner } from './components'
import { useHaven } from './context'

function BackupContents({ plan }: { plan: BackupPlan }) {
  return (
    <div className="space-y-3">
      <ul className="well divide-y divide-rule-2 px-3 text-sm" aria-label="Backup contents">
        {plan.items.map((item) => (
          <li key={item.kind + item.name} className="break-words py-2">
            {item.kind === 'volume' ? `Docker volume ${item.name}` : item.name}
          </li>
        ))}
      </ul>
      {plan.images.length > 0 && (
        <details className="text-xs text-ink-2">
          <summary className="cursor-pointer">Image references</summary>
          <ul className="mt-2 space-y-1 break-all font-mono">
            {plan.images.map((image) => <li key={image}>{image}</li>)}
          </ul>
        </details>
      )}
      {plan.omitted.length > 0 && (
        <details className="text-xs text-warn" open>
          <summary className="cursor-pointer">Excluded storage ({plan.omitted.length})</summary>
          <ul className="mt-2 list-disc space-y-1 break-words pl-4 text-ink-2">
            {plan.omitted.map((item) => <li key={item}>{item}</li>)}
          </ul>
        </details>
      )}
    </div>
  )
}

export function BackupDialog({ app, onClose }: { app: InstalledApp; onClose: () => void }) {
  const { openJob, refreshApps } = useHaven()
  const [plan, setPlan] = useState<BackupPlan | null>(null)
  const [destination, setDestination] = useState('')
  const [folders, setFolders] = useState<string[]>([])
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')

  useEffect(() => {
    let active = true
    api.backupPlan(app.id)
      .then((value) => { if (active) setPlan(value) })
      .catch((e: Error) => { if (active) setError(e.message) })
    return () => { active = false }
  }, [app.id])

  const save = async () => {
    setBusy(true)
    setError('')
    try {
      const job = await api.createBackup(app.id, destination.trim(), folders)
      onClose()
      openJob(job.id, `Back up ${app.name}`)
      refreshApps()
    } catch (e) {
      setError((e as Error).message)
      setBusy(false)
    }
  }

  const optionalFolders = plan?.items.filter((item) => item.kind === 'folder') ?? []

  return (
    <Modal
      open
      onClose={() => { if (!busy) onClose() }}
      label="Backup"
      title={app.name}
      footer={
        <>
          <button className="key" disabled={busy} onClick={onClose}>Cancel</button>
          <button className="key key-primary" disabled={busy || !plan || !destination.trim()} onClick={save}>
            {busy && <Spinner size={14} />} Create backup
          </button>
        </>
      }
    >
      <div className="space-y-4">
        <Notice label="During the backup">Haven stops the app while copying its files. The backup contains its settings and passwords, so choose a private folder on the server.</Notice>
        <label className="block space-y-1 text-sm">
          <span>Destination folder</span>
          <input className="field w-full font-mono" placeholder="/mnt/backups" value={destination} disabled={busy} onChange={(e) => setDestination(e.target.value)} />
        </label>
        {plan ? <BackupContents plan={{ ...plan, items: plan.items.filter((item) => item.kind !== 'folder') }} /> : !error && <Spinner />}
        {optionalFolders.length > 0 && (
          <fieldset className="space-y-2">
            <legend className="etch mb-2">Include host folders</legend>
            {optionalFolders.map((item) => (
              <label key={item.path} className="flex items-start gap-2 text-sm">
                <input
                  type="checkbox"
                  className="mt-1 accent-accent"
                  checked={folders.includes(item.path)}
                  disabled={busy}
                  onChange={(e) => setFolders((selected) => e.target.checked ? [...selected, item.path] : selected.filter((path) => path !== item.path))}
                />
                <span className="break-all font-mono">{item.path}</span>
              </label>
            ))}
            <p className="text-xs text-muted">Pause anything else writing to these folders before backing up.</p>
          </fieldset>
        )}
        <p className="text-xs text-muted">Container images and remote databases need separate backups.</p>
        {error && <ErrorLine>{error}</ErrorLine>}
      </div>
    </Modal>
  )
}

export function RestoreBackupDialog({ onClose }: { onClose: () => void }) {
  const { openJob, refreshApps } = useHaven()
  const [path, setPath] = useState('')
  const [manifest, setManifest] = useState<BackupManifest | null>(null)
  const [restoreFolders, setRestoreFolders] = useState(false)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')

  const inspect = async () => {
    setBusy(true)
    setError('')
    try {
      setManifest(await api.inspectBackup(path.trim()))
    } catch (e) {
      setError((e as Error).message)
    } finally {
      setBusy(false)
    }
  }

  const restore = async () => {
    if (!manifest) return
    setBusy(true)
    setError('')
    try {
      const job = await api.restoreBackup(path.trim(), manifest.appId, restoreFolders)
      onClose()
      openJob(job.id, `Restore ${manifest.appName}`)
      refreshApps()
    } catch (e) {
      setError((e as Error).message)
      setBusy(false)
    }
  }

  return (
    <Modal
      open
      onClose={() => { if (!busy) onClose() }}
      label="Backups"
      title="Restore an app"
      footer={
        <>
          <button className="key" disabled={busy} onClick={onClose}>Cancel</button>
          <button className="key key-primary" disabled={busy || !path.trim()} onClick={manifest ? restore : inspect}>
            {busy && <Spinner size={14} />} {manifest ? 'Restore app' : 'Review backup'}
          </button>
        </>
      }
    >
      <div className="space-y-4">
        <label className="block space-y-1 text-sm">
          <span>Backup folder on the server</span>
          <input
            className="field w-full font-mono"
            placeholder="/mnt/backups/app.haven-backup"
            value={path}
            disabled={busy}
            onChange={(e) => {
              setPath(e.target.value)
              setManifest(null)
              setRestoreFolders(false)
              setError('')
            }}
          />
        </label>
        <p className="text-sm text-ink-2">Use a backup you trust. Restore needs unused app storage and leaves the app stopped for review.</p>
        {manifest && (
          <>
            <p className="text-sm font-semibold">{manifest.appName} <span className="font-normal text-muted">· {new Date(manifest.created).toLocaleString()}</span></p>
            <BackupContents plan={manifest} />
            {manifest.items.some((item) => item.kind === 'folder') && (
              <label className="flex items-start gap-2 text-sm">
                <input type="checkbox" className="mt-1 accent-accent" checked={restoreFolders} disabled={busy} onChange={(e) => setRestoreFolders(e.target.checked)} />
                <span>Restore the included host folders to their original paths. Those paths must be unused.</span>
              </label>
            )}
          </>
        )}
        {error && <ErrorLine>{error}</ErrorLine>}
      </div>
    </Modal>
  )
}
