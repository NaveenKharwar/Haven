import { useCallback, useEffect, useRef, useState } from 'react'
import { ArrowUp, ChevronRight, Copy, Download, Eye, File as FileIcon, Folder, FolderPlus, LayoutGrid, List, Pencil, Shield, ShieldAlert, Trash2, Upload, X } from 'lucide-react'
import { api, formatBytes, type FileEntry, type FileListing, type FilesInfo } from './api'
import { ErrorLine, Modal, Spinner, cx, toast } from './components'
import { UploadQueue, useFileUploads } from './fileuploads'

const IMG = /\.(png|jpe?g|gif|webp|svg|bmp)$/i
const VIDEO = /\.(mp4|m4v|webm|ogv|mov|mkv)$/i
const AUDIO = /\.(mp3|m4a|wav|ogg|oga|flac|opus)$/i
const PDF = /\.pdf$/i

function when(iso: string): string {
  const d = new Date(iso)
  return d.toLocaleDateString(undefined, { day: 'numeric', month: 'short', year: 'numeric' })
}

type FileView = 'list' | 'grid'
const viewKey = 'haven:files-view'

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
  const [view, setView] = useState<FileView>(() => localStorage.getItem(viewKey) === 'grid' ? 'grid' : 'list')
  const fileInput = useRef<HTMLInputElement>(null)
  const currentPath = useRef<string | null>(null)
  const requestedPath = useRef<string | null>(null)
  const loadSequence = useRef(0)

  const loadInfo = useCallback(async () => {
    const i = await api.filesInfo()
    setInfo(i)
    return i
  }, [])
  const open = useCallback(async (p: string, preserveSelection = false) => {
    const sequence = ++loadSequence.current
    requestedPath.current = p
    setBusy(true)
    setError('')
    try {
      const l = await api.filesList(p)
      if (loadSequence.current !== sequence) return
      currentPath.current = l.path
      setList(l)
      setPath(l.path)
      setPicked((previous) => preserveSelection
        ? new Set(l.entries?.filter((e) => previous.has(e.path)).map((e) => e.path))
        : new Set())
    } catch (e) {
      if (loadSequence.current === sequence) setError((e as Error).message)
    } finally {
      if (loadSequence.current === sequence) {
        requestedPath.current = null
        setBusy(false)
      }
    }
  }, [])

  useEffect(() => {
    let active = true
    loadInfo()
      .then((i) => {
        if (!active) return
        const start = initialPath || i.roots.find((r) => r.name === 'App data')?.path || i.roots[0]?.path
        if (start) void open(start)
      })
      .catch((e) => { if (active) setError((e as Error).message) })
    return () => {
      active = false
      ++loadSequence.current
    }
  }, [loadInfo, open, initialPath])

  const refresh = () => {
    if (currentPath.current && !requestedPath.current) void open(currentPath.current, true)
  }
  const uploads = useFileUploads((destination) => {
    if (destination === currentPath.current) refresh()
  })
  const changeView = (next: FileView) => {
    setView(next)
    localStorage.setItem(viewKey, next)
  }
  const upload = (files: FileList) => {
    if (path && !busy) uploads.enqueue(path, files)
  }

  const entries = list?.entries || []
  const selectedEntry = picked.size === 1 ? entries.find((entry) => picked.has(entry.path)) : undefined
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

        {path && <FileBreadcrumbs path={path} parent={list?.parent} roots={info?.roots || []} onOpen={open} />}

        <div className="flex flex-wrap items-center justify-between gap-2">
          <div className="flex flex-wrap items-center gap-2">
            <button className="key key-sm" disabled={!list?.parent || busy} onClick={() => list?.parent && open(list.parent)}>
              <ArrowUp size={13} /> Up
            </button>
            <button className="key key-sm" disabled={!path || busy || uploads.active} onClick={() => fileInput.current?.click()}>
              <Upload size={13} /> Upload
            </button>
            <input ref={fileInput} type="file" multiple hidden onChange={({ currentTarget }) => {
              if (currentTarget.files) upload(currentTarget.files)
              currentTarget.value = ''
            }} />
            <button className="key key-sm" disabled={!path || busy} onClick={() => setDialog({ kind: 'mkdir' })}>
              <FolderPlus size={13} /> New folder
            </button>
            {admin && <button className="key key-sm key-flat" disabled={!path || busy} onClick={() => setDialog({ kind: 'folders' })}>Folder sizes</button>}
          </div>
          <div className="well inline-flex gap-0.5 p-0.5" role="group" aria-label="File view">
            <button className={cx('key key-sm gap-1.5', view !== 'list' && 'key-flat')} aria-pressed={view === 'list'} onClick={() => changeView('list')}><List size={15} /> List</button>
            <button className={cx('key key-sm gap-1.5', view !== 'grid' && 'key-flat')} aria-pressed={view === 'grid'} onClick={() => changeView('grid')}><LayoutGrid size={14} /> Grid</button>
          </div>
        </div>

        {picked.size > 0 && (
          <div className="flex flex-wrap items-center gap-2 border-y border-rule py-2">
            <span className="mr-1 text-sm font-medium" role="status">{picked.size} selected</span>
            {selectedEntry && <button className="key key-sm" disabled={busy} onClick={() => activate(selectedEntry)}>Open</button>}
            <button className="key key-sm" disabled={busy} onClick={() => setDialog({ kind: 'move', paths: [...picked], copy: false })}>Move</button>
            <button className="key key-sm" disabled={busy} onClick={() => setDialog({ kind: 'move', paths: [...picked], copy: true })}><Copy size={13} /> Copy</button>
            <button className="key key-sm key-danger" disabled={busy} onClick={() => setDialog({ kind: 'delete', paths: [...picked] })}><Trash2 size={13} /> Delete</button>
            <button className="key key-sm key-flat ml-auto" onClick={() => setPicked(new Set())}>Clear selection</button>
          </div>
        )}

        <UploadQueue queue={uploads} path={path} />
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
          className="well min-h-[12rem]"
          aria-busy={busy}
          onDragOver={(e) => {
            if (e.dataTransfer.types.includes('Files')) {
              e.preventDefault()
              e.dataTransfer.dropEffect = busy || uploads.active || !path ? 'none' : 'copy'
            }
          }}
          onDrop={(e) => {
            e.preventDefault()
            if (e.dataTransfer.files.length) upload(e.dataTransfer.files)
          }}
        >
          {!list && error ? (
            <p className="p-8 text-center text-sm text-muted">Pick a location above to continue.</p>
          ) : !list ? (
            <div className="grid place-items-center p-10" role="status"><Spinner /><span className="sr-only">Loading files</span></div>
          ) : entries.length === 0 ? (
            <p className="p-8 text-center text-sm text-muted">This folder is empty. Drop files here to upload them.</p>
          ) : (
            <>
              {view === 'list' && (
                <div className="file-list-heading hidden items-center gap-3 border-b border-rule px-4 py-2 md:grid" aria-hidden="true">
                  <span className="etch">Name</span><span className="etch text-right">Size</span><span className="etch text-right">Modified</span><span />
                </div>
              )}
              <ul className={view === 'grid' ? 'file-tiles grid' : 'divide-y divide-rule-2'} aria-label="Files and folders">
                {entries.map((e) => {
                  const selected = picked.has(e.path)
                  const Icon = e.dir ? Folder : FileIcon
                  return (
                    <li key={e.path} className={cx('file-entry relative min-w-0', view === 'grid' ? 'file-tile border-b border-r border-rule' : 'file-row flex items-center gap-2 px-3', selected && 'file-entry-selected')}>
                      <input type="checkbox" aria-label={`Select ${e.name}`} checked={selected} disabled={busy} onChange={() => toggle(e.path)} className={cx('h-4 w-4 shrink-0 accent-[var(--accent)]', view === 'grid' && 'absolute left-3 top-3 z-10')} />
                      <button
                        className={cx('file-entry-key min-w-0 select-none text-left', view === 'grid' ? 'flex w-full flex-col items-start gap-2 px-3 pb-2 pt-9' : 'grid flex-1 items-center gap-3 py-2')}
                        disabled={busy}
                        aria-label={e.name}
                        title={`${e.name} · ${e.dir ? 'Folder' : formatBytes(e.size)} · ${when(e.mod)}`}
                        onClick={(event) => {
                          if (event.detail === 0) {
                            void activate(e)
                          }
                        }}
                        onDoubleClick={() => void activate(e)}
                      >
                        <span className={cx('flex min-w-0 gap-2', view === 'grid' ? 'w-full flex-col' : 'items-center')}>
                          <Icon size={view === 'grid' ? 32 : 17} className={cx('shrink-0', e.dir ? 'text-accent' : 'text-muted')} />
                          <span className="block min-w-0 truncate text-sm font-medium">{e.name}{e.link && <span className="ml-2 text-xs text-muted">link</span>}</span>
                        </span>
                        <span className={cx('font-mono text-xs text-muted', view === 'grid' ? 'block' : 'hidden text-right md:block')}>{e.dir ? (view === 'grid' ? 'Folder' : '—') : formatBytes(e.size)}</span>
                        {view === 'list' && <span className="hidden text-right text-xs text-muted md:block">{when(e.mod)}</span>}
                      </button>
                      <div className={cx('flex shrink-0 justify-end gap-0.5', view === 'grid' ? 'file-tile-actions px-2 pb-1' : 'w-[5.75rem]')}>
                        {!e.dir && (
                          <a className="key key-flat key-sm key-icon" href={api.filesDownloadUrl(e.path)} aria-label={`Download ${e.name}`} title="Download"><Download size={14} /></a>
                        )}
                        {!e.dir && (
                          <button className="key key-flat key-sm key-icon" disabled={busy} aria-label={`Preview ${e.name}`} title="Preview" onClick={() => void activate(e)}><Eye size={14} /></button>
                        )}
                        <button className="key key-flat key-sm key-icon" disabled={busy} aria-label={`Rename ${e.name}`} title="Rename" onClick={() => setDialog({ kind: 'rename', e })}><Pencil size={14} /></button>
                      </div>
                    </li>
                  )
                })}
              </ul>
            </>
          )}
        </div>
        {list && <p className="text-xs text-muted">{list.total > entries.length ? `Showing ${entries.length} of ${list.total} items` : `${list.total} ${list.total === 1 ? 'item' : 'items'}`}</p>}
      </div>

      {dialog?.kind === 'mkdir' && path && <NameDialog title="New folder" label="Folder name" action="Create" onClose={() => setDialog(null)} onSubmit={async (n) => { await api.filesMkdir(path.replace(/\/$/, '') + '/' + n); refresh() }} />}
      {dialog?.kind === 'rename' && <NameDialog title="Rename" label="New name" initial={dialog.e.name} action="Rename" onClose={() => setDialog(null)} onSubmit={async (n) => { await api.filesRename(dialog.e.path, n); refresh() }} />}
      {dialog?.kind === 'delete' && <DeleteDialog paths={dialog.paths} onClose={() => setDialog(null)} onDone={() => refresh()} />}
      {dialog?.kind === 'edit' && <EditDialog e={dialog.e} onClose={() => setDialog(null)} onSaved={refresh} />}
      {dialog?.kind === 'view' && <ViewDialog e={dialog.e} onClose={() => setDialog(null)} onEdit={() => setDialog({ kind: 'edit', e: dialog.e })} />}
      {dialog?.kind === 'advanced' && info && <AccessDialog info={info} onClose={() => setDialog(null)} onChanged={async () => { const i = await loadInfo(); if (path && !i.full && !i.roots.some((r) => path === r.path || path.startsWith(r.path + '/'))) open(i.roots[0].path); else refresh() }} />}
      {dialog?.kind === 'folders' && path && <SizesDialog path={path} onClose={() => setDialog(null)} onOpen={(p) => { setDialog(null); open(p) }} />}
      {dialog?.kind === 'move' && path && info && (
        <DestinationDialog
          locations={info.roots}
          initial={path}
          paths={dialog.paths}
          copy={dialog.copy}
          onClose={() => setDialog(null)}
          onSubmit={async (destination) => {
            try {
              await api.filesMove(dialog.paths, destination, dialog.copy)
            } finally {
              refresh()
            }
          }}
        />
      )}
    </section>
  )
}

