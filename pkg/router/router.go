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
	"net/http"
	"net/http/httputil"
	"strings"
	"time"

	"github.com/agent-sandbox/agent-sandbox/pkg/activator"
	"k8s.io/client-go/kubernetes"
	"k8s.io/klog/v2"
	kubeclient "knative.dev/pkg/client/injection/kube/client"
)

type SandboxRouter struct {
	SharedTransport http.RoundTripper
	client          kubernetes.Interface
	rootCtx         context.Context
	activator       *activator.Activator
}

func NewSandboxRouter(ctx context.Context, a *activator.Activator) *SandboxRouter {
	transport := getTransport()

	sr := &SandboxRouter{
		SharedTransport: transport,
		rootCtx:         ctx,
		activator:       a,
	}
	sr.client = kubeclient.Get(ctx)

	return sr
}

func (s *SandboxRouter) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	port := r.URL.Query().Get("port")

	if name == "" {
		http.Error(w, "missing sandbox name in url", http.StatusBadRequest)
		klog.Error("Missing sandbox name in url: ", r.RequestURI)
		return
	}

	if port == "" {
		port = "0"
	}

	prefixToStrip := fmt.Sprintf("/sandbox/%s", name)

	klog.Info("proxy router ", name, " port=", port, " prefixToStrip=", prefixToStrip)

	//remove port query param from request url
	q := r.URL.Query()
	q.Del("port")
	r.URL.RawQuery = q.Encode()

	targetURL, err := AcquireDest(s.rootCtx, name, port)
	if err != nil {
		http.Error(w, fmt.Sprintf("failed to acquire destination ip for sandbox %s: %v, possible instance is not ready yet, please retry later or checking pod status!", name, err), http.StatusBadGateway)
		return
	}
	s.activator.RecordActive(name)

	proxy := httputil.NewSingleHostReverseProxy(targetURL)
	proxy.Transport = s.SharedTransport
	proxy.ModifyResponse = func(resp *http.Response) error {
		klog.V(2).Infof("routed sandbox request, response status %v, url %s %s", resp.StatusCode, resp.Request.Method, resp.Request.URL)
		return nil
	}

	proxy.Director = func(req *http.Request) {
		req.URL.Scheme = targetURL.Scheme
		req.URL.Host = targetURL.Host
		req.Host = targetURL.Host
		req.URL.Path = strings.TrimPrefix(req.URL.Path, prefixToStrip)
		klog.Info("Proxying request to sandbox ", "name=", name, " url=", req.Method, req.URL)
		req.Header.Set("X-Request-ID", fmt.Sprintf("%d", time.Now().UnixNano()))
	}

	proxy.ErrorHandler = func(rw http.ResponseWriter, req *http.Request, err error) {
		klog.Error("Proxy error for sandbox ", name, " error=", err)
		http.Error(rw, "upstream return error "+err.Error(), http.StatusBadRequest)
	}

	rw := NewResponseWare(w, http.StatusOK)
	proxy.ServeHTTP(rw, r)
	klog.Info("route completed request to sandbox ", name)
	return
}
