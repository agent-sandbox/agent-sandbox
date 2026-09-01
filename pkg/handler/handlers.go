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
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"path"
	"sort"
	"strconv"
	"strings"
	"sync"
	"text/template"

	"github.com/agent-sandbox/agent-sandbox/pkg/activator"
	"github.com/agent-sandbox/agent-sandbox/pkg/auth"
	"github.com/agent-sandbox/agent-sandbox/pkg/capacity"
	"github.com/agent-sandbox/agent-sandbox/pkg/config"
	"github.com/agent-sandbox/agent-sandbox/pkg/sandbox"
	"github.com/agent-sandbox/agent-sandbox/pkg/telemetry"
	"github.com/agent-sandbox/agent-sandbox/pkg/utils"
	"github.com/gorilla/websocket"
	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/klog/v2"
	"sigs.k8s.io/yaml"
)

const maxUploadBodyBytes = 100 << 20

type ClientSessionCache struct {
	// TODO session cleanup?
	sessions sync.Map
}

type Handler struct {
	rootCtx      context.Context
	controller   *sandbox.Controller
	sessionCache *ClientSessionCache
	activator    *activator.Activator
}

func NewHandler(rootCtx context.Context, c *sandbox.Controller, a *activator.Activator) *Handler {
	cache := &ClientSessionCache{}

	return &Handler{
		rootCtx:      rootCtx,
		controller:   c,
		activator:    a,
		sessionCache: cache,
	}
}

func sanitizeDownloadFileName(filePath string) string {
	base := path.Base(strings.TrimSpace(filePath))
	if base == "" || base == "." || base == "/" {
		return "download.bin"
	}
	return base
}

func (a *Handler) ListSandboxFiles(r *http.Request) (interface{}, error) {
	name := strings.TrimSpace(r.PathValue("name"))
	if name == "" {
		return nil, fmt.Errorf("sandbox name is required")
	}

	if _, err := a.controller.Get(name); err != nil {
		return nil, fmt.Errorf("sandbox %s not found", name)
	}

	targetPath := r.URL.Query().Get("path")
	result, err := a.controller.ListSandboxFiles(name, targetPath)
	if err != nil {
		return nil, err
	}

	return result, nil
}

func (a *Handler) UploadSandboxFile(r *http.Request) (interface{}, error) {
	name := strings.TrimSpace(r.PathValue("name"))
	if name == "" {
		return nil, fmt.Errorf("sandbox name is required")
	}

	if _, err := a.controller.Get(name); err != nil {
		return nil, fmt.Errorf("sandbox %s not found", name)
	}

	targetPath := r.URL.Query().Get("path")

	if err := r.ParseMultipartForm(maxUploadBodyBytes); err != nil {
		return nil, fmt.Errorf("failed to parse multipart form: %v", err)
	}

	file, fileHeader, err := r.FormFile("file")
	if err != nil {
		return nil, fmt.Errorf("file is required")
	}
	defer file.Close()

	result, err := a.controller.UploadSandboxFile(name, targetPath, file, fileHeader.Filename)
	if err != nil {
		return nil, err
	}

	return result, nil
}

func (a *Handler) DownloadSandboxFile(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimSpace(r.PathValue("name"))
	if name == "" {
		w.WriteHeader(http.StatusBadRequest)
		Err(w, "sandbox name is required")
		return
	}

	if _, err := a.controller.Get(name); err != nil {
		w.WriteHeader(http.StatusNotFound)
		Err(w, fmt.Sprintf("sandbox %s not found", name))
		return
	}

	filePath := strings.TrimSpace(r.URL.Query().Get("path"))
	if filePath == "" {
		w.WriteHeader(http.StatusBadRequest)
		Err(w, "path is required")
		return
	}

	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", sanitizeDownloadFileName(filePath)))

	if err := a.controller.DownloadSandboxFile(name, filePath, w); err != nil {
		w.Header().Del("Content-Disposition")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		Err(w, err.Error())
		return
	}
}

