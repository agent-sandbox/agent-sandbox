import { requestEnvelope } from './http'
import type { CreateSandboxRequest, Sandbox, SandboxMetricsData, ServerInfo } from './types'

export async function getServerInfo(): Promise<ServerInfo> {
  return requestEnvelope<ServerInfo>('/config/info', { method: 'GET' })
}

export async function listSandboxes(): Promise<Sandbox[]> {
  const data = await requestEnvelope<Sandbox[] | undefined>('/sandbox', {
    method: 'GET',
  })

  return Array.isArray(data) ? data : []
}

export async function createSandbox(payload: CreateSandboxRequest = {}): Promise<Sandbox> {
  return requestEnvelope<Sandbox>('/sandbox', {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
    },
    body: JSON.stringify(payload),
  })
}

export async function deleteSandbox(name: string): Promise<string> {
  return requestEnvelope<string>(`/sandbox/${encodeURIComponent(name)}`, {
    method: 'DELETE',
  })
}

export async function getSandboxMetrics(names: string[]): Promise<SandboxMetricsData> {
  return requestEnvelope<SandboxMetricsData>('/sandbox/metrics', {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
    },
    body: JSON.stringify({ names }),
  })
}
