import { useCallback, useEffect, useRef, useState } from 'react'
import { ArrowUp, ChevronRight, Copy, Download, Eye, File as FileIcon, Folder, FolderPlus, Pencil, Shield, ShieldAlert, Trash2, Upload, X } from 'lucide-react'
import { api, formatBytes, type FileEntry, type FileListing, type FilesInfo } from './api'
import { ErrorLine, Modal, Spinner, cx, toast } from './components'

const IMG = /\.(png|jpe?g|gif|webp|svg|bmp)$/i
const VIDEO = /\.(mp4|m4v|webm|ogv|mov|mkv)$/i
const AUDIO = /\.(mp3|m4a|wav|ogg|oga|flac|opus)$/i
const PDF = /\.pdf$/i

function when(iso: string): string {
  const d = new Date(iso)
  return d.toLocaleDateString(undefined, { day: 'numeric', month: 'short', year: 'numeric' })
}

function crumbs(path: string): { name: string; path: string }[] {
  const out = [{ name: '/', path: '/' }]
  let acc = ''
  for (const part of path.split('/').filter(Boolean)) {
    acc += '/' + part
    out.push({ name: part, path: acc })
  }
  return out
}

/** The file explorer: safe folders by default, whole disk after accepting the risk. */
export default function FilesPanel({ initialPath, admin = false }: { initialPath?: string; admin?: boolean }) {
  const [info, setInfo] = useState<FilesInfo | null>(null)
  const [path, setPath] = useState<string | null>(null)
  const [list, setList] = useState<FileListing | null>(null)
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)
  const [picked, setPicked] = useState<Set<string>>(new Set())
  const [dialog, setDialog] = useState<null | { kind: 'mkdir' } | { kind: 'rename'; e: FileEntry } | { kind: 'delete'; paths: string[] } | { kind: 'edit'; e: FileEntry } | { kind: 'view'; e: FileEntry } | { kind: 'advanced' } | { kind: 'folders' } | { kind: 'move'; paths: string[]; copy: boolean }>(null)
  const [progress, setProgress] = useState<{ name: string; frac: number } | null>(null)
  const fileInput = useRef<HTMLInputElement>(null)

  const loadInfo = useCallback(async () => {
    const i = await api.filesInfo()
    setInfo(i)
    return i
  }, [])
  const open = useCallback(async (p: string) => {
    setBusy(true)
    setError('')
    try {
      const l = await api.filesList(p)
      setList(l)
      setPath(l.path)
      setPicked(new Set())
    } catch (e: any) {
      setError(e.message)
      setList(null)
    } finally {
      setBusy(false)
    }
  }, [])

  useEffect(() => {
    loadInfo()
      .then((i) => open(initialPath || i.roots.find((r) => r.name === 'App data')?.path || i.roots[0].path))
      .catch((e) => setError(e.message))
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [loadInfo, open, initialPath])

  const refresh = () => path && open(path)
  const upload = async (files: FileList | File[]) => {
    if (!path) return
    for (const f of Array.from(files)) {
      setProgress({ name: f.name, frac: 0 })
      try {
        await api.filesUpload(path, f, (frac) => setProgress({ name: f.name, frac }))
        toast.ok(`Uploaded ${f.name}`)
      } catch (e: any) {
        toast.error(e.message)
        break
      }
    }
    setProgress(null)
    refresh()
  }

  const entries = list?.entries || []
  const toggle = (p: string) =>
    setPicked((s) => {
      const n = new Set(s)
      n.has(p) ? n.delete(p) : n.add(p)
      return n
    })
  const activate = (e: FileEntry) => {
    if (e.dir) return open(e.path)
    setDialog({ kind: 'view', e })
  }

  return (
    <section className="plate overflow-hidden" aria-label="File explorer">
      <div className="plate-head">
        <h2 className="etch">Files</h2>
        {info && !admin && <span className="etch flex items-center gap-2 text-[0.625rem]"><Shield size={12} /> Safe folders</span>}
        {info && admin && (
          <button className={cx('key key-sm gap-2', info.full && 'key-danger')} onClick={() => setDialog({ kind: 'advanced' })}>
            {info.full ? <ShieldAlert size={13} /> : <Shield size={13} />}
            {info.full ? 'Advanced: whole disk' : 'Safe folders'}
          </button>
        )}
      </div>

      <div className="space-y-3 p-4 sm:p-5">
        {info && info.roots.length > 0 && (
          <div className="flex flex-wrap gap-1.5" aria-label="Locations">
            {info.roots.map((r) => (
              <button key={r.path} className={cx('key key-sm', path && (path === r.path || (r.path !== '/' && path.startsWith(r.path + '/'))) ? 'key-primary' : 'key-flat')} onClick={() => open(r.path)}>
                {r.name}
              </button>
            ))}
          </div>
        )}

        {path && (
          <nav className="flex flex-wrap items-center gap-0.5 font-mono text-xs" aria-label="Folder path">
            {crumbs(path).map((c, i, a) => (
              <span key={c.path} className="flex items-center gap-0.5">
                <button className="rounded px-1 py-0.5 hover:bg-raised hover:text-ink" onClick={() => open(c.path)}>
                  {c.name}
                </button>
                {i < a.length - 1 && i > 0 && <ChevronRight size={11} className="text-muted" />}
              </span>
            ))}
          </nav>
        )}

        <div className="flex flex-wrap items-center gap-2">
          <button className="key key-sm" disabled={!list?.parent || busy} onClick={() => list?.parent && open(list.parent)}>
            <ArrowUp size={13} /> Up
          </button>
          <button className="key key-sm" disabled={!path} onClick={() => fileInput.current?.click()}>
            <Upload size={13} /> Upload
          </button>
          <input ref={fileInput} type="file" multiple hidden onChange={(e) => e.target.files && upload(e.target.files).then(() => (e.target.value = ''))} />
          <button className="key key-sm" disabled={!path} onClick={() => setDialog({ kind: 'mkdir' })}>
            <FolderPlus size={13} /> New folder
          </button>
          <button className="key key-sm" disabled={!path} onClick={() => setDialog({ kind: 'folders' })}>
            Folder sizes
          </button>
          {picked.size > 0 && (
            <span className="ml-auto flex flex-wrap items-center gap-2">
              <span className="text-xs text-muted">{picked.size} selected</span>
              <button className="key key-sm" onClick={() => setDialog({ kind: 'move', paths: [...picked], copy: false })}>
                Move
              </button>
              <button className="key key-sm" onClick={() => setDialog({ kind: 'move', paths: [...picked], copy: true })}>
                <Copy size={13} /> Copy
              </button>
              <button className="key key-sm key-danger" onClick={() => setDialog({ kind: 'delete', paths: [...picked] })}>
                <Trash2 size={13} /> Delete
              </button>
            </span>
          )}
        </div>

        {progress && (
          <div className="well p-3 text-xs">
            Uploading {progress.name}… {Math.round(progress.frac * 100)}%
            <div className="mt-2 h-1.5 overflow-hidden rounded bg-unlit">
              <div className="h-full bg-accent" style={{ width: `${progress.frac * 100}%` }} />
            </div>
          </div>
        )}
        {error && (
          <div className="space-y-2">
            <ErrorLine>{error}</ErrorLine>
            {admin && error.includes('Advanced access') && (
              <button className="key key-sm" onClick={() => setDialog({ kind: 'advanced' })}>
                <ShieldAlert size={13} /> Review access…
              </button>
            )}
          </div>
        )}

        <div
          className="well relative min-h-[12rem] overflow-hidden"
          onDragOver={(e) => e.preventDefault()}
          onDrop={(e) => {
            e.preventDefault()
            if (e.dataTransfer.files.length) upload(e.dataTransfer.files)
          }}
        >
          {!list && error ? (
            <p className="p-8 text-center text-sm text-muted">Pick a location above to continue.</p>
          ) : busy && !list ? (
            <div className="grid place-items-center p-10">
              <Spinner />
            </div>
          ) : entries.length === 0 ? (
            <p className="p-8 text-center text-sm text-muted">This folder is empty. Drop files here to upload them.</p>
          ) : (
            <ul className="divide-y divide-rule-2" aria-label="Files and folders">
              {entries.map((e) => (
                <li key={e.path} className="group flex items-center gap-2 px-2 py-1.5 hover:bg-raised">
                  <input type="checkbox" aria-label={`Select ${e.name}`} checked={picked.has(e.path)} onChange={() => toggle(e.path)} className="h-4 w-4 shrink-0 accent-[var(--accent)]" />
                  <button className="flex min-w-0 flex-1 items-center gap-3 py-1 text-left" onClick={() => activate(e)}>
                    {e.dir ? <Folder size={16} className="shrink-0 text-accent" /> : <FileIcon size={16} className="shrink-0 text-muted" />}
                    <span className="min-w-0 flex-1 truncate text-sm">
                      {e.name}
                      {e.link && <span className="ml-2 text-xs text-muted">link</span>}
                    </span>
                    <span className="hidden shrink-0 font-mono text-xs text-muted sm:block">{e.dir ? '' : formatBytes(e.size)}</span>
                    <span className="hidden w-28 shrink-0 text-right text-xs text-muted md:block">{when(e.mod)}</span>
                  </button>
                  <span className="flex shrink-0 gap-0.5">
                    {!e.dir && (
                      <a className="key key-flat key-sm key-icon" href={api.filesDownloadUrl(e.path)} aria-label={`Download ${e.name}`} title="Download">
                        <Download size={13} />
                      </a>
                    )}
                    {!e.dir && (
                      <button className="key key-flat key-sm key-icon" aria-label={`View ${e.name}`} title="View" onClick={() => setDialog({ kind: 'view', e })}>
                        <Eye size={13} />
                      </button>
                    )}
                    <button className="key key-flat key-sm key-icon" aria-label={`Rename ${e.name}`} title="Rename" onClick={() => setDialog({ kind: 'rename', e })}>
                      <Pencil size={13} />
                    </button>
                  </span>
                </li>
              ))}
            </ul>
          )}
        </div>
        {list && list.total > entries.length && <p className="text-xs text-muted">Showing the first {entries.length} of {list.total} items.</p>}
      </div>

      {dialog?.kind === 'mkdir' && path && <NameDialog title="New folder" label="Folder name" action="Create" onClose={() => setDialog(null)} onSubmit={async (n) => { await api.filesMkdir(path.replace(/\/$/, '') + '/' + n); refresh() }} />}
      {dialog?.kind === 'rename' && <NameDialog title="Rename" label="New name" initial={dialog.e.name} action="Rename" onClose={() => setDialog(null)} onSubmit={async (n) => { await api.filesRename(dialog.e.path, n); refresh() }} />}
      {dialog?.kind === 'delete' && <DeleteDialog paths={dialog.paths} onClose={() => setDialog(null)} onDone={() => refresh()} />}
      {dialog?.kind === 'edit' && <EditDialog e={dialog.e} onClose={() => setDialog(null)} onSaved={refresh} />}
      {dialog?.kind === 'view' && <ViewDialog e={dialog.e} onClose={() => setDialog(null)} onEdit={() => setDialog({ kind: 'edit', e: dialog.e })} />}
      {dialog?.kind === 'advanced' && info && <AccessDialog info={info} onClose={() => setDialog(null)} onChanged={async () => { const i = await loadInfo(); if (path && !i.full && !i.roots.some((r) => path === r.path || path.startsWith(r.path + '/'))) open(i.roots[0].path); else refresh() }} />}
      {dialog?.kind === 'folders' && path && <SizesDialog path={path} onClose={() => setDialog(null)} onOpen={(p) => { setDialog(null); open(p) }} />}
      {dialog?.kind === 'move' && path && <NameDialog title={dialog.copy ? 'Copy to…' : 'Move to…'} label="Destination folder (full path)" initial={path} action={dialog.copy ? 'Copy' : 'Move'} onClose={() => setDialog(null)} onSubmit={async (to) => { await api.filesMove(dialog.paths, to, dialog.copy); refresh() }} />}
    </section>
  )
}

