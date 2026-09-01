import { requestEnvelope } from './http'
import type { RateLimitUserStatus } from './types'

export type SandboxSummary = {
  total: number
}

export type CapacityDefaultConfig = {
  enabled: boolean
  max_concurrency: number
  max_sandbox: number
}

export type CapacitySummary = {
  default_config: CapacityDefaultConfig
  user_total: number
  users: RateLimitUserStatus[]
}

export type DashboardStatusData = {
  leader: string
  sandboxes: SandboxSummary
  capacity: CapacitySummary
}

// Public endpoint — no auth header required. The Dashboard's pre-login view
// uses this; the same call also serves the post-login view.
export async function getDashboardStatus(): Promise<DashboardStatusData> {
  return requestEnvelope<DashboardStatusData>('/status', { method: 'GET' })
}
