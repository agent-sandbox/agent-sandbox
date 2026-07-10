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
	"errors"
	"net"
	"net/http"
	"syscall"
	"time"

	"k8s.io/klog/v2"
)

const (
	sandboxProxyDialMaxAttempts = 20
	sandboxProxyDialRetryDelay  = 300 * time.Millisecond
)

func dialContextWithRetry(dialer *net.Dialer) func(context.Context, string, string) (net.Conn, error) {
	return func(ctx context.Context, network, address string) (net.Conn, error) {
		var lastErr error
		for attempt := 1; attempt <= sandboxProxyDialMaxAttempts; attempt++ {
			conn, err := dialer.DialContext(ctx, network, address)
			if err == nil || !isConnectionRefused(err) {
				return conn, err
			}
			klog.V(1).Infof("retry sandbox proxy dial attempt=%d network=%s address=%s error=%v", attempt, network, address, err)

			lastErr = err
			// The pod can have an IP before the sandbox service starts listening on its port.
			select {
			case <-time.After(sandboxProxyDialRetryDelay):
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}
		return nil, lastErr
	}
}

func isConnectionRefused(err error) bool {
	return errors.Is(err, syscall.ECONNREFUSED)
}

func getTransport() *http.Transport {
	dialer := &net.Dialer{
		Timeout:   5 * time.Second,
		KeepAlive: 30 * time.Second,
	}

	transport := &http.Transport{
		DialContext: dialContextWithRetry(dialer),

		TLSHandshakeTimeout: 5 * time.Second,

		ResponseHeaderTimeout: 35 * time.Second,

		MaxIdleConns:        2000,
		MaxIdleConnsPerHost: 30,
		IdleConnTimeout:     330 * time.Second,
	}

	return transport
}