function NameDialog({ title, label, initial = '', action, onClose, onSubmit }: { title: string; label: string; initial?: string; action: string; onClose: () => void; onSubmit: (v: string) => Promise<void> }) {
  const [v, setV] = useState(initial)
  const [err, setErr] = useState('')
  const [busy, setBusy] = useState(false)
  const go = async () => {
    setBusy(true)
    setErr('')
    try {
      await onSubmit(v.trim())
      onClose()
    } catch (e: any) {
      setErr(e.message)
      setBusy(false)
    }
  }
  return (
    <Modal open onClose={onClose} title={title} footer={<button className="key key-primary" disabled={busy || !v.trim()} onClick={go}>{action}</button>}>
      <label className="etch mb-1 block">{label}</label>
      <input className="field w-full" value={v} autoFocus onChange={(e) => setV(e.target.value)} onKeyDown={(e) => e.key === 'Enter' && v.trim() && go()} />
      {err && <ErrorLine className="mt-3">{err}</ErrorLine>}
    </Modal>
  )
}

function DeleteDialog({ paths, onClose, onDone }: { paths: string[]; onClose: () => void; onDone: () => void }) {
  const [err, setErr] = useState('')
  const [busy, setBusy] = useState(false)
  return (
    <Modal open onClose={onClose} title="Delete permanently?" footer={
      <button className="key key-danger" disabled={busy} onClick={async () => {
        setBusy(true)
        try {
          await api.filesDelete(paths)
          onDone()
          onClose()
        } catch (e: any) {
          setErr(e.message)
          setBusy(false)
        }
      }}>
        <Trash2 size={14} /> Delete {paths.length} item{paths.length === 1 ? '' : 's'}
      </button>
    }>
      <p className="text-sm">There is no trash on the server, so this can't be undone.</p>
      <ul className="well mt-3 max-h-40 overflow-auto p-2 font-mono text-xs">
        {paths.map((p) => <li key={p} className="break-all">{p}</li>)}
      </ul>
      {err && <ErrorLine className="mt-3">{err}</ErrorLine>}
    </Modal>
  )
}

