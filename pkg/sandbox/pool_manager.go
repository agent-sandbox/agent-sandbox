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
	"strings"
	"sync"
	"time"

	"k8s.io/apimachinery/pkg/api/errors"
	rsclient "knative.dev/pkg/client/injection/kube/informers/apps/v1/replicaset"
	"knative.dev/pkg/reconciler"

	"github.com/agent-sandbox/agent-sandbox/pkg/config"
	v1 "k8s.io/api/apps/v1"
	v1meta "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"
	kubeclient "knative.dev/pkg/client/injection/kube/client"
)

// PoolManager manages sandbox pool replicasets
type poolCandidate struct {
	replicaset *v1.ReplicaSet
	reserved   bool
}

type PoolManager struct {
	client         kubernetes.Interface
	rootCtx        context.Context
	replenishQueue workqueue.RateLimitingInterface

	candidateLock sync.Mutex
	// candidateByTemplate e.g. {"python": {"rs1": candidate1, "rs2": candidate2}, "nodejs": {"rs3": candidate3}}
	candidateByTemplate map[string]map[string]*poolCandidate
}

// NewPoolManager creates a new PoolManager instance
func NewPoolManager(ctx context.Context) *PoolManager {
	c := kubeclient.Get(ctx)
	pm := &PoolManager{
		client:              c,
		rootCtx:             ctx,
		replenishQueue:      workqueue.NewRateLimitingQueue(workqueue.DefaultControllerRateLimiter()),
		candidateByTemplate: make(map[string]map[string]*poolCandidate),
	}
	pm.registerReplicaSetEventHandler()
	return pm
}

func (pm *PoolManager) registerReplicaSetEventHandler() {
	_, err := rsclient.Get(pm.rootCtx).Informer().AddEventHandler(cache.FilteringResourceEventHandler{
		FilterFunc: reconciler.ChainFilterFuncs(
			reconciler.LabelFilterFunc(PoolLabel, "true", false),
		),
		Handler: cache.ResourceEventHandlerFuncs{
			AddFunc: func(obj any) {
				pm.upsertCandidateReplicaSet(obj)
			},
			UpdateFunc: func(_, newObj any) {
				pm.upsertCandidateReplicaSet(newObj)
			},
			// also call it when PoolLabel change to false
			DeleteFunc: func(obj any) {
				pm.removeCandidateReplicaSet(obj)
			},
		},
	})
	if err != nil {
		klog.Fatalf("failed to add replica set event handler: %v", err)
	}
}

// insert or update
func (pm *PoolManager) upsertCandidateReplicaSet(obj any) {
	rs, ok := obj.(*v1.ReplicaSet)
	if !ok || rs == nil {
		return
	}

	tplName := rs.GetLabels()[TPLLabel]
	tpl, err := config.GetTemplateByName(tplName)
	if err != nil || tpl == nil {
		pm.removeCandidateReplicaSet(rs)
		return
	}

	if rs.Status.ReadyReplicas == 0 {
		pm.removeCandidateReplicaSet(rs)
		return
	}

	rsImg := rs.Spec.Template.Spec.Containers[0].Image
	if rsImg != tpl.Image {
		pm.removeCandidateReplicaSet(rs)
		return
	}

	pm.candidateLock.Lock()
	defer pm.candidateLock.Unlock()

	if pm.candidateByTemplate[tplName] == nil {
		pm.candidateByTemplate[tplName] = make(map[string]*poolCandidate)
	}
	candidate := pm.candidateByTemplate[tplName][rs.Name]
	if candidate == nil {
		candidate = &poolCandidate{}
		pm.candidateByTemplate[tplName][rs.Name] = candidate
	}
	candidate.replicaset = rs.DeepCopy()
	candidate.reserved = false

	// for logs
	candidateNames := make([]string, 0, len(pm.candidateByTemplate[tplName]))
	for name, item := range pm.candidateByTemplate[tplName] {
		if item == nil {
			continue
		}
		if item.reserved {
			candidateNames = append(candidateNames, fmt.Sprintf("%s(reserved)", name))
			continue
		}
		candidateNames = append(candidateNames, name)
	}
	klog.V(1).Infof("pool candidates template=%s count=%d items=[%s]", tplName, len(candidateNames), strings.Join(candidateNames, ", "))
}

