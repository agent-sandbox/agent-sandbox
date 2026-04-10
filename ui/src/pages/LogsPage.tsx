import { ChangeEvent, useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { useSearchParams } from 'react-router-dom'

import { getSandboxLogs } from '../lib/api/logs'
import { listSandboxes } from '../lib/api/sandbox'
import type { Sandbox } from '../lib/api/types'

const refreshIntervalOptions = [2000, 5000, 10000, 30000]

export default function LogsPage() {
  const [searchParams, setSearchParams] = useSearchParams()
  const [sandboxes, setSandboxes] = useState<Sandbox[]>([])
  const [selectedSandboxName, setSelectedSandboxName] = useState('')
  const [logsText, setLogsText] = useState('')
  const [isSandboxesLoading, setIsSandboxesLoading] = useState(false)
  const [isLogsLoading, setIsLogsLoading] = useState(false)
  const [sandboxesError, setSandboxesError] = useState('')
  const [logsError, setLogsError] = useState('')
  const [isAutoRefresh, setIsAutoRefresh] = useState(true)
  const [refreshIntervalMs, setRefreshIntervalMs] = useState(5000)

  const logsRequestInFlightRef = useRef(false)
  const selectedSandboxNameRef = useRef('')

  selectedSandboxNameRef.current = selectedSandboxName

  const loadLogs = useCallback(async (sandboxName: string, options?: { silent?: boolean }) => {
    const name = sandboxName.trim()
    if (!name || logsRequestInFlightRef.current) {
      return
    }

    logsRequestInFlightRef.current = true
    if (!options?.silent) {
      setIsLogsLoading(true)
    }
    setLogsError('')

    try {
      const data = await getSandboxLogs(name)
      if (selectedSandboxNameRef.current !== name) {
        return
      }
      setLogsText(data.logs ?? '')
    } catch (error) {
      if (selectedSandboxNameRef.current !== name) {
        return
      }
      const message = error instanceof Error ? error.message : 'Failed to load logs'
      setLogsError(message)
      setLogsText('')
    } finally {
      logsRequestInFlightRef.current = false
      if (!options?.silent) {
        setIsLogsLoading(false)
      }
    }
  }, [])

  const refreshSandboxes = useCallback(async () => {
    setIsSandboxesLoading(true)
    setSandboxesError('')

    try {
      const list = await listSandboxes()
      setSandboxes(list)

      if (list.length === 0) {
        setSelectedSandboxName('')
        setLogsText('')
        setLogsError('')
        return
      }

      const fromQuery = searchParams.get('sandbox')?.trim() ?? ''
      const hasQueryTarget = Boolean(fromQuery) && list.some((sandbox) => sandbox.name === fromQuery)
      const currentSelectionValid = Boolean(selectedSandboxNameRef.current) && list.some((sandbox) => sandbox.name === selectedSandboxNameRef.current)

      const nextSelected = hasQueryTarget
        ? fromQuery
        : currentSelectionValid
          ? selectedSandboxNameRef.current
          : list[0]?.name ?? ''

      setSelectedSandboxName(nextSelected)

      if (nextSelected) {
        const nextParams = new URLSearchParams(searchParams)
        nextParams.set('sandbox', nextSelected)
        setSearchParams(nextParams, { replace: true })
      }
    } catch (error) {
      const message = error instanceof Error ? error.message : 'Failed to load sandboxes'
      setSandboxesError(message)
      setSelectedSandboxName('')
      setLogsText('')
      setLogsError('')
    } finally {
      setIsSandboxesLoading(false)
    }
  }, [searchParams, setSearchParams])

  useEffect(() => {
    void refreshSandboxes()
  }, [refreshSandboxes])

  useEffect(() => {
    if (!selectedSandboxName) {
      return
    }

    void loadLogs(selectedSandboxName)
  }, [selectedSandboxName, loadLogs])

  useEffect(() => {
    if (!isAutoRefresh || !selectedSandboxName) {
      return
    }

    const timer = window.setInterval(() => {
      void loadLogs(selectedSandboxNameRef.current, { silent: true })
    }, refreshIntervalMs)

    return () => {
      window.clearInterval(timer)
    }
  }, [isAutoRefresh, refreshIntervalMs, selectedSandboxName, loadLogs])

  useEffect(() => {
    if (!selectedSandboxName) {
      return
    }

    const stillExists = sandboxes.some((sandbox) => sandbox.name === selectedSandboxName)
    if (!stillExists) {
      setLogsError(`Sandbox "${selectedSandboxName}" no longer exists.`)
      setLogsText('')
    }
  }, [sandboxes, selectedSandboxName])

  const canRefreshLogs = Boolean(selectedSandboxName) && !isLogsLoading
  const canChangeInterval = isAutoRefresh
  const logsLines = useMemo(() => logsText.split('\n'), [logsText])
  const showLogs = logsText.length > 0 || isLogsLoading

  const handleSandboxChange = (event: ChangeEvent<HTMLSelectElement>) => {
    const next = event.target.value
    setSelectedSandboxName(next)

    const nextParams = new URLSearchParams(searchParams)
    if (next) {
      nextParams.set('sandbox', next)
    } else {
      nextParams.delete('sandbox')
    }
    setSearchParams(nextParams, { replace: true })
  }

  const handleRefresh = () => {
    if (!selectedSandboxName) {
      return
    }
    void loadLogs(selectedSandboxName)
  }

  const handleRefreshIntervalChange = (event: ChangeEvent<HTMLSelectElement>) => {
    const parsed = Number.parseInt(event.target.value, 10)
    if (!Number.isNaN(parsed) && parsed > 0) {
      setRefreshIntervalMs(parsed)
    }
  }

  const logsPlaceholder = isLogsLoading ? 'Loading logs...' : 'No logs yet.'

  return (
    <>
      <header className="card border border-base-300 bg-base-100 shadow-sm">
        <div className="card-body gap-3">
          <div>
            <h2 className="text-2xl font-semibold">Sandbox Logs</h2>
            <p className="text-sm text-base-content/70">View logs in a dedicated page and switch sandbox anytime.</p>
          </div>

          <div className="flex flex-wrap items-center gap-2">
            <label className="flex items-center gap-2">
              <span className="text-sm">Sandbox</span>
              <select
                className="select select-sm select-bordered w-full max-w-xs"
                value={selectedSandboxName}
                onChange={handleSandboxChange}
                disabled={isSandboxesLoading || sandboxes.length === 0}
              >
                {sandboxes.length === 0 ? (
                  <option value="">No sandboxes</option>
                ) : (
                  sandboxes.map((sandbox, index) => {
                    const name = sandbox.name ?? ''
                    return (
                      <option key={name || sandbox.id || `logs-sandbox-${index}`} value={name}>
                        {name || '-'}
                      </option>
                    )
                  })
                )}
              </select>
            </label>

            <button className={`btn btn-sm btn-outline ${isLogsLoading ? 'btn-disabled' : ''}`} type="button" onClick={handleRefresh} disabled={!canRefreshLogs}>
              {isLogsLoading ? 'Refreshing...' : 'Refresh'}
            </button>

            <button className="btn btn-sm btn-outline" type="button" onClick={() => void refreshSandboxes()} disabled={isSandboxesLoading}>
              {isSandboxesLoading ? 'Loading...' : 'Reload Sandboxes'}
            </button>

            <label className="label cursor-pointer gap-2 py-0">
              <span className="label-text text-sm">Auto Refresh</span>
              <input
                className="toggle toggle-sm"
                type="checkbox"
                checked={isAutoRefresh}
                onChange={() => {
                  setIsAutoRefresh((prev) => !prev)
                }}
                disabled={!selectedSandboxName}
              />
            </label>

            <label className="flex items-center gap-2">
              <span className="text-sm">Interval</span>
              <select className="select select-sm select-bordered" value={String(refreshIntervalMs)} onChange={handleRefreshIntervalChange} disabled={!canChangeInterval}>
                {refreshIntervalOptions.map((interval) => (
                  <option key={interval} value={interval}>
                    {interval / 1000}s
                  </option>
                ))}
              </select>
            </label>
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

      {logsError && (
        <section>
          <div className="alert alert-error">
            <span>Failed to load logs. {logsError}</span>
          </div>
        </section>
      )}

      <section>
        <div className="card border border-base-300 bg-base-100 shadow-sm">
          <div className="card-body gap-3">
            <h3 className="card-title text-lg">Logs Output - <span className="text-sm text-base-content/70">{selectedSandboxName}</span></h3>

            {!selectedSandboxName && sandboxes.length === 0 ? (
              <p className="text-sm text-base-content/60">No sandboxes found.</p>
            ) : showLogs ? (
              <div className="mockup-code h-[calc(100vh-300px)] overflow-auto">
                {logsLines.map((line, index) => (
                  <pre key={`line-${index}`} data-prefix={String(index + 1)}>
                    <code>{line || ' '}</code>
                  </pre>
                ))}
                {logsText.endsWith('\n') && (
                  <pre data-prefix={String(logsLines.length + 1)}>
                    <code> </code>
                  </pre>
                )}
              </div>
            ) : (
              <p className="text-sm text-base-content/60">{logsPlaceholder}</p>
            )}
          </div>
        </div>
      </section>
    </>
  )
}
