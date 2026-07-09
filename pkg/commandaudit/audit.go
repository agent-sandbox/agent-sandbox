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

package commandaudit

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"path"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const defaultStoreSize = 500

type contextKey struct{}

type SandboxInfo struct {
	TenantID    string
	SandboxID   string
	SandboxName string
}

type Record struct {
	ID          uint64    `json:"id"`
	TenantID    string    `json:"tenant_id,omitempty"`
	SandboxID   string    `json:"sandbox_id,omitempty"`
	SandboxName string    `json:"sandbox_name"`
	Source      string    `json:"source"`
	CommandText string    `json:"command_text"`
	Cwd         string    `json:"cwd,omitempty"`
	ObservedAt  time.Time `json:"observed_at"`
	Detail      string    `json:"detail,omitempty"`
}

type ListOptions struct {
	TenantID    string
	SandboxName string
	Search      string
	From        time.Time
	To          time.Time
	Limit       int
}

type Store struct {
	mu     sync.RWMutex
	max    int
	nextID uint64
	items  []Record
}

var DefaultStore = NewStore(defaultStoreSize)

func NewStore(max int) *Store {
	if max <= 0 {
		max = defaultStoreSize
	}
	return &Store{max: max}
}

func ContextWithSandboxInfo(ctx context.Context, info SandboxInfo) context.Context {
	return context.WithValue(ctx, contextKey{}, info)
}

func SandboxInfoFromContext(ctx context.Context) SandboxInfo {
	if ctx == nil {
		return SandboxInfo{}
	}
	info, _ := ctx.Value(contextKey{}).(SandboxInfo)
	return info
}

func RecordProxyRequest(r *http.Request, info SandboxInfo, upstreamPath, port string) error {
	if r == nil || DefaultStore == nil {
		return nil
	}
	if strings.ToUpper(strings.TrimSpace(r.Method)) != http.MethodPost || !isProcessStartPath(upstreamPath) {
		return nil
	}
	if r.Body == nil {
		return nil
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		return err
	}
	_ = r.Body.Close()
	resetRequestBody(r, body)

	record, ok := buildProcessStartRecord(info, upstreamPath, port, body)
	if !ok {
		return nil
	}
	DefaultStore.Record(record)
	return nil
}

