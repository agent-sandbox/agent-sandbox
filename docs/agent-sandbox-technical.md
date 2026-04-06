---
verified-commit: c90dab4
verified-date: 2026-04-06
key-files:
  - path: pkg/config/config.go
    symbols: [Config, Template, GetTemplateByName, CheckConfigmap, ShouldLoadTemplates, WatchConfigMap]
  - path: pkg/sandbox/sandbox.go
    symbols: [Sandbox, Make]
  - path: pkg/sandbox/controller.go
    symbols: [Controller, Create, GetSandbox, StreamContainerLogs, SandboxMetrics, ExecCommandInPod, DetectShell, StreamSandboxTerminal]
  - path: pkg/sandbox/pool_manager.go
    symbols: [PoolManager, StartPoolSyncing, AcquirePoolReplicaSet, adaptReplicasetToSandbox, replenishPoolAsync]
  - path: pkg/handler/handlers.go
    symbols: [Handler, StreamSandboxTerminalWS, ExecuteSandboxTerminal, DetectSandboxShell, StreamSandboxTrafficWS]
  - path: pkg/router/router.go
    symbols: [SandboxRouter]
  - path: pkg/scaler/autoscaler.go
    symbols: [Scaler, ScalingDownOfTimeout]
  - path: ui/src/pages/TemplatesConfigPage.tsx
    symbols: [TemplatesConfigPage, toSaveTemplatesPayload]
---

# Agent Sandbox - Technical Documentation

## Introduction

Agent Sandbox is a Kubernetes-native service that creates, manages, and proxies isolated container environments ("sandboxes") on demand. Each sandbox is a Kubernetes ReplicaSet with a single pod running a container image defined by a template. Sandboxes are ephemeral: they are created for a task, optionally kept warm in a pool, and automatically deleted when they time out.

The system is designed to be consumed by AI agents and automation pipelines that need disposable execution environments — a Python interpreter, a browser automation session, a desktop environment, or a custom image. Consumers interact via a REST API (compatible with E2B SDK), an MCP server, or the built-in web UI.

**Key behaviors:**

- A sandbox maps 1:1 to a Kubernetes ReplicaSet in a configured namespace.
- Templates define the container image and runtime properties. A request picks a template by name; the rest is automatic.
- Pool pre-warming keeps sandbox instances ready before they are requested, reducing cold-start time.
- A proxy layer routes HTTP traffic from `GET /sandbox/{name}/*` directly to the pod, so each sandbox can serve its own API.
- A scaler runs every 5 minutes and deletes sandboxes that have exceeded their `timeout`.
- All runtime configuration (templates and the ReplicaSet YAML template) is stored in a Kubernetes ConfigMap and hot-reloaded on change.

---

## 1. Overview

### Core Concepts

| Concept | Description |
|---------|-------------|
| Template | Named preset that defines image, port, resources, args, and pool settings |
| Sandbox | Running instance — a Kubernetes ReplicaSet + pod created from a template |
| Pool | Pre-warmed sandbox instances held ready for immediate acquisition |
| ReplicaSet template | Go `text/template` YAML used to render the Kubernetes ReplicaSet manifest |
| Activator | Records last-request/last-response timestamps per sandbox for idle tracking |
| Scaler | Background loop that deletes timed-out sandboxes every 5 minutes |

### Sandbox States

| State | Meaning |
|-------|---------|
| `creating` | ReplicaSet created, pod not yet ready |
| `running` | `ReadyReplicas == Replicas` in the ReplicaSet |
| `ready` | Alias used by pool — startup command completed |
| `paused` | Reserved (scale-down to 0 replicas) |
| `unready` | Pod exists but not passing readiness checks |

### Default Resource Values

| Field | Default |
|-------|---------|
| CPU request | `50m` |
| Memory request | `100Mi` |
| CPU limit | `2000m` |
| Memory limit | `4000Mi` |
| Timeout | 1800 s (30 min) |
| Idle timeout | `-1` (disabled) |
| Port | `8080` |

---

## 2. Template System

Templates are defined in `config/templates.json` and loaded into a Kubernetes ConfigMap (`agent-sandbox-templates`) at startup. Changes to the ConfigMap are hot-reloaded via a Knative configmap watcher.

