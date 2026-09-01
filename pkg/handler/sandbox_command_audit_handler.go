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
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/agent-sandbox/agent-sandbox/pkg/auth"
	"github.com/agent-sandbox/agent-sandbox/pkg/commandaudit"
)

type SandboxCommandAuditListResponse struct {
	Items     []commandaudit.Record `json:"items"`
	FetchedAt time.Time             `json:"fetchedAt"`
}

type SandboxCommandAuditSandboxListResponse struct {
	Items     []string  `json:"items"`
	FetchedAt time.Time `json:"fetchedAt"`
}

func (a *Handler) ListSandboxCommandAudits(r *http.Request) (interface{}, error) {
	opts, err := parseSandboxCommandAuditListOptions(r)
	if err != nil {
		return nil, err
	}

	return SandboxCommandAuditListResponse{
		Items:     commandaudit.DefaultStore.List(opts),
		FetchedAt: time.Now().UTC(),
	}, nil
}

func (a *Handler) ListSandboxCommandAuditSandboxes(r *http.Request) (interface{}, error) {
	opts, err := parseSandboxCommandAuditListOptions(r)
	if err != nil {
		return nil, err
	}

	return SandboxCommandAuditSandboxListResponse{
		Items:     commandaudit.DefaultStore.SandboxNames(opts),
		FetchedAt: time.Now().UTC(),
	}, nil
}

func parseSandboxCommandAuditListOptions(r *http.Request) (commandaudit.ListOptions, error) {
	if r == nil {
		return commandaudit.ListOptions{}, fmt.Errorf("request is required")
	}

	user := auth.GetUserTokenFromContext(r.Context())
	if user == "" {
		return commandaudit.ListOptions{}, fmt.Errorf("user not found, api key may be invalid")
	}

	query := r.URL.Query()
	opts := commandaudit.ListOptions{
		TenantID:    strings.TrimSpace(query.Get("tenant_id")),
		SandboxName: strings.TrimSpace(query.Get("sandbox")),
		Search:      strings.TrimSpace(query.Get("search")),
	}

	if !strings.HasPrefix(user, "sys-") {
		opts.TenantID = user
	}

	if rawLimit := strings.TrimSpace(query.Get("limit")); rawLimit != "" {
		limit, err := strconv.Atoi(rawLimit)
		if err != nil {
			return commandaudit.ListOptions{}, fmt.Errorf("invalid limit: %v", err)
		}
		if limit <= 0 {
			return commandaudit.ListOptions{}, fmt.Errorf("limit must be a positive integer")
		}
		opts.Limit = limit
	}

	from, err := parseSandboxCommandAuditTime(query.Get("from"))
	if err != nil {
		return commandaudit.ListOptions{}, fmt.Errorf("invalid from: %v", err)
	}
	opts.From = from

	to, err := parseSandboxCommandAuditTime(query.Get("to"))
	if err != nil {
		return commandaudit.ListOptions{}, fmt.Errorf("invalid to: %v", err)
	}
	opts.To = to

	return opts, nil
}

func parseSandboxCommandAuditTime(value string) (time.Time, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}, nil
	}

	formats := []string{
		time.RFC3339,
		"2006-01-02T15:04",
		"2006-01-02T15:04:05",
		"2006-01-02 15:04",
		"2006-01-02 15:04:05",
	}
	for _, format := range formats {
		parsed, err := time.ParseInLocation(format, value, time.Local)
		if err == nil {
			return parsed, nil
		}
	}
	return time.Time{}, fmt.Errorf("expected RFC3339 or yyyy-mm-ddThh:mm")
}
