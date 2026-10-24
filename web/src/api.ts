export type Setting = {
  key: string
  label: string
  type: 'string' | 'port' | 'path' | 'password' | 'number' | 'select'
  default: string
  description?: string
  required: boolean
  generate?: boolean
  protocols?: string[]
  options?: string[]
  prefill?: string
}

export type WebSpec = { port: string; path?: string; scheme?: string }

export type CatalogApp = {
  id: string
  name: string
  category: string
  tagline: string
  description: string
  icon: string
  website?: string
  version: string
  archs: string[]
  minRamMb?: number
  notes?: string
  hostPorts?: string[]
  screenshots?: string[]
  web?: WebSpec
  settings: Setting[]
  compose: string
}

export type Container = {
  id: string
  name: string
  image: string
  state: string
  status: string
  health: string
  ports: string
  project: string
  service: string
  cpu?: number // percent of the whole server
  mem?: number // bytes
}

export type AppUsage = {
  cpu: number // percent of the whole server
  mem: number // bytes
  memPercent: number
  images: number // bytes
  data: number // bytes
  dataAt?: string
  live: boolean
}

export type InstalledApp = {
  id: string
  catalogId?: string
  name: string
  icon?: string
  category?: string
  version?: string
  web?: WebSpec
  custom?: boolean
  installedAt: string
  status: 'running' | 'partial' | 'starting' | 'stopped' | 'error' | 'unknown'
  containers: Container[]
  webPort?: string
  busy?: string
  updateAvailable?: string
  notes?: string
  usage?: AppUsage
}

export type Check = { name: string; level: 'ok' | 'warn' | 'fail'; message: string }

export type Job = {
  id: string
  app: string
  action: string
  status: 'running' | 'success' | 'failed'
  error?: string
  started: string
  ended?: string
  log?: string[]
  help?: { title: string; why: string; fix: string; command?: string }
}

export type DiskInfo = { mount: string; device: string; fstype: string; total: number; used: number; free: number; percent: number }
export type Point = { t: number; cpu: number; mem: number; rx: number; tx: number }
export type SystemInfo = {
  hostname: string
  os: string
  kernel: string
  arch: string
  uptime: number
  cpuModel: string
  cpuCores: number
  cpuPercent: number
  perCore: number[] | null
  load: [number, number, number]
  memTotal: number
  memUsed: number
  memPercent: number
  swapTotal: number
  swapUsed: number
  disks: DiskInfo[] | null
  netRx: number
  netTx: number
  netRxTotal: number
  netTxTotal: number
  tempC: number | null
  docker: string
  version: string
  history: Point[] | null
}

export type Finding = { level: 'ok' | 'info' | 'warn' | 'bad'; area: string; title: string; detail?: string; fix?: string; evidence?: string[] }
export type Diagnosis = { target: string; level: Finding['level']; verdict: string; cause?: Finding; findings: Finding[]; report: string; ranAt: string }

export type PowerStatus = {
  supported: boolean
  reason?: string
  laptop: boolean
  lidState?: string
  keepAwake: boolean
  lidOnBattery: string
  lidOnPower: string
  lidDocked: string
  sleepBlocked: boolean
  effective: boolean
  desktop: boolean
  screenSupported: boolean
  screenOff: boolean
  battery?: { percent: number; status: string; acOnline: boolean }
  message?: string
}

export type LocalNameStatus = {
  supported: boolean
  reason?: string
  hostname: string
  installed: boolean
  running: boolean
  advertised: string
  resolves: boolean
  port80: boolean
  port80Free: boolean
  addresses: string[] | null
  firewall: boolean
  havenPort: string
  jobId?: string
  suggestName: string
}

export type RemoteStatus = {
  supported: boolean
  reason?: string
  installed: boolean
  state: string
  authUrl?: string
  hostName?: string
  dnsName?: string
  ips?: string[]
  tailnet?: string
  magicDns: boolean
  peers: number
  peersOnline: number
  keyExpiry?: string
  version?: string
  urls?: string[]
  jobId?: string
}

