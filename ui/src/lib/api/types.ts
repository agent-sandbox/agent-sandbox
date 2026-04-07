export type ApiEnvelope<T> = {
  code: string
  data?: T
  error?: string
}

export type ServerInfo = {
  proxyDomain: string
  version: string
}

export type Sandbox = {
  name?: string
  template?: string
  image?: string
  status?: string
  id?: string
  port?: string
  app?: string
  timeout?: number
  created_at?: string
  cpu?: string
  memory?: string
  cpu_limit?: string
  memory_limit?: string
  metadata?: Record<string, string>
  extra_ports?: number[]
}

export type CreateSandboxRequest = {
  name?: string
  template?: string
  image?: string
  timeout?: number
}

export type TemplateResources = {
  cpu?: string
  memory?: string
  cpuLimit?: string
  memoryLimit?: string
}

export type TemplatePool = {
  size?: number
  readySize?: number
  probePort?: number
  warmupCmd?: string
  startupCmd?: string
  resources?: TemplateResources
}

export type Template = {
  name?: string
  pattern?: string
  image?: string
  port?: number
  type?: string
  metadata?: Record<string, string>
  noStartupProbe?: boolean
  args?: string[]
  extraPorts?: number[]
  envVars?: Record<string, string>
  shell?: string
  resources?: TemplateResources
  pool?: TemplatePool
  description?: string
}

export type PoolTemplate = Template

export type PoolSandbox = Sandbox

export type SandboxLogsData = {
  sandbox: string
  pod: string
  container: string
  logs: string
  fetchedAt: string
}

export type SandboxTerminalRequest = {
  command: string
}

export type SandboxTerminalResult = {
  sandbox: string
  pod: string
  container: string
  command: string
  output: string
  errorOutput: string
  executedAt: string
}

export type SandboxTerminalWSClientMessage =
  | {
      type: 'init'
      cols: number
      rows: number
    }
  | {
      type: 'input'
      data: string
    }
  | {
      type: 'resize'
      cols: number
      rows: number
    }
  | {
      type: 'close'
    }

export type SandboxTerminalWSServerMessage = {
  type: 'ready' | 'output' | 'error' | 'exit' | 'closed'
  data?: string
  code?: number
}

export type TerminalConnectionState = 'disconnected' | 'connecting' | 'connected'

export type TerminalSessionEvent =
  | { type: 'open' }
  | { type: 'message'; message: unknown }
  | { type: 'error'; message: string }
  | { type: 'close' }

export type SandboxFileEntry = {
  name: string
  path: string
  isDir: boolean
  size: number
}

export type SandboxFilesListData = {
  sandbox: string
  pod: string
  container: string
  path: string
  entries: SandboxFileEntry[]
  fetchedAt: string
}

export type SandboxFileUploadData = {
  sandbox: string
  pod: string
  container: string
  path: string
  fileName: string
  uploadedAt: string
}

export type SandboxFileDeleteData = {
  sandbox: string
  pod: string
  container: string
  path: string
  deletedAt: string
}

export type SandboxFileDownloadResult = {
  blob: Blob
  fileName: string
}

export type SandboxMetricsItem = {
  sandbox: string
  pod: string
  cpuMilli: number
  memoryBytes: number
  memoryMB: number
  sampledAt: string
}

export type SandboxMetricsData = {
  items: Record<string, SandboxMetricsItem>
}

export type SandboxEventInvolvedObject = {
  kind: string
  name: string
  namespace: string
  apiVersion: string
  fieldPath: string
}

export type SandboxEventItem = {
  name: string
  reason: string
  type: string
  message: string
  count: number
  eventTime: string
  firstTimestamp: string
  lastTimestamp: string
  involvedObject: SandboxEventInvolvedObject
}

export type SandboxEventsData = {
  items: SandboxEventItem[]
  fetchedAt: string
}

export type TrafficFlow = {
  type: 'flow' | 'error'
  timestamp: number
  method?: string
  url: string
  host?: string
  path?: string
  status?: number
  req_size?: number
  res_size?: number
  req_body?: string
  res_body?: string
  req_headers?: Record<string, string>
  res_headers?: Record<string, string>
  content_type?: string
  duration_ms?: number
  message?: string
}

export type TrafficWSServerMessage = TrafficFlow
