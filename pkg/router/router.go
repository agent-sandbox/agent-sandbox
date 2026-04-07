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
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httputil"
	"regexp"
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
	s.activator.RecordLastEvent(activator.EventTypeLastRequest, name)

	proxy := httputil.NewSingleHostReverseProxy(targetURL)
	proxy.Transport = s.SharedTransport
	proxy.ModifyResponse = func(resp *http.Response) error {
		klog.V(2).Infof("routed sandbox request, url %s %s, response status %v", resp.Request.Method, resp.Request.URL, resp.StatusCode)
		if strings.Contains(resp.Header.Get("Content-Type"), "text/html") {
			body, err := io.ReadAll(resp.Body)
			resp.Body.Close()
			if err != nil {
				return err
			}
			base := fmt.Sprintf(`<base href="/sandbox/%s/">`, name)
			modified := injectBaseTag(body, base)
			resp.Body = io.NopCloser(bytes.NewReader(modified))
			resp.ContentLength = int64(len(modified))
			resp.Header.Del("Content-Encoding")
		}
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
	s.activator.RecordLastEvent(activator.EventTypeLastResponse, name)
	klog.Info("route request to sandbox ", name, " completed")
	return
}

// subdomainPortRe matches an optional port suffix in the subdomain label.
// "sbx-myapp-abc123-6080" → name="sbx-myapp-abc123", port="6080"
// "sbx-myapp-abc123"      → name="sbx-myapp-abc123", port=""
var subdomainPortRe = regexp.MustCompile(`^(.+)-(\d+)$`)

// SubdomainServeHTTP handles requests arriving via {name}[-port].{SandboxProxyDomain}.
// The sandbox name and optional port are extracted from the Host header.
// Encoding the port in the subdomain (e.g. {name}-6080.s.yourdomain.com) ensures
// all subsequent asset and WebSocket requests also reach the correct port.
func (s *SandboxRouter) SubdomainServeHTTP(w http.ResponseWriter, r *http.Request) {
	host := r.Host
	if idx := strings.LastIndex(host, ":"); idx != -1 {
		host = host[:idx]
	}
	label := strings.SplitN(host, ".", 2)[0]

	if label == "" {
		http.Error(w, "cannot resolve sandbox name from host", http.StatusBadRequest)
		return
	}

	name := label
	port := "0"
	if m := subdomainPortRe.FindStringSubmatch(label); m != nil {
		name = m[1]
		port = m[2]
	}

	// ?port= query param still works as an override if needed
	if qport := r.URL.Query().Get("port"); qport != "" {
		port = qport
	}
	q := r.URL.Query()
	q.Del("port")
	r.URL.RawQuery = q.Encode()

	targetURL, err := AcquireDest(s.rootCtx, name, port)
	if err != nil {
		http.Error(w, fmt.Sprintf("failed to acquire destination for sandbox %s: %v", name, err), http.StatusBadGateway)
		return
	}
	s.activator.RecordLastEvent(activator.EventTypeLastRequest, name)

	proxy := httputil.NewSingleHostReverseProxy(targetURL)
	proxy.Transport = s.SharedTransport
	proxy.Director = func(req *http.Request) {
		req.URL.Scheme = targetURL.Scheme
		req.URL.Host = targetURL.Host
		req.Host = targetURL.Host
		// no path stripping — app receives the original path unchanged
		klog.Info("Subdomain proxying request to sandbox ", "name=", name, " url=", req.Method, req.URL)
		req.Header.Set("X-Request-ID", fmt.Sprintf("%d", time.Now().UnixNano()))
	}
	proxy.ErrorHandler = func(rw http.ResponseWriter, req *http.Request, err error) {
		klog.Error("Subdomain proxy error for sandbox ", name, " error=", err)
		http.Error(rw, "upstream error: "+err.Error(), http.StatusBadGateway)
	}

	rw := NewResponseWare(w, http.StatusOK)
	proxy.ServeHTTP(rw, r)
	s.activator.RecordLastEvent(activator.EventTypeLastResponse, name)
	klog.Info("subdomain route request to sandbox ", name, " completed")
}

// injectBaseTag inserts a <base href="..."> tag as the first child of <head>.
// If no <head> tag is found, the base tag is prepended to the document.
func injectBaseTag(body []byte, base string) []byte {
	s := string(body)
	// find <head> with optional attributes (case-insensitive)
	lower := strings.ToLower(s)
	idx := strings.Index(lower, "<head")
	if idx != -1 {
		// advance past the closing '>' of the <head> tag
		end := strings.Index(s[idx:], ">")
		if end != -1 {
			insert := idx + end + 1
			return []byte(s[:insert] + base + s[insert:])
		}
	}
	return []byte(base + s)
}