### Template Fields

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `name` | string | yes | Unique identifier used when creating a sandbox |
| `image` | string | yes | Container image (full reference with tag) |
| `description` | string | yes | Human-readable purpose; also exposed to MCP tools |
| `port` | int | no | Container port for startup probe and proxy routing |
| `type` | string | no | `""` (static) or `"dynamic"` — see Dynamic Templates below |
| `pattern` | string | no | Regexp with named groups `name` and `version`; required when `type=dynamic` |
| `args` | `[]string` | no | Container `args` passed to the pod (overrides image default CMD args) |
| `envVars` | `map[string]string` | no | Template-level env vars injected into all sandboxes using this template; sandbox-supplied `envVars` take precedence |
| `shell` | string | no | Shell path used for the WebSocket terminal (e.g. `/bin/bash`). When set, the terminal uses this shell directly instead of the auto-detect fallback |
| `noStartupProbe` | bool | no | Disable the TCP startup probe (useful for images that do not listen on a port) |
| `metadata` | `map[string]string` | no | Arbitrary key/value — special keys: `runtimeClassName`, `data_vol`, `config_vol` |
| `resources.cpu` | string | no | CPU request (e.g. `"0.2"`) |
| `resources.memory` | string | no | Memory request (e.g. `"200Mi"`) |
| `resources.cpuLimit` | string | no | CPU limit |
| `resources.memoryLimit` | string | no | Memory limit |
| `pool.size` | int | no | Number of pre-warmed instances to maintain |
| `pool.probePort` | int | no | Port used for pool startup probe (overrides `port`) |
| `pool.warmupCmd` | string | no | Command to run when creating pool pods — format: `"<cmd>, <arg1> <arg2>"` |
| `pool.startupCmd` | string | no | Command run inside pool pod after acquisition before returning to caller |
| `pool.resources` | Resources | no | Separate resource limits for pool (warm) pods |

### Special Metadata Keys

| Key | Effect in sandbox.yaml |
|-----|------------------------|
| `runtimeClassName` | Sets `spec.runtimeClassName` on the pod (e.g. `gvisor`) |
| `data_vol` | Mounts a `hostPath` volume at the specified path (e.g. `/opt/data`) |
| `config_vol` | Mounts a `hostPath` volume at the specified path for config files |
| `mitm` | When `"true"`, injects a `mitm-init` init container and a `mitmproxy` sidecar for live traffic inspection (see Section 10) |

### Dynamic Templates

A dynamic template matches sandbox requests by regexp instead of exact name. Named capture groups `(?P<name>...)` and `(?P<version>...)` substitute into the `image` field.

```json
{
  "name": "code-interpreter-biz",
  "pattern": "faas-code-(?P<name>.+)\\.(?P<version>.+)$",
  "image": "ghcr.io/agent-sandbox/<name>:<version>",
  "type": "dynamic"
}
```

Request with `template: "faas-code-myapp.1.2.3"` → image `ghcr.io/agent-sandbox/myapp:1.2.3`.

Dynamic templates are skipped by the pool syncer (pools require a fixed image).

### Configuration Storage

Both config assets are stored in the same ConfigMap `agent-sandbox-templates`:

| ConfigMap key | Content |
|---------------|---------|
| `templates` | JSON array of Template objects |
| `sandbox-template` | Go `text/template` YAML for Kubernetes ReplicaSet |

**Implementation:** `pkg/config/config.go` — `CheckConfigmap()`, `ShouldLoadTemplates()`, `WatchConfigMap()`

---

## 3. Sandbox Lifecycle

```
┌─────────────────────────────────────────────────────────────────────────────────┐
│                         SANDBOX CREATE FLOW                                      │
├─────────────────────────────────────────────────────────────────────────────────┤
│                                                                                  │
│  1. HTTP POST /api/v1/sandboxes                                                  │
│     ├─ Decode JSON body into Sandbox struct                                      │
│     └─ Validate user token from Authorization header                            │
│                                                                                  │
│  2. Sandbox.Make()                                                               │
│     ├─ Resolve template by name (or image fallback → "custom")                  │
│     ├─ Copy template.Args → sandbox.Args (if sandbox has none)                  │
│     ├─ Merge template.EnvVars → sandbox.EnvVars (sandbox values win)            │
│     ├─ Merge template.Metadata → sandbox.Metadata                               │
│     ├─ Apply template resources (CPU, memory) if not set on request             │
│     ├─ Generate UUID id, compute name "sbx-{template}-{id[:20]}"                │
│     └─ Set status = "creating"                                                   │
│                                                                                  │
│  3. Controller.Create()                                                          │
│     ├─ PoolManager.AcquirePoolReplicaSet()                                       │
│     │   ├─ [POOL HIT]  Find available RS, adapt labels/annotations, return it   │
│     │   └─ [POOL MISS] Build & create new ReplicaSet via sandbox.yaml template  │
│     ├─ If from pool AND pool.startupCmd set → exec startup command in pod       │
│     └─ WaitForReplicaSetReady (polls until ReadyReplicas == 1)                  │
│                                                                                  │
│  4. Return Sandbox JSON with status = "running"                                  │
│                                                                                  │
│  EXAMPLE:                                                                        │
│     POST body: {"template": "code-interpreter"}                                  │
│     → resolves to image ghcr.io/agent-sandbox/code-interpreter:0.4.0            │
│     → name: sbx-code-interpreter-a3f9b21c04d8e1f700ab                           │
│     → K8s ReplicaSet created in namespace "agent-sandbox"                       │
│                                                                                  │
└─────────────────────────────────────────────────────────────────────────────────┘
```

