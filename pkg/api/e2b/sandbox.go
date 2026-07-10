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
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/agent-sandbox/agent-sandbox/pkg/api/e2b/api"
	"github.com/agent-sandbox/agent-sandbox/pkg/auth"
	"github.com/agent-sandbox/agent-sandbox/pkg/capacity"
	"github.com/agent-sandbox/agent-sandbox/pkg/sandbox"
	"github.com/agent-sandbox/agent-sandbox/pkg/telemetry"
	"github.com/agent-sandbox/agent-sandbox/pkg/utils"
	"k8s.io/klog/v2"
)

func (a *Handler) GetSandbox(w http.ResponseWriter, r *http.Request) {
	sandboxID := r.PathValue("sandboxID")
	if sandboxID == "" {
		sendAPIError(w, http.StatusBadRequest, "sandboxID is required")
		return
	}

	klog.V(2).Infof("Get sandbox sandboxID=%s", sandboxID)

	sb, err := a.controller.GetByID(sandboxID)
	if err != nil {
		klog.Errorf("Get sandbox %s error: %v", sandboxID, err)
		sendAPIError(w, http.StatusNotFound, fmt.Sprintf("sandbox %s not found", sandboxID))
		return
	}

	apiSbx := a.convertToE2BSandbox(sb)
	sendAPIOK(w, http.StatusOK, apiSbx)
	return
}

func (a *Handler) ListSandboxes(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUserTokenFromContext(r.Context())
	if user == "" {
		sendAPIError(w, http.StatusBadRequest, "User not found, api key may be invalid")
		return
	}

	sbs, err := a.controller.List(user)
	if err != nil {
		sendAPIError(w, http.StatusInternalServerError, fmt.Sprintf("failed to list sandboxes: %v", err))
		return
	}

	var apiSandboxes = []*api.Sandbox{}
	for _, sb := range sbs {
		apiSbx := a.convertToE2BSandbox(sb)
		apiSandboxes = append(apiSandboxes, apiSbx)
	}

	sendAPIOK(w, http.StatusOK, apiSandboxes)
	return
}

func (a *Handler) DeleteSandbox(w http.ResponseWriter, r *http.Request) {
	sandboxID := r.PathValue("sandboxID")
	if sandboxID == "" {
		sendAPIError(w, http.StatusBadRequest, "sandboxID is required")
		return
	}

	klog.V(2).Infof("Delete sandbox sandboxID=%s", sandboxID)

	err := a.controller.DeleteByIDWithReason(sandboxID, telemetry.ReasonE2BSDK)
	if err != nil {
		klog.ErrorS(err, "error deleting sandbox", "sandboxID", sandboxID)
		sendAPIError(w, http.StatusBadRequest, fmt.Sprintf("failed to delete sandbox %s: %v", sandboxID, err))
		return
	}

	sendAPIOK(w, http.StatusOK, nil)
	return
}

func (a *Handler) PostSandboxes(w http.ResponseWriter, r *http.Request) {
	var request *api.NewSandbox

	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		sendAPIError(w, http.StatusBadRequest, fmt.Sprintf("failed to decode request body: %v", err))
		return
	}

	klog.V(2).Infof("Post sandboxes with request %+v", request)

	// check template is valid, includes number,- and string, e.g. sandbox-template-demo-version2026 by regexp
	patten := `^[a-zA-Z0-9\-\.]+$`
	matched, _ := regexp.MatchString(patten, request.TemplateID)
	if !matched {
		sendAPIError(w, http.StatusBadRequest, "invalid template ID format, only alphanumeric characters and hyphens are allowed")
		return
	}

	sbx, err := a.CreateSandbox(r.Context(), request)
	if err != nil {
		sendAPIError(w, err.Code, fmt.Sprintf("failed to create sandbox: %v", err.ClientMsg))
		return
	}

	sendAPIOK(w, http.StatusCreated, sbx)
	return
}

