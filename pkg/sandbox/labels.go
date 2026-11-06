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
	"errors"
	"fmt"
	"maps"
	"sort"
	"strings"

	v1 "k8s.io/api/apps/v1"
	v1meta "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/validation"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/util/retry"
	"k8s.io/klog/v2"
	podclient "knative.dev/pkg/client/injection/kube/informers/core/v1/pod"

	"github.com/agent-sandbox/agent-sandbox/pkg/config"
)

const (
	// MetadataLabels is the metadata key that carries user labels, e.g. "team=a,count=2".
	MetadataLabels = "labels"
	// UserLabelPrefix keeps user labels apart from system labels such as sbx-user.
	UserLabelPrefix = "sbx.label/"
	MaxUserLabels   = 32
)

// ErrInvalidLabelSelector marks a List error caused by the caller's selector (a 400, not a 500).
var ErrInvalidLabelSelector = errors.New("invalid labels selector")

// ErrConditionsNotMet marks an update rejected because the current labels do not match its conditions.
var ErrConditionsNotMet = errors.New("conditions not met")

// ParseLabels parses "k1=v1,k2=v2" into a map. An empty string yields an empty map.
func ParseLabels(raw string) (map[string]string, error) {
	out := map[string]string{}
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return out, nil
	}
	for _, pair := range strings.Split(raw, ",") {
		k, v, found := strings.Cut(strings.TrimSpace(pair), "=")
		k, v = strings.TrimSpace(k), strings.TrimSpace(v)
		if !found || k == "" {
			return nil, fmt.Errorf("invalid label %q, expected key=value", pair)
		}
		// Key and value share the label value rules: <=63 chars, [A-Za-z0-9._-], alphanumeric at both ends.
		if errs := validation.IsValidLabelValue(k); len(errs) > 0 {
			return nil, fmt.Errorf("invalid label key %q: %s", k, strings.Join(errs, "; "))
		}
		if errs := validation.IsValidLabelValue(v); len(errs) > 0 {
			return nil, fmt.Errorf("invalid label value %q of key %q: %s", v, k, strings.Join(errs, "; "))
		}
		if _, dup := out[k]; dup {
			return nil, fmt.Errorf("duplicate label key %q", k)
		}
		out[k] = v
	}
	if len(out) > MaxUserLabels {
		return nil, fmt.Errorf("too many labels %d, max is %d", len(out), MaxUserLabels)
	}
	return out, nil
}

// FormatLabels renders labels as "k1=v1,k2=v2" sorted by key, the same format ParseLabels reads.
func FormatLabels(m map[string]string) string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, k+"="+m[k])
	}
	return strings.Join(parts, ",")
}

// ExtractUserLabels returns the user labels of a K8s label set, with the prefix stripped.
func ExtractUserLabels(k8sLabels map[string]string) map[string]string {
	out := map[string]string{}
	for k, v := range k8sLabels {
		if name, ok := strings.CutPrefix(k, UserLabelPrefix); ok {
			out[name] = v
		}
	}
	return out
}

// replaceUserLabels drops all user labels from dst and writes the given set (full replace).
// System labels are untouched. Returns dst, allocated when nil.
func replaceUserLabels(dst map[string]string, userLabels map[string]string) map[string]string {
	if dst == nil {
		dst = map[string]string{}
	}
	for k := range dst {
		if strings.HasPrefix(k, UserLabelPrefix) {
			delete(dst, k)
		}
	}
	for k, v := range userLabels {
		dst[UserLabelPrefix+k] = v
	}
	return dst
}

// applyUserLabelsToReplicaSet writes user labels to the ReplicaSet and its pod template,
// so a pod recreated on resume carries them too.
func applyUserLabelsToReplicaSet(rs *v1.ReplicaSet, userLabels map[string]string) {
	rs.SetLabels(replaceUserLabels(rs.GetLabels(), userLabels))
	rs.Spec.Template.SetLabels(replaceUserLabels(rs.Spec.Template.GetLabels(), userLabels))
}

