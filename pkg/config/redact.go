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

import "fmt"

// Redacted replaces secret values in logs and other %v output.
const Redacted = "[REDACTED]"

// MaskUserKey keeps the leading 2/3 of a user key (matches pkg/telemetry's
// maskUser). User keys are API keys, so anything logged or exposed publicly
// goes through this instead of the raw value.
func MaskUserKey(s string) string {
	n := len(s) * 2 / 3
	if n < 1 {
		return s
	}
	return s[:n]
}

// RedactEnvVars returns a copy of env for logging, keeping the names and
// replacing every value: sandbox env vars routinely carry users' own secrets.
func RedactEnvVars(env map[string]string) map[string]string {
	if env == nil {
		return nil
	}
	out := make(map[string]string, len(env))
	for k := range env {
		out[k] = Redacted
	}
	return out
}

func redactIfSet(s string) string {
	if s == "" {
		return ""
	}
	return Redacted
}

// String keeps API tokens out of %v output, e.g. the startup "Loaded config" log.
// Token lists and per-user settings (keyed by API key) are reported as counts,
// single tokens as [REDACTED].
func (c *Config) String() string {
	if c == nil {
		return "<nil>"
	}
	return fmt.Sprintf("{LeaderName:%s APIVersion:%s APIBaseURL:%s ServerAddr:%s SystemToken:%s APITokens:%d "+
		"RateLimit:%+v RateLimitUsers:%d Telemetry:%+v SandboxNamespace:%s SandboxBlueprintFile:%s "+
		"SandboxTemplatesConfigFile:%s SandboxDefaultImage:%s SandboxDefaultTemplate:%s ConfigmapName:%s "+
		"EnvName:%s LogLevel:%s ExperimentTokens:%s PauseResume:%v PoolEnable:%v}",
		c.LeaderName, c.APIVersion, c.APIBaseURL, c.ServerAddr, redactIfSet(c.SystemToken), len(c.APITokens),
		c.RateLimit, len(c.RateLimitUsers), c.Telemetry, c.SandboxNamespace, c.SandboxBlueprintFile,
		c.SandboxTemplatesConfigFile, c.SandboxDefaultImage, c.SandboxDefaultTemplate, c.ConfigmapName,
		c.EnvName, c.LogLevel, redactIfSet(c.ExperimentTokens), c.PauseResume, c.PoolEnable)
}
