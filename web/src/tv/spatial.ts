import { useEffect, useRef } from 'react'
import { play } from './sound'
import { setMode } from '../mode'
import { go } from '../router'

// Spatial navigation for TV remotes. Every focusable item carries `data-nav`.
// Arrow keys move focus to the nearest item in that direction; items in the
// same `data-nav-row` remember the last one focused, so moving down and back
// up returns you where you were. An open sheet marks itself `data-nav-trap`
// so focus can't wander behind it.

export function enterTv() {
  go('tv')
}

export function exitTv() {
  setMode('normal')
  go('overview')
}

type Dir = 'up' | 'down' | 'left' | 'right'

const ARROWS: Record<string, Dir> = { ArrowUp: 'up', ArrowDown: 'down', ArrowLeft: 'left', ArrowRight: 'right' }
// Back keys across TV platforms: Tizen (10009), webOS (461), Android/Fire TV.
const BACK_KEYS = new Set(['Escape', 'GoBack', 'BrowserBack', 'XF86Back', 'Back'])
const BACK_CODES = new Set([461, 10009])

const rowMemory = new Map<string, HTMLElement>()

function scope(): ParentNode {
  const traps = document.querySelectorAll('[data-nav-trap]')
  return traps.length ? traps[traps.length - 1] : document
}

function focusables(root: ParentNode): HTMLElement[] {
  return Array.from(root.querySelectorAll<HTMLElement>('[data-nav]')).filter(
    (el) => !el.hasAttribute('disabled') && el.getAttribute('aria-disabled') !== 'true' && el.getClientRects().length > 0,
  )
}

function smooth(): ScrollBehavior {
  // Light mode (weak TVs) and reduced-motion users get instant jumps.
  return window.matchMedia('(prefers-reduced-motion: reduce)').matches || document.querySelector('.tv-lite') ? 'auto' : 'smooth'
}

export function focusEl(el: HTMLElement) {
  el.focus({ preventScroll: true })
  el.scrollIntoView({ block: 'nearest', inline: 'nearest', behavior: smooth() })
  // The first row sits under the meters: keep them on screen.
  if (el.dataset.navRow === 'apps') window.scrollTo({ top: 0, behavior: smooth() })
  const row = el.dataset.navRow
  if (row) rowMemory.set(row, el)
}

export function focusDefault() {
  const els = focusables(scope())
  const el = els.find((e) => e.dataset.navDefault !== undefined) || els[0]
  if (el) focusEl(el)
}

function move(dir: Dir) {
  const els = focusables(scope())
  if (els.length === 0) return
  const cur = document.activeElement as HTMLElement | null
  if (!cur || !els.includes(cur)) {
    focusDefault()
    return
  }
  const r = cur.getBoundingClientRect()
  const cx = r.left + r.width / 2
  const cy = r.top + r.height / 2
  let best: HTMLElement | null = null
  let bestScore = Infinity
  for (const el of els) {
    if (el === cur) continue
    const b = el.getBoundingClientRect()
    const bx = b.left + b.width / 2
    const by = b.top + b.height / 2
    let gap: number
    let cross: number
    if (dir === 'right' || dir === 'left') {
      if (dir === 'right' ? bx <= cx + 1 : bx >= cx - 1) continue
      gap = dir === 'right' ? Math.max(0, b.left - r.right) : Math.max(0, r.left - b.right)
      const overlap = Math.min(r.bottom, b.bottom) - Math.max(r.top, b.top)
      cross = overlap > 0 ? 0 : Math.abs(by - cy)
    } else {
      if (dir === 'down' ? by <= cy + 1 : by >= cy - 1) continue
      gap = dir === 'down' ? Math.max(0, b.top - r.bottom) : Math.max(0, r.top - b.bottom)
      cross = Math.abs(bx - cx)
    }
    const score = gap + cross * 2.5
    if (score < bestScore) {
      bestScore = score
      best = el
    }
  }
  if (!best) {
    play('edge')
    return
  }
  // Entering another row: go back to where you were in it.
  const row = best.dataset.navRow
  if (row && row !== cur.dataset.navRow) {
    const remembered = rowMemory.get(row)
    if (remembered && remembered.isConnected && els.includes(remembered)) best = remembered
  }
  focusEl(best)
  play('move')
}

/** Wires the remote: arrows move focus, OK clicks (native), Back calls onBack. */
export function useSpatialNav(onBack: () => void) {
  const back = useRef(onBack)
  back.current = onBack
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      const t = e.target as HTMLElement
      const typing = t.matches('input, textarea, select')
      const dir = ARROWS[e.key]
      if (dir) {
        if (typing && (dir === 'left' || dir === 'right')) return
        e.preventDefault()
        move(dir)
        return
      }
      if (BACK_KEYS.has(e.key) || BACK_CODES.has(e.keyCode) || (e.key === 'Backspace' && !typing)) {
        e.preventDefault()
        play('back')
        back.current()
        return
      }
      if ((e.key === 'Enter' || e.key === ' ') && t.hasAttribute('data-nav') && !typing) play('select')
    }
    window.addEventListener('keydown', onKey)
    const t = setTimeout(() => {
      const a = document.activeElement as HTMLElement | null
      if (!a || !a.hasAttribute('data-nav')) focusDefault()
    }, 60)
    return () => {
      window.removeEventListener('keydown', onKey)
      clearTimeout(t)
    }
  }, [])
}
