import { getAuthToken } from '../auth/token'
import { API_BASE_URL } from '../config/api'
import { requestEnvelope } from './http'
import type {
  SandboxTerminalRequest,
  SandboxTerminalResult,
  SandboxTerminalWSClientMessage,
  TerminalSessionEvent,
} from './types'

export async function detectSandboxShell(name: string): Promise<string> {
  const result = await requestEnvelope<{ shell: string }>(`/terminal/sandbox/${encodeURIComponent(name)}/detect-shell`, {
    method: 'GET',
  })
  return result.shell
}

export async function executeSandboxTerminal(name: string, payload: SandboxTerminalRequest): Promise<SandboxTerminalResult> {
  return requestEnvelope<SandboxTerminalResult>(`/terminal/sandbox/${encodeURIComponent(name)}`, {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
    },
    body: JSON.stringify(payload),
  })
}

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

export function buildTerminalWsUrl(name: string): string {
  const wsBase = toWebSocketBase(API_BASE_URL).replace(/\/$/, '')
  const url = new URL(`${wsBase}/terminal/sandbox/${encodeURIComponent(name)}/ws`)
  const token = getAuthToken()
  if (token) {
    url.searchParams.set('api_key', token)
  }
  return url.toString()
}

export function createSandboxTerminalSession(name: string, onEvent: (event: TerminalSessionEvent) => void) {
  const ws = new WebSocket(buildTerminalWsUrl(name))

  ws.addEventListener('open', () => {
    onEvent({ type: 'open' })
  })

  ws.addEventListener('message', (event) => {
    let parsed: unknown
    try {
      parsed = JSON.parse(String(event.data))
    } catch {
      onEvent({ type: 'error', message: 'Invalid terminal message payload.' })
      return
    }

    onEvent({ type: 'message', message: parsed })
  })

  ws.addEventListener('error', () => {
    onEvent({ type: 'error', message: 'Terminal connection error.' })
  })

  ws.addEventListener('close', () => {
    onEvent({ type: 'close' })
  })

  return {
    send(message: SandboxTerminalWSClientMessage) {
      if (ws.readyState === WebSocket.OPEN) {
        ws.send(JSON.stringify(message))
      }
    },
    close() {
      if (ws.readyState === WebSocket.OPEN || ws.readyState === WebSocket.CONNECTING) {
        ws.close()
      }
    },
  }
}