func (a *Handler) DeleteSandboxFile(r *http.Request) (interface{}, error) {
	name := strings.TrimSpace(r.PathValue("name"))
	if name == "" {
		return nil, fmt.Errorf("sandbox name is required")
	}

	if _, err := a.controller.Get(name); err != nil {
		return nil, fmt.Errorf("sandbox %s not found", name)
	}

	filePath := strings.TrimSpace(r.URL.Query().Get("path"))
	if filePath == "" {
		return nil, fmt.Errorf("path is required")
	}

	result, err := a.controller.DeleteSandboxFile(name, filePath)
	if err != nil {
		return nil, err
	}

	return result, nil
}

func (a *Handler) CreateSandbox(r *http.Request) (interface{}, error) {
	user := auth.GetUserTokenFromContext(r.Context())
	if user == "" {
		return nil, fmt.Errorf("user not found, api key may be invalid")
	}

	if capacity.GlobalLimiter.Enabled() {
		_, err := capacity.GlobalLimiter.AcquireCreate(user)
		if err != nil {
			limitErr := err.(*capacity.LimitError)
			return nil, &HTTPError{Code: limitErr.Code, Message: limitErr.Message}
		}
	}

	var sb = sandbox.GetDefaultSandbox()
	sb.User = user

	if err := json.NewDecoder(r.Body).Decode(sb); err != nil {
		return "", fmt.Errorf("failed to decode request body: %v", err)
	}

	// check sandbox with the same name already exists
	// if sb.Name is empty, it will be generated by controller, so no need to check
	if sb.Name != "" {
		exist, _ := a.controller.Get(sb.Name)
		if exist != nil {
			return "", fmt.Errorf("sandbox %s already exists", sb.Name)
		}
	}

	// init name and valid fields
	if err := sb.Make(); err != nil {
		return nil, fmt.Errorf("error create sandbox: %v", err)
	}

	klog.V(2).Infof("Create sandbox opts %v", sb)

	sbCreated, err := a.controller.Create(sb)
	if err != nil {
		klog.Errorf("Failed to create sandbox, err: %v", err)
		return "", fmt.Errorf("failed to create new sandbox, error: %v", err)
	}

	return sbCreated, nil
}

func (a *Handler) ListSandbox(r *http.Request) (interface{}, error) {
	user := auth.GetUserTokenFromContext(r.Context())
	if user == "" {
		return nil, fmt.Errorf("user not found, api key may be invalid")
	}

	var sbs []*sandbox.Sandbox
	var err error

	if strings.HasPrefix(user, "sys-") {
		sbs, err = a.controller.ListAll()
	} else {
		sbs, err = a.controller.List(user)
	}

	if err != nil {
		return "", fmt.Errorf("no sandboxes found %v", err)
	}

	return sbs, nil
}

func (a *Handler) GetSandbox(r *http.Request) (interface{}, error) {
	name := r.PathValue("name")
	if name == "" {
		return nil, fmt.Errorf("sandbox name is required")
	}

	klog.V(2).Infof("Get sandbox name=%s", name)

	sb, _ := a.controller.Get(name)
	if sb == nil {
		return "", fmt.Errorf("sandbox %s not found", name)
	}

	return sb, nil
}

func (a *Handler) DelSandbox(r *http.Request) (interface{}, error) {
	name := r.PathValue("name")
	if name == "" {
		return nil, fmt.Errorf("sandbox name is required")
	}

	klog.V(2).Infof("Delete sandbox name=%s", name)

	err := a.controller.DeleteWithReason(name, telemetry.ReasonAPIRequest)
	if err != nil {
		return "", fmt.Errorf("failed to delete sandbox %s: %v", name, err)
	}

	return fmt.Sprintf("Sandbox %s deleted successfully", name), nil
}

func (a *Handler) PauseSandbox(r *http.Request) (interface{}, error) {
	name := r.PathValue("name")
	if name == "" {
		return nil, fmt.Errorf("sandbox name is required")
	}

	klog.V(2).Infof("Pause sandbox name=%s", name)

	sb, err := a.controller.Get(name)
	if err != nil {
		return nil, fmt.Errorf("sandbox %s not found", name)
	}
	if err := a.controller.Pause(sb, "api"); err != nil {
		return nil, fmt.Errorf("failed to pause sandbox %s: %v", name, err)
	}

	return fmt.Sprintf("Sandbox %s paused successfully", name), nil
}

