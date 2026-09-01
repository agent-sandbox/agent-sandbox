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

package leader

import (
	"context"
	"os"
	"sync/atomic"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/leaderelection"
	"k8s.io/client-go/tools/leaderelection/resourcelock"
	"k8s.io/klog/v2"
)

// currentLeader holds the identity (typically pod name) of whichever replica
// currently holds the lease. Updated by leader election callbacks; read by
// status endpoints / UI.
var currentLeader atomic.Value // string

// Current returns the identity of the current leader, or "" if unknown yet.
func Current() string {
	if v := currentLeader.Load(); v != nil {
		return v.(string)
	}
	return ""
}

// Config controls the lease used for leader election.
type Config struct {
	// LeaseName is the K8s Lease object name. Must match across replicas.
	LeaseName string
	// Namespace where the Lease lives. Typically the agent-sandbox namespace.
	Namespace string
	// Identity uniquely identifies this replica. Defaults to HOSTNAME.
	Identity string
	// Client is the K8s clientset used to read/write the Lease.
	Client kubernetes.Interface
}

// reelectBackoff is how long to wait after a lost-leadership cycle before
// re-entering the election. Short enough that recovery is fast, long enough
// that we don't hammer the API server on persistent failures.
const reelectBackoff = 5 * time.Second

// RunAsLeader blocks until ctx is done. Each time this replica is elected
// leader it invokes onStart with a child context that is canceled when
// leadership is lost. Re-enters the election after a lost-leadership cycle so
// this replica stays a viable leader candidate for the lifetime of the
// process.
//
// Lease parameters use conservative defaults:
//   - LeaseDuration: 30s (max time a leader can be partitioned and still be
//     considered leader)
//   - RenewDeadline: 20s (leader must renew within this window)
//   - RetryPeriod:    4s  (followers retry to acquire the lease this often)
func RunAsLeader(ctx context.Context, cfg Config, onStart func(ctx context.Context)) {
	identity := cfg.Identity
	if identity == "" {
		identity = os.Getenv("HOSTNAME")
	}
	if identity == "" {
		h, _ := os.Hostname()
		identity = h
	}

	lock := &resourcelock.LeaseLock{
		LeaseMeta: metav1.ObjectMeta{
			Name:      cfg.LeaseName,
			Namespace: cfg.Namespace,
		},
		Client: cfg.Client.CoordinationV1(),
		LockConfig: resourcelock.ResourceLockConfig{
			Identity: identity,
		},
	}

	leConfig := leaderelection.LeaderElectionConfig{
		Lock:            lock,
		ReleaseOnCancel: true,
		LeaseDuration:   30 * time.Second,
		RenewDeadline:   20 * time.Second,
		RetryPeriod:     4 * time.Second,
		Callbacks: leaderelection.LeaderCallbacks{
			OnStartedLeading: func(c context.Context) {
				klog.Infof("Acquired leadership: identity=%s lease=%s/%s", identity, cfg.Namespace, cfg.LeaseName)
				onStart(c)
			},
			OnStoppedLeading: func() {
				klog.Warningf("Lost leadership: identity=%s lease=%s/%s", identity, cfg.Namespace, cfg.LeaseName)
				// Don't clear currentLeader here — OnNewLeader will fire with
				// the actual replacement once another replica acquires the
				// lease. Stale-by-a-few-seconds is better than empty.
			},
			OnNewLeader: func(newID string) {
				currentLeader.Store(newID)
				klog.V(1).Infof("New leader observed: identity=%s", newID)
			},
		},
	}

	// leaderelection.RunOrDie is one-shot: after a lost-leadership cycle it
	// returns and never re-elects. Wrap it so this replica keeps competing
	// for the lease as long as the process is alive.
	defer func() {
		klog.Warningf("Leader election goroutine cycle stopped: identity=%s reason=%v", identity, ctx.Err())
	}()

	for {
		if ctx.Err() != nil {
			return
		}
		leaderelection.RunOrDie(ctx, leConfig)
		if ctx.Err() != nil {
			return
		}
		klog.Warningf("Leader election cycle ended (identity=%s); re-entering after %s", identity, reelectBackoff)
		select {
		case <-ctx.Done():
			return
		case <-time.After(reelectBackoff):
		}
	}
}
