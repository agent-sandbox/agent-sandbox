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
	"time"

	"github.com/agent-sandbox/agent-sandbox/pkg/activator"
	"github.com/agent-sandbox/agent-sandbox/pkg/config"
	"github.com/agent-sandbox/agent-sandbox/pkg/telemetry"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	v1meta "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"
)

// TODO, consider to HPA resume

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
	lastSnapshot := annotations[AnnotationProcessSnapshot]

	snapshot, err := s.captureProcessSnapshot(sb)
	if err != nil {
		s.EventPause(sb, reason, err)
		s.tlogOp(sb, telemetry.EventNameSandboxPause, reason, "last snapshot:"+lastSnapshot, err, start)
		return err
	}

	replicas := int32(0)
	annotations[AnnotationPaused] = "true"
	annotations[AnnotationPausedAt] = time.Now().UTC().Format(time.RFC3339)
	annotations[AnnotationPauseReason] = reason
	if snapshot != "" {
		annotations[AnnotationProcessSnapshot] = snapshot
	} else {
		klog.Warningf("Empty process snapshot for sandbox %s when pausing, this may cause failure when resume", sb.Name)
	}
	rsCopy.SetAnnotations(annotations)
	rsCopy.Spec.Replicas = &replicas

	_, err = s.kclient.AppsV1().ReplicaSets(config.Cfg.SandboxNamespace).Update(context.TODO(), rsCopy, v1meta.UpdateOptions{})
	s.EventPause(sb, reason, err)
	s.tlogOp(sb, telemetry.EventNameSandboxPause, reason, fmt.Sprintf("snapshot: %s, last snapshot:%s", snapshot, lastSnapshot), err, start)
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
		s.tlogOp(sb, telemetry.EventNameSandboxSnapshot, "", "", err, start)
		return err
	}

	rsCopy := sb.ReplicaSet.DeepCopy()
	annotations := rsCopy.GetAnnotations()
	if annotations == nil {
		annotations = map[string]string{}
	}

	annotations[AnnotationProcessSnapshot] = snapshot
	rsCopy.SetAnnotations(annotations)

	_, err = s.kclient.AppsV1().ReplicaSets(config.Cfg.SandboxNamespace).Update(context.TODO(), rsCopy, v1meta.UpdateOptions{})
	s.tlogOp(sb, telemetry.EventNameSandboxSnapshot, "", "snapshot:"+snapshot, err, start)
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
	s.tlogOp(sb, telemetry.EventNameSandboxSnapshotDelete, "", "", err, start)
	return err
}

func (s *Controller) Resume(sb *Sandbox, reason string) error {
	if deriveSandboxStatus(sb.ReplicaSet) != Paused {
		return nil
	}

	klog.Infof("Resuming sandbox %s, reason %s", sb.Name, reason)
	start := time.Now()

	rsCopy := sb.ReplicaSet.DeepCopy()
	annotations := rsCopy.GetAnnotations()
	snapshot := annotations[AnnotationProcessSnapshot]
	//snapshot = "eyJjYXB0dXJlZF90aW1lIjoiMjAyNi0wNS0yMlQwNzozMTozOS41Mjg2NjlaIiwicHJvY2Vzc2VzIjpbeyJjb25maWciOnsiY21kIjoiL2Jpbi9iYXNoIiwiYXJncyI6WyItbCIsIi1jIiwicHl0aG9uIC1tIGh0dHAuc2VydmVyIDgwMDEiXX0sInBpZCI6MjAzfV19"
	delete(annotations, AnnotationPaused)
	delete(annotations, AnnotationPausedAt)
	delete(annotations, AnnotationPauseReason)
	annotations[AnnotationResumedAt] = time.Now().UTC().Format(time.RFC3339)
	annotations[AnnotationResumeReason] = reason
	rsCopy.SetAnnotations(annotations)

	replicas := int32(1)
	rsCopy.Spec.Replicas = &replicas
	_, err := s.kclient.AppsV1().ReplicaSets(config.Cfg.SandboxNamespace).Update(context.TODO(), rsCopy, v1meta.UpdateOptions{})
	switch {
	case err == nil:
		sb.ReplicaSet = rsCopy
	case errors.IsConflict(err):
		// Another concurrent Resume call already won the race and will
		// restore the process snapshot itself; just wait for the pod it
		// brings up instead of failing this request.
		klog.Infof("Sandbox %s resume update conflicted, another caller is already resuming it", sb.Name)
		snapshot = ""
	default:
		s.EventResume(sb, reason, err)
		s.tlogOp(sb, telemetry.EventNameSandboxResume, reason, "", err, start)
		return err
	}

	if err := s.WaitForReplicaSetReady(sb); err != nil {
		s.EventResume(sb, reason, err)
		s.tlogOp(sb, telemetry.EventNameSandboxResume, reason, "", err, start)
		return err
	}
	if snapshot != "" {
		if err := s.restoreProcessSnapshot(sb, snapshot); err != nil {
			s.EventResume(sb, reason, err)
			s.tlogOp(sb, telemetry.EventNameSandboxResume, reason, "", err, start)
			return err
		}
	}

	s.EventResume(sb, reason, nil)
	s.tlogOp(sb, telemetry.EventNameSandboxResume, reason, "snapshot:"+snapshot, nil, start)
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
