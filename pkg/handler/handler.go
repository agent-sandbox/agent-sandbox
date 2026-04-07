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
	"strings"

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

	// Subdomain proxy: intercept {name}.{SandboxProxyDomain} before the mux
	var finalHandler http.Handler = loggedMux
	if config.Cfg.SandboxProxyDomain != "" {
		srHandler := router.NewSandboxRouter(rootCtx, a)
		finalHandler = subdomainMiddleware(srHandler, loggedMux)
		klog.Info("Subdomain proxy enabled for domain: ", config.Cfg.SandboxProxyDomain)
	}

	server := &http.Server{
		Addr:    config.Cfg.ServerAddr,
		Handler: finalHandler,
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
	ahh.mux.HandleFunc(fmt.Sprintf("GET %s/sandbox/{name}", config.Cfg.APIBaseURL), func(w http.ResponseWriter, r *http.Request) { wrapperHandler(w, r, sbHeader.GetSandbox) })
	ahh.mux.HandleFunc(fmt.Sprintf("POST %s/sandbox/metrics", config.Cfg.APIBaseURL), func(w http.ResponseWriter, r *http.Request) { wrapperHandler(w, r, sbHeader.SandboxMetrics) })

	ahh.mux.HandleFunc(fmt.Sprintf("GET %s/logs/sandbox/{name}", config.Cfg.APIBaseURL), func(w http.ResponseWriter, r *http.Request) { wrapperHandler(w, r, sbHeader.GetSandboxLogs) })
	ahh.mux.HandleFunc(fmt.Sprintf("GET %s/events", config.Cfg.APIBaseURL), func(w http.ResponseWriter, r *http.Request) { wrapperHandler(w, r, sbHeader.ListSandboxEvents) })

	ahh.mux.HandleFunc(fmt.Sprintf("POST %s/terminal/sandbox/{name}", config.Cfg.APIBaseURL), func(w http.ResponseWriter, r *http.Request) { wrapperHandler(w, r, sbHeader.ExecuteSandboxTerminal) })
	ahh.mux.HandleFunc(fmt.Sprintf("GET %s/terminal/sandbox/{name}/ws", config.Cfg.APIBaseURL), sbHeader.StreamSandboxTerminalWS)
	ahh.mux.HandleFunc(fmt.Sprintf("GET %s/terminal/sandbox/{name}/detect-shell", config.Cfg.APIBaseURL), func(w http.ResponseWriter, r *http.Request) { wrapperHandler(w, r, sbHeader.DetectSandboxShell) })
	ahh.mux.HandleFunc(fmt.Sprintf("GET %s/traffic/sandbox/{name}/ws", config.Cfg.APIBaseURL), sbHeader.StreamSandboxTrafficWS)

	ahh.mux.HandleFunc(fmt.Sprintf("GET %s/sandbox/files/{name}", config.Cfg.APIBaseURL), func(w http.ResponseWriter, r *http.Request) { wrapperHandler(w, r, sbHeader.ListSandboxFiles) })
	ahh.mux.HandleFunc(fmt.Sprintf("POST %s/sandbox/files/{name}/upload", config.Cfg.APIBaseURL), func(w http.ResponseWriter, r *http.Request) { wrapperHandler(w, r, sbHeader.UploadSandboxFile) })
	ahh.mux.HandleFunc(fmt.Sprintf("DELETE %s/sandbox/files/{name}", config.Cfg.APIBaseURL), func(w http.ResponseWriter, r *http.Request) { wrapperHandler(w, r, sbHeader.DeleteSandboxFile) })
	ahh.mux.HandleFunc(fmt.Sprintf("GET %s/sandbox/files/{name}/download", config.Cfg.APIBaseURL), sbHeader.DownloadSandboxFile)

	// Rest API for config
	ahh.mux.HandleFunc(fmt.Sprintf("GET %s/config/templates", config.Cfg.APIBaseURL), func(w http.ResponseWriter, r *http.Request) { wrapperHandler(w, r, sbHeader.GetTemplatesConfig) })
	ahh.mux.HandleFunc(fmt.Sprintf("POST %s/config/templates", config.Cfg.APIBaseURL), func(w http.ResponseWriter, r *http.Request) { wrapperHandler(w, r, sbHeader.SaveTemplatesConfig) })
	ahh.mux.HandleFunc(fmt.Sprintf("GET %s/config/sandbox-template", config.Cfg.APIBaseURL), func(w http.ResponseWriter, r *http.Request) { wrapperHandler(w, r, sbHeader.GetSandboxTemplateConfig) })
	ahh.mux.HandleFunc(fmt.Sprintf("POST %s/config/sandbox-template", config.Cfg.APIBaseURL), func(w http.ResponseWriter, r *http.Request) { wrapperHandler(w, r, sbHeader.SaveSandboxTemplateConfig) })

	// Rest API for pool management
	ahh.mux.HandleFunc(fmt.Sprintf("GET %s/pool", config.Cfg.APIBaseURL), func(w http.ResponseWriter, r *http.Request) { wrapperHandler(w, r, sbHeader.ListPool) })
	ahh.mux.HandleFunc(fmt.Sprintf("GET %s/pool/sandbox/{name}", config.Cfg.APIBaseURL), func(w http.ResponseWriter, r *http.Request) { wrapperHandler(w, r, sbHeader.ListPoolSandbox) })
	ahh.mux.HandleFunc(fmt.Sprintf("DELETE %s/pool/{name}", config.Cfg.APIBaseURL), func(w http.ResponseWriter, r *http.Request) { wrapperHandler(w, r, sbHeader.DeletePoo) })

	// e2b API
	e2bHeader := e2bapi.NewHandler(ahh.rootCtx, c, a)
	e2bHeader.RegisterHandlersWithOptions(ahh.mux)

	// SandboxHandler router, route calls to Sandbox container
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
	ahh.mux.HandleFunc("/", e2bHeader.SandboxRouterNative())
}

// subdomainMiddleware intercepts requests whose Host header matches
// {name}.{SandboxProxyDomain} and routes them to the sandbox pod directly,
// without path stripping. All other requests fall through to next.
func subdomainMiddleware(sr *router.SandboxRouter, next http.Handler) http.Handler {
	suffix := "." + config.Cfg.SandboxProxyDomain
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host := r.Host
		if idx := strings.LastIndex(host, ":"); idx != -1 {
			host = host[:idx]
		}
		if strings.HasSuffix(host, suffix) {
			sr.SubdomainServeHTTP(w, r)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func wrapperHandler(w http.ResponseWriter, r *http.Request, f func(*http.Request) (interface{}, error)) {
	result, err := f(r)
	if err != nil {
		Err(w, err.Error())
		return
	}
	Ok(w, result)
	return
}
