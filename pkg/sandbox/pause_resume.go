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
	"encoding/json"
	"fmt"
	"time"

	"github.com/agent-sandbox/agent-sandbox/pkg/activator"
	"github.com/agent-sandbox/agent-sandbox/pkg/config"
	"github.com/agent-sandbox/agent-sandbox/pkg/telemetry"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	v1meta "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
)

// TODO, consider to HPA resume

// Pause scales the sandbox to zero. Process snapshots are explicit (SDK create_snapshot / API):
// pause never captures one, it keeps whatever snapshot the user took and resume restores it.
func (s *Controller) Pause(sb *Sandbox, reason string) error {
	if deriveSandboxStatus(sb.ReplicaSet) == Paused {
		return nil
	}

	start := time.Now()

	rsCopy := sb.ReplicaSet.DeepCopy()
	annotations := rsCopy.GetAnnotations()
	if annotations == nil {
		annotations = map[string]string{}
	}
	snapshotSummary := summarizeSnapshot(annotations[AnnotationProcessSnapshot])

	replicas := int32(0)
	annotations[AnnotationPaused] = "true"
	annotations[AnnotationPausedAt] = time.Now().UTC().Format(time.RFC3339)
	annotations[AnnotationPauseReason] = reason
	delete(annotations, AnnotationResumeStatus)
	rsCopy.SetAnnotations(annotations)
	rsCopy.Spec.Replicas = &replicas

	_, err := s.kclient.AppsV1().ReplicaSets(config.Cfg.SandboxNamespace).Update(context.TODO(), rsCopy, v1meta.UpdateOptions{})
	if err != nil {
		klog.Errorf("Failed to pause sandbox %s, reason %s: %v", sb.Name, reason, err)
	} else {
		klog.Infof("Paused sandbox %s, reason %s, snapshot: %s, took %v", sb.Name, reason, snapshotSummary, time.Since(start))
	}
	s.EventPause(sb, reason, err)
	s.tlogOp(sb, telemetry.EventNameSandboxPause, reason, "snapshot: "+snapshotSummary, err, start)
	return err
}

// tlogOp emits a TLog for a sandbox lifecycle operation (pause/resume/snapshot/...).
// reason may be empty for operations that don't have one (e.g. snapshot).
func (s *Controller) tlogOp(sb *Sandbox, eventName, reason string, msg string, err error, start time.Time) {
	tlog := telemetry.TLog{
		EventName: eventName,
		Reason:    reason,
		Success:   err == nil,
		Duration:  time.Since(start).Seconds(),
		Message:   msg,
	}
	if msg == "" {
		tlog.Message = eventName + " " + reason
	}
	if err != nil {
		tlog.Message = msg + " error:" + err.Error()
	}
	TLog(sb, tlog)
}

func (s *Controller) SandboxProcessSnapshot(sb *Sandbox) error {
	start := time.Now()

	snapshot, err := s.captureProcessSnapshot(sb)
	if err != nil {
		klog.Errorf("Failed to capture process snapshot of sandbox %s: %v", sb.Name, err)
		s.EventSnapshot(sb, "SandboxSnapshotCreated", "SandboxSnapshotCreateFailed", "", err)
		s.tlogOp(sb, telemetry.EventNameSandboxSnapshot, "", "", err, start)
		return err
	}
	snapshotSummary := summarizeSnapshot(snapshot)

	rsCopy := sb.ReplicaSet.DeepCopy()
	annotations := rsCopy.GetAnnotations()
	if annotations == nil {
		annotations = map[string]string{}
	}

	annotations[AnnotationProcessSnapshot] = snapshot
	rsCopy.SetAnnotations(annotations)

	_, err = s.kclient.AppsV1().ReplicaSets(config.Cfg.SandboxNamespace).Update(context.TODO(), rsCopy, v1meta.UpdateOptions{})
	if err != nil {
		klog.Errorf("Failed to save process snapshot of sandbox %s: %v", sb.Name, err)
	} else {
		klog.Infof("Saved process snapshot of sandbox %s: %s", sb.Name, snapshotSummary)
	}
	s.EventSnapshot(sb, "SandboxSnapshotCreated", "SandboxSnapshotCreateFailed", snapshotSummary, err)
	s.tlogOp(sb, telemetry.EventNameSandboxSnapshot, "", "snapshot: "+snapshotSummary, err, start)
	return err
}

