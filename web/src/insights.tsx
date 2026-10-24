import { useState, type ReactNode } from 'react'
import { Check, Pencil, RefreshCw, X } from 'lucide-react'
import { api, formatBytes, formatUptime, type DevicesSnapshot, type InsightsInfo, type LanDevice, type PublicInfo } from './api'
import { ErrorLine, Led, Spinner, cx, toast, usePoll } from './components'
import { agoText } from './usage'

function Block({ title, hint, children, aside }: { title: string; hint?: string; children: ReactNode; aside?: ReactNode }) {
  return (
    <section className="plate overflow-hidden" aria-label={title}>
      <div className="plate-head">
        <h2 className="etch">{title}</h2>
        {aside}
      </div>
      <div className="p-4 sm:p-5">
        {hint && <p className="mb-3 text-sm text-ink-2">{hint}</p>}
        {children}
      </div>
    </section>
  )
}

function Rows({ rows }: { rows: [string, ReactNode][] }) {
  const shown = rows.filter(([, v]) => v !== undefined && v !== null && v !== '' && v !== false)
  return (
    <dl className="divide-y divide-rule-2 text-sm">
      {shown.map(([k, v]) => (
        <div key={k} className="grid gap-x-4 gap-y-0.5 py-2 sm:grid-cols-[11rem_minmax(0,1fr)]">
          <dt className="text-muted">{k}</dt>
          <dd className="min-w-0 break-words">{v}</dd>
        </div>
      ))}
    </dl>
  )
}

const mono = (s: ReactNode) => <span className="font-mono text-[0.8125rem]">{s}</span>

