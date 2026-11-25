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

package sandbox

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/agent-sandbox/agent-sandbox/pkg/config"
	v1core "k8s.io/api/core/v1"
	v1meta "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/klog/v2"
	podclient "knative.dev/pkg/client/injection/kube/informers/core/v1/pod"
)

const (
	// Max distinct events returned to the client; the server log keeps the full list.
	diagnoseMaxEvents = 10
	// Long event messages (e.g. image pull rpc errors) are cut to keep the API error readable.
	diagnoseMaxMsgLen = 500
	// Diagnostics run after the readiness wait already timed out, so keep the extra latency small.
	diagnoseTimeout = 5 * time.Second
)

// DiagnoseSandbox returns a compact reason why a sandbox is not ready, e.g.
// "pod Pending(Unschedulable); events: FailedScheduling(x5): 0/3 nodes are available: 3 Insufficient cpu."
// Best effort, returns "" when nothing is found.
func (s *Controller) DiagnoseSandbox(name string) string {
	ctx, cancel := context.WithTimeout(context.Background(), diagnoseTimeout)
	defer cancel()

	var parts []string

	pods, err := podclient.Get(s.rootCtx).Lister().Pods(config.Cfg.SandboxNamespace).
		List(labels.Set{"sandbox": name}.AsSelector())
	if err != nil {
		klog.Warningf("diagnose sandbox %s: failed to list pods: %v", name, err)
	}
	if len(pods) == 0 {
		parts = append(parts, "no pod created")
	}
	for _, pod := range pods {
		parts = append(parts, "pod "+summarizePodStatus(pod))
	}

	// RS events cover pod creation failures (quota/admission), pod events cover scheduling, image pull, probes.
	events := s.listObjectEvents(ctx, "ReplicaSet", name)
	for _, pod := range pods {
		events = append(events, s.listObjectEvents(ctx, "Pod", pod.Name)...)
	}
	for _, e := range events {
		klog.Infof("diagnose sandbox %s: [%s/%s] %s %s: %s (x%d)", name, e.InvolvedObject.Kind, e.InvolvedObject.Name, e.Type, e.Reason, e.Message, e.Count)
	}
	if msg := formatDiagnoseEvents(events); msg != "" {
		parts = append(parts, "events: "+msg)
	}

	return strings.Join(parts, "; ")
}

func (s *Controller) listObjectEvents(ctx context.Context, kind, name string) []v1core.Event {
	list, err := s.kclient.CoreV1().Events(config.Cfg.SandboxNamespace).List(ctx, v1meta.ListOptions{
		FieldSelector: fmt.Sprintf("involvedObject.kind=%s,involvedObject.name=%s", kind, name),
	})
	if err != nil {
		klog.Warningf("diagnose sandbox: failed to list events of %s/%s: %v", kind, name, err)
		return nil
	}
	return list.Items
}

// formatDiagnoseEvents keeps only Warning events (newest first, deduplicated, capped).
// Without any warning it falls back to the latest Normal event to show where startup is stuck (e.g. Pulling).
func formatDiagnoseEvents(events []v1core.Event) string {
	sort.SliceStable(events, func(i, j int) bool {
		return sandboxEventSortTime(events[i]).After(sandboxEventSortTime(events[j]))
	})

	var picked []v1core.Event
	seen := map[string]bool{}
	for _, e := range events {
		if e.Type != v1core.EventTypeWarning || isEchoEvent(e) {
			continue
		}
		key := e.Reason + "|" + e.Message
		if seen[key] {
			continue
		}
		seen[key] = true
		picked = append(picked, e)
		if len(picked) >= diagnoseMaxEvents {
			break
		}
	}
	if len(picked) == 0 && len(events) > 0 {
		picked = events[:1]
	}

	items := make([]string, 0, len(picked))
	for _, e := range picked {
		item := e.Reason
		if e.Count > 1 {
			item += fmt.Sprintf("(x%d)", e.Count)
		}
		items = append(items, item+": "+compactMessage(e.Message))
	}
	return strings.Join(items, " | ")
}

// isEchoEvent detects kubelet events like "Error: ErrImagePull" that only repeat the
// container waiting reason already shown in the pod summary.
func isEchoEvent(e v1core.Event) bool {
	msg := strings.TrimSpace(e.Message)
	return strings.HasPrefix(msg, "Error: ") && !strings.Contains(strings.TrimPrefix(msg, "Error: "), " ")
}

// compactMessage flattens whitespace and truncates to diagnoseMaxMsgLen runes.
func compactMessage(msg string) string {
	msg = strings.Join(strings.Fields(msg), " ")
	if r := []rune(msg); len(r) > diagnoseMaxMsgLen {
		msg = string(r[:diagnoseMaxMsgLen]) + "..."
	}
	return msg
}

// summarizePodStatus renders e.g. "Pending(Unschedulable)" or
// "Running, main CrashLoopBackOff(last Error exit=1, restarts=4)". Details live in events.
func summarizePodStatus(pod *v1core.Pod) string {
	var b strings.Builder
	b.WriteString(string(pod.Status.Phase))

	var reasons []string
	if pod.Status.Reason != "" {
		reasons = append(reasons, pod.Status.Reason)
	}
	// Only PodScheduled carries a useful reason (Unschedulable); Ready/ContainersReady just echo container states.
	for _, c := range pod.Status.Conditions {
		if c.Type == v1core.PodScheduled && c.Status != v1core.ConditionTrue && c.Reason != "" {
			reasons = append(reasons, c.Reason)
		}
	}
	if len(reasons) > 0 {
		fmt.Fprintf(&b, "(%s)", strings.Join(reasons, ","))
	}

	statuses := append([]v1core.ContainerStatus{}, pod.Status.InitContainerStatuses...)
	statuses = append(statuses, pod.Status.ContainerStatuses...)
	for _, cs := range statuses {
		if state := containerStateSummary(cs); state != "" {
			fmt.Fprintf(&b, ", %s %s", cs.Name, state)
		}
	}
	return b.String()
}

// containerStateSummary returns "" for a healthy running container.
func containerStateSummary(cs v1core.ContainerStatus) string {
	var state string
	switch {
	case cs.State.Waiting != nil:
		state = cs.State.Waiting.Reason
	case cs.State.Terminated != nil:
		state = fmt.Sprintf("%s(exit=%d)", cs.State.Terminated.Reason, cs.State.Terminated.ExitCode)
	case cs.State.Running != nil && !cs.Ready:
		state = "NotReady"
	}

	// Last termination reveals crash loops (e.g. OOMKilled, Error) while the container is backing off.
	if lt := cs.LastTerminationState.Terminated; lt != nil && state != "" {
		state += fmt.Sprintf("(last %s exit=%d, restarts=%d)", lt.Reason, lt.ExitCode, cs.RestartCount)
	}
	return state
}
