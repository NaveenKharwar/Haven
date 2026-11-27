import { useEffect, useRef, useState } from 'react'
import { RotateCw, X } from 'lucide-react'
import { api, ApiError, formatBytes } from './api'
import { ErrorLine } from './components'

type Transfer = {
  id: string
  path: string
  file: File
  bytes: number
  status: 'waiting' | 'uploading' | 'failed'
  error?: string
}

const chunkSize = 4 * 1024 * 1024

async function upload(transfer: Transfer, signal: AbortSignal, progress: (bytes: number) => void) {
  const { id, path, file } = transfer
  const query = new URLSearchParams({ id, path, name: file.name, size: String(file.size) })
  let state = await api.fileUploadStatus(query)
  progress(state.offset)
  let retries = 0
  while (!state.done) {
    signal.throwIfAborted()
    const offset = state.offset
    query.set('offset', String(offset))
    try {
      state = await api.fileUploadChunk(query, file.slice(offset, offset + chunkSize), signal, (bytes) => progress(offset + bytes))
      progress(state.offset)
      retries = 0
    } catch (error) {
      const retryable = error instanceof ApiError && (error.status === 0 || error.status >= 500)
      if (signal.aborted || !retryable || retries >= 2) throw error
      retries++
      await new Promise((resolve) => setTimeout(resolve, retries * 500))
      // A repeated PUT at the old offset returns the latest committed offset.
    }
  }
}

export function useFileUploads(onUploaded: (path: string) => void) {
  const [transfers, setTransfers] = useState<Transfer[]>([])
  const worker = useRef<AbortController | null>(null)
  useEffect(() => () => worker.current?.abort(), [])

  const run = async (batch: Transfer[]) => {
    if (worker.current || batch.length === 0) return
    const controller = new AbortController()
    worker.current = controller
    const update = (id: string, next: Partial<Transfer>) => {
      if (!controller.signal.aborted) {
        setTransfers((items) => items.map((item) => item.id === id ? { ...item, ...next } : item))
      }
    }
    const completed = new Set<string>()
    try {
      for (const item of batch) {
        if (controller.signal.aborted) break
        update(item.id, { status: 'uploading', error: undefined })
        try {
          await upload(item, controller.signal, (bytes) => update(item.id, { bytes }))
          if (!controller.signal.aborted) {
            completed.add(item.path)
            setTransfers((items) => items.filter((transfer) => transfer.id !== item.id))
          }
        } catch (e) {
          update(item.id, { status: 'failed', error: (e as Error).message })
        }
      }
    } finally {
      worker.current = null
      if (!controller.signal.aborted) completed.forEach(onUploaded)
    }
  }

  const enqueue = (path: string, files: FileList) => {
    if (worker.current) return
    const batch = Array.from(files, (file): Transfer => ({
      id: Array.from(crypto.getRandomValues(new Uint8Array(16)), (byte) => byte.toString(16).padStart(2, '0')).join(''),
      path, file, bytes: 0, status: 'waiting',
    }))
    setTransfers((items) => [...items, ...batch])
    void run(batch)
  }
  const active = transfers.some((transfer) => transfer.status !== 'failed')
  const retry = (item: Transfer) => void run([item])
  const remove = (id: string) => setTransfers((items) => items.filter((item) => item.id !== id))
  return { transfers, active, enqueue, retry, remove }
}

export function UploadQueue({ queue, path }: { queue: ReturnType<typeof useFileUploads>; path: string | null }) {
  if (queue.transfers.length === 0) return null
  return (
    <section aria-label="Uploads" className="well">
      <h3 className="etch border-b border-rule px-3 py-2">Uploads</h3>
      <ul className="max-h-56 divide-y divide-rule-2 overflow-y-auto">
        {queue.transfers.map((item) => {
          const failed = item.status === 'failed'
          const percent = item.file.size ? Math.floor(item.bytes / item.file.size * 100) : 0
          return (
            <li key={item.id} className="flex flex-wrap items-center justify-between gap-x-3 gap-y-2 p-3">
              <div className="min-w-0 flex-1">
                <p className="break-words text-sm font-medium">{item.file.name}</p>
                {item.path !== path && <p className="break-all text-xs text-muted">{item.path}</p>}
                {failed
                  ? <ErrorLine className="mt-1 text-xs" size="inherit">{item.error}</ErrorLine>
                  : <p className="text-xs text-muted">{item.status === 'waiting' ? 'Queued' : item.bytes >= item.file.size ? 'Saving to folder…' : `${percent}% · ${formatBytes(item.bytes)} of ${formatBytes(item.file.size)}`}</p>}
                {item.status === 'uploading' && <progress className="file-progress mt-2 block h-1 w-full" value={item.bytes} max={item.file.size || 1} aria-label={`Uploading ${item.file.name}`} />}
              </div>
              {failed && (
                <div className="flex shrink-0 gap-1">
                  <button className="key key-sm" disabled={queue.active} onClick={() => queue.retry(item)} aria-label={`Retry upload of ${item.file.name}`}><RotateCw size={13} /> Retry</button>
                  <button className="key key-flat key-sm key-icon" onClick={() => queue.remove(item.id)} aria-label={`Remove upload of ${item.file.name}`}><X size={14} /></button>
                </div>
              )}
            </li>
          )
        })}
      </ul>
    </section>
  )
}