**Implementation:** `pkg/sandbox/sandbox.go:Make()`, `pkg/sandbox/controller.go:Create()`

### Sandbox Deletion

- `DELETE /api/v1/sandboxes/{name}` — deletes by name
- `DELETE /api/v1/sandboxes/id/{id}` — deletes by ID
- Scaler deletes automatically on timeout (see Section 7)
- The underlying Kubernetes ReplicaSet (and its pod) is deleted; Kubernetes garbage-collects the pod

### State Stored in K8s

All sandbox state is serialized as JSON in the ReplicaSet annotation `sandbox-data`. There is no separate database. On list/get operations the server reads this annotation back.

```
┌─────────────────────────────────────────────────────────────────────────────────┐
│  ReplicaSet labels (used for filtering)                                          │
│    sbx-id       = {uuid without dashes}                                          │
│    sbx-user     = {api token}                                                    │
│    sbx-template = {template name}                                                │
│    sbx-pool     = "true" | "false"                                               │
│    sbx-time     = {unix timestamp}                                               │
│                                                                                  │
│  ReplicaSet annotation                                                           │
│    sandbox-data = {full Sandbox JSON}                                            │
└─────────────────────────────────────────────────────────────────────────────────┘
```

---

## 4. Pool Manager

The pool keeps pre-warmed ReplicaSets ready to be handed out immediately, avoiding cold-start latency.

```
┌─────────────────────────────────────────────────────────────────────────────────┐
│                         POOL SYNC FLOW                                           │
├─────────────────────────────────────────────────────────────────────────────────┤
│                                                                                  │
│  Runs every 1 minute (ticker) OR on explicit trigger after acquisition           │
│                                                                                  │
│  For each template with pool.size > 0:                                           │
│    1. List available pool RS (label sbx-pool=true, sbx-template={name})         │
│    2. Skip RS with outdated image (deletes them)                                 │
│    3. needed = pool.size - len(available)                                        │
│    4. For each missing slot:                                                     │
│       ├─ Create GetDefaultSandbox with IsPool=true                               │
│       ├─ If pool.warmupCmd set → parse "cmd, arg1 arg2" → sb.Cmd / sb.Args      │
│       └─ Call Make() + createReplicaSet()                                        │
│                                                                                  │
│  ACQUISITION (on sandbox create request):                                        │
│    1. Find available pool RS for template                                        │
│    2. If none → create fresh RS (non-pool path)                                  │
│    3. If found → adaptReplicasetToSandbox():                                     │
│       ├─ Set sbx-pool=false on labels                                            │
│       ├─ Update sbx-user, sbx-time labels                                        │
│       ├─ Replace sandbox-data annotation with actual sandbox JSON               │
│       └─ Re-render pod spec from sandbox.yaml template so metadata-driven       │
│          features (e.g. mitm sidecar injection) are applied to the acquired RS  │
│    4. Enqueue replenish trigger (refill pool async)                              │
│                                                                                  │
└─────────────────────────────────────────────────────────────────────────────────┘
```

**Implementation:** `pkg/sandbox/pool_manager.go:StartPoolSyncing()`, `AcquirePoolReplicaSet()`

**Important:** `warmupCmd` uses a comma-separator format `"<cmd>, <arg1> <arg2>"` (legacy). For new templates prefer the `args` field at the top-level template instead.

---

## 5. Kubernetes Deployment Template (sandbox.yaml)

The ReplicaSet manifest is rendered by Go `text/template` using a `SandboxKube` context:

```go
type SandboxKube struct {
    Sandbox   *Sandbox   // full sandbox object
    RawData   string     // JSON of sandbox (stored in annotation)
    Namespace string
}
```

### Template Variables Reference

