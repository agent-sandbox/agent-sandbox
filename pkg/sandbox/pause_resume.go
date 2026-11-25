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
	v1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	v1meta "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/util/retry"
	"k8s.io/klog/v2"
)

// Pause scales the sandbox to zero. Process snapshots are explicit (SDK create_snapshot / API):
// pause never captures one, it keeps whatever snapshot the user took and resume restores it.
func (s *Controller) Pause(sb *Sandbox, reason string) error {
	if deriveSandboxStatus(sb.ReplicaSet) == Paused {
		return nil
	}

	start := time.Now()
	snapshotSummary := summarizeSnapshot(sb.ReplicaSet.GetAnnotations()[AnnotationProcessSnapshot])

	// no resourceVersion: pausing needs no optimistic lock
	err := s.setPaused(sb, true, reason, "")
	if err != nil {
		klog.Errorf("Failed to pause sandbox %s, reason %s: %v", sb.Name, reason, err)
	} else {
		klog.Infof("Paused sandbox %s, reason %s, snapshot: %s, took %v", sb.Name, reason, snapshotSummary, time.Since(start))
	}
	s.EventPause(sb, reason, err)
	s.tlogOp(sb, telemetry.EventNameSandboxPause, reason, "snapshot: "+snapshotSummary, err, start)
	return err
}

// setPaused scales the sandbox to zero and marks it paused, or (!paused) scales it to one,
// clears the pause marks and starts a "resuming" resume status; reason is the pause or resume
// reason. A non-empty resourceVersion makes the patch conditional (Conflict if the ReplicaSet
// changed), so Resume knows whether it won a race. Events and telemetry are left to the caller.
func (s *Controller) setPaused(sb *Sandbox, paused bool, reason, resourceVersion string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	replicas := 0
	// a null value removes the annotation in a merge patch
	annotations := map[string]any{
		AnnotationPaused:      "true",
		AnnotationPausedAt:    now,
		AnnotationPauseReason: reason,
	}
	if !paused {
		replicas = 1
		annotations = map[string]any{
			AnnotationPaused:       nil,
			AnnotationPausedAt:     nil,
			AnnotationPauseReason:  nil,
			AnnotationResumedAt:    now,
			AnnotationResumeReason: reason,
			AnnotationResumeStatus: ResumeStatus{State: ResumeStateResuming}.encode(),
		}
	}
	metadata := map[string]any{"annotations": annotations}
	if resourceVersion != "" {
		metadata["resourceVersion"] = resourceVersion
	}
	return s.patchReplicaSet(sb, map[string]any{
		"metadata": metadata,
		"spec":     map[string]any{"replicas": replicas},
	})
}

// patchReplicaSet merge patches the sandbox's ReplicaSet, retrying transient errors, and
// refreshes sb.ReplicaSet and sb.Status.
func (s *Controller) patchReplicaSet(sb *Sandbox, patchObj map[string]any) error {
	patch, err := json.Marshal(patchObj)
	if err != nil {
		return err
	}

	// Conflict (a conditional patch lost) and NotFound (sandbox deleted) are final.
	var patched *v1.ReplicaSet
	err = retry.OnError(retry.DefaultBackoff, func(err error) bool {
		return !errors.IsConflict(err) && !errors.IsNotFound(err)
	}, func() error {
		var perr error
		patched, perr = s.kclient.AppsV1().ReplicaSets(config.Cfg.SandboxNamespace).Patch(context.TODO(), sb.Name, types.MergePatchType, patch, v1meta.PatchOptions{})
		return perr
	})
	if err != nil {
		return err
	}
	sb.ReplicaSet = patched
	sb.Status = deriveSandboxStatus(patched)
	return nil
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

	err = s.saveProcessSnapshot(sb, snapshot)
	if err != nil {
		klog.Errorf("Failed to save process snapshot of sandbox %s: %v", sb.Name, err)
	} else {
		klog.Infof("Saved process snapshot of sandbox %s: %s", sb.Name, snapshotSummary)
	}
	s.EventSnapshot(sb, "SandboxSnapshotCreated", "SandboxSnapshotCreateFailed", snapshotSummary, err)
	s.tlogOp(sb, telemetry.EventNameSandboxSnapshot, "", "snapshot: "+snapshotSummary, err, start)
	return err
}

// saveProcessSnapshot writes the snapshot annotation with a merge patch, not an Update of sb.ReplicaSet:
// a labels update just before changes the pod template, so the ReplicaSet controller bumps its status
// and the cached resourceVersion would conflict.
func (s *Controller) saveProcessSnapshot(sb *Sandbox, snapshot string) error {
	return s.patchAnnotations(sb, map[string]any{AnnotationProcessSnapshot: snapshot})
}

// patchAnnotations merge patches only the given annotations (nil removes one).
func (s *Controller) patchAnnotations(sb *Sandbox, annotations map[string]any) error {
	return s.patchReplicaSet(sb, map[string]any{"metadata": map[string]any{"annotations": annotations}})
}