func (pm *PoolManager) removeCandidateReplicaSet(obj any) {
	var rs *v1.ReplicaSet
	switch t := obj.(type) {
	case *v1.ReplicaSet:
		rs = t
	case cache.DeletedFinalStateUnknown:
		deletedRS, ok := t.Obj.(*v1.ReplicaSet)
		if !ok {
			return
		}
		rs = deletedRS
	default:
		return
	}
	if rs == nil {
		return
	}

	tplName := rs.GetLabels()[TPLLabel]

	pm.candidateLock.Lock()
	defer pm.candidateLock.Unlock()

	if tplName == "" {
		return
	}

	if templateCandidates, exists := pm.candidateByTemplate[tplName]; exists {
		delete(templateCandidates, rs.Name)
	}

}

func (pm *PoolManager) reserveCandidateForTemplate(templateName string) *v1.ReplicaSet {
	pm.candidateLock.Lock()
	defer pm.candidateLock.Unlock()

	templateCandidates, ok := pm.candidateByTemplate[templateName]
	if !ok || len(templateCandidates) == 0 {
		return nil
	}

	var selected *poolCandidate
	for _, candidate := range templateCandidates {
		if candidate == nil || candidate.replicaset == nil {
			continue
		}
		if candidate.reserved {
			continue
		}
		if selected == nil || candidate.replicaset.CreationTimestamp.Before(&selected.replicaset.CreationTimestamp) {
			selected = candidate
		}
	}

	if selected == nil {
		return nil
	}

	selected.reserved = true
	return selected.replicaset.DeepCopy()
}

func (pm *PoolManager) removeCandidateByName(templateName, name string) {
	if templateName == "" || name == "" {
		return
	}

	pm.candidateLock.Lock()
	defer pm.candidateLock.Unlock()

	if templateCandidates, exists := pm.candidateByTemplate[templateName]; exists {
		delete(templateCandidates, name)
	}
}

func (pm *PoolManager) releaseReservedPool(templateName, name string) {
	if templateName == "" || name == "" {
		return
	}

	pm.candidateLock.Lock()
	defer pm.candidateLock.Unlock()

	if templateCandidates, exists := pm.candidateByTemplate[templateName]; exists {
		if candidate, ok := templateCandidates[name]; ok && candidate != nil {
			candidate.reserved = false
		}
	}
}

// AcquirePoolReplicaSet tries to acquire a pool replicaset for use
// It marks the pool replicaset as in-use and updates its metadata with actual sandbox data
// Returns error if concurrent update conflict occurs (optimistic locking)
func (pm *PoolManager) AcquirePoolReplicaSet(sb *Sandbox) (*v1.ReplicaSet, bool, error) {
	for {
		poolRS := pm.reserveCandidateForTemplate(sb.TemplateObj.Name)
		if poolRS == nil {
			createdRS, err := pm.createReplicaSet(sb)
			if err != nil || createdRS == nil {
				return nil, false, err
			}
			return createdRS, false, nil
		}

		acquiredRS, err := pm.adaptReplicasetToSandbox(poolRS, sb)
		if err != nil {
			klog.Warningf("failed to adapt pool replicaset=%s: error=%v", poolRS.Name, err)
			if errors.IsConflict(err) {
				pm.removeCandidateByName(sb.TemplateObj.Name, poolRS.Name)
			} else {
				// for other errors, just release the reservation and let it be retried in the next loop, no need to remove from candidate map since it might be a transient error and the replicaset is still valid
				pm.releaseReservedPool(sb.TemplateObj.Name, poolRS.Name)
			}
			continue
		}

		pm.replenishQueue.AddAfter("replenish", 1*time.Second)
		return acquiredRS, true, nil
	}
}

