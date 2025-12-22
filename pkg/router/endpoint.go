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

package router

import (
    "context"
    "fmt"
    "math/rand"
    "net/url"

    "k8s.io/apimachinery/pkg/labels"
    podclient "knative.dev/pkg/client/injection/kube/informers/core/v1/pod"
)

func acquireDest(ctx context.Context, name string) (*url.URL, error) {
    selector, _ := labels.Parse(fmt.Sprintf("sandbox=" + name))
    pods, err := podclient.Get(ctx).Lister().List(selector)
    if err != nil || len(pods) == 0 {
        return nil, fmt.Errorf("sandbox pods not found")
    }

    pod := pods[rand.Intn(len(pods))]
    ip := pod.Status.PodIP
    if ip == "" {
        return nil, fmt.Errorf("sandbox pod IP not found")
    }

    targetURL, _ := url.Parse(fmt.Sprintf("http://%s:%s", ip, "8080"))

    return targetURL, nil
}
