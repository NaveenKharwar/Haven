import { useState } from 'react'
import { Crown, KeyRound, Trash2, UserPlus } from 'lucide-react'
import { api, type Person, type Role } from './api'
import { AppIcon, ErrorLine, Modal, Spinner, cx, toast, usePoll } from './components'
import { useHaven } from './context'

const ROLES: { id: Exclude<Role, 'admin'>; label: string; blurb: string }[] = [
  { id: 'manager', label: 'Manager', blurb: 'Installs and manages apps, uses Files in the safe folders. No server settings or people.' },
  { id: 'member', label: 'Member', blurb: 'Sees and opens the apps you pick, and can restart them. Changes nothing else.' },
  { id: 'guest', label: 'Guest', blurb: 'Only sees and opens the apps you pick. Nothing else.' },
]
const roleWord: Record<Role, string> = { admin: 'Admin', manager: 'Manager', member: 'Member', guest: 'Guest' }

/** Settings > People: the admin adds people and chooses what they can reach. */
export default function PeoplePanel() {
  const { user } = useHaven()
  const { data, refresh } = usePoll(async () => (await api.people()).people, 20000)
  const [dialog, setDialog] = useState<null | { kind: 'add' } | { kind: 'edit'; p: Person } | { kind: 'reset'; p: Person } | { kind: 'remove'; p: Person } | { kind: 'admin'; p: Person }>(null)
  const people = data || []
  const done = () => {
    setDialog(null)
    refresh()
  }
  return (
    <section className="plate overflow-hidden" aria-label="People">
      <div className="plate-head">
        <h2 className="etch">People</h2>
        <button className="key key-sm key-primary" onClick={() => setDialog({ kind: 'add' })}>
          <UserPlus size={13} /> Add person
        </button>
      </div>
      <div className="space-y-4 p-4 sm:p-5">
        <p className="text-sm text-ink-2">
          Give people their own sign-in. They reach Haven over Tailscale (share this server with them there), then sign in here. There is only ever one admin: you.
        </p>
        {!data && <Spinner />}
        <ul className="divide-y divide-rule-2">
          {people.map((p) => (
            <li key={p.username} className="flex flex-wrap items-center gap-3 py-3">
              <span className="min-w-0 flex-1">
                <span className="flex flex-wrap items-center gap-2">
                  <span className="font-semibold">{p.username}</span>
                  {p.username.toLowerCase() === user.toLowerCase() && <span className="text-xs text-muted">(you)</span>}
                  <span className={cx('tag', p.role === 'admin' && 'tag-accent')}>{p.role === 'admin' && <Crown size={11} className="mr-1" />}{roleWord[p.role]}</span>
                  {p.disabled && <span className="tag text-bad">Disabled</span>}
                </span>
                {(p.role === 'member' || p.role === 'guest') && (
                  <span className="mt-0.5 block text-xs text-muted">{p.apps.length === 0 ? 'No apps shared yet' : `${p.apps.length} app${p.apps.length === 1 ? '' : 's'}: ${p.apps.join(', ')}`}</span>
                )}
              </span>
              {p.role !== 'admin' && (
                <span className="flex flex-wrap gap-1.5">
                  <button className="key key-sm" onClick={() => setDialog({ kind: 'edit', p })}>Edit</button>
                  <button className="key key-sm key-icon" aria-label={`Reset password for ${p.username}`} title="Reset password" onClick={() => setDialog({ kind: 'reset', p })}><KeyRound size={13} /></button>
                  <button className="key key-sm key-icon" aria-label={`Make ${p.username} the admin`} title="Make admin" disabled={p.disabled} onClick={() => setDialog({ kind: 'admin', p })}><Crown size={13} /></button>
                  <button className="key key-sm key-icon key-danger" aria-label={`Remove ${p.username}`} title="Remove" onClick={() => setDialog({ kind: 'remove', p })}><Trash2 size={13} /></button>
                </span>
              )}
            </li>
          ))}
        </ul>
      </div>
      {dialog?.kind === 'add' && <PersonDialog onClose={() => setDialog(null)} onDone={done} />}
      {dialog?.kind === 'edit' && <PersonDialog person={dialog.p} onClose={() => setDialog(null)} onDone={done} />}
      {dialog?.kind === 'reset' && <ResetDialog p={dialog.p} onClose={() => setDialog(null)} onDone={done} />}
      {dialog?.kind === 'remove' && <RemoveDialog p={dialog.p} onClose={() => setDialog(null)} onDone={done} />}
      {dialog?.kind === 'admin' && <AdminDialog p={dialog.p} onClose={() => setDialog(null)} />}
    </section>
  )
}

function AppPicker({ value, onChange }: { value: string[]; onChange: (v: string[]) => void }) {
  const { apps } = useHaven()
  if (apps.length === 0) return <p className="text-xs text-muted">No apps are installed yet. You can share apps with this person later.</p>
  return (
    <ul className="well max-h-48 divide-y divide-rule-2 overflow-auto" aria-label="Apps this person can see">
      {apps.map((a) => (
        <li key={a.id}>
          <label className="flex cursor-pointer items-center gap-3 px-3 py-2">
            <input type="checkbox" className="h-4 w-4 accent-[var(--accent)]" checked={value.includes(a.id)} onChange={(e) => onChange(e.target.checked ? [...value, a.id] : value.filter((x) => x !== a.id))} />
            <AppIcon src={a.icon} name={a.name} size={22} />
            <span className="text-sm">{a.name}</span>
          </label>
        </li>
      ))}
    </ul>
  )
}