| Variable | Source |
|----------|--------|
| `{{.Sandbox.Name}}` | Generated name `sbx-{template}-{id}` |
| `{{.Namespace}}` | `SANDBOX_NAMESPACE` env var |
| `{{.RawData}}` | JSON-serialized Sandbox (stored in `sandbox-data` annotation) |
| `{{.Sandbox.Image}}` | From template or request |
| `{{.Sandbox.Cmd}}` | Optional container command override |
| `{{.Sandbox.Args}}` | Container args (from template `args` field) |
| `{{.Sandbox.Port}}` | Container port |
| `{{.Sandbox.EnvVars}}` | Map of env vars from request |
| `{{.Sandbox.CPU}}` / `{{.Sandbox.Memory}}` | Resource requests |
| `{{.Sandbox.CPULimit}}` / `{{.Sandbox.MemoryLimit}}` | Resource limits |
| `{{.Sandbox.Metadata "runtimeClassName"}}` | Optional runtime class |
| `{{.Sandbox.Metadata "data_vol"}}` | Optional data volume mount path |
| `{{.Sandbox.Metadata "config_vol"}}` | Optional config volume mount path |
| `{{.Sandbox.TemplateObj.NoStartupProbe}}` | Disable TCP startup probe |

### Conditional Sections

| Condition | Effect |
|-----------|--------|
| `{{if .Sandbox.Cmd}}` | Adds `command:` to container spec |
| `{{if .Sandbox.Args}}` | Adds `args:` list to container spec |
| `{{if .Sandbox.EnvVars}}` | Adds extra `env:` entries |
| `{{if or data_vol config_vol}}` | Adds `volumeMounts:` and `volumes:` |
| `{{if not .Sandbox.TemplateObj.NoStartupProbe}}` | Adds TCP `startupProbe` on the sandbox port |
| `{{if index .Sandbox.Metadata "mitm"}}` | Injects `mitm-init` init container and `mitmproxy` sidecar (see Section 10) |

The rendered YAML is validated at save time (`SaveSandboxTemplateConfig`) by parsing it as a `appsv1.ReplicaSet` struct — invalid YAML is rejected before being stored.

---

## 6. HTTP API Endpoints

Base path: `/api/v1`

### Sandbox Endpoints

| Method | Route | Description |
|--------|-------|-------------|
| `POST` | `/sandboxes` | Create sandbox |
| `GET` | `/sandboxes` | List sandboxes for current token |
| `GET` | `/sandboxes/{name}` | Get sandbox by name |
| `DELETE` | `/sandboxes/{name}` | Delete sandbox by name |
| `DELETE` | `/sandboxes/id/{id}` | Delete sandbox by ID |
| `GET` | `/sandboxes/{name}/logs` | Get container logs (`?tailLines=N`) |
| `GET` | `/sandboxes/{name}/events` | List ReplicaSet events |
| `POST` | `/sandboxes/{name}/terminal` | Execute a shell command (one-shot) |
| `GET` | `/sandboxes/{name}/terminal/ws` | WebSocket interactive terminal |
| `GET` | `/terminal/sandbox/{name}/detect-shell` | Detect the first usable shell in the sandbox container (returns `{"shell": "/bin/bash"}`) |
| `GET` | `/sandboxes/{name}/files` | List files (`?path=/some/dir`) |
| `POST` | `/sandboxes/{name}/files` | Upload file (`multipart/form-data`, `?path=`) |
| `GET` | `/sandboxes/{name}/files/download` | Download file (`?path=/file`) |
| `DELETE` | `/sandboxes/{name}/files` | Delete file (`?path=/file`) |
| `POST` | `/sandboxes/{name}/metrics` | Get CPU/memory metrics for a list of sandboxes |
| `GET` | `/traffic/sandbox/{name}/ws` | WebSocket live traffic stream (requires `mitm=true` metadata) |

### Proxy Endpoint

| Method | Route | Description |
|--------|-------|-------------|
| `ANY` | `/sandbox/{name}/*` | Reverse-proxy to pod IP on sandbox port (`?port=N` to override) |

### Config Endpoints

| Method | Route | Description |
|--------|-------|-------------|
| `GET` | `/config/templates` | Get raw templates JSON from ConfigMap |
| `POST` | `/config/templates` | Save templates JSON to ConfigMap (triggers hot reload) |
| `GET` | `/config/sandbox-template` | Get sandbox.yaml template string |
| `POST` | `/config/sandbox-template` | Save sandbox.yaml (validated before save) |

### Pool Endpoints