function PublicAddress() {
  const [info, setInfo] = useState<PublicInfo | null>(null)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const look = async () => {
    setBusy(true)
    setError('')
    try {
      setInfo(await api.publicAddress())
    } catch (e) {
      setError((e as Error).message)
    } finally {
      setBusy(false)
    }
  }
  return (
    <div className="space-y-3">
      {info ? (
        <Rows
          rows={[
            ['Public address', mono(info.ip)],
            ['Provider', info.isp],
            ['Appears to be in', [info.city, info.region, info.country].filter(Boolean).join(', ')],
            ['Time zone', info.timezone],
          ]}
        />
      ) : (
        <p className="text-sm text-ink-2">This is the address the internet sees, and roughly where it points. Haven asks ipwho.is, a free lookup service, which sees your public address. It only happens when you press the button.</p>
      )}
      {info && <p className="text-xs text-muted">The place is the provider's best guess for the address, usually the nearest city, not your home.</p>}
      <button className="key key-sm" onClick={look} disabled={busy} data-loading={busy}>
        {busy ? <Spinner size={13} /> : <RefreshCw size={13} />} {info ? 'Look up again' : 'Look up my public address'}
      </button>
      <ErrorLine>{error}</ErrorLine>
    </div>
  )
}

function ServerFacts({ d }: { d: InsightsInfo }) {
  const h = d.hardware
  const s = d.system
  const n = d.network
  const open = d.ports.filter((p) => p.scope === 'network')
  const local = d.ports.filter((p) => p.scope !== 'network')
  return (
    <div className="space-y-5">
      <Block title="Hardware" hint="What this machine is made of.">
        <Rows
          rows={[
            ['Machine', [h.vendor, h.product].filter(Boolean).join(' ') || (h.virtual ? `Virtual machine (${h.virtual})` : '')],
            ['Motherboard', h.board],
            ['Firmware', h.bios],
            ['Processor', h.cpuModel && `${h.cpuModel} · ${h.cpuCores} cores, ${h.cpuThreads} threads${h.cpuMaxMhz ? ` · up to ${(h.cpuMaxMhz / 1000).toFixed(1)} GHz` : ''}`],
            ['Memory', h.memTotal ? formatBytes(h.memTotal, 0) : ''],
            ['Graphics', (h.gpus || []).join('; ')],
            ['Storage', (h.drives || []).length ? (
              <ul className="space-y-1">
                {(h.drives || []).map((x) => (
                  <li key={x.name}>
                    {x.model || x.name} <span className="text-muted">· {formatBytes(x.size, 0)} · {x.kind}{x.bus ? ` · ${x.bus}` : ''} · {x.name}</span>
                  </li>
                ))}
              </ul>
            ) : ''],
            ['Temperature', (h.temperature || []).length ? (h.temperature || []).map((t) => `${t.name} ${t.c.toFixed(0)}°C`).join(' · ') : ''],
            ['Battery', h.battery],
          ]}
        />
      </Block>

      <Block title="System">
        <Rows
          rows={[
            ['Name', mono(s.hostname)],
            ['Operating system', s.name],
            ['Kernel', mono(`${s.kernel} · ${s.arch}`)],
            ['Running for', s.boot ? `${formatUptime(Math.floor(Date.now() / 1000) - s.boot)}, since ${new Date(s.boot * 1000).toLocaleString()}` : ''],
            ['Time zone', s.timezone],
            ['Clock', s.ntp && (
              <span className="inline-flex items-center gap-2">
                <Led s={s.ntp === 'synchronised' ? 'ok' : 'warn'} label={s.ntp} /> {s.ntp}
              </span>
            )],
            ['Docker', s.docker && mono(s.docker)],
            ['Processes', s.processes ? String(s.processes) : ''],
          ]}
        />
      </Block>

      <Block title="Network" hint="How this server connects.">
        <Rows rows={[['Router', n.gateway && mono(n.gateway)], ['DNS servers', n.dns.length ? mono(n.dns.join('  ')) : '']]} />
        <ul className="mt-3 space-y-2">
          {n.interfaces.map((i) => (
            <li key={i.name} className="well px-3 py-2 text-sm">
              <div className="flex flex-wrap items-center gap-2">
                <Led s={i.up ? 'ok' : 'off'} label={i.up ? 'Up' : 'Down'} />
                <span className="font-mono">{i.name}</span>
                <span className="tag">{i.kind}</span>
                {i.router && <span className="tag tag-accent">Main connection</span>}
                {i.speedMbps ? <span className="text-xs text-muted">{i.speedMbps >= 1000 ? `${i.speedMbps / 1000} Gbit/s` : `${i.speedMbps} Mbit/s`}</span> : null}
              </div>
              <div className="mt-1 break-all font-mono text-xs text-muted">{[...i.addrs, i.mac].filter(Boolean).join(' · ')}</div>
            </li>
          ))}
        </ul>
      </Block>

      <Block title="Internet address">
        <PublicAddress />
      </Block>

      <Block
        title="What is reachable"
        hint="Programs waiting for connections. The first group can be reached from other devices on your network. Check that you expect each one."
      >
        {d.ports.length === 0 ? (
          <p className="text-sm text-muted">Haven can't list these here (it needs the `ss` tool on the server).</p>
        ) : (
          <div className="space-y-4">
            <PortList title="Open to your network" ports={open} />
            <PortList title="This server only" ports={local} />
          </div>
        )}
      </Block>
    </div>
  )
}

function PortList({ title, ports }: { title: string; ports: InsightsInfo['ports'] }) {
  if (!ports.length) return null
  return (
    <div>
      <p className="etch mb-1">{title}</p>
      <ul className="divide-y divide-rule-2 text-sm">
        {ports.map((p) => (
          <li key={`${p.proto}${p.port}${p.address}`} className="flex flex-wrap items-baseline gap-x-3 py-1.5">
            <span className="w-24 font-mono">{p.port}<span className="ml-1 text-xs text-muted">{p.proto}</span></span>
            <span className="min-w-0 flex-1">{p.process || <span className="text-muted">unknown program</span>}</span>
            <span className="font-mono text-xs text-muted">{p.address}</span>
          </li>
        ))}
      </ul>
    </div>
  )
}

// --- devices ------------------------------------------------------------------------------

function deviceTitle(d: LanDevice) {
  return d.label || d.name || (d.self ? 'This server' : d.router ? 'Router' : d.vendor) || (d.randomized ? 'Phone or tablet (private address)' : 'Unnamed device')
}

function DeviceRow({ d, onChanged }: { d: LanDevice; onChanged: () => void }) {
  const [editing, setEditing] = useState(false)
  const [text, setText] = useState(d.label || '')
  const save = async () => {
    try {
      await api.labelDevice(d.mac, text)
      setEditing(false)
      onChanged()
    } catch (e) {
      toast.error((e as Error).message)
    }
  }
  const forget = async () => {
    try {
      await api.forgetDevice(d.mac)
      onChanged()
    } catch (e) {
      toast.error((e as Error).message)
    }
  }
  const agent = d.agent ? d.agent.replace(/Mozilla\/5\.0 /, '').replace(/\(([^)]*)\).*/, '$1') : ''
  return (
    <li className="flex flex-wrap items-start gap-x-3 gap-y-1 px-1 py-3">
      <span className="mt-1.5">
        <Led s={d.online ? 'ok' : 'off'} label={d.online ? 'Online' : 'Not seen now'} />
      </span>
      <div className="min-w-0 flex-1">
        <div className="flex flex-wrap items-center gap-2">
          {editing ? (
            <form
              className="flex items-center gap-1"
              onSubmit={(e) => {
                e.preventDefault()
                save()
              }}
            >
              <input autoFocus className="field h-8 w-52" value={text} maxLength={60} placeholder="Name this device" onChange={(e) => setText(e.target.value)} />
              <button type="submit" className="key key-sm key-icon" aria-label="Save name">
                <Check size={13} />
              </button>
              <button type="button" className="key key-flat key-sm key-icon" aria-label="Cancel" onClick={() => setEditing(false)}>
                <X size={13} />
              </button>
            </form>
          ) : (
            <>
              <span className={cx('font-medium', !d.online && 'text-ink-2')}>{deviceTitle(d)}</span>
              {!d.self && (
                <button className="key key-flat key-sm key-icon" aria-label={`Name ${deviceTitle(d)}`} onClick={() => setEditing(true)}>
                  <Pencil size={12} />
                </button>
              )}
            </>
          )}
          {d.self && <span className="tag">This server</span>}
          {d.router && <span className="tag">Router</span>}
          {d.new && <span className="tag tag-accent">New</span>}
        </div>
        <div className="mt-0.5 flex flex-wrap gap-x-3 font-mono text-xs text-muted">
          <span>{d.ip}</span>
          {!d.self && <span>{d.mac}</span>}
          {d.name && d.label && <span>{d.name}</span>}
          {d.vendor ? <span className="font-sans">{d.vendor}</span> : d.randomized && !d.self ? <span className="font-sans">Private address. Phones use these on purpose, and the device can't be told apart.</span> : null}
        </div>
        {agent && (
          <div className="mt-0.5 text-xs text-muted">
            Opened Haven {d.requests} {d.requests === 1 ? 'time' : 'times'} · {agent}
          </div>
        )}
      </div>
      <div className="text-right text-xs text-muted">
        {d.online ? 'Online now' : `Last seen ${agoText(d.lastSeen).replace('measured ', '')}`}
        {!d.online && !d.self && (
          <button className="key key-flat key-sm ml-2" onClick={forget} title="Remove from the list">
            Forget
          </button>
        )}
      </div>
    </li>
  )
}

function Devices() {
  const { data, refresh } = usePoll(api.lanDevices, 30000)
  const [scan, setScan] = useState<DevicesSnapshot | null>(null)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const snap = scan || data
  const run = async () => {
    setBusy(true)
    setError('')
    try {
      setScan(await api.scanDevices())
    } catch (e) {
      setError((e as Error).message)
    } finally {
      setBusy(false)
    }
  }
  const changed = async () => {
    setScan(null)
    refresh()
  }
  const online = snap?.devices.filter((d) => d.online).length || 0
  return (
    <Block
      title="Devices on your network"
      hint="Everything Haven can see on your home network. Press Scan to wake up quiet devices. Give each one a name so you know what is what, and check that nothing unfamiliar is there."
      aside={snap ? <span className="font-mono text-xs text-muted">{online} online · {snap.devices.length} known</span> : undefined}
    >
      <div className="mb-3 flex flex-wrap items-center gap-3">
        <button className="key key-sm key-primary" onClick={run} disabled={busy} data-loading={busy}>
          {busy ? <Spinner size={13} /> : <RefreshCw size={13} />} {busy ? 'Scanning…' : 'Scan the network'}
        </button>
        {snap?.subnets?.length ? <span className="font-mono text-xs text-muted">{snap.subnets.join(', ')}</span> : null}
        {snap?.scanned && !snap.scanned.startsWith('0001') ? <span className="text-xs text-muted">Last scan {agoText(snap.scanned).replace('measured ', '')}</span> : null}
      </div>
      <ErrorLine>{error}</ErrorLine>
      {snap?.note && <p className="text-sm text-muted">{snap.note}</p>}
      {!snap ? (
        <p className="text-sm text-muted">Looking…</p>
      ) : (
        <ul className="divide-y divide-rule-2">
          {snap.devices.map((d) => (
            <DeviceRow key={d.mac} d={d} onChanged={changed} />
          ))}
        </ul>
      )}
      <p className="mt-4 text-xs text-muted">
        Haven reads this server's own view of the network, so it sees names and addresses, not what each device does. A device that blocks pings can still appear here. Haven tells you when a new one shows up.
      </p>
    </Block>
  )
}

export default function InsightsPanel() {
  const { data, error } = usePoll(api.insights, 60000)
  return (
    <div className="space-y-5">
      <Devices />
      {error && <ErrorLine>{error}</ErrorLine>}
      {!data && !error ? (
        <p className="flex items-center gap-2 text-sm text-muted">
          <Spinner size={13} /> Reading the server…
        </p>
      ) : data ? (
        <ServerFacts d={data} />
      ) : null}
    </div>
  )
}
