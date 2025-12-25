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
    "bytes"
    "encoding/json"
    "fmt"
    "time"

    "github.com/agent-sandbox/agent-sandbox/pkg/config"
    "k8s.io/apimachinery/pkg/labels"
    "k8s.io/apimachinery/pkg/util/wait"
    "k8s.io/client-go/kubernetes"
    "k8s.io/klog/v2"
    kubeclient "knative.dev/pkg/client/injection/kube/client"
    rsclient "knative.dev/pkg/client/injection/kube/informers/apps/v1/replicaset"
    podclient "knative.dev/pkg/client/injection/kube/informers/core/v1/pod"

    "context"
    "text/template"

    "sigs.k8s.io/yaml"

    v1 "k8s.io/api/apps/v1"
    v1core "k8s.io/api/core/v1"
    v1meta "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type Controller struct {
    client  kubernetes.Interface
    rootCtx context.Context
}

func NewController(ctx context.Context) *Controller {
    sh := &Controller{
        rootCtx: ctx,
    }
    sh.client = kubeclient.Get(ctx)
    return sh
}

func (s *Controller) Get(name string) *Sandbox {
    rs, err := s.client.AppsV1().ReplicaSets(config.Cfg.SandboxNamespace).Get(context.TODO(), name, v1meta.GetOptions{})
    if err != nil {
        return nil
    }
    raw := rs.Annotations["sandbox-data"]
    sb := &Sandbox{}
    json.Unmarshal([]byte(raw), sb)
    return sb
}

func (s *Controller) List() []*Sandbox {
    selector, _ := labels.Parse("owner=agent-sandbox")
    rss, err := rsclient.Get(s.rootCtx).Lister().List(selector)
    if err != nil {
        return nil
    }
    var sandboxes []*Sandbox
    for _, rs := range rss {
        raw := rs.Annotations["sandbox-data"]
        sb := &Sandbox{}
        json.Unmarshal([]byte(raw), sb)
        sandboxes = append(sandboxes, sb)
    }
    return sandboxes
}

func (s *Controller) Create(sb *Sandbox) error {
    kubeClient := kubeclient.Get(s.rootCtx)
    if kubeClient == nil {
        return fmt.Errorf("failed to get kube client, kubeClient is nil")
    }

    sb.Make()
    raw, _ := json.Marshal(sb)
    tplData := SandboxKube{
        Sandbox:   sb,
        RawData:   string(raw),
        Namespace: config.Cfg.SandboxNamespace,
    }
    tmpl, err := template.New("rs").Parse(SandboxDeployTemplate)
    if err != nil {
        return fmt.Errorf("parse template fail: %v", err)
    }

    var buf bytes.Buffer
    err = tmpl.Execute(&buf, tplData)
    if err != nil {
        return fmt.Errorf("execute template fail: %v", err)
    }

    rsObj := &v1.ReplicaSet{}
    if err = yaml.Unmarshal(buf.Bytes(), rsObj); err != nil {
        return fmt.Errorf("unmarshal template fail: %v", err)
    }

    if _, err := s.client.AppsV1().ReplicaSets(config.Cfg.SandboxNamespace).Create(context.TODO(), rsObj, v1meta.CreateOptions{}); err != nil {
        return fmt.Errorf("create replicaset fail: %v", err)
    }

    if perr := wait.PollUntilContextTimeout(context.TODO(), 500*time.Millisecond, 5*time.Minute, true, func(ctx context.Context) (bool, error) {
        rsCreated, err := s.client.AppsV1().ReplicaSets(config.Cfg.SandboxNamespace).Get(context.TODO(), sb.Name, v1meta.GetOptions{})
        if err != nil {
            return false, err
        }
        // Check if the ReplicaSet is ready
        replicas := *rsCreated.Spec.Replicas
        if replicas == rsCreated.Status.ReadyReplicas {
            klog.Infof("ReplicaSet %s in namespace %s is ready. Desired: %d, Ready: %d",
                sb.Name, config.Cfg.SandboxNamespace, replicas, rsCreated.Status.ReadyReplicas)
            return true, nil
        } else {
            klog.V(2).Infof("ReplicaSet %s in namespace %s is NOT ready. Desired: %d, Ready: %d\n",
                sb.Name, config.Cfg.SandboxNamespace, replicas, rsCreated.Status.ReadyReplicas)
            return false, nil
        }
    }); perr != nil {
        return fmt.Errorf("timeout waiting for replicaset to be ready: %v", perr)
    }

    return nil
}

func (s *Controller) GetInstances(name string) []*v1core.Pod {
    selector, _ := labels.Parse(fmt.Sprintf("sandbox=%s", name))
    pods, err := podclient.Get(context.TODO()).Lister().List(selector)
    if err != nil {
        return nil
    }
    return pods
}

func (s *Controller) Delete(name string) error {
    err := s.client.AppsV1().ReplicaSets(config.Cfg.SandboxNamespace).Delete(context.TODO(), name, v1meta.DeleteOptions{})
    return err
}
