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
	"time"

	e2bapi "github.com/agent-sandbox/agent-sandbox/pkg/api/e2b/api"
	"github.com/agent-sandbox/agent-sandbox/pkg/router"
	"k8s.io/klog/v2"
)

const defaultEnvdPort = "49983"

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
func (s *Controller) listEnvdProcesses(sb *Sandbox) ([]e2bapi.ProcessInfo, error) {
	payload, err := json.Marshal(e2bapi.ListRequest{})
	if err != nil {
		return nil, err
	}
	dest, err := router.AcquireDest(s.rootCtx, sb.Name, defaultEnvdPort)
	if err != nil {
		return nil, err
	}
	url := dest.JoinPath("/process.Process/List").String()
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-Access-Token", sb.ID)
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

func (s *Controller) restoreProcessSnapshot(sb *Sandbox, snapshot string) error {
	if snapshot == "" {
		return nil
	}
	payload, err := decodeProcessSnapshot(snapshot)
	if err != nil {
		return err
	}
	dest, err := router.AcquireDest(s.rootCtx, sb.Name, defaultEnvdPort)
	if err != nil {
		return err
	}
	processStartURL := dest.JoinPath("/process.Process/Start").String()
	for _, process := range payload.Processes {
		if err := s.startEnvdProcess(sb.ID, processStartURL, process); err != nil {
			return err
		}
	}
	return nil
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
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
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

	klog.Info("start envd process for ", processStartURL, ", with config ", process.Config)

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

	klog.Infof("envd process started, pid=%d", startResponse.Event.Start.PID)
	return nil
}
