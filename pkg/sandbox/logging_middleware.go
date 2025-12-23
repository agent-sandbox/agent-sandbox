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
    "context"
    "time"

    "github.com/modelcontextprotocol/go-sdk/mcp"
    "k8s.io/klog/v2"
)

// createLoggingMiddleware creates an MCP middleware that logs method calls.
func createLoggingMiddleware() mcp.Middleware {
    return func(next mcp.MethodHandler) mcp.MethodHandler {
        return func(
            ctx context.Context,
            method string,
            req mcp.Request,
        ) (mcp.Result, error) {
            start := time.Now()
            sessionID := req.GetSession().ID()

            // Log request details.
            klog.Info("mcp request session:", sessionID, " method:", method)

            // Call the actual handler.
            result, err := next(ctx, method, req)

            // Log response details.
            duration := time.Since(start)

            if err != nil {
                klog.Errorf("mcp request failed session %s method %s duration %s err: %v", sessionID, method, duration, err)
            } else {
                klog.Infof("mcp request success session %s method %s duration %s ", sessionID, method, duration)
            }

            return result, err
        }
    }
}