// BuildListSelector ANDs sbx-user=<user> with the caller's label selector, whose keys get the user label prefix.
// The user requirement is always added, so the caller's selector can only narrow the result.
func BuildListSelector(user, labelSelector string) (labels.Selector, error) {
	userReq, err := labels.NewRequirement(UserLabel, "=", []string{user})
	if err != nil {
		return nil, fmt.Errorf("invalid user: %v", err)
	}
	selector := labels.NewSelector().Add(*userReq)
	if strings.TrimSpace(labelSelector) == "" {
		return selector, nil
	}

	parsed, err := ParseUserSelector(labelSelector)
	if err != nil {
		return nil, err
	}
	reqs, _ := parsed.Requirements()
	for _, r := range reqs {
		prefixed, err := labels.NewRequirement(UserLabelPrefix+r.Key(), r.Operator(), r.Values().List())
		if err != nil {
			return nil, err
		}
		selector = selector.Add(*prefixed)
	}
	return selector, nil
}

// ParseUserSelector parses a K8s label selector over user labels (keys without prefix), e.g. "team=a,count<3".
// An empty string yields nil (no requirement).
func ParseUserSelector(raw string) (labels.Selector, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	parsed, err := labels.Parse(raw)
	if err != nil {
		return nil, err
	}
	reqs, _ := parsed.Requirements()
	for _, r := range reqs {
		if strings.Contains(r.Key(), "/") {
			return nil, fmt.Errorf("key %q must not contain '/'", r.Key())
		}
	}
	return parsed, nil
}

// syncPodLabels applies mutate to the labels of every pod of the sandbox.
func syncPodLabels(ctx context.Context, client kubernetes.Interface, sandboxName string, mutate func(map[string]string)) error {
	pods, err := podclient.Get(ctx).Lister().Pods(config.Cfg.SandboxNamespace).List(labels.Set{"sandbox": sandboxName}.AsSelector())
	if err != nil {
		return fmt.Errorf("failed to list pods for sandbox %s: %w", sandboxName, err)
	}
	for _, pod := range pods {
		podName := pod.Name
		if err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
			latest, getErr := client.CoreV1().Pods(config.Cfg.SandboxNamespace).Get(ctx, podName, v1meta.GetOptions{})
			if getErr != nil {
				return getErr
			}
			podLabels := latest.GetLabels()
			if podLabels == nil {
				podLabels = map[string]string{}
			}
			mutate(podLabels)
			latest.SetLabels(podLabels)
			_, updateErr := client.CoreV1().Pods(config.Cfg.SandboxNamespace).Update(ctx, latest, v1meta.UpdateOptions{})
			return updateErr
		}); err != nil {
			return fmt.Errorf("failed to update pod %s labels: %w", podName, err)
		}
	}
	return nil
}