func (a *Handler) ResumeSandbox(r *http.Request) (interface{}, error) {
	name := r.PathValue("name")
	if name == "" {
		return nil, fmt.Errorf("sandbox name is required")
	}

	klog.V(2).Infof("Resume sandbox name=%s", name)

	sb, err := a.controller.Get(name)
	if err != nil {
		return nil, fmt.Errorf("sandbox %s not found", name)
	}
	if err := a.controller.Resume(sb, "api"); err != nil {
		return nil, fmt.Errorf("failed to resume sandbox %s: %v", name, err)
	}

	return fmt.Sprintf("Sandbox %s resumed successfully", name), nil
}

type RateLimitStatus struct {
	User                string `json:"user,omitempty"`
	ConcurrencyActive   int    `json:"concurrency_active"`
	ConcurrencyMax      int    `json:"concurrency_max"`
	SandboxCurrent      int    `json:"sandbox_current"`
	SandboxMax          int    `json:"sandbox_max"`
	SandboxUsagePercent int    `json:"sandbox_usage_percent"`
}

type AllRateLimitStatus struct {
	DefaultConfig RateLimitDefaultConfig `json:"default_config"`
	Users         []RateLimitStatus      `json:"users"`
}

type RateLimitDefaultConfig struct {
	Enabled        bool `json:"enabled"`
	MaxConcurrency int  `json:"max_concurrency"`
	MaxSandbox     int  `json:"max_sandbox"`
}

func buildRateLimitStatus(user string, includeUser bool, active, maxConcurrency, current, maxSandbox int) RateLimitStatus {
	usagePercent := 0
	if maxSandbox > 0 {
		usagePercent = (current * 100) / maxSandbox
	}

	status := RateLimitStatus{
		ConcurrencyActive:   active,
		ConcurrencyMax:      maxConcurrency,
		SandboxCurrent:      current,
		SandboxMax:          maxSandbox,
		SandboxUsagePercent: usagePercent,
	}
	if includeUser {
		status.User = user
	}
	return status
}

func (a *Handler) GetRateLimitStatus(r *http.Request) (interface{}, error) {
	if auth.GetUserTokenFromContext(r.Context()) == "" {
		return nil, fmt.Errorf("user not found, api key may be invalid")
	}

	if capacity.GlobalLimiter == nil {
		return AllRateLimitStatus{
			DefaultConfig: RateLimitDefaultConfig{},
			Users:         []RateLimitStatus{},
		}, nil
	}

	defaultConfig := capacity.GlobalLimiter.DefaultConfig()
	result := AllRateLimitStatus{
		DefaultConfig: RateLimitDefaultConfig{
			Enabled:        defaultConfig.Enabled,
			MaxConcurrency: defaultConfig.MaxConcurrency,
			MaxSandbox:     defaultConfig.MaxSandbox,
		},
		Users: []RateLimitStatus{},
	}

	counts, err := capacity.GlobalLimiter.CountAllByUser()
	if err != nil {
		klog.Warningf("Failed to list user sandbox counts for rate limit stats: %v", err)
		counts = map[string]int{}
	}

	userSet := map[string]struct{}{}
	for u := range counts {
		if u != "" {
			userSet[u] = struct{}{}
		}
	}
	for _, userCfg := range capacity.GlobalLimiter.UserConfigs() {
		if userCfg.User != "" {
			userSet[userCfg.User] = struct{}{}
		}
	}

	allUsers := make([]string, 0, len(userSet))
	for u := range userSet {
		allUsers = append(allUsers, u)
	}
	sort.Strings(allUsers)

	for _, u := range allUsers {
		active, maxConcurrency := capacity.GlobalLimiter.ConcurrencyStats(u)
		_, maxSandbox := capacity.GlobalLimiter.UserConfig(u)
		result.Users = append(result.Users, buildRateLimitStatus(u, true, active, maxConcurrency, counts[u], maxSandbox))
	}

	return result, nil
}

