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
	"slices"
	"strings"

	"k8s.io/klog/v2"
)

type FeatureConfig struct {
	ExperimentTokens string `split_words:"true" default:"testuser-aef134ef-7aa1-945e-9399-7df9a4ad0c3f" required:"false"`

	PauseResume bool `split_words:"true" required:"false"`

	PoolEnable bool `split_words:"true" required:"false"`
}

// CheckFeature checks is experiment user. If not set, returns true for all users.
func CheckFeature(feature bool, user string) bool {
	if !feature {
		return false
	}

	tokens := strings.Split(Cfg.ExperimentTokens, ",")
	if len(tokens) > 0 {
		r := slices.Contains(tokens, user)
		klog.Infof("ExperimentTokens: %v, user: %v", tokens, user)
		return r
	}

	klog.Infof("ExperimentTokens is empty, enabling all features for user %s", user)
	return true
}
