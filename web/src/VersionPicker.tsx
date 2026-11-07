import { useEffect, useRef, useState } from 'react'
import { LoaderCircle, Check, AlertCircle } from 'lucide-react'
import { api, type VersionInfo } from './api'
import { Led, cx } from './components'

const DOCKER_TAG_RE = /^[a-zA-Z0-9][a-zA-Z0-9_.-]{0,127}$/

export interface VersionPickerProps {
  catalogId?: string
  appId?: string
  value: string
  defaultVersion: string
  currentVersion?: string
  onChange: (version: string, isValid: boolean) => void
  disabled?: boolean
}

type CheckState = {
  checking: boolean
  valid: boolean
  message?: string
  warning?: string
}

export function VersionPicker({
  catalogId,
  appId,
  value,
  defaultVersion,
  currentVersion,
  onChange,
  disabled,
}: VersionPickerProps) {
  const [info, setInfo] = useState<VersionInfo | null>(null)
  const [loadingInfo, setLoadingInfo] = useState(false)
  const [mode, setMode] = useState<'preset' | 'custom'>('preset')
  const [customInput, setCustomInput] = useState('')
  const [checkState, setCheckState] = useState<CheckState>({ checking: false, valid: true })

  const checkTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null)
  const currentReqRef = useRef(0)

  // Fetch available versions from registry backend
  useEffect(() => {
    let active = true
    setLoadingInfo(true)

    const fetchVersions = async () => {
      try {
        let res: VersionInfo
        if (catalogId) {
          res = await api.catalogVersions(catalogId)
        } else if (appId) {
          res = await api.appVersions(appId)
        } else {
          return
        }
        if (active) {
          setInfo(res)
        }
      } catch (err) {
        console.warn('Failed to load version presets from registry:', err)
      } finally {
        if (active) {
          setLoadingInfo(false)
        }
      }
    }

    fetchVersions()
    return () => {
      active = false
    }
  }, [catalogId, appId])

  // Build the list of preset buttons
  // 1. Current (if installed app)
  // 2. Default / Recommended (if catalog app or if different from current)
  // 3. latest
  // 4. Up to 3 recent semver versions
  const presets: { id: string; version: string; label: string; badge?: string }[] = []

  const recVersion = info?.default || defaultVersion
  const currVersion = currentVersion || info?.current

  if (currVersion) {
    presets.push({
      id: 'current',
      version: currVersion,
      label: currVersion,
      badge: 'Current',
    })
  }

  if (recVersion && (!currVersion || recVersion !== currVersion)) {
    presets.push({
      id: 'recommended',
      version: recVersion,
      label: recVersion,
      badge: currVersion ? 'Catalog default' : 'Recommended',
    })
  }

  presets.push({
    id: 'latest',
    version: 'latest',
    label: 'latest',
    badge: 'Latest image',
  })

  if (info?.recent && info.recent.length > 0) {
    const recents = info.recent.filter(
      (v) => v !== recVersion && v !== currVersion && v !== 'latest'
    )
    for (const r of recents.slice(0, 3)) {
      presets.push({
        id: `recent-${r}`,
        version: r,
        label: r,
      })
    }
  }

  // Determine if current `value` matches one of the presets or if it's custom
  useEffect(() => {
    const isMatchingPreset = presets.some((p) => p.version === value)
    if (!isMatchingPreset && value && value !== recVersion && value !== currVersion && value !== 'latest') {
      setMode('custom')
      setCustomInput(value)
    }
  }, [value, presets.length])

  // Live checker for custom manual tag input
  const validateAndCheckCustom = (tag: string) => {
    const trimmed = tag.trim()

    if (checkTimerRef.current) {
      clearTimeout(checkTimerRef.current)
    }

    if (!trimmed) {
      setCheckState({
        checking: false,
        valid: false,
        message: 'Please enter a version tag',
      })
      onChange('', false)
      return
    }

    if (!DOCKER_TAG_RE.test(trimmed)) {
      setCheckState({
        checking: false,
        valid: false,
        message: 'Invalid tag format. Must contain only letters, numbers, dots, hyphens, or underscores.',
      })
      onChange(trimmed, false)
      return
    }

    // Set optimistic checking state
    setCheckState({ checking: true, valid: false, message: 'Checking registry…' })

    const reqId = ++currentReqRef.current

    checkTimerRef.current = setTimeout(async () => {
      try {
        const res = await api.checkVersion({
          catalogId,
          appId,
          version: trimmed,
        })

        if (currentReqRef.current !== reqId) return

        if (!res.valid || !res.exists) {
          setCheckState({
            checking: false,
            valid: false,
            message: res.error || `Version "${trimmed}" was not found in the container registry.`,
          })
          onChange(trimmed, false)
        } else if (res.warning) {
          setCheckState({
            checking: false,
            valid: true,
            warning: res.warning,
          })
          onChange(trimmed, true)
        } else {
          setCheckState({
            checking: false,
            valid: true,
            message: 'Version verified on registry',
          })
          onChange(trimmed, true)
        }
      } catch (e) {
        if (currentReqRef.current !== reqId) return
        // Network or server error checking registry: warn but do not hard-block
        setCheckState({
          checking: false,
          valid: true,
          warning: 'Could not contact registry to verify tag. Please double check the tag.',
        })
        onChange(trimmed, true)
      }
    }, 350)
  }

  const selectPreset = (tag: string) => {
    if (disabled) return
    if (checkTimerRef.current) {
      clearTimeout(checkTimerRef.current)
    }
    setMode('preset')
    setCheckState({ checking: false, valid: true })
    onChange(tag, true)
  }

  const selectCustomMode = () => {
    if (disabled) return
    setMode('custom')
    const initial = mode === 'custom' && customInput ? customInput : (value && !presets.some((p) => p.version === value) ? value : '')
    setCustomInput(initial)
    validateAndCheckCustom(initial)
  }

  return (
    <div className="space-y-2.5">
      <div className="flex items-center justify-between">
        <label className="etch block">Version</label>
        {loadingInfo && (
          <span className="flex items-center gap-1 font-mono text-[0.6875rem] text-muted">
            <LoaderCircle className="spin" size={11} />
            checking registry…
          </span>
        )}
      </div>

      {/* Preset options pills */}
      <div className="flex flex-wrap items-center gap-1.5" role="group" aria-label="Version selection">
        {presets.map((preset) => {
          const isSelected = mode === 'preset' && value === preset.version
          return (
            <button
              key={preset.id}
              type="button"
              disabled={disabled}
              onClick={() => selectPreset(preset.version)}
              className={cx(
                'key key-sm gap-1.5 font-mono transition-colors',
                isSelected
                  ? 'border-rule-hi bg-plate text-ink shadow-sm'
                  : 'key-flat text-ink-2 hover:border-rule hover:text-ink'
              )}
              title={preset.badge ? `${preset.version} (${preset.badge})` : preset.version}
            >
              <span
                className={cx(
                  'h-1.5 w-1.5 shrink-0 rounded-full transition-colors',
                  isSelected ? 'bg-accent shadow-[0_0_5px_var(--accent)]' : 'bg-unlit'
                )}
              />
              <span>{preset.label}</span>
              {preset.badge && (
                <span className="ml-1 text-[0.625rem] font-sans uppercase tracking-wider text-muted opacity-80">
                  {preset.badge}
                </span>
              )}
            </button>
          )
        })}

        {/* Custom manual entry toggle button */}
        <button
          type="button"
          disabled={disabled}
          onClick={selectCustomMode}
          className={cx(
            'key key-sm gap-1.5 font-mono transition-colors',
            mode === 'custom'
              ? 'border-rule-hi bg-plate text-ink shadow-sm'
              : 'key-flat text-ink-2 hover:border-rule hover:text-ink'
          )}
          title="Enter a custom version tag manually"
        >
          <span
            className={cx(
              'h-1.5 w-1.5 shrink-0 rounded-full transition-colors',
              mode === 'custom' ? 'bg-accent shadow-[0_0_5px_var(--accent)]' : 'bg-unlit'
            )}
          />
          <span>Custom…</span>
        </button>
      </div>

      {/* Manual Input view when Custom is active */}
      {mode === 'custom' && (
        <div className="space-y-2 pt-1 animate-in fade-in duration-150">
          <div className="relative max-w-sm">
            <input
              id="custom-version-input"
              className={cx(
                'field font-mono text-sm pr-9',
                !checkState.valid && checkState.message && !checkState.checking && 'border-bad focus:border-bad'
              )}
              value={customInput}
              onChange={(e) => {
                setCustomInput(e.target.value)
                validateAndCheckCustom(e.target.value)
              }}
              placeholder={recVersion || 'e.g. 10.9.10 or 31-apache'}
              autoFocus
              disabled={disabled}
            />
            <span className="pointer-events-none absolute right-3 top-1/2 -translate-y-1/2 flex items-center">
              {checkState.checking ? (
                <LoaderCircle className="spin text-muted" size={15} />
              ) : checkState.valid && !checkState.warning && customInput.trim() ? (
                <Check className="text-ok" size={15} />
              ) : !checkState.valid && customInput.trim() ? (
                <AlertCircle className="text-bad" size={15} />
              ) : null}
            </span>
          </div>

          {/* Inline live validation feedback */}
          <div className="min-h-[1.25rem] text-xs">
            {checkState.checking ? (
              <p className="flex items-center gap-1.5 text-muted">
                <LoaderCircle className="spin shrink-0" size={12} />
                Matching tag with container registry…
              </p>
            ) : !checkState.valid && checkState.message ? (
              <p className="flex items-center gap-1.5 text-bad">
                <Led s="bad" />
                <span>{checkState.message}</span>
              </p>
            ) : checkState.warning ? (
              <p className="flex items-center gap-1.5 text-warn">
                <Led s="warn" />
                <span>{checkState.warning}</span>
              </p>
            ) : customInput.trim() ? (
              <p className="flex items-center gap-1.5 text-ok">
                <Led s="ok" />
                <span>Tag exists in registry ({customInput.trim()})</span>
              </p>
            ) : (
              <p className="text-muted">Enter any published image tag from the upstream registry.</p>
            )}
          </div>
        </div>
      )}

      {mode === 'preset' && (
        <p className="text-[0.6875rem] font-mono text-muted">
          Active tag: <span className="text-ink">{value}</span>
        </p>
      )}
    </div>
  )
}