function EditDialog({ e, onClose, onSaved }: { e: FileEntry; onClose: () => void; onSaved: () => void }) {
  const [text, setText] = useState<string | null>(null)
  const [orig, setOrig] = useState('')
  const [err, setErr] = useState('')
  const [busy, setBusy] = useState(false)
  useEffect(() => {
    api.filesRead(e.path).then((r) => { setText(r.content); setOrig(r.content) }).catch((x) => setErr(x.message))
  }, [e.path])
  return (
    <Modal open wide onClose={onClose} title={e.name} label="Edit file" footer={
      <>
        <a className="key" href={api.filesDownloadUrl(e.path)}><Download size={14} /> Download</a>
        <button className="key key-primary" disabled={busy || text === null || text === orig} onClick={async () => {
          setBusy(true)
          try {
            await api.filesWrite(e.path, text!)
            toast.ok('Saved')
            onSaved()
            onClose()
          } catch (x: any) {
            setErr(x.message)
            setBusy(false)
          }
        }}>Save</button>
      </>
    }>
      {err && <ErrorLine className="mb-3">{err}</ErrorLine>}
      {text === null && !err ? <Spinner /> : text !== null && <textarea className="field tape h-[22rem] w-full resize-none font-mono text-xs" spellCheck={false} value={text} onChange={(x) => setText(x.target.value)} />}
      <p className="mt-2 break-all font-mono text-[0.6875rem] text-muted">{e.path}</p>
    </Modal>
  )
}

