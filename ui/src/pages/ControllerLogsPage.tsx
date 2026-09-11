import { useCallback, useEffect, useState } from 'react'
import { useSearchParams } from 'react-router-dom'

import TelemetryRangeControls from '../components/common/TelemetryRangeControls'
import {
  TELEMETRY_TIME_RANGES,
  getTelemetryLogs,
  type TelemetryLogEntry,
  type TelemetryLogEventFilter,
  type TelemetryLogsData,
  type TelemetryTimeRange,
} from '../lib/api/telemetry'

const LIMIT_OPTIONS = [50, 100, 200, 500]
const DEFAULT_LIMIT = 100
const DEFAULT_RANGE: TelemetryTimeRange = '24h'
const REFRESH_INTERVAL_OPTIONS_MS = [30_000, 60_000, 120_000, 300_000, 600_000]
const DEFAULT_REFRESH_INTERVAL_MS = 120_000

function isRange(v: string | null): v is TelemetryTimeRange {
  return v !== null && (TELEMETRY_TIME_RANGES as readonly string[]).includes(v)
}

const EVENT_OPTIONS: { value: TelemetryLogEventFilter; label: string }[] = [
  { value: '', label: 'All' },
  { value: 'create', label: 'Create' },
  { value: 'delete', label: 'Delete' },
  { value: 'pause', label: 'Pause' },
  { value: 'resume', label: 'Resume' },
  { value: 'snapshot', label: 'Snapshot' },
  { value: 'snapshot_delete', label: 'Snapshot Delete' },
]

function isEvent(v: string | null): v is TelemetryLogEventFilter {
  return v === null || EVENT_OPTIONS.some((o) => o.value === v)
}

function isLimit(v: string | null): number | null {
  if (v === null) return null
  const n = Number.parseInt(v, 10)
  return Number.isFinite(n) && LIMIT_OPTIONS.includes(n) ? n : null
}

function formatLogTime(v: unknown): string {
  if (typeof v !== 'string' || v === '') return '-'
  const d = new Date(v)
  if (Number.isNaN(d.getTime())) return v
  return d.toLocaleString(undefined, {
    month: 'short',
    day: 'numeric',
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit',
  })
}

function formatRefreshInterval(ms: number): string {
  if (ms < 60_000) return `${ms / 1000}s`
  return `${ms / 60_000}m`
}

function errorMessage(err: unknown): string {
  if (err instanceof Error) return err.message
  return 'Unknown error'
}

