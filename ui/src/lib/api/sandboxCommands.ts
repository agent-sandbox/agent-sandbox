import { requestEnvelope } from './http'
import type { SandboxCommandAuditData, SandboxCommandAuditSandboxesData } from './types'

type ListSandboxCommandsOptions = {
  tenantId?: string
  sandbox?: string
  search?: string
  from?: string
  to?: string
  limit?: number
}

type ListSandboxCommandSandboxesOptions = {
  tenantId?: string
  search?: string
  limit?: number
}

function appendTextParam(params: URLSearchParams, key: string, value: string | undefined) {
  const trimmed = value?.trim() ?? ''
  if (trimmed) {
    params.set(key, trimmed)
  }
}

function appendLimitParam(params: URLSearchParams, limit: number | undefined) {
  if (typeof limit === 'number' && Number.isFinite(limit) && limit > 0) {
    params.set('limit', String(Math.trunc(limit)))
  }
}

export async function listSandboxCommands(options?: ListSandboxCommandsOptions): Promise<SandboxCommandAuditData> {
  const params = new URLSearchParams()

  appendTextParam(params, 'tenant_id', options?.tenantId)
  appendTextParam(params, 'sandbox', options?.sandbox)
  appendTextParam(params, 'search', options?.search)
  appendTextParam(params, 'from', options?.from)
  appendTextParam(params, 'to', options?.to)
  appendLimitParam(params, options?.limit)

  const query = params.toString()
  const path = `/sandbox-commands${query ? `?${query}` : ''}`

  return requestEnvelope<SandboxCommandAuditData>(path, {
    method: 'GET',
  })
}

export async function listSandboxCommandSandboxes(options?: ListSandboxCommandSandboxesOptions): Promise<SandboxCommandAuditSandboxesData> {
  const params = new URLSearchParams()

  appendTextParam(params, 'tenant_id', options?.tenantId)
  appendTextParam(params, 'search', options?.search)
  appendLimitParam(params, options?.limit)

  const query = params.toString()
  const path = `/sandbox-commands/sandboxes${query ? `?${query}` : ''}`

  return requestEnvelope<SandboxCommandAuditSandboxesData>(path, {
    method: 'GET',
  })
}
