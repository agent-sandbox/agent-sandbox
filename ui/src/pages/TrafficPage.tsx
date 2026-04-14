import { useEffect, useRef, useState } from 'react'
import { useSearchParams } from 'react-router-dom'

import { createSandboxTrafficSession } from '../lib/api/traffic'
import type { TrafficFlow } from '../lib/api/types'
import { listSandboxes } from '../lib/api/sandbox'
import type { Sandbox } from '../lib/api/types'

const MAX_FLOWS = 500

function formatBytes(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`
  return `${(bytes / (1024 * 1024)).toFixed(1)} MB`
}

function formatDateTime(ts: number): string {
  const d = new Date(ts * 1000)
  const date = d.toLocaleDateString([], { month: '2-digit', day: '2-digit' })
  const time = d.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit', second: '2-digit' })
  return `${date} ${time}`
}

function rowColorClass(status?: number, flowType?: string): string {
  if (flowType === 'error') return 'bg-red-900/30 text-red-300'
  if (!status) return ''
  if (status >= 500) return 'bg-red-900/20'
  if (status >= 400) return 'bg-orange-900/20'
  if (status >= 300) return 'bg-yellow-900/20'
  if (status >= 200) return 'bg-green-900/10'
  return ''
}

function statusBadgeClass(status?: number): string {
  if (!status) return 'badge badge-ghost badge-sm'
  if (status >= 500) return 'badge badge-error badge-sm'
  if (status >= 400) return 'badge badge-warning badge-sm'
  if (status >= 300) return 'badge badge-info badge-sm'
  if (status >= 200) return 'badge badge-success badge-sm'
  return 'badge badge-ghost badge-sm'
}

function tryFormatJSON(str?: string): { text: string; isJson: boolean } {
  if (!str) return { text: '', isJson: false }
  try {
    const parsed = JSON.parse(str)
    return { text: JSON.stringify(parsed, null, 2), isJson: true }
  } catch {
    return { text: str, isJson: false }
  }
}

function HeadersSection({ label, headers }: { label: string; headers?: Record<string, string> }) {
  if (!headers || Object.keys(headers).length === 0) return null
  return (
    <div>
      <div className="mb-1 text-xs font-semibold uppercase tracking-wide text-base-content/50">{label}</div>
      <div className="overflow-auto rounded bg-base-300 p-2 text-xs" style={{ maxHeight: '180px' }}>
        {Object.entries(headers).map(([k, v]) => (
          <div key={k} className="flex gap-2 py-0.5">
            <span className="shrink-0 font-mono text-primary/80">{k}:</span>
            <span className="break-all font-mono text-base-content/80">{v}</span>
          </div>
        ))}
      </div>
    </div>
  )
}

function BodySection({ label, body, contentType }: { label: string; body?: string; contentType?: string }) {
  if (!body) return null
  const isJsonType = contentType?.includes('json') ?? false
  const { text, isJson } = isJsonType || body.trimStart().startsWith('{') || body.trimStart().startsWith('[')
    ? tryFormatJSON(body)
    : { text: body, isJson: false }

  return (
    <div>
      <div className="mb-1 text-xs font-semibold uppercase tracking-wide text-base-content/50">{label}</div>
      <pre className={`overflow-auto rounded bg-base-300 p-2 text-xs ${isJson ? 'text-green-400' : 'text-base-content/80'}`} style={{ maxHeight: '240px', whiteSpace: 'pre-wrap', wordBreak: 'break-all' }}>
        {text}
      </pre>
    </div>
  )
}

function FlowDetail({ flow, onClose }: { flow: TrafficFlow; onClose: () => void }) {
  const content = (
    <>
      <div className="flex items-center justify-between border-b border-base-300 px-4 py-2">
        <div className="flex items-center gap-2">
          {flow.method && (
            <span className="badge badge-outline badge-xs font-mono">{flow.method}</span>
          )}
          {flow.status != null && (
            <span className={statusBadgeClass(flow.status)}>{flow.status}</span>
          )}
        </div>
        <button className="btn btn-ghost btn-xs" onClick={onClose} type="button">✕</button>
      </div>

      <div className="flex-1 overflow-auto p-4 space-y-4">
        <div>
          <div className="mb-1 text-xs font-semibold uppercase tracking-wide text-base-content/50">URL</div>
          <div className="break-all font-mono text-xs text-base-content/90">{flow.url}</div>
        </div>

        <div className="grid grid-cols-2 gap-3">
          <div>
            <div className="text-xs font-semibold uppercase tracking-wide text-base-content/50">Time</div>
            <div className="font-mono text-xs">{formatDateTime(flow.timestamp)}</div>
          </div>
          {flow.duration_ms != null && (
            <div>
              <div className="text-xs font-semibold uppercase tracking-wide text-base-content/50">Duration</div>
              <div className="font-mono text-xs">{flow.duration_ms} ms</div>
            </div>
          )}
          {flow.req_size != null && (
            <div>
              <div className="text-xs font-semibold uppercase tracking-wide text-base-content/50">Req size</div>
              <div className="font-mono text-xs">{formatBytes(flow.req_size)}</div>
            </div>
          )}
          {flow.res_size != null && (
            <div>
              <div className="text-xs font-semibold uppercase tracking-wide text-base-content/50">Res size</div>
              <div className="font-mono text-xs">{formatBytes(flow.res_size)}</div>
            </div>
          )}
          {flow.content_type && (
            <div className="col-span-2">
              <div className="text-xs font-semibold uppercase tracking-wide text-base-content/50">Content-Type</div>
              <div className="font-mono text-xs break-all">{flow.content_type}</div>
            </div>
          )}
        </div>

        {flow.type === 'error' && flow.message && (
          <div>
            <div className="mb-1 text-xs font-semibold uppercase tracking-wide text-base-content/50">Error</div>
            <div className="text-xs text-error">{flow.message}</div>
          </div>
        )}

        <HeadersSection label="Request headers" headers={flow.req_headers} />
        <BodySection label="Request body" body={flow.req_body} />
        <HeadersSection label="Response headers" headers={flow.res_headers} />
        <BodySection label="Response body" body={flow.res_body} contentType={flow.content_type} />

        {!flow.req_body && !flow.res_body && !flow.req_headers && !flow.res_headers && flow.type !== 'error' && (
          <p className="text-xs text-base-content/40">
            No body or headers captured. Update your <code>logger.py</code> ConfigMap to include <code>req_headers</code> / <code>res_headers</code>.
          </p>
        )}
      </div>
    </>
  )

  return (
    <>
      {/* Mobile: full-screen modal overlay */}
      <div className="fixed inset-0 z-50 flex flex-col bg-base-100 md:hidden">
        {content}
      </div>
      {/* Desktop: side panel */}
      <div className="hidden w-96 shrink-0 flex-col border-l border-base-300 bg-base-100 md:flex">
        {content}
      </div>
    </>
  )
}

export default function TrafficPage() {
  const [searchParams, setSearchParams] = useSearchParams()
  const [sandboxes, setSandboxes] = useState<Sandbox[]>([])
  const [selectedName, setSelectedName] = useState('')
  const [flows, setFlows] = useState<TrafficFlow[]>([])
  const [connected, setConnected] = useState(false)
  const [filter, setFilter] = useState('')
  const [methodFilter, setMethodFilter] = useState('All')
  const [protocolFilter, setProtocolFilter] = useState('All')
  const [sandboxesLoading, setSandboxesLoading] = useState(false)
  const [selectedFlow, setSelectedFlow] = useState<TrafficFlow | null>(null)

  const sessionRef = useRef<ReturnType<typeof createSandboxTrafficSession> | null>(null)
  const bottomRef = useRef<HTMLDivElement>(null)
  const tableRef = useRef<HTMLDivElement>(null)
  const autoScrollRef = useRef(true)
  const selectedNameRef = useRef('')
  selectedNameRef.current = selectedName

  useEffect(() => {
    setSandboxesLoading(true)
    listSandboxes()
      .then((list) => {
        setSandboxes(list)
        const fromQuery = searchParams.get('sandbox')?.trim() ?? ''
        const hasQueryTarget = Boolean(fromQuery) && list.some((s) => s.name === fromQuery)
        const next = hasQueryTarget ? fromQuery : list[0]?.name ?? ''
        setSelectedName(next)
        if (next) {
          const p = new URLSearchParams(searchParams)
          p.set('sandbox', next)
          setSearchParams(p, { replace: true })
        }
      })
      .catch(() => {})
      .finally(() => setSandboxesLoading(false))
  }, []) // eslint-disable-line react-hooks/exhaustive-deps

  useEffect(() => {
    if (!selectedName) return

    sessionRef.current?.close()
    setFlows([])
    setConnected(false)
    setSelectedFlow(null)

    const session = createSandboxTrafficSession(selectedName, (event) => {
      if (event.type === 'open') setConnected(true)
      if (event.type === 'close') setConnected(false)
      if (event.type === 'flow') {
        setFlows((prev) => [...prev.slice(-(MAX_FLOWS - 1)), event.flow])
      }
    })

    sessionRef.current = session
    return () => {
      session.close()
      setConnected(false)
    }
  }, [selectedName])

  useEffect(() => {
    if (autoScrollRef.current && bottomRef.current) {
      bottomRef.current.scrollIntoView({ behavior: 'smooth' })
    }
  }, [flows])

  const handleTableScroll = () => {
    const el = tableRef.current
    if (!el) return
    const atBottom = el.scrollHeight - el.scrollTop - el.clientHeight < 40
    autoScrollRef.current = atBottom
  }

  const handleSandboxChange = (name: string) => {
    setSelectedName(name)
    const p = new URLSearchParams(searchParams)
    if (name) {
      p.set('sandbox', name)
    } else {
      p.delete('sandbox')
    }
    setSearchParams(p, { replace: true })
  }

  const visibleFlows = flows.filter((f) => {
    if (filter && !f.url.toLowerCase().includes(filter.toLowerCase())) return false
    if (methodFilter !== 'All' && f.method !== methodFilter) return false
    if (protocolFilter !== 'All') {
      const isHttps = f.url.startsWith('https://')
      if (protocolFilter === 'HTTPS' && !isHttps) return false
      if (protocolFilter === 'HTTP' && isHttps) return false
    }
    return true
  })

  const methods = ['All', ...Array.from(new Set(flows.map((f) => f.method).filter(Boolean)))] as string[]

  return (
    <>
      <header className="card border border-base-300 bg-base-100 shadow-sm">
        <div className="card-body gap-3">
          <div className="flex flex-wrap items-center justify-between gap-2">
            <div>
              <h2 className="text-2xl font-semibold">Traffic Monitor</h2>
              <p className="text-sm text-base-content/70">
                Live HTTP/HTTPS traffic via mitmproxy sidecar. Requires sandbox started with{' '}
                <code className="text-xs">metadata.mitm=true</code>.
              </p>
            </div>
            <div className="flex items-center gap-2">
              <span className={`badge ${connected ? 'badge-success' : 'badge-ghost'} gap-1`}>
                <span>{connected ? '●' : '○'}</span>
                {connected ? 'LIVE' : 'DISCONNECTED'}
              </span>
              <button
                className="btn btn-sm btn-outline"
                type="button"
                onClick={() => setFlows([])}
              >
                Clear
              </button>
            </div>
          </div>

          <div className="flex flex-wrap items-center gap-2">
            <label className="flex items-center gap-2">
              <span className="text-sm">Sandbox</span>
              <select
                className="select select-sm select-bordered w-full max-w-xs"
                value={selectedName}
                onChange={(e) => handleSandboxChange(e.target.value)}
                disabled={sandboxesLoading || sandboxes.length === 0}
              >
                {sandboxes.length === 0 ? (
                  <option value="">No sandboxes</option>
                ) : (
                  sandboxes.map((s) => (
                    <option key={s.name ?? s.id} value={s.name ?? ''}>
                      {s.name ?? '-'}
                    </option>
                  ))
                )}
              </select>
            </label>

            <input
              className="input input-sm input-bordered w-full max-w-xs"
              placeholder="Filter by URL..."
              value={filter}
              onChange={(e) => setFilter(e.target.value)}
            />

            <label className="flex items-center gap-2">
              <span className="text-sm">Protocol</span>
              <select
                className="select select-sm select-bordered"
                value={protocolFilter}
                onChange={(e) => setProtocolFilter(e.target.value)}
              >
                <option>All</option>
                <option>HTTP</option>
                <option>HTTPS</option>
              </select>
            </label>

            <label className="flex items-center gap-2">
              <span className="text-sm">Method</span>
              <select
                className="select select-sm select-bordered"
                value={methodFilter}
                onChange={(e) => setMethodFilter(e.target.value)}
              >
                {methods.map((m) => (
                  <option key={m}>{m}</option>
                ))}
              </select>
            </label>
          </div>
        </div>
      </header>

      <section>
        <div className="card border border-base-300 bg-base-100 shadow-sm overflow-hidden">
          <div className="card-body p-0 flex flex-row">
            {!selectedName ? (
              <p className="p-6 text-sm text-base-content/60">Select a sandbox to begin monitoring traffic.</p>
            ) : (
              <>
                <div
                  ref={tableRef}
                  className="overflow-auto flex-1"
                  style={{ maxHeight: 'calc(100vh - 320px)' }}
                  onScroll={handleTableScroll}
                >
                  <table className="table table-xs table-pin-rows w-full">
                    <thead>
                      <tr>
                        <th className="w-36">Time</th>
                        <th className="w-20">Method</th>
                        <th>URL</th>
                        <th className="w-16">Status</th>
                        <th className="w-16">ms</th>
                        <th className="w-20">Size</th>
                      </tr>
                    </thead>
                    <tbody>
                      {visibleFlows.length === 0 ? (
                        <tr>
                          <td colSpan={6} className="py-6 text-center text-sm text-base-content/50">
                            {connected ? 'Waiting for traffic...' : 'Not connected — sandbox may not have mitm=true metadata.'}
                          </td>
                        </tr>
                      ) : (
                        visibleFlows.map((f, i) => (
                          <tr
                            key={i}
                            className={`cursor-pointer hover:brightness-110 ${rowColorClass(f.status, f.type)} ${selectedFlow === f ? 'outline outline-1 outline-primary/40' : ''}`}
                            onClick={() => setSelectedFlow(selectedFlow === f ? null : f)}
                          >
                            <td className="font-mono text-xs text-base-content/60 whitespace-nowrap">
                              {formatDateTime(f.timestamp)}
                            </td>
                            <td>
                              {f.method ? (
                                <span className="badge badge-outline badge-xs font-mono">{f.method}</span>
                              ) : (
                                <span className="text-base-content/40">—</span>
                              )}
                            </td>
                            <td
                              className="max-w-xs overflow-hidden text-ellipsis whitespace-nowrap font-mono text-xs"
                              title={f.url}
                            >
                              {f.type === 'error' ? (
                                <span className="text-error">{f.message ?? f.url}</span>
                              ) : (
                                f.url
                              )}
                            </td>
                            <td>
                              {f.status != null ? (
                                <span className={statusBadgeClass(f.status)}>{f.status}</span>
                              ) : (
                                <span className="text-base-content/40">—</span>
                              )}
                            </td>
                            <td className="text-right font-mono text-xs">
                              {f.duration_ms != null ? f.duration_ms : <span className="text-base-content/40">—</span>}
                            </td>
                            <td className="text-right font-mono text-xs">
                              {f.res_size != null ? formatBytes(f.res_size) : <span className="text-base-content/40">—</span>}
                            </td>
                          </tr>
                        ))
                      )}
                    </tbody>
                  </table>
                  <div ref={bottomRef} />
                </div>

                {selectedFlow && (
                  <FlowDetail flow={selectedFlow} onClose={() => setSelectedFlow(null)} />
                )}
              </>
            )}
          </div>
        </div>
      </section>
    </>
  )
}
