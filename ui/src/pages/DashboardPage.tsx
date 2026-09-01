import { ChangeEvent, useCallback, useEffect, useRef, useState } from 'react'

import { listSandboxEvents } from '../lib/api/events'
import { type NormalizedRateLimitStatusData } from '../lib/api/ratelimit'
import { getDashboardStatus } from '../lib/api/status'
import type { SandboxEventItem } from '../lib/api/types'

const refreshIntervalOptions = [2000, 5000, 10000, 30000]
const limitOptions = [50, 100, 200, 500]

const emptyRateLimitStatus: NormalizedRateLimitStatusData = {
  default_config: {
    enabled: false,
    max_concurrency: 0,
    max_sandbox: 0,
  },
  users: [],
}

type LoadError = {
  section: string
  message: string
}

const topItemLimit = 5
const progressColorClasses = ['progress-primary', 'progress-secondary', 'progress-accent', 'progress-info', 'progress-success', 'progress-warning']

function clampPercent(value: number): number {
  return Math.max(0, Math.min(value, 100))
}

function progressColorClass(index: number): string {
  return progressColorClasses[index % progressColorClasses.length]
}

function formatKeyDisplay(value?: string): string {
  const key = value?.trim()
  if (!key) {
    return 'Unknown Key'
  }

  const parts = key.split('-').filter(Boolean)
  return `${parts.length >= 2 ? parts.slice(0, 2).join('-') : parts[0]}...`
}

function formatLimit(value: number): string {
  return value > 0 ? String(value) : 'Unlimited'
}

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

function formatError(error: unknown, fallback: string): string {
  return error instanceof Error ? error.message : fallback
}

