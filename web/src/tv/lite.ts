import { useEffect, useState } from 'react'
import { isTvDevice } from '../api'

// Light mode keeps TV mode easy on weak hardware (many Android TVs have little
// memory): no animated background, no animations or transitions, slower
// refreshing and fewer tiles. It is on by default on a detected TV.

const KEY = 'haven-tv-lite'

export function liteOn(): boolean {
  try {
    const v = localStorage.getItem(KEY)
    if (v) return v === 'on'
  } catch {
    /* storage blocked */
  }
  return isTvDevice()
}

export function setLite(on: boolean) {
  try {
    localStorage.setItem(KEY, on ? 'on' : 'off')
  } catch {
    /* storage blocked */
  }
  window.dispatchEvent(new Event('haven:lite'))
}

/** Picking TV mode on a device that never chose before starts it in Light mode. */
export function defaultLiteForTv() {
  try {
    if (!localStorage.getItem(KEY)) localStorage.setItem(KEY, 'on')
  } catch {
    /* storage blocked */
  }
}

export function useLite(): boolean {
  const [v, setV] = useState(liteOn)
  useEffect(() => {
    const f = () => setV(liteOn())
    window.addEventListener('haven:lite', f)
    return () => window.removeEventListener('haven:lite', f)
  }, [])
  return v
}