func (pm *PoolManager) adaptReplicasetToSandbox(rs *v1.ReplicaSet, sb *Sandbox) (*v1.ReplicaSet, error) {
	// Create a deep copy for update
	rsCopy := rs.DeepCopy()

	rsLabels := rsCopy.GetLabels()

	// for fallback if update failed
	originalID := sb.ID
	originalName := sb.Name

	// Retain the pool replicaset ID as sandbox ID
	sb.ID = rsLabels[IDLabel]
	sb.Name = rsCopy.Name

	// Reset as in-use and update labels
	rsLabels[UserLabel] = sb.User
	rsLabels[PoolLabel] = "false"
	rsCopy.SetLabels(rsLabels)

	// Reset annotations with actual sandbox data
	raw, _ := json.Marshal(sb)
	anns := rsCopy.GetAnnotations()
	if anns == nil {
		anns = make(map[string]string)
	}
	anns["sandbox-data"] = string(raw)
	rsCopy.SetAnnotations(anns)

	// Re-render pod spec from template so metadata-driven features (e.g. mitm) are applied
	freshRS, err := buildReplicaSet(sb)
	if err != nil {
		klog.Warningf("failed to re-render pod spec for sandbox %s, proceeding without update: %v", sb.Name, err)
	} else {
		rsCopy.Spec.Template.Spec = freshRS.Spec.Template.Spec
	}

	// Update the ReplicaSet in Kubernetes
	_, err = pm.client.AppsV1().ReplicaSets(config.Cfg.SandboxNamespace).Update(context.TODO(), rsCopy, v1meta.UpdateOptions{})

	if err != nil {
		sb.ID = originalID
		sb.Name = originalName
		klog.Error("failed to update pool replicaset error=", err, ", sandbox=", sb)
		return nil, err
	}

	klog.V(2).Infof("adapted pool replicaset %s for sandbox %s", rs.Name, sb.Name)
	return rsCopy, nil
}

// createReplicaSet creates a specified number of pool replicasets for a template
// force indicates whether to create at least one replicaset when not existing ones before list return empty
func (pm *PoolManager) createReplicaSet(sb *Sandbox) (*v1.ReplicaSet, error) {
	klog.Infof("creating replicasets for sandbox %v", sb)

	var createdRS *v1.ReplicaSet

	var err error
	if createdRS, err = pm.createSingleReplicaSet(sb); err != nil {
		return nil, err
	}

	return createdRS, nil
}

// createSingleReplicaSet creates a single pool replicaset
func (pm *PoolManager) createSingleReplicaSet(sb *Sandbox) (*v1.ReplicaSet, error) {
	// Create a pool sandbox with same configuration as user request
	rs, err := buildReplicaSet(sb)
	if err != nil {
		return nil, fmt.Errorf("failed to build replicaset error %v", err)
	}

	lbs := rs.GetLabels()
	lbs[PoolLabel] = "false"
	// Set pool labels
	if sb.IsPool {
		lbs[PoolLabel] = "true"
	}
	rs.SetLabels(lbs)

	// Create ReplicaSet in Kubernetes
	createdRS, err := pm.client.AppsV1().ReplicaSets(config.Cfg.SandboxNamespace).Create(context.TODO(), rs, v1meta.CreateOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to create pool replicaset in kubernetes replicaset: %+v error: %+v", rs, err)
	}

	klog.V(2).Infof("created pool replicaset for sandbox %v", sb)
	return createdRS, nil
}

