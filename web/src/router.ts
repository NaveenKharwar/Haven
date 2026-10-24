// Pages live at real paths (/updates, /settings, /tv). Older links used the form /#/updates;
// those are rewritten to the plain path when the page opens.

const NAV = 'haven:navigate'

export function readPath(): string {
  return window.location.pathname.replace(/^\/+|\/+$/g, '')
}

/** Moves to a page without reloading. */
export function go(path: string) {
  const to = '/' + path.replace(/^\/+/, '')
  if (window.location.pathname !== to || window.location.hash) window.history.pushState(null, '', to)
  window.dispatchEvent(new Event(NAV))
}

export function subscribePath(cb: () => void) {
  window.addEventListener('popstate', cb)
  window.addEventListener(NAV, cb)
  return () => {
    window.removeEventListener('popstate', cb)
    window.removeEventListener(NAV, cb)
  }
}

/** Turns an old /#/page address into /page. */
export function upgradeLegacyHash() {
  const m = window.location.hash.match(/^#\/?([a-z]+)/)
  if (m) window.history.replaceState(null, '', '/' + m[1])
}
