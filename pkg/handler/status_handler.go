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
	"net/http"
	"sort"

	"github.com/agent-sandbox/agent-sandbox/pkg/capacity"
	"github.com/agent-sandbox/agent-sandbox/pkg/leader"
	"github.com/agent-sandbox/agent-sandbox/pkg/sandbox"
	"k8s.io/klog/v2"
)

// sandboxSummary is the aggregate sandbox view shown on the Dashboard.
type sandboxSummary struct {
	Total int `json:"total"`
}

// capacitySummary mirrors the existing AllRateLimitStatus shape but with
// user_key values masked to the leading 2/3 (matches the telemetry pipeline's
// masking) so this endpoint can stay public.
type capacitySummary struct {
	DefaultConfig RateLimitDefaultConfig `json:"default_config"`
	UserTotal     int                    `json:"user_total"`
	Users         []RateLimitStatus      `json:"users"`
}

// dashboardStatusData is the response of GET /api/v1/status. The endpoint is
// public — no auth required — and exposes only aggregate counters plus masked
// per-user capacity. The Dashboard page consumes it as a single round-trip.
type dashboardStatusData struct {
	Leader    string          `json:"leader"`
	Sandboxes sandboxSummary  `json:"sandboxes"`
	Capacity  capacitySummary `json:"capacity"`
}

// GetDashboardStatus returns the public Dashboard payload.
func (a *Handler) GetDashboardStatus(r *http.Request) (interface{}, error) {
	data := dashboardStatusData{
		Leader: leader.Current(),
	}

	// Sandboxes: total count only. Per-template / per-status breakdowns are
	// intentionally omitted to avoid parsing every sandbox-data annotation on
	// each refresh.
	if total, err := a.controller.CountSandboxes(""); err == nil {
		data.Sandboxes.Total = total
	} else {
		klog.Warningf("Failed to count sandboxes for /status: %v", err)
	}

	// Capacity: same structure as /ratelimit but with user keys masked.
	if capacity.GlobalLimiter != nil {
		def := capacity.GlobalLimiter.DefaultConfig()
		data.Capacity.DefaultConfig = RateLimitDefaultConfig{
			Enabled:        def.Enabled,
			MaxConcurrency: def.MaxConcurrency,
			MaxSandbox:     def.MaxSandbox,
		}

		counts, err := capacity.GlobalLimiter.CountAllByUser()
		if err != nil {
			klog.Warningf("Failed to list user sandbox counts for /status: %v", err)
			counts = map[string]int{}
		}

		userSet := map[string]struct{}{}
		for u := range counts {
			if u != "" {
				userSet[u] = struct{}{}
			}
		}
		for _, userCfg := range capacity.GlobalLimiter.UserConfigs() {
			if userCfg.User != "" {
				userSet[userCfg.User] = struct{}{}
			}
		}
		users := make([]string, 0, len(userSet))
		for u := range userSet {
			users = append(users, u)
		}
		sort.Strings(users)

		data.Capacity.UserTotal = len(users)
		for _, u := range users {
			active, maxConcurrency := capacity.GlobalLimiter.ConcurrencyStats(u)
			_, maxSandbox := capacity.GlobalLimiter.UserConfig(u)
			row := buildRateLimitStatus(u, true, active, maxConcurrency, counts[u], maxSandbox)
			row.User = sandbox.MaskUserKey(row.User)
			data.Capacity.Users = append(data.Capacity.Users, row)
		}
	}

	return data, nil
}
