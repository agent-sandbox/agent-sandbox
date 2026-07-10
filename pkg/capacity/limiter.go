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

package capacity

import (
	"fmt"
	"net/http"
	"sort"

	"github.com/agent-sandbox/agent-sandbox/pkg/config"
	"k8s.io/klog/v2"
)

var ErrRateLimitExceeded = fmt.Errorf("rate limit exceeded: too many concurrent sandbox creation requests")

var ErrCapacityExceeded = fmt.Errorf("capacity exceeded: maximum sandbox limit reached")

// sandboxController is the slice of the sandbox controller we depend on. The
// concurrency limit reads CountByUserInCreate (informer-backed) so that the
// limit holds across replicas — we can't keep an in-memory counter in a
// multi-instance deployment.
type sandboxController interface {
	CountSandboxes(user string) (int, error)
	CountByUserInCreate(user string) (int, error)
	CountAllByUser() (map[string]int, error)
}

type UserConfig struct {
	User           string `json:"user"`
	MaxConcurrency int    `json:"max_concurrency"`
	MaxSandbox     int    `json:"max_sandbox"`
}

type LimitError struct {
	Code    int
	Message string
}

func (e *LimitError) Error() string {
	return e.Message
}

type RateLimiter struct {
	controller sandboxController
}

var GlobalLimiter *RateLimiter

func Init(controller sandboxController) {
	GlobalLimiter = NewRateLimiter(controller)
}

func NewRateLimiter(controller sandboxController) *RateLimiter {
	if controller == nil {
		klog.Fatal("Rate limiter requires a non-nil sandbox controller")
	}
	klog.Info("Rate limiter initialized")
	return &RateLimiter{controller: controller}
}

func defaultConfig() config.RateLimitConfig {
	if config.Cfg == nil {
		return config.RateLimitConfig{
			Enabled:        true,
			MaxConcurrency: 3,
			MaxSandbox:     100,
		}
	}
	return config.Cfg.GetRateLimit()
}

func userConfigs() []config.UserRateLimitConfig {
	if config.Cfg == nil {
		return nil
	}
	return config.Cfg.GetRateLimitUsers()
}

func (rl *RateLimiter) getUserConfig(user string) (maxConcurrency, maxSandbox int) {
	cfg := defaultConfig()
	maxConcurrency = cfg.MaxConcurrency
	maxSandbox = cfg.MaxSandbox

	for _, userCfg := range userConfigs() {
		if userCfg.User != user {
			continue
		}
		if userCfg.MaxConcurrency > 0 {
			maxConcurrency = userCfg.MaxConcurrency
		}
		if userCfg.MaxSandbox > 0 {
			maxSandbox = userCfg.MaxSandbox
		}
		break
	}
	return
}

func (rl *RateLimiter) CheckCapacity(user string) (bool, int, int, error) {
	if !rl.Enabled() {
		return false, 0, 0, nil
	}

	_, maxSandbox := rl.getUserConfig(user)
	if maxSandbox <= 0 {
		return false, 0, 0, nil
	}

	currentCount, err := rl.controller.CountSandboxes(user)
	if err != nil {
		return false, 0, 0, err
	}

	return currentCount >= maxSandbox, maxSandbox, currentCount, nil
}

// TryAcquire checks whether the user is below their concurrency limit. The
// "current count" is the number of RSs whose status is still Creating,
// read from the informer cache — so the limit holds across all replicas
// without an in-process counter.
//
// This is eventually consistent: two replicas hitting the check at the same
// time can each see currentCount==max-1 and both admit a request, so the
// effective limit may briefly exceed max by 1. That's acceptable for a soft
// concurrency cap.
func (rl *RateLimiter) TryAcquire(user string) (acquired bool, maxConcurrency int, currentCount int, err error) {
	if !rl.Enabled() {
		return true, 0, 0, nil
	}

	maxConcurrency, _ = rl.getUserConfig(user)
	if maxConcurrency <= 0 {
		return true, 0, 0, nil
	}

	currentCount, err = rl.controller.CountByUserInCreate(user)
	if err != nil {
		return true, maxConcurrency, 0, err
	}

	if currentCount >= maxConcurrency {
		return false, maxConcurrency, currentCount, nil
	}
	return true, maxConcurrency, currentCount, nil
}

func (rl *RateLimiter) AcquireCreate(user string) (func(), error) {
	if !rl.Enabled() {
		return nil, nil
	}

	exceeded, maxSandbox, currentCount, err := rl.CheckCapacity(user)
	if err != nil {
		klog.Warningf("Failed to check capacity for user=%s: %v, allowing request", user, err)
	} else if exceeded {
		klog.Warningf("Capacity exceeded for user=%s: current=%d, max=%d", user, currentCount, maxSandbox)
		return nil, &LimitError{
			Code:    http.StatusRequestEntityTooLarge,
			Message: fmt.Sprintf("capacity exceeded: you have reached the maximum sandbox limit (%d/%d), please delete some sandboxes before creating new ones", currentCount, maxSandbox),
		}
	}

	acquired, maxConcurrency, currentCount, err := rl.TryAcquire(user)
	if err != nil {
		klog.Warningf("Rate limit check failed for user=%s: %v, allowing request", user, err)
		return nil, nil
	}
	if !acquired {
		klog.Warningf("Rate limit exceeded for user=%s: current=%d, max=%d", user, currentCount, maxConcurrency)
		return nil, &LimitError{
			Code:    http.StatusTooManyRequests,
			Message: fmt.Sprintf("rate limit exceeded: too many concurrent sandbox creation requests (%d/%d), please retry later", currentCount, maxConcurrency),
		}
	}

	return nil, nil
}

func (rl *RateLimiter) ConcurrencyStats(user string) (concurrencyActive int, concurrencyMax int) {
	if !rl.Enabled() {
		return 0, 0
	}

	concurrencyMax, _ = rl.getUserConfig(user)
	if n, err := rl.controller.CountByUserInCreate(user); err == nil {
		concurrencyActive = n
	}
	return
}

func (rl *RateLimiter) CountAllByUser() (map[string]int, error) {
	return rl.controller.CountAllByUser()
}

func (rl *RateLimiter) DefaultConfig() *config.RateLimitConfig {
	cfg := defaultConfig()
	return &cfg
}

func (rl *RateLimiter) UserConfig(user string) (maxConcurrency, maxSandbox int) {
	return rl.getUserConfig(user)
}

func (rl *RateLimiter) UserConfigs() []UserConfig {
	configs := []UserConfig{}
	seen := map[string]struct{}{}
	for _, userCfg := range userConfigs() {
		if userCfg.User == "" {
			continue
		}
		if _, ok := seen[userCfg.User]; ok {
			continue
		}
		seen[userCfg.User] = struct{}{}
		maxConcurrency, maxSandbox := rl.getUserConfig(userCfg.User)
		configs = append(configs, UserConfig{
			User:           userCfg.User,
			MaxConcurrency: maxConcurrency,
			MaxSandbox:     maxSandbox,
		})
	}
	sort.Slice(configs, func(i, j int) bool {
		return configs[i].User < configs[j].User
	})
	return configs
}

func (rl *RateLimiter) Enabled() bool {
	return defaultConfig().Enabled
}
