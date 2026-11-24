// The Overview widgets: their keys, names and default order.
export type WKey = 'weather' | 'aqi' | 'markets' | 'currency' | 'news' | 'clock' | 'notes' | 'links' | 'status' | 'backup'
export type WidgetWidth = 'compact' | 'half' | 'full'
export type WidgetSizes = Partial<Record<WKey, { width: WidgetWidth }>>

export const W_NAMES: Record<WKey, string> = { weather: 'Weather', aqi: 'Air Quality', markets: 'Markets', currency: 'Currency', news: 'News', clock: 'Clock', notes: 'Notes', links: 'Quick links', status: 'Status', backup: 'Backup health' }

export const W_DEFAULT: WKey[] = ['weather', 'aqi', 'markets', 'currency', 'news', 'clock', 'notes', 'links', 'status', 'backup']

// These can name addresses inside the home network, so only managers and the admin see them.
export const PRIVATE_KEYS: WKey[] = ['notes', 'links', 'status', 'backup']