func (s *Controller) DeleteProcessSnapshot(sb *Sandbox) error {
	start := time.Now()

	rsCopy := sb.ReplicaSet.DeepCopy()
	annotations := rsCopy.GetAnnotations()
	if annotations == nil {
		annotations = map[string]string{}
	}

	annotations[AnnotationProcessSnapshot] = ""
	rsCopy.SetAnnotations(annotations)

	_, err := s.kclient.AppsV1().ReplicaSets(config.Cfg.SandboxNamespace).Update(context.TODO(), rsCopy, v1meta.UpdateOptions{})
	if err != nil {
		klog.Errorf("Failed to delete process snapshot of sandbox %s: %v", sb.Name, err)
	} else {
		klog.Infof("Deleted process snapshot of sandbox %s", sb.Name)
	}
	s.EventSnapshot(sb, "SandboxSnapshotDeleted", "SandboxSnapshotDeleteFailed", "", err)
	s.tlogOp(sb, telemetry.EventNameSandboxSnapshotDelete, "", "", err, start)
	return err
}

func (s *Controller) Resume(sb *Sandbox, reason string) error {
	if deriveSandboxStatus(sb.ReplicaSet) != Paused {
		return nil
	}

	start := time.Now()

	rsCopy := sb.ReplicaSet.DeepCopy()
	annotations := rsCopy.GetAnnotations()
	snapshot := annotations[AnnotationProcessSnapshot]
	snapshotSummary := summarizeSnapshot(snapshot)
	klog.Infof("Resuming sandbox %s, reason %s, snapshot: %s", sb.Name, reason, snapshotSummary)
	//snapshot = "eyJjYXB0dXJlZF90aW1lIjoiMjAyNi0wNS0yMlQwNzozMTozOS41Mjg2NjlaIiwicHJvY2Vzc2VzIjpbeyJjb25maWciOnsiY21kIjoiL2Jpbi9iYXNoIiwiYXJncyI6WyItbCIsIi1jIiwicHl0aG9uIC1tIGh0dHAuc2VydmVyIDgwMDEiXX0sInBpZCI6MjAzfV19"
	delete(annotations, AnnotationPaused)
	delete(annotations, AnnotationPausedAt)
	delete(annotations, AnnotationPauseReason)
	annotations[AnnotationResumedAt] = time.Now().UTC().Format(time.RFC3339)
	annotations[AnnotationResumeReason] = reason
	// no snapshot means nothing to restore, so that dimension is already satisfied
	status := ResumeStatus{SnapshotRestored: snapshot == ""}
	annotations[AnnotationResumeStatus] = status.encode()
	rsCopy.SetAnnotations(annotations)

	replicas := int32(1)
	rsCopy.Spec.Replicas = &replicas
	// Only the caller whose update wins owns the resume status.
	recordStatus := func(ResumeStatus) {}
	_, err := s.kclient.AppsV1().ReplicaSets(config.Cfg.SandboxNamespace).Update(context.TODO(), rsCopy, v1meta.UpdateOptions{})
	switch {
	case err == nil:
		sb.ReplicaSet = rsCopy
		recordStatus = func(st ResumeStatus) { s.patchResumeStatus(sb, st) }
	case errors.IsConflict(err):
		// Another concurrent Resume(Multiple requests accessed concurrent when sandbox is paused) call already won the race and will
		// restore the process snapshot and record the resume status itself;
		// just wait for the pod it brings up instead of failing this request.
		klog.Infof("Sandbox %s resume update conflicted, another caller is already resuming it", sb.Name)
		snapshot = ""
	default:
		err = fmt.Errorf("update replicaset: %w", err)
		klog.Errorf("Failed to resume sandbox %s: %v", sb.Name, err)
		s.EventResume(sb, reason, err)
		s.tlogOp(sb, telemetry.EventNameSandboxResume, reason, "snapshot: "+snapshotSummary, err, start)
		return err
	}

	if err := s.WaitForReplicaSetReady(sb); err != nil {
		err = fmt.Errorf("wait for pod ready: %w", err)
		klog.Errorf("Failed to resume sandbox %s after %v: %v", sb.Name, time.Since(start), err)
		status.Error = err.Error()
		recordStatus(status)
		s.EventResume(sb, reason, err)
		s.tlogOp(sb, telemetry.EventNameSandboxResume, reason, "snapshot: "+snapshotSummary, err, start)
		return err
	}
	klog.Infof("Sandbox %s pod is ready after %v", sb.Name, time.Since(start))
	status.PodReady = true

	if snapshot != "" {
		restoreStart := time.Now()
		result, err := s.restoreProcessSnapshot(sb, snapshot)
		status.Total, status.Started, status.Skipped = result.Total, result.Started, result.Skipped
		if err != nil {
			err = fmt.Errorf("restore process snapshot (%s): %w", result, err)
			klog.Errorf("Failed to resume sandbox %s: %v", sb.Name, err)
			status.Error = err.Error()
			recordStatus(status)
			s.EventSnapshotRestore(sb, result, err)
			s.EventResume(sb, reason, err)
			s.tlogOp(sb, telemetry.EventNameSandboxResume, reason, "snapshot: "+snapshotSummary, err, start)
			return err
		}
		klog.Infof("Restored process snapshot of sandbox %s in %v: %s", sb.Name, time.Since(restoreStart), result)
		s.EventSnapshotRestore(sb, result, nil)
		snapshotSummary = fmt.Sprintf("%s, restore %s", snapshotSummary, result)
		status.SnapshotRestored = true
	}
	recordStatus(status)

	klog.Infof("Resumed sandbox %s, reason %s, took %v", sb.Name, reason, time.Since(start))
	s.EventResume(sb, reason, nil)
	s.tlogOp(sb, telemetry.EventNameSandboxResume, reason, "snapshot: "+snapshotSummary, nil, start)
	return nil
}

