import { useState } from 'react'
import { Lock, RefreshCw } from 'lucide-react'
import { api, type NetDevice, type WifiNetwork } from './api'
import { ErrorLine, Led, Modal, Spinner, cx, toast, usePoll } from './components'
import { useHaven } from './context'
import { PasswordInput } from './Auth'

function Bars({ signal }: { signal: number }) {
  const n = signal > 75 ? 4 : signal > 50 ? 3 : signal > 25 ? 2 : 1
  return (
    <span className="flex h-3.5 items-end gap-0.5" role="img" aria-label={`Signal ${signal}%`}>
      {[1, 2, 3, 4].map((i) => (
        <i key={i} className={cx('w-1 rounded-[1px]', i <= n ? 'bg-ink-2' : 'bg-unlit')} style={{ height: `${i * 25}%` }} />
      ))}
    </span>
  )
}

export default function NetworkPanel() {
  const { openJob } = useHaven()
  const { data: st, refresh } = usePoll(api.network, 10000)
  const [nets, setNets] = useState<WifiNetwork[] | null>(null)
  const [scanning, setScanning] = useState(false)
  const [pick, setPick] = useState<{ ssid: string; secure: boolean; hidden?: boolean } | null>(null)
  const [ipFor, setIpFor] = useState<NetDevice | null>(null)
  const [error, setError] = useState('')

  const scan = async () => {
    setScanning(true)
    setError('')
    try {
      setNets(await api.wifiScan())
    } catch (e) {
      setError((e as Error).message)
    } finally {
      setScanning(false)
    }
  }
  const run = async (fn: () => Promise<{ id: string }>, title: string) => {
    try {
      const job = await fn()
      openJob(job.id, title)
      setTimeout(refresh, 4000)
    } catch (e) {
      setError((e as Error).message)
    }
  }

  const online = st?.internet
  return (
    <section className="plate overflow-hidden" aria-label="Network">
      <div className="plate-head">
        <h2 className="etch">Network & Wi-Fi</h2>
        {st && (
          <span className="flex items-center gap-2">
            <Led s={online ? 'ok' : 'bad'} />
            <span className="etch text-[0.625rem]">{online ? 'Online' : 'No internet'}</span>
          </span>
        )}
      </div>
      <div className="space-y-5 p-4 sm:p-5">
        {!st ? (
          <Spinner />
        ) : !st.supported ? (
          <p className="text-sm text-ink-2">{st.reason} Install Haven on your Ubuntu server and this panel manages Wi-Fi and the network.</p>
        ) : !st.nmReady ? (
          <div className="space-y-3">
            <p className="text-sm text-ink-2">
              To scan and join Wi-Fi from here, Haven needs <span className="text-ink">NetworkManager</span>. Wired connections that are already set up keep working as they are.
            </p>
            {st.jobId ? (
              <p className="flex items-center gap-2 text-sm">
                <Spinner size={14} /> Setting up…
              </p>
            ) : (
              <button className="key key-primary" onClick={() => run(api.networkSetup, 'networking')}>
                Set up networking
              </button>
            )}
            {st.devices.map((d, i) => (
              <p key={i} className="font-mono text-xs text-ink-2">
                Connected as {d.ips.join(', ')}
              </p>
            ))}
          </div>
        ) : (
          <>
            <ul className="divide-y divide-rule-2">
              {st.devices.map((d) => (
                <li key={d.name} className="flex flex-wrap items-center gap-x-3 gap-y-1 py-2.5">
                  <Led s={d.state === 'connected' ? 'ok' : d.state === 'unavailable' || !d.managed ? 'off' : 'warn'} />
                  <span className="min-w-0 flex-1">
                    <span className="block text-sm font-medium">
                      {d.type === 'wifi' ? 'Wi-Fi' : 'Wired'} · <span className="font-mono text-xs">{d.name}</span>
                    </span>
                    <span className="block truncate font-mono text-xs text-muted">
                      {d.state === 'connected' ? `${d.connection} · ${d.ips.join(', ') || 'no address'}${d.gateway ? ` · router ${d.gateway}` : ''}` : d.managed ? d.state : 'managed by another tool'}
                    </span>
                  </span>
                  {d.state === 'connected' && d.managed && (
                    <button className="key key-sm" onClick={() => setIpFor(d)}>
                      IP address
                    </button>
                  )}
                </li>
              ))}
              {st.devices.length === 0 && <li className="py-2 text-sm text-muted">No network adapters found.</li>}
            </ul>

            {st.wifiDev ? (
              <div className="space-y-3">
                <div className="flex items-center justify-between gap-3">
                  <h3 className="etch">Wi-Fi networks</h3>
                  <button className="key key-sm" onClick={scan} disabled={scanning}>
                    {scanning ? <Spinner size={13} /> : <RefreshCw size={13} />} {nets ? 'Scan again' : 'Scan'}
                  </button>
                </div>
                {!st.wifiOn && <p className="text-sm text-warn">Wi-Fi is switched off. Connecting to a network turns it on.</p>}
                {nets && (
                  <ul className="well divide-y divide-rule-2">
                    {nets.length === 0 && <li className="px-3 py-3 text-sm text-muted">No networks found. Move closer to the router and scan again.</li>}
                    {nets.map((w) => (
                      <li key={w.ssid}>
                        <button className="flex w-full items-center gap-3 px-3 py-2.5 text-left hover:bg-raised" onClick={() => (w.inUse ? undefined : setPick({ ssid: w.ssid, secure: !!w.security && !w.saved }))}>
                          <Bars signal={w.signal} />
                          <span className="min-w-0 flex-1 truncate text-sm">{w.ssid}</span>
                          {w.security && <Lock size={12} className="text-muted" aria-label="Secured" />}
                          {w.inUse ? <span className="tag tag-ok">Connected</span> : w.saved ? <span className="tag">Saved</span> : null}
                        </button>
                      </li>
                    ))}
                  </ul>
                )}
                <button className="key key-flat key-sm -ml-2" onClick={() => setPick({ ssid: '', secure: true, hidden: true })}>
                  Join a hidden network
                </button>
                {st.saved.length > 0 && (
                  <div>
                    <h3 className="etch mb-1">Saved networks</h3>
                    <ul className="divide-y divide-rule-2">
                      {st.saved.map((s) => (
                        <li key={s.uuid} className="flex items-center gap-3 py-2 text-sm">
                          <span className="min-w-0 flex-1 truncate">{s.name}</span>
                          {s.active && <span className="tag tag-ok">In use</span>}
                          <button
                            className="key key-sm key-danger"
                            onClick={async () => {
                              if (s.active && !confirm(`${s.name} is the network this server uses. Forgetting it may cut Haven off until you reconnect. Continue?`)) return
                              try {
                                await api.wifiForget(s.uuid)
                                refresh()
                              } catch (e) {
                                toast.error((e as Error).message)
                              }
                            }}
                          >
                            Forget
                          </button>
                        </li>
                      ))}
                    </ul>
                  </div>
                )}
              </div>
            ) : (
              <p className="text-sm text-ink-2">No Wi-Fi adapter is available to Haven. If this server has one, it may be controlled by another tool, or switched off with a hardware key.</p>
            )}
            <ErrorLine>{error}</ErrorLine>
            <p className="text-xs text-muted">If you connect this server to a different Wi-Fi network, its address changes. Reopen Haven by name (see Local address) or with the new IP shown above.</p>
          </>
        )}
      </div>

      {pick && (
        <JoinDialog
          pick={pick}
          onClose={() => setPick(null)}
          onJoin={(ssid, pw, hidden) => {
            setPick(null)
            run(() => api.wifiConnect(ssid, pw, hidden), `Wi-Fi ${ssid}`)
          }}
        />
      )}
      {ipFor && st && <IpDialog dev={ipFor} onClose={() => setIpFor(null)} onDone={(fn) => { setIpFor(null); run(fn, 'network address') }} />}
    </section>
  )
}

