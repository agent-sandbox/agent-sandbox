import { ChangeEvent, useCallback, useEffect, useRef, useState } from 'react'
import { useSearchParams } from 'react-router-dom'

import { listSandboxCommandSandboxes, listSandboxCommands } from '../lib/api/sandboxCommands'
import type { SandboxCommandAuditItem } from '../lib/api/types'

const limitOptions = [50, 100, 200, 500]

type Filters = {
  tenantId: string
  sandbox: string
  search: string
  from: string
  to: string
  limit: number
}

const defaultFilters: Filters = {
  tenantId: '',
  sandbox: '',
  search: '',
  from: '',
  to: '',
  limit: 100,
}

function parseLimit(value: string | null): number {
  if (!value) {
    return 100
  }

  const parsed = Number.parseInt(value, 10)
  if (Number.isNaN(parsed) || parsed <= 0) {
    return 100
  }
  return Math.min(parsed, 500)
}

function filtersFromSearchParams(searchParams: URLSearchParams): Filters {
  return {
    tenantId: searchParams.get('tenant')?.trim() ?? '',
    sandbox: searchParams.get('sandbox')?.trim() ?? '',
    search: searchParams.get('command')?.trim() ?? '',
    from: searchParams.get('from')?.trim() ?? '',
    to: searchParams.get('to')?.trim() ?? '',
    limit: parseLimit(searchParams.get('limit')),
  }
}

function formatObservedTime(value: string): string {
  const parsed = Date.parse(value)
  if (Number.isNaN(parsed)) {
    return '-'
  }
  return new Date(parsed).toLocaleString()
}

function compactSandboxName(item: SandboxCommandAuditItem): string {
  return item.sandbox_name || item.sandbox_id || '-'
}