| Method | Route | Description |
|--------|-------|-------------|
| `GET` | `/pool` | List all pool templates with ready counts |
| `GET` | `/pool/{name}` | List pool sandboxes for a template |
| `DELETE` | `/pool/{name}` | Delete all pool sandboxes for a template |

### MCP Endpoint

| Method | Route | Description |
|--------|-------|-------------|
| `ANY` | `/mcp` | Streamable HTTP MCP server (tools: createSandbox, getSandbox, listSandbox, deleteSandbox, sandboxExecutor) |

### E2B-Compatible Endpoints

| Method | Route | Description |
|--------|-------|-------------|
| `POST` | `/v1/sandboxes` | E2B-compatible create |
| `GET` | `/v1/sandboxes` | E2B-compatible list |
| `DELETE` | `/v1/sandboxes/{sandboxID}` | E2B-compatible delete |

### Authentication

All API endpoints require a Bearer token: `Authorization: Bearer <token>`.

Tokens are configured via the `API_TOKENS` environment variable (comma-separated). A built-in system token is always present. Tokens prefixed with `sys-` can list all sandboxes regardless of owner.

---

## 7. Scaler (Auto-Cleanup)

```
┌─────────────────────────────────────────────────────────────────────────────────┐
│                         TIMEOUT SCALER FLOW                                      │
├─────────────────────────────────────────────────────────────────────────────────┤
│                                                                                  │
│  Runs every 5 minutes                                                            │
│                                                                                  │
│  For each non-pool sandbox:                                                      │
│    1. Read creation time from label sbx-time (unix timestamp)                   │
│    2. If timeout == -1 → skip                                                    │
│    3. If now > createdAt + timeout → delete ReplicaSet                           │
│    4. Emit K8s Event "ScaleDownTimeout" on the ReplicaSet object                 │
│                                                                                  │
└─────────────────────────────────────────────────────────────────────────────────┘
```

**Implementation:** `pkg/scaler/timeout_scaler.go:ScalingDownOfTimeout()`

Default timeout: 1800 s (30 min). Maximum timeout: 86400 s (24 h). Set `timeout: -1` in the create request to disable.

---

## 8. Sandbox Proxy (Router)

`GET /sandbox/{name}/*` proxies the request to the pod's IP and configured port.

- The pod IP is resolved by listing pods with the label `sandbox={name}` in the sandbox namespace.
- The path prefix `/sandbox/{name}` is stripped before forwarding.
- An optional `?port=N` query parameter overrides the default port.
- The activator records `lastRequest` and `lastResponse` timestamps on each proxied call (used for future idle-timeout logic).

**Implementation:** `pkg/router/router.go`, `pkg/router/endpoint.go`

---

## 9. WebSocket Terminal

The terminal endpoint (`/api/v1/sandboxes/{name}/terminal/ws`) uses `kubectl exec` under the hood via the Kubernetes API streaming exec interface.

### WebSocket Message Protocol

| Direction | Type | Payload | Description |
|-----------|------|---------|-------------|
| Server→Client | `ready` | `data: "connected to {pod}/{container}"` | Session established |
| Client→Server | `init` / `resize` | `cols`, `rows` | Set terminal dimensions |
| Client→Server | `input` | `data: "<chars>"` | Keyboard input |
| Server→Client | `output` | `data: "<chars>"` | Terminal output |
| Client→Server | `close` | — | Client requests close |
| Server→Client | `exit` | `code: 0\|1` | Process exited |
| Server→Client | `error` | `data: "<msg>"` | Error message |
| Server→Client | `closed` | — | Session closed |

Default shell: `sh -lc "if command -v bash >/dev/null 2>&1; then exec bash -il; else exec sh -i; fi"`

If the sandbox template has a `shell` field set (e.g. `shell: "/bin/bash"`), that path is used directly as the shell command, bypassing the default detection logic.

---

## 10. Traffic Monitor (mitmproxy Sidecar)

When a sandbox is created with `metadata.mitm=true`, two extra containers are injected into the pod by the `sandbox.yaml` template.

### Pod injection

