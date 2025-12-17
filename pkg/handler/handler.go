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
	"fmt"
	"net/http"
	"time"

	"github.com/agent-sandbox/agent-sandbox/pkg/config"
	"github.com/agent-sandbox/agent-sandbox/pkg/net"
	"k8s.io/klog/v2"
)

type ApiHttpHandler struct {
	mux     *http.ServeMux
	rootCtx context.Context
}

func New(rootCtx context.Context) *http.Server {
	mux := http.NewServeMux()

	ah := &ApiHttpHandler{
		rootCtx: rootCtx,
		mux:     mux,
	}
	ah.regHandlers()

	server := &http.Server{
		Addr:         config.Cfg.ServerAddr,
		Handler:      mux,
		ReadTimeout:  5 * time.Minute,
		WriteTimeout: 30 * time.Second,
	}

	klog.Info("Api server ", "addr=", config.Cfg.ServerAddr)
	return server
}

func (a *ApiHttpHandler) regHandlers() {
	// Rest API for Sandbox management
	a.mux.HandleFunc(fmt.Sprintf("POST %s/sandboxes", config.Cfg.APIBaseURL), a.createSandbox)
	a.mux.HandleFunc(fmt.Sprintf("GET %s/sandboxes", config.Cfg.APIBaseURL), a.listSandboxes)
	a.mux.HandleFunc(fmt.Sprintf("DELETE %s/sandboxes/{name}", config.Cfg.APIBaseURL), a.delSandbox)
	a.mux.HandleFunc(fmt.Sprintf("GET %s/sandboxes/{name}", config.Cfg.APIBaseURL), a.getSandbox)

	// SandboxHandler router, route calls to Sandbox container
	sr := net.NewSandboxRouter(a.rootCtx)
	a.mux.HandleFunc("/sandbox/{name}/*", sr.ServeHTTP)

	a.mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "OK")
		return
	})
}

func (a *ApiHttpHandler) createSandbox(w http.ResponseWriter, r *http.Request) {
	var sb = DefaultSandbox
	err := json.NewDecoder(r.Body).Decode(sb)
	if err != nil {
		Err(w, "Invalid JSON format, err "+err.Error())
		return
	}
	sb.Make()

	klog.V(2).Infof("Create sandbox opts %v", sb)

	sbHandler := NewSandboxHandler(a.rootCtx, sb)
	exist := sbHandler.Get()
	if exist != nil {
		Err(w, fmt.Sprintf("Sandbox %s already exists", sb.Name))
		return
	}

	err = sbHandler.Create()

	if err != nil {
		klog.Errorf("Failed to create sandbox, err: %v", err)
		Err(w, "Failed to create sandbox, err "+err.Error())
		return
	}

	Ok(w, fmt.Sprintf("Sandbox %s created successfully", sb.Name))
	return
}

func (a *ApiHttpHandler) listSandboxes(w http.ResponseWriter, r *http.Request) {
	sbHandler := NewSandboxHandler(a.rootCtx, nil)
	sbs := sbHandler.List()
	if sbs == nil {
		Err(w, "No sandboxes found")
		return
	}

	Ok(w, sbs)
	return
}

func (a *ApiHttpHandler) getSandbox(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	klog.V(2).Infof("Delete sandbox name=%s", name)
	sb := &Sandbox{
		Name: name,
	}

	sbHandler := NewSandboxHandler(a.rootCtx, sb)
	sboGet := sbHandler.Get()
	if sb == nil {
		Err(w, fmt.Sprintf("Sandbox %s not found", name))
		return
	}

	Ok(w, sboGet)
	return
}

func (a *ApiHttpHandler) delSandbox(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	klog.V(2).Infof("Delete sandbox name=%s", name)
	sb := &Sandbox{
		Name: name,
	}

	sbHandler := NewSandboxHandler(a.rootCtx, sb)
	err := sbHandler.Delete()
	if err != nil {
		Err(w, "Failed to delete sandbox, err "+err.Error())
		return
	}

	Ok(w, fmt.Sprintf("Sandbox %s deleted successfully", name))
	return
}