export type Background = { shader: string; preset: string; dim: number; speed: number }
export type Place = { name: string; region?: string; country?: string; cc?: string; lat: number; lon: number }
export type WidgetSettings = {
  places: Place[]
  units: 'metric' | 'imperial'
  home: string
  currencies: string[]
  markets: string[]
  newsScope: 'local' | 'top' | 'off'
  sources: { name: string; url: string }[]
  hidden: string[] | null
  order?: string[]
  clocks?: string[]
  clockFormat?: '24' | '12'
  notes?: string
  links?: { name: string; url: string }[]
  checks?: { name: string; target: string }[]
}
export type Drive = { name: string; model?: string; size: number; kind: string; bus?: string }
export type InsightsInfo = {
  hardware: { vendor?: string; product?: string; board?: string; bios?: string; virtual?: string; cpuModel: string; cpuCores: number; cpuThreads: number; cpuMaxMhz?: number; memTotal: number; gpus: string[] | null; drives: Drive[] | null; battery?: string; temperature: { name: string; c: number }[] | null }
  system: { name: string; kernel: string; arch: string; boot: number; timezone: string; ntp?: string; docker?: string; hostname: string; processes?: number }
  network: { interfaces: { name: string; kind: string; mac?: string; up: boolean; addrs: string[]; speedMbps?: number; mtu: number; router: boolean }[]; gateway?: string; dns: string[] }
  ports: { port: number; proto: string; scope: string; process?: string; address: string }[]
}
export type PublicInfo = { ip: string; city: string; region: string; country: string; isp: string; timezone: string; checked: string }
export type LanDevice = { mac: string; ip: string; name?: string; label?: string; vendor?: string; randomized: boolean; online: boolean; new: boolean; self: boolean; router: boolean; firstSeen: string; lastSeen: string; agent?: string; requests?: number }
export type DevicesSnapshot = { devices: LanDevice[]; subnets: string[]; scanned?: string; note?: string }
export type UISettings = { backgroundWeb: Background; backgroundTv: Background; widgets: WidgetSettings }

export type Weather = {
  place: string
  temp: number
  feels: number
  humidity: number
  wind: number
  code: number
  isDay: boolean
  units: string
  windUnits: string
  days: { date: string; code: number; max: number; min: number; rain: number }[]
}
export type NewsItem = { title: string; link: string; source: string; published: string; summary?: string }
export type News = { scope: string; label: string; items: NewsItem[] }
export type Rate = { code: string; rate: number; change: number; history: number[] | null }
export type CurrencyData = { home: string; date: string; rates: Rate[] }
export type Quote = {
  symbol: string
  name: string
  currency: string
  price: number
  change: number
  changePct: number
  history: number[] | null
  local?: { price: number; currency: string; unit: string }
  error?: string
}
type W<T> = { data?: T; error?: string; off?: boolean }
export type CheckResult = { name: string; target: string; up: boolean; pending?: boolean; ms?: number; error?: string; since?: string }
export type WidgetData = { status: W<CheckResult[]>; weather: W<Weather[]>; news: W<News[]>; currency: W<CurrencyData>; markets: W<Quote[]>; settings: WidgetSettings }

export type Role = 'admin' | 'manager' | 'member' | 'guest'
export type AuthStatus = { setupRequired: boolean; authenticated: boolean; user?: string; role?: Role; apps?: string[]; version: string; repo?: string; author?: string; authorUrl?: string }
export type ReleaseNote = { version: string; notes?: string; url?: string; published?: string }
export type SelfStatus = { supported: boolean; reason?: string; current: string; latest?: string; available: boolean; notes?: string; url?: string; checked?: string; error?: string; repo?: string; versions?: ReleaseNote[]; more?: number }
export type NetTest = { host: string; steps: { name: string; ok: boolean; detail: string; ms?: number }[]; verdict: string; fix?: string; works: boolean }
export type Person = { username: string; role: Role; disabled: boolean; apps: string[]; created?: string }
const RANK: Record<Role, number> = { guest: 1, member: 2, manager: 3, admin: 4 }
/** True when a role is at least as powerful as another. The server checks this too. */
export const atLeast = (role: Role | undefined, min: Role) => (role ? RANK[role] : 0) >= RANK[min]

export class ApiError extends Error {
  status: number
  checks?: Check[]
  constructor(message: string, status: number, checks?: Check[]) {
    super(message)
    this.status = status
    this.checks = checks
  }
}

