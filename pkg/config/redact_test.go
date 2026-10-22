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
	"fmt"
	"strings"
	"testing"
)

func TestConfigStringRedactsTokens(t *testing.T) {
	c := &Config{
		SystemToken:    "sys-secret0001",
		APITokens:      []string{"sys-secret0001", "user-secret0002"},
		APITokensRaw:   "user-secret0002",
		RateLimitUsers: []UserRateLimitConfig{{User: "user-secret0002", MaxSandbox: 5}},
		ServerAddr:     "0.0.0.0:10000",
		FeatureConfig:  FeatureConfig{ExperimentTokens: "user-secret0002"},
	}
	c.RateLimitUsersRaw = `[{"user":"user-secret0002"}]`

	got := fmt.Sprintf("Loaded config %+v", c)

	if strings.Contains(got, "secret") {
		t.Errorf("config output contains a token: %s", got)
	}
	for _, want := range []string{"ServerAddr:0.0.0.0:10000", "SystemToken:" + Redacted, "APITokens:2", "RateLimitUsers:1"} {
		if !strings.Contains(got, want) {
			t.Errorf("config output missing %q: %s", want, got)
		}
	}
}

func TestRedactEnvVars(t *testing.T) {
	env := map[string]string{"OPENAI_API_KEY": "sk-secret"}

	got := RedactEnvVars(env)

	if got["OPENAI_API_KEY"] != Redacted {
		t.Errorf("RedactEnvVars = %v, want the name kept and the value redacted", got)
	}
	if env["OPENAI_API_KEY"] != "sk-secret" {
		t.Errorf("original env modified: %v", env)
	}
	if RedactEnvVars(nil) != nil {
		t.Errorf("RedactEnvVars(nil) should be nil")
	}
}
