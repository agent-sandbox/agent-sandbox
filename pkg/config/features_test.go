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

package config

import (
	"bytes"
	"flag"
	"io"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/klog/v2"
)

func TestCheckFeatureDoesNotLogAPIKeys(t *testing.T) {
	buf := captureKlog(t, "0")

	prev := Cfg
	defer func() { Cfg = prev }()
	Cfg = &Config{FeatureConfig: FeatureConfig{ExperimentTokens: "other-0123456789abcdef,user-fedcba9876543210"}}

	if !CheckFeature(true, "user-fedcba9876543210") {
		t.Errorf("CheckFeature = false, want true for a listed user")
	}
	klog.Flush()

	out := buf.String()
	if !strings.Contains(out, "ExperimentTokens") {
		t.Fatalf("CheckFeature did not log: %q", out)
	}
	for _, key := range []string{"other-0123456789abcdef", "user-fedcba9876543210"} {
		if strings.Contains(out, key) {
			t.Errorf("log contains the full key %q: %q", key, out)
		}
	}
}

func TestWatchConfigMapDoesNotLogRuntimeConfig(t *testing.T) {
	buf := captureKlog(t, "0")

	prev := Cfg
	defer func() { Cfg = prev }()
	Cfg = &Config{}

	WatchConfigMap()(&corev1.ConfigMap{Data: map[string]string{
		RuntimeConfigMapKey: `{"system_token":"sys-secret0001","api_tokens_raw":"user-secret0002"}`,
	}})
	klog.Flush()

	out := buf.String()
	if !strings.Contains(out, "runtime config updated") {
		t.Fatalf("runtime config change was not logged: %q", out)
	}
	if strings.Contains(out, "secret") {
		t.Errorf("log contains a token: %q", out)
	}
	if got := Cfg.GetSystemToken(); got != "sys-secret0001" {
		t.Errorf("runtime config not applied: SystemToken = %q", got)
	}
}

// captureKlog sends klog output at verbosity v to the returned buffer and
// restores klog's defaults (stderr, -v=0) when the test ends.
func captureKlog(t *testing.T, v string) *bytes.Buffer {
	t.Helper()
	fs := flag.NewFlagSet("klog", flag.ContinueOnError)
	klog.InitFlags(fs)
	_ = fs.Set("v", v)
	_ = fs.Set("logtostderr", "false")
	_ = fs.Set("alsologtostderr", "false")
	var buf bytes.Buffer
	klog.SetOutput(&buf)
	t.Cleanup(func() {
		klog.Flush()
		klog.SetOutput(io.Discard)
		_ = fs.Set("v", "0")
		klog.LogToStderr(true)
	})
	return &buf
}