async function request<T>(method: string, url: string, body?: unknown): Promise<T> {
  const res = await fetch(url, {
    method,
    credentials: 'same-origin',
    headers: { 'X-Haven': '1', ...(body !== undefined ? { 'Content-Type': 'application/json' } : {}) },
    body: body !== undefined ? JSON.stringify(body) : undefined,
  })
  let data: any = null
  try {
    data = await res.json()
  } catch {
    /* empty body */
  }
  if (!res.ok) {
    if (res.status === 401 && !url.startsWith('/api/auth/')) {
      window.dispatchEvent(new Event('haven:unauthorized'))
    }
    throw new ApiError(data?.error || `Request failed (${res.status})`, res.status, data?.checks)
  }
  return data as T
}

export type FileEntry = { name: string; path: string; dir: boolean; size: number; mod: string; mode: string; link?: boolean }
export type FileListing = { path: string; parent?: string; entries: FileEntry[]; total: number }
export type FilesInfo = { full: boolean; roots: { name: string; path: string; extra?: boolean }[] }
export type CleanupItem = { id: string; title: string; info: string; bytes: number; note?: string; hint?: string; path?: string }
export type FolderSize = { name: string; path: string; bytes: number }

const q = encodeURIComponent

export const api = {
  widgetTest: (host: string) => request<NetTest>('GET', `/api/widgets/test?host=${encodeURIComponent(host)}`),
  selfUpdate: () => request<SelfStatus>('GET', '/api/haven/update'),
  selfUpdateCheck: () => request<SelfStatus>('POST', '/api/haven/update/check'),
  selfUpdateApply: () => request<Job>('POST', '/api/haven/update/apply'),
  people: () => request<{ people: Person[] }>('GET', '/api/users'),
  addPerson: (b: { username: string; password: string; role: Role; apps: string[] }) => request<Person>('POST', '/api/users', b),
  updatePerson: (name: string, b: { role?: Role; apps?: string[]; disabled?: boolean }) => request<Person>('PUT', `/api/users/${encodeURIComponent(name)}`, b),
  deletePerson: (name: string) => request('DELETE', `/api/users/${encodeURIComponent(name)}`),
  resetPerson: (name: string, password: string) => request('POST', `/api/users/${encodeURIComponent(name)}/password`, { password }),
  transferAdmin: (to: string, password: string) => request('POST', '/api/users/transfer', { to, password }),
  filesInfo: () => request<FilesInfo>('GET', '/api/files/info'),
  filesList: (path: string) => request<FileListing>('GET', `/api/files/list?path=${q(path)}`),
  filesRead: (path: string) => request<{ content: string }>('GET', `/api/files/read?path=${q(path)}`),
  filesWrite: (path: string, content: string) => request('PUT', '/api/files/write', { path, content }),
  filesMkdir: (path: string) => request('POST', '/api/files/mkdir', { path }),
  filesRename: (path: string, name: string) => request('POST', '/api/files/rename', { path, name }),
  filesMove: (paths: string[], to: string, copy: boolean) => request('POST', '/api/files/move', { paths, to, copy }),
  filesDelete: (paths: string[]) => request('POST', '/api/files/delete', { paths }),
  filesAccess: (body: { full?: boolean; accept?: boolean; add?: string; remove?: string }) => request<FilesInfo>('POST', '/api/files/access', body),
  filesDownloadUrl: (path: string, inline = false) => `/api/files/download?path=${q(path)}${inline ? '&inline=1' : ''}`,
  filesUpload: (dir: string, file: File, onProgress: (frac: number) => void) =>
    new Promise<void>((resolve, reject) => {
      const x = new XMLHttpRequest()
      x.open('POST', `/api/files/upload?path=${q(dir)}`)
      x.setRequestHeader('X-Haven', '1')
      x.upload.onprogress = (e) => e.lengthComputable && onProgress(e.loaded / e.total)
      x.onload = () => {
        if (x.status >= 200 && x.status < 300) return resolve()
        let msg = `Upload failed (${x.status})`
        try {
          msg = JSON.parse(x.responseText).error || msg
        } catch {
          /* not json */
        }
        reject(new Error(msg))
      }
      x.onerror = () => reject(new Error('Upload failed: connection lost'))
      const fd = new FormData()
      fd.append('file', file, file.name)
      x.send(fd)
    }),
  cleanupScan: () => request<{ items: CleanupItem[] }>('GET', '/api/cleanup'),
  cleanupRun: (ids: string[]) => request<Job>('POST', '/api/cleanup/run', { ids }),
  cleanupSizes: (path: string) => request<{ path: string; items: FolderSize[] }>('GET', `/api/cleanup/sizes?path=${q(path)}`),

  status: () => request<AuthStatus>('GET', '/api/auth/status'),
  setup: (token: string, username: string, password: string) => request('POST', '/api/auth/setup', { token, username, password }),
  login: (username: string, password: string) => request('POST', '/api/auth/login', { username, password }),
  logout: () => request('POST', '/api/auth/logout'),
  changePassword: (current: string, next: string) => request('POST', '/api/auth/password', { current, new: next }),
  system: () => request<SystemInfo>('GET', '/api/system'),
  catalog: () => request<CatalogApp[]>('GET', '/api/catalog'),
  preflight: (catalogId: string, id: string, values: Record<string, string>) =>
    request<{ checks: Check[] }>('POST', '/api/preflight', { catalogId, id, values }),
  apps: () => request<{ apps: InstalledApp[]; containers: Container[]; dockerError?: string }>('GET', '/api/apps'),
  app: (id: string) =>
    request<{ meta: InstalledApp; compose: string; env: Record<string, string>; settings?: Setting[] }>('GET', `/api/apps/${id}`),
  install: (catalogId: string, id: string, values: Record<string, string>) => request<Job>('POST', '/api/apps', { catalogId, id, values }),
  installCustom: (id: string, name: string, compose: string, webPort: string) =>
    request<Job>('POST', '/api/apps/custom', { id, name, compose, webPort }),
  action: (id: string, action: 'start' | 'stop' | 'restart' | 'update') => request<Job>('POST', `/api/apps/${id}/${action}`),
  reconfigure: (id: string, values: Record<string, string>) => request<Job>('PUT', `/api/apps/${id}/settings`, { values }),
  uninstall: (id: string, removeData: boolean) => request<Job>('DELETE', `/api/apps/${id}${removeData ? '?removeData=1' : ''}`),
  logs: (id: string, tail = 300) => request<{ logs: string }>('GET', `/api/apps/${id}/logs?tail=${tail}`),
  containerAction: (id: string, action: 'start' | 'stop' | 'restart') => request('POST', `/api/containers/${id}/${action}`),
  containerLogs: (id: string, tail = 300) => request<{ logs: string }>('GET', `/api/containers/${id}/logs?tail=${tail}`),
  job: (id: string) => request<Job>('GET', `/api/jobs/${id}`),
  jobs: () => request<Job[]>('GET', '/api/jobs'),
  tvStart: () => request<{ code: string; id: string; expiresIn: number }>('POST', '/api/auth/tv/start'),
  tvPoll: (id: string) => request<{ status: 'pending' | 'approved' | 'expired' }>('POST', '/api/auth/tv/poll', { id }),
  tvApprove: (code: string) => request('POST', '/api/auth/tv/approve', { code }),
  diagnoseApp: (id: string) => request<Diagnosis>('POST', `/api/apps/${id}/diagnose`),
  diagnoseServer: () => request<Diagnosis>('GET', '/api/diagnose'),
  power: () => request<PowerStatus>('GET', '/api/host/power'),
  setPower: (body: { keepAwake?: boolean; screenOff?: boolean }) => request<PowerStatus>('POST', '/api/host/power', body),
  localName: () => request<LocalNameStatus>('GET', '/api/host/name'),
  setLocalName: (name: string, port80: boolean) => request<Job>('POST', '/api/host/name', { name, port80 }),
  setPort80: (on: boolean) => request<LocalNameStatus>('POST', '/api/host/port80', { on }),
  remote: () => request<RemoteStatus>('GET', '/api/remote'),
  remoteSetup: () => request<Job>('POST', '/api/remote/setup'),
  remoteAction: (action: 'down' | 'logout') => request<RemoteStatus>('POST', `/api/remote/${action}`),
  insights: () => request<InsightsInfo>('GET', '/api/insights'),
  publicAddress: () => request<PublicInfo>('POST', '/api/insights/public'),
  lanDevices: () => request<DevicesSnapshot>('GET', '/api/insights/devices'),
  scanDevices: () => request<DevicesSnapshot>('POST', '/api/insights/devices/scan'),
  labelDevice: (mac: string, label: string) => request('PUT', '/api/insights/devices', { mac, label }),
  forgetDevice: (mac: string) => request('PUT', '/api/insights/devices', { mac, forget: true }),
  accessLog: () => request<AccessSnapshot>('GET', '/api/logs/access'),
  avahiLog: () => request<{ log: string }>('GET', '/api/logs/avahi'),
  testName: () => request<Diagnosis>('GET', '/api/host/name/test'),
  lanOnly: () => request<{ interfaces: string[] }>('POST', '/api/host/name/lan-only'),
  updates: () => request<UpdatesSnapshot>('GET', '/api/updates'),
  checkUpdates: () => request('POST', '/api/updates/check'),
  applyUpdates: (ids: string[], prune: boolean) => request<Job>('POST', '/api/updates/apply', { ids, prune }),
  notifications: () => request<{ items: Notification[]; unread: number }>('GET', '/api/notifications'),
  readNotifications: (ids: number[] | 'all') => request('POST', '/api/notifications/read', ids === 'all' ? { all: true } : { ids }),
  clearNotifications: (all: boolean) => request('POST', '/api/notifications/clear', { all }),
  network: () => request<NetworkStatus>('GET', '/api/network'),
  wifiScan: () => request<WifiNetwork[]>('GET', '/api/network/wifi'),
  wifiConnect: (ssid: string, password: string, hidden: boolean) => request<Job>('POST', '/api/network/wifi/connect', { ssid, password, hidden }),
  wifiForget: (uuid: string) => request('POST', '/api/network/wifi/forget', { uuid }),
  setStatic: (b: StaticIP) => request<Job>('POST', '/api/network/static', b),
  networkSetup: () => request<Job>('POST', '/api/network/setup'),
  ui: () => request<UISettings>('GET', '/api/ui'),
  saveUI: (u: UISettings) => request<UISettings>('PUT', '/api/ui', u),
  widgets: () => request<WidgetData>('GET', '/api/widgets'),
  checkFeed: (url: string) => request<{ title: string; count: number }>('POST', '/api/widgets/feed-check', { url }),
  places: (q: string) => request<Place[]>('GET', `/api/widgets/places?q=${encodeURIComponent(q)}`),
}

