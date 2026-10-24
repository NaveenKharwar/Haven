import { createContext, useContext } from 'react'
import type { CatalogApp, Container, InstalledApp, Role, SystemInfo } from './api'

export type Route = 'overview' | 'apps' | 'catalog' | 'updates' | 'server' | 'settings'

export type HavenCtx = {
  user: string
  role: Role
  version: string
  repo?: string
  author: string
  authorUrl?: string
  apps: InstalledApp[]
  appsReady: boolean // false until the first answer, so pages show a placeholder, not "nothing installed"
  containers: Container[]
  dockerError?: string
  catalog: CatalogApp[]
  sys: SystemInfo | null
  updatesCount: number
  refreshApps: () => void
  openJob: (jobId: string, title?: string) => void
  openApp: (id: string) => void
  navigate: (route: Route) => void
}

export const Ctx = createContext<HavenCtx | null>(null)

export function useHaven(): HavenCtx {
  const c = useContext(Ctx)
  if (!c) throw new Error('Haven context missing')
  return c
}
