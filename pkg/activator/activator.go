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

package activator

import (
	"context"
	"sync"
	"time"

	"github.com/agent-sandbox/agent-sandbox/pkg/config"
	"github.com/agent-sandbox/agent-sandbox/pkg/telemetry"
	appsv1 "k8s.io/api/apps/v1"
	coordinationv1 "k8s.io/api/coordination/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/util/retry"
	"k8s.io/klog/v2"
	kubeclient "knative.dev/pkg/client/injection/kube/client"
	rsclient "knative.dev/pkg/client/injection/kube/informers/apps/v1/replicaset"

	"k8s.io/client-go/tools/record"
)

const (
	ComponentName = "agent-sandbox-activator"
)

const (
	// TODO eventTypePaused
	EventTypePaused string = "SandboxPaused"
	EventTypeResume string = "SandboxResumed"

	recordLeaseInterval = 2 * time.Minute
)

type Activator struct {
	rootCtx            context.Context
	recorder           record.EventRecorder
	lastEventRecordAt  map[string]time.Time
	lastEventRecordMux sync.Mutex
}

func NewActivator(ctx context.Context, recorder record.EventRecorder) *Activator {
	a := &Activator{
		rootCtx:           ctx,
		recorder:          recorder,
		lastEventRecordAt: make(map[string]time.Time),
	}
	return a
}

func (a *Activator) Recorder() record.EventRecorder {
	return a.recorder
}

// RecordActive records that the given sandbox was just used, by renewing a
// per-sandbox Lease.
func (a *Activator) RecordActive(name string) {
	now := time.Now()

	a.lastEventRecordMux.Lock()
	lastAt, ok := a.lastEventRecordAt[name]
	if ok && now.Sub(lastAt) < recordLeaseInterval {
		a.lastEventRecordMux.Unlock()
		return
	}
	a.lastEventRecordAt[name] = now
	a.lastEventRecordMux.Unlock()

	go a.renewActiveLease(name, now)
}

// rollbackActiveRecord undoes the debounce entry set by RecordActive when the
// renewal ultimately fails, so the next request for this sandbox retries
// immediately instead of silently going quiet for a full recordLeaseInterval.
func (a *Activator) rollbackActiveRecord(name string, recordedAt time.Time) {
	a.lastEventRecordMux.Lock()
	if current, exists := a.lastEventRecordAt[name]; exists && current.Equal(recordedAt) {
		delete(a.lastEventRecordAt, name)
	}
	a.lastEventRecordMux.Unlock()
}

func (a *Activator) renewActiveLease(name string, now time.Time) {
	ns := config.Cfg.SandboxNamespace
	renewTime := metav1.NewMicroTime(now)
	leases := kubeclient.Get(a.rootCtx).CoordinationV1().Leases(ns)

	err := retry.OnError(retry.DefaultRetry, func(err error) bool {
		return apierrors.IsConflict(err) || apierrors.IsAlreadyExists(err)
	}, func() error {
		lease, getErr := leases.Get(a.rootCtx, name, metav1.GetOptions{})
		if apierrors.IsNotFound(getErr) {
			rs, rsErr := rsclient.Get(a.rootCtx).Lister().ReplicaSets(ns).Get(name)
			if rsErr != nil {
				return rsErr
			}
			newLease := &coordinationv1.Lease{
				ObjectMeta: metav1.ObjectMeta{
					Labels:    map[string]string{"app": ComponentName},
					Name:      name,
					Namespace: ns,
					OwnerReferences: []metav1.OwnerReference{
						*metav1.NewControllerRef(rs, appsv1.SchemeGroupVersion.WithKind("ReplicaSet")),
					},
				},
				Spec: coordinationv1.LeaseSpec{
					RenewTime: &renewTime,
				},
			}
			_, createErr := leases.Create(a.rootCtx, newLease, metav1.CreateOptions{})
			return createErr
		}
		if getErr != nil {
			return getErr
		}
		lease.Spec.RenewTime = &renewTime
		_, updateErr := leases.Update(a.rootCtx, lease, metav1.UpdateOptions{})
		return updateErr
	})
	if err != nil {
		a.rollbackActiveRecord(name, now)
		klog.ErrorS(err, "Failed to renew active lease", "name", name)
		tlog := telemetry.TLog{LogName: "sandbox.recordActive", Sbx: telemetry.SbxInfo{SandboxName: name}, Success: false, Message: err.Error()}
		telemetry.EmitTLog(tlog)
	}

	klog.V(2).InfoS("renew active lease", "name", name, "err", err)
}

// GetLastRequestTime gets the last recorded active time for the given sandbox name.
func (a *Activator) GetLastRequestTime(name string) int64 {
	lease, err := kubeclient.Get(a.rootCtx).CoordinationV1().Leases(config.Cfg.SandboxNamespace).Get(a.rootCtx, name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return 0
	}
	if err != nil {
		klog.ErrorS(err, "Failed to get last active lease", "name", name)
		return 0
	}
	if lease.Spec.RenewTime == nil {
		return 0
	}
	return lease.Spec.RenewTime.Time.Unix()
}