export default function DashboardPage() {
  const [sandboxTotal, setSandboxTotal] = useState(0)
  const [rateLimitStatus, setRateLimitStatus] = useState<NormalizedRateLimitStatusData>(emptyRateLimitStatus)
  const [leader, setLeader] = useState('')
  const [events, setEvents] = useState<SandboxEventItem[]>([])
  const [loadErrors, setLoadErrors] = useState<LoadError[]>([])
  const [isLoading, setIsLoading] = useState(false)
  const [isAutoRefresh, setIsAutoRefresh] = useState(true)
  const [refreshIntervalMs, setRefreshIntervalMs] = useState(5000)
  const [eventLimit, setEventLimit] = useState(100)
  const requestInFlightRef = useRef(false)

  const loadDashboard = useCallback(async () => {
    if (requestInFlightRef.current) {
      return
    }

    requestInFlightRef.current = true
    setIsLoading(true)
    setLoadErrors([])

    try {
      const [statusResult, eventsResult] = await Promise.allSettled([
        getDashboardStatus(),
        listSandboxEvents({ limit: eventLimit }),
      ])
      const nextErrors: LoadError[] = []

      if (statusResult.status === 'fulfilled') {
        const s = statusResult.value
        setSandboxTotal(s.sandboxes?.total ?? 0)
        setRateLimitStatus({
          default_config: {
            enabled: s.capacity?.default_config?.enabled ?? true,
            max_concurrency: s.capacity?.default_config?.max_concurrency ?? 0,
            max_sandbox: s.capacity?.default_config?.max_sandbox ?? 0,
          },
          users: Array.isArray(s.capacity?.users) ? s.capacity.users : [],
        })
        setLeader(typeof s.leader === 'string' ? s.leader : '')
      } else {
        nextErrors.push({ section: 'Status', message: formatError(statusResult.reason, 'Failed to load status') })
        setSandboxTotal(0)
        setRateLimitStatus(emptyRateLimitStatus)
        setLeader('')
      }

      if (eventsResult.status === 'fulfilled') {
        setEvents(Array.isArray(eventsResult.value.items) ? eventsResult.value.items : [])
      } else {
        nextErrors.push({ section: 'Events', message: formatError(eventsResult.reason, 'Failed to load events') })
        setEvents([])
      }

      setLoadErrors(nextErrors)
    } finally {
      requestInFlightRef.current = false
      setIsLoading(false)
    }
  }, [eventLimit])

  useEffect(() => {
    void loadDashboard()
  }, [loadDashboard])

  useEffect(() => {
    if (!isAutoRefresh) {
      return
    }

    const timer = window.setInterval(() => {
      void loadDashboard()
    }, refreshIntervalMs)

    return () => {
      window.clearInterval(timer)
    }
  }, [isAutoRefresh, loadDashboard, refreshIntervalMs])

  const handleRefreshIntervalChange = (event: ChangeEvent<HTMLSelectElement>) => {
    const parsed = Number.parseInt(event.target.value, 10)
    if (!Number.isNaN(parsed) && parsed > 0) {
      setRefreshIntervalMs(parsed)
    }
  }

  const handleEventLimitChange = (event: ChangeEvent<HTMLSelectElement>) => {
    const parsed = Number.parseInt(event.target.value, 10)
    if (!Number.isNaN(parsed) && parsed > 0) {
      setEventLimit(parsed)
    }
  }

  return (
      <>
          <header className="card border border-base-300 bg-base-100 shadow-sm">
              <div className="card-body gap-3">
                  <div className="flex flex-wrap items-start justify-between gap-3">
                      <div>
                          <h2 className="text-2xl font-semibold">Dashboard (Real time)</h2>
                          <p className="text-sm text-base-content/70">Monitor sandbox count, capacity, API keys,
                              templates, and recent events.</p>
                      </div>
                  </div>
                  <div className="flex flex-wrap items-center justify-end gap-2">
                      <label className="label cursor-pointer gap-2 py-0">
                          <span className="label-text text-sm">Auto Refresh</span>
                          <input
                              className="toggle toggle-sm"
                              type="checkbox"
                              checked={isAutoRefresh}
                              onChange={() => {
                                  setIsAutoRefresh((prev) => !prev)
                              }}
                          />
                      </label>
                      <select className="select select-sm select-bordered w-20" value={refreshIntervalMs}
                              onChange={handleRefreshIntervalChange}>
                          {refreshIntervalOptions.map((option) => (
                              <option key={option} value={option}>
                                  {option / 1000}s
                              </option>
                          ))}
                      </select>
                      <select className="select select-sm select-bordered w-28" value={eventLimit}
                              onChange={handleEventLimitChange}>
                          {limitOptions.map((option) => (
                              <option key={option} value={option}>
                                  {option} events
                              </option>
                          ))}
                      </select>
                      <button type="button" className="btn btn-sm btn-outline" onClick={() => void loadDashboard()}
                              disabled={isLoading}>
                          {isLoading ? 'Refreshing...' : 'Refresh'}
                      </button>
                  </div>
              </div>
          </header>

          {loadErrors.length > 0 && (
              <section>
                  <div className="alert alert-error">
                      <div>
                          <div className="font-semibold">Some dashboard data could not be loaded.</div>
                          <ul className="list-disc pl-5 text-sm">
                              {loadErrors.map((error) => (
                                  <li key={error.section}>
                                      {error.section}: {error.message}
                                  </li>
                              ))}
                          </ul>
                      </div>
                  </div>
              </section>
          )}
          <div role="alert" className="alert " style={{justifyContent: 'end'}}>
              Current Leader: <span className="badge  badge-info">{leader}</span>
          </div>
          <section className="grid gap-3 xl:grid-cols-12">
              <div className="card border border-base-300 bg-base-100 shadow-sm xl:col-span-2">
                  <div className="card-body gap-4 py-4">
                      <div className="flex items-center justify-between gap-3">
                          <h3 className="card-title text-lg">Sandboxes</h3>
                      </div>
                      <div className="stat">
                          <div className="stat-figure text-primary">
                          </div>
                          <div className="stat-value text-primary">{sandboxTotal} </div>
                          <div className="stat-desc">Total sandboxes in current</div>
                      </div>
                  </div>
              </div>

              <div className="card border border-base-300 bg-base-100 shadow-sm xl:col-span-10">
                  <div className="card-body gap-4 py-4">
                      <div className="flex flex-wrap items-center justify-between gap-3">
                          <div className="flex flex-wrap items-center gap-3">
                              <h3 className="card-title text-lg">Capacity</h3>
                              <div className="badge badge-secondary badge-outline">
                                  {rateLimitStatus.default_config.enabled ? 'Enabled' : 'Disabled'}
                              </div>
                              <div className="badge badge-outline">Default
                                  Sandbox: {formatLimit(rateLimitStatus.default_config.max_sandbox)}</div>
                              <div className="badge badge-outline">Default
                                  Concurrency: {formatLimit(rateLimitStatus.default_config.max_concurrency)}</div>
                              <span className="text-sm font-thin">(TOP-5)</span>
                          </div>
                      </div>
                      <div className="grid gap-3 md:grid-cols-3 xl:grid-cols-6">
                          {rateLimitStatus.users.length === 0 ? (
                              <div className="text-sm text-base-content/60">No per-key capacity data found.</div>
                          ) : (
                              rateLimitStatus.users.slice(0, topItemLimit).map((user, index) => {
                                  const usagePercent = user.sandbox_max > 0 ? clampPercent(user.sandbox_usage_percent) : 0
                                  return (
                                      <div key={user.user || `capacity-${index}`}
                                           className="rounded-box border border-base-300 p-3">
                                          <div className="mb-2 text-sm">
                                              <span
                                                  className="truncate font-medium">{formatKeyDisplay(user.user)}</span>
                                          </div>
                                          <progress className={`progress ${progressColorClass(index)} h-2 w-full`}
                                                    value={usagePercent} max="100"/>
                                          <div
                                              className="mt-1 flex items-center justify-between gap-3 text-xs text-base-content/70">
                                              <span
                                                  className="tabular-nums">{user.sandbox_current} / {formatLimit(user.sandbox_max)}</span>
                                              <span
                                                  className="tabular-nums">{user.sandbox_max > 0 ? `${usagePercent}%` : '-'}</span>
                                          </div>
                                          <div
                                              className="mt-2 text-xs text-base-content/60">Concurrency: {user.concurrency_active} / {formatLimit(user.concurrency_max)}</div>
                                      </div>
                                  )
                              })
                          )}
                      </div>
                  </div>
              </div>
          </section>

          <section>
              <div className="card border border-base-300 bg-base-100 shadow-sm">
                  <div className="card-body gap-4">
                      <div className="flex flex-wrap items-center justify-between gap-2">
                          <h3 className="card-title text-lg">Recent Events</h3>
                          <div className="badge badge-outline">{events.length} events</div>
                      </div>

                      <div className="h-[calc(100vh-28rem)] min-h-72 overflow-auto rounded-box border border-base-300">
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
                              {events.length === 0 && (
                                  <tr>
                                      <td colSpan={6} className="py-8 text-center text-base-content/60">
                                          No events found.
                                      </td>
                                  </tr>
                              )}
                              {events.map((event) => {
                                  const eventType = event.type || '-'
                                  const badgeClass = eventType.toLowerCase() === 'warning' ? 'badge-warning' : 'badge-info'

                                  return (
                                      <tr key={event.name}>
                                          <td className="whitespace-nowrap text-xs">{formatEventTime(event)}</td>
                                          <td>
                                              <span className={`badge badge-sm ${badgeClass}`}>{eventType}</span>
                                          </td>
                                          <td className="font-medium">{event.reason || '-'}</td>
                                          <td>{event.involvedObject?.name || '-'}</td>
                                          <td className="max-w-xl whitespace-normal text-sm text-base-content/80">{event.message || '-'}</td>
                                          <td className="text-right tabular-nums">{event.count}</td>
                                      </tr>
                                  )
                              })}
                              </tbody>
                          </table>
                      </div>
                  </div>
              </div>
          </section>
      </>
  )
}
