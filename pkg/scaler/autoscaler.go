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
	"context"
	"time"

	"github.com/agent-sandbox/agent-sandbox/pkg/activator"
	"github.com/agent-sandbox/agent-sandbox/pkg/sandbox"
	"k8s.io/klog/v2"
)

const (
	ScalingCheckInterval = 5 * time.Minute
)

type Scaler struct {
	rootCtx    context.Context
	activator  *activator.Activator
	controller *sandbox.Controller
}

func NewScaler(ctx context.Context, a *activator.Activator, c *sandbox.Controller) *Scaler {
	scaler := &Scaler{
		rootCtx:    ctx,
		activator:  a,
		controller: c,
	}
	return scaler
}

func (s *Scaler) RunScaling() {
	// Periodically check for sandboxes to scale down
	klog.Info("timeout and idle timeout  scaler started")
	ticker := time.NewTicker(ScalingCheckInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			sbs, err := s.controller.ListAll()
			if err == nil {
				s.ScalingDownOfTimeout(sbs)
				s.ScalingDownOfIdleTimeout(sbs)
			} else {
				klog.Error("Failed to list sandboxes for scaling down: ", err)
			}
		case <-s.rootCtx.Done():
			klog.Info("Scaler stopping")
			return
		}
	}
}
