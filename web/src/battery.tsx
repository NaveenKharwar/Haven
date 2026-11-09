import { useEffect, useRef, useState } from 'react'
import {
  AlertTriangle,
  BatteryCharging,
  BatteryLow,
  BatteryMedium,
  BatteryWarning,
  X,
  Zap,
} from 'lucide-react'
import { cx, Ladder, Readout } from './components'
import type { BatteryInfo } from './api'

export function BatteryHeaderItem({
  battery,
}: {
  battery?: BatteryInfo | null
}) {
  const [open, setOpen] = useState(false)
  const popoverRef = useRef<HTMLDivElement>(null)

  // Close dropdown on outside click
  useEffect(() => {
    function handleClickOutside(e: MouseEvent | TouchEvent) {
      if (popoverRef.current && !popoverRef.current.contains(e.target as Node)) {
        setOpen(false)
      }
    }
    if (open) {
      document.addEventListener('mousedown', handleClickOutside)
      document.addEventListener('touchstart', handleClickOutside)
      return () => {
        document.removeEventListener('mousedown', handleClickOutside)
        document.removeEventListener('touchstart', handleClickOutside)
      }
    }
  }, [open])

  // If no battery is present (e.g. headless server / desktop), hide completely
  if (!battery) {
    return null
  }

  const isCharging = battery.status === 'Charging'
  const isFull = battery.status === 'Full' || (battery.acOnline && battery.percent >= 98)
  const isLow = !battery.acOnline && battery.percent <= 20
  const isCritical = !battery.acOnline && battery.percent <= 10
  const hasIssue = Boolean(battery.issue || (battery.health && battery.health !== 'Good'))

  return (
    <span className="relative inline-flex items-baseline" ref={popoverRef}>
      <button
        type="button"
        onClick={() => setOpen(!open)}
        className={cx(
          'inline-flex items-baseline gap-1 cursor-pointer transition-colors select-none p-0 m-0 border-0 bg-transparent font-mono text-xs group',
          isCritical
            ? 'text-bad animate-pulse font-medium'
            : isLow
            ? 'text-warn font-medium'
            : hasIssue
            ? 'text-warn'
            : 'text-muted hover:text-ink'
        )}
        title={
          battery.issue ||
          `Battery: ${battery.percent}% (${battery.status}) · Click for details`
        }
        aria-label="Battery status"
        aria-expanded={open}
      >
        <span>
          BAT{' '}
          <Readout
            value={battery.percent}
            digits={3}
            className={cx(
              isCritical
                ? 'text-bad font-semibold'
                : isLow
                ? 'text-warn font-semibold'
                : hasIssue
                ? 'text-warn'
                : 'text-ink group-hover:text-ink'
            )}
          />
          %
        </span>

        {/* State icons */}
        {isCharging && (
          <Zap size={11} className="text-accent fill-accent shrink-0 self-center inline" />
        )}
        {!isCharging && battery.acOnline && isFull && (
          <span className="text-[10px] text-ink-2 font-mono self-center">AC</span>
        )}
        {!isCharging && isLow && (
          <BatteryWarning size={12} className="text-warn shrink-0 self-center inline" />
        )}
        {hasIssue && !isLow && (
          <AlertTriangle size={11} className="text-warn shrink-0 self-center inline" />
        )}
      </button>

      {open && (
        <BatteryPopover
          battery={battery}
          onClose={() => setOpen(false)}
          popoverRef={popoverRef}
        />
      )}
    </span>
  )
}

function BatteryPopover({
  battery,
  onClose,
  popoverRef,
}: {
  battery: BatteryInfo
  onClose: () => void
  popoverRef: React.RefObject<HTMLDivElement | null>
}) {
  const isCharging = battery.status === 'Charging'
  const isLow = !battery.acOnline && battery.percent <= 20
  const hasIssue = Boolean(battery.issue || (battery.health && battery.health !== 'Good'))

  return (
    <div
      ref={popoverRef}
      className="absolute right-0 top-full mt-2.5 z-50 w-72 rounded-sm border border-rule bg-paper p-3.5 shadow-xl font-sans text-xs text-ink animate-in fade-in zoom-in-95 duration-100"
    >
      <div className="flex items-center justify-between border-b border-rule pb-2 mb-2.5">
        <div className="flex items-center gap-1.5 font-medium">
          {isCharging ? (
            <BatteryCharging size={15} className="text-accent" />
          ) : isLow ? (
            <BatteryLow size={15} className="text-bad" />
          ) : (
            <BatteryMedium size={15} className="text-ink" />
          )}
          <span>Power & Battery</span>
        </div>
        <button
          onClick={onClose}
          className="text-muted hover:text-ink p-0.5 rounded transition-colors cursor-pointer"
          aria-label="Close battery details"
        >
          <X size={13} />
        </button>
      </div>

      <div className="space-y-2.5">
        {/* Level readout + Progress Bar */}
        <div>
          <div className="flex items-baseline justify-between mb-1">
            <span className="font-mono text-base font-semibold text-ink">
              {battery.percent}%
            </span>
            <span className="text-[11px] font-mono text-muted uppercase">
              {battery.status} {battery.acOnline ? '(AC Connected)' : '(On Battery)'}
            </span>
          </div>
          <Ladder percent={battery.percent} segs={20} plain={!isLow} />
        </div>

        {/* Health & Specs */}
        <div className="rounded border border-rule/60 bg-paper-2/40 p-2 space-y-1 text-[11px]">
          <div className="flex justify-between">
            <span className="text-muted">Condition</span>
            <span className={cx('font-medium', hasIssue ? 'text-warn' : 'text-ok')}>
              {battery.health || 'Good'}
            </span>
          </div>
          {battery.healthPercent !== undefined && (
            <div className="flex justify-between">
              <span className="text-muted">Maximum Capacity</span>
              <span className="font-mono">{battery.healthPercent}% of design</span>
            </div>
          )}
          <div className="flex justify-between">
            <span className="text-muted">Power Source</span>
            <span>{battery.acOnline ? 'AC Adapter' : 'Internal Battery'}</span>
          </div>
        </div>

        {/* Issues / Alerts */}
        {battery.issue && (
          <div className="rounded border border-rule bg-well p-2 text-[11px] text-warn flex items-start gap-1.5">
            <AlertTriangle size={13} className="shrink-0 mt-0.5" />
            <span>{battery.issue}</span>
          </div>
        )}
      </div>
    </div>
  )
}