func (s *Controller) DeleteProcessSnapshot(sb *Sandbox) error {
	start := time.Now()

	// merge patch, for the same reason as saveProcessSnapshot
	err := s.patchAnnotations(sb, map[string]any{AnnotationProcessSnapshot: ""})
	if err != nil {
		klog.Errorf("Failed to delete process snapshot of sandbox %s: %v", sb.Name, err)
	} else {
		klog.Infof("Deleted process snapshot of sandbox %s", sb.Name)
	}
	s.EventSnapshot(sb, "SandboxSnapshotDeleted", "SandboxSnapshotDeleteFailed", "", err)
	s.tlogOp(sb, telemetry.EventNameSandboxSnapshotDelete, "", "", err, start)
	return err
}

// Resume scales a paused sandbox up, waits for the pod, initializes envd and restores the
// process snapshot. Any failure pauses it again, so it never stays half resumed (e.g. with an
// unauthenticated envd) and the next resume retries. Progress is kept in the resume status:
// while "resuming", other callers (concurrent or arriving later) wait on it, and
// PauseStuckResumes ends a resume that never finished.
func (s *Controller) Resume(sb *Sandbox, reason string) error {
	return s.resume(sb, reason, true)
}

// resume implements Resume. retryIfIdle allows one more try when the scale-up conflicted but
// nobody is resuming.
func (s *Controller) resume(sb *Sandbox, reason string, retryIfIdle bool) error {
	if sb.Resuming() {
		return s.waitResume(sb, reason, false)
	}
	if deriveSandboxStatus(sb.ReplicaSet) != Paused {
		return nil
	}

	start := time.Now()
	snapshot := sb.ReplicaSet.GetAnnotations()[AnnotationProcessSnapshot]
	snapshotSummary := summarizeSnapshot(snapshot)
	klog.Infof("Resuming sandbox %s, reason %s, snapshot: %s", sb.Name, reason, snapshotSummary)
	finish := func(err error) error {
		if err != nil {
			klog.Errorf("Failed to resume sandbox %s after %v: %v", sb.Name, time.Since(start), err)
		} else {
			klog.Infof("Resumed sandbox %s, reason %s, took %v", sb.Name, reason, time.Since(start))
		}
		s.EventResume(sb, reason, err)
		s.tlogOp(sb, telemetry.EventNameSandboxResume, reason, "snapshot: "+snapshotSummary, err, start)
		return err
	}

	// Conditional on the copy read: of concurrent callers only one scales up, the others wait.
	err := s.setPaused(sb, false, reason, sb.ReplicaSet.ResourceVersion)
	if errors.IsConflict(err) {
		klog.Infof("Sandbox %s resume conflicted, waiting for the resume in progress", sb.Name)
		return s.waitResume(sb, reason, retryIfIdle)
	}
	if err != nil {
		return finish(fmt.Errorf("update replicaset: %w", err))
	}

	status := ResumeStatus{State: ResumeStateResuming, SnapshotRestored: snapshot == ""}
	snapshotSummary, err = s.bringUpResumed(sb, &status, snapshot, snapshotSummary, start)
	if err == nil {
		status.State = ResumeStateSucceeded
		if err = s.writeResumeStatus(sb, status); err != nil {
			err = fmt.Errorf("record resume status: %w", err)
		}
	}
	if err != nil {
		s.failResume(sb, status, err, "resume_failed")
	}
	return finish(err)
}

// resumeTimeout bounds a whole resume: pod ready, envd init and a margin for the restore.
func resumeTimeout() time.Duration {
	_, readyTimeout := readyWaitTimings()
	return readyTimeout + envdTimeout + time.Minute
}

// waitResume waits for the resume in progress, reading the ReplicaSet from the API server (the
// cache may lag). Once it is no longer running: not paused means it succeeded (unless failed);
// paused means it failed, unless it was never seen running after a conflict (the conflict was
// another update, nobody is resuming), then retryIfIdle tries once more.
func (s *Controller) waitResume(sb *Sandbox, reason string, retryIfIdle bool) error {
	interval, _ := readyWaitTimings()
	seenResuming := false
	var latest *v1.ReplicaSet
	err := wait.PollUntilContextTimeout(context.TODO(), interval, resumeTimeout(), true, func(ctx context.Context) (bool, error) {
		rs, err := s.kclient.AppsV1().ReplicaSets(config.Cfg.SandboxNamespace).Get(ctx, sb.Name, v1meta.GetOptions{})
		if err != nil {
			if errors.IsNotFound(err) {
				return false, err
			}
			return false, nil // transient, poll again
		}
		latest = rs
		_, running := latestResume(rs)
		seenResuming = seenResuming || running
		return !running, nil
	})
	if err != nil {
		return fmt.Errorf("wait for the resume of sandbox %s: %w", sb.Name, err)
	}

	sb.ReplicaSet = latest
	sb.Status = deriveSandboxStatus(latest)
	status, _ := latestResume(latest)
	switch {
	case sb.Status != Paused && status.State == ResumeStateFailed: // failed, and pausing failed too
		return fmt.Errorf("resume of sandbox %s failed: %s", sb.Name, status.Error)
	case sb.Status != Paused:
		return nil
	case !seenResuming && retryIfIdle:
		return s.resume(sb, reason, false)
	default:
		return fmt.Errorf("resume of sandbox %s did not complete, it is paused (reason %s): %s",
			sb.Name, latest.Annotations[AnnotationPauseReason], status.Error)
	}
}