function PersonDialog({ person, onClose, onDone }: { person?: Person; onClose: () => void; onDone: () => void }) {
  const [username, setUsername] = useState(person?.username || '')
  const [password, setPassword] = useState('')
  const [role, setRole] = useState<Exclude<Role, 'admin'>>((person?.role as Exclude<Role, 'admin'>) || 'member')
  const [apps, setApps] = useState<string[]>(person?.apps || [])
  const [disabled, setDisabled] = useState(person?.disabled || false)
  const [err, setErr] = useState('')
  const [busy, setBusy] = useState(false)
  const limited = role === 'member' || role === 'guest'
  const save = async () => {
    setBusy(true)
    setErr('')
    try {
      if (person) await api.updatePerson(person.username, { role, apps: limited ? apps : [], disabled })
      else await api.addPerson({ username: username.trim(), password, role, apps: limited ? apps : [] })
      toast.ok(person ? 'Saved' : `Added ${username.trim()}`)
      onDone()
    } catch (e: any) {
      setErr(e.message)
      setBusy(false)
    }
  }
  return (
    <Modal open wide onClose={onClose} title={person ? `Edit ${person.username}` : 'Add a person'} label="People" footer={<button className="key key-primary" disabled={busy || (!person && (!username.trim() || password.length < 8))} onClick={save}>{person ? 'Save' : 'Add person'}</button>}>
      <div className="space-y-4">
        {!person && (
          <div className="grid gap-4 sm:grid-cols-2">
            <div>
              <label className="etch mb-1 block" htmlFor="pn">Username</label>
              <input id="pn" className="field w-full" autoFocus autoComplete="off" value={username} onChange={(e) => setUsername(e.target.value)} />
            </div>
            <div>
              <label className="etch mb-1 block" htmlFor="pp">Password (8+ characters)</label>
              <input id="pp" type="text" className="field w-full font-mono" autoComplete="off" value={password} onChange={(e) => setPassword(e.target.value)} />
            </div>
          </div>
        )}
        <fieldset>
          <legend className="etch mb-2">Role</legend>
          <div className="space-y-2">
            {ROLES.map((r) => (
              <label key={r.id} className={cx('flex cursor-pointer items-start gap-3 rounded-[3px] border p-3', role === r.id ? 'border-accent bg-accent-wash' : 'border-rule')}>
                <input type="radio" name="role" className="mt-1 accent-[var(--accent)]" checked={role === r.id} onChange={() => setRole(r.id)} />
                <span><span className="block text-sm font-semibold">{r.label}</span><span className="block text-xs text-ink-2">{r.blurb}</span></span>
              </label>
            ))}
          </div>
        </fieldset>
        {limited && (
          <div>
            <div className="etch mb-2">Apps they can see</div>
            <AppPicker value={apps} onChange={setApps} />
          </div>
        )}
        {person && (
          <label className="flex items-center gap-3 text-sm">
            <input type="checkbox" className="h-4 w-4 accent-[var(--accent)]" checked={disabled} onChange={(e) => setDisabled(e.target.checked)} />
            Disabled (signs them out everywhere and blocks sign-in)
          </label>
        )}
        {err && <ErrorLine>{err}</ErrorLine>}
      </div>
    </Modal>
  )
}

function ResetDialog({ p, onClose, onDone }: { p: Person; onClose: () => void; onDone: () => void }) {
  const [pw, setPw] = useState('')
  const [err, setErr] = useState('')
  return (
    <Modal open onClose={onClose} title={`New password for ${p.username}`} footer={
      <button className="key key-primary" disabled={pw.length < 8} onClick={async () => {
        try {
          await api.resetPerson(p.username, pw)
          toast.ok(`Password changed. ${p.username} was signed out everywhere.`)
          onDone()
        } catch (e: any) {
          setErr(e.message)
        }
      }}>Set password</button>
    }>
      <label className="etch mb-1 block" htmlFor="rp">New password (8+ characters)</label>
      <input id="rp" className="field w-full font-mono" autoFocus autoComplete="off" value={pw} onChange={(e) => setPw(e.target.value)} />
      {err && <ErrorLine className="mt-3">{err}</ErrorLine>}
    </Modal>
  )
}

function RemoveDialog({ p, onClose, onDone }: { p: Person; onClose: () => void; onDone: () => void }) {
  const [err, setErr] = useState('')
  return (
    <Modal open onClose={onClose} title={`Remove ${p.username}?`} footer={
      <button className="key key-danger" onClick={async () => {
        try {
          await api.deletePerson(p.username)
          onDone()
        } catch (e: any) {
          setErr(e.message)
        }
      }}><Trash2 size={14} /> Remove</button>
    }>
      <p className="text-sm">{p.username} can no longer sign in, and any phone, computer or TV they use is signed out. This can't be undone.</p>
      {err && <ErrorLine className="mt-3">{err}</ErrorLine>}
    </Modal>
  )
}

function AdminDialog({ p, onClose }: { p: Person; onClose: () => void }) {
  const [pw, setPw] = useState('')
  const [err, setErr] = useState('')
  return (
    <Modal open onClose={onClose} title={`Make ${p.username} the admin?`} footer={
      <button className="key key-danger" disabled={!pw} onClick={async () => {
        try {
          await api.transferAdmin(p.username, pw)
          window.location.reload()
        } catch (e: any) {
          setErr(e.message)
        }
      }}>Hand over admin</button>
    }>
      <div className="space-y-3 text-sm">
        <p>{p.username} becomes the one admin, with full control of the server and everyone's access. You become a Manager and can't undo this yourself.</p>
        <label className="etch block" htmlFor="ap">Your password</label>
        <input id="ap" type="password" className="field w-full" autoFocus autoComplete="current-password" value={pw} onChange={(e) => setPw(e.target.value)} />
        {err && <ErrorLine>{err}</ErrorLine>}
      </div>
    </Modal>
  )
}