export default function ControllerLogsPage() {
  const [searchParams, setSearchParams] = useSearchParams()
  const range: TelemetryTimeRange = isRange(searchParams.get('since'))
    ? (searchParams.get('since') as TelemetryTimeRange)
    : DEFAULT_RANGE
  const eventParam = searchParams.get('event')
  const event: TelemetryLogEventFilter = isEvent(eventParam)
    ? ((eventParam ?? '') as TelemetryLogEventFilter)
    : ''
  const limit = isLimit(searchParams.get('limit')) ?? DEFAULT_LIMIT
  const userKey = searchParams.get('user') ?? ''
  const startISO = searchParams.get('start') ?? ''
  const endISO = searchParams.get('end') ?? ''

  const [userKeyInput, setUserKeyInput] = useState(userKey)
  useEffect(() => {
    setUserKeyInput(userKey)
  }, [userKey])

  const [data, setData] = useState<TelemetryLogsData | null>(null)
  const [loading, setLoading] = useState(true)
  const [loadError, setLoadError] = useState('')

  const [isAutoRefresh, setIsAutoRefresh] = useState(true)
  const [refreshIntervalMs, setRefreshIntervalMs] = useState(DEFAULT_REFRESH_INTERVAL_MS)

  const setFilters = useCallback(
    (next: {
      since?: TelemetryTimeRange
      startISO?: string
      endISO?: string
      event?: TelemetryLogEventFilter
      limit?: number
      userKey?: string
    }) => {
      const params = new URLSearchParams(searchParams)
      if (next.since !== undefined) {
        params.set('since', next.since)
        // Selecting a preset clears any custom range.
        params.delete('start')
        params.delete('end')
      }
      if (next.startISO !== undefined || next.endISO !== undefined) {
        const s = next.startISO ?? startISO
        const e = next.endISO ?? endISO
        if (s && e) {
          params.set('start', s)
          params.set('end', e)
        } else {
          params.delete('start')
          params.delete('end')
        }
      }
      if (next.event !== undefined) {
        if (next.event === '') params.delete('event')
        else params.set('event', next.event)
      }
      if (next.limit !== undefined) {
        if (next.limit === DEFAULT_LIMIT) params.delete('limit')
        else params.set('limit', String(next.limit))
      }
      if (next.userKey !== undefined) {
        if (next.userKey.trim() === '') params.delete('user')
        else params.set('user', next.userKey.trim())
      }
      setSearchParams(params, { replace: true })
    },
    [searchParams, setSearchParams, startISO, endISO]
  )

  const load = useCallback(
    async (opts?: { silent?: boolean }) => {
      if (!opts?.silent) {
        setLoading(true)
      }
      setLoadError('')
      try {
        const d = await getTelemetryLogs({
          since: range,
          startISO: startISO || undefined,
          endISO: endISO || undefined,
          event,
          userKey: userKey || undefined,
          limit,
        })
        setData(d)
      } catch (err) {
        setLoadError(errorMessage(err))
        if (!opts?.silent) setData(null)
      } finally {
        if (!opts?.silent) setLoading(false)
      }
    },
    [range, startISO, endISO, event, limit, userKey]
  )

  useEffect(() => {
    void load()
  }, [load])

  useEffect(() => {
    if (!isAutoRefresh) return
    const timer = window.setInterval(() => {
      void load({ silent: true })
    }, refreshIntervalMs)
    return () => window.clearInterval(timer)
  }, [isAutoRefresh, refreshIntervalMs, load])

  const items = data?.items ?? []

  return (
    <>
      <header className="card border border-base-300 bg-base-100 shadow-sm">
        <div className="card-body gap-3">
          <div>
            <h2 className="text-2xl font-semibold">Controller Logs</h2>
            <p className="text-sm text-base-content/70">
                Sandboxes lifecycle logs emitted by the sandbox controller.
            </p>
          </div>

          <TelemetryRangeControls
            since={range}
            startISO={startISO}
            endISO={endISO}
            onSelectPreset={(s) => setFilters({ since: s })}
            onApplyCustom={(s, e) => setFilters({ startISO: s, endISO: e })}
            onClearCustom={() => setFilters({ startISO: '', endISO: '' })}
          />

          <div className="flex flex-wrap items-center gap-2">
            <span className="text-sm">Type:</span>
            <select
                style={{width: "150px"}}
              className="select select-sm select-bordered"
              value={event}
              onChange={(e) => setFilters({ event: e.target.value as TelemetryLogEventFilter })}
            >
              {EVENT_OPTIONS.map(({ value, label }) => (
                <option key={value || 'all'} value={value}>
                  {label}
                </option>
              ))}
            </select>

            <span className="text-sm">UserKey:</span>
            <div className="join">
              <input
                type="text"
                placeholder="Filter by user key…"
                className="input input-sm input-bordered join-item w-80"
                value={userKeyInput}
                onChange={(e) => setUserKeyInput(e.target.value)}
                onKeyDown={(e) => {
                  if (e.key === 'Enter') setFilters({ userKey: userKeyInput })
                }}
              />
              <button className="btn btn-sm join-item" onClick={() => setFilters({ userKey: userKeyInput })}>
                Apply
              </button>
              {userKeyInput && (
                <button
                  className="btn btn-sm join-item btn-ghost"
                  onClick={() => {
                    setUserKeyInput('')
                    setFilters({ userKey: '' })
                  }}
                >
                  Clear
                </button>
              )}
            </div>

            <label className="flex items-center gap-2">
              <span className="text-sm">Limit</span>
              <select
                className="select select-sm select-bordered"
                value={String(limit)}
                onChange={(e) => {
                  const n = Number.parseInt(e.target.value, 10)
                  if (!Number.isNaN(n)) setFilters({ limit: n })
                }}
              >
                {LIMIT_OPTIONS.map((n) => (
                  <option key={n} value={n}>
                    {n}
                  </option>
                ))}
              </select>
            </label>

            <div className="flex-1" />

            <label className="label cursor-pointer gap-2 py-0">
              <span className="label-text text-sm">Auto refresh</span>
              <input
                type="checkbox"
                className="toggle toggle-sm "
                checked={isAutoRefresh}
                onChange={(e) => setIsAutoRefresh(e.target.checked)}
              />
            </label>
            <select
              className="select select-sm select-bordered  w-20"
              value={String(refreshIntervalMs)}
              disabled={!isAutoRefresh}
              onChange={(e) => {
                const n = Number.parseInt(e.target.value, 10)
                if (!Number.isNaN(n) && n > 0) setRefreshIntervalMs(n)
              }}
            >
              {REFRESH_INTERVAL_OPTIONS_MS.map((ms) => (
                <option key={ms} value={ms}>
                  {formatRefreshInterval(ms)}
                </option>
              ))}
            </select>
            <button className="btn btn-sm btn-outline" onClick={() => void load()}>
              Refresh
            </button>
          </div>
        </div>
      </header>

      <div className="mt-4">
        <div className="card bg-base-100 shadow-sm">
          <div className="card-body p-4">
            {loadError && (
              <div className="mb-3 alert alert-warning">
                <span>{loadError}</span>
              </div>
            )}

            {loading ? (
              <div className="flex h-40 items-center justify-center text-sm text-base-content/60">
                <span className="loading loading-spinner loading-sm mr-2" /> Loading…
              </div>
            ) : items.length === 0 ? (
              <div className="flex h-40 items-center justify-center text-sm text-base-content/60">
                No telemetry records in this window.
              </div>
            ) : (
              <div className="overflow-x-auto">
                <table className="table table-sm">
                  <thead>
                    <tr>
                      <th>Time / Sandbox ID / User / Status</th>
                      <th>Content</th>
                    </tr>
                  </thead>
                  <tbody>
                    {items.map((it, idx) => (
                      <LogRow key={String(it['event_id'] ?? idx)} entry={it} />
                    ))}
                  </tbody>
                </table>
              </div>
            )}
          </div>
        </div>
      </div>
    </>
  )
}