// PauseStuckResumes pauses sandboxes still "resuming" longer than any resume takes (e.g. the
// controller restarted mid-resume); nothing else would end them. Run by the scaler.
func (s *Controller) PauseStuckResumes(sbs []*Sandbox) {
	for _, sb := range sbs {
		if sb.ReplicaSet == nil {
			continue
		}
		status, running := latestResume(sb.ReplicaSet)
		if !running || time.Since(status.UpdatedAt) < resumeTimeout() {
			continue
		}
		err := fmt.Errorf("resume did not finish within %v", resumeTimeout())
		klog.Warningf("Sandbox %s: %v, pausing it again", sb.Name, err)
		s.failResume(sb, status, err, "resume_timeout")
	}
}

// failResume records the failed resume, then pauses the sandbox again. The status goes first,
// so concurrent callers that see the pause read the error. Pause logs its own failure.
func (s *Controller) failResume(sb *Sandbox, status ResumeStatus, cause error, pauseReason string) {
	status.State = ResumeStateFailed
	status.Error = cause.Error()
	if err := s.writeResumeStatus(sb, status); err != nil {
		klog.Warningf("Failed to record the failed resume of sandbox %s: %v", sb.Name, err)
	}
	_ = s.Pause(sb, pauseReason)
}

// ResumeState is the state of the latest resume in ResumeStatus.
type ResumeState string

const (
	ResumeStateResuming  ResumeState = "resuming"
	ResumeStateSucceeded ResumeState = "succeeded"
	ResumeStateFailed    ResumeState = "failed"
)

// ResumeStatus is the progress and result of the latest resume: the resume-status annotation,
// returned as metadata["resumeStatus"].
type ResumeStatus struct {
	State           ResumeState `json:"state"`
	PodReady        bool        `json:"podReady"`
	EnvdInitialized bool        `json:"envdInitialized"`
	// SnapshotRestored is also true when there is no snapshot to restore.
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

// latestResume returns the resume status of rs (zero if absent or invalid), and whether that
// resume is still running: "resuming" and not paused (a pause mid-resume ends it).
func latestResume(rs *v1.ReplicaSet) (st ResumeStatus, running bool) {
	if raw := rs.GetAnnotations()[AnnotationResumeStatus]; raw != "" {
		_ = json.Unmarshal([]byte(raw), &st)
	}
	return st, st.State == ResumeStateResuming && deriveSandboxStatus(rs) != Paused
}

// Resuming reports whether a resume of sb is in progress (per sb's copy of the ReplicaSet).
func (sb *Sandbox) Resuming() bool {
	if sb.ReplicaSet == nil {
		return false
	}
	_, running := latestResume(sb.ReplicaSet)
	return running
}

func (s *Controller) writeResumeStatus(sb *Sandbox, st ResumeStatus) error {
	return s.patchAnnotations(sb, map[string]any{AnnotationResumeStatus: st.encode()})
}

// bringUpResumed runs the steps after the scale-up, recording each in status: wait for the pod,
// initialize envd, restore the snapshot. It returns the summary with the restore result, and an
// error naming the failed step.
func (s *Controller) bringUpResumed(sb *Sandbox, status *ResumeStatus, snapshot, snapshotSummary string, start time.Time) (string, error) {
	if err := s.WaitForReplicaSetReady(sb); err != nil {
		err = fmt.Errorf("wait for pod ready: %w", err)
		// the pod is gone once paused again, so keep its diagnostics in the error
		if diag := s.DiagnoseSandbox(sb.Name); diag != "" {
			err = fmt.Errorf("%w; diagnostics: %s", err, diag)
		}
		return snapshotSummary, err
	}
	klog.Infof("Sandbox %s pod is ready after %v", sb.Name, time.Since(start))
	status.PodReady = true

	// a fresh envd: init it before the restore, so restored processes get the env vars
	if err := s.initEnvd(sb); err != nil {
		return snapshotSummary, fmt.Errorf("envd init: %w", err)
	}
	status.EnvdInitialized = true

	if snapshot == "" {
		return snapshotSummary, nil
	}
	restoreStart := time.Now()
	result, err := s.restoreProcessSnapshot(sb, snapshot)
	status.Total, status.Started, status.Skipped = result.Total, result.Started, result.Skipped
	if err != nil {
		s.EventSnapshotRestore(sb, result, err)
		return snapshotSummary, fmt.Errorf("restore process snapshot (%s): %w", result, err)
	}
	klog.Infof("Restored process snapshot of sandbox %s in %v: %s", sb.Name, time.Since(restoreStart), result)
	s.EventSnapshotRestore(sb, result, nil)
	status.SnapshotRestored = true
	return fmt.Sprintf("%s, restore %s", snapshotSummary, result), nil
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
