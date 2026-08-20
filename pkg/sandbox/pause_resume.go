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

	snapshot, err := s.captureProcessSnapshot(sb)
	if err != nil {
		s.EventPause(sb, reason, err)
		return err
	}

	rsCopy := sb.ReplicaSet.DeepCopy()
	annotations := rsCopy.GetAnnotations()
	if annotations == nil {
		annotations = map[string]string{}
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
	return err
}

func (s *Controller) SandboxProcessSnapshot(sb *Sandbox) error {
	snapshot, err := s.captureProcessSnapshot(sb)
	if err != nil {
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
	return err
}

func (s *Controller) DeleteProcessSnapshot(sb *Sandbox) error {
	rsCopy := sb.ReplicaSet.DeepCopy()
	annotations := rsCopy.GetAnnotations()
	if annotations == nil {
		annotations = map[string]string{}
	}

	annotations[AnnotationProcessSnapshot] = ""
	rsCopy.SetAnnotations(annotations)

	_, err := s.kclient.AppsV1().ReplicaSets(config.Cfg.SandboxNamespace).Update(context.TODO(), rsCopy, v1meta.UpdateOptions{})
	return err
}

func (s *Controller) Resume(sb *Sandbox, reason string) error {
	if deriveSandboxStatus(sb.ReplicaSet) != Paused {
		return nil
	}

	klog.Infof("Resuming sandbox %s, reason %s", sb.Name, reason)

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
		return err
	}

	if err := s.WaitForReplicaSetReady(sb); err != nil {
		s.EventResume(sb, reason, err)
		return err
	}
	if snapshot != "" {
		if err := s.restoreProcessSnapshot(sb, snapshot); err != nil {
			s.EventResume(sb, reason, err)
			return err
		}
	}

	s.EventResume(sb, reason, nil)
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
