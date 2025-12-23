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
    "net/http"

    "github.com/modelcontextprotocol/go-sdk/mcp"
    "k8s.io/klog/v2"
)

func (a *Handler) McpSseHandler() *mcp.StreamableHTTPHandler {
    server := mcp.NewServer(&mcp.Implementation{
        Name:    "agent-sandbox-mcp-server",
        Version: "1.0.0",
    }, nil)

    // Add MCP-level logging middleware.
    server.AddReceivingMiddleware(createLoggingMiddleware())

    // Add the tools.
    mcp.AddTool(server, &mcp.Tool{
        Name:        "createSandbox",
        Description: "Create a new Sandbox for execution python code, browser use, etc. Return Tools schema of Sandbox  for further use by call sandboxExecutor Tool.",
    }, a.CreateSandboxTool)

    mcp.AddTool(server, &mcp.Tool{
        Name:        "getSandbox",
        Description: "Get the details of a Sandbox by name",
    }, a.GetSandboxTool)

    mcp.AddTool(server, &mcp.Tool{
        Name:        "deleteSandbox",
        Description: "Delete a Sandbox by name",
    }, a.DelSandboxTool)

    mcp.AddTool(server, &mcp.Tool{
        Name:        "sandboxExecutor",
        Description: "Execute commands or actions inside the Sandbox",
    }, a.executorHandler)

    // Create the streamable HTTP handler.
    handler := mcp.NewStreamableHTTPHandler(func(req *http.Request) *mcp.Server {
        return server
    }, &mcp.StreamableHTTPOptions{
        Stateless: true,
        //JSONResponse: true,
    })

    return handler
}

func (a *Handler) CreateSandboxTool(ctx context.Context, req *mcp.CallToolRequest, sandbox *SandboxBase) (*mcp.CallToolResult, any, error) {
    klog.V(2).Infof("Create sandbox opts %v", sandbox)

    exist := a.controller.Get(sandbox.Name)
    if exist != nil {
        return nil, nil, fmt.Errorf("sandbox %s already exists", sandbox.Name)
    }

    err := a.controller.Create(&Sandbox{
        SandboxBase: *sandbox,
    })

    if err != nil {
        klog.Errorf("Failed to create sandbox, err: %v", err)
        return nil, nil, fmt.Errorf("failed to create new sandbox, error: %v", err)
    }

    stools, err := a.sandboxTools(ctx, sandbox.Name)
    if err != nil {
        stools = fmt.Sprintf("failed to get Sandbox tools error: %s, please retry to get Sandbox Tools by call getSandbox Tool", err.Error())
    }
    stools = fmt.Sprintf("\nYou can use the following Tools to interact with the Sandbox:\n%s", stools)

    return &mcp.CallToolResult{
        Content: []mcp.Content{
            &mcp.TextContent{Text: fmt.Sprintf("Sandbox created, Sandbox name:%s, %s", sandbox.Name, stools)},
        },
    }, nil, nil
}

func (a *Handler) GetSandboxTool(ctx context.Context, req *mcp.CallToolRequest, sandbox *SandboxBase) (*mcp.CallToolResult, any, error) {
    klog.V(2).Infof("Get sandbox tool by name=%s", sandbox.Name)

    sb := a.controller.Get(sandbox.Name)
    if sb == nil {
        return nil, nil, fmt.Errorf("sandbox %s not found", sandbox.Name)
    }

    sbJson, err := json.Marshal(sb)
    if err != nil {
        return nil, nil, fmt.Errorf("failed to marshal sandbox %s: %v", sandbox.Name, err)
    }

    stools, err := a.sandboxTools(ctx, sandbox.Name)
    if err != nil {
        stools = fmt.Sprintf("failed to get Sandbox tools error: %s, please retry to get Sandbox Tools by call getSandbox Tool", err.Error())
    }
    stools = fmt.Sprintf("\nYou can use the following Tools to interact with the Sandbox:\n%s", stools)

    text := fmt.Sprintf("Sandbox details:\n%s\n%s", string(sbJson), stools)
    return &mcp.CallToolResult{
        Content: []mcp.Content{
            &mcp.TextContent{Text: text},
        },
    }, nil, nil
}

func (a *Handler) DelSandboxTool(ctx context.Context, req *mcp.CallToolRequest, sandbox *SandboxBase) (*mcp.CallToolResult, any, error) {
    klog.V(2).Infof("Delete sandbox tool by name=%s", sandbox.Name)

    err := a.controller.Delete(sandbox.Name)
    if err != nil {
        return nil, nil, fmt.Errorf("failed to delete Sandbox %s: %v", sandbox.Name, err)
    }

    return &mcp.CallToolResult{
        Content: []mcp.Content{
            &mcp.TextContent{Text: fmt.Sprintf("Sandbox %s deleted successfully", sandbox.Name)},
        },
    }, nil, nil
}
