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

package sandbox

import (
    "context"
    "encoding/json"
    "fmt"
    "net/http"

    "k8s.io/klog/v2"
)

type Handler struct {
    rootCtx    context.Context
    controller *Controller
}

func NewHandler(rootCtx context.Context) *Handler {
    c := NewController(rootCtx)

    return &Handler{
        rootCtx:    rootCtx,
        controller: c,
    }
}

func (a *Handler) CreateSandbox(r *http.Request) (interface{}, error) {
    var sb = DefaultSandbox
    err := json.NewDecoder(r.Body).Decode(sb)
    if err != nil {
        return "", fmt.Errorf("failed to decode request body: %v", err)
    }
    sb.Make()

    klog.V(2).Infof("Create sandbox opts %v", sb)

    exist := a.controller.Get(sb.Name)
    if exist != nil {
        return "", fmt.Errorf("sandbox %s already exists", sb.Name)
    }

    err = a.controller.Create(sb)

    if err != nil {
        klog.Errorf("Failed to create sandbox, err: %v", err)
        return "", fmt.Errorf("failed to create new sandbox, error: %v", err)
    }

    return fmt.Sprintf("Sandbox %s created successfully", sb.Name), nil
}

func (a *Handler) ListSandboxes(r *http.Request) (interface{}, error) {
    sbs := a.controller.List()
    if sbs == nil {
        return "", fmt.Errorf("no sandboxes found")
    }

    return sbs, nil
}

func (a *Handler) GetSandbox(r *http.Request) (interface{}, error) {
    name := r.PathValue("name")
    klog.V(2).Infof("Delete sandbox name=%s", name)
    sb := &Sandbox{
        Name: name,
    }

    sboGet := a.controller.Get(name)
    if sb == nil {
        return "", fmt.Errorf("sandbox %s not found", name)
    }

    return sboGet, nil
}

func (a *Handler) DelSandbox(r *http.Request) (interface{}, error) {
    name := r.PathValue("name")
    klog.V(2).Infof("Delete sandbox name=%s", name)

    err := a.controller.Delete(name)
    if err != nil {
        return "", fmt.Errorf("failed to delete sandbox %s: %v", name, err)
    }

    return fmt.Sprintf("Sandbox %s deleted successfully", name), nil
}