function JoinDialog({ pick, onClose, onJoin }: { pick: { ssid: string; secure: boolean; hidden?: boolean }; onClose: () => void; onJoin: (ssid: string, pw: string, hidden: boolean) => void }) {
  const [ssid, setSsid] = useState(pick.ssid)
  const [pw, setPw] = useState('')
  return (
    <Modal
      open
      onClose={onClose}
      label="Wi-Fi"
      title={pick.hidden ? 'Hidden network' : pick.ssid}
      footer={
        <>
          <button className="key" onClick={onClose}>
            Cancel
          </button>
          <button className="key key-primary" disabled={!ssid || (pick.secure && pw.length > 0 && pw.length < 8)} onClick={() => onJoin(ssid, pw, !!pick.hidden)}>
            Connect
          </button>
        </>
      }
    >
      <form
        className="space-y-4"
        onSubmit={(e) => {
          e.preventDefault()
          if (ssid) onJoin(ssid, pw, !!pick.hidden)
        }}
      >
        {pick.hidden && (
          <div className="space-y-1.5">
            <label htmlFor="ssid" className="etch block">
              Network name
            </label>
            <input id="ssid" className="field" value={ssid} onChange={(e) => setSsid(e.target.value)} maxLength={32} autoFocus />
          </div>
        )}
        {pick.secure && (
          <div className="space-y-1.5">
            <label htmlFor="wpw" className="etch block">
              Password
            </label>
            <PasswordInput id="wpw" value={pw} onChange={setPw} autoComplete="off" />
          </div>
        )}
        <p className="text-xs text-muted">Haven can lose its connection while switching networks. If joining fails, it goes back to the previous network by itself.</p>
        <button className="sr-only">Connect</button>
      </form>
    </Modal>
  )
}

