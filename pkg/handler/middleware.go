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
	"net/url"
	"strings"
	"time"

	e2bapi "github.com/agent-sandbox/agent-sandbox/pkg/api/e2b"
	"github.com/agent-sandbox/agent-sandbox/pkg/auth"
	"github.com/agent-sandbox/agent-sandbox/pkg/config"
	"k8s.io/klog/v2"
)

// credentialHeaders carry secrets and are never written to the request log.
var credentialHeaders = []string{
	"Authorization",
	"Proxy-Authorization",
	"Cookie",
	"Set-Cookie",
	"X-Api-Key",
	"X-Access-Token",
}

// redactHeaders returns a copy of h for logging, with credential values replaced.
// The original header is not modified.
func redactHeaders(h http.Header) http.Header {
	out := h.Clone()
	for _, name := range credentialHeaders {
		if _, ok := out[name]; ok {
			out[name] = []string{config.Redacted}
		}
	}
	return out
}

// credentialQueryParams carry secrets in the URL and are never written to the request log.
// The UI terminal WebSocket sends api_key this way because browsers can't set WS headers.
var credentialQueryParams = []string{"api_key"}

// redactURL returns u as a string for logging, with credential query values replaced.
// The original URL is not modified.
func redactURL(u *url.URL) string {
	q := u.Query()
	redacted := false
	for _, name := range credentialQueryParams {
		if q.Has(name) {
			q.Set(name, config.Redacted)
			redacted = true
		}
	}
	if !redacted {
		return u.String()
	}
	out := *u
	// keep the marker readable instead of %5BREDACTED%5D
	out.RawQuery = strings.ReplaceAll(q.Encode(), url.QueryEscape(config.Redacted), config.Redacted)
	return out.String()
}

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

		klog.V(1).Infof("Started request: %s %s:%s%s, request headers %v", r.Method, scheme, r.Host, redactURL(r.URL), redactHeaders(r.Header))

		next.ServeHTTP(w, r)

		klog.V(1).Infof("Finished request: %s %s://%s%s, Time taken: %v, response header: %v", r.Method, scheme, r.Host, redactURL(r.URL), time.Since(start), redactHeaders(w.Header()))
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
	path := r.URL.Path

	// /sandbox/{name} is not protected by API key, authorization is handled by the sandbox itself

	return strings.HasPrefix(path, "/mcp") ||
		strings.HasPrefix(path, config.Cfg.APIBaseURL+"/") ||
		strings.HasPrefix(path, e2bapi.BaseURL+"/")
}

func writeUnauthorized(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnauthorized)
	_ = json.NewEncoder(w).Encode(&response{
		Code:  "401",
		Error: "Unauthorized",
	})
}