func (pm *PoolManager) StartPoolSyncing() {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	go func() {
		for {
			select {
			case <-ticker.C:
				pm.replenishQueue.Add("replenish")
			case <-pm.rootCtx.Done():
				pm.replenishQueue.ShutDown()
				return
			}
		}
	}()

	for {
		item, shutdown := pm.replenishQueue.Get()
		if shutdown {
			klog.Info("Scaler stopping")
			return
		}

		func() {
			defer pm.replenishQueue.Done(item)
			pm.replenishPoolAsync()
			pm.replenishQueue.Forget(item)
		}()
	}
}

// ReplenishPoolAsync asynchronously replenishes the pool to the target size
func (pm *PoolManager) replenishPoolAsync() {
	tpls := config.Templates
	for _, tpl := range tpls {
		// skip if type is dynamic, since dynamic template image is determined by regexp pattern,
		// if what to use pool, define a static template with static image
		if tpl.Type == "dynamic" {
			continue
		}

		count := tpl.Pool.Size
		if count <= 0 {
			continue
		}

		// check exist pool rs
		sbs, err := pm.listAvailablePoolReplicaSets(tpl)
		if err != nil {
			klog.Errorf("failed to list available pool replicasets when CreatePoolReplicaSets %s, error: %v", tpl.Name, err)
			continue
		}

		// reduce exist pool rs
		count = count - len(sbs)

		if count <= 0 {
			continue
		}

		klog.V(2).Infof("creating pool replicasets for sandbox %v, count %v", tpl.Name, count)
		for i := 0; i < count; i++ {
			sb := GetDefaultSandbox()
			sb.Template = tpl.Name
			sb.User = config.SystemToken
			sb.IsPool = true
			if tpl.Pool.WarmupCmd != "" {
				// "tail, -f /dev/null"
				cmds := strings.Split(tpl.Pool.WarmupCmd, ",")
				sb.Cmd = cmds[0]
				if len(cmds) > 1 {
					sb.Args = strings.Split(cmds[1], " ")
				}
			}

			// init name and valid fields
			if err := sb.Make(); err != nil {
				klog.Errorf("failed to init pool sandbox for template %s: %v", tpl.Name, err)
				continue
			}

			_, err := pm.createReplicaSet(sb)
			if err != nil {
				klog.Errorf("failed to create pool replicaset %d/%d for sandbox %v: %v", i+1, count, sb, err)
			} else {
				klog.V(2).Infof("successfully replenished pool replicasets for template %s", tpl.Name)
			}
		}

	}
}

// ListAvailablePoolReplicaSets lists available pool replicasets for a given template
func (pm *PoolManager) listAvailablePoolReplicaSets(template *config.Template) ([]*v1.ReplicaSet, error) {
	// Build selector: sbx-pool=true, sbx-pool-in-use=false, sbx-template=<template>
	selector := labels.Set{
		PoolLabel: "true",
		TPLLabel:  template.Name,
	}.AsSelector()

	rsList, err := rsclient.Get(pm.rootCtx).Lister().List(selector)

	if err != nil {
		klog.Errorf("failed to list pool replicasets for template %s: %v", template.Name, err)
		return nil, err
	}

	result := []*v1.ReplicaSet{}
	for i := range rsList {
		rs := rsList[i]
		rsImg := rs.Spec.Template.Spec.Containers[0].Image

		// check image is same as template, in case some pool replicaset is created with old template image when template is updated
		// if not same, delete it and skip
		if rsImg != template.Image {
			klog.Warningf("deleting pool replicaset %s with outdated image %s for template %s", rs.Name, rsImg, template.Name)
			err := pm.client.AppsV1().ReplicaSets(config.Cfg.SandboxNamespace).Delete(context.TODO(), rs.Name, v1meta.DeleteOptions{})
			if err != nil {
				klog.Errorf("failed to delete pool replicaset %s with outdated image %s for template %s: %v", rs.Name, rsImg, template.Name, err)
			}
			continue
		}

		result = append(result, rs)
	}

	klog.V(2).Infof("found %d available pool replicasets for template %s", len(result), template.Name)
	return result, nil
}