func (a *Handler) GetSandboxLogs(r *http.Request) (interface{}, error) {
	name := r.PathValue("name")
	if name == "" {
		return nil, fmt.Errorf("sandbox name is required")
	}

	const defaultTailLines int64 = 200
	const maxTailLines int64 = 2000

	tailLines := defaultTailLines
	if rawTailLines := r.URL.Query().Get("tailLines"); rawTailLines != "" {
		parsedTailLines, err := strconv.ParseInt(rawTailLines, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid tailLines: %v", err)
		}
		if parsedTailLines <= 0 {
			return nil, fmt.Errorf("tailLines must be a positive integer")
		}
		if parsedTailLines > maxTailLines {
			tailLines = maxTailLines
		} else {
			tailLines = parsedTailLines
		}
	}

	logs, err := a.controller.GetSandboxLogs(name, tailLines)
	if err != nil {
		return nil, err
	}

	return logs, nil
}

func (a *Handler) ListSandboxEvents(r *http.Request) (interface{}, error) {
	const defaultLimit int64 = 100
	const maxLimit int64 = 500

	sandboxName := strings.TrimSpace(r.URL.Query().Get("sandbox"))
	limit := defaultLimit

	if rawLimit := strings.TrimSpace(r.URL.Query().Get("limit")); rawLimit != "" {
		parsedLimit, err := strconv.ParseInt(rawLimit, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid limit: %v", err)
		}
		if parsedLimit <= 0 {
			return nil, fmt.Errorf("limit must be a positive integer")
		}
		if parsedLimit > maxLimit {
			limit = maxLimit
		} else {
			limit = parsedLimit
		}
	}

	result, err := a.controller.ListReplicaSetEvents(sandboxName, limit)
	if err != nil {
		return nil, err
	}

	return result, nil
}

type SandboxMetricsRequest struct {
	Names []string `json:"names"`
}

type SandboxMetricsResponse struct {
	Items map[string]sandbox.SandboxMetricsItem `json:"items"`
}

func (a *Handler) SandboxMetrics(r *http.Request) (interface{}, error) {
	var req SandboxMetricsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		return nil, fmt.Errorf("failed to decode request body: %v", err)
	}

	normalizedNames := make([]string, 0, len(req.Names))
	seen := make(map[string]struct{}, len(req.Names))
	for _, name := range req.Names {
		trimmed := strings.TrimSpace(name)
		if trimmed == "" {
			continue
		}
		if _, exists := seen[trimmed]; exists {
			continue
		}
		seen[trimmed] = struct{}{}
		normalizedNames = append(normalizedNames, trimmed)
	}
	if len(normalizedNames) == 0 {
		return nil, fmt.Errorf("names is required")
	}

	items, err := a.controller.SandboxMetrics(normalizedNames)
	if err != nil {
		return nil, err
	}

	return &SandboxMetricsResponse{Items: items}, nil
}

type SandboxTerminalRequest struct {
	Command string `json:"command"`
}

type SandboxTerminalWSMessage struct {
	Type string `json:"type"`
	Data string `json:"data,omitempty"`
	Cols uint16 `json:"cols,omitempty"`
	Rows uint16 `json:"rows,omitempty"`
	Code int    `json:"code,omitempty"`
}

type terminalWSStreamWriter struct {
	send func(SandboxTerminalWSMessage) error
}

func (w *terminalWSStreamWriter) Write(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	if err := w.send(SandboxTerminalWSMessage{Type: "output", Data: string(p)}); err != nil {
		return 0, err
	}
	return len(p), nil
}

type terminalWSInputReader struct {
	inputCh <-chan string
	buf     []byte
}

func (r *terminalWSInputReader) Read(p []byte) (int, error) {
	for len(r.buf) == 0 {
		next, ok := <-r.inputCh
		if !ok {
			return 0, io.EOF
		}
		r.buf = []byte(next)
	}

	n := copy(p, r.buf)
	r.buf = r.buf[n:]
	return n, nil
}