export type NetDevice = { name: string; type: string; state: string; connection?: string; connUuid?: string; ips: string[]; gateway?: string; dns: string[]; managed: boolean }
export type WifiNetwork = { ssid: string; signal: number; security: string; inUse: boolean; saved: boolean }
export type SavedWifi = { name: string; uuid: string; auto: boolean; active: boolean }
export type NetworkStatus = {
  supported: boolean
  reason?: string
  nmReady: boolean
  installed: boolean
  devices: NetDevice[]
  wifiOn: boolean
  wifiDev?: string
  saved: SavedWifi[]
  internet: boolean
  jobId?: string
}
export type StaticIP = { uuid: string; auto?: boolean; address?: string; prefix?: number; gateway?: string; dns?: string[] }

export type AccessEntry = { seq: number; time: string; ip: string; host: string; port: string; method: string; path: string; status: number; ms: number; bytes: number; agent: string }
export type ConnEvent = { seq: number; time: string; ip: string; port: string; kind: string; waitedMs: number; note: string }
export type ClientInfo = { ip: string; lastSeen: string; hosts: string[]; requests: number; errors: number; agent: string }
export type AccessSnapshot = { entries: AccessEntry[]; events: ConnEvent[]; clients: ClientInfo[]; hidden: number; since: string }

