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

package client

import (
    "context"

    "github.com/agent-sandbox/agent-sandbox/pkg/config"
    corev1 "k8s.io/api/core/v1"
    "k8s.io/apimachinery/pkg/watch"
    "k8s.io/client-go/kubernetes/scheme"
    v1 "k8s.io/client-go/kubernetes/typed/core/v1"
    "k8s.io/client-go/tools/record"
    "k8s.io/klog/v2"
    kubeclient "knative.dev/pkg/client/injection/kube/client"
    "knative.dev/pkg/controller"
)

func CreateRecorderEventImpl(ctx context.Context, agentName string) record.EventRecorder {
    recorder := controller.GetEventRecorder(ctx)
    if recorder == nil {
        // Create event broadcaster
        klog.V(2).Infof("Creating event broadcaster")
        eventBroadcaster := record.NewBroadcaster()

        watches := []watch.Interface{
            eventBroadcaster.StartLogging(klog.Infof),
            eventBroadcaster.StartRecordingToSink(
                &v1.EventSinkImpl{Interface: kubeclient.Get(ctx).CoreV1().Events(config.Cfg.SandboxNamespace)}),
        }
        recorder = eventBroadcaster.NewRecorder(scheme.Scheme, corev1.EventSource{Component: agentName})
        go func() {
            <-ctx.Done()
            for _, w := range watches {
                w.Stop()
            }
        }()
    }
    return recorder
}
