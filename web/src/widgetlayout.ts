import { useLayoutEffect, useRef, useState, type PointerEvent } from 'react'
import { W_DEFAULT, type WKey, type WidgetSizes } from './widgetmeta'

export type WidgetLayout = { order: WKey[]; hidden: WKey[]; sizes: WidgetSizes }

export function widgetOrder(order: string[] = []): WKey[] {
  const keys = [...new Set(order)].filter((key): key is WKey => W_DEFAULT.includes(key as WKey))
  return [...keys, ...W_DEFAULT.filter((key) => !keys.includes(key))]
}

type Drop = { key: WKey; side: 'left' | 'right' | 'top' | 'bottom' }
type Drag = { key: WKey; id: number; x: number; y: number; startX: number; startY: number; frame: number; active: boolean }

export function useWidgetLayoutMotion(move: (key: WKey, target: WKey, after: boolean) => void) {
  const board = useRef<HTMLDivElement>(null)
  const drag = useRef<Drag | null>(null)
  const previous = useRef<Map<string, DOMRect> | null>(null)
  const [dragging, setDragging] = useState<WKey | null>(null)
  const [drop, setDrop] = useState<Drop | null>(null)
  const cards = () => Array.from(board.current?.querySelectorAll<HTMLElement>('[data-widget-key]') ?? [])

  const measure = () => {
    if (window.matchMedia('(prefers-reduced-motion: reduce)').matches) return
    previous.current = new Map(cards().map((card) => [card.dataset.widgetKey!, card.getBoundingClientRect()]))
  }

  useLayoutEffect(() => {
    const before = previous.current
    previous.current = null
    if (!before) return
    const shifts = cards().map((card) => {
      const old = before.get(card.dataset.widgetKey!)
      const next = card.getBoundingClientRect()
      return { card, x: old ? old.left - next.left : 0, y: old ? old.top - next.top : 0 }
    })
    for (const { card, x, y } of shifts) {
      if (x || y) card.animate([{ transform: `translate(${x}px, ${y}px)` }, { transform: 'none' }], {
        duration: 200, easing: 'cubic-bezier(0.16, 1, 0.3, 1)',
      })
    }
  })

  function destination(x: number, y: number): Drop | null {
    const bounds = board.current?.getBoundingClientRect()
    if (!bounds || x < bounds.left || x > bounds.right || y < bounds.top || y > bounds.bottom) return null
    const slots = cards()
    let nearest: { index: number; key: WKey; rect: DOMRect; distance: number } | undefined
    for (const [index, card] of slots.entries()) {
      const rect = card.getBoundingClientRect()
      const dx = Math.max(rect.left - x, 0, x - rect.right)
      const dy = Math.max(rect.top - y, 0, y - rect.bottom)
      const distance = dx * dx + dy * dy
      if (!nearest || distance < nearest.distance) nearest = { index, key: card.dataset.widgetKey as WKey, rect, distance }
    }
    if (!nearest || nearest.key === drag.current?.key) return null
    const { key, rect } = nearest
    const side = rect.width >= bounds.width * 0.85
      ? y < rect.top + rect.height / 2 ? 'top' : 'bottom'
      : x < rect.left + rect.width / 2 ? 'left' : 'right'
    const source = slots.findIndex((card) => card.dataset.widgetKey === drag.current?.key)
    const insertion = nearest.index + Number(side === 'right' || side === 'bottom') - Number(source < nearest.index)
    if (source === insertion) return null
    return { key, side }
  }

  function frame() {
    const active = drag.current
    if (!active?.active) return
    const edge = 88
    const top = Math.max(0, edge - active.y)
    const bottom = Math.max(0, active.y - (window.innerHeight - edge))
    if (top || bottom) window.scrollBy(0, Math.max(-18, Math.min(18, (bottom - top) / 4)))
    const next = destination(active.x, active.y)
    setDrop((old) => old?.key === next?.key && old?.side === next?.side ? old : next)
    active.frame = requestAnimationFrame(frame)
  }

  function cancel() {
    if (!drag.current) return
    cancelAnimationFrame(drag.current.frame)
    drag.current = null
    setDragging(null)
    setDrop(null)
  }

  useLayoutEffect(() => () => {
    if (drag.current) cancelAnimationFrame(drag.current.frame)
  }, [])

  function start(event: PointerEvent<HTMLButtonElement>, key: WKey) {
    if (!event.isPrimary || event.button !== 0) return
    const { pointerId, clientX, clientY } = event
    event.currentTarget.setPointerCapture(pointerId)
    drag.current = { key, id: pointerId, x: clientX, y: clientY, startX: clientX, startY: clientY, frame: 0, active: false }
  }

  function track(event: PointerEvent<HTMLButtonElement>) {
    const active = drag.current
    if (active?.id !== event.pointerId) return
    active.x = event.clientX
    active.y = event.clientY
    if (!active.active && Math.hypot(active.x - active.startX, active.y - active.startY) >= 6) {
      active.active = true
      setDragging(active.key)
      active.frame = requestAnimationFrame(frame)
    }
  }

  function finish(event: PointerEvent<HTMLButtonElement>) {
    const active = drag.current
    if (active?.id !== event.pointerId) return
    const target = active.active ? destination(event.clientX, event.clientY) : null
    cancel()
    if (target) move(active.key, target.key, target.side === 'right' || target.side === 'bottom')
  }

  return { board, dragging, drop, measure, start, track, finish, cancel }
}