// Fields broken out into their own columns; everything else (including _msg)
// gets dumped into the Content cell as JSON.
const EXTRACTED_FIELDS = new Set(['_time', 'user_key', 'sandbox_id', 'success'])

function buildContent(entry: TelemetryLogEntry): string {
  const rest: Record<string, unknown> = {}
  for (const [k, v] of Object.entries(entry)) {
    if (EXTRACTED_FIELDS.has(k)) continue
    // Drop VictoriaLogs internal fields (e.g. _stream, _stream_id). _msg is
    // kept because it carries the human-readable body.
    if (k.startsWith('_') && k !== '_msg') continue
    rest[k] = v
  }
  try {
    return JSON.stringify(rest)
  } catch {
    return ''
  }
}

function LogRow({ entry }: { entry: TelemetryLogEntry }) {
  const content = buildContent(entry)
  return (
    <tr>
        <td className="truncate"  style={{verticalAlign: "top"}}>
            <div className="whitespace-nowrap font-mono text-xs">{formatLogTime(entry['_time'])}</div>
            <div className="text-base-content/50">{String(entry['sandbox_id'] ?? '-')} </div>
            <span className="text-sm text-base-content/50">{String(entry['user_key'] ?? '-')}</span>
            <br/>
            <div className={`badge badge-soft  badge-xs ${entry['success'] === 'true' ? 'badge-info' : 'badge-warning'}`} >{String(entry['success'] ?? '-')}</div>
        </td>
        <td className="font-mono text-xs text-base-content/80 break-all">
        {content}
      </td>
    </tr>
  )
}
