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

package telemetry

import (
	"context"
	"math"
	mrand "math/rand"
	"sync"
	"sync/atomic"
	"time"

	"go.opentelemetry.io/otel/log"
)

const SchemaVersion = 1

// Reason values for sandbox.delete. Kept as constants so call sites do not
// pass arbitrary strings.
const (
	ReasonAPIRequest          = "api_request"
	ReasonE2BSDK              = "e2b_sdk"
	ReasonMCP                 = "mcp"
	ReasonTimeout             = "timeout"
	ReasonIdleTimeout         = "idle_timeout"
	ReasonPauseFailedFallback = "pause_failed_fallback"
	ReasonTemplateCleanup     = "template_cleanup"
	ReasonAdmin               = "admin"
	ReasonUnknown             = "unknown"
)

// SbxInfo describes the sandbox a TLog is about. Embedded in TLog so future
// non-lifecycle events (pause / resume / exec / file ops) can attach the same
// identity block.
type SbxInfo struct {
	UserKey     string
	SandboxID   string
	SandboxName string
	Template    string
	App         string
	FromPool    bool
	AutoPause   bool
	CreatedAt   time.Time
}

// TLog is the unified telemetry record. The body (Message) maps to
// VictoriaLogs' `_msg` column; every other field becomes a queryable attribute.
//
// Duration is the event-specific elapsed time in seconds (operation duration
// on create, sandbox lifetime on delete, etc.). Callers compute it.
type TLog struct {
	RequestID string
	LogName   string
	Success   bool

	Sbx SbxInfo

	Duration float64
	Reason   string
	Message  string
}

// EmitTLog is non-blocking. If the buffer is full, the record is dropped and
// the drop counter is incremented.
func EmitTLog(tlog TLog) {
	mu.RLock()
	on := enabled
	c := tLogCH
	rate := sampleRate
	mu.RUnlock()
	if !on || c == nil {
		return
	}
	if !sample(rate) {
		return
	}
	tlog.Sbx.UserKey = maskUser(tlog.Sbx.UserKey)
	select {
	case c <- tlog:
	default:
		dropCount.Add(1)
	}
}

var (
	mu         sync.RWMutex
	enabled    bool
	sampleRate float64
	tLogCH     chan TLog
	logger     log.Logger
	dropCount  atomic.Uint64
	drainOnce  sync.Once
	shutdownFn func(context.Context) error
	rng        = mrand.New(mrand.NewSource(time.Now().UnixNano()))
	rngMu      sync.Mutex
)

// Configure wires runtime config into the emitter. Safe to call once at startup.
// logger may be nil (events still increment counters; OTel records are dropped).
func Configure(cfg Settings, l log.Logger, shutdown func(context.Context) error) {
	mu.Lock()
	defer mu.Unlock()

	enabled = cfg.Enabled
	sampleRate = clampSample(cfg.SampleRate)
	bufSize := cfg.BufferSize
	if bufSize <= 0 {
		bufSize = 1024
	}
	tLogCH = make(chan TLog, bufSize)
	logger = l
	shutdownFn = shutdown

	drainOnce.Do(func() {
		go drainTLog()
	})
}

// Settings is the subset of config.TelemetryConfig the emitter actually reads.
// Defined locally so pkg/telemetry has no dependency on pkg/config.
type Settings struct {
	Enabled    bool
	BufferSize int
	SampleRate float64
}

// Shutdown flushes the OTel exporter. Non-blocking after timeout.
func Shutdown(ctx context.Context) error {
	mu.RLock()
	fn := shutdownFn
	mu.RUnlock()
	if fn == nil {
		return nil
	}
	return fn(ctx)
}

// DroppedCount returns how many events have been dropped because the buffer was full.
func DroppedCount() uint64 { return dropCount.Load() }

func drainTLog() {
	for ev := range tLogCH {
		mu.RLock()
		l := logger
		mu.RUnlock()
		if l == nil {
			continue
		}
		emitTLogRecord(l, ev)
	}
}

func emitTLogRecord(l log.Logger, tlog TLog) {
	var rec log.Record
	rec.SetEventName(tlog.LogName)
	rec.SetTimestamp(time.Now())
	rec.SetSeverity(log.SeverityInfo)
	// Body maps to VictoriaLogs' _msg column.
	if tlog.Message == "" {
		tlog.Message = tlog.LogName
	}
	rec.SetBody(log.StringValue(tlog.Message))
	rec.AddAttributes(
		log.String("request_id", tlog.RequestID),
		log.String("event_name", tlog.LogName),
		log.Bool("success", tlog.Success),
		log.String("user_key", tlog.Sbx.UserKey),
		log.String("sandbox_id", tlog.Sbx.SandboxID),
		log.String("sandbox_name", tlog.Sbx.SandboxName),
		log.String("template", tlog.Sbx.Template),
		log.String("app", tlog.Sbx.App),
		log.Bool("from_pool", tlog.Sbx.FromPool),
		log.Bool("auto_pause", tlog.Sbx.AutoPause),
		log.String("created_at", rfc3339(tlog.Sbx.CreatedAt)),
		log.Float64("duration_seconds", tlog.Duration),
		log.String("reason", tlog.Reason),
		log.Int("schema_version", SchemaVersion),
	)
	l.Emit(context.Background(), rec)
}

// maskUser keeps the leading 2/3 of the user key. This preserves enough prefix
// for grouping in dashboards while dropping the trailing identifying suffix
// (e.g. "testuser-aef134ef-..." → "testuser-aef134ef-7aa1-9").
func maskUser(user string) string {
	if user == "" {
		return ""
	}
	n := len(user) * 2 / 3
	if n < 1 {
		return user
	}
	return user[:n]
}

func sample(rate float64) bool {
	if rate >= 1.0 {
		return true
	}
	if rate <= 0 {
		return false
	}
	rngMu.Lock()
	defer rngMu.Unlock()
	return rng.Float64() < rate
}

func clampSample(r float64) float64 {
	if math.IsNaN(r) || r < 0 {
		return 0
	}
	if r > 1 {
		return 1
	}
	return r
}

func rfc3339(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}