export default function SandboxCommandsPage() {
  const [searchParams, setSearchParams] = useSearchParams()

  const [filters, setFilters] = useState<Filters>(() => filtersFromSearchParams(searchParams))
  const [sandboxes, setSandboxes] = useState<string[]>([])
  const [commands, setCommands] = useState<SandboxCommandAuditItem[]>([])
  const [fetchedAt, setFetchedAt] = useState('')

  const [isSandboxesLoading, setIsSandboxesLoading] = useState(false)
  const [isCommandsLoading, setIsCommandsLoading] = useState(false)

  const [sandboxesError, setSandboxesError] = useState('')
  const [commandsError, setCommandsError] = useState('')

  const filtersRef = useRef(filters)
  filtersRef.current = filters

  const updateQuery = useCallback(
    (nextFilters: Filters) => {
      const params = new URLSearchParams()

      if (nextFilters.tenantId) {
        params.set('tenant', nextFilters.tenantId)
      }
      if (nextFilters.sandbox) {
        params.set('sandbox', nextFilters.sandbox)
      }
      if (nextFilters.search) {
        params.set('command', nextFilters.search)
      }
      if (nextFilters.from) {
        params.set('from', nextFilters.from)
      }
      if (nextFilters.to) {
        params.set('to', nextFilters.to)
      }
      if (nextFilters.limit !== defaultFilters.limit) {
        params.set('limit', String(nextFilters.limit))
      }

      setSearchParams(params, { replace: true })
    },
    [setSearchParams],
  )

  const setFilterValue = (key: keyof Filters, value: string | number) => {
    const nextFilters = {
      ...filtersRef.current,
      [key]: value,
    }
    setFilters(nextFilters)
    updateQuery(nextFilters)
  }

  const loadCommands = useCallback(async (activeFilters: Filters) => {
    setIsCommandsLoading(true)
    setCommandsError('')

    try {
      const data = await listSandboxCommands({
        tenantId: activeFilters.tenantId || undefined,
        sandbox: activeFilters.sandbox || undefined,
        search: activeFilters.search || undefined,
        from: activeFilters.from || undefined,
        to: activeFilters.to || undefined,
        limit: activeFilters.limit,
      })

      if (filtersRef.current !== activeFilters) {
        return
      }

      setCommands(Array.isArray(data.items) ? data.items : [])
      setFetchedAt(data.fetchedAt || '')
    } catch (error) {
      if (filtersRef.current !== activeFilters) {
        return
      }
      const message = error instanceof Error ? error.message : 'Failed to load sandbox commands'
      setCommandsError(message)
      setCommands([])
    } finally {
      setIsCommandsLoading(false)
    }
  }, [])

  const refreshSandboxes = useCallback(async () => {
    setIsSandboxesLoading(true)
    setSandboxesError('')

    try {
      const activeFilters = filtersRef.current
      const data = await listSandboxCommandSandboxes({
        tenantId: activeFilters.tenantId || undefined,
      })
      setSandboxes(Array.isArray(data.items) ? data.items : [])
    } catch (error) {
      const message = error instanceof Error ? error.message : 'Failed to load sandboxes'
      setSandboxesError(message)
      setSandboxes([])
    } finally {
      setIsSandboxesLoading(false)
    }
  }, [])

  useEffect(() => {
    const nextFilters = filtersFromSearchParams(searchParams)
    setFilters(nextFilters)
  }, [searchParams])

  useEffect(() => {
    void loadCommands(filters)
  }, [filters, loadCommands])

  useEffect(() => {
    void refreshSandboxes()
  }, [refreshSandboxes])

  const handleTenantChange = (event: ChangeEvent<HTMLInputElement>) => {
    setFilterValue('tenantId', event.target.value)
  }

  const handleSandboxChange = (event: ChangeEvent<HTMLSelectElement>) => {
    setFilterValue('sandbox', event.target.value)
  }

  const handleSearchChange = (event: ChangeEvent<HTMLInputElement>) => {
    setFilterValue('search', event.target.value)
  }

  const handleFromChange = (event: ChangeEvent<HTMLInputElement>) => {
    setFilterValue('from', event.target.value)
  }

  const handleToChange = (event: ChangeEvent<HTMLInputElement>) => {
    setFilterValue('to', event.target.value)
  }

  const handleLimitChange = (event: ChangeEvent<HTMLSelectElement>) => {
    setFilterValue('limit', parseLimit(event.target.value))
  }

  const handleRefresh = () => {
    void loadCommands(filtersRef.current)
  }

  const fetchedAtLabel = fetchedAt ? new Date(fetchedAt).toLocaleString() : '-'

  return (
    <>
      <header className="card border border-base-300 bg-base-100 shadow-sm">
        <div className="card-body gap-3">
          <div className="flex flex-wrap items-start justify-between gap-3">
            <div>
              <h2 className="text-2xl font-semibold">Sandbox Commands</h2>
              <p className="text-sm text-base-content/70">Audit E2B command starts observed by the sandbox proxy.</p>
            </div>
            <div className="text-right text-xs text-base-content/60">
              <div>Visible scope</div>
              <div>Fetched {fetchedAtLabel}</div>
            </div>
          </div>

          <div className="grid gap-2 xl:grid-cols-[180px_minmax(220px,1fr)_minmax(220px,1fr)_170px_170px_110px_auto_auto]">
            <label className="form-control">
              <span className="label py-1">
                <span className="label-text text-xs">Tenant</span>
              </span>
              <input className="input input-sm input-bordered" value={filters.tenantId} placeholder="All tenants" onChange={handleTenantChange} />
            </label>

            <label className="form-control">
              <span className="label py-1">
                <span className="label-text text-xs">Sandbox</span>
              </span>
              <select className="select select-sm select-bordered" value={filters.sandbox} onChange={handleSandboxChange} disabled={isSandboxesLoading}>
                <option value="">All sandboxes</option>
                {sandboxes.map((sandbox) => (
                  <option key={sandbox} value={sandbox}>
                    {sandbox}
                  </option>
                ))}
              </select>
            </label>

            <label className="form-control">
              <span className="label py-1">
                <span className="label-text text-xs">Command</span>
              </span>
              <input className="input input-sm input-bordered" value={filters.search} placeholder="Search command text" onChange={handleSearchChange} />
            </label>

            <label className="form-control">
              <span className="label py-1">
                <span className="label-text text-xs">From</span>
              </span>
              <input className="input input-sm input-bordered" type="datetime-local" value={filters.from} onChange={handleFromChange} />
            </label>

            <label className="form-control">
              <span className="label py-1">
                <span className="label-text text-xs">To</span>
              </span>
              <input className="input input-sm input-bordered" type="datetime-local" value={filters.to} onChange={handleToChange} />
            </label>

            <label className="form-control">
              <span className="label py-1">
                <span className="label-text text-xs">Limit</span>
              </span>
              <select className="select select-sm select-bordered" value={String(filters.limit)} onChange={handleLimitChange}>
                {limitOptions.map((value) => (
                  <option key={value} value={value}>
                    {value}
                  </option>
                ))}
              </select>
            </label>

            <div className="flex items-end">
              <button className={`btn btn-sm btn-outline w-full ${isCommandsLoading ? 'btn-disabled' : ''}`} type="button" onClick={handleRefresh} disabled={isCommandsLoading}>
                {isCommandsLoading ? 'Refreshing...' : 'Refresh'}
              </button>
            </div>

            <div className="flex items-end">
              <button className="btn btn-sm btn-outline w-full" type="button" onClick={() => void refreshSandboxes()} disabled={isSandboxesLoading}>
                {isSandboxesLoading ? 'Loading...' : 'Reload Sandboxes'}
              </button>
            </div>
          </div>
        </div>
      </header>

      {sandboxesError && (
        <section>
          <div className="alert alert-error">
            <span>{sandboxesError}</span>
          </div>
        </section>
      )}

      {commandsError && (
        <section>
          <div className="alert alert-error">
            <span>{commandsError}</span>
          </div>
        </section>
      )}

      <section>
        <div className="card border border-base-300 bg-base-100 shadow-sm">
          <div className="card-body gap-4">
            <div className="flex items-center justify-between gap-3">
              <h3 className="card-title text-lg">Command</h3>
              <span className="badge badge-outline">{commands.length} rows</span>
            </div>

            <div className="h-[calc(100vh-20rem)] overflow-x-auto rounded-box border border-base-300">
              <table className="table table-pin-rows table-zebra">
                <thead>
                  <tr>
                    <th>Observed</th>
                    <th>Tenant</th>
                    <th>Sandbox</th>
                    <th>Source</th>
                    <th>Command</th>
                  </tr>
                </thead>
                <tbody>
                  {isCommandsLoading ? (
                    <tr>
                      <td colSpan={5} className="text-center text-base-content/70">
                        Loading commands...
                      </td>
                    </tr>
                  ) : commands.length === 0 ? (
                    <tr>
                      <td colSpan={5} className="text-center text-base-content/70">
                        No commands found.
                      </td>
                    </tr>
                  ) : (
                    commands.map((item) => (
                      <tr key={item.id}>
                        <td className="whitespace-nowrap">{formatObservedTime(item.observed_at)}</td>
                        <td className="max-w-[170px] truncate">{item.tenant_id || '-'}</td>
                        <td className="max-w-[260px]">
                          <div className="truncate">{compactSandboxName(item)}</div>
                          {item.sandbox_id && <div className="truncate text-xs text-base-content/50">{item.sandbox_id}</div>}
                        </td>
                        <td>
                          <span className="badge badge-sm badge-outline">{item.source || '-'}</span>
                        </td>
                        <td className="min-w-[480px] max-w-[860px]">
                          <pre className="whitespace-pre-wrap break-words rounded bg-base-200 px-3 py-2 font-mono text-xs leading-relaxed">{item.command_text || '-'}</pre>
                          {item.cwd && <div className="mt-1 text-xs text-base-content/50">cwd: {item.cwd}</div>}
                        </td>
                      </tr>
                    ))
                  )}
                </tbody>
              </table>
            </div>
          </div>
        </div>
      </section>
    </>
  )
}
