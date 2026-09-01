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
	"strconv"
	"time"

	"github.com/agent-sandbox/agent-sandbox/pkg/config"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/wait"
	podclient "knative.dev/pkg/client/injection/kube/informers/core/v1/pod"
)

func AcquireDest(rootCtx context.Context, name string, port string) (*url.URL, error) {
	selector, _ := labels.Parse(fmt.Sprintf("sandbox=" + name))

	destIP := ""
	var destPod *v1.Pod
	var pods []*v1.Pod
	var err error

	// Wait for pods to be ready avoid faster than rs creation and caching issue
	if perr := wait.PollUntilContextTimeout(context.TODO(), 300*time.Millisecond, 5*time.Second, true, func(ctx context.Context) (bool, error) {
		pods, err = podclient.Get(rootCtx).Lister().Pods(config.Cfg.SandboxNamespace).List(selector)
		if err != nil {
			return false, err
		}
		if len(pods) == 0 {
			return false, fmt.Errorf("pod not found")
		}

		// wait for pod to be got ip
		destPod = pods[rand.Intn(len(pods))]
		destIP = destPod.Status.PodIP
		if destIP == "" {
			return false, fmt.Errorf("sandbox pod IP not found")
		}

		return true, nil
	}); perr != nil {
		return nil, fmt.Errorf("timeout waiting for get pods for sandbox %v error: %v", name, perr)
	}

	// read port from first container port if not specified
	if port == "0" {
		port = strconv.FormatInt(int64(destPod.Spec.Containers[0].Ports[0].ContainerPort), 10)
	}

	targetURL, _ := url.Parse(fmt.Sprintf("http://%s:%s", destIP, port))

	return targetURL, nil
}
