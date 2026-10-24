// Which kind of screen this device is: a TV driven by a remote, or a normal
// computer or phone. Chosen once on the first screen and remembered here.

export type DeviceMode = 'tv' | 'normal'
const KEY = 'haven-device-mode'

export function getMode(): DeviceMode | null {
  try {
    const v = localStorage.getItem(KEY)
    return v === 'tv' || v === 'normal' ? v : null
  } catch {
    return null
  }
}

export function setMode(m: DeviceMode | null) {
  try {
    if (m) localStorage.setItem(KEY, m)
    else localStorage.removeItem(KEY)
  } catch {
    /* storage blocked */
  }
}
