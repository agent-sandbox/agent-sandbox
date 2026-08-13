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

package e2b

import (
	"net/url"
	"strconv"
	"strings"

	"github.com/agent-sandbox/agent-sandbox/pkg/api/e2b/api"
)

// GetV2SandboxesParams is generated from the e2b OpenAPI description and declares
// metadata, state, nextToken and limit on GET /v2/sandboxes. Nothing bound them to
// the request, so every query was answered with the caller's full sandbox list.
//
// These helpers parse and apply exactly those four, matching the contract the
// generated type documents.

// parseListParams reads the declared query parameters off the request.
//
// state is an array in the OpenAPI description. Both encodings real clients emit
// are accepted: repeated (state=running&state=paused) and comma-separated
// (state=running,paused).
//
// An unparseable limit or an unknown state is reported rather than ignored: the
// contract says these are typed, and silently discarding a filter the caller asked
// for is how an unfiltered response gets mistaken for an empty one.
func parseListParams(q url.Values) (*api.GetV2SandboxesParams, string) {
	p := &api.GetV2SandboxesParams{}

	if raw, ok := q["metadata"]; ok && len(raw) > 0 && raw[0] != "" {
		v := raw[0]
		p.Metadata = &v
	}

	if raw, ok := q["state"]; ok {
		var states []api.SandboxState
		for _, item := range raw {
			for _, part := range strings.Split(item, ",") {
				part = strings.TrimSpace(part)
				if part == "" {
					continue
				}
				st := api.SandboxState(part)
				if !st.Valid() {
					return nil, "invalid state: " + part
				}
				states = append(states, st)
			}
		}
		if len(states) > 0 {
			p.State = &states
		}
	}

	if raw := strings.TrimSpace(q.Get("limit")); raw != "" {
		n, err := strconv.ParseInt(raw, 10, 32)
		if err != nil || n < 1 {
			return nil, "invalid limit: " + raw
		}
		limit := api.PaginationLimit(n)
		p.Limit = &limit
	}

	if raw := strings.TrimSpace(q.Get("nextToken")); raw != "" {
		tok := api.PaginationNextToken(raw)
		p.NextToken = &tok
	}

	return p, ""
}

// parseMetadataFilter decodes the metadata query into key/value pairs.
//
// The generated type documents the format as `user=abc&app=prod` with each key and
// value URL encoded, so the value arrives double-encoded: once for the pair and once
// for the surrounding query string. net/url has already undone the outer layer by the
// time this sees it, so exactly one decode remains.
//
// A malformed filter returns ok=false and the caller answers 400. It must never
// degrade to "no filter" - a caller asking for a workspace that does not exist has to
// get an empty list, never somebody else's sandboxes.
func parseMetadataFilter(raw string) (map[string]string, bool) {
	if raw == "" {
		return nil, true
	}
	out := map[string]string{}
	for _, pair := range strings.Split(raw, "&") {
		if pair == "" {
			continue
		}
		k, v, found := strings.Cut(pair, "=")
		if !found || k == "" {
			return nil, false
		}
		dk, err := url.QueryUnescape(k)
		if err != nil {
			return nil, false
		}
		dv, err := url.QueryUnescape(v)
		if err != nil {
			return nil, false
		}
		out[dk] = dv
	}
	return out, true
}

// metadataMatches reports whether every requested pair is present on the sandbox.
// Subset semantics, matching e2b: a sandbox carrying extra keys still matches.
func metadataMatches(sbxMeta map[string]string, want map[string]string) bool {
	for k, v := range want {
		got, ok := sbxMeta[k]
		if !ok || got != v {
			return false
		}
	}
	return true
}

// stateMatches reports whether the sandbox is in one of the requested states.
func stateMatches(state api.SandboxState, want []api.SandboxState) bool {
	for _, w := range want {
		if state == w {
			return true
		}
	}
	return false
}

// applyListFilters filters, then pages. Order matters: limit has to apply to what
// survived filtering, or a limit could hide the very rows a filter selected.
//
// nextToken is a plain index cursor here. It is opaque to clients by contract, and
// the list is already sorted CreatedAt-descending by the controller, so an index is
// stable enough for paging a single caller's sandboxes. Returns the page and the
// token for the next one ("" when the page is the last).
func applyListFilters(in []*api.Sandbox, p *api.GetV2SandboxesParams, meta map[string]string) ([]*api.Sandbox, string) {
	out := make([]*api.Sandbox, 0, len(in))
	for _, sbx := range in {
		if len(meta) > 0 && !metadataMatches(sbx.Metadata, meta) {
			continue
		}
		if p.State != nil && !stateMatches(sbx.State, *p.State) {
			continue
		}
		out = append(out, sbx)
	}

	start := 0
	if p.NextToken != nil {
		if n, err := strconv.Atoi(string(*p.NextToken)); err == nil && n > 0 {
			start = n
		}
	}
	if start >= len(out) {
		return []*api.Sandbox{}, ""
	}
	out = out[start:]

	if p.Limit != nil && int(*p.Limit) < len(out) {
		next := strconv.Itoa(start + int(*p.Limit))
		return out[:*p.Limit], next
	}
	return out, ""
}
