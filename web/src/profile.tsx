import { useEffect, useRef, useState } from 'react'
import { ChevronDown, LogOut, Settings as Cog, Tv } from 'lucide-react'
import { Led, cx } from './components'

/** The signed-in account: an initial, a name, and a small menu. */
export function Avatar({ name, size = 32 }: { name: string; size?: number }) {
  return (
    <span className="relative inline-grid shrink-0 place-items-center rounded-[3px] border border-rule bg-accent-wash font-mono font-semibold text-accent" style={{ width: size, height: size, fontSize: size * 0.42 }} aria-hidden>
      {(name[0] || '?').toUpperCase()}
      <span className="absolute -bottom-[3px] -right-[3px] grid h-[11px] w-[11px] place-items-center rounded-full bg-paper leading-none">
        <Led s="ok" />
      </span>
    </span>
  )
}

const ROLE_LABEL: Record<string, string> = { admin: 'Administrator', manager: 'Manager', member: 'Member', guest: 'Guest' }

export default function ProfileMenu({ user, role, onSettings, onTv, onLogout }: { user: string; role: string; onSettings: () => void; onTv: () => void; onLogout: () => void }) {
  const [open, setOpen] = useState(false)
  const box = useRef<HTMLDivElement>(null)
  useEffect(() => {
    if (!open) return
    const away = (e: MouseEvent) => !box.current?.contains(e.target as Node) && setOpen(false)
    const esc = (e: KeyboardEvent) => e.key === 'Escape' && setOpen(false)
    document.addEventListener('mousedown', away)
    window.addEventListener('keydown', esc)
    return () => {
      document.removeEventListener('mousedown', away)
      window.removeEventListener('keydown', esc)
    }
  }, [open])
  const item = 'flex w-full items-center gap-2.5 px-3 py-2 text-left text-sm hover:bg-raised'
  return (
    <div ref={box} className="relative">
      <button className="key key-sm h-9 gap-2 pl-1 pr-2" onClick={() => setOpen(!open)} aria-haspopup="menu" aria-expanded={open} aria-label={`Account: ${user}`}>
        <Avatar name={user} size={26} />
        <span className="hidden max-w-[7rem] truncate md:inline">{user}</span>
        <ChevronDown size={13} className={cx('hidden text-muted transition-transform md:block', open && 'rotate-180')} />
      </button>
      {open && (
        <div role="menu" className="plate rise absolute right-0 top-full z-40 mt-2 w-60 overflow-hidden shadow-xl">
          <div className="flex items-center gap-3 border-b border-rule-2 px-3 py-3">
            <Avatar name={user} size={38} />
            <div className="min-w-0">
              <div className="truncate text-sm font-semibold">{user}</div>
              <div className="font-mono text-[0.6875rem] text-muted">{ROLE_LABEL[role] || role}</div>
            </div>
          </div>
          <button role="menuitem" className={item} onClick={() => { setOpen(false); onSettings() }}>
            <Cog size={14} className="text-muted" /> Settings
          </button>
          <button role="menuitem" className={item} onClick={() => { setOpen(false); onTv() }}>
            <Tv size={14} className="text-muted" /> TV mode
          </button>
          <button role="menuitem" className={cx(item, 'border-t border-rule-2 text-bad')} onClick={onLogout}>
            <LogOut size={14} /> Sign out
          </button>
        </div>
      )}
    </div>
  )
}
