import { CSSProperties, ChangeEvent, FormEvent, ReactNode, useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { useNavigate } from 'react-router-dom'

import { createSandbox, deleteSandbox, getSandboxMetrics, listSandboxes } from '../lib/api/sandbox'
import type { CreateSandboxRequest, Sandbox, SandboxMetricsItem } from '../lib/api/types'

type CreateFormState = {
  name: string
  template: string
  image: string
  timeout: string
}

type MetricsStatus = 'idle' | 'loading' | 'ready' | 'error'

type SandboxMetricsState = {
  status: MetricsStatus
  item?: SandboxMetricsItem
}

type SandboxSortKey = 'created-desc' | 'created-asc' | 'name-asc' | 'name-desc'

const initialCreateFormState: CreateFormState = {
  name: '',
  template: '',
  image: '',
  timeout: '',
}

const metricsBatchSize = 10
const refreshIntervalOptions = [2000, 5000, 10000, 30000]

function formatCreatedAt(value?: string): string {
  if (!value) {
    return '-'
  }

  const date = new Date(value)
  if (Number.isNaN(date.getTime())) {
    return value
  }

  return date.toLocaleString()
}

function getCreatedTimestamp(value?: string): number {
  if (!value) {
    return 0
  }

  const parsed = Date.parse(value)
  if (Number.isNaN(parsed)) {
    return 0
  }

  return parsed
}

function buildCreatePayload(form: CreateFormState): CreateSandboxRequest {
  const payload: CreateSandboxRequest = {}

  const name = form.name.trim()
  const template = form.template.trim()
  const image = form.image.trim()
  const timeout = form.timeout.trim()

  if (name) {
    payload.name = name
  }

  if (template) {
    payload.template = template
  }

  if (image) {
    payload.image = image
  }

  if (timeout) {
    const timeoutNumber = Number.parseInt(timeout, 10)
    if (!Number.isNaN(timeoutNumber)) {
      payload.timeout = timeoutNumber
    }
  }

  return payload
}

function parseCpuMilli(value?: string): number | null {
  if (!value) {
    return null
  }
  const trimmed = value.trim()
  if (!trimmed) {
    return null
  }

  if (trimmed.endsWith('m')) {
    const milli = Number.parseFloat(trimmed.slice(0, -1))
    return Number.isFinite(milli) && milli > 0 ? milli : null
  }

  const cores = Number.parseFloat(trimmed)
  if (!Number.isFinite(cores) || cores <= 0) {
    return null
  }
  return cores * 1000
}

function parseMemoryBytes(value?: string): number | null {
  if (!value) {
    return null
  }

  const trimmed = value.trim()
  if (!trimmed) {
    return null
  }

  const match = trimmed.match(/^([0-9]+(?:\.[0-9]+)?)([a-zA-Z]+)?$/)
  if (!match) {
    return null
  }

  const amount = Number.parseFloat(match[1])
  if (!Number.isFinite(amount) || amount <= 0) {
    return null
  }

  const unit = (match[2] ?? '').toLowerCase()
  const factors: Record<string, number> = {
    '': 1,
    b: 1,
    k: 1000,
    m: 1000 ** 2,
    g: 1000 ** 3,
    t: 1000 ** 4,
    p: 1000 ** 5,
    ki: 1024,
    mi: 1024 ** 2,
    gi: 1024 ** 3,
    ti: 1024 ** 4,
    pi: 1024 ** 5,
  }

  const factor = factors[unit]
  if (!factor) {
    return null
  }

  return amount * factor
}

function clampPercent(value: number): number {
  if (!Number.isFinite(value)) {
    return 0
  }
  if (value < 0) {
    return 0
  }
  return value
}

function buildRadialStyle(percent: number): CSSProperties {
  return {
    '--value': `${Math.round(percent)}`,
    '--size': '2rem',
    '--thickness': '1.5px',
  } as CSSProperties
}

function renderMetricFallback(metrics: SandboxMetricsState | undefined, text: string): string {
  if (!metrics || metrics.status === 'idle' || metrics.status === 'loading') {
    return '...'
  }
  if (metrics.status !== 'ready' || !metrics.item) {
    return '-'
  }
  return text
}

function wrapWithResourcesTooltip(content: ReactNode, metrics: SandboxMetricsState | undefined, sandbox: Sandbox) {
  const item = metrics?.status === 'ready' ? metrics.item : undefined

  return (
    <div className="tooltip tooltip-left">
      <div className="tooltip-content">
        CPU:  {sandbox.cpu || '-'} - {sandbox.cpu_limit || '-'}, Usage:  {item ? `${item.cpuMilli}m` : '-'}<br />
        Memory:  {sandbox.memory || '-'} - {sandbox.memory_limit || '-'}, Usage: {item ? `${Math.round(item.memoryMB)}MiB` : '-'}
      </div>
      <div>{content}</div>
    </div>
  )
}

function renderCpuCell(metrics: SandboxMetricsState | undefined, sandbox: Sandbox) {
  const fallback = renderMetricFallback(metrics, metrics?.item ? `${metrics.item.cpuMilli}m` : '-')
  if (fallback !== `${metrics?.item?.cpuMilli ?? ''}m`) {
    return wrapWithResourcesTooltip(fallback, metrics, sandbox)
  }

  const item = metrics?.item
  if (!item) {
    return wrapWithResourcesTooltip('-', metrics, sandbox)
  }

  const cpuRequestMilli = parseCpuMilli(sandbox.cpu)
  if (!cpuRequestMilli) {
    return wrapWithResourcesTooltip(`${item.cpuMilli}m`, metrics, sandbox)
  }

  const percent = clampPercent((item.cpuMilli / cpuRequestMilli) * 100)
  return wrapWithResourcesTooltip(
    <div>
      <div className="radial-progress text-info" style={buildRadialStyle(percent)} role="progressbar" aria-valuenow={Math.round(percent)}>
        {Math.round(percent)}
      </div>
    </div>,
    metrics,
    sandbox
  )
}

function renderMemoryCell(metrics: SandboxMetricsState | undefined, sandbox: Sandbox) {
  const fallback = renderMetricFallback(metrics, metrics?.item ? `${Math.round(metrics.item.memoryMB)} MiB` : '-')
  if (fallback !== `${Math.round(metrics?.item?.memoryMB ?? 0)} MiB`) {
    return wrapWithResourcesTooltip(fallback, metrics, sandbox)
  }

  const item = metrics?.item
  if (!item) {
    return wrapWithResourcesTooltip('-', metrics, sandbox)
  }

  const memoryRequestBytes = parseMemoryBytes(sandbox.memory)
  if (!memoryRequestBytes) {
    return wrapWithResourcesTooltip(`${Math.round(item.memoryMB)} MiB`, metrics, sandbox)
  }

  const percent = clampPercent((item.memoryBytes / memoryRequestBytes) * 100)
  return wrapWithResourcesTooltip(
    <div>
      <div className="radial-progress text-warning" style={buildRadialStyle(percent)} role="progressbar" aria-valuenow={Math.round(percent)}>
        {Math.round(percent)}
      </div>
    </div>,
    metrics,
    sandbox
  )
}

export default function SandboxesPage() {
  const navigate = useNavigate()
  const [sandboxes, setSandboxes] = useState<Sandbox[]>([])
  const [isListLoading, setIsListLoading] = useState(false)
  const [isCreateSubmitting, setIsCreateSubmitting] = useState(false)
  const [deletingName, setDeletingName] = useState<string | null>(null)
  const [confirmDeleteName, setConfirmDeleteName] = useState<string | null>(null)
  const [pageError, setPageError] = useState('')
  const [actionError, setActionError] = useState('')
  const [createErrorMessage, setCreateErrorMessage] = useState('')
  const [successMessage, setSuccessMessage] = useState('')
  const [formState, setFormState] = useState<CreateFormState>(initialCreateFormState)
  const [isCreateModalOpen, setIsCreateModalOpen] = useState(false)
  const [metricsByName, setMetricsByName] = useState<Record<string, SandboxMetricsState>>({})
  const [isAutoRefresh, setIsAutoRefresh] = useState(true)
  const [refreshIntervalMs, setRefreshIntervalMs] = useState(5000)
  const [sortKey, setSortKey] = useState<SandboxSortKey>('created-desc')

  const inflightNamesRef = useRef<Set<string>>(new Set())
  const requestSerialByNameRef = useRef<Record<string, number>>({})
  const fetchDebounceTimerRef = useRef<number | null>(null)
  const listRequestInFlightRef = useRef(false)
  const deletingNameRef = useRef<string | null>(null)

  deletingNameRef.current = deletingName

  const refreshSandboxes = useCallback(async (options?: { keepMessages?: boolean }) => {
    if (listRequestInFlightRef.current) {
      return
    }

    listRequestInFlightRef.current = true
    setIsListLoading(true)
    setPageError('')

    if (!options?.keepMessages) {
      setActionError('')
      setSuccessMessage('')
    }

    try {
      const list = await listSandboxes()
      setSandboxes(list)
    } catch (error) {
      const message = error instanceof Error ? error.message : 'Failed to load sandboxes'
      setPageError(message)
    } finally {
      listRequestInFlightRef.current = false
      setIsListLoading(false)
    }
  }, [])

  useEffect(() => {
    void refreshSandboxes()
  }, [refreshSandboxes])

  useEffect(() => {
    if (!isAutoRefresh) {
      return
    }

    const timer = window.setInterval(() => {
      if (deletingNameRef.current) {
        return
      }
      void refreshSandboxes({ keepMessages: true })
    }, refreshIntervalMs)

    return () => {
      window.clearInterval(timer)
    }
  }, [isAutoRefresh, refreshIntervalMs, refreshSandboxes])

  useEffect(() => {
    const existingNames = new Set(sandboxes.map((sandbox) => sandbox.name?.trim() ?? '').filter((name) => name !== ''))

    setMetricsByName((prev) => {
      const next: Record<string, SandboxMetricsState> = {}
      for (const [name, value] of Object.entries(prev)) {
        if (existingNames.has(name)) {
          next[name] = value
        }
      }
      for (const name of existingNames) {
        if (!next[name]) {
          next[name] = { status: 'idle' }
        }
      }
      return next
    })

    const nextInflight = new Set<string>()
    for (const name of inflightNamesRef.current) {
      if (existingNames.has(name)) {
        nextInflight.add(name)
      }
    }
    inflightNamesRef.current = nextInflight

    const nextRequestSerialByName: Record<string, number> = {}
    for (const [name, serial] of Object.entries(requestSerialByNameRef.current)) {
      if (existingNames.has(name)) {
        nextRequestSerialByName[name] = serial
      }
    }
    requestSerialByNameRef.current = nextRequestSerialByName
  }, [sandboxes])

  useEffect(() => {
    if (fetchDebounceTimerRef.current !== null) {
      window.clearTimeout(fetchDebounceTimerRef.current)
    }

    fetchDebounceTimerRef.current = window.setTimeout(() => {
      const sandboxNames = sandboxes.map((sandbox) => (sandbox.name ?? '').trim()).filter((name) => name !== '')
      const namesToFetch = sandboxNames
        .filter((name) => {
          if (inflightNamesRef.current.has(name)) {
            return false
          }
          const current = metricsByName[name]
          return !current || current.status === 'idle'
        })
        .slice(0, metricsBatchSize)

      if (namesToFetch.length === 0) {
        return
      }

      for (const name of namesToFetch) {
        inflightNamesRef.current.add(name)
      }

      setMetricsByName((prev) => {
        const next = { ...prev }
        for (const name of namesToFetch) {
          next[name] = { status: 'loading' }
        }
        return next
      })

      const requestSerials: Record<string, number> = {}
      for (const name of namesToFetch) {
        const nextSerial = (requestSerialByNameRef.current[name] ?? 0) + 1
        requestSerialByNameRef.current[name] = nextSerial
        requestSerials[name] = nextSerial
      }

      void getSandboxMetrics(namesToFetch)
        .then((result) => {
          const items = result.items ?? {}
          setMetricsByName((prev) => {
            const next = { ...prev }
            for (const name of namesToFetch) {
              if (requestSerialByNameRef.current[name] !== requestSerials[name]) {
                continue
              }

              const item = items[name]
              if (item) {
                next[name] = { status: 'ready', item }
              } else {
                next[name] = { status: 'ready' }
              }
            }
            return next
          })
        })
        .catch(() => {
          setMetricsByName((prev) => {
            const next = { ...prev }
            for (const name of namesToFetch) {
              if (requestSerialByNameRef.current[name] !== requestSerials[name]) {
                continue
              }
              next[name] = { status: 'error' }
            }
            return next
          })
        })
        .finally(() => {
          for (const name of namesToFetch) {
            inflightNamesRef.current.delete(name)
          }
        })
    }, 120)

    return () => {
      if (fetchDebounceTimerRef.current !== null) {
        window.clearTimeout(fetchDebounceTimerRef.current)
      }
    }
  }, [metricsByName, sandboxes])

  const sortedSandboxes = useMemo(() => {
    const next = [...sandboxes]

    next.sort((a, b) => {
      if (sortKey === 'created-desc') {
        return getCreatedTimestamp(b.created_at) - getCreatedTimestamp(a.created_at)
      }

      if (sortKey === 'created-asc') {
        return getCreatedTimestamp(a.created_at) - getCreatedTimestamp(b.created_at)
      }

      const nameA = (a.name ?? '').toLocaleLowerCase()
      const nameB = (b.name ?? '').toLocaleLowerCase()
      if (sortKey === 'name-asc') {
        return nameA.localeCompare(nameB)
      }

      return nameB.localeCompare(nameA)
    })

    return next
  }, [sandboxes, sortKey])

  const isDeleteDisabled = (sandboxName: string) => !sandboxName || Boolean(deletingName)

  const handleDeleteClick = (sandboxName?: string) => {
    if (!sandboxName) {
      return
    }
    setConfirmDeleteName(sandboxName)
  }

  const handleRefresh = async () => {
    await refreshSandboxes({ keepMessages: true })
  }

  const handleCreateSandbox = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault()

    const timeoutText = formState.timeout.trim()
    if (timeoutText) {
      const parsed = Number.parseInt(timeoutText, 10)
      if (Number.isNaN(parsed)) {
        setCreateErrorMessage('Timeout must be an integer in seconds.')
        setSuccessMessage('')
        return
      }
    }

    setIsCreateSubmitting(true)
    setCreateErrorMessage('')
    setActionError('')
    setSuccessMessage('')

    try {
      await createSandbox(buildCreatePayload(formState))
      setFormState(initialCreateFormState)
      setIsCreateModalOpen(false)
      setSuccessMessage('Sandbox created successfully.')
      await refreshSandboxes({ keepMessages: true })
    } catch (error) {
      const message = error instanceof Error ? error.message : 'Failed to create sandbox'
      setCreateErrorMessage(message)
    } finally {
      setIsCreateSubmitting(false)
    }
  }

  const handleDeleteSandbox = async (name?: string) => {
    if (!name) {
      setActionError('Sandbox name is missing and cannot be deleted.')
      setSuccessMessage('')
      setConfirmDeleteName(null)
      return
    }

    setDeletingName(name)
    setActionError('')
    setSuccessMessage('')

    try {
      await deleteSandbox(name)
      setSuccessMessage(`Sandbox "${name}" deleted successfully.`)
      await refreshSandboxes({ keepMessages: true })
    } catch (error) {
      const message = error instanceof Error ? error.message : `Failed to delete sandbox "${name}"`
      setActionError(message)
    } finally {
      setDeletingName(null)
      setConfirmDeleteName(null)
    }
  }

  const handleRefreshIntervalChange = (event: ChangeEvent<HTMLSelectElement>) => {
    const parsed = Number.parseInt(event.target.value, 10)
    if (!Number.isNaN(parsed) && parsed > 0) {
      setRefreshIntervalMs(parsed)
    }
  }

  const handleSortChange = (event: ChangeEvent<HTMLSelectElement>) => {
    setSortKey(event.target.value as SandboxSortKey)
  }

  return (
    <>
      <header className="card border border-base-300 bg-base-100 shadow-sm">
        <div className="card-body gap-3">
          <div className="flex flex-wrap items-center justify-between gap-3">
            <div>
              <h2 className="text-2xl font-semibold">Sandbox Management </h2>
              <p className="text-sm text-base-content/70">Manage sandbox instances with list, creation, and deletion.</p>
            </div>
          </div>
        </div>
      </header>

      {(actionError || successMessage) && (
        <section className="space-y-2">
          {actionError && (
            <div className="alert alert-error">
              <span>{actionError}</span>
            </div>
          )}
          {successMessage && (
            <div className="alert alert-success">
              <span>{successMessage}</span>
            </div>
          )}
        </section>
      )}

      <section>
        <div className="card border border-base-300 bg-base-100 shadow-sm">
          <div className="card-body gap-4">
            <div className="flex items-center justify-between gap-2">
              <h3 className="card-title text-lg">Sandboxes List</h3>
              <div className="flex flex-wrap items-center gap-2">
                <label className="label cursor-pointer gap-2 py-0">
                  <span className="label-text text-sm">Auto Refresh</span>
                  <input
                    className="toggle toggle-sm"
                    type="checkbox"
                    checked={isAutoRefresh}
                    onChange={() => {
                      setIsAutoRefresh((prev) => !prev)
                    }}
                    disabled={Boolean(deletingName)}
                  />
                </label>

                <label className="flex items-center gap-2">
                  <span className="text-sm">Interval</span>
                  <select className="select select-sm select-bordered" value={String(refreshIntervalMs)} onChange={handleRefreshIntervalChange} disabled={!isAutoRefresh || Boolean(deletingName)}>
                    {refreshIntervalOptions.map((interval) => (
                      <option key={interval} value={interval}>
                        {interval / 1000}s
                      </option>
                    ))}
                  </select>
                </label>

                <label className="flex items-center gap-2">
                  <span className="text-sm">Sort</span>
                  <select className="select select-sm select-bordered" value={sortKey} onChange={handleSortChange}>
                    <option value="created-desc">Created (newest first)</option>
                    <option value="created-asc">Created (oldest first)</option>
                    <option value="name-asc">Name (A-Z)</option>
                    <option value="name-desc">Name (Z-A)</option>
                  </select>
                </label>

                <button
                  className={`btn btn-sm btn-outline ${isListLoading ? 'btn-disabled' : ''}`}
                  type="button"
                  onClick={() => {
                    void handleRefresh()
                  }}
                  disabled={isListLoading}
                >
                  {isListLoading ? 'Refreshing...' : 'Refresh'}
                </button>
                <button
                  className="btn btn-sm btn-primary"
                  type="button"
                  onClick={() => {
                    setActionError('')
                    setCreateErrorMessage('')
                    setSuccessMessage('')
                    setIsCreateModalOpen(true)
                  }}
                >
                  Create Sandbox
                </button>
              </div>
            </div>

            {pageError && (
              <div className="alert alert-error">
                <span>{pageError}</span>
              </div>
            )}

            <div className="h-[calc(100vh-18rem)] overflow-auto rounded-box border border-base-300">
              <table className="table table-pin-rows table-zebra">
                <thead>
                  <tr>
                    <th>#</th>
                    <th>Name</th>
                    <th>Template</th>
                    <th>Image</th>
                    <th>Status</th>
                    <th>CPU %</th>
                    <th>Memory %</th>
                    <th>Timeout</th>
                    <th>Created</th>
                    <th className="text-center">Actions</th>
                  </tr>
                </thead>
                <tbody>
                  {sortedSandboxes.length === 0 ? (
                    <tr>
                      <td className="text-center text-base-content/70" colSpan={10}>
                        {isListLoading ? 'Loading sandboxes...' : 'No sandboxes found.'}
                      </td>
                    </tr>
                  ) : (
                    sortedSandboxes.map((sandbox, index) => {
                      const sandboxName = sandbox.name ?? ''
                      const isDeleting = deletingName === sandboxName
                      const metrics = sandboxName ? metricsByName[sandboxName] : undefined

                      return (
                        <tr key={sandboxName || sandbox.id || `sandbox-${index}`}>
                          <td>{index + 1}</td>
                          <td className="font-medium">
                            <a className="link link-hover link-success" href={`/sandbox/${sandbox.name}/health`} target="_blank" rel="noreferrer">
                              {sandbox.name || '-'}
                            </a>
                            <div className="text-xs text-base-content/40 font-normal">
                              <a className="link link-hover" href={`/sandboxes/router/${sandbox.id}/${sandbox.port}/health`} target="_blank" rel="noreferrer">
                                {sandbox.id || '-'}
                              </a>
                            </div>
                          </td>
                          <td>{sandbox.template || '-'}</td>
                          <td>{sandbox.image || '-'}</td>
                          <td>
                            {sandbox.status ? (
                              <span className={`badge badge-sm ${sandbox.status === 'running' ? 'badge-success' : 'badge-warning'}`}>{sandbox.status}</span>
                            ) : (
                              '-'
                            )}
                          </td>
                          <td style={{ fontSize: '10px' }}>{renderCpuCell(metrics, sandbox)}</td>
                          <td style={{ fontSize: '10px' }}>{renderMemoryCell(metrics, sandbox)}</td>
                          <td>{typeof sandbox.timeout === 'number' ? `${sandbox.timeout}s` : '-'}</td>
                          <td>{formatCreatedAt(sandbox.created_at)}</td>
                          <td className="text-right">
                            <div className="text-center justify-center">
                              <div className="mb-2 ">
                                <button
                                  className="btn btn-xs btn-outline mr-2"
                                  type="button"
                                  disabled={!sandboxName || Boolean(deletingName)}
                                  onClick={() => {
                                    navigate(`/logs?sandbox=${encodeURIComponent(sandboxName)}`)
                                  }}
                                >
                                  Logs
                                </button>
                                <button
                                  className="btn btn-xs btn-outline"
                                  type="button"
                                  disabled={!sandboxName || Boolean(deletingName)}
                                  onClick={() => {
                                    navigate(`/files?sandbox=${encodeURIComponent(sandboxName)}`)
                                  }}
                                >
                                  Files
                                </button>
                              </div>
                              <div>
                                <button
                                  className="btn btn-xs btn-warning btn-outline mr-1"
                                  type="button"
                                  disabled={!sandboxName || Boolean(deletingName)}
                                  onClick={() => {
                                    navigate(`/terminal?sandbox=${encodeURIComponent(sandboxName)}`)
                                  }}
                                >
                                  Terminal
                                </button>
                                <button
                                  className={`btn btn-xs btn-outline btn-error ${isDeleting ? 'btn-disabled' : ''}`}
                                  type="button"
                                  disabled={isDeleteDisabled(sandboxName)}
                                  onClick={() => {
                                    handleDeleteClick(sandbox.name)
                                  }}
                                >
                                  {isDeleting ? 'Deleting...' : 'Delete'}
                                </button>
                              </div>
                            </div>
                          </td>
                        </tr>
                      )
                    })
                  )}
                </tbody>
              </table>
            </div>
          </div>
        </div>
      </section>

      <dialog className={`modal ${isCreateModalOpen ? 'modal-open' : ''}`} open={isCreateModalOpen}>
        <div className="modal-box">
          <h3 className="text-lg font-bold">Create Sandbox</h3>
          <form className="mt-4 space-y-3" onSubmit={(event) => void handleCreateSandbox(event)}>
            <label className="form-control w-full">
              <div className="label">
                <span className="label-text">Name</span>
              </div>
              <input
                className="input input-sm input-bordered w-full"
                type="text"
                value={formState.name}
                onChange={(event) => {
                  setFormState((prev) => ({ ...prev, name: event.target.value }))
                }}
                placeholder="Optional"
              />
            </label>

            <label className="form-control w-full">
              <div className="label">
                <span className="label-text">Template</span>
              </div>
              <input
                className="input input-sm input-bordered w-full"
                type="text"
                value={formState.template}
                onChange={(event) => {
                  setFormState((prev) => ({ ...prev, template: event.target.value }))
                }}
                placeholder="Optional"
              />
            </label>

            <label className="form-control w-full">
              <div className="label">
                <span className="label-text">Image</span>
              </div>
              <input
                className="input input-sm input-bordered w-full"
                type="text"
                value={formState.image}
                onChange={(event) => {
                  setFormState((prev) => ({ ...prev, image: event.target.value }))
                }}
                placeholder="Optional"
              />
            </label>

            <label className="form-control w-full">
              <div className="label">
                <span className="label-text">Timeout (seconds)</span>
              </div>
              <input
                className="input input-sm input-bordered w-full"
                type="text"
                value={formState.timeout}
                onChange={(event) => {
                  setFormState((prev) => ({ ...prev, timeout: event.target.value }))
                }}
                placeholder="Optional"
              />
            </label>

            <div className="modal-action mt-4">
              <button
                className="btn btn-ghost"
                type="button"
                onClick={() => {
                  if (isCreateSubmitting) {
                    return
                  }
                  setCreateErrorMessage('')
                  setIsCreateModalOpen(false)
                }}
                disabled={isCreateSubmitting}
              >
                Cancel
              </button>
              <button className={`btn btn-primary ${isCreateSubmitting ? 'btn-disabled' : ''}`} type="submit" disabled={isCreateSubmitting}>
                {isCreateSubmitting ? 'Creating...' : 'Create Sandbox'}
              </button>
            </div>
          </form>
        </div>
        <button
          className="modal-backdrop"
          type="button"
          aria-label="Close"
          onClick={() => {
            if (isCreateSubmitting) {
              return
            }
            setCreateErrorMessage('')
            setIsCreateModalOpen(false)
          }}
        />
      </dialog>

      <dialog className={`modal ${createErrorMessage ? 'modal-open' : ''}`} open={Boolean(createErrorMessage)}>
        <div className="modal-box relative">
          <button
            className="btn btn-sm btn-circle btn-ghost absolute right-2 top-2"
            type="button"
            aria-label="Close"
            onClick={() => {
              setCreateErrorMessage('')
            }}
          >
            ✕
          </button>
          <h3 className="text-lg font-bold">Create Sandbox Failed</h3>
          <p className="py-4">{createErrorMessage}</p>
        </div>
        <button
          className="modal-backdrop"
          type="button"
          aria-label="Close"
          onClick={() => {
            setCreateErrorMessage('')
          }}
        />
      </dialog>

      <dialog className={`modal ${confirmDeleteName ? 'modal-open' : ''}`} open={Boolean(confirmDeleteName)}>
        <div className="modal-box">
          <h3 className="text-lg font-bold">Delete Sandbox</h3>
          <p className="py-4">
            Are you sure you want to delete sandbox <span className="font-semibold">"{confirmDeleteName}"</span>?
          </p>
          <div className="modal-action">
            <button
              className="btn btn-ghost"
              type="button"
              onClick={() => {
                if (deletingName) {
                  return
                }
                setConfirmDeleteName(null)
              }}
              disabled={Boolean(deletingName)}
            >
              Cancel
            </button>
            <button
              className={`btn btn-error ${deletingName ? 'btn-disabled' : ''}`}
              type="button"
              disabled={Boolean(deletingName)}
              onClick={() => {
                void handleDeleteSandbox(confirmDeleteName ?? undefined)
              }}
            >
              {deletingName ? 'Deleting...' : 'Delete'}
            </button>
          </div>
        </div>
        <button
          className="modal-backdrop"
          type="button"
          aria-label="Close"
          onClick={() => {
            if (deletingName) {
              return
            }
            setConfirmDeleteName(null)
          }}
        />
      </dialog>
    </>
  )
}
