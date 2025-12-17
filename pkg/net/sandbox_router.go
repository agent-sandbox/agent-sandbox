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

package net

import (
	"context"
	"fmt"
	"math/rand"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/kubernetes"
	kubeclient "knative.dev/pkg/client/injection/kube/client"
	podclient "knative.dev/pkg/client/injection/kube/informers/core/v1/pod"
)

type SandboxRouter struct {
	SharedTransport http.RoundTripper
	client          kubernetes.Interface
	rootCtx         context.Context
}

func NewSandboxRouter(ctx context.Context) *SandboxRouter {
	transport := &http.Transport{
		DialContext: (&net.Dialer{
			Timeout:   5 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,

		TLSHandshakeTimeout: 5 * time.Second,

		ResponseHeaderTimeout: 300 * time.Second,

		MaxIdleConns:        100,
		MaxIdleConnsPerHost: 20,
		IdleConnTimeout:     90 * time.Second,
	}

	sr := &SandboxRouter{
		SharedTransport: transport,
		rootCtx:         ctx,
	}
	sr.client = kubeclient.Get(ctx)

	return sr
}

func (s *SandboxRouter) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	fmt.Printf("DynamicProxyRouter ServeHTTP name=%s\n", name)

	prefixToStrip := "/sandbox/" + name

	selector, _ := labels.Parse(fmt.Sprintf("sandbox=" + name))
	pods, err := podclient.Get(s.rootCtx).Lister().List(selector)
	if err != nil || len(pods) == 0 {
		http.Error(w, "Sandbox pods not found", http.StatusNotFound)
		return
	}

	pod := pods[rand.Intn(len(pods))]
	ip := pod.Status.PodIP
	if ip == "" {
		http.Error(w, "Sandbox pod IP not found", http.StatusNotFound)
		return
	}

	targetURL, _ := url.Parse("http://" + ip)

	proxy := httputil.NewSingleHostReverseProxy(targetURL)
	proxy.Transport = s.SharedTransport

	proxy.Director = func(req *http.Request) {
		req.URL.Scheme = targetURL.Scheme
		req.URL.Host = targetURL.Host
		req.Host = targetURL.Host

		req.URL.Path = strings.TrimPrefix(req.URL.Path, prefixToStrip)
		req.Header.Set("X-Request-ID", fmt.Sprintf("%d", time.Now().UnixNano()))
	}

	proxy.ServeHTTP(w, r)
	fmt.Printf("DynamicProxyRouter ServeHTTP done name=%s\n", name)
	return
}
