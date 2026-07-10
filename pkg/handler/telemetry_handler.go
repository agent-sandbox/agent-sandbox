/*
 * Copyright 2025 The https://github.com/agent-sandbox/agent-sandbox Authors.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package handler

import (
	"context"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/agent-sandbox/agent-sandbox/pkg/config"
	"github.com/agent-sandbox/agent-sandbox/pkg/telemetry"
)

const (
	installYAMLPath = "install/victorialogs.yaml"

	maxTopUsers       = 50
	maxLogsLimit      = 500
	defaultLogsLimit  = 100
	queryTimeoutSecs  = 10
	maxAbsoluteWindow = 31 * 24 * time.Hour
)

// timeWindow carries the resolved time filter for a request.
type timeWindow struct {
	// LogsQL `_time:` filter clause to splice into a query.
	Clause string
	// Absolute start / end. Always populated, used by stats_query_range.
	Start time.Time
	End   time.Time
}

// resolveTimeWindow reads `from` (required) and optional `to` (defaults to
// now) as RFC3339 timestamps and builds the LogsQL `_time:` clause.
func resolveTimeWindow(q url.Values) (timeWindow, error) {
	fromStr := strings.TrimSpace(q.Get("from"))
	if fromStr == "" {
		return timeWindow{}, fmt.Errorf("from is required")
	}
	from, err := time.Parse(time.RFC3339, fromStr)
	if err != nil {
		return timeWindow{}, fmt.Errorf("invalid from: %w", err)
	}

	var to time.Time
	if toStr := strings.TrimSpace(q.Get("to")); toStr != "" {
		to, err = time.Parse(time.RFC3339, toStr)
		if err != nil {
			return timeWindow{}, fmt.Errorf("invalid to: %w", err)
		}
	} else {
		to = time.Now().UTC()
	}

	if !to.After(from) {
		return timeWindow{}, fmt.Errorf("to must be after from")
	}
	if to.Sub(from) > maxAbsoluteWindow {
		return timeWindow{}, fmt.Errorf("range too long (max %s)", maxAbsoluteWindow)
	}

	fromU := from.UTC()
	toU := to.UTC()
	clause := fmt.Sprintf(`_time:[%s, %s]`, fromU.Format(time.RFC3339), toU.Format(time.RFC3339))
	return timeWindow{Clause: clause, Start: fromU, End: toU}, nil
}

// stepForWindow picks a bucket width given an absolute window. Mirrors
// telemetry.DefaultStep but works on any duration, not just allow-listed ones.
func stepForWindow(window time.Duration) time.Duration {
	switch {
	case window <= 6*time.Hour:
		return 10 * time.Minute
	case window <= 24*time.Hour:
		return time.Hour
	case window <= 7*24*time.Hour:
		return 6 * time.Hour
	default:
		return 24 * time.Hour
	}
}

// telemetryStatusData is the response of GET /api/v1/telemetry/status.
type telemetryStatusData struct {
	Enabled       bool   `json:"enabled"`
	OTLPEndpoint  string `json:"otlp_endpoint"`
	OTLPURLPath   string `json:"otlp_url_path"`
	OTLPInsecure  bool   `json:"otlp_insecure"`
	QueryEndpoint string `json:"query_endpoint"`
}

// GetTelemetryStatus reports config knobs; always callable even when
// telemetry is disabled so the UI can drive its setup wizard.
func (a *Handler) GetTelemetryStatus(r *http.Request) (interface{}, error) {
	cfg := config.Cfg.Telemetry
	return telemetryStatusData{
		Enabled:       cfg.Enabled,
		OTLPEndpoint:  cfg.OTLPEndpoint,
		OTLPURLPath:   cfg.OTLPURLPath,
		OTLPInsecure:  cfg.OTLPInsecure,
		QueryEndpoint: cfg.QueryEndpoint(),
	}, nil
}

// telemetrySummaryData is the response of GET /api/v1/telemetry/summary.
type telemetrySummaryData struct {
	CreateTotal        int64   `json:"create_total"`
	CreateSuccess      int64   `json:"create_success"`
	CreateFailed       int64   `json:"create_failed"`
	DeleteTotal        int64   `json:"delete_total"`
	P50DurationSeconds float64 `json:"p50_duration_seconds"`
	P90DurationSeconds float64 `json:"p90_duration_seconds"`
	P99DurationSeconds float64 `json:"p99_duration_seconds"`
	P50AliveSeconds    float64 `json:"p50_alive_seconds"`
	P90AliveSeconds    float64 `json:"p90_alive_seconds"`
}

// GetTelemetrySummary returns headline KPIs over the given window.
func (a *Handler) GetTelemetrySummary(r *http.Request) (interface{}, error) {
	tw, err := resolveTimeWindow(r.URL.Query())
	if err != nil {
		return nil, err
	}
	userFilter := telemetry.EscapeLogsQL(r.URL.Query().Get("user_key"))

	client, err := newQueryClient()
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(r.Context(), queryTimeoutSecs*time.Second)
	defer cancel()

	timeFilter := tw.Clause
	userClause := ""
	if userFilter != "" {
		userClause = fmt.Sprintf(` user_key:"%s"`, userFilter)
	}

	queries := map[string]string{
		"create_total":   fmt.Sprintf(`event_name:"sandbox.create"%s %s | stats count() as n`, userClause, timeFilter),
		"create_success": fmt.Sprintf(`event_name:"sandbox.create" success:true%s %s | stats count() as n`, userClause, timeFilter),
		"create_failed":  fmt.Sprintf(`event_name:"sandbox.create" success:false%s %s | stats count() as n`, userClause, timeFilter),
		"delete_total":   fmt.Sprintf(`event_name:"sandbox.delete"%s %s | stats count() as n`, userClause, timeFilter),
		"duration":       fmt.Sprintf(`event_name:"sandbox.create" success:true%s %s | stats quantile(0.5, duration_seconds) as p50, quantile(0.9, duration_seconds) as p90, quantile(0.99, duration_seconds) as p99`, userClause, timeFilter),
		// "alive" is just the delete event's duration_seconds (the sandbox lifetime).
		"alive": fmt.Sprintf(`event_name:"sandbox.delete"%s %s | stats quantile(0.5, duration_seconds) as p50, quantile(0.9, duration_seconds) as p90`, userClause, timeFilter),
	}

	type result struct {
		key string
		res []telemetry.ScalarResult
		err error
	}
	results := make(chan result, len(queries))
	var wg sync.WaitGroup
	for k, q := range queries {
		wg.Add(1)
		go func(k, q string) {
			defer wg.Done()
			r, err := client.StatsQuery(ctx, q, time.Time{})
			results <- result{key: k, res: r, err: err}
		}(k, q)
	}
	wg.Wait()
	close(results)

	data := telemetrySummaryData{}
	for r := range results {
		if r.err != nil {
			return nil, r.err
		}
		switch r.key {
		case "create_total":
			data.CreateTotal = int64(firstValue(r.res))
		case "create_success":
			data.CreateSuccess = int64(firstValue(r.res))
		case "create_failed":
			data.CreateFailed = int64(firstValue(r.res))
		case "delete_total":
			data.DeleteTotal = int64(firstValue(r.res))
		case "duration":
			data.P50DurationSeconds = labeledValue(r.res, "p50")
			data.P90DurationSeconds = labeledValue(r.res, "p90")
			data.P99DurationSeconds = labeledValue(r.res, "p99")
		case "alive":
			data.P50AliveSeconds = labeledValue(r.res, "p50")
			data.P90AliveSeconds = labeledValue(r.res, "p90")
		}
	}
	return data, nil
}

// telemetryTimeseriesData is the response of GET /api/v1/telemetry/timeseries.
type telemetryTimeseriesData struct {
	Step    string            `json:"step"`
	Buckets []timeseriesPoint `json:"buckets,omitempty"`
	Series  []timeseriesGroup `json:"series,omitempty"`
}

type timeseriesPoint struct {
	T time.Time `json:"t"`
	N float64   `json:"n"`
}

type timeseriesGroup struct {
	Name   string            `json:"name"`
	Points []timeseriesPoint `json:"points"`
}

// GetTelemetryTimeseries returns time-bucketed counts. Optional group_by
// switches the response shape to one series per label value.
func (a *Handler) GetTelemetryTimeseries(r *http.Request) (interface{}, error) {
	q := r.URL.Query()
	tw, err := resolveTimeWindow(q)
	if err != nil {
		return nil, err
	}
	step := stepForWindow(tw.End.Sub(tw.Start))
	if s := q.Get("step"); s != "" {
		if d, err := time.ParseDuration(s); err == nil && d > 0 {
			step = d
		}
	}

	event := q.Get("event")
	if event == "" {
		event = "create"
	}
	if event != "create" && event != "delete" {
		return nil, fmt.Errorf("invalid event %q", event)
	}
	eventName := "sandbox." + event

	groupBy := q.Get("group_by")
	if groupBy != "" && !isAllowedGroupBy(groupBy) {
		return nil, fmt.Errorf("invalid group_by %q", groupBy)
	}

	userFilter := telemetry.EscapeLogsQL(q.Get("user_key"))
	userClause := ""
	if userFilter != "" {
		userClause = fmt.Sprintf(` user_key:"%s"`, userFilter)
	}

	client, err := newQueryClient()
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(r.Context(), queryTimeoutSecs*time.Second)
	defer cancel()

	start := tw.Start
	end := tw.End

	var logsql string
	if groupBy == "" {
		logsql = fmt.Sprintf(`event_name:"%s"%s | stats count() as n`, eventName, userClause)
	} else {
		logsql = fmt.Sprintf(`event_name:"%s"%s | stats by (%s) count() as n`, eventName, userClause, groupBy)
	}

	series, err := client.StatsQueryRange(ctx, logsql, start, end, step)
	if err != nil {
		return nil, err
	}

	out := telemetryTimeseriesData{Step: formatDuration(step)}
	if groupBy == "" {
		// Expect a single series; flatten to buckets.
		if len(series) == 0 {
			out.Buckets = []timeseriesPoint{}
			return out, nil
		}
		s := series[0]
		out.Buckets = make([]timeseriesPoint, 0, len(s.Points))
		for _, p := range s.Points {
			out.Buckets = append(out.Buckets, timeseriesPoint{T: p.T, N: p.V})
		}
		return out, nil
	}

	out.Series = make([]timeseriesGroup, 0, len(series))
	for _, s := range series {
		name := s.Labels[groupBy]
		if name == "" {
			name = "(empty)"
		}
		pts := make([]timeseriesPoint, 0, len(s.Points))
		for _, p := range s.Points {
			pts = append(pts, timeseriesPoint{T: p.T, N: p.V})
		}
		out.Series = append(out.Series, timeseriesGroup{Name: name, Points: pts})
	}
	sort.Slice(out.Series, func(i, j int) bool { return out.Series[i].Name < out.Series[j].Name })
	return out, nil
}

// telemetryByUserData is the response of GET /api/v1/telemetry/by_user.
type telemetryByUserData struct {
	Users []userCount `json:"users"`
}

type userCount struct {
	UserKey string `json:"user_key"`
	N       int64  `json:"n"`
}

// GetTelemetryByUser returns top-N users ordered by event count.
func (a *Handler) GetTelemetryByUser(r *http.Request) (interface{}, error) {
	q := r.URL.Query()
	tw, err := resolveTimeWindow(q)
	if err != nil {
		return nil, err
	}
	event := q.Get("event")
	if event == "" {
		event = "create"
	}
	if event != "create" && event != "delete" {
		return nil, fmt.Errorf("invalid event %q", event)
	}
	eventName := "sandbox." + event

	top := 10
	if s := q.Get("top"); s != "" {
		n, err := strconv.Atoi(s)
		if err != nil || n <= 0 {
			return nil, fmt.Errorf("invalid top %q", s)
		}
		if n > maxTopUsers {
			n = maxTopUsers
		}
		top = n
	}

	client, err := newQueryClient()
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(r.Context(), queryTimeoutSecs*time.Second)
	defer cancel()

	logsql := fmt.Sprintf(`event_name:"%s" %s | stats by (user_key) count() as n | sort by (n desc) | limit %d`, eventName, tw.Clause, top)
	res, err := client.StatsQuery(ctx, logsql, time.Time{})
	if err != nil {
		return nil, err
	}

	users := make([]userCount, 0, len(res))
	for _, r := range res {
		users = append(users, userCount{
			UserKey: r.Labels["user_key"],
			N:       int64(r.Value),
		})
	}
	sort.Slice(users, func(i, j int) bool { return users[i].N > users[j].N })
	return telemetryByUserData{Users: users}, nil
}

// telemetryDurationsData is the response of GET /api/v1/telemetry/durations.
type telemetryDurationsData struct {
	Metric  string           `json:"metric"`
	Buckets []durationBucket `json:"buckets"`
	P50     float64          `json:"p50"`
	P90     float64          `json:"p90"`
	P99     float64          `json:"p99"`
}

type durationBucket struct {
	LE *float64 `json:"le"` // null = +Inf bucket
	N  int64    `json:"n"`
}

// Bucket boundaries for the create-duration and sandbox-lifetime histograms.
// Both metrics now share the duration_seconds attribute (lifetime is the
// delete event's elapsed time).
var durationBucketsSec = []float64{0.1, 0.5, 1, 5, 30, 120, 600}
var aliveBucketsSec = []float64{60, 300, 900, 1800, 3600, 7200, 14400, 28800, 86400}

// GetTelemetryDurations returns a histogram + percentiles for duration_ms or
// alive_seconds.
func (a *Handler) GetTelemetryDurations(r *http.Request) (interface{}, error) {
	q := r.URL.Query()
	tw, err := resolveTimeWindow(q)
	if err != nil {
		return nil, err
	}
	metric := q.Get("metric")
	if metric == "" {
		metric = "duration_seconds"
	}
	var (
		eventName string
		field     string
		buckets   []float64
	)
	switch metric {
	case "duration_seconds":
		eventName = "sandbox.create"
		field = "duration_seconds"
		buckets = durationBucketsSec
	case "alive_seconds":
		// "alive_seconds" is the option name; the underlying field is
		// duration_seconds on the delete event.
		eventName = "sandbox.delete"
		field = "duration_seconds"
		buckets = aliveBucketsSec
	default:
		return nil, fmt.Errorf("invalid metric %q", metric)
	}

	userFilter := telemetry.EscapeLogsQL(q.Get("user_key"))
	userClause := ""
	if userFilter != "" {
		userClause = fmt.Sprintf(` user_key:"%s"`, userFilter)
	}

	client, err := newQueryClient()
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(r.Context(), queryTimeoutSecs*time.Second)
	defer cancel()

	pctQuery := fmt.Sprintf(`event_name:"%s"%s %s | stats quantile(0.5, %s) as p50, quantile(0.9, %s) as p90, quantile(0.99, %s) as p99`, eventName, userClause, tw.Clause, field, field, field)
	pctRes, err := client.StatsQuery(ctx, pctQuery, time.Time{})
	if err != nil {
		return nil, err
	}

	// Histogram via repeated bucket count queries. Cheap compared to scanning
	// in the client and avoids needing LogsQL `histogram_quantile`.
	type bucketHit struct {
		LE  *float64
		N   int64
		err error
	}
	hits := make(chan bucketHit, len(buckets)+1)
	var wg sync.WaitGroup
	for _, le := range buckets {
		wg.Add(1)
		go func(le float64) {
			defer wg.Done()
			leLocal := le
			cq := fmt.Sprintf(`event_name:"%s"%s %s %s:<=%g | stats count() as n`, eventName, userClause, tw.Clause, field, le)
			r, err := client.StatsQuery(ctx, cq, time.Time{})
			hits <- bucketHit{LE: &leLocal, N: int64(firstValue(r)), err: err}
		}(le)
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		cq := fmt.Sprintf(`event_name:"%s"%s %s | stats count() as n`, eventName, userClause, tw.Clause)
		r, err := client.StatsQuery(ctx, cq, time.Time{})
		hits <- bucketHit{LE: nil, N: int64(firstValue(r)), err: err}
	}()
	wg.Wait()
	close(hits)

	totalN := int64(0)
	cumulative := map[float64]int64{}
	for h := range hits {
		if h.err != nil {
			return nil, h.err
		}
		if h.LE == nil {
			totalN = h.N
			continue
		}
		cumulative[*h.LE] = h.N
	}

	// Convert cumulative counts to per-bucket counts.
	resultBuckets := make([]durationBucket, 0, len(buckets)+1)
	prev := int64(0)
	for _, le := range buckets {
		c := cumulative[le]
		n := c - prev
		if n < 0 {
			n = 0
		}
		leCopy := le
		resultBuckets = append(resultBuckets, durationBucket{LE: &leCopy, N: n})
		prev = c
	}
	resultBuckets = append(resultBuckets, durationBucket{LE: nil, N: int64(math.Max(0, float64(totalN-prev)))})

	return telemetryDurationsData{
		Metric:  metric,
		Buckets: resultBuckets,
		P50:     labeledValue(pctRes, "p50"),
		P90:     labeledValue(pctRes, "p90"),
		P99:     labeledValue(pctRes, "p99"),
	}, nil
}

// telemetryLogsData is the response of GET /api/v1/telemetry/logs.
type telemetryLogsData struct {
	Items []map[string]any `json:"items"`
}

// GetTelemetryLogs returns the most recent raw telemetry records matching the
// filters, used by the UI's log viewer.
func (a *Handler) GetTelemetryLogs(r *http.Request) (interface{}, error) {
	q := r.URL.Query()
	tw, err := resolveTimeWindow(q)
	if err != nil {
		return nil, err
	}

	event := q.Get("event")
	if event != "" && event != "create" && event != "delete" {
		return nil, fmt.Errorf("invalid event %q", event)
	}

	limit := defaultLogsLimit
	if s := q.Get("limit"); s != "" {
		n, err := strconv.Atoi(s)
		if err != nil || n <= 0 {
			return nil, fmt.Errorf("invalid limit %q", s)
		}
		if n > maxLogsLimit {
			n = maxLogsLimit
		}
		limit = n
	}

	userFilter := telemetry.EscapeLogsQL(q.Get("user_key"))

	// Build the LogsQL filter. We restrict to known event names so the viewer
	// never accidentally surfaces unrelated VictoriaLogs records.
	var eventClause string
	if event == "" {
		eventClause = `(event_name:"sandbox.create" OR event_name:"sandbox.delete")`
	} else {
		eventClause = fmt.Sprintf(`event_name:"sandbox.%s"`, event)
	}
	userClause := ""
	if userFilter != "" {
		userClause = fmt.Sprintf(` user_key:"%s"`, userFilter)
	}

	logsql := fmt.Sprintf(`%s%s %s | sort by (_time desc) | limit %d`, eventClause, userClause, tw.Clause, limit)

	client, err := newQueryClient()
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(r.Context(), queryTimeoutSecs*time.Second)
	defer cancel()

	items, err := client.LogQuery(ctx, logsql, limit)
	if err != nil {
		return nil, err
	}
	return telemetryLogsData{Items: items}, nil
}

// --- helpers ---

func newQueryClient() (*telemetry.QueryClient, error) {
	endpoint := strings.TrimSpace(config.Cfg.Telemetry.QueryEndpoint())
	if endpoint == "" {
		return nil, fmt.Errorf("telemetry OTLP endpoint not configured")
	}
	return telemetry.NewQueryClient(endpoint), nil
}

func isAllowedGroupBy(s string) bool {
	switch s {
	case "template", "reason", "success", "from_pool", "auto_pause", "user_key":
		return true
	}
	return false
}

func firstValue(rs []telemetry.ScalarResult) float64 {
	if len(rs) == 0 {
		return 0
	}
	return rs[0].Value
}

// labeledValue looks up a per-stat label among scalar rows. LogsQL's stats
// quantile returns one row whose labels carry the alias names, so we need to
// scan all rows looking for the requested alias as a label key.
func labeledValue(rs []telemetry.ScalarResult, alias string) float64 {
	for _, r := range rs {
		if _, ok := r.Labels["__name__"]; ok {
			if r.Labels["__name__"] == alias {
				return r.Value
			}
		}
		if v, ok := r.Labels[alias]; ok {
			if f, err := strconv.ParseFloat(v, 64); err == nil {
				return f
			}
		}
	}
	// Fallback: single-row queries with a single aliased value land here.
	if len(rs) == 1 {
		return rs[0].Value
	}
	return 0
}

func formatDuration(d time.Duration) string {
	if d%(24*time.Hour) == 0 {
		return fmt.Sprintf("%dd", int(d/(24*time.Hour)))
	}
	if d%time.Hour == 0 {
		return fmt.Sprintf("%dh", int(d/time.Hour))
	}
	return d.String()
}
