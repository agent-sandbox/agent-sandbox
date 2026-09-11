import { requestEnvelope } from './http'

export type TelemetryStatusData = {
  enabled: boolean
  otlp_endpoint: string
  otlp_url_path: string
  otlp_insecure: boolean
  query_endpoint: string
  install_yaml_path: string
}

export type TelemetrySummaryData = {
  create_total: number
  create_success: number
  create_failed: number
  delete_total: number
  p50_duration_seconds: number
  p90_duration_seconds: number
  p99_duration_seconds: number
  p50_alive_seconds: number
  p90_alive_seconds: number
}

export type TelemetryTimeseriesPoint = {
  t: string
  n: number
}

export type TelemetryTimeseriesGroup = {
  name: string
  points: TelemetryTimeseriesPoint[]
}

export type TelemetryTimeseriesData = {
  step: string
  buckets?: TelemetryTimeseriesPoint[]
  series?: TelemetryTimeseriesGroup[]
}

export type TelemetryByUserData = {
  users: Array<{ user_key: string; n: number }>
}

export type TelemetryDurationBucket = {
  le: number | null
  n: number
}

export type TelemetryDurationsData = {
  metric: 'duration_seconds' | 'alive_seconds'
  buckets: TelemetryDurationBucket[]
  p50: number
  p90: number
  p99: number
}

// VictoriaLogs returns each log record as a flat JSON object. We keep it
// loosely typed and let the page pick the fields it knows about.
export type TelemetryLogEntry = Record<string, unknown>

export type TelemetryLogsData = {
  items: TelemetryLogEntry[]
}

export type TelemetryLogEventFilter = '' | 'create' | 'delete' | 'pause' | 'resume' | 'snapshot' | 'snapshot_delete'

export const TELEMETRY_TIME_RANGES = ['1h', '6h', '24h', '7d', '30d'] as const
export type TelemetryTimeRange = (typeof TELEMETRY_TIME_RANGES)[number]

const PRESET_MS: Record<TelemetryTimeRange, number> = {
  '1h': 60 * 60 * 1000,
  '6h': 6 * 60 * 60 * 1000,
  '24h': 24 * 60 * 60 * 1000,
  '7d': 7 * 24 * 60 * 60 * 1000,
  '30d': 30 * 24 * 60 * 60 * 1000,
}

// A page can pick either a preset relative window or an explicit absolute
// [start, end] pair. Either way the API call always sends `from`/`to`.
export type TelemetryRange = {
  since: TelemetryTimeRange
  startISO?: string
  endISO?: string
}

function rangeQuery(r: TelemetryRange): Record<string, string | undefined> {
  if (r.startISO && r.endISO) {
    return { from: r.startISO, to: r.endISO }
  }
  const now = new Date()
  const from = new Date(now.getTime() - PRESET_MS[r.since])
  return { from: from.toISOString(), to: now.toISOString() }
}

function buildQuery(params: Record<string, string | number | undefined>): string {
  const parts: string[] = []
  for (const [k, v] of Object.entries(params)) {
    if (v === undefined || v === '') continue
    parts.push(`${encodeURIComponent(k)}=${encodeURIComponent(String(v))}`)
  }
  return parts.length === 0 ? '' : `?${parts.join('&')}`
}

export async function getTelemetryStatus(): Promise<TelemetryStatusData> {
  return requestEnvelope<TelemetryStatusData>('/telemetry/status', { method: 'GET' })
}

export async function getTelemetrySummary(p: TelemetryRange & {
  userKey?: string
}): Promise<TelemetrySummaryData> {
  return requestEnvelope<TelemetrySummaryData>(
    `/telemetry/summary${buildQuery({ ...rangeQuery(p), user_key: p.userKey })}`,
    { method: 'GET' }
  )
}

export async function getTelemetryTimeseries(p: TelemetryRange & {
  event: 'create' | 'delete'
  groupBy?: string
  userKey?: string
  step?: string
}): Promise<TelemetryTimeseriesData> {
  return requestEnvelope<TelemetryTimeseriesData>(
    `/telemetry/timeseries${buildQuery({
      ...rangeQuery(p),
      event: p.event,
      group_by: p.groupBy,
      user_key: p.userKey,
      step: p.step,
    })}`,
    { method: 'GET' }
  )
}

export async function getTelemetryByUser(p: TelemetryRange & {
  event: 'create' | 'delete'
  top?: number
}): Promise<TelemetryByUserData> {
  return requestEnvelope<TelemetryByUserData>(
    `/telemetry/by_user${buildQuery({ ...rangeQuery(p), event: p.event, top: p.top })}`,
    { method: 'GET' }
  )
}

export async function getTelemetryDurations(p: TelemetryRange & {
  metric: 'duration_seconds' | 'alive_seconds'
  userKey?: string
}): Promise<TelemetryDurationsData> {
  return requestEnvelope<TelemetryDurationsData>(
    `/telemetry/durations${buildQuery({
      ...rangeQuery(p),
      metric: p.metric,
      user_key: p.userKey,
    })}`,
    { method: 'GET' }
  )
}

export async function getTelemetryLogs(p: TelemetryRange & {
  event?: TelemetryLogEventFilter
  userKey?: string
  limit?: number
}): Promise<TelemetryLogsData> {
  return requestEnvelope<TelemetryLogsData>(
    `/telemetry/logs${buildQuery({
      ...rangeQuery(p),
      event: p.event || undefined,
      user_key: p.userKey,
      limit: p.limit,
    })}`,
    { method: 'GET' }
  )
}
