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
	"encoding/json"

	"k8s.io/klog/v2"
)

type UserRateLimitConfig struct {
	User           string `json:"user"`
	MaxConcurrency int    `json:"max_concurrency"`
	MaxSandbox     int    `json:"max_sandbox"`
}

type RateLimitConfig struct {
	Enabled        bool `json:"enabled" split_words:"true" default:"false"`
	MaxConcurrency int  `json:"max_concurrency" split_words:"true" default:"10"`
	MaxSandbox     int  `json:"max_sandbox" split_words:"true" default:"100"`
}

func (c *RateLimitConfig) UnmarshalJSON(data []byte) error {
	type rateLimitConfig RateLimitConfig
	var raw struct {
		rateLimitConfig
		EnabledLegacy        *bool `json:"Enabled"`
		MaxConcurrencyLegacy *int  `json:"MaxConcurrency"`
		MaxSandboxLegacy     *int  `json:"MaxSandbox"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	*c = RateLimitConfig(raw.rateLimitConfig)
	if raw.EnabledLegacy != nil {
		c.Enabled = *raw.EnabledLegacy
	}
	if raw.MaxConcurrencyLegacy != nil {
		c.MaxConcurrency = *raw.MaxConcurrencyLegacy
	}
	if raw.MaxSandboxLegacy != nil {
		c.MaxSandbox = *raw.MaxSandboxLegacy
	}
	return nil
}

func (c *RateLimitConfig) Validate() {
	if c.MaxConcurrency < 0 {
		klog.Warningf("MaxConcurrency invalid (%d), using default 10", c.MaxConcurrency)
		c.MaxConcurrency = 10
	}
	if c.MaxSandbox < 0 {
		klog.Warningf("MaxSandbox invalid (%d), using default 100", c.MaxSandbox)
		c.MaxSandbox = 100
	}
}

func (c *UserRateLimitConfig) Validate() bool {
	if c.User == "" {
		return false
	}
	if c.MaxConcurrency < 0 {
		klog.Warningf("UserRateLimitConfig.MaxConcurrency negative for user=%s, will use default", c.User)
		c.MaxConcurrency = 0
	}
	if c.MaxSandbox < 0 {
		klog.Warningf("UserRateLimitConfig.MaxSandbox negative for user=%s, will use default", c.User)
		c.MaxSandbox = 0
	}
	return true
}