func (s *Controller) EventPause(sb *Sandbox, reason string, err error) {
	t := corev1.EventTypeNormal
	r := "SandboxPaused"
	m := fmt.Sprintf("Sandbox paused, name %s, reason %s", sb.Name, reason)

	if err != nil {
		t = corev1.EventTypeWarning
		r = "SandboxPausedFailed"
		m = fmt.Sprintf("Failed to paused sandbox, name %s, error %v", sb.Name, err.Error())
	}

	activator.RecordEvent(s.recorder, t, sb.Name, r, m)
}

func (s *Controller) EventResume(sb *Sandbox, reason string, err error) {
	t := corev1.EventTypeNormal
	r := "SandboxResume"
	m := fmt.Sprintf("Sandbox resume, name %s, reason %s", sb.Name, reason)

	if err != nil {
		t = corev1.EventTypeWarning
		r = "SandboxResumeFailed"
		m = fmt.Sprintf("Failed to resume sandbox, name %s, error %v", sb.Name, err.Error())
	}

	activator.RecordEvent(s.recorder, t, sb.Name, r, m)
}

// EventSnapshot records the outcome of an explicit snapshot create/delete.
func (s *Controller) EventSnapshot(sb *Sandbox, okReason, failReason, detail string, err error) {
	if err != nil {
		activator.RecordEvent(s.recorder, corev1.EventTypeWarning, sb.Name, failReason,
			fmt.Sprintf("Process snapshot operation failed, name %s, error %v", sb.Name, err))
		return
	}
	m := fmt.Sprintf("Process snapshot operation succeeded, name %s", sb.Name)
	if detail != "" {
		m += ", " + detail
	}
	activator.RecordEvent(s.recorder, corev1.EventTypeNormal, sb.Name, okReason, m)
}

// EventSnapshotRestore records the outcome of restoring processes on resume.
func (s *Controller) EventSnapshotRestore(sb *Sandbox, result restoreResult, err error) {
	if err != nil {
		activator.RecordEvent(s.recorder, corev1.EventTypeWarning, sb.Name, "SandboxSnapshotRestoreFailed",
			fmt.Sprintf("Failed to restore process snapshot, name %s, %s, error %v", sb.Name, result, err))
		return
	}
	activator.RecordEvent(s.recorder, corev1.EventTypeNormal, sb.Name, "SandboxSnapshotRestored",
		fmt.Sprintf("Process snapshot restored, name %s, %s", sb.Name, result))
}

// ResumeStatus records how the latest resume went. The resume fully succeeded when
// PodReady and SnapshotRestored are both true.
//
// SnapshotRestored is true when every snapshotted process is running, or when there is no
// snapshot (Total == 0). While false, a non-empty Error means the resume failed; an empty
// Error means it is still in progress.
type ResumeStatus struct {
	PodReady         bool      `json:"podReady"`
	SnapshotRestored bool      `json:"snapshotRestored"`
	Total            int       `json:"total,omitempty"`
	Started          int       `json:"started,omitempty"`
	Skipped          int       `json:"skipped,omitempty"`
	Error            string    `json:"error,omitempty"`
	UpdatedAt        time.Time `json:"updatedAt"`
}

func (st ResumeStatus) encode() string {
	st.UpdatedAt = time.Now().UTC()
	raw, _ := json.Marshal(st)
	return string(raw)
}

// patchResumeStatus writes only the resume-status annotation with a merge patch, so it never
// conflicts with concurrent ReplicaSet updates. A failure is logged, not returned: the status
// is informational and must not change the resume result.
func (s *Controller) patchResumeStatus(sb *Sandbox, st ResumeStatus) {
	patch, _ := json.Marshal(map[string]any{
		"metadata": map[string]any{"annotations": map[string]string{AnnotationResumeStatus: st.encode()}},
	})
	patched, err := s.kclient.AppsV1().ReplicaSets(config.Cfg.SandboxNamespace).Patch(context.TODO(), sb.Name, types.MergePatchType, patch, v1meta.PatchOptions{})
	if err != nil {
		klog.Warningf("Failed to record resume status of sandbox %s (podReady=%v snapshotRestored=%v): %v", sb.Name, st.PodReady, st.SnapshotRestored, err)
		return
	}
	// keep the caller's copy current, so a response built right after Resume shows this status
	sb.ReplicaSet = patched
}
