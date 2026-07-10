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
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/agent-sandbox/agent-sandbox/pkg/api/e2b"
	"github.com/agent-sandbox/agent-sandbox/pkg/auth"
	"github.com/agent-sandbox/agent-sandbox/pkg/config"
	"k8s.io/klog/v2"
)

// LoggingMiddleware logs the request details and execution time
func LoggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/healthz" {
			next.ServeHTTP(w, r)
			return
		}

		start := time.Now()

		scheme := "http"
		if r.TLS != nil {
			scheme = "https"
		}

		klog.V(1).Infof("Started request: %s %s:%s%s, request headers %v", r.Method, scheme, r.Host, r.URL, r.Header)

		next.ServeHTTP(w, r)

		klog.V(1).Infof("Finished request: %s %s://%s%s, Time taken: %v, response header: %v", r.Method, scheme, r.Host, r.URL, time.Since(start), w.Header())
	})
}

func ApiKeyAuthMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !shouldProtectAPIRequest(r) {
			next.ServeHTTP(w, r)
			return
		}

		token, ok := auth.ValidateRequestToken(r)
		if !ok {
			writeUnauthorized(w)
			return
		}

		r = r.WithContext(context.WithValue(r.Context(), "user", token))

		next.ServeHTTP(w, r)
	})
}

func shouldProtectAPIRequest(r *http.Request) bool {
	if config.Cfg == nil {
		return false
	}
	// /api/v1/status is public — it serves the Dashboard's aggregate counters
	// (sandbox total, user total, capacity), with user keys masked.
	if r.URL.Path == config.Cfg.APIBaseURL+"/status" {
		return false
	}
	return strings.HasPrefix(r.URL.Path, config.Cfg.APIBaseURL+"/") || strings.HasPrefix(r.URL.Path, e2b.BaseURL+"/")
}

func writeUnauthorized(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnauthorized)
	_ = json.NewEncoder(w).Encode(&response{
		Code:  "401",
		Error: "Unauthorized",
	})
}
