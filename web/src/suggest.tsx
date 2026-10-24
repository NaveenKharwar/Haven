import { useEffect, useId, useRef, useState, type ReactNode } from 'react'
import { Spinner, cx } from './components'

export type Suggestion = { key: string; label: string; hint?: string }

/**
 * A text box with a list of suggestions under it. One look for every search
 * box in Haven: type to filter, arrow keys to move, Enter to pick, Escape to
 * close. The list sits in the page (not floating), so dialogs and cards never
 * clip it.
 */
export function Suggest({
  value,
  onChange,
  items,
  onPick,
  placeholder,
  label,
  busy,
  disabled,
  empty,
  className,
  autoFocus,
  footer,
}: {
  value: string
  onChange: (v: string) => void
  items: Suggestion[] | null
  onPick: (s: Suggestion) => void
  placeholder?: string
  label: string
  busy?: boolean
  disabled?: boolean
  empty?: string // shown when a search found nothing
  className?: string
  autoFocus?: boolean
  footer?: ReactNode
}) {
  const id = useId()
  const [active, setActive] = useState(0)
  const [closed, setClosed] = useState(false)
  const list = useRef<HTMLUListElement>(null)
  useEffect(() => {
    setActive(0)
    setClosed(false)
  }, [items])
  useEffect(() => {
    list.current?.children[active]?.scrollIntoView({ block: 'nearest' })
  }, [active])
  const open = !!items && !closed

  const pick = (s: Suggestion) => {
    onPick(s)
    onChange('')
  }
  return (
    <div className={cx('space-y-2', className)}>
      <div className="relative">
        <input
          className="field"
          role="combobox"
          aria-expanded={open}
          aria-controls={`${id}-list`}
          aria-activedescendant={open && items?.[active] ? `${id}-${active}` : undefined}
          aria-autocomplete="list"
          aria-label={label}
          autoComplete="off"
          autoFocus={autoFocus}
          placeholder={placeholder}
          disabled={disabled}
          value={value}
          onChange={(e) => onChange(e.target.value)}
          onKeyDown={(e) => {
            if (!items || items.length === 0) return
            if (e.key === 'ArrowDown') {
              e.preventDefault()
              setClosed(false)
              setActive((a) => Math.min(items.length - 1, a + 1))
            } else if (e.key === 'ArrowUp') {
              e.preventDefault()
              setActive((a) => Math.max(0, a - 1))
            } else if (e.key === 'Enter' && open) {
              e.preventDefault()
              pick(items[active])
            } else if (e.key === 'Escape' && open) {
              e.stopPropagation()
              setClosed(true)
            }
          }}
        />
        {busy && (
          <span className="absolute right-3 top-1/2 -translate-y-1/2">
            <Spinner size={14} />
          </span>
        )}
      </div>
      {open && (
        <ul ref={list} id={`${id}-list`} role="listbox" aria-label={`${label} suggestions`} className="well max-h-56 divide-y divide-rule-2 overflow-y-auto">
          {items!.length === 0 && <li className="px-3 py-2 text-sm text-muted">{empty || 'Nothing found.'}</li>}
          {items!.map((s, i) => (
            <li key={s.key} id={`${id}-${i}`} role="option" aria-selected={i === active}>
              <button
                type="button"
                tabIndex={-1}
                className={cx('block w-full px-3 py-2 text-left text-sm hover:bg-raised', i === active && 'bg-raised')}
                onMouseEnter={() => setActive(i)}
                onClick={() => pick(s)}
              >
                {s.label}
                {s.hint && <span className="text-muted"> · {s.hint}</span>}
              </button>
            </li>
          ))}
          {footer}
        </ul>
      )}
    </div>
  )
}
