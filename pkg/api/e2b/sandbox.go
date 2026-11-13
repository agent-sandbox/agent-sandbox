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
	"errors"
	"fmt"
	"maps"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/agent-sandbox/agent-sandbox/pkg/api/e2b/api"
	"github.com/agent-sandbox/agent-sandbox/pkg/auth"
	"github.com/agent-sandbox/agent-sandbox/pkg/capacity"
	"github.com/agent-sandbox/agent-sandbox/pkg/config"
	"github.com/agent-sandbox/agent-sandbox/pkg/sandbox"
	"github.com/agent-sandbox/agent-sandbox/pkg/telemetry"
	"github.com/agent-sandbox/agent-sandbox/pkg/utils"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/klog/v2"
)

// getOwnedSandbox looks up sandboxID for the caller via Controller.GetOwnedByID (the shared access
// rule). On failure it writes the error and returns nil: not found (including another user's
// sandbox, so its existence is not revealed) -> 404, any other lookup error -> 400.
func (a *Handler) getOwnedSandbox(w http.ResponseWriter, r *http.Request, sandboxID string) *sandbox.Sandbox {
	sb, err := a.controller.GetOwnedByID(sandboxID, auth.GetUserTokenFromContext(r.Context()))
	if err != nil {
		klog.Errorf("Get sandbox %s error: %v", sandboxID, err)
		if errors.Is(err, sandbox.ErrSandboxNotFound) {
			sendAPIError(w, http.StatusNotFound, fmt.Sprintf("sandbox %s not found", sandboxID))
		} else {
			sendAPIError(w, http.StatusBadRequest, fmt.Sprintf("failed to get sandbox %s: %v", sandboxID, err))
		}
		return nil
	}
	return sb
}

func (a *Handler) GetSandbox(w http.ResponseWriter, r *http.Request) {
	sandboxID := r.PathValue("sandboxID")
	if sandboxID == "" {
		sendAPIError(w, http.StatusBadRequest, "sandboxID is required")
		return
	}

	klog.V(2).Infof("Get sandbox sandboxID=%s", sandboxID)

	sb := a.getOwnedSandbox(w, r, sandboxID)
	if sb == nil {
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

	// GET /v2/sandboxes declares metadata, state, nextToken and limit (see
	// api.GetV2SandboxesParams, generated from the e2b description). They were never
	// read off the request, so every call returned the caller's whole list and a
	// filter that matched nothing came back looking like a filter that matched
	// everything.
	params, perr := parseListParams(r.URL.Query())
	if perr != "" {
		sendAPIError(w, http.StatusBadRequest, perr)
		return
	}
	var metaFilter map[string]string
	if params.Metadata != nil {
		var ok bool
		metaFilter, ok = parseMetadataFilter(*params.Metadata)
		if !ok {
			sendAPIError(w, http.StatusBadRequest, "invalid metadata filter, expected URL-encoded key=value pairs")
			return
		}
	}

	// metadata["labels"] is a K8s label selector, filtered by the controller on the K8s side;
	// the remaining metadata keys are matched exactly below.
	// The Python SDK quotes each value before url-encoding the pairs, so one encoding layer is left
	// after parseMetadataFilter. A valid selector never contains '%', so decoding it once more is safe.
	labelSelector, err := url.PathUnescape(metaFilter[sandbox.MetadataLabels])
	if err != nil {
		sendAPIError(w, http.StatusBadRequest, fmt.Sprintf("%v: %v", sandbox.ErrInvalidLabelSelector, err))
		return
	}
	delete(metaFilter, sandbox.MetadataLabels)

	sbs, err := a.controller.List(user, labelSelector)
	if errors.Is(err, sandbox.ErrInvalidLabelSelector) {
		sendAPIError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err != nil {
		sendAPIError(w, http.StatusInternalServerError, fmt.Sprintf("failed to list sandboxes: %v", err))
		return
	}

	var apiSandboxes = []*api.Sandbox{}
	for _, sb := range sbs {
		apiSbx := a.convertToE2BSandbox(sb)
		apiSandboxes = append(apiSandboxes, apiSbx)
	}

	// Filter AFTER conversion: state and metadata are read from the e2b shape, so
	// the filter sees exactly what the caller sees.
	apiSandboxes, nextToken := applyListFilters(apiSandboxes, params, metaFilter)
	if nextToken != "" {
		w.Header().Set("X-Next-Token", nextToken)
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

	if a.getOwnedSandbox(w, r, sandboxID) == nil {
		return
	}

	err := a.controller.DeleteByIDWithReason(sandboxID, telemetry.ReasonE2BSDK)
	if err != nil {
		klog.ErrorS(err, "error deleting sandbox", "sandboxID", sandboxID)
		sendAPIError(w, http.StatusBadRequest, fmt.Sprintf("failed to delete sandbox %s: %v", sandboxID, err))
		return
	}

	sendAPIOK(w, http.StatusOK, nil)
	return
}

// redactNewSandbox returns a copy of n for logging, with env var values replaced.
func redactNewSandbox(n *api.NewSandbox) *api.NewSandbox {
	if n == nil {
		return nil
	}
	out := *n
	out.EnvVars = config.RedactEnvVars(n.EnvVars)
	return &out
}

func (a *Handler) PostSandboxes(w http.ResponseWriter, r *http.Request) {
	var request *api.NewSandbox

	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		sendAPIError(w, http.StatusBadRequest, fmt.Sprintf("failed to decode request body: %v", err))
		return
	}

	klog.V(2).Infof("Post sandboxes with request %+v", redactNewSandbox(request))

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

	klog.Infof("Creating sandbox orgin newSandbox is %+v", redactNewSandbox(newSandbox))

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

	user := auth.GetUserTokenFromContext(r.Context())
	if user == "" {
		sendAPIError(w, http.StatusBadRequest, "User not found, api key may be invalid")
		return
	}

	sb := a.getOwnedSandbox(w, r, sandboxID)
	if sb == nil {
		return
	}
	var err error

	// The e2b SDK has no update-metadata call, so create_snapshot carries a JSON "metadata" header:
	// "labels" replaces the labels, "conditions" (a label selector) must match the current labels.
	// Both are applied before the snapshot; invalid input or unmet conditions skip it.
	if raw := r.Header.Get("metadata"); raw != "" {
		userLabels, conditions, perr := parseSnapshotMetadata(raw)
		if perr != nil {
			sendAPIError(w, http.StatusBadRequest, perr.Error())
			return
		}
		if userLabels != nil || conditions != nil {
			if err = a.controller.UpdateUserLabels(sb, userLabels, conditions); err != nil {
				if errors.Is(err, sandbox.ErrConditionsNotMet) {
					sendAPIError(w, http.StatusPreconditionFailed, err.Error())
					return
				}
				klog.Errorf("Failed to update labels of sandbox %s: %v", sb.Name, err)
				sendAPIError(w, http.StatusInternalServerError, err.Error())
				return
			}
		}
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

	sb := a.getOwnedSandbox(w, r, sandboxID)
	if sb == nil {
		return
	}

	err := a.controller.DeleteProcessSnapshot(sb)
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

	sb := a.getOwnedSandbox(w, r, sandboxID)
	if sb == nil {
		return
	}

	if sb.Status == sandbox.Paused && sb.AutoResume {
		err := a.controller.Resume(sb, "SDKGetSandbox")
		if err != nil {
			klog.Errorf("Failed to resume sandbox %s: %v", sb.Name, err)
			sendAPIError(w, http.StatusFailedDependency, fmt.Sprintf("failed to resume sandbox %s: %v", sb.Name, err))
			return
		}
		// metadata was read before the resume; show this resume's result
		sb.RefreshRuntimeMetadata()
	}

	apiSbx := a.convertToE2BSandbox(sb)

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

		a.router.Proxy(w, r, sbName, port, fmt.Sprintf("/sandboxes/router/%s/%s", sandboxID, port))
	}
}

func (a *Handler) SandboxRouterOfDomain() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		scheme := "http"
		if r.TLS != nil {
			scheme = "https"
		}
		klog.Info("SandboxRouterOfDomain ", r.Method, " ", scheme, "://", r.Host, r.RequestURI, " query=", r.URL.RawQuery)

		port, sandboxID, ok := ParseSandboxHost(r.Host)
		if !ok {
			klog.Info("no matched sandbox proxy pattern, host=", r.Host)
			sendAPIError(w, http.StatusNotFound, "invalid sandbox request "+r.Host)
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
			err = a.controller.Resume(sb, "Request")
			if err != nil {
				klog.Errorf("Failed to resume sandbox %s: %v", sb.Name, err)
				sendAPIError(w, http.StatusFailedDependency, fmt.Sprintf("failed to resume sandbox %s: %v", sb.Name, err))
				return
			}
		}

		a.router.Proxy(w, r, sbName, port, "")
	}
}