var terminalWSUpgrader = websocket.Upgrader{
	ReadBufferSize:  4096,
	WriteBufferSize: 4096,
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

func defaultInteractiveShellCommand() []string {
	return []string{"sh", "-lc", "if command -v bash >/dev/null 2>&1; then exec bash -il; else exec sh -i; fi"}
}

func (a *Handler) StreamSandboxTerminalWS(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimSpace(r.PathValue("name"))
	if name == "" {
		w.WriteHeader(http.StatusBadRequest)
		Err(w, "sandbox name is required")
		return
	}

	if _, err := a.controller.Get(name); err != nil {
		w.WriteHeader(http.StatusNotFound)
		Err(w, fmt.Sprintf("sandbox %s not found", name))
		return
	}

	conn, err := terminalWSUpgrader.Upgrade(w, r, nil)
	if err != nil {
		klog.Errorf("failed to upgrade websocket for sandbox %s: %v", name, err)
		return
	}
	defer conn.Close()

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	inputCh := make(chan string, 64)
	resizeCh := make(chan utils.TerminalSize, 8)
	incomingCh := make(chan SandboxTerminalWSMessage, 32)
	readErrCh := make(chan error, 1)
	streamDone := make(chan error, 1)
	writeMu := &sync.Mutex{}

	send := func(message SandboxTerminalWSMessage) error {
		writeMu.Lock()
		defer writeMu.Unlock()
		return conn.WriteJSON(message)
	}

	defer close(inputCh)
	defer close(resizeCh)

	reader := &terminalWSInputReader{inputCh: inputCh}
	writer := &terminalWSStreamWriter{send: send}

	go func() {
		err := a.controller.StreamSandboxTerminal(ctx, name, defaultInteractiveShellCommand(), reader, writer, resizeCh, func(session *sandbox.SandboxTerminalSession) {
			if sendErr := send(SandboxTerminalWSMessage{Type: "ready", Data: fmt.Sprintf("connected to %s/%s", session.Pod, session.Container)}); sendErr != nil {
				klog.Warningf("failed to send ready message for sandbox %s: %v", name, sendErr)
				cancel()
			}
		})
		streamDone <- err
	}()

	go func() {
		for {
			var msg SandboxTerminalWSMessage
			if err := conn.ReadJSON(&msg); err != nil {
				readErrCh <- err
				return
			}
			incomingCh <- msg
		}
	}()

	handleStreamDone := func(err error) {
		if err != nil {
			_ = send(SandboxTerminalWSMessage{Type: "error", Data: err.Error()})
			_ = send(SandboxTerminalWSMessage{Type: "exit", Code: 1})
		} else {
			_ = send(SandboxTerminalWSMessage{Type: "exit", Code: 0})
		}
		_ = send(SandboxTerminalWSMessage{Type: "closed"})
	}

	for {
		select {
		case err := <-streamDone:
			handleStreamDone(err)
			return
		case err := <-readErrCh:
			if websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
				cancel()
				return
			}
			_ = send(SandboxTerminalWSMessage{Type: "error", Data: fmt.Sprintf("websocket read error: %v", err)})
			cancel()
			return
		case msg := <-incomingCh:
			switch msg.Type {
			case "init", "resize":
				if msg.Cols == 0 || msg.Rows == 0 {
					continue
				}
				select {
				case resizeCh <- utils.TerminalSize{Width: msg.Cols, Height: msg.Rows}:
				default:
				}
			case "input":
				if msg.Data == "" {
					continue
				}
				select {
				case inputCh <- msg.Data:
				case <-ctx.Done():
					return
				}
			case "close":
				cancel()
				err := <-streamDone
				handleStreamDone(err)
				return
			default:
				_ = send(SandboxTerminalWSMessage{Type: "error", Data: fmt.Sprintf("unsupported message type: %s", msg.Type)})
			}
		case <-ctx.Done():
			return
		}
	}
}

func (a *Handler) ExecuteSandboxTerminal(r *http.Request) (interface{}, error) {
	name := r.PathValue("name")
	if name == "" {
		return nil, fmt.Errorf("sandbox name is required")
	}

	var req SandboxTerminalRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		return nil, fmt.Errorf("failed to decode request body: %v", err)
	}

	commandText := strings.TrimSpace(req.Command)
	if commandText == "" {
		return nil, fmt.Errorf("command is required")
	}

	result, err := a.controller.ExecuteSandboxTerminal(name, []string{"sh", "-lc", commandText})
	if err != nil {
		return nil, err
	}

	return result, nil
}

// ------------------------------------------------------
// Config handlers
// ------------------------------------------------------

