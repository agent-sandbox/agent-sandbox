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
	"net/http"

	"github.com/agent-sandbox/agent-sandbox/pkg/activator"
	e2bapi "github.com/agent-sandbox/agent-sandbox/pkg/api/e2b"
	"github.com/agent-sandbox/agent-sandbox/pkg/config"
	"github.com/agent-sandbox/agent-sandbox/pkg/router"
	"github.com/agent-sandbox/agent-sandbox/pkg/sandbox"
	"k8s.io/klog/v2"
)

type ApiHttpHandler struct {
	mux        *http.ServeMux
	rootCtx    context.Context
	activator  *activator.Activator
	controller *sandbox.Controller
}

func New(rootCtx context.Context, a *activator.Activator, c *sandbox.Controller) *http.Server {
	mux := http.DefaultServeMux

	ah := &ApiHttpHandler{
		rootCtx:    rootCtx,
		mux:        mux,
		activator:  a,
		controller: c,
	}
	ah.regHandlers()

	// Wrap mux with api key auth middleware and global logging middleware
	authMux := ApiKeyAuthMiddleware(mux)
	loggedMux := LoggingMiddleware(authMux)

	server := &http.Server{
		Addr:    config.Cfg.ServerAddr,
		Handler: loggedMux,
	}

	klog.Info("Api server ", "addr=", config.Cfg.ServerAddr)
	return server
}

