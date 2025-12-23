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
    "fmt"

    "github.com/agent-sandbox/agent-sandbox/pkg/config"
    corev1 "k8s.io/api/core/v1"
    v1meta "k8s.io/apimachinery/pkg/apis/meta/v1"
    "k8s.io/klog/v2"
    kubeclient "knative.dev/pkg/client/injection/kube/client"
    rsclient "knative.dev/pkg/client/injection/kube/informers/apps/v1/replicaset"

    "k8s.io/client-go/tools/record"
)

const (
    ComponentName = "agent-sandbox-activator"
)

const (
    EventTypeLastRequest string = "LastRequestTime"

    EventTypeLastResponse string = "LastResponseTime"
)

type Activator struct {
    rootCtx  context.Context
    recorder record.EventRecorder
}

func NewActivator(ctx context.Context) *Activator {
    recorder := getRecorder(ctx)
    a := &Activator{
        rootCtx:  ctx,
        recorder: recorder,
    }
    return a
}

func (a *Activator) RecordLastEvent(eventType string, name string) {
    rs, err := rsclient.Get(a.rootCtx).Lister().ReplicaSets(config.Cfg.SandboxNamespace).Get(name)
    if err != nil {
        klog.ErrorS(err, "Failed to record event ", "name", name)
        return
    }
    annotations := make(map[string]string)
    annotations["sandbox-data"] = rs.Annotations["sandbox-data"]
    a.recorder.AnnotatedEventf(rs, annotations, corev1.EventTypeNormal, eventType, "")
}

// GetLastRequestTime gets the last request event for the given sandbox name.
// return lastTimestamp of EventTypeLastRequest
func (a *Activator) GetLastRequestTime(name string) int64 {
    kubeClient := kubeclient.Get(a.rootCtx)

    fieldSelector := fmt.Sprintf("involvedObject.name=%s,involvedObject.kind=ReplicaSet", name)

    listOptions := v1meta.ListOptions{
        FieldSelector: fieldSelector,
    }

    items, err := kubeClient.CoreV1().Events(config.Cfg.SandboxNamespace).List(context.TODO(), listOptions)
    if err != nil {
        klog.ErrorS(err, "Failed to get last request event", "name", name)
        return 0
    }
    for _, item := range items.Items {
        if item.Reason == EventTypeLastRequest {
            return item.LastTimestamp.Unix()
        }
    }

    return 0
}