function IpDialog({ dev, onClose, onDone }: { dev: NetDevice; onClose: () => void; onDone: (fn: () => Promise<{ id: string }>) => void }) {
  const cur = dev.ips[0]?.split('/')[0] || ''
  const [address, setAddress] = useState(cur)
  const [prefix, setPrefix] = useState(dev.ips[0]?.split('/')[1] || '24')
  const [gateway, setGateway] = useState(dev.gateway || '')
  const [dns, setDns] = useState(dev.dns.join(', '))
  const id = dev.connUuid || ''
  return (
    <Modal
      open
      onClose={onClose}
      label="IP address"
      title={dev.connection || dev.name}
      footer={
        <>
          <button className="key" onClick={onClose}>
            Cancel
          </button>
          <button className="key" disabled={!id} onClick={() => onDone(() => api.setStatic({ uuid: id, auto: true }))}>
            Use automatic (DHCP)
          </button>
          <button className="key key-primary" disabled={!id} onClick={() => onDone(() => api.setStatic({ uuid: id, address, prefix: Number(prefix), gateway, dns: dns.split(/[,\s]+/).filter(Boolean) }))}>
            Use this fixed address
          </button>
        </>
      }
    >
      <div className="space-y-4">
        <p className="text-sm text-ink-2">
          A fixed address stays the same on your network. Choose one <span className="text-ink">outside your router's automatic range</span> (for example .50 or higher on a home network). After applying, open Haven at the new address.
        </p>
        {!id && <p className="text-sm text-warn">Haven can't find this connection's settings. Set a fixed address in your router instead (a DHCP reservation).</p>}
        <div className="grid grid-cols-1 gap-4 sm:grid-cols-3">
          <div className="space-y-1.5 sm:col-span-2">
            <label htmlFor="ip" className="etch block">Address</label>
            <input id="ip" className="field font-mono" value={address} onChange={(e) => setAddress(e.target.value)} inputMode="decimal" />
          </div>
          <div className="space-y-1.5">
            <label htmlFor="pf" className="etch block">Size (prefix)</label>
            <input id="pf" className="field font-mono" value={prefix} onChange={(e) => setPrefix(e.target.value.replace(/\D/g, ''))} />
          </div>
          <div className="space-y-1.5">
            <label htmlFor="gw" className="etch block">Router</label>
            <input id="gw" className="field font-mono" value={gateway} onChange={(e) => setGateway(e.target.value)} />
          </div>
          <div className="space-y-1.5 sm:col-span-2">
            <label htmlFor="dns" className="etch block">DNS (optional)</label>
            <input id="dns" className="field font-mono" value={dns} onChange={(e) => setDns(e.target.value)} placeholder="router, 1.1.1.1" />
          </div>
        </div>
        <p className="text-xs text-muted">If the router doesn't answer at the new address, Haven puts the old settings back.</p>
      </div>
    </Modal>
  )
}
