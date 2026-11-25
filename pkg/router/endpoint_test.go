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

package router

import (
	"testing"
	"time"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var t0 = time.Date(2026, 11, 16, 9, 35, 0, 0, time.UTC)

func pod(name string, created time.Time, phase v1.PodPhase, ready bool) *v1.Pod {
	status := v1.ConditionFalse
	if ready {
		status = v1.ConditionTrue
	}
	return &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: name, CreationTimestamp: metav1.NewTime(created)},
		Status: v1.PodStatus{
			Phase:      phase,
			Conditions: []v1.PodCondition{{Type: v1.PodReady, Status: status}},
		},
	}
}

func deleting(p *v1.Pod) *v1.Pod {
	now := metav1.NewTime(t0)
	p.DeletionTimestamp = &now
	return p
}

func TestNewestPod(t *testing.T) {
	running := pod("sb-hpn2r", t0, v1.PodRunning, true)
	later := pod("sb-later", t0.Add(time.Second), v1.PodRunning, true)

	cases := []struct {
		name string
		pods []*v1.Pod
		want *v1.Pod
	}{
		{
			// Reproduced on a cluster: pods created while the device plugin restarted
			// failed admission in the same second as the one that started, and sorted
			// after it by name.
			name: "failed sibling from the same second does not hide the running pod",
			pods: []*v1.Pod{
				pod("sb-dlmhm", t0, v1.PodFailed, false),
				running,
				pod("sb-w585k", t0, v1.PodFailed, false),
			},
			want: running,
		},
		{
			name: "a newer failed pod does not hide the running pod",
			pods: []*v1.Pod{running, pod("sb-aaaaa", t0.Add(time.Second), v1.PodFailed, false)},
			want: running,
		},
		{
			name: "a newer succeeded pod does not hide the running pod",
			pods: []*v1.Pod{running, pod("sb-zzzzz", t0.Add(time.Second), v1.PodSucceeded, false)},
			want: running,
		},
		{
			name: "only terminated pods: none",
			pods: []*v1.Pod{pod("sb-a", t0, v1.PodFailed, false), pod("sb-b", t0, v1.PodSucceeded, false)},
			want: nil,
		},
		{
			name: "newest live pod not ready yet: none, not the older one",
			pods: []*v1.Pod{running, pod("sb-new", t0.Add(time.Second), v1.PodPending, false)},
			want: nil,
		},
		{
			name: "newest live pod being deleted: none, not the older one",
			pods: []*v1.Pod{running, deleting(pod("sb-deleting", t0.Add(time.Second), v1.PodRunning, true))},
			want: nil,
		},
		{
			name: "newest live pod by creation time",
			pods: []*v1.Pod{later, running},
			want: later,
		},
		{
			name: "single running pod",
			pods: []*v1.Pod{running},
			want: running,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := newestPod(tc.pods)
			if got != tc.want {
				t.Errorf("newestPod() = %s, want %s", podName(got), podName(tc.want))
			}
		})
	}
}

func podName(p *v1.Pod) string {
	if p == nil {
		return "<nil>"
	}
	return p.Name
}