function ViewDialog({ e, onClose, onEdit }: { e: FileEntry; onClose: () => void; onEdit: () => void }) {
  const url = api.filesDownloadUrl(e.path, true)
  const media = IMG.test(e.name) || VIDEO.test(e.name) || AUDIO.test(e.name) || PDF.test(e.name)
  const [text, setText] = useState<string | null>(null)
  const [err, setErr] = useState('')
  const textable = !media && e.size <= 2 << 20
  useEffect(() => {
    if (!textable) return
    api.filesRead(e.path).then((r) => setText(r.content)).catch((x) => setErr(x.message))
  }, [e.path, textable])
  return (
    <Modal open wide onClose={onClose} title={e.name} label="View" footer={
      <>
        <a className="key" href={api.filesDownloadUrl(e.path)}><Download size={14} /> Download</a>
        {text !== null && <button className="key key-primary" onClick={onEdit}><Pencil size={14} /> Edit</button>}
      </>
    }>
      {IMG.test(e.name) && <img src={url} alt={e.name} className="mx-auto max-h-[65vh] max-w-full object-contain" />}
      {VIDEO.test(e.name) && <video src={url} controls autoPlay className="mx-auto max-h-[65vh] w-full bg-black" />}
      {AUDIO.test(e.name) && <audio src={url} controls autoPlay className="w-full" />}
      {PDF.test(e.name) && <iframe src={url} title={e.name} className="h-[65vh] w-full rounded border border-rule bg-white" />}
      {textable && text === null && !err && <Spinner />}
      {textable && text !== null && <pre className="well tape h-[22rem] overflow-auto whitespace-pre-wrap break-all p-3 font-mono text-xs">{text}</pre>}
      {err && (
        <div className="text-sm text-ink-2">
          <p>{err}</p>
          <p className="mt-2">Use Download to open it on your computer.</p>
        </div>
      )}
      {!media && !textable && !err && <p className="text-sm text-ink-2">This file is too large to show here ({formatBytes(e.size)}). Use Download.</p>}
      <p className="mt-3 break-all font-mono text-[0.6875rem] text-muted">{e.path}</p>
    </Modal>
  )
}

