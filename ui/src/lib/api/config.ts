import { requestEnvelope } from './http'
import type { RuntimeConfigPayload, RuntimeConfigStatus, Template } from './types'

export async function getRuntimeConfigStatus(): Promise<RuntimeConfigStatus> {
  return requestEnvelope<RuntimeConfigStatus>('/config/runtime', {
    method: 'GET',
  })
}

export async function saveRuntimeConfig(payload: RuntimeConfigPayload): Promise<RuntimeConfigStatus> {
  return requestEnvelope<RuntimeConfigStatus>('/config/runtime', {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
    },
    body: JSON.stringify(payload),
  })
}

export async function getTemplatesConfig(): Promise<string> {
  return requestEnvelope<string>('/config/templates', {
    method: 'GET',
  })
}

export async function saveTemplatesConfig(payload: Template[]): Promise<string> {
  return requestEnvelope<string>('/config/templates', {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
    },
    body: JSON.stringify(payload),
  })
}

export async function getSandboxBlueprintConfig(): Promise<string> {
  return requestEnvelope<string>('/config/blueprint', {
    method: 'GET',
  })
}

export async function saveSandboxBlueprintConfig(payload: string): Promise<string> {
  return requestEnvelope<string>('/config/blueprint', {
    method: 'POST',
    headers: {
      'Content-Type': 'text/plain; charset=utf-8',
    },
    body: payload,
  })
}