export type ImgStatus = { ref: string; status: 'current' | 'update' | 'restart' | 'unknown'; detail?: string }
export type AppUpdate = { id: string; name: string; icon?: string; version?: string; catalogVersion?: string; images: ImgStatus[]; hasUpdate: boolean; checked: string; busy?: string }
export type SystemUpdates = { supported: boolean; dockerCurrent?: string; dockerNew?: string; upgradable: number; command?: string }
export type UpdatesSnapshot = { apps: AppUpdate[]; checked: string; checking: boolean; system: SystemUpdates; lastError?: string }
export type Notification = { id: number; time: string; level: 'info' | 'ok' | 'warn' | 'bad'; title: string; body?: string; route?: string; job?: string; read: boolean }

/** Tells every screen that widget or background settings changed. */
export function announceUIChange() {
  window.dispatchEvent(new Event('haven:ui'))
}

export function formatMoney(n: number, currency: string, digits?: number): string {
  try {
    return new Intl.NumberFormat(undefined, { style: 'currency', currency, maximumFractionDigits: digits ?? (n >= 1000 ? 0 : 2) }).format(n)
  } catch {
    return `${n.toFixed(2)} ${currency}`
  }
}

export function formatNumber(n: number, digits = 2): string {
  return new Intl.NumberFormat(undefined, { maximumFractionDigits: n >= 1000 ? 0 : digits }).format(n)
}

