import { ChangeEvent, useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { useSearchParams } from 'react-router-dom'

import { listSandboxEvents } from '../lib/api/events'
import { listSandboxes } from '../lib/api/sandbox'
import type { Sandbox, SandboxEventItem } from '../lib/api/types'

const limitOptions = [50, 100, 200, 500]

function formatEventTime(item: SandboxEventItem): string {
  const candidates = [item.eventTime, item.lastTimestamp, item.firstTimestamp]
  for (const candidate of candidates) {
    const parsed = Date.parse(candidate)
    if (Number.isNaN(parsed)) {
      continue
    }

    const date = new Date(parsed)
    if (date.getUTCFullYear() <= 1) {
      continue
    }

    return date.toLocaleString()
  }
  return '-'
}

export default function EventsPage() {
  const [searchParams, setSearchParams] = useSearchParams()

  const [sandboxes, setSandboxes] = useState<Sandbox[]>([])
  const [events, setEvents] = useState<SandboxEventItem[]>([])
  const [selectedSandboxName, setSelectedSandboxName] = useState('')
  const [limit, setLimit] = useState(100)

  const [isSandboxesLoading, setIsSandboxesLoading] = useState(false)
  const [isEventsLoading, setIsEventsLoading] = useState(false)

  const [sandboxesError, setSandboxesError] = useState('')
  const [eventsError, setEventsError] = useState('')

  const selectedSandboxNameRef = useRef('')
  const limitRef = useRef(100)

  selectedSandboxNameRef.current = selectedSandboxName
  limitRef.current = limit

  const updateQuery = useCallback(
    (nextSandbox: string, nextLimit: number) => {
      const params = new URLSearchParams(searchParams)

      if (nextSandbox) {
        params.set('sandbox', nextSandbox)
      } else {
        params.delete('sandbox')
      }

      if (nextLimit === 100) {
        params.delete('limit')
      } else {
        params.set('limit', String(nextLimit))
      }

      setSearchParams(params, { replace: true })
    },
    [searchParams, setSearchParams],
  )

  const loadEvents = useCallback(async (sandboxName: string, limitValue: number) => {
    setIsEventsLoading(true)
    setEventsError('')

    try {
      const data = await listSandboxEvents({ sandbox: sandboxName || undefined, limit: limitValue })

      if (selectedSandboxNameRef.current !== sandboxName || limitRef.current !== limitValue) {
        return
      }

      setEvents(Array.isArray(data.items) ? data.items : [])
    } catch (error) {
      if (selectedSandboxNameRef.current !== sandboxName || limitRef.current !== limitValue) {
        return
      }
      const message = error instanceof Error ? error.message : 'Failed to load events'
      setEventsError(message)
      setEvents([])
    } finally {
      setIsEventsLoading(false)
    }
  }, [])

  const refreshSandboxes = useCallback(async () => {
    setIsSandboxesLoading(true)
    setSandboxesError('')

    try {
      const list = await listSandboxes()
      setSandboxes(list)

      const fromQuery = searchParams.get('sandbox')?.trim() ?? ''
      const hasQueryTarget = Boolean(fromQuery) && list.some((sandbox) => sandbox.name === fromQuery)
      const currentSelectionValid = Boolean(selectedSandboxNameRef.current) && list.some((sandbox) => sandbox.name === selectedSandboxNameRef.current)

      const nextSandbox = hasQueryTarget ? fromQuery : currentSelectionValid ? selectedSandboxNameRef.current : ''
      setSelectedSandboxName(nextSandbox)
      updateQuery(nextSandbox, limitRef.current)
    } catch (error) {
      const message = error instanceof Error ? error.message : 'Failed to load sandboxes'
      setSandboxesError(message)
      setSandboxes([])
      setSelectedSandboxName('')
    } finally {
      setIsSandboxesLoading(false)
    }
  }, [searchParams, updateQuery])

  useEffect(() => {
    const rawLimit = searchParams.get('limit')?.trim() ?? ''
    if (!rawLimit) {
      setLimit(100)
      return
    }

    const parsed = Number.parseInt(rawLimit, 10)
    if (Number.isNaN(parsed) || parsed <= 0) {
      setLimit(100)
      return
    }

    const capped = Math.min(parsed, 500)
    setLimit(capped)
  }, [searchParams])

  useEffect(() => {
    void refreshSandboxes()
  }, [refreshSandboxes])

  useEffect(() => {
    void loadEvents(selectedSandboxName, limit)
  }, [selectedSandboxName, limit, loadEvents])

  const handleSandboxChange = (event: ChangeEvent<HTMLSelectElement>) => {
    const nextSandbox = event.target.value
    setSelectedSandboxName(nextSandbox)
    updateQuery(nextSandbox, limit)
  }

  const handleLimitChange = (event: ChangeEvent<HTMLSelectElement>) => {
    const parsed = Number.parseInt(event.target.value, 10)
    if (Number.isNaN(parsed) || parsed <= 0) {
      return
    }

    const nextLimit = Math.min(parsed, 500)
    setLimit(nextLimit)
    updateQuery(selectedSandboxName, nextLimit)
  }

  const handleRefresh = () => {
    void loadEvents(selectedSandboxName, limit)
  }

  const sortedEvents = useMemo(() => events, [events])

  return (
    <>
      <header className="card border border-base-300 bg-base-100 shadow-sm">
        <div className="card-body gap-3">
          <div>
            <h2 className="text-2xl font-semibold">Sandbox Events</h2>
            <p className="text-sm text-base-content/70">View Sandboxes events and filter by sandbox.</p>
          </div>

          <div className="flex flex-wrap items-center gap-2">
            <label className="flex items-center gap-2">
              <span className="text-sm">Sandbox</span>
              <select
                className="select select-sm select-bordered w-full max-w-xs"
                value={selectedSandboxName}
                onChange={handleSandboxChange}
                disabled={isSandboxesLoading}
              >
                <option value="">All sandboxes</option>
                {sandboxes.map((sandbox, index) => {
                  const name = sandbox.name ?? ''
                  return (
                    <option key={name || sandbox.id || `events-sandbox-${index}`} value={name}>
                      {name || '-'}
                    </option>
                  )
                })}
              </select>
            </label>

            <label className="flex items-center gap-2">
              <span className="text-sm">Limit</span>
              <select className="select select-sm select-bordered" value={String(limit)} onChange={handleLimitChange}>
                {limitOptions.map((value) => (
                  <option key={value} value={value}>
                    {value}
                  </option>
                ))}
              </select>
            </label>

            <button className={`btn btn-sm btn-outline ${isEventsLoading ? 'btn-disabled' : ''}`} type="button" onClick={handleRefresh} disabled={isEventsLoading}>
              {isEventsLoading ? 'Refreshing...' : 'Refresh'}
            </button>

            <button className="btn btn-sm btn-outline" type="button" onClick={() => void refreshSandboxes()} disabled={isSandboxesLoading}>
              {isSandboxesLoading ? 'Loading...' : 'Reload Sandboxes'}
            </button>
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

      {eventsError && (
        <section>
          <div className="alert alert-error">
            <span>{eventsError}</span>
          </div>
        </section>
      )}

      <section>
        <div className="card border border-base-300 bg-base-100 shadow-sm">
          <div className="card-body gap-4">
            <h3 className="card-title text-lg">Events</h3>

            <div className="h-[calc(100vh-18rem)] overflow-x-auto rounded-box border border-base-300">
              <table className="table table-pin-rows table-zebra">
                <thead>
                  <tr>
                    <th>Time</th>
                    <th>Type</th>
                    <th>Reason</th>
                    <th>Sandbox</th>
                    <th>Message</th>
                    <th className="text-right">Count</th>
                  </tr>
                </thead>
                <tbody>
                  {isEventsLoading ? (
                    <tr>
                      <td colSpan={6} className="text-center text-base-content/70">
                        Loading events...
                      </td>
                    </tr>
                  ) : sortedEvents.length === 0 ? (
                    <tr>
                      <td colSpan={6} className="text-center text-base-content/70">
                        No events found.
                      </td>
                    </tr>
                  ) : (
                    sortedEvents.map((item) => (
                      <tr key={item.name}>
                        <td>{formatEventTime(item)}</td>
                        <td>
                          <span className={`badge badge-sm ${item.type === 'Warning' ? 'badge-warning' : 'badge-info'}`}>{item.type || '-'}</span>
                        </td>
                        <td>{item.reason || '-'}</td>
                        <td>{item.involvedObject?.name || '-'}</td>
                        <td className="max-w-[760px] whitespace-normal break-words">{item.message || '-'}</td>
                        <td className="text-right">{item.count}</td>
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
