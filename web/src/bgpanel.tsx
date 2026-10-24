import { useState } from 'react'
import type { Background } from './api'
import { cx } from './components'
import { families, ShaderView } from './backdrop'
import { SavedMark, useUISettings } from './look'

// Background picker for the web and TV. Loaded only on the Settings page,
// together with the shader library.

const MOTION: [number, string][] = [
  [0, 'Still'],
  [0.4, 'Slow'],
  [1, 'Normal'],
]

export default function BackgroundPanel() {
  const { ui, update, saved } = useUISettings()
  const [target, setTarget] = useState<'web' | 'tv'>('web')
  const bg: Background | undefined = ui ? (target === 'tv' ? ui.backgroundTv : ui.backgroundWeb) : undefined
  const [family, setFamily] = useState<string | null>(null)
  if (!ui || !bg) return null
  const famId = family ?? (bg.shader || 'meshGradient')
  const fam = families.find((f) => f.id === famId) || families[0]
  const set = (patch: Partial<Background>) =>
    update((u) => (target === 'tv' ? { ...u, backgroundTv: { ...u.backgroundTv, ...patch } } : { ...u, backgroundWeb: { ...u.backgroundWeb, ...patch } }))

  return (
    <section className="plate overflow-hidden" aria-label="Background">
      <div className="plate-head">
        <h2 className="etch">Background</h2>
        <SavedMark state={saved} />
      </div>
      <div className="space-y-5 p-4 sm:p-5">
        <div className="flex flex-wrap items-center gap-3">
          <div className="well inline-flex gap-0.5 p-0.5" role="tablist" aria-label="Where">
            {(['web', 'tv'] as const).map((t) => (
              <button
                key={t}
                role="tab"
                aria-selected={target === t}
                onClick={() => {
                  setTarget(t)
                  setFamily(null)
                }}
                className={cx('key key-sm gap-2 border-transparent', target !== t && 'key-flat')}
              >
                <span className={cx('h-1.5 w-1.5 rounded-full', target === t ? 'bg-accent' : 'bg-unlit')} />
                {t === 'web' ? 'This web app' : 'TV mode'}
              </button>
            ))}
          </div>
          <span className="text-sm text-ink-2">
            Now: <span className="text-ink">{bg.shader ? `${families.find((f) => f.id === bg.shader)?.label} · ${bg.preset}` : 'Plain faceplate'}</span>
          </span>
        </div>

        <div className="flex flex-wrap gap-1.5" role="radiogroup" aria-label="Style">
          <button role="radio" aria-checked={!bg.shader} className={cx('key key-sm', !bg.shader && 'text-accent')} onClick={() => set({ shader: '', preset: '' })}>
            None
          </button>
          {families.map((f) => (
            <button key={f.id} role="radio" aria-checked={famId === f.id && !!bg.shader} className={cx('key key-sm', famId === f.id && 'border-accent')} onClick={() => setFamily(f.id)}>
              {f.label}
            </button>
          ))}
        </div>

        <div className="grid grid-cols-2 gap-3 sm:grid-cols-3 xl:grid-cols-5">
          {fam.presets.map((p) => {
            const on = bg.shader === fam.id && bg.preset === p.name
            return (
              <button
                key={p.name}
                onClick={() => set({ shader: fam.id, preset: p.name, dim: bg.shader ? bg.dim : target === 'tv' ? 35 : 55, speed: bg.shader ? bg.speed : 0.4 })}
                className={cx('group overflow-hidden rounded-[3px] border text-left', on ? 'border-accent ring-2 ring-accent' : 'border-rule hover:border-rule-hi')}
                aria-pressed={on}
              >
                <div className="aspect-video bg-well">
                  <ShaderView shader={fam.id} preset={p.name} speed={0.5} maxPixelCount={320 * 180} />
                </div>
                <div className="flex items-center justify-between bg-plate px-2 py-1.5 text-xs">
                  {p.name}
                  {on && <span className="h-1.5 w-1.5 rounded-full bg-accent" />}
                </div>
              </button>
            )
          })}
        </div>

        {bg.shader && (
          <div className="grid grid-cols-1 gap-5 sm:grid-cols-2">
            <div className="space-y-2">
              <label htmlFor="dim" className="etch block">
                Dim · {bg.dim}%
              </label>
              <input id="dim" type="range" min={0} max={90} step={5} value={bg.dim} onChange={(e) => set({ dim: Number(e.target.value) })} className="w-full accent-[var(--accent)]" />
              <p className="text-xs text-muted">Darken the background so text stays easy to read.</p>
            </div>
            <div className="space-y-2">
              <span className="etch block">Motion</span>
              <div className="well inline-flex gap-0.5 p-0.5" role="radiogroup" aria-label="Motion">
                {MOTION.map(([v, label]) => (
                  <button key={label} role="radio" aria-checked={bg.speed === v} onClick={() => set({ speed: v })} className={cx('key key-sm border-transparent', bg.speed !== v && 'key-flat')}>
                    {label}
                  </button>
                ))}
              </div>
              <p className="text-xs text-muted">Still uses the least power{target === 'tv' ? ', best for slower TVs' : ''}.</p>
            </div>
          </div>
        )}
      </div>
    </section>
  )
}
