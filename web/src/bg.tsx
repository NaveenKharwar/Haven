import { lazy, Suspense, useEffect, useState } from 'react'
import { api, type Background } from './api'

// The shader library loads only when a background is chosen.
const Backdrop = lazy(() => import('./backdrop'))

/** Loads the saved background for the web or TV, and reloads it when settings change. */
export function useBackground(which: 'web' | 'tv', pollMs = 0): Background | null {
  const [bg, setBg] = useState<Background | null>(null)
  useEffect(() => {
    const load = () =>
      api
        .ui()
        .then((u) => {
          const next = which === 'tv' ? u.backgroundTv : u.backgroundWeb
          // Keep the same object if nothing changed, so the shader isn't rebuilt.
          setBg((cur) => (JSON.stringify(cur) === JSON.stringify(next) ? cur : next))
        })
        .catch(() => {})
    load()
    window.addEventListener('haven:ui', load)
    // A TV can't hear settings changed on a phone, so it checks now and then.
    const t = pollMs ? setInterval(load, pollMs) : undefined
    return () => {
      window.removeEventListener('haven:ui', load)
      clearInterval(t)
    }
  }, [which, pollMs])
  return bg
}

export function BackdropLayer({ bg, tv }: { bg: Background | null; tv?: boolean }) {
  if (!bg || !bg.shader) return null
  return (
    <Suspense fallback={null}>
      <Backdrop bg={bg} tv={tv} />
    </Suspense>
  )
}
