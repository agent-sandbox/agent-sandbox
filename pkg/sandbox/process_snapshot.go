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
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	e2bapi "github.com/agent-sandbox/agent-sandbox/pkg/api/e2b/api"
	"github.com/agent-sandbox/agent-sandbox/pkg/router"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/klog/v2"
)

const (
	defaultEnvdPort = "49983"

	// envd may start after the pod is Ready: the startupProbe checks the template port,
	// and fast-startup templates have no probe at all. Restore retries its first envd call
	// until envd answers or envdReadyTimeout elapses.
	envdReadyTimeout   = 30 * time.Second
	envdRetryInterval  = 500 * time.Millisecond
	envdRequestTimeout = 5 * time.Second
)

type processSnapshotPayload struct {
	CapturedTime time.Time            `json:"captured_time"`
	Processes    []e2bapi.ProcessInfo `json:"processes"`
}

// captureProcessSnapshot reads envd-managed processes and returns a base64-encoded JSON snapshot.
func (s *Controller) captureProcessSnapshot(sb *Sandbox) (string, error) {
	processes, err := s.listEnvdProcesses(sb)
	if err != nil {
		return "", err
	}
	return encodeProcessSnapshot(processes)
}

// listEnvdProcesses calls envd's process.Process/List endpoint for the running sandbox.
// TODO filter background process by special Env ,e.g.: sbx.commands.run("python server.py", background=True, envs={"SBX_BACKGROUND": "1"})
func (s *Controller) listEnvdProcesses(sb *Sandbox) ([]e2bapi.ProcessInfo, error) {
	payload, err := json.Marshal(e2bapi.ListRequest{})
	if err != nil {
		return nil, err
	}
	dest, err := router.AcquireDest(s.rootCtx, sb.Name, defaultEnvdPort)
	if err != nil {
		return nil, err
	}
	return listEnvdProcessesAt(dest, sb.ID, payload)
}

func listEnvdProcessesAt(dest *url.URL, token string, payload []byte) ([]e2bapi.ProcessInfo, error) {
	ctx, cancel := context.WithTimeout(context.Background(), envdRequestTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, dest.JoinPath("/process.Process/List").String(), bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-Access-Token", token)
	req.Header.Set("Connect-Protocol-Version", "1")
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("envd process list returned status %d", resp.StatusCode)
	}
	var response e2bapi.ListResponse
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return nil, err
	}
	return response.Processes, nil
}

// encodeProcessSnapshot serializes the filtered process list and returns annotation-safe base64 JSON.
func encodeProcessSnapshot(processes []e2bapi.ProcessInfo) (string, error) {
	payload := processSnapshotPayload{
		CapturedTime: time.Now().UTC(),
		Processes:    processes,
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(raw), nil
}

func decodeProcessSnapshot(snapshot string) (*processSnapshotPayload, error) {
	raw, err := base64.StdEncoding.DecodeString(snapshot)
	if err != nil {
		return nil, err
	}
	var payload processSnapshotPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, err
	}
	return &payload, nil
}

// restoreResult summarizes a snapshot restore for logs, events and telemetry.
type restoreResult struct {
	Total   int
	Started int
	Skipped int
}

func (r restoreResult) String() string {
	return fmt.Sprintf("total=%d started=%d skipped(already running)=%d", r.Total, r.Started, r.Skipped)
}

// restoreProcessSnapshot waits for envd to answer, then starts every snapshotted process
// that is not already running, so a repeated restore does not start duplicates.
func (s *Controller) restoreProcessSnapshot(sb *Sandbox, snapshot string) (restoreResult, error) {
	var result restoreResult
	if snapshot == "" {
		return result, nil
	}
	payload, err := decodeProcessSnapshot(snapshot)
	if err != nil {
		return result, fmt.Errorf("decode process snapshot: %w", err)
	}
	result.Total = len(payload.Processes)
	if result.Total == 0 {
		klog.Infof("Process snapshot of sandbox %s is empty, nothing to restore", sb.Name)
		return result, nil
	}

	dest, err := router.AcquireDest(s.rootCtx, sb.Name, defaultEnvdPort)
	if err != nil {
		return result, fmt.Errorf("acquire envd address: %w", err)
	}
	readyStart := time.Now()
	running, err := waitEnvdAndListProcesses(dest, sb.ID)
	if err != nil {
		return result, err
	}
	klog.Infof("envd of sandbox %s answered after %v, %d processes running, restoring %d", sb.Name, time.Since(readyStart), len(running), result.Total)
	runningKeys := map[string]bool{}
	for _, p := range running {
		for _, k := range processKeys(p) {
			runningKeys[k] = true
		}
	}

	processStartURL := dest.JoinPath("/process.Process/Start").String()
	for i, process := range payload.Processes {
		if isProcessRunning(runningKeys, process) {
			result.Skipped++
			klog.Infof("Skip restoring process %d/%d of sandbox %s, already running: %s", i+1, result.Total, sb.Name, describeProcess(process))
			continue
		}
		if err := s.startEnvdProcess(sb.ID, processStartURL, process); err != nil {
			return result, fmt.Errorf("start process %d/%d (%s): %w", i+1, result.Total, describeProcess(process), err)
		}
		result.Started++
	}
	return result, nil
}