type RuntimeConfigStatus struct {
	ConfigMapKey           string                       `json:"config_map_key"`
	SystemToken            string                       `json:"system_token"`
	APITokensRaw           string                       `json:"api_tokens_raw"`
	RateLimit              config.RateLimitConfig       `json:"rate_limit"`
	RateLimitUsersRaw      string                       `json:"rate_limit_users_raw"`
	RateLimitUsers         []config.UserRateLimitConfig `json:"rate_limit_users"`
	SandboxDefaultImage    string                       `json:"sandbox_default_image"`
	SandboxDefaultTemplate string                       `json:"sandbox_default_template"`
}

func requireSystemTokenAccess(r *http.Request) error {
	user := auth.GetUserTokenFromContext(r.Context())
	if !strings.HasPrefix(user, "sys-") {
		return &HTTPError{Code: http.StatusForbidden, Message: "runtime config requires system token access"}
	}
	return nil
}

func buildRuntimeConfigStatus() RuntimeConfigStatus {
	runtimeConfig := config.Cfg.RuntimeConfigSnapshot()
	var systemToken, apiTokensRaw, rateLimitUsersRaw, sandboxDefaultImage, sandboxDefaultTemplate string
	var rateLimit config.RateLimitConfig
	if runtimeConfig.SystemToken != nil {
		systemToken = *runtimeConfig.SystemToken
	}
	if runtimeConfig.APITokensRaw != nil {
		apiTokensRaw = *runtimeConfig.APITokensRaw
	}
	if runtimeConfig.RateLimit != nil {
		rateLimit = *runtimeConfig.RateLimit
	}
	if runtimeConfig.RateLimitUsersRaw != nil {
		rateLimitUsersRaw = *runtimeConfig.RateLimitUsersRaw
	}
	if runtimeConfig.SandboxDefaultImage != nil {
		sandboxDefaultImage = *runtimeConfig.SandboxDefaultImage
	}
	if runtimeConfig.SandboxDefaultTemplate != nil {
		sandboxDefaultTemplate = *runtimeConfig.SandboxDefaultTemplate
	}

	rateLimitUsers, _ := config.ParseRateLimitUsers(rateLimitUsersRaw)

	return RuntimeConfigStatus{
		ConfigMapKey:           config.RuntimeConfigMapKey,
		SystemToken:            systemToken,
		APITokensRaw:           apiTokensRaw,
		RateLimit:              rateLimit,
		RateLimitUsersRaw:      rateLimitUsersRaw,
		RateLimitUsers:         rateLimitUsers,
		SandboxDefaultImage:    sandboxDefaultImage,
		SandboxDefaultTemplate: sandboxDefaultTemplate,
	}
}

func (a *Handler) GetRuntimeConfig(r *http.Request) (interface{}, error) {
	if err := requireSystemTokenAccess(r); err != nil {
		return nil, err
	}

	return buildRuntimeConfigStatus(), nil
}

func (a *Handler) SaveRuntimeConfig(r *http.Request) (interface{}, error) {
	if err := requireSystemTokenAccess(r); err != nil {
		return nil, err
	}

	var runtimeConfig config.RuntimeConfig
	if err := json.NewDecoder(r.Body).Decode(&runtimeConfig); err != nil {
		return "", fmt.Errorf("failed to decode request body: %v", err)
	}

	if runtimeConfig.RateLimitUsersRaw != nil {
		if _, err := config.ParseRateLimitUsers(*runtimeConfig.RateLimitUsersRaw); err != nil {
			return "", fmt.Errorf("rate_limit_users_raw must be a valid JSON array: %v", err)
		}
	}

	mergedRuntimeConfig := config.Cfg.MergedRuntimeConfig(runtimeConfig)
	content, err := config.RuntimeConfigContent(mergedRuntimeConfig)
	if err != nil {
		return "", fmt.Errorf("failed to marshal runtime config: %v", err)
	}

	if err := config.Cfg.SaveRuntimeConfigToCM(content); err != nil {
		return "", fmt.Errorf("failed to save runtime config error: %v", err)
	}

	config.Cfg.ApplyRuntimeConfig(mergedRuntimeConfig)

	return buildRuntimeConfigStatus(), nil
}

func (a *Handler) GetTemplatesConfig(r *http.Request) (interface{}, error) {
	return config.Cfg.ReadTemplatesFromCM()
}