func (s *Store) Record(record Record) {
	if s == nil {
		return
	}
	record.TenantID = strings.TrimSpace(record.TenantID)
	record.SandboxID = strings.TrimSpace(record.SandboxID)
	record.SandboxName = strings.TrimSpace(record.SandboxName)
	record.Source = strings.TrimSpace(record.Source)
	record.CommandText = strings.TrimSpace(record.CommandText)
	record.Cwd = strings.TrimSpace(record.Cwd)
	if record.SandboxName == "" || record.CommandText == "" {
		return
	}
	if record.Source == "" {
		record.Source = "commands.run"
	}
	if record.ObservedAt.IsZero() {
		record.ObservedAt = time.Now().UTC()
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	s.nextID++
	record.ID = s.nextID
	s.items = append(s.items, record)
	if overflow := len(s.items) - s.max; overflow > 0 {
		copy(s.items, s.items[overflow:])
		s.items = s.items[:s.max]
	}
}

func (s *Store) List(opts ListOptions) []Record {
	if s == nil {
		return nil
	}
	limit := normalizeLimit(opts.Limit, 100, s.max)
	tenantID := strings.TrimSpace(opts.TenantID)
	sandboxName := strings.TrimSpace(opts.SandboxName)
	search := strings.ToLower(strings.TrimSpace(opts.Search))

	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make([]Record, 0, limit)
	for i := len(s.items) - 1; i >= 0 && len(result) < limit; i-- {
		item := s.items[i]
		if tenantID != "" && item.TenantID != tenantID {
			continue
		}
		if sandboxName != "" && item.SandboxName != sandboxName {
			continue
		}
		if search != "" && !strings.Contains(strings.ToLower(item.CommandText), search) {
			continue
		}
		if !opts.From.IsZero() && item.ObservedAt.Before(opts.From) {
			continue
		}
		if !opts.To.IsZero() && item.ObservedAt.After(opts.To) {
			continue
		}
		result = append(result, item)
	}
	return result
}

func (s *Store) SandboxNames(opts ListOptions) []string {
	if s == nil {
		return nil
	}
	limit := normalizeLimit(opts.Limit, 1000, 2000)
	tenantID := strings.TrimSpace(opts.TenantID)
	search := strings.ToLower(strings.TrimSpace(opts.Search))

	s.mu.RLock()
	defer s.mu.RUnlock()

	seen := make(map[string]struct{})
	names := make([]string, 0, limit)
	for i := len(s.items) - 1; i >= 0 && len(names) < limit; i-- {
		item := s.items[i]
		if tenantID != "" && item.TenantID != tenantID {
			continue
		}
		if item.SandboxName == "" {
			continue
		}
		if search != "" && !strings.Contains(strings.ToLower(item.SandboxName), search) {
			continue
		}
		if _, ok := seen[item.SandboxName]; ok {
			continue
		}
		seen[item.SandboxName] = struct{}{}
		names = append(names, item.SandboxName)
	}
	sort.Strings(names)
	return names
}

type processStartRequest struct {
	Process processConfig `json:"process"`
	Tag     string        `json:"tag,omitempty"`
	Stdin   bool          `json:"stdin,omitempty"`
}

type processConfig struct {
	Cmd  string            `json:"cmd,omitempty"`
	Args []string          `json:"args,omitempty"`
	Envs map[string]string `json:"envs,omitempty"`
	Cwd  string            `json:"cwd,omitempty"`
}

func buildProcessStartRecord(info SandboxInfo, upstreamPath, port string, body []byte) (Record, bool) {
	req, err := parseProcessStartBody(body)
	if err != nil {
		return Record{}, false
	}
	commandText := processCommandText(req.Process)
	if strings.TrimSpace(commandText) == "" {
		return Record{}, false
	}

	detail := map[string]interface{}{
		"path": cleanAuditPath(upstreamPath),
	}
	if port = strings.TrimSpace(port); port != "" && port != "0" {
		detail["port"] = port
	}
	if tag := strings.TrimSpace(req.Tag); tag != "" {
		detail["tag"] = tag
	}
	if req.Stdin {
		detail["stdin"] = true
	}
	if len(req.Process.Envs) > 0 {
		detail["env_count"] = len(req.Process.Envs)
	}
	detailJSON, _ := json.Marshal(detail)

	return Record{
		TenantID:    info.TenantID,
		SandboxID:   info.SandboxID,
		SandboxName: info.SandboxName,
		Source:      "commands.run",
		CommandText: commandText,
		Cwd:         req.Process.Cwd,
		ObservedAt:  time.Now().UTC(),
		Detail:      string(detailJSON),
	}, true
}

func parseProcessStartBody(body []byte) (processStartRequest, error) {
	var req processStartRequest
	body = bytes.TrimSpace(body)
	if len(body) == 0 {
		return req, fmt.Errorf("empty request body")
	}

	payload := body
	if body[0] != '{' {
		var ok bool
		payload, ok = connectJSONPayload(body)
		if !ok {
			return req, fmt.Errorf("request body is not plain JSON or connect JSON")
		}
	}
	if err := json.Unmarshal(payload, &req); err != nil {
		return req, err
	}
	return req, nil
}

func connectJSONPayload(body []byte) ([]byte, bool) {
	if len(body) < 5 {
		return nil, false
	}
	flags := body[0]
	if flags&0x01 != 0 {
		return nil, false
	}
	size := int(binary.BigEndian.Uint32(body[1:5]))
	if size <= 0 || size > len(body)-5 {
		return nil, false
	}
	payload := bytes.TrimSpace(body[5 : 5+size])
	if len(payload) == 0 || payload[0] != '{' {
		return nil, false
	}
	return payload, true
}

func processCommandText(process processConfig) string {
	cmd := strings.TrimSpace(process.Cmd)
	if script, ok := shellCommandScript(cmd, process.Args); ok {
		return strings.TrimSpace(script)
	}
	return strings.TrimSpace(joinCommandLine(append([]string{cmd}, process.Args...)))
}

func shellCommandScript(cmd string, args []string) (string, bool) {
	name := path.Base(strings.TrimSpace(cmd))
	switch name {
	case "sh", "bash", "zsh", "dash", "ksh":
	default:
		return "", false
	}

	for i, arg := range args {
		if arg == "-c" && i+1 < len(args) {
			return args[i+1], true
		}
		if strings.HasPrefix(arg, "-") && strings.Contains(arg, "c") && i+1 < len(args) {
			return args[i+1], true
		}
	}
	return "", false
}

func joinCommandLine(parts []string) string {
	quoted := make([]string, 0, len(parts))
	for _, part := range parts {
		quoted = append(quoted, shellQuoteArg(part))
	}
	return strings.Join(quoted, " ")
}

func shellQuoteArg(arg string) string {
	if arg == "" {
		return "''"
	}
	if !strings.ContainsAny(arg, " \t\r\n\"'\\$`!;&|<>(){}[]*?") {
		return arg
	}
	return "'" + strings.ReplaceAll(arg, "'", `'\''`) + "'"
}

func isProcessStartPath(value string) bool {
	value = strings.TrimRight(cleanAuditPath(value), "/")
	return value == "/process.Process/Start" || strings.HasSuffix(value, "/process.Process/Start")
}

func cleanAuditPath(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "/"
	}
	if !strings.HasPrefix(value, "/") {
		value = "/" + value
	}
	if value != "/" {
		value = strings.TrimRight(value, "/")
	}
	return value
}

func resetRequestBody(r *http.Request, body []byte) {
	r.Body = io.NopCloser(bytes.NewReader(body))
	r.ContentLength = int64(len(body))
	r.Header.Set("Content-Length", strconv.Itoa(len(body)))
}

func normalizeLimit(value, fallback, max int) int {
	if value <= 0 {
		value = fallback
	}
	if max > 0 && value > max {
		return max
	}
	return value
}