function FileBreadcrumbs({ path, parent, roots, onOpen }: {
  path: string
  parent?: string
  roots: FilesInfo['roots']
  onOpen: (p: string) => void
}) {
  const parts = crumbs(path)
  const root = roots.filter((r) => r.path === '/' || path === r.path || path.startsWith(r.path + '/'))
    .sort((a, b) => b.path.length - a.path.length)[0]
  const start = root ? parts.findIndex((part) => part.path === root.path) : -1
  const visible = start >= 0 ? [{ ...parts[start], name: root.name }, ...parts.slice(start + 1)] : parts.slice(parent ? -2 : -1)
  return (
    <nav className="flex min-w-0 flex-wrap items-center gap-1 break-all font-mono text-xs" aria-label="Folder path">
      {visible.slice(0, -1).map((part) => (
        <span key={part.path} className="flex min-w-0 items-center gap-1">
          <button className="min-w-0 px-1 py-1 text-muted hover:bg-raised hover:text-ink" onClick={() => onOpen(part.path)}>{part.name}</button>
          <ChevronRight size={12} className="shrink-0 text-muted" />
        </span>
      ))}
      <span aria-current="location" title={path} className="px-1 py-1 text-ink">{visible.at(-1)?.name}</span>
    </nav>
  )
}

function DestinationDialog({ locations, initial, paths, copy, onClose, onSubmit }: {
  locations: FilesInfo['roots']
  initial: string
  paths: string[]
  copy: boolean
  onClose: () => void
  onSubmit: (destination: string) => Promise<void>
}) {
  const [destination, setDestination] = useState(initial)
  const [list, setList] = useState<FileListing | null>(null)
  const [loadError, setLoadError] = useState('')
  const [actionError, setActionError] = useState('')
  const [saving, setSaving] = useState(false)
  const blocked = paths.some((path) => path === list?.path || list?.path.startsWith(path + '/'))
  const canSubmit = list && list.path !== initial && !blocked && !saving
  const action = copy ? 'Copy' : 'Move'

  useEffect(() => {
    let active = true
    api.filesList(destination)
      .then((loaded) => { if (active) setList(loaded) })
      .catch((error) => { if (active) setLoadError((error as Error).message) })
    return () => { active = false }
  }, [destination])

  const browse = (path: string) => {
    if (saving || path === destination) return
    setList(null)
    setLoadError('')
    setActionError('')
    setDestination(path)
  }

  return (
    <Modal open onClose={onClose} title={copy ? 'Copy to folder' : 'Move to folder'} footer={
      <button className="key key-primary" disabled={!canSubmit} onClick={async () => {
        if (!canSubmit) return
        setSaving(true)
        setActionError('')
        try {
          await onSubmit(list.path)
          onClose()
        } catch (e) {
          setActionError((e as Error).message)
          setSaving(false)
        }
      }}>{action} {paths.length} {paths.length === 1 ? 'item' : 'items'} here</button>
    }>
      <div className="flex flex-wrap gap-1.5" aria-label="Destination locations">
        {locations.map((location) => <button className="key key-sm" key={location.path} onClick={() => browse(location.path)} disabled={saving}>{location.name}</button>)}
      </div>
      <div className="my-3 flex items-start gap-2">
        <button className="key key-sm" disabled={!list?.parent || saving} onClick={() => list?.parent && browse(list.parent)}><ArrowUp size={14} /> Up</button>
        {list && <FileBreadcrumbs path={list.path} parent={list.parent} roots={locations} onOpen={browse} />}
      </div>
      {loadError ? <ErrorLine>{loadError}</ErrorLine> : !list ? <Spinner /> : (
        <ul className="well max-h-56 divide-y divide-rule-2 overflow-y-auto" aria-label="Destination folders">
          {list.entries?.filter((entry) => entry.dir).map((entry) => (
            <li key={entry.path}>
              <button className="flex w-full items-center gap-2 px-3 py-2 text-left text-sm hover:bg-raised" disabled={saving} onClick={() => browse(entry.path)}>
                <Folder size={15} className="shrink-0 text-accent" /><span className="min-w-0 flex-1 break-words">{entry.name}</span><ChevronRight size={14} className="shrink-0 text-muted" />
              </button>
            </li>
          ))}
          {!list.entries?.some((entry) => entry.dir) && <li className="px-3 py-4 text-sm text-muted">No folders here.</li>}
        </ul>
      )}
      {list?.path === initial && <p className="mt-2 text-sm text-muted">Choose a different folder.</p>}
      {blocked && <p className="mt-2 text-sm text-muted">Choose a location outside the selected folders.</p>}
      {actionError && <ErrorLine className="mt-3">{actionError}</ErrorLine>}
    </Modal>
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