```
┌──────────────────────────────────────────────────────────────────────────────┐
│  Pod (mitm=true)                                                              │
│                                                                               │
│  ┌─────────────────┐  iptables redirect    ┌────────────────────────────┐   │
│  │  sandbox        │  port 80/443 ───────▶ │  mitmproxy sidecar         │   │
│  │  (main workload)│                       │  mitmdump --mode transparent│   │
│  └─────────────────┘                       │  --listen-port 8877         │   │
│                                            │  -s /addon/logger.py        │   │
│  ┌─────────────────┐                       │  stdout: JSON lines         │   │
│  │  mitm-init      │ sets iptables rules   └────────────┬───────────────┘   │
│  │  (init, NET_ADMIN)│ before main start                │                   │
│  └─────────────────┘                                    │ kubectl logs -f    │
└─────────────────────────────────────────────────────────┼────────────────────┘
                                                          │
                                          ┌───────────────▼────────────────────┐
                                          │  StreamSandboxTrafficWS handler     │
                                          │  GET /api/v1/traffic/sandbox/{name}/ws│
                                          │  bufio.Scanner → WebSocket frames   │
                                          └───────────────┬────────────────────┘
                                                          │ WebSocket
                                          ┌───────────────▼────────────────────┐
                                          │  TrafficPage.tsx                    │
                                          │  live table: method/URL/status/     │
                                          │  duration/size, with filters        │
                                          └─────────────────────────────────────┘
```

### mitm-init init container

- Image: `alpine:3.19`
- Capability: `NET_ADMIN`
- Installs `iptables` and adds two `OUTPUT` `REDIRECT` rules:
  - TCP port 80 → 8877
  - TCP port 443 → 8877
- Excludes traffic from UID 1000 (the mitmproxy process itself) to prevent routing loops.

### mitmproxy sidecar container

- Image: `mitmproxy/mitmproxy:10`
- Runs as UID 1000
- Command: `mitmdump --mode transparent --listen-port 8877 --set ssl_insecure=true -s /addon/logger.py`
- Mounts `agent-sandbox-mitm-addon` ConfigMap at `/addon` — must exist in the sandbox namespace before any `mitm=true` sandbox is started.
- Emits one JSON line to stdout per completed HTTP/HTTPS flow via the `logger.py` addon.

### logger.py addon (ConfigMap `agent-sandbox-mitm-addon`)

The addon implements two mitmproxy hooks:

| Hook | Fires when | Output `type` |
|------|-----------|---------------|
| `response` | A full request/response cycle completes | `"flow"` |
| `error` | A flow fails (connection refused, TLS error, etc.) | `"error"` |

Each line is a JSON object flushed immediately to stdout so `kubectl logs --follow` can stream it.

### WebSocket stream

`GET /api/v1/traffic/sandbox/{name}/ws?api_key=<token>`

**Handler:** `pkg/handler/handlers.go:StreamSandboxTrafficWS`

1. Validates sandbox exists and has `metadata.mitm=true`; returns `400` otherwise.
2. Upgrades the connection to WebSocket using the shared `terminalWSUpgrader`.
3. Calls `Controller.StreamContainerLogs(ctx, name, "mitmproxy")` — a `kubectl logs --follow` stream on the `mitmproxy` container.
4. Reads lines with `bufio.Scanner`. Only lines starting with `{` (valid JSON) are forwarded as WebSocket text frames; all other output (mitmproxy startup banners, etc.) is silently dropped.
5. The stream ends when the client disconnects, the pod is deleted, or the context is cancelled.

**`Controller.StreamContainerLogs`** (`pkg/sandbox/controller.go`):
- Selects the running pod for the sandbox (falls back to first pod if none is in `Running` phase).
- Calls `kclient.CoreV1().Pods(namespace).GetLogs(podName, &PodLogOptions{Container: container, Follow: true}).Stream(ctx)`.
- Returns an `io.ReadCloser` that the handler scans line by line.

### TrafficFlow JSON schema

| Field | Type | Description |
|-------|------|-------------|
| `type` | `"flow"` \| `"error"` | Entry kind |
| `timestamp` | float | Unix timestamp of request start |
| `method` | string | HTTP verb (e.g. `GET`) |
| `url` | string | Full URL including scheme |
| `host` | string | Hostname (without port) |
| `path` | string | Request path |
| `status` | int | HTTP response status code |
| `req_size` | int | Request body size in bytes |
| `res_size` | int | Response body size in bytes |
| `req_body` | string | Request body content (UTF-8, capped at 10 KB) |
| `res_body` | string | Response body content (UTF-8, capped at 10 KB) |
| `content_type` | string | `Content-Type` response header |
| `duration_ms` | int | Round-trip time in milliseconds |
| `message` | string | Error description (only for `type=error`) |

### One-time cluster prerequisite

The `agent-sandbox-mitm-addon` ConfigMap must be applied once per cluster before any `mitm=true` sandbox is started:

```bash
kubectl apply -n agent-sandbox -f - <<'EOF'
apiVersion: v1
kind: ConfigMap
metadata:
  name: agent-sandbox-mitm-addon
  namespace: agent-sandbox
data:
  logger.py: |
    ... (see README Traffic Monitor section for full content)
EOF
```

If the ConfigMap is missing, the `mitmproxy` container will crash with a `MountVolume` error and the pod will not become ready.

### HTTPS decryption

Without additional configuration, HTTPS flows appear as `CONNECT` tunnel entries (host + timing visible, body not). To see decrypted content:

| Approach | Mechanism | Works for |
|----------|-----------|-----------|
| **CA install** | Copy mitmproxy CA cert into container trust store via `startupCmd` or custom image build | All stacks |
| **`SSLKEYLOGFILE`** | Set `envVars.SSLKEYLOGFILE=/tmp/ssl.log` — process writes TLS session keys; mitmproxy reads them | Python, Node.js, curl, Chrome (OpenSSL/NSS only; does **not** work for Go or Rust) |

See the README Traffic Monitor section for step-by-step instructions for both approaches.

---

## 11. Core Classes Reference

### Backend

| Class | Path | Purpose |
|-------|------|---------|
| `Config` | `pkg/config/config.go` | Global config loaded from env vars |
| `Template` | `pkg/config/config.go` | Template definition struct |
| `Sandbox` | `pkg/sandbox/sandbox.go` | Runtime sandbox state and make logic |
| `Controller` | `pkg/sandbox/controller.go` | CRUD operations on ReplicaSets via K8s API |
| `PoolManager` | `pkg/sandbox/pool_manager.go` | Pool pre-warming and acquisition |
| `Handler` | `pkg/handler/handlers.go` | HTTP handler methods |
| `SandboxRouter` | `pkg/router/router.go` | Reverse proxy to pod |
| `Scaler` | `pkg/scaler/autoscaler.go` | Orchestrates timeout scaler loop |

### Key Methods

| Method | Description |
|--------|-------------|
| `Sandbox.Make()` | Resolves template, merges fields, generates ID/name, sets defaults |
| `Controller.Create()` | Acquires or creates ReplicaSet, waits for readiness |
| `Controller.GetSandbox()` | Reads sandbox from ReplicaSet `sandbox-data` annotation |
| `Controller.StreamContainerLogs()` | Opens a `kubectl logs --follow` stream for a named container in a sandbox pod |
| `Controller.DetectShell()` | Probes a list of candidate shells (`sh`, `bash`, `/bin/sh`, `/bin/bash`, etc.) inside the sandbox container; returns the first one that executes successfully |
| `PoolManager.AcquirePoolReplicaSet()` | Returns pool RS or creates fresh one |
| `PoolManager.adaptReplicasetToSandbox()` | Converts pool RS to user RS (label+annotation update) |
| `PoolManager.replenishPoolAsync()` | Fills pool back to configured size |
| `Scaler.ScalingDownOfTimeout()` | Deletes expired sandboxes |
| `Handler.StreamSandboxTrafficWS()` | WebSocket handler that pipes mitmproxy JSON log lines to the client |

---

## 12. UI

The React frontend is served from `/ui/dist` (built output embedded in the Docker image).

### Pages

| Page | Route | Description |
|------|-------|-------------|
| Sandboxes | `/` | List running sandboxes with status, age, template |
| Logs | `/sandboxes/{name}/logs` | Container log viewer |
| Terminal | `/sandboxes/{name}/terminal` | WebSocket terminal (xterm.js) |
| Files | `/sandboxes/{name}/files` | File browser, upload, download, delete |
| Traffic | `/traffic` | Live HTTP/HTTPS traffic monitor (requires `mitm=true`, see Section 10) |
| Events | `/events` | Kubernetes events viewer |
| Pool List | `/pool` | Pre-warmed pool templates with ready counts |
| Pool Detail | `/pool/{name}` | Pool sandbox instances for a template |
| Templates Config | `/config/templates` | Form editor for `templates.json` |
| Sandbox Template | `/config/sandbox-template` | Raw editor for `sandbox.yaml` |

### Templates Config Form Fields