function SizesDialog({ path, onClose, onOpen }: { path: string; onClose: () => void; onOpen: (p: string) => void }) {
  const [res, setRes] = useState<{ name: string; path: string; bytes: number }[] | null>(null)
  const [err, setErr] = useState('')
  useEffect(() => {
    api.cleanupSizes(path).then((r) => setRes(r.items)).catch((e) => setErr(e.message))
  }, [path])
  const max = Math.max(1, ...(res || []).map((r) => r.bytes))
  return (
    <Modal open wide onClose={onClose} title="What is using space here?" label="Folder sizes">
      <p className="mb-3 break-all font-mono text-xs text-muted">{path}</p>
      {err && <ErrorLine>{err}</ErrorLine>}
      {!res && !err && <div className="flex items-center gap-3 text-sm text-ink-2"><Spinner /> Measuring… big folders take a minute.</div>}
      <ul className="space-y-2">
        {res?.map((r) => (
          <li key={r.path}>
            <button className="w-full text-left" onClick={() => onOpen(r.path)}>
              <span className="flex justify-between text-sm"><span className="truncate">{r.name}</span><span className="ml-3 font-mono text-xs text-muted">{formatBytes(r.bytes)}</span></span>
              <span className="mt-1 block h-1.5 overflow-hidden rounded bg-unlit"><span className="block h-full bg-accent" style={{ width: `${(r.bytes / max) * 100}%` }} /></span>
            </button>
          </li>
        ))}
      </ul>
    </Modal>
  )
}

