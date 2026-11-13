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

package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/agent-sandbox/agent-sandbox/pkg/auth"
	"github.com/agent-sandbox/agent-sandbox/pkg/sandbox"
	"github.com/agent-sandbox/agent-sandbox/pkg/telemetry"
	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"k8s.io/klog/v2"
)

// Deprecated, recommend to use skills instead.

func (a *Handler) McpSseHandler() *mcp.StreamableHTTPHandler {
	server := mcp.NewServer(&mcp.Implementation{
		Name: "agent-sandbox-mcp-server",

		Version: "1.0.0",
	}, nil)

	// Add MCP-level logging middleware.
	server.AddReceivingMiddleware(createLoggingMiddleware())

	// Add the tools.
	inputSchema := &jsonschema.Schema{
		Type: "object",
		Properties: map[string]*jsonschema.Schema{
			"name":     {Type: "string", Description: "The name of the Sandbox to create. You can leave it empty to auto generate or specify it yourself with contextual meaning. only contain lowercase letters numbers and '-', max length 50, add timestamp suffix to avoid name conflict, e.g. 'sandbox-execute-code-1766483780."},
			"template": {Type: "string", Description: "The template name to use for creating the Sandbox. Must be one of the predefined templates. "},
		},
	}
	inputSchemaName := &jsonschema.Schema{
		Type: "object",
		Properties: map[string]*jsonschema.Schema{
			"name": {Type: "string", Description: "The name of the Sandbox"},
		},
	}
	mcp.AddTool(server, &mcp.Tool{
		Name:        "createSandbox",
		Description: "Create a new Sandbox for execution python or javascript code, browser use, etc. Return Tools schema of Sandbox  for further use by call sandboxExecutor.",
		InputSchema: inputSchema,
	}, a.CreateSandboxTool)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "getSandbox",
		Description: "Get the details of a Sandbox by name, include the Tools schema for further use by call sandboxExecutor.",
		InputSchema: inputSchemaName,
	}, a.GetSandboxTool)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "listSandbox",
		Description: "List all existing Sandboxes. return name and basic info of each Sandbox, you can use getSandbox Tool to get more details and Tools schema of each Sandbox.",
		InputSchema: &jsonschema.Schema{
			Type: "object",
			Properties: map[string]*jsonschema.Schema{
				"template": {Type: "string", Description: "The template of the Sandboxes, optional filter by template name."},
			},
		},
	}, a.ListSandboxTool)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "deleteSandbox",
		Description: "Delete a Sandbox by name. Best practice to delete the Sandbox after all tasks are done to free resources.",
		InputSchema: inputSchemaName,
	}, a.DelSandboxTool)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "sandboxExecutor",
		Description: "Execute a Tool inside the specified Sandbox.",
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

func (a *Handler) CreateSandboxTool(ctx context.Context, req *mcp.CallToolRequest, input *sandbox.SandboxBase) (*mcp.CallToolResult, any, error) {
	user := auth.GetUserTokenFromContext(ctx)
	if user == "" {
		return nil, nil, fmt.Errorf("failed to get user from context")
	}

	sb := sandbox.GetDefaultSandbox()
	sb.User = user

	sb.SandboxBase = *input

	klog.V(2).Infof("Create sandbox opts %v", sb)

	exist, _ := a.controller.Get(sb.Name)
	if exist != nil {
		return nil, nil, fmt.Errorf("sandbox %s already exists", sb.Name)
	}

	// init name and valid fields
	if err := sb.Make(); err != nil {
		return nil, nil, fmt.Errorf("error create sandbox: %v", err)
	}

	sbCreated, err := a.controller.Create(sb)

	if err != nil {
		klog.Errorf("Failed to create sandbox, err: %v", err)
		return nil, nil, fmt.Errorf("failed to create new sandbox, error: %v", err)
	}

	stools, err := a.sandboxTools(ctx, sb.Name)
	if err != nil {
		stools = fmt.Sprintf("failed to get Sandbox tools error: %s, please retry to get Sandbox Tools by call getSandbox Tool", err.Error())
	}
	stools = fmt.Sprintf("\nYou can use the following Tools to interact with the Sandbox:\n%s", stools)

	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{Text: fmt.Sprintf("Sandbox created, Sandbox name:%s, status:%s, %s", sbCreated.Name, sbCreated.Status, stools)},
		},
	}, nil, nil
}

func (a *Handler) ListSandboxTool(ctx context.Context, req *mcp.CallToolRequest, input *sandbox.SandboxBase) (*mcp.CallToolResult, any, error) {
	user := auth.GetUserTokenFromContext(ctx)
	if user == "" {
		return nil, nil, fmt.Errorf("failed to get user from context")
	}

	sbs, err := a.controller.List(user, "")
	if err != nil {
		return nil, nil, fmt.Errorf("failed to list sandboxes: %v", err)
	}

	sbsJson, err := json.MarshalIndent(sandbox.ToViewList(sbs), "", "  ")
	if err != nil {
		return nil, nil, fmt.Errorf("failed to marshal sandboxes: %v", err)
	}

	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{Text: fmt.Sprintf("Existing Sandboxes:\n%s", string(sbsJson))},
		},
	}, nil, nil
}

func (a *Handler) GetSandboxTool(ctx context.Context, req *mcp.CallToolRequest, input *sandbox.SandboxBase) (*mcp.CallToolResult, any, error) {
	if input.Name == "" {
		return nil, nil, fmt.Errorf("sandbox name is required")
	}

	klog.V(2).Infof("Get sandbox tool by name=%s", input.Name)

	sb, err := a.getOwnedSandbox(ctx, input.Name)
	if err != nil {
		return nil, nil, err
	}

	sbJson, err := json.Marshal(sb.ToView())
	if err != nil {
		return nil, nil, fmt.Errorf("failed to marshal sandbox %s: %v", input.Name, err)
	}

	stools, err := a.sandboxTools(ctx, input.Name)
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

func (a *Handler) DelSandboxTool(ctx context.Context, req *mcp.CallToolRequest, input *sandbox.SandboxBase) (*mcp.CallToolResult, any, error) {
	if input.Name == "" {
		return nil, nil, fmt.Errorf("sandbox name is required")
	}

	klog.V(2).Infof("Delete sandbox tool by name=%s", input.Name)

	if _, err := a.getOwnedSandbox(ctx, input.Name); err != nil {
		return nil, nil, err
	}

	err := a.controller.DeleteWithReason(input.Name, telemetry.ReasonMCP)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to delete Sandbox %s: %v", input.Name, err)
	}

	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{Text: fmt.Sprintf("Sandbox %s deleted successfully", input.Name)},
		},
	}, nil, nil
}
