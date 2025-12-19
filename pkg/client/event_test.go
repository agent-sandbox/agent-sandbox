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

//Test CreateRecorder
import (
    "context"
    "fmt"
    "testing"
    "time"

    "github.com/agent-sandbox/agent-sandbox/pkg/config"
    corev1 "k8s.io/api/core/v1"
    v1meta "k8s.io/apimachinery/pkg/apis/meta/v1"
    "k8s.io/klog/v2"
    kubeclient "knative.dev/pkg/client/injection/kube/client"
    "knative.dev/pkg/injection"
)

func TestCreateRecorder(t *testing.T) {
    kubecfg := injection.ParseAndGetRESTConfigOrDie()
    klog.Info("Cluster info ", "host=", kubecfg.Host)
    rootCtx, cancel := context.WithCancel(context.Background())
    defer cancel()

    rootCtx = injection.WithNamespaceScope(rootCtx, config.Cfg.SandboxNamespace)
    rootCtx, _ = injection.Default.SetupInformers(rootCtx, kubecfg)

    recorder := CreateRecorderEventImpl(rootCtx, "test-component")

    if recorder == nil {
        t.Errorf("Expected recorder to be created, got nil")
    }

    //lastRequestTime: ""
    //lastResponseTime: ""
    kubeClient := kubeclient.Get(rootCtx)

    fieldSelector := fmt.Sprintf("involvedObject.name=%s,involvedObject.kind=ReplicaSet", "ymcas-infra-7d96554d66")

    listOptions := v1meta.ListOptions{
        FieldSelector: fieldSelector,
    }
    items, err := kubeClient.CoreV1().Events(config.Cfg.SandboxNamespace).List(context.TODO(), listOptions)
    t.Log(items.Items, err)

    return

    rs, err := kubeClient.AppsV1().ReplicaSets("ymcas").Get(context.TODO(), "ymcas-infra-7d96554d66", v1meta.GetOptions{})
    if err != nil {
        t.Errorf("Failed to get ReplicaSet: %v", err)
    }

    recorder.Eventf(rs, corev1.EventTypeNormal, "LastRequestTime", "")
    time.Sleep(3 * time.Second)
    recorder.Eventf(rs, corev1.EventTypeNormal, "LastResponseTime", "")

    recorder.Eventf(rs, corev1.EventTypeNormal, "LastRequestTime", "")
    time.Sleep(1 * time.Second)
    recorder.Eventf(rs, corev1.EventTypeNormal, "LastResponseTime", "")

    time.Sleep(100 * time.Second)

}