function AccessDialog({ info, onClose, onChanged }: { info: FilesInfo; onClose: () => void; onChanged: () => Promise<void> }) {
  const [step, setStep] = useState<'main' | 'risk'>('main')
  const [ack, setAck] = useState(false)
  const [add, setAdd] = useState('')
  const [err, setErr] = useState('')
  const [i, setI] = useState(info)
  const apply = async (body: Parameters<typeof api.filesAccess>[0]) => {
    setErr('')
    try {
      setI(await api.filesAccess(body))
      await onChanged()
      return true
    } catch (e: any) {
      setErr(e.message)
      return false
    }
  }
  if (step === 'risk') {
    return (
      <Modal open onClose={onClose} title={<span className="flex items-center gap-2 text-bad"><ShieldAlert size={18} /> Turn on Advanced access?</span>} footer={
        <>
          <button className="key" onClick={() => setStep('main')}>Back</button>
          <button className="key key-danger" disabled={!ack} onClick={async () => { if (await apply({ full: true, accept: true })) onClose() }}>Give full access</button>
        </>
      }>
        <div className="space-y-3 text-sm">
          <p>The file explorer will be able to open <b>every file on this server</b>, not only your apps' folders.</p>
          <ul className="list-disc space-y-1 pl-5 text-ink-2">
            <li>Haven runs with full control of the server. Anyone signed in to Haven could read passwords, keys and personal files.</li>
            <li>Deleting or editing the wrong system file can stop the server from starting.</li>
            <li>Important system folders such as <span className="font-mono">/etc</span>, <span className="font-mono">/usr</span> and <span className="font-mono">/home</span> are still protected from being deleted or renamed.</li>
          </ul>
          <label className="flex items-start gap-3">
            <input type="checkbox" className="mt-1 h-4 w-4 accent-[var(--accent)]" checked={ack} onChange={(e) => setAck(e.target.checked)} />
            <span>I understand the risk and want to give Haven's file explorer full access.</span>
          </label>
          {err && <ErrorLine>{err}</ErrorLine>}
        </div>
      </Modal>
    )
  }
  return (
    <Modal open onClose={onClose} title="File access" footer={<button className="key key-primary" onClick={onClose}>Done</button>}>
      <div className="space-y-4 text-sm">
        <div className="well p-3">
          <div className="flex items-center justify-between gap-3">
            <div>
              <div className="flex items-center gap-2 font-semibold">{i.full ? <ShieldAlert size={15} className="text-bad" /> : <Shield size={15} className="text-ok" />} {i.full ? 'Advanced: whole disk' : 'Safe folders only'}</div>
              <p className="mt-1 text-ink-2">{i.full ? 'The explorer can open any folder on the server.' : 'The explorer opens only the folders below.'}</p>
            </div>
            {i.full ? <button className="key key-sm" onClick={() => apply({ full: false })}>Go back to safe</button> : <button className="key key-sm key-danger" onClick={() => setStep('risk')}>Advanced…</button>}
          </div>
        </div>
        <div>
          <div className="etch mb-2">Allowed folders</div>
          <ul className="space-y-1">
            {i.roots.filter((r) => r.path !== '/').map((r) => (
              <li key={r.path} className="flex items-center justify-between gap-2 font-mono text-xs">
                <span className="break-all">{r.path}</span>
                {r.extra && <button className="key key-flat key-sm key-icon" aria-label={`Remove ${r.path}`} onClick={() => apply({ remove: r.path })}><X size={13} /></button>}
              </li>
            ))}
          </ul>
          <div className="mt-3 flex gap-2">
            <input className="field min-w-0 flex-1" placeholder="Add a folder, e.g. /mnt/usb" value={add} onChange={(e) => setAdd(e.target.value)} onKeyDown={(e) => e.key === 'Enter' && add.trim() && apply({ add: add.trim() }).then((ok) => ok && setAdd(''))} />
            <button className="key" disabled={!add.trim()} onClick={() => apply({ add: add.trim() }).then((ok) => ok && setAdd(''))}>Add</button>
          </div>
        </div>
        {err && <ErrorLine>{err}</ErrorLine>}
      </div>
    </Modal>
  )
}