// UpdateUserLabels replaces the user labels of the sandbox (full replace, last write wins).
// sandbox-data is updated first (metadata.labels; its previous value moves to last-applied-configuration),
// then the ReplicaSet, pod template and running pods are re-derived from it.
// sb.ReplicaSet is refreshed, so later updates of the same sandbox do not conflict; pods are synced best effort.
//
// conditions, when not nil, must match the current labels in sandbox-data, checked on the same read as the
// write, otherwise ErrConditionsNotMet is returned and nothing is changed.
// userLabels nil means labels are not changed: only the conditions are checked.
func (s *Controller) UpdateUserLabels(sb *Sandbox, userLabels map[string]string, conditions labels.Selector) error {
	ctx := s.rootCtx
	var updated *v1.ReplicaSet
	var derived map[string]string
	unchanged := false
	err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		latest, getErr := s.kclient.AppsV1().ReplicaSets(config.Cfg.SandboxNamespace).Get(ctx, sb.Name, v1meta.GetOptions{})
		if getErr != nil {
			return getErr
		}
		// Conditions are checked on the same read write is based on, so a concurrent change
		// in between makes the Update conflict and the check run again.
		if conditions != nil || userLabels == nil {
			current := &Sandbox{}
			if err := json.Unmarshal([]byte(latest.Annotations[AnnotationSandboxData]), current); err != nil {
				return fmt.Errorf("invalid %s annotation of %s: %v", AnnotationSandboxData, sb.Name, err)
			}
			if conditions != nil && !conditions.Matches(labels.Set(current.UserLabels())) {
				return fmt.Errorf("%w: %s, current labels %q", ErrConditionsNotMet, conditions, current.Metadata[MetadataLabels])
			}
			if userLabels == nil { // check only
				updated, unchanged = latest, true
				return nil
			}
		}
		// sandbox-data first: on an invalid sandbox-data nothing is changed.
		changed, err := applyConfigUpdate(latest, setLabelsConfig(userLabels))
		if err != nil {
			return err
		}
		// ReplicaSet labels are an expansion of sandbox-data, derived from what was just stored.
		data := &Sandbox{}
		if err := json.Unmarshal([]byte(latest.Annotations[AnnotationSandboxData]), data); err != nil {
			return fmt.Errorf("invalid %s annotation of %s: %v", AnnotationSandboxData, sb.Name, err)
		}
		derived = data.UserLabels()
		// Nothing to write when sandbox-data is unchanged and the ReplicaSet already mirrors it.
		if !changed && maps.Equal(ExtractUserLabels(latest.Labels), derived) &&
			maps.Equal(ExtractUserLabels(latest.Spec.Template.Labels), derived) {
			updated, unchanged = latest, true
			return nil
		}
		applyUserLabelsToReplicaSet(latest, derived)
		var updateErr error
		updated, updateErr = s.kclient.AppsV1().ReplicaSets(config.Cfg.SandboxNamespace).Update(ctx, latest, v1meta.UpdateOptions{})
		return updateErr
	})
	if err != nil {
		return fmt.Errorf("failed to update labels of sandbox %s: %w", sb.Name, err)
	}
	sb.ReplicaSet = updated
	if unchanged {
		return nil
	}
	// Keep the in-memory metadata in line with the new sandbox-data, on a copy: the map may be shared.
	meta := maps.Clone(sb.Metadata)
	if meta == nil {
		meta = map[string]string{}
	}
	setMetadataLabels(meta, derived)
	sb.Metadata = meta

	if err := syncPodLabels(ctx, s.kclient, sb.Name, func(m map[string]string) { replaceUserLabels(m, derived) }); err != nil {
		klog.Errorf("failed to sync pod labels of sandbox %s: %v", sb.Name, err)
	}
	return nil
}

// mergeLabels validates metadata["labels"] and fills keys the sandbox does not set from the template.
// The result stays in metadata["labels"] in canonical form ("k1=v1,k2=v2" sorted by key), absent when empty.
func (sb *Sandbox) mergeLabels(templateLabels string) error {
	own, err := ParseLabels(sb.Metadata[MetadataLabels])
	if err != nil {
		return err
	}
	tpl, err := ParseLabels(templateLabels)
	if err != nil {
		return fmt.Errorf("template labels: %v", err)
	}
	for k, v := range tpl {
		if _, ok := own[k]; !ok {
			own[k] = v
		}
	}
	if len(own) > MaxUserLabels {
		return fmt.Errorf("too many labels %d, max is %d", len(own), MaxUserLabels)
	}
	setMetadataLabels(sb.Metadata, own)
	return nil
}

// setMetadataLabels writes labels to metadata["labels"] in canonical form, removing the key when empty.
func setMetadataLabels(metadata map[string]string, userLabels map[string]string) {
	if len(userLabels) == 0 {
		delete(metadata, MetadataLabels)
		return
	}
	metadata[MetadataLabels] = FormatLabels(userLabels)
}

// UserLabels returns the user labels of the sandbox from metadata["labels"], i.e. from sandbox-data,
// the source of truth. ReplicaSet labels are only derived from it. Make has validated the value, so a
// parse error cannot happen for a made sandbox; it yields no labels.
func (sb *Sandbox) UserLabels() map[string]string {
	m, err := ParseLabels(sb.Metadata[MetadataLabels])
	if err != nil {
		klog.Errorf("invalid labels in metadata of sandbox %s: %v", sb.Name, err)
		return map[string]string{}
	}
	return m
}
