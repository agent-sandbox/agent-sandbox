---
icon: lucide/plug
---

# API Reference

Agent-Sandbox exposes two API surfaces: a **Native REST API** and an **E2B-compatible API**. All endpoints require an API key passed in the `Authorization` header.

## Authentication

All API requests must include an API key:

```bash
Authorization: Bearer <your-api-key>
```

Default system token: `sys-2492a85b10ed4cb083b2c76b181eac96`

---

## Native REST API

Base path: `/api/v1`

### Create Sandbox

```
POST /api/v1/sandbox
```

**Request body:**

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| name | string | no | Sandbox name. Auto-generated if omitted. Lowercase letters, numbers, and `-`, max 50 chars. |
| image | string | no | Container image override. |

**Example:**

```bash
curl -X POST http://<host>/api/v1/sandbox \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer <api-key>" \
  -d '{"name":"sandbox-01"}'
```

**Response (200):**

```json
{
  "code": "0",
  "data": "Sandbox sandbox-01 created successfully"
}
```

### List Sandboxes

```
GET /api/v1/sandbox
```

**Example:**

```bash
curl http://<host>/api/v1/sandbox \
  -H "Authorization: Bearer <api-key>"
```

**Response (200):** Array of sandbox objects.

### Get Sandbox

```
GET /api/v1/sandbox/{name}
```

**Path parameters:**

| Parameter | Type | Description |
|-----------|------|-------------|
| name | string | Sandbox name |

**Response (200):** Sandbox detail object.

### Delete Sandbox

```
DELETE /api/v1/sandbox/{name}
```

**Example:**

```bash
curl -X DELETE http://<host>/api/v1/sandbox/sandbox-01 \
  -H "Authorization: Bearer <api-key>"
```

**Response (200):**

```json
{
  "code": "0",
  "data": "Sandbox sandbox-01 deleted successfully"
}
```

## E2B-Compatible API

Base path: `/e2b/v1`

These endpoints are compatible with the E2B protocol and SDK.

### Create Sandbox

```
POST /e2b/v1/sandboxes
```

**Request body:**

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| templateID | string | yes | Template identifier (e.g. `code-interpreter`). Version suffix like `-v1` is auto-stripped. Only alphanumeric, hyphens, and dots allowed. |
| timeout | int | no | Time-to-live in seconds. |
| envVars | object | no | Environment variables as key-value pairs. |
| metadata | object | no | Custom metadata as key-value pairs. |
| secure | bool | no | Enable HTTPS-only mode. |
| allowInternetAccess | bool | no | Allow outbound internet (default true). |
| autoPause | bool | no | Auto-pause on timeout instead of killing. |
| network | object | no | Network config with `allowOut`, `denyOut`, `allowPublicTraffic`, `maskRequestHost`. |
| mcp | object | no | MCP configuration. |

**Example:**

```bash
curl -X POST http://<host>/e2b/v1/sandboxes \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer <api-key>" \
  -d '{"templateID":"code-interpreter","timeout":300}'
```

**Response (201):**

```json
{
  "sandboxID": "e94466d4e94466d4",
  "clientID": "client-id-x",
  "envdVersion": "0.1.1",
  "envdAccessToken": "e94466d4e94466d4",
  "templateID": "code-interpreter",
  "trafficAccessToken": "e94466d4e94466d4",
  "domain": "example.domain.com",
  "metadata": { "name": "sandbox-abc123" },
  "cpuCount": 2000,
  "memoryMB": 4096,
  "diskSizeMB": 5120,
  "startedAt": "2026-01-27T10:00:00Z",
  "endAt": "2026-01-27T10:05:00Z",
  "state": "running"
}
```

### Get Sandbox

```
GET /e2b/v1/sandboxes/{sandboxID}
```

**Path parameters:**

| Parameter | Type | Description |
|-----------|------|-------------|
| sandboxID | string | Sandbox identifier |

**Response (200):** Same as Create Sandbox response.

**Error (404):**

```json
{
  "code": 404,
  "message": "sandbox <sandboxID> not found"
}
```

### List Sandboxes

```
GET /e2b/v1/v2/sandboxes
```

**Response (200):** Array of sandbox objects.

### Connect to Sandbox

```
POST /e2b/v1/sandboxes/{sandboxID}/connect
```

**Request body:**

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| timeout | int32 | no | Timeout in seconds from now after which the sandbox should expire. |

**Response (201):** Sandbox object (same shape as Get).

### Delete Sandbox

```
DELETE /e2b/v1/sandboxes/{sandboxID}
```

**Response (200):** Empty on success.

---

## Error Format

All API errors follow this JSON structure:

```json
{
  "code": 400,
  "message": "human-readable error description"
}
```

Common HTTP status codes:

| Status | Meaning |
|--------|---------|
| 400 | Bad request (invalid input, missing parameters) |
| 401 | Unauthorized (missing or invalid API key) |
| 404 | Sandbox not found |
| 500 | Internal server error |

---


## Sandbox tools REST API

### Terminal

```
POST /api/v1/terminal/sandbox/{name}
```

Execute a command in the sandbox.

### Files

```
GET    /api/v1/sandbox/files/{name}                # List files
POST   /api/v1/sandbox/files/{name}/upload          # Upload file
DELETE /api/v1/sandbox/files/{name}                  # Delete file
GET    /api/v1/sandbox/files/{name}/download         # Download file
```

### Logs

```
GET /api/v1/logs/sandbox/{name}            # Get sandbox logs
```

### List Events

```
GET /api/v1/events
```

**Response (200):** Array of sandbox events.

## Config API

```
GET  /api/v1/config/templates              # Get templates config
POST /api/v1/config/templates              # Save templates config
GET  /api/v1/config/blueprint              # Get sandbox blueprint config
POST /api/v1/config/blueprint              # Save sandbox blueprint config
```

## Pool API

```
GET    /api/v1/pool                         # List pools
GET    /api/v1/pool/sandbox/{name}          # List pool sandboxes
DELETE /api/v1/pool/{name}                  # Delete pool
```


---

## Other Endpoints

| Path | Description |
|------|-------------|
| `/mcp` | MCP server endpoint (Streamable HTTP) |
| `/sandbox/{name}/` | Sandbox container router |
| `/sandboxes/router/{sandboxID}/{port}/` | E2B-style sandbox port router |
| `/healthz` | Health check, returns `{"status":"ok","version":"..."}` |
| `/ui/` | Web UI static files |