func (a *Handler) SaveTemplatesConfig(r *http.Request) (interface{}, error) {
	var templates []*config.Template
	err := json.NewDecoder(r.Body).Decode(&templates)
	if err != nil {
		return "", fmt.Errorf("failed to decode request body: %v", err)
	}

	templatesData, err := json.MarshalIndent(templates, "", "  ")
	if err != nil {
		return "", fmt.Errorf("failed to marshal templates config: %v", err)
	}

	templatesContent := string(templatesData)
	klog.V(2).Infof("Save templates config: %s", templatesContent)

	if err := config.Cfg.SaveTemplatesToCM(templatesContent); err != nil {
		return "", fmt.Errorf("failed to save templates config error: %v", err)
	} else {
		return "ok", nil
	}
}

func (a *Handler) GetSandboxBlueprintConfig(r *http.Request) (interface{}, error) {
	return config.Cfg.ReadSandboxBlueprintFromCM()
}

func validateSandboxBlueprintContent(content string) error {
	tmpl, err := template.New("sandbox-blueprint").Parse(content)
	if err != nil {
		return fmt.Errorf("invalid sandbox blueprint syntax: %v", err)
	}

	sampleSandbox := sandbox.GetDefaultSandbox()
	sampleSandbox.Name = "sbx-sample"
	sampleSandbox.Image = config.Cfg.GetSandboxDefaultImage()
	sampleSandbox.TemplateObj = &config.Template{Name: sampleSandbox.Template, Image: sampleSandbox.Image}
	tplData := &sandbox.SandboxKube{
		Sandbox:   sampleSandbox,
		RawData:   `{"name":"sbx-sample"}`,
		Namespace: config.Cfg.SandboxNamespace,
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, tplData); err != nil {
		return fmt.Errorf("failed to render sandbox blueprint: %v", err)
	}

	rsObj := &appsv1.ReplicaSet{}
	if err := yaml.Unmarshal(buf.Bytes(), rsObj); err != nil {
		return fmt.Errorf("rendered sandbox blueprint is not valid ReplicaSet YAML: %v", err)
	}

	return nil
}

func (a *Handler) SaveSandboxBlueprintConfig(r *http.Request) (interface{}, error) {
	contentBytes, err := io.ReadAll(r.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read request body: %v", err)
	}

	content := string(contentBytes)
	if strings.TrimSpace(content) == "" {
		return "", fmt.Errorf("sandbox blueprint content is required")
	}

	if err := validateSandboxBlueprintContent(content); err != nil {
		return "", err
	}

	if err := config.Cfg.SaveSandboxBlueprintToCM(content); err != nil {
		return "", fmt.Errorf("failed to save sandbox blueprint config error: %v", err)
	}

	return "ok", nil
}

// ------------------------------------------------------
// Pool handlers
// ------------------------------------------------------

// ListPool returns the list of pool, list exist rs by template name, include pool size is change to 0
func (a *Handler) ListPool(r *http.Request) (interface{}, error) {
	poolTemplates := []*config.Template{}

	rss, err := a.controller.ListAllPool()
	if err != nil {
		return poolTemplates, fmt.Errorf("failed to list pool: %v", err)
	}

	// get all templates in rss  and count templates by name to readySize
	templateCount := make(map[string]int)
	for _, rs := range rss {
		tplName := rs.Template
		templateCount[tplName]++
	}

	for tplName, count := range templateCount {
		tpl, _ := config.GetTemplateByName(tplName)
		tpl.Pool.ReadySize = count
		poolTemplates = append(poolTemplates, tpl)
	}

	return poolTemplates, nil
}

func (a *Handler) ListPoolSandbox(r *http.Request) (interface{}, error) {
	name := r.PathValue("name")
	if name == "" {
		return nil, fmt.Errorf("pool name is required")
	}

	sbs, err := a.controller.ListPoolSandbox(name)
	if err != nil {
		return "", fmt.Errorf("no pool sandboxes found %v", err)
	}

	return sbs, nil
}

func (a *Handler) DeletePoolSandboxes(r *http.Request) (interface{}, error) {
	name := r.PathValue("name")
	if name == "" {
		return nil, fmt.Errorf("pool name is required")
	}

	err := a.controller.DeleteByTemplateName(name)
	if err != nil {
		return "", fmt.Errorf("pool sandboxes delete error %v", err)
	}

	return "ok", nil
}