// waitEnvdAndListProcesses lists envd's running processes, retrying until envd answers or
// envdReadyTimeout elapses. The list doubles as the readiness check, so no health endpoint is needed.
func waitEnvdAndListProcesses(dest *url.URL, token string) ([]e2bapi.ProcessInfo, error) {
	payload, _ := json.Marshal(e2bapi.ListRequest{})
	var running []e2bapi.ProcessInfo
	var lastErr error
	err := wait.PollUntilContextTimeout(context.Background(), envdRetryInterval, envdReadyTimeout, true, func(ctx context.Context) (bool, error) {
		running, lastErr = listEnvdProcessesAt(dest, token, payload)
		return lastErr == nil, nil
	})
	if err != nil {
		return nil, fmt.Errorf("envd not ready within %v: %v", envdReadyTimeout, lastErr)
	}
	return running, nil
}

// processKeys identifies a process by tag (when set) and by its command line.
func processKeys(p e2bapi.ProcessInfo) []string {
	keys := []string{"cmd:" + p.Config.Cmd + "\x00" + strings.Join(p.Config.Args, "\x00") + "\x00" + p.Config.Cwd}
	if p.Tag != "" {
		keys = append(keys, "tag:"+p.Tag)
	}
	return keys
}

func isProcessRunning(runningKeys map[string]bool, p e2bapi.ProcessInfo) bool {
	for _, k := range processKeys(p) {
		if runningKeys[k] {
			return true
		}
	}
	return false
}

// describeProcess renders a process for logs without its env vars, which may hold secrets.
func describeProcess(p e2bapi.ProcessInfo) string {
	return fmt.Sprintf("cmd=%q args=%q cwd=%q tag=%q", p.Config.Cmd, p.Config.Args, p.Config.Cwd, p.Tag)
}

// summarizeSnapshot describes a snapshot for logs and telemetry; the raw snapshot includes process env vars.
func summarizeSnapshot(snapshot string) string {
	if snapshot == "" {
		return "none"
	}
	payload, err := decodeProcessSnapshot(snapshot)
	if err != nil {
		return "invalid"
	}
	return fmt.Sprintf("%d processes captured at %s", len(payload.Processes), payload.CapturedTime.Format(time.RFC3339))
}

// for application/connect+json
func encodeConnectJSON(v any) ([]byte, error) {
	payload, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	body := make([]byte, 5+len(payload))
	binary.BigEndian.PutUint32(body[1:5], uint32(len(payload)))
	copy(body[5:], payload)
	return body, nil
}

func readConnectMessage(r io.Reader) ([]byte, byte, error) {
	header := make([]byte, 5)
	if _, err := io.ReadFull(r, header); err != nil {
		return nil, 0, err
	}
	msgLen := int(binary.BigEndian.Uint32(header[1:5]))
	message := make([]byte, msgLen)
	if _, err := io.ReadFull(r, message); err != nil {
		return nil, 0, err
	}
	return message, header[0], nil
}

func (s *Controller) startEnvdProcess(token string, processStartURL string, process e2bapi.ProcessInfo) error {
	payload, err := encodeConnectJSON(e2bapi.StartRequest{Process: process.Config, Tag: process.Tag})
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), envdRequestTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, processStartURL, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("X-Access-Token", token)
	req.Header.Set("Authorization", "Basic dXNlcjo=")
	req.Header.Set("Connect-Protocol-Version", "1")
	//req.Header.Set("Connect-Timeout-Ms", "0")
	req.Header.Set("Content-Type", "application/connect+json")

	klog.Infof("Starting envd process at %s: %s", processStartURL, describeProcess(process))

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("envd process start returned status %d, body  %s", resp.StatusCode, body)
	}

	message, flags, err := readConnectMessage(resp.Body)
	if err != nil {
		return err
	}
	if flags&0x02 != 0 {
		return fmt.Errorf("enavd process start returned end stream before start event: %s", message)
	}
	var startResponse e2bapi.StartResponse
	if err := json.Unmarshal(message, &startResponse); err != nil {
		return err
	}
	if startResponse.Event.Start.PID == 0 {
		return fmt.Errorf("envd process start response missing pid, body %s", message)
	}

	klog.Infof("envd process started, pid=%d: %s", startResponse.Event.Start.PID, describeProcess(process))
	return nil
}
