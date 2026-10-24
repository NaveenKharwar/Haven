import { useState, type FormEvent, type ReactNode } from 'react'
import { Eye, EyeOff, LoaderCircle } from 'lucide-react'
import { api } from './api'
import { ErrorLine, Led } from './components'

function Faceplate({ title, children, foot }: { title: string; children: ReactNode; foot?: ReactNode }) {
  return (
    <div className="flex min-h-screen items-center justify-center px-4 py-12">
      <div className="rise w-full max-w-[25rem]">
        <div className="plate">
          <div className="plate-head">
            <span className="flex items-center gap-3">
              <span className="grid h-5 w-5 place-items-center rounded-[2px] bg-accent">
                <span className="h-1.5 w-1.5 rounded-full bg-accent-ink" />
              </span>
              <span className="text-[0.95rem] font-bold tracking-[0.28em]">HAVEN</span>
            </span>
            <span className="flex items-center gap-2">
              <span className="vent" aria-hidden>
                <i />
                <i />
                <i />
              </span>
              <Led s="ok" />
            </span>
          </div>
          <div className="p-5 sm:p-6">
            <h1 className="mb-5 text-xl font-semibold tracking-tight">{title}</h1>
            {children}
          </div>
        </div>
        {foot && <div className="mt-4 text-center text-xs text-muted">{foot}</div>}
      </div>
    </div>
  )
}

export function PasswordInput({ value, onChange, autoComplete, placeholder, id }: { value: string; onChange: (v: string) => void; autoComplete: string; placeholder?: string; id?: string }) {
  const [show, setShow] = useState(false)
  return (
    <div className="relative">
      <input id={id} className="field pr-10" type={show ? 'text' : 'password'} value={value} onChange={(e) => onChange(e.target.value)} autoComplete={autoComplete} placeholder={placeholder} required />
      <button type="button" className="absolute inset-y-0 right-0 flex w-10 items-center justify-center text-muted hover:text-ink" onClick={() => setShow(!show)} aria-label={show ? 'Hide password' : 'Show password'}>
        {show ? <EyeOff size={15} /> : <Eye size={15} />}
      </button>
    </div>
  )
}

function Field({ label, htmlFor, hint, children }: { label: string; htmlFor: string; hint?: ReactNode; children: ReactNode }) {
  return (
    <div className="space-y-2">
      <label htmlFor={htmlFor} className="etch block">
        {label}
      </label>
      {children}
      {hint && <p className="text-xs text-muted">{hint}</p>}
    </div>
  )
}



export function Setup({ onDone }: { onDone: () => void }) {
  const [token, setToken] = useState('')
  const [username, setUsername] = useState('admin')
  const [password, setPassword] = useState('')
  const [confirm, setConfirm] = useState('')
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)

  const submit = async (e: FormEvent) => {
    e.preventDefault()
    setError('')
    if (password !== confirm) return setError('The two passwords are different.')
    setBusy(true)
    try {
      await api.setup(token, username, password)
      onDone()
    } catch (err) {
      setError((err as Error).message)
    } finally {
      setBusy(false)
    }
  }

  return (
    <Faceplate title="Set up this server" foot="The setup code is printed by the installer.">
      <form onSubmit={submit} className="space-y-4">
        <Field
          label="Setup code"
          htmlFor="token"
          hint={
            <>
              Lost it? Run <code className="font-mono text-ink-2">sudo cat /opt/haven/setup-token</code>
            </>
          }
        >
          <input id="token" className="field font-mono uppercase tracking-[0.3em]" value={token} onChange={(e) => setToken(e.target.value)} placeholder="XXXXXXXX" autoFocus required />
        </Field>
        <Field label="Username" htmlFor="user">
          <input id="user" className="field" value={username} onChange={(e) => setUsername(e.target.value)} autoComplete="username" required />
        </Field>
        <Field label="Password" htmlFor="pw">
          <PasswordInput id="pw" value={password} onChange={setPassword} autoComplete="new-password" placeholder="8 characters or more" />
        </Field>
        <Field label="Repeat password" htmlFor="pw2">
          <PasswordInput id="pw2" value={confirm} onChange={setConfirm} autoComplete="new-password" />
        </Field>
        <ErrorLine>{error}</ErrorLine>
        <button className="key key-primary w-full" disabled={busy} data-loading={busy}>
          {busy && <LoaderCircle size={15} className="spin" />} Create admin account
        </button>
      </form>
    </Faceplate>
  )
}

export function Login({ onDone, onChangeDevice }: { onDone: () => void; onChangeDevice?: () => void }) {
  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)

  const submit = async (e: FormEvent) => {
    e.preventDefault()
    setError('')
    setBusy(true)
    try {
      await api.login(username, password)
      onDone()
    } catch (err) {
      setError((err as Error).message)
    } finally {
      setBusy(false)
    }
  }

  return (
    <Faceplate
      title="Sign in"
      foot={
        onChangeDevice && (
          <button className="underline decoration-rule-hi underline-offset-4 hover:text-ink" onClick={onChangeDevice}>
            Not a computer? Choose TV mode
          </button>
        )
      }
    >
      <form onSubmit={submit} className="space-y-4">
        <Field label="Username" htmlFor="user">
          <input id="user" className="field" value={username} onChange={(e) => setUsername(e.target.value)} autoComplete="username" autoFocus required />
        </Field>
        <Field label="Password" htmlFor="pw">
          <PasswordInput id="pw" value={password} onChange={setPassword} autoComplete="current-password" />
        </Field>
        <ErrorLine>{error}</ErrorLine>
        <button className="key key-primary w-full" disabled={busy} data-loading={busy}>
          {busy && <LoaderCircle size={15} className="spin" />} Sign in
        </button>
      </form>
    </Faceplate>
  )
}
