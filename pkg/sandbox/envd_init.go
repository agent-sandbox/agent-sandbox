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
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/agent-sandbox/agent-sandbox/pkg/router"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/klog/v2"
)

// envdInitTimeout bounds how long create and resume wait for envd to accept /init.
// It is short on purpose: init is best effort and must not noticeably delay
// sandboxes whose image does not run envd.
const envdInitTimeout = 5 * time.Second

// errEnvdTokenConflict means envd already holds a different access token, which
// retrying cannot fix.
var errEnvdTokenConflict = errors.New("envd already initialized with a different access token")

// envdInitRequest is the body of envd's POST /init.
type envdInitRequest struct {
	EnvVars     map[string]string `json:"envVars,omitempty"`
	AccessToken string            `json:"accessToken,omitempty"`
}

// envdAccessToken is the token envd is initialized with. It is the sandbox ID,
// which the E2B API returns to SDKs as envdAccessToken and which the platform's
// own envd calls (process snapshot) send.
func envdAccessToken(sb *Sandbox) string {
	return sb.ID
}

// initEnvd sends envd the sandbox access token and env vars through POST /init.
//
// envd only learns both through /init: it ignores the container environment
// (only PATH is inherited by processes) and, with no token set, accepts every
// request without authentication. It runs on every new pod, i.e. after create
// and after resume. /init is idempotent for the same token, so concurrent
// callers are safe.
//
// Callers treat a failure as a warning only: images without envd, or an envd
// that starts late, keep the previous behavior instead of failing the sandbox.
func (s *Controller) initEnvd(sb *Sandbox) error {
	dest, err := router.AcquireDest(s.rootCtx, sb.Name, defaultEnvdPort)
	if err != nil {
		return fmt.Errorf("acquire envd address: %w", err)
	}

	token := envdAccessToken(sb)
	payload, err := json.Marshal(envdInitRequest{EnvVars: sb.EnvVars, AccessToken: token})
	if err != nil {
		return err
	}

	// envd may start after the pod is Ready, so retry until it answers.
	start := time.Now()
	var lastErr error
	err = wait.PollUntilContextTimeout(context.Background(), envdRetryInterval, envdInitTimeout, true, func(ctx context.Context) (bool, error) {
		lastErr = postEnvdInit(dest, token, payload)
		if errors.Is(lastErr, errEnvdTokenConflict) {
			return false, lastErr
		}
		return lastErr == nil, nil
	})
	if err != nil {
		if errors.Is(lastErr, errEnvdTokenConflict) {
			return lastErr
		}
		return fmt.Errorf("envd not initialized within %v: %v", envdInitTimeout, lastErr)
	}

	// Env var values may hold secrets; log the count only.
	klog.Infof("Initialized envd of sandbox %s with %d env vars after %v", sb.Name, len(sb.EnvVars), time.Since(start))
	return nil
}

// postEnvdInit performs one POST /init attempt.
func postEnvdInit(dest *url.URL, token string, payload []byte) error {
	ctx, cancel := context.WithTimeout(context.Background(), envdRequestTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, dest.JoinPath("/init").String(), bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	// Older envd (e.g. 0.2.10) authenticates /init by header once a token is set,
	// newer envd by the token in the body; sending both covers repeated inits on either.
	req.Header.Set("X-Access-Token", token)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	switch {
	case resp.StatusCode >= http.StatusOK && resp.StatusCode < http.StatusMultipleChoices:
		return nil
	case resp.StatusCode == http.StatusConflict || resp.StatusCode == http.StatusUnauthorized:
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("envd init %w: status %d, body %s", errEnvdTokenConflict, resp.StatusCode, body)
	default:
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("envd init returned status %d, body %s", resp.StatusCode, body)
	}
}
