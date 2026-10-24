// The Overview widgets: their keys, names and default order.
export type WKey = 'weather' | 'markets' | 'currency' | 'news' | 'clock' | 'notes' | 'links' | 'status'

export const W_NAMES: Record<WKey, string> = { weather: 'Weather', markets: 'Markets', currency: 'Currency', news: 'News', clock: 'Clock', notes: 'Notes', links: 'Quick links', status: 'Status' }

export const W_DEFAULT: WKey[] = ['weather', 'markets', 'currency', 'news', 'clock', 'notes', 'links', 'status']

// These can name addresses inside the home network, so only managers and the admin see them.
export const PRIVATE_KEYS: WKey[] = ['notes', 'links', 'status']