| Field | Input Type | Notes |
|-------|-----------|-------|
| Name | Text | Required |
| Image | Text | Required |
| Port | Number | Container port |
| Description | Text | |
| Type | Text | `""` or `"dynamic"` |
| Pattern | Text | Required when type=dynamic |
| No Startup Probe | Checkbox | |
| Args | Textarea | One arg per line |
| Shell | Text | Shell path for terminal (e.g. `/bin/bash`); leave blank to use default detection |
| Env Vars | Textarea | `key=value` one per line; merged with sandbox-supplied env vars (sandbox wins) |
| Metadata | Textarea | `key=value` one per line |
| Resources | Sub-form | cpu, memory, cpuLimit, memoryLimit |
| Pool Resources | Sub-form | Separate resources for warm pods |
| Pool Ready Size | Number | Read-only, reflects live count |
| Pool Size | Number | Target pool size |
| Pool Probe Port | Number | |
| Pool Warmup Command | Text | `"cmd, arg1 arg2"` format |
| Pool Startup Command | Text | Exec'd in pod after acquisition |

The editor has two modes: **Form** (structured input) and **Raw JSON** (free-text). Switching modes serializes/deserializes the current state.

**Implementation:** `ui/src/pages/TemplatesConfigPage.tsx`

---

## 13. Configuration (Environment Variables)

| Variable | Default | Description |
|----------|---------|-------------|
| `SERVER_ADDR` | `0.0.0.0:10000` | API server listen address |
| `SANDBOX_NAMESPACE` | `default` | Kubernetes namespace for sandbox ReplicaSets |
| `SANDBOX_DEFAULT_TEMPLATE` | `aio` | Template used when no template/image in request |
| `SANDBOX_DEFAULT_IMAGE` | `ghcr.io/agent-infra/sandbox:latest` | Image used with default template |
| `SANDBOX_TEMPLATES_CONFIG_FILE` | `config/templates.json` | Local file seeded to ConfigMap on first run |
| `SANDBOX_TEMPLATE_FILE` | `config/sandbox.yaml` | Local sandbox YAML seeded to ConfigMap on first run |
| `API_TOKENS` | `""` | Comma-separated additional Bearer tokens |
| `API_VERSION` | `v1` | API base path prefix |

---

## 14. Build Process

The project is built as a Docker image containing three artifacts:

| Artifact | Source | Destination in image |
|----------|--------|----------------------|
| Go binary | `go build -o agent-sandbox .` | `/app` |
| UI static files | `cd ui && npm run build` | `/ui/dist` |
| Config files | `config/` directory | `/config/` |

### build.ps1

```powershell
# 1. Build UI (Vite + TypeScript)
Push-Location ui
npm install
npm run build      # outputs to ui/dist/
Pop-Location

# 2. Cross-compile Go binary for Linux/amd64
$env:GOOS="linux"
$env:GOARCH="amd64"
go build -o agent-sandbox .

---

## 15. Extending the System

### Adding a New Template

1. Edit `config/templates.json` (or use the UI at `/config/templates`).
2. Add a new object with at minimum `name`, `image`, `description`.
3. Set `port` to the port your container listens on (used for startup probe and proxy).
4. Set `noStartupProbe: true` if the container does not listen on any TCP port.
5. Set `args` if the container needs specific command arguments.
6. Add `metadata.runtimeClassName` for gVisor isolation.
7. Add `metadata.data_vol` / `metadata.config_vol` for persistent host-path storage.
8. Add `metadata.mitm=true` to enable transparent HTTP/HTTPS traffic inspection for all sandboxes using this template (requires the `agent-sandbox-mitm-addon` ConfigMap to be present in the namespace — see Section 10).
9. Save — the ConfigMap watcher reloads templates without a restart.

### Adding a New Template Field

1. Add the field to `Template` struct in `pkg/config/config.go`.
2. Propagate the field in `Sandbox.Make()` in `pkg/sandbox/sandbox.go` (similar to the `Args` merge block).
3. If it affects the pod spec, add it to `config/sandbox.yaml` using `{{if .Sandbox.NewField}}` guards.
4. Add the field to the TypeScript type in `ui/src/lib/api/types.ts`.
5. Add a form control in `ui/src/pages/TemplatesConfigPage.tsx`.
6. Rebuild with `build.ps1`.

### Modifying the ReplicaSet Template

Edit via the UI at `/config/sandbox-template` or directly in `config/sandbox.yaml`.
The API validates the template on save by rendering it with a sample sandbox and parsing the output as YAML — syntax errors are rejected immediately.

---

## Changelog

| Date | Commit | Changes |
|------|--------|---------|
| 2026-04-06 | c90dab4 | Added `envVars` and `shell` fields to Template; shell detection in terminal WS handler; `DetectSandboxShell` API endpoint; pool `adaptReplicasetToSandbox` now re-renders pod spec from template; UI form fields for Shell and Env Vars |