func (a *Handler) CreateSandbox(ctx context.Context, newSandbox *api.NewSandbox) (*api.Sandbox, *APIError) {
	user := auth.GetUserTokenFromContext(ctx)
	if user == "" {
		return nil, &APIError{
			ClientMsg: "User not found, api key may be invalid",
			Code:      http.StatusBadRequest,
		}
	}

	// check capacity limit
	if capacity.GlobalLimiter.Enabled() {
		_, err := capacity.GlobalLimiter.AcquireCreate(user)
		if err != nil {
			limitErr := err.(*capacity.LimitError)
			return nil, &APIError{ClientMsg: limitErr.Message, Code: limitErr.Code}
		}
	}

	// gen id and default labels of user, key
	var sb = sandbox.GetDefaultSandbox()
	sb.User = user

	//code-interpreter-v1, remove  rightmost  version part of TemplateID in default mode
	tplID := strings.Split(newSandbox.TemplateID, "-v")[0]

	sb.Template = tplID
	sb.Metadata = newSandbox.Metadata
	sb.EnvVars = newSandbox.EnvVars
	sb.Timeout = newSandbox.Timeout

	if newSandbox.AutoPause != nil {
		sb.AutoPause = *newSandbox.AutoPause
	}
	if newSandbox.AutoResume != nil {
		sb.AutoResume = newSandbox.AutoResume.Enabled
	}

	idleTimeoutStr := newSandbox.Metadata["idleTimeout"]
	if idleTimeoutStr != "" {
		v, err := strconv.Atoi(idleTimeoutStr)
		if err != nil {
			return nil, &APIError{
				Err:       err,
				ClientMsg: "invalid idleTimeout value, must be an integer",
				Code:      http.StatusBadRequest,
			}
		}
		sb.IdleTimeout = v
	}

	// init name and valid fields
	if err := sb.Make(); err != nil {
		return nil, &APIError{
			Err:       err,
			ClientMsg: "error creating sandbox, params error " + err.Error(),
			Code:      http.StatusBadRequest,
		}
	}

	klog.Infof("Creating sandbox orgin newSandbox is %+v", newSandbox)

	sbCreated, err := a.controller.Create(sb)
	if err != nil {
		klog.ErrorS(err, "error creating sandbox", "sandbox", sb)
		return nil, &APIError{
			Err:       err,
			ClientMsg: "error creating sandbox, error " + err.Error(),
			Code:      http.StatusBadRequest,
		}
	}

	apiSbx := a.convertToE2BSandbox(sbCreated)

	return apiSbx, nil
}

func (a *Handler) SnapshotSandbox(w http.ResponseWriter, r *http.Request) {
	sandboxID := r.PathValue("sandboxID")
	if sandboxID == "" {
		sendAPIError(w, http.StatusBadRequest, "sandboxID is required")
		return
	}

	sb, err := a.controller.GetByID(sandboxID)
	if err != nil {
		klog.Errorf("Get sandbox %s error: %v", sandboxID, err)
		sendAPIError(w, http.StatusNotFound, fmt.Sprintf("sandbox %s not found", sandboxID))
		return
	}

	err = a.controller.SandboxProcessSnapshot(sb)
	if err != nil {
		klog.Errorf("Failed to snapshot sandbox %s: %v", sb.Name, err)
		sendAPIError(w, http.StatusInternalServerError, fmt.Sprintf("failed to snapshot sandbox %s: %v", sb.Name, err))
		return
	}

	apiSbx := api.Snapshot{
		SnapshotID: sb.ID,
		Names:      []string{sb.Name},
	}
	sendAPIOK(w, http.StatusCreated, apiSbx)
	return
}