func (ahh *ApiHttpHandler) regHandlers() {
	a := ahh.activator
	c := ahh.controller

	// Rest API for Sandbox management
	sbHeader := NewHandler(ahh.rootCtx, c, a)
	ahh.mux.HandleFunc(fmt.Sprintf("POST %s/sandbox", config.Cfg.APIBaseURL), func(w http.ResponseWriter, r *http.Request) { wrapperHandler(w, r, sbHeader.CreateSandbox) })
	ahh.mux.HandleFunc(fmt.Sprintf("GET %s/sandbox", config.Cfg.APIBaseURL), func(w http.ResponseWriter, r *http.Request) { wrapperHandler(w, r, sbHeader.ListSandbox) })
	ahh.mux.HandleFunc(fmt.Sprintf("DELETE %s/sandbox/{name}", config.Cfg.APIBaseURL), func(w http.ResponseWriter, r *http.Request) { wrapperHandler(w, r, sbHeader.DelSandbox) })
	ahh.mux.HandleFunc(fmt.Sprintf("POST %s/sandbox/pause/{name}", config.Cfg.APIBaseURL), func(w http.ResponseWriter, r *http.Request) { wrapperHandler(w, r, sbHeader.PauseSandbox) })
	ahh.mux.HandleFunc(fmt.Sprintf("POST %s/sandbox/resume/{name}", config.Cfg.APIBaseURL), func(w http.ResponseWriter, r *http.Request) { wrapperHandler(w, r, sbHeader.ResumeSandbox) })
	ahh.mux.HandleFunc(fmt.Sprintf("GET %s/sandbox/{name}", config.Cfg.APIBaseURL), func(w http.ResponseWriter, r *http.Request) { wrapperHandler(w, r, sbHeader.GetSandbox) })
	ahh.mux.HandleFunc(fmt.Sprintf("POST %s/sandbox/metrics", config.Cfg.APIBaseURL), func(w http.ResponseWriter, r *http.Request) { wrapperHandler(w, r, sbHeader.SandboxMetrics) })
	ahh.mux.HandleFunc(fmt.Sprintf("GET %s/ratelimit", config.Cfg.APIBaseURL), func(w http.ResponseWriter, r *http.Request) { wrapperHandler(w, r, sbHeader.GetRateLimitStatus) })
	ahh.mux.HandleFunc(fmt.Sprintf("GET %s/status", config.Cfg.APIBaseURL), func(w http.ResponseWriter, r *http.Request) { wrapperHandler(w, r, sbHeader.GetDashboardStatus) })

	// Telemetry dashboard
	ahh.mux.HandleFunc(fmt.Sprintf("GET %s/telemetry/status", config.Cfg.APIBaseURL), func(w http.ResponseWriter, r *http.Request) { wrapperHandler(w, r, sbHeader.GetTelemetryStatus) })
	ahh.mux.HandleFunc(fmt.Sprintf("GET %s/telemetry/summary", config.Cfg.APIBaseURL), func(w http.ResponseWriter, r *http.Request) { wrapperHandler(w, r, sbHeader.GetTelemetrySummary) })
	ahh.mux.HandleFunc(fmt.Sprintf("GET %s/telemetry/timeseries", config.Cfg.APIBaseURL), func(w http.ResponseWriter, r *http.Request) { wrapperHandler(w, r, sbHeader.GetTelemetryTimeseries) })
	ahh.mux.HandleFunc(fmt.Sprintf("GET %s/telemetry/by_user", config.Cfg.APIBaseURL), func(w http.ResponseWriter, r *http.Request) { wrapperHandler(w, r, sbHeader.GetTelemetryByUser) })
	ahh.mux.HandleFunc(fmt.Sprintf("GET %s/telemetry/durations", config.Cfg.APIBaseURL), func(w http.ResponseWriter, r *http.Request) { wrapperHandler(w, r, sbHeader.GetTelemetryDurations) })
	ahh.mux.HandleFunc(fmt.Sprintf("GET %s/telemetry/logs", config.Cfg.APIBaseURL), func(w http.ResponseWriter, r *http.Request) { wrapperHandler(w, r, sbHeader.GetTelemetryLogs) })

	ahh.mux.HandleFunc(fmt.Sprintf("GET %s/logs/sandbox/{name}", config.Cfg.APIBaseURL), func(w http.ResponseWriter, r *http.Request) { wrapperHandler(w, r, sbHeader.GetSandboxLogs) })
	ahh.mux.HandleFunc(fmt.Sprintf("GET %s/events", config.Cfg.APIBaseURL), func(w http.ResponseWriter, r *http.Request) { wrapperHandler(w, r, sbHeader.ListSandboxEvents) })

	ahh.mux.HandleFunc(fmt.Sprintf("POST %s/terminal/sandbox/{name}", config.Cfg.APIBaseURL), func(w http.ResponseWriter, r *http.Request) { wrapperHandler(w, r, sbHeader.ExecuteSandboxTerminal) })
	ahh.mux.HandleFunc(fmt.Sprintf("GET %s/terminal/sandbox/{name}/ws", config.Cfg.APIBaseURL), sbHeader.StreamSandboxTerminalWS)

	ahh.mux.HandleFunc(fmt.Sprintf("GET %s/sandbox/files/{name}", config.Cfg.APIBaseURL), func(w http.ResponseWriter, r *http.Request) { wrapperHandler(w, r, sbHeader.ListSandboxFiles) })
	ahh.mux.HandleFunc(fmt.Sprintf("POST %s/sandbox/files/{name}/upload", config.Cfg.APIBaseURL), func(w http.ResponseWriter, r *http.Request) { wrapperHandler(w, r, sbHeader.UploadSandboxFile) })
	ahh.mux.HandleFunc(fmt.Sprintf("DELETE %s/sandbox/files/{name}", config.Cfg.APIBaseURL), func(w http.ResponseWriter, r *http.Request) { wrapperHandler(w, r, sbHeader.DeleteSandboxFile) })
	ahh.mux.HandleFunc(fmt.Sprintf("GET %s/sandbox/files/{name}/download", config.Cfg.APIBaseURL), sbHeader.DownloadSandboxFile)

	// Rest API for config
	ahh.mux.HandleFunc(fmt.Sprintf("GET %s/config/runtime", config.Cfg.APIBaseURL), func(w http.ResponseWriter, r *http.Request) { wrapperHandler(w, r, sbHeader.GetRuntimeConfig) })
	ahh.mux.HandleFunc(fmt.Sprintf("POST %s/config/runtime", config.Cfg.APIBaseURL), func(w http.ResponseWriter, r *http.Request) { wrapperHandler(w, r, sbHeader.SaveRuntimeConfig) })
	ahh.mux.HandleFunc(fmt.Sprintf("GET %s/config/templates", config.Cfg.APIBaseURL), func(w http.ResponseWriter, r *http.Request) { wrapperHandler(w, r, sbHeader.GetTemplatesConfig) })
	ahh.mux.HandleFunc(fmt.Sprintf("POST %s/config/templates", config.Cfg.APIBaseURL), func(w http.ResponseWriter, r *http.Request) { wrapperHandler(w, r, sbHeader.SaveTemplatesConfig) })
	ahh.mux.HandleFunc(fmt.Sprintf("GET %s/config/blueprint", config.Cfg.APIBaseURL), func(w http.ResponseWriter, r *http.Request) { wrapperHandler(w, r, sbHeader.GetSandboxBlueprintConfig) })
	ahh.mux.HandleFunc(fmt.Sprintf("POST %s/config/blueprint", config.Cfg.APIBaseURL), func(w http.ResponseWriter, r *http.Request) { wrapperHandler(w, r, sbHeader.SaveSandboxBlueprintConfig) })

	// Rest API for pool management
	ahh.mux.HandleFunc(fmt.Sprintf("GET %s/pool", config.Cfg.APIBaseURL), func(w http.ResponseWriter, r *http.Request) { wrapperHandler(w, r, sbHeader.ListPool) })
	ahh.mux.HandleFunc(fmt.Sprintf("GET %s/pool/sandbox/{name}", config.Cfg.APIBaseURL), func(w http.ResponseWriter, r *http.Request) { wrapperHandler(w, r, sbHeader.ListPoolSandbox) })
	ahh.mux.HandleFunc(fmt.Sprintf("DELETE %s/pool/{name}", config.Cfg.APIBaseURL), func(w http.ResponseWriter, r *http.Request) { wrapperHandler(w, r, sbHeader.DeletePoolSandboxes) })

	// e2b API
	e2bHeader := e2bapi.NewHandler(ahh.rootCtx, c, a)
	e2bHeader.RegisterHandlersWithOptions(ahh.mux)

	// SandboxHandler router, route calls to Sandbox container(proxy)
	srHandler := router.NewSandboxRouter(ahh.rootCtx, a)
	ahh.mux.HandleFunc("/sandbox/{name}/", srHandler.ServeHTTP)

	ahh.mux.Handle("/mcp", sbHeader.McpSseHandler())

	ahh.mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		// return the json of status and version
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"status":"ok","version":"%s"}`, config.Version)
		return
	})

	// ui router for static files
	uiDistFS := http.FileServer(http.Dir("ui/dist"))
	ahh.mux.Handle("/ui/", http.StripPrefix("/ui/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		uiDistFS.ServeHTTP(w, r)
	})))

	// e2b sandbox execute endpoint
	ahh.mux.HandleFunc("/sandboxes/router/{sandboxID}/{port}/", e2bHeader.SandboxRouterOfPath())
	// catch-all handler for any unmatched requests, mainly for E2B sandbox proxy purpose
	ahh.mux.HandleFunc("/", e2bHeader.SandboxRouterOfDomain())
}

func wrapperHandler(w http.ResponseWriter, r *http.Request, f func(*http.Request) (interface{}, error)) {
	result, err := f(r)
	if err != nil {
		if httpErr, ok := err.(*HTTPError); ok {
			ErrWithCode(w, httpErr.Code, httpErr.Message)
			return
		}
		Err(w, err.Error())
		return
	}
	Ok(w, result)
	return
}
