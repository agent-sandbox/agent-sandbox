import { getAuthToken } from '../auth/token'
import { API_BASE_URL } from '../config/api'
import type { TrafficFlow } from './types'

export type TrafficSessionEvent =
  | { type: 'open' }
  | { type: 'flow'; flow: TrafficFlow }
  | { type: 'error'; message: string }
  | { type: 'close' }

function toWebSocketBase(baseUrl: string): string {
  if (baseUrl.startsWith('http://')) {
    return `ws://${baseUrl.slice('http://'.length)}`
  }
  if (baseUrl.startsWith('https://')) {
    return `wss://${baseUrl.slice('https://'.length)}`
  }

  const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:'
  const normalizedBase = baseUrl.startsWith('/') ? baseUrl : `/${baseUrl}`
  return `${protocol}//${window.location.host}${normalizedBase}`
}

export function buildTrafficWsUrl(name: string): string {
  const wsBase = toWebSocketBase(API_BASE_URL).replace(/\/$/, '')
  const url = new URL(`${wsBase}/traffic/sandbox/${encodeURIComponent(name)}/ws`)
  const token = getAuthToken()
  if (token) {
    url.searchParams.set('api_key', token)
  }
  return url.toString()
}

export function createSandboxTrafficSession(
  name: string,
  onEvent: (event: TrafficSessionEvent) => void,
) {
  const ws = new WebSocket(buildTrafficWsUrl(name))

  ws.addEventListener('open', () => {
    onEvent({ type: 'open' })
  })

  ws.addEventListener('message', (event) => {
    let flow: TrafficFlow
    try {
      flow = JSON.parse(String(event.data)) as TrafficFlow
    } catch {
      onEvent({ type: 'error', message: 'Failed to parse traffic message' })
      return
    }
    onEvent({ type: 'flow', flow })
  })

  ws.addEventListener('error', () => {
    onEvent({ type: 'error', message: 'WebSocket error' })
  })

  ws.addEventListener('close', () => {
    onEvent({ type: 'close' })
  })

  return {
    close() {
      if (ws.readyState === WebSocket.OPEN || ws.readyState === WebSocket.CONNECTING) {
        ws.close()
      }
    },
  }
}