// sandboxHostPattern matches E2B sandbox hosts: {port}-{sandbox_id}.{domain}, e.g. "49999-e94466d4e94466d4.example.com".
var sandboxHostPattern = regexp.MustCompile(`^([0-9]{4,5})-([a-f0-9]{8,48})\.[a-zA-Z0-9.-]+`)

// ParseSandboxHost extracts the port and sandbox ID from an E2B sandbox host.
func ParseSandboxHost(host string) (port, sandboxID string, ok bool) {
	m := sandboxHostPattern.FindStringSubmatch(host)
	if m == nil {
		return "", "", false
	}
	return m[1], m[2], true
}

// convertToE2BSandbox converts an internal sandbox.Sandbox to api.Sandbox format
func (a *Handler) convertToE2BSandbox(sb *sandbox.Sandbox) *api.Sandbox {
	apiSbx := GetDefaultE2BSandbox()
	apiSbx.SandboxID = sb.ID
	apiSbx.TemplateID = sb.Template

	apiSbx.EnvdAccessToken = sb.ID
	apiSbx.TrafficAccessToken = sb.ID

	if sb.Metadata != nil {
		// clone: sb.Metadata may belong to a cached sandbox
		apiSbx.Metadata = maps.Clone(sb.Metadata)
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

// parseSnapshotMetadata reads the JSON "metadata" header of create_snapshot,
// e.g. {"labels":"team=a,count=3","conditions":"count=2,agentid=abc"}.
// userLabels is nil when the header has no labels key (labels unchanged), and empty for "" (clear all).
// conditions is a K8s label selector over the current labels, nil when absent or empty.
func parseSnapshotMetadata(raw string) (userLabels map[string]string, conditions labels.Selector, err error) {
	var meta map[string]string
	if err := json.Unmarshal([]byte(raw), &meta); err != nil {
		return nil, nil, fmt.Errorf("invalid metadata header, expected a JSON object of strings: %v", err)
	}
	if v, ok := meta[sandbox.MetadataLabels]; ok {
		if userLabels, err = sandbox.ParseLabels(v); err != nil {
			return nil, nil, err
		}
	}
	if conditions, err = sandbox.ParseUserSelector(meta[metadataConditions]); err != nil {
		return nil, nil, fmt.Errorf("invalid conditions: %v", err)
	}
	return userLabels, conditions, nil
}

// metadataConditions is the create_snapshot metadata key holding the label selector the update requires.
const metadataConditions = "conditions"
