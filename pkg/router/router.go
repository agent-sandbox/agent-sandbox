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
    kubeclient "knative.dev/pkg/client/injection/kube/client"
)

type SandboxRouter struct {
    SharedTransport http.RoundTripper
    client          kubernetes.Interface
    rootCtx         context.Context
    activator       *activator.Activator
}

func NewSandboxRouter(ctx context.Context) *SandboxRouter {
    transport := getTransport()
    a := activator.NewActivator(ctx)

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
    fmt.Printf("DynamicProxyRouter ServeHTTP name=%s\n", name)
    s.activator.RecordLastRequest(name)

    prefixToStrip := "/sandbox/" + name

    targetURL, err := acquireDest(s.rootCtx, name)
    if err != nil {
        http.Error(w, fmt.Sprintf("failed to acquire destination ip for sandbox %s: %v", name, err), http.StatusBadGateway)
        return
    }

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
    s.activator.RecordLastResponse(name)
    fmt.Printf("DynamicProxyRouter ServeHTTP done name=%s\n", name)
    return
}
