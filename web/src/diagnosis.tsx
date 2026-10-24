import { useEffect, useState } from 'react'
import QRCode from 'qrcode'
import { Copy, RotateCw } from 'lucide-react'
import type { Diagnosis, Finding } from './api'
import { Led, Spinner, cx } from './components'

const tag: Record<Finding['level'], [string, string]> = {
  ok: ['tag-ok', 'Pass'],
  info: ['', 'Info'],
  warn: ['tag-warn', 'Warn'],
  bad: ['tag-bad', 'Fail'],
}
const led: Record<Finding['level'], 'ok' | 'warn' | 'bad' | 'off'> = { ok: 'ok', info: 'off', warn: 'warn', bad: 'bad' }

export async function copyText(text: string): Promise<boolean> {
  try {
    await navigator.clipboard.writeText(text)
    return true
  } catch {
    // Clipboard API needs https or localhost; fall back to a hidden textarea.
    const ta = document.createElement('textarea')
    ta.value = text
    ta.style.position = 'fixed'
    ta.style.opacity = '0'
    document.body.appendChild(ta)
    ta.select()
    const ok = document.execCommand('copy')
    ta.remove()
    return ok
  }
}

/** A diagnosis: the verdict first, then every finding, worst first. */
export function DiagnosisView({ d, running, onRun }: { d: Diagnosis | null; running: boolean; onRun: () => void }) {
  const [copied, setCopied] = useState(false)
  const [showOk, setShowOk] = useState(false)
  if (!d) {
    return (
      <div className="flex items-center gap-3 py-6 text-sm text-ink-2">
        <Spinner /> Checking containers, ports, folders, logs and the server…
      </div>
    )
  }
  const problems = d.findings.filter((f) => f.level !== 'ok')
  const passes = d.findings.filter((f) => f.level === 'ok')
  return (
    <div className="space-y-4">
      <section className={cx('plate overflow-hidden', d.level === 'bad' && 'border-bad', d.level === 'warn' && 'border-warn')} aria-live="polite">
        <div className="flex items-start gap-3 p-4">
          <span className="mt-1.5">
            <Led s={led[d.level]} />
          </span>
          <div className="min-w-0 flex-1">
            <p className="font-semibold">{d.verdict}</p>
            {d.cause?.fix && <p className="mt-1 text-sm text-ink-2">{d.cause.fix}</p>}
            <p className="mt-2 font-mono text-[0.6875rem] text-muted">
              {passes.length} passed · {problems.length} to look at · {new Date(d.ranAt).toLocaleTimeString()}
            </p>
          </div>
        </div>
        <div className="flex flex-wrap gap-2 border-t border-rule-2 px-4 py-3">
          <button className="key key-sm" onClick={onRun} disabled={running} data-loading={running}>
            {running ? <Spinner size={13} /> : <RotateCw size={13} />} Run again
          </button>
          <button
            className="key key-sm"
            onClick={async () => {
              setCopied(await copyText(d.report))
              setTimeout(() => setCopied(false), 2000)
            }}
          >
            <Copy size={13} /> {copied ? 'Copied' : 'Copy report'}
          </button>
          <span className="self-center text-xs text-muted">Passwords are removed from the report.</span>
        </div>
      </section>

      {problems.length > 0 && (
        <ul className="plate divide-y divide-rule-2 overflow-hidden">
          {problems.map((f, i) => (
            <FindingRow key={i} f={f} />
          ))}
        </ul>
      )}

      <div>
        <button className="key key-flat key-sm -ml-2" onClick={() => setShowOk(!showOk)} aria-expanded={showOk}>
          {showOk ? 'Hide' : 'Show'} {passes.length} passed checks
        </button>
        {showOk && (
          <ul className="plate mt-2 divide-y divide-rule-2 overflow-hidden">
            {passes.map((f, i) => (
              <FindingRow key={i} f={f} />
            ))}
          </ul>
        )}
      </div>
    </div>
  )
}

function FindingRow({ f }: { f: Finding }) {
  return (
    <li className="flex items-start gap-3 px-4 py-3">
      <span className={cx('tag mt-0.5 w-12 shrink-0 justify-center', tag[f.level][0])}>{tag[f.level][1]}</span>
      <div className="min-w-0 flex-1">
        <div className="etch mb-0.5 text-[0.625rem]">{f.area}</div>
        <p className="text-sm font-medium">{f.title}</p>
        {f.detail && <p className="mt-1 text-sm text-ink-2">{f.detail}</p>}
        {f.fix && (
          <p className="mt-1 text-sm text-ink-2">
            <span className="font-medium text-ink">Fix: </span>
            {f.fix}
          </p>
        )}
        {f.evidence && f.evidence.length > 0 && (
          <div className="well tape mt-2 overflow-x-auto p-2">
            {f.evidence.map((e, i) => (
              <div key={i} className="whitespace-pre-wrap break-all">
                {e}
              </div>
            ))}
          </div>
        )}
      </div>
    </li>
  )
}

/** QR code drawn in the theme's QR colours (always dark on light, for scanners). */
export function QR({ text, size = 148 }: { text: string; size?: number }) {
  const [svg, setSvg] = useState('')
  useEffect(() => {
    QRCode.toString(text, { type: 'svg', margin: 1, errorCorrectionLevel: 'M', color: { dark: '#000000', light: '#ffffff' } })
      .then((s) => setSvg(s.replace('fill="#ffffff"', 'fill="none"').replace('stroke="#000000"', 'stroke="currentColor"')))
      .catch(() => setSvg(''))
  }, [text])
  return (
    <div
      className="qr shrink-0 rounded-[3px] p-2"
      style={{ width: size, height: size }}
      role="img"
      aria-label={`QR code for ${text}`}
      // The SVG comes from the qrcode library, not from user input.
      dangerouslySetInnerHTML={{ __html: svg }}
    />
  )
}
