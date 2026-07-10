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
	"fmt"
	"net/http"
	"os"

	"github.com/agent-sandbox/agent-sandbox/pkg/activator"
	"github.com/agent-sandbox/agent-sandbox/pkg/api/e2b/api"
	"github.com/agent-sandbox/agent-sandbox/pkg/sandbox"

	"context"
)

const (
	BaseURL = "/e2b/v1"
)

type Handler struct {
	rootCtx    context.Context
	controller *sandbox.Controller
	activator  *activator.Activator
	domain     string
}

func NewHandler(rootCtx context.Context, c *sandbox.Controller, a *activator.Activator) *Handler {
	domain := "localhost"
	if domainEnv := os.Getenv("E2B_DOMAIN"); domainEnv != "" {
		domain = domainEnv
	}

	return &Handler{
		rootCtx:    rootCtx,
		controller: c,
		activator:  a,
		domain:     domain,
	}
}

func GetDefaultE2BSandbox() *api.Sandbox {
	return &api.Sandbox{
		EnvdVersion:     "0.1.1",
		EnvdAccessToken: "envd-access-token-x",
		ClientID:        "client-id-x",
		TemplateID:      "code-interpreter-v1",
		Metadata:        map[string]string{},
	}
}

// RegisterHandlersWithOptions creates http.Handler with additional options
func (a *Handler) RegisterHandlersWithOptions(mux *http.ServeMux) {
	mux.HandleFunc(fmt.Sprintf("POST %s/sandboxes", BaseURL), a.PostSandboxes)
	mux.HandleFunc(fmt.Sprintf("GET %s/sandboxes/{sandboxID}", BaseURL), a.GetSandbox)
	mux.HandleFunc(fmt.Sprintf("GET %s/v2/sandboxes", BaseURL), a.ListSandboxes)

	//E2B does not support delete sandbox now, delete API is reserved for call with http api
	mux.HandleFunc(fmt.Sprintf("DELETE %s/sandboxes/{sandboxID}", BaseURL), a.DeleteSandbox)

	mux.HandleFunc(fmt.Sprintf("POST %s/sandboxes/{sandboxID}/connect", BaseURL), a.ConnectSandbox)
	mux.HandleFunc(fmt.Sprintf("POST %s/sandboxes/{sandboxID}/snapshots", BaseURL), a.SnapshotSandbox)
	mux.HandleFunc(fmt.Sprintf("DELETE %s/sandboxes/{sandboxID}/snapshots", BaseURL), a.DeleteSnapshotSandbox)

}