func (a *Handler) DeleteSnapshotSandbox(w http.ResponseWriter, r *http.Request) {
	sandboxID := r.PathValue("sandboxID")
	if sandboxID == "" {
		sendAPIError(w, http.StatusBadRequest, "sandboxID is required")
		return
	}

	sb, err := a.controller.GetByID(sandboxID)
	if err != nil {
		klog.Errorf("Get sandbox %s error: %v", sandboxID, err)
		sendAPIError(w, http.StatusNotFound, fmt.Sprintf("sandbox %s not found", sandboxID))
		return
	}

	err = a.controller.DeleteProcessSnapshot(sb)
	if err != nil {
		klog.Errorf("Failed to delete snapshot sandbox %s: %v", sb.Name, err)
		sendAPIError(w, http.StatusInternalServerError, fmt.Sprintf("failed to delete snapshot sandbox %s: %v", sb.Name, err))
		return
	}

	apiSbx := api.Snapshot{
		SnapshotID: sb.ID,
		Names:      []string{sb.Name},
	}
	sendAPIOK(w, http.StatusOK, apiSbx)
	return
}

func (a *Handler) ConnectSandbox(w http.ResponseWriter, r *http.Request) {
	sandboxID := r.PathValue("sandboxID")
	if sandboxID == "" {
		sendAPIError(w, http.StatusBadRequest, "sandboxID is required")
		return
	}

	sb, err := a.controller.GetByID(sandboxID)
	if err != nil {
		klog.Errorf("Get sandbox %s error: %v", sandboxID, err)
		sendAPIError(w, http.StatusNotFound, fmt.Sprintf("sandbox %s not found", sandboxID))
		return
	}

	resumed := false
	if sb.Status == sandbox.Paused && sb.AutoResume {
		err = a.controller.Resume(sb, "SDKGetSandbox")
		if err != nil {
			klog.Errorf("Failed to resume sandbox %s: %v", sb.Name, err)
			sendAPIError(w, http.StatusFailedDependency, fmt.Sprintf("failed to resume sandbox %s: %v", sb.Name, err))
			return
		}
		resumed = true
	}

	apiSbx := a.convertToE2BSandbox(sb)
	if resumed {
		apiSbx.Metadata["resumed"] = "true"
	}

	sendAPIOK(w, http.StatusOK, apiSbx)
	return
}

func (a *Handler) SandboxRouterOfPath() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		scheme := "http"
		if r.TLS != nil {
			scheme = "https"
		}
		klog.Info("SandboxRouterOfPath ", r.Method, " ", scheme, "://", r.Host, r.RequestURI, " query=", r.URL.RawQuery)

		sandboxID := r.PathValue("sandboxID")
		if sandboxID == "" {
			sendAPIError(w, http.StatusBadRequest, "sandboxID is required")
			return
		}

		port := r.PathValue("port")
		if port == "" {
			sendAPIError(w, http.StatusBadRequest, "port is required")
			return
		}

		sb, err := a.controller.GetByID(sandboxID)
		if err != nil {
			klog.Errorf("Get sandbox %s error: %v", sandboxID, err)
			sendAPIError(w, http.StatusNotFound, fmt.Sprintf("sandbox %s not found", sandboxID))
			return
		}
		sbName := sb.Name

		if sb.Status == sandbox.Paused && sb.AutoResume {
			err = a.controller.Resume(sb, "RequestOfPath")
			if err != nil {
				klog.Errorf("Failed to resume sandbox %s: %v", sb.Name, err)
				sendAPIError(w, http.StatusFailedDependency, fmt.Sprintf("failed to resume sandbox %s: %v", sb.Name, err))
				return
			}
		}

		// rewrite url to /sandbox/{name}/{port}/...
		prefixToStrip := fmt.Sprintf("/sandboxes/router/%s/%s", sandboxID, port)
		postPath := strings.TrimPrefix(r.URL.Path, prefixToStrip)

		pxyURL := fmt.Sprintf("/sandbox/%s%s", sbName, postPath)
		r.URL.Path = pxyURL
		//add port query param to url
		if r.URL.RawQuery == "" {
			r.URL.RawQuery = "port=" + port
		} else {
			r.URL.RawQuery = r.URL.RawQuery + "&port=" + port
		}

		klog.Info("ExecuteSandbox proxying... new url=", r.URL)
		http.DefaultServeMux.ServeHTTP(w, r)
		return
	}
}

