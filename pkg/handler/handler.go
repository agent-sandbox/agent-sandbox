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
    "time"

    "github.com/agent-sandbox/agent-sandbox/pkg/config"
    "github.com/agent-sandbox/agent-sandbox/pkg/router"
    "github.com/agent-sandbox/agent-sandbox/pkg/sandbox"
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
    sbHeader := sandbox.NewHandler(a.rootCtx)
    a.mux.HandleFunc(fmt.Sprintf("POST %s/sandbox", config.Cfg.APIBaseURL), func(w http.ResponseWriter, r *http.Request) { wrapperHandler(w, r, sbHeader.CreateSandbox) })
    a.mux.HandleFunc(fmt.Sprintf("GET %s/sandbox", config.Cfg.APIBaseURL), func(w http.ResponseWriter, r *http.Request) { wrapperHandler(w, r, sbHeader.ListSandbox) })
    a.mux.HandleFunc(fmt.Sprintf("DELETE %s/sandbox/{name}", config.Cfg.APIBaseURL), func(w http.ResponseWriter, r *http.Request) { wrapperHandler(w, r, sbHeader.DelSandbox) })
    a.mux.HandleFunc(fmt.Sprintf("GET %s/sandbox/{name}", config.Cfg.APIBaseURL), func(w http.ResponseWriter, r *http.Request) { wrapperHandler(w, r, sbHeader.GetSandbox) })

    // SandboxHandler router, route calls to Sandbox container
    srHandler := router.NewSandboxRouter(a.rootCtx)
    a.mux.HandleFunc("/sandbox/{name}/*", srHandler.ServeHTTP)

    a.mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
        fmt.Fprintf(w, "OK")
        return
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
