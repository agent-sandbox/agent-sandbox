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

package scaler

import (
	"time"

	"github.com/agent-sandbox/agent-sandbox/pkg/config"
	"github.com/agent-sandbox/agent-sandbox/pkg/sandbox"
	"github.com/agent-sandbox/agent-sandbox/pkg/telemetry"
	"k8s.io/klog/v2"
)

func (s *Scaler) ScalingDownOfTimeout(sbs []*sandbox.Sandbox) {
	for _, sb := range sbs {
		baseTime := sb.CreatedAt
		if resumedAt := sb.ReplicaSet.Annotations[sandbox.AnnotationResumedAt]; resumedAt != "" {
			if t, err := time.Parse(time.RFC3339, resumedAt); err == nil {
				baseTime = t
			}
		}

		timeout := sb.Timeout
		if timeout <= 0 {
			continue
		}

		if sb.Status == sandbox.Paused {
			continue
		}

		if time.Since(baseTime) < ScalingCheckInterval {
			continue
		}

		tt := baseTime.Add(time.Duration(timeout) * time.Second)
		if tt.Before(time.Now()) {
			if sb.AutoPause && config.CheckFeature(config.Cfg.PauseResume, sb.User) {
				klog.Infof("Paused sandbox %s TimeoutBase %s Timeout %v", sb.Name, baseTime, sb.Timeout)
				if err := s.controller.Pause(sb, "timeout"); err != nil {
					klog.Errorf("Failed to pause sandbox %v, error %v", sb, err)
					continue
				}
			} else {
				klog.Infof("Scaled down sandbox %s TimeoutBase %s Timeout %v IdleTimeout %v", sb.Name, baseTime, sb.Timeout, sb.IdleTimeout)
				if err := s.controller.DeleteWithReason(sb.Name, telemetry.ReasonTimeout); err != nil {
					klog.Errorf("Failed to scale down sandbox %v, error %v", sb, err)
					continue
				}
			}

			// to reduce kube-apiserver pressure, QPS not over 100
			time.Sleep(10 * time.Millisecond)
		}

	}

}