func (a *Handler) SandboxRouterOfDomain() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		scheme := "http"
		if r.TLS != nil {
			scheme = "https"
		}
		klog.Info("SandboxRouterOfDomain ", r.Method, " ", scheme, "://", r.Host, r.RequestURI, " query=", r.URL.RawQuery)

		// match host and request url is {port}-{sandbox_id}.{domain}/...
		reqHost := r.Host
		//"49999-e94466d4e94466d4.example.com"
		patten := `^[0-9]{4,5}-[a-f0-9]{8,48}\.[a-zA-Z0-9.-]+`
		matched, _ := regexp.MatchString(patten, reqHost)
		if !matched {
			// otherwise just return 404
			klog.Info("no matched sandbox proxy pattern, url=", reqHost)
			sendAPIError(w, http.StatusNotFound, "invalid sandbox request "+reqHost)
			return
		}

		// match port from reqURL
		portSvr := regexp.MustCompile(`^([0-9]{4,5})-([a-f0-9]+)\.`)
		submatches := portSvr.FindStringSubmatch(reqHost)
		if len(submatches) < 3 {
			klog.Info("invalid matched sandbox request pattern, url=", reqHost)
			sendAPIError(w, http.StatusNotFound, "invalid sandbox request, no port found "+reqHost)
			return
		}
		port := submatches[1]
		sandboxID := submatches[2]

		sb, err := a.controller.GetByID(sandboxID)
		if err != nil {
			klog.Errorf("Get sandbox %s error: %v", sandboxID, err)
			sendAPIError(w, http.StatusNotFound, fmt.Sprintf("sandbox %s not found", sandboxID))
			return
		}
		sbName := sb.Name

		if sb.Status == sandbox.Paused && sb.AutoResume {
			err = a.controller.Resume(sb, "Request")
			if err != nil {
				klog.Errorf("Failed to resume sandbox %s: %v", sb.Name, err)
				sendAPIError(w, http.StatusFailedDependency, fmt.Sprintf("failed to resume sandbox %s: %v", sb.Name, err))
				return
			}
		}

		// rewrite url to /sandbox/{name}/{port}/...
		pxyURL := fmt.Sprintf("/sandbox/%s%s", sbName, r.URL.Path)
		r.URL.Path = pxyURL
		if r.URL.RawQuery == "" {
			r.URL.RawQuery = "port=" + port
		} else {
			r.URL.RawQuery = r.URL.RawQuery + "&port=" + port
		}

		klog.Info("matched sandbox proxy pattern, proxying... new url=", r.URL)
		http.DefaultServeMux.ServeHTTP(w, r)
		return
	}
}

// convertToE2BSandbox converts an internal sandbox.Sandbox to api.Sandbox format
func (a *Handler) convertToE2BSandbox(sb *sandbox.Sandbox) *api.Sandbox {
	apiSbx := GetDefaultE2BSandbox()
	apiSbx.SandboxID = sb.ID
	apiSbx.TemplateID = sb.Template

	apiSbx.EnvdAccessToken = sb.ID
	apiSbx.TrafficAccessToken = sb.ID

	if sb.Metadata != nil {
		apiSbx.Metadata = sb.Metadata
	}

	apiSbx.Metadata["name"] = sb.Name

	rs := utils.CalculateResourceToQuantity(sb.CPU, sb.Memory)
	apiSbx.CpuCount = rs.CPUMilli
	apiSbx.MemoryMB = rs.MemoryMB
	apiSbx.DiskSizeMB = rs.DiskSizeMB

	apiSbx.StartedAt = sb.CreatedAt

	apiSbx.State = api.Running
	if sb.Status != "" {
		apiSbx.State = api.SandboxState(sb.Status)
	}

	onTimeout := api.Kill
	if sb.AutoPause {
		onTimeout = api.Pause
	}
	apiSbx.Lifecycle = &api.SandboxLifecycle{
		AutoResume: sb.AutoResume,
		OnTimeout:  onTimeout,
	}

	return apiSbx
}
