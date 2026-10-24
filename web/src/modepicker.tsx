import { Monitor, Tv } from 'lucide-react'
import { isTvDevice } from './api'
import type { DeviceMode } from './mode'
import { useSpatialNav } from './tv/spatial'

/** First screen when signed out: "Which screen is this?" Works with a remote. */
export default function ModePicker({ onPick }: { onPick: (m: DeviceMode) => void }) {
  useSpatialNav(() => {})
  const tvLike = isTvDevice()
  const tiles: { id: DeviceMode; title: string; text: string; Icon: typeof Tv }[] = [
    { id: 'tv', title: 'TV', text: 'Big tiles you move around with a remote. Signs in with a code from your phone.', Icon: Tv },
    { id: 'normal', title: 'Computer or phone', text: 'The full web interface, with keyboard and mouse or touch.', Icon: Monitor },
  ]
  return (
    <div className="tv flex min-h-screen flex-col">
      <header className="flex items-center gap-[0.6em] border-b border-rule px-[3em] py-[1.1em]">
        <span className="grid h-[1.3em] w-[1.3em] place-items-center rounded-[2px] bg-accent">
          <span className="h-[0.4em] w-[0.4em] rounded-full bg-accent-ink" />
        </span>
        <span className="text-[1.2em] font-bold tracking-[0.3em]">HAVEN</span>
      </header>
      <main className="flex flex-1 flex-col justify-center px-[3em] py-[2em]">
        <h1 className="text-[2.6em] font-semibold tracking-tight">Where are you using Haven?</h1>
        <p className="mt-[0.5em] text-[1.05em] text-ink-2">Use the arrow keys and OK on your remote, or click. Haven remembers your choice on this device.</p>
        <div className="mt-[2em] flex flex-wrap gap-[1.4em]" role="group" aria-label="Device type">
          {tiles.map(({ id, title, text, Icon }) => (
            <button
              key={id}
              data-nav
              data-nav-row="mode"
              data-nav-default={(id === 'tv') === tvLike ? '' : undefined}
              onClick={() => onPick(id)}
              className="plate flex h-[14em] w-[19em] flex-col justify-between bg-raised p-[1.4em] text-left"
            >
              <Icon className="h-[2.6em] w-[2.6em] text-accent" strokeWidth={1.4} aria-hidden />
              <span>
                <span className="block text-[1.5em] font-semibold">{title}</span>
                <span className="mt-[0.3em] block text-[0.85em] leading-snug text-ink-2">{text}</span>
              </span>
            </button>
          ))}
        </div>
      </main>
    </div>
  )
}
