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
    "encoding/json"
    "fmt"

    "github.com/agent-sandbox/agent-sandbox/pkg/router"
    "github.com/modelcontextprotocol/go-sdk/mcp"
    "k8s.io/klog/v2"
)

type SandboxTool struct {
    SandboxName string `json:"sandbox_name,omitempty" required:"true" jsonschema:"The name of the Sandbox to execute the command in"`

    ToolName string `json:"tool_name,omitempty" required:"true" jsonschema:"The name of the Tool to execute inside the Sandbox"`

    Arguments interface{} `json:"arguments,omitempty" jsonschema:"The arguments or command of the Tool to execute inside the Sandbox"`
}

func (a *Handler) executorHandler(ctx context.Context, req *mcp.CallToolRequest, tool *SandboxTool) (*mcp.CallToolResult, any, error) {
    klog.V(2).Infof("Sandbox executor called with tool: %v", tool)
    if tool.SandboxName == "" {
        return nil, nil, fmt.Errorf("sandbox_name is required")
    }
    if tool.ToolName == "" {
        return nil, nil, fmt.Errorf("tool_name is required")
    }

    session, err := a.acquireClientSession(ctx, tool.SandboxName)
    if err != nil {
        return nil, nil, fmt.Errorf("failed to acquire client session for sandbox %s: %v", tool.SandboxName, err)
    }

    result, err := session.CallTool(ctx, &mcp.CallToolParams{
        Name:      tool.ToolName,
        Arguments: tool.Arguments,
    })

    if err != nil {
        return nil, nil, fmt.Errorf("failed to call tool %s in sandbox %s: %v", tool.ToolName, tool.SandboxName, err)
    }

    resultText := ""
    for _, content := range result.Content {
        if textContent, ok := content.(*mcp.TextContent); ok {
            resultText += textContent.Text
        }
    }

    return &mcp.CallToolResult{
        Content: []mcp.Content{
            &mcp.TextContent{
                Text: resultText,
            },
        },
    }, nil, nil
}

func (a *Handler) acquireClientSession(ctx context.Context, name string) (*mcp.ClientSession, error) {
    var session *mcp.ClientSession

    sbIP, err := router.AcquireDest(a.rootCtx, name)
    if err != nil {
        return nil, fmt.Errorf("failed to acquire destination IP for sandbox %s: %v", name, err)
    }

    url := fmt.Sprintf("%s/mcp", sbIP.String())
    klog.V(2).Infof("Connecting to MCP server at %s", url)

    // Get session from cache if exists
    if val, ok := a.sessionCache.sessions.Load(url); ok {
        session = val.(*mcp.ClientSession)
        //test session validity
        err := session.Ping(ctx, nil)
        if err == nil {
            klog.V(2).Infof("Reusing existing MCP client session for %s", url)
            return session, nil
        }
        klog.V(2).Infof("Existing MCP client session for %s is invalid, reconnecting...", url)
        // Remove invalid session from cache
        a.sessionCache.sessions.Delete(url)
    }

    // Create an MCP client.
    client := mcp.NewClient(&mcp.Implementation{
        Name:    "sandbox-mcp-client",
        Version: "1.0.0",
    }, nil)

    // Connect to the server.
    session, err = client.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: url}, nil)
    if err != nil {
        return nil, fmt.Errorf("failed to connect to MCP server at %s: %v", url, err)
    }
    a.sessionCache.sessions.Store(url, session)

    return session, err
}

func (a *Handler) sandboxTools(ctx context.Context, name string) (string, error) {
    session, err := a.acquireClientSession(ctx, name)
    if err != nil {
        return "", fmt.Errorf("failed to acquire client session for sandbox %s: %v", name, err)
    }

    toolsResult, err := session.ListTools(ctx, nil)
    if err != nil {
        return "", fmt.Errorf("failed to list tools in sandbox %s: %v", name, err)
    }

    toolsJson, err := json.MarshalIndent(toolsResult.Tools, "", "  ")
    if err != nil {
        return "", fmt.Errorf("failed to marshal tools list for sandbox %s: %v", name, err)
    }

    return string(toolsJson), nil
}