export function timeAgo(iso: string): string {
  const s = Math.round((Date.now() - new Date(iso).getTime()) / 1000)
  if (!isFinite(s) || s < 0) return ''
  if (s < 3600) return `${Math.max(1, Math.round(s / 60))} min ago`
  if (s < 86400) return `${Math.round(s / 3600)} h ago`
  return `${Math.round(s / 86400)} d ago`
}

/** Short status words, written like a device's status display. */
export function statusWord(status: string, busy?: string): string {
  if (busy) return `${busy}…`
  return (
    { running: 'Running', starting: 'Starting', partial: 'Partial', stopped: 'Stopped', error: 'Fault', unknown: 'Unknown' } as Record<string, string>
  )[status] || status
}

export function ledState(status: string, busy?: string): 'ok' | 'warn' | 'bad' | 'busy' | 'off' {
  if (busy) return 'busy'
  if (status === 'running') return 'ok'
  if (status === 'starting' || status === 'partial') return 'warn'
  if (status === 'error') return 'bad'
  return 'off'
}

export function containerLed(c: Container): 'ok' | 'warn' | 'bad' | 'off' {
  if (c.state === 'running') return c.health === 'unhealthy' ? 'bad' : c.health === 'starting' ? 'warn' : 'ok'
  if (c.state === 'restarting' || c.state === 'dead') return 'bad'
  if (c.state === 'exited' && !c.status.startsWith('Exited (0)')) return 'bad'
  return 'off'
}

export function isTvDevice(): boolean {
  return /SmartTV|SMART-TV|Tizen|Web0S|webOS|NetCast|BRAVIA|HbbTV|Viera|AFT[A-Z]|CrKey|GoogleTV|Android TV|AppleTV|Roku|Opera TV/i.test(navigator.userAgent)
}

export function appUrl(web: WebSpec | undefined, port: string | undefined): string | null {
  if (!web || !port) return null
  const scheme = web.scheme || 'http'
  return `${scheme}://${window.location.hostname}:${port}${web.path || ''}`
}

export function formatBytes(n: number, digits = 1): string {
  if (!n || n < 0) return '0 B'
  const units = ['B', 'KB', 'MB', 'GB', 'TB', 'PB']
  const i = Math.min(Math.floor(Math.log(n) / Math.log(1024)), units.length - 1)
  return `${(n / Math.pow(1024, i)).toFixed(i === 0 ? 0 : digits)} ${units[i]}`
}

export function formatRate(n: number): string {
  return `${formatBytes(n)}/s`
}

export function formatUptime(s: number): string {
  const d = Math.floor(s / 86400)
  const h = Math.floor((s % 86400) / 3600)
  const m = Math.floor((s % 3600) / 60)
  if (d > 0) return `${d}d ${String(h).padStart(2, '0')}h`
  if (h > 0) return `${h}h ${String(m).padStart(2, '0')}m`
  return `${m}m`
}

/** Splits a byte count into a number and unit for instrument-style readouts. */
export function splitBytes(n: number, digits = 1): [string, string] {
  const [v, u] = formatBytes(n, digits).split(' ')
  return [v, u]
}

export function timeOf(iso: string): string {
  const d = new Date(iso)
  return `${String(d.getHours()).padStart(2, '0')}:${String(d.getMinutes()).padStart(2, '0')}`
}
