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

package tests_test

//Test CreateRecorder
import (
    "log"
    "testing"

    "context"

    "github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestMCP(t *testing.T) {
    url := "http://localhost:10000/mcp"
    ctx := context.Background()

    // Create the URL for the server.
    log.Printf("Connecting to MCP server at %s", url)

    // Create an MCP client.
    client := mcp.NewClient(&mcp.Implementation{
        Name:    "mcp-client",
        Version: "1.0.0",
    }, nil)

    // Connect to the server.
    session, err := client.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: url}, nil)
    if err != nil {
        log.Fatalf("Failed to connect: %v", err)
    }
    defer session.Close()

    log.Printf("Connected to server (session ID: %s)", session.ID())

    // First, list available tools.
    log.Println("Listing available tools...")
    toolsResult, err := session.ListTools(ctx, nil)
    if err != nil {
        log.Fatalf("Failed to list tools: %v", err)
    }

    for _, tool := range toolsResult.Tools {
        log.Printf("  - %s: %s: %s\n", tool.Name, tool.Description, tool.InputSchema)
    }

    // Call the tool.
    result, err := session.CallTool(ctx, &mcp.CallToolParams{
        Name: "getSandbox",
        Arguments: map[string]any{
            "name": "aiosb-1",
        },
    })
    if err != nil {
        log.Printf("Failed to get time for %s: %v\n", "sb1", err)
    }

    // Print the result.
    for _, content := range result.Content {
        if textContent, ok := content.(*mcp.TextContent); ok {
            log.Printf("  %s", textContent.Text)
        }
    }

    log.Println("Client completed successfully")
}
