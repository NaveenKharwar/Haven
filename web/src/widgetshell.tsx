import type { ReactNode } from 'react'
import { cx } from './components'

/** The card every Overview widget sits in. */
export function Widget({ title, aside, children, className }: { title: string; aside?: ReactNode; children: ReactNode; className?: string }) {
  return (
    <section className={cx('widget-card group/w plate flex h-full w-full min-w-0 flex-col overflow-hidden', className)} aria-label={title}>
      <div className="plate-head widget-heading">
        <h2 className="etch min-w-0 truncate" title={title}>{title}</h2>
        {aside && <div className="flex min-w-0 shrink-0 items-center">{aside}</div>}
      </div>
      <div className="flex min-h-0 flex-1 flex-col">{children}</div>
    </section>
  )
}
